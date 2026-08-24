import { test, expect, request as pwRequest } from '@playwright/test';

// Every account-scoped surface must demand a session token, and the admin
// surfaces must additionally demand admin standing. An accountId is public
// enough to guess, so accountId-only access would be a full authz bypass.
const ACCOUNT_SCOPED = [
  '/api/platform/moderation/admin/overview',
  '/api/platform/moderation/admin/reports/resolve',
  '/api/platform/moderation/overview',
  '/api/platform/email-outbox/overview',
  '/api/platform/account-sessions/overview',
  '/api/platform/account-security/overview',
  '/api/platform/inbox/overview',
  '/api/platform/friends/overview',
  '/api/platform/challenges/overview',
];

test.describe('authorization', () => {
  test('account-scoped endpoints reject a forged accountId with no session token', async ({ baseURL }) => {
    const api = await pwRequest.newContext({ baseURL });
    const offenders: string[] = [];

    for (const path of ACCOUNT_SCOPED) {
      const response = await api.post(path, {
        data: { accountId: 'acct_0000000000000000', limit: 5 },
        headers: { 'content-type': 'application/json' },
      });
      // 401/403/404 are all acceptable refusals; a 200 is not.
      if (response.status() === 200) {
        offenders.push(`${path} -> 200 ${(await response.text()).slice(0, 120)}`);
      }
    }

    expect(offenders, 'endpoints served data for an unauthenticated accountId').toEqual([]);
    await api.dispose();
  });

  test('a guest session cannot be resumed from its public guest id alone', async ({ baseURL }) => {
    const api = await pwRequest.newContext({ baseURL });
    const list = await api.get('/api/platform/guests?limit=1');
    expect(list.ok()).toBeTruthy();
    const guestId = (await list.json())?.guests?.[0]?.guestId as string | undefined;
    test.skip(!guestId, 'no guests published yet');

    for (const body of [{ guestId }, { guestId, sessionSecret: 'wrong' }, { guestId, sessionToken: 'wrong' }]) {
      const response = await api.post('/api/platform/guest-sessions', {
        data: body,
        headers: { 'content-type': 'application/json' },
      });
      expect(response.status(), `guest takeover accepted for ${JSON.stringify(body)}`).not.toBe(200);
    }
    await api.dispose();
  });
});
