# Deploy Chess404 To Railway

This repo is now prepared for a first real Railway staging deployment.

> **Deployed reality as of 2026-08-24** (free-plan topology):
> - `platform-service` is a **suite container**: it runs both `platform-service`
>   (on `$PORT`) and `matchmaking-service` (on internal port `8084`) via
>   `deploy/railway/platform-suite.Dockerfile`. Peers reach matchmaking through
>   `http://${{platform-service.RAILWAY_PRIVATE_DOMAIN}}:8084`.
> - **Redis is NOT a Railway service.** It is an external Upstash database
>   (`rediss://...@civil-fawn-99150.upstash.io:6379`). The free plan caps
>   provisioned resources, so Redis lives outside Railway.
> - Several env vars are **hard requirements enforced by `envutil.Require`** at
>   boot (`FATAL: missing required environment variables` otherwise). They were
>   missing from earlier versions of this doc; they are marked ⚠️ below.

## Recommended Railway service names

Use these exact service names so the reference variables below work as written:

- `web`
- `gateway`
- `match-service`
- `platform-service` (runs matchmaking too)
- `postgres`

External: one Upstash Redis instance shared by all services.

## Dockerfile path for each service

Keep the repository root as the source directory for every service and set the Dockerfile path per service:

- `web` -> `deploy/railway/web.Dockerfile`
- `gateway` -> `deploy/railway/gateway.Dockerfile`
- `match-service` -> `deploy/railway/match-service.Dockerfile`
- `platform-service` -> `deploy/railway/platform-suite.Dockerfile`

You can set that in Railway service settings or via the config variable:

- `RAILWAY_DOCKERFILE_PATH=deploy/railway/<file>.Dockerfile`

## Public domains

Generate public domains for:

- `web`
- `match-service`

You can leave these private:

- `gateway`
- `platform-service`
- `matchmaking-service`

Reason:

- browsers need a public URL for the Next app
- the current client also connects directly to the realtime match service for websocket updates
- the other backend services are already proxied through the web app or gateway

## Health checks

Set the Railway health check path for:

- `gateway` -> `/readyz`
- `match-service` -> `/readyz`
- `platform-service` -> `/readyz` (covers the suite; matchmaking has no
  Railway-level check but is supervised by the suite entrypoint — if it dies,
  the container exits and Railway restarts everything)

`web` can use `/`.

## Required variables

Generate one strong shared secret for service-to-service auth and set it on
every service (each binary accepts several aliases; `INTERNAL_SERVICE_TOKEN`
works everywhere):

```env
INTERNAL_SERVICE_TOKEN=<openssl rand -hex 32>
```

`ALLOWED_ORIGINS` is a comma-separated CORS origin list — at minimum the public
`web` domain:

```env
ALLOWED_ORIGINS=https://<web-domain>
```

### ⚠️ Boot-fatal requirements per binary

- `match-service` requires: `PLATFORM_SERVICE_INTERNAL_URL`, `ALLOWED_ORIGINS`,
  `INTERNAL_SERVICE_TOKEN`, and either `MATCH_REDIS_URL` or
  `MATCH_STATE_BACKEND=memory`
- `gateway` requires: the three `*_INTERNAL_URL`s + `ALLOWED_ORIGINS`
- `platform-service` requires: `ALLOWED_ORIGINS`

## `web`

```env
NODE_ENV=production
NEXT_PUBLIC_GATEWAY_URL=/api/gateway
NEXT_PUBLIC_PLATFORM_SERVICE_URL=/api/platform
NEXT_PUBLIC_MATCHMAKING_SERVICE_URL=/api/matchmaking
NEXT_PUBLIC_MATCH_SERVICE_HTTP_BASE=https://${{match-service.RAILWAY_PUBLIC_DOMAIN}}/api
NEXT_PUBLIC_MATCH_SERVICE_WS_URL=wss://${{match-service.RAILWAY_PUBLIC_DOMAIN}}
GATEWAY_INTERNAL_URL=http://${{gateway.RAILWAY_PRIVATE_DOMAIN}}:${{gateway.PORT}}
MATCH_SERVICE_INTERNAL_URL=http://${{match-service.RAILWAY_PRIVATE_DOMAIN}}:${{match-service.PORT}}
PLATFORM_SERVICE_INTERNAL_URL=http://${{platform-service.RAILWAY_PRIVATE_DOMAIN}}:${{platform-service.PORT}}
MATCHMAKING_SERVICE_INTERNAL_URL=http://${{platform-service.RAILWAY_PRIVATE_DOMAIN}}:8084
INTERNAL_SERVICE_TOKEN=<shared-secret>
GATEWAY_INTERNAL_SERVICE_TOKEN=<shared-secret>
PLATFORM_INTERNAL_SERVICE_TOKEN=<shared-secret>
```

Note: the `NEXT_PUBLIC_*` values are **Next.js build-time** variables. Set them
(and generate the match-service public domain) BEFORE web's first build, or the
client will bake in empty strings.

## `gateway`

```env
MATCH_SERVICE_INTERNAL_URL=http://${{match-service.RAILWAY_PRIVATE_DOMAIN}}:${{match-service.PORT}}
PLATFORM_SERVICE_INTERNAL_URL=http://${{platform-service.RAILWAY_PRIVATE_DOMAIN}}:${{platform-service.PORT}}
MATCHMAKING_SERVICE_INTERNAL_URL=http://${{platform-service.RAILWAY_PRIVATE_DOMAIN}}:8084
ALLOWED_ORIGINS=https://<web-domain>
GATEWAY_INTERNAL_SERVICE_TOKEN=<shared-secret>
INTERNAL_SERVICE_TOKEN=<shared-secret>
```

## `match-service`

```env
MATCH_ARCHIVE_BACKEND=postgres
MATCH_ARCHIVE_POSTGRES_URL=${{postgres.DATABASE_URL}}
MATCH_STATE_BACKEND=redis
MATCH_REDIS_URL=rediss://default:<upstash-password>@<upstash-host>:6379
PLATFORM_SERVICE_INTERNAL_URL=http://${{platform-service.RAILWAY_PRIVATE_DOMAIN}}:${{platform-service.PORT}}
ALLOWED_ORIGINS=https://<web-domain>
INTERNAL_SERVICE_TOKEN=<shared-secret>
```

## `platform-service` (suite: platform + matchmaking)

```env
# suite wiring
MATCHMAKING_ADDR=0.0.0.0:8084

# platform stores -> Postgres
MATCH_ARCHIVE_BACKEND=postgres
MATCH_ARCHIVE_POSTGRES_URL=${{postgres.DATABASE_URL}}
GUEST_STORE_BACKEND=postgres
ACCOUNT_STORE_BACKEND=postgres
FRIEND_STORE_BACKEND=postgres
DIRECT_CHALLENGE_STORE_BACKEND=postgres
PLATFORM_POSTGRES_URL=${{postgres.DATABASE_URL}}

# platform match claims -> Upstash Redis
MATCH_CLAIM_STORE_BACKEND=redis
MATCH_CLAIM_STORE_REDIS_URL=rediss://default:<upstash-password>@<upstash-host>:6379
MATCH_CLAIM_STORE_TTL_SECONDS=43200

# matchmaking ticket store -> Upstash Redis
MATCHMAKING_TICKET_STORE_BACKEND=redis
MATCHMAKING_TICKET_STORE_REDIS_URL=rediss://default:<upstash-password>@<upstash-host>:6379

ALLOWED_ORIGINS=https://<web-domain>
INTERNAL_SERVICE_TOKEN=<shared-secret>
```

## First staging checklist

After the first deploy:

1. Open the `web` public domain.
2. Check the `Status` page in the app.
3. Verify all backend services are healthy.
4. Create guest sessions.
5. Start a fresh match.
6. Verify:
   - moves sync live
   - cards preview correctly
   - abort works early
   - round draws occur on the correct schedule
   - queue can create a room
   - history/rankings/account pages load

## Important note

This is a staging-ready deployment path, not the final production-hardening pass.

Still recommended after first successful staging deploy:

- move match-service browser traffic behind a cleaner gateway/proxy shape
- add Railway environments for `staging` and `production`
- add custom domains
- add monitoring and alerting
- add load testing before public launch

## Rated beta launch checklist

Use this checklist before calling the hosted `/play` flow launch-ready.

1. Verify Railway service health:
   - `web` loads its public domain
   - `gateway` returns `200` from `/readyz`
   - `match-service` returns `200` from `/readyz`
   - `platform-service` returns `200` from `/readyz`
   - matchmaking is healthy if gateway `/status` reports it (no direct Railway check)
2. Verify backend wiring from the web app:
   - open `/status`
   - confirm gateway, platform, match, and matchmaking all report healthy
   - confirm `/api/gateway/bootstrap` returns healthy downstream state instead of localhost connection errors
3. Verify data backends:
   - Upstash Redis credentials are wired into `platform-service` (suite) and `match-service`
   - Railway Postgres is attached to `platform-service` and `match-service`
4. Run the hosted `/play` smoke test:
   - signed-out player can join casual
   - signed-out player sees `Sign in to join rated`
   - signed-in player can join rated
   - queued refresh restores queue state
   - matched refresh restores the live match or shows `Return to Match`
   - private invite link opens the canonical `/match/:id` route
   - a fully occupied invite room resolves to spectate or room-full behavior

## Beta data reset before launch

If production beta data becomes noisy, clear it before the public rated beta push so recovery logic is restoring real player state instead of test leftovers.

### Redis

Reset stale queue and claim state:

```text
chess404:matchmaking:tickets
chess404:platform:match-claims
```

### Postgres

Review and clear test-only rows from:

```sql
guests
finalized_matches
direct_challenges
```

Suggested approach:

1. Remove stale queue tickets and stale match claims first.
2. Delete or archive test guest rows only if you intentionally want a clean guest ladder.
3. Delete abandoned direct challenges that were created for testing and no longer map to a live product scenario.
4. Redeploy the services after cleanup so fresh hosted sessions repopulate state from a known baseline.

Do not clear production data blindly once real players are using the platform. Snapshot Postgres first, then remove only the stale beta-era rows you intend to reset.
