# NNUE / engine — quick verification report

Date: 2026-09-02. No code edits, only reads and one new script (`scripts/debug_independent_encoder.py`).

## TL;DR

| Check | Result |
|---|---|
| Independent Python encoder matches Go golden file on all cases | **PASS** (25/25) |
| `pytrainer/features.py` matches Go golden file on all cases | **PASS** (25/25) |
| Independent encoder and `pytrainer` agree on every case | **PASS** (25/25, all 19 card-state cases) |
| `NumFeatures` in Go / Python trainer / independent | **agree = 2580** |
| `services/realtime/nnue_weights.bin` is loadable by the new `nnue.Load()` | **NO — wrong magic, wrong format** |
| `services/realtime/internal/engine/nnue/pytrainer/trained.bin` is loadable | **YES** (magic `C404NNU2`, 669,388 bytes = exact match for 2580→128→32→1) |
| AGENTS.md / CLAUDE.md "773→512→1" / "847→512→1" claim | **STALE — actual architecture is 2580→128→32→1** |

## What I verified, concretely

### 1. Three independent encoders, one oracle

I wrote `scripts/debug_independent_encoder.py` from the Go source `services/realtime/internal/engine/nnue/features.go` and the oracle `services/realtime/internal/engine/nnue/testdata/golden_features.json` only. I never read `pytrainer/features.py` while writing it.

The script reads the 25 golden cases and re-derives the expected feature set from FEN + card state alone. All 25 match. The 19 cases that exercise card state (hand sizes 0..5 per side, frozen/shielded counts, fortress booleans) all match exactly.

The Python trainer `pytrainer/features.py` also matches all 25 cases. The two Python encoders produce bit-identical output on every case.

This is the strongest evidence possible without a Go toolchain: the encoder has a single, well-specified interpretation that two independent readers arrive at identically.

Run it:
```bash
python3 scripts/debug_independent_encoder.py
```

### 2. `NumFeatures` = 2580, not 773 or 847

`features.go:31-55` defines:
- `numKingBuckets = 4`
- `numPieceSquareFeatures = 64 * 5 * 2 = 640` (5 non-king piece types, 2 colors)
- `numChessFeatures = 4 * 640 = 2560`
- `numCountBuckets = 3` for {0, 1, 2-or-more}
- `numCardCountFeatures = 3 * 6 = 18` (white/black × {hand, frozen, shielded})
- `numCardBoolFeatures = 2` (fortress white, fortress black)
- `numCardFeatures = 20`
- **`NumFeatures = 2560 + 20 = 2580`**

`pytrainer/features.py:17-28` has the same constants. The `pytrainer/trained.bin` weight file is **669,388 bytes**, which decodes to `8 (magic) + 2580·128·2 + 128·4 + 128·32·2 + 32·4 + 32·2 + 4` exactly.

The `Network` shape in `network.go:33-44` is `NumFeatures → AccumulatorSize=128 → Hidden2Size=32 → 1`, with `WeightScale=64` and `ClipMax=127·WeightScale`. This is a 2-hidden-layer clipped-ReLU network with a single shared quantization scale.

`AGENTS.md` says "NNUE 773→512→1 used when `nnue_weights.bin` exists" and "Card-aware NNUE: architecture bumped from 773→847 inputs". `CLAUDE.md` likely has the same. These numbers are wrong. Either:
- They are stale notes from an earlier architecture that was never updated, or
- The trainer was retrained on the actual 2580-input architecture and the notes were forgotten.

The actual code, the actual weight file, and the actual pytrainer all agree on 2580→128→32→1. The docs are out of date.

### 3. The shipped `nnue_weights.bin` is not loadable by the new engine

`services/realtime/nnue_weights.bin`:
- 7,675,920 bytes, dated 2026-07-29
- First 8 bytes: `O\x03\x00\x00\x00\x04\x00\x00`
- Expected magic for the new `nnue.Network`: `C404NNU2` (literal bytes 0x43 0x34 0x30 0x34 0x4E 0x4E 0x55 0x32)

The new `network.go:188-189` explicitly rejects this:
```go
if string(header) != magicHeader {
    return nil, fmt.Errorf("unrecognized weights file header %q (expected %q -- this is not a Phase 3 nnue package weights file)", header, magicHeader)
}
```

The new package's comment (`network.go:147-149`) even says this file is from a different architecture:
> distinct from the old engine's nnue_weights.bin, which is a completely different architecture (a from-scratch float32 MLP, see internal/engine/v1/nnue.go)

And `engine/v1/nnue.go:64-77` calls that out even more bluntly:
> The shipped nnue_weights.bin does not evaluate positions correctly: the trainer encodes the board with a8 as square 0 (scripts/train_nnue.py) while this package encodes a1 as square 0, so the network is queried on the vertical mirror of what it was fitted to; the trainer fits plain ReLU while inference below applies leaky ReLU with slope 0.1; the 5 modifier inputs are never set during training but are set here; and self-play deals hands without ever playing a card, so the 74 card features were fit against a target they cannot influence. The observable result is that the symmetric starting position scores about -322cp and removing a black pawn makes White's score worse.

So `v1/nnue.go` knows its own weights file is broken and gates it behind `CHESS404_ENGINE_NNUE=1` (default off). The new `engine/nnue/Load()` will simply reject the file. The *new* engine's weights file lives at `services/realtime/internal/engine/nnue/pytrainer/trained.bin` (669 KB, correct magic, correct size).

The repo-root `nnue_weights.bin` is a 7.6 MB vestigial artifact that no current code path will accept. The most likely consumer in any "load the NNUE" path is the gauntlet (`cmd/nnue-gauntlet/main.go:8-25`) which requires the user to pass `-weights` explicitly. So nothing in the live engine accidentally tries to load it — but the file's presence is misleading, and any future code that "just opens nnue_weights.bin" will silently get an error.

## Recommended next step (small, reversible, valuable)

The user asked what to do next. Given:
- The new engine's encoder is verified to be correct (all three implementations agree).
- The new engine's weights file is in the right place with the right magic.
- The repo-root `nnue_weights.bin` is a 7.6 MB vestigial file from a different architecture that no active code path uses.

The single highest-leverage action I would do next is **a one-line addition to the new engine's `Load()` path so the engine picks the correct weights file automatically**, with the search package wiring it up. Concretely, this would mean:
1. The gauntlet and any future code that wants the trained new network has a clear, single discovery path.
2. The old 7.6 MB file can be moved out of the repo root to remove the foot-gun.

But this is a *Go* code change, and there's no Go toolchain installed in this environment. I can't compile or test it here. The right move is for you to either:
- Install Go 1.25 and let me make the change, or
- Tell me to make the textual edit blind, knowing I can't `go test ./...` afterward.

The two-line code change I'd propose (when a Go toolchain is available):

```go
// in cmd/nnue-gauntlet/main.go, change
weightsPath := flag.String("weights", "", "path to a trained nnue.Network weights file (required)")
// to also check the default pytrainer path:
defaultPath := "internal/engine/nnue/pytrainer/trained.bin"
if _, err := os.Stat(defaultPath); err == nil {
    *weightsPath = defaultPath
}
```

And add a `services/realtime/.gitignore`-style entry to stop tracking the vestigial root-level `nnue_weights.bin` (or move it to `engine/v1/nnue_weights.bin` and have v1's `Load` open it from that path). Neither is urgent, and both are reversible.

## Files this report touched

- `scripts/debug_independent_encoder.py` (new, 178 lines, pure-Python verification harness)

Nothing else was modified. The 16 dirty files in `git status` are unchanged.
