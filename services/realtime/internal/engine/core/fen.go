package core

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseFEN builds a Position from Forsyth-Edwards Notation. Standard chess
// FEN has no room for card state (lava, fortress, hands, ...) -- that's
// exactly the boundary Phase 1 draws deliberately, see position.go's package
// comment -- so this only ever produces a plain Position.
func ParseFEN(fen string) (*Position, error) {
	fields := strings.Fields(strings.TrimSpace(fen))
	if len(fields) < 4 {
		return nil, fmt.Errorf("core: FEN needs at least 4 fields (board, side, castling, en passant), got %d: %q", len(fields), fen)
	}

	p := NewEmptyPosition()

	ranks := strings.Split(fields[0], "/")
	if len(ranks) != 8 {
		return nil, fmt.Errorf("core: FEN board must have 8 ranks, got %d: %q", len(ranks), fields[0])
	}
	// FEN lists rank 8 first, rank 1 last -- the opposite of this package's
	// rank-0-is-rank-1 convention (see bitboard.go's Square doc comment).
	for i, rankStr := range ranks {
		rank := 7 - i
		file := 0
		for _, ch := range rankStr {
			if ch >= '1' && ch <= '8' {
				file += int(ch - '0')
				continue
			}
			piece, err := pieceFromFENChar(ch)
			if err != nil {
				return nil, fmt.Errorf("core: %w in %q", err, fen)
			}
			if file > 7 {
				return nil, fmt.Errorf("core: rank %q overflows 8 files: %q", rankStr, fen)
			}
			p.SetPiece(NewSquare(file, rank), piece)
			file++
		}
		if file != 8 {
			return nil, fmt.Errorf("core: rank %q does not sum to 8 files: %q", rankStr, fen)
		}
	}

	switch fields[1] {
	case "w":
		p.sideToMove = White
	case "b":
		p.sideToMove = Black
	default:
		return nil, fmt.Errorf("core: invalid side-to-move field %q: %q", fields[1], fen)
	}

	if fields[2] != "-" {
		for _, ch := range fields[2] {
			switch ch {
			case 'K':
				p.castling |= CastleWhiteKingside
			case 'Q':
				p.castling |= CastleWhiteQueenside
			case 'k':
				p.castling |= CastleBlackKingside
			case 'q':
				p.castling |= CastleBlackQueenside
			default:
				return nil, fmt.Errorf("core: invalid castling field character %q: %q", ch, fen)
			}
		}
	}

	if fields[3] == "-" {
		p.enPassant = NoSquare
	} else {
		sq, err := squareFromAlgebraic(fields[3])
		if err != nil {
			return nil, fmt.Errorf("core: %w: %q", err, fen)
		}
		p.enPassant = sq
	}

	p.halfMoveClock = 0
	if len(fields) > 4 {
		if n, err := strconv.Atoi(fields[4]); err == nil {
			p.halfMoveClock = n
		}
	}
	p.fullMoveNum = 1
	if len(fields) > 5 {
		if n, err := strconv.Atoi(fields[5]); err == nil {
			p.fullMoveNum = n
		}
	}

	// Side/castling/en-passant above were set directly, bypassing the
	// hash-aware helpers -- see the matching comment in NewStartingPosition.
	p.hash = computeHash(p)
	return p, nil
}

// MustParseFEN is ParseFEN for callers (tests, package-level fixtures) that
// already know the FEN is well-formed and would rather panic loudly on a
// typo than thread an error through.
func MustParseFEN(fen string) *Position {
	p, err := ParseFEN(fen)
	if err != nil {
		panic(err)
	}
	return p
}

// ToFEN renders p as a standard 6-field FEN string -- ParseFEN's inverse,
// used by Phase 3's self-play export (a Position needs to survive a
// round trip through a text file a Python trainer reads) and by anything
// else that wants a compact, standard, human-readable position summary.
func (p *Position) ToFEN() string {
	rows := make([]string, 8)
	for rank := 7; rank >= 0; rank-- {
		var b strings.Builder
		empty := 0
		flush := func() {
			if empty > 0 {
				b.WriteString(strconv.Itoa(empty))
				empty = 0
			}
		}
		for file := 0; file < 8; file++ {
			piece := p.PieceAt(NewSquare(file, rank))
			if piece.IsNone() {
				empty++
				continue
			}
			flush()
			b.WriteString(fenCharForPiece(piece))
		}
		flush()
		rows[7-rank] = b.String()
	}
	board := strings.Join(rows, "/")

	side := "w"
	if p.sideToMove == Black {
		side = "b"
	}

	castling := ""
	if p.HasCastleRight(CastleWhiteKingside) {
		castling += "K"
	}
	if p.HasCastleRight(CastleWhiteQueenside) {
		castling += "Q"
	}
	if p.HasCastleRight(CastleBlackKingside) {
		castling += "k"
	}
	if p.HasCastleRight(CastleBlackQueenside) {
		castling += "q"
	}
	if castling == "" {
		castling = "-"
	}

	enPassant := "-"
	if p.enPassant != NoSquare {
		enPassant = p.enPassant.String()
	}

	return fmt.Sprintf("%s %s %s %s %d %d", board, side, castling, enPassant, p.halfMoveClock, p.fullMoveNum)
}

var fenCharByType = map[PieceType]string{
	Pawn: "p", Knight: "n", Bishop: "b", Rook: "r", Queen: "q", King: "k",
}

func fenCharForPiece(p Piece) string {
	ch := fenCharByType[p.Type]
	if p.Color == White {
		return strings.ToUpper(ch)
	}
	return ch
}

func pieceFromFENChar(ch rune) (Piece, error) {
	color := White
	lower := ch
	if ch >= 'a' && ch <= 'z' {
		color = Black
	} else {
		lower = ch + ('a' - 'A')
	}
	var pt PieceType
	switch lower {
	case 'p':
		pt = Pawn
	case 'n':
		pt = Knight
	case 'b':
		pt = Bishop
	case 'r':
		pt = Rook
	case 'q':
		pt = Queen
	case 'k':
		pt = King
	default:
		return Piece{}, fmt.Errorf("invalid FEN piece character %q", ch)
	}
	return Piece{Type: pt, Color: color}, nil
}

func squareFromAlgebraic(s string) (Square, error) {
	if len(s) != 2 {
		return NoSquare, fmt.Errorf("invalid square %q", s)
	}
	file := int(s[0] - 'a')
	rank := int(s[1] - '1')
	if !Valid(file, rank) {
		return NoSquare, fmt.Errorf("invalid square %q", s)
	}
	return NewSquare(file, rank), nil
}
