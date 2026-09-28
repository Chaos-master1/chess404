import { internalServiceTokenForTarget } from '../../../_lib/internal-service';
import { proxyRealtime } from '../../_lib/proxy';

export const dynamic = 'force-dynamic';

const matchServiceBaseUrl = resolveBackendBaseUrl(
  process.env.MATCH_SERVICE_INTERNAL_URL,
  'http://match-service.railway.internal:8080',
);

const platformServiceBaseUrl = resolveBackendBaseUrl(
  process.env.PLATFORM_SERVICE_INTERNAL_URL,
  'http://platform-service.railway.internal:8080',
);

export async function GET(
  request: Request,
  context: { params: Promise<{ matchId: string }> }
): Promise<Response> {
  const { matchId } = await context.params;
  const seatResolution = await resolveVerifiedMatchSeat(request, matchId);
  const verifiedSeat = seatResolution.seat;
  const upstreamUrl = `${matchServiceBaseUrl}/api/matches/${encodeURIComponent(matchId)}`;
  const upstreamHeaders = buildInternalHeaders(request.headers, 'match');
  if (verifiedSeat) {
    // The match service applies the per-seat hidden-card view. Only forward a
    // bearer credential after the platform service has established that this
    // browser owns that exact match seat; never treat a public guest ID as
    // sufficient proof.
    upstreamHeaders.set('X-Player-ID', verifiedSeat.guestId);
    upstreamHeaders.set('X-Player-Secret', verifiedSeat.playerSecret);
  }
  const upstream = await fetch(upstreamUrl, {
    method: 'GET',
    headers: upstreamHeaders,
    cache: 'no-store',
  });
  const body = await upstream.text();
  if (!upstream.ok) {
    console.error(`[MATCH_FETCH] upstream ${upstreamUrl} returned ${upstream.status}: ${body.slice(0, 200)}`);
    return new Response(body, {
      status: upstream.status,
      headers: filterResponseHeaders(upstream.headers),
    });
  }

  let snapshot: MatchSnapshotResponse;
  try {
    snapshot = JSON.parse(body) as MatchSnapshotResponse;
  } catch {
    return Response.json({ error: 'invalid match service response' }, { status: 502 });
  }

  if (isLocalRequest(request) || verifiedSeat) {
    return Response.json(snapshot, { status: 200, headers: noStoreHeaders() });
  }

  const publicReadable = isPublicSpectatorReadable(snapshot);
  if (!publicReadable) {
    // Ownership could not be PROVEN or DISPROVEN because the platform claims
    // service itself was unreachable. Answering 404 here would tell clients
    // the terminal "match is gone" verdict for what may be a live private
    // match -- the exact failure mode behind repeated "match is not public"
    // errors during platform-service incidents. 503 is retryable and honest.
    if (seatResolution.dependencyFailed) {
      console.error('[MATCH_FETCH] claims service unreachable; refusing to answer 404 for an unverifiable match');
      return Response.json({ error: 'match access check unavailable' }, { status: 503, headers: noStoreHeaders() });
    }
    // Log only which credential slots were present, never the identifiers
    // themselves -- this line runs on every unauthorized read attempt.
    const credentialsPresent = {
      white: !!request.headers.get('x-chess404-white-session-token') || !!request.headers.get('x-chess404-white-session-secret'),
      black: !!request.headers.get('x-chess404-black-session-token') || !!request.headers.get('x-chess404-black-session-secret'),
    };
    console.error(`[MATCH_FETCH] auth failed and match is not public-readable`, JSON.stringify(credentialsPresent));
    return Response.json({ error: 'match is not public' }, { status: 404, headers: noStoreHeaders() });
  }

  return Response.json(buildPublicSpectatorSnapshot(snapshot), {
    status: 200,
    headers: noStoreHeaders(),
  });
}

export async function POST(
  request: Request,
  context: { params: Promise<{ matchId: string }> }
): Promise<Response> {
  const { matchId } = await context.params;
  if (!isLocalRequest(request)) {
    return Response.json({
      error: 'direct match intents are not public; use the gateway match flow',
    }, { status: 404 });
  }
  return proxyRealtime(request, `/api/matches/${matchId}/intents`);
}

interface MatchSnapshotResponse {
  match: Record<string, unknown>;
  replayHead?: number;
  replayFrames?: unknown[];
  events?: Array<Record<string, unknown>>;
  seqNum?: number;
}

interface MatchClaimResponse {
  matchId?: string;
  guestId?: string;
  status?: string;
  playerSecret?: string;
}

// Redaction markers match-service writes into snapshots/archive rows. A claim
// secret equal to one of these is not a credential.
function isUsableSeatSecret(secret: string): boolean {
  return secret !== '' && secret !== '<redacted>';
}

function isPublicSpectatorReadable(snapshot: MatchSnapshotResponse): boolean {
  const match = snapshot.match ?? {};
  const status = normalize(match.status);
  if (status !== 'active') {
    return false;
  }
  if (normalize(match.queue) === 'direct') {
    return false;
  }
  if (normalize(match.winner) || normalize(match.finishReason)) {
    return false;
  }
  return true;
}

function buildPublicSpectatorSnapshot(snapshot: MatchSnapshotResponse): MatchSnapshotResponse {
  const match = { ...(snapshot.match ?? {}) };
  delete match.whiteGuestId;
  delete match.blackGuestId;
  delete match.whiteAccountId;
  delete match.blackAccountId;
  delete match.whitePlayerSecret;
  delete match.blackPlayerSecret;
  delete match.seenClientMoveIds;
  delete match.whiteHand;
  delete match.blackHand;
  delete match.chatMessages;
  delete match.invisiblePiece;
  delete match.cheaterState;
  delete match.radarRevealFor;
  delete match.drawOfferTime;

  return {
    match: {
      ...match,
      whiteHand: [],
      blackHand: [],
      chatMessages: [],
    },
    replayHead: snapshot.replayHead ?? 0,
    replayFrames: [],
    events: (snapshot.events ?? []).map((event) => ({
      id: event.id,
      matchId: event.matchId,
      type: event.type,
      at: event.at,
    })),
    seqNum: snapshot.seqNum,
  };
}

interface VerifiedMatchSeat {
  guestId: string;
  playerSecret: string;
}

interface SeatResolution {
  seat: VerifiedMatchSeat | null;
  // True when at least one claims-service attempt failed for infrastructure
  // reasons (network error, 5xx) rather than an authorization verdict. The
  // caller must then never treat seat === null as "not the owner".
  dependencyFailed: boolean;
}

async function resolveVerifiedMatchSeat(request: Request, matchId: string): Promise<SeatResolution> {
  const candidates = readGuestSessionCandidates(request.headers);
  const sideSecrets = readSideSecretsFromCookies(request.headers);
  let dependencyFailed = false;
  for (const candidate of candidates) {
    const playerSecret = candidate.sessionSecret || (candidate.side ? sideSecrets[candidate.side] : undefined);
    const payload: Record<string, string> = {
      matchId,
      guestId: candidate.guestId,
    };
    if (candidate.sessionToken) {
      payload.sessionToken = candidate.sessionToken;
    } else if (playerSecret) {
      payload.sessionSecret = playerSecret;
    }

    try {
      const response = await fetch(`${platformServiceBaseUrl}/api/platform/match-claims`, {
        method: 'POST',
        headers: buildInternalHeaders(new Headers({ 'Content-Type': 'application/json', Accept: 'application/json' }), 'platform'),
        cache: 'no-store',
        body: JSON.stringify(payload),
      });
      if (!response.ok) {
        // 401/403/404 are the claims service's real authorization verdicts
        // (bad session, not a participant, match finished). 5xx means the
        // service itself is unhealthy -- that is not evidence of anything
        // about ownership.
        if (response.status >= 500) {
          dependencyFailed = true;
        }
        continue;
      }
      const claim = await response.json() as MatchClaimResponse;
      // The platform claims route only answers 200 while the claimed match is
      // still active (it 404s finished matches itself), and the claim payload
      // carries no status field -- a matched matchId + guestId pair is the
      // complete ownership proof.
      if (normalize(claim.matchId) === normalize(matchId) && normalize(claim.guestId) === normalize(candidate.guestId)) {
        // Queue-matched seats hold server-generated secrets the browser was
        // never given, so the browser's own session secret cannot scope the
        // match-service snapshot. The claims route (having proven the
        // browser's session) resolves the seat's real credential via the
        // internal seat-secret path and returns it in the issued claim; use
        // that server-side. It never leaves this process.
        const claimSecret = trimValue(claim.playerSecret);
        if (isUsableSeatSecret(claimSecret)) {
          return { seat: { guestId: candidate.guestId, playerSecret: claimSecret }, dependencyFailed };
        }
        // Private/direct matches: the browser does hold the seat secret
        // (it created the match with it). A session token alone cannot scope
        // the match-service response, so never downgrade to a broad snapshot.
        return {
          seat: playerSecret ? { guestId: candidate.guestId, playerSecret } : null,
          dependencyFailed,
        };
      }
    } catch {
      // Network failure talking to the claims service: an outage, never an
      // ownership verdict.
      dependencyFailed = true;
      continue;
    }
  }
  return { seat: null, dependencyFailed };
}

function readGuestSessionCandidates(headers: Headers): Array<{ guestId: string; sessionToken?: string; sessionSecret?: string; side?: 'white' | 'black' }> {
  const sides = ['white', 'black'] as const;
  const candidates: Array<{ guestId: string; sessionToken?: string; sessionSecret?: string; side?: 'white' | 'black' }> = sides.map((side) => ({
    side,
    // IDs, session tokens, and session secrets are opaque values. In
    // particular, tokens and secrets may be base64url values, where changing
    // case changes the credential. Normalize only when comparing identifiers,
    // never while forwarding an authentication value.
    guestId: trimValue(headers.get(`x-chess404-${side}-guest-id`)),
    sessionToken: trimValue(headers.get(`x-chess404-${side}-session-token`)) || undefined,
    sessionSecret: trimValue(headers.get(`x-chess404-${side}-session-secret`)) || undefined,
  })).filter((candidate) => candidate.guestId);

  const generic: { guestId: string; sessionToken?: string; sessionSecret?: string } = {
    guestId: trimValue(headers.get('x-chess404-guest-id')),
    sessionToken: trimValue(headers.get('x-chess404-session-token')) || undefined,
    sessionSecret: trimValue(headers.get('x-chess404-session-secret')) || undefined,
  };
  if (generic.guestId) {
    candidates.push(generic);
  }
  return candidates;
}

function readSideSecretsFromCookies(headers: Headers): Record<'white' | 'black', string | undefined> {
  const cookie = headers.get('cookie') ?? '';
  const parse = (name: string): string | undefined => {
    const match = cookie.match(new RegExp(`(?:^|;)\\s*${name}=([^;]*)`));
    return match ? decodeURIComponent(match[1].trim()) : undefined;
  };
  return {
    white: parse('session_secret_white'),
    black: parse('session_secret_black'),
  };
}

function isLocalRequest(request: Request): boolean {
  if (process.env.NODE_ENV === 'production') {
    return false;
  }
  const host = request.headers.get('host')?.toLowerCase() ?? '';
  return host.startsWith('localhost') || host.startsWith('127.0.0.1');
}

function normalize(value: unknown): string {
  return typeof value === 'string' ? value.trim().toLowerCase() : '';
}

function trimValue(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}

function noStoreHeaders(): Headers {
  const headers = new Headers();
  headers.set('Cache-Control', 'no-store');
  return headers;
}

function filterHeaders(headers: Headers): Headers {
  const next = new Headers();
  headers.forEach((value, key) => {
    const lower = key.toLowerCase();
    if (lower === 'host' || lower === 'connection' || lower === 'content-length') {
      return;
    }
    next.set(key, value);
  });
  return next;
}

function buildInternalHeaders(headers: Headers, target: 'match' | 'platform'): Headers {
  const next = filterHeaders(headers);
  const token = internalServiceTokenForTarget(target);
  if (token) {
    next.set('x-chess404-service-token', token);
  }
  return next;
}

function filterResponseHeaders(headers: Headers): Headers {
  const next = new Headers();
  headers.forEach((value, key) => {
    const lower = key.toLowerCase();
    if (lower === 'content-length' || lower === 'connection' || lower === 'transfer-encoding') {
      return;
    }
    next.set(key, value);
  });
  next.set('Cache-Control', 'no-store');
  return next;
}

function resolveBackendBaseUrl(explicit: string | undefined, fallback: string): string {
  const value = explicit?.trim().replace(/\/$/, '');
  if (!value || value.includes('${{') || /:\s*$/.test(value)) {
    return fallback;
  }
  return value;
}
