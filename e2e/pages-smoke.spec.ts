import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
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
  '/privacy',
  '/terms',
];

// Routes that must NOT be reachable in production.
const BLOCKED_ROUTES = ['/dashboard'];

// Accessibility smoke (WCAG 2.0/2.1 A+AA). A CRITICAL violation means a page
// is broken for assistive technology at the structural level (missing page
// language, unlabeled form controls, empty buttons) and blocks this gate;
// lower-impact findings are logged with the route so the report shows the
// debt without failing the run -- the gate hardens to them once the backlog
// is triaged. Auth-gated routes (/account, /friends, /inbox) render their
// signed-out states here, which is still the structure users first see.
test.describe('accessibility axe smoke', () => {
  for (const route of ROUTES) {
    test(`${route} has no critical WCAG A/AA violations`, async ({ page }) => {
      await page.goto(route, { waitUntil: 'domcontentloaded' });
      await dismissOnboarding(page);
      // Let the client shell finish its first data fetches so asynchronously
      // rendered content is scanned too.
      await page.waitForTimeout(4_000);

      const results = await new AxeBuilder({ page })
        .withTags(['wcag2a', 'wcag2aa'])
        .analyze();

      const critical = results.violations.filter(v => v.impact === 'critical');
      const lower = results.violations.filter(v => v.impact && v.impact !== 'critical');
      if (lower.length > 0) {
        const summary = lower
          .map(v => `${v.impact}/${v.id}: ${v.nodes.length} node(s)`)
          .join(', ');
        console.log(`[axe] ${route} non-critical findings -> ${summary}`);
      }
      expect(critical, `${route} critical a11y violations`).toEqual([]);
    });
  }
});

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

test.describe('account page identity', () => {
  test('account page never shows seat-panel ghosts or raw identity dumps', async ({ page }) => {
    const errors = collectErrors(page);
    await page.goto('/account', { waitUntil: 'domcontentloaded' });
    await dismissOnboarding(page);
    await page.waitForTimeout(4_000);

    const body = (await page.locator('body').innerText().catch(() => '')) ?? '';

    // The local-sandbox "two seats" vocabulary must never leak onto the
    // hosted account page (ghost panel read a stale second-seat slot).
    expect(body).not.toMatch(/white seat account/i);
    expect(body).not.toMatch(/black seat account/i);

    // Raw internal identifiers stay behind the collapsed disclosure, and the
    // disclosure is closed by default, so the first paint shows none of them.
    expect(body).not.toMatch(/guest_[0-9a-f]{8,}/);
    expect(body).not.toMatch(/accttok_/);

    expect(errors.pageErrors, 'account page uncaught exceptions').toEqual([]);
  });
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
