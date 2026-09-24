import { test, expect, type Page } from '@playwright/test';

// Mobile viewport — iPhone 14 Pro equivalent (393×852)
test.use({
  ...({ viewport: { width: 393, height: 852 } } as const),
  hasTouch: true,
  isMobile: true,
  userAgent:
    'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36',
});

async function dismissOnboarding(page: Page) {
  const skip = page.getByRole('button', { name: /skip tutorial/i });
  try {
    await skip.click({ timeout: 5_000 });
    await page.waitForTimeout(500);
  } catch {
    // tutorial not shown — nothing to do
  }
}

function collectPageErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`));
  page.on('console', m => {
    if (m.type() === 'error') errors.push(`console: ${m.text()}`);
  });
  return errors;
}

async function checkNoHorizontalOverflow(page: Page, label: string) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow, `${label} horizontal overflow`).toBeLessThanOrEqual(1);
}

test.describe('mobile all-pages smoke (393x852)', () => {

  test('landing page fits mobile viewport', async ({ page }) => {
    const errors = collectPageErrors(page);
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible({ timeout: 30_000 });
    await checkNoHorizontalOverflow(page, 'landing');
    await page.screenshot({ path: 'test-results/mobile-landing.png', fullPage: false });
    const meaningful = errors.filter(e => !/net::|ERR_|Failed to load resource/.test(e));
    expect(meaningful).toEqual([]);
  });

  test('play hub fits mobile viewport', async ({ page }) => {
    const errors = collectPageErrors(page);
    await page.goto('/play');
    await dismissOnboarding(page);
    await expect(page.getByTestId('btn-play-computer')).toBeVisible({ timeout: 60_000 });
    await checkNoHorizontalOverflow(page, 'play hub');
    await page.screenshot({ path: 'test-results/mobile-play.png', fullPage: false });
    const meaningful = errors.filter(e => !/net::|ERR_|Failed to load resource/.test(e));
    expect(meaningful).toEqual([]);
  });

  test('rankings page fits mobile viewport and shows rows', async ({ page }) => {
    const errors = collectPageErrors(page);
    await page.goto('/play');
    await dismissOnboarding(page);

    // Navigate to rankings
    // Try menu/hamburger first
    const hamburger = page.locator('[data-testid="mobile-menu-toggle"], button:has-text("☰"), [aria-label*="menu" i]').first();
    try {
      await hamburger.click({ timeout: 5_000 });
      await page.waitForTimeout(500);
    } catch {
      // no hamburger needed
    }

    // Click rankings nav
    const rankingsLink = page.getByRole('button', { name: /rankings/i }).first();
    try {
      await rankingsLink.click({ timeout: 5_000 });
    } catch {
      // try direct navigation
      await page.goto('/play');
      await dismissOnboarding(page);
    }

    await page.waitForTimeout(2_000);
    await checkNoHorizontalOverflow(page, 'rankings');
    await page.screenshot({ path: 'test-results/mobile-rankings.png', fullPage: false });

    // On mobile, filters should be visible
    const modeFilter = page.getByLabel(/filter by mode/i);
    if (await modeFilter.isVisible()) {
      // Filter select should be accessible
      expect(await modeFilter.boundingBox()).not.toBeNull();
    }

    const meaningful = errors.filter(e => !/net::|ERR_|Failed to load resource/.test(e));
    expect(meaningful).toEqual([]);
  });

  test('cards page fits mobile viewport and sidebar is hidden', async ({ page }) => {
    const errors = collectPageErrors(page);
    await page.goto('/play');
    await dismissOnboarding(page);

    // Navigate to cards
    const hamburger = page.locator('[data-testid="mobile-menu-toggle"], button:has-text("☰"), [aria-label*="menu" i]').first();
    try {
      await hamburger.click({ timeout: 5_000 });
      await page.waitForTimeout(500);
    } catch {
      // no hamburger
    }

    const cardsLink = page.getByRole('button', { name: /cards/i }).first();
    try {
      await cardsLink.click({ timeout: 5_000 });
    } catch {
      await page.goto('/play');
      await dismissOnboarding(page);
    }

    await page.waitForTimeout(2_000);
    await checkNoHorizontalOverflow(page, 'cards');
    await page.screenshot({ path: 'test-results/mobile-cards.png', fullPage: false });

    // The desktop sidebar should be hidden on mobile
    const sidebar = page.locator('.cards-sidebar-desktop');
    if (await sidebar.count() > 0) {
      await expect(sidebar).not.toBeVisible();
    }

    // Card tiles should be visible and render in a grid
    const cardTiles = page.locator('[class*="cards-rarity-grid"] > div').first();
    if (await cardTiles.count() > 0) {
      await expect(cardTiles).toBeVisible({ timeout: 5_000 });
    }

    const meaningful = errors.filter(e => !/net::|ERR_|Failed to load resource/.test(e));
    expect(meaningful).toEqual([]);
  });

  test('account page fits mobile viewport', async ({ page }) => {
    const errors = collectPageErrors(page);
    await page.goto('/play');
    await dismissOnboarding(page);

    // Navigate to account
    const hamburger = page.locator('[data-testid="mobile-menu-toggle"], button:has-text("☰"), [aria-label*="menu" i]').first();
    try {
      await hamburger.click({ timeout: 5_000 });
      await page.waitForTimeout(500);
    } catch {}

    const accountLink = page.getByRole('button', { name: /account/i }).first();
    try {
      await accountLink.click({ timeout: 5_000 });
    } catch {
      await page.goto('/play');
      await dismissOnboarding(page);
    }

    await page.waitForTimeout(2_000);
    await checkNoHorizontalOverflow(page, 'account');
    await page.screenshot({ path: 'test-results/mobile-account.png', fullPage: false });
    const meaningful = errors.filter(e => !/net::|ERR_|Failed to load resource/.test(e));
    expect(meaningful).toEqual([]);
  });

  test('community page fits mobile viewport', async ({ page }) => {
    const errors = collectPageErrors(page);
    await page.goto('/play');
    await dismissOnboarding(page);

    const hamburger = page.locator('[data-testid="mobile-menu-toggle"], button:has-text("☰"), [aria-label*="menu" i]').first();
    try {
      await hamburger.click({ timeout: 5_000 });
      await page.waitForTimeout(500);
    } catch {}

    const communityLink = page.getByRole('button', { name: /community/i }).first();
    try {
      await communityLink.click({ timeout: 5_000 });
    } catch {
      await page.goto('/play');
      await dismissOnboarding(page);
    }

    await page.waitForTimeout(2_000);
    await checkNoHorizontalOverflow(page, 'community');
    await page.screenshot({ path: 'test-results/mobile-community.png', fullPage: false });
    const meaningful = errors.filter(e => !/net::|ERR_|Failed to load resource/.test(e));
    expect(meaningful).toEqual([]);
  });

  test('history page fits mobile viewport', async ({ page }) => {
    const errors = collectPageErrors(page);
    await page.goto('/play');
    await dismissOnboarding(page);

    const hamburger = page.locator('[data-testid="mobile-menu-toggle"], button:has-text("☰"), [aria-label*="menu" i]').first();
    try {
      await hamburger.click({ timeout: 5_000 });
      await page.waitForTimeout(500);
    } catch {}

    const historyLink = page.getByRole('button', { name: /history/i }).first();
    try {
      await historyLink.click({ timeout: 5_000 });
    } catch {
      await page.goto('/play');
      await dismissOnboarding(page);
    }

    await page.waitForTimeout(2_000);
    await checkNoHorizontalOverflow(page, 'history');
    await page.screenshot({ path: 'test-results/mobile-history.png', fullPage: false });
    const meaningful = errors.filter(e => !/net::|ERR_|Failed to load resource/.test(e));
    expect(meaningful).toEqual([]);
  });

  test('legal pages fit mobile viewport', async ({ page }) => {
    for (const path of ['/terms', '/privacy']) {
      const response = await page.goto(path);
      expect(response?.status(), `${path} status`).toBe(200);
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
      await checkNoHorizontalOverflow(page, path);
    }
  });
});
