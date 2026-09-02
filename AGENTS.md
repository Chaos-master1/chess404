## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

When the user types `/graphify`, invoke the `skill` tool with `skill: "graphify"` before doing anything else.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- graphify-out/ is generated output and is gitignored (untracked 2026-09-02); the graph still lives on disk for local queries. Dirty graphify-out/ files are expected after hooks or incremental updates; dirty graph files are not a reason to skip graphify. Only skip graphify if the task is about stale or incorrect graph output, or the user explicitly says not to use it.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).

## Objective
- Build the ultimate 404 Chess engine combining NNUE eval, MCTS for stochastic card decisions, and alpha-beta search — and the realtime platform around it.

## Where things stand (2026-09-02)

- **Live engine (production):** the legacy engine now lives at
  `services/realtime/internal/engine/v1/` (moved 2026-09-02; all importers
  updated). It is NOT a Stockfish wrapper — Stockfish is blind to 404-chess
  modifiers (lava, bombs, fog, fusion, cards).
- **Engine rebuild (in progress, not live):** a ground-up kernel + search at
  `services/realtime/internal/engine/{core,actions,search,conform,nnue}` —
  Phases 1–2 complete (bitboard kernel, card-aware alpha-beta with PIMC
  fair-play search); Phases 3 (real NNUE) and 4 (wire into match-service)
  remain. Authoritative detail: CLAUDE.md → "Engine rebuild".
- **NNUE:** the live v1 engine uses 847 inputs (12×64 pieces + 5 modifiers +
  74 hand features) with 1024×1024 hidden layers — the older "773→512→1" /
  "847→512→1" journal entries are stale on the layer sizes. The REBUILD's NNUE
  (`internal/engine/nnue`) is a different, verified architecture:
  2580→128→32→1, Go and Python trainers in agreement — see
  [docs/audits/2026-09-02-nnue-verification.md](docs/audits/2026-09-02-nnue-verification.md)
  (the old v1-format `nnue_weights.bin` was removed from the repo on
  2026-09-02 — wrong format for both engines, recoverable from git history;
  the rebuild's real weights are `internal/engine/nnue/pytrainer/trained.bin`,
  which the gauntlet now loads by default).
- **Conventions worth knowing:** search score is always white-perspective
  (quiescence negates for the minimizing side); NNUE is used when
  `nnue_weights.bin` exists and falls back to classical eval otherwise.

Do not journal running status here — status narrative lives in CLAUDE.md
(keep it current instead) and historical audits in docs/audits/ and
docs/history/. Commands, production topology, and architecture notes are in
CLAUDE.md / ARCHITECTURE.md / README.md.
