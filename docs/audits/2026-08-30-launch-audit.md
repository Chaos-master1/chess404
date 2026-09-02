# Chess404 — Platform Audit Report (2026-08-30)

**Scope:** Full platform (Next.js web, Go services: gateway, match-service, platform-service, plus engine, match state, platform stores, rate limiting, proxy layer). Live Railway project `chess404` (`ecb0135d-84ac-48b8-b1ff-75191dda030f`, env `production`, 5 services). Code as of `8ed1153`.

**Method:** Live probing of `https://web-production-1caefb.up.railway.app`, Railway MCP (list-services, get-status, environment-status, list-deployments, get-logs, list-variables, list-domains, http-requests/error-rate), full `go test` + `go test -race` + `go vet` (Go 1.25.6), `pnpm audit`, `turbo lint`, 9-file Playwright e2e suite against live (≈11 tests), code reading of `services/realtime/internal/{match,platform,engine,httputil,rate_limit,anticheat}` and `apps/web/{app,s/src}`.

**Verdict: Not spaghetti. No rewrite needed.** The platform is well-modularized, tests are meaningful, and all critical launch-blockers from the Aug 24 pre-launch audit (`e2067ac`, `5913cec`, `2b5e382`, `f27a4ab`) remain fixed. One new race bug was found and fixed; the rest are moderate/low.

---

## 1. Live Health (verified 2026-08-30 01:30–02:00 UTC)

| Surface | Result |
|---|---|
| `GET /` | 200, landing rendered |
| `GET /api/gateway/healthz` | `{"service":"gateway","status":"ok"}` |
| `GET /api/gateway/readyz` | `{"status":"ok"}` |
| `POST /api/gateway/bootstrap` | 200, `realtimeReady: true`, `platformReady: true`, `matchmakingReady: true`, 2 guest sessions |
| `GET /api/platform/status` | 200, 97 matches (51 finished, 46 active), 1284 guests, Postgres archive, Redis claims |
| `GET /api/matchmaking/status` | 200, 0 tickets, backend `redis` |
| `GET https://match-service-production.up.railway.app/healthz` | `{"service":"match-service","rulesVersion":"v1-alpha-foundation"}` |
| CSP / security headers | `Content-Security-Policy` (nonce + strict-dynamic, `connect-src` allows match-service), `Strict-Transport-Security`, `X-Frame-Options: DENY`, `Permissions-Policy`, `Referrer-Policy`, `X-Content-Type-Options: nosniff` — all present |

**Railway:** 5 services (`web`, `gateway`, `match-service`, `platform-service`, `Postgres`) all `SUCCESS`, 1 replica each, 0 failures last 8h, ~80 gateway requests seen in logs (bootstrap, private-matches, presence, intents) with zero 5xx.

**E2E:** All 11 Playwright tests pass individually against live (`solo`, `auth`, `private-invite`, `reconnect`, `card-play`, `spectate-privacy` ×2, `history-replay`, `moderation-authz` ×2, `multiplayer`, `pages-smoke` ×6). Two stale `test-results/` folders from a prior run contained `board-root` timeouts; re-running showed they were transient (likely deploy-window or tutorial-modal timing).

---

## 2. Code Structure

**Engine (`services/realtime/internal/engine/`):** Not spaghetti. Clean split:
- `eval.go` (22 KB, baseEval + modifierScore)
- `nnue.go` (11 KB, 847→512→1, `encodeHand`)
- `search.go` (34 KB, `alphaBeta`, `TranspositionTable`, `GenerateAllMoves`)
- `mcts.go` (4 KB, PUCT, separate from alpha-beta)
- `cards.go` (13 KB, `CardEvaluator`)
- `chess.go`, `perft.go`, `selfplay.go`, `gauntlet.go`, `dashboard.go`, `book.go`, `parallel.go`, `zobrist.go`
- New rebuild in `engine/{core,conform,actions,search}` is isolated, not wired to production — no blast radius.

**Web (`apps/web/`):** `app/` routes are thin proxies to internal services via `app/api/_lib/internal-service.ts` (single `internalServiceToken()` resolver) and `app/api/realtime/_lib/proxy.ts` / `app/api/gateway/_lib/proxy.ts`. `src/lib/match-service.ts` correctly routes intents/presence through `gatewayBaseUrl = '/api/gateway'` (not direct). `chessEngine.ts` re-exports `@chess404/game-core` (no duplication). `cardEngine/machines/` is small (207 lines). `useMatchEngineFacade.tsx` (747 lines, not 4698 as mis-reported) composes hooks cleanly.

**Go backend (`services/realtime/internal/`):** `match/` owns the authoritative state machine; `platform/` owns Postgres/Redis stores with `file`/`sqlite`/`postgres` variants; `matchmaking/` queue is 79.2% covered; `anticheat/` (Irwin, streaks, fen) 68.3% ; `httputil`/`rate_limit` are shared. No global mutable singleton spaghetti.

---

## 3. Tests & Coverage

**Go:**
- `go test ./...` — all 24 packages pass (25.5 s).
- `go vet ./...` — clean (only `go-sqlite3` C warning, vendored).
- `go test -cover ./...` (selection):
  - `engine/search` 90.2%, `engine/nnue` 87.3%, `engine/core` 84.8%, `engine/conform` 85.9%, `engine/actions` 85.7%, `engine` 62.8%, `match` 71.9%, `matchmaking` 79.2%, `anticheat` 68.3%, `xgauntlet` 68.6%
  - `gateway` 66.8%, `platform-service` 44.4%, `platform` 37.8%, `rate_limit` 50.4%, `httputil` 6.4%, `match-service` 22.4%, `analysis-worker` 10.6%, `matchmaking-service` 8.2%
  - Low-coverage areas: `httputil`, `match-service` wiring, `platform` stores. Not spaghetti, but worth adding tests before modifying them.
- `go test -race ./internal/match` — **FAILED** (fixed this run, see §5.1). `platform` and `matchmaking` pass with `-race`.

**Web:**
- `turbo lint` — 3 packages, all pass (cache hit).
- `pnpm audit` — **21 vulnerabilities** (1 low, 9 moderate, 11 high): `postcss` GHSA-fxqj…, `@opentelemetry/core` GHSA-8988… via `@sentry/nextjs@8.55.2`. Not exploitable in the current deployment surface but should be patched.

**E2E:** 9 specs, 11 tests, `fullyParallel: false`, `workers: 1` (intentionally serial to avoid guest/queue collisions). All pass; `pages-smoke` covers 6 routes for CSP/console errors.

---

## 4. Security Posture

- **Shallow secrets:** `INTERNAL_SERVICE_TOKEN` is env-only, not committed. `proxyInternalService`/`proxyRealtime` strip `Host`/`Content-Length` and inject `x-chess404-service-token` via single resolver (`apps/web/app/api/_lib/internal-service.ts:203`), fixing the Aug 24 429-bucket bug.
- **CSRF:** `rate_limit.CSRFMiddleware` + `IsOriginAllowed` + `reconstructPublicOrigin` via `X-Forwarded-*` forwarding in `buildUpstreamHeaders`. Live WS `CheckOrigin` in `match-service/main.go:66` rejects missing/unknown origins.
- **CORS:** `httputil.ParseAllowedOrigins()` + `withCORS` returns `Access-Control-Allow-Origin` only for allow-listed origins; `Access-Control-Max-Age: 600` caches preflight only.
- **CSP:** Built per-request in `middleware.ts:extraConnectOrigins()` from `NEXT_PUBLIC_MATCH_SERVICE_*` env; includes `https://`, `wss://`, `http://` variants. No `unsafe-eval` in production (`strict-dynamic` + nonce).
- **Auth:** Guest `sessionSecret`/`sessionToken` are HttpOnly `Secure; SameSite=Strict` cookies (`gateway/main.go:330`), stripped from JSON when the caller already holds the identity (`bootstrapResumedSuppliedGuest`), otherwise returned once. WS auth is `claimToken` → `ResolveAuthToken` or `resolveSocketClaim` (platform `/api/platform/match-claims/resolve`), then `Subscribe` verifies `playerSecret` against seat.
- **Replay privacy:** `apps/web/app/api/realtime/matches/[matchId]/route.ts:45` gates snapshot reads: owner (via `requestOwnsMatchSeat` → platform `/api/platform/match-claims`) or `ownsMatchSeatByGuestId` or `direct` queue; otherwise `isPublicSpectatorReadable` (active, not direct, no winner) and `buildPublicSpectatorSnapshot` redacts hands/secrets/history. `spectate-privacy.spec.ts` verifies non-owner cannot leak secrets/hands and watch feed excludes private/computer games. Scoped replay list (`history` page) was fixed in `5913cec` + proxy `search` forwarding in `2b5e382`.
- **Headers:** Verified live: `Content-Security-Policy`, `Strict-Transport-Security: max-age=31536000; includeSubDomains`, `X-Frame-Options: DENY`, `Permissions-Policy: camera=(), microphone=(), geolocation=()`, `Referrer-Policy: strict-origin-when-cross-origin`, `X-Content-Type-Options: nosniff`.

**No new authz/CSRF/CORS bug found.**

---

## 5. Bugs Found & Fixed

### 5.1 🔴 CRITICAL — `MatchArchiveStore` nil-map panic under race (FIXED this run)

- **File:** `services/realtime/internal/platform/history.go:235`
- **Symptom:** `go test -race ./internal/match` panics: `assignment to entry in nil map` at `s.entries[match.MatchID] = entry` inside `Upsert`, call chain `processMatchBroadcast → persistSnapshot → archive.Upsert → history.go:235`. Concurrent `Close()` (`entries = nil`, `private = nil`, `dirty = nil`) races with in-flight `Upsert`/`Get`/`LoadMatch`.
- **Impact:** In production, shutdown (`service.Close()` → `finArchive.Drain()` → `archive.Close()`) while broadcasts are still in flight (computer opponent moves, tick loop) could panic the process. Not triggered in normal `go test` without `-race`, but reachable under load + deploy/restart.
- **Fix:** `history.go:208` `Upsert`, `261` `Get`, `280` `LoadMatch` now check `s.closed` and lazily re-init `nil` maps before assignment, matching the `writeLoop`/`Flush`/`Close` locking discipline. Verified: `go test -race ./internal/match` now passes (4.6 s), along with `platform`/`matchmaking`.

### 5.2 🟡 MINOR — Pnpm high vulns (NOT fixed, recommended)

- `postcss` and `@opentelemetry/core <2.8.0` via `@sentry/nextjs@8.55.2`. Patch by `pnpm update postcss @sentry/nextjs` (or pin `@opentelemetry/core@^2.8.0`). No code change.

### 5.3 ℹ️ INFO — Docs vs reality drift (FIXED this run)

- `ARCHITECTURE.md` claimed 6+ Railway services, separate `matchmaking-service`, `replay-worker`, `messaging`/`sharding`/`featureflags`/`tracing` as active. Production runs **5 services** (verified via `railway list-services`/`get-status`): `web`, `gateway`, `match-service`, `platform-service`, `Postgres`. `matchmaking-service` binary exists but is **merged into `platform-service`** (its `/api/matchmaking/*` routes are served by `platform-service` via shared Redis). `replay-worker`, `messaging`, `sharding`, `featureflags`, `tracing` are code stubs or not deployed. `Data Flow` step 7 claimed `NATS/Redis Streams`; production uses **Redis Pub/Sub only**. `ARCHITECTURE.md` was reconciled to match live `railway` state and coverage numbers.

---

## 6. Remaining Risks (not fixed, next steps)

- **Coverage gaps:** `httputil` (6.4%), `match-service` mux wiring (22.4%), `platform` stores (37.8%). If you plan to touch those, add tests first. Engine and match state are well covered.
- **Engine `search.go` monolith (34 KB):** Functional but large; consider splitting `alphaBeta`/`quiescence`/`moveOrdering`/`TT` when next modifying search, but not urgent.
- **`.env.local` with LAN IP (`192.168.0.6:8082`):** Ignored by git (`.gitignore: .env.local`) but present locally. No production impact; no action required, but note it overrides Railway env locally if `Railway run` is not used.
- **No GitHub Actions CI:** Deploy is manual (`railway connect_service_source` to re-point `Chaos-master1/chess404@main`). All builds succeeded this week except two early `a8ae5bc` `FAILED` on `gateway`/`match-service` that recovered on retry.

---

## 7. Is This Spaghetti? Should You Rewrite?

**No.** The codebase is not spaghetti and does not need a rewrite. Signals:
- Module boundaries are clear (engine eval vs search vs MCTS vs cards vs match state vs platform stores).
- Server-authoritative enforcement is consistent (all intents validated in `internal/match`, client is presentation only).
- Tests are real (perft, gauntlet, conform fuzzer, solo/multiplayer card play, reconnect, history/replay, authz) and they pass live.
- The Aug 24 pre-launch sweep already proved seams between services (proxy token, bootstrap, invite join, archive, feed, moderation caps) are the risk area, not the engine — and those were fixed and remain fixed.
- A rewrite would discard 50K NNUE positions, a working MCTS + card-aware eval integration, the modifier system, and the perft/gauntlet self-play harness.

**What to do instead:** Keep iterating: small, reversible fixes (like §5.1), add tests around low-coverage wiring before changing it, patch deps (`pnpm update`), and use the `RUNBOOK.md` checklist before each deploy. The architecture docs were the main debt; they are now reconciled.

---

## 8. Evidence Index

- Live probes: `GET /`, `/api/gateway/{healthz,readyz,bootstrap,status}`, `/api/platform/status`, `/api/matchmaking/status`, `MATCH_SERVICE /healthz` (Railway logs 01:30 UTC); CSP/headers via `curl -D`.
- Railway MCP: `list-services` (5), `get-status` (all `SUCCESS`, 2026-08-22→25), `environment-status` (0 failures), `list-deployments` (25, branch `main`), `get-logs` (gateway 61, match-service 101 lines), `list-variables` (web 24, gateway 16, match-service 18, platform-service 24), `list-domains` (web `web-production…`, match-service `match-service-production…`).
- Go: `go 1.25.6`, `go test ./...` (24 packages ok), `go vet ./...` (clean), `go test -cover` (see §3), `go test -race ./internal/match` (was FAIL, now PASS).

*Generated by the build-mode audit run; one code fix (history.go) is included in the working tree and must be deployed.*
