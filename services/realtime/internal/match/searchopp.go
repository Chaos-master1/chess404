package match

// searchopp.go wires the rebuilt engine stack (internal/engine/core bitboard
// move generation + internal/engine/search PIMC hidden-hand search) into the
// match service as the computer opponent's chess-move brain.
//
// Architecture: a composite opponent. Card decisions and card-target
// selection stay with the hardened v1 ComputerOpponent (its card legality
// filters and findBestTarget logic encode dozens of production bugs found
// by the gauntlets). Chess moves are picked by the NEW engine:
// FairPlaySearchTimed runs Perfect-Information Monte-Carlo over plausible
// opponent hands -- the opponent's real hand is never read, only its size
// (which is public, and after the face-down-hand fix is exactly what the
// client shows too). The chosen action is re-validated against THIS
// package's own rules (legalMovesWithFusion king-safety semantics) before
// it becomes an intent; anything the rules reject (or any panic inside the
// new stack) falls back to the v1 engine's decision for that turn, and the
// service's ensureComputerMadeProgressLocked remains the final guarantee
// the match can never deadlock.
//
// This lives inside package match (not a subpackage) because the legality
// anchor (legalMovesWithFusion / king-safety filtered moves) is unexported,
// and engine/conform already imports this package, so it cannot be imported
// back.

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/engine/actions"
	"github.com/chess404/realtime/internal/engine/core"
	"github.com/chess404/realtime/internal/engine/search"
	"github.com/chess404/realtime/internal/engine/v1"
)

// computerOpponent is the match service's view of a computer opponent: the
// v1.ComputerOpponent surface (MakeMove / HandleSelectTarget) exactly, so a
// bare v1 instance satisfies it unchanged.
type computerOpponent interface {
	MakeMove(state *contracts.MatchState) *contracts.PlayerIntent
	HandleSelectTarget(state *contracts.MatchState) *contracts.PlayerIntent
}

// compile-time proof that v1's opponent satisfies the interface.
var _ computerOpponent = (*v1.ComputerOpponent)(nil)

// searchOpponent is the composite: v1 for cards and card targets, the
// rebuilt search stack for chess moves.
type searchOpponent struct {
	inner *v1.ComputerOpponent
	rng   *rand.Rand
}

// newSearchOpponent builds a search-backed opponent at the given difficulty and color.
func newSearchOpponent(difficulty v1.Difficulty, color string) *searchOpponent {
	if color == "" {
		color = "black"
	}
	seed := time.Now().UnixNano()
	return &searchOpponent{
		inner: v1.NewComputerOpponent(difficulty, color),
		rng:   rand.New(rand.NewSource(seed)),
	}
}

// MakeMove returns the computer's next intent. Card plays are delegated to
// v1 unchanged; chess moves go through the new search.
func (o *searchOpponent) MakeMove(state *contracts.MatchState) *contracts.PlayerIntent {
	// Ask the v1 card brain whether a card is worth playing this turn (it
	// already models hand filtering, double-move suppression and difficulty
	// gates). MakeCardDecision is exactly MakeMove's card block, extracted
	// so this costs microseconds instead of a full v1 chess search.
	cardIntent := o.inner.MakeCardDecision(state)
	if cardIntent != nil {
		return cardIntent
	}

	moveIntent, err := o.searchMoveIntent(state)
	if moveIntent != nil && err == nil {
		return moveIntent
	}
	// The new stack could not produce a legal move (conversion failure,
	// recovered panic, no legal action). v1's full answer (its own chess
	// search) is the proven fallback for this turn;
	// ensureComputerMadeProgressLocked remains the last-resort guarantee.
	return o.inner.MakeMove(state)
}

// HandleSelectTarget delegates card-target selection to v1 unchanged.
func (o *searchOpponent) HandleSelectTarget(state *contracts.MatchState) *contracts.PlayerIntent {
	return o.inner.HandleSelectTarget(state)
}

// searchMoveIntent converts the match state into the engine's representation,
// runs the hidden-hand search, converts the chosen action back, and validates
// it against this package's own rules before returning it.
func (o *searchOpponent) searchMoveIntent(state *contracts.MatchState) (intent *contracts.PlayerIntent, err error) {
	defer func() {
		if r := recover(); r != nil {
			// The new engine must never take down the match service. Any
			// panic becomes an error, which MakeMove routes to v1.
			intent = nil
			err = fmt.Errorf("search engine panic: %v", r)
		}
	}()

	pos, ov, err := toEnginePosition(state)
	if err != nil {
		return nil, err
	}
	mover := core.Black
	if o.inner.Color == "white" || state.Turn == "white" {
		mover = core.White
	}

	myHand := toEngineHand(state.BlackHand)
	opponentHandSize := len(state.WhiteHand)
	if mover == core.White {
		myHand = toEngineHand(state.WhiteHand)
		opponentHandSize = len(state.BlackHand)
	}

	timeLimit, samples := searchBudgetFor(o.inner)
	results := search.FairPlaySearchTimed(pos, ov, myHand, mover, opponentHandSize, samples, timeLimit, 32, o.rng)

	movedSet := make(map[string]struct{}, len(state.Moved))
	for _, key := range state.Moved {
		movedSet[key] = struct{}{}
	}

	for _, result := range results {
		if result.Action.Kind != actions.ActionMove {
			continue // move search only; the card brain is v1
		}
		from := contracts.Square{Row: result.Action.Move.From.Rank(), Col: result.Action.Move.From.File()}
		to := contracts.Square{Row: result.Action.Move.To.Rank(), Col: result.Action.Move.To.File()}
		candidate := contracts.PlayerIntent{Type: "make_move", MatchID: state.MatchID, From: &from, To: &to}
		if result.Action.Move.IsPromotion() {
			candidate.Promotion = result.Action.Move.Promotion.String()
		}
		if !searchMoveIsLegal(state, from, to, movedSet) {
			continue
		}
		return &candidate, nil
	}
	return nil, fmt.Errorf("no searched action survived legality validation")
}

// searchBudgetFor maps the v1 difficulty ladder onto search budgets so the
// difficulty names keep their meaning. Beginner deliberately searches
// shallow and short; higher tiers think longer and sample more opponent
// hands (PIMC strength scales with both). Depth cap 32 is the search's own
// iterative-deepening ceiling (v1 Difficulty.SearchDepth matches it).
func searchBudgetFor(inner *v1.ComputerOpponent) (time.Duration, int) {
	switch inner.Difficulty {
	case v1.DifficultyBeginner:
		return 80 * time.Millisecond, 2
	case v1.DifficultyEasy:
		return 150 * time.Millisecond, 3
	case v1.DifficultyHard:
		return 400 * time.Millisecond, 6
	case v1.DifficultyExpert:
		return 700 * time.Millisecond, 8
	default: // medium
		return 250 * time.Millisecond, 4
	}
}

// toEnginePosition ports conform.ToPosition's MatchState->core.Position
// conversion (this file lives inside match, which conform imports, so the
// conform package itself cannot be imported back).
func toEnginePosition(state *contracts.MatchState) (*core.Position, *core.CardOverlay, error) {
	fen := matchStateToFEN(state)
	pos, err := core.ParseFEN(fen)
	if err != nil {
		return nil, nil, fmt.Errorf("searchopp: converting match state to FEN %q: %w", fen, err)
	}
	if pos.KingSquare(core.White) == core.NoSquare {
		return nil, nil, fmt.Errorf("searchopp: converted position has no white king (FEN %q, matchID %s)", fen, state.MatchID)
	}
	if pos.KingSquare(core.Black) == core.NoSquare {
		return nil, nil, fmt.Errorf("searchopp: converted position has no black king (FEN %q, matchID %s)", fen, state.MatchID)
	}
	return pos, toEngineOverlay(state), nil
}

// toEngineOverlay ports conform.ToOverlay.
func toEngineOverlay(state *contracts.MatchState) *core.CardOverlay {
	ov := core.NewCardOverlay()
	for row := 0; row < len(state.Board); row++ {
		for col := 0; col < len(state.Board[row]); col++ {
			piece := state.Board[row][col]
			if piece == nil {
				continue
			}
			sq := core.NewSquare(col, row)
			if piece.Frozen {
				ov.SetFrozen(sq, true)
			}
			if piece.Shielded {
				castFullMove := 0
				if piece.ShieldTurn != nil {
					castFullMove = *piece.ShieldTurn - 1
				}
				ov.SetShielded(sq, castFullMove)
			}
			if piece.FusedWith != "" {
				ov.SetFused(sq, core.PieceTypeFromString(piece.FusedWith))
			}
		}
	}
	for _, zone := range state.FortressZones {
		ov.SetFortress(core.ColorFromString(zone.OwnerColor), core.NewSquare(zone.LeftCol, zone.TopRow), zone.TurnsLeft)
	}
	for _, lava := range state.LavaSquares {
		ov.AddLava(core.NewSquare(lava.Col, lava.Row), lava.MovesLeft)
	}
	for _, bomb := range state.BombPieces {
		ov.AddBomb(core.NewSquare(bomb.Col, bomb.Row), core.ColorFromString(bomb.OwnerColor), bomb.TurnsLeft)
	}
	for _, hole := range state.BlackHoles {
		ov.AddBlackHole(
			core.NewSquare(hole.Sq1.Col, hole.Sq1.Row),
			core.NewSquare(hole.Sq2.Col, hole.Sq2.Row),
			core.ColorFromString(hole.OwnerColor), hole.TurnsLeft)
	}
	return ov
}

func matchStateToFEN(state *contracts.MatchState) string {
	rows := make([]string, 8)
	for row := 0; row < 8; row++ {
		rows[7-row] = fenRankString(state.Board[row])
	}
	board := strings.Join(rows, "/")

	side := "w"
	if state.Turn == "black" {
		side = "b"
	}
	castling := deriveCastlingString(state)
	enPassant := deriveEnPassantSquare(state)
	return fmt.Sprintf("%s %s %s %s %d %d", board, side, castling, enPassant, state.HalfMoveClock, state.FullMoveNum)
}

func fenRankString(row []*contracts.Piece) string {
	var b strings.Builder
	empty := 0
	flush := func() {
		if empty > 0 {
			b.WriteString(strconv.Itoa(empty))
			empty = 0
		}
	}
	for col := 0; col < len(row); col++ {
		piece := row[col]
		if piece == nil {
			empty++
			continue
		}
		flush()
		b.WriteString(fenPieceLetter(piece))
	}
	flush()
	return b.String()
}

var fenLetterByType = map[string]string{
	"pawn": "p", "knight": "n", "bishop": "b", "rook": "r", "queen": "q", "king": "k",
}

func fenPieceLetter(p *contracts.Piece) string {
	letter := fenLetterByType[p.Type]
	if letter == "" {
		letter = "q" // unknown type: never leave the FEN corrupt
	}
	if p.Color == "white" {
		return strings.ToUpper(letter)
	}
	return letter
}

func deriveCastlingString(state *contracts.MatchState) string {
	moved := make(map[string]bool, len(state.Moved))
	for _, key := range state.Moved {
		moved[key] = true
	}
	board := state.Board

	castling := ""
	if !moved["0-4"] && squareHasType(board, 0, 4, "king") {
		if !moved["0-7"] && squareHasType(board, 0, 7, "rook") {
			castling += "K"
		}
		if !moved["0-0"] && squareHasType(board, 0, 0, "rook") {
			castling += "Q"
		}
	}
	if !moved["7-4"] && squareHasType(board, 7, 4, "king") {
		if !moved["7-7"] && squareHasType(board, 7, 7, "rook") {
			castling += "k"
		}
		if !moved["7-0"] && squareHasType(board, 7, 0, "rook") {
			castling += "q"
		}
	}
	if castling == "" {
		return "-"
	}
	return castling
}

func squareHasType(board [][]*contracts.Piece, row, col int, pieceType string) bool {
	p := board[row][col]
	return p != nil && p.Type == pieceType
}

func deriveEnPassantSquare(state *contracts.MatchState) string {
	lm := state.LastMove
	if lm == nil {
		return "-"
	}
	toPiece := state.Board[lm.To.Row][lm.To.Col]
	if toPiece == nil || toPiece.Type != "pawn" {
		return "-"
	}
	rowDelta := lm.From.Row - lm.To.Row
	if rowDelta != 2 && rowDelta != -2 {
		return "-"
	}
	midRow := (lm.From.Row + lm.To.Row) / 2
	return string("abcdefgh"[lm.To.Col]) + string("12345678"[midRow])
}

func toEngineHand(cards []contracts.GameCard) actions.Hand {
	hand := make(actions.Hand, 0, len(cards))
	for _, card := range cards {
		if card.ID == "" {
			continue // face-down stubs have no identity to model
		}
		hand = append(hand, actions.CardInstance{ID: card.ID, Mechanic: actions.Mechanic(card.Mechanic)})
	}
	return hand
}

// searchMoveIsLegal validates a candidate move against this package's own
// rules (the trust anchor): the piece must exist on `from`, belong to the
// side to move, and `to` must be among its king-safety-filtered legal
// destinations. For pawn moves reaching the last rank a promotion piece is
// filled in (the rules engine treats promotion from/to pairs uniformly, and
// v1/applyMove handle the piece choice).
// doubleMoveAllowsMove reports whether the active double-move constraint
// (match_actions.go's applyMove guard) permits a move from `from`: a "same"
// (solo) double move must move the tracked piece again; a "diff" (twin)
// double move must move a DIFFERENT piece. The constraint only binds on the
// second half (MovesLeft == 1).
func doubleMoveAllowsMove(state *contracts.MatchState, from contracts.Square) bool {
	if state.DoubleMove == nil || state.DoubleMove.MovesLeft != 1 || state.DoubleMove.TrackedSq == nil {
		return true
	}
	tracked := *state.DoubleMove.TrackedSq
	same := from.Row == tracked.Row && from.Col == tracked.Col
	if state.DoubleMove.Type == "same" {
		return same
	}
	if state.DoubleMove.Type == "diff" {
		return !same
	}
	return true
}

func searchMoveIsLegal(state *contracts.MatchState, from, to contracts.Square, movedSet map[string]struct{}) bool {
	if from.Row < 0 || from.Row > 7 || from.Col < 0 || from.Col > 7 ||
		to.Row < 0 || to.Row > 7 || to.Col < 0 || to.Col > 7 {
		return false
	}
	piece := state.Board[from.Row][from.Col]
	if piece == nil || piece.Color != state.Turn {
		return false
	}
	if !doubleMoveAllowsMove(state, from) {
		return false
	}
	dests := legalMovesWithFusion(state.Board, from, state.LastMove, movedSet, state.FortressZones)
	for _, d := range dests {
		if d.Row == to.Row && d.Col == to.Col {
			return true
		}
	}
	return false
}
