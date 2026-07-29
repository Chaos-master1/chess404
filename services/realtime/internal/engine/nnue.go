package engine

import (
	"encoding/binary"
	"log"
	"math"
	"os"
	"strings"

	"github.com/chess404/realtime/internal/contracts"
)

type NNUE struct {
	InputSize      int
	HiddenSize     int
	HiddenSize2    int
	Weights [][]float32
	Biases  [][]float32
	loaded  bool
}

const (
	nnuePieceTypes = 12
	nnueSquares    = 64
	nnueModifiers  = 5
	nnueHandSize   = 74
	nnueInputSize  = nnuePieceTypes*nnueSquares + nnueModifiers + nnueHandSize
	nnueHiddenSize = 1024
	nnueHidden2Size = 1024
)

var mechanicNames = [37]string{
	"freeze", "shield", "sniper", "badsniper", "teleport", "jump",
	"swapme", "swapus", "swaphim", "clone", "halffuse", "fullfusion",
	"doublemove_same", "doublemove_diff", "promote", "demote", "demotehim", "promotehim",
	"mindcontrol", "borrow", "reverse", "undo", "mirror", "invisible",
	"lavaground", "fog_village", "fortress", "radar",
	"unabomber", "blackhole", "parasite", "fakepiece", "cheater", "gambler",
	"smallsacrifice", "bigsacrifice", "joker",
}

var defaultNNUE *NNUE

// nnueEnabled reports whether the learned evaluation should be used instead of
// the hand-crafted one.
//
// It defaults to OFF, deliberately. The shipped nnue_weights.bin does not
// evaluate positions correctly: the trainer encodes the board with a8 as square
// 0 (scripts/train_nnue.py) while this package encodes a1 as square 0, so the
// network is queried on the vertical mirror of what it was fitted to; the
// trainer fits plain ReLU while inference below applies leaky ReLU with slope
// 0.1; the 5 modifier inputs are never set during training but are set here;
// and self-play deals hands without ever playing a card, so the 74 card
// features were fit against a target they cannot influence. The observable
// result is that the symmetric starting position scores about -322cp and
// removing a black pawn makes White's score worse.
//
// Until those are fixed and the network is retrained, the hand-crafted
// evaluation is strictly better. Set CHESS404_ENGINE_NNUE=1 to opt in for
// experiments -- Evaluate is still wired up and exercised by tests.
func nnueEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CHESS404_ENGINE_NNUE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

const nnueWeightsFilename = "nnue_weights.bin"

func init() {
	defaultNNUE = &NNUE{}
	// The error is intentionally non-fatal: the weights file is optional and
	// EvaluateWithModifiers falls back to the hand-crafted evaluation. It is
	// logged rather than swallowed so a missing/corrupt file is diagnosable
	// instead of silently degrading the engine, which is what happened before.
	if err := defaultNNUE.Load(nnueWeightsFilename); err != nil {
		log.Printf("engine: NNUE weights not loaded (%v); using hand-crafted evaluation", err)
	}
}

func NewNNUE() *NNUE {
	return &NNUE{
		InputSize:   nnueInputSize,
		HiddenSize:  nnueHiddenSize,
		HiddenSize2: nnueHidden2Size,
	}
}

// Load reads the weights file. It tries, in order: an explicit absolute path
// from CHESS404_NNUE_WEIGHTS_PATH, then `path` and a couple of relative
// fallbacks for `go test`/local dev, where the working directory is
// predictably services/realtime or one of its subpackages.
//
// The relative fallbacks alone are why this silently never found the file in
// production: match-service.Dockerfile's runtime image never copied
// nnue_weights.bin in at all, and even if it had, the container's working
// directory is / (CMD ["/usr/local/bin/match-service"]), which none of
// "nnue_weights.bin", "../nnue_weights.bin", "../../nnue_weights.bin" resolve
// from -- so Load failed every time, silently (the error was swallowed
// entirely until the surrounding init() fix). The env var gives deployment a
// way to point at wherever the file actually lands that doesn't depend on
// guessing the working directory.
func (n *NNUE) Load(path string) error {
	var data []byte
	var err error
	paths := []string{path, "../" + path, "../../" + path}
	if explicit := strings.TrimSpace(os.Getenv("CHESS404_NNUE_WEIGHTS_PATH")); explicit != "" {
		paths = append([]string{explicit}, paths...)
	}
	for _, p := range paths {
		data, err = os.ReadFile(p)
		if err == nil {
			break
		}
	}
	if err != nil {
		return err
	}
	if len(data) < 12 {
		return os.ErrClosed
	}
	inputSize := int(binary.LittleEndian.Uint32(data[0:4]))
	hiddenSize := int(binary.LittleEndian.Uint32(data[4:8]))
	hiddenSize2 := int(binary.LittleEndian.Uint32(data[8:12]))
	if inputSize != nnueInputSize || hiddenSize != nnueHiddenSize || hiddenSize2 != nnueHidden2Size {
		return os.ErrClosed
	}
	n.InputSize = inputSize
	n.HiddenSize = hiddenSize
	n.HiddenSize2 = hiddenSize2

	offset := 12
	n.Weights = make([][]float32, 3)
	n.Biases = make([][]float32, 3)

	n.Weights[0] = make([]float32, inputSize*hiddenSize)
	n.Biases[0] = make([]float32, hiddenSize)
	n.Weights[1] = make([]float32, hiddenSize*hiddenSize2)
	n.Biases[1] = make([]float32, hiddenSize2)
	n.Weights[2] = make([]float32, hiddenSize2)
	n.Biases[2] = make([]float32, 1)

	for i := range n.Weights[0] {
		n.Weights[0][i] = math.Float32frombits(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
	}
	for i := range n.Biases[0] {
		n.Biases[0][i] = math.Float32frombits(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
	}
	for i := range n.Weights[1] {
		n.Weights[1][i] = math.Float32frombits(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
	}
	for i := range n.Biases[1] {
		n.Biases[1][i] = math.Float32frombits(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
	}
	for i := range n.Weights[2] {
		n.Weights[2][i] = math.Float32frombits(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
	}
	for i := range n.Biases[2] {
		n.Biases[2][i] = math.Float32frombits(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
	}
	n.loaded = true
	return nil
}

func (n *NNUE) Evaluate(board [][]*contracts.Piece, lavas []contracts.LavaSquare, fortresses []contracts.FortressZone, bombs []contracts.BombPiece, fogs []contracts.FogZone, blackHoles []contracts.BlackHoleZone, whiteHand, blackHand []contracts.GameCard) int {
	if !n.loaded {
		return 0
	}
	input := make([]float32, nnueInputSize)
	n.encodeBoard(board, input)
	n.encodeModifiers(lavas, fortresses, bombs, fogs, blackHoles, input)
	n.encodeHand(whiteHand, blackHand, input)

	h1 := make([]float32, n.HiddenSize)
	n.forward(input, nil, h1)
	h2 := make([]float32, n.HiddenSize2)
	n.forward(h1, h2, nil)
	var output float32
	for j := range h2 {
		if h2[j] != 0 {
			output += n.Weights[2][j] * h2[j]
		}
	}
	output += n.Biases[2][0]

	return int(output * 100)
}

// forward applies one linear layer plus ReLU. This must match
// scripts/train_nnue.py's NNUE.forward exactly (`torch.clamp(h, min=0)`,
// i.e. plain ReLU) -- it previously used leaky ReLU with an arbitrary,
// undocumented slope of 0.1 for negative sums, so every negative-preactivation
// unit in a trained network computed a different function at inference than
// the one it was fit to compute under.
func (n *NNUE) forward(input, output, hidden []float32) {
	if hidden != nil {
		for j := range hidden {
			var sum float32
			base := j * n.InputSize
			for i := 0; i < n.InputSize; i++ {
				if input[i] != 0 {
					sum += n.Weights[0][base+i] * input[i]
				}
			}
			sum += n.Biases[0][j]
			if sum < 0 {
				sum = 0
			}
			hidden[j] = sum
		}
		return
	}
	for j := range output {
		var sum float32
		base := j * n.HiddenSize
		for i := 0; i < n.HiddenSize; i++ {
			if input[i] != 0 {
				sum += n.Weights[1][base+i] * input[i]
			}
		}
		sum += n.Biases[1][j]
		if sum < 0 {
			sum = 0
		}
		output[j] = sum
	}
}

func (n *NNUE) Loaded() bool { return n.loaded }

// encodeBoard sets one input feature per occupied square, at index
// `(colorIdx*6 + typeIdx)*64 + sq` where `sq = boardRow*8 + col` and
// `boardRow` follows THIS PACKAGE's own convention: board[0] is rank 1 (a1 is
// square 0), matching every other Go function in this package (chess.go,
// perft.go's FEN parser at `boardRow := 7 - fenRow`, search.go) and the wire
// contract.MatchState.Board itself.
//
// scripts/train_nnue.py's encode_fen must produce the identical index for the
// identical real-world square. A FEN's rank order is top-to-bottom (its first
// '/'-separated segment is rank 8), the OPPOSITE of boardRow -- so the trainer
// converts with the same `7 - fenRow` most Go FEN parsing already uses,
// rather than using the raw FEN row directly. It did not do this: it computed
// `sq = fen_row*8 + col`, i.e. treated a8 as square 0. Every trained weight
// was therefore fit against the vertical mirror of the position this function
// queries it on -- confirmed by TestNNUEBoardEncodingContractWithTrainer,
// which pins the exact index this function must produce for a fixed
// reference square, matching the corrected trainer formula in a comment
// alongside it so the two can be checked against each other by inspection.
func (n *NNUE) encodeBoard(board [][]*contracts.Piece, input []float32) {
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			p := board[r][c]
			if p == nil {
				continue
			}
			colorIdx := 0
			if p.Color == "black" {
				colorIdx = 1
			}
			typeIdx := pieceNNUEIndex(p.Type)
			pieceSq := (colorIdx*6 + typeIdx) * 64
			sq := r*8 + c
			if pieceSq+sq < nnuePieceTypes*nnueSquares {
				input[pieceSq+sq] = 1.0
			}
		}
	}
}

func (n *NNUE) encodeModifiers(lavas []contracts.LavaSquare, fortresses []contracts.FortressZone, bombs []contracts.BombPiece, fogs []contracts.FogZone, blackHoles []contracts.BlackHoleZone, input []float32) {
	modOffset := nnuePieceTypes * nnueSquares
	if len(lavas) > 0 && modOffset < nnueInputSize {
		input[modOffset] = 1.0
	}
	modOffset++
	if len(bombs) > 0 && modOffset < nnueInputSize {
		input[modOffset] = 1.0
	}
	modOffset++
	if len(fortresses) > 0 && modOffset < nnueInputSize {
		input[modOffset] = 1.0
	}
	modOffset++
	if len(fogs) > 0 && modOffset < nnueInputSize {
		input[modOffset] = 1.0
	}
	modOffset++
	if len(blackHoles) > 0 && modOffset < nnueInputSize {
		input[modOffset] = 1.0
	}
}

func (n *NNUE) encodeHand(whiteHand, blackHand []contracts.GameCard, input []float32) {
	handOffset := nnuePieceTypes*nnueSquares + nnueModifiers

	whiteMap := make(map[string]bool, len(whiteHand))
	for _, c := range whiteHand {
		whiteMap[c.Mechanic] = true
	}
	blackMap := make(map[string]bool, len(blackHand))
	for _, c := range blackHand {
		blackMap[c.Mechanic] = true
	}

	for i, mechanic := range mechanicNames {
		idx := handOffset + i
		if idx < nnueInputSize && whiteMap[mechanic] {
			input[idx] = 1.0
		}
		idx2 := handOffset + 37 + i
		if idx2 < nnueInputSize && blackMap[mechanic] {
			input[idx2] = 1.0
		}
	}
}

func pieceNNUEIndex(ptype string) int {
	switch ptype {
	case "pawn":
		return 0
	case "knight":
		return 1
	case "bishop":
		return 2
	case "rook":
		return 3
	case "queen":
		return 4
	case "king":
		return 5
	}
	return 0
}
