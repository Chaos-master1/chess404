// Live probe of the fixed claim-token path on production (commit 3c9bf77),
// executed inside a real browser page on the web origin (Origin header and
// fetch/WebSocket behavior identical to the production client).
// 1) Create a computer match through the web gateway proxy (real user path).
// 2) WS auth with the claim token  -> expect auth.success.
// 3) WS auth AGAIN with the same token (renewal) -> expect auth.success.
// 4) WS auth with a forged token -> expect auth.error.
// 5) WS auth with the seat secret (legacy path) -> expect auth.success.
import { chromium } from 'playwright';

const BASE = 'https://web-production-1caefb.up.railway.app';
const MATCH_WS = 'wss://match-service-production.up.railway.app';

async function main() {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' });

  const results = await page.evaluate(async (matchWs) => {
    const out = [];
    const pass = (n, d) => out.push(`PASS ${n}${d ? ' :: ' + d : ''}`);
    const fail = (n, d) => out.push(`FAIL ${n}: ${d}`);

    // web serving check
    out.push(`INFO page-url ${location.origin} title=${document.title.slice(0, 40)}`);

    const guestId = 'probe-' + Math.random().toString(36).slice(2, 10);
    const sessionSecret = 'probesecret-' + Math.random().toString(36).slice(2, 14);

    let created;
    try {
      const res = await fetch('/api/gateway/private-matches', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          guest: { guestId, sessionSecret },
          queue: 'direct',
          modeId: 'computer',
          difficulty: 'beginner',
          clockSeconds: 300,
          preferredSeat: 'white',
        }),
      });
      const body = await res.text();
      if (res.status !== 200 && res.status !== 201) {
        throw new Error(`status ${res.status} ${body.slice(0, 200)}`);
      }
      created = JSON.parse(body);
    } catch (e) {
      fail('create-match', String(e.message || e));
      return out;
    }
    const matchId = created.matchId;
    const claim = created.claim || {};
    // The gateway may mint a replacement guest session; the seat belongs to
    // the minted identity, so later credential checks must use it (this is
    // exactly what the production client does with the response).
    const effectiveSecret = (created.guestSession && created.guestSession.sessionSecret) || sessionSecret;
    pass('create-match', `matchId=${matchId} claimToken=${claim.claimToken ? 'yes' : 'NO'} secret=${claim.playerSecret ? 'yes' : 'NO'}`);

    const wsUrl = `${matchWs}/api/matches/${matchId}/ws`;
    const authOnce = (payload) => new Promise((resolve, reject) => {
      const ws = new WebSocket(wsUrl);
      const timer = setTimeout(() => { try { ws.close(); } catch {} reject(new Error('timeout')); }, 15000);
      ws.onmessage = (ev) => {
        let msg; try { msg = JSON.parse(ev.data); } catch { msg = { type: ev.data }; }
        clearTimeout(timer);
        try { ws.close(); } catch {}
        resolve(msg.type);
      };
      ws.onerror = () => { clearTimeout(timer); reject(new Error('ws error')); };
      ws.onopen = () => ws.send(JSON.stringify(payload));
    });

    try {
      const t = await authOnce({ claimToken: claim.claimToken });
      t === 'auth.success' ? pass('ws-claim-token-auth') : fail('ws-claim-token-auth', `got ${t}`);
    } catch (e) { fail('ws-claim-token-auth', String(e.message || e)); }

    try {
      const t = await authOnce({ claimToken: claim.claimToken });
      t === 'auth.success' ? pass('ws-claim-token-renewal') : fail('ws-claim-token-renewal', `got ${t}`);
    } catch (e) { fail('ws-claim-token-renewal', String(e.message || e)); }

    try {
      const t = await authOnce({ claimToken: 'forged-' + 'x'.repeat(24) });
      t === 'auth.error' ? pass('ws-forged-token-rejected') : fail('ws-forged-token-rejected', `got ${t}`);
    } catch (e) { fail('ws-forged-token-rejected', String(e.message || e)); }

    try {
      const t = await authOnce({ playerId: claim.playerId, playerSecret: effectiveSecret });
      t === 'auth.success' ? pass('ws-seat-secret-auth') : fail('ws-seat-secret-auth', `got ${t}`);
    } catch (e) { fail('ws-seat-secret-auth', String(e.message || e)); }

    return out;
  }, MATCH_WS);

  console.log(results.join('\n'));
  const failures = results.filter(r => r.startsWith('FAIL')).length;
  console.log(failures === 0 ? 'ALL PROBES PASSED' : `${failures} PROBE FAILURES`);
  await browser.close();
  process.exit(failures === 0 ? 0 : 1);
}

main().catch((e) => { console.error('probe crashed:', e); process.exit(2); });
