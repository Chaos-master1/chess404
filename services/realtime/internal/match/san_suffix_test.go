package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// Regression: moveNotation produced bare SAN with no check ("+") or mate
// ("#") suffixes, so move lists could not distinguish a checking move from a
// quiet one. applyMove now appends the suffix via suffixForMove after the
// board update, and the move_applied payload carries moveSuffix separately.
// Board indexing: row 0 is rank 1 (white's back rank), row 7 is rank 8.
func TestApplyMoveAppendsCheckSuffix(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "san_check",
		WhiteGuestID: "guest-white",
		BlackGuestID: "guest-black",
	}, now)

	c := testMatchContainer(t, service, "san_check")
	c.mu.Lock()
	board := emptyBoard()
	board[0][4] = &contracts.Piece{Type: "king", Color: "white"} // e1
	board[0][3] = &contracts.Piece{Type: "rook", Color: "white"} // d1
	board[7][4] = &contracts.Piece{Type: "king", Color: "black"} // e8
	c.state.Board = board
	c.state.Turn = "white"
	c.mu.Unlock()

	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "make_move",
		MatchID:  "san_check",
		PlayerID: "white_player",
		From:     &contracts.Square{Row: 0, Col: 3},
		To:       &contracts.Square{Row: 7, Col: 3},
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("expected Rd1-d8 to apply, got %v", err)
	}

	if len(c.state.MoveHistory) == 0 {
		t.Fatal("expected move history entry")
	}
	last := c.state.MoveHistory[len(c.state.MoveHistory)-1]
	if last != "Rd8+" {
		t.Fatalf("expected check suffix in notation, got %q", last)
	}
	if c.state.Turn != "black" {
		t.Fatalf("expected black to move, got %q", c.state.Turn)
	}
}

// Mate on the move: the suffix must be "#" and the pipeline must finish the
// match by checkmate, matching the notation the server itself recorded.
// Back-rank mate: Ra1xa8#, with the black king boxed in by its own d7/e7/f7
// pawns, a white g7 pawn covering f8, and the white king on h6 covering g8.
func TestApplyMoveAppendsMateSuffixAndFinishesMatch(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 6, 12, 12, 30, 0, 0, time.UTC)
	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "san_mate",
		WhiteGuestID: "guest-white",
		BlackGuestID: "guest-black",
	}, now)

	c := testMatchContainer(t, service, "san_mate")
	c.mu.Lock()
	board := emptyBoard()
	board[5][7] = &contracts.Piece{Type: "king", Color: "white"} // h6
	board[0][0] = &contracts.Piece{Type: "rook", Color: "white"} // a1
	board[6][6] = &contracts.Piece{Type: "pawn", Color: "white"} // g7
	board[7][4] = &contracts.Piece{Type: "king", Color: "black"} // e8
	board[7][0] = &contracts.Piece{Type: "rook", Color: "black"} // a8
	board[6][3] = &contracts.Piece{Type: "pawn", Color: "black"} // d7
	board[6][4] = &contracts.Piece{Type: "pawn", Color: "black"} // e7
	board[6][5] = &contracts.Piece{Type: "pawn", Color: "black"} // f7
	board[1][7] = &contracts.Piece{Type: "pawn", Color: "black"} // h2
	c.state.Board = board
	c.state.Turn = "white"
	c.state.WhiteHand = nil
	c.state.BlackHand = nil
	c.mu.Unlock()

	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "make_move",
		MatchID:  "san_mate",
		PlayerID: "white_player",
		From:     &contracts.Square{Row: 0, Col: 0},
		To:       &contracts.Square{Row: 7, Col: 0},
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("expected Ra1xa8 to apply, got %v", err)
	}

	last := c.state.MoveHistory[len(c.state.MoveHistory)-1]
	if last != "Rxa8#" {
		t.Fatalf("expected mate suffix in notation, got %q", last)
	}
	if c.state.Status != "finished" || c.state.Winner != "white" || c.state.FinishReason != "checkmate" {
		t.Fatalf("expected checkmate finish, got status=%q winner=%q reason=%q", c.state.Status, c.state.Winner, c.state.FinishReason)
	}
}
