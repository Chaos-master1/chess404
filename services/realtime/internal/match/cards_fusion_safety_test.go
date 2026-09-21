package match

import (
	"testing"

	"github.com/chess404/realtime/internal/contracts"
)

// Regression tests for the fusion-aware king-safety fix in cards_util.go.
//
// Root cause: the card-effect safety helpers (kingsRemainSafe,
// ensurePieceRemovalKeepsOwnKingSafe, ensureRemovalDoesNotCreateCheck) used
// the plain isAttacked, which switches on piece.Type only. A fused piece
// (FusedWith != "") attacks as BOTH of its types -- that is how the rules
// layer (chess.go: legalMovesWithFusion, IsCheckmate, isAttackedWithFusion)
// already treats it. The card helpers therefore validated card effects
// against a weaker attack model than the game itself, so a card could leave
// a king "in check" under the game's own rules -- e.g. a demote/bomb that
// removes the knight half of an enemy rook+knight fusion still left the
// rook body attacking with its fusion-granted knight moves, invisible to
// every card-side safety check. The helpers now use isAttackedWithFusion
// everywhere (kingsRemainSafe simply delegates to kingsRemainSafeWithFusion).
//
// These tests pin helper-level behavior in both directions: fused attacks
// MUST count (fail closed) and non-fused boards MUST be unaffected (no
// false positives from the stronger attack model).

// Board indexing used below: board[row][col], row 0 = rank 8, col 0 = a-file.
// d4 = {4,3}, e2 = {6,4}, e1 = {7,4}.
var (
	fusedAttackerSquare = contracts.Square{Row: 4, Col: 3} // d4
	fusedVictimSquare   = contracts.Square{Row: 6, Col: 4} // e2: knight-shape from d4
	quietKingSquare     = contracts.Square{Row: 7, Col: 4} // e1: neither rook- nor knight-attacked from d4
)

func newFusedRookKnight(color string) *contracts.Piece {
	return &contracts.Piece{Type: "rook", Color: color, FusedWith: "knight"}
}

func place(board [][]*contracts.Piece, sq contracts.Square, p *contracts.Piece) {
	board[sq.Row][sq.Col] = p
}

// A white rook+knight fusion on d4 attacks the black king on e2 as a knight
// even though its body is a rook. Plain isAttacked misses it entirely; king
// safety must not.
func TestKingsRemainSafeCountsFusedKnightShapedAttack(t *testing.T) {
	board := emptyBoard()
	place(board, fusedVictimSquare, &contracts.Piece{Type: "king", Color: "black"})
	place(board, fusedAttackerSquare, newFusedRookKnight("white"))

	if kingsRemainSafe(board, nil) {
		t.Fatal("kingsRemainSafe must treat the fusion's knight-shaped attack on the king as check; plain isAttacked misses it")
	}
	if !isAttackedWithFusion(board, fusedVictimSquare, "white", nil) {
		t.Fatal("isAttackedWithFusion should see the knight-shaped attack from the fused rook")
	}
	if isAttacked(board, fusedVictimSquare, "white", nil) {
		t.Fatal("sanity: plain isAttacked should NOT see the knight-shaped attack (that blindness is the bug being guarded)")
	}
}

// Control for the fix: the same fused rook with the king out of both attack
// patterns must remain "safe" -- switching to the fusion-aware variant must
// not introduce false checks.
func TestKingsRemainSafeToleratesFusedAttackerWithoutCheck(t *testing.T) {
	board := emptyBoard()
	place(board, quietKingSquare, &contracts.Piece{Type: "king", Color: "black"})
	place(board, fusedAttackerSquare, newFusedRookKnight("white"))

	if !kingsRemainSafe(board, nil) {
		t.Fatal("kingsRemainSafe should be safe when the fused piece attacks the king neither as rook nor as knight")
	}
}

// ensureRemovalDoesNotCreateCheck (owner-king branch): the white king on e2
// is knight-attacked by the black rook+knight fusion on d4. Any removal that
// leaves that state in place must be rejected; plain isAttacked would allow
// every removal because it cannot see the fusion's knight attacks.
func TestEnsureRemovalRejectsOwnerKingFusedAttacked(t *testing.T) {
	board := emptyBoard()
	place(board, fusedVictimSquare, &contracts.Piece{Type: "king", Color: "white"})
	place(board, fusedAttackerSquare, newFusedRookKnight("black"))

	err := ensureRemovalDoesNotCreateCheck(board, contracts.Square{Row: 6, Col: 3}, "white", nil)
	if err == nil {
		t.Fatal("ensureRemovalDoesNotCreateCheck must reject removals that leave the owner king fused-attacked (knight-shaped attack from a rook+knight fusion)")
	}
}

// ensureRemovalDoesNotCreateCheck (enemy-king branch): mirror of the above --
// the enemy king is the fused-attacked one, and the card owner must not be
// allowed to create that state either.
func TestEnsureRemovalRejectsEnemyKingFusedAttacked(t *testing.T) {
	board := emptyBoard()
	place(board, fusedVictimSquare, &contracts.Piece{Type: "king", Color: "black"})
	place(board, fusedAttackerSquare, newFusedRookKnight("white"))

	err := ensureRemovalDoesNotCreateCheck(board, contracts.Square{Row: 6, Col: 3}, "white", nil)
	if err == nil {
		t.Fatal("ensureRemovalDoesNotCreateCheck must reject removals that leave the enemy king fused-attacked")
	}
}

// Control: with the king out of both attack patterns, an unrelated removal
// stays legal -- the fusion-aware check must not over-reject.
func TestEnsureRemovalAllowsQuietBoard(t *testing.T) {
	board := emptyBoard()
	place(board, quietKingSquare, &contracts.Piece{Type: "king", Color: "white"})
	place(board, fusedAttackerSquare, newFusedRookKnight("black"))

	if err := ensureRemovalDoesNotCreateCheck(board, contracts.Square{Row: 6, Col: 3}, "white", nil); err != nil {
		t.Fatalf("removal on a quiet board should be allowed, got: %v", err)
	}
}

// ensurePieceRemovalKeepsOwnKingSafe: removing white's own piece while the
// white king sits under a fusion's knight-shaped attack must be rejected
// (plain isAttacked would wave it through).
func TestEnsurePieceRemovalKeepsOwnKingSafeIsFusionAware(t *testing.T) {
	board := emptyBoard()
	place(board, fusedVictimSquare, &contracts.Piece{Type: "king", Color: "white"})
	place(board, fusedAttackerSquare, newFusedRookKnight("black"))
	place(board, contracts.Square{Row: 4, Col: 4}, &contracts.Piece{Type: "rook", Color: "white"}) // e4, removed below

	if err := ensurePieceRemovalKeepsOwnKingSafe(board, contracts.Square{Row: 4, Col: 4}, nil); err == nil {
		t.Fatal("removing a piece while the own king is fused-attacked must be rejected; plain isAttacked misses the knight-shaped attack")
	}
}
