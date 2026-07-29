package engine

import (
	"testing"

	"github.com/chess404/realtime/internal/contracts"
)

// TestComputerNeverSelectsAnUnplayableCard is a regression test for a live
// production bug: on Medium/Hard/Expert, the computer opponent degenerated
// into pushing/capturing with the a-file pawn, then the b-file, and so on --
// exactly firstLegalMoveForColor's raw board-scan order -- for the entire
// rest of the game, on essentially every game at those difficulties.
//
// Root cause: HandleSelectTarget/findBestTarget cannot produce a working
// select_target for 17 of the 37 mechanics (see the comment on
// computerPlayableMechanics). match_lifecycle.go abandons an unresolvable
// pending card without removing it from hand, so the exact same card scores
// highest again on the next turn, forever. Difficulty.ShouldPlayCard is a
// deterministic threshold with no randomness for Medium/Hard/Expert, so once
// one of these difficulties drew one of the 17, every single turn burned all
// 5 retry attempts on the same dead card and fell through to
// ensureComputerMadeProgressLocked's board-scan fallback -- which is the
// exact behavior reported.
//
// This hands the computer a hand made ENTIRELY of broken-targeting mechanics
// against an otherwise-normal position with plenty of legal chess moves, at
// every difficulty. Confirmed to fail without the fix: MakeMove returns a
// play_card intent for "lavaground" instead of a chess move.
func TestComputerNeverSelectsAnUnplayableCard(t *testing.T) {
	unplayableHand := []contracts.GameCard{
		{ID: "c1", Mechanic: "lavaground"},   // dispatched, but findBestTarget has no case
		{ID: "c2", Mechanic: "fakepiece"},    // same
		{ID: "c3", Mechanic: "shield"},       // never dispatched at all
		{ID: "c4", Mechanic: "fortress"},     // never dispatched at all
		{ID: "c5", Mechanic: "teleport"},     // never dispatched at all
		{ID: "c6", Mechanic: "swapme"},       // dispatched via a SelectionID that never validates
		{ID: "c7", Mechanic: "joker"},        // never dispatched at all
	}

	for _, name := range []string{"medium", "hard", "expert"} {
		diff := ParseDifficulty(name)
		t.Run(name, func(t *testing.T) {
			state := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
			state.MatchID = "diag"
			state.BlackHand = unplayableHand

			co := NewComputerOpponent(diff, "black")
			intent := co.MakeMove(state)

			if intent == nil {
				t.Fatal("expected a real intent, got nil")
			}
			if intent.Type == "play_card" {
				t.Fatalf("%s: computer selected an unplayable card %q -- this is exactly the bug: it will never leave hand and will be re-selected forever",
					name, intent.CardID)
			}
			if intent.Type != "make_move" {
				t.Fatalf("expected a make_move intent, got type %q", intent.Type)
			}
		})
	}
}

// TestFilterHandForComputerExcludesEveryBrokenMechanic pins the exact
// allowlist against the full 37-mechanic pool, so adding a new mechanic (or
// fixing one of the 17) has to touch this list deliberately rather than
// silently inheriting whatever computerPlayableMechanics said before.
func TestFilterHandForComputerExcludesEveryBrokenMechanic(t *testing.T) {
	broken := []string{
		"lavaground", "fakepiece", // dispatched, no findBestTarget case
		"shield", "unabomber", "fortress", "fog_village", "teleport", "jump",
		"blackhole", "joker", "smallsacrifice", "bigsacrifice", // never dispatched
		"swapme", "swapus", "swaphim", "halffuse", "fullfusion", // bogus SelectionID
	}
	if len(broken) != 17 {
		t.Fatalf("test setup error: expected 17 broken mechanics, listed %d", len(broken))
	}

	hand := make([]contracts.GameCard, len(broken))
	for i, m := range broken {
		hand[i] = contracts.GameCard{ID: m, Mechanic: m}
	}

	filtered := filterHandForComputer(hand)
	if len(filtered) != 0 {
		t.Fatalf("expected every known-broken mechanic to be filtered out, got %d survivors: %+v", len(filtered), filtered)
	}

	working := []string{
		"doublemove_same", "doublemove_diff", "undo", "reverse", "mirror",
		"gambler", "radar", "cheater", "freeze", "sniper", "badsniper",
		"demotehim", "mindcontrol", "borrow", "parasite", "demote",
		"promotehim", "promote", "clone", "invisible",
	}
	if len(working) != 20 {
		t.Fatalf("test setup error: expected 20 working mechanics, listed %d", len(working))
	}
	if len(working)+len(broken) != 37 {
		t.Fatalf("working + broken must cover all 37 mechanics, got %d", len(working)+len(broken))
	}

	workingHand := make([]contracts.GameCard, len(working))
	for i, m := range working {
		workingHand[i] = contracts.GameCard{ID: m, Mechanic: m}
	}
	if got := filterHandForComputer(workingHand); len(got) != len(working) {
		t.Fatalf("expected every known-working mechanic to survive filtering, got %d of %d", len(got), len(working))
	}
}
