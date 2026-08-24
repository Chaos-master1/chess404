import { test, expect } from '@playwright/test';
import { collectErrors, dismissOnboarding } from './_helpers';

// Every public route the app ships. A route that throws, violates its own CSP,
// or scrolls horizontally on a phone is a launch defect regardless of whether
// any other spec exercises its feature.
const ROUTES = [
  '/',
  '/play',
  '/queue',
  '/cards',
  '/history',
  '/watch',
  '/rankings',
  '/community',
  '/friends',
  '/inbox',
  '/profiles',
  '/account',
  '/status',
  '/admin',
];

// Routes that must NOT be reachable in production.
const BLOCKED_ROUTES = ['/dashboard'];

test.describe('all routes render clean', () => {
  for (const route of ROUTES) {
    test(`${route} has no console errors or CSP violations`, async ({ page }) => {
      const errors = collectErrors(page);
      const response = await page.goto(route, { waitUntil: 'domcontentloaded' });
      expect(response?.status(), `${route} HTTP status`).toBeLessThan(400);
      await dismissOnboarding(page);
      // Let the client shell finish its first data fetches.
      await page.waitForTimeout(4_000);

      // The app shell must actually have rendered something.
      const bodyText = (await page.locator('body').innerText().catch(() => '')) ?? '';
      expect(bodyText.trim().length, `${route} rendered empty`).toBeGreaterThan(0);
      await expect(page.getByText(/application error|something went wrong/i)).toHaveCount(0);

      expect(errors.pageErrors, `${route} uncaught exceptions`).toEqual([]);
      expect(errors.csp, `${route} CSP violations`).toEqual([]);
      expect(errors.console, `${route} console errors`).toEqual([]);
    });
  }
});

test.describe('dev-only routes stay private', () => {
  for (const route of BLOCKED_ROUTES) {
    test(`${route} is not served in production`, async ({ page }) => {
      const response = await page.goto(route, { waitUntil: 'domcontentloaded' });
      expect(response?.status(), `${route} should 404 in production`).toBe(404);
    });
  }
});

test.describe('mobile layout', () => {
  test('key routes fit a 390x844 phone viewport', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    const offenders: string[] = [];
    for (const route of ['/', '/play', '/cards', '/history', '/rankings']) {
      await page.goto(route, { waitUntil: 'domcontentloaded' });
      await dismissOnboarding(page);
      await page.waitForTimeout(2_500);
      const overflow = await page.evaluate(() => ({
        scrollWidth: document.documentElement.scrollWidth,
        clientWidth: document.documentElement.clientWidth,
      }));
      if (overflow.scrollWidth > overflow.clientWidth + 1) {
        offenders.push(`${route}: ${overflow.scrollWidth}px content in ${overflow.clientWidth}px viewport`);
      }
    }
    expect(offenders, 'routes overflowing the phone viewport').toEqual([]);
  });
});
