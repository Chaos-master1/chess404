// Package nnue is Phase 3 of the engine rebuild: a real quantized,
// incrementally-updated evaluation network, replacing engine/search's
// placeholder material+overlay eval.go the way the plan calls for.
//
// Scope, stated plainly: this is a genuine, working, tested
// accumulator-based quantized network with card-aware inputs -- not a
// production-grade, fully-tuned one. Real differences from a
// state-of-the-art NNUE (e.g. Stockfish's): a single shared accumulator
// computed from White's own king-bucket perspective, rather than a true
// dual White-sees/Black-sees accumulator pair; 4 coarse king buckets
// (quadrants) rather than per-square HalfKP buckets; one quantization
// scale shared across layers rather than per-layer tuning. These are
// deliberate simplifications to keep the whole pipeline (features,
// incremental accumulator, quantized inference, training, round-trip
// verification) tractable and CORRECT within this phase, rather than
// partially building a more ambitious architecture. Widening the king
// bucketing or adding a second perspective later is additive, not a
// rewrite, since the accumulator/quantization machinery doesn't care how
// many buckets or perspectives feed it.
package nnue

import (
	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
)

const (
	// numKingBuckets divides the board into quadrants by White's own king
	// position -- the "condition every feature on the king" idea HalfKP
	// captures precisely, simplified to 4 coarse buckets instead of 64.
	numKingBuckets = 4
	// numPieceSquareFeatures: 64 squares x 5 non-king piece types x 2
	// colors. Kings are excluded -- a king's own square is exactly what
	// determines the bucket, so encoding it again as a piece-square feature
	// would be redundant.
	numPieceSquareFeatures = 64 * 5 * 2
	numChessFeatures       = numKingBuckets * numPieceSquareFeatures

	// numCountBuckets buckets a count into {0, 1, 2-or-more} -- coarse on
	// purpose; a learned network doesn't need the exact count of frozen
	// pieces to know "having several is different from having one".
	numCountBuckets = 3
	// Card-aware auxiliary features (deliberately NOT king-bucketed --
	// these are global facts about the position, not spatial ones):
	// hand size (mine, theirs), frozen piece count (mine, theirs),
	// shielded piece count (mine, theirs) -- each a numCountBuckets-wide
	// one-hot -- plus a fortress-active boolean (mine, theirs).
	numCardCountFeatures = numCountBuckets * 6
	numCardBoolFeatures  = 2
	numCardFeatures      = numCardCountFeatures + numCardBoolFeatures

	// NumFeatures is the total input dimension: every accumulator row
	// (Network.L1Weights) has exactly this many entries.
	NumFeatures = numChessFeatures + numCardFeatures
)

var pieceTypeIndex = map[core.PieceType]int{
	core.Pawn: 0, core.Knight: 1, core.Bishop: 2, core.Rook: 3, core.Queen: 4,
}

// kingBucket buckets kingSq into one of the 4 board quadrants.
func kingBucket(kingSq core.Square) int {
	bucket := 0
	if kingSq.File() >= 4 {
		bucket |= 1
	}
	if kingSq.Rank() >= 4 {
		bucket |= 2
	}
	return bucket
}

// pieceSquareFeature is the (non-king-bucketed) index for a piece of type
// pt and color c sitting on sq -- combined with a king bucket by
// ActiveFeatures/KingBucketOf to form a full feature index.
func pieceSquareFeature(sq core.Square, pt core.PieceType, c core.Color) int {
	return int(sq)*5*2 + pieceTypeIndex[pt]*2 + int(c)
}

// KingBucketOf exposes kingBucket for callers that need to detect a
// king-bucket change (e.g. deciding whether an incremental update or a
// full Refresh is required after a move) without recomputing it themselves.
func KingBucketOf(p *core.Position) int {
	return kingBucket(p.KingSquare(core.White))
}

// PieceFeature returns the full (king-bucketed) feature index for a piece
// of type pt and color c on sq, given bucket (the current king bucket) --
// the incremental-update primitive: when a non-king piece moves, its OLD
// square's feature is removed and its NEW square's feature is added, both
// computed via this same function with the SAME bucket (since only the
// king's own move changes the bucket).
func PieceFeature(bucket int, sq core.Square, pt core.PieceType, c core.Color) int {
	return bucket*numPieceSquareFeatures + pieceSquareFeature(sq, pt, c)
}

func appendCountBucket(feats []int, base, count int) ([]int, int) {
	bucket := count
	if bucket > numCountBuckets-1 {
		bucket = numCountBuckets - 1
	}
	return append(feats, base+bucket), base + numCountBuckets
}

func countWhere(p *core.Position, c core.Color, match func(sq core.Square) bool) int {
	count := 0
	pieces := p.Occupied(c)
	for pieces.Any() {
		var sq core.Square
		sq, pieces = pieces.PopLSB()
		if match(sq) {
			count++
		}
	}
	return count
}

// ActiveFeatures returns every active feature index for (p, ov,
// whiteHand, blackHand) -- the full, from-scratch feature set a Refresh
// needs. Deliberately takes actions.Hand directly (not a bundled struct)
// so this package has no dependency on engine/search, which is the
// package that will import THIS one to use as an eval -- avoiding an
// import cycle.
func ActiveFeatures(p *core.Position, ov *core.CardOverlay, whiteHand, blackHand actions.Hand) []int {
	bucket := kingBucket(p.KingSquare(core.White))
	feats := make([]int, 0, 32)

	for sq := core.Square(0); sq < 64; sq++ {
		piece := p.PieceAt(sq)
		if piece.IsNone() || piece.Type == core.King {
			continue
		}
		feats = append(feats, PieceFeature(bucket, sq, piece.Type, piece.Color))
	}

	base := numChessFeatures
	feats, base = appendCountBucket(feats, base, len(whiteHand))
	feats, base = appendCountBucket(feats, base, len(blackHand))
	feats, base = appendCountBucket(feats, base, countWhere(p, core.White, ov.IsFrozen))
	feats, base = appendCountBucket(feats, base, countWhere(p, core.Black, ov.IsFrozen))
	feats, base = appendCountBucket(feats, base, countWhere(p, core.White, ov.IsShielded))
	feats, base = appendCountBucket(feats, base, countWhere(p, core.Black, ov.IsShielded))
	if ov.HasFortress(core.White) {
		feats = append(feats, base)
	}
	base++
	if ov.HasFortress(core.Black) {
		feats = append(feats, base)
	}
	return feats
}
