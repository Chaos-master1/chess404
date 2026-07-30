package search

import "github.com/chess404/realtime/internal/engine/core"

// evaluate is a deliberately simple placeholder: material (classical 1/3/
// 3/5/9 scale, x100 for finer resolution) plus a handful of small,
// clearly-placeholder overlay-aware terms (Frozen is bad for its owner,
// Shielded and an active Fortress are mildly good for their owner). This
// is NOT a tuned classical evaluation -- Phase 3 replaces this with a real
// quantized NNUE trained on actual card play, which is what should learn
// these weights properly. This function exists so Phase 2's search has
// *something* real to score leaf nodes with while it's being built and
// tested; it is not the deliverable.
//
// Returns a score from White's perspective (positive good for White);
// callers negate for Black's perspective, matching negamax convention.
func evaluate(p *core.Position, ov *core.CardOverlay) int {
	score := materialScore(p)
	score += overlayScore(p, ov)
	return score
}

var evalPieceTypes = [5]core.PieceType{core.Pawn, core.Knight, core.Bishop, core.Rook, core.Queen}

func materialScore(p *core.Position) int {
	score := 0
	for _, pt := range evalPieceTypes {
		score += p.PieceBitboard(pt, core.White).PopCount() * pt.Value() * 100
		score -= p.PieceBitboard(pt, core.Black).PopCount() * pt.Value() * 100
	}
	return score
}

func overlayScore(p *core.Position, ov *core.CardOverlay) int {
	score := 0
	for sq := core.Square(0); sq < 64; sq++ {
		piece := p.PieceAt(sq)
		if piece.IsNone() {
			continue
		}
		sign := 1
		if piece.Color == core.Black {
			sign = -1
		}
		if ov.IsFrozen(sq) {
			score -= sign * 50
		}
		if ov.IsShielded(sq) {
			score += sign * 30
		}
	}
	if ov.HasFortress(core.White) {
		score += 40
	}
	if ov.HasFortress(core.Black) {
		score -= 40
	}
	return score
}

// evaluateForMover returns evaluate's score from mover's own perspective
// (negamax convention: always "good for whoever is about to move").
func evaluateForMover(p *core.Position, ov *core.CardOverlay, mover core.Color) int {
	score := evaluate(p, ov)
	if mover == core.Black {
		return -score
	}
	return score
}
