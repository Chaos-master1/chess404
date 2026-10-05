package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// TestFinishedMatchViewerFetchSkipsSeatProof is a regression test for a live
// bug: a browser tab left open on a finished game retried its GET and WS seat
// claim forever. The archived copy of a finished match is stored with seat
// secrets redacted, so seat proof could never succeed against it -- every
// viewer fetch returned an unmapped 400, the client never saw the finished
// snapshot that tells it to stop, and the loop ran every ~15s until the tab
// was closed (match-service logs: GET + seat-secret 400 pairs, 2026-10-05
// 05:10-05:11 UTC, match_1791169241627_524538ab).
//
// A finished match is public record: viewer fetches must succeed without seat
// proof (spectator scope, exactly like the history views), while ACTIVE
// matches keep demanding valid seat credentials.
func TestFinishedMatchViewerFetchSkipsSeatProof(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "finished_viewer",
		WhiteGuestID: "guest-white",
		BlackGuestID: "guest-black",
	}, now)

	// While ACTIVE, bad seat credentials must still be refused.
	if _, err := service.GetMatchForViewer("finished_viewer", "guest-white", "wrong-secret"); err == nil {
		t.Fatalf("expected an active match to refuse viewer fetch with bad seat credentials")
	}

	// White resigns -> match finished.
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "resign",
		MatchID:  "finished_viewer",
		PlayerID: "guest-white",
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("expected white's resign to finish the match, got %v", err)
	}

	// After finish: the same bad credentials now yield the snapshot instead
	// of an error -- this is what lets the client see "finished" and stop.
	resp, err := service.GetMatchForViewer("finished_viewer", "guest-white", "wrong-secret")
	if err != nil {
		t.Fatalf("expected a finished match to be viewable without seat proof, got %v", err)
	}
	if resp.Match.Status != "finished" {
		t.Fatalf("expected finished status, got %q", resp.Match.Status)
	}

	// Fully anonymous viewers get it too (spectator scope).
	if _, err := service.GetMatchForViewer("finished_viewer", "", ""); err != nil {
		t.Fatalf("expected anonymous viewer fetch of a finished match to succeed, got %v", err)
	}
}
