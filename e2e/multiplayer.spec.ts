import { test, expect } from '@playwright/test';

test.describe('multiplayer casual queue', () => {
  test('two seats queued from one browser pair into a live match', async ({ page }) => {
    await page.goto('/play');

    // Embedded QueuePage renders a white and a black lane, each with its own
    // guest profile. Joining both creates two distinct guests that the
    // matchmaking service pairs with each other.
    const joinWhite = page.getByTestId('btn-join-white');
    await expect(joinWhite).toBeVisible({ timeout: 60_000 });
    await joinWhite.click();

    // White lane should flip to "queued" (cancel button appears)
    await expect(page.getByTestId('btn-cancel-white')).toBeVisible({ timeout: 30_000 });

    // Small gap so the white ticket is persisted before black joins
    await page.waitForTimeout(2_000);

    const joinBlack = page.getByTestId('btn-join-black');
    await expect(joinBlack).toBeVisible({ timeout: 30_000 });
    await joinBlack.click();

    // Matchmaking pairs the two tickets; the app opens the live board.
    // Free-tier cold paths can be slow, hence the generous ceiling.
    const board = page.getByTestId('board-root');
    await expect(board).toBeVisible({ timeout: 120_000 });

    // Refresh resilience: reload and confirm we come back to a rendered board
    // (either immediately restored or via the Return-to-Match surface).
    await page.reload();
    const restored =
      (await board.isVisible().catch(() => false)) ||
      (await page.getByText(/return to match/i).isVisible().catch(() => false));
    if (!restored) {
      // Some restore flows require one click through the hub
      const back = page.getByRole('button', { name: /return to match/i });
      if (await back.isVisible().catch(() => false)) {
        await back.click();
      }
    }
    await expect(board).toBeVisible({ timeout: 60_000 });

    // Resign to end cleanly (accept confirm dialog)
    page.once('dialog', d => void d.accept());
    await page.getByTestId('btn-resign').click();
    await page.waitForTimeout(3_000);
    await expect(page.getByTestId('board-root')).toBeVisible();
  });
});
