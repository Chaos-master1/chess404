package v1

// Engine-vs-engine strength measurement.
//
// Before this file there was no way to tell whether a change made the engine
// stronger. The test suite could prove a change compiled, passed perft, and
// returned a non-nil move -- nothing more. Every tuning decision was therefore
// a guess, and a regression was indistinguishable from an improvement.
//
// This provides the missing gate: play two configurations head to head over
// colour-balanced opening pairs, then report the Elo difference with an error
// margin and an SPRT verdict. Nothing in the rebuild should be declared an
// improvement without a run through here.

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// Outcome is the result of a single game, always from White's point of view.
type Outcome int

const (
	OutcomeWhiteWin Outcome = iota
	OutcomeBlackWin
	OutcomeDraw
)

// Contender is one side of a match-up: a named search configuration.
type Contender struct {
	Name string
	// TimeLimit is the per-move search budget.
	TimeLimit time.Duration
	// MaxDepth caps iterative deepening. 0 means "use the usual high cap and
	// let TimeLimit do the work", which is how the production opponent runs.
	MaxDepth int
	// TTSizeEntries is the transposition table size. 0 uses the same size the
	// production ComputerOpponent uses, so a gauntlet result transfers.
	TTSizeEntries int
}

func (c Contender) maxDepth() int {
	if c.MaxDepth > 0 {
		return c.MaxDepth
	}
	return 32
}

func (c Contender) ttSize() int {
	if c.TTSizeEntries > 0 {
		return c.TTSizeEntries
	}
	return 1 << 16
}

// GauntletConfig controls a match-up.
type GauntletConfig struct {
	// Pairs is the number of opening pairs. Each pair is played twice with
	// colours swapped, so the total game count is Pairs*2. Pairing is the main
	// variance-reduction lever: both engines get the same openings from both
	// sides, so an easy opening can't favour one of them.
	Pairs int
	// OpeningPlies is how many random plies to play out to create each opening.
	// Openings are derived from Seed, so a rerun with the same seed replays the
	// same book -- results are comparable across runs.
	OpeningPlies int
	// MaxPly ends an unfinished game as a draw. The engine has no repetition
	// detection, so without this a drawn ending can shuffle indefinitely.
	MaxPly int
	Seed   int64
	// Elo0/Elo1 are the SPRT hypothesis bounds: H0 "no better than Elo0" vs
	// H1 "at least Elo1 better". Alpha/Beta are the error rates.
	Elo0, Elo1  float64
	Alpha, Beta float64
	// OnGame, if set, is called after each game for progress reporting.
	OnGame func(played int, r GauntletResult)
}

// DefaultGauntletConfig is a reasonable starting point: +0 vs +10 Elo at 5%
// error rates, the standard bounds for "is this change a real improvement".
func DefaultGauntletConfig() GauntletConfig {
	return GauntletConfig{
		Pairs:        50,
		OpeningPlies: 6,
		MaxPly:       250,
		Seed:         1,
		Elo0:         0,
		Elo1:         10,
		Alpha:        0.05,
		Beta:         0.05,
	}
}

// GauntletResult is the running score of a match-up, always from A's point of
// view (A wins / A losses / draws).
type GauntletResult struct {
	AWins, BWins, Draws int
}

// Games is the number of decided-or-drawn games played so far.
func (r GauntletResult) Games() int { return r.AWins + r.BWins + r.Draws }

// Score is A's score rate in [0,1]: a win counts 1, a draw 0.5.
func (r GauntletResult) Score() float64 {
	n := r.Games()
	if n == 0 {
		return 0.5
	}
	return (float64(r.AWins) + 0.5*float64(r.Draws)) / float64(n)
}

// Elo is A's estimated rating advantage over B, in Elo points. A clean sweep
// is unbounded, so it is reported as +/-Inf rather than silently clamped.
func (r GauntletResult) Elo() float64 {
	return eloFromScore(r.Score())
}

func eloFromScore(score float64) float64 {
	if score <= 0 {
		return math.Inf(-1)
	}
	if score >= 1 {
		return math.Inf(1)
	}
	return -400 * math.Log10(1/score-1)
}

// EloErrorMargin is the half-width of the ~95% confidence interval on Elo,
// derived from the standard error of the score rate. A result whose margin is
// wider than the difference it claims has not measured anything.
func (r GauntletResult) EloErrorMargin() float64 {
	n := r.Games()
	if n < 2 {
		return math.Inf(1)
	}
	mu := r.Score()
	// Trinomial variance of the per-game score around the observed mean.
	variance := (float64(r.AWins)*sq(1-mu) + float64(r.Draws)*sq(0.5-mu) + float64(r.BWins)*sq(0-mu)) / float64(n)
	if variance <= 0 {
		return 0
	}
	stdErr := math.Sqrt(variance / float64(n))
	lo := eloFromScore(clamp01(mu - 1.96*stdErr))
	hi := eloFromScore(clamp01(mu + 1.96*stdErr))
	if math.IsInf(lo, 0) || math.IsInf(hi, 0) {
		return math.Inf(1)
	}
	return (hi - lo) / 2
}

// SPRTVerdict is the sequential test's conclusion.
type SPRTVerdict string

const (
	// SPRTAcceptH1 means the change is accepted as a real improvement.
	SPRTAcceptH1 SPRTVerdict = "accept H1 (improvement)"
	// SPRTAcceptH0 means the change is rejected as no improvement.
	SPRTAcceptH0 SPRTVerdict = "accept H0 (no improvement)"
	// SPRTContinue means more games are needed to decide.
	SPRTContinue SPRTVerdict = "continue"
)

// LLR is the sequential probability ratio test's log-likelihood ratio under
// the normal approximation to the trinomial model -- the same formulation
// chess engine testing frameworks use. It lets a match-up stop as soon as the
// evidence is conclusive rather than always playing a fixed game count.
//
// Returns 0 (no evidence either way) whenever the sample has essentially no
// spread to measure from -- either too few games, or, degenerately, a run
// that so far is entirely draws. A naive implementation "nudges" a zero
// variance up to some tiny epsilon so the division stays finite; that is
// wrong, not just inelegant; it turns "we have no information yet" into
// "we have overwhelming information", because dividing by a near-zero
// denominator amplifies whatever tiny, meaningless residual happens to be in
// the numerator into an arbitrarily large LLR. Caught by
// TestGauntletDetectsAKnownStrengthGap: 2 games, both drawn, produced an LLR
// of -207 and an immediate (wrong) "no improvement" verdict.
func (r GauntletResult) LLR(elo0, elo1 float64) float64 {
	n := r.Games()
	if n < 2 {
		return 0
	}
	mu := r.Score()
	variance := (float64(r.AWins)*sq(1-mu) + float64(r.Draws)*sq(0.5-mu) + float64(r.BWins)*sq(0-mu)) / float64(n)
	const minMeasurableVariance = 1e-4
	if variance < minMeasurableVariance {
		return 0
	}
	s0 := scoreFromElo(elo0)
	s1 := scoreFromElo(elo1)
	return float64(n) * (s1 - s0) * (mu - (s0+s1)/2) / variance
}

// minSPRTGames is a floor below which SPRT never returns a verdict,
// regardless of what the raw LLR happens to compute to. The normal
// approximation the LLR is built on only holds once enough games have been
// played for the trinomial score distribution to be well-behaved; real
// engine-testing frameworks (Fishtest, cutechess-cli) apply the same kind of
// floor for the same reason. Defense in depth alongside the variance guard in
// LLR -- either one alone would have caught the false-positive in
// TestGauntletDetectsAKnownStrengthGap.
const minSPRTGames = 10

// SPRT returns the verdict and the current LLR against the configured bounds.
func (r GauntletResult) SPRT(elo0, elo1, alpha, beta float64) (SPRTVerdict, float64) {
	llr := r.LLR(elo0, elo1)
	if r.Games() < minSPRTGames {
		return SPRTContinue, llr
	}
	upper := math.Log((1 - beta) / alpha)
	lower := math.Log(beta / (1 - alpha))
	switch {
	case llr >= upper:
		return SPRTAcceptH1, llr
	case llr <= lower:
		return SPRTAcceptH0, llr
	default:
		return SPRTContinue, llr
	}
}

func scoreFromElo(elo float64) float64 { return 1 / (1 + math.Pow(10, -elo/400)) }
func sq(x float64) float64             { return x * x }

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// Summary renders the result the way an engine developer expects to read it.
func (r GauntletResult) Summary(aName, bName string, elo0, elo1, alpha, beta float64) string {
	verdict, llr := r.SPRT(elo0, elo1, alpha, beta)
	elo := r.Elo()
	margin := r.EloErrorMargin()

	eloStr := fmt.Sprintf("%+.1f", elo)
	if math.IsInf(elo, 0) {
		eloStr = "inf"
	}
	marginStr := fmt.Sprintf("%.1f", margin)
	if math.IsInf(margin, 0) {
		marginStr = "inf"
	}

	return fmt.Sprintf(
		"%s vs %s: %d games  W%d L%d D%d  score %.3f  Elo %s +/- %s  LLR %.2f  -> %s",
		aName, bName, r.Games(), r.AWins, r.BWins, r.Draws,
		r.Score(), eloStr, marginStr, llr, verdict,
	)
}

// PlayGame plays one complete game and returns the outcome from White's point
// of view. Terminal detection is the engine's own: no legal move is mate or
// stalemate depending on check, plus insufficient material, plus a ply cap
// standing in for the repetition detection the search does not yet have.
func PlayGame(white, black Contender, opening *contracts.MatchState, maxPly int) Outcome {
	state := cloneMatchState(opening)
	whiteTT := NewTranspositionTable(white.ttSize())
	blackTT := NewTranspositionTable(black.ttSize())

	for ply := 0; ply < maxPly; ply++ {
		forWhite := state.Turn == "white"

		if isInsufficientMaterial(state.Board) {
			return OutcomeDraw
		}

		moves := generateAllMoves(state, forWhite)
		if len(moves) == 0 {
			if isKingInCheck(state) {
				// Side to move is mated.
				if forWhite {
					return OutcomeBlackWin
				}
				return OutcomeWhiteWin
			}
			return OutcomeDraw
		}

		side, tt := white, whiteTT
		if !forWhite {
			side, tt = black, blackTT
		}

		result := SearchWithTime(state, side.maxDepth(), tt, side.TimeLimit)
		best := result.BestMove
		if best.From == best.To {
			// The search returned nothing usable (it can bail on the deadline
			// before completing even depth 1). Fall back to the first legal
			// move so the game continues rather than scoring a phantom loss.
			best = moves[0]
		}
		state = applyMoveCopy(state, &best)
	}

	// Hit the ply cap with neither side converting: score it a draw.
	return OutcomeDraw
}

// RunGauntlet plays a colour-balanced match-up between two contenders and
// returns the accumulated result. It stops early once SPRT reaches a verdict.
func RunGauntlet(a, b Contender, cfg GauntletConfig) GauntletResult {
	rng := rand.New(rand.NewSource(cfg.Seed))
	var result GauntletResult

	for pair := 0; pair < cfg.Pairs; pair++ {
		opening := randomOpening(rng, cfg.OpeningPlies)

		// Game 1: A as White. Game 2: the same opening, colours swapped. Any
		// advantage baked into the opening is then handed to both engines
		// equally, which is what makes small Elo differences measurable at
		// reasonable game counts.
		for game := 0; game < 2; game++ {
			var outcome Outcome
			if game == 0 {
				outcome = PlayGame(a, b, opening, cfg.MaxPly)
				switch outcome {
				case OutcomeWhiteWin:
					result.AWins++
				case OutcomeBlackWin:
					result.BWins++
				default:
					result.Draws++
				}
			} else {
				outcome = PlayGame(b, a, opening, cfg.MaxPly)
				switch outcome {
				case OutcomeWhiteWin:
					result.BWins++
				case OutcomeBlackWin:
					result.AWins++
				default:
					result.Draws++
				}
			}

			if cfg.OnGame != nil {
				cfg.OnGame(result.Games(), result)
			}
		}

		if verdict, _ := result.SPRT(cfg.Elo0, cfg.Elo1, cfg.Alpha, cfg.Beta); verdict != SPRTContinue {
			return result
		}
	}

	return result
}

// randomOpening plays out a few random legal plies from the initial position.
// Derived from the caller's seeded rng, so a given config always produces the
// same opening book.
func randomOpening(rng *rand.Rand, plies int) *contracts.MatchState {
	state := MatchStateFromFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	for i := 0; i < plies; i++ {
		moves := generateAllMoves(state, state.Turn == "white")
		if len(moves) == 0 {
			break
		}
		m := moves[rng.Intn(len(moves))]
		state = applyMoveCopy(state, &m)
	}
	return state
}
