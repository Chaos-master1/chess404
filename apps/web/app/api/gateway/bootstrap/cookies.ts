// buildSessionSecretCookies parses a gateway bootstrap JSON payload and
// returns Set-Cookie strings minting the HttpOnly session cookies on the web
// origin itself (the gateway sets them on its internal domain, where the
// browser ignores them): session_secret_{side} carries the resumable secret
// and session_guest_{side} the guest id, so a browser that loses localStorage
// can still resume purely from cookies -- the gateway folds both back into
// the bootstrap identity. Pure and testable: no Response or headers
// construction here.
export function buildSessionSecretCookies(payload: unknown): string[] {
  const sessions = asRecord((payload as { guestSessions?: unknown } | null)?.guestSessions);
  const cookies: string[] = [];
  for (const side of ['white', 'black'] as const) {
    const session = asRecord(sessions?.[side]);
    const secret = typeof session?.sessionSecret === 'string' ? session.sessionSecret : '';
    const guestId = typeof asRecord(session?.guest)?.guestId === 'string' ? (asRecord(session?.guest)?.guestId as string) : '';
    if (!secret && !guestId) {
      continue;
    }
    if (secret) {
      cookies.push(
        [
          `session_secret_${side}=${secret}`,
          'Path=/',
          'HttpOnly',
          'Secure',
          'SameSite=Lax',
          'Max-Age=31536000',
        ].join('; '),
      );
    }
    if (guestId) {
      cookies.push(
        [
          `session_guest_${side}=${encodeURIComponent(guestId)}`,
          'Path=/',
          'HttpOnly',
          'Secure',
          'SameSite=Lax',
          'Max-Age=31536000',
        ].join('; '),
      );
    }
  }
  return cookies;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : null;
}
