# Production checklist — 2026-09-02

Things that cannot be done from the repo and need the Railway dashboard (or
GitHub secrets UI). Each item names the evidence and the exact action.

## 1. Email is silently in preview mode

`cmd/platform-service/account_email_delivery.go:230` defaults
`ACCOUNT_EMAIL_DELIVERY_PROVIDER` to `preview`, which only logs password-reset
links. Password reset therefore does not actually email anyone in production.

**Action:** set the SMTP block on the `platform-service` service in Railway —
the full env-var list with explanations is in
[DEPLOY_RAILWAY.md](../../DEPLOY_RAILWAY.md) (`platform-service` section).
Minimum: `ACCOUNT_EMAIL_DELIVERY_PROVIDER=smtp`, `ACCOUNT_EMAIL_SMTP_ADDRESS`,
`ACCOUNT_EMAIL_SMTP_FROM`, `ACCOUNT_EMAIL_SMTP_USERNAME`,
`ACCOUNT_EMAIL_SMTP_PASSWORD`, `ACCOUNT_EMAIL_SMTP_TLS=true`.
Verify: request a password reset and confirm the mail arrives.

## 2. No database backups — RESOLVED 2026-10-05 (verified end-to-end incl. restore)

The daily dump now runs for real, and a restore has been proven:

- **Schedule:** `.github/workflows/backup.yml` → `deploy/postgres-backup.sh`
  daily 06:00 UTC (+ `workflow_dispatch` for on-demand runs).
- **Storage:** Cloudflare R2 bucket `chess404-backups` (WEUR). Endpoint
  `https://136535a865013e08836f0ef4ae847fb7.r2.cloudflarestorage.com`, key
  prefix `postgres/`, retention via the bucket lifecycle rule
  `prune-postgres-30d` (deletes `postgres/*` after 30 days).
- **Credentials:** all five GitHub secrets set (`BACKUP_DATABASE_URL`,
  `BACKUP_AWS_S3_BUCKET`, `BACKUP_AWS_ACCESS_KEY_ID`,
  `BACKUP_AWS_SECRET_ACCESS_KEY`, `BACKUP_AWS_ENDPOINT_URL`). The S3 pair is
  an account-owned Cloudflare API token minted via the REST API, scoped to
  exactly this bucket (`Workers R2 Storage Bucket Item Write`; Access Key ID
  = token id, Secret = SHA-256 of the token value per R2's auth docs). No
  expiry; revoke in the CF dashboard if it ever leaks.
- **Server-version pin:** ubuntu-latest ships pg_dump 16, the Railway server
  is 18.6 — the workflow installs `postgresql-client-18` from PGDG (PR #19)
  and pins the major in one place for the next bump.
- **Restore drill (in-job):** every run restores the fresh dump into a
  throwaway `postgres:18` container with `ON_ERROR_STOP` and asserts ≥20
  public tables and ≥1 row in `accounts` / `account_credentials` (PR #20).
  First drill: run 37254716196 → `tables=21 accounts=18
  account_credentials=18`. A dump that cannot be restored fails the job.
- **First verified objects:** run 37251585752 uploaded 1.9 MB
  (`chess404_railway_20261005T012911Z.sql.gz`), confirmed present via the R2
  objects API.

Railway dashboard PITR remains the recommended *primary* mechanism (point-in-
time recovery for accidental writes is something dumps cannot do) — still a
dashboard/billing decision.

## 3. match-service deploy may be stale — RESOLVED 2026-09-06

All four services (web, gateway, match-service, platform-service) verified
deployed from current `main` via the Railway CLI (SUCCESS deployments at
2026-09-06 02:22 on commit `5812b67`). Auto-deploy has fired on every push to
`main` since 2026-09-04; keep an eye on the dashboard after pushes, but the
August failure mode has not recurred.

## 4. Moderation admin — RESOLVED 2026-10-05

`PLATFORM_ADMIN_HANDLES=lazy-to-move` is set on `platform-service` in
production (Railway variables, auto-redeployed). Handle resolution was
verified against the live DB: `account_credentials.email` join `accounts` on
`account_id` → handle `lazy-to-move`. A moderation admin now exists.

## 5. Security scan triage — DONE 2026-09-06

A fresh sealed deep scan ran 2026-09-06 (`scan-2026-09-06T00-49-28.313Z-5b852ec787cb`,
152 findings: 49 high / 103 medium) and every finding was dispositioned —
the bulk are scanner artifacts on untracked Playwright trace-viewer assets
and vendored minified Stockfish JS; the first-party findings were reviewed
individually (parameterized SQL, config-derived internal URLs, client-side
same-origin fetches). Full dispositions:
[docs/audits/2026-09-06-mimosa-scan-triage.md](../audits/2026-09-06-mimosa-scan-triage.md).
The 2026-09-02 quick pass is superseded. Note: the workspace commit hook
(`mimosa` L3) still hard-blocks commits on two of the triaged false positives
(`pytrainer/network.py`, `anticheat/stockfish.go`) and has no suppression
mechanism — commits go through the GitHub API until that is adjusted.

## 6. Per-IP rate-limit keying — VERIFIED SAFE 2026-10-04

`rate_limit.ClientIP` trusts forwarded headers on Railway (`RAILWAY_ENVIRONMENT`
auto-enables trust via `trustForwardedHeaders()`), so the live risk was spoofed
`X-Real-IP` / `X-Forwarded-For` values minting fresh per-IP limiter keys.
Verified empirically: a direct request to match-service carrying
`X-Forwarded-For: 6.6.6.6` arrived server-side as
`ip="154.110.214.204, 152.233.13.166"` — the spoof was dropped and the real
connecting IP is the first value. This matches Railway staff's documented edge
behavior (edge strips and rewrites XFF; first value = real connecting IP;
`X-Real-IP` overwritten as a single source of truth; see the employee reply in
https://station.railway.com/questions/security-critical-questions-on-edge-prox-8fddd775).
Leftmost-XFF parsing in `ClientIP` is therefore correct for Railway: no code
change needed, and no `TRUST_FORWARDED_HEADERS` variable to set (the
`RAILWAY_ENVIRONMENT` default already covers production).

## 7. Internal service token on room creation — FIXED 2026-10-04

The post-#13 soak regression (20-pair: 22/40 completed with 18 tickets stuck
queued; 100-pair: 22/200) traced to `httpMatchCreator.CreateMatch` sending
`POST /api/matches` to match-service **without** the
`X-Chess404-Service-Token` header. match-service's trusted bypass
(`GlobalIPRateLimitMiddleware(rl, internalToken)`) was wired correctly but
never saw a token to match, so every room creation counted against the
global per-IP budget of 60 req/min. #12's slow pairing spread creations out;
#13's fast pairing burst them: match-service logged 116 × 429 on
`POST /api/matches` in the 100-pair soak minute, all from the matchmaking
container IP. Each 429 made `completePairingLocked` silently roll both
tickets back to queued (no log line — a forensic blind spot, also fixed),
and the subsequent re-enqueue hit the active-ticket re-join path, which
never re-attempts pairing — stranding the pair until a later enqueue or the
queued TTL.

Fix (matchmaking-service): new `matchServiceCallerToken()` reads
match-service's accept list in precedence order
(`MATCH_INTERNAL_SERVICE_TOKEN`, `PLATFORM_INTERNAL_SERVICE_TOKEN`,
`CHESS404_INTERNAL_SERVICE_TOKEN`, `INTERNAL_SERVICE_TOKEN` — mirroring the
existing `platformServiceCallerToken()` pattern) and `httpMatchCreator` now
sets the header on every create when a token is configured. All other
service-to-service callers already sent the token; this was the one gap.
Regression tests (`match_creator_test.go`) reproduce the burst against the
real global limiter: tokened burst 100/100 OK, untokened burst 429s.
Operational note: nothing to configure — the shared token envs already set
on both services satisfy the lookup.

## 8. Burst-pairing stall after #13 — FIXED 2026-10-04 (three-part root cause)

The post-#13 soaks (20-pair: 22/40 with 18 stuck; 100-pair: 22/200) had
**three** stacked causes, each real, only the last one decisive for the
staircase:

1. **Missing service token (PR #14).** `httpMatchCreator.CreateMatch` sent
   `POST /api/matches` without `X-Chess404-Service-Token`, so room creations
   counted against match-service's global 60 req/min per-IP limit — the
   100-pair soak minute logged 116 × 429s. Every 429 rolled the pair back to
   queued **silently** (also fixed: WARN log on rollback), and the
   re-enqueue's active-ticket re-join path never re-attempts pairing →
   stranded pairs. `matchServiceCallerToken()` mirrors
   `platformServiceCallerToken()` (destination's accept list).
2. **Serialized pub/sub dial (PR #15).** `RedisBroadcaster.Subscribe` dialed
   its dedicated connection under the global `b.mu`; `CreateMatch` →
   `ensureRedisRelay` runs on every create (and hydrate). Necessary hygiene,
   but the probe was unchanged after deploying it — see the deployment note.
3. **Archive write under the global overlay lock (PR #16) — the decisive
   serializer.** `MatchArchiveStore.FlushMatch` held `persistMu` + `s.mu`
   across its single-row Postgres upsert, and that one `Exec` measures
   **~450ms** on the shared managed Postgres. Concurrent creations queued one
   archive write at a time: a production probe showed a perfect arithmetic
   staircase, 909ms → 9.7s for a burst of 20 (`+~450ms` per step), against
   matchmaking's 3s create timeout. The write loop's whole-transaction
   `persistLocked` had the mirror bug (blocks every in-memory Upsert).
   Single-row backends now snapshot under the lock and write outside it with
   a generation-guarded dirty-clear; the file backend stays atomic (its
   whole-file persist would erase siblings — caught by a test mid-development)
   and SQLite caps its pool at one connection (single-writer engine).

**Deployment lesson:** match-service is its own Railway service. The first
two verification cycles ran `railway up -s platform-service` (which ships
the platform + matchmaking binaries), so the match-service-side fixes in
#15/#16 were merged but not running, and the probe appeared to disprove the
hypothesis. Deploy the service that owns the changed binary before judging a
fix. Always-on create-path timing logs (`match create timing: create=…
archive_flush=… total=…`) now make this path observable permanently.

**Final verified state (2026-10-04):** burst-20 creates 9.7s → **1.56s**
wall, zero >2.5s; 20-pair soak **40/40 in 9.4s** (post-#12 baseline: 14.2s);
100-pair soak **200/200 in 35.3s** — the first 100-pair pass ever recorded —
0 stuck, 0 leaks, 0 claim failures, 0 × 429; e2e
`queue-handoff-pairing-window` green; lobby drained to queuedCount=0.
