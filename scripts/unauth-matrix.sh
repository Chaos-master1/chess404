#!/usr/bin/env bash
# Unauthenticated request matrix over the web /api/** surface (launch-audit P1).
# Pace: 1.2s between calls to stay under the prod 60/min per-IP limiter.
# Verdicts: 401/403/404/405 = expected authz outcome; 429 = limiter (note);
# 2xx/3xx = REVIEW (public by design?); 5xx = FAIL (crash or data leak).
set -u
BASE="${E2E_BASE_URL:-https://web-production-5adfa.up.railway.app}"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

map_route() {
  echo "$1" | sed -E \
    -e 's|^apps/web/app||' \
    -e 's|/route\.ts$||' \
    -e 's|\[matchId\]|matrix-match-0000|g' \
    -e 's|\[ticketId\]|matrix-ticket-0000|g' \
    -e 's|\[accountId\]|00000000-0000-0000-0000-000000000000|g' \
    -e 's|\[guestId\]|matrix-guest-0000|g' \
    -e 's|\[handle\]|matrix-handle-0000|g' \
    -e 's|\[challengeId\]|matrix-challenge-0000|g' \
    -e 's|\[requestId\]|matrix-request-0000|g'
}

ROUTES="$(cd "$REPO" && find apps/web/app/api -name route.ts | sort | while read -r f; do map_route "$f"; done)"

echo "matrix base: $BASE"
echo "method	path	http	status-class"
fail=0; review=0; total=0
hit() {
  local method="$1" path="$2" body="${3:-}" out code
  if [ "$method" = "POST" ]; then
    code="$(curl -s -o /tmp/matrix-body.$$ -w '%{http_code}' -X POST --max-time 20 \
      -H 'Content-Type: application/json' --data "$body" "$BASE$path")"
  else
    code="$(curl -s -o /tmp/matrix-body.$$ -w '%{http_code}' --max-time 20 "$BASE$path")"
  fi
  total=$((total+1))
  case "$code" in
    401|403|404|405) class=ok ;;
    429) class=limited ;;
    5*) class=FAIL; fail=$((fail+1)) ;;
    *) class=REVIEW; review=$((review+1)) ;;
  esac
  printf '%s\t%s\t%s\t%s\n' "$method" "$path" "$code" "$class"
  # Print the body for anything that is not a clean authz answer.
  case "$code" in
    401|403|404|405|429) ;;
    *) head -c 200 /tmp/matrix-body.$$ | tr '\n' ' '; echo ;;
  esac
  rm -f /tmp/matrix-body.$$
  sleep 1.2
}

echo "=== GET sweep (all routes) ==="
while read -r r; do hit GET "$r"; done <<EOF
$ROUTES
EOF

echo "=== POST sweep (mutation-shaped routes, empty JSON body) ==="
while read -r r; do hit POST "$r" '{}'; done <<EOF
/api/matchmaking/queues/tickets
/api/gateway/private-matches
/api/gateway/private-matches/matrix-match-0000/join
/api/gateway/private-matches/matrix-match-0000/rematch
/api/gateway/matches/matrix-match-0000/intents
/api/gateway/matches/matrix-match-0000/presence
/api/gateway/challenges
/api/gateway/challenges/matrix-challenge-0000/accept
/api/platform/account-auth/login
/api/platform/account-auth/register
/api/platform/account-auth/logout
/api/platform/account-auth/credentials
/api/platform/account-auth/password-reset/request
/api/platform/account-auth/password-reset/confirm
/api/platform/account-auth/email-verification/request
/api/platform/account-auth/email-verification/confirm
/api/platform/accounts/claim
/api/platform/account-sessions/revoke
/api/platform/account-sessions/revoke-others
/api/platform/guest-sessions
/api/platform/guest-results
/api/platform/match-claims
/api/platform/match-claims/active
/api/platform/challenges/matrix-challenge-0000/respond
/api/platform/challenges/matrix-challenge-0000/cancel
/api/platform/friends/requests
/api/platform/friends/requests/matrix-request-0000/respond
/api/platform/friends/remove
/api/platform/moderation/blocks
/api/platform/moderation/blocks/remove
/api/platform/moderation/admin/reports/resolve
/api/platform/inbox/read
/api/platform/inbox/read-all
/api/platform/accounts/matrix-handle-0000
EOF

echo "=== summary: total=$total fail=$fail review=$review ==="
[ "$fail" -eq 0 ]
