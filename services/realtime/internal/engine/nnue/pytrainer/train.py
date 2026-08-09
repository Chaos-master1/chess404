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


def mirror_fen_board(board_field):
    """Vertical rank flip (rank r -> rank 7-r) with piece colors swapped,
    files untouched -- the standard color-mirror transform: physically,
    swap which side of the board White/Black's pieces start from, without
    also mirroring kingside/queenside (that would require reversing files
    too, which is NOT what a color swap means in chess)."""
    ranks = board_field.split("/")
    return "/".join(r.swapcase() for r in reversed(ranks))


def mirror_record(r):
    """The color-swapped counterpart of a self-play record: what White
    experienced, Black now experiences instead, and vice versa. Chess (and
    this card variant) has no inherent asymmetry between colors beyond
    move order, so training on a record's mirror image too is a free,
    exactly-valid doubling of the dataset -- and more importantly, it
    FORCES the network to learn a symmetric relationship between "my
    material/position" and the label, rather than absorbing whatever
    color imbalance happens to exist in a finite self-play sample. Without
    this, a network trained on real data came back with a strong
    White-favoring bias strong enough to misjudge even a bare king+queen
    endgame (confirmed directly: scored "Black up a whole queen" as GOOD
    for White) and lost a 200-game gauntlet 0-0-200 against the untrained
    placeholder eval as either color.
    """
    board_field = r["fen"].split()[0]
    mirrored_fen = mirror_fen_board(board_field) + " w - - 0 1"  # trailing fields are dummy; active_features never reads them
    return {
        "fen": mirrored_fen,
        "whiteHandSize": r["blackHandSize"],
        "blackHandSize": r["whiteHandSize"],
        "frozenWhite": r["frozenBlack"],
        "frozenBlack": r["frozenWhite"],
        "shieldedWhite": r["shieldedBlack"],
        "shieldedBlack": r["shieldedWhite"],
        "fortressWhite": r["fortressBlack"],
        "fortressBlack": r["fortressWhite"],
        "label": -r["label"],
    }


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
    parser.add_argument("--no-mirror", action="store_true", help="disable color-mirror data augmentation (on by default)")
    args = parser.parse_args()

    records = load_records(args.data)
    if not records:
        sys.exit(f"no records found in {args.data}")
    print(f"loaded {len(records)} positions from {args.data}", file=sys.stderr)

    if not args.no_mirror:
        records = records + [mirror_record(r) for r in records]
        print(f"augmented with color-mirrored records: {len(records)} total", file=sys.stderr)

    model = train(records, args.epochs, args.batch_size, args.lr, args.seed)
    export(model, args.out)
    print(f"wrote quantized weights to {args.out}", file=sys.stderr)


if __name__ == "__main__":
    main()
