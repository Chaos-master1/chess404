import { test, expect, type Page } from '@playwright/test';
import { collectErrors, dismissOnboarding, move } from './_helpers';

// Selectors fall back to accessible text so the spec runs against a deployment
// that predates the data-testid additions.
function createInviteButton(page: Page) {
  return page
    .getByTestId('btn-create-invite')
    .or(page.getByRole('button', { name: /create private invite match/i }))
    .first();
}

function inviteUrlNode(page: Page) {
  return page.getByTestId('invite-url').or(page.getByText(/\/match\//).first()).first();
}

test.describe('private invite', () => {
  test('host creates an invite and a second browser claims the open seat', async ({ browser }) => {
    test.setTimeout(300_000);

    const hostCtx = await browser.newContext();
    const guestCtx = await browser.newContext();
    const host = await hostCtx.newPage();
    const guest = await guestCtx.newPage();
    const guestErrors = collectErrors(guest);

    await host.setViewportSize({ width: 1440, height: 1000 });
    await guest.setViewportSize({ width: 1440, height: 1000 });

    await host.goto('/play');
    await dismissOnboarding(host, 'btn-play-computer');

    const create = createInviteButton(host);
    await expect(create).toBeVisible({ timeout: 60_000 });
    await expect(create).toBeEnabled({ timeout: 30_000 });
    await create.click();

    const inviteNode = inviteUrlNode(host);
    await expect(inviteNode).toBeVisible({ timeout: 60_000 });
    const inviteUrl = ((await inviteNode.innerText()) ?? '').trim();
    const matchId = inviteUrl.match(/\/match\/([^/?#\s]+)/)?.[1];
    expect(matchId, `could not parse a match id out of "${inviteUrl}"`).toBeTruthy();

    // Host opens its own room.
    await host.goto(`/match/${matchId}`);
    await expect(host.getByTestId('board-root')).toBeVisible({ timeout: 90_000 });

    // Second browser opens the shared link cold: no local storage, no claim.
    await guest.goto(`/match/${matchId}`);
    await dismissOnboarding(guest);
    await expect(guest.getByTestId('board-root')).toBeVisible({ timeout: 90_000 });

    // The invitee must actually own the open seat, not spectate it. The resign
    // control only renders for a seated player, so it is the seat proof.
    await expect(
      guest.getByTestId('btn-resign'),
      'invitee never received a seat in the private match',
    ).toBeVisible({ timeout: 60_000 });

    await expect(guest.getByText(/missing player credentials/i)).toHaveCount(0);
    await expect(guest.getByText(/spectat/i)).toHaveCount(0);

    // Host is white by default in this flow: a legal opening move must stick.
    await move(host, 'e2', 'e4');
    await host.waitForTimeout(4_000);

    // And the invitee must be able to answer it.
    await move(guest, 'e7', 'e5', 'black');
    await guest.waitForTimeout(4_000);

    expect(guestErrors.pageErrors, 'invitee uncaught exceptions').toEqual([]);

    await hostCtx.close();
    await guestCtx.close();
  });
});
