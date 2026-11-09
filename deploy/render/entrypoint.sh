#!/bin/sh
set -e

echo "=== Starting Chess404 Unified Backend on Render ==="

# Set internal loopback addresses for microservices
export PLATFORM_ADDR="127.0.0.1:8083"
export MATCH_SERVICE_ADDR="127.0.0.1:8082"
export MATCHMAKING_ADDR="127.0.0.1:8084"
export GATEWAY_ADDR="127.0.0.1:8090"

# Internal loopback URLs for inter-service communication
export PLATFORM_SERVICE_INTERNAL_URL="http://127.0.0.1:8083"
export MATCH_SERVICE_INTERNAL_URL="http://127.0.0.1:8082"
export MATCHMAKING_SERVICE_INTERNAL_URL="http://127.0.0.1:8084"

# Set default store backends if not provided
export MATCH_STATE_BACKEND="${MATCH_STATE_BACKEND:-memory}"
export MATCH_CLAIM_STORE_BACKEND="${MATCH_CLAIM_STORE_BACKEND:-memory}"
export RATE_LIMIT_BACKEND="${RATE_LIMIT_BACKEND:-memory}"
export MATCHMAKING_TICKET_STORE_BACKEND="${MATCHMAKING_TICKET_STORE_BACKEND:-memory}"

# Ensure data directory exists
mkdir -p data

echo "Starting platform-service on $PLATFORM_ADDR..."
/usr/local/bin/platform-service &
PLATFORM_PID=$!

echo "Starting matchmaking-service on $MATCHMAKING_ADDR..."
/usr/local/bin/matchmaking-service &
MATCHMAKING_PID=$!

echo "Starting match-service on $MATCH_SERVICE_ADDR..."
/usr/local/bin/match-service &
MATCH_PID=$!

echo "Starting gateway on $GATEWAY_ADDR..."
/usr/local/bin/gateway &
GATEWAY_PID=$!

# Trap signals and shut down child processes
shutdown() {
    echo "Shutting down Chess404 backend services..."
    kill -TERM "$PLATFORM_PID" "$MATCHMAKING_PID" "$MATCH_PID" "$GATEWAY_PID" 2>/dev/null || true
    wait "$PLATFORM_PID" "$MATCHMAKING_PID" "$MATCH_PID" "$GATEWAY_PID" 2>/dev/null || true
    exit 0
}
trap shutdown TERM INT

# Wait a brief moment for internal services to bind
sleep 1

echo "Starting public reverse proxy on 0.0.0.0:${PORT:-8080}..."
exec /usr/local/bin/proxy
