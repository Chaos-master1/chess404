# Phase 3 NNUE diagnosis — 2026-09-06

Why the trained network loses to the placeholder eval, and what the pipeline
verified before that conclusion could be trusted. Companion to
[2026-09-02-nnue-verification.md](2026-09-02-nnue-verification.md).

## Baselines recorded

| Network | Data trained on | Gauntlet vs placeholder (150 ms/move) | Score |
|---|---|---|---|
| `trained.bin` (Aug 9) | partial Aug-9 dataset | 0 W / 0 D / 20 L (2026-09-02, seed 7) | 0% · Elo ≈ −1600 |
| `trained-v2.bin` (this session) | full 147k ×2 mirrored | 1 W / 1 D / 38 L (seed 11) | 3.75% · Elo ≈ −564 |

`trained-v2.bin` is kept on disk (not committed) as the v2 baseline; the
committed default weights remain `pytrainer/trained.bin`.

## What was ruled out (each with its check)

1. **Feature encoder** — verified 2026-09-02: three independent
   implementations agree on all 25 golden cases. Unchanged since.
2. **Weight file format / loading** — magic + size verified; gauntlet and
   tests load it.
3. **Sign convention** — new probe test
   `internal/engine/search/nnue_scale_test.go` pins the white-perspective
   sign on constructed positions: start ≈ 0, white-up-a-queen positive,
   black-up-a-queen negative. Both v1 and v2 pass. The historical white-bias
   bug described in train.py's mirror docstring is fixed.
4. **Go quantized inference vs Python float math** — replicated Go's integer
   forward pass in Python: it tracks the float model within quantization
   noise (start −31 vs +7, white+Q 354 vs 383, black+Q −114 vs −223). The
   gap is in the MODEL, not the inference.
5. **Training data quality** — labels span [−600, +600], track material
   monotonically (mat +9 → mean +511, mat −9 → −552, Pearson r = 0.58), and
   the color-mirror augmentation provably commutes with the feature encoder
   (`active_features(mirror(fen)) == active_features(swapped fen)`), and the
   encoder is side-to-move-independent (so mirror_record's forced ` w ` is
   harmless).

## Root cause

The Aug-9 dataset is **147k positions from depth-2 self-play**. Three
consequences, all confirmed by measurement:

- The label noise floor is high: outcomes of shallow games are near-random
  conditional on the encoded features (which omit side-to-move, castling and
  exact piece squares beyond king buckets). Training plateaus at MSE ≈ 56k
  (RMSE ≈ 237 on ±600 labels) — the model learned what is learnable, which
  is not much.
- The material gradient the model extracts is too weak to drive search: a
  full queen moves the eval by only ~150 cp (v1) / ~380 cp (v2) against the
  placeholder's 900, and the v2 model is still color-asymmetric (−127 for
  black) despite mirrored data — weak features let optimizer noise shape the
  function.
- Deep-search positions are out of distribution: the gauntlet searches to
  depth ~32 with iterative deepening; the data never contains such lines, so
  leaf evaluations there are noise.

Retraining on the same data cannot fix this — v2's improvement (0% → 3.75%)
came from using the full dataset, but the data itself is the bottleneck.

## Next step (started 2026-09-06)

Fresh self-play from a deeper decider, then retrain and re-gauntlet. Fixed
depth 6 proved impractically slow (PIMC hand-sampling multiplies the search;
not one game finished in 12 minutes), so the run uses a time budget instead —
5× the old data's 200 ms/move, ~2-4 min/game, started 2026-09-06:

```bash
go run ./cmd/nnue-selfplay -games 120 -ms-per-move 1000 -plies 140 -seed 202 \
  -out internal/engine/nnue/pytrainer/selfplay_1s.jsonl
python3 train.py selfplay_1s.jsonl trained-v3.bin
NNUE_WEIGHTS_PATH=../nnue/pytrainer/trained-v3.bin \
  go test ./internal/engine/search/ -run TestNNUEEvalScaleAndSign -v
go run ./cmd/nnue-gauntlet -weights internal/engine/nnue/pytrainer/trained-v3.bin -games 40
```

Success bar: v3 passes the scale/sign probe with a queen worth ≥ 500 and
scores > 50% vs the placeholder; only then does it replace
`pytrainer/trained.bin` as the committed default.
