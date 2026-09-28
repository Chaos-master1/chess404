import { describe, expect, it } from 'vitest';

import { buildSessionSecretCookies } from './cookies';

const baseGuest = { guest: { guestId: 'guest_1', displayName: 'A' } };

function payloadWith(overrides: { white?: unknown; black?: unknown }) {
  return { guestSessions: { white: overrides.white, black: overrides.black } };
}

describe('buildSessionSecretCookies', () => {
  it('mints HttpOnly Secure Lax secret and guest-id cookies per seat', () => {
    const cookies = buildSessionSecretCookies(
      payloadWith({
        white: { ...baseGuest, sessionSecret: 'sec_white' },
        black: { ...baseGuest, sessionSecret: 'sec_black' },
      }),
    );
    expect(cookies).toEqual([
      'session_secret_white=sec_white; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=31536000',
      `session_guest_white=${encodeURIComponent('guest_1')}; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=31536000`,
      'session_secret_black=sec_black; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=31536000',
      `session_guest_black=${encodeURIComponent('guest_1')}; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=31536000`,
    ]);
  });

  it('mints only the guest-id cookie when the resumed session redacted the secret', () => {
    const cookies = buildSessionSecretCookies(
      payloadWith({
        white: { ...baseGuest, sessionSecret: '' },
        black: undefined,
      }),
    );
    expect(cookies).toEqual([
      `session_guest_white=${encodeURIComponent('guest_1')}; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=31536000`,
    ]);
  });

  it('emits nothing for a seat with no session', () => {
    expect(buildSessionSecretCookies(payloadWith({ white: undefined, black: undefined }))).toEqual([]);
  });

  it('tolerates a null, missing, or malformed payload', () => {
    expect(buildSessionSecretCookies(null)).toEqual([]);
    expect(buildSessionSecretCookies({})).toEqual([]);
    expect(buildSessionSecretCookies({ guestSessions: { white: 'garbage', black: 42 } })).toEqual([]);
  });
});
