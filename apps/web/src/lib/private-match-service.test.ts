// @vitest-environment node
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createPrivateMatch } from './private-match-service';
import { readStoredGuestIdentity } from './session-storage';

class MemoryStorage {
  private readonly values = new Map<string, string>();

  getItem(key: string): string | null {
    return this.values.get(key) ?? null;
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value);
  }

  removeItem(key: string): void {
    this.values.delete(key);
  }
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('private match access', () => {
  it('persists the guest session resolved by the gateway before opening the new room', async () => {
    const storage = new MemoryStorage();
    vi.stubGlobal('window', { localStorage: storage });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      matchId: 'room-1',
      seatColor: 'white',
      waitingForOpponent: true,
      snapshot: { match: { matchId: 'room-1' } },
      guestSession: {
        guest: { guestId: 'fresh-guest' },
        sessionSecret: 'fresh-secret',
        sessionToken: 'fresh-token',
        expiresAt: '2099-01-01T00:00:00.000Z',
      },
    }), { status: 201 })));

    await createPrivateMatch({
      identity: { guestId: 'expired-guest', sessionSecret: 'expired-secret' },
    });

    expect(readStoredGuestIdentity('white')).toEqual({
      guestId: 'fresh-guest',
      sessionSecret: 'fresh-secret',
      sessionToken: 'fresh-token',
      sessionExpiresAt: '2099-01-01T00:00:00.000Z',
    });
  });
});
