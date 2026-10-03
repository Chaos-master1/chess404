import { test, expect } from '@playwright/test';
import { collectErrors, dismissOnboarding, move } from './_helpers';

test.describe('match resilience', () => {
  test('a live match survives reload and an offline/online cycle', async ({ page, context }) => {
    test.setTimeout(300_000);
    const errors = collectErrors(page);

    await page.goto('/play');
    await dismissOnboarding(page, 'btn-play-computer');
    // One entry point only: the hub's difficulty grid would start a SECOND
    // match, and that creation raced the first navigation (detached button,
    // 409 conflict) instead of opening the board.
    await page.getByTestId('btn-play-computer').click();
    await expect(page.getByTestId('board-root')).toBeVisible({ timeout: 90_000 });

    await move(page, 'e2', 'e4');
    await page.waitForTimeout(10_000);
    const matchUrl = page.url();

    // 1. Hard reload must restore the same live match, still playable.
    await page.reload();
    await dismissOnboarding(page);
    await expect(page.getByTestId('board-root')).toBeVisible({ timeout: 90_000 });
    await expect(page.getByTestId('btn-resign')).toBeVisible({ timeout: 60_000 });
    await expect(page.getByText(/missing player credentials/i)).toHaveCount(0);

    // 2. Network drop and recovery: the stream must come back on its own.
    await context.setOffline(true);
    await page.waitForTimeout(8_000);
    await context.setOffline(false);
    await page.waitForTimeout(15_000);

    // A move landing after the reconnect is the real proof the stream healed.
    await move(page, 'd2', 'd4');
    await page.waitForTimeout(10_000);
    await expect(page.getByText(/cannot connect/i)).toHaveCount(0);
    expect(page.url()).toBe(matchUrl);

    expect(errors.pageErrors, 'uncaught exceptions across reconnect').toEqual([]);
  });
});
