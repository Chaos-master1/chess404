# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: multiplayer.spec.ts >> multiplayer casual queue >> two browsers queueing casually get paired into a live synced match
- Location: e2e/multiplayer.spec.ts:54:7

# Error details

```
Error: expect(locator).toHaveCount(expected) failed

Locator:  getByText(/missing player credentials/i)
Expected: 0
Received: 1
Timeout:  30000ms

Call log:
  - Expect "toHaveCount" with timeout 30000ms
  - waiting for getByText(/missing player credentials/i)
    64 × locator resolved to 1 element
       - unexpected value "1"

```

# Page snapshot

```yaml
- generic [active] [ref=e1]:
  - link "Skip to content" [ref=e2] [cursor=pointer]:
    - /url: "#main-content"
  - alert [ref=e3]
  - main [ref=e4]:
    - generic [ref=e6]:
      - complementary [ref=e7]:
        - generic [ref=e17]:
          - generic [ref=e18]: Chess404
          - generic [ref=e19]: Card Chess
        - generic [ref=e20]:
          - generic [ref=e21]: Core
          - button "Play" [ref=e22] [cursor=pointer]
          - button "Watch" [ref=e28] [cursor=pointer]
          - button "Rankings" [ref=e35] [cursor=pointer]
          - button "Profiles" [ref=e43] [cursor=pointer]
        - generic [ref=e50]:
          - generic [ref=e51]: Library
          - button "History" [ref=e52] [cursor=pointer]
          - button "Cards" [ref=e60] [cursor=pointer]
          - button "Community" [ref=e67] [cursor=pointer]
        - button "Sign In" [ref=e77] [cursor=pointer]
      - main [ref=e85]:
        - generic [ref=e86]:
          - generic [ref=e87]:
            - generic [ref=e88]:
              - generic [ref=e90]:
                - generic [ref=e91]: Hollow Cipher 348
                - generic [ref=e92]: "Rating: 1200"
              - generic [ref=e93]: 10:00
            - generic [ref=e95]:
              - generic [ref=e96]: 🃏
              - generic [ref=e97]: Click a card to preview
              - generic [ref=e98]: "Cannot connect: missing player credentials. Try re-entering the match."
              - button "↻ Reconnect" [ref=e99] [cursor=pointer]
              - generic [ref=e100]:
                - generic [ref=e101]:
                  - generic [ref=e102]: ⚪
                  - generic [ref=e103]: ○ Card available
                - generic [ref=e104]:
                  - generic [ref=e105]: ⚫
                  - generic [ref=e106]: ○ Card available
                - generic [ref=e107]: Max 10 cards per player
            - generic [ref=e108]:
              - generic [ref=e110]:
                - generic [ref=e111]: Echo Cipher 344
                - generic [ref=e112]: "Rating: 1200"
              - generic [ref=e113]: 9:25
          - generic [ref=e114]:
            - generic [ref=e115]: No cards in hand
            - application "Chess board" [ref=e119] [cursor=pointer]
            - generic [ref=e121]: No cards in hand
          - generic [ref=e123]:
            - generic [ref=e124]:
              - generic [ref=e125]:
                - generic [ref=e126]: Online Match Live
                - generic [ref=e127]: match a873b2
              - generic [ref=e128]:
                - generic [ref=e129]:
                  - generic [ref=e130]: Round
                  - generic [ref=e131]: "1"
                  - generic [ref=e132]: cards dealt at r2
                - generic [ref=e133]:
                  - generic [ref=e134]: ♔
                  - generic [ref=e135]: WHITE
                - generic [ref=e136]:
                  - generic [ref=e137]: ∞ Card Pool
                  - generic [ref=e138]: Infinite drawsRarity weighted
              - generic [ref=e139]:
                - generic [ref=e140]: Drop Rates
                - generic [ref=e141]:
                  - generic [ref=e142]: TRASH
                  - generic [ref=e145]: 5%
                - generic [ref=e146]:
                  - generic [ref=e147]: COMMON
                  - generic [ref=e150]: 40%
                - generic [ref=e151]:
                  - generic [ref=e152]: RARE
                  - generic [ref=e155]: 30%
                - generic [ref=e156]:
                  - generic [ref=e157]: EPIC
                  - generic [ref=e160]: 20%
                - generic [ref=e161]:
                  - generic [ref=e162]: LEGENDARY
                  - generic [ref=e165]: 5%
            - generic [ref=e167]:
              - generic [ref=e168]:
                - button "Chat" [ref=e169] [cursor=pointer]
                - button "Moves" [ref=e170] [cursor=pointer]
                - button "Engine" [ref=e171] [cursor=pointer]
              - generic [ref=e172]: No moves played yet.
            - generic [ref=e176]:
              - generic [ref=e177]: "Turn: ⚪ White"
              - generic [ref=e180]:
                - button "✕ Abort" [ref=e181] [cursor=pointer]
                - button "🏳 Resign" [ref=e182] [cursor=pointer]
                - button "🤝 Draw" [ref=e183] [cursor=pointer]
            - generic [ref=e184]:
              - button "🔊 Sound On" [ref=e185] [cursor=pointer]
              - button "🏳 CB Off" [ref=e186] [cursor=pointer]
              - generic [ref=e187]: ← → review · Esc cancel
```

# Test source

```ts
  1   | import { test, expect, type Browser, type Page } from '@playwright/test';
  2   | 
  3   | async function enterQueueSurface(page: Page) {
  4   |   await page.setViewportSize({ width: 1440, height: 1000 });
  5   |   await page.goto('/play');
  6   |   // The onboarding modal hydrates late in fresh contexts and covers the app.
  7   |   // Poll-dismiss it rather than a single early attempt.
  8   |   const skip = page.getByRole('button', { name: /skip tutorial/i });
  9   |   for (let i = 0; i < 12; i++) {
  10  |     if ((await skip.isVisible().catch(() => false))) {
  11  |       await skip.click().catch(() => {});
  12  |       await page.waitForTimeout(400);
  13  |       continue;
  14  |     }
  15  |     if (i > 2 && (await page.getByTestId('btn-join-white').isVisible().catch(() => false))) break;
  16  |     await page.waitForTimeout(1_000);
  17  |   }
  18  | }
  19  | 
  20  | async function stableJoinButton(page: Page) {
  21  |   const join = page.getByTestId('btn-join-white');
  22  |   await expect(join).toBeVisible({ timeout: 60_000 });
  23  |   await expect(join).toBeEnabled({ timeout: 30_000 });
  24  |   return join;
  25  | }
  26  | 
  27  | // Joins the white lane and reports whether the queue stayed clean.
  28  | // An instant board means we were paired against a ghost/stale ticket.
  29  | async function joinWhite(page: Page): Promise<'queued' | 'ghost-matched'> {
  30  |   const join = await stableJoinButton(page);
  31  |   // Retry the click once if a transient overlay (late tutorial) intercepts it
  32  |   try {
  33  |     await join.click({ timeout: 10_000 });
  34  |   } catch {
  35  |     const skip = page.getByRole('button', { name: /skip tutorial/i });
  36  |     for (let i = 0; i < 8 && (await skip.isVisible().catch(() => false)); i++) {
  37  |       await skip.click().catch(() => {});
  38  |       await page.waitForTimeout(400);
  39  |     }
  40  |     const retry = await stableJoinButton(page);
  41  |     await retry.click({ timeout: 10_000 });
  42  |   }
  43  |   const cancel = page.getByTestId('btn-cancel-white');
  44  |   const board = page.getByTestId('board-root');
  45  |   for (let i = 0; i < 20; i++) {
  46  |     if (await cancel.isVisible().catch(() => false)) return 'queued';
  47  |     if (await board.isVisible().catch(() => false)) return 'ghost-matched';
  48  |     await page.waitForTimeout(500);
  49  |   }
  50  |   throw new Error('neither queued nor matched after join');
  51  | }
  52  | 
  53  | test.describe('multiplayer casual queue', () => {
  54  |   test('two browsers queueing casually get paired into a live synced match', async ({ browser }) => {
  55  |     test.setTimeout(300_000);
  56  | 
  57  |     // Drain pass: if stale tickets pair us instantly, abandon and retry
  58  |     // with fresh contexts until the white join lands in a clean queue.
  59  |     let a: Page | null = null;
  60  |     let b: Page | null = null;
  61  |     for (let attempt = 1; attempt <= 4; attempt++) {
  62  |       const ctxA = await browser.newContext();
  63  |       const ctxB = await browser.newContext();
  64  |       const pa = await ctxA.newPage();
  65  |       const pb = await ctxB.newPage();
  66  |       await enterQueueSurface(pa);
  67  |       await enterQueueSurface(pb);
  68  | 
  69  |       const state = await joinWhite(pa);
  70  |       if (state === 'ghost-matched') {
  71  |         console.log(`attempt ${attempt}: paired against a ghost ticket; retrying with fresh contexts`);
  72  |         await ctxA.close();
  73  |         await ctxB.close();
  74  |         continue;
  75  |       }
  76  | 
  77  |       // Clean queue — bring in the second player. In hosted runtime every
  78  |       // client renders a single "Your player" lane (btn-join-white); seat
  79  |       // colors are assigned server-side after pairing.
  80  |       await pb.waitForTimeout(2_000);
  81  |       const joinB = await stableJoinButton(pb);
  82  |       await joinB.click({ timeout: 15_000 });
  83  | 
  84  |       await expect(
  85  |         pb.getByTestId('board-root').or(pb.getByTestId('btn-resign')).first(),
  86  |       ).toBeVisible({ timeout: 120_000 });
  87  |       await expect(
  88  |         pa.getByTestId('board-root').or(pa.getByTestId('btn-resign')).first(),
  89  |       ).toBeVisible({ timeout: 90_000 });
  90  | 
  91  |       a = pa;
  92  |       b = pb;
  93  | 
  94  |       // Regression guard (ghost-pairing credential bug): once paired, neither
  95  |       // side may surface the missing-credentials failure. Give the WS attach
  96  |       // a few seconds, then assert it never appeared.
  97  |       await pb.waitForTimeout(6_000);
  98  |       for (const page of [pa, pb]) {
> 99  |         await expect(page.getByText(/missing player credentials/i)).toHaveCount(0);
      |                                                                     ^ Error: expect(locator).toHaveCount(expected) failed
  100 |       }
  101 | 
  102 |       // Refresh resilience on B: reload restores the live match
  103 |       await b.reload();
  104 |       await expect(
  105 |         b.getByTestId('board-root')
  106 |           .or(b.getByText(/return to match/i))
  107 |           .or(b.getByRole('button', { name: /return to match/i })),
  108 |       ).toBeVisible({ timeout: 90_000 });
  109 | 
  110 |       break;
  111 |     }
  112 | 
  113 |     test.expect(a, 'never reached a clean paired match').toBeTruthy();
  114 | 
  115 |     // Resign from whichever side exposes the control to archive the match
  116 |     for (const page of [b!, a!]) {
  117 |       const resign = page.getByTestId('btn-resign');
  118 |       if (await resign.isVisible().catch(() => false)) {
  119 |         page.once('dialog', d => void d.accept());
  120 |         await resign.click();
  121 |         break;
  122 |       }
  123 |     }
  124 |     await b!.waitForTimeout(3_000);
  125 | 
  126 |     await a!.context().close();
  127 |     await b!.context().close();
  128 |   });
  129 | });
  130 | 
```