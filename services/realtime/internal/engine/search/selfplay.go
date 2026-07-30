package search

import (
	"math/rand"

	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
)

// SelfPlayRecord is one recorded position from a self-play game, in
// exactly the fields needed to reconstruct nnue.ActiveFeatures' input:
// FEN for piece placement/side/castling/en-passant, plus hand sizes and
// overlay COUNTS for the card-aware features -- not exact squares, since
// ActiveFeatures only ever buckets by count, never by which specific
// square is frozen/shielded. Label is the training target: the FINAL game
// outcome (+1 White win, -1 Black win, 0 draw) from White's perspective,
// scaled to roughly the placeholder eval's material units so a network
// trained on it produces scores comparable to (and swappable with) the
// Phase 2 eval it replaces.
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

// GenerateSelfPlayGame plays one full game using eval-driven search for
// both sides (a fresh Searcher per decision, matching how a real match
// calls the engine independently each turn), with REAL, freshly-sampled
// hands for both sides (actions.SampleHand from the actual rarity-weighted
// 37-card pool) -- unlike the old engine's self-play pipeline, which the
// plan flags as never actually playing a card during self-play at all
// (selfplay.go there only ever calls applyMoveCopy). Cards are real
// candidate actions at every decision here, so a self-play game can and
// does draw on and play them exactly like a real match would, producing
// training signal for the card-aware features ActiveFeatures adds, not
// just chess ones.
//
// Turns are driven as up to two explicit BestMove calls (allowCard=true,
// then allowCard=false if the first came back as a card) rather than
// letting one BestMove call resolve a whole turn internally, because
// self-play needs to inspect and apply each half separately (record the
// pre-card position, apply the card, then record and search the
// resulting position for the mandatory move).
func GenerateSelfPlayGame(eval Evaluator, rng *rand.Rand, depth, maxPlies, handSize int) []SelfPlayRecord {
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

	mover := core.White
	for ply := 0; ply < maxPlies; ply++ {
		status := actions.TerminalStatus(p, ov, hands.For(mover))
		if status == core.Checkmate {
			if mover == core.White {
				outcome = -1
			} else {
				outcome = 1
			}
			break
		}
		if status == core.Stalemate {
			outcome = 0
			break
		}

		recorded = append(recorded, snapshotPartial(p, ov, hands))

		s := NewSearcherWithEval(eval)
		chosen, _ := s.BestMove(p, ov, hands, mover, true, depth)
		if chosen.Kind == actions.ActionCard {
			u := actions.ApplyCardAction(p, ov, chosen)
			_ = u // self-play never undoes -- the card is genuinely played
			hands = hands.With(mover, hands.For(mover).Without(chosen.Card.ID))

			recorded = append(recorded, snapshotPartial(p, ov, hands))

			s2 := NewSearcherWithEval(eval)
			chosen, _ = s2.BestMove(p, ov, hands, mover, false, depth)
		}

		// chosen is now guaranteed a move (GenerateActions always includes
		// every submittable move; a hand with cards but no legal card
		// targets, or allowCard=false, still leaves moves on the table).
		// applyMoveWithTicks applies the move immediately as a side effect
		// and returns a closure to UNDO it -- self-play deliberately never
		// calls that closure, since the whole point here is to advance the
		// game forward and keep the move applied.
		_ = applyMoveWithTicks(p, ov, mover, chosen.Move)
		mover = mover.Opposite()
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
