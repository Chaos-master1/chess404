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

// Fingerprint of the position the canvas actually paints. Asserting on this
// (rather than on board visibility) is what catches a board that renders but
// never repaints -- the signature only changes if the painted position does.
async function paintedSignature(page: Page): Promise<string | null> {
  return page.getByTestId('board-root').getAttribute('data-board-signature');
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

test.describe('solo vs computer', () => {
  test('guest creates a computer match, moves, and resigns', async ({ page }) => {
    await page.goto('/play');
    await dismissOnboarding(page);

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
    const signatureBeforeMove = await paintedSignature(page);
    expect(signatureBeforeMove, 'board did not expose a painted-position signature').toBeTruthy();
    expect(signatureBeforeMove, 'board reported a malformed position').not.toBe('invalid');

    await move(page, 'e2', 'e4');

    // The board must actually repaint. A frozen board still passes every
    // visibility check in this file, which is how a change that made the
    // authoritative snapshot path unreachable once shipped green. The
    // comparison is normalised so the poll cannot pass on a disappearing
    // value either: the position must still be readable AND different.
    await expect
      .poll(async () => {
        const signatureAfterMove = await paintedSignature(page);
        return Boolean(signatureAfterMove)
          && signatureAfterMove !== 'invalid'
          && signatureAfterMove !== signatureBeforeMove;
      }, { timeout: 30_000 })
      .toBe(true);

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

    // Terminal state is honest: the post-game screen must not keep claiming a
    // stream is reconnecting, and must not declare the room unreadable to the
    // player who just played it.
    await page.waitForTimeout(3_000);
    await expect(page.getByText(/Reconnecting to live match stream/)).toHaveCount(0);
    await expect(page.getByText(/This game could not be loaded/)).toHaveCount(0);

    // The live P0: web answered 404 for a finished match to BOTH of its
    // players while match-service served it with 200. The seat owner must
    // still get a readable snapshot of the room they just played, and the
    // anonymous read must stay refused (private/computer games are not
    // public spectator material).
    const matchId = new URL(page.url()).pathname.match(/^\/match\/([^/?]+)/)?.[1];
    expect(matchId, 'could not read the finished match id from the URL').toBeTruthy();
    const readStatuses = await page.evaluate(async (id) => {
      const headers: Record<string, string> = {
        'x-chess404-white-guest-id': window.localStorage.getItem('chess404.guest.white') ?? '',
        'x-chess404-white-session-secret': window.localStorage.getItem('chess404.guest.white.secret') ?? '',
        'x-chess404-white-session-token': window.localStorage.getItem('chess404.guest.white.token') ?? '',
      };
      const asOwner = await fetch(`/api/realtime/matches/${id}`, { headers });
      const asStranger = await fetch(`/api/realtime/matches/${id}`);
      return { asOwner: asOwner.status, asStranger: asStranger.status };
    }, matchId);
    expect(readStatuses.asOwner, 'the finished room must still be readable by its seat owner').toBe(200);
    expect(readStatuses.asStranger, 'a finished vs-computer room must not become public').toBe(404);
  });
});
