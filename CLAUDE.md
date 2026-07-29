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

Integration tests (require Docker; matches CI in `.github/workflows/ci.yml`):

```powershell
docker compose -f deploy/docker-compose.integration.yml up -d --wait
& "C:\Program Files\Go\bin\go.exe" test ./internal/integration/... -v -count=1 -timeout 300s -tags=integration
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

**Go service layout** (`services/realtime/internal/`): `match/` (state machine, card resolution, snapshots, Redis/memory stores), `engine/` (custom chess engine — eval, alpha-beta search, NNUE, opening book, 5 computer difficulty levels), `platform/` (accounts, guests, friendships, moderation, notifications — each with `file`/`sqlite`/`postgres` backend variants selected by env var, e.g. `GUEST_STORE_BACKEND`), `matchmaking/` (queue/ticketing), `anticheat/` (replay-based cheat detection, Stockfish cross-check), `httputil`/`rate_limit`/`logging`/`metrics` (cross-cutting). Most `internal/platform/*_postgres.go` / `*_sqlite.go` files have a matching `_test.go` — check both when changing a store interface.

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

**Known remaining gaps (from a full pre-launch audit, not yet addressed — see plan history for full detail):** both players' seat secrets are serialized in every snapshot/API response with no `json:"-"`; WebSocket `Subscribe` doesn't verify the player secret; `join` lets an unauthenticated caller rewrite seat identity/account binding; API responses send a 10-minute public `Cache-Control`; frontend logs request headers (incl. session tokens) to the console; email delivery defaults to a `preview` provider that only logs reset tokens (no real email sent); `/authoritative` dev harness is publicly reachable; mobile is largely unusable (7 pages have no nav entry point, no responsive reflow); `railway.json`'s per-service healthchecks/restart-policy/volumes/cron are silently ignored by Railway's actual schema. Treat this list as a starting point, not exhaustive — re-audit before a real public launch.

## Important Note
After major changes, please update this file (@CLAUDE.md). Keep this file up-to-date with the project's status.