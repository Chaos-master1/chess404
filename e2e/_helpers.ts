import { expect, type Page } from '@playwright/test';

export const FILES = 'abcdefgh';

// Board is a single <canvas>; squares are addressed by computed coordinates.
// White-at-bottom orientation: row 0 = rank 8, col 0 = file a.
export function squarePoint(algebraic: string, viewerColor: 'white' | 'black') {
  const fileIdx = FILES.indexOf(algebraic[0]);
  const rank = Number(algebraic[1]);
  if (fileIdx < 0 || rank < 1 || rank > 8) throw new Error(`bad square ${algebraic}`);
  return { col: fileIdx, row: viewerColor === 'white' ? 8 - rank : rank - 1 };
}

export async function clickSquare(
  page: Page,
  algebraic: string,
  viewerColor: 'white' | 'black' = 'white',
) {
  const board = page.getByTestId('board-root');
  await expect(board).toBeVisible();
  const box = await board.boundingBox();
  if (!box) throw new Error('board not laid out');
  const { col, row } = squarePoint(algebraic, viewerColor);
  await page.mouse.click(
    box.x + ((col + 0.5) * box.width) / 8,
    box.y + ((row + 0.5) * box.height) / 8,
  );
}

export async function move(
  page: Page,
  from: string,
  to: string,
  viewerColor: 'white' | 'black' = 'white',
) {
  await clickSquare(page, from, viewerColor);
  await page.waitForTimeout(250);
  await clickSquare(page, to, viewerColor);
}

// First-visit onboarding modal (z-index 10000) covers the whole app and
// hydrates late in fresh contexts, so poll-dismiss rather than click once.
export async function dismissOnboarding(page: Page, settleTestId?: string) {
  const skip = page.getByRole('button', { name: /skip tutorial/i });
  let dismissed = false;
  for (let i = 0; i < 15; i++) {
    if (await skip.isVisible().catch(() => false)) {
      await skip.click().catch(() => {});
      dismissed = true;
      await page.waitForTimeout(400);
      continue;
    }
    // The modal hydrates late, so a single early miss proves nothing. Keep
    // watching until it has been dismissed, or until the page we are waiting
    // for is interactive.
    if (dismissed) return;
    if (settleTestId && i > 2 && (await page.getByTestId(settleTestId).isVisible().catch(() => false))) {
      return;
    }
    await page.waitForTimeout(1_000);
  }
}

// Registers a fresh account through the real UI and leaves the context signed
// in on /account. Registration claims the browser's current guest session, so
// matches played afterwards are attributed to the new account's linked guest --
// which is what makes account-scoped surfaces (history) show them.
//
// The guest session is minted asynchronously and its arrival re-renders the
// form, clearing anything typed before it lands, so fills are verified and
// retried rather than assumed.
export async function registerAccount(
  page: Page,
  credentials: { handle: string; email: string; password: string },
) {
  const { handle, email, password } = credentials;
  await page.goto('/account');
  await dismissOnboarding(page);

  // Falls back to position for deployments that predate the tab test ids
  // (index 0 of "Register" is the tab itself; "Sign In" also names a nav item).
  const registerTab = page.getByTestId('auth-tab-register')
    .or(page.getByRole('button', { name: /^register$/i }))
    .first();
  if (await registerTab.isVisible({ timeout: 15_000 }).catch(() => false)) {
    await registerTab.click().catch(() => {});
  }

  const handleField = page.getByPlaceholder('wizard404error');
  const emailField = page.getByPlaceholder('you@example.com');
  const passwordField = page.getByPlaceholder('Choose a strong password');
  for (let attempt = 0; attempt < 6; attempt++) {
    await handleField.fill(handle);
    await emailField.fill(email);
    await passwordField.fill(password);
    await page.waitForTimeout(1_500);
    if ((await handleField.inputValue()) === handle && (await emailField.inputValue()) === email) break;
  }
  expect(await handleField.inputValue(), 'the register form kept clearing itself').toBe(handle);

  await page.getByRole('button', { name: /create account/i }).last().click();

  // Registering navigates straight to the play hub, so come back to the
  // account surface to inspect the session it just created.
  await page.waitForTimeout(6_000);
  await page.goto('/account');
  await dismissOnboarding(page);
  await expect(
    page.getByText(new RegExp(`@${handle}`)).first(),
    'registration did not produce a signed-in session',
  ).toBeVisible({ timeout: 60_000 });
}

// Unique per run so specs can be re-run against the same production DB. The
// e2e_ prefix matters: the platform excludes these test accounts from the
// public leaderboard, so runs never leak handles into live rankings.
export function uniqueE2EHandle(): string {
  return `e2e_${Date.now().toString(36)}${Math.floor(Math.random() * 1e4).toString(36)}`.slice(0, 24);
}

export interface PageErrors {
  console: string[];
  csp: string[];
  pageErrors: string[];
  requestFailures: string[];
}

// Attaches listeners that record everything a launch-blocking console audit
// cares about. Call before the first navigation.
export function collectErrors(page: Page): PageErrors {
  const bag: PageErrors = { console: [], csp: [], pageErrors: [], requestFailures: [] };
  page.on('console', msg => {
    if (msg.type() !== 'error') return;
    const text = msg.text();
    if (/Content Security Policy|Refused to (load|connect|execute|apply)/i.test(text)) {
      bag.csp.push(text);
    } else {
      bag.console.push(text);
    }
  });
  page.on('pageerror', err => bag.pageErrors.push(String(err)));
  page.on('requestfailed', req => {
    const failure = req.failure()?.errorText ?? 'unknown';
    // Aborted navigations and cancelled prefetches are noise, not defects.
    if (/ERR_ABORTED|net::ERR_CANCELED/.test(failure)) return;
    bag.requestFailures.push(`${req.method()} ${req.url()} -> ${failure}`);
  });
  return bag;
}
