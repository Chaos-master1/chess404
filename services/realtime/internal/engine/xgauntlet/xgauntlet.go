// Package xgauntlet is the cross-engine gauntlet -- Phase E0 of the
// pre-launch engine programme. Before this package existed there was no way
// to answer "is the new engine (internal/engine/search, the rebuild) better
// than the one actually running in production (internal/engine, via
// ComputerOpponent) yet?" The two existing gauntlets can't answer it:
// internal/engine/gauntlet.go only pits old-engine search CONFIGS against
// each other (same engine, different depth/time/TT size), and
// internal/engine/search/gauntlet.go only pits EVALUATORS against each other
// inside the new search (same search, different leaf eval). Neither can put
// the two actual engines on the board against one another.
//
// Design: every game is played through the REAL internal/match.Service --
// production's own rules engine, cards included -- using only its public API
// (CreateMatch / GetMatch / ApplyIntent). This package has zero rules
// knowledge of its own; it only knows how to ask an Engine "what do you want
// to do with this position" and submit the answer. That is deliberate: a
// bespoke reimplementation (as the old gauntlet.go's generateAllMoves/
// applyMoveCopy is) can drift from the real rules and silently measure
// something else. Driving the actual Service means a strength result here
// transfers directly to production, and any engine bug that only manifests
// through the real intent protocol (the exact class of bug behind the
// 2026-07-29 "push push push" outage) surfaces here too, not just in a
// separate wiring pass later.
//
// Result accounting reuses internal/engine's GauntletResult/Outcome/SPRT
// machinery unchanged (it is pure win/loss/draw counting with no dependency
// on how a game was produced), so a cross-engine result and an old-engine
// self-play baseline are directly comparable numbers.
package xgauntlet

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/engine"
	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/conform"
	"github.com/chess404/realtime/internal/engine/core"
	"github.com/chess404/realtime/internal/engine/search"
	"github.com/chess404/realtime/internal/match"
)

// Engine is the contract both the old and new engines are adapted to. It
// matches internal/engine.ComputerOpponent's existing shape exactly --
// MakeMove/HandleSelectTarget -- which is also the exact shape
// internal/match's autoPlayComputerDepthLimited drives production against
// (match_lifecycle.go:443,508). Reusing it here, rather than inventing a new
// interface, means this harness exercises precisely the boundary production
// exercises, for both engines equally.
type Engine interface {
	MakeMove(state *contracts.MatchState) *contracts.PlayerIntent
	HandleSelectTarget(state *contracts.MatchState) *contracts.PlayerIntent
}

// *engine.ComputerOpponent already satisfies Engine with no wrapper --
// verified at compile time.
var _ Engine = (*engine.ComputerOpponent)(nil)

// EngineFactory builds a fresh Engine instance seated as color ("white" or
// "black") for one game. A factory, not a shared instance, because
// *engine.ComputerOpponent carries a mutex and per-game move-ordering state
// (tt, cardEval's rng) that must not leak between games -- exactly how
// internal/match itself constructs one ComputerOpponent per match
// (match_lifecycle.go:99), never reused across matches.
type EngineFactory func(color string) Engine

// GameConfig controls how one game is set up and how long it is allowed to
// run.
type GameConfig struct {
	// MaxPly ends an unfinished game as a draw -- internal/match has no
	// repetition detection, so without this a drawn ending could shuffle
	// indefinitely (same rationale as internal/engine/gauntlet.go's MaxPly).
	MaxPly int
	// MaxSubDecisionsPerTurn bounds how many consecutive card plays a single
	// engine may make before it must move (or the harness gives up on that
	// turn and forces a fallback move) -- mirrors
	// autoPlayComputerDepthLimited's depth>5 guard (match_lifecycle.go:435),
	// which exists for the identical reason: a card never flips state.Turn,
	// so without a bound a buggy engine that always prefers "play another
	// card" over "move" would spin forever on one turn.
	MaxSubDecisionsPerTurn int
}

// DefaultGameConfig mirrors the bounds internal/match's own computer-opponent
// loop uses.
func DefaultGameConfig() GameConfig {
	return GameConfig{MaxPly: 250, MaxSubDecisionsPerTurn: 5}
}

// PlayOneGame plays one complete game between white and black through a real
// match.Service and returns the outcome from White's point of view, reusing
// engine.Outcome so results are directly comparable with the old gauntlet's.
//
// seed drives BOTH the match's own deterministic card draws (RNGSeed,
// contracts.CreateMatchRequest.Seed) and this function's opening-ply choices,
// so replaying the same seed against a different engine pairing hands both
// sides an identical opening and identical card sequence -- the same
// variance-reduction principle internal/engine/gauntlet.go's colour-swapped
// pairing uses, extended to cover the extra source of variance real games
// have that the old gauntlet's reimplementation does not: cards.
func PlayOneGame(svc *match.Service, white, black EngineFactory, cfg GameConfig, seed int64, openingPlies int) (engine.Outcome, error) {
	// vclock is a per-game VIRTUAL clock, not real wall time. internal/match
	// enforces a per-color, per-match intent rate limit (match_throttle.go:
	// 5-burst, 10/sec refill) as an anti-spam/anti-cheat safeguard -- a real
	// safeguard this harness must respect, not bypass. But this harness can
	// make decisions (especially the new engine at small gauntlet time
	// budgets) far faster in real wall-clock time than any human or the
	// production computer-opponent path ever would, which starved the token
	// bucket and produced "rate limited: too many intents" errors that were
	// a harness artifact, not an engine bug. Since ApplyIntent takes `now`
	// as a caller-supplied parameter (used for the rate limiter's own
	// elapsed-time math AND the match clock), advancing a fixed step per
	// call satisfies the limiter without any real sleep -- and the step is
	// small enough relative to any reasonable ClockSeconds that it never
	// manufactures a spurious clock-timeout event either.
	vclock := time.Now()
	const intentStep = 150 * time.Millisecond
	nextNow := func() time.Time {
		vclock = vclock.Add(intentStep)
		return vclock
	}

	const whiteGuest, blackGuest = "xgauntlet-white", "xgauntlet-black"
	const whiteSecret, blackSecret = "xgauntlet-white-secret", "xgauntlet-black-secret"

	snap := svc.CreateMatch(contracts.CreateMatchRequest{
		Seed:              seed,
		ModeID:            contracts.MatchModeOpenCards,
		WhiteGuestID:      whiteGuest,
		BlackGuestID:      blackGuest,
		WhitePlayerSecret: whiteSecret,
		BlackPlayerSecret: blackSecret,
		WhiteName:         "xgauntlet-white",
		BlackName:         "xgauntlet-black",
	}, vclock)
	matchID := snap.Match.MatchID

	rng := rand.New(rand.NewSource(seed))
	if err := playRandomOpening(svc, matchID, whiteGuest, whiteSecret, blackGuest, blackSecret, openingPlies, rng, nextNow); err != nil {
		return engine.OutcomeDraw, fmt.Errorf("xgauntlet: opening setup: %w", err)
	}

	whiteEngine := white("white")
	blackEngine := black("black")

	// pendingCardIsStale latches once a pending card is ever left dangling
	// (see the stuckPendingCard branch below) -- see its use above for why
	// that makes every future play_card attempt this game a guaranteed
	// rejection.
	pendingCardIsStale := false

	for ply := 0; ply < cfg.MaxPly; ply++ {
		snapResp, err := svc.GetMatch(matchID)
		if err != nil {
			return engine.OutcomeDraw, fmt.Errorf("xgauntlet: get match: %w", err)
		}
		state := &snapResp.Match

		if state.Status != "active" {
			return outcomeFromFinishedState(state), nil
		}

		movingEngine, playerID, playerSecret, moverColor := whiteEngine, whiteGuest, whiteSecret, "white"
		if state.Turn == "black" {
			movingEngine, playerID, playerSecret, moverColor = blackEngine, blackGuest, blackSecret, "black"
		}

		madeProgress := false
		for sub := 0; sub < cfg.MaxSubDecisionsPerTurn; sub++ {
			refreshed, err := svc.GetMatch(matchID)
			if err != nil {
				return engine.OutcomeDraw, fmt.Errorf("xgauntlet: get match: %w", err)
			}
			state = &refreshed.Match
			if state.Status != "active" {
				return outcomeFromFinishedState(state), nil
			}

			var intent *contracts.PlayerIntent
			if pendingCardIsStale || state.PendingCard != nil {
				// A PendingCard is already set at the top of a fresh
				// decision -- either a stale one abandoned earlier this
				// game (pendingCardIsStale, see the stuckPendingCard branch
				// below), or, defensively, ANY other case this harness's
				// own bookkeeping did not anticipate. Either way,
				// applyPlayCard's very first guard rejects ANY new
				// play_card intent -- from either color -- for as long as
				// ANY PendingCard is set, regardless of whose it is
				// (match_cards.go:29-31) -- a fact that holds regardless of
				// WHY a PendingCard is present, so checking for it directly
				// here is strictly more robust than only trusting the
				// pendingCardIsStale flag to have caught every path that
				// could lead here. Skip straight to a normal move rather
				// than spending a decision on a play_card call already
				// known to fail.
				if state.PendingCard != nil {
					pendingCardIsStale = true
				}
				intent, _ = fallbackMove(state)
			} else {
				intent = movingEngine.MakeMove(state)
			}
			if intent == nil {
				// No card worth playing and (per this engine's own judgment)
				// no move -- fall back to any legal move so the harness never
				// stalls on a decision an engine failed to make. Mirrors
				// ensureComputerMadeProgressLocked's role in production
				// (match_lifecycle.go:556): a best-effort engine call is
				// allowed to give up; the harness, like the real service,
				// guarantees the game still progresses.
				fallback, ferr := fallbackMove(state)
				if ferr != nil {
					// No legal move AND no card played -- this is checkmate
					// or stalemate that internal/match's own automatic-finish
					// evaluation should have already caught via the last
					// applied intent. If we get here, treat it as a draw
					// rather than looping forever.
					return engine.OutcomeDraw, nil
				}
				intent = fallback
			}
			intent.PlayerID = playerID
			intent.PlayerSecret = playerSecret
			intent.MatchID = matchID

			if _, err := svc.ApplyIntent(*intent, nextNow()); err != nil {
				// A rejected intent from either engine is a real bug (E1-E6
				// exist to find and fix exactly this class of problem for the
				// new engine) -- surfaced to the caller rather than silently
				// skipped, so a gauntlet run's error rate is itself a signal.
				return engine.OutcomeDraw, fmt.Errorf("xgauntlet: engine %s submitted an invalid intent (type=%s): %w", state.Turn, intent.Type, err)
			}
			madeProgress = true
			stuckPendingCard := false

			// Resolve any pending card target(s) before deciding whether the
			// turn continues -- exactly the sequence
			// autoPlayComputerDepthLimited runs (match_lifecycle.go:507-527).
			for guard := 0; guard < 3; guard++ {
				pendingSnap, err := svc.GetMatch(matchID)
				if err != nil {
					return engine.OutcomeDraw, fmt.Errorf("xgauntlet: get match: %w", err)
				}
				pendingState := &pendingSnap.Match
				if pendingState.PendingCard == nil {
					break
				}
				if pendingState.PendingCard.OwnerColor != moverColor {
					// Not this ply's own card -- a stale one dangling from
					// an earlier stuckPendingCard episode (see below), whose
					// OwnerColor is frozen at whoever got stuck while Turn
					// keeps flipping normally via moves. Resolving it as if
					// it were fresh would submit a select_target stamped
					// with the WRONG color's identity and get rejected with
					// "only the card owner can select the target" -- found
					// by xgauntlet's E0 cross-engine gauntlet hitting exactly
					// that on a later, unrelated turn. Leave it untouched;
					// pendingCardIsStale (set when it was first abandoned)
					// already stops either side from attempting a new
					// play_card for the rest of this game.
					break
				}
				targetIntent := movingEngine.HandleSelectTarget(pendingState)
				if targetIntent == nil {
					// Engine played a card but has no target for it.
					// Production's real abandonment (match_lifecycle.go:
					// 517-526) clears PendingCard by direct, privileged
					// mutation of matchContainer.state -- state this harness,
					// deliberately confined to match.Service's public API
					// (see the package doc), has no way to reach. Submitting
					// a garbage select_target intent to fake it does NOT
					// work: applySelectTarget rejects it and PendingCard
					// stays set, so a later attempt to play a DIFFERENT card
					// this same turn fails with "resolve the pending card
					// target first" -- a harness bug this fix removes, found
					// via exactly that failure in a real gauntlet run.
					// applyMove itself never checks PendingCard, so a
					// fallback move still succeeds and ends the turn; the
					// stale PendingCard is then left dangling in match state
					// for the rest of the game, same as it would be in
					// production after ensureComputerMadeProgressLocked's
					// fallback -- a real (separate, minor) gap worth noting
					// for L5, not something to paper over here.
					stuckPendingCard = true
					break
				}
				targetIntent.PlayerID = playerID
				targetIntent.PlayerSecret = playerSecret
				targetIntent.MatchID = matchID
				if _, err := svc.ApplyIntent(*targetIntent, nextNow()); err != nil {
					return engine.OutcomeDraw, fmt.Errorf("xgauntlet: engine %s submitted an invalid select_target: %w", state.Turn, err)
				}
			}

			if stuckPendingCard {
				pendingCardIsStale = true
				// Re-fetch live state rather than trusting the snapshot
				// captured at the top of this sub-iteration: defensive
				// against any ordering this harness's own bookkeeping did
				// not anticipate (e.g. a prior guard iteration's partial
				// resolution changing something before the engine gave up).
				live, err := svc.GetMatch(matchID)
				if err != nil {
					return engine.OutcomeDraw, fmt.Errorf("xgauntlet: get match: %w", err)
				}
				if live.Match.Status != "active" {
					return outcomeFromFinishedState(&live.Match), nil
				}
				if live.Match.Turn != moverColor {
					// Something ended this player's turn already (e.g. a
					// clock timeout charged during the stuck resolution
					// attempts) -- nothing left for this player to do this
					// ply; let the outer ply loop re-evaluate from scratch.
					break
				}
				fallback, ferr := fallbackMove(&live.Match)
				if ferr != nil {
					return engine.OutcomeDraw, nil
				}
				fallback.PlayerID = playerID
				fallback.PlayerSecret = playerSecret
				fallback.MatchID = matchID
				if _, err := svc.ApplyIntent(*fallback, nextNow()); err != nil {
					return engine.OutcomeDraw, fmt.Errorf("xgauntlet: fallback move rejected after a stuck pending card: %w", err)
				}
				break
			}

			afterCard, err := svc.GetMatch(matchID)
			if err != nil {
				return engine.OutcomeDraw, fmt.Errorf("xgauntlet: get match: %w", err)
			}
			if afterCard.Match.Status != "active" {
				return outcomeFromFinishedState(&afterCard.Match), nil
			}
			if afterCard.Match.Turn != state.Turn {
				// A real move was applied and the turn advanced -- this
				// player's turn is over.
				break
			}
			// Still this player's turn (a card was played, not a move):
			// loop and ask the same engine what to do next.
		}
		if !madeProgress {
			return engine.OutcomeDraw, fmt.Errorf("xgauntlet: %s made no progress after %d sub-decisions", state.Turn, cfg.MaxSubDecisionsPerTurn)
		}
	}

	// Ply cap reached with neither side converting.
	return engine.OutcomeDraw, nil
}

func outcomeFromFinishedState(state *contracts.MatchState) engine.Outcome {
	switch state.Winner {
	case "white":
		return engine.OutcomeWhiteWin
	case "black":
		return engine.OutcomeBlackWin
	default:
		return engine.OutcomeDraw
	}
}

// playRandomOpening plays openingPlies random legal moves from the standard
// starting position through the real service, alternating white/black,
// deriving each choice from rng -- the real-match analogue of
// internal/engine/gauntlet.go's randomOpening, needed for the same reason:
// without opening diversity, every gauntlet game would follow one forced
// line and measure nothing beyond that line.
func playRandomOpening(svc *match.Service, matchID, whiteGuest, whiteSecret, blackGuest, blackSecret string, plies int, rng *rand.Rand, nextNow func() time.Time) error {
	for i := 0; i < plies; i++ {
		snap, err := svc.GetMatch(matchID)
		if err != nil {
			return err
		}
		state := &snap.Match
		if state.Status != "active" {
			return nil
		}
		p, err := conform.ToPosition(state)
		if err != nil {
			return err
		}
		ov := conform.ToOverlay(state)
		playerID, playerSecret := whiteGuest, whiteSecret
		if state.Turn == "black" {
			playerID, playerSecret = blackGuest, blackSecret
		}
		moves := core.GenerateSubmittableMoves(p, ov)
		if len(moves) == 0 {
			return nil
		}
		m := moves[rng.Intn(len(moves))]
		from, to, promotion := conform.MoveToIntentFields(m)
		intent := contracts.PlayerIntent{
			Type: "make_move", MatchID: matchID,
			PlayerID: playerID, PlayerSecret: playerSecret,
			From: &from, To: &to, Promotion: promotion,
		}
		if _, err := svc.ApplyIntent(intent, nextNow()); err != nil {
			return fmt.Errorf("opening ply %d: %w", i, err)
		}
	}
	return nil
}

// fallbackMove picks the first legal chess move for the side to move,
// converted from engine/core's own generator (never internal/match's private
// generateAllMoves, which this package deliberately never imports -- see the
// package doc). Used only when an Engine's MakeMove gives up; it is not
// itself a strength contender.
func fallbackMove(state *contracts.MatchState) (*contracts.PlayerIntent, error) {
	p, err := conform.ToPosition(state)
	if err != nil {
		return nil, err
	}
	ov := conform.ToOverlay(state)
	moves := core.GenerateSubmittableMoves(p, ov)
	if len(moves) == 0 {
		return nil, fmt.Errorf("no legal move available")
	}

	// core.GenerateSubmittableMoves has no notion of state.DoubleMove
	// (CLAUDE.md's engine rebuild note: "pending-card/double-move
	// turn-sequencing state remains unmodeled" in the new engine) --
	// unfiltered, its first move can violate any of applyMove's double-move
	// guards: the FIRST move of a double move may not itself check the
	// enemy king (match_actions.go:105-108), the second "same" move must
	// move the exact tracked piece, and the second "diff" move must move a
	// DIFFERENT piece (match_actions.go:45-52). Found by xgauntlet's E0
	// cross-engine gauntlet: this fallback itself (used whenever either
	// engine's MakeMove gives up) was rejected with "solo double move
	// requires moving the same piece again" -- the same class of bug this
	// harness exists to catch in the engines it drives, this time in the
	// harness's own code.
	if state.DoubleMove != nil && state.DoubleMove.MovesLeft == 2 {
		filtered := moves[:0]
		for _, m := range moves {
			u := core.MakeMoveWithOverlay(p, ov, m)
			checksEnemy := core.InCheckWithFusion(p, ov, p.SideToMove())
			p.UnmakeMove(u)
			if checksEnemy {
				continue
			}
			filtered = append(filtered, m)
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("no legal move available that satisfies the active double move")
		}
		moves = filtered
	} else if state.DoubleMove != nil && state.DoubleMove.MovesLeft == 1 && state.DoubleMove.TrackedSq != nil {
		tracked := contracts.Square{Row: state.DoubleMove.TrackedSq.Row, Col: state.DoubleMove.TrackedSq.Col}
		filtered := moves[:0]
		for _, m := range moves {
			from, _, _ := conform.MoveToIntentFields(m)
			isTracked := from == tracked
			if state.DoubleMove.Type == "same" && !isTracked {
				continue
			}
			if state.DoubleMove.Type == "diff" && isTracked {
				continue
			}
			filtered = append(filtered, m)
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("no legal move available that satisfies the active double move")
		}
		moves = filtered
	}

	from, to, promotion := conform.MoveToIntentFields(moves[0])
	return &contracts.PlayerIntent{Type: "make_move", From: &from, To: &to, Promotion: promotion}, nil
}

// RunConfig controls a colour-balanced match-up of pairs, mirroring
// internal/engine/gauntlet.go's GauntletConfig/RunGauntlet shape exactly so
// a cross-engine result and an old-engine self-play baseline read the same
// way.
type RunConfig struct {
	Pairs        int
	OpeningPlies int
	Game         GameConfig
	Seed         int64
	Elo0, Elo1   float64
	Alpha, Beta  float64
	OnGame       func(played int, r engine.GauntletResult)
}

// DefaultRunConfig mirrors internal/engine.DefaultGauntletConfig: +0 vs +10
// Elo at 5% error rates.
func DefaultRunConfig() RunConfig {
	return RunConfig{
		Pairs: 50, OpeningPlies: 6, Game: DefaultGameConfig(),
		Seed: 1, Elo0: 0, Elo1: 10, Alpha: 0.05, Beta: 0.05,
	}
}

// RunGauntlet plays a colour-balanced match-up between contender A and
// contender B, stopping early once SPRT reaches a verdict, exactly like
// internal/engine.RunGauntlet -- this is the E0 deliverable: for the first
// time, a is free to be internal/engine.ComputerOpponent and b free to be a
// NewEngineAdapter (or vice versa), because both are just EngineFactory
// values.
func RunGauntlet(svc *match.Service, a, b EngineFactory, cfg RunConfig) engine.GauntletResult {
	var result engine.GauntletResult
	for pair := 0; pair < cfg.Pairs; pair++ {
		seed := cfg.Seed + int64(pair)

		outcomeA, errA := PlayOneGame(svc, a, b, cfg.Game, seed, cfg.OpeningPlies)
		if errA == nil {
			switch outcomeA {
			case engine.OutcomeWhiteWin:
				result.AWins++
			case engine.OutcomeBlackWin:
				result.BWins++
			default:
				result.Draws++
			}
		}
		if cfg.OnGame != nil {
			cfg.OnGame(result.Games(), result)
		}

		outcomeB, errB := PlayOneGame(svc, b, a, cfg.Game, seed, cfg.OpeningPlies)
		if errB == nil {
			switch outcomeB {
			case engine.OutcomeWhiteWin:
				result.BWins++
			case engine.OutcomeBlackWin:
				result.AWins++
			default:
				result.Draws++
			}
		}
		if cfg.OnGame != nil {
			cfg.OnGame(result.Games(), result)
		}

		if verdict, _ := result.SPRT(cfg.Elo0, cfg.Elo1, cfg.Alpha, cfg.Beta); verdict != engine.SPRTContinue {
			return result
		}
	}
	return result
}

// OldEngineFactory adapts internal/engine.ComputerOpponent to EngineFactory.
func OldEngineFactory(difficulty engine.Difficulty) EngineFactory {
	return func(color string) Engine {
		return engine.NewComputerOpponent(difficulty, color)
	}
}

// NewEngineAdapter drives the rebuild's search (internal/engine/search) as
// an Engine, via the fair-play entry point (FairPlaySearchTimed) -- the
// production-shaped path that never reads the opponent's real hand, only its
// size. Deliberately scoped to what E0 needs to MEASURE the rebuild, not a
// production-quality wiring (that is E6): it only knows how to submit the
// select_target sequence the seven currently-modeled mechanics need, checked
// directly against internal/match/match_cards.go's applySelectTarget case
// for each -- freeze/shield/fortress/lavaground/unabomber resolve on a single
// Target-bearing select_target, blackhole/halffuse/fullfusion need exactly
// two, submitted via Target both times (state.PendingCard.Target's
// nil-ness, not a SelectionID, distinguishes first from second -- none of
// the seven modeled mechanics uses SelectionID at all, unlike promote/demote/
// swap*, which this adapter does not attempt because actions.GenerateActions
// never proposes them; see actions/candidates.go).
type NewEngineAdapter struct {
	Color     string
	TimeLimit time.Duration
	MaxDepth  int
	Samples   int
	rng       *rand.Rand

	// pendingSecond stashes the second target for a two-target mechanic
	// between the play_card call and the second HandleSelectTarget call.
	// pendingFirst is not stashed the same way: it is read on the very next
	// HandleSelectTarget call and cleared immediately after use.
	pendingFirst  *contracts.Square
	pendingSecond *contracts.Square
}

// NewNewEngineAdapter builds an adapter seated as color. samples/maxDepth <=
// 0 fall back to small-but-real defaults suitable for gauntlet play (a full
// production-grade budget is E5/E6's concern, not E0's).
func NewNewEngineAdapter(color string, timeLimit time.Duration, maxDepth, samples int, seed int64) *NewEngineAdapter {
	if maxDepth <= 0 {
		maxDepth = 4
	}
	if samples <= 0 {
		samples = 4
	}
	return &NewEngineAdapter{
		Color: color, TimeLimit: timeLimit, MaxDepth: maxDepth, Samples: samples,
		rng: rand.New(rand.NewSource(seed)),
	}
}

var _ Engine = (*NewEngineAdapter)(nil)

// NewEngineFactory adapts NewEngineAdapter to EngineFactory. Each call gets
// its own rng stream (seeded from base+a running counter) so successive
// games in a gauntlet don't replay identical PIMC sampling.
func NewEngineFactory(timeLimit time.Duration, maxDepth, samples int, baseSeed int64) EngineFactory {
	var counter int64
	return func(color string) Engine {
		counter++
		return NewNewEngineAdapter(color, timeLimit, maxDepth, samples, baseSeed+counter)
	}
}

func (a *NewEngineAdapter) coreColor() core.Color {
	if a.Color == "white" {
		return core.White
	}
	return core.Black
}

func handFor(state *contracts.MatchState, color string) []contracts.GameCard {
	if color == "white" {
		return state.WhiteHand
	}
	return state.BlackHand
}

func opposite(color string) string {
	if color == "white" {
		return "black"
	}
	return "white"
}

// toActionsHand converts the wire hand into actions.Hand, keeping every
// card's mechanic string as-is. Cards whose mechanic is one of the 30 this
// package's search does not model simply generate zero actions
// (actions/candidates.go's default case) -- they occupy a hand slot but
// contribute nothing, matching the plan's documented E3 gap rather than
// masking it here.
func toActionsHand(cards []contracts.GameCard) actions.Hand {
	hand := make(actions.Hand, 0, len(cards))
	for _, c := range cards {
		hand = append(hand, actions.CardInstance{ID: c.ID, Mechanic: actions.Mechanic(c.Mechanic)})
	}
	return hand
}

func coreSquareToContracts(sq core.Square) contracts.Square {
	return contracts.Square{Row: sq.Rank(), Col: sq.File()}
}

func (a *NewEngineAdapter) MakeMove(state *contracts.MatchState) *contracts.PlayerIntent {
	if state.Status != "active" {
		return nil
	}
	p, err := conform.ToPosition(state)
	if err != nil {
		return nil
	}
	ov := conform.ToOverlay(state)
	myHand := toActionsHand(handFor(state, a.Color))
	oppHandSize := len(handFor(state, opposite(a.Color)))

	results := search.FairPlaySearchTimed(p, ov, myHand, a.coreColor(), oppHandSize, a.Samples, a.TimeLimit, a.MaxDepth, a.rng)
	if len(results) == 0 {
		return nil
	}
	return a.actionToIntent(state, results[0].Action)
}

func (a *NewEngineAdapter) actionToIntent(state *contracts.MatchState, act actions.Action) *contracts.PlayerIntent {
	if act.Kind == actions.ActionMove {
		from, to, promotion := conform.MoveToIntentFields(act.Move)
		return &contracts.PlayerIntent{Type: "make_move", MatchID: state.MatchID, From: &from, To: &to, Promotion: promotion}
	}

	a.pendingFirst = nil
	a.pendingSecond = nil
	if act.Targets.NumTargets >= 1 {
		first := coreSquareToContracts(act.Targets.First)
		a.pendingFirst = &first
	}
	if act.Targets.NumTargets == 2 {
		second := coreSquareToContracts(act.Targets.Second)
		a.pendingSecond = &second
	}
	return &contracts.PlayerIntent{Type: "play_card", MatchID: state.MatchID, CardID: act.Card.ID}
}

func (a *NewEngineAdapter) HandleSelectTarget(state *contracts.MatchState) *contracts.PlayerIntent {
	if state.PendingCard == nil {
		return nil
	}
	if state.PendingCard.Target == nil {
		if a.pendingFirst == nil {
			return nil
		}
		target := a.pendingFirst
		a.pendingFirst = nil
		return &contracts.PlayerIntent{Type: "select_target", MatchID: state.MatchID, Target: target}
	}
	if a.pendingSecond == nil {
		return nil
	}
	target := a.pendingSecond
	a.pendingSecond = nil
	return &contracts.PlayerIntent{Type: "select_target", MatchID: state.MatchID, Target: target}
}
