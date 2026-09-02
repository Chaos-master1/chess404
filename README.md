# Chess404

Chess404 is a realtime chess-variant platform — chess plus a 37-card power
system (lava, bombs, fog, fusion, freeze, …) — with a server-authoritative Go
backend, a custom chess engine for the computer opponent, and a Next.js web
client, in a pnpm/Turborepo monorepo.

## Repository map

```
apps/web/            Next.js 15 frontend + same-origin /api proxy routes
packages/contracts/  shared TypeScript domain/protocol types
packages/game-core/  shared deterministic rules, card pool, RNG (owns cards.json)
services/realtime/   Go backend (single module)
  cmd/               gateway, match-service, platform-service, matchmaking-service,
                     analysis-worker, replay-worker, migrate + engine tools
                     (404chess-engine, nnue-gauntlet, nnue-selfplay, xgauntlet)
  internal/match/    authoritative match state machine and card resolution
  internal/engine/   computer opponent: v1/ (live in production) and the
                     core|actions|search|nnue|conform rebuild (in progress)
  internal/platform/ accounts, guests, friendships, moderation, notifications
Chess404Mobile/      React Native app (own toolchain, outside the pnpm workspace)
e2e/                 Playwright end-to-end specs (run against production by default)
scripts/             loadtest/, windows/ (PowerShell ops), dev-assets/, Python tooling
docs/                audits/, audit-archive/, launch-audit/, history/
deploy/              Railway Dockerfiles, docker-compose integration stack,
                     Grafana/Loki/Prometheus config
.github/workflows/   CI (go vet/test/build, integration, docker build, web lint/build)
```

## Prerequisites

- Node 20+ and pnpm 9 (`corepack enable`)
- Go 1.25 (Linux: e.g. `~/sdk/go1.25.6/bin`; Windows: `C:\Program Files\Go\bin\go.exe`)
- Docker (integration tests only)
- Playwright browsers for e2e: `pnpm exec playwright install`

## Everyday commands

From the repo root (Turborepo over `apps/*` + `packages/*`):

```bash
pnpm install
pnpm dev        # turbo run dev --parallel
pnpm build      # turbo run build
pnpm lint       # turbo run lint  (web "lint" is a typecheck, not ESLint)
pnpm test
pnpm test:e2e   # Playwright — targets production unless E2E_BASE_URL is set
```

Go backend, from `services/realtime`:

```bash
go build ./...
go vet  ./...
go test ./...
go test ./internal/match/... -run TestName -v
```

On Windows, invoke Go by absolute path: `& "C:\Program Files\Go\bin\go.exe" test ./...`.

Integration tests (Postgres + Redis via Docker; same as CI):

```bash
docker compose -f deploy/docker-compose.integration.yml up -d --wait
export TEST_POSTGRES_URL="postgres://test:test@localhost:5432/chess404_test?sslmode=disable"
export TEST_REDIS_URL="redis://localhost:6379/0"
( cd services/realtime && go test ./internal/integration/... -v -count=1 -timeout 300s -tags=integration )
docker compose -f deploy/docker-compose.integration.yml down -v
```

## Running the full local stack

Windows (PowerShell, each service in its own window):

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\windows\start-local.ps1
# Postgres-backed variant:
powershell -ExecutionPolicy Bypass -File .\scripts\windows\setup-postgres.ps1      # one-time DB + .pg-creds.env
powershell -ExecutionPolicy Bypass -File .\scripts\windows\start-local-postgres.ps1
```

Linux/macOS: start the same services manually — `apps/web` via `pnpm dev`, and
each Go service from `services/realtime` with its env vars (see
`services/realtime/.env.example` and [RUNBOOK.md](./RUNBOOK.md)).

Local URLs: web `http://localhost:3000`, gateway `:8090/healthz`,
match-service `:8082/healthz`, platform-service `:8083/healthz`,
matchmaking-service `:8084/healthz`.

## Deployment

Production runs on Railway: **5 services** (`web`, `gateway`, `match-service`,
`platform-service`, `Postgres`) with Upstash Redis for match-claims and
matchmaking tickets. Matchmaking runs inside the platform-service container
(port 8084); there is no separate matchmaking-service or replay-worker in
production.

- Deploy guide: [DEPLOY_RAILWAY.md](./DEPLOY_RAILWAY.md)
- Rollback procedure: [docs/launch-audit/rollback-procedure.md](./docs/launch-audit/rollback-procedure.md)
- Ops runbook: [RUNBOOK.md](./RUNBOOK.md)

## More documentation

- [ARCHITECTURE.md](./ARCHITECTURE.md) — services, data flow, card mechanics
- [CLAUDE.md](./CLAUDE.md) — agent guidance, production topology, audit journal
- [docs/audits/](./docs/audits/) — current audits (launch audit, NNUE verification)
- [docs/audit-archive/](./docs/audit-archive/) — superseded audit reports (history only)
- CI: [.github/workflows/ci.yml](./.github/workflows/ci.yml) is the canonical definition of what "green" means
