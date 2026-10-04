// @vitest-environment node
import { afterEach, describe, expect, it, vi } from 'vitest';
import { GET } from './route';

const matchId = 'private-room-1';
const matchUrl = `http://match-service.railway.internal:8080/api/matches/${matchId}`;
const claimsUrl = 'http://platform-service.railway.internal:8080/api/platform/match-claims';

function snapshot(queue = 'direct', status = 'active', extra: Record<string, unknown> = {}) {
  return {
    match: {
      matchId,
      queue,
      status,
      whiteGuestId: 'white-guest',
      blackGuestId: 'black-guest',
      whitePlayerSecret: 'white-seat-secret',
      blackPlayerSecret: 'black-seat-secret',
      whiteHand: [{ id: 'white-card' }],
      blackHand: [{ id: 'black-card' }],
      ...extra,
    },
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('private match snapshot route', () => {
  it('forwards a seat credential only after platform ownership verification', async () => {
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      expect(init?.signal).toBeInstanceOf(AbortSignal); // every upstream call must be bounded (see app/api/_lib/internal-service.ts)
      if (url === claimsUrl) {
        expect(JSON.parse(String(init?.body))).toMatchObject({
          matchId,
          guestId: 'White-Guest',
          sessionSecret: 'White-Session-Secret',
        });
        // Mirrors the real IssuedMatchSeatClaim payload, which has no status field.
        return new Response(JSON.stringify({ matchId, guestId: 'White-Guest' }));
      }
      expect(url).toBe(matchUrl);
      const headers = new Headers(init?.headers);
      expect(headers.get('x-player-id')).toBe('White-Guest');
      expect(headers.get('x-player-secret')).toBe('White-Session-Secret');
      const scoped = snapshot();
      scoped.match.whitePlayerSecret = '';
      scoped.match.blackPlayerSecret = '';
      scoped.match.blackHand = [];
      return new Response(JSON.stringify(scoped));
    });
    vi.stubGlobal('fetch', fetchMock);

    const response = await GET(new Request(`https://web.example/api/realtime/matches/${matchId}`, {
      headers: {
        'x-chess404-white-guest-id': 'White-Guest',
        'x-chess404-white-session-secret': 'White-Session-Secret',
      },
    }), { params: Promise.resolve({ matchId }) });

    expect(response.status).toBe(200);
    const body = await response.json();
    expect(body.match.whiteHand).toEqual([{ id: 'white-card' }]);
    expect(body.match.blackHand).toEqual([]);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('does not return a direct-match snapshot when ownership verification fails', async () => {
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      expect(init?.signal).toBeInstanceOf(AbortSignal); // every upstream call must be bounded (see app/api/_lib/internal-service.ts)
      if (url === claimsUrl) return new Response(JSON.stringify({ error: 'unauthorized' }), { status: 401 });
      expect(url).toBe(matchUrl);
      const headers = new Headers(init?.headers);
      expect(headers.get('x-player-id')).toBeNull();
      expect(headers.get('x-player-secret')).toBeNull();
      return new Response(JSON.stringify(snapshot()));
    });
    vi.stubGlobal('fetch', fetchMock);

    const response = await GET(new Request(`https://web.example/api/realtime/matches/${matchId}`, {
      headers: {
        'x-chess404-white-guest-id': 'white-guest',
        'x-chess404-white-session-secret': 'wrong-secret',
      },
    }), { params: Promise.resolve({ matchId }) });

    expect(response.status).toBe(404);
    await expect(response.json()).resolves.toEqual({ error: 'match is not public' });
  });

  // A finished match must stay readable to a seat owner. Live incident: the
  // claims route refused every completed match, so this layer had no verified
  // seat, fell through to the public-spectator gate (which requires
  // status === 'active') and answered 404 "match is not public" for BOTH
  // players of every finished game -- while match-service served the very same
  // match with 200. The client then polled a finished match for 34 minutes.
  // A verified seat must short-circuit the public gate regardless of status.
  it('returns 200 to a verified seat owner for a finished match', async () => {
    const fetchMock = vi.fn(async (url: string) => {
      if (url === claimsUrl) {
        return new Response(JSON.stringify({ matchId, guestId: 'White-Guest' }));
      }
      expect(url).toBe(matchUrl);
      const finished = snapshot('direct', 'finished', { winner: 'white', finishReason: 'checkmate' });
      return new Response(JSON.stringify(finished));
    });
    vi.stubGlobal('fetch', fetchMock);

    const response = await GET(new Request(`https://web.example/api/realtime/matches/${matchId}`, {
      headers: {
        'x-chess404-white-guest-id': 'White-Guest',
        'x-chess404-white-session-secret': 'White-Session-Secret',
      },
    }), { params: Promise.resolve({ matchId }) });

    expect(response.status).toBe(200);
    const body = await response.json();
    expect(body.match.status).toBe('finished');
    expect(body.match.winner).toBe('white');
  });

  // Widening finished-match READS for owners must not widen PUBLIC access:
  // spectate-privacy.spec.ts requires that vs-computer and direct matches stay
  // out of the anonymous surface. An unauthenticated read of a finished match
  // must still be refused.
  it('still refuses an unauthenticated read of a finished match', async () => {
    const fetchMock = vi.fn(async (url: string) => {
      if (url === claimsUrl) {
        return new Response(JSON.stringify({ error: 'not a participant' }), { status: 403 });
      }
      expect(url).toBe(matchUrl);
      return new Response(JSON.stringify(snapshot('direct', 'finished')));
    });
    vi.stubGlobal('fetch', fetchMock);

    const response = await GET(new Request(`https://web.example/api/realtime/matches/${matchId}`), {
      params: Promise.resolve({ matchId }),
    });

    expect(response.status).toBe(404);
    await expect(response.json()).resolves.toEqual({ error: 'match is not public' });
  });

  // A platform-service outage must never be read as an authorization verdict:
  // answering 404 tells clients the terminal "match is gone" verdict for what
  // may be a live private match, and the client then drops its seat state.
  it('answers 503 (not 404) when the claims service is down and the match is not public-readable', async () => {
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      expect(init?.signal).toBeInstanceOf(AbortSignal); // every upstream call must be bounded (see app/api/_lib/internal-service.ts)
      if (url === claimsUrl) return new Response(JSON.stringify({ error: 'internal' }), { status: 500 });
      expect(url).toBe(matchUrl);
      return new Response(JSON.stringify(snapshot()));
    });
    vi.stubGlobal('fetch', fetchMock);

    const response = await GET(new Request(`https://web.example/api/realtime/matches/${matchId}`, {
      headers: {
        'x-chess404-white-guest-id': 'white-guest',
        'x-chess404-white-session-secret': 'wrong-secret',
      },
    }), { params: Promise.resolve({ matchId }) });

    expect(response.status).toBe(503);
    await expect(response.json()).resolves.toEqual({ error: 'match access check unavailable' });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('answers 503 when the claims service is unreachable over the network', async () => {
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      expect(init?.signal).toBeInstanceOf(AbortSignal); // every upstream call must be bounded (see app/api/_lib/internal-service.ts)
      if (url === claimsUrl) throw new TypeError('fetch failed');
      expect(url).toBe(matchUrl);
      return new Response(JSON.stringify(snapshot()));
    });
    vi.stubGlobal('fetch', fetchMock);

    const response = await GET(new Request(`https://web.example/api/realtime/matches/${matchId}`, {
      headers: {
        'x-chess404-white-guest-id': 'white-guest',
        'x-chess404-white-session-secret': 'wrong-secret',
      },
    }), { params: Promise.resolve({ matchId }) });

    expect(response.status).toBe(503);
  });

  it('defensively removes bearer secrets from public spectator snapshots', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      expect(url).toBe(matchUrl);
      return new Response(JSON.stringify(snapshot('casual')));
    }));

    const response = await GET(new Request(`https://web.example/api/realtime/matches/${matchId}`), {
      params: Promise.resolve({ matchId }),
    });

    expect(response.status).toBe(200);
    const body = await response.json();
    expect(body.match.whitePlayerSecret).toBeUndefined();
    expect(body.match.blackPlayerSecret).toBeUndefined();
    expect(body.match.whiteHand).toEqual([]);
    expect(body.match.blackHand).toEqual([]);
  });
});
