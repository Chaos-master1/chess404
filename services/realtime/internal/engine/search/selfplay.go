package search

import (
	"math/rand"
	"time"

	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
)

// SelfPlayRecord is one recorded position from a self-play game, in
// exactly the fields needed to reconstruct nnue.ActiveFeatures' input:
// FEN for piece placement/side/castling/en-passant, plus hand sizes and
// overlay COUNTS for the card-aware features -- not exact squares, since
// ActiveFeatures only ever buckets by count, never by which specific
// square is frozen/shielded. Label is the training target, scaled to
// roughly the placeholder eval's material units so a network trained on
// it produces scores comparable to (and swappable with) the Phase 2 eval
// it replaces: +1/-1 (scaled) for a genuine checkmate/decided game from
// White's perspective, 0 for a genuine stalemate, or -- for a game that
// hit the ply cap or got stuck with no available action, both artifacts
// of shallow self-play rather than real chess-rules results -- a graded
// value from adjudicateByMaterial reflecting who was actually ahead when
// the game stopped, per GenerateSelfPlayGame's `decided` handling.
type SelfPlayRecord struct {
	FEN           string  `json:"fen"`
	WhiteHandSize int     `json:"whiteHandSize"`
	BlackHandSize int     `json:"blackHandSize"`
	FrozenWhite   int     `json:"frozenWhite"`
	FrozenBlack   int     `json:"frozenBlack"`
	ShieldedWhite int     `json:"shieldedWhite"`
	ShieldedBlack int     `json:"shieldedBlack"`
	FortressWhite bool    `json:"fortressWhite"`
	FortressBlack bool    `json:"fortressBlack"`
	Label         float64 `json:"label"`
}

// outcomeScale converts a -1/0/+1 game result into the placeholder eval's
// material units (pawn=100) -- roughly "a bit more than a rook" of
// confidence for a decisive result, comparable in scale to what a real
// position's material balance would read.
const outcomeScale = 600.0

// selfPlayTTSlots sizes the throwaway TT each self-play decision gets --
// see NewSearcherWithEvalAndTTSize's doc comment for why this can't just
// use the default (1<<20, ~33MB): a shallow depth-2 search visits at most
// a few thousand nodes, nowhere near enough to need a million slots, and
// allocating the default size fresh for every ply of every game is what
// caused a real out-of-memory crash generating a multi-hundred-game
// dataset.
const selfPlayTTSlots = 1 << 14

// adjudicationScale is the material-score magnitude (materialScore's
// centipawn-ish units, see eval.go) treated as "a full point" of
// adjudicated outcome -- a queen (900) up or down. Chosen so a clearly
// won position (up a whole minor piece or more) still reads as solidly
// decisive after clamping, while a marginal edge stays a fraction of a
// point rather than being rounded up to a full "win".
const adjudicationScale = 900.0

// adjudicateByMaterial grades an UNFINISHED self-play game (hit the ply
// cap, or mover got stuck with nothing available -- see GenerateSelfPlayGame's
// `decided` handling) by final material balance rather than flattening it
// to a draw. clampFloat keeps the result within the same [-1, 1] range a
// genuine checkmate produces, so Label stays on one consistent scale
// regardless of how the game actually ended.
func adjudicateByMaterial(p *core.Position) float64 {
	return clampFloat(float64(materialScore(p))/adjudicationScale, -1, 1)
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// decisionFunc picks one action for mover at (p, ov, hands), returning
// ok=false exactly when BestMove/BestMoveTimed would (see their doc
// comments) -- the abstraction generateSelfPlayGameWithDecider needs to
// share its turn loop between a fixed-depth search (GenerateSelfPlayGame)
// and a wall-clock-budgeted one (GenerateSelfPlayGameTimed) without
// duplicating that loop.
type decisionFunc func(p *core.Position, ov *core.CardOverlay, hands Hands, mover core.Color, allowCard bool) (actions.Action, bool)

// GenerateSelfPlayGame plays one full game using a FIXED search depth for
// both sides. See generateSelfPlayGameWithDecider for the shared turn
// loop and GenerateSelfPlayGameTimed for the wall-clock-budgeted
// counterpart -- fixed low depth (2, in every dataset generated so far)
// produces weak, tactically shallow games; prefer the timed variant for
// actual training data unless a specific fixed depth is what's being
// tested.
func GenerateSelfPlayGame(eval Evaluator, rng *rand.Rand, depth, maxPlies, handSize int) []SelfPlayRecord {
	decide := func(p *core.Position, ov *core.CardOverlay, hands Hands, mover core.Color, allowCard bool) (actions.Action, bool) {
		s := NewSearcherWithEvalAndTTSize(eval, selfPlayTTSlots)
		a, _, ok := s.BestMove(p, ov, hands, mover, allowCard, depth)
		return a, ok
	}
	return generateSelfPlayGameWithDecider(decide, rng, maxPlies, handSize)
}

// GenerateSelfPlayGameTimed plays one full game using BestMoveTimed (a
// wall-clock time budget with iterative deepening, see iterative.go) for
// both sides instead of a fixed depth -- meaningfully stronger, more
// tactically sound games for the same reason BestMoveTimed exists at all:
// depth 2 can find an advantage but routinely can't calculate all the way
// to actually converting it, producing self-play games dominated by
// shuffling rather than real chess. Confirmed necessary directly: a
// network trained on fixed-depth-2 self-play data got even unambiguous
// whole-queen material imbalances backwards on manual inspection (Task 10
// gauntlet follow-up), most plausibly because depth-2 games' final
// outcomes are dominated by which side stumbled into a good card sequence
// rather than by the actual material/positional state of any given
// recorded position.
func GenerateSelfPlayGameTimed(eval Evaluator, rng *rand.Rand, timePerMove time.Duration, maxDepth, maxPlies, handSize int) []SelfPlayRecord {
	decide := func(p *core.Position, ov *core.CardOverlay, hands Hands, mover core.Color, allowCard bool) (actions.Action, bool) {
		s := NewSearcherWithEvalAndTTSize(eval, selfPlayTTSlots)
		a, _, _, ok := s.BestMoveTimed(p, ov, hands, mover, allowCard, timePerMove, maxDepth)
		return a, ok
	}
	return generateSelfPlayGameWithDecider(decide, rng, maxPlies, handSize)
}

// generateSelfPlayGameWithDecider is the shared turn loop both
// GenerateSelfPlayGame and GenerateSelfPlayGameTimed drive: REAL,
// freshly-sampled hands for both sides (actions.SampleHand from the
// actual rarity-weighted 37-card pool) -- unlike the old engine's
// self-play pipeline, which the plan flags as never actually playing a
// card during self-play at all (selfplay.go there only ever calls
// applyMoveCopy). Cards are real candidate actions at every decision
// here, so a self-play game can and does draw on and play them exactly
// like a real match would, producing training signal for the card-aware
// features ActiveFeatures adds, not just chess ones.
//
// Turns are driven as up to two explicit decide calls (allowCard=true,
// then allowCard=false if the first came back as a card) rather than
// resolving a whole turn in one call, because self-play needs to inspect
// and apply each half separately (record the pre-card position, apply
// the card, then record and search the resulting position for the
// mandatory move).
func generateSelfPlayGameWithDecider(decide decisionFunc, rng *rand.Rand, maxPlies, handSize int) []SelfPlayRecord {
	p := core.NewStartingPosition()
	ov := core.NewCardOverlay()
	hands := Hands{
		White: actions.SampleHand(rng, handSize),
		Black: actions.SampleHand(rng, handSize),
	}

	type partial struct {
		fen                          string
		whiteHandSize, blackHandSize int
		frozenWhite, frozenBlack     int
		shieldedWhite, shieldedBlack int
		fortressWhite, fortressBlack bool
	}
	var recorded []partial
	outcome := 0.0
	// decided is true only once a genuine chess-rules result (checkmate,
	// or a real stalemate) has set outcome -- both are correct, final
	// answers, not artifacts of this harness's limits. Every OTHER way
	// the loop ends (the ply cap, or mover getting stuck with literally
	// nothing available) is an artifact of shallow-search self-play, not
	// a real game-theoretic draw: self-play games can run out the full
	// ply budget shuffling around a large material edge without ever
	// finding the (often many-ply) forcing sequence to actually deliver
	// mate -- confirmed directly (a smoke-test batch came back 100%
	// outcome 0 across every recorded position at fixed depth 2).
	// Labeling every such game a flat draw would throw away exactly the
	// signal a value network most needs -- "this side is clearly
	// winning" -- so those cases are adjudicated by final material
	// balance instead (see adjudicateByMaterial), the standard technique
	// real engines' self-play pipelines use for unfinished games, rather
	// than only ever training on the rare game that reaches an actual
	// mate.
	decided := false

	mover := core.White
	for ply := 0; ply < maxPlies; ply++ {
		status := actions.TerminalStatus(p, ov, hands.For(mover))
		if status == core.Checkmate {
			if mover == core.White {
				outcome = -1
			} else {
				outcome = 1
			}
			decided = true
			break
		}
		if status == core.Stalemate {
			outcome = 0
			decided = true
			break
		}

		recorded = append(recorded, snapshotPartial(p, ov, hands))

		chosen, ok := decide(p, ov, hands, mover, true)
		if !ok {
			// mover has genuinely nothing available (see BestMove's doc
			// comment: TerminalStatus is Frozen-blind, so this can happen
			// without the position being checkmate/stalemate, e.g. every
			// mobile piece is frozen). Not a real draw -- adjudicate below.
			break
		}
		if chosen.Kind == actions.ActionCard {
			u := actions.ApplyCardAction(p, ov, chosen)
			_ = u // self-play never undoes -- the card is genuinely played
			hands = hands.With(mover, hands.For(mover).Without(chosen.Card.ID))

			recorded = append(recorded, snapshotPartial(p, ov, hands))

			chosen, ok = decide(p, ov, hands, mover, false)
			if !ok {
				// The card is already committed for real (self-play never
				// undoes a played card); with no mandatory move available
				// to complete the turn, the game ends here -- adjudicated
				// below, same as the ply-cap case.
				break
			}
		}

		// chosen is now guaranteed a real move (GenerateActions always
		// includes every submittable move; a hand with cards but no legal
		// card targets, or allowCard=false, still leaves moves on the
		// table, and the ok checks above rule out the zero-actions case).
		// applyMoveWithTicks applies the move immediately as a side effect
		// and returns a closure to UNDO it -- self-play deliberately never
		// calls that closure, since the whole point here is to advance the
		// game forward and keep the move applied.
		_ = applyMoveWithTicks(p, ov, mover, chosen.Move)
		mover = mover.Opposite()
	}
	if !decided {
		outcome = adjudicateByMaterial(p)
	}

	records := make([]SelfPlayRecord, len(recorded))
	for i, r := range recorded {
		records[i] = SelfPlayRecord{
			FEN: r.fen, WhiteHandSize: r.whiteHandSize, BlackHandSize: r.blackHandSize,
			FrozenWhite: r.frozenWhite, FrozenBlack: r.frozenBlack,
			ShieldedWhite: r.shieldedWhite, ShieldedBlack: r.shieldedBlack,
			FortressWhite: r.fortressWhite, FortressBlack: r.fortressBlack,
			Label: outcome * outcomeScale,
		}
	}
	return records
}

func snapshotPartial(p *core.Position, ov *core.CardOverlay, hands Hands) struct {
	fen                          string
	whiteHandSize, blackHandSize int
	frozenWhite, frozenBlack     int
	shieldedWhite, shieldedBlack int
	fortressWhite, fortressBlack bool
} {
	return struct {
		fen                          string
		whiteHandSize, blackHandSize int
		frozenWhite, frozenBlack     int
		shieldedWhite, shieldedBlack int
		fortressWhite, fortressBlack bool
	}{
		fen:           p.ToFEN(),
		whiteHandSize: len(hands.White), blackHandSize: len(hands.Black),
		frozenWhite: countOverlay(p, ov, core.White, ov.IsFrozen), frozenBlack: countOverlay(p, ov, core.Black, ov.IsFrozen),
		shieldedWhite: countOverlay(p, ov, core.White, ov.IsShielded), shieldedBlack: countOverlay(p, ov, core.Black, ov.IsShielded),
		fortressWhite: ov.HasFortress(core.White), fortressBlack: ov.HasFortress(core.Black),
	}
}

func countOverlay(p *core.Position, _ *core.CardOverlay, c core.Color, has func(core.Square) bool) int {
	count := 0
	pieces := p.Occupied(c)
	for pieces.Any() {
		var sq core.Square
		sq, pieces = pieces.PopLSB()
		if has(sq) {
			count++
		}
	}
	return count
}
