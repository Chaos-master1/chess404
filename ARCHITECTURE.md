# Chess404 Architecture

## Overview

Chess404 is a chess + cards platform with a server-authoritative architecture. The backend enforces all game rules, card effects, and clock management. The frontend is a thin rendering layer.

## Monorepo Structure

```
chess404/
├── apps/web/              # Next.js frontend (public surface, proxies to gateway)
├── packages/
│   ├── contracts/         # Shared TypeScript types
│   └── game-core/         # Card pool, rules, RNG
├── services/realtime/
│   ├── cmd/
│   │   ├── gateway/               # HTTP reverse proxy, auth, CORS (public via web)
│   │   ├── match-service/         # WebSocket host, game state (public WS)
│   │   ├── platform-service/      # Accounts, guests, history + queue ticketing*
│   │   ├── matchmaking-service/   # Queue binary (code exists, not a separate Railway service*)
│   │   ├── replay-worker/         # Async replay archival (planned, not deployed)
│   │   └── migrate/               # DB migrations
│   └── internal/
│       ├── contracts/             # Go domain types
│       ├── match/                 # Game engine, state machine (71.9% cover)
│       ├── engine/                # Custom chess engine (eval, search, AI; search 90.2% cover)
│       ├── platform/              # Postgres stores (37.8% cover)
│       ├── matchmaking/           # Queue logic (79.2% cover, now served via platform-service)
│       ├── anticheat/             # Irwin + replay analysis (68.3% cover)
│       ├── httputil/              # CORS, CSRF, retry, circuit breaker (6.4% cover)
│       ├── metrics/               # Prometheus /metrics
│       ├── logging/               # Structured slog JSON
│       └── rate_limit/            # Per-IP and per-player limits (50.4% cover)
│       # Note: messaging/, sharding/, featureflags/, tracing/ are library stubs
│       # or future work — not separate services in production.
└── deploy/
    ├── railway/           # Dockerfiles for all services
    └── grafana/           # Dashboard JSON (not deployed)
```
* `platform-service` in production serves both `platform` and `matchmaking` logic to stay within Railway free-tier service limits (5 services: web, gateway, match-service, platform-service, Postgres). The `matchmaking-service` binary exists for future scale-out but is not a separate Railway service.

Live Railway topology (2026-08-30): `web` (public), `gateway` (internal, via web `/api/gateway/*`), `match-service` (`wss://match-service-production.up.railway.app`), `platform-service` (also serves `/api/matchmaking/*` and `/api/platform/*`), `Postgres`.

## Service Architecture (production, 2026-08-30)

```
Browser ──► web (Next.js, public) ──► gateway (internal, via web /api/gateway/*)
                                    ├── match-service (WebSocket wss://match-service-production.up.railway.app + HTTP internal)
                                    └── platform-service ──┬── /api/platform/* (accounts, guests, history, Postgres)
                                                            └── /api/matchmaking/* (queue tickets, Redis) [* merged ]
```

- **Gateway**: Proxied through web (`/api/gateway/*`). Auth, CORS, CSRF, rate limiting, service token injection. Single logical entry point; not a public domain (traffic enters via `web-production-1caefb.up.railway.app`).
- **Match Service**: Hosts live games via WebSocket (`/api/matches/{id}/ws`). Server-authoritative state machine. 37 card effects resolved server-side. Custom chess engine for computer opponent. Public WS at `wss://match-service-production.up.railway.app`.
- **Platform Service**: Guest accounts, registered accounts, match history (Postgres archive, 97 matches live), rankings, friendships, notifications, moderation. In production also serves matchmaking queue endpoints (`/api/matchmaking/*`) via shared Redis.
- **Matchmaking Service**: Queue ticketing, Elo-based matching, direct challenges. Binary exists in `services/realtime/cmd/matchmaking-service/` but is **merged into `platform-service` in production** (free-tier service cap).
- **Replay Worker**: Async archival of finished matches. Code exists (`services/realtime/cmd/replay-worker/`), not deployed as a separate service — archival is done inline by `platform-service`.

## Game Engine

The match service contains a full chess engine (`internal/engine/`) with:

- **Evaluation**: Piece-square tables, material, positional bonuses, king safety
- **Search**: Alpha-beta with iterative deepening, transposition table, move ordering
- **Card evaluation**: Scores all 37 card mechanics based on board state
- **Computer opponent**: 5 difficulty levels (Beginner=2ply through Expert=8ply)

## Card System

37 unique cards with mechanics: freeze, shield, sniper, heal, swap, promote, demote, clone, teleport, jump, borrow, mindcontrol, parasite, lava, invisible, bomb, fog, fortress, doublemove, reverse, undo, mirror, fakepiece, blackhole, sacrifice, gambler, radar, cheater, joker.

Card lifecycle: pool → hand (1 per round) → play → resolve. Server validates and resolves all effects.

## Data Flow

1. Client sends intent via WebSocket (`wss://match-service-production.up.railway.app/api/matches/{id}/ws`) or HTTP (`POST /api/gateway/matches/{id}/intents` → gateway → match-service)
2. Gateway (or direct WS) authenticates via `X-Chess404-Service-Token` + per-seat `X-Player-ID`/`X-Player-Secret` or claim token; CSRF validated via `Origin`/`X-Forwarded-*`
3. Match-service validates intent against game state (`match.Service.ApplyIntent`, seqNum staleness check)
4. State machine applies move/card/effect (37 mechanics, `match` 71.9% cover, `engine/search` 90.2% cover)
5. New state broadcast to all connected clients (Redis Pub/Sub if `MATCH_REDIS_URL` set, else in-memory)
6. State dual-written to memory + Redis (if configured) + Postgres archive via `finalizingArchiveStore`
7. Archive: `platform.MatchArchiveStore` dual-writes to memory + Postgres (`MATCH_ARCHIVE_POSTGRES_URL`), with lazy DB queries for Postgres backend (`useQueries=true`). No NATS in production — Redis Pub/Sub only.

## Infrastructure (2026-08-30 verified)

- **State storage**: In-memory + Redis (`MATCH_REDIS_URL`, `MatchStore` + `Broadcaster`) with Postgres archive (`MATCH_ARCHIVE_POSTGRES_URL`, `MatchArchiveStore`). `platform-service` and `match-service` share Postgres + Redis.
- **Cross-instance**: Redis Pub/Sub for WebSocket broadcast (if `MATCH_REDIS_URL` set). No NATS in production.
- **Sharding**: Consistent hash ring code exists but is not wired as a separate service; match-service handles all matches on one instance (verified single replica per service).
- **Observability**: `slog` JSON logs (verified on all 5 services), Prometheus `/metrics` (wired but no Grafana deployed), Sentry (`apps/web/sentry.*.config.ts` + `instrumentation.ts`), OpenTelemetry stub (not collected). Moderate coverage on logging/metrics (6–50%).
- **Deployment**: Docker multi-stage builds, Railway hosting (5 services, 1 Postgres replica), no GitHub Actions CI/CD file found (deploy is manual via `railway up` or git push). `turbo` for local builds.
- **Feature flags**: `contracts` platform caps (`/api/platform/capabilities`) with per-cap boolean toggles; `platform-service` serves them from Postgres.

## Security

- Server-authoritative: no client trust for game state
- CSRF protection via double-submit cookie pattern
- CORS with explicit origin allow-list
- Rate limiting: 60/min API, 10/sec per-player intents
- Auth tokens: cryptographic random (SHA-256 fallback)
- CSP headers evaluated at request time (not build time)
- Credentials redacted from all log output
