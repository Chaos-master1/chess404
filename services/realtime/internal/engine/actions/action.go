// Package actions is Phase 2 of the engine rebuild -- the plan's L1
// "unified action space": one Action type covering both chess moves and
// card plays, so the search (engine/search, built on top of this package)
// can compare "play Freeze on the defender" against "play Nxe5" on the same
// footing and find the coordinated plan neither move alone gives ("Freeze
// the defender, then Bxh7 mates").
//
// Scope: this package does NOT cover all 37 card mechanics. It covers seven,
// chosen to span the tactically distinct categories a combined search
// actually needs to demonstrate: a single-target debuff (Freeze), a
// single-target buff (Shield), a zone/terrain placement (Fortress), a
// delayed single-square trap (Lavaground), a delayed single-piece AoE
// (Unabomber), a delayed dual-square AoE (BlackHole), and piece fusion
// (HalfFuse/FullFusion). The other 30 mechanics (teleport, swap, clone,
// mindcontrol, borrow, parasite, promote/demote family, sniper/badsniper,
// doublemove, mirror, reverse, undo, gambler, radar, cheater, sacrifice,
// invisible, fakepiece, fog_village, joker) are NOT modeled as Actions here
// -- generateCardActions returns nil for them (see candidates.go), so they
// are simply never proposed to the search. Adding one is mechanical: a new
// Mechanic constant, a candidate proposer, and an apply case, following the
// pattern the existing seven establish.
//
// Every rule here is read from internal/match/match_cards.go's
// applySelectTarget (the exact case for each mechanic), not inferred from
// card text -- the same conformance discipline as engine/core's overlay
// work.
package actions

import "github.com/chess404/realtime/internal/engine/core"

// Mechanic identifies a card's rules mechanic, matching
// contracts.GameCard.Mechanic's wire-format string exactly.
type Mechanic string

const (
	MechanicFreeze     Mechanic = "freeze"
	MechanicShield     Mechanic = "shield"
	MechanicFortress   Mechanic = "fortress"
	MechanicLavaground Mechanic = "lavaground"
	MechanicUnabomber  Mechanic = "unabomber"
	MechanicBlackhole  Mechanic = "blackhole"
	MechanicHalfFuse   Mechanic = "halffuse"
	MechanicFullFusion Mechanic = "fullfusion"
)

// CardInstance is one card in a hand -- just enough for rules purposes.
// The wire format's cosmetic fields (Name, Rarity, Color, Icon, Desc, ...)
// don't affect rules and are deliberately omitted; ID is kept only because
// removing a specific card from a Hand (once played) needs to identify
// which one, matching internal/match's own CardID-keyed removal.
type CardInstance struct {
	ID       string
	Mechanic Mechanic
}

// Hand is a player's held cards -- a plain slice (small, rarely mutated
// mid-search compared to piece moves) rather than a bitset-per-mechanic,
// since Mechanic is a string and the max hand size is 10
// (contracts.Piece-adjacent constant in internal/match), not 64 squares.
type Hand []CardInstance

// HasAnyCard reports whether h holds at least one card. internal/match
// suppresses checkmate/stalemate detection while the side to move holds ANY
// card, regardless of mechanic (match_cards.go's
// shouldEvaluateAutomaticMatchFinish) -- see terminal.go, which uses this.
func (h Hand) HasAnyCard() bool { return len(h) > 0 }

// Without returns a copy of h with the card matching id removed (a no-op
// copy if id isn't present) -- mirrors internal/match's removeCardFromHand,
// used once an Action's card is applied.
func (h Hand) Without(id string) Hand {
	out := make(Hand, 0, len(h))
	for _, c := range h {
		if c.ID == id {
			continue
		}
		out = append(out, c)
	}
	return out
}

// ActionKind discriminates a chess move from a card play.
type ActionKind int

const (
	ActionMove ActionKind = iota
	ActionCard
)

// CardTargets carries however many squares a mechanic's targeting needs:
// zero (none of the seven modeled here, but kept for future mechanics like
// doublemove/undo/mirror that resolve without a target), one (Freeze,
// Shield, Fortress, Lavaground, Unabomber), or two (BlackHole, HalfFuse,
// FullFusion). This is the search's view of "play this card" as ONE atomic
// action -- internal/match's client-facing protocol submits the two-target
// mechanics as two sequential select_target intents (contracts.PendingCardState),
// but the search only cares about the final, complete effect.
type CardTargets struct {
	NumTargets     int
	First, Second  core.Square
}

// Action is one atomic thing a player can do: a chess move, or playing one
// card with its targets. A tagged union over a small value type (like
// core.Move), not an interface -- generating or comparing an Action never
// allocates.
type Action struct {
	Kind    ActionKind
	Move    core.Move
	Card    CardInstance
	Targets CardTargets
}

// GenerateActions returns every action the side to move may take right
// now: card plays (only if allowCard -- see the turn-model doc below) plus
// every submittable chess move (core.GenerateSubmittableMoves, which is
// already Frozen-aware). Deliberately NOT limited to what will turn out to
// be "good"; K-truncation happens per-mechanic in candidates.go, using
// cheap heuristics the plan calls for -- not full enumeration of the
// underlying (up to 64-square, or up to C(64,2)-pair) target space.
//
// Turn model: internal/match itself allows unbounded card plays before a
// move (state.Turn only flips in applyMove, never in applyPlayCard/
// applySelectTarget -- chess.go has no card-count gate at all). This
// package deliberately tightens that to the plan's "at most one card + one
// move per turn" (matching client convention, and avoiding a combinatorial
// blowup in the search tree): callers pass allowCard=true at the start of a
// turn, and allowCard=false immediately after choosing an ActionCard, so a
// second consecutive card is never generated -- only a chess move can end
// that turn. See engine/search for how this threads through the search
// loop (no side-to-move flip after an ActionCard, only after an ActionMove).
func GenerateActions(p *core.Position, ov *core.CardOverlay, hand Hand, allowCard bool) []Action {
	var out []Action
	if allowCard {
		for _, card := range hand {
			out = append(out, generateCardActions(p, ov, card)...)
		}
	}
	for _, m := range core.GenerateSubmittableMoves(p, ov) {
		out = append(out, Action{Kind: ActionMove, Move: m})
	}
	return out
}
