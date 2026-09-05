// @vitest-environment node
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  readStoredGuestIdentity,
  resolveGuestSessionWrite,
  writeStoredGuestIdentity,
} from './session-storage';

// session-storage no-ops without a window; this shim exercises the real
// localStorage read/write paths inside the node environment.
function stubLocalStorage() {
  const backing = new Map<string, string>();
  const storage = {
    getItem: (key: string) => backing.get(key) ?? null,
    setItem: (key: string, value: string) => void backing.set(key, value),
    removeItem: (key: string) => void backing.delete(key),
    clear: () => backing.clear(),
  };
  vi.stubGlobal('window', { localStorage: storage });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('resolveGuestSessionWrite', () => {
  it('keeps stored credentials when the response resumes the same guest with redacted credentials', () => {
    const resolved = resolveGuestSessionWrite(
      { guestId: 'guest_a', sessionSecret: '', sessionToken: undefined },
      { guestId: 'guest_a', sessionSecret: 'guestsess_live', sessionToken: 'guesttok_live' },
    );
    expect(resolved.sessionSecret).toBe('guestsess_live');
    expect(resolved.sessionToken).toBe('guesttok_live');
  });

  it('overwrites credentials when the response carries a fresh secret', () => {
    const resolved = resolveGuestSessionWrite(
      { guestId: 'guest_a', sessionSecret: 'guestsess_rotated', sessionToken: 'guesttok_new' },
      { guestId: 'guest_a', sessionSecret: 'guestsess_old', sessionToken: 'guesttok_old' },
    );
    expect(resolved.sessionSecret).toBe('guestsess_rotated');
    expect(resolved.sessionToken).toBe('guesttok_new');
  });

  it('wipes credentials for a different guest id without a secret', () => {
    const resolved = resolveGuestSessionWrite(
      { guestId: 'guest_b', sessionSecret: '' },
      { guestId: 'guest_a', sessionSecret: 'guestsess_old', sessionToken: 'guesttok_old' },
    );
    expect(resolved.sessionSecret).toBe('');
    expect(resolved.sessionToken).toBeNull();
  });
});

describe('writeStoredGuestIdentity round-trip', () => {
  it('survives a resume-ack re-write without losing the secret', () => {
    stubLocalStorage();
    writeStoredGuestIdentity('white', 'guest_a', 'guestsess_live', { sessionToken: 'guesttok_live' });
    const stored = readStoredGuestIdentity('white');
    const resolved = resolveGuestSessionWrite(
      { guestId: 'guest_a', sessionSecret: '' },
      stored,
    );
    writeStoredGuestIdentity('white', resolved.guestId, resolved.sessionSecret, {
      sessionToken: resolved.sessionToken,
      sessionExpiresAt: null,
    });
    expect(readStoredGuestIdentity('white')).toMatchObject({
      guestId: 'guest_a',
      sessionSecret: 'guestsess_live',
      sessionToken: 'guesttok_live',
    });
  });
});
