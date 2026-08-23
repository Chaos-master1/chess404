package actions

import (
	"sort"

	"github.com/chess404/realtime/internal/engine/core"
)

// Per-mechanic candidate proposers: staged, K-truncated, policy-ordered
// generation per the plan's L1 spec -- never enumerate the full raw target
// space (up to 64 squares for zone/trap placements, up to C(64,2)=2016
// pairs for BlackHole). Every proposer uses a simple, clearly-named cheap
// heuristic (piece value, Chebyshev distance to a king) rather than a
// learned policy -- exactly the kind of placeholder the plan calls
// "cheap heuristics", refinable once a real policy prior exists (Phase 3).
//
// Every VALIDATION rule (who can be targeted, value caps, adjacency,
// redundancy) is copied from internal/match/match_cards.go's
// applySelectTarget case for that mechanic, not just the heuristic RANKING
// on top of it -- an under-generated candidate here is corrected to match
// the reference before it's a ranking problem.

func generateCardActions(p *core.Position, ov *core.CardOverlay, card CardInstance) []Action {
	switch card.Mechanic {
	case MechanicFreeze:
		return freezeCandidates(p, card)
	case MechanicShield:
		return shieldCandidates(p, ov, card)
	case MechanicFortress:
		return fortressCandidates(p, card)
	case MechanicLavaground:
		return lavagroundCandidates(p, ov, card)
	case MechanicUnabomber:
		return unabomberCandidates(p, card)
	case MechanicBlackhole:
		return blackholeCandidates(p, card)
	case MechanicHalfFuse:
		return fusionCandidates(p, ov, card, true)
	case MechanicFullFusion:
		return fusionCandidates(p, ov, card, false)
	default:
		// Not one of the seven mechanics this package models -- never
		// proposed to the search. See the package comment.
		return nil
	}
}

func chebyshev(a, b core.Square) int {
	df := a.File() - b.File()
	if df < 0 {
		df = -df
	}
	dr := a.Rank() - b.Rank()
	if dr < 0 {
		dr = -dr
	}
	if df > dr {
		return df
	}
	return dr
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func singleTargetActions(card CardInstance, squares []core.Square) []Action {
	out := make([]Action, len(squares))
	for i, sq := range squares {
		out[i] = Action{Kind: ActionCard, Card: card, Targets: CardTargets{NumTargets: 1, First: sq}}
	}
	return out
}

// freezeCandidates: enemy non-king pieces (match_cards.go:239-251), ranked
// by material value descending -- freezing the opponent's most valuable
// mobile piece is the cheap default; a real policy would weigh "is this
// piece defending something" instead.
func freezeCandidates(p *core.Position, card CardInstance) []Action {
	const cap = 6
	enemy := p.SideToMove().Opposite()
	squares := rankedByValueDescending(p, enemy)
	if len(squares) > cap {
		squares = squares[:cap]
	}
	return singleTargetActions(card, squares)
}

// shieldCandidates: own non-king pieces (match_cards.go:252-266), ranked
// with currently-attacked pieces first (protecting something the opponent
// could take right now), then by value descending.
func shieldCandidates(p *core.Position, ov *core.CardOverlay, card CardInstance) []Action {
	const cap = 6
	mover := p.SideToMove()
	enemy := mover.Opposite()

	type scored struct {
		sq       core.Square
		attacked bool
		value    int
	}
	var candidates []scored
	pieces := p.Occupied(mover)
	for pieces.Any() {
		var sq core.Square
		sq, pieces = pieces.PopLSB()
		piece := p.PieceAt(sq)
		if piece.Type == core.King {
			continue
		}
		candidates = append(candidates, scored{sq, core.IsAttackedWithFusion(p, ov, sq, enemy), piece.Type.Value()})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].attacked != candidates[j].attacked {
			return candidates[i].attacked
		}
		return candidates[i].value > candidates[j].value
	})
	if len(candidates) > cap {
		candidates = candidates[:cap]
	}
	squares := make([]core.Square, len(candidates))
	for i, c := range candidates {
		squares[i] = c.sq
	}
	return singleTargetActions(card, squares)
}

// fortressCandidates: any anchor square, clamped to [0,6]x[0,6] the same
// way match_cards.go's fortress case does (:962-963) so no two candidates
// collapse to the same clamped zone, ranked by Chebyshev distance to
// whichever king (own or enemy) is closer -- a fortress is tactically
// relevant either restricting the enemy king or shielding your own.
func fortressCandidates(p *core.Position, card CardInstance) []Action {
	const cap = 8
	mover := p.SideToMove()
	ownKing := p.KingSquare(mover)
	enemyKing := p.KingSquare(mover.Opposite())

	type scored struct {
		anchor core.Square
		dist   int
	}
	seen := make(map[core.Square]bool, 49)
	var candidates []scored
	for file := 0; file <= 6; file++ {
		for rank := 0; rank <= 6; rank++ {
			anchor := core.NewSquare(file, rank)
			if seen[anchor] {
				continue
			}
			seen[anchor] = true
			d := chebyshev(anchor, enemyKing)
			if own := chebyshev(anchor, ownKing); own < d {
				d = own
			}
			candidates = append(candidates, scored{anchor, d})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].dist < candidates[j].dist })
	if len(candidates) > cap {
		candidates = candidates[:cap]
	}
	squares := make([]core.Square, len(candidates))
	for i, c := range candidates {
		squares[i] = c.anchor
	}
	return singleTargetActions(card, squares)
}

// lavagroundCandidates: empty squares without an existing trap
// (match_cards.go:910-927), ranked by distance to the enemy king.
func lavagroundCandidates(p *core.Position, ov *core.CardOverlay, card CardInstance) []Action {
	const cap = 8
	enemyKing := p.KingSquare(p.SideToMove().Opposite())
	occ := p.OccupiedAll()

	type scored struct {
		sq   core.Square
		dist int
	}
	var candidates []scored
	for sq := core.Square(0); sq < 64; sq++ {
		if occ.Has(sq) || ov.HasLava(sq) {
			continue
		}
		candidates = append(candidates, scored{sq, chebyshev(sq, enemyKing)})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].dist < candidates[j].dist })
	if len(candidates) > cap {
		candidates = candidates[:cap]
	}
	squares := make([]core.Square, len(candidates))
	for i, c := range candidates {
		squares[i] = c.sq
	}
	return singleTargetActions(card, squares)
}

// unabomberCandidates: own non-king pieces (match_cards.go:1006-1023,
// no existing-bomb check -- double-bombing is allowed by the reference),
// ranked by distance to the enemy king: a bomb planted on a piece already
// close to the action is more likely to detonate somewhere valuable by the
// time its 2-ply fuse runs out.
func unabomberCandidates(p *core.Position, card CardInstance) []Action {
	const cap = 6
	mover := p.SideToMove()
	enemyKing := p.KingSquare(mover.Opposite())

	type scored struct {
		sq   core.Square
		dist int
	}
	var candidates []scored
	pieces := p.Occupied(mover)
	for pieces.Any() {
		var sq core.Square
		sq, pieces = pieces.PopLSB()
		if p.PieceAt(sq).Type == core.King {
			continue
		}
		candidates = append(candidates, scored{sq, chebyshev(sq, enemyKing)})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].dist < candidates[j].dist })
	if len(candidates) > cap {
		candidates = candidates[:cap]
	}
	squares := make([]core.Square, len(candidates))
	for i, c := range candidates {
		squares[i] = c.sq
	}
	return singleTargetActions(card, squares)
}

// blackholeCandidates: any two distinct squares (match_cards.go:818-851 --
// no adjacency or occupancy requirement). The raw space is C(64,2)=2016;
// this reduces it by first ranking every SINGLE square (distance to the
// enemy king, with a bonus for currently holding an enemy piece), keeping
// the best few, then pairing only within that reduced pool.
func blackholeCandidates(p *core.Position, card CardInstance) []Action {
	const singlePoolSize = 6
	mover := p.SideToMove()
	enemyKing := p.KingSquare(mover.Opposite())
	enemy := p.Occupied(mover.Opposite())

	type scored struct {
		sq    core.Square
		score int
	}
	candidates := make([]scored, 64)
	for sq := core.Square(0); sq < 64; sq++ {
		score := chebyshev(sq, enemyKing)
		if enemy.Has(sq) {
			score -= 2
		}
		candidates[sq] = scored{sq, score}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].score < candidates[j].score })
	if len(candidates) > singlePoolSize {
		candidates = candidates[:singlePoolSize]
	}

	var out []Action
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			out = append(out, Action{Kind: ActionCard, Card: card, Targets: CardTargets{
				NumTargets: 2, First: candidates[i].sq, Second: candidates[j].sq,
			}})
		}
	}
	return out
}

// fusionCandidates covers both HalfFuse and FullFusion (match_cards.go:
// 1024-1163): First is the piece that gets consumed, Second is the
// survivor that gains the FusedWith tag (or, for a bishop+rook pair either
// order, becomes a plain queen instead) -- order matters, so (a,b) and
// (b,a) are distinct actions, not deduplicated. capped applies HalfFuse's
// value cap (first piece's value must be < 6, and the combined value must
// be <= 6 unless it's the bishop+rook exception, match_cards.go:1025,
// :1038-1039, :1076-1079); FullFusion has no such cap.
//
// The raw space here is naturally small (adjacency alone bounds it to at
// most a handful of pairs per piece in a real position), so unlike the
// zone/placement mechanics above this returns every valid pair rather than
// truncating to a top-K -- there is no oversized space to cut down.
func fusionCandidates(p *core.Position, ov *core.CardOverlay, card CardInstance, capped bool) []Action {
	mover := p.SideToMove()
	var squares []core.Square
	pieces := p.Occupied(mover)
	for pieces.Any() {
		var sq core.Square
		sq, pieces = pieces.PopLSB()
		piece := p.PieceAt(sq)
		if piece.Type == core.King {
			continue
		}
		if ov.FusedWith(sq) != core.NoPieceType {
			continue
		}
		squares = append(squares, sq)
	}

	var out []Action
	for i, a := range squares {
		for j, b := range squares {
			if i == j {
				continue
			}
			if chebyshev(a, b) > 1 {
				continue
			}
			typeA, typeB := p.PieceAt(a).Type, p.PieceAt(b).Type
			if isFusionRedundant(typeA, typeB, a, b) {
				continue
			}
			isBishopRook := (typeA == core.Bishop && typeB == core.Rook) || (typeA == core.Rook && typeB == core.Bishop)
			if capped {
				if typeA.Value() >= 6 {
					continue
				}
				if !isBishopRook && typeA.Value()+typeB.Value() > 6 {
					continue
				}
			}
			candidate := Action{Kind: ActionCard, Card: card, Targets: CardTargets{NumTargets: 2, First: a, Second: b}}
			if fusionLeavesAKingInCheck(p, ov, candidate) {
				continue
			}
			out = append(out, candidate)
		}
	}
	return out
}

// isFusionRedundant mirrors fusionRedundancy (match_cards.go:1844-1861)
// exactly: same type, queen+rook, queen+bishop, queen+pawn, or same-color-
// square bishop+bishop all add no new movement over one of the pieces alone.
func isFusionRedundant(typeA, typeB core.PieceType, sqA, sqB core.Square) bool {
	if typeA == typeB {
		return true
	}
	if (typeA == core.Queen && typeB == core.Rook) || (typeA == core.Rook && typeB == core.Queen) {
		return true
	}
	if (typeA == core.Queen && typeB == core.Bishop) || (typeA == core.Bishop && typeB == core.Queen) {
		return true
	}
	if (typeA == core.Queen && typeB == core.Pawn) || (typeA == core.Pawn && typeB == core.Queen) {
		return true
	}
	if typeA == core.Bishop && typeB == core.Bishop {
		colorA := (sqA.File() + sqA.Rank()) % 2
		colorB := (sqB.File() + sqB.Rank()) % 2
		if colorA == colorB {
			return true
		}
	}
	return false
}

// fusionLeavesAKingInCheck mirrors match_cards.go's kingsRemainSafeWithFusion
// (checked after applying halffuse/fullfusion at match_cards.go:1095,:1159):
// BOTH kings, not just the mover's -- removing the "first" piece can expose
// a discovered check on either side depending on what it was blocking.
// Found missing here by xgauntlet's E0 cross-engine gauntlet: a real
// ComputerOpponent-vs-NewEngineAdapter game had the search choose a
// fullfusion pair that internal/match correctly rejected with "fullfusion
// would leave a king in check" -- this package's candidate generator had no
// equivalent filter at all before this fix, unlike freeze/shield/etc.'s
// existing validation-mirroring discipline.
func fusionLeavesAKingInCheck(p *core.Position, ov *core.CardOverlay, a Action) bool {
	undo := ApplyCardAction(p, ov, a)
	defer UndoCardAction(p, ov, undo)
	return core.InCheckWithFusion(p, ov, core.White) || core.InCheckWithFusion(p, ov, core.Black)
}

// rankedByValueDescending returns every non-king piece of c, most valuable
// first.
func rankedByValueDescending(p *core.Position, c core.Color) []core.Square {
	type scored struct {
		sq    core.Square
		value int
	}
	var candidates []scored
	pieces := p.Occupied(c)
	for pieces.Any() {
		var sq core.Square
		sq, pieces = pieces.PopLSB()
		piece := p.PieceAt(sq)
		if piece.Type == core.King {
			continue
		}
		candidates = append(candidates, scored{sq, piece.Type.Value()})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].value > candidates[j].value })
	out := make([]core.Square, len(candidates))
	for i, c := range candidates {
		out[i] = c.sq
	}
	return out
}
