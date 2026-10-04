import {
  buildInternalHeaders,
  filterResponseHeaders,
  NULL_BODY_STATUSES,
  resolveBackendBaseUrl,
  UPSTREAM_TIMEOUT_MS,
} from '../../_lib/internal-service';

const backendBaseUrl = resolveBackendBaseUrl(
  process.env.MATCH_SERVICE_INTERNAL_URL,
  'http://match-service.railway.internal:8080',
);

export async function proxyRealtime(request: Request, path: string): Promise<Response> {
  const url = `${backendBaseUrl}${path}`;
  const init: RequestInit = {
    method: request.method,
    headers: buildInternalHeaders(request.headers, 'match'),
    cache: 'no-store',
    signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS),
  };

  if (request.method !== 'GET' && request.method !== 'HEAD') {
    init.body = await request.text();
  }

  try {
    const upstream = await fetch(url, init);
    const body = await upstream.text();

    return new Response(NULL_BODY_STATUSES.has(upstream.status) ? null : body, {
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
