const backendBaseUrl = resolveBackendBaseUrl(
  process.env.MATCH_SERVICE_INTERNAL_URL,
  'http://match-service.railway.internal:8080',
);

// undici's default headers timeout is 300s. Without a bound, a wedged
// match-service pins this handler for five minutes per request.
const UPSTREAM_TIMEOUT_MS = 8000;

export async function proxyRealtime(request: Request, path: string): Promise<Response> {
  const url = `${backendBaseUrl}${path}`;
  const init: RequestInit = {
    method: request.method,
    headers: buildUpstreamHeaders(request.headers),
    cache: 'no-store',
    signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS),
  };

  if (request.method !== 'GET' && request.method !== 'HEAD') {
    init.body = await request.text();
  }

  try {
    const upstream = await fetch(url, init);
    const body = await upstream.text();

    return new Response(body, {
      status: upstream.status,
      headers: filterResponseHeaders(upstream.headers),
    });
  } catch (error) {
    // Previously this threw and Next returned an opaque 500. A timeout or an
    // unreachable upstream is a gateway condition, not a bug in this handler.
    const timedOut = error instanceof Error && error.name === 'TimeoutError';
    return Response.json(
      { error: timedOut ? 'match service timed out' : 'match service is unreachable' },
      { status: timedOut ? 504 : 502, headers: { 'cache-control': 'no-store' } },
    );
  }
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

function buildUpstreamHeaders(headers: Headers): Headers {
  const next = filterHeaders(headers);
  const token = internalServiceToken();
  if (token) {
    next.set('x-chess404-service-token', token);
  }
  return next;
}

function internalServiceToken(): string {
  return (
    process.env.MATCH_INTERNAL_SERVICE_TOKEN ??
    process.env.CHESS404_INTERNAL_SERVICE_TOKEN ??
    process.env.INTERNAL_SERVICE_TOKEN ??
    ''
  ).trim();
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
  return next;
}

function resolveBackendBaseUrl(explicit: string | undefined, fallback: string): string {
  const value = explicit?.trim().replace(/\/$/, '');
  if (!value || value.includes('${{') || /:\s*$/.test(value)) {
    return fallback;
  }
  return value;
}
