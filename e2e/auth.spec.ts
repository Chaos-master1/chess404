import { test, expect } from '@playwright/test';
import { collectErrors, dismissOnboarding } from './_helpers';

// Unique per run so the spec can be re-run against the same production DB.
function uniqueHandle(): string {
  return `e2e_${Date.now().toString(36)}${Math.floor(Math.random() * 1e4).toString(36)}`.slice(0, 24);
}

test.describe('account auth', () => {
  test('register, sign out, sign back in, and reject a bad password', async ({ page }) => {
    test.setTimeout(300_000);
    const errors = collectErrors(page);
    const handle = uniqueHandle();
    const email = `${handle}@example.com`;
    const password = 'Chess404-e2e-passw0rd!';

    await page.goto('/account');
    await dismissOnboarding(page);

    const registerTab = page.getByRole('button', { name: /^create account$/i }).first();
    if (await registerTab.isVisible({ timeout: 15_000 }).catch(() => false)) {
      await registerTab.click().catch(() => {});
    }

    await page.getByPlaceholder('wizard404error').fill(handle);
    await page.getByPlaceholder('you@example.com').fill(email);
    await page.getByPlaceholder('Choose a strong password').fill(password);
    await page.getByRole('button', { name: /create account/i }).last().click();

    // Signed-in state is identified by the device sign-out control.
    const signOut = page.getByRole('button', { name: /sign out on this device/i });
    await expect(signOut, 'registration did not produce a signed-in session').toBeVisible({ timeout: 60_000 });

    await signOut.click();
    await page.waitForTimeout(3_000);

    const loginTab = page.getByRole('button', { name: /^sign in$/i }).first();
    if (await loginTab.isVisible({ timeout: 15_000 }).catch(() => false)) {
      await loginTab.click().catch(() => {});
    }

    // Wrong password must be refused.
    await page.getByPlaceholder('wizard404error or you@example.com').first().fill(handle);
    await page.getByPlaceholder('Your account password').fill('definitely-not-the-password');
    await page.getByRole('button', { name: /^sign in$/i }).last().click();
    await page.waitForTimeout(5_000);
    await expect(
      page.getByRole('button', { name: /sign out on this device/i }),
      'a wrong password produced a signed-in session',
    ).toHaveCount(0);

    // Right password must work.
    await page.getByPlaceholder('Your account password').fill(password);
    await page.getByRole('button', { name: /^sign in$/i }).last().click();
    await expect(
      page.getByRole('button', { name: /sign out on this device/i }),
      'valid credentials did not sign in',
    ).toBeVisible({ timeout: 60_000 });

    expect(errors.pageErrors, 'uncaught exceptions in the auth flow').toEqual([]);
  });
});
