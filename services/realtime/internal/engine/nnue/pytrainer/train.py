"""Trains a Phase-3-compatible NNUE network from Go self-play data and
exports it in Go's exact binary weights format.

Usage:
    go run ./cmd/nnue-selfplay -games 300 -out selfplay.jsonl
    python train.py selfplay.jsonl trained.bin

Architecture mirrors internal/engine/nnue/network.go exactly (see that
file's package comment and this directory's network.py): sparse
NUM_FEATURES input, summed via an EmbeddingBag into a 128-wide accumulator
-- this dense sum IS what Go's Network.Refresh/Add/Remove maintain
incrementally, just computed from scratch per sample here -- clipped-ReLU,
Linear(128, 32), clipped-ReLU, Linear(32, 1). Trained in real-valued
(float) space against each self-play position's final-game-outcome label
(SelfPlayRecord.Label, White's perspective, see selfplay.go), then every
weight/bias in every layer is quantized by network.py's shared
WEIGHT_SCALE and written via save_network -- so Go's Load()/Evaluate() can
consume the result with no format translation.

The clipped-ReLU activation is clamped to CLIP_MAX_REAL (network.go's
ClipMax expressed in real, unquantized units) during training so the
learned weights already live in a range that survives quantization without
the clip ceiling silently discarding signal.
"""
import argparse
import itertools
import json
import sys

import torch
import torch.nn as nn

from features import NUM_FEATURES, active_features
from network import ACCUMULATOR_SIZE, CLIP_MAX_REAL, HIDDEN2_SIZE, quantize_bias, quantize_weight, save_network


class NNUE(nn.Module):
    def __init__(self):
        super().__init__()
        self.l1 = nn.EmbeddingBag(NUM_FEATURES, ACCUMULATOR_SIZE, mode="sum")
        self.l1_bias = nn.Parameter(torch.zeros(ACCUMULATOR_SIZE))
        self.l2 = nn.Linear(ACCUMULATOR_SIZE, HIDDEN2_SIZE)
        self.out = nn.Linear(HIDDEN2_SIZE, 1)
        nn.init.uniform_(self.l1.weight, -0.05, 0.05)

    def forward(self, indices, offsets):
        h0 = self.l1(indices, offsets) + self.l1_bias
        h1 = torch.clamp(h0, 0.0, CLIP_MAX_REAL)
        h2 = torch.clamp(self.l2(h1), 0.0, CLIP_MAX_REAL)
        return self.out(h2).squeeze(-1)


def load_records(path):
    records = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line:
                records.append(json.loads(line))
    return records


def encode_record(r):
    return active_features(
        r["fen"],
        r["whiteHandSize"],
        r["blackHandSize"],
        r["frozenWhite"],
        r["frozenBlack"],
        r["shieldedWhite"],
        r["shieldedBlack"],
        r["fortressWhite"],
        r["fortressBlack"],
    )


def batch_tensors(records, feature_cache, idxs, device):
    feats_batch = [feature_cache[i] for i in idxs]
    labels = torch.tensor([records[i]["label"] for i in idxs], dtype=torch.float32, device=device)
    indices = torch.tensor(list(itertools.chain.from_iterable(feats_batch)), dtype=torch.long, device=device)
    offsets = torch.tensor(
        [0] + list(itertools.accumulate(len(f) for f in feats_batch))[:-1],
        dtype=torch.long,
        device=device,
    )
    return indices, offsets, labels


def train(records, epochs, batch_size, lr, seed):
    torch.manual_seed(seed)
    device = "cuda" if torch.cuda.is_available() else "cpu"

    feature_cache = [encode_record(r) for r in records]
    model = NNUE().to(device)
    optimizer = torch.optim.Adam(model.parameters(), lr=lr)
    loss_fn = nn.MSELoss()

    n = len(records)
    for epoch in range(epochs):
        perm = torch.randperm(n).tolist()
        total_loss = 0.0
        for start in range(0, n, batch_size):
            batch_idx = perm[start : start + batch_size]
            indices, offsets, labels = batch_tensors(records, feature_cache, batch_idx, device)

            optimizer.zero_grad()
            pred = model(indices, offsets)
            loss = loss_fn(pred, labels)
            loss.backward()
            optimizer.step()
            total_loss += loss.item() * len(batch_idx)

        print(f"epoch {epoch + 1}/{epochs}: mse={total_loss / n:.2f}", file=sys.stderr)

    return model


def export(model, out_path):
    l1_weight = model.l1.weight.detach().cpu()
    l1_bias = model.l1_bias.detach().cpu()
    l2_weight = model.l2.weight.detach().cpu()  # torch Linear: (out=HIDDEN2_SIZE, in=ACCUMULATOR_SIZE)
    l2_bias = model.l2.bias.detach().cpu()
    out_weight = model.out.weight.detach().cpu().view(-1)  # (HIDDEN2_SIZE,)
    out_bias = model.out.bias.detach().cpu().item()

    q_l1_weights = [
        [quantize_weight(l1_weight[f, i].item()) for i in range(ACCUMULATOR_SIZE)] for f in range(NUM_FEATURES)
    ]
    q_l1_bias = [quantize_bias(l1_bias[i].item()) for i in range(ACCUMULATOR_SIZE)]
    # network.go's L2Weights is [AccumulatorSize][Hidden2Size], i.e. indexed
    # [input unit][output unit] -- the TRANSPOSE of torch's Linear.weight,
    # which is [out_features, in_features].
    q_l2_weights = [
        [quantize_weight(l2_weight[j, i].item()) for j in range(HIDDEN2_SIZE)] for i in range(ACCUMULATOR_SIZE)
    ]
    q_l2_bias = [quantize_bias(l2_bias[j].item()) for j in range(HIDDEN2_SIZE)]
    q_out_weights = [quantize_weight(out_weight[j].item()) for j in range(HIDDEN2_SIZE)]
    q_out_bias = quantize_bias(out_bias)

    save_network(out_path, q_l1_weights, q_l1_bias, q_l2_weights, q_l2_bias, q_out_weights, q_out_bias)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("data", help="JSONL self-play data (cmd/nnue-selfplay output)")
    parser.add_argument("out", help="output path for the quantized .bin weights file")
    parser.add_argument("--epochs", type=int, default=30)
    parser.add_argument("--batch-size", type=int, default=64)
    parser.add_argument("--lr", type=float, default=1e-3)
    parser.add_argument("--seed", type=int, default=0)
    args = parser.parse_args()

    records = load_records(args.data)
    if not records:
        sys.exit(f"no records found in {args.data}")
    print(f"loaded {len(records)} positions from {args.data}", file=sys.stderr)

    model = train(records, args.epochs, args.batch_size, args.lr, args.seed)
    export(model, args.out)
    print(f"wrote quantized weights to {args.out}", file=sys.stderr)


if __name__ == "__main__":
    main()
