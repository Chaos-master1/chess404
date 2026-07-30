package actions

import "github.com/chess404/realtime/internal/engine/core"

// Applying a chosen Action's effect to (p, ov, hand). For ActionMove,
// callers use core.MakeMoveWithOverlay/Position.UnmakeMove directly (their
// undo token is core's own unexported type, opaque but usable via `:=`
// type inference -- there is no way for this package to name that type in
// a wrapper signature, and no need to: core already exports everything a
// caller needs for the move case). This file covers only the ActionCard
// case, which core has no existing apply/undo primitive for at all.

// CardUndo restores whatever ApplyCardAction changed. A full snapshot
// (Position is a small value type; CardOverlay.Clone() deep-copies its
// zone slices), not an incremental undo -- card actions are a minority of
// search nodes next to chess moves, so this is a deliberate simplicity/
// performance tradeoff for Phase 2's MVP scope, matching movegen.go's own
// "obvious correctness over premature optimization" precedent.
type CardUndo struct {
	pos core.Position
	ov  *core.CardOverlay
}

// ApplyCardAction applies a's card effect to p/ov in place (a must have
// Kind == ActionCard) and returns the snapshot needed to undo it. Does NOT
// touch hand -- callers compute hand.Without(a.Card.ID) themselves for the
// recursive call and simply keep using their own prior `hand` value for
// sibling branches, since Hand is an immutable-style value type (Without
// returns a new slice); there is nothing to "undo" there.
func ApplyCardAction(p *core.Position, ov *core.CardOverlay, a Action) CardUndo {
	undo := CardUndo{pos: *p, ov: ov.Clone()}
	applyCardEffect(p, ov, a)
	return undo
}

// UndoCardAction reverses ApplyCardAction, given the token it returned.
func UndoCardAction(p *core.Position, ov *core.CardOverlay, u CardUndo) {
	*p = u.pos
	*ov = *u.ov
}

func applyCardEffect(p *core.Position, ov *core.CardOverlay, a Action) {
	switch a.Card.Mechanic {
	case MechanicFreeze:
		ov.SetFrozen(a.Targets.First, true)
	case MechanicShield:
		// Piece.ShieldTurn = state.FullMoveNum + 1 in internal/match, computed
		// from FullMoveNum AT CAST TIME (match_cards.go:264) -- since this
		// package's turn model always applies the card BEFORE the move (see
		// action.go), p.FullMoveNumber() here is exactly that pre-move value.
		ov.SetShielded(a.Targets.First, p.FullMoveNumber())
	case MechanicFortress:
		ov.SetFortress(p.SideToMove(), a.Targets.First, 2)
	case MechanicLavaground:
		ov.AddLava(a.Targets.First, 2)
	case MechanicUnabomber:
		ov.AddBomb(a.Targets.First, p.SideToMove(), 2)
	case MechanicBlackhole:
		ov.AddBlackHole(a.Targets.First, a.Targets.Second, p.SideToMove(), 2)
	case MechanicHalfFuse, MechanicFullFusion:
		applyFusion(p, ov, a.Targets.First, a.Targets.Second)
	}
}

// applyFusion mirrors match_cards.go's halffuse/fullfusion resolution
// (:1083-1099, :1146-1158): the FIRST piece is deleted outright; the SECOND
// survives, either gaining a FusedWith tag or -- for the bishop+rook
// special case, either order -- becoming a plain queen with no tag at all.
func applyFusion(p *core.Position, ov *core.CardOverlay, first, second core.Square) {
	firstType := p.PieceAt(first).Type
	secondPiece := p.PieceAt(second)
	isBishopRook := (firstType == core.Bishop && secondPiece.Type == core.Rook) ||
		(firstType == core.Rook && secondPiece.Type == core.Bishop)

	p.RemovePiece(first)
	ov.ClearOverlay(first)

	if isBishopRook {
		p.RemovePiece(second)
		p.SetPiece(second, core.Piece{Type: core.Queen, Color: secondPiece.Color})
		ov.ClearFused(second)
	} else {
		ov.SetFused(second, firstType)
	}
}
