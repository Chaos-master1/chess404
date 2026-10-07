package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// redactedArchiveLoader mimics the real archive: finished matches come back
// with seat secrets redacted at rest, so no presented credential can ever
// match them.
type redactedArchiveLoader struct{}

func (l *redactedArchiveLoader) Upsert(contracts.MatchSnapshotResponse) error { return nil }

func (l *redactedArchiveLoader) LoadMatch(matchID string) (contracts.MatchState, []contracts.ResolvedEvent, bool) {
	if matchID != "finished_archive" {
		return contracts.MatchState{}, nil, false
	}
	board := make([][]*contracts.Piece, 8)
	for i := range board {
		board[i] = make([]*contracts.Piece, 8)
	}
	return contracts.MatchState{
		MatchID:           matchID,
		Status:            "finished",
		Turn:              "white",
		Board:             board,
		WhiteGuestID:      "guest-white",
		BlackGuestID:      "guest-black",
		WhitePlayerSecret: "redacted",
		BlackPlayerSecret: "redacted",
	}, nil, true
}

// TestFinishedMatchSubscribeSkipsSeatProof mirrors
// TestFinishedMatchViewerFetchSkipsSeatProof for the WS transport: a finished
// match is public record, so subscribing must succeed without seat proof
// (spectator scope) exactly like the viewer fetch, while ACTIVE matches keep
// demanding valid seat credentials. Before this fix, every WS subscribe of a
// finished game whose seat secrets were gone (GC'd or archive-restored)
// returned the same unmapped 400 the GET fix removed -- the identical
// retry-forever loop, one transport over.
func TestFinishedMatchSubscribeSkipsSeatProof(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "finished_subscribe",
		WhiteGuestID: "guest-white",
		BlackGuestID: "guest-black",
	}, now)

	// While ACTIVE, bad seat credentials must still be refused.
	if _, _, _, err := service.Subscribe("finished_subscribe", "guest-white", "wrong-secret"); err == nil {
		t.Fatalf("expected an active match to refuse WS subscribe with bad seat credentials")
	}

	// White resigns -> match finished.
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "resign",
		MatchID:  "finished_subscribe",
		PlayerID: "guest-white",
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("expected white's resign to finish the match, got %v", err)
	}

	// After finish: the same credentials now yield a subscription whose
	// initial snapshot says "finished" -- what the client needs to stop.
	ch, unsubscribe, initial, err := service.Subscribe("finished_subscribe", "guest-white", "wrong-secret")
	if err != nil {
		t.Fatalf("expected a finished match to be subscribable without seat proof, got %v", err)
	}
	defer unsubscribe()
	if ch == nil {
		t.Fatalf("expected a live subscription channel")
	}
	if initial.Match.Status != "finished" {
		t.Fatalf("expected finished status in the initial snapshot, got %q", initial.Match.Status)
	}
}

// TestFinishedArchivedMatchSubscribeSkipsSeatProof pins the case that makes
// seat proof structurally impossible: the match no longer lives in memory and
// is restored from the archive, whose copy has redacted secrets. Even the
// owner's real secret cannot match -- the subscribe must still succeed.
func TestFinishedArchivedMatchSubscribeSkipsSeatProof(t *testing.T) {
	service := NewServiceWithArchive(&redactedArchiveLoader{})

	ch, unsubscribe, initial, err := service.Subscribe("finished_archive", "guest-white", "the-real-secret")
	if err != nil {
		t.Fatalf("expected an archive-restored finished match to be subscribable without seat proof, got %v", err)
	}
	defer unsubscribe()
	if ch == nil {
		t.Fatalf("expected a live subscription channel")
	}
	if initial.Match.Status != "finished" {
		t.Fatalf("expected finished status in the initial snapshot, got %q", initial.Match.Status)
	}

	// Symmetry guard: the HTTP viewer path must accept the same call.
	if _, err := service.GetMatchForViewer("finished_archive", "guest-white", "the-real-secret"); err != nil {
		t.Fatalf("expected the viewer fetch to accept the same credentials, got %v", err)
	}
}
