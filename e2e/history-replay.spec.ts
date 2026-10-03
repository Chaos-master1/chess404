import { test, expect } from '@playwright/test';
import { collectErrors, dismissOnboarding, move, registerAccount, uniqueE2EHandle } from './_helpers';

test.describe('history and replay', () => {
  test('a finished match is archived and replayable', async ({ page }) => {
    test.setTimeout(420_000);
    const errors = collectErrors(page);

    // History is deliberately an account feature (guests get a sign-up gate),
    // so the spec signs in first. Registration claims this browser's guest,
    // which is what attributes the archived computer game to the account.
    const handle = uniqueE2EHandle();
    await registerAccount(page, {
      handle,
      email: `${handle}@example.com`,
      password: 'Chess404-e2e-passw0rd!',
    });

    await page.goto('/play');
    await dismissOnboarding(page, 'btn-play-computer');
    // One entry point only: the hub's difficulty grid would start a second,
    // conflicting match instead of opening this one.
    await page.getByTestId('btn-play-computer').click();
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
