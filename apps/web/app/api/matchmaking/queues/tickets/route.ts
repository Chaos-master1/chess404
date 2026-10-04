import { proxyMatchmaking } from '../../_lib/proxy';
import {
  buildUpstreamHeaders,
  filterResponseHeaders,
  isLocalRequest,
  NULL_BODY_STATUSES,
  resolveBackendBaseUrl,
  UPSTREAM_TIMEOUT_MS,
} from '../../../_lib/internal-service';

export const dynamic = 'force-dynamic';

const matchmakingBaseUrl = resolveBackendBaseUrl(
  process.env.MATCHMAKING_SERVICE_INTERNAL_URL,
  'http://matchmaking-service.railway.internal:8080',
);

const platformBaseUrl = resolveBackendBaseUrl(
  process.env.PLATFORM_SERVICE_INTERNAL_URL,
  'http://platform-service.railway.internal:8080',
);

interface QueueTicketCreatePayload {
  guestId?: string;
  queue?: 'casual' | 'rated';
  modeId?: string;
  rating?: number;
  clockSeconds?: number;
  clockIncrement?: number;
  displayName?: string;
  accountId?: string;
  accountSessionToken?: string;
}

interface PlatformAccountSessionPayload {
  account: {
    accountId: string;
    primaryGuestId: string;
    linkedGuestIds?: string[];
  };
}

export async function GET(request: Request): Promise<Response> {
  const { search } = new URL(request.url);
  if (!isLocalRequest(request)) {
    const params = new URLSearchParams(search);
    if (params.has('guestId') || params.has('accountId')) {
      return jsonError('raw queue ticket lookup is not public', 403);
    }
    return Response.json({ tickets: [] }, {
      status: 200,
      headers: noStoreHeaders(),
    });
  }
  return proxyMatchmaking(request, `/api/queues/tickets${search}`);
}

export async function POST(request: Request): Promise<Response> {
  let payload: QueueTicketCreatePayload;
  try {
    payload = (await request.json()) as QueueTicketCreatePayload;
  } catch {
    return jsonError('invalid queue ticket payload', 400);
  }

  const queue = payload.queue === 'rated' ? 'rated' : 'casual';
  const guestId = payload.guestId?.trim() ?? '';
  if (!guestId) {
    return jsonError('guestId is required', 400);
  }

  if (queue === 'rated') {
    const accountId = payload.accountId?.trim() ?? '';
    const sessionToken = payload.accountSessionToken?.trim() ?? '';
    if (!accountId || !sessionToken) {
      return jsonError('Rated queue requires a signed-in Chess404 account.', 401);
    }

    const session = await validateRatedAccountSession(request, accountId, sessionToken);
    if (session instanceof Response) {
      return session;
    }

    const linkedGuestIds = new Set<string>([
      session.account.primaryGuestId,
      ...(session.account.linkedGuestIds ?? []),
    ].map(value => value.trim()).filter(Boolean));

    if (!linkedGuestIds.has(guestId)) {
      return jsonError('Rated queue must use a guest linked to the signed-in Chess404 account.', 403);
    }
  }

  return forwardMatchmaking(request, {
    guestId,
    accountId: payload.accountId?.trim() ?? undefined,
    queue,
    modeId: payload.modeId,
    rating: payload.rating,
    clockSeconds: payload.clockSeconds,
    clockIncrement: payload.clockIncrement,
    displayName: payload.displayName,
  });
}

async function validateRatedAccountSession(
  request: Request,
  accountId: string,
  sessionToken: string,
): Promise<PlatformAccountSessionPayload | Response> {
  let upstream: Response;
  let body: string;
  try {
    upstream = await fetch(`${platformBaseUrl}/api/platform/account-sessions`, {
      method: 'POST',
      headers: ensureJSONHeaders(buildUpstreamHeaders(request, 'platform')),
      cache: 'no-store',
      signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS),
      body: JSON.stringify({ accountId, sessionToken }),
    });
    body = await upstream.text();
  } catch (error) {
    // A timeout or refused connection is an upstream outage, not a bug in
    // this handler: JSON 504/502 instead of an opaque Next 500.
    const timedOut = error instanceof Error && error.name === 'TimeoutError';
    return jsonError(
      timedOut ? 'platform service timed out' : 'platform service is unreachable',
      timedOut ? 504 : 502,
      noStoreHeaders(),
    );
  }
  if (!upstream.ok) {
    const headers = filterResponseHeaders(upstream.headers);
    const fallbackStatus = upstream.status === 403 ? 403 : 401;
    const parsed = tryParseErrorPayload(body);
    if (upstream.status === 403 && parsed?.restrictionKind) {
      return new Response(body, {
        status: upstream.status,
        headers,
      });
    }
    return jsonError('Rated queue requires a signed-in Chess404 account.', fallbackStatus, headers);
  }

  try {
    return JSON.parse(body) as PlatformAccountSessionPayload;
  } catch {
    return jsonError('failed to validate rated account session', 502);
  }
}

async function forwardMatchmaking(request: Request, payload: QueueTicketCreatePayload): Promise<Response> {
  // The enqueue POST must carry the same upstream contract as every other
  // proxy path: an Origin the backend's CSRF check can validate even when the
  // caller is a server (browsers omit Origin on same-origin POSTs), and the
  // internal service token that takes the request out of the shared per-IP
  // bulkheads. Without them a busy origin's enqueues ride the raw 60/min
  // global cap and 429 under load.
  let upstream: Response;
  let body: string;
  try {
    upstream = await fetch(`${matchmakingBaseUrl}/api/queues/tickets`, {
      method: 'POST',
      headers: ensureJSONHeaders(buildUpstreamHeaders(request)),
      cache: 'no-store',
      signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS),
      body: JSON.stringify(payload),
    });
    body = await upstream.text();
  } catch (error) {
    // See validateRatedAccountSession: gateway conditions answer 504/502.
    const timedOut = error instanceof Error && error.name === 'TimeoutError';
    return jsonError(
      timedOut ? 'matchmaking service timed out' : 'matchmaking service is unreachable',
      timedOut ? 504 : 502,
      noStoreHeaders(),
    );
  }
  return new Response(NULL_BODY_STATUSES.has(upstream.status) ? null : body, {
    status: upstream.status,
    headers: filterResponseHeaders(upstream.headers),
  });
}

function jsonError(message: string, status: number, headers?: Headers): Response {
  const nextHeaders = headers ? new Headers(headers) : new Headers();
  nextHeaders.set('Content-Type', 'application/json');
  return new Response(JSON.stringify({ error: message }), {
    status,
    headers: nextHeaders,
  });
}

function tryParseErrorPayload(body: string): { error?: string; restrictionKind?: string; restrictionReason?: string } | null {
  try {
    return JSON.parse(body) as { error?: string; restrictionKind?: string; restrictionReason?: string };
  } catch {
    return null;
  }
}

function ensureJSONHeaders(headers: Headers): Headers {
  const next = new Headers(headers);
  if (!next.has('Content-Type')) {
    next.set('Content-Type', 'application/json');
  }
  if (!next.has('Accept')) {
    next.set('Accept', 'application/json');
  }
  return next;
}

function noStoreHeaders(): Headers {
  const headers = new Headers();
  headers.set('Cache-Control', 'no-store');
  return headers;
}
