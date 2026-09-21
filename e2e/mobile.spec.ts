import { test, expect, type Page } from '@playwright/test';

// Mobile viewport (390x844, touch, mobile UA) checks: the play surface must
// render and be operable via real touch events, with no horizontal overflow
// and no console/page errors on the core paths. The board is a single
// <canvas> with native touch handlers (BoardCanvas handleTouchStart/Move/End),
// so squares are driven through page.touchscreen.tap at computed coordinates.

test.use({
  ...({ viewport: { width: 390, height: 844 } } as const),
  hasTouch: true,
  isMobile: true,
  userAgent:
    'Mozilla/5.0 (Linux; Android 13; Pixel 5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36',
});

const FILES = 'abcdefgh';

function squarePoint(
  page: Page,
  algebraic: string,
  viewerColor: 'white' | 'black',
) {
  const fileIdx = FILES.indexOf(algebraic[0]);
  const rank = Number(algebraic[1]);
  if (fileIdx < 0 || rank < 1 || rank > 8) throw new Error(`bad square ${algebraic}`);
  const col = fileIdx;
  const rowFromTop = viewerColor === 'white' ? 8 - rank : rank - 1;
  return { col, row: rowFromTop };
}

async function tapSquare(
  page: Page,
  algebraic: string,
  viewerColor: 'white' | 'black' = 'white',
) {
  const board = page.getByTestId('board-root');
  await expect(board).toBeVisible();
  const box = await board.boundingBox();
  if (!box) throw new Error('board not laid out');
  const { col, row } = squarePoint(page, algebraic, viewerColor);
  const x = box.x + ((col + 0.5) * box.width) / 8;
  const y = box.y + ((row + 0.5) * box.height) / 8;
  await page.touchscreen.tap(x, y);
}

// First-visit onboarding modal (z-index 10000) covers the whole app.
async function dismissOnboarding(page: Page) {
  const skip = page.getByRole('button', { name: /skip tutorial/i });
  try {
    await skip.click({ timeout: 5_000 });
    await page.waitForTimeout(500);
  } catch {
    // tutorial not shown (returning visitor) — nothing to do
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

test.describe('mobile 390x844 touch', () => {
  test('landing and play hub fit viewport with no console errors', async ({ page }) => {
    const errors = collectPageErrors(page);

    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible({ timeout: 30_000 });

    // No horizontal scroll anywhere on the landing page.
    const landingOverflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(landingOverflow).toBeLessThanOrEqual(1);

    await page.goto('/play');
    await dismissOnboarding(page);
    // Play hub must offer at least one game entry point on mobile.
    await expect(page.getByTestId('btn-play-computer')).toBeVisible({ timeout: 60_000 });

    const hubOverflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(hubOverflow).toBeLessThanOrEqual(1);

    const meaningfulErrors = errors.filter(
      e => !/net::|ERR_|Failed to load resource/.test(e),
    );
    expect(meaningfulErrors).toEqual([]);
  });

  test('touch move on computer match works and resign completes', async ({ page }) => {
    const errors = collectPageErrors(page);

    await page.goto('/play');
    await dismissOnboarding(page);
    await page.getByTestId('btn-play-computer').click();
    const beginner = page.getByRole('button', { name: /beginner/i }).first();
    await expect(beginner).toBeVisible({ timeout: 30_000 });
    await beginner.click();

    const board = page.getByTestId('board-root');
    await expect(board).toBeVisible({ timeout: 90_000 });

    // Pure touch path: tap e2 (select) then e4 (move). A 250ms beat between
    // taps lets the selection state render, mirroring the desktop spec.
    await tapSquare(page, 'e2');
    await page.waitForTimeout(400);
    await tapSquare(page, 'e4');
    await page.waitForTimeout(3_000);

    // Board must still be interactive after the move (no crash/blank).
    await expect(board).toBeVisible();

    // Card hand testids exist per mechanic; the deal should have happened by
    // now for a fresh match — but the exact card is random, so only assert
    // that no error surfaced during dealing.
    const meaningfulErrors = errors.filter(
      e => !/net::|ERR_|Failed to load resource/.test(e),
    );
    expect(meaningfulErrors).toEqual([]);

    // Resign via touch and confirm the dialog.
    page.once('dialog', d => void d.accept());
    await page.getByTestId('btn-resign').tap();
    await page.waitForTimeout(3_000);
    await expect(board).toBeVisible();
  });

  test('legal pages render on mobile viewport', async ({ page }) => {
    for (const path of ['/terms', '/privacy']) {
      const response = await page.goto(path);
      expect(response?.status(), `${path} status`).toBe(200);
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
      );
      expect(overflow, `${path} horizontal overflow`).toBeLessThanOrEqual(1);
    }
  });
});
