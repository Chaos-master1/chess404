// Upstream budgets for internal service calls. These exist because undici's
// default headers timeout is 300s, which is far longer than any healthy
// internal hop and long enough for a wedged upstream to exhaust the Next.js
// event loop.
const UPSTREAM_TIMEOUT_MS = 8000;
const UPSTREAM_STREAM_TIMEOUT_MS = 15000;

// The Fetch spec forbids a body on these statuses -- the Response
// constructor throws "Invalid response status code" if body is anything
// other than null, even an empty string. Presence heartbeats return 204,
// so every 204 through this proxy crashed into the catch block below and
// surfaced as a misleading "gateway is unreachable" 502.
const NULL_BODY_STATUSES = new Set([204, 205, 304]);

interface InternalServiceProxyConfig {
  explicitUrl?: string;
  fallbackUrl: string;
  envName: string;
  serviceName: string;
}

interface ResolvedInternalService {
  baseUrl: string;
  usedFallback: boolean;
  warning?: string;
}

export async function proxyInternalService(request: Request, path: string, config: InternalServiceProxyConfig): Promise<Response> {
  const resolved = resolveInternalServiceBaseUrl(config);
  const url = `${resolved.baseUrl}${path}`;
  const init: RequestInit = {
    method: request.method,
    headers: buildUpstreamHeaders(request),
    cache: 'no-store',
    // Without this, undici waits 300s for headers. A single wedged upstream
    // then pins a Next handler for five minutes, and enough of them exhaust
    // the event loop and take the whole web service down with it.
    signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS),
  };

  if (request.method !== 'GET' && request.method !== 'HEAD') {
    init.body = await request.text();
  }

  try {
    const upstream = await fetch(url, init);
    const body = await upstream.text();
    const headers = filterResponseHeaders(upstream.headers);
    if (resolved.warning) {
      headers.set('x-chess404-proxy-warning', resolved.warning);
    }
    return new Response(NULL_BODY_STATUSES.has(upstream.status) ? null : body, {
      status: upstream.status,
      headers,
    });
  } catch (error) {
    return buildProxyFailureResponse(config, resolved, error);
  }
}

export async function proxyInternalServiceStream(request: Request, path: string, config: InternalServiceProxyConfig): Promise<Response> {
  const resolved = resolveInternalServiceBaseUrl(config);
  const url = `${resolved.baseUrl}${path}`;
  const init: RequestInit = {
    method: request.method,
    headers: buildUpstreamHeaders(request),
    cache: 'no-store',
    // Longer than the unary path: this is a long-lived SSE stream, so the
    // budget only needs to cover establishing it, not the stream lifetime.
    signal: AbortSignal.timeout(UPSTREAM_STREAM_TIMEOUT_MS),
  };

  if (request.method !== 'GET' && request.method !== 'HEAD') {
    init.body = await request.text();
  }

  try {
    const upstream = await fetch(url, init);
    const headers = filterResponseHeaders(upstream.headers);
    if (resolved.warning) {
      headers.set('x-chess404-proxy-warning', resolved.warning);
    }
    return new Response(NULL_BODY_STATUSES.has(upstream.status) ? null : upstream.body, {
      status: upstream.status,
      headers,
    });
  } catch (error) {
    return buildProxyFailureResponse(config, resolved, error);
  }
}

function buildProxyFailureResponse(
  config: InternalServiceProxyConfig,
  resolved: ResolvedInternalService,
  error: unknown,
): Response {
  const detail = error instanceof Error ? error.message : 'unreachable upstream';
  const guidance = `${config.envName} must be a full internal URL with a port, for example ${config.fallbackUrl}.`;
  const warning = resolved.warning ? `${resolved.warning}. ` : '';
  return Response.json({
    error: `${config.serviceName} is unreachable. ${warning}${guidance} Attempted ${resolved.baseUrl}. Upstream error: ${detail}`,
  }, { status: 502 });
}

function resolveInternalServiceBaseUrl(config: InternalServiceProxyConfig): ResolvedInternalService {
  const fallback = sanitizeBaseUrl(config.fallbackUrl) ?? config.fallbackUrl.trim().replace(/\/$/, '');
  const explicit = sanitizeBaseUrl(config.explicitUrl);

  if (!explicit) {
    return { baseUrl: fallback, usedFallback: true };
  }

  try {
    const parsed = new URL(explicit);
    if (requiresPortFallback(parsed)) {
      return {
        baseUrl: fallback,
        usedFallback: true,
        warning: `${config.envName} omitted the internal service port`,
      };
    }
    return { baseUrl: parsed.toString().replace(/\/$/, ''), usedFallback: false };
  } catch {
    return {
      baseUrl: fallback,
      usedFallback: true,
      warning: `${config.envName} is not a valid URL`,
    };
  }
}

function sanitizeBaseUrl(value?: string): string | null {
  const trimmed = value?.trim().replace(/\/$/, '');
  if (!trimmed || trimmed.includes('${{') || /:\s*$/.test(trimmed)) {
    return null;
  }
  return trimmed;
}

function requiresPortFallback(url: URL): boolean {
  if (url.port) {
    return false;
  }
  const hostname = url.hostname.toLowerCase();
  return hostname.endsWith('.railway.internal') || hostname === 'localhost' || hostname === '127.0.0.1';
}

export function filterHeaders(headers: Headers): Headers {
  const next = new Headers();
  headers.forEach((value, key) => {
    const lower = key.toLowerCase();
    if (
      lower === 'host' ||
      lower === 'connection' ||
      lower === 'content-length'
    ) {
      return;
    }
    next.set(key, value);
  });
  return next;
}

// buildUpstreamHeaders prepares the headers for the outgoing request to an
// internal backend service. It does two things on top of filterHeaders:
//
//   1. Injects X-Forwarded-Proto and X-Forwarded-Host from the incoming
//      request, so the backend can reconstruct the public origin for its
//      CSRF/origin checks. The browser does NOT send an Origin header for
//      same-origin POSTs (only a Referer with a path), and the gateway's
//      CSRF check uses X-Forwarded-* to compute the expected origin.
//
//   2. Sets the Origin header to the public origin when the browser did
//      not provide one (same-origin POST). Without this, server-to-server
//      POSTs from the gateway arrive at the backend with no Origin and
//      are rejected with 403 "CSRF check failed: origin header required".
export function buildUpstreamHeaders(request: Request): Headers {
  const headers = filterHeaders(request.headers);
  const url = new URL(request.url);
  const forwardedHost = headers.get('x-forwarded-host') ?? url.host;
  const forwardedProto = headers.get('x-forwarded-proto') ?? url.protocol.replace(':', '');
  if (!headers.has('x-forwarded-host')) {
    headers.set('x-forwarded-host', forwardedHost);
  }
  if (!headers.has('x-forwarded-proto')) {
    headers.set('x-forwarded-proto', forwardedProto);
  }
  if (!headers.has('origin') && forwardedHost) {
    headers.set('origin', `${forwardedProto}://${forwardedHost}`);
  }
  const token = internalServiceToken();
  if (token) {
    headers.set('x-chess404-service-token', token);
  }
  return headers;
}

// One resolver for every proxy in this app. The realtime proxy used to keep its
// own copy that only looked at MATCH_INTERNAL_SERVICE_TOKEN /
// CHESS404_INTERNAL_SERVICE_TOKEN / INTERNAL_SERVICE_TOKEN -- none of which are
// set in production -- so match-service traffic went out unauthenticated and
// every player's requests shared one internal IP's 60/min global rate limit.
export function internalServiceToken(): string {
  return (
    process.env.MATCH_INTERNAL_SERVICE_TOKEN ??
    process.env.GATEWAY_INTERNAL_SERVICE_TOKEN ??
    process.env.PLATFORM_INTERNAL_SERVICE_TOKEN ??
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
