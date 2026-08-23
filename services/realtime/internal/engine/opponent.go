package engine

import (
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

const (
	timeBeginner = 100 * time.Millisecond
	timeEasy     = 250 * time.Millisecond
	timeMedium   = 500 * time.Millisecond
	timeHard     = 1000 * time.Millisecond
	timeExpert   = 2000 * time.Millisecond
)

type Difficulty int

const (
	DifficultyBeginner Difficulty = iota
	DifficultyEasy
	DifficultyMedium
	DifficultyHard
	DifficultyExpert
)

func ParseDifficulty(s string) Difficulty {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "beginner":
		return DifficultyBeginner
	case "easy":
		return DifficultyEasy
	case "hard":
		return DifficultyHard
	case "expert":
		return DifficultyExpert
	default:
		return DifficultyMedium
	}
}

func (d Difficulty) SearchDepth() int {
	return 32
}

func (d Difficulty) TimeLimit() time.Duration {
	switch d {
	case DifficultyBeginner:
		return timeBeginner
	case DifficultyEasy:
		return timeEasy
	case DifficultyMedium:
		return timeMedium
	case DifficultyHard:
		return timeHard
	case DifficultyExpert:
		return timeExpert
	default:
		return timeMedium
	}
}

func (d Difficulty) ShouldPlayCard(card contracts.GameCard, score int) bool {
	switch d {
	case DifficultyBeginner:
		return score >= 60 && rand.Float64() < 0.3
	case DifficultyEasy:
		return score >= 50 && rand.Float64() < 0.5
	case DifficultyMedium:
		return score >= 40
	case DifficultyHard:
		return score >= 30
	case DifficultyExpert:
		return score >= 20
	default:
		return score >= 40
	}
}

type ComputerOpponent struct {
	Difficulty Difficulty
	Color      string
	rng        *rand.Rand
	tt         *TranspositionTable
	cardEval   *CardEvaluator
	mu         sync.Mutex
}

func NewComputerOpponent(difficulty Difficulty, color string) *ComputerOpponent {
	seed := time.Now().UnixNano()
	return &ComputerOpponent{
		Difficulty: difficulty,
		Color:      color,
		rng:        rand.New(rand.NewSource(seed)),
		tt:         NewTranspositionTable(1 << 16),
		cardEval:   NewCardEvaluator(rand.New(rand.NewSource(seed + 1))),
	}
}

// computerPlayableMechanics is every mechanic HandleSelectTarget/findBestTarget
// can actually carry through to a successful select_target (or that needs no
// target at all). It is deliberately a strict subset of the 37 mechanics --
// see the comment on filterHandForComputer for why this exists.
var computerPlayableMechanics = map[string]bool{
	// Zero-target: applyPlayCard resolves these immediately, no PendingCard,
	// so there is nothing to fail.
	"doublemove_same": true, "doublemove_diff": true, "undo": true,
	"reverse": true, "mirror": true, "gambler": true, "radar": true, "cheater": true,
	// One-target, SINGLE-phase (applySelectTarget resolves fully on the one
	// select_target call findBestTarget's Target feeds it -- no
	// pending.Target staging, no SelectionID). Verified by reading each
	// mechanic's applySelectTarget case directly, not inferred from
	// findBestTarget having a switch case for it (see the six-mechanic
	// exclusion below, all of which also have a findBestTarget case but are
	// NOT actually single-phase).
	"freeze": true, "sniper": true, "badsniper": true,
	"mindcontrol": true, "borrow": true, "invisible": true,
	// The following SIX mechanics are deliberately NOT here even though
	// findBestTarget (below) has a switch case for each of them: they are
	// actually TWO-phase in applySelectTarget (match_cards.go), staging
	// pending.Target (and, for the promote/demote family, pending.Options)
	// on the FIRST select_target and requiring a second call to finish --
	// but findBestTarget has no notion of "which phase am I in" at all. Every
	// call re-derives a target from scratch with the same single-shot
	// heuristic, so a second call either resubmits data the reference
	// already has (rejected) or names an option/selection it never provides
	// (SelectionID stays empty). Concretely:
	//   - "parasite": phase 1 needs the mover's OWN non-king host
	//     (match_cards.go:684-703); findBestTarget's shared case for it
	//     (grouped with freeze/sniper/etc.) always picks an ENEMY piece, so
	//     the very first call is rejected: "parasite requires your own
	//     non-king host".
	//   - "clone": phase 1 (own piece) actually succeeds, since
	//     findBestTarget's clone case does pick the mover's own piece -- but
	//     phase 2 needs an adjacent EMPTY destination
	//     (match_cards.go:751-767), and a second findBestTarget("clone", ...)
	//     call just returns the SAME occupied source square again: "clone
	//     destination must be empty".
	//   - "demote", "demotehim", "promote", "promotehim": share ONE
	//     applySelectTarget case (match_cards.go:323-355) that stages
	//     pending.Target + pending.Options (the safe transform choices) on
	//     the first call and then requires intent.SelectionID on the second
	//     (match_cards.go:356-362) -- HandleSelectTarget never supplies one.
	// Every one of these six was proven broken by xgauntlet's E0
	// cross-engine gauntlet -- real ComputerOpponent-vs-ComputerOpponent
	// games through the actual match.Service failed on exactly these
	// errors -- not by static inspection. Before this fix, all six were
	// wrongly asserted "working" in filterHandForComputer's own regression
	// test (TestFilterHandForComputerExcludesEveryBrokenMechanic). Because
	// an abandoned pending card is never removed from hand
	// (match_lifecycle.go's abandon-on-failure path), any difficulty that
	// drew and chose one of these six would silently fail to play it,
	// forever eligible to be re-chosen on a later turn -- the same failure
	// shape as the original 17-mechanic "push push push" bug, just each
	// individually less visible since none of these six is a
	// high-frequency top-scoring card pick.
}

// filterHandForComputer returns a copy of hand containing only mechanics the
// computer can actually complete.
//
// This is the fix for a live bug where the computer, once it happened to draw
// and select ANY of the other 17 mechanics, got permanently stuck: a card
// only leaves the hand when select_target successfully resolves it, but for
// these 17, HandleSelectTarget/findBestTarget either has no matching case at
// all (lavaground, fakepiece -- dispatched but unimplemented), never dispatches
// in the first place (shield, unabomber, fortress, fog_village, teleport, jump,
// blackhole, joker, smallsacrifice, bigsacrifice), or submits a SelectionID
// (swapme/swapus/swaphim/halffuse/fullfusion's PendingCard.Options[0]) that
// applySelectTarget never accepts. match_lifecycle.go's
// autoPlayComputerDepthLimited abandons the pending card on failure but never
// removes it from hand, so on the VERY NEXT turn the same card scores highest
// again, gets picked again, fails again -- forever. Because
// Difficulty.ShouldPlayCard is a deterministic score threshold with no
// randomness for Medium/Hard/Expert (Beginner/Easy additionally roll dice
// against it, which is why they mostly didn't show this), once one of these
// difficulties drew one of the 17, it got stuck making zero real chess moves
// for the rest of the game -- burning all 5 retry levels on the same dead
// card every turn and falling through every time to
// ensureComputerMadeProgressLocked's firstLegalMoveForColor fallback, a raw
// board scan starting from the a-file. That fallback IS the reported "pushes
// the a-file pawn, then b-file, then c-file..." behavior; it was never a
// pawn-specific bug, it was the total absence of any other move being tried.
func filterHandForComputer(hand []contracts.GameCard) []contracts.GameCard {
	filtered := make([]contracts.GameCard, 0, len(hand))
	for _, card := range hand {
		if computerPlayableMechanics[card.Mechanic] {
			filtered = append(filtered, card)
		}
	}
	return filtered
}

// filterCurrentlyIllegalReverse drops "reverse" from hand whenever playing it
// right now would be rejected. Unlike every mechanic filterHandForComputer
// excludes, "reverse" is not ALWAYS broken -- match_cards.go's "reverse" case
// (applyPlayCard:87-118) is fully dispatched and CardEvaluator.reverseValue
// (cards.go:387-396) scores it correctly when it's legal. What's missing is
// that reverseValue never checks the two preconditions applyPlayCard itself
// enforces: fewer than two history snapshots ("no move to reverse yet"), or
// either king ending up in check after restoring the older snapshot
// ("cannot reverse because your/enemy king would be in check"). Without this
// filter, a high-scoring "reverse" could be selected and then rejected
// outright, exactly like every other card in this file's failure class.
// Found by xgauntlet's E0 cross-engine gauntlet: a real game had "reverse"
// picked and rejected with "cannot reverse because enemy king would be in
// check".
func filterCurrentlyIllegalReverse(hand []contracts.GameCard, state *contracts.MatchState, color string) []contracts.GameCard {
	hasReverse := false
	for _, card := range hand {
		if card.Mechanic == "reverse" {
			hasReverse = true
			break
		}
	}
	if !hasReverse || reverseIsCurrentlyLegal(state, color) {
		return hand
	}
	filtered := make([]contracts.GameCard, 0, len(hand))
	for _, card := range hand {
		if card.Mechanic != "reverse" {
			filtered = append(filtered, card)
		}
	}
	return filtered
}

// reverseIsCurrentlyLegal mirrors applyPlayCard's "reverse" preconditions
// exactly (match_cards.go:87-97): at least two history snapshots exist, and
// restoring the second-to-last one leaves neither king in check.
func reverseIsCurrentlyLegal(state *contracts.MatchState, color string) bool {
	if len(state.History) < 2 {
		return false
	}
	restored := state.History[len(state.History)-2].Board
	if king := findKing(restored, color); king != nil && isAttackedWithFusion(restored, *king, oppositeColor(color)) {
		return false
	}
	if oppKing := findKing(restored, oppositeColor(color)); oppKing != nil && isAttackedWithFusion(restored, *oppKing, color) {
		return false
	}
	return true
}

func (co *ComputerOpponent) MakeMove(state *contracts.MatchState) *contracts.PlayerIntent {
	co.mu.Lock()
	defer co.mu.Unlock()

	if state.Status != "active" {
		return nil
	}

	// Card scoring reads WhiteHand/BlackHand straight off state; evaluate
	// against a filtered copy so a card the computer cannot complete is never
	// even a candidate, instead of catching the failure after the fact.
	cardState := *state
	cardState.WhiteHand = filterCurrentlyIllegalReverse(filterHandForComputer(state.WhiteHand), state, "white")
	cardState.BlackHand = filterCurrentlyIllegalReverse(filterHandForComputer(state.BlackHand), state, "black")

	// A card can never be played while a double move is in progress
	// (applyPlayCard's guard, match_cards.go:26-28: "resolve the active
	// double move before playing another card") -- but nothing here checked
	// state.DoubleMove before now, so ShouldPlayCard could still decide to
	// play a newly-favourable card mid-double-move, submit it, and have it
	// rejected outright by the real rules. Found by xgauntlet's E0
	// cross-engine gauntlet: a real game failed with exactly that rejection.
	// The consequence is silent, not a crash: autoPlayComputerDepthLimited
	// (match_lifecycle.go) restores state on a rejected intent and returns,
	// so the computer's second move of its own double move simply never
	// happens that cycle -- the same "computer contributes nothing this
	// turn" shape the original push-push-push bug had, just gated on a
	// rarer precondition (a double move in progress at the moment a new
	// card also scores high enough to want to play).
	if state.DoubleMove == nil && co.cardEval.ShouldPlayCard(&cardState, co.Color == "white") {
		play := co.cardEval.BestCardToPlay(&cardState, co.Color == "white")
		if play != nil && co.Difficulty.ShouldPlayCard(play.Card, play.Score) {
			return &contracts.PlayerIntent{
				Type:     "play_card",
				MatchID:  state.MatchID,
				CardID:   play.Card.ID,
			}
		}
	}

	// Probe opening book first (only in opening phase). Skipped entirely
	// while a double move is active: the book has no notion of "this move
	// must avoid checking the enemy king" (first half) or "this move must
	// move the exact piece the first half landed with" (second half,
	// "same" type) -- same underlying gap as the two fallbacks below, just
	// reachable earlier because this branch returns before ever calling
	// SearchWithTime. Found by xgauntlet's E0 cross-engine gauntlet: a real
	// game had a book move rejected with "solo double move requires moving
	// the same piece again" despite the fallback logic below existing,
	// because the book probe short-circuited past it.
	if state.DoubleMove == nil && state.FullMoveNum <= 10 {
		bookMove := defaultBook.Probe(state, co.Color == "white")
		if bookMove != nil && bookMove.From != bookMove.To {
			return &contracts.PlayerIntent{
				Type:    "make_move",
				MatchID: state.MatchID,
				From:    &bookMove.From,
				To:      &bookMove.To,
			}
		}
	}

	searchDepth := co.Difficulty.SearchDepth()
	timeLimit := co.Difficulty.TimeLimit()

	// Dynamic time adjustment based on position complexity.
	stateTurn := state.Turn == "white"
	moves := generateAllMoves(state, stateTurn)
	numMoves := len(moves)
	if numMoves == 0 {
		return nil
	}
	if numMoves == 1 {
		// Only one legal move: play instantly (but still search a little).
		timeLimit = timeLimit / 5
		if timeLimit < 10*time.Millisecond {
			timeLimit = 10 * time.Millisecond
		}
	} else {
		// Count pieces on the board: more pieces = more complex.
		pieceCount := 0
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				if state.Board[r][c] != nil {
					pieceCount++
				}
			}
		}
		// Scale time: more pieces = more time (max 2x), fewer = less (min 0.5x).
		factor := 0.5 + float64(pieceCount)/64.0
		timeLimit = time.Duration(float64(timeLimit) * factor)
		// In complex positions (many legal moves), spend more time.
		if numMoves > 30 {
			timeLimit += timeLimit / 2
		}
	}

	result := SearchWithTime(state, searchDepth, co.tt, timeLimit)

	if result.BestMove.From == (contracts.Square{}) && result.BestMove.To == (contracts.Square{}) {
		return nil
	}

	// The FIRST of a double move's two moves may not itself put the enemy
	// king in check (applyMove's guard, match_actions.go:105-108: "first
	// double move cannot put enemy king in check") -- but SearchWithTime has
	// no notion of this precondition, so an ordinary eval-driven search is
	// free to prefer a checking move here exactly because check is usually
	// strong. Found by xgauntlet's E0 cross-engine gauntlet: a real game
	// failed with exactly this rejection. Rather than teach the general
	// search about a card-specific, turn-position-specific rule, fall back
	// to the first legal candidate (from the same `moves` already generated
	// above) that doesn't trip the constraint -- correctness over
	// optimality here, matching the ply-cap and no-result fallbacks
	// elsewhere in this file and in gauntlet.go.
	if state.DoubleMove != nil && state.DoubleMove.MovesLeft == 2 && IsKingInCheck(applyMoveCopy(state, &result.BestMove)) {
		replaced := false
		for i := range moves {
			if IsKingInCheck(applyMoveCopy(state, &moves[i])) {
				continue
			}
			result.BestMove = moves[i]
			replaced = true
			break
		}
		if !replaced {
			// Every legal move checks the enemy king -- vanishingly rare,
			// but possible. Nothing safe to play; let the caller's own
			// fallback (ensureComputerMadeProgressLocked in production,
			// xgauntlet's fallbackMove in the gauntlet) take over.
			return nil
		}
	}

	// The SECOND move of a "doublemove_same" turn must move the exact piece
	// the first move landed with (applyMove's guard, match_actions.go:45-48:
	// "solo double move requires moving the same piece again", keyed off
	// state.DoubleMove.TrackedSq) -- but, same root cause as the
	// discovered-check case just above, SearchWithTime has no notion of this
	// precondition and is free to prefer moving a different, more valuable
	// piece instead. Found by xgauntlet's E0 cross-engine gauntlet: a real
	// game failed with exactly this rejection. Same fallback strategy:
	// prefer the first legal candidate that DOES move the tracked piece.
	if state.DoubleMove != nil && state.DoubleMove.MovesLeft == 1 && state.DoubleMove.Type == "same" && state.DoubleMove.TrackedSq != nil && result.BestMove.From != *state.DoubleMove.TrackedSq {
		replaced := false
		for i := range moves {
			if moves[i].From != *state.DoubleMove.TrackedSq {
				continue
			}
			result.BestMove = moves[i]
			replaced = true
			break
		}
		if !replaced {
			// The tracked piece has no legal move at all (pinned, or
			// captured/displaced by some other effect since the first half)
			// -- nothing safe to play; let the caller's own fallback take
			// over, same as the discovered-check case.
			return nil
		}
	}

	intent := &contracts.PlayerIntent{
		Type:     "make_move",
		MatchID:  state.MatchID,
		From:     &result.BestMove.From,
		To:       &result.BestMove.To,
	}

	return intent
}

func (co *ComputerOpponent) HandleSelectTarget(state *contracts.MatchState) *contracts.PlayerIntent {
	if state.PendingCard == nil {
		return nil
	}

	mechanic := state.PendingCard.Mechanic
	ownerColor := state.PendingCard.OwnerColor

	if mechanic == "freeze" || mechanic == "sniper" || mechanic == "badsniper" ||
		mechanic == "demote" || mechanic == "demotehim" || mechanic == "promote" ||
		mechanic == "promotehim" || mechanic == "mindcontrol" || mechanic == "borrow" ||
		mechanic == "parasite" || mechanic == "lavaground" || mechanic == "clone" ||
		mechanic == "invisible" || mechanic == "fakepiece" {

		target := co.findBestTarget(state, mechanic, ownerColor)
		if target != nil {
			return &contracts.PlayerIntent{
				Type:        "select_target",
				MatchID:     state.MatchID,
				SelectionID: targetSelectionID(mechanic),
				Target:      target,
			}
		}
	}

	if mechanic == "swapme" || mechanic == "swapus" || mechanic == "swaphim" || mechanic == "halffuse" || mechanic == "fullfusion" {
		if state.PendingCard.Options != nil && len(state.PendingCard.Options) > 0 {
			return &contracts.PlayerIntent{
				Type:        "select_target",
				MatchID:     state.MatchID,
				SelectionID: state.PendingCard.Options[0],
			}
		}
	}

	return nil
}

// colorFlipLeavesAKingInCheck mirrors mindcontrol's and borrow's shared
// board-safety check exactly (match_cards.go:653,679-680 and :675,679-680,
// via kingsRemainSafe): simulate flipping the piece at (r,c) to ownerColor,
// then require BOTH kings remain unattacked. Borrowed/BorrowCount changes
// don't affect attacks, so the same simulation covers both mechanics.
func colorFlipLeavesAKingInCheck(state *contracts.MatchState, ownerColor string, r, c int) bool {
	nextBoard := cloneBoard(state.Board)
	nextBoard[r][c].Color = ownerColor
	if king := findKing(nextBoard, "white"); king != nil && isAttackedWithFusion(nextBoard, *king, "black") {
		return true
	}
	if king := findKing(nextBoard, "black"); king != nil && isAttackedWithFusion(nextBoard, *king, "white") {
		return true
	}
	return false
}

// removalLeavesAKingInCheck mirrors ensureRemovalDoesNotCreateCheck exactly
// (match_cards.go:1670-1685, used by sniper and badsniper): simulate
// removing the piece at target, then require BOTH kings remain unattacked
// (using isAttacked, not isAttackedWithFusion -- matching the reference,
// which does not account for fusion here).
func removalLeavesAKingInCheck(board [][]*contracts.Piece, target contracts.Square, ownerColor string) bool {
	nextBoard := cloneBoard(board)
	nextBoard[target.Row][target.Col] = nil
	if king := findKing(nextBoard, ownerColor); king != nil && isAttacked(nextBoard, *king, oppositeColor(ownerColor)) {
		return true
	}
	enemyColor := oppositeColor(ownerColor)
	if king := findKing(nextBoard, enemyColor); king != nil && isAttacked(nextBoard, *king, ownerColor) {
		return true
	}
	return false
}

func (co *ComputerOpponent) findBestTarget(state *contracts.MatchState, mechanic, ownerColor string) *contracts.Square {
	opponent := oppositeColor(ownerColor)

	switch mechanic {
	// freeze: enemy non-king, no other precondition (match_cards.go's
	// "freeze" case: color/type check only). The only mechanic in this
	// family with nothing else to verify.
	case "freeze":
		bestValue := 0
		var bestSquare *contracts.Square
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := state.Board[r][c]
				if piece == nil || piece.Color != opponent || piece.Type == "king" {
					continue
				}
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
					sq := contracts.Square{Row: r, Col: c}
					bestSquare = &sq
				}
			}
		}
		return bestSquare

	// sniper: enemy non-king, and removing it must not expose either king
	// (match_cards.go:278: ensureRemovalDoesNotCreateCheck) -- checked
	// regardless of whether the target turns out to be shielded (shield
	// blocks the capture as an OUTCOME, not a validation failure, so it
	// does not change which targets are legal to select).
	case "sniper":
		bestValue := 0
		var bestSquare *contracts.Square
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := state.Board[r][c]
				if piece == nil || piece.Color != opponent || piece.Type == "king" {
					continue
				}
				sq := contracts.Square{Row: r, Col: c}
				if removalLeavesAKingInCheck(state.Board, sq, ownerColor) {
					continue
				}
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
					bestSquare = &sq
				}
			}
		}
		return bestSquare

	// badsniper targets the MOVER'S OWN non-king piece -- the opposite of
	// every other mechanic in this family (match_cards.go:303-305:
	// "badsniper requires your own non-king target"). Before this fix,
	// badsniper shared freeze/sniper's enemy-piece filter and so ALWAYS
	// picked an enemy piece, which applySelectTarget ALWAYS rejects --
	// unconditionally broken, not just on an edge case, despite being
	// allowlisted as computer-playable. Found by xgauntlet's E0
	// cross-engine gauntlet. Same removal-safety check as sniper.
	case "badsniper":
		bestValue := 0
		var bestSquare *contracts.Square
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := state.Board[r][c]
				if piece == nil || piece.Color != ownerColor || piece.Type == "king" {
					continue
				}
				sq := contracts.Square{Row: r, Col: c}
				if removalLeavesAKingInCheck(state.Board, sq, ownerColor) {
					continue
				}
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
					bestSquare = &sq
				}
			}
		}
		return bestSquare

	// mindcontrol: enemy non-king, not frozen, not shielded, not inside an
	// enemy fortress, and flipping its color must not expose either king
	// (match_cards.go:658-682). Every one of these beyond the base
	// color/type filter was missing before this fix and found only by
	// xgauntlet's E0 cross-engine gauntlet hitting each rejection in a real
	// game -- shielded specifically was never even exercised by the
	// gauntlet runs so far (frozen and unsafe were caught first) but is
	// fixed alongside them since it is the identical class of gap.
	case "mindcontrol":
		bestValue := 0
		var bestSquare *contracts.Square
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := state.Board[r][c]
				if piece == nil || piece.Color != opponent || piece.Type == "king" {
					continue
				}
				if piece.Frozen || piece.Shielded {
					continue
				}
				sq := contracts.Square{Row: r, Col: c}
				if fortressEntryBlocked(state.FortressZones, ownerColor, sq) {
					continue
				}
				if colorFlipLeavesAKingInCheck(state, ownerColor, r, c) {
					continue
				}
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
					bestSquare = &sq
				}
			}
		}
		return bestSquare

	// borrow: enemy non-king, not frozen, not borrowed 3+ times already,
	// not inside an enemy fortress, and flipping its color must not expose
	// either king (match_cards.go:631-656). Same fix rationale as
	// mindcontrol -- BorrowCount and fortress were the two additional gaps
	// specific to borrow.
	case "borrow":
		bestValue := 0
		var bestSquare *contracts.Square
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := state.Board[r][c]
				if piece == nil || piece.Color != opponent || piece.Type == "king" {
					continue
				}
				if piece.Frozen || piece.BorrowCount >= 3 {
					continue
				}
				sq := contracts.Square{Row: r, Col: c}
				if fortressEntryBlocked(state.FortressZones, ownerColor, sq) {
					continue
				}
				if colorFlipLeavesAKingInCheck(state, ownerColor, r, c) {
					continue
				}
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
					bestSquare = &sq
				}
			}
		}
		return bestSquare

	case "demotehim", "parasite":
		// No longer reachable: both excluded from computerPlayableMechanics
		// (demotehim shares the two-phase promote/demote family's gap;
		// parasite needs an own-piece host before an enemy target -- see
		// the allowlist's own comment). Left dispatched, matching the
		// existing pattern for every other excluded-but-still-switched-on
		// mechanic below, rather than deleting reachable-looking code that
		// filterHandForComputer already guarantees never runs.
		return nil

	case "demote", "promotehim":
		bestValue := 0
		var bestSquare *contracts.Square
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := state.Board[r][c]
				if piece == nil || piece.Type == "king" || piece.Type == "pawn" {
					continue
				}
				value := pieceValue(piece.Type)
				if piece.Color == opponent && value > bestValue {
					bestValue = value
					sq := contracts.Square{Row: r, Col: c}
					bestSquare = &sq
				}
			}
		}
		return bestSquare

	case "promote":
		bestValue := 0
		var bestSquare *contracts.Square
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := state.Board[r][c]
				if piece == nil || piece.Color != ownerColor || piece.Type == "king" || piece.Type == "queen" {
					continue
				}
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
					sq := contracts.Square{Row: r, Col: c}
					bestSquare = &sq
				}
			}
		}
		return bestSquare

	case "clone":
		bestValue := 0
		var bestSquare *contracts.Square
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := state.Board[r][c]
				if piece == nil || piece.Color != ownerColor || piece.Type == "king" {
					continue
				}
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
					sq := contracts.Square{Row: r, Col: c}
					bestSquare = &sq
				}
			}
		}
		return bestSquare

	case "invisible":
		bestValue := 0
		var bestSquare *contracts.Square
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				piece := state.Board[r][c]
				if piece == nil || piece.Color != ownerColor || piece.Type == "king" || piece.Invisible {
					continue
				}
				value := pieceValue(piece.Type)
				if value > bestValue {
					bestValue = value
					sq := contracts.Square{Row: r, Col: c}
					bestSquare = &sq
				}
			}
		}
		return bestSquare
	}

	return nil
}

func targetSelectionID(mechanic string) string {
	switch mechanic {
	case "freeze":
		return "freeze_target"
	case "sniper", "badsniper":
		return "sniper_target"
	case "demote", "demotehim":
		return "demote_target"
	case "promote", "promotehim":
		return "promote_target"
	case "mindcontrol", "borrow":
		return "mindcontrol_target"
	case "clone":
		return "clone_source"
	case "invisible":
		return "invisible_source"
	case "lavaground":
		return "lavaground_target"
	case "parasite":
		return "parasite_target"
	case "fakepiece":
		return "fakepiece_target"
	default:
		return "target"
	}
}
