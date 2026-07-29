package engine

import (
	"encoding/binary"
	"math"
	"os"

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

func init() {
	defaultNNUE = &NNUE{}
	if err := defaultNNUE.Load("nnue_weights.bin"); err != nil {
	}
}

func NewNNUE() *NNUE {
	return &NNUE{
		InputSize:   nnueInputSize,
		HiddenSize:  nnueHiddenSize,
		HiddenSize2: nnueHidden2Size,
	}
}

func (n *NNUE) Load(path string) error {
	var data []byte
	var err error
	paths := []string{path, "../" + path, "../../" + path}
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
				sum *= 0.1
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
			sum *= 0.1
		}
		output[j] = sum
	}
}

func (n *NNUE) Loaded() bool { return n.loaded }

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
