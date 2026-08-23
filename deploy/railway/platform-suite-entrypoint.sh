#!/bin/sh
# Runs platform-service (on $PORT) and matchmaking-service (on MATCHMAKING_ADDR)
# inside one container. If either process dies, the container exits non-zero so
# Railway's ON_FAILURE restart policy replaces the whole suite.
set -u

/usr/local/bin/platform-service &
PLATFORM_PID=$!
/usr/local/bin/matchmaking-service &
MATCHMAKING_PID=$!

shutdown() {
    kill "$PLATFORM_PID" "$MATCHMAKING_PID" 2>/dev/null
    wait 2>/dev/null
    exit 0
}
trap shutdown TERM INT

while :; do
    if ! kill -0 "$PLATFORM_PID" 2>/dev/null; then
        echo "platform-suite: platform-service exited, stopping container" >&2
        break
    fi
    if ! kill -0 "$MATCHMAKING_PID" 2>/dev/null; then
        echo "platform-suite: matchmaking-service exited, stopping container" >&2
        break
    fi
    sleep 2
done

kill "$PLATFORM_PID" "$MATCHMAKING_PID" 2>/dev/null
exit 1
