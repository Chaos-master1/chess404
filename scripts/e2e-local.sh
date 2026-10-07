#!/usr/bin/env bash
# Local E2E stack: starts postgres+redis (docker) and the four Go services
# (native binaries, built via the cached golang:1.25 toolchain) with an env
# wiring that mirrors production topology. The web app runs via `next dev`
# (started separately by the caller or by this script with --web).
#
# Usage:
#   scripts/e2e-local.sh build    # compile Go binaries into services/realtime/bin
#   scripts/e2e-local.sh start    # start infra + services (+ web by default)
#   scripts/e2e-local.sh stop     # stop everything this script started
#   scripts/e2e-local.sh status   # health summary
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT/services/realtime/bin"
LOGS=/tmp/chess404-e2e-logs
# Store URLs are overridable: on hosts where docker's host->container port
# forwarding is broken (userland proxy resets connections), point these at
# host-networked containers instead, e.g.
#   E2E_PG_URL=postgres://test:test@127.0.0.1:5543/chess404_e2e?sslmode=disable \
#   E2E_REDIS_URL=redis://127.0.0.1:6390/0
PGURL="${E2E_PG_URL:-postgres://test:test@127.0.0.1:55432/chess404_e2e?sslmode=disable}"
REDIS="${E2E_REDIS_URL:-redis://127.0.0.1:6379/0}"
TOKEN="local-e2e-token"
# Hosted-runtime detection on the web client keys off the page hostname not
# being localhost/127.0.0.1, so E2E runs hit the app over the LAN IP. Backends
# validate the browser Origin header (CORS + WS CheckOrigin + CSRF) against
# this allow-list, hence the LAN origin must be included here.
E2E_LAN_ORIGIN="${E2E_LAN_ORIGIN:-}"
if [ -z "$E2E_LAN_ORIGIN" ]; then
  for CAND in "$(hostname -I 2>/dev/null)"; do
    E2E_LAN_ORIGIN="$(echo "$CAND" | tr ' ' '\n' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' | head -1)"
  done
fi
ALLOWED="http://localhost:3000,http://127.0.0.1:3000"
if [ -n "$E2E_LAN_ORIGIN" ]; then
  ALLOWED="$ALLOWED,http://$E2E_LAN_ORIGIN:3000"
fi
PIDS=()

mkdir -p "$LOGS"

build() {
  docker run --rm -v "$ROOT/services/realtime":/app -v chess404-gocache:/go \
    -w /app -e GOFLAGS=-mod=mod -e CGO_ENABLED=0 golang:1.25-bookworm \
    sh -c "go build -o bin/match-service ./cmd/match-service && \
           go build -o bin/gateway ./cmd/gateway && \
           go build -o bin/platform-service ./cmd/platform-service && \
           go build -o bin/matchmaking-service ./cmd/matchmaking-service" \
    || { echo "build failed"; return 1; }
  echo "binaries in $BIN"
}

start_infra() {
  docker start chess404-e2e-postgres >/dev/null 2>&1 || \
    docker run -d --name chess404-e2e-postgres -e POSTGRES_USER=test \
      -e POSTGRES_PASSWORD=test -e POSTGRES_DB=chess404_e2e -p 55432:5432 \
      postgres:16-alpine >/dev/null
  docker compose -f "$ROOT/deploy/docker-compose.integration.yml" up -d redis >/dev/null 2>&1 || \
    docker run -d --name chess404-e2e-redis -p 6379:6379 redis:7-alpine >/dev/null
  for i in $(seq 1 30); do
    docker exec chess404-e2e-postgres pg_isready -U test -d chess404_e2e >/dev/null 2>&1 && break
    sleep 1
  done
  echo "infra ready"
}

start_service() {
  local name="$1"; shift
  # setsid fully detaches the service from this script's process group so it
  # survives the launcher (and any parent shell) exiting.
  setsid "$@" >"$LOGS/$name.log" 2>&1 < /dev/null &
  PIDS+=("$!")
}

start_backends() {
  start_service match-service env \
    MATCH_SERVICE_ADDR=:8082 INTERNAL_SERVICE_TOKEN=$TOKEN \
    PLATFORM_SERVICE_INTERNAL_URL=http://127.0.0.1:8083 \
    MATCH_STATE_BACKEND=memory \
    MATCH_ARCHIVE_BACKEND=postgres MATCH_ARCHIVE_POSTGRES_URL="$PGURL" \
    COMPUTER_OPPONENT=search \
    ALLOWED_ORIGINS="$ALLOWED" \
    "$BIN/match-service"

  start_service platform-service env \
    PLATFORM_ADDR=:8083 INTERNAL_SERVICE_TOKEN=$TOKEN \
    MATCH_SERVICE_INTERNAL_URL=http://127.0.0.1:8082 \
    MATCHMAKING_SERVICE_INTERNAL_URL=http://127.0.0.1:8084 \
    MATCH_ARCHIVE_BACKEND=postgres MATCH_ARCHIVE_POSTGRES_URL="$PGURL" \
    GUEST_STORE_BACKEND=postgres GUEST_STORE_POSTGRES_URL="$PGURL" \
    ACCOUNT_STORE_BACKEND=postgres ACCOUNT_STORE_POSTGRES_URL="$PGURL" \
    MATCH_CLAIM_STORE_BACKEND=redis MATCH_CLAIM_STORE_REDIS_URL="$REDIS" \
    MATCHMAKING_TICKET_STORE_BACKEND=redis MATCHMAKING_TICKET_STORE_REDIS_URL="$REDIS" \
    RATE_LIMIT_BACKEND=memory \
    ALLOWED_ORIGINS="$ALLOWED" \
    "$BIN/platform-service"

  start_service matchmaking-service env \
    MATCHMAKING_ADDR=:8084 INTERNAL_SERVICE_TOKEN=$TOKEN \
    MATCH_SERVICE_INTERNAL_URL=http://127.0.0.1:8082 \
    PLATFORM_SERVICE_INTERNAL_URL=http://127.0.0.1:8083 \
    MATCHMAKING_TICKET_STORE_BACKEND=redis MATCHMAKING_TICKET_STORE_REDIS_URL="$REDIS" \
    RATE_LIMIT_BACKEND=memory \
    ALLOWED_ORIGINS="$ALLOWED" \
    "$BIN/matchmaking-service"

  start_service gateway env \
    GATEWAY_ADDR=:8090 INTERNAL_SERVICE_TOKEN=$TOKEN \
    MATCH_SERVICE_INTERNAL_URL=http://127.0.0.1:8082 \
    PLATFORM_SERVICE_INTERNAL_URL=http://127.0.0.1:8083 \
    MATCHMAKING_SERVICE_INTERNAL_URL=http://127.0.0.1:8084 \
    ALLOWED_ORIGINS="$ALLOWED" \
    "$BIN/gateway"
}

start_web() {
  # Process env beats .env.local in Next.js, so passing the WS URL here
  # overrides stale NEXT_PUBLIC_* values in the developer's .env.local and
  # keeps the generated CSP connect-src allow-list correct for this stack.
  # match-service HTTP stays on the same-origin /api/realtime proxy; only the
  # websocket goes direct to the match-service listener (port 8082 is the
  # public WS entry in the production topology too).
  local WS_ORIGIN="ws://127.0.0.1:8082"
  if [ -n "$E2E_LAN_ORIGIN" ]; then WS_ORIGIN="ws://$E2E_LAN_ORIGIN:8082"; fi
  start_service web env \
    GATEWAY_INTERNAL_URL=http://127.0.0.1:8090 \
    MATCH_SERVICE_INTERNAL_URL=http://127.0.0.1:8082 \
    PLATFORM_SERVICE_INTERNAL_URL=http://127.0.0.1:8083 \
    MATCHMAKING_SERVICE_INTERNAL_URL=http://127.0.0.1:8084 \
    INTERNAL_SERVICE_TOKEN=$TOKEN \
    NEXT_PUBLIC_MATCH_SERVICE_WS_URL="$WS_ORIGIN" \
    sh -c "cd '$ROOT/apps/web' && exec npx next dev --port 3000"
}

wait_healthy() {
  local url="$1" name="$2"
  for i in $(seq 1 40); do
    curl -sf "$url" >/dev/null 2>&1 && { echo "$name up"; return 0; }
    sleep 1
  done
  echo "$name FAILED to become healthy ($url)"; return 1
}

status() {
  for u in "match-service http://127.0.0.1:8082/healthz" \
           "platform http://127.0.0.1:8083/healthz" \
           "matchmaking http://127.0.0.1:8084/healthz" \
           "gateway http://127.0.0.1:8090/healthz" \
           "web http://127.0.0.1:3000/"; do
    set -- $u
    printf "%-14s %s\n" "$1" "$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "$2")"
  done
}

case "${1:-start}" in
  build) build ;;
  start)
    start_infra
    start_backends
    [ "${2:-}" = "--no-web" ] || start_web
    wait_healthy http://127.0.0.1:8082/healthz match-service
    wait_healthy http://127.0.0.1:8083/healthz platform-service
    wait_healthy http://127.0.0.1:8084/healthz matchmaking-service
    wait_healthy http://127.0.0.1:8090/healthz gateway
    [ "${2:-}" = "--no-web" ] || wait_healthy http://127.0.0.1:3000/ web
    status
    ;;
  stop)
    # PIDS is empty when stop runs as its own invocation, so guard the array
    # expansion: a bare $PIDS aborts the script under `set -u` before the
    # pkill fallbacks below ever run.
    for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done
    pkill -f "services/realtime/bin/" 2>/dev/null
    pkill -f "next dev --port 3000" 2>/dev/null
    echo stopped
    ;;
  status) status ;;
  *) echo "usage: $0 {build|start|stop|status}"; exit 1 ;;
esac
