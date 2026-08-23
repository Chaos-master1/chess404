# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Chess404 is a realtime chess-variant platform (chess + a 37-card power system) with a server-authoritative Go backend and a Next.js web client, in a pnpm/Turborepo monorepo.

- `apps/web` — Next.js 15 (App Router) frontend and same-origin API proxy routes
- `services/realtime` — Go backend: `gateway`, `match-service`, `platform-service`, `matchmaking-service`, `replay-worker`, `analysis-worker`, `migrate`
- `packages/contracts` — shared TypeScript domain/protocol types
- `packages/game-core` — shared deterministic rules, card pool, RNG (source of truth for `cards.json`)
- `Chess404Mobile` — separate React Native app (own toolchain, not part of the pnpm workspace)
- `deploy/` — Railway Dockerfiles, Grafana/Loki/Prometheus config, docker-compose for integration tests
- Root `client/` is a legacy, gitignored build artifact directory — not part of the active source tree; ignore it.

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full service/data-flow diagram and card mechanic list. `PROJECT_STATUS.md` and `services/realtime/README.md` are running narrative changelogs kept by prior sessions — they accumulate fast and go stale quickly; prefer reading the actual code over trusting their specifics.

## Commands

Root (Turborepo, runs across `apps/*` and `packages/*`):

```powershell
pnpm install
pnpm dev              # turbo run dev --parallel
pnpm build            # turbo run build
pnpm lint             # turbo run lint
pnpm test             # turbo run test
```

Per-package (useful for iterating on just one thing):

```powershell
pnpm --filter @chess404/web dev
pnpm --filter @chess404/web run lint     # runs `next typegen` + `tsc --noEmit`, NOT eslint
pnpm --filter @chess404/game-core run lint
pnpm --filter @chess404/contracts run lint
```

There is no ESLint config for the web app — its "lint" is a TypeScript typecheck (`apps/web/scripts/lint-types.mjs`). `game-core`/`contracts` "lint" and "test" scripts are also just `tsc --noEmit` — there is no unit test suite in the TS packages despite `vitest` being a listed devDependency.

Go backend (from `services/realtime`; Go is not always on PATH — call the binary directly if `go` is unrecognized):

```powershell
& "C:\Program Files\Go\bin\go.exe" test ./...
& "C:\Program Files\Go\bin\go.exe" test ./internal/match/... -run TestName -v
& "C:\Program Files\Go\bin\go.exe" vet ./...
& "C:\Program Files\Go\bin\go.exe" build ./...
```

Integration tests (require Docker; matches CI in `.github/workflows/ci.yml`). Run from the repo root, except the `go test` line itself, which needs `services/realtime` (package paths are relative to its `go.mod`):

```powershell
docker compose -f deploy/docker-compose.integration.yml up -d --wait
$env:TEST_POSTGRES_URL = "postgres://test:test@localhost:5432/chess404_test?sslmode=disable"
$env:TEST_REDIS_URL = "redis://localhost:6379/0"
Push-Location services/realtime
& "C:\Program Files\Go\bin\go.exe" test ./internal/integration/... -v -count=1 -timeout 300s -tags=integration
Pop-Location
docker compose -f deploy/docker-compose.integration.yml down -v
```

Run the whole local stack (4 Go services + web, each in its own PowerShell window):

```powershell
powershell -ExecutionPolicy Bypass -File .\start-local.ps1
```

Same, but with Postgres-backed persistence instead of file-backed:

```powershell
powershell -ExecutionPolicy Bypass -File .\setup-postgres.ps1      # one-time DB + .pg-creds.env
powershell -ExecutionPolicy Bypass -File .\start-local-postgres.ps1
```

Local URLs: web `http://localhost:3000`, gateway `:8090/healthz`, match-service `:8082/healthz`, platform-service `:8083/healthz`, matchmaking-service `:8084/healthz`.

## Architecture notes worth knowing before editing

**Server-authoritative, thin client.** All game rules, the 37 card effects, and clock/timeout management are enforced in `services/realtime/internal/match`. The frontend renders backend snapshots and sends intents (`make_move`, `play_card`, `select_target`, `send_chat`, `offer_draw`, `respond_draw`, `resign`) — treat client-side board/card logic in `apps/web/src` as presentation, not rules authority, when deciding where a gameplay fix belongs.

**`apps/web/app/*/page.tsx` are thin route shims, not real pages.** The actual UI is a single client-only shell: `apps/web/app/ClientApp.tsx` dynamically imports `apps/web/src/App.tsx` with `ssr: false` (avoids hydration mismatches from `localStorage`/WebSocket use at init). Most `app/<route>/page.tsx` files just call `platform.setActivePage(...)` and render `null` — the corresponding `apps/web/src/<Name>Page.tsx` component under the `PlatformContext`/App shell is what actually renders. When asked to change a page's behavior, go to the `src/*Page.tsx` component, not the route file, unless the task is routing/metadata itself.

**`cards.json` has two copies, one source of truth.** `packages/game-core/src/cards.json` is authoritative; `services/realtime/internal/match/cards.json` is a generated copy. After editing card data, run:

```powershell
node packages/game-core/scripts/sync-cards-json.mjs
```

**Go service layout** (`services/realtime/internal/`): `match/` (state machine, card resolution, snapshots, Redis/memory stores), `engine/` (the LIVE custom chess engine actually used in production today — eval, alpha-beta search, NNUE, opening book, 5 computer difficulty levels), `engine/core` (a NEW, separate bitboard rules kernel — Phase 1 of an in-progress engine rebuild, NOT yet wired into any running service; see "Engine rebuild" below), `engine/conform` (differential-fuzzing conformance harness comparing `engine/core` against `internal/match`), `platform/` (accounts, guests, friendships, moderation, notifications — each with `file`/`sqlite`/`postgres` backend variants selected by env var, e.g. `GUEST_STORE_BACKEND`), `matchmaking/` (queue/ticketing), `anticheat/` (replay-based cheat detection, Stockfish cross-check), `httputil`/`rate_limit`/`logging`/`metrics` (cross-cutting). Most `internal/platform/*_postgres.go` / `*_sqlite.go` files have a matching `_test.go` — check both when changing a store interface.

**Env-driven backend selection.** Persistence backends (`file` | `sqlite` | `postgres`/`redis`) are chosen per-store via env vars (see `services/realtime/.env.example` and `apps/web/.env.example`), not compile-time flags. When debugging "data didn't persist" issues, check which backend a given service was started with before assuming a code bug. **Production is Redis-backed** (Upstash, free tier — `db_request_limit` is a real monthly cap that has been hit before; avoid adding unconditional per-request/per-poll Redis traffic).

**Windows-first dev environment.** Scripts (`start-local*.ps1`, `setup-postgres.ps1`, `restart-local.ps1`) are PowerShell, and Go is invoked via an absolute path in most docs/scripts because it isn't reliably on PATH in this environment — match that pattern rather than assuming a Unix shell.

## Current status (2026-07-29)

Production: `web-production-ddc27.up.railway.app`, Railway project `ample-vitality`, 7 services, all deploy `SUCCESS`. Live-verified working end to end (real move round-trip, computer auto-reply, clock ticking, no stale-state loop) as of this date.

**Fixed and live-verified this pass — do not re-diagnose these from scratch, but do re-verify if symptoms recur:**
- `matchMap` self-deadlock: `gcFinishedMatches`/`collectAndBroadcast` used to call `Delete` (write lock) from inside `Range` (read lock held across the callback) on the same shard `RWMutex`, permanently wedging that shard ~30 min after the first finished/waiting match — this was the root cause of a slow (10s+) bootstrap and `authoritative: false`. Fixed in `internal/match/state.go` (`gcFinishedMatches`, `processMatchBroadcast`/`collectAndBroadcast`): collect IDs/containers during `Range`, mutate after it returns.
- Computer opponent got stuck forever: engine-built intents had no `PlayerID`, and an unresolvable card target left a dangling `PendingCard`. Fixed in `internal/match/match_lifecycle.go` (computer seat gets its own secret + stamped identity) and a guaranteed-progress fallback (`ensureComputerMadeProgressLocked` / `chess.go:firstLegalMoveForColor`).
- Brand-new match claims returned an empty seat secret (`main.go` was encoding the raw claim instead of `.IssuedView()`), and the gateway preferred a single-use claim token over an already-known secret — both broke the second player's first presence/intent call.
- Redis connection pool corruption in production (`Conn has unread data, removing it`, continuous): all 6 `redis.NewClient` construction sites used go-redis's 3s default timeout against a remote Upstash instance. Fixed with explicit 10s Dial/Read/Write timeouts.
- Redis volume: match-claims `persist()` rewrote the *entire* claims hash on every single write; client polling fired every 5s unconditionally even with a healthy WebSocket. Both fixed (`saveOne`/`deleteMany`; poll now skips while `wsConnectedRef` is true). Upstash free tier was hit anyway (usage already logged before the fix landed) — a new Upstash account/database was provisioned and wired into all 4 Railway services; the same monthly cap still applies going forward, so keep an eye on it.
- Public Watch feed showed private invites, vs-computer games, and aborted games — `IsPublicReplayableMatch`/`IsPublicLiveSpectateMatch` (`internal/platform/history_public.go`) now exclude `queue=="direct"`, `modeId=="computer"`, `finishReason=="abort"`.
- Stale-client-state retry loop: a move rejected with 409 (`client state is stale`) never refreshed the client's cached `expectedSeqNum` on failure, so a client that fell behind once (e.g. missed a WS message) retried the same stale value forever — froze both move submission and the clock display (clock is server-driven-only, so no successful move = no new broadcast = frozen display). Fixed in `apps/web/src/lib/match-service.ts` (`applyIntent` refetches and updates the seq cache on any failure) and `useMatchEngineFacade.tsx` (visibly resyncs via `applyAuthoritativeSnapshot`). Verified live in production by forcing a synthetic 409 via a monkey-patched `fetch` and confirming the automatic background resync fires and the match stays playable afterward.
- **Computer opponent "push push push" bug (the original live bug report that triggered the engine rebuild below):** on Medium/Hard/Expert, the computer would permanently degrade to mechanically pushing/capturing pawns file-by-file (a-file first) with zero other move types, for the rest of the game — Easy/Beginner escaped it probabilistically due to their randomness gate, which is why the report said it was difficulty-specific. Root cause: 17 of 37 card mechanics had broken/absent target-selection logic in `internal/engine/opponent.go`; a card only leaves hand on successful resolution and a failed card is abandoned but never removed, so the same broken card got re-selected forever once chosen — permanently starving the real chess search and leaving only the guaranteed-progress fallback's board-scan (`firstLegalMoveForColor`), which explains the exact a-file-first pattern. Fixed with a `computerPlayableMechanics` allowlist + `filterHandForComputer` in `opponent.go` so the computer's card-play decision only ever considers mechanics with working target-selection. Live-verified end to end on 2026-07-29: played 10 full rounds (20 plies) against Computer Expert in production, including the computer drawing and fully resolving a RARE Double-Move card (both halves executed, `Bxb2+` then `Ba3`, engine list confirms) and returning to normal varied chess play (knight development, a genuine `Nd3+` fork, castling, pawn moves) afterward — no degenerate pattern at any point.

**Engine rebuild — Phases 1 and 2 complete, not yet live:** a ground-up rewrite of the computer opponent's engine is in progress at `services/realtime/internal/engine/{core,conform,actions,search}` — a NEW, separate codebase, not a modification of the live `internal/engine` used in production today. **Nothing outside these packages imports this code yet** — it has zero effect on production, and the live computer opponent is still the old `internal/engine`.

Phase 1 (kernel): bitboards, magic sliders, make/unmake, full Zobrist hashing, card overlay planes for Frozen/Shielded/FusedWith/Fortress/Lava/Bomb/BlackHole. Perft-verified to depth 6 against standard reference values; `engine/conform`'s differential fuzzing shows zero mismatches against `internal/match` across the full ~1,392-candidate brute force at the standard opening, hand-built Frozen/Fortress/FusedWith scenarios, and multiple 40-ply random-walk games. Fog is deliberately unmodeled (confirmed zero rules effect in `internal/match`).

Phase 2 (combined search): `engine/actions` (unified Action type covering both chess moves and card plays — a representative subset of 7 mechanics: Freeze/Shield/Fortress/Lavaground/Unabomber/BlackHole/HalfFuse+FullFusion, not all 37; the other 30 are unmodeled by design, same posture as Fog) and `engine/search` (negamax/alpha-beta with cards as first-class tree nodes, a transposition table with correct exact/lower/upper bound handling, and PIMC fair-play search that samples a plausible opponent hand from the real rarity-weighted 37-card pool rather than ever reading the actual hidden hand). The card-tactics suite (`engine/search/cardtactics_test.go` + `search_test.go`) proves the headline capability directly: the search finds Freeze-then-capture, Shield-saves-a-hanging-piece, and Fusion-enables-an-otherwise-impossible-capture, and a direct empirical test confirms the CURRENT PRODUCTION engine (`internal/engine/opponent.go`) cannot find the same Freeze+capture coordination even though it can mechanically play the card, because it scores cards and moves on independent scales and never re-examines the board after deciding to play a card. Two real bugs were caught and fixed while building this: card actions were silently double-negated in the search (freezing pieces backwards), and a card effect (Fusion) can rarely open a fresh attacking line onto the enemy king mid-turn, which the engine now handles as a decisive result instead of crashing.

Remaining phases (per the plan file used to drive this work): Phase 3 (real quantized NNUE trained with actual card play, replacing Phase 2's placeholder material+overlay eval), Phase 4 (extract as its own scaled service, wire into match-service, live deploy). Pending-card/double-move turn-sequencing state remains unmodeled (the turn model is deliberately simplified to at-most-one-card-plus-one-move, tighter than internal/match's actual unbounded `card* move` structure).

**Known remaining gaps (checked against live production/Railway on 2026-07-29 — some items an earlier audit flagged turned out to already be fixed or already live; this list reflects what's actually still true):**
- Both players' seat secrets are still serialized in `contracts.MatchState` JSON with no `json:"-"` (`internal/contracts/contracts.go`) — verify current redaction coverage before trusting any snapshot response is safe to expose.
- WebSocket `Subscribe`, the `join` intent's seat-identity rewrite, response `Cache-Control` headers, the `/authoritative` dev harness, and client-side game-over/local-move fallbacks were all flagged by the same audit — status of each needs re-checking against current code rather than assumed from the original audit's line numbers, which have drifted.
- Email delivery still defaults to a `preview` provider (logs the reset link instead of sending real email).
- Mobile is largely unusable (no nav entry point for several pages, no responsive reflow).
- **No database backups exist.** `deploy/postgres-backup.sh` works standalone but nothing schedules it, and without `AWS_S3_BUCKET` set it would write to a Railway container's ephemeral disk — a backup that vanishes on redeploy. Railway's own Postgres **Backups tab → Enable PITR** (pgBackRest, ~4-week restore window) is the right fix and needs no extra credentials, but it's a dashboard action with a real (small) billing cost — a human decision, not something to enable autonomously. See `deploy/railway/reference-config.json` for full detail.
- Repo-root `railway.json` was deleted (2026-07-29) — it used a `services`/`cron`/`volumes` shape Railway's actual config-as-code schema doesn't support (`build`/`deploy`/`environments` only, single-service), so it was silently ignored in full. The healthcheck/restart-policy/replica settings it described **are** live — verified directly against each service's Railway config, applied via the dashboard/API, not this file. What it also referenced but is genuinely NOT live: a `Sentry` service (doesn't exist) and the backup cron above. See `deploy/railway/reference-config.json` for the current human-readable record.

Treat this list as a starting point, not exhaustive — re-audit before a real public launch.

## Important Note
After major changes, please update this file (@CLAUDE.md). Keep this file up-to-date with the project's status.