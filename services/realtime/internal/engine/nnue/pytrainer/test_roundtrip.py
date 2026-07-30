"""Task 8's namesake check: verifies this Python feature encoder
(features.py) produces the EXACT SAME active-feature set as the Go
implementation (../features.go) for every case in
../testdata/golden_features.json.

That file is generated fresh from live Go code by
TestGenerateNNUEFeatureFixtures (../fixtures_test.go) every time `go test`
runs, so a mismatch here means Python's port has drifted from Go, not that
the fixture is stale.

Runnable standalone (no pytest dependency required):

    python test_roundtrip.py

Also discoverable by pytest if installed, since it's plain assert-based
test functions.
"""
import json
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).parent))
from features import active_features  # noqa: E402

GOLDEN_PATH = pathlib.Path(__file__).parent.parent / "testdata" / "golden_features.json"


def _load_cases():
    if not GOLDEN_PATH.exists():
        raise FileNotFoundError(
            f"{GOLDEN_PATH} does not exist -- run `go test ./internal/engine/nnue/...` "
            "first to regenerate it from the current Go implementation"
        )
    return json.loads(GOLDEN_PATH.read_text())


def test_golden_features_round_trip():
    cases = _load_cases()
    assert cases, "expected at least one golden case"

    for case in cases:
        got = active_features(
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
        want = case["features"]

        assert len(got) == len(set(got)), f"case {case['name']!r}: Python produced duplicate feature indices"
        assert set(got) == set(want), (
            f"case {case['name']!r}: feature mismatch\n"
            f"  Go:          {sorted(want)}\n"
            f"  Python:      {sorted(got)}\n"
            f"  Go-only:     {sorted(set(want) - set(got))}\n"
            f"  Python-only: {sorted(set(got) - set(want))}"
        )


if __name__ == "__main__":
    cases = _load_cases()
    test_golden_features_round_trip()
    print(f"OK: round-trip verified for all {len(cases)} cases in {GOLDEN_PATH}")
