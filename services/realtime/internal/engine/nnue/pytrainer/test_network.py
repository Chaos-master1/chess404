"""Sanity check for network.py's binary format, independent of Go: what
save_network writes, load_network must read back exactly -- the Python
side of the same guarantee nnue_test.go's TestSaveLoadRoundTrip proves for
the Go implementation. Run standalone: `python test_network.py`.
"""
import os
import tempfile

from network import ACCUMULATOR_SIZE, HIDDEN2_SIZE, MAGIC_HEADER, load_network, quantize_bias, quantize_weight, save_network
from features import NUM_FEATURES


def test_save_load_round_trip():
    l1_weights = [[quantize_weight(0.001 * ((f + i) % 7 - 3)) for i in range(ACCUMULATOR_SIZE)] for f in range(NUM_FEATURES)]
    l1_bias = [quantize_bias(0.01 * i) for i in range(ACCUMULATOR_SIZE)]
    l2_weights = [[quantize_weight(0.002 * ((i + j) % 5 - 2)) for j in range(HIDDEN2_SIZE)] for i in range(ACCUMULATOR_SIZE)]
    l2_bias = [quantize_bias(0.02 * j) for j in range(HIDDEN2_SIZE)]
    out_weights = [quantize_weight(0.003 * (j % 4 - 2)) for j in range(HIDDEN2_SIZE)]
    out_bias = quantize_bias(-1.5)

    fd, path = tempfile.mkstemp(suffix=".bin")
    os.close(fd)
    try:
        save_network(path, l1_weights, l1_bias, l2_weights, l2_bias, out_weights, out_bias)
        got = load_network(path)
        assert got == (l1_weights, l1_bias, l2_weights, l2_bias, out_weights, out_bias)

        with open(path, "rb") as f:
            assert f.read(len(MAGIC_HEADER)) == MAGIC_HEADER
    finally:
        os.remove(path)


def test_quantize_clamps_to_storage_range():
    assert quantize_weight(1000.0) == 32767
    assert quantize_weight(-1000.0) == -32768
    assert quantize_bias(1e12) == 2147483647
    assert quantize_bias(-1e12) == -2147483648


if __name__ == "__main__":
    test_save_load_round_trip()
    test_quantize_clamps_to_storage_range()
    print("OK: network.py save/load round trip and quantization clamping verified")
