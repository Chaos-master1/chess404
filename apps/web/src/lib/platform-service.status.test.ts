// @vitest-environment node
import { afterEach, describe, expect, it, vi } from 'vitest';
import { claimMatchSeat, type HttpError } from './platform-service';

afterEach(() => {
  vi.unstubAllGlobals();
});

// The match facade separates "the server says this room is gone" (404/410 ->
// graceful gone-latch card + warning log) from "the request never landed"
// (generic connection error + console.error) purely via err.status. These
// tests pin that unwrapResponse never drops the status again.
describe('platform-service error status contract', () => {
  it('attaches status 404 to the "unknown match archive" error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ error: 'unknown match archive' }),
      { status: 404, headers: { 'Content-Type': 'application/json' } },
    )));

    const err = await claimMatchSeat({ matchId: 'match_gone', guestId: 'guest-1' })
      .then(() => null)
      .catch((e: HttpError) => e);

    expect(err).toBeInstanceOf(Error);
    expect(err?.message).toBe('unknown match archive');
    expect(err?.status).toBe(404);
  });

  it('attaches status 429 and keeps the Retry-After message', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ error: 'too many requests' }),
      { status: 429, headers: { 'Content-Type': 'application/json', 'Retry-After': '7' } },
    )));

    const err = await claimMatchSeat({ matchId: 'match_x', guestId: 'guest-1' })
      .then(() => null)
      .catch((e: HttpError) => e);

    expect(err?.message).toContain('rate limited, retry after 7s');
    expect(err?.status).toBe(429);
  });

  it('falls back to a generic message but still attaches the status when the body is not JSON', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(
      'gateway timeout',
      { status: 504 },
    )));

    const err = await claimMatchSeat({ matchId: 'match_x', guestId: 'guest-1' })
      .then(() => null)
      .catch((e: HttpError) => e);

    expect(err?.message).toBe('Request failed with 504');
    expect(err?.status).toBe(504);
  });
});
