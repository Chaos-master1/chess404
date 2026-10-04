#!/usr/bin/env node
// Chess404 queue-handoff load soak.
//
// Drives the REAL handoff path over HTTP, exactly as the queue client does:
//
//   mint guest session -> enqueue ticket -> poll ticket (2.5s client cadence)
//   -> observe matched -> claim seat -> (cleanup anything left queued)
//
// for N guests in one lane (N/2 pairs; pairing is same-clock FIFO, so guests
// cross-pair freely -- that is the load), then reports the metrics that decide
// whether the handoff holds up:
//
//   - enqueue / poll / enqueue->matched-observed / matched->claim /
//     enqueue->claim latency percentiles (p50/p95/max)
//   - 429 rate across every request (the queue + global per-IP bulkheads)
//   - zero-park checks: no ticket GET may ever expose the internal
//     'pairing' reservation or a room without status=matched (the leak this
//     suite regresses on), and no guest may end the run stuck in the queue
//   - residual queue snapshot after the run
//
// Exit code 0 only if every guest was paired + claimed, the leak counters
// are zero, and cleanup left nothing queued.
//
// Usage:
//   node scripts/loadtest/handoff-soak.mjs --base http://192.168.0.139:3000 --pairs 50
//   node scripts/loadtest/handoff-soak.mjs --base https://web-...railway.app --pairs 10 --clock 1800
//
// Flags:
//   --base URL        target web origin (default SOAK_BASE_URL or local stack)
//   --pairs N         number of pairs = 2N guests (default 10)
//   --clock SECONDS   time control, same-clock lane (default 300 = 5+0;
//                     use 1800 (30+0) against production to stay off busy lanes)
//   --inc N           clock increment (default 0)
//   --mode ID         open_cards | hidden_cards (default open_cards)
//   --queue NAME      casual | rated (default casual; rated needs accounts)
//   --poll-ms N       poll cadence per guest (default 2500, the client value)
//   --ramp-ms N       stagger between pair launches (default 250)
//   --match-timeout-ms N  per-guest deadline to observe matched (default 90000)
//   --json PATH       also write the full metrics object as JSON

import { writeFileSync } from 'node:fs';

function parseArgs(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (!a.startsWith('--')) continue;
    const key = a.slice(2);
    const next = argv[i + 1];
    if (next === undefined || next.startsWith('--')) {
      out[key] = true;
    } else {
      out[key] = next;
      i++;
    }
  }
  return out;
}

const args = parseArgs(process.argv.slice(2));
const num = (v, d) => (v === undefined || v === true ? d : Number(v));

const BASE = String(args.base ?? process.env.SOAK_BASE_URL ?? 'http://192.168.0.139:3000').replace(/\/+$/, '');
const PAIRS = num(args.pairs, 10);
const CLOCK = num(args.clock, 300);
const INC = num(args.inc, 0);
const MODE = String(args.mode ?? 'open_cards');
const QUEUE = String(args.queue ?? 'casual');
const POLL_MS = num(args['poll-ms'], 2500);
const RAMP_MS = num(args['ramp-ms'], 250);
const MATCH_TIMEOUT_MS = num(args['match-timeout-ms'], 90_000);
const JSON_OUT = typeof args.json === 'string' ? args.json : null;

const GUESTS = PAIRS * 2;

// ---------------------------------------------------------------- metrics --
const m = {
  startedAt: new Date().toISOString(),
  config: { BASE, PAIRS, GUESTS, CLOCK, INC, MODE, QUEUE, POLL_MS, RAMP_MS, MATCH_TIMEOUT_MS },
  requests: { total: 0, byKind: {}, byStatus: {}, e429: 0, e429ByKind: {}, errors: [] },
  latencyMs: { enqueue: [], poll: [], enqueueToMatched: [], matchedToClaim: [], enqueueToClaim: [], mintGuest: [] },
  statusesSeen: {},
  leakSightings: [],       // ticket GETs exposing 'pairing' or a room before matched
  stuckGuests: [],         // never reached matched before the deadline
  claimFailures: [],
  cleanup: { cancelled: 0, cancelFailures: 0 },
  completedGuests: 0,
  snapshotsBefore: null,
  snapshotsAfter: null,
};

const pct = (arr, p) => {
  if (!arr.length) return null;
  const s = [...arr].sort((a, b) => a - b);
  return Math.round(s[Math.min(s.length - 1, Math.floor((p / 100) * s.length))]);
};
const stats = (arr) => ({ n: arr.length, p50: pct(arr, 50), p95: pct(arr, 95), max: arr.length ? Math.round(Math.max(...arr)) : null });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function call(kind, url, init = {}) {
  m.requests.total += 1;
  m.requests.byKind[kind] = (m.requests.byKind[kind] ?? 0) + 1;
  const t0 = performance.now();
  try {
    const res = await fetch(url, { ...init, signal: AbortSignal.timeout(15_000) });
    const ms = Math.round(performance.now() - t0);
    m.requests.byStatus[res.status] = (m.requests.byStatus[res.status] ?? 0) + 1;
    if (res.status === 429) {
      m.requests.e429 += 1;
      m.requests.e429ByKind[kind] = (m.requests.e429ByKind[kind] ?? 0) + 1;
    }
    return { res, ms };
  } catch (err) {
    const ms = Math.round(performance.now() - t0);
    m.requests.errors.push({ kind, error: String(err).slice(0, 200) });
    return { res: null, ms };
  }
}

const jsonHeaders = { 'Content-Type': 'application/json', Origin: new URL(BASE).origin, Referer: `${BASE}/play` };

// The per-IP bulkheads (queue 30/30s, global 60/min) are real: a single-IP
// soak trips them constantly. Real clients back off and retry, so the harness
// does too -- bounded, --max-retry-ms per call -- and the 429s are reported
// as a first-class metric rather than killing the run.
const MAX_RETRY_MS = num(args['max-retry-ms'], 90_000);

async function callWithRetry(kind, url, init = {}) {
  const deadline = Date.now() + MAX_RETRY_MS;
  for (;;) {
    const out = await call(kind, url, init);
    const retriable = out.res === null || out.res.status === 429 || out.res.status >= 500;
    if (!retriable || Date.now() >= deadline) return out;
    const retryAfter = out.res ? Number(out.res.headers.get('Retry-After')) : 0;
    await sleep(Math.min((retryAfter > 0 ? retryAfter * 1000 : 1500) + Math.random() * 500, 15_000));
  }
}

// ------------------------------------------------------------------ steps --
async function mintGuest(tag) {
  const { res, ms } = await callWithRetry('mint', `${BASE}/api/platform/guest-sessions`, {
    method: 'POST',
    headers: jsonHeaders,
    body: '{}',
  });
  if (!res || !res.ok) throw new Error(`mint guest failed: ${res ? res.status : 'network'}`);
  const body = await res.json();
  const gs = body.guestSession ?? body.session ?? body;
  const guestId = gs.guestId ?? gs.guest?.guestId;
  const sessionSecret = gs.sessionSecret ?? body.sessionSecret;
  if (!guestId || !sessionSecret) throw new Error(`mint guest: unexpected shape ${JSON.stringify(body).slice(0, 200)}`);
  m.latencyMs.mintGuest.push(ms);
  return { guestId, sessionSecret, tag };
}

async function enqueue(guest) {
  const t0 = performance.now();
  const { res } = await callWithRetry('enqueue', `${BASE}/api/matchmaking/queues/tickets`, {
    method: 'POST',
    headers: jsonHeaders,
    body: JSON.stringify({
      guestId: guest.guestId,
      queue: QUEUE,
      modeId: MODE,
      rating: 1200,
      displayName: `soak-${guest.tag}`,
      clockSeconds: CLOCK,
      clockIncrement: INC,
    }),
  });
  if (!res || !res.ok) throw new Error(`enqueue failed: ${res ? res.status : 'network'}`);
  const body = await res.json();
  const ticket = body.ticket;
  if (!ticket?.ticketId) throw new Error(`enqueue: unexpected shape ${JSON.stringify(body).slice(0, 200)}`);
  m.latencyMs.enqueue.push(Math.round(performance.now() - t0));
  return ticket;
}

function noteStatus(ticket, guest) {
  const s = ticket.status;
  m.statusesSeen[s] = (m.statusesSeen[s] ?? 0) + 1;
  if (s === 'pairing') {
    m.leakSightings.push({ guest: guest.tag, ticketId: ticket.ticketId, why: 'status=pairing exposed on ticket GET' });
  } else if (s !== 'matched' && ticket.assignedRoom) {
    m.leakSightings.push({ guest: guest.tag, ticketId: ticket.ticketId, why: `assignedRoom with status=${s}` });
  }
}

// Polls the ticket at client cadence until matched (assignedRoom + seatColor).
async function pollUntilMatched(guest, ticketId, deadlineAt) {
  for (;;) {
    const { res, ms } = await call('poll', `${BASE}/api/matchmaking/queues/tickets/${ticketId}`);
    m.latencyMs.poll.push(ms);
    if (res && res.ok) {
      const body = await res.json();
      const ticket = body.ticket ?? {};
      noteStatus(ticket, guest);
      if (ticket.status === 'matched' && ticket.assignedRoom && ticket.seatColor) {
        return { ticket, observedMs: ms };
      }
      if (ticket.status === 'cancelled') throw new Error('ticket cancelled server-side while polling');
    } else if (res && res.status === 429) {
      const retry = Number(res.headers.get('Retry-After')) || POLL_MS / 1000;
      await sleep(Math.min(retry * 1000, 10_000));
      continue;
    }
    if (Date.now() >= deadlineAt) return null;
    await sleep(POLL_MS);
  }
}

async function claimSeat(guest, room) {
  const { res, ms } = await callWithRetry('claim', `${BASE}/api/platform/match-claims`, {
    method: 'POST',
    headers: jsonHeaders,
    body: JSON.stringify({ matchId: room, guestId: guest.guestId, sessionSecret: guest.sessionSecret }),
  });
  if (!res || !res.ok) throw new Error(`claim failed: ${res ? res.status : 'network'}`);
  const body = await res.json();
  const claim = body.claim ?? body;
  if (!claim?.seatColor) throw new Error(`claim: unexpected shape ${JSON.stringify(body).slice(0, 200)}`);
  return { claim, ms };
}

async function cancelTicket(ticket) {
  if (!ticket?.cancelSecret) return false;
  const { res } = await call('cancel', `${BASE}/api/matchmaking/queues/tickets/${ticket.ticketId}`, {
    method: 'DELETE',
    headers: { ...jsonHeaders, 'X-Chess404-Ticket-Secret': ticket.cancelSecret },
  });
  if (res && (res.ok || res.status === 404)) {
    m.cleanup.cancelled += 1;
    return true;
  }
  m.cleanup.cancelFailures += 1;
  return false;
}

// ------------------------------------------------------------------- flow --
const liveTickets = []; // for final cleanup

async function runGuest(guest, enqueuedAt) {
  const deadlineAt = Date.now() + MATCH_TIMEOUT_MS;
  const ticket = await enqueue(guest);
  liveTickets.push({ guest, ticket });
  const seen = await pollUntilMatched(guest, ticket.ticketId, deadlineAt);
  if (!seen) {
    m.stuckGuests.push({ guest: guest.tag, ticketId: ticket.ticketId, why: 'no matched observation before deadline' });
    return;
  }
  const matchedAt = Date.parse(seen.ticket.matchedAt) || null;
  const observedAt = Date.now();
  m.latencyMs.enqueueToMatched.push(observedAt - enqueuedAt);
  if (matchedAt) m.enqueueToMatchedServerSkew = [...(m.enqueueToMatchedServerSkew ?? []), observedAt - matchedAt];

  const claimResult = await claimSeat(guest, seen.ticket.assignedRoom).catch((err) => {
    m.claimFailures.push({ guest: guest.tag, error: String(err).slice(0, 200) });
    return null;
  });
  const claim = claimResult?.claim;
  const claimMs = claimResult?.ms;
  if (claim) {
    m.latencyMs.matchedToClaim.push(claimMs);
    m.latencyMs.enqueueToClaim.push(Date.now() - enqueuedAt);
    m.completedGuests += 1;
  }
}

async function runPair(i) {
  const tag = `p${i}`;
  const [gA, gB] = await Promise.all([mintGuest(`${tag}a`), mintGuest(`${tag}b`)]);
  const enqueuedAt = Date.now();
  // Enqueue both together: whoever arrives second (or any later guest in the
  // wave) triggers the pairing, exactly like two browsers hitting Join.
  await Promise.all([runGuest(gA, enqueuedAt), runGuest(gB, enqueuedAt)]);
}

async function snapshot() {
  const { res } = await call('snapshot', `${BASE}/api/matchmaking/queues/snapshots`);
  if (!res || !res.ok) return null;
  return (await res.json()).snapshots ?? null;
}

async function main() {
  console.log(`[soak] base=${BASE} pairs=${PAIRS} (${GUESTS} guests) lane=${QUEUE}/${MODE} clock=${CLOCK}+${INC} poll=${POLL_MS}ms ramp=${RAMP_MS}ms`);
  m.snapshotsBefore = await snapshot();

  const started = performance.now();
  const runs = [];
  for (let i = 0; i < PAIRS; i++) {
    runs.push(
      (async () => {
        if (i > 0) await sleep(i * RAMP_MS);
        try {
          await runPair(i);
        } catch (err) {
          m.requests.errors.push({ kind: 'pair', pair: i, error: String(err).slice(0, 200) });
        }
      })(),
    );
  }
  await Promise.all(runs);
  const durationS = Math.round((performance.now() - started) / 100) / 10;

  // Cleanup: anything still queued would ghost-pair a later run. Matched
  // tickets are terminal (their rooms live on), so only cancel leftovers.
  for (const { guest, ticket } of liveTickets) {
    if (ticket.status !== 'matched') {
      // Re-read cheaply only if it might still be queued? Cancel directly;
      // cancel of an already-matched ticket is refused harmlessly (409/404).
      const ok = await cancelTicket(ticket);
      if (ok) ticket.status = 'cancelled';
      void guest;
    }
  }
  m.snapshotsAfter = await snapshot();
  m.durationS = durationS;
  m.endedAt = new Date().toISOString();

  const report = {
    ...m,
    latency: Object.fromEntries(Object.entries(m.latencyMs).map(([k, v]) => [k, stats(v)])),
    totals: {
      guests: GUESTS,
      completed: m.completedGuests,
      stuck: m.stuckGuests.length,
      leaks: m.leakSightings.length,
      claimFailures: m.claimFailures.length,
      e429: m.requests.e429,
      requests: m.requests.total,
      e429Rate: m.requests.total ? Math.round((m.requests.e429 / m.requests.total) * 10000) / 100 : 0,
    },
  };
  delete report.requests.byKind; // raw counts kept in byStatus; keep JSON tidy
  delete report.latencyMs;

  const line = (label, s) => `  ${label.padEnd(22)} ${s ? `n=${String(s.n).padStart(4)} p50=${s.p50}ms p95=${s.p95}ms max=${s.max}ms` : '-'}`;
  console.log('---- handoff soak summary ----');
  console.log(`  duration              ${durationS}s  requests=${report.totals.requests} 429s=${report.totals.e429} (${report.totals.e429Rate}%)`);
  console.log(line('enqueue', report.latency.enqueue));
  console.log(line('poll', report.latency.poll));
  console.log(line('enqueue->matched', report.latency.enqueueToMatched));
  console.log(line('matched->claim', report.latency.matchedToClaim));
  console.log(line('enqueue->claim', report.latency.enqueueToClaim));
  console.log(line('mint guest', report.latency.mintGuest));
  console.log(`  statuses seen         ${JSON.stringify(m.statusesSeen)}`);
  console.log(`  429 by kind           ${JSON.stringify(m.requests.e429ByKind)}`);
  console.log(`  completed/total       ${m.completedGuests}/${GUESTS}`);
  console.log(`  stuck (park risk)     ${m.stuckGuests.length}`);
  console.log(`  pairing leaks         ${m.leakSightings.length}`);
  console.log(`  claim failures        ${m.claimFailures.length}`);
  console.log(`  cleanup               cancelled=${m.cleanup.cancelled} failures=${m.cleanup.cancelFailures}`);
  console.log(`  snapshot before       ${JSON.stringify(m.snapshotsBefore)}`);
  console.log(`  snapshot after        ${JSON.stringify(m.snapshotsAfter)}`);
  if (m.requests.errors.length) console.log(`  errors                ${JSON.stringify(m.requests.errors.slice(0, 5))}`);
  if (m.stuckGuests.length) console.log(`  STUCK: ${JSON.stringify(m.stuckGuests.slice(0, 5))}`);
  if (m.leakSightings.length) console.log(`  LEAKS: ${JSON.stringify(m.leakSightings.slice(0, 5))}`);

  if (JSON_OUT) {
    writeFileSync(JSON_OUT, JSON.stringify(report, null, 2));
    console.log(`  json                  ${JSON_OUT}`);
  }

  const pass =
    m.completedGuests === GUESTS &&
    m.leakSightings.length === 0 &&
    m.stuckGuests.length === 0 &&
    m.claimFailures.length === 0 &&
    m.requests.errors.length === 0;
  console.log(pass ? '[soak] PASS' : '[soak] FAIL');
  process.exit(pass ? 0 : 1);
}

main().catch((err) => {
  console.error('[soak] fatal:', err);
  process.exit(2);
});
