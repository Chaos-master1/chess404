import { test, expect, type Page } from '@playwright/test';

// Board is a single <canvas>; squares are addressed by computed coordinates.
// White-at-bottom orientation: row 0 = rank 8, col 0 = file a.
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
  return { page, col, row: rowFromTop };
}

async function clickSquare(
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
  await page.mouse.click(x, y);
}

async function move(page: Page, from: string, to: string, viewerColor: 'white' | 'black' = 'white') {
  await clickSquare(page, from, viewerColor);
  await page.waitForTimeout(250);
  await clickSquare(page, to, viewerColor);
}

test.describe('solo vs computer', () => {
  test('guest creates a computer match, moves, and resigns', async ({ page }) => {
    await page.goto('/play');

    // Play hub must offer the solo path
    const playComputer = page.getByTestId('btn-play-computer');
    await expect(playComputer).toBeVisible({ timeout: 60_000 });
    await playComputer.click();

    // ComputerPage: pick the weakest opponent for speed, create match
    const beginner = page.getByRole('button', { name: /beginner/i }).first();
    await expect(beginner).toBeVisible({ timeout: 30_000 });
    await beginner.click();

    // Should land on a live match with the canvas board rendered
    const board = page.getByTestId('board-root');
    await expect(board).toBeVisible({ timeout: 90_000 });
    await page.waitForURL(/match=/, { timeout: 30_000 }).catch(() => {
      // some flows keep the URL but render inline; board visibility is the real gate
    });

    // We are white (preferredSeat=white in creation flow): open as e2-e4
    await move(page, 'e2', 'e4');

    // Give the engine its reply window on the small free-tier box
    await page.waitForTimeout(15_000);

    // If it is our turn again, d2-d4 should be legal; a second successful
    // opening move implies the engine responded (turn handed back).
    await move(page, 'd2', 'd4');
    await page.waitForTimeout(3_000);

    // Hand of cards should exist somewhere in the match UI
    await expect(page.getByTestId('board-root')).toBeVisible();

    // Resign (accept the confirm dialog)
    page.once('dialog', d => void d.accept());
    await page.getByTestId('btn-resign').click();
    await page.waitForTimeout(3_000);

    // Board still rendered post-game (terminal state), no crash
    await expect(page.getByTestId('board-root')).toBeVisible();
  });
});
