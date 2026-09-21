import { test, expect } from '@playwright/test';
import { clickSquare, collectErrors, dismissOnboarding, move } from './_helpers';

// The card system is the product's differentiator, and a card is only removed
// from a hand when the SERVER resolves it. So the assertion that matters is the
// hand count after a reload -- reload re-reads the authoritative snapshot, which
// a purely local UI change cannot fake.
//
// Locator note: both hands render `data-testid="hand-card-<mechanic>"` and the
// opponent's (top) hand comes FIRST in the DOM. A naive `.nth(0)` therefore
// clicks the opponent's cards, where "Use card" is correctly disabled ("Not
// your card to use") -- which silently starved earlier versions of this test.
// The viewer's hand is the bottom hand: anchor on the PARENT of the last
// hand-card element to scope every probe to the viewer's own cards.
test.describe('card play', () => {
  test('playing a card is applied server-side, not just in the UI', async ({ page }) => {
    test.setTimeout(600_000);
    const errors = collectErrors(page);

    await page.goto('/play');
    await dismissOnboarding(page, 'btn-play-computer');
    await page.getByTestId('btn-play-computer').click();

    const beginner = page.getByRole('button', { name: /beginner/i }).first();
    if (await beginner.isVisible({ timeout: 20_000 }).catch(() => false)) {
      await beginner.click();
    }
    await expect(page.getByTestId('board-root')).toBeVisible({ timeout: 90_000 });

    const anyHandCard = page.locator('[data-testid^="hand-card-"]');
    await expect(anyHandCard.first()).toBeVisible({ timeout: 60_000 });
    // The bottom (viewer) hand root is the parent of the last hand-card in the DOM.
    const viewerHand = anyHandCard
      .last()
      .locator('xpath=ancestor::div[1]')
      .locator('[data-testid^="hand-card-"]');
    const before = await viewerHand.count();
    expect(before, 'no cards dealt').toBeGreaterThan(0);

    // No mana system exists: the server gates cards at one-per-turn (canUseCard),
    // and the dealt hand is random. Take quiet flank pawn moves -- legal whatever
    // the engine replies -- and probe the hand once it is white's turn again.
    const quietMoves: Array<[string, string]> = [
      ['a2', 'a3'], ['h2', 'h3'], ['b2', 'b3'], ['a3', 'a4'],
      ['h3', 'h4'], ['b3', 'b4'], ['c2', 'c3'], ['g2', 'g3'],
      ['c3', 'c4'], ['g3', 'g4'], ['b4', 'b5'], ['h4', 'h5'],
    ];
    // Cards need wildly different targets (enemy pieces, own pieces, empty
    // squares, multi-step sequences), and wrong targets are rejected with a
    // visible message -- so probe a mixed candidate list and stop at the first
    // hand reduction. The joker opens a transformation picker the driver does
    // not script, so skip it.
    const targets = ['b8', 'g8', 'a8', 'h8', 'e7', 'd7', 'd1', 'c1', 'd5', 'e5'];

    // A failed probe can leave a pending card open, which poisons later board
    // clicks, so cancel it before moving on.
    async function cancelPendingCard() {
      await page.keyboard.press('Escape').catch(() => {});
      await page.getByText('✕ cancel', { exact: false }).click({ timeout: 600 }).catch(() => {});
      await page.getByRole('button', { name: '✕ Cancel' }).click({ timeout: 400 }).catch(() => {});
    }

    // A stuck stream used to leave this test clicking a zombie page for its
    // whole 10-minute budget. Fail loudly instead of spinning.
    async function failIfStreamZombie() {
      const banner = page.getByText('Reconnecting to live match stream');
      if (!(await banner.isVisible().catch(() => false))) return;
      for (let i = 0; i < 20; i++) {
        await page.waitForTimeout(3_000);
        if (!(await banner.isVisible().catch(() => false))) return;
      }
      throw new Error('live match stream stuck on "Reconnecting..." for 60s (zombie-stream regression)');
    }

    // Try to use ONE viewer card: returns true if the hand shrank (the server
    // resolved a card). Iterates the viewer's cards first; when "Use" is
    // disabled the turn belongs to the engine, so bail out cheaply instead of
    // sweeping targets against a locked card.
    async function tryUseOneCard(): Promise<boolean> {
      const count = await viewerHand.count();
      for (let i = count - 1; i >= 0; i--) {
        const card = viewerHand.nth(i);
        if ((await card.getAttribute('data-testid'))?.includes('joker')) continue;
        await card.click();
        const use = page.getByRole('button', { name: /^use card$/i });
        if (!(await use.isVisible({ timeout: 1_200 }).catch(() => false))) {
          await cancelPendingCard();
          continue;
        }
        if (!(await use.isEnabled().catch(() => false))) {
          await cancelPendingCard();
          return false; // engine's turn -- re-probe after it replies
        }
        await use.click();
        for (const target of targets) {
          if ((await viewerHand.count()) < count) return true;
          await clickSquare(page, target);
          await page.waitForTimeout(600);
        }
        if ((await viewerHand.count()) < count) return true;
        await cancelPendingCard();
      }
      return false;
    }

    let played = false;
    outer: for (const [from, to] of quietMoves) {
      await failIfStreamZombie();
      await move(page, from, to);
      // Poll up to ~40s for the engine reply (turn flips back to white).
      for (let attempt = 0; attempt < 26 && !played; attempt++) {
        await page.waitForTimeout(1_200);
        await failIfStreamZombie();
        played = await tryUseOneCard();
      }
      if (played) break outer;
    }

    expect(played, 'no card could be played in twelve turns').toBe(true);

    // Server truth: the reduced hand must survive a reload.
    await page.reload();
    await dismissOnboarding(page);
    await expect(page.getByTestId('board-root')).toBeVisible({ timeout: 90_000 });
    await page.waitForTimeout(6_000);
    const after = await viewerHand.count();
    expect(after, 'card came back after reload -- it was never resolved server-side')
      .toBeLessThan(before);

    expect(errors.pageErrors, 'uncaught exceptions during card play').toEqual([]);
  });
});
