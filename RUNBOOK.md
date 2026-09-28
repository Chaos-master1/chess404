# Chess404 Runbook (live checklist, 2026-08-30)

## Railway topology

- **Project:** `chess404` (`ecb0135d-84ac-48b8-b1ff-75191dda030f`, env `production` `5ddaedf0-d11e-4cc7-9f31-3014083c8e65`)
- **Services (5, 1 replica each):** `web` (`web-production-1caefb.up.railway.app`, Next.js 15), `gateway` (internal, via `web` → `/api/gateway/*`), `match-service` (`wss://match-service-production.up.railway.app`, ws + `/api/matches/*`), `platform-service` (also serves `/api/matchmaking/*`), `Postgres` (`postgres-ssl:18`).
- **Public domains:** only `web` and `match-service`. Do not generate extra domains unless you intend to expose another service.

## What to check before you deploy

```bash
# from repo root
pnpm lint                          # turbo lint (contracts, game-core, web)
pnpm test && pnpm build             # includes the web Vitest suite and production bundle
export PATH=$PATH:/home/Houssem/sdk/go1.25.6/bin
(cd services/realtime && go vet ./... && go test ./... -count=1 -timeout 300s)
(cd services/realtime && go test -race ./internal/match -count=1)  # catches the Close-vs-Upsert race
```

Do not count a Playwright run against the current public Railway URL as
pre-deploy proof for a new candidate—it exercises the old release. Run the
live scenarios below only after Railway reports the intended commit as
successfully deployed.

## How to deploy (Railway has no auto-deploy)

After pushing to `main`, Railway does not necessarily build. Check and trigger:

```bash
# via MCP or dashboard:
# list-services → get-status → list-deployments (look for branch main, status SUCCESS, recent timestamp)
# if a service is still on an old commit (e.g. a8ae5bc vs main 5913cec), re-point it:
# railway connect_service_source(projectId, serviceId, repo="Chaos-master1/chess404", branch="main")
```

Every service should converge on the same `main` commit within a few minutes. Watch its build log; a second `connect` after a transient `FAILED` usually succeeds.

## Mandatory live gate after deploy

```bash
BASE=https://web-production-1caefb.up.railway.app
curl -sS $BASE/api/gateway/healthz          | jq .      # {"service":"gateway","status":"ok"}
curl -sS $BASE/api/platform/status          | jq '.service, .archive.totalMatches, .archive.activeMatches'
curl -sS $BASE/api/matchmaking/status       | jq '.service, .stats.backend, .stats.totalTickets'
curl -sS https://match-service-production.up.railway.app/healthz | jq .  # match-service direct
# CSP/headers
curl -sS -D - $BASE/ -o /dev/null | grep -i -E 'content-security|strict-transport|x-frame|referrer|permissions'
# rate limit sanity: 80 proxied calls should be zero 429s (see e2067ac fix)
for i in $(seq 1 80); do curl -sS -X POST $BASE/api/gateway/bootstrap -H 'content-type: application/json' -d '{}' -o /dev/null -w '%{http_code}\n' & done | sort | uniq -c
```

On the client, open the live site, create a **vs computer** match (Play → Beginner) and confirm:
- board (`data-testid="board-root"`) appears within ~5 s,
- `e2-e4` → engine replies within 15 s,
- reload → board + `Resign` button reappear (reconnect test),
- `/watch` → no private/computer rooms are listed (spectate privacy).

Then run the release-critical flows serially against the deployed Railway URL:

```bash
BASE=https://web-production-1caefb.up.railway.app
for f in e2e/solo.spec.ts e2e/private-invite.spec.ts e2e/reconnect.spec.ts e2e/history-replay.spec.ts; do
  E2E_BASE_URL="$BASE" timeout 300 pnpm exec playwright test "$f" --reporter=line || exit 1
done
```

The minimum launch evidence is: a computer player can make its first move;
a cold invitee receives and can use the open private seat; a reload/offline
reconnect remains playable; and a finished game appears in history with a
replay frame. Any failure is a release blocker, even if health probes are 200.

## Logs to tail during deploy

Via MCP `get-logs` (or dashboard Logs tab), filter to `deploy` stream for `match-service` and `gateway`:
- `match:create: starting` / `ok` — match creation path.
- `gw:create-private: match created matchID=...` — private/computer creation via gateway.
- `gw:sync-match: ok` / `connection error to platform-service` — archive dual-write.
- Any `panic: assignment to entry in nil map` from `history.go:235` — regressed the race fix in audit 2026-08-30.

## Rolling back

Railway keeps prior builds. In dashboard → service → Deployments → pick the previous `SUCCESS` (e.g. `226ee5a3` for web, `994a84db` for platform-service) → Redeploy. No DB migration to reverse (archival is idempotent).

## Secrets & env

- `INTERNAL_SERVICE_TOKEN` / `GATEWAY_INTERNAL_SERVICE_TOKEN` / `PLATFORM_INTERNAL_SERVICE_TOKEN` — single resolver in `apps/web/app/api/_lib/internal-service.ts`. If you add a new internal hop, use the same resolver (not a per-proxy copy).
- `MATCH_REDIS_URL` / `MATCH_ARCHIVE_POSTGRES_URL` / `PLATFORM_POSTGRES_URL` — `file|sqlite|postgres|redis` per-store. Production is `postgres` (archive) + `redis` (claims/tickets). Free-tier Redis has a monthly `db_request_limit` — avoid new unconditional Redis polls.
- Never log `playerSecret`/`claimToken`/`sessionSecret` — all paths redact (`RedactSnapshotSecrets`, `redactToken`).

## When to run the full audit again

After any change to `services/realtime/internal/match` (state machine), `services/realtime/internal/platform` (stores), or `apps/web/app/api/*/_lib/*` (proxies), re-run `go test -race ./internal/match` and the `pages-smoke` spec against live before merging.

## Internal service tokens: per-service migration (4.1)

Every backend service accepts the legacy **shared** internal token
(`CHESS404_INTERNAL_SERVICE_TOKEN` / `INTERNAL_SERVICE_TOKEN`) and now also a
**service-specific** token, checked first by each resolver:

| Service | Specific env (preferred) | Also accepts (fallback) |
| --- | --- | --- |
| gateway | `GATEWAY_INTERNAL_SERVICE_TOKEN` | `PLATFORM_INTERNAL_SERVICE_TOKEN`, `CHESS404_INTERNAL_SERVICE_TOKEN`, `INTERNAL_SERVICE_TOKEN` |
| match-service | `MATCH_INTERNAL_SERVICE_TOKEN` | `PLATFORM_INTERNAL_SERVICE_TOKEN`, `CHESS404_INTERNAL_SERVICE_TOKEN`, `INTERNAL_SERVICE_TOKEN` |
| platform-service | `PLATFORM_INTERNAL_SERVICE_TOKEN` | `CHESS404_INTERNAL_SERVICE_TOKEN`, `INTERNAL_SERVICE_TOKEN` |
| matchmaking-service | `MATCHMAKING_INTERNAL_SERVICE_TOKEN` | `PLATFORM_INTERNAL_SERVICE_TOKEN`, `CHESS404_INTERNAL_SERVICE_TOKEN`, `INTERNAL_SERVICE_TOKEN` |
| web (proxy) | sends the first set of: `MATCH_INTERNAL_SERVICE_TOKEN`, `GATEWAY_INTERNAL_SERVICE_TOKEN`, `PLATFORM_INTERNAL_SERVICE_TOKEN`, `CHESS404_INTERNAL_SERVICE_TOKEN`, `INTERNAL_SERVICE_TOKEN` | resolver: `apps/web/app/api/_lib/internal-service.ts` |

Why: with one shared value, a single leaked proxy route inherits
internal-caller trust against *every* service. Each service now boot-logs a
`[security]` warning when it has no specific token or when a shared env holds
the same value as its specific one — watch for these lines during deploy.

**Important caller constraint before you rotate anything:** the web proxy and
the gateway each send *one* token value to *all* backends (their resolvers are
not per-target yet), and each backend compares against a single expected
value. So today you cannot give two callers of the same backend different
tokens, and full per-service isolation additionally requires a small code
change: per-target token injection in `buildUpstreamHeaders` (web) and in the
gateway's outbound request builder. Until that lands, the staged env-only
migration below is still worth doing — it stages distinct credentials and
removes reliance on the *name* `INTERNAL_SERVICE_TOKEN` — but the shared
value must keep working for the multi-caller backends (platform,
matchmaking, match).

### Migration checklist

1. **Inventory** current values per service (Railway → service → Variables).
   Note which services share the same token value today (boot warnings list
   the colliding env names).
2. **Stage specific envs** (do not remove shared ones yet):
   `openssl rand -hex 32` once per service; set
   `GATEWAY_INTERNAL_SERVICE_TOKEN` / `MATCH_INTERNAL_SERVICE_TOKEN` /
   `PLATFORM_INTERNAL_SERVICE_TOKEN` / `MATCHMAKING_INTERNAL_SERVICE_TOKEN`
   on their services, and on **web** set the same *value* the corresponding
   callee expects — because web sends one value everywhere, all of
   gateway/match/platform/matchmaking must accept web's value during this
   stage (via their shared fallbacks).
3. **Redeploy all five services**, then run the mandatory live gate below.
   Every queue/match/bootstrap flow must still pass; a 401/403 storm means a
   caller/callee pair disagree on the value.
4. **Per-caller isolation (code work, not yet done):** add per-target token
   selection to the web proxy (`buildUpstreamHeaders`) and the gateway
   outbound builder, then give web its own token, distinct from gateway's.
   After this lands, backends can drop the shared fallbacks one service at a
   time (start with matchmaking — two callers only).
5. **Retire the shared envs** last, after the boot warnings show no service
   relies on them. Keep `INTERNAL_SERVICE_TOKEN` out of any new docs.

## Owner operations checklist (4.3)

Things only the account owner can do from the Railway/Upstash/email-provider
dashboards — the code cannot do them for you:

- **Postgres backups / PITR.** Production Postgres is the Railway plugin
  (`postgres-ssl:18`); `PLATFORM_POSTGRES_URL` and
  `MATCH_ARCHIVE_POSTGRES_URL` both point at it. Losing it loses accounts,
  ratings, and match history. Dashboard → Postgres service → **Backups**:
  enable scheduled backups and, if your plan includes it, point-in-time
  recovery; verify a restore once into a throwaway instance before you need
  it. Matchmaking tickets live in Upstash Redis (`MATCH_REDIS_URL`), not
  Postgres — check Upstash's own backup/eviction settings and remember the
  free tier's monthly `db_request_limit`.
- **SMTP provider.** Account verification/password email is sent by
  platform-service over SMTP: `ACCOUNT_EMAIL_PROVIDER=smtp` plus
  `ACCOUNT_EMAIL_SMTP_ADDRESS` (host:port), `ACCOUNT_EMAIL_SMTP_FROM`,
  `ACCOUNT_EMAIL_SMTP_USERNAME`, `ACCOUNT_EMAIL_SMTP_PASSWORD`,
  `ACCOUNT_EMAIL_SMTP_TLS=true` (hard-required for any non-loopback host —
  the service refuses to start delivery otherwise),
  `ACCOUNT_EMAIL_SMTP_FROM_NAME`, `ACCOUNT_EMAIL_SMTP_REPLY_TO`,
  `ACCOUNT_EMAIL_SMTP_MESSAGE_DOMAIN`. Use a transactional provider's SMTP
  relay (Resend/Postmark/Mailgun) and verify the sender domain (SPF/DKIM) or
  every mail lands in spam. If delivery init fails, deploy logs show
  `failed to initialize account email delivery`.
- **Moderation admin access.** Set `PLATFORM_ADMIN_HANDLES` (comma/space
  separated handles) and/or `PLATFORM_ADMIN_ACCOUNT_IDS` on platform-service.
  The capabilities endpoint drives whether the admin UI renders; both envs
  authorize actions. Put at least one owner account in that set before
  launch — an empty set means nobody can resolve reports or bans.
- **Re-run the deploy gates** in "What to check before you deploy" and the
  "Mandatory live gate" above after touching any of the above.

## Session hardening shipped (stage 4, 2026-09)

Per-target internal tokens (4.1 step 4) are DONE: the web proxy now picks the
token per callee (`internalServiceTokenForTarget` in
`apps/web/app/api/_lib/internal-service.ts`, used by `buildUpstreamHeaders`),
and the gateway picks per-target tokens in its outbound builder. Each backend
matches its own specific env first, then the shared fallbacks. The "one value
everywhere" caller constraint above is gone; retiring shared fallbacks one
service at a time (start with matchmaking) is now purely an env operation.

Also shipped this stage:

- **Cookie session resume.** The gateway folds `session_secret_{white|black}`
  and `session_guest_{side}` cookies into the bootstrap request
  (`foldSessionCookieIdentities` in `gateway_mux.go`, BEFORE the payload is
  built). JSON credentials in the request body still win per-field. Web
  dual-writes both cookie shapes per seat (`buildSessionSecretCookies`).
- **Rated is account-only, enforced server-side at every entry point:**
  gateway private create (403 without an account session), private join
  (gated on the target room's stored queue; uninspectable rooms fail through
  so real upstream errors surface), rematch, matchmaking enqueue (client
  gate + `CHESS404_PUBLIC_BETA_READY` capability), and the trusted
  finalizer (platform rejects rated archival unless BOTH guests hold
  accounts). Tests: `TestGatewayRatedPrivateMatchRequiresAccount`,
  `TestGatewayCasualPrivateMatchStillAllowsGuests`.
- **Name policy.** Random generated names are display fallbacks only. The
  server renames a linked guest to the account handle at claim/register/
  login (`renameLinkedGuestToHandle`); the web mirrors
  `account.handle` into the local profile at bootstrap. Guests render as
  "Anonymous" rather than a generated name. NOTE: matches created before
  this deploy keep their stored `whiteName`/`blackName` — the fix shows on
  new matches or after re-claim.
- **Unrated ladders never display a borrowed number.** Account/Profiles mode
  tiles show an em dash ("no rated games yet") until that specific mode has
  rated games; previously they displayed the blended Elo, which moved when
  unrelated games landed. Elo itself only ever moves for rated games between
  two accounts (verified through the finalize pipeline).
- **Gameplay/UI fixes.** Shielded pieces are movable (shield only absorbs a
  capture; moving drops it); card hand re-select drops the card on touch
  (selection raise is style-driven, hover skipped while selected); chat
  auto-scroll is container-scoped so incoming chat no longer yanks the match
  page on phones; card hand fan clamps to container width (full 10-card hand
  no longer overlaps board/panels on narrow laptops); computer mode removed
  from queue/invite/challenge pickers (vs-computer lives in its own Play-hub
  section); Inbox feed embedded in Friends (single Social nav entry with a
  combined unread badge; /inbox stays as a deep link).
