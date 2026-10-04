// @vitest-environment node
import { afterEach, describe, expect, it, vi } from 'vitest';
import { POST } from './route';

const platformUrl = 'http://platform-service.railway.internal:8080/api/platform/account-sessions';
const matchmakingUrl = 'http://matchmaking-service.railway.internal:8080/api/queues/tickets';

afterEach(() => {
  vi.unstubAllGlobals();
});

// Every upstream fetch this route makes must carry an abort signal: undici's
// default headers timeout is 300s, and an unbounded call pins the Next.js
// handler for that long when an upstream wedges. This is the central proxy
// policy in app/api/_lib/internal-service.ts; these tests keep the raw fetches
// in this route from regressing to unbounded.
describe('queue tickets route upstream budgets', () => {
  it('bounds the matchmaking enqueue call with an abort signal', async () => {
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      expect(url).toBe(matchmakingUrl);
      expect(init?.signal).toBeInstanceOf(AbortSignal);
      return new Response(JSON.stringify({ ticketId: 'ticket_1', status: 'queued' }), { status: 201 });
    });
    vi.stubGlobal('fetch', fetchMock);

    const response = await POST(new Request('https://web.example/api/matchmaking/queues/tickets', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ guestId: 'guest-1', queue: 'casual' }),
    }));

    expect(response.status).toBe(201);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('bounds the rated account-session validation call with an abort signal', async () => {
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      expect(init?.signal).toBeInstanceOf(AbortSignal);
      if (url === platformUrl) {
        return new Response(JSON.stringify({
          account: { accountId: 'acct-1', primaryGuestId: 'guest-1' },
        }));
      }
      expect(url).toBe(matchmakingUrl);
      return new Response(JSON.stringify({ ticketId: 'ticket_1', status: 'queued' }), { status: 201 });
    });
    vi.stubGlobal('fetch', fetchMock);

    const response = await POST(new Request('https://web.example/api/matchmaking/queues/tickets', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ guestId: 'guest-1', queue: 'rated', accountId: 'acct-1', accountSessionToken: 'tok' }),
    }));

    expect(response.status).toBe(201);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
