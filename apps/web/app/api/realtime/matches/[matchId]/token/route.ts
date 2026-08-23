import { proxyRealtime } from '../../../_lib/proxy';

export const dynamic = 'force-dynamic';

// match-service exposes GET/POST /api/matches/{id}/token to exchange a seat's
// playerId+playerSecret for a short-lived WebSocket auth token, but no route
// under /api/realtime ever proxied it. fetchAuthToken() in
// apps/web/src/lib/match-service.ts builds its URL from httpBaseUrl
// ("/api/realtime"), so every call 404'd, returned null through its bare
// `catch`, and the socket then authed with `claimToken: null`. The server
// answered "unauthorized" and closed, the client retried forever, and the live
// stream never came up -- every match silently degraded to ~1s HTTP polling,
// which is also unauthenticated and therefore spectator-scoped, so neither
// player ever saw their own card hand. Same missing-proxy-route class as the
// intents/presence gap already documented in match-service.ts.
export async function GET(
  request: Request,
  context: { params: Promise<{ matchId: string }> }
): Promise<Response> {
  const { matchId } = await context.params;
  return proxyRealtime(request, `/api/matches/${encodeURIComponent(matchId)}/token`);
}

export async function POST(
  request: Request,
  context: { params: Promise<{ matchId: string }> }
): Promise<Response> {
  const { matchId } = await context.params;
  return proxyRealtime(request, `/api/matches/${encodeURIComponent(matchId)}/token`);
}
