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
