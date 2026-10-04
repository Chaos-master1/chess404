import { test, expect, type Page, type Route } from '@playwright/test';
import { dismissOnboarding } from './_helpers';

// Regression (2026-10-03): the matchmaking service once serialized its
// INTERNAL two-phase pairing reservation through the ticket-read responses
// (status "pairing" plus the half-created room). The web queue stopped
// polling (it only fetched 'queued' tickets) and its auto-open effect never
// fired (it required 'matched'), parking the browser on "Matched - opening
// game..." forever while the opponent played on.
//
// This test replays that exact leak through a real browser: every ticket GET
// for this page is intercepted and rewritten to the historical raw shape for
// a bounded number of polls. The client must (1) keep presenting an active
// seek while the window is injected instead of rendering a parked/matched
// card, (2) keep polling until the window clears, and (3) never navigate
// anywhere from the injected data alone.
//
// Shared-lobby hazard: a leftover queued ticket pairs a future run against a
// dead opponent ("ghost"). Two defenses: the test joins in the 5+0 lane
// (production's known ghosts are 10+0 seeks and pairing needs an exact clock
// match), and if a ghost in THIS lane still pairs the join instantly, the
// pairing consumes it -- the test re-enters and joins again on the now-clean
// lane. Queued tickets also expire after 10 minutes server-side. The test
// cancels its own ticket on every exit path so a failed assertion cannot
// poison the next run.

async function enterQueueSurface(page: Page) {
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto('/play', { timeout: 180_000 });
  await dismissOnboarding(page, 'btn-join-white');
}

const MAX_INJECTED_POLLS = 4;
const LEAKED_ROOM = 'room_e2epairingwindow';

// Joins the white lane and only returns once the queue card (the cancel
// button) is up. An instant board means we paired against a leftover ticket;
// re-entering and joining again lands on the cleaned-up lane.
async function joinUntilQueued(page: Page, maxAttempts: number): Promise<void> {
  const cancel = page.getByTestId('btn-cancel-white');
  const board = page.getByTestId('board-root');
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    const join = page.getByTestId('btn-join-white');
    await expect(join).toBeVisible({ timeout: 60_000 });
    await expect(join).toBeEnabled({ timeout: 30_000 });
    await join.click({ timeout: 10_000 });

    for (let i = 0; i < 40; i++) {
      if (await cancel.isVisible().catch(() => false)) return;
      if (await board.isVisible().catch(() => false)) break;
      await page.waitForTimeout(500);
    }
    if (await cancel.isVisible().catch(() => false)) return;
    const ghostPaired = await board.isVisible().catch(() => false);
    if (!ghostPaired) {
      throw new Error('neither the queue card nor a board appeared after join');
    }
    if (attempt === maxAttempts) {
      throw new Error(`lane still poisoned after ${maxAttempts} joins: kept pairing against leftover tickets`);
    }
    // The pairing above consumed the leftover ticket. Start over on /play;
    // the claimed ticket ref was already cleared at navigation time, so the
    // surface comes back clean.
    await enterQueueSurface(page);
  }
}

test.describe('multiplayer casual queue', () => {
  test('a leaked pairing reservation cannot park the queue handoff', async ({ page }) => {
    test.setTimeout(180_000);

    let injectedPolls = 0;
    let pollsObserved = 0;
    let realStatus: string | null = null;

    const injectHistoricalLeak = async (route: Route) => {
      const response = await route.fetch();
      if (!response.ok()) {
        await route.fulfill({ response });
        return;
      }
      const payload = await response.json();
      const real = (payload.ticket ?? {}) as Record<string, unknown>;
      realStatus = typeof real.status === 'string' ? real.status : null;
      pollsObserved += 1;
      if (injectedPolls < MAX_INJECTED_POLLS && real.status === 'queued') {
        injectedPolls += 1;
        const leaked = {
          ...real,
          // The historical raw wire shape: internal reservation status plus
          // the half-created room, no seat fields.
          status: 'pairing',
          assignedRoom: LEAKED_ROOM,
          seatColor: undefined,
          matchedWith: undefined,
          opponentName: undefined,
        };
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...payload, ticket: leaked }) });
        return;
      }
      await route.fulfill({ response });
    };

    await page.route('**/api/matchmaking/queues/tickets/ticket_*', injectHistoricalLeak);

    try {
      await enterQueueSurface(page);

      // 5+0 lane: production's abandoned 10+0 tickets cannot pair with this
      // run's ticket (pairing requires an exact clock match).
      await page.getByRole('button', { name: /^5\+0 Blitz$/ }).click();

      await joinUntilQueued(page, 3);

      // 1) While the window is injected, the queue card must still present an
      // active seek. With the old client this was the parked state: polling
      // had stopped and the card read "Matched - opening game...".
      await expect(page.getByTestId('btn-cancel-white')).toBeVisible({ timeout: 30_000 });
      await expect(page.getByText(/matched - opening game/i)).toHaveCount(0);

      // 2) The bounded leak window must actually be exercised: the client
      // keeps polling through it (the poll loop runs on a 2.5s cadence, so
      // wait for all four rewrites instead of racing the first one).
      await expect
        .poll(() => injectedPolls, { timeout: 60_000, intervals: [500] })
        .toBe(MAX_INJECTED_POLLS);
      await expect(page.getByTestId('btn-cancel-white')).toBeVisible();
      await expect(page.getByText(/matched - opening game/i)).toHaveCount(0);

      // 3) The client must still be polling after the window ends (fresh
      // real responses must arrive once injection stops).
      await expect
        .poll(() => pollsObserved, { timeout: 45_000, intervals: [500] })
        .toBeGreaterThan(MAX_INJECTED_POLLS + 1);

      // 4) The injected room must never leak into the session: no navigation
      // to it, no stored room meta carrying it, and no board from the fake
      // "matched" state.
      await expect(page.getByTestId('board-root').or(page.getByTestId('btn-resign')).first()).not.toBeVisible();
      const leakedInSession = await page.evaluate(room => {
        const hits: string[] = [];
        for (let i = 0; i < localStorage.length; i++) {
          const key = localStorage.key(i);
          const value = localStorage.getItem(key) ?? '';
          if (value.includes(room)) hits.push(key);
        }
        return hits;
      }, LEAKED_ROOM);
      expect(leakedInSession, 'injected room must not leak into session storage').toEqual([]);
      expect(page.url()).not.toContain(`/match/${LEAKED_ROOM}`);
      expect(realStatus === 'matched' ? page.url() : '').not.toContain(`/match/${LEAKED_ROOM}`);
    } finally {
      // Never leave a live ticket behind: it would ghost-pair a later run.
      const cancel = page.getByTestId('btn-cancel-white');
      if (await cancel.isVisible().catch(() => false)) {
        await cancel.click({ timeout: 10_000 }).catch(() => {});
        await page
          .getByTestId('btn-join-white')
          .waitFor({ state: 'visible', timeout: 20_000 })
          .catch(() => {});
      }
    }
  });
});
