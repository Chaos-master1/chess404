// @vitest-environment node
import { afterEach, describe, expect, it, vi } from 'vitest';
import { GET } from './route';

const matchId = 'private-room-1';
const matchUrl = `http://match-service.railway.internal:8080/api/matches/${matchId}`;
const claimsUrl = 'http://platform-service.railway.internal:8080/api/platform/match-claims';

function snapshot(queue = 'direct') {
  return {
    match: {
      matchId,
      queue,
      status: 'active',
      whiteGuestId: 'white-guest',
      blackGuestId: 'black-guest',
      whitePlayerSecret: 'white-seat-secret',
      blackPlayerSecret: 'black-seat-secret',
      whiteHand: [{ id: 'white-card' }],
      blackHand: [{ id: 'black-card' }],
    },
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('private match snapshot route', () => {
  it('forwards a seat credential only after platform ownership verification', async () => {
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
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
