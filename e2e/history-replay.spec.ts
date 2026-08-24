import { test, expect } from '@playwright/test';
import { collectErrors, dismissOnboarding, move } from './_helpers';

test.describe('history and replay', () => {
  test('a finished match is archived and replayable', async ({ page }) => {
    test.setTimeout(300_000);
    const errors = collectErrors(page);

    await page.goto('/play');
    await dismissOnboarding(page, 'btn-play-computer');
    await page.getByTestId('btn-play-computer').click();
    const beginner = page.getByRole('button', { name: /beginner/i }).first();
    if (await beginner.isVisible({ timeout: 20_000 }).catch(() => false)) await beginner.click();
    await expect(page.getByTestId('board-root')).toBeVisible({ timeout: 90_000 });

    // Play a couple of moves so the archive has real content, then resign.
    await move(page, 'e2', 'e4');
    await page.waitForTimeout(9_000);
    await move(page, 'd2', 'd4');
    await page.waitForTimeout(9_000);

    page.once('dialog', d => void d.accept());
    await page.getByTestId('btn-resign').click();
    await page.waitForTimeout(8_000);

    await page.goto('/history');
    await dismissOnboarding(page);
    await page.waitForTimeout(8_000);

    const body = await page.locator('body').innerText();
    expect(body, 'history page did not render its own heading').toMatch(/match history/i);
    // An archived match must actually be listed -- an empty-state message here
    // means the finished game never reached the archive.
    expect(body, 'the match just finished is not listed in history')
      .not.toMatch(/no (matches|games|history)/i);
    // The replay surface must be reachable for it.
    expect(body, 'no replay frames exposed for the archived match').toMatch(/replay frame/i);

    expect(errors.pageErrors, 'uncaught exceptions on history').toEqual([]);
    expect(errors.csp, 'CSP violations on history').toEqual([]);
  });
});
