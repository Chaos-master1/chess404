"""
Independent re-derivation of the NNUE feature encoder for chess404.

This file is written ONLY from:
  - services/realtime/internal/engine/nnue/features.go  (Go source)
  - services/realtime/internal/engine/nnue/pytrainer/testdata/golden_features.json  (the oracle)

It does NOT copy from pytrainer/features.py. If pytrainer/features.py and
this file agree, the agreement is independent evidence of correctness; if
they diverge, this file agrees with features.go (by construction) and
pytrainer/features.py is the suspect.

The encoding rules (taken from features.go):
  - White's king square selects a bucket in {0,1,2,3} (4 quadrants).
  - For each non-king piece on a square, emit one feature index:
      bucket * 640 + sq*10 + pieceType*2 + color
    where pieceType is in {0..4} for {Pawn, Knight, Bishop, Rook, Queen}.
    (So full piece-square slot is 64*5*2 = 640.)
  - Then 6 count buckets, each a 1-of-3 one-hot in {0, 1, 2-or-more}:
      handWhite, handBlack, frozenWhite, frozenBlack, shieldedWhite, shieldedBlack
  - Then 2 fortress booleans: fortressWhite, fortressBlack.
  - Total: 4*640 + 3*6 + 2 = 2580.
"""

import json
import sys
from pathlib import Path

# ---- constants copied DIRECTLY from features.go:27-55 ----
NUM_KING_BUCKETS = 4
NUM_PIECE_SQUARE_FEATURES = 64 * 5 * 2
NUM_CHESS_FEATURES = NUM_KING_BUCKETS * NUM_PIECE_SQUARE_FEATURES

NUM_COUNT_BUCKETS = 3
NUM_CARD_COUNT_FEATURES = NUM_COUNT_BUCKETS * 6
NUM_CARD_BOOL_FEATURES = 2
NUM_CARD_FEATURES = NUM_CARD_COUNT_FEATURES + NUM_CARD_BOOL_FEATURES
NUM_FEATURES = NUM_CHESS_FEATURES + NUM_CARD_FEATURES

# pieceTypeIndex from features.go:57-59
PIECE_TYPE_INDEX = {"P": 0, "N": 1, "B": 2, "R": 3, "Q": 4}

# core.PieceType values: Pawn=1, Knight=2, Bishop=3, Rook=4, Queen=5, King=6
PIECE_TYPE_FROM_FEN = {
    "P": 1, "N": 2, "B": 3, "R": 4, "Q": 5, "K": 6,
    "p": 1, "n": 2, "b": 3, "r": 4, "q": 5, "k": 6,
}
WHITE, BLACK = 0, 1
KING = 6


def king_bucket(file_idx, rank_idx):
    """features.go:62-71: kingBucket for square in board-internal coords.
    file_idx in 0..7 (a..h), rank_idx in 0..7 (rank1..rank8 internal).
    """
    bucket = 0
    if file_idx >= 4:
        bucket |= 1
    if rank_idx >= 4:
        bucket |= 2
    return bucket


def parse_fen_board(fen):
    """Returns (piece_at, white_king_square).
    piece_at: dict {square: (piece_type_core, color)} with square = rank*8+file,
       rank=0 is rank 1 (White's back rank), file=0 is a-file. Matches core.Square.
    white_king_square: square index of the white king, or None.
    FEN ranks are top-to-bottom (rank 8 first), files left-to-right.
    """
    board_field = fen.split()[0]
    ranks = board_field.split("/")
    if len(ranks) != 8:
        raise ValueError(f"FEN board must have 8 ranks: {fen!r}")

    piece_at = {}
    white_king_sq = None
    for i, rank_str in enumerate(ranks):
        internal_rank = 7 - i  # FEN rank 8 (i=0) -> internal rank 7
        file_idx = 0
        for ch in rank_str:
            if ch.isdigit():
                file_idx += int(ch)
                continue
            color = BLACK if ch.islower() else WHITE
            pt = PIECE_TYPE_FROM_FEN[ch]
            sq = internal_rank * 8 + file_idx
            piece_at[sq] = (pt, color)
            if pt == KING and color == WHITE:
                white_king_sq = sq
            file_idx += 1
        if file_idx != 8:
            raise ValueError(f"rank {rank_str!r} does not sum to 8 files: {fen!r}")

    if white_king_sq is None:
        raise ValueError(f"FEN has no white king: {fen!r}")
    return piece_at, white_king_sq


def active_features_independent(
    fen,
    white_hand_size,
    black_hand_size,
    frozen_white,
    frozen_black,
    shielded_white,
    shielded_black,
    fortress_white,
    fortress_black,
):
    """features.go:124-150 re-implemented line-for-line in Python.
    Order of iteration over squares is 0..63 (matches Go's for sq := 0; sq < 64).
    """
    piece_at, wk_sq = parse_fen_board(fen)
    bucket = king_bucket(wk_sq % 8, wk_sq // 8)

    feats = []
    for sq in range(64):
        pt_color = piece_at.get(sq)
        if pt_color is None:
            continue
        pt, c = pt_color
        if pt == KING:
            continue
        idx = bucket * NUM_PIECE_SQUARE_FEATURES + sq * 10 + PIECE_TYPE_INDEX[
            {1: "P", 2: "N", 3: "B", 4: "R", 5: "Q"}[pt]
        ] * 2 + c
        feats.append(idx)

    base = NUM_CHESS_FEATURES
    for count in (white_hand_size, black_hand_size, frozen_white, frozen_black, shielded_white, shielded_black):
        b = count
        if b > NUM_COUNT_BUCKETS - 1:
            b = NUM_COUNT_BUCKETS - 1
        feats.append(base + b)
        base += NUM_COUNT_BUCKETS

    if fortress_white:
        feats.append(base)
    base += 1
    if fortress_black:
        feats.append(base)
    base += 1
    return sorted(feats)


def main():
    golden_path = Path("services/realtime/internal/engine/nnue/testdata/golden_features.json")
    data = json.loads(golden_path.read_text())
    total = len(data)
    passed = 0
    failures = []
    for case in data:
        got = active_features_independent(
            case["fen"],
            case["whiteHandSize"],
            case["blackHandSize"],
            case["frozenWhite"],
            case["frozenBlack"],
            case["shieldedWhite"],
            case["shieldedBlack"],
            case["fortressWhite"],
            case["fortressBlack"],
        )
        want = sorted(case["features"])
        if got == want:
            passed += 1
        else:
            missing = sorted(set(want) - set(got))
            extra = sorted(set(got) - set(want))
            failures.append((case["name"], case["fen"], want, got, missing, extra))

    print(f"Encoder parity: {passed}/{total} golden cases match.")
    if failures:
        print("\nFAILURES:")
        for name, fen, want, got, missing, extra in failures:
            print(f"\n  case: {name}")
            print(f"  fen:  {fen}")
            print(f"  want ({len(want)}): {want}")
            print(f"  got  ({len(got)}): {got}")
            if missing:
                print(f"  missing from got: {missing}")
            if extra:
                print(f"  extra in got:     {extra}")
        sys.exit(1)
    else:
        print("All encoder outputs match Go golden_features.json.")
        print(f"NUM_FEATURES = {NUM_FEATURES} (Go: features.go:54 NumFeatures = numChessFeatures + numCardFeatures)")
        print(f"  = {NUM_KING_BUCKETS} * {NUM_PIECE_SQUARE_FEATURES} + {NUM_COUNT_BUCKETS}*6 + {NUM_CARD_BOOL_FEATURES}")


if __name__ == "__main__":
    main()
