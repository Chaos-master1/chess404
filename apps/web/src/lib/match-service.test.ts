// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  configureMatchServiceRuntime,
  connectToMatchStream,
  type MatchStreamStatus,
} from './match-service';

// Pins down the stream layer's terminal verdict for a room it cannot read.
// The three client timers that ask the server about a room (the stream's own
// poll, the 15s reconciliation and the 5s fallback poll) all derive their
// stop conditions from this one verdict, so the boundary tested here is the
// boundary that keeps a finished/archived room from being polled forever --
// without ever calling a merely unhealthy backend, or one transient burst of
// 404s, "this room cannot be read".

type ScriptedResponse = { status: number } | { snapshot: Record<string, unknown> };

const ROOM_BURST_SEQ = 20;
const ROOM_TAIL_SEQ = 99;
// Nine refusals: one short of the budget, so the next good answer must win.
const TRANSIENT_BURST = 9;

function liveSnapshot(matchId: string, seqNum: number) {
  return { matchId, seqNum, match: { matchId, status: 'active' } };
}

function finishedSnapshot(matchId: string, seqNum: number) {
  return { matchId, seqNum, match: { matchId, status: 'finished' } };
}

// The stream layer needs `window` only as a timer/location host, plus the
// localStorage shim that buildMatchFetchHeaders reads identically to a
// browser. Everything else (fetch, WebSocket) is stubbed per test.
function stubBrowserEnvironment(): void {
  const storage = new Map<string, string>();
  vi.stubGlobal('window', {
    location: { protocol: 'http:', host: 'localhost:3000', pathname: '/match/test' },
    localStorage: {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => void storage.set(key, value),
      removeItem: (key: string) => void storage.delete(key),
    },
    setTimeout: (fn: () => void, ms?: number) => globalThis.setTimeout(fn, ms),
    clearTimeout: (id?: number) => globalThis.clearTimeout(id),
    setInterval: (fn: () => void, ms?: number) => globalThis.setInterval(fn, ms),
    clearInterval: (id?: number) => globalThis.clearInterval(id),
    addEventListener: () => {},
    removeEventListener: () => {},
  });
  vi.stubGlobal('navigator', { onLine: true });
}

// Answers real GETs by match id, consuming each match's scripted responses
// before falling back. Returns the match ids read, in order.
function installFetch(options: {
  script?: Record<string, ScriptedResponse[]>;
  fallback: (matchId: string) => ScriptedResponse;
}) {
  const reads: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const matchId = decodeURIComponent(String(input).split('/').pop() ?? '');
      reads.push(matchId);
      const queued = options.script?.[matchId];
      const next = queued?.length ? queued.shift()! : options.fallback(matchId);
      if ('status' in next) {
        return new Response(JSON.stringify({ error: 'match is not public' }), { status: next.status });
      }
      return new Response(JSON.stringify(next.snapshot), { status: 200 });
    }),
  );
  return { reads };
}

function collect(matchId: string) {
  const statuses: MatchStreamStatus[] = [];
  const seqNums: number[] = [];
  const stream = connectToMatchStream(matchId, {
    onSnapshot: snapshot => {
      if (snapshot.seqNum) seqNums.push(snapshot.seqNum);
    },
    onStatusChange: status => statuses.push(status),
  });
  return { stream, statuses, seqNums };
}

describe('connectToMatchStream terminal verdicts', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    stubBrowserEnvironment();
    // A base that resolves to no WebSocket endpoint keeps the stream on its
    // HTTP path, which is where the retry budget and the verdict live.
    configureMatchServiceRuntime({ httpBaseUrl: 'poll-only-test' });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    configureMatchServiceRuntime({ httpBaseUrl: '/api/realtime' });
  });

  it('stops asking about a room the server refuses and reports it as unreadable', async () => {
    const { reads } = installFetch({ fallback: () => ({ status: 404 }) });
    const { stream, statuses, seqNums } = collect('room_dead');

    await vi.advanceTimersByTimeAsync(10 * 60_000);

    expect(seqNums).toEqual([]);
    expect(statuses[statuses.length - 1]).toBe('unreadable');
    // Bounded, not "eventually": the retry budget is spent, then it is over.
    const readsAtVerdict = reads.length;
    expect(readsAtVerdict).toBe(10);

    await vi.advanceTimersByTimeAsync(10 * 60_000);
    expect(reads.length).toBe(readsAtVerdict);
    stream.disconnect();
  });

  it('keeps reading a live room that answers again after a transient 404 burst', async () => {
    const { reads } = installFetch({
      script: {
        room_live: [
          ...Array.from({ length: TRANSIENT_BURST }, () => ({ status: 404 }) as ScriptedResponse),
          { snapshot: liveSnapshot('room_live', ROOM_BURST_SEQ) },
        ],
      },
      fallback: matchId => ({ snapshot: liveSnapshot(matchId, ROOM_TAIL_SEQ) }),
    });
    const { stream, statuses, seqNums } = collect('room_live');

    await vi.advanceTimersByTimeAsync(10 * 60_000);

    expect(statuses).not.toContain('unreadable');
    expect(statuses).toContain('connected');
    // The room answered on the tenth read and kept being read after that:
    // a burst shorter than the budget is not a verdict.
    expect(seqNums[0]).toBe(ROOM_BURST_SEQ);
    expect(seqNums.length).toBeGreaterThan(1);
    expect(reads.length).toBeGreaterThan(10);
    stream.disconnect();
  });

  it('does not call a room unreadable when the backend is failing rather than refusing', async () => {
    // A 5xx storm is an unhealthy backend, not a verdict on the room: only a
    // refusal (404/410) may publish 'unreadable'. The degraded stream reports
    // 'disconnected' and keeps trying slowly so the outage self-heals.
    let calls = 0;
    installFetch({
      fallback: () => {
        calls += 1;
        return { status: 500 };
      },
    });
    const { stream, statuses } = collect('room_flaky');

    await vi.advanceTimersByTimeAsync(10 * 60_000);

    expect(statuses).toContain('disconnected');
    expect(statuses).not.toContain('unreadable');
    // Still asking, on its slow retry cadence: the room may come back.
    expect(calls).toBeGreaterThan(10);
    stream.disconnect();
  });

  it('gives the room a fresh budget when an explicit retry asks again', async () => {
    // One refusal is left in the script when the verdict lands: a retry that
    // inherited the spent budget (or the terminal latch) would publish
    // 'unreadable' again on that single answer and never reschedule, so the
    // affordance that offers to try again would be a lie.
    const { reads } = installFetch({
      script: {
        room_retry: [
          ...Array.from({ length: 10 }, () => ({ status: 404 }) as ScriptedResponse),
          { status: 404 },
        ],
      },
      fallback: matchId => ({ snapshot: liveSnapshot(matchId, ROOM_TAIL_SEQ) }),
    });
    const { stream, statuses, seqNums } = collect('room_retry');

    await vi.advanceTimersByTimeAsync(10 * 60_000);
    expect(statuses[statuses.length - 1]).toBe('unreadable');
    expect(seqNums).toEqual([]);

    stream.retry();
    await vi.advanceTimersByTimeAsync(60_000);

    expect(seqNums).toContain(ROOM_TAIL_SEQ);
    expect(statuses[statuses.length - 1]).toBe('connected');
    expect(reads.length).toBeGreaterThan(11);
    stream.disconnect();
  });

  it('keeps a room\u2019s verdict from spilling into another room', async () => {
    const { reads } = installFetch({
      fallback: matchId => (matchId === 'room_dead'
        ? { status: 404 }
        : { snapshot: liveSnapshot(matchId, ROOM_TAIL_SEQ) }),
    });
    const dead = collect('room_dead');
    const live = collect('room_live');

    await vi.advanceTimersByTimeAsync(10 * 60_000);

    expect(dead.statuses).toContain('unreadable');
    expect(dead.seqNums).toEqual([]);
    expect(live.statuses).not.toContain('unreadable');
    expect(live.seqNums.length).toBeGreaterThan(0);
    // The live room kept reconciling long after the dead room gave up.
    expect(reads.filter(id => id === 'room_live').length).toBeGreaterThan(10);
    dead.stream.disconnect();
    live.stream.disconnect();
  });

  it('reports a finished room as connected, and stops after the final snapshot', async () => {
    const { reads } = installFetch({
      fallback: matchId => ({ snapshot: finishedSnapshot(matchId, ROOM_TAIL_SEQ) }),
    });
    const { stream, statuses, seqNums } = collect('room_done');

    await vi.advanceTimersByTimeAsync(5 * 60_000);

    expect(seqNums).toEqual([ROOM_TAIL_SEQ]);
    expect(statuses).toContain('connected');
    expect(statuses).not.toContain('unreadable');
    expect(reads).toHaveLength(1);
    stream.disconnect();
  });
});
