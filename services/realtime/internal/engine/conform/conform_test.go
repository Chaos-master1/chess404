package conform

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/engine/core"
)

func fixedNow() time.Time {
	return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
}

// emptyBoard returns an 8x8 board of nil pieces, ready for a test to place
// exactly the pieces a scenario needs.
func emptyBoard() [][]*contracts.Piece {
	board := make([][]*contracts.Piece, 8)
	for r := range board {
		board[r] = make([]*contracts.Piece, 8)
	}
	return board
}

// customState builds a valid contracts.MatchState from a real CreateMatch
// response (guaranteeing every field ApplyIntent might check is populated
// sensibly -- clock, status, hands, etc.) and then overwrites just the
// board/turn/moved/last-move fields a test scenario needs, rather than
// hand-building 30+ struct fields and guessing which ones are load-bearing.
func customState(t *testing.T, board [][]*contracts.Piece, turn string) contracts.MatchState {
	t.Helper()
	_, base := NewStandardMatch("template", fixedNow())
	base.Board = board
	base.Turn = turn
	base.Moved = []string{}
	base.LastMove = nil
	return base
}

// TestLegalSetConformanceStandardOpening is the harness's own sanity check:
// before trusting it on hand-built overlay scenarios, confirm it agrees with
// itself on the one position with a completely independent oracle (perft,
// already exhaustively verified in movegen_test.go/perft_test.go). If this
// fails, the bug is almost certainly in the conform package's conversion or
// probing logic, not in engine/core or internal/match.
func TestLegalSetConformanceStandardOpening(t *testing.T) {
	_, state := NewStandardMatch("standard_opening_probe", fixedNow())
	pos, err := ToPosition(&state)
	if err != nil {
		t.Fatalf("ToPosition: %v", err)
	}
	ov := ToOverlay(&state)

	mismatches := LegalSetConformance(state, pos, ov, core.White, fixedNow())
	for _, m := range mismatches {
		t.Error(m)
	}
	if len(mismatches) > 0 {
		t.Fatalf("%d mismatches at the standard opening (see above)", len(mismatches))
	}
}

// TestLegalSetConformanceWithFrozenPiece verifies GenerateSubmittableMoves'
// Frozen-awareness matches internal/match's hard "frozen pieces cannot
// move" guard (match_actions.go:42-44) exactly: the frozen knight's
// candidates must ALL be rejected by internal/match, while the unfrozen
// king still moves normally.
func TestLegalSetConformanceWithFrozenPiece(t *testing.T) {
	board := emptyBoard()
	board[0][4] = &contracts.Piece{Type: "king", Color: "white"}
	board[0][1] = &contracts.Piece{Type: "knight", Color: "white", Frozen: true}
	board[7][4] = &contracts.Piece{Type: "king", Color: "black"}
	state := customState(t, board, "white")

	pos, err := ToPosition(&state)
	if err != nil {
		t.Fatalf("ToPosition: %v", err)
	}
	ov := ToOverlay(&state)
	if !ov.IsFrozen(core.NewSquare(1, 0)) {
		t.Fatal("test setup error: expected b1 to convert as Frozen")
	}

	mismatches := LegalSetConformance(state, pos, ov, core.White, fixedNow())
	for _, m := range mismatches {
		t.Error(m)
	}
}

// TestLegalSetConformanceWithFortressZone verifies the fortress landing/
// path-crossing rules (overlays.go's fortressBlockMask, unifying
// internal/match's fortressEntryBlocked/pathCrossesFortress/
// isInsideEnemyFortress) produce the same accept/reject decisions as
// internal/match's own three separate checks.
func TestLegalSetConformanceWithFortressZone(t *testing.T) {
	board := emptyBoard()
	board[0][4] = &contracts.Piece{Type: "king", Color: "white"}
	board[0][0] = &contracts.Piece{Type: "rook", Color: "white"}
	board[7][4] = &contracts.Piece{Type: "king", Color: "black"}
	state := customState(t, board, "white")
	state.FortressZones = []contracts.FortressZone{
		{TopRow: 2, LeftCol: 0, TurnsLeft: 2, OwnerColor: "black"}, // a3-b4
	}

	pos, err := ToPosition(&state)
	if err != nil {
		t.Fatalf("ToPosition: %v", err)
	}
	ov := ToOverlay(&state)
	if !ov.HasFortress(core.Black) {
		t.Fatal("test setup error: expected the fortress zone to convert")
	}

	mismatches := LegalSetConformance(state, pos, ov, core.White, fixedNow())
	for _, m := range mismatches {
		t.Error(m)
	}
}

// TestLegalSetConformanceWithFusedPiece verifies FusedWith's union movegen
// (both real-type and secondary-type destinations) matches
// legalMovesWithFusion's clone-and-retype approach exactly.
func TestLegalSetConformanceWithFusedPiece(t *testing.T) {
	board := emptyBoard()
	board[0][1] = &contracts.Piece{Type: "king", Color: "white"} // b1, off every d4 line
	board[3][3] = &contracts.Piece{Type: "bishop", Color: "white", FusedWith: "rook"} // d4
	board[7][0] = &contracts.Piece{Type: "king", Color: "black"} // a8, off every d4 line
	state := customState(t, board, "white")

	pos, err := ToPosition(&state)
	if err != nil {
		t.Fatalf("ToPosition: %v", err)
	}
	ov := ToOverlay(&state)
	if ov.FusedWith(core.NewSquare(3, 3)) != core.Rook {
		t.Fatal("test setup error: expected d4 to convert as fused with Rook")
	}

	mismatches := LegalSetConformance(state, pos, ov, core.White, fixedNow())
	for _, m := range mismatches {
		t.Error(m)
	}
}

// TestRandomWalkPlainChessConformance drives several independent random
// games from the standard opening, comparing engine/core's move application
// against internal/match's after every single ply -- this is the
// "differential fuzzing... asserting identical state after every action"
// the engine rebuild plan calls for, for the plain-chess subset of the
// rules (no cards are played mid-walk, so overlay state stays empty
// throughout; overlay integration is covered by the LegalSetConformance
// tests above instead).
func TestRandomWalkPlainChessConformance(t *testing.T) {
	const gamesPerRun = 5
	const maxPlies = 40

	for seed := int64(0); seed < gamesPerRun; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			pick := func(moves []core.Move) core.Move {
				return moves[rng.Intn(len(moves))]
			}
			matchID := fmt.Sprintf("random_walk_%d", seed)
			result := RandomWalk(matchID, maxPlies, pick, fixedNow())
			if result.Mismatch != "" {
				t.Fatalf("seed %d: %s (reached ply %d)", seed, result.Mismatch, result.Plies)
			}
			if result.Plies == 0 {
				t.Fatalf("seed %d: walked zero plies -- test setup error", seed)
			}
		})
	}
}
