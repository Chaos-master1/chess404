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

**Env-driven backend selection.** Persistence backends (`file` | `sqlite` | `postgres`/`redis`) are chosen per-store via env vars (see `services/realtime/.env.example` and `apps/web/.env.example`), not compile-time flags. When debugging "data didn't persist" issues, check which backend a given service was started with before assuming a code bug.

**Windows-first dev environment.** Scripts (`start-local*.ps1`, `setup-postgres.ps1`, `restart-local.ps1`) are PowerShell, and Go is invoked via an absolute path in most docs/scripts because it isn't reliably on PATH in this environment — match that pattern rather than assuming a Unix shell.


## Important Note 
After major changes, please update this file (@CLUADE.md). Keep this file up-to-date with the project's status.