package nnue

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
)

const (
	// AccumulatorSize is the first hidden layer's width -- the
	// incrementally-maintained part of the network.
	AccumulatorSize = 128
	// Hidden2Size is the second (small, dense, non-incremental) hidden
	// layer's width.
	Hidden2Size = 32

	// WeightScale is the single quantization scale shared by every layer
	// (a real production network tunes this per layer; one shared scale
	// keeps this phase's arithmetic simple and easy to verify by hand).
	// Weights are stored as int16 values equal to round(realWeight *
	// WeightScale).
	WeightScale = 64
	// ClipMax is the clipped-ReLU ceiling applied to every layer's
	// pre-activation sum, in the SAME scaled units the accumulator/hidden
	// layers use (i.e. corresponds to a real activation of ClipMax /
	// WeightScale before the next layer rescales it back down).
	ClipMax = 127 * WeightScale
)

// Network holds quantized weights for a 2-hidden-layer network:
//
//	sparse input (NumFeatures) --Accumulator (incremental)--> AccumulatorSize
//	  --clipped ReLU--> Hidden2Size --clipped ReLU--> 1 scalar output
//
// See the package comment for how this compares to a production NNUE.
type Network struct {
	L1Weights [NumFeatures][AccumulatorSize]int16
	L1Bias    [AccumulatorSize]int32

	L2Weights [AccumulatorSize][Hidden2Size]int16
	L2Bias    [Hidden2Size]int32

	OutWeights [Hidden2Size]int16
	OutBias    int32
}

// NewRandomNetwork returns a Network with small random weights -- useful
// for tests (verifying the accumulator/forward-pass machinery is
// internally consistent) and as an untrained starting point before
// training overwrites the weights.
func NewRandomNetwork(rng *rand.Rand) *Network {
	n := &Network{}
	randSmall := func() int16 { return int16(rng.Intn(2*WeightScale) - WeightScale) }
	for f := 0; f < NumFeatures; f++ {
		for i := 0; i < AccumulatorSize; i++ {
			n.L1Weights[f][i] = randSmall()
		}
	}
	for i := 0; i < AccumulatorSize; i++ {
		for j := 0; j < Hidden2Size; j++ {
			n.L2Weights[i][j] = randSmall()
		}
	}
	for j := 0; j < Hidden2Size; j++ {
		n.OutWeights[j] = randSmall()
	}
	return n
}

// Accumulator is the incrementally-maintained first-layer activation for
// one position -- the entire point of "incremental" NNUE: adding or
// removing one piece's feature is an O(AccumulatorSize) add/subtract of
// that feature's weight row, not an O(NumFeatures x AccumulatorSize) full
// recompute from scratch (the defect the plan flags in the OLD engine's
// NNUE, nnue.go's forward() rebuilding everything at every single node).
type Accumulator struct {
	values [AccumulatorSize]int32
}

// Refresh recomputes acc from scratch given the complete active feature
// set. Required whenever White's own king moves (every feature's king
// bucket changes at once, so no incremental delta applies) and to
// initialize a fresh search node's accumulator.
func (n *Network) Refresh(acc *Accumulator, features []int) {
	acc.values = n.L1Bias
	for _, f := range features {
		row := &n.L1Weights[f]
		for i := range acc.values {
			acc.values[i] += int32(row[i])
		}
	}
}

// Add turns feature ON incrementally.
func (n *Network) Add(acc *Accumulator, feature int) {
	row := &n.L1Weights[feature]
	for i := range acc.values {
		acc.values[i] += int32(row[i])
	}
}

// Remove turns feature OFF incrementally.
func (n *Network) Remove(acc *Accumulator, feature int) {
	row := &n.L1Weights[feature]
	for i := range acc.values {
		acc.values[i] -= int32(row[i])
	}
}

func clippedReLU(x, max int32) int32 {
	if x < 0 {
		return 0
	}
	if x > max {
		return max
	}
	return x
}

// Evaluate runs the forward pass from an already-computed accumulator
// through the remaining layers to a final scalar score, in the same
// classical-material x100 scale engine/search's placeholder eval uses (so
// the two are directly comparable and swappable in a gauntlet).
func (n *Network) Evaluate(acc *Accumulator) int {
	var h1 [AccumulatorSize]int32
	for i, v := range acc.values {
		h1[i] = clippedReLU(v, ClipMax)
	}

	var h2 [Hidden2Size]int32
	for j := 0; j < Hidden2Size; j++ {
		sum := n.L2Bias[j]
		for i := 0; i < AccumulatorSize; i++ {
			sum += (h1[i] / WeightScale) * int32(n.L2Weights[i][j])
		}
		h2[j] = clippedReLU(sum, ClipMax)
	}

	out := n.OutBias
	for j := 0; j < Hidden2Size; j++ {
		out += (h2[j] / WeightScale) * int32(n.OutWeights[j])
	}
	return int(out / WeightScale)
}

// magicHeader identifies this package's weight file format -- distinct
// from the old engine's nnue_weights.bin, which is a completely different
// architecture (a from-scratch float32 MLP, see internal/engine/nnue.go),
// not a version of this one.
const magicHeader = "C404NNU2"

// Save writes n in a simple, fully-specified binary format: an 8-byte
// magic header, then every weight/bias array in field order, each element
// little-endian.
func (n *Network) Save(w io.Writer) error {
	if _, err := w.Write([]byte(magicHeader)); err != nil {
		return err
	}
	for f := 0; f < NumFeatures; f++ {
		if err := binary.Write(w, binary.LittleEndian, n.L1Weights[f]); err != nil {
			return fmt.Errorf("writing L1Weights[%d]: %w", f, err)
		}
	}
	if err := binary.Write(w, binary.LittleEndian, n.L1Bias); err != nil {
		return fmt.Errorf("writing L1Bias: %w", err)
	}
	for i := 0; i < AccumulatorSize; i++ {
		if err := binary.Write(w, binary.LittleEndian, n.L2Weights[i]); err != nil {
			return fmt.Errorf("writing L2Weights[%d]: %w", i, err)
		}
	}
	if err := binary.Write(w, binary.LittleEndian, n.L2Bias); err != nil {
		return fmt.Errorf("writing L2Bias: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, n.OutWeights); err != nil {
		return fmt.Errorf("writing OutWeights: %w", err)
	}
	return binary.Write(w, binary.LittleEndian, n.OutBias)
}

// Load reads a Network written by Save.
func Load(r io.Reader) (*Network, error) {
	header := make([]byte, len(magicHeader))
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	if string(header) != magicHeader {
		return nil, fmt.Errorf("unrecognized weights file header %q (expected %q -- this is not a Phase 3 nnue package weights file)", header, magicHeader)
	}

	n := &Network{}
	for f := 0; f < NumFeatures; f++ {
		if err := binary.Read(r, binary.LittleEndian, &n.L1Weights[f]); err != nil {
			return nil, fmt.Errorf("reading L1Weights[%d]: %w", f, err)
		}
	}
	if err := binary.Read(r, binary.LittleEndian, &n.L1Bias); err != nil {
		return nil, fmt.Errorf("reading L1Bias: %w", err)
	}
	for i := 0; i < AccumulatorSize; i++ {
		if err := binary.Read(r, binary.LittleEndian, &n.L2Weights[i]); err != nil {
			return nil, fmt.Errorf("reading L2Weights[%d]: %w", i, err)
		}
	}
	if err := binary.Read(r, binary.LittleEndian, &n.L2Bias); err != nil {
		return nil, fmt.Errorf("reading L2Bias: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &n.OutWeights); err != nil {
		return nil, fmt.Errorf("reading OutWeights: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &n.OutBias); err != nil {
		return nil, fmt.Errorf("reading OutBias: %w", err)
	}
	return n, nil
}
