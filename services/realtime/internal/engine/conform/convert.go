// Package conform is the differential-fuzzing conformance harness for the
// engine rebuild -- "the trust anchor for the whole rebuild" per the plan.
// It compares services/realtime/internal/engine/core (the new bitboard
// kernel) against services/realtime/internal/match (the authoritative
// server rules) and asserts they agree.
//
// internal/match exposes ZERO rules functions directly: legalMoves,
// pseudoMoves, isAttacked, positionKey, applyMove, applyIntent are all
// unexported (verified by reading every file in that package). The only
// exported entry points that ultimately exercise the real rules logic are
// three methods on *match.Service: CreateMatch, JoinMatchSeat, and
// ApplyIntent -- exactly what internal/match's own black-box
// integration_test.go (package match_test) already drives. This package
// does the same: it never reads or reimplements internal/match's rules, it
// only calls its public API and diffs the result against engine/core.
//
// This file converts a contracts.MatchState (internal/match's
// representation: [][]*contracts.Piece, string-typed fields, castling/en
// passant derived on demand from Moved+Board+LastMove) into engine/core's
// (*core.Position, *core.CardOverlay) pair, so both sides can be compared on
// common ground.
package conform

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/engine/core"
)

// ToPosition converts state's board/side/castling/en-passant/clock fields
// into a *core.Position, by building a standard FEN string and handing it to
// core.ParseFEN -- reusing an already-perft-verified parser instead of
// poking Position's unexported fields (this package is intentionally
// outside core, exactly as it is outside match). contracts.Square{Row,Col}
// and core's rank*8+file convention agree with zero remapping (Row 0 = rank
// 1 in both -- see makeBoard, internal/match/chess.go:11-28, and
// core/bitboard.go's package comment), so only the string-vs-enum type
// translation and the FEN assembly are needed here.
func ToPosition(state *contracts.MatchState) (*core.Position, error) {
	fen := matchStateToFEN(state)
	p, err := core.ParseFEN(fen)
	if err != nil {
		return nil, fmt.Errorf("conform: converting match state to FEN %q: %w", fen, err)
	}
	return p, nil
}

// ToOverlay converts state's card-effect state (per-piece Frozen/Shielded/
// FusedWith flags, plus the Lava/Bomb/Fortress/BlackHole zone lists) into a
// *core.CardOverlay, using only CardOverlay's exported constructors --
// exactly the API a Phase 2 consumer converting from the wire format would
// use, which is the point of building it this way now. Fog is deliberately
// skipped (see overlays.go's package comment: no rules effect).
func ToOverlay(state *contracts.MatchState) *core.CardOverlay {
	ov := core.NewCardOverlay()

	for row := 0; row < len(state.Board); row++ {
		for col := 0; col < len(state.Board[row]); col++ {
			piece := state.Board[row][col]
			if piece == nil {
				continue
			}
			sq := core.NewSquare(col, row)
			if piece.Frozen {
				ov.SetFrozen(sq, true)
			}
			if piece.Shielded {
				// Piece.ShieldTurn IS ALREADY the expiry threshold
				// (match_cards.go: shieldTurn := state.FullMoveNum + 1 at
				// cast time). SetShielded's parameter is instead "the
				// FullMoveNum AT CAST", from which it derives the same
				// threshold internally (+1) -- so passing ShieldTurn-1
				// reproduces the exact stored expiry, not an approximation.
				castFullMove := 0
				if piece.ShieldTurn != nil {
					castFullMove = *piece.ShieldTurn - 1
				}
				ov.SetShielded(sq, castFullMove)
			}
			if piece.FusedWith != "" {
				ov.SetFused(sq, core.PieceTypeFromString(piece.FusedWith))
			}
		}
	}

	for _, zone := range state.FortressZones {
		ov.SetFortress(core.ColorFromString(zone.OwnerColor), core.NewSquare(zone.LeftCol, zone.TopRow), zone.TurnsLeft)
	}
	for _, lava := range state.LavaSquares {
		ov.AddLava(core.NewSquare(lava.Col, lava.Row), lava.MovesLeft)
	}
	for _, bomb := range state.BombPieces {
		ov.AddBomb(core.NewSquare(bomb.Col, bomb.Row), core.ColorFromString(bomb.OwnerColor), bomb.TurnsLeft)
	}
	for _, hole := range state.BlackHoles {
		ov.AddBlackHole(
			core.NewSquare(hole.Sq1.Col, hole.Sq1.Row),
			core.NewSquare(hole.Sq2.Col, hole.Sq2.Row),
			core.ColorFromString(hole.OwnerColor), hole.TurnsLeft)
	}
	return ov
}

// matchStateToFEN assembles a standard 6-field FEN string. Castling and en
// passant are DERIVED, not stored fields on MatchState -- this reproduces
// internal/match's own positionKey (chess.go:408-433) derivation exactly
// (moved-key presence + actual piece type still on the home square for
// castling; last-move-was-a-2-square-pawn-move for en passant), since
// positionKey is the closest existing analog in the reference to "give me
// this position's FEN-equivalent summary", and conformance means matching
// that derivation precisely, not a idealized reimplementation of it.
func matchStateToFEN(state *contracts.MatchState) string {
	rows := make([]string, 8)
	for row := 0; row < 8; row++ {
		// FEN lists rank 8 first; contracts row 7 = rank 8 (row 0 = rank 1).
		rows[7-row] = fenRankString(state.Board[row])
	}
	board := strings.Join(rows, "/")

	side := "w"
	if state.Turn == "black" {
		side = "b"
	}

	castling := deriveCastlingString(state)
	enPassant := deriveEnPassantSquare(state)

	return fmt.Sprintf("%s %s %s %s %d %d", board, side, castling, enPassant, state.HalfMoveClock, state.FullMoveNum)
}

func fenRankString(row []*contracts.Piece) string {
	var b strings.Builder
	empty := 0
	flush := func() {
		if empty > 0 {
			b.WriteString(strconv.Itoa(empty))
			empty = 0
		}
	}
	for col := 0; col < len(row); col++ {
		piece := row[col]
		if piece == nil {
			empty++
			continue
		}
		flush()
		b.WriteString(fenPieceLetter(piece))
	}
	flush()
	return b.String()
}

var fenLetterByType = map[string]string{
	"pawn": "p", "knight": "n", "bishop": "b", "rook": "r", "queen": "q", "king": "k",
}

func fenPieceLetter(p *contracts.Piece) string {
	letter := fenLetterByType[p.Type]
	if p.Color == "white" {
		return strings.ToUpper(letter)
	}
	return letter
}

// deriveCastlingString mirrors positionKey's castling derivation exactly
// (chess.go:408-425): a right is available iff the relevant square was
// never vacated (absent from Moved, keyed "row-col") AND the expected piece
// type is still physically on that square -- note the reference does not
// re-check the piece's Color here either, matching it precisely rather than
// "fixing" an assumption that's never been wrong in practice (nothing moves
// an opposing piece onto a castling home square without also being a Moved
// entry for it).
func deriveCastlingString(state *contracts.MatchState) string {
	moved := make(map[string]bool, len(state.Moved))
	for _, key := range state.Moved {
		moved[key] = true
	}
	board := state.Board

	castling := ""
	if !moved["0-4"] && squareHasType(board, 0, 4, "king") {
		if !moved["0-7"] && squareHasType(board, 0, 7, "rook") {
			castling += "K"
		}
		if !moved["0-0"] && squareHasType(board, 0, 0, "rook") {
			castling += "Q"
		}
	}
	if !moved["7-4"] && squareHasType(board, 7, 4, "king") {
		if !moved["7-7"] && squareHasType(board, 7, 7, "rook") {
			castling += "k"
		}
		if !moved["7-0"] && squareHasType(board, 7, 0, "rook") {
			castling += "q"
		}
	}
	if castling == "" {
		return "-"
	}
	return castling
}

func squareHasType(board [][]*contracts.Piece, row, col int, pieceType string) bool {
	p := board[row][col]
	return p != nil && p.Type == pieceType
}

// deriveEnPassantSquare mirrors positionKey's en-passant derivation exactly
// (chess.go:427-433): unconditional on the last move being a 2-square pawn
// move, with no check for an adjacent capturing pawn -- see
// zobrist_test.go's TestSamePositionByDifferentMoveOrdersHashesIdentically
// comment for why this "naive" convention (shared by core/move.go) is a
// deliberate conformance target, not a simplification to fix.
func deriveEnPassantSquare(state *contracts.MatchState) string {
	lm := state.LastMove
	if lm == nil {
		return "-"
	}
	toPiece := state.Board[lm.To.Row][lm.To.Col]
	if toPiece == nil || toPiece.Type != "pawn" {
		return "-"
	}
	rowDelta := lm.From.Row - lm.To.Row
	if rowDelta != 2 && rowDelta != -2 {
		return "-"
	}
	midRow := (lm.From.Row + lm.To.Row) / 2
	return string("abcdefgh"[lm.To.Col]) + string("12345678"[midRow])
}

// MoveToIntentFields extracts the (From, To, Promotion) a contracts.PlayerIntent
// needs to submit m as a "make_move" intent.
func MoveToIntentFields(m core.Move) (from, to contracts.Square, promotion string) {
	from = contracts.Square{Row: m.From.Rank(), Col: m.From.File()}
	to = contracts.Square{Row: m.To.Rank(), Col: m.To.File()}
	if m.IsPromotion() {
		promotion = m.Promotion.String()
	}
	return from, to, promotion
}
