package conform

import (
	"fmt"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/engine/core"
	"github.com/chess404/realtime/internal/match"
)

// Fixed test credentials. WhiteGuestID/BlackGuestID are deliberately left
// unset on every state this package builds, so requireIntentColor
// (match_lifecycle.go:678) falls into its substring-match fallback for
// "white_player"/"black_player" -- the exact pattern internal/match's own
// state_test.go (testPlayerColor/applyTestIntent) already relies on.
const (
	whiteSecret = "conform-white-secret"
	blackSecret = "conform-black-secret"
)

// NewStandardMatch creates a fresh match.Service and a brand-new match at
// the standard starting position (CreateMatch never starts anywhere else).
func NewStandardMatch(matchID string, now time.Time) (*match.Service, contracts.MatchState) {
	svc := match.NewService()
	resp := svc.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           matchID,
		WhitePlayerSecret: whiteSecret,
		BlackPlayerSecret: blackSecret,
	}, now)
	return svc, resp.Match
}

// NewSeededMatch creates a fresh match.Service+MemoryMatchStore and injects
// state directly as matchID's persisted snapshot, so the first
// ApplyIntent/GetMatch call hydrates from it via the exported
// match.MatchStore.LoadState path (state.go's hydrateFromRedisLocked) --
// the only way to reach an arbitrary, non-standard-start position through
// internal/match's public API without replaying a real card sequence.
// MemoryMatchStore.SaveState JSON-marshals immediately (store_memory.go:42),
// so state is safe to reuse across many NewSeededMatch calls (e.g. once per
// candidate probe in LegalSetConformance) with no aliasing between them --
// each call gets its own independent snapshot bytes.
func NewSeededMatch(matchID string, state contracts.MatchState) *match.Service {
	state.MatchID = matchID
	store := match.NewMemoryMatchStore()
	_ = store.SaveState(matchID, contracts.MatchSnapshotResponse{Match: state})
	return match.NewServiceWithStoreAndBroadcaster(nil, store, match.NoopBroadcaster{})
}

// SubmitMove submits m as a "make_move" intent for color.
func SubmitMove(svc *match.Service, matchID string, color core.Color, m core.Move, now time.Time) (contracts.MatchSnapshotResponse, error) {
	from, to, promotion := MoveToIntentFields(m)
	playerID, secret := "white_player", whiteSecret
	if color == core.Black {
		playerID, secret = "black_player", blackSecret
	}
	return svc.ApplyIntent(contracts.PlayerIntent{
		Type:         "make_move",
		MatchID:      matchID,
		PlayerID:     playerID,
		PlayerSecret: secret,
		From:         &from,
		To:           &to,
		Promotion:    promotion,
	}, now)
}

// RandomWalkResult summarizes one random-walk fuzz run.
type RandomWalkResult struct {
	Plies    int
	GameOver bool
	Mismatch string
}

// RandomWalk drives a fresh standard-start match for up to maxPlies plies.
// At each step it converts internal/match's current state to
// (core.Position, core.CardOverlay), asks engine/core for its submittable
// moves, picks one via pick, submits that SAME move to internal/match, and
// asserts: (1) internal/match accepts it (catches engine/core
// over-generating an illegal move), and (2) applying it independently via
// core.MakeMoveWithOverlay produces a position/overlay that exactly matches
// what internal/match's own application produced (catches a move-APPLICATION
// bug, as opposed to a move-generation one). Stops early on checkmate/
// stalemate (either engine reporting zero submittable moves ends the walk,
// since GenerateSubmittableMoves is used, not the Frozen-blind
// TerminalStatusWithOverlay) or on the first mismatch.
func RandomWalk(matchID string, maxPlies int, pick func([]core.Move) core.Move, now time.Time) RandomWalkResult {
	svc, state := NewStandardMatch(matchID, now)

	for ply := 0; ply < maxPlies; ply++ {
		pos, err := ToPosition(&state)
		if err != nil {
			return RandomWalkResult{Plies: ply, Mismatch: fmt.Sprintf("ply %d: converting match state: %v", ply, err)}
		}
		ov := ToOverlay(&state)

		legal := core.GenerateSubmittableMoves(pos, ov)
		if len(legal) == 0 {
			return RandomWalkResult{Plies: ply, GameOver: true}
		}
		m := pick(legal)
		mover := core.ColorFromString(state.Turn)

		resp, err := SubmitMove(svc, matchID, mover, m, now.Add(time.Duration(ply+1)*time.Second))
		if err != nil {
			return RandomWalkResult{Plies: ply, Mismatch: fmt.Sprintf(
				"ply %d: engine/core thought %v %v->%v was legal but internal/match rejected it: %v", ply, mover, m.From, m.To, err)}
		}

		// pos/ov are mutated in place by MakeMoveWithOverlay -- after this
		// call they represent engine/core's OWN independent application of
		// m, to compare against internal/match's resulting state below.
		core.MakeMoveWithOverlay(pos, ov, m)

		gotPos, err := ToPosition(&resp.Match)
		if err != nil {
			return RandomWalkResult{Plies: ply, Mismatch: fmt.Sprintf("ply %d: converting result state: %v", ply, err)}
		}
		gotOv := ToOverlay(&resp.Match)

		if mismatch, ok := PositionsMatch(pos, gotPos); !ok {
			return RandomWalkResult{Plies: ply, Mismatch: fmt.Sprintf("ply %d (%v %v->%v): position mismatch: %s", ply, mover, m.From, m.To, mismatch)}
		}
		if mismatch, ok := OverlaysMatch(ov, gotOv); !ok {
			return RandomWalkResult{Plies: ply, Mismatch: fmt.Sprintf("ply %d (%v %v->%v): overlay mismatch: %s", ply, mover, m.From, m.To, mismatch)}
		}

		state = resp.Match
	}
	return RandomWalkResult{Plies: maxPlies}
}

// moveKey is a Move stripped of its Flag -- Flag is an engine/core-internal
// disambiguator (quiet vs en passant vs castle vs double push) never
// exposed on the wire; comparing (From, To, Promotion) triples is the right
// granularity for matching engine/core's legal set against what a
// contracts.PlayerIntent (which only ever carries From/To/Promotion) can
// express.
type moveKey struct {
	From, To  core.Square
	Promotion core.PieceType
}

func legalMoveKeys(moves []core.Move) map[moveKey]bool {
	keys := make(map[moveKey]bool, len(moves))
	for _, m := range moves {
		keys[moveKey{m.From, m.To, m.Promotion}] = true
	}
	return keys
}

// allCandidateKeys enumerates every (from,to[,promotion]) combination for
// pieces belonging to color -- deliberately NOT limited to engine/core's own
// legal-move output, so probing internal/match's acceptance across this
// full space can catch engine/core UNDER-generating (missing a move
// internal/match would allow), not just over-generating.
func allCandidateKeys(pos *core.Position, color core.Color) []moveKey {
	var candidates []moveKey
	occ := pos.Occupied(color)
	for from := core.Square(0); from < 64; from++ {
		if !occ.Has(from) {
			continue
		}
		piece := pos.PieceAt(from)
		for to := core.Square(0); to < 64; to++ {
			if to == from {
				continue
			}
			if piece.Type == core.Pawn && (to.Rank() == 0 || to.Rank() == 7) {
				for _, promo := range [4]core.PieceType{core.Knight, core.Bishop, core.Rook, core.Queen} {
					candidates = append(candidates, moveKey{from, to, promo})
				}
				continue
			}
			candidates = append(candidates, moveKey{from, to, core.NoPieceType})
		}
	}
	return candidates
}

// LegalSetConformance checks that engine/core's notion of "this move will
// actually be accepted" -- core.GenerateSubmittableMoves, which is
// Frozen-AWARE (unlike GenerateLegalMovesWithOverlay) -- EXACTLY matches
// internal/match's ACCEPT/REJECT behavior over every plausible candidate
// for color, when internal/match is seeded at baseState (which must already
// describe the same position as pos/ov -- callers build it via
// NewStandardMatch + mutation, then ToPosition/ToOverlay it to get pos/ov,
// keeping a single source of truth for the scenario).
//
// GenerateSubmittableMoves, not GenerateLegalMovesWithOverlay, is the
// correct comparison target here: internal/match's applyMove rejects a
// frozen piece's move via a hard guard BEFORE ever consulting
// legalMovesWithFusion (match_actions.go:42-44) -- so "will ApplyIntent
// accept this" corresponds to the Frozen-aware submittable set, not the
// Frozen-blind one GenerateLegalMovesWithOverlay/TerminalStatusWithOverlay
// deliberately preserve for stalemate/checkmate classification instead (see
// overlays_movegen.go).
//
// A fresh Service+MatchID is used per candidate (NewSeededMatch reseeds
// baseState every time) specifically to sidestep ApplyIntent's per-seat
// rate limiter (5-burst/10-per-second, match_throttle.go) -- a shared match
// probed many times in a row would have later candidates misclassified as
// "rejected" once the burst is exhausted, which is indistinguishable from a
// genuine illegal-move rejection without relying on unexported error
// internals. A fresh matchContainer's token bucket always starts full, so
// this sidesteps the limiter entirely rather than racing it with timestamp
// bookkeeping.
func LegalSetConformance(baseState contracts.MatchState, pos *core.Position, ov *core.CardOverlay, color core.Color, now time.Time) []string {
	engineLegal := legalMoveKeys(core.GenerateSubmittableMoves(pos, ov))
	candidates := allCandidateKeys(pos, color)

	var mismatches []string
	for i, cand := range candidates {
		matchID := fmt.Sprintf("conform_probe_%d", i)
		svc := NewSeededMatch(matchID, baseState)
		m := core.Move{From: cand.From, To: cand.To, Promotion: cand.Promotion}
		_, err := SubmitMove(svc, matchID, color, m, now)

		matchAccepted := err == nil
		engineAccepted := engineLegal[cand]
		if matchAccepted != engineAccepted {
			mismatches = append(mismatches, fmt.Sprintf(
				"%v %v->%v (promo=%v): engine/core legal=%v, internal/match accepted=%v (err=%v)",
				color, cand.From, cand.To, cand.Promotion, engineAccepted, matchAccepted, err))
		}
	}
	return mismatches
}
