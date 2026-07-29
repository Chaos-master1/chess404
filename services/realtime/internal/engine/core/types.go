package core

// Color is the side to move or a piece's owner.
type Color int8

const (
	White Color = iota
	Black
)

// Opposite returns the other color.
func (c Color) Opposite() Color {
	if c == White {
		return Black
	}
	return White
}

func (c Color) String() string {
	if c == White {
		return "white"
	}
	return "black"
}

// ColorFromString parses the wire format's "white"/"black" strings.
func ColorFromString(s string) Color {
	if s == "white" {
		return White
	}
	return Black
}

// PieceType is one of the six chess piece types. Values match the (colorIdx*6
// + typeIdx) convention already used by this codebase's NNUE encoder
// (nnue.go's pieceNNUEIndex) and Zobrist hasher (zobrist.go's pieceIndex) --
// deliberately reused rather than reinvented, so a future shared feature
// encoder doesn't have to reconcile a third convention.
type PieceType int8

// NoPieceType is deliberately the zero value (iota starts here, not at Pawn).
// A Go struct field of type PieceType that's never explicitly set --
// including every Move{} literal that doesn't set Promotion -- is zero-valued
// by the language, silently. If Pawn were 0, every such move would silently
// mean "this promotes to a pawn" and IsPromotion() would be true for it; that
// exact bug was caught by TestMakeUnmakeCapture during development (a
// same-square double-occupancy corruption: the moved queen's real bitboard
// bit coexisted with a bogus promoted-to-pawn bit from a plain, non-promoting
// capture). Whatever this package's zero value is, it must mean "none" --
// this is that fix, not a stylistic preference.
const (
	NoPieceType PieceType = iota
	Pawn
	Knight
	Bishop
	Rook
	Queen
	King
)

func (pt PieceType) String() string {
	switch pt {
	case Pawn:
		return "pawn"
	case Knight:
		return "knight"
	case Bishop:
		return "bishop"
	case Rook:
		return "rook"
	case Queen:
		return "queen"
	case King:
		return "king"
	default:
		return "none"
	}
}

// PieceTypeFromString parses the wire format's lowercase type names.
func PieceTypeFromString(s string) PieceType {
	switch s {
	case "pawn":
		return Pawn
	case "knight":
		return Knight
	case "bishop":
		return Bishop
	case "rook":
		return Rook
	case "queen":
		return Queen
	case "king":
		return King
	default:
		return NoPieceType
	}
}

// Piece is a (type, color) pair. PieceIndex gives the 0-11 index used to
// select a piece bitboard: colorIdx*6 + typeIdx, e.g. white pawn = 0, black
// pawn = 6.
type Piece struct {
	Type  PieceType
	Color Color
}

// NoPiece represents an empty square where a *Piece is expected.
var NoPiece = Piece{Type: NoPieceType}

func (p Piece) IsNone() bool { return p.Type == NoPieceType }

// Index returns this piece's 0-11 bitboard-array index. -1 for Type
// converts PieceType's 1-6 range (see the NoPieceType comment above for why
// it isn't 0-5) back to the 0-5 slot range colorIdx*6+typeIdx expects --
// preserving the exact packed convention this codebase's NNUE encoder
// (nnue.go's pieceNNUEIndex) and Zobrist hasher (zobrist.go's pieceIndex)
// already use, rather than introducing a fourth numbering.
func (p Piece) Index() int { return int(p.Color)*6 + int(p.Type) - 1 }

// pieceValue is the classical material scale already used by
// internal/match's card resolution (match_cards.go's pieceValue: 1/3/3/5/9),
// NOT the centipawn scale internal/engine/eval.go uses (100/320/330/500/900).
// The two engines disagreeing on this was flagged as a real drift risk in the
// pre-rebuild audit; this package follows internal/match's scale since
// engine/conform checks against internal/match directly, and card mechanics
// like sacrifice thresholds are defined in these units.
var pieceValueTable = [7]int{
	NoPieceType: 0, Pawn: 1, Knight: 3, Bishop: 3, Rook: 5, Queen: 9, King: 0,
}

func (pt PieceType) Value() int { return pieceValueTable[pt] }
