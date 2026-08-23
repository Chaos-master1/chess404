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
		// These six all have a findBestTarget case (so it LOOKS like they're
		// handled) but are secretly two-phase in applySelectTarget --
		// findBestTarget has no notion of "which phase", so it derives the
		// same single-shot target every call. Found by xgauntlet's E0
		// cross-engine gauntlet (real ComputerOpponent-vs-ComputerOpponent
		// games through the actual match.Service failed on exactly these),
		// not by inspection -- this test previously asserted all six were
		// working. See the comment on computerPlayableMechanics for the
		// exact failure mode of each.
		"parasite", "clone", "demote", "demotehim", "promote", "promotehim",
	}
	if len(broken) != 23 {
		t.Fatalf("test setup error: expected 23 broken mechanics, listed %d", len(broken))
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
		"mindcontrol", "borrow", "invisible",
	}
	if len(working) != 14 {
		t.Fatalf("test setup error: expected 14 working mechanics, listed %d", len(working))
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

// TestFindBestTargetSkipsFrozenPieceForMindcontrolAndBorrow is a regression
// test for a bug xgauntlet's E0 cross-engine gauntlet found in a real
// ComputerOpponent-vs-ComputerOpponent game: findBestTarget's shared
// value-ranking case picked the highest-value enemy piece with no regard for
// whether it was frozen, but applySelectTarget rejects a frozen target for
// both mindcontrol and borrow (match_cards.go:666-667, :639-640). Without the
// fix, the computer would submit a select_target that always fails whenever
// its highest-value enemy target happens to be frozen, and (like every other
// mechanic in this file) the card is then abandoned without leaving hand.
func TestFindBestTargetSkipsFrozenPieceForMindcontrolAndBorrow(t *testing.T) {
	state := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	state.MatchID = "diag"
	// Freeze the only piece a naive value-ranking would pick (black's queen,
	// the highest-value non-king piece on the board) so a correct
	// implementation must fall through to a lower-value, unfrozen piece.
	for r := range state.Board {
		for c := range state.Board[r] {
			p := state.Board[r][c]
			if p != nil && p.Color == "black" && p.Type == "queen" {
				p.Frozen = true
			}
		}
	}

	co := NewComputerOpponent(DifficultyMedium, "white")
	for _, mechanic := range []string{"mindcontrol", "borrow"} {
		t.Run(mechanic, func(t *testing.T) {
			target := co.findBestTarget(state, mechanic, "white")
			if target == nil {
				t.Fatalf("expected a non-frozen fallback target, got nil")
			}
			piece := state.Board[target.Row][target.Col]
			if piece == nil || piece.Frozen {
				t.Fatalf("expected a non-frozen target, got %+v at %+v", piece, target)
			}
		})
	}
}

// TestMakeMoveNeverPlaysACardDuringAnActiveDoubleMove is a regression test
// for a bug xgauntlet's E0 cross-engine gauntlet found: MakeMove's
// card-evaluation branch never checked state.DoubleMove before deciding to
// play a card, so it could submit play_card while a double move was in
// progress -- applyPlayCard rejects that outright ("resolve the active
// double move before playing another card", match_cards.go:26-28) -- and the
// computer's second move of its own double move would silently never happen.
func TestMakeMoveNeverPlaysACardDuringAnActiveDoubleMove(t *testing.T) {
	state := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	state.MatchID = "diag"
	state.DoubleMove = &contracts.DoubleMoveState{Type: "diff", MovesLeft: 1}
	// A hand entirely of high-scoring, computer-playable cards -- if
	// state.DoubleMove is not checked, ShouldPlayCard/BestCardToPlay has
	// every reason to want to play one of these right now.
	state.WhiteHand = []contracts.GameCard{
		{ID: "c1", Mechanic: "freeze", Rarity: "rare"},
		{ID: "c2", Mechanic: "sniper", Rarity: "rare"},
	}

	co := NewComputerOpponent(DifficultyExpert, "white")
	intent := co.MakeMove(state)
	if intent == nil {
		t.Fatal("expected a real intent, got nil")
	}
	if intent.Type == "play_card" {
		t.Fatalf("computer played a card (%q) while a double move was active -- applyPlayCard will reject this and the computer's move is silently lost", intent.CardID)
	}
}
