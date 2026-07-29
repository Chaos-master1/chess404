## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

When the user types `/graphify`, invoke the `skill` tool with `skill: "graphify"` before doing anything else.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- Dirty graphify-out/ files are expected after hooks or incremental updates; dirty graph files are not a reason to skip graphify. Only skip graphify if the task is about stale or incorrect graph output, or the user explicitly says not to use it.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).

## Objective
- Build the ultimate 404 Chess engine combining NNUE eval (trained via classical eval targets), MCTS for stochastic card decisions, and alpha-beta search.

## Key Context
- Custom engine in `services/realtime/internal/engine/` — NOT a Stockfish wrapper.
- Stockfish is blind to 404-chess modifiers (lava, bombs, fog, fusion, cards).
- Search score is always white-perspective (quiescence negates for minimizing side).
- NNUE 773→512→1 used when `nnue_weights.bin` exists; falls back to classical eval.
- Starting position eval: 71 cp (vs 933/−515 before 50K random-position training).

## Work State
### Completed
- **Card-aware NNUE**: architecture bumped from 773→847 inputs (+74 hand card features, 37 per player). `encodeHand()` in nnue.go maps mechanic names to binary feature vector. Python training script matches Go ordering exactly. Passed through EvaluateWithModifiers → Evaluate → Search → MCTS → CardEvaluator's evalDiff.
- **50K positions with random hands** generated via `--gen-positions 50000`, retrained 847→512→1. Val loss 0.0862 (vs 0.1133 for 773-input). All tests pass.
- **NNUE works end-to-end**: 50K random positions (mean 0.1, std 61.3) trained with Huber loss, early stopping (epoch 16). Starting eval: 104 cp.
- **`--gen-positions N`**: generates random positions with classical eval + random hands.
- **Eval refactored**: `baseEval` + `modifierScore` split; `EvaluateWithModifiers` uses NNUE base + classical modifiers (bomb/lava/fortress).
- **Futility pruning fixed**: `i > 0 &&` guard; all debug prints removed.
- **MCTS**: PUCT, 800+ sims, NNUE leaf eval, card-aware decision. `--mcts`, `--mcts-sims`.
- **Self-play**: raw search scores (no TD targets). Pipeline fixed (div-by-zero, depth param).
- **All tests pass** including bomb/lava/fortress modifiers with NNUE loaded, NNUE loaded at 847 inputs.

### Next Move
1. Self-play (`--selfplay 200` at depth 4+) to generate game data where hand features actually correlate with outcomes.
2. Retrain NNUE on game data (mix with classical eval data).
3. Repeat: deeper self-play → better NNUE → card-aware evaluation.
4. Build live thinking dashboard (WebSocket + React).
5. Add Lazy SMP parallel search.
