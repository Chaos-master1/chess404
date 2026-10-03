import { test, expect, type Page } from '@playwright/test';
import { collectErrors, dismissOnboarding, registerAccount, uniqueE2EHandle } from './_helpers';

// Both auth surfaces render a "Sign In" control and one of them is a disabled
// tab, so click whichever instance is actually enabled.
async function clickSignIn(page: Page) {
  const buttons = page.getByRole('button', { name: /^sign in$/i });
  const count = await buttons.count();
  for (let i = count - 1; i >= 0; i--) {
    if (await buttons.nth(i).isEnabled().catch(() => false)) {
      await buttons.nth(i).click();
      return;
    }
  }
  throw new Error('no enabled Sign In control on the page');
}

test.describe('account auth', () => {
  test('register, sign out, sign back in, and reject a bad password', async ({ page }) => {
    test.setTimeout(300_000);
    const errors = collectErrors(page);
    const handle = uniqueE2EHandle();
    const email = `${handle}@example.com`;
    const password = 'Chess404-e2e-passw0rd!';

    // Leaves the context signed in on /account; the handle showing there is
    // the session proof this spec builds on.
    await registerAccount(page, { handle, email, password });

    // Two controls share this accessible name: the per-other-device revoke
    // buttons in the sessions list, and this device's own Sign Out beside the
    // seat sign-in form. The latter is last in the DOM.
    await page.getByRole('button', { name: /^sign out$/i }).last().click();
    await page.waitForTimeout(4_000);
    await page.goto('/account');
    await dismissOnboarding(page);

    // Signing out can leave either surface mounted: the standalone auth form
    // ("wizard404error or you@example.com") or the account page's own seat
    // sign-in ("aurora_fox or player@example.com"). Accept whichever is there.
    // The standalone auth surface tabs between register/login/recover; the
    // account surface shows its seat sign-in form directly.
    const loginTab = page.getByTestId('auth-tab-login')
      .or(page.getByRole('button', { name: /^sign in$/i }).nth(1))
      .first();
    if (await loginTab.isVisible({ timeout: 15_000 }).catch(() => false)) {
      await loginTab.click().catch(() => {});
    }
    const identifier = page.getByPlaceholder(/or (you|player)@example\.com/).first();
    const secret = page.getByPlaceholder(/your account password|your password/i).first();
    await expect(identifier, 'no sign-in form after signing out').toBeVisible({ timeout: 30_000 });

    // Wrong password must be refused.
    await identifier.fill(handle);
    await secret.fill('definitely-not-the-password');
    await clickSignIn(page);
    await page.waitForTimeout(6_000);
    await expect(
      page.getByText(new RegExp(`@${handle}`)),
      'a wrong password produced a signed-in session',
    ).toHaveCount(0);

    // Right password must work. Signing in navigates to the play hub, so come
    // back to the account surface to confirm the session exists.
    await secret.fill(password);
    await clickSignIn(page);
    await page.waitForTimeout(6_000);
    await page.goto('/account');
    await dismissOnboarding(page);
    await expect(
      page.getByText(new RegExp(`@${handle}`)).first(),
      'valid credentials did not sign in',
    ).toBeVisible({ timeout: 60_000 });

    expect(errors.pageErrors, 'uncaught exceptions in the auth flow').toEqual([]);
  });
});
