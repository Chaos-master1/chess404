package conform

import (
	"fmt"

	"github.com/chess404/realtime/internal/engine/core"
)

// PositionsMatch compares two positions field-by-field -- not just by
// Hash() -- so a real divergence produces a readable diff instead of just
// "hashes differ". Returns ("", true) on a match, or a human-readable
// description of the first difference found.
func PositionsMatch(a, b *core.Position) (mismatch string, ok bool) {
	if a.SideToMove() != b.SideToMove() {
		return fmt.Sprintf("side to move: %v vs %v", a.SideToMove(), b.SideToMove()), false
	}
	for sq := core.Square(0); sq < 64; sq++ {
		pa, pb := a.PieceAt(sq), b.PieceAt(sq)
		if pa != pb {
			return fmt.Sprintf("square %v: %+v vs %+v", sq, pa, pb), false
		}
	}
	rights := []struct {
		name string
		bit  uint8
	}{
		{"White O-O", core.CastleWhiteKingside},
		{"White O-O-O", core.CastleWhiteQueenside},
		{"Black O-O", core.CastleBlackKingside},
		{"Black O-O-O", core.CastleBlackQueenside},
	}
	for _, r := range rights {
		if a.HasCastleRight(r.bit) != b.HasCastleRight(r.bit) {
			return fmt.Sprintf("castling right %s: %v vs %v", r.name, a.HasCastleRight(r.bit), b.HasCastleRight(r.bit)), false
		}
	}
	if a.EnPassant() != b.EnPassant() {
		return fmt.Sprintf("en passant square: %v vs %v", a.EnPassant(), b.EnPassant()), false
	}
	if a.Hash() != b.Hash() {
		// Every field checked above agreed, yet the hash disagrees -- would
		// mean the hash itself is wrong, which is exactly the kind of bug
		// this harness exists to surface, so it's reported rather than
		// silently trusted.
		return fmt.Sprintf("Hash() disagrees (%#x vs %#x) despite identical piece placement/side/castling/en-passant -- a Zobrist bug, not a rules bug", a.Hash(), b.Hash()), false
	}
	return "", true
}

// OverlaysMatch compares the legality-relevant overlay fields
// (Frozen/Shielded/FusedWith/Fortress) square by square for a readable
// diff, then falls back to Hash() equality to also cover the
// Lava/Bomb/BlackHole zone lists, which have no exported per-entry
// accessors (by design -- see overlays.go; those mechanics have no legality
// effect, so a square-by-square breakdown isn't needed to act on a
// mismatch, only to detect one).
func OverlaysMatch(a, b *core.CardOverlay) (mismatch string, ok bool) {
	for sq := core.Square(0); sq < 64; sq++ {
		if a.IsFrozen(sq) != b.IsFrozen(sq) {
			return fmt.Sprintf("square %v Frozen: %v vs %v", sq, a.IsFrozen(sq), b.IsFrozen(sq)), false
		}
		if a.IsShielded(sq) != b.IsShielded(sq) {
			return fmt.Sprintf("square %v Shielded: %v vs %v", sq, a.IsShielded(sq), b.IsShielded(sq)), false
		}
		if a.FusedWith(sq) != b.FusedWith(sq) {
			return fmt.Sprintf("square %v FusedWith: %v vs %v", sq, a.FusedWith(sq), b.FusedWith(sq)), false
		}
	}
	for _, c := range [2]core.Color{core.White, core.Black} {
		if a.HasFortress(c) != b.HasFortress(c) {
			return fmt.Sprintf("%v HasFortress: %v vs %v", c, a.HasFortress(c), b.HasFortress(c)), false
		}
		if a.FortressMask(c) != b.FortressMask(c) {
			return fmt.Sprintf("%v FortressMask: %#x vs %#x", c, a.FortressMask(c), b.FortressMask(c)), false
		}
	}
	if a.Hash() != b.Hash() {
		return fmt.Sprintf("CardOverlay.Hash() disagrees (%#x vs %#x) despite identical Frozen/Shielded/FusedWith/Fortress -- likely a Lava/Bomb/BlackHole zone-list divergence", a.Hash(), b.Hash()), false
	}
	return "", true
}
