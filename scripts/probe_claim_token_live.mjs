// Live probe of the fixed claim-token path on production (commit 3c9bf77).
// 1) Create a computer match through the web gateway proxy (real user path).
// 2) WS auth with the claim token  -> expect auth.success.
// 3) WS auth AGAIN with the same token (renewal) -> expect auth.success.
// 4) WS auth with a forged token -> expect auth.error.
// 5) WS auth with the seat secret (legacy path) -> expect auth.success.
const BASE = 'https://web-production-1caefb.up.railway.app';
const MATCH_WS = 'wss://match-service-production.up.railway.app';

function guestIdentity() {
  const guestId = 'probe-' + Math.random().toString(36).slice(2, 10);
  const sessionSecret = 'probesecret-' + Math.random().toString(36).slice(2, 14);
  return { guestId, sessionSecret };
}

async function createComputerMatch(identity) {
  const res = await fetch(`${BASE}/api/gateway/private-matches`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      guest: { guestId: identity.guestId, sessionSecret: identity.sessionSecret },
      queue: 'direct',
      modeId: 'computer',
      difficulty: 'beginner',
      clockSeconds: 300,
      preferredSeat: 'white',
    }),
  });
  const body = await res.text();
  if (res.status !== 200 && res.status !== 201) {
    throw new Error(`create failed: ${res.status} ${body.slice(0, 300)}`);
  }
  return JSON.parse(body);
}

function wsAuthOnce(wsUrl, authPayload, timeoutMs = 15000) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(wsUrl);
    const timer = setTimeout(() => {
      try { ws.close(); } catch {}
      reject(new Error('ws timeout'));
    }, timeoutMs);
    ws.onmessage = (ev) => {
      let msg;
      try { msg = JSON.parse(ev.data); } catch { msg = { type: ev.data }; }
      clearTimeout(timer);
      try { ws.close(); } catch {}
      resolve(msg.type);
    };
    ws.onerror = () => {
      clearTimeout(timer);
      reject(new Error('ws error'));
    };
    ws.onopen = () => ws.send(JSON.stringify(authPayload));
  });
}

(async () => {
  const results = [];
  const fail = (name, detail) => { results.push(`FAIL ${name}: ${detail}`); };
  const pass = (name, detail) => { results.push(`PASS ${name}${detail ? ' :: ' + detail : ''}`); };

  // --- web serving check ---
  const page = await fetch(BASE + '/');
  const html = await page.text();
  if (page.status === 200 && html.includes('<')) pass('web-serves', `status ${page.status}`);
  else fail('web-serves', `status ${page.status}`);

  // --- create match (real user path) ---
  const identity = guestIdentity();
  let created;
  try {
    created = await createComputerMatch(identity);
  } catch (e) {
    fail('create-match', e.message);
    console.log(results.join('\n'));
    process.exit(1);
  }
  const matchId = created.matchId;
  const claim = created.claim || {};
  pass('create-match', `matchId=${matchId} claimToken=${claim.claimToken ? 'yes' : 'NO'} secret=${claim.playerSecret ? 'yes' : 'NO'}`);

  const wsUrl = `${MATCH_WS}/api/matches/${matchId}/ws`;

  // --- 2) WS auth with claim token ---
  try {
    const t = await wsAuthOnce(wsUrl, { claimToken: claim.claimToken });
    if (t === 'auth.success') pass('ws-claim-token-auth');
    else fail('ws-claim-token-auth', `got ${t}`);
  } catch (e) { fail('ws-claim-token-auth', e.message); }

  // --- 3) WS auth AGAIN with the same token (renewal semantics) ---
  try {
    const t = await wsAuthOnce(wsUrl, { claimToken: claim.claimToken });
    if (t === 'auth.success') pass('ws-claim-token-renewal');
    else fail('ws-claim-token-renewal', `got ${t}`);
  } catch (e) { fail('ws-claim-token-renewal', e.message); }

  // --- 4) Forged token must be rejected ---
  try {
    const t = await wsAuthOnce(wsUrl, { claimToken: 'forged-' + 'x'.repeat(24) });
    if (t === 'auth.error') pass('ws-forged-token-rejected');
    else fail('ws-forged-token-rejected', `got ${t} (expected auth.error)`);
  } catch (e) { fail('ws-forged-token-rejected', e.message); }

  // --- 5) Seat-secret path still works ---
  try {
    const t = await wsAuthOnce(wsUrl, { playerId: claim.playerId, playerSecret: claim.playerSecret });
    if (t === 'auth.success') pass('ws-seat-secret-auth');
    else fail('ws-seat-secret-auth', `got ${t}`);
  } catch (e) { fail('ws-seat-secret-auth', e.message); }

  console.log(results.join('\n'));
  const failures = results.filter(r => r.startsWith('FAIL')).length;
  console.log(failures === 0 ? 'ALL PROBES PASSED' : `${failures} PROBE FAILURES`);
  process.exit(failures === 0 ? 0 : 1);
})();
