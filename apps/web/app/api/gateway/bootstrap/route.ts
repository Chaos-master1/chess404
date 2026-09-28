import { filterHeaders } from '../../_lib/internal-service';
import { proxyGateway } from '../_lib/proxy';
import { buildSessionSecretCookies } from './cookies';

export const dynamic = 'force-dynamic';

export async function GET(request: Request): Promise<Response> {
  return bootstrapWithSecretCookies(request);
}

export async function POST(request: Request): Promise<Response> {
  return bootstrapWithSecretCookies(request);
}

// The gateway (on its internal Railway domain) already sets HttpOnly
// session_secret_* cookies, but this proxy re-wraps the upstream response on
// the public web origin: upstream Set-Cookie headers never reach the browser,
// and filterResponseHeaders would collapse multiple Set-Cookies anyway. The
// dual-write below re-mints the SAME cookies for the web origin so browsers
// can move off localStorage secrets without a coordinated frontend switch --
// localStorage stays the primary source until the backend reads cookies.
async function bootstrapWithSecretCookies(request: Request): Promise<Response> {
  const upstream = await proxyGateway(request, '/api/session/bootstrap');
  let payload: unknown;
  try {
    payload = await upstream.clone().json();
  } catch {
    return upstream; // non-JSON upstream (proxy 502, empty 204): pass through
  }
  const response = new Response(
    NULL_BODY_STATUSES.has(upstream.status) ? null : JSON.stringify(payload),
    {
      status: upstream.status,
      headers: filterHeaders(upstream.headers),
    },
  );
  for (const cookie of buildSessionSecretCookies(payload)) {
    response.headers.append('set-cookie', cookie);
  }
  return response;
}

// The Fetch spec forbids a body on these statuses -- the Response constructor
// throws "Invalid response status code" if body is anything other than null.
const NULL_BODY_STATUSES = new Set([204, 205, 304]);