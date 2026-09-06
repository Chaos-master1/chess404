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

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full service/data-flow diagram and card mechanic list. `docs/history/PROJECT_STATUS.md` and `services/realtime/README.md` are running narrative changelogs kept by prior sessions — they accumulate fast and go stale quickly; prefer reading the actual code over trusting their specifics.

## Commands

Root (Turborepo, runs across `apps/*` and `packages/*`):

```bash
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

Go backend (from `services/realtime`; Go is not always on PATH — on this Linux machine it lives at `~/sdk/go1.25.6/bin`, on Windows call `& "C:\Program Files\Go\bin\go.exe"` directly):

```bash
go test ./...
go test ./internal/match/... -run TestName -v
go vet ./...
go build ./...
```

Integration tests (require Docker; matches CI in `.github/workflows/ci.yml`). Run from the repo root, except the `go test` line itself, which needs `services/realtime` (package paths are relative to its `go.mod`):

```bash
docker compose -f deploy/docker-compose.integration.yml up -d --wait
export TEST_POSTGRES_URL="postgres://test:test@localhost:5432/chess404_test?sslmode=disable"
export TEST_REDIS_URL="redis://localhost:6379/0"
( cd services/realtime && go test ./internal/integration/... -v -count=1 -timeout 300s -tags=integration )
docker compose -f deploy/docker-compose.integration.yml down -v
```

Run the whole local stack on Windows (4 Go services + web, each in its own PowerShell window; scripts live in `scripts/windows/`):

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\windows\start-local.ps1
```

Same, but with Postgres-backed persistence instead of file-backed:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\windows\setup-postgres.ps1      # one-time DB + .pg-creds.env
powershell -ExecutionPolicy Bypass -File .\scripts\windows\start-local-postgres.ps1
```

Local URLs: web `http://localhost:3000`, gateway `:8090/healthz`, match-service `:8082/healthz`, platform-service `:8083/healthz`, matchmaking-service `:8084/healthz`.

## Architecture notes worth knowing before editing

**Server-authoritative, thin client.** All game rules, the 37 card effects, and clock/timeout management are enforced in `services/realtime/internal/match`. The frontend renders backend snapshots and sends intents (`make_move`, `play_card`, `select_target`, `send_chat`, `offer_draw`, `respond_draw`, `resign`) — treat client-side board/card logic in `apps/web/src` as presentation, not rules authority, when deciding where a gameplay fix belongs.

**`apps/web/app/*/page.tsx` are thin route shims, not real pages.** The actual UI is a single client-only shell: `apps/web/app/ClientApp.tsx` dynamically imports `apps/web/src/App.tsx` with `ssr: false` (avoids hydration mismatches from `localStorage`/WebSocket use at init). Most `app/<route>/page.tsx` files just call `platform.setActivePage(...)` and render `null` — the corresponding `apps/web/src/<Name>Page.tsx` component under the `PlatformContext`/App shell is what actually renders. When asked to change a page's behavior, go to the `src/*Page.tsx` component, not the route file, unless the task is routing/metadata itself.

**`cards.json` has two copies, one source of truth.** `packages/game-core/src/cards.json` is authoritative; `services/realtime/internal/match/cards.json` is a generated copy. After editing card data, run:

```bash
node packages/game-core/scripts/sync-cards-json.mjs
```

**Go service layout** (`services/realtime/internal/`): `match/` (state machine, card resolution, snapshots, Redis/memory stores), `engine/v1/` (the LIVE custom chess engine actually used in production today — eval, alpha-beta search, NNUE, opening book, 5 computer difficulty levels; moved out of `internal/engine/` on 2026-09-02), `engine/core` (a NEW, separate bitboard rules kernel — Phase 1 of an in-progress engine rebuild, NOT yet wired into any running service; see "Engine rebuild" below), `engine/conform` (differential-fuzzing conformance harness comparing `engine/core` against `internal/match`), `platform/` (accounts, guests, friendships, moderation, notifications — each with `file`/`sqlite`/`postgres` backend variants selected by env var, e.g. `GUEST_STORE_BACKEND`), `matchmaking/` (queue/ticketing), `anticheat/` (replay-based cheat detection, Stockfish cross-check), `httputil`/`rate_limit`/`logging`/`metrics` (cross-cutting). Most `internal/platform/*_postgres.go` / `*_sqlite.go` files have a matching `_test.go` — check both when changing a store interface.

**Env-driven backend selection.** Persistence backends (`file` | `sqlite` | `postgres`/`redis`) are chosen per-store via env vars (see `services/realtime/.env.example` and `apps/web/.env.example`), not compile-time flags. When debugging "data didn't persist" issues, check which backend a given service was started with before assuming a code bug. **Production is Redis-backed** (Upstash, free tier — `db_request_limit` is a real monthly cap that has been hit before; avoid adding unconditional per-request/per-poll Redis traffic).

**Dual dev environments.** The repo was developed Windows-first; the primary working machine is now Linux (Fedora, Go at `~/sdk/go1.25.6/bin`). Ops scripts are PowerShell under `scripts/windows/`; the Linux equivalent of the local stack is manual startup (see README / RUNBOOK.md). Match whichever environment you are actually on rather than assuming one.

## Current status (2026-08-24)

### Release gate — 2026-09-04 → verified 2026-09-06

The pre-launch RC (`05e16e3`, incl. `7fc4180`) was deployed to all four
services on 2026-09-04 (auto-deploy worked; every deployment shows SUCCESS on
the merge commit). The 2026-09-06 production E2E run then found three real
defects in the RC, each fixed and verified live the same day:

- **Private/computer match reads 404'd for their own owner** (board never
  rendered). Two web bugs: the RC's snapshot route required a `claim.status`
  field the platform claims API never emits (its own Vitest mocks fabricated
  it), and the façade's bootstrap resume-ack overwrote the stored guest
  secret with the redacted response, leaving later reads unauthenticated.
  Fixed in `2e6dcac` + `c4e3601`.
- **A finished match was listed in history but its replay 404'd.** The
  platform archive's `Get`/`LoadMatch` served the in-memory overlay (stale
  creation-time `status=active` from the gateway's creation sync) instead of
  the finished Postgres row match-service writes directly at match end.
  Fixed in `5812b67` (backend-first reads + regression test).

Gate status after the fixes: **solo (computer match), private-invite, and
history-replay specs pass against production** — the gate's required
private/computer-match → finish → history/replay flow is green. The
`card-play` spec still times out on UI automation (the played card never
resolves through canvas target clicks within the spec's budget), but the
property it guards was verified live server-side on 2026-09-06: a
play_card → select_target intent sequence returned 200 and the dealt hand
shrank 3 → 2 across fresh authenticated snapshot reads. Repairing the spec's
canvas-target interaction (edge squares are the suspects) is follow-up work,
not a launch blocker. Mimosa deep-scan triage is done
([docs/audits/2026-09-06-mimosa-scan-triage.md](docs/audits/2026-09-06-mimosa-scan-triage.md);
152 findings, all dispositioned, no code changes required).

Remaining human/operations gates: configure a real SMTP provider (password
reset still uses preview delivery), enable and verify database backups, appoint
at least one moderation admin, triage the dependency-security scan, and review
Railway capacity/billing (the account showed only 18 days or $3.46 remaining on
2026-09-04). Do not create production environments or enable paid features
without the owner's explicit approval.

Production topology (verified directly against Railway, not from docs):

- URL: `https://web-production-1caefb.up.railway.app` (the older `web-production-ddc27` host is dead)
- Railway project `chess404` (`ecb0135d-84ac-48b8-b1ff-75191dda030f`), **5 services**: `web`, `gateway`, `match-service`, `platform-service`, `Postgres`. There is no separate `matchmaking-service` (it runs inside the platform-service container on port 8084) and no `replay-worker`.
- Persistence is **split**: all platform stores (accounts, guests, friendships, moderation, notifications, match archive) are `postgres`; match-claims and matchmaking tickets are Upstash `redis`.
- Only `web` and `match-service` have public domains. `gateway` and `platform-service` are internal-only and reachable solely through the web app's `/api/*` proxy routes.
- `GATEWAY_INTERNAL_URL`, `MATCH_SERVICE_INTERNAL_URL` and `PLATFORM_SERVICE_INTERNAL_URL` are all set to a value ending in a bare `:` with no port. This is harmless today only because `app/api/_lib/internal-service.ts` detects the missing port and falls back to a hardcoded `:8080` — which happens to be correct. Fix the variables rather than relying on that.

### Pre-launch audit, 2026-08-24

A full launch-readiness pass ran against live production: Go build/vet/test in a `golang:1.25` container, a production `pnpm build`/typecheck, a Playwright suite against the live site, an unauthenticated request matrix over every `/api/**` route, and a k6 load test. Everything below is **fixed, deployed and verified live** (commits `e2067ac`..`41b8eeb`).

**Railway auto-deploy does not fire on push.** After pushing to `main`, no service started a build for ten minutes; `gateway` and `match-service` had been sitting on `a8ae5bc` since Aug 24 00:15 while `web`/`platform-service` were on `74da367`. Re-pointing a service at `Chaos-master1/chess404@main` (`connect_service_source`) triggers a build immediately, and that is how every deploy in this pass was started. **`match-service` is still on `a8ae5bc`** — it never picked up `74da367`'s hosted-queue seat-secret fix, and has two `FAILED` builds in its history. Redeploy it deliberately and watch the build.

Fixed this pass (each with a check that fails if it regresses):

- **Nothing was ever archived.** `POST /api/platform/matches` required the snapshot to carry a seat secret, but match-service now redacts secrets from every snapshot it emits, so the gateway forwarded a redacted snapshot and *every* archive write returned 400 (confirmed in gateway logs, 100% failure rate; the production archive held zero rows). The secret requirement is gone and the endpoint is gated on the internal service token instead. History, replays and archived results all depended on this.
- **All match-service traffic shared one 60 req/min bucket.** `app/api/realtime/_lib/proxy.ts` resolved the internal service token from `MATCH_INTERNAL_SERVICE_TOKEN` / `CHESS404_INTERNAL_SERVICE_TOKEN` / `INTERNAL_SERVICE_TOKEN` — none of which are set on the web service — so it sent no token, skipped the trusted bypass, and every player's snapshot/intent/presence call counted against the web container's single internal IP. The load test reproduced it as a wall of 429s. It now uses the shared resolver in `app/api/_lib/internal-service.ts`.
- **New visitors could not register.** `/api/session/bootstrap` set an HttpOnly cookie and stripped `sessionSecret`/`sessionToken` from the JSON, on the assumption the client already had them. For a first-time visitor that request *is* the session creation, so the browser stored a guest id with no secret; registration then failed with "unauthorized guest session", and because the resume could never succeed, **every page load minted two fresh guest rows**. The gateway now only strips the secret when the caller's own guest id was the one resumed.
- **Private invites had no join path.** `joinPrivateMatch` was a dead import — never called anywhere. An invitee opening a shared link got a board and a "missing player credentials" error. A cold visitor is now offered the open seat via the gateway join endpoint (`useGameState.claimOpenSeatForVisitor`).
- **A network blip ejected players from live matches.** A failed snapshot fetch cleared the stored active match id, so an offline moment was indistinguishable from a deleted match. Errors now carry their HTTP status and only a definitive 404/410 clears state.
- **Hosted spectators were treated as phantom white players.** `applyAuthoritativeSnapshot` defaulted `viewerSeat` to `white` for any match with no local claim; that fallback is now local-play only.
- **Moderation admin by handle never worked.** `isModerationAdminAccount` only checked `PLATFORM_ADMIN_ACCOUNT_IDS`, while the capability flag that renders the admin panel also accepted `PLATFORM_ADMIN_HANDLES` — so a handles-only deployment showed the panel and 403'd every action. (Neither variable is set in production, so **there is currently no moderation admin at all**.)
- **`/status` crashed** on `snapshot.platform.archive.totalMatches`; the platform status endpoint never sent the `archive`/`accounts`/`claims`/`guests` stat objects the page requires. It sends them now (each store already had a `Stats()`).
- **`/dashboard` shipped publicly** — an engine debug console hardwired to `ws://localhost:8765`, which the site's own CSP blocks. It is now gated in `middleware.ts`, which answers a real 404; gating it in the route's layout with `notFound()` rendered the not-found page but still returned 200.
- **A player's own history was unreachable in three separate places.** `/history` queried the public archive (which excludes vs-computer and private games); the replay detail endpoint applied the same public rule, so scoped entries 404'd on open; the detail proxy route dropped the query string, so the `guestId` that authorizes a participant never reached platform-service; and the page read the guest id once at mount, before the guest session is minted. All four are fixed: scoped list queries skip the public filter, the detail endpoint serves a finished match to a caller naming a participating guest id, the proxy forwards `search`, and the guest id arrives as a prop so both queries re-run when it appears.

Verified healthy (do not re-litigate without new evidence): seat-secret redaction on anonymous snapshot reads; WebSocket subscribe requires a real seat secret (constant-time) and rejects spectators; account-scoped endpoints all demand a session token, admin endpoints demand admin standing; guest sessions cannot be resumed from a public guest id; the public watch feed excludes `queue=direct`, `modeId=computer` and aborted matches; CSP nonce matches between header and script tags; the web proxy refuses direct match creation in production; card play resolves server-side (verified across a reload); no horizontal overflow at 390×844.

Verified after deploy, against production: `/dashboard` 404s; `/status` renders; a first-time visitor receives its guest secret and can register; a bootstrap carrying credentials no longer mints guests (100 → 100); a finished match archives and replays; the watch feed lists no unclaimed rooms; 80 consecutive proxied calls to match-service returned **zero** 429s (the same pattern produced a wall of them before); a participant opens their own replay (200) while an anonymous reader and a non-participant still get 404.

Load test (k6, live, 150 VUs ≈ 231 req/s through the public origin, measured **before** the rate-limit fix): pages all 200 with p95 1.85 s; `web` peaked at 1.55 vCPU / 322 MB and was the bottleneck — every route is `force-dynamic` with `no-store`, so nothing is cached and each visitor gets a full SSR render. Go services stayed near-idle (match-service peaked 0.15 GB / 4% CPU) and logged no panics. Direct public requests to `match-service` are throttled to ~60/min per IP as designed; unauthenticated match creation there is possible but IP-limited and abandoned matches are reaped after 30 min.

**Engine rebuild — Phases 1 and 2 complete, not yet live:** a ground-up rewrite of the computer opponent's engine is in progress at `services/realtime/internal/engine/{core,conform,actions,search,nnue}` — a NEW, separate codebase, not a modification of the live `internal/engine/v1` used in production today. **Nothing outside these packages imports this code yet** — it has zero effect on production, and the live computer opponent is still `internal/engine/v1`.

Phase 1 (kernel): bitboards, magic sliders, make/unmake, full Zobrist hashing, card overlay planes for Frozen/Shielded/FusedWith/Fortress/Lava/Bomb/BlackHole. Perft-verified to depth 6 against standard reference values; `engine/conform`'s differential fuzzing shows zero mismatches against `internal/match` across the full ~1,392-candidate brute force at the standard opening, hand-built Frozen/Fortress/FusedWith scenarios, and multiple 40-ply random-walk games. Fog is deliberately unmodeled (confirmed zero rules effect in `internal/match`).

Phase 2 (combined search): `engine/actions` (unified Action type covering both chess moves and card plays — a representative subset of 7 mechanics: Freeze/Shield/Fortress/Lavaground/Unabomber/BlackHole/HalfFuse+FullFusion, not all 37; the other 30 are unmodeled by design, same posture as Fog) and `engine/search` (negamax/alpha-beta with cards as first-class tree nodes, a transposition table with correct exact/lower/upper bound handling, and PIMC fair-play search that samples a plausible opponent hand from the real rarity-weighted 37-card pool rather than ever reading the actual hidden hand). The card-tactics suite (`engine/search/cardtactics_test.go` + `search_test.go`) proves the headline capability directly: the search finds Freeze-then-capture, Shield-saves-a-hanging-piece, and Fusion-enables-an-otherwise-impossible-capture, and a direct empirical test confirms the CURRENT PRODUCTION engine (`internal/engine/v1/opponent.go`) cannot find the same Freeze+capture coordination even though it can mechanically play the card, because it scores cards and moves on independent scales and never re-examines the board after deciding to play a card. Two real bugs were caught and fixed while building this: card actions were silently double-negated in the search (freezing pieces backwards), and a card effect (Fusion) can rarely open a fresh attacking line onto the enemy king mid-turn, which the engine now handles as a decisive result instead of crashing.

Remaining phases (per the plan file used to drive this work): Phase 3 (real quantized NNUE trained with actual card play, replacing Phase 2's placeholder material+overlay eval), Phase 4 (extract as its own scaled service, wire into match-service, live deploy). Pending-card/double-move turn-sequencing state remains unmodeled (the turn model is deliberately simplified to at-most-one-card-plus-one-move, tighter than internal/match's actual unbounded `card* move` structure).

**Phase 3 status (2026-09-06):** the pipeline is verified end to end (encoder, weight format, Go/Python parity, gauntlet) but the Aug-9 dataset — 147k positions from depth-2 self-play — is the bottleneck: the retrained v2 network improved 0% → 3.75% vs the placeholder eval and still loses badly; deep-search positions are out of distribution and the material signal is too weak to drive search. Full diagnosis with baselines and next commands:
[docs/audits/2026-09-06-phase3-nnue-diagnosis.md](docs/audits/2026-09-06-phase3-nnue-diagnosis.md). A deeper (depth-6) self-play generation is running; retrain + re-gauntlet is the next engine session's first task. `internal/engine/search/nnue_scale_test.go` now guards the eval's sign/scale.

**Known remaining gaps (2026-08-24, unfixed):**
- The gateway's `syncMatchSnapshotToPlatformService` only fires at match creation/join, never at match end — but this does not break archiving, because match-service writes finished snapshots straight to the same Postgres `archives` table via `persistSnapshot`. Verified end to end: a resigned casual match is archived, replayable and listed within seconds.
- **A guest id is now enough to list and open that player's private and vs-computer games.** That is the same trust level as the existing public guest directory, and the replay payload is sanitized, but it is a deliberate widening — revisit it if guest ids ever become sensitive.
- **Page loads occasionally exceed 45 s.** Two spec runs failed on a `page.goto` navigation timeout against `/play` and `/account` while p95 under load was 1.85 s. **2026-09-02 follow-up: not an SSR-compute problem.** A production build served locally renders every page in 8–16 ms (110 ms on the very first request after server start; HTML shells are 9–12 KB). HTML caching is also structurally unavailable: the per-request CSP nonce (`middleware.ts` → `x-nonce` → `layout.tsx`) and the middleware's global `Cache-Control: no-store` (a deliberate fix for stale env-config CDN caching) both force per-request HTML. The remaining suspects are operational: Railway cold starts of the `web` container, cold/unhealthy Go services behind the `/api` proxy (match-service was on a stale deploy at audit time), or first-connection overhead. Fix directions: keep instances warm (min replica), finish the match-service redeploy, watch real-user timings; do not chase page-level caching.
- No moderation admin exists in production (`PLATFORM_ADMIN_ACCOUNT_IDS` / `PLATFORM_ADMIN_HANDLES` are both unset).
- Email delivery still defaults to a `preview` provider (logs the reset link instead of sending real email).
- The internal service token is a single shared value across all four services, and the web proxy stamps it on every proxied request, so any browser-reachable proxy route inherits internal-caller trust. Archive writes are not exposed this way (the web route is GET-only), but new proxy routes must be checked against this.
- `GET /api/platform/guests` and `/api/platform/rankings` publish the full player roster unauthenticated. That is a deliberate directory (the client uses it), and a guest id alone grants nothing — but it is enumerable.
- **No database backups exist.** Railway's Postgres **Backups tab → Enable PITR** is the right fix; it is a dashboard action with a small billing cost, so it needs a human decision. `deploy/postgres-backup.sh` works standalone but nothing schedules it and without `AWS_S3_BUCKET` it writes to an ephemeral container disk.
- Repo-root `railway.json` was deleted (2026-07-29): it used a `services`/`cron`/`volumes` shape Railway's config-as-code schema does not support. The healthcheck/restart-policy/replica settings it described **are** live, applied via the dashboard/API. See `deploy/railway/reference-config.json`.

E2E lives in `e2e/` and runs against production by default (`E2E_BASE_URL` overrides; `pnpm test:e2e`). Shared helpers are in `e2e/_helpers.ts`. Go 1.25 is installed on the Linux machine at `~/sdk/go1.25.6/bin` (add it to PATH); on machines without Go, the Docker route (`golang:1.25`) still works but SELinux blocks bind-mounting the repo directly — copy the module to a scratch dir and mount it with `:z`.

The full 2026-08-30 launch audit report lives at [docs/audits/2026-08-30-launch-audit.md](docs/audits/2026-08-30-launch-audit.md); superseded audit history is under `docs/audit-archive/`, and the old running status journal is `docs/history/PROJECT_STATUS.md` (stale — historical only).

## Important Note
After major changes, please update this file (@CLAUDE.md). Keep this file up-to-date with the project's status.
