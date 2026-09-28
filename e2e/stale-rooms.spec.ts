import { test, expect } from '@playwright/test';
import { collectErrors } from './_helpers';

// The stale-room UX contract verified in the deep-debug sweep: a match id
// that does not exist (bad invite link, finished room) must render the
// graceful "no longer exists" card and log a WARNING, never an error-level
// bootstrap failure. The one resource-level 404 per fallback step (claim ->
// join -> fetchMatch) is the browser's own log line, not an app error.
test.describe('gone matches', () => {
  test('never-existent match id renders the graceful unavailable card', async ({ page }) => {
    const errors = collectErrors(page);
    const warnings: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'warning') warnings.push(msg.text());
    });

    await page.goto('/match/match_doesnotexist_123');

    await expect(page.getByText('This match no longer exists or has finished.')).toBeVisible();
    expect(
      warnings.some(w => w.includes('match unavailable (404)')),
      'gone rooms must be latched with a warning-level log',
    ).toBe(true);
    expect(
      errors.console.filter(t => t.includes('bootstrapAuthoritativeMatch')),
      'gone rooms must never log an error-level bootstrap failure',
    ).toEqual([]);
    expect(errors.pageErrors, 'no uncaught exceptions').toEqual([]);
  });
});

// /lobbies has no Next route (the lobbies view lives inside /play). The 404
// page used to mount the SPA shell underneath itself, which started the
// matchmaking polling loop on an error page; App.tsx no longer maps the dead
// pathname, so no matchmaking request may fire while the 404 is up.
test.describe('orphaned routes', () => {
  test('/lobbies renders the 404 page without spawning queue polling', async ({ page }) => {
    const matchmakingCalls: string[] = [];
    page.on('request', req => {
      if (req.url().includes('/api/matchmaking')) {
        matchmakingCalls.push(req.url());
      }
    });

    const response = await page.goto('/lobbies');
    expect(response?.status(), '/lobbies should 404 at the document level').toBe(404);
    await expect(page.getByText('Page not found')).toBeVisible();

    // Outlive one queue-poll interval (2.5s) to catch late polling starts.
    await page.waitForTimeout(3_000);
    expect(matchmakingCalls, 'the 404 page must not mount the play-hub polling loop').toEqual([]);
  });
});
