import { test, expect } from '@playwright/test';
import { clickSquare, collectErrors, dismissOnboarding, move } from './_helpers';

// The card system is the product's differentiator, and a card is only removed
// from a hand when the SERVER resolves it. So the assertion that matters is the
// hand count after a reload -- reload re-reads the authoritative snapshot, which
// a purely local UI change cannot fake.
test.describe('card play', () => {
  test('playing a card is applied server-side, not just in the UI', async ({ page }) => {
    test.setTimeout(300_000);
    const errors = collectErrors(page);

    await page.goto('/play');
    await dismissOnboarding(page, 'btn-play-computer');
    await page.getByTestId('btn-play-computer').click();

    const beginner = page.getByRole('button', { name: /beginner/i }).first();
    if (await beginner.isVisible({ timeout: 20_000 }).catch(() => false)) {
      await beginner.click();
    }
    await expect(page.getByTestId('board-root')).toBeVisible({ timeout: 90_000 });

    const cards = page.locator('[data-testid^="hand-card-"]');
    await expect(cards.first()).toBeVisible({ timeout: 60_000 });
    const before = await cards.count();
    expect(before, 'no cards dealt').toBeGreaterThan(0);

    // Mana accrues one per turn and the cheapest spells cost 2, so nothing is
    // playable on move one. Take quiet flank pawn moves -- legal whatever the
    // engine replies -- and retry the hand after each one.
    const quietMoves: Array<[string, string]> = [
      ['a2', 'a3'], ['h2', 'h3'], ['b2', 'b3'], ['a3', 'a4'],
      ['h3', 'h4'], ['b3', 'b4'], ['c2', 'c3'], ['g2', 'g3'],
    ];
    // Most mechanics target an enemy piece; the black back rank and pawn line
    // are the targets that stay occupied through the opening.
    const enemyTargets = ['b8', 'g8', 'e7', 'd7', 'a8', 'h8'];

    let played = false;
    for (const [from, to] of quietMoves) {
      const count = await cards.count();
      for (let i = 0; i < count && !played; i++) {
        await cards.nth(i).click();
        const use = page.getByRole('button', { name: /^use card$/i });
        if (!(await use.isVisible({ timeout: 3_000 }).catch(() => false))) continue;
        if (!(await use.isEnabled().catch(() => false))) continue;
        await use.click();
        await page.waitForTimeout(4_000);
        // A card that needs a target sits in a pending state until it gets one.
        for (const target of enemyTargets) {
          if ((await cards.count()) < count) break;
          await clickSquare(page, target);
          await page.waitForTimeout(3_000);
        }
        if ((await cards.count()) < count) played = true;
      }
      if (played) break;
      await move(page, from, to);
      await page.waitForTimeout(9_000);
    }

    expect(played, 'no card could be played in eight turns of accumulated mana').toBe(true);

    // Server truth: the reduced hand must survive a reload.
    await page.reload();
    await dismissOnboarding(page);
    await expect(page.getByTestId('board-root')).toBeVisible({ timeout: 90_000 });
    await page.waitForTimeout(6_000);
    const after = await page.locator('[data-testid^="hand-card-"]').count();
    expect(after, 'card came back after reload -- it was never resolved server-side')
      .toBeLessThan(before);

    expect(errors.pageErrors, 'uncaught exceptions during card play').toEqual([]);
  });
});
