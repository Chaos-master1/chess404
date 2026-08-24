import { test, expect, request as pwRequest } from '@playwright/test';

// The public watch feed and the anonymous snapshot endpoint are the two places
// a stranger can read match data. Neither may expose seat secrets or hands, and
// the feed must not advertise private or vs-computer games.
// The web proxy refuses direct match creation in production, so the anonymous
// read is exercised against match-service's own public origin -- which is the
// surface a stranger can actually reach.
const MATCH_SERVICE =
  process.env.E2E_MATCH_SERVICE_URL ?? 'https://match-service-production.up.railway.app';

test.describe('spectator privacy', () => {
  test('anonymous match reads leak neither secrets nor hands', async ({ baseURL }) => {
    const api = await pwRequest.newContext({ baseURL: MATCH_SERVICE });

    const created = await api.post('/api/matches', {
      data: { modeId: 'open_cards', queue: 'casual' },
      headers: { 'content-type': 'application/json', origin: baseURL ?? '' },
    });
    expect(created.ok(), `match creation failed: ${created.status()}`).toBeTruthy();
    const matchId = (await created.json())?.match?.matchId as string;
    expect(matchId).toBeTruthy();

    const anon = await api.get(`/api/matches/${matchId}`);
    expect(anon.ok()).toBeTruthy();
    const raw = await anon.text();
    const snapshot = JSON.parse(raw);

    expect(snapshot.match?.whitePlayerSecret ?? '', 'white seat secret exposed').toBe('');
    expect(snapshot.match?.blackPlayerSecret ?? '', 'black seat secret exposed').toBe('');
    expect((snapshot.match?.whiteHand ?? []).length, 'white hand exposed to anonymous reader').toBe(0);
    expect((snapshot.match?.blackHand ?? []).length, 'black hand exposed to anonymous reader').toBe(0);
    expect(raw, 'a secret-shaped field survived redaction').not.toMatch(/"(white|black)PlayerSecret":"[^"]+"/);

    await api.dispose();
  });

  test('the public watch feed excludes private and vs-computer games', async ({ baseURL }) => {
    const api = await pwRequest.newContext({ baseURL });
    const response = await api.get('/api/platform/matches?status=finished&limit=50');
    expect(response.ok()).toBeTruthy();
    const payload = await response.json();
    const entries: Array<Record<string, unknown>> = payload?.matches ?? [];
    const leaked = entries.filter(e => e.queue === 'direct' || e.modeId === 'computer' || e.finishReason === 'abort');
    expect(leaked, 'private/computer/aborted matches are visible in the public feed').toEqual([]);
    await api.dispose();
  });
});
