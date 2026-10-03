// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { claimMatchSeat, fetchActiveMatchClaim } from './platform-service';

// Production defect (2026-10-03): a claim request that stalls without erroring
// is indistinguishable from a slow one, and the queue -> match handoff only
// clears its busy flag when the promise settles. A paired player therefore sat
// on "Matched - opening game..." forever while their opponent played alone.
// These tests pin the ceiling that makes the handoff reach a navigation.
describe('platform-service claim timeouts', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  // Mimics a connection that opens and then never answers: nothing resolves,
  // and only an abort can end the request.
  function stallUntilAborted() {
    const fetchMock = vi.fn((_url: string, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => reject(new Error('aborted')));
    }));
    vi.stubGlobal('fetch', fetchMock);
    return fetchMock;
  }

  it('aborts claimMatchSeat at its ceiling instead of hanging forever', async () => {
    const fetchMock = stallUntilAborted();

    const pending = claimMatchSeat({ matchId: 'room_stall', guestId: 'guest_stall' });
    const assertion = expect(pending).rejects.toThrow(/abort/i);
    await vi.advanceTimersByTimeAsync(10_000);
    await assertion;

    // The request must carry the abort signal, or the browser has no way to
    // kill a stalled connection.
    expect(fetchMock.mock.calls[0]?.[1]?.signal).toBeInstanceOf(AbortSignal);
  });

  it('aborts fetchActiveMatchClaim too, so claim recovery cannot wedge', async () => {
    stallUntilAborted();

    const pending = fetchActiveMatchClaim({ guestId: 'guest_stall' });
    const assertion = expect(pending).rejects.toThrow(/abort/i);
    await vi.advanceTimersByTimeAsync(10_000);
    await assertion;
  });

  it('leaves a claim that answers in time untouched', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(
      JSON.stringify({
        matchId: 'room_ok',
        guestId: 'guest_ok',
        seatColor: 'white',
        playerId: 'white',
        playerSecret: 'secret_ok',
        claimToken: 'token_ok',
        expiresAt: '2026-10-03T13:00:00Z',
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    )));

    const claim = await claimMatchSeat({ matchId: 'room_ok', guestId: 'guest_ok' });
    expect(claim.matchId).toBe('room_ok');
    expect(claim.playerSecret).toBe('secret_ok');
  });
});
