"""Python port of internal/engine/nnue/network.go's quantization and binary
weights format. train.py trains in ordinary float32, then this module
quantizes and serializes the result into EXACTLY the layout Go's Load()
expects: an 8-byte magic header, then L1Weights/L1Bias/L2Weights/L2Bias/
OutWeights/OutBias in that field order, every scalar little-endian --
so Go can load a Python-trained network with zero format translation.

WeightScale is shared by every layer, matching network.go's package
comment: quantized(x) = round(x * WEIGHT_SCALE), clamped to the storage
type's range (int16 for weights, int32 for biases).
"""
import struct

from features import NUM_FEATURES

ACCUMULATOR_SIZE = 128
HIDDEN2_SIZE = 32
WEIGHT_SCALE = 64
CLIP_MAX = 127 * WEIGHT_SCALE
# The activation ceiling in "real" (unquantized) units -- ClipMax expressed
# as a real-valued clamp for training, since training happens in float
# space and quantization is a post-hoc step (see train.py).
CLIP_MAX_REAL = CLIP_MAX / WEIGHT_SCALE

MAGIC_HEADER = b"C404NNU2"

_INT16_MIN, _INT16_MAX = -32768, 32767
_INT32_MIN, _INT32_MAX = -2147483648, 2147483647


def _clamp(x, lo, hi):
    return max(lo, min(hi, x))


def quantize_weight(x):
    return _clamp(round(x * WEIGHT_SCALE), _INT16_MIN, _INT16_MAX)


def quantize_bias(x):
    return _clamp(round(x * WEIGHT_SCALE), _INT32_MIN, _INT32_MAX)


def save_network(path, l1_weights, l1_bias, l2_weights, l2_bias, out_weights, out_bias):
    """Writes a network in network.go's Save/Load binary format.

    l1_weights: NUM_FEATURES rows x ACCUMULATOR_SIZE int16 (Go's L1Weights[f][i])
    l1_bias:    ACCUMULATOR_SIZE int32                     (Go's L1Bias[i])
    l2_weights: ACCUMULATOR_SIZE rows x HIDDEN2_SIZE int16  (Go's L2Weights[i][j])
    l2_bias:    HIDDEN2_SIZE int32                          (Go's L2Bias[j])
    out_weights: HIDDEN2_SIZE int16                         (Go's OutWeights[j])
    out_bias:   int32                                       (Go's OutBias)
    """
    if len(l1_weights) != NUM_FEATURES:
        raise ValueError(f"expected {NUM_FEATURES} L1Weights rows, got {len(l1_weights)}")
    if len(l2_weights) != ACCUMULATOR_SIZE:
        raise ValueError(f"expected {ACCUMULATOR_SIZE} L2Weights rows, got {len(l2_weights)}")

    with open(path, "wb") as f:
        f.write(MAGIC_HEADER)
        for row in l1_weights:
            f.write(struct.pack(f"<{ACCUMULATOR_SIZE}h", *row))
        f.write(struct.pack(f"<{ACCUMULATOR_SIZE}i", *l1_bias))
        for row in l2_weights:
            f.write(struct.pack(f"<{HIDDEN2_SIZE}h", *row))
        f.write(struct.pack(f"<{HIDDEN2_SIZE}i", *l2_bias))
        f.write(struct.pack(f"<{HIDDEN2_SIZE}h", *out_weights))
        f.write(struct.pack("<i", out_bias))


def load_network(path):
    """Reads a network written by save_network (or by Go's Save) -- mainly
    useful for tests that want to confirm a round trip without needing a Go
    process, e.g. asserting save_network(load_network(x)) == x."""
    with open(path, "rb") as f:
        header = f.read(len(MAGIC_HEADER))
        if header != MAGIC_HEADER:
            raise ValueError(f"unrecognized weights file header {header!r} (expected {MAGIC_HEADER!r})")

        l1_weights = [list(struct.unpack(f"<{ACCUMULATOR_SIZE}h", f.read(2 * ACCUMULATOR_SIZE))) for _ in range(NUM_FEATURES)]
        l1_bias = list(struct.unpack(f"<{ACCUMULATOR_SIZE}i", f.read(4 * ACCUMULATOR_SIZE)))
        l2_weights = [list(struct.unpack(f"<{HIDDEN2_SIZE}h", f.read(2 * HIDDEN2_SIZE))) for _ in range(ACCUMULATOR_SIZE)]
        l2_bias = list(struct.unpack(f"<{HIDDEN2_SIZE}i", f.read(4 * HIDDEN2_SIZE)))
        out_weights = list(struct.unpack(f"<{HIDDEN2_SIZE}h", f.read(2 * HIDDEN2_SIZE)))
        out_bias = struct.unpack("<i", f.read(4))[0]

    return l1_weights, l1_bias, l2_weights, l2_bias, out_weights, out_bias
