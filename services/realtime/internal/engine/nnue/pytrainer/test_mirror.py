"""Sanity checks for train.py's color-mirror data augmentation: mirroring
twice must return to the original (up to the dummy trailing FEN fields,
which active_features never reads), and a known position's mirror must
match hand computation.
"""
from features import active_features
from train import mirror_fen_board, mirror_record


def test_mirror_fen_board_is_an_involution():
    starting = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR"
    once = mirror_fen_board(starting)
    twice = mirror_fen_board(once)
    assert twice == starting, f"expected double-mirror to return the original, got {twice!r}"


def test_mirror_fen_board_known_position():
    # White king e1, White rook a1; Black king e8. Mirroring should
    # produce: Black king e8->e1 swapped to white... wait, let's reason
    # directly: mirror flips rank (r -> 7-r) and swaps color. White King
    # on e1 (rank index 0 from the bottom) moves to rank index 7 (e8) and
    # becomes black. Black King on e8 moves to e1 and becomes white.
    fen = "4k3/8/8/8/8/8/8/R3K3"
    mirrored = mirror_fen_board(fen)
    assert mirrored == "r3k3/8/8/8/8/8/8/4K3", mirrored


def test_mirror_record_swaps_counts_and_negates_label():
    r = {
        "fen": "4k3/8/8/8/8/8/8/R3K3 w - - 0 1",
        "whiteHandSize": 3,
        "blackHandSize": 1,
        "frozenWhite": 2,
        "frozenBlack": 0,
        "shieldedWhite": 0,
        "shieldedBlack": 1,
        "fortressWhite": True,
        "fortressBlack": False,
        "label": 250.0,
    }
    m = mirror_record(r)
    assert m["whiteHandSize"] == 1 and m["blackHandSize"] == 3
    assert m["frozenWhite"] == 0 and m["frozenBlack"] == 2
    assert m["shieldedWhite"] == 1 and m["shieldedBlack"] == 0
    assert m["fortressWhite"] is False and m["fortressBlack"] is True
    assert m["label"] == -250.0


def test_mirrored_record_produces_color_swapped_features():
    # A single white rook, far from any king-bucket-affecting square,
    # should become a single BLACK rook (at the rank-flipped square) in
    # the mirrored record's features.
    r = {
        "fen": "4k3/8/8/8/8/8/8/4K2R w - - 0 1",  # white king e1, white rook h1
        "whiteHandSize": 0, "blackHandSize": 0,
        "frozenWhite": 0, "frozenBlack": 0,
        "shieldedWhite": 0, "shieldedBlack": 0,
        "fortressWhite": False, "fortressBlack": False,
        "label": 0.0,
    }
    m = mirror_record(r)
    mirrored_feats = active_features(
        m["fen"], m["whiteHandSize"], m["blackHandSize"],
        m["frozenWhite"], m["frozenBlack"], m["shieldedWhite"], m["shieldedBlack"],
        m["fortressWhite"], m["fortressBlack"],
    )
    # The original white king (e1) and white rook (h1) swap color AND rank
    # together (h1 -> h8, as a BLACK rook); the original black king (e8)
    # becomes the new WHITE king, landing on e1 (not e8) -- e1/e8 swap
    # roles along with everything else on their respective ranks.
    from features import PIECE_TYPE_INDEX, BLACK, king_bucket
    h8_square = 7 * 8 + 7
    king_file, king_rank = 4, 0  # white king now on e1
    bucket = king_bucket(king_file, king_rank)
    expected_rook_feature = bucket * 640 + (h8_square * 5 * 2 + PIECE_TYPE_INDEX[4] * 2 + BLACK)
    assert expected_rook_feature in mirrored_feats, (bucket, mirrored_feats)


if __name__ == "__main__":
    test_mirror_fen_board_is_an_involution()
    test_mirror_fen_board_known_position()
    test_mirror_record_swaps_counts_and_negates_label()
    test_mirrored_record_produces_color_swapped_features()
    print("OK: mirror augmentation sanity checks passed")
