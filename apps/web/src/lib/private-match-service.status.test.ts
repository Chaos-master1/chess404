// @vitest-environment node
import { afterEach, describe, expect, it, vi } from 'vitest';
import { joinPrivateMatch, type HttpError } from './private-match-service';

afterEach(() => {
  vi.unstubAllGlobals();
});

// joinPrivateMatch failures are swallowed silently by the facade's fallback
// chain, but the error must still carry the HTTP status so any future caller
// (and the facade, if it ever inspects it) sees the server's verdict.
describe('private-match-service error status contract', () => {
  it('attaches status 404 to a join against a nonexistent room', async () => {
    vi.stubGlobal('window', { localStorage: new MemoryStorage() });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ error: 'unknown private match' }),
      { status: 404, headers: { 'Content-Type': 'application/json' } },
    )));

    const err = await joinPrivateMatch({ matchId: 'match_gone', identity: {} })
      .then(() => null)
      .catch((e: HttpError) => e);

    expect(err?.message).toBe('unknown private match');
    expect(err?.status).toBe(404);
  });
});

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
