import { test, expect, type Browser, type Page } from '@playwright/test';

async function enterQueueSurface(page: Page) {
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto('/play');
  // The onboarding modal hydrates late in fresh contexts and covers the app.
  // Poll-dismiss it rather than a single early attempt.
  const skip = page.getByRole('button', { name: /skip tutorial/i });
  for (let i = 0; i < 12; i++) {
    if ((await skip.isVisible().catch(() => false))) {
      await skip.click().catch(() => {});
      await page.waitForTimeout(400);
      continue;
    }
    if (i > 2 && (await page.getByTestId('btn-join-white').isVisible().catch(() => false))) break;
    await page.waitForTimeout(1_000);
  }
}

async function stableJoinButton(page: Page) {
  const join = page.getByTestId('btn-join-white');
  await expect(join).toBeVisible({ timeout: 60_000 });
  await expect(join).toBeEnabled({ timeout: 30_000 });
  return join;
}

// Joins the white lane and reports whether the queue stayed clean.
// An instant board means we were paired against a ghost/stale ticket.
async function joinWhite(page: Page): Promise<'queued' | 'ghost-matched'> {
  const join = await stableJoinButton(page);
  // Retry the click once if a transient overlay (late tutorial) intercepts it
  try {
    await join.click({ timeout: 10_000 });
  } catch {
    const skip = page.getByRole('button', { name: /skip tutorial/i });
    for (let i = 0; i < 8 && (await skip.isVisible().catch(() => false)); i++) {
      await skip.click().catch(() => {});
      await page.waitForTimeout(400);
    }
    const retry = await stableJoinButton(page);
    await retry.click({ timeout: 10_000 });
  }
  const cancel = page.getByTestId('btn-cancel-white');
  const board = page.getByTestId('board-root');
  for (let i = 0; i < 20; i++) {
    if (await cancel.isVisible().catch(() => false)) return 'queued';
    if (await board.isVisible().catch(() => false)) return 'ghost-matched';
    await page.waitForTimeout(500);
  }
  throw new Error('neither queued nor matched after join');
}

test.describe('multiplayer casual queue', () => {
  test('two browsers queueing casually get paired into a live synced match', async ({ browser }) => {
    test.setTimeout(300_000);

    // Drain pass: if stale tickets pair us instantly, abandon and retry
    // with fresh contexts until the white join lands in a clean queue.
    let a: Page | null = null;
    let b: Page | null = null;
    for (let attempt = 1; attempt <= 4; attempt++) {
      const ctxA = await browser.newContext();
      const ctxB = await browser.newContext();
      const pa = await ctxA.newPage();
      const pb = await ctxB.newPage();
      await enterQueueSurface(pa);
      await enterQueueSurface(pb);

      const state = await joinWhite(pa);
      if (state === 'ghost-matched') {
        console.log(`attempt ${attempt}: paired against a ghost ticket; retrying with fresh contexts`);
        await ctxA.close();
        await ctxB.close();
        continue;
      }

      // Clean queue — bring in the second player. In hosted runtime every
      // client renders a single "Your player" lane (btn-join-white); seat
      // colors are assigned server-side after pairing.
      await pb.waitForTimeout(2_000);
      const joinB = await stableJoinButton(pb);
      await joinB.click({ timeout: 15_000 });

      await expect(
        pb.getByTestId('board-root').or(pb.getByTestId('btn-resign')).first(),
      ).toBeVisible({ timeout: 120_000 });
      await expect(
        pa.getByTestId('board-root').or(pa.getByTestId('btn-resign')).first(),
      ).toBeVisible({ timeout: 90_000 });

      a = pa;
      b = pb;

      // Regression guard (ghost-pairing credential bug): once paired, neither
      // side may surface the missing-credentials failure. Give the WS attach
      // a few seconds, then assert it never appeared.
      await pb.waitForTimeout(6_000);
      for (const page of [pa, pb]) {
        await expect(page.getByText(/missing player credentials/i)).toHaveCount(0);
      }

      // Refresh resilience on B: reload restores the live match
      await b.reload();
      await expect(
        b.getByTestId('board-root')
          .or(b.getByText(/return to match/i))
          .or(b.getByRole('button', { name: /return to match/i })),
      ).toBeVisible({ timeout: 90_000 });

      break;
    }

    test.expect(a, 'never reached a clean paired match').toBeTruthy();

    // Resign from whichever side exposes the control to archive the match
    for (const page of [b!, a!]) {
      const resign = page.getByTestId('btn-resign');
      if (await resign.isVisible().catch(() => false)) {
        page.once('dialog', d => void d.accept());
        await resign.click();
        break;
      }
    }
    await b!.waitForTimeout(3_000);

    await a!.context().close();
    await b!.context().close();
  });
});
