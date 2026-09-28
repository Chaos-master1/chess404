package match

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// Asynchronous persistence + cross-instance publish.
//
// Why this exists: the hosted deployment talks to Upstash Redis across the
// WAN (~70-90ms per round trip), and every mutation used to issue TWO
// synchronous round trips (SaveSnapshotAtomic + Publish) while holding the
// match mutex, plus the IncSeq round trip that must stay synchronous. That
// put a ~1000ms wall-clock floor on every accepted move intent in production
// (measured 2026-09-25: 942-1043ms per intent, vs ~260ms for a rejected one)
// because the board only updates when the snapshot broadcast arrives.
//
// The split now is:
//   - SYNCHRONOUS (ordering- or correctness-critical, and cheap): the IncSeq
//     mint (nextSeqNum) and the local WS subscriber delivery. The acting
//     client sees its move immediately after the intent round trip.
//   - ASYNCHRONOUS (throughput work, latest-wins safe): the archive upsert,
//     the SaveSnapshotAtomic pipeline, and the pub/sub publish. Every
//     component is a full-state overwrite, so coalescing a match's backlog
//     down to its newest job per component is always correct.
//
// Ordering invariants:
//   - Per match, exactly one worker owns the backlog at a time and runs the
//     newest state per component strictly after older ones; a match's save
//     can never overtake a newer save of the same match.
//   - Redis STATE may trail the newest broadcast by up to one background
//     flush. Consumers reconcile by seqNum (drop => forced client resync),
//     the same policy as a dropped WS frame. The seq counter itself is
//     written synchronously by IncSeq, so staleness rejection stays exact.
//     Cross-instance concurrent mutation of one live match was never a
//     supported topology (single match-service container); this widens the
//     restart/eviction hydration window by at most one flush.
//   - Terminal states (match finished) and match creation bypass the queue
//     and run inline (after draining that match's backlog), so the
//     authoritative state is in Redis before the call returns.
//   - Close() drains the queue before returning, so redeploys do not lose
//     the tail of in-flight writes.

// persistWorkerCount is the number of background IO workers. Workers are
// IO-bound (Upstash round trips); per-match serialization comes from the
// ownership protocol below, so this only caps how many matches flush in
// parallel.
const persistWorkerCount = 4

// redisSaveBundle carries the pre-marshaled components of one
// SaveSnapshotAtomic write. Marshaling happens on the mutation path (cheap,
// CPU-only) so the background worker does no allocation-heavy work.
type redisSaveBundle struct {
	state       []byte
	secretWhite string
	secretBlack string
	history     []byte
	events      []byte
	presence    []byte
	seenIDs     []byte
	// legacyState is set when the full snapshot would not marshal; the
	// worker then falls back to the older SaveState path exactly as
	// saveToRedis always did.
	legacyState *contracts.MatchSnapshotResponse
}

// asyncPersistJob is one unit of deferred IO for a match. The archive upsert
// is NOT part of deferred IO: it is a cheap in-memory write-behind that stays
// synchronous on the mutation path (tests and archive ordering depend on
// it). Only the WAN-bound components are deferred. Either component may be
// nil.
type asyncPersistJob struct {
	save    *redisSaveBundle
	publish []byte
}

func (s *Service) buildRedisSaveBundle(snapshot contracts.MatchSnapshotResponse, presence *matchPresenceState) *redisSaveBundle {
	if s.store == nil {
		return nil
	}
	b := &redisSaveBundle{
		secretWhite: hashSecret(snapshot.Match.WhitePlayerSecret),
		secretBlack: hashSecret(snapshot.Match.BlackPlayerSecret),
	}
	var err error
	if b.history, err = json.Marshal(snapshot.Match.History); err != nil {
		s.Log.Error("failed to marshal history for redis", "matchId", snapshot.Match.MatchID, "error", err)
		b.history = nil
	}
	if b.events, err = json.Marshal(snapshot.Events); err != nil {
		s.Log.Error("failed to marshal events for redis", "matchId", snapshot.Match.MatchID, "error", err)
		b.events = nil
	}
	if presence != nil {
		if b.presence, err = json.Marshal(presence); err != nil {
			s.Log.Error("failed to marshal presence for redis", "matchId", snapshot.Match.MatchID, "error", err)
			b.presence = nil
		}
	}
	if len(snapshot.Match.SeenClientMoveIDs) > 0 {
		if b.seenIDs, err = json.Marshal(snapshot.Match.SeenClientMoveIDs); err != nil {
			s.Log.Error("failed to marshal seen client move ids for redis", "matchId", snapshot.Match.MatchID, "error", err)
			b.seenIDs = nil
		}
	}
	if b.state, err = json.Marshal(snapshot); err != nil {
		// The state payload is the one component hydration cannot rebuild
		// from the others; keep the old per-component fallback.
		s.Log.Error("failed to marshal state for redis", "matchId", snapshot.Match.MatchID, "error", err)
		b.state = nil
		b.legacyState = &snapshot
	}
	return b
}

// buildPublishPayload marshals a redacted snapshot for the cross-instance
// pub/sub. Returns nil when there is no real broadcaster.
func (s *Service) buildPublishPayload(snapshot contracts.MatchSnapshotResponse) []byte {
	if s.broadcaster == nil {
		return nil
	}
	if _, ok := s.broadcaster.(NoopBroadcaster); ok {
		return nil
	}
	// Every consumer of this data -- local subscribers, cross-instance
	// relays -- runs it through filterStateForColor before it reaches a
	// client, which already strips secrets. Redacting here too means the
	// plaintext secret never transits Redis pub/sub at all, even on our own
	// private channel.
	snapshot.Match = redactSeatSecrets(snapshot.Match)
	envelope := redisBroadcastEnvelope{OriginInstanceID: s.instanceID, Snapshot: snapshot}
	data, err := json.Marshal(envelope)
	if err != nil {
		s.Log.Error("failed to marshal snapshot for broadcast", "matchId", snapshot.Match.MatchID, "error", err)
		return nil
	}
	return data
}

// queueCommit schedules one deferred-IO job. Jobs arriving after shutdown
// started run inline instead of queueing.
func (s *Service) queueCommit(matchID string, job *asyncPersistJob) {
	s.persistMu.Lock()
	if s.persistStopped {
		s.persistMu.Unlock()
		s.runPersistJob(matchID, job)
		return
	}
	s.persistQueues[matchID] = append(s.persistQueues[matchID], job)
	s.persistCond.Broadcast()
	s.persistMu.Unlock()
}

// drainMatchPersist blocks until the match has no queued or in-flight
// deferred IO. A caller about to write the match's state inline (terminal
// flush, creation) holds ordering by waiting here first: an older queued
// write can never land after a flushed terminal state and revert Redis to a
// pre-finish snapshot. Workers never take the match mutex, so a mutator
// holding c.mu cannot deadlock against them.
func (s *Service) drainMatchPersist(matchID string) {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	for len(s.persistQueues[matchID]) > 0 || s.persistOwned[matchID] {
		s.persistCond.Wait()
	}
}

// queueSave schedules the Redis snapshot save (deferred; no WAN round trip
// on the mutation path). The caller has already done the inline archive
// upsert via persistSnapshot.
func (s *Service) queueSave(snapshot contracts.MatchSnapshotResponse, presence *matchPresenceState) {
	b := s.buildRedisSaveBundle(snapshot, presence)
	if b == nil {
		return
	}
	s.queueCommit(snapshot.Match.MatchID, &asyncPersistJob{save: b})
}

// queuePublish schedules just the cross-instance publish.
func (s *Service) queuePublish(matchID string, payload []byte) {
	if payload == nil {
		return
	}
	s.queueCommit(matchID, &asyncPersistJob{publish: payload})
}

func (s *Service) runPersistJob(matchID string, job *asyncPersistJob) {
	if job.save != nil {
		s.runRedisSave(matchID, job.save)
	}
	if len(job.publish) > 0 && s.broadcaster != nil {
		if err := s.broadcaster.Publish(matchID, job.publish); err != nil {
			s.Log.Error("failed to publish to redis", "matchId", matchID, "error", err)
		}
	}
}

func (s *Service) runRedisSave(matchID string, b *redisSaveBundle) {
	if s.store == nil {
		return
	}
	if b.legacyState != nil {
		if err := s.store.SaveState(matchID, *b.legacyState); err != nil {
			s.Log.Error("failed to save state to redis", "matchId", matchID, "error", err)
		}
		return
	}
	if err := s.store.SaveSnapshotAtomic(
		matchID,
		b.state,
		b.secretWhite,
		b.secretBlack,
		b.history,
		b.events,
		b.presence,
		b.seenIDs,
	); err != nil {
		s.Log.Error("failed to save snapshot to redis", "matchId", matchID, "error", err)
	}
}

// persistWorker drains the queue. Invariant: the persistMu is HELD at the top
// of every loop iteration. Ownership protocol: a worker claims a match's
// backlog (persistOwned), merges it into one job holding the newest state per
// component (latest-wins; every component is a full overwrite), releases the
// mutex to run the IO, then re-claims the match if new jobs arrived while it
// was writing. Only an empty backlog releases ownership, so a match's writes
// are strictly ordered and never concurrent with each other. Every ownership
// release broadcasts, waking flushers parked in drainMatchPersist and Close.
func (s *Service) persistWorker() {
	defer s.persistWG.Done()
	s.persistMu.Lock()
	held := "" // match whose ownership this worker holds, "" if none
	for {
		if held != "" {
			if q := s.persistQueues[held]; len(q) > 0 {
				// New work for the match we just served: fold it into one
				// merged job (newest component wins) and write again.
				job, count := s.mergeLocked(held, q)
				s.persistMu.Unlock()
				if count > 1 {
					s.Log.Info("persist queue coalesced writes", "matchId", held, "coalesced", count-1)
				}
				s.runPersistJob(held, job)
				s.persistMu.Lock()
				continue
			}
			// Backlog fully drained: release ownership, wake drainers.
			delete(s.persistOwned, held)
			s.persistCond.Broadcast()
			held = ""
		}
		picked := ""
		for id, q := range s.persistQueues {
			if len(q) == 0 {
				delete(s.persistQueues, id)
				continue
			}
			if s.persistOwned[id] {
				continue
			}
			picked = id
			break
		}
		if picked == "" {
			if s.persistStopped {
				s.persistMu.Unlock()
				return
			}
			s.persistCond.Wait()
			continue
		}
		s.persistOwned[picked] = true
		job, count := s.mergeLocked(picked, s.persistQueues[picked])
		held = picked
		s.persistMu.Unlock()
		if count > 1 {
			s.Log.Info("persist queue coalesced writes", "matchId", held, "coalesced", count-1)
		}
		s.runPersistJob(held, job)
		s.persistMu.Lock()
	}
}

// mergeLocked folds a match's queued jobs into one job (newest component
// wins) and clears the queue. Caller holds persistMu.
func (s *Service) mergeLocked(matchID string, q []*asyncPersistJob) (*asyncPersistJob, int) {
	merged := &asyncPersistJob{}
	for _, j := range q {
		if j.save != nil {
			merged.save = j.save
		}
		if j.publish != nil {
			merged.publish = j.publish
		}
	}
	count := len(q)
	delete(s.persistQueues, matchID)
	return merged, count
}

func (s *Service) startPersistWorkers() {
	s.persistMu.Lock()
	s.persistCond = sync.NewCond(&s.persistMu)
	s.persistQueues = make(map[string][]*asyncPersistJob)
	s.persistOwned = make(map[string]bool)
	s.persistMu.Unlock()
	for i := 0; i < persistWorkerCount; i++ {
		s.persistWG.Add(1)
		go s.persistWorker()
	}
}

// persistQueueIdle reports whether all deferred IO has been flushed. Test
// helper; callers must not rely on it for correctness.
func (s *Service) persistQueueIdle() bool {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	return len(s.persistQueues) == 0 && len(s.persistOwned) == 0
}

// persistQueueWait blocks until the queue is fully drained (or the timeout
// elapses). Test helper used to make async-behavior assertions deterministic
// without sleeping.
func (s *Service) persistQueueWait(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.persistQueueIdle() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return s.persistQueueIdle()
}
