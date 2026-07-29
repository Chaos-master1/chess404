package match

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/engine"
	"github.com/chess404/realtime/internal/logging"
	"github.com/chess404/realtime/internal/metrics"
)

const (
	rulesVersion                 = "v1-alpha-foundation"
	defaultClock                 = int64(10 * 60 * 1000)
	maxHandSize                  = 10
	drawFromRound                = 8
	drawEveryRounds              = 3
	presenceHeartbeatTimeout     = 25 * time.Second
	disconnectGracePeriod        = 45 * time.Second
	disconnectGraceBothPeriod    = 2 * time.Minute
	disconnectGraceBoth          = "both"
	maxIntentsPerSecondPerPlayer = 10
	matchMapShards               = 32
)

type matchShard struct {
	mu      sync.RWMutex
	matches map[string]*matchContainer
}

var (
	ErrMatchNotFound     = errors.New("match not found")
	ErrMatchSeatFull     = errors.New("match has no open seats")
	ErrMatchJoinFinished = errors.New("match is finished")
	// ErrUnauthorizedSeatClaim is returned when a caller matches a seat's guest
	// ID but cannot prove ownership with that seat's player secret.
	ErrUnauthorizedSeatClaim = errors.New("unauthorized seat claim")
	ErrStaleClientState  = errors.New("client state is stale; refresh from latest snapshot")
)

type matchContainer struct {
	mu       sync.Mutex
	state    *contracts.MatchState
	events   []contracts.ResolvedEvent
	presence *matchPresenceState
	subs     map[chan contracts.MatchSnapshotResponse]string
	seqNum   int64
	computer *engine.ComputerOpponent
}

func newMatchContainer(state *contracts.MatchState, events []contracts.ResolvedEvent, presence *matchPresenceState) *matchContainer {
	return &matchContainer{
		state:    state,
		events:   events,
		presence: presence,
		subs:     make(map[chan contracts.MatchSnapshotResponse]string),
	}
}

type computerMoveTask struct {
	c   *matchContainer
	now time.Time
}

type matchMap struct {
	shards [matchMapShards]*matchShard
}

func newMatchMap() *matchMap {
	mm := &matchMap{}
	for i := 0; i < matchMapShards; i++ {
		mm.shards[i] = &matchShard{matches: make(map[string]*matchContainer)}
	}
	return mm
}

func (mm *matchMap) shardFor(matchID string) *matchShard {
	h := sha256.Sum256([]byte(matchID))
	idx := int(h[0]) % matchMapShards
	return mm.shards[idx]
}

func (mm *matchMap) Load(matchID string) (*matchContainer, bool) {
	s := mm.shardFor(matchID)
	s.mu.RLock()
	c, ok := s.matches[matchID]
	s.mu.RUnlock()
	return c, ok
}

func (mm *matchMap) Store(matchID string, c *matchContainer) {
	s := mm.shardFor(matchID)
	s.mu.Lock()
	s.matches[matchID] = c
	s.mu.Unlock()
}

func (mm *matchMap) Delete(matchID string) {
	s := mm.shardFor(matchID)
	s.mu.Lock()
	delete(s.matches, matchID)
	s.mu.Unlock()
}

func (mm *matchMap) Len() int {
	total := 0
	for i := 0; i < matchMapShards; i++ {
		mm.shards[i].mu.RLock()
		total += len(mm.shards[i].matches)
		mm.shards[i].mu.RUnlock()
	}
	return total
}

func (mm *matchMap) Range(fn func(matchID string, c *matchContainer) bool) {
	for i := 0; i < matchMapShards; i++ {
		mm.shards[i].mu.RLock()
		for id, c := range mm.shards[i].matches {
			if !fn(id, c) {
				mm.shards[i].mu.RUnlock()
				return
			}
		}
		mm.shards[i].mu.RUnlock()
	}
}

func (mm *matchMap) RangeLocked(fn func(matchID string, c *matchContainer)) {
	for i := 0; i < matchMapShards; i++ {
		mm.shards[i].mu.Lock()
		for id, c := range mm.shards[i].matches {
			fn(id, c)
		}
		mm.shards[i].mu.Unlock()
	}
}

type Service struct {
	mu               sync.Mutex
	matches          *matchMap
	archive          MatchArchiver
	store            MatchStore
	broadcaster      Broadcaster
	stopCh           chan struct{}
	authTokens       map[string]authTokenEntry
	tokenStore       TokenStore
	Log              *logging.Logger
	computerCh       chan computerMoveTask
	computerWorkerWg sync.WaitGroup

	// instanceID tags every snapshot this process publishes to the shared
	// broadcaster so relayRedisBroadcasts can recognize and skip its own
	// process's publishes -- without it, a process that both mutates a match
	// and relays for it would deliver every broadcast to its local
	// subscribers twice.
	instanceID string

	relayMu      sync.Mutex
	relayStarted map[string]bool
}

type authTokenEntry struct {
	PlayerID     string
	PlayerSecret string
	ExpiresAt    time.Time
}

type matchPresenceState struct {
	WhiteLastSeenAt         time.Time
	BlackLastSeenAt         time.Time
	WhiteConnected          bool
	BlackConnected          bool
	DisconnectGraceFor      string
	DisconnectGraceDeadline *time.Time
	WhiteLastIntentAt       time.Time
	BlackLastIntentAt       time.Time
	WhiteTokens             float64
	WhiteLastRefill         time.Time
	BlackTokens             float64
	BlackLastRefill         time.Time
}

type MatchArchiver interface {
	Upsert(snapshot contracts.MatchSnapshotResponse) error
}

type MatchArchiveLoader interface {
	MatchArchiver
	LoadMatch(matchID string) (contracts.MatchState, []contracts.ResolvedEvent, bool)
}

type MatchArchiveBootstrapper interface {
	MatchArchiveLoader
	ListUnfinishedMatchIDs(limit int) []string
}

type ServiceStats struct {
	LoadedMatches     int `json:"loadedMatches"`
	ActiveMatches     int `json:"activeMatches"`
	FinishedMatches   int `json:"finishedMatches"`
	SubscriberCount   int `json:"subscriberCount"`
	BufferedEventSets int `json:"bufferedEventSets"`
}

func NewService() *Service {
	return NewServiceWithArchive(nil)
}

func NewServiceWithArchive(archive MatchArchiver) *Service {
	return NewServiceWithStoreAndBroadcaster(archive, NewMemoryMatchStore(), NoopBroadcaster{})
}

func NewServiceWithStoreAndBroadcaster(archive MatchArchiver, store MatchStore, broadcaster Broadcaster) *Service {
	return NewServiceWithStoreBroadcasterAndTokenStore(archive, store, broadcaster, nil)
}

func NewServiceWithStoreBroadcasterAndTokenStore(archive MatchArchiver, store MatchStore, broadcaster Broadcaster, tokenStore TokenStore) *Service {
	service := &Service{
		matches:      newMatchMap(),
		archive:      archive,
		store:        store,
		broadcaster:  broadcaster,
		stopCh:       make(chan struct{}),
		authTokens:   make(map[string]authTokenEntry),
		tokenStore:   tokenStore,
		Log:          logging.New("match-service"),
		computerCh:   make(chan computerMoveTask, 100),
		instanceID:   newInstanceID(),
		relayStarted: make(map[string]bool),
	}
	if loader, ok := archive.(MatchArchiveBootstrapper); ok {
		service.restoreArchivedMatchesLocked(loader)
	}

	go service.startBroadcaster()
	go service.startGC()
	go service.cleanupAuthTokensLoop()
	numWorkers := runtime.NumCPU()
	if numWorkers < 2 {
		numWorkers = 2
	}
	for i := 0; i < numWorkers; i++ {
		service.computerWorkerWg.Add(1)
		go service.computerWorker()
	}
	service.Log.Info("computer worker pool started", "workers", numWorkers)

	return service
}

func newInstanceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "inst-" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return "inst-" + hex.EncodeToString(b)
}

func (s *Service) getMatchContainer(matchID string) *matchContainer {
	c, _ := s.matches.Load(matchID)
	return c
}

func (s *Service) GetMatch(matchID string) (contracts.MatchSnapshotResponse, error) {
	s.mu.Lock()
	c, ok := s.ensureMatchLoadedLocked(matchID)
	s.mu.Unlock()
	if !ok {
		return contracts.MatchSnapshotResponse{}, ErrMatchNotFound
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	return buildSnapshotWithPresence(c.state, s.ensurePresenceStateLocked(c, now), len(c.events), nil, now), nil
}

// GetMatchForViewer returns the snapshot as a specific viewer is allowed to
// see it: seat secrets stripped, and the opponent's hand / private card state
// hidden unless the caller proves seat ownership with a valid player secret.
//
// An empty playerID is treated as a spectator. A non-empty playerID with a
// secret that does not match the seat is rejected outright rather than being
// silently downgraded, so a caller cannot probe for a seat by guessing.
func (s *Service) GetMatchForViewer(matchID, playerID, playerSecret string) (contracts.MatchSnapshotResponse, error) {
	s.mu.Lock()
	c, ok := s.ensureMatchLoadedLocked(matchID)
	s.mu.Unlock()
	if !ok {
		return contracts.MatchSnapshotResponse{}, ErrMatchNotFound
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	viewerColor := ""
	if strings.TrimSpace(playerID) != "" {
		color, err := requireIntentColor(c.state, strings.TrimSpace(playerID), strings.TrimSpace(playerSecret))
		if err != nil {
			return contracts.MatchSnapshotResponse{}, err
		}
		viewerColor = color
	}

	now := time.Now().UTC()
	base := buildSnapshotWithPresence(c.state, s.ensurePresenceStateLocked(c, now), len(c.events), nil, now)
	return contracts.MatchSnapshotResponse{
		Match:        filterStateForColor(base.Match, viewerColor),
		ReplayHead:   base.ReplayHead,
		ReplayFrames: base.ReplayFrames,
		Events:       filterEventsForColor(base.Events, viewerColor),
	}, nil
}

func (s *Service) HeartbeatPresence(matchID string, req contracts.MatchPresenceRequest, now time.Time) error {
	s.mu.Lock()
	c, ok := s.ensureMatchLoadedLocked(matchID)
	s.mu.Unlock()
	if !ok {
		return ErrMatchNotFound
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.Status == "finished" {
		return nil
	}

	color, err := requireIntentColor(c.state, strings.TrimSpace(req.PlayerID), strings.TrimSpace(req.PlayerSecret))
	if err != nil {
		return err
	}

	presence := s.ensurePresenceStateLocked(c, now)
	presenceHeartbeat(presence, color, now)
	return nil
}

func (s *Service) MarkDisconnected(matchID string, playerID string, playerSecret string, now time.Time) error {
	s.mu.Lock()
	c, ok := s.ensureMatchLoadedLocked(matchID)
	s.mu.Unlock()
	if !ok || c.state.Status == "finished" {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	color, err := requireIntentColor(c.state, strings.TrimSpace(playerID), strings.TrimSpace(playerSecret))
	if err != nil {
		return err
	}

	presence := s.ensurePresenceStateLocked(c, now)
	if color == "white" {
		if !presence.WhiteConnected {
			return nil
		}
		presence.WhiteLastSeenAt = time.Time{}
		presence.WhiteConnected = false
	} else {
		if c.state.ModeID == contracts.MatchModeComputer {
			return nil
		}
		if !presence.BlackConnected {
			return nil
		}
		presence.BlackLastSeenAt = time.Time{}
		presence.BlackConnected = false
	}

	snapshot := buildSnapshotWithPresence(c.state, presence, len(c.events), nil, now)
	s.broadcastLocked(c, snapshot)
	return nil
}

// redactPlayerSecret returns a short fingerprint of a player
// secret so logs are useful for debugging without exposing the full
// secret. Empty string becomes "<empty>".
func redactPlayerSecret(s string) string {
	if s == "" {
		return "<empty>"
	}
	if len(s) <= 6 {
		return s[:1] + "***"
	}
	return s[:6] + "...len=" + strconv.Itoa(len(s))
}

// Subscribe attaches a snapshot stream for a viewer. playerSecret must prove
// ownership of the seat identified by playerID; without it a caller could pass
// any opponent's guest ID (which is public in every snapshot) and be served
// that seat's private hand for the rest of the match.
func (s *Service) Subscribe(matchID string, playerID string, playerSecret string) (<-chan contracts.MatchSnapshotResponse, func(), contracts.MatchSnapshotResponse, error) {
	s.mu.Lock()
	c, ok := s.ensureMatchLoadedLocked(matchID)
	s.mu.Unlock()
	if !ok {
		return nil, nil, contracts.MatchSnapshotResponse{}, ErrMatchNotFound
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.subs == nil {
		c.subs = make(map[chan contracts.MatchSnapshotResponse]string)
	}

	const maxSubscribersPerMatch = 50
	if len(c.subs) >= maxSubscribersPerMatch {
		return nil, nil, contracts.MatchSnapshotResponse{}, errors.New("max subscribers reached for match")
	}

	// Resolve the seat through the same constant-time secret check the intent
	// path uses. Identity alone is not sufficient: guest IDs are public.
	playerColor := ""
	if strings.TrimSpace(playerID) != "" {
		color, err := requireIntentColor(c.state, strings.TrimSpace(playerID), strings.TrimSpace(playerSecret))
		if err != nil {
			return nil, nil, contracts.MatchSnapshotResponse{}, err
		}
		playerColor = color
	}

	ch := make(chan contracts.MatchSnapshotResponse, 128)
	c.subs[ch] = playerColor

	now := time.Now().UTC()
	baseInitial := buildSnapshotWithPresence(c.state, s.ensurePresenceStateLocked(c, now), len(c.events), c.events, now)
	initial := contracts.MatchSnapshotResponse{
		Match:      filterStateForColor(baseInitial.Match, playerColor),
		ReplayHead: baseInitial.ReplayHead,
		Events:     filterEventsForColor(baseInitial.Events, playerColor),
	}

	unsubscribe := func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if _, present := c.subs[ch]; present {
			delete(c.subs, ch)
			close(ch)
		}
	}

	return ch, unsubscribe, initial, nil
}

func (s *Service) ensureMatchLoadedLocked(matchID string) (*matchContainer, bool) {
	if c, ok := s.matches.Load(matchID); ok {
		return c, true
	}

	restored, events, presence, ok := s.resolveMatchStateLocked(matchID)
	if !ok {
		return nil, false
	}

	if len(restored.History) == 0 {
		restored.History = []contracts.PositionState{capturePositionState(&restored)}
	}

	return s.loadMatchContainerLocked(matchID, restored, events, presence), true
}

func (s *Service) restoreArchivedMatchesLocked(loader MatchArchiveBootstrapper) {
	for _, matchID := range loader.ListUnfinishedMatchIDs(0) {
		if _, ok := s.matches.Load(matchID); ok {
			continue
		}
		// resolveMatchStateLocked prefers Redis when it has fresher data for
		// an ID the archive already told us is unfinished; it falls back to
		// this same archive row when Redis has nothing (TTL'd out, or the
		// match predates Redis being wired at all).
		restored, events, presence, ok := s.resolveMatchStateLocked(matchID)
		if !ok {
			continue
		}
		if len(restored.History) == 0 {
			restored.History = []contracts.PositionState{capturePositionState(&restored)}
		}
		s.loadMatchContainerLocked(matchID, restored, events, presence)
	}
}

// resolveMatchStateLocked tries the shared Redis store before the archive.
// Redis is the hot cross-instance layer written on every mutation
// (saveToRedis) with a short TTL; the archive (Postgres/SQLite/file) is
// written on the same cadence but is slower, and for the file/sqlite backends
// is not shared across instances at all. Preferring Redis means an instance
// that never handled this match's mutations still sees the latest state
// instead of a potentially-stale or entirely local-only archive row.
func (s *Service) resolveMatchStateLocked(matchID string) (contracts.MatchState, []contracts.ResolvedEvent, *matchPresenceState, bool) {
	if restored, events, presence, ok := s.hydrateFromRedisLocked(matchID); ok {
		return restored, events, presence, true
	}

	loader, ok := s.archive.(MatchArchiveLoader)
	if !ok {
		return contracts.MatchState{}, nil, nil, false
	}
	restored, events, ok := loader.LoadMatch(matchID)
	if !ok {
		return contracts.MatchState{}, nil, nil, false
	}
	return restored, events, nil, true
}

// hydrateFromRedisLocked rebuilds match state from a single Redis read.
// SaveState stores the full contracts.MatchSnapshotResponse -- Match (board,
// hands, seat secrets, position history) plus Events -- so LoadState alone is
// sufficient; the separate SaveHistory/SaveEvents keys and the hashed
// SaveSecrets key are not read here (SaveSecrets stores an HMAC, not the
// plaintext, so it cannot authenticate a caller-supplied secret and is not
// usable for this purpose). LoadPresence is read separately because presence
// (connection/heartbeat/rate-limit state) is not part of MatchState at all.
func (s *Service) hydrateFromRedisLocked(matchID string) (contracts.MatchState, []contracts.ResolvedEvent, *matchPresenceState, bool) {
	if s.store == nil {
		return contracts.MatchState{}, nil, nil, false
	}

	var snapshot contracts.MatchSnapshotResponse
	if err := s.store.LoadState(matchID, &snapshot); err != nil || snapshot.Match.MatchID == "" {
		return contracts.MatchState{}, nil, nil, false
	}

	var presence *matchPresenceState
	if data, err := s.store.LoadPresence(matchID); err == nil && len(data) > 0 {
		var p matchPresenceState
		if json.Unmarshal(data, &p) == nil {
			presence = &p
		}
	}

	return snapshot.Match, snapshot.Events, presence, true
}

func (s *Service) loadMatchContainerLocked(matchID string, restored contracts.MatchState, events []contracts.ResolvedEvent, presence *matchPresenceState) *matchContainer {
	// Restore SeenClientMoveIDs from Redis store if available
	if s.store != nil {
		if data, err := s.store.LoadSeenClientMoveIDs(matchID); err == nil && len(data) > 0 {
			var ids []string
			if json.Unmarshal(data, &ids) == nil {
				restored.SeenClientMoveIDs = ids
			}
		}
	}
	if presence == nil {
		presence = newRecoveredMatchPresenceState(&restored)
	}
	c := newMatchContainer(&restored, append([]contracts.ResolvedEvent{}, events...), presence)
	if s.store != nil {
		if seq, err := s.store.LoadSeq(matchID); err == nil {
			c.seqNum = seq
		}
	}
	s.matches.Store(matchID, c)

	// This instance did not create the match (CreateMatch stores directly,
	// bypassing this function), so it has no other way to learn about future
	// mutations made elsewhere. Relay Redis broadcasts into this container's
	// local subscribers so a spectator or player whose connection landed on
	// this instance still sees a live match being played on another one.
	// CreateMatch subscribes too, for the same reason in the other direction.
	s.ensureRedisRelay(matchID)

	return c
}

const authTokenTTL = 5 * time.Minute

func (s *Service) CreateAuthToken(playerID, playerSecret string, now time.Time) string {
	raw := make([]byte, 16)
	var token string
	if _, err := rand.Read(raw); err != nil {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s_%s_%d", playerID, playerSecret, now.UnixNano())))
		token = "at_" + hex.EncodeToString(h[:16])
	} else {
		token = "at_" + hex.EncodeToString(raw)
	}
	entry := authTokenEntry{
		PlayerID:     playerID,
		PlayerSecret: playerSecret,
		ExpiresAt:    now.Add(authTokenTTL),
	}
	if s.tokenStore != nil {
		if err := s.tokenStore.Create(token, entry, authTokenTTL); err != nil {
			s.Log.Error("failed to store auth token in redis, falling back to memory", "error", err)
		} else {
			return token
		}
	}
	s.mu.Lock()
	s.authTokens[token] = entry
	s.mu.Unlock()
	return token
}

func (s *Service) ResolveAuthToken(token string) (string, string, bool) {
	if token == "" {
		return "", "", false
	}
	if s.tokenStore != nil {
		entry, ok, err := s.tokenStore.Resolve(token)
		if err != nil {
			s.Log.Error("failed to resolve auth token from redis, falling back to memory", "error", err)
		} else if ok {
			return entry.PlayerID, entry.PlayerSecret, true
		} else if !ok && err == nil {
			return "", "", false
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.authTokens[token]
	if !ok {
		return "", "", false
	}
	if time.Now().After(entry.ExpiresAt) {
		delete(s.authTokens, token)
		return "", "", false
	}
	delete(s.authTokens, token)
	return entry.PlayerID, entry.PlayerSecret, true
}

func (s *Service) cleanupAuthTokensLoop() {
	if s.tokenStore != nil {
		return
	}
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			for token, entry := range s.authTokens {
				if now.After(entry.ExpiresAt) {
					delete(s.authTokens, token)
				}
			}
			s.mu.Unlock()
		}
	}
}

func (s *Service) Stats() ServiceStats {
	stats := ServiceStats{}
	s.matches.Range(func(_ string, c *matchContainer) bool {
		c.mu.Lock()
		stats.LoadedMatches++
		stats.BufferedEventSets++
		if c.state.Status == "finished" {
			stats.FinishedMatches++
		} else {
			stats.ActiveMatches++
		}
		stats.SubscriberCount += len(c.subs)
		c.mu.Unlock()
		return true
	})
	return stats
}

func (s *Service) Close() {
	close(s.stopCh)
}

func (s *Service) persistSnapshot(snapshot contracts.MatchSnapshotResponse) {
	if s.archive == nil {
		return
	}
	persisted := snapshot
	persisted.Match.WhiteConnected = false
	persisted.Match.BlackConnected = false
	persisted.Match.DisconnectGraceFor = ""
	persisted.Match.DisconnectGraceDeadline = nil
	if err := s.archive.Upsert(persisted); err != nil {
		s.Log.Error("failed to persist snapshot", "matchId", snapshot.Match.MatchID, "error", err)
	}
}

func (s *Service) saveToRedis(snapshot contracts.MatchSnapshotResponse, presence *matchPresenceState) {
	if s.store == nil {
		return
	}
	matchID := snapshot.Match.MatchID

	// The full snapshot -- including seat secrets -- is stored here so
	// hydrateFromRedisLocked can rebuild a container on another instance (or
	// after this one evicts it from memory) with intent auth still working.
	// This is a direct point-to-point Redis read/write, not a broadcast: the
	// same trust tier as the archive, which has always retained secrets for
	// the same restart-recovery reason.
	if err := s.store.SaveState(matchID, snapshot); err != nil {
		s.Log.Error("failed to save state to redis", "matchId", matchID, "error", err)
	}

	// Kept for existing SaveSecrets/LoadSecrets callers and tests. Hydration
	// does not use this: it stores an HMAC, not the plaintext, so it cannot
	// authenticate a caller-supplied secret.
	if err := s.store.SaveSecrets(matchID, hashSecret(snapshot.Match.WhitePlayerSecret), hashSecret(snapshot.Match.BlackPlayerSecret)); err != nil {
		s.Log.Error("failed to save secrets to redis", "matchId", matchID, "error", err)
	}

	historyData, err := json.Marshal(snapshot.Match.History)
	if err == nil {
		_ = s.store.SaveHistory(matchID, historyData)
	}

	eventsData, err := json.Marshal(snapshot.Events)
	if err == nil {
		_ = s.store.SaveEvents(matchID, eventsData)
	}

	if presence != nil {
		presenceData, err := json.Marshal(presence)
		if err == nil {
			_ = s.store.SavePresence(matchID, presenceData)
		}
	}

	if len(snapshot.Match.SeenClientMoveIDs) > 0 {
		seenIDsData, err := json.Marshal(snapshot.Match.SeenClientMoveIDs)
		if err == nil {
			_ = s.store.SaveSeenClientMoveIDs(matchID, seenIDsData)
		}
	}
}

// redisBroadcastEnvelope wraps a published snapshot with the id of the
// instance that produced it, so relayRedisBroadcasts can recognize and skip
// its own process's publishes.
type redisBroadcastEnvelope struct {
	OriginInstanceID string                          `json:"originInstanceId"`
	Snapshot         contracts.MatchSnapshotResponse `json:"snapshot"`
}

func (s *Service) publishToRedis(matchID string, snapshot contracts.MatchSnapshotResponse) {
	if s.broadcaster == nil {
		return
	}
	if _, ok := s.broadcaster.(NoopBroadcaster); ok {
		return
	}
	// Every consumer of this data -- local subscribers via broadcastLocked,
	// and cross-instance subscribers via relayRedisBroadcasts -- runs it
	// through filterStateForColor before it reaches a client, which already
	// strips secrets. Redacting here too means the plaintext secret never
	// transits Redis pub/sub at all, even on our own private channel.
	snapshot.Match = redactSeatSecrets(snapshot.Match)
	envelope := redisBroadcastEnvelope{OriginInstanceID: s.instanceID, Snapshot: snapshot}
	data, err := json.Marshal(envelope)
	if err != nil {
		s.Log.Error("failed to marshal snapshot for broadcast", "matchId", matchID, "error", err)
		return
	}
	if err := s.broadcaster.Publish(matchID, data); err != nil {
		s.Log.Error("failed to publish to redis", "matchId", matchID, "error", err)
	}
}

// ensureRedisRelay subscribes this instance to cross-instance broadcasts for
// matchID, once per matchID per process. Only called for matches reached
// through the hydrate path (loadMatchContainerLocked) -- a match created
// locally via CreateMatch never subscribes to its own channel, which is what
// keeps a single instance from receiving and re-delivering its own
// broadcasts a second time.
func (s *Service) ensureRedisRelay(matchID string) {
	if _, ok := s.broadcaster.(NoopBroadcaster); ok {
		return
	}

	s.relayMu.Lock()
	if s.relayStarted == nil {
		s.relayStarted = make(map[string]bool)
	}
	if s.relayStarted[matchID] {
		s.relayMu.Unlock()
		return
	}
	s.relayStarted[matchID] = true
	s.relayMu.Unlock()

	ch := s.broadcaster.Subscribe(matchID)
	if ch == nil {
		s.relayMu.Lock()
		delete(s.relayStarted, matchID)
		s.relayMu.Unlock()
		return
	}
	go s.relayRedisBroadcasts(matchID, ch)
}

func (s *Service) relayRedisBroadcasts(matchID string, ch <-chan []byte) {
	for data := range ch {
		var envelope redisBroadcastEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			s.Log.Error("redis relay: failed to unmarshal broadcast envelope", "matchId", matchID, "error", err)
			continue
		}
		if envelope.OriginInstanceID == s.instanceID {
			continue
		}
		s.deliverRelayedSnapshot(matchID, envelope.Snapshot)
	}
	s.relayMu.Lock()
	delete(s.relayStarted, matchID)
	s.relayMu.Unlock()
}

// deliverRelayedSnapshot pushes a snapshot produced by another instance to
// this instance's local subscribers only. It does not republish (that would
// create an infinite relay loop across instances) and does not mint a new
// seq (the snapshot already carries the seq its origin assigned via
// nextSeqNum) -- it only advances the local cache so ApplyIntent's staleness
// check reflects the latest known global sequence even on instances that
// never produced a broadcast themselves.
func (s *Service) deliverRelayedSnapshot(matchID string, snapshot contracts.MatchSnapshotResponse) {
	c, ok := s.matches.Load(matchID)
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if snapshot.SeqNum > c.seqNum {
		c.seqNum = snapshot.SeqNum
	}
	deliverToSubscribersLocked(c, snapshot)
}

// nextSeqNum assigns the sequence number for an outgoing broadcast. It uses
// the shared store's atomic counter so seq numbers are globally monotonic
// across every instance broadcasting for this match, not just this process --
// MemoryMatchStore implements the same counter locally for single-instance/
// test runs, so this is a safe default in every configuration.
func (s *Service) nextSeqNum(c *matchContainer) int64 {
	if s.store != nil {
		if seq, err := s.store.IncSeq(c.state.MatchID); err == nil {
			c.seqNum = seq
			return seq
		}
		s.Log.Error("failed to increment seq via store, falling back to local counter", "matchId", c.state.MatchID)
	}
	c.seqNum++
	return c.seqNum
}

func (s *Service) broadcastLocked(c *matchContainer, snapshot contracts.MatchSnapshotResponse) {
	snapshot.SeqNum = s.nextSeqNum(c)

	// Strip replay frames from periodic broadcasts to reduce bandwidth.
	// Replay frames are still sent on initial Subscribe and via ApplyIntent
	// so clients can resync on reconnect.
	snapshot.ReplayFrames = nil

	s.publishToRedis(c.state.MatchID, snapshot)
	deliverToSubscribersLocked(c, snapshot)
}

func deliverToSubscribersLocked(c *matchContainer, snapshot contracts.MatchSnapshotResponse) {
	if len(c.subs) == 0 {
		return
	}

	cachedWhite := snapshot
	cachedWhite.Match = filterStateForColor(snapshot.Match, "white")
	cachedWhite.Events = filterEventsForColor(snapshot.Events, "white")
	cachedBlack := snapshot
	cachedBlack.Match = filterStateForColor(snapshot.Match, "black")
	cachedBlack.Events = filterEventsForColor(snapshot.Events, "black")
	cachedSpec := snapshot
	cachedSpec.Match = filterStateForColor(snapshot.Match, "")
	cachedSpec.Events = filterEventsForColor(snapshot.Events, "")

	for ch, color := range c.subs {
		if color == "white" {
			pushSnapshot(ch, cachedWhite)
		} else if color == "black" {
			pushSnapshot(ch, cachedBlack)
		} else {
			pushSnapshot(ch, cachedSpec)
		}
	}
}

func pushSnapshot(ch chan contracts.MatchSnapshotResponse, snapshot contracts.MatchSnapshotResponse) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("pushSnapshot: recovered panic for seq=%d: %v", snapshot.SeqNum, r)
		}
	}()
	select {
	case ch <- snapshot:
	default:
		metrics.PushSnapshotDrops.Inc()
		log.Printf("pushSnapshot: dropping event seq=%d for channel %p (buffer full) — forcing client resync", snapshot.SeqNum, ch)
		close(ch)
	}
}

func (s *Service) startBroadcaster() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case now, ok := <-ticker.C:
			if !ok {
				return
			}
			s.collectAndBroadcast(now.UTC())
		}
	}
}

const broadcastConcurrency = 20

func (s *Service) startGC() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case now, ok := <-ticker.C:
			if !ok {
				return
			}
			s.gcFinishedMatches(now.UTC())
		}
	}
}

func (s *Service) collectAndBroadcast(now time.Time) {
	sem := make(chan struct{}, broadcastConcurrency)
	var wg sync.WaitGroup

	// Snapshot the container set before fanning out. Acquiring the broadcast
	// semaphore inside Range would block while holding a shard RLock, so one
	// slow WebSocket write would stall match creation on that whole shard.
	containers := make([]*matchContainer, 0, s.matches.Len())
	s.matches.Range(func(_ string, c *matchContainer) bool {
		containers = append(containers, c)
		return true
	})

	for _, c := range containers {
		sem <- struct{}{}
		wg.Add(1)
		go func(mc *matchContainer) {
			defer func() {
				<-sem
				wg.Done()
			}()
			s.processMatchBroadcast(mc, now)
		}(c)
	}

	wg.Wait()
}

func (s *Service) processMatchBroadcast(c *matchContainer, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state.Status == "finished" {
		return
	}

	presence := s.ensurePresenceStateLocked(c, now)

	recentCutoff := now.Add(-presenceHeartbeatTimeout)
	hasRecentActivity := (!presence.WhiteLastSeenAt.IsZero() && presence.WhiteLastSeenAt.After(recentCutoff)) ||
		(!presence.BlackLastSeenAt.IsZero() && presence.BlackLastSeenAt.After(recentCutoff))
	if hasRecentActivity {
		timeoutEvents := syncClockForMutation(c.state, now)
		if len(timeoutEvents) > 0 {
			c.events = append(c.events, timeoutEvents...)
			s.broadcastLocked(c, buildSnapshotWithPresence(c.state, presence, len(c.events), timeoutEvents, now))
		}
		s.persistSnapshot(buildSnapshot(c.state, len(c.events), c.events, now))
		if len(timeoutEvents) > 0 {
			return
		}
	}

	runtimeEvents := evaluatePresenceRuntime(c.state, presence, now)
	if len(runtimeEvents) > 0 {
		c.events = append(c.events, runtimeEvents...)
		s.persistSnapshot(buildSnapshot(c.state, len(c.events), c.events, now))
		s.broadcastLocked(c, buildSnapshotWithPresence(c.state, presence, len(c.events), runtimeEvents, now))
		return
	}
	if len(c.subs) == 0 {
		return
	}
	s.broadcastLocked(c, buildSnapshotWithPresence(c.state, presence, len(c.events), nil, now))
}

func (s *Service) computerWorker() {
	defer s.computerWorkerWg.Done()
	for {
		select {
		case <-s.stopCh:
			return
		case task := <-s.computerCh:
			task.c.mu.Lock()
			s.autoPlayComputerDepthLimited(task.c, task.now, 0)
			s.ensureComputerMadeProgressLocked(task.c, task.now)
			task.c.mu.Unlock()
		}
	}
}

func (s *Service) gcFinishedMatches(now time.Time) {
	const finishedMatchTTL = 30 * time.Minute
	const waitingMatchTTL = 30 * time.Minute

	// Collect first, delete after Range returns. Range holds the shard's
	// RLock across the callback, and Delete takes that same shard's write
	// lock -- calling Delete from inside Range self-deadlocks the goroutine
	// and leaves the shard mutex permanently held, which wedges every
	// Load/Store/Range on that shard for the lifetime of the process.
	var stale []string
	s.matches.Range(func(matchID string, c *matchContainer) bool {
		c.mu.Lock()
		status := c.state.Status
		updatedAt := c.state.UpdatedAt
		c.mu.Unlock()

		switch status {
		case "finished":
			if now.Sub(updatedAt) >= finishedMatchTTL {
				stale = append(stale, matchID)
			}
		case "waiting":
			if now.Sub(updatedAt) >= waitingMatchTTL {
				stale = append(stale, matchID)
			}
		}
		return true
	})

	for _, matchID := range stale {
		s.matches.Delete(matchID)
	}
}


