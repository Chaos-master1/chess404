package match

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// TestComputerFirstMoveRepliesAcrossSeeds is the regression guard for the
// production "the computer never moved" stall (2026-10-07): a vs-computer
// match where black's auto-reply never arrives, the client keeps clicking,
// and every click returns 400 "cannot move out of turn". Prod evidence showed
// the worker picking the task up instantly and then never returning from
// computer.MakeMove -- a hang, not a slow search, and not reproducible with a
// forced seed because chooseSeed ignores request seeds. This test therefore
// plays the first move of N fresh matches (random hands, like prod) and
// requires black's reply to land within the watchdog on EVERY one. On a hang
// it dumps all goroutine stacks, which names the exact loop inside the v1
// engine. Slow by design (~seconds per match): skipped under -short.
func TestComputerFirstMoveRepliesAcrossSeeds(t *testing.T) {
	if testing.Short() {
		t.Skip("hang hunt is slow; run without -short")
	}
	const matches = 12
	const replyWatchdog = 8 * time.Second

	for i := 0; i < matches; i++ {
		i := i
		t.Run(fmt.Sprintf("match_%02d", i), func(t *testing.T) {
			service := NewService()
			defer service.Close()
			now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

			matchID := fmt.Sprintf("hang_hunt_%02d", i)
			service.CreateMatch(contracts.CreateMatchRequest{
				MatchID:           matchID,
				ModeID:            contracts.MatchModeComputer,
				Difficulty:        "medium",
				WhiteGuestID:      "guest_white",
				WhitePlayerSecret: "white-secret",
			}, now)

			// The exact flow from the prod repro: one white pawn double-push.
			resp, err := service.ApplyIntent(contracts.PlayerIntent{
				Type:         "make_move",
				MatchID:      matchID,
				PlayerID:     "guest_white",
				PlayerSecret: "white-secret",
				From:         &contracts.Square{Row: 1, Col: 4},
				To:           &contracts.Square{Row: 3, Col: 4},
			}, now.Add(time.Second))
			if err != nil {
				t.Fatalf("white's first move was rejected: %v", err)
			}
			if resp.Match.Turn != "black" {
				t.Fatalf("after white's move the turn should be black, got %q", resp.Match.Turn)
			}

			// Watchdog: black's reply must land within the budget.
			deadline := time.Now().Add(replyWatchdog)
			for time.Now().Before(deadline) {
				snap, err := service.GetMatch(matchID)
				if err != nil {
					t.Fatalf("polling the match failed: %v", err)
				}
				if snap.Match.Status != "active" {
					return // decided/finished counts as a reply
				}
				if snap.Match.Turn == "white" {
					return // computer replied
				}
				time.Sleep(200 * time.Millisecond)
			}

			stacks := make([]byte, 1<<20)
			stacks = stacks[:runtime.Stack(stacks, true)]
			t.Fatalf("computer never replied within %s (match %s) -- v1 MakeMove hang; goroutine dump:\n%s",
				replyWatchdog, matchID, stacks)
		})
	}
}
