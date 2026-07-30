"""Python port of internal/engine/nnue/features.go.

This module MUST stay index-for-index identical to the Go implementation.
test_roundtrip.py verifies that against testdata/golden_features.json,
which the Go test TestGenerateNNUEFeatureFixtures
(../fixtures_test.go) regenerates from the live Go code on every run --
so the golden file can never silently drift out of sync with Go, only
Python's port of it can, which is exactly what the round-trip test
catches.

Only piece placement and White's king square come from the FEN; every
other field (hand sizes, frozen/shielded counts, fortress booleans) is
passed in directly, matching ActiveFeatures' own signature (it takes
counts, not squares -- see that function's doc comment).
"""

NUM_KING_BUCKETS = 4
# 64 squares x 5 non-king piece types x 2 colors.
NUM_PIECE_SQUARE_FEATURES = 64 * 5 * 2
NUM_CHESS_FEATURES = NUM_KING_BUCKETS * NUM_PIECE_SQUARE_FEATURES

# Buckets a count into {0, 1, 2-or-more}.
NUM_COUNT_BUCKETS = 3
NUM_CARD_COUNT_FEATURES = NUM_COUNT_BUCKETS * 6
NUM_CARD_BOOL_FEATURES = 2
NUM_CARD_FEATURES = NUM_CARD_COUNT_FEATURES + NUM_CARD_BOOL_FEATURES

NUM_FEATURES = NUM_CHESS_FEATURES + NUM_CARD_FEATURES

WHITE, BLACK = 0, 1

# core.PieceType values (types.go): NoPieceType=0, Pawn=1, Knight=2,
# Bishop=3, Rook=4, Queen=5, King=6. pieceTypeIndex (features.go) remaps
# the five non-king types to a dense 0..4 range; King is never looked up
# in it (ActiveFeatures skips king squares entirely).
PIECE_TYPE_INDEX = {1: 0, 2: 1, 3: 2, 4: 3, 5: 4}
KING_PIECE_TYPE = 6

FEN_PIECE_TYPE = {"p": 1, "n": 2, "b": 3, "r": 4, "q": 5, "k": 6}


def king_bucket(king_file, king_rank):
    """Mirrors features.go's kingBucket: quadrant of the board White's king sits in."""
    bucket = 0
    if king_file >= 4:
        bucket |= 1
    if king_rank >= 4:
        bucket |= 2
    return bucket


def piece_square_feature(square, piece_type, color):
    return square * 5 * 2 + PIECE_TYPE_INDEX[piece_type] * 2 + color


def piece_feature(bucket, square, piece_type, color):
    return bucket * NUM_PIECE_SQUARE_FEATURES + piece_square_feature(square, piece_type, color)


def _append_count_bucket(feats, base, count):
    bucket = min(count, NUM_COUNT_BUCKETS - 1)
    feats.append(base + bucket)
    return base + NUM_COUNT_BUCKETS


class Board:
    """Piece placement decoded from a FEN's first field only. Square
    numbering matches core.Square exactly: rank*8+file, 0=a1 .. 63=h8
    (see core/bitboard.go's Square doc comment and ParseFEN's own
    rank-8-first-in-FEN-but-rank-0-first-internally handling)."""

    def __init__(self, fen):
        self.piece_at = {}
        self.white_king_square = None

        board_field = fen.split()[0]
        ranks = board_field.split("/")
        if len(ranks) != 8:
            raise ValueError(f"FEN board must have 8 ranks: {fen!r}")

        for i, rank_str in enumerate(ranks):
            rank = 7 - i
            file = 0
            for ch in rank_str:
                if ch.isdigit():
                    file += int(ch)
                    continue
                color = BLACK if ch.islower() else WHITE
                piece_type = FEN_PIECE_TYPE[ch.lower()]
                square = rank * 8 + file
                self.piece_at[square] = (piece_type, color)
                if piece_type == KING_PIECE_TYPE and color == WHITE:
                    self.white_king_square = square
                file += 1
            if file != 8:
                raise ValueError(f"rank {rank_str!r} does not sum to 8 files: {fen!r}")

        if self.white_king_square is None:
            raise ValueError(f"FEN has no white king: {fen!r}")


def active_features(
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
    """Python equivalent of nnue.ActiveFeatures. Returns the list of active
    feature indices for the given position/card-state summary -- order is
    not meaningful (ActiveFeatures's own order depends on Go's map/loop
    iteration and this port's differs), only the resulting SET matters,
    exactly like a multi-hot input vector's nonzero positions."""
    board = Board(fen)
    king_file, king_rank = board.white_king_square % 8, board.white_king_square // 8
    bucket = king_bucket(king_file, king_rank)

    feats = []
    for square, (piece_type, color) in sorted(board.piece_at.items()):
        if piece_type == KING_PIECE_TYPE:
            continue
        feats.append(piece_feature(bucket, square, piece_type, color))

    base = NUM_CHESS_FEATURES
    base = _append_count_bucket(feats, base, white_hand_size)
    base = _append_count_bucket(feats, base, black_hand_size)
    base = _append_count_bucket(feats, base, frozen_white)
    base = _append_count_bucket(feats, base, frozen_black)
    base = _append_count_bucket(feats, base, shielded_white)
    base = _append_count_bucket(feats, base, shielded_black)
    if fortress_white:
        feats.append(base)
    base += 1
    if fortress_black:
        feats.append(base)
    base += 1

    return feats
