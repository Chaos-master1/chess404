// Package core is the bitboard rules kernel for the Chess404 engine rebuild
// (Phase 1 of the engine plan). It exists to replace the previous search
// substrate -- an 8x8 array-of-pointers board deep-copied at every search
// node (services/realtime/internal/engine/search.go's cloneMatchState /
// applyMoveCopy) -- with bitboards, precomputed attack tables, and
// make/unmake, and to give card mechanics a representation (overlay bit
// planes) that composes with legality instead of being bolted on after it.
//
// Every function here is pure chess plus the overlay hooks; it knows nothing
// about search, evaluation, or the network wire format. internal/match
// remains the single source of truth for game rules -- see
// internal/engine/conform for the differential fuzzer that keeps this
// package honest against it.
package core

import "math/bits"

// Bitboard is a 64-bit set of squares. Square 0 is a1, square 63 is h8,
// square = rank*8 + file -- the SAME convention contracts.Square{Row, Col}
// already uses throughout this codebase (Row 0 = rank 1, Col 0 = file a), so
// converting a wire-format square is `Square(sq.Row*8 + sq.Col)` with no
// remapping. This was a deliberate choice: the previous NNUE encoder and its
// Python trainer disagreed on exactly this convention (a1=0 vs a8=0) and it
// took a silent, months-long strength bug to notice. One convention, used
// everywhere in this repo, is the fix that actually sticks.
type Bitboard uint64

// Square identifies one of the 64 squares, 0 (a1) to 63 (h8).
type Square int8

const (
	NoSquare Square = -1
)

// File and Rank extract the 0-7 file (a-h) and rank (1-8) components.
func (s Square) File() int { return int(s) & 7 }
func (s Square) Rank() int { return int(s) >> 3 }

// NewSquare builds a Square from 0-based file and rank. Out-of-range inputs
// are the caller's responsibility -- this is a hot-path primitive, not a
// validated constructor.
func NewSquare(file, rank int) Square { return Square(rank*8 + file) }

// Valid reports whether file/rank (and therefore the resulting square) are
// on the board.
func Valid(file, rank int) bool { return file >= 0 && file < 8 && rank >= 0 && rank < 8 }

// Bit returns the single-bit Bitboard for this square.
func (s Square) Bit() Bitboard { return Bitboard(1) << uint(s) }

// String renders algebraic notation, e.g. Square(0).String() == "a1".
func (s Square) String() string {
	if s < 0 || s > 63 {
		return "-"
	}
	return string([]byte{byte('a' + s.File()), byte('1' + s.Rank())})
}

// Bitboard primitives. These wrap math/bits rather than reimplementing bit
// tricks: it's the standard library, already correct, and already about as
// fast as hand-rolled de Bruijn sequences on every platform Go targets.

// PopCount returns the number of set bits.
func (b Bitboard) PopCount() int { return bits.OnesCount64(uint64(b)) }

// LSB returns the lowest-indexed set square, or NoSquare if b is empty.
func (b Bitboard) LSB() Square {
	if b == 0 {
		return NoSquare
	}
	return Square(bits.TrailingZeros64(uint64(b)))
}

// PopLSB returns the lowest-indexed set square and the bitboard with that bit
// cleared, in one step -- the standard "iterate set bits" idiom:
//
//	for bb != 0 {
//	    var sq Square
//	    sq, bb = bb.PopLSB()
//	    ...
//	}
func (b Bitboard) PopLSB() (Square, Bitboard) {
	sq := b.LSB()
	return sq, b&(b-1)
}

// Has reports whether square sq is set.
func (b Bitboard) Has(sq Square) bool { return b&sq.Bit() != 0 }

// Set returns b with sq set.
func (b Bitboard) Set(sq Square) Bitboard { return b | sq.Bit() }

// Clear returns b with sq cleared.
func (b Bitboard) Clear(sq Square) Bitboard { return b &^ sq.Bit() }

// Any/Empty are readability aliases for the b != 0 / b == 0 checks that show
// up constantly in movegen.
func (b Bitboard) Any() bool   { return b != 0 }
func (b Bitboard) Empty() bool { return b == 0 }

// File and rank masks, plus the shift helpers built on them. Sliding these
// masks is how leaper/ray generation avoids wrapping around the board edges
// (e.g. a knight on h1 must not "attack" a3 by wrapping through the a-file).
const (
	FileA Bitboard = 0x0101010101010101
	FileH Bitboard = FileA << 7
	Rank1 Bitboard = 0x00000000000000FF
	Rank8 Bitboard = Rank1 << 56

	NotFileA Bitboard = ^FileA
	NotFileH Bitboard = ^FileH

	AllSquares Bitboard = ^Bitboard(0)
)

// FileMask and RankMask return the full file/rank containing a square.
func FileMask(file int) Bitboard { return FileA << uint(file) }
func RankMask(rank int) Bitboard { return Rank1 << uint(8*rank) }

// The eight one-step shifts, each masked to prevent file wraparound. Named
// north/south (rank direction) and east/west (file direction) rather than
// up/down/left/right, which is ambiguous once a board can be viewed from
// either side.
func north(b Bitboard) Bitboard     { return b << 8 }
func south(b Bitboard) Bitboard     { return b >> 8 }
func east(b Bitboard) Bitboard      { return (b &^ FileH) << 1 }
func west(b Bitboard) Bitboard      { return (b &^ FileA) >> 1 }
func northEast(b Bitboard) Bitboard { return (b &^ FileH) << 9 }
func northWest(b Bitboard) Bitboard { return (b &^ FileA) << 7 }
func southEast(b Bitboard) Bitboard { return (b &^ FileH) >> 7 }
func southWest(b Bitboard) Bitboard { return (b &^ FileA) >> 9 }
