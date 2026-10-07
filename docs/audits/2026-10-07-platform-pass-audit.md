# Platform pass — four-dimension audit (2026-10-07)

Scope: full-platform pass per the approved 5-phase plan (P0 security/WIP,
P1 prod E2E, P2 this audit, P3 top-gap fixes, P4 NNUE dataset restart, P5 docs).
Commit range: `cef8289..4959d41` (9 commits this pass). Prod: all services on
`e46c3ac` + web on `1fb3068` (Next 15.5.27, time-control labels).

## Scores

| Dimension | Score | Summary |
|---|---|---|
| SPEC | 8.5/10 | Behavior matches CLAUDE.md/SPEC surfaces; two shipped gaps closed this pass (finished-match WS subscribe, time-control label); remaining gaps are owner-blocked, not spec drift. |
| DESIGN | 8/10 | Clean layering (web → gateway → services), defense-in-depth worked in practice (reject-path caught the v1 engine's illegal double-move). Shared internal token is the weakest structural point. |
| CORRECTNESS | 7.5/10 | 46/46 prod E2E, multiplayer ×3, 105-request unauth matrix clean, 20/100-pair soaks clean, httputil 96.3% cover. One latent v1-engine double-move bug found (see below). |
| QUALITY | 8/10 | Gates green (Go vet/test, -race, web lint/vitest/build, CI docker matrix); two new test suites; a11y smoke added; docs debt called out honestly below. |

## This pass's changes (evidence)

- **fix(match)** `9900572` — WS subscribe of finished matches without seat
  proof (mirrors #24's GET fix; 3 regression tests). Prod-verified.
- **chore(deps)** `83a05a3` — next 15.5.27 (2 CRITICAL RCEs patched),
  sharp 0.35.5 / fast-uri 3.1.8 / brace-expansion 2.1.7 / source-map-js 1.2.2
  overrides; turbo range bumped `^2.1.0`→`^2.11.7` by the same install (lockfile
  agrees), which also cleared turbo's moderate+low advisories.
  pnpm audit: prod 23 vulns → 8 (0 crit/1 high/7 mod).
- **fix(httputil)** `2ca8117` — CircuitBreaker.Allow never incremented
  `halfOpenCount`: `halfOpenMax` was dead config; half-open admitted unlimited
  probes at a failing dependency. Fixed + state-machine tests.
- **test(httputil)** `e46c3ac` — 6.3% → 96.3% coverage: body-limit cap,
  recovery no-leak, request-id/Flush/Hijack passthrough, CORS allow-list
  (empty admits nothing, exact case-insensitive match), breaker, retry.
- **fix(nnue-selfplay)** `5f462bd` — per-game flush + progress (the stalled
  484K dataset died in a bufio buffer; now crash-safe).
- **feat(web)** `1fb3068` — match time-control label on player bars
  (TS contracts now declare `clockSeconds`/`clockIncrement`; shared
  `lib/clock.ts`; legacy snapshots without the fields show no chip).
- **chore(e2e)** `c85a616`+`2abf5ff` — unauth request-matrix script (fixed
  first-run bug: GET sweep used absolute paths as URLs).
- **test(e2e)** `4959d41` — axe WCAG A/AA smoke on all 16 public routes.

## Verification results (2026-10-07, prod)

- Playwright full suite: **46/46** (15.5m). Multiplayer spec: **3/3 × 3 runs**.
- Unauth matrix (105 requests): **0 fail**. 2xx accounted: health/status/
  readyz/capabilities/rankings/queue snapshots are public by design;
  `/api/platform/{accounts,guests,matches}` are public directories (matches
  list strips private rooms at `routes_matches.go:62`, public predicate);
  param-validation 400s are not authz bypasses; guest-session mint +
  password-reset-request are intended public flows.
- Soaks (handoff path, prod): **20 pairs 20/20 in 6.2s**, **100 pairs 200/200
  in 18s** (pre-pass baselines: 9.4s / 35.3s) — 0 stuck, 0 pairing leaks,
  0 claim failures, **0 × 429**, queue drained.
- a11y axe (wcag2a+2aa, all 16 routes): **0 critical**; 1 serious finding:
  `/cards` scrollable-region-focusable (1 node) — sole a11y backlog item.

## Findings

### F1. v1 engine submits an illegal double-move (xgauntlet flake) — DOCUMENTED, NOT PATCHED

CI run `37660412272` (go-test) failed on
`TestPlayOneGameOldVsOldCompletes`: the **old v1 engine** submitted
`make_move` rejected by the real match service — *"first double move cannot
put enemy king in check"*. The v1 search path already has
`FirstDoubleMoveRejected` + candidate fallback (patched twice before per
inline comments) and the book probe is gated off during double moves, yet a
case still slips through — the mirror predicate diverges from `applyMove`'s
actual guard in some position, and no minimal repro is known.

Why not patched now: v1 is the legacy engine (the rebuild at
`internal/engine/{core,search}` supersedes it), the fix surface is subtle
search logic with regression risk mid-pass, and the service-side rejection
(defense in depth) worked exactly as designed. Re-armed by the rebuilt
dataset work: xgauntlet will exercise this heavily in Phase 4/5; if it
reproduces with a seed, capture the state dump then.

### F2. Dependency audit — remaining items (pnpm audit, 2026-10-07)

Prod: 8 (0 crit/1 high/7 mod). All-audit: 13 (2 crit dev-only).

- **braces 3.0.3 HIGH (prod, stack-exhaustion DoS)** — no upstream fix exists
  (`fix: <0.0.0`); document-only per plan. Re-check on any braces release.
- **tinypool 1.1.1 ×2 CRITICAL (dev-only, via vitest 3)** — fix = vitest 4
  major bump (`>=4.1.11`); out of scope mid-pass, low runtime exposure
  (dev/test tooling only). Recipe: `pnpm -w add -D vitest@^4` + fix breaking
  config, re-run web tests, commit separately.
- **@opentelemetry/instrumentation-* ×7 moderate (prod, via Sentry 8.55.2)** —
  db-username span exposure; fixed in OTEL ≥0.67/0.73/2.8. No direct dep;
  clears with a Sentry major bump. Report-only per plan.
- **turbo 2.11.7** — now clean (cleared by the range bump).

### F3. Shared INTERNAL_SERVICE_TOKEN across all four services — runbook ready, rotation NOT executed

All 8 token-bearing variables share one value (sha256 prefix `dd92aa0f2d18`,
len 64): `INTERNAL_SERVICE_TOKEN` on all services + `GATEWAY_*`/`PLATFORM_*`
on web. Any single-service compromise grants full internal trust. The code
already reads distinct per-destination vars (`matchServiceCallerToken()`
mirrors `platformServiceCallerToken()`), but there is no dual-accept window,
so a rotation is a coordinated 4-service change where one stale variable = a
production internal-401 outage.

Runbook (execute in a quiet window, ~10 min):
1. Pick two new 64-char tokens: T_GW (web↔gateway), T_SVC (web/match/platform
   internal). 
2. `railway variables --service gateway --set "INTERNAL_SERVICE_TOKEN=$T_GW"`
   → gateway redeploys accepting T_GW only.
3. web: `--set "GATEWAY_INTERNAL_SERVICE_TOKEN=$T_GW"` (web → gateway calls
   recover immediately).
4. platform: `--set "INTERNAL_SERVICE_TOKEN=$T_SVC"`; then match:
   same; then web: `--set "INTERNAL_SERVICE_TOKEN=$T_SVC"
   "PLATFORM_INTERNAL_SERVICE_TOKEN=$T_SVC"`.
5. Smoke: web readyz, gateway status, one full queue→match→move flow,
   `GET /metrics` with the right bearer per service.
Rollback: set the old value back per service (redeploys are instant).
Recommend doing this with a dual-accept code change later
(accept old OR new for one deploy cycle) to make it zero-outage.

### F4. Backups: RESOLVED 2026-10-05, REGRESSED by 2026-10-07 (owner action required)

- The `postgres-backup` workflow was fully armed and verified on 2026-10-05
  (R2 bucket, restore drill, first verified objects — see production
  checklist §2). **Today's scheduled run (`37623044600`, 2026-10-07 06:00
  UTC) no-ops:** it logged *"postgres-backup is not configured. Set the
  BACKUP_* repository secrets…"* — i.e. at least one of the five
  `BACKUP_*` repo secrets is now empty/absent. Green checkmark ≠ a backup.
- Railway PITR remains unavailable on the trial plan (recorded decision).
- Owner steps: re-set `BACKUP_DATABASE_URL`, `BACKUP_AWS_S3_BUCKET`,
  `BACKUP_AWS_ACCESS_KEY_ID`, `BACKUP_AWS_SECRET_ACCESS_KEY`,
  `BACKUP_AWS_ENDPOINT_URL` (values/notes in checklist §2), then
  `workflow_dispatch` once and verify an object lands in the bucket + the
  restore drill passes. Worth checking whether the Cloudflare API token or
  a GitHub org secret policy change removed them.

### F5. Ops verified clean this pass

- No bare-`:` internal URL envs remain (all `*_INTERNAL_URL` well-formed;
  the empty `url` fields in gateway status responses are deliberate
  redaction, `gateway_status.go:23`).
- `ALLOWED_ORIGINS` pinned to the prod web origin on all three Go services.
- Railway deploy stall (auto-deploy dead since Oct 5; match-service had 2
  days of FAILED deploys incl. #24's) — all services now ship via
  `railway up -s <svc>`; GitHub auto-deploy still not firing (worth a
  Railway support ticket / webhook re-auth as follow-up).

## Coverage-driven test plan (next gaps, measured)

Baseline before this pass (full-repo docker coverage): httputil 6.3%,
cmd/match-service 28%, platform 39.3%, rate_limit 51.8%, match 73.3%,
matchmaking 82.9%. This pass closed httputil (96.3%). Ordered next:

1. **cmd/match-service (28%)** — route-level authz: the `/api` handlers'
   token checks and guest-vs-seat scoping are the next security-critical
   untested surface. Table-test handlers with a stubbed Service.
2. **cmd/platform-service (39.3%)** — the biggest handler surface; prioritize
   the money-adjacent paths (account-auth/*, match-claims, sessions).
3. **rate_limit (51.8%)** — window-boundary and burst-edge tests.
4. **match (73.3%)** — clock edge cases (flag with pending card), replay
   worker restart mid-flush.
5. Frontend vitest is thin but E2E coverage is strong (46 specs incl. auth,
   authz, spectate-privacy, reconnect); invest backend-first.
6. A11y: fix the single `/cards` finding, then tighten the axe gate to
   `serious` once triaged.

## Owner-blocked (report-only, unchanged)

SMTP provider for real email delivery (password reset is preview-mode);
backups (F4); logo assets; billing/Railway capacity review; rated-rematch
Elo decision; rematch private-only; guests-invisible sweep.
