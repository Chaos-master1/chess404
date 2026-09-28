import type { MatchModeId, MatchPresenceRequest, MatchSnapshotMessage, PlayerIntent } from '@chess404/contracts';
import { DEFAULT_MATCH_MODE_ID } from '@chess404/contracts';
import { readStoredGuestIdentity } from './session-storage';

const gatewayBaseUrl = '/api/gateway';
let httpBaseUrl = '/api/realtime';
let wsBaseUrl = '';
// Both floors keep one spectator under the Go services' 60 req/min global
// per-IP limiter (rate_limit.DefaultGlobalIPLimit): 1000ms = 60/min exactly,
// so the idle poll sits just under it and leaves headroom for the REST of the
// client's traffic (bootstrap, presence, tickets) sharing the same IP bucket.
// The old 750ms floor (80 req/min) meant a few clients behind one NAT -- or
// one client whose WS had died -- hit 429 walls during exactly the degraded
// period when the poll fallback was doing its job.
const MATCH_POLL_INTERVAL_MS = 1100;
const MATCH_POLL_RETRY_INTERVAL_MS = 1000;

const latestSeqByMatch = new Map<string, number>();
const wsConnections = new Map<string, WebSocket>();

export function recordMatchSeqNum(matchId: string, seqNum: number | undefined): void {
  if (seqNum && seqNum > 0) {
    latestSeqByMatch.set(matchId, seqNum);
  }
}

export function getLatestSeqNum(matchId: string): number {
  return latestSeqByMatch.get(matchId) ?? 0;
}

export interface MatchServiceRuntimeConfig {
  httpBaseUrl?: string;
  wsBaseUrl?: string;
}

export interface CreateMatchInput {
  matchId?: string;
  seed?: number;
  clockSeconds?: number;
  clockIncrement?: number;
  starterHandMode?: 'starter_three' | 'full_catalog';
  queue?: 'casual' | 'rated' | 'direct';
  modeId?: MatchModeId;
  whiteGuestId?: string;
  blackGuestId?: string;
  whiteAccountId?: string;
  blackAccountId?: string;
  whiteName?: string;
  blackName?: string;
  whitePlayerSecret?: string;
  blackPlayerSecret?: string;
  whiteClaimToken?: string;
  blackClaimToken?: string;
}

export interface StoredRoomMeta extends CreateMatchInput {
  viewerSeat?: 'white' | 'black' | null;
  whiteClaimExpiresAt?: string;
  blackClaimExpiresAt?: string;
  difficulty?: string;
}

const ROOM_META_PREFIX = 'chess404.room.';

export function configureMatchServiceRuntime(config?: MatchServiceRuntimeConfig): void {
  const nextHttpBase = normalizeBaseUrl(config?.httpBaseUrl);
  if (nextHttpBase) {
    httpBaseUrl = nextHttpBase;
  }

  const nextWsBase = normalizeBaseUrl(config?.wsBaseUrl);
  if (nextWsBase) {
    wsBaseUrl = toWebSocketBaseUrl(nextWsBase);
  }
}

export async function createMatch(input: CreateMatchInput = {}): Promise<MatchSnapshotMessage> {
  const response = await fetch(`${httpBaseUrl}/matches`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(input)
  });

  return unwrapResponse<MatchSnapshotMessage>(response);
}

export async function fetchAuthToken(matchId: string, playerId: string, playerSecret: string): Promise<string | null> {
  try {
    const response = await fetch(`${httpBaseUrl}/matches/${matchId}/token`, {
      method: 'GET',
      headers: { 'X-Player-ID': playerId, 'X-Player-Secret': playerSecret },
    });
    if (!response.ok) {
      return null;
    }
    const data = await response.json() as { token: string };
    return data.token;
  } catch {
    return null;
  }
}

// Hard budget for one fetchMatch attempt. Callers pass their own external
// signal (component-scoped aborts must always win); the per-attempt controller
// only enforces this cap so a wedged proxy cannot hang the caller forever.
const MATCH_FETCH_ATTEMPT_TIMEOUT_MS = 15_000;
// Prod has observed 12-16s stalls under load (dev compiles behave the same),
// so a single 15s attempt aborts right as the upstream would have answered.
// Retrying with backoff lets a slow-but-alive upstream win eventually instead
// of surfacing "Match could not load" to both players.
const MATCH_FETCH_RETRIES = 3;
const MATCH_FETCH_RETRY_BASE_MS = 400;

async function fetchMatchOnce(matchId: string, signal?: AbortSignal): Promise<MatchSnapshotMessage> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), MATCH_FETCH_ATTEMPT_TIMEOUT_MS);
  const combinedSignal = signal ? anySignal([signal, controller.signal]) : controller.signal;
  try {
    const response = await fetch(`${httpBaseUrl}/matches/${matchId}`, {
      method: 'GET',
      headers: buildMatchFetchHeaders(),
      cache: 'no-store',
      signal: combinedSignal,
    });
    return unwrapResponse<MatchSnapshotMessage>(response);
  } finally {
    clearTimeout(timeout);
  }
}

export async function fetchMatch(matchId: string, signal?: AbortSignal): Promise<MatchSnapshotMessage> {
  let lastError: unknown;
  for (let attempt = 0; attempt <= MATCH_FETCH_RETRIES; attempt++) {
    if (signal?.aborted) {
      throw new DOMException('fetchMatch aborted', 'AbortError');
    }
    try {
      return await fetchMatchOnce(matchId, signal);
    } catch (err) {
      lastError = err;
      // Never retry deterministic failures: the match does not exist or the
      // viewer is not allowed to see it. A retry only delays the inevitable.
      const errStatus = err instanceof Error ? (err as HttpError).status : undefined;
      if (typeof errStatus === 'number' && errStatus >= 400 && errStatus < 500) {
        throw err;
      }
      if (err instanceof Error && /\b404\b|\b403\b/.test(err.message) && !/rate limited/i.test(err.message)) {
        throw err;
      }
      // Caller cancelled (unmount/navigation) -- stop immediately.
      if (err instanceof DOMException && err.name === 'AbortError' && signal?.aborted) {
        throw err;
      }
      if (attempt < MATCH_FETCH_RETRIES) {
        const backoff = MATCH_FETCH_RETRY_BASE_MS * 2 ** attempt;
        await new Promise(resolve => setTimeout(resolve, backoff));
      }
    }
  }
  throw lastError;
}

function anySignal(signals: AbortSignal[]): AbortSignal {
  const controller = new AbortController();
  for (const signal of signals) {
    if (signal.aborted) {
      controller.abort(signal.reason);
      return controller.signal;
    }
    signal.addEventListener('abort', () => controller.abort(signal.reason), { once: true });
  }
  return controller.signal;
}

export async function ensureMatch(input: CreateMatchInput & { matchId: string }): Promise<MatchSnapshotMessage> {
  try {
    return await fetchMatch(input.matchId);
  } catch (err) {
    if (err instanceof Error && /404|not found/i.test(err.message)) {
      return createMatch(input);
    }
    throw err;
  }
}

export function sendIntentViaWs(matchId: string, intent: Omit<PlayerIntent, 'matchId'>): boolean {
  const ws = wsConnections.get(matchId);
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    return false;
  }
  const latestSeq = latestSeqByMatch.get(matchId) ?? 0;
  const intentWithSeq = { ...intent, expectedSeqNum: latestSeq } as Omit<PlayerIntent, 'matchId'>;
  ws.send(JSON.stringify({
    type: 'apply_intent',
    payload: {
      ...intentWithSeq,
      matchId
    }
  }));
  return true;
}

export async function applyIntent(matchId: string, intent: Omit<PlayerIntent, 'matchId'>): Promise<MatchSnapshotMessage> {
  const latestSeq = latestSeqByMatch.get(matchId) ?? 0;
  const intentWithSeq = { ...intent, expectedSeqNum: latestSeq } as Omit<PlayerIntent, 'matchId'>;
  const response = await fetch(buildIntentUrl(matchId), {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      intent: {
        ...intentWithSeq,
        matchId
      }
    })
  });

  try {
    const snapshot = await unwrapResponse<MatchSnapshotMessage>(response);
    if (snapshot?.seqNum && snapshot.seqNum > 0) {
      latestSeqByMatch.set(matchId, snapshot.seqNum);
    }
    return snapshot;
  } catch (err) {
    // The most common rejection here is the server's staleness check
    // (expectedSeqNum behind its current counter) -- typically because a
    // WebSocket update was missed during a brief disconnect. Every caller
    // of applyIntent (moves, card plays, target selection, joker picks...)
    // read latestSeqByMatch fresh on each attempt but nothing wrote to it
    // again after a failure, so a client that fell behind once resent that
    // same now-permanently-stale value on every retry and failed the same
    // way forever, with no path back short of a full page reload. This
    // does not change what the caller sees -- the same error still
    // rejects the same way -- it just refreshes the tracked seq in the
    // background so the *next* attempt has a chance of succeeding.
    void fetchMatch(matchId).then(fresh => {
      if (fresh?.seqNum && fresh.seqNum > 0) {
        latestSeqByMatch.set(matchId, fresh.seqNum);
      }
    }).catch(() => {});
    throw err;
  }
}

export async function sendMatchPresenceHeartbeat(
  matchId: string,
  presence: MatchPresenceRequest,
): Promise<void> {
  const response = await fetch(buildPresenceUrl(matchId), {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(presence),
  });

  if (!response.ok) {
    await unwrapResponse<never>(response);
  }
}

export function createSeatSecret(): string {
  if (typeof globalThis !== 'undefined' && globalThis.crypto?.randomUUID) {
    return globalThis.crypto.randomUUID();
  }
  return `seat_${Date.now()}_${Math.random().toString(36).slice(2, 12)}`;
}

export function resolveSeatSecret(existingSecret?: string | null, guestSessionSecret?: string | null): string {
  const stored = normalizeSecret(existingSecret);
  if (stored) {
    return stored;
  }
  const session = normalizeSecret(guestSessionSecret);
  if (session) {
    return session;
  }
  return createSeatSecret();
}

export function readStoredRoomMeta(matchId: string): StoredRoomMeta | null {
  if (typeof window === 'undefined') {
    return null;
  }
  const raw = window.localStorage.getItem(`${ROOM_META_PREFIX}${matchId}`);
  if (!raw) {
    return null;
  }
  try {
    const parsed = JSON.parse(raw) as StoredRoomMeta;
    return {
      ...parsed,
      modeId: parsed.modeId ?? DEFAULT_MATCH_MODE_ID,
    };
  } catch {
    return null;
  }
}

export function writeStoredRoomMeta(matchId: string, meta: StoredRoomMeta | null): void {
  if (typeof window === 'undefined') {
    return;
  }
  const key = `${ROOM_META_PREFIX}${matchId}`;
  if (!meta) {
    window.localStorage.removeItem(key);
    return;
  }
  // Empty-string credentials are NOT credentials: a claim whose playerSecret
  // came back "" (server refused/omitted it) must not be stored as if the
  // browser held the seat secret -- downstream code checks
  // `playerSecret?.trim()` and a stored "" both reads as "present" to
  // Object.entries and as "absent" to trim checks, producing the worst
  // combination (heartbeat attempts that can never authenticate + fallback
  // paths that think a secret exists). Store undefined instead so every
  // "is this credential present?" check agrees.
  const sanitized: StoredRoomMeta = {
    ...meta,
    whitePlayerSecret: normalizeSecret(meta.whitePlayerSecret) || undefined,
    blackPlayerSecret: normalizeSecret(meta.blackPlayerSecret) || undefined,
    whiteClaimToken: normalizeSecret(meta.whiteClaimToken) || undefined,
    blackClaimToken: normalizeSecret(meta.blackClaimToken) || undefined,
  };
  window.localStorage.setItem(key, JSON.stringify({
    ...sanitized,
    modeId: sanitized.modeId ?? DEFAULT_MATCH_MODE_ID,
  }));
}

// "Gone" = the server answered definitively that this match is finished or
// archived and this browser can no longer read it (404/410). Cached for this
// page session so revisiting the room does not re-fail on every navigation
// and every reconciliation poll. Module-scoped on purpose: cleared by a page
// reload, so a re-created match with a recycled ID is still reachable after a
// refresh. The 404 from our own proxy for an unreadable private match is
// indistinguishable from a true match-gone 404 at this layer, so anything that
// must retry through it passes { force: true }.
const goneMatchIds = new Set<string>();

export function markMatchGone(matchId: string): void {
  const trimmed = matchId?.trim();
  if (trimmed) goneMatchIds.add(trimmed);
}

export function isMatchGone(matchId: string): boolean {
  return goneMatchIds.has(matchId?.trim() ?? '');
}

export function connectToMatchStream(
  matchId: string,
  handlers: {
    onSnapshot: (snapshot: MatchSnapshotMessage) => void;
    onStatusChange?: (status: 'connecting' | 'connected' | 'reconnecting' | 'disconnected') => void;
    onError?: (error: Event) => void;
  },
  playerIdentity?: { playerId?: string; playerSecret?: string; playerClaimToken?: string } | null
): { disconnect: () => void; retry: () => void } {
  let socket: WebSocket | null = null;
  let reconnectTimer: number | null = null;
  let pollTimer: number | null = null;
  let disposed = false;
  let reconnectAttempt = 0;
  let lastSeqNum = 0;
  let isWsConnected = false;
  let pollFailures = 0;
  // Set once ANY authoritative snapshot has been applied for this match.
  // Distinguishes "the room existed and is now gone" (finished/archived:
  // stop polling) from "we could never see it at all" (auth/creds problem:
  // keep retrying, a reconnect may fix it).
  let sawLiveSnapshot = false;
  // Terminal-state latch: once a snapshot with status "finished" has been
  // delivered, the stream is done. The server stops broadcasting after the
  // final state, so without this latch every reconnect attempt, poll tick
  // and watchdog fired forever -- the post-game "Reconnecting…" banner +
  // ping loop on finished matches.
  let finished = false;
  // Zombie-socket watchdog: the server pings every 20s (browser answers
  // automatically) and the 1s tick broadcasts snapshots, so a HEALTHY stream
  // delivers a message at least every ~2s. If nothing arrives for 45s while
  // the socket claims to be open, the connection is silently dead (NAT
  // timeout, proxy drop) -- close it so the reconnect path takes over
  // instead of the UI sitting on a frozen board believing it is live.
  let lastStreamMessageAt = 0;
  let watchdogTimer: number | null = null;
  const STREAM_WATCHDOG_MS = 45_000;

  const startWatchdog = () => {
    stopWatchdog();
    lastStreamMessageAt = Date.now();
    watchdogTimer = window.setInterval(() => {
      if (disposed || finished || !isWsConnected) return;
      if (Date.now() - lastStreamMessageAt <= STREAM_WATCHDOG_MS) return;
      console.warn('[match-stream] no messages for ' + STREAM_WATCHDOG_MS / 1000 + 's on an open socket; forcing reconnect');
      try { socket?.close(); } catch { /* already closing */ }
    }, 5_000);
  };

  const stopWatchdog = () => {
    if (watchdogTimer !== null) {
      window.clearInterval(watchdogTimer);
      watchdogTimer = null;
    }
  };

  const clearReconnectTimer = () => {
    if (reconnectTimer !== null) {
      window.clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
  };

  const clearPollTimer = () => {
    if (pollTimer !== null) {
      window.clearTimeout(pollTimer);
      pollTimer = null;
    }
  };

  const schedulePoll = (delay = MATCH_POLL_INTERVAL_MS) => {
    if (disposed) {
      return;
    }
    if (isWsConnected) {
      clearPollTimer();
      return;
    }
    clearPollTimer();
    pollTimer = window.setTimeout(async () => {
      pollTimer = null;
      if (disposed) {
        return;
      }
      let nextDelay = MATCH_POLL_INTERVAL_MS;
      try {
        const snapshot = await fetchMatch(matchId);
        if (!disposed) {
          pollFailures = 0;
          sawLiveSnapshot = true;
          if (snapshot.seqNum) recordMatchSeqNum(matchId, snapshot.seqNum);
          handlers.onSnapshot(snapshot);
          if (snapshot.match?.status === 'finished') {
            finished = true;
            clearPollTimer();
            handlers.onStatusChange?.('connected');
          }
          handlers.onStatusChange?.('connected');
        }
      } catch (error) {
        if (!disposed) {
          // Back off exponentially so a rate-limited or unreachable gateway
          // cannot turn the fallback into a hot loop (this exact loop used to
          // spin at 750ms forever, leaving the match a zombie with a
          // "Reconnecting..." banner and no way back).
          pollFailures += 1;
          nextDelay = Math.min(30_000, MATCH_POLL_RETRY_INTERVAL_MS * 2 ** Math.min(pollFailures - 1, 5));
          const retryAfter = error instanceof Error ? /retry after (\d+)s/.exec(error.message) : null;
          if (retryAfter) nextDelay = Math.max(nextDelay, (parseInt(retryAfter[1], 10) || 1) * 1000);
          // A definitive 404/410 after the match has delivered real snapshots
          // means the room was archived/GC'd out from under us -- usually
          // because it FINISHED and the server stopped serving it to this
          // viewer. Polling it forever produced the post-game 404 storm and
          // the stuck reconnect banner. Treat it as a graceful terminal stop,
          // exactly like a finished snapshot.
          const errStatus = (error as { status?: number } | null)?.status;
          const definitiveGone = (errStatus === 404 || errStatus === 410) && sawLiveSnapshot;
          if (definitiveGone) {
            finished = true;
            clearPollTimer();
            handlers.onStatusChange?.('connected');
            return;
          }
          // After ten straight failures stop pretending to recover: surface
          // the manual ↻ Reconnect affordance. The loop keeps trying slowly
          // in the background so a transient outage still self-heals.
          handlers.onStatusChange?.(pollFailures >= 10 ? 'disconnected' : 'reconnecting');
        }
      } finally {
        if (!disposed && !finished) schedulePoll(nextDelay);
      }
    }, delay);
  };

  const maxReconnectAttempts = 10;
  const scheduleReconnect = () => {
    if (disposed || finished) {
      return;
    }
    if (reconnectAttempt >= maxReconnectAttempts) {
      console.warn('max reconnect attempts reached, falling back to polling');
      handlers.onStatusChange?.('connected');
      schedulePoll(0);
      return;
    }
    if (typeof navigator !== 'undefined' && !navigator.onLine) {
      // Offline: wait for the network to come back, then reconnect with a
      // fresh attempt budget instead of burning retries while offline.
      const onOnline = () => {
        window.removeEventListener('online', onOnline);
        reconnectAttempt = 0;
        if (!disposed) connect();
      };
      window.addEventListener('online', onOnline);
      return;
    }
    clearReconnectTimer();
    handlers.onStatusChange?.('reconnecting');
    const delay = Math.min(5000, 500 * 2 ** Math.min(reconnectAttempt, 4)) + Math.random() * 1000;
    reconnectAttempt += 1;
    reconnectTimer = window.setTimeout(() => {
      reconnectTimer = null;
      connect();
    }, delay);
  };

  const connect = () => {
    if (disposed) {
      return;
    }
    handlers.onStatusChange?.(reconnectAttempt > 0 ? 'reconnecting' : 'connecting');
    const nextSocketUrl = resolveWebSocketBaseUrl();
    if (!nextSocketUrl) {
      handlers.onStatusChange?.('connected');
      schedulePoll(reconnectAttempt > 0 ? MATCH_POLL_RETRY_INTERVAL_MS : 0);
      return;
    }

    const wsUrl = `${nextSocketUrl}/api/matches/${matchId}/ws`;

    let authPromise: Promise<{ claimToken: string | null }>;
    // The seat secret is durable; a claim token is single-use and is already
    // spent by the bootstrap/join that created this room. Preferring the token
    // made every reconnect re-send a dead credential -> auth.error -> close ->
    // retry forever, so the live stream never came up and every client silently
    // degraded to 1s HTTP polling. Secret first, token only as a fallback.
    if (playerIdentity?.playerId?.trim() && playerIdentity?.playerSecret?.trim()) {
      authPromise = fetchAuthToken(matchId, playerIdentity.playerId.trim(), playerIdentity.playerSecret.trim())
        .then(token => ({ claimToken: token }));
    } else if (playerIdentity?.playerClaimToken?.trim()) {
      authPromise = Promise.resolve({ claimToken: playerIdentity.playerClaimToken!.trim() });
    } else {
      // Spectate has no player identity, so the WS stream is unavailable;
      // polling fallback is the intended path.
      handlers.onStatusChange?.('connected');
      isWsConnected = true;
      schedulePoll(0);
      return;
    }

    authPromise.then(({ claimToken }) => {
      if (disposed) return;
      const nextSocket = new WebSocket(wsUrl);
      socket = nextSocket;

      let authReceived = false;

      nextSocket.addEventListener('open', () => {
        wsConnections.set(matchId, nextSocket);
        nextSocket.send(JSON.stringify({ type: 'auth', claimToken }));
      });

      nextSocket.addEventListener('message', event => {
        try {
          const msg = JSON.parse(event.data as string) as { type?: string; payload?: MatchSnapshotMessage };
          if (msg.type === 'auth.success') {
            if (authReceived) return;
            authReceived = true;
            reconnectAttempt = 0;
            isWsConnected = true;
            lastStreamMessageAt = Date.now();
            startWatchdog();
            handlers.onStatusChange?.('connected');
            return;
          }
          if (msg.type === 'auth.error') {
            nextSocket.close();
            handlers.onStatusChange?.('disconnected');
            return;
          }
          if (!authReceived) return;
          lastStreamMessageAt = Date.now();
          if (msg.type === 'match.snapshot' && msg.payload) {
            const snapshot = msg.payload;
            if (snapshot.seqNum && lastSeqNum > 0 && snapshot.seqNum > lastSeqNum + 1) {
              // A seq gap means a dropped stream event; refetch is the recovery.
              fetchMatch(matchId).then(fullSnapshot => {
                if (!disposed) handlers.onSnapshot(fullSnapshot);
              }).catch(() => {});
            }
            if (snapshot.seqNum) {
              lastSeqNum = snapshot.seqNum;
              recordMatchSeqNum(matchId, snapshot.seqNum);
            }
            sawLiveSnapshot = true;
            handlers.onSnapshot(snapshot);
            if (snapshot.match?.status === 'finished' && !finished) {
              // Final state delivered: stop quietly. Closing the socket here
              // must NOT enter the reconnect loop -- that loop was the
              // post-game "Reconnecting…" banner + sound ping on finished
              // matches.
              finished = true;
              stopWatchdog();
              clearPollTimer();
              clearReconnectTimer();
              try { nextSocket.close(); } catch { /* already closing */ }
              handlers.onStatusChange?.('connected');
            }
          }
        } catch {
          // Ignore malformed payloads.
        }
      });

      nextSocket.addEventListener('error', event => {
        handlers.onError?.(event);
        isWsConnected = false;
        if (!disposed) nextSocket.close();
      });

      nextSocket.addEventListener('close', () => {
        if (socket === nextSocket) socket = null;
        if (wsConnections.get(matchId) === nextSocket) wsConnections.delete(matchId);
        isWsConnected = false;
        stopWatchdog();
        if (!disposed && !finished) scheduleReconnect();
      });
    }).catch(() => {
      if (!disposed) schedulePoll(0);
    });
  };

  connect();

  const manualRetry = () => {
    if (disposed) return;
    reconnectAttempt = 0;
    pollFailures = 0;
    clearReconnectTimer();
    clearPollTimer();
    if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
      socket.close();
    }
    socket = null;
    handlers.onStatusChange?.('connecting');
    connect();
  };

  return {
    disconnect: () => {
      disposed = true;
      clearReconnectTimer();
      clearPollTimer();
      stopWatchdog();
      handlers.onStatusChange?.('disconnected');
      if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
        socket.close();
      }
    },
    retry: manualRetry,
  };
}

// Callers need to tell "the server says this match is gone" apart from "the
// request never landed". Without the status, a transient offline blip looked
// identical to a 404 and wiped the client's active-match state.
export interface HttpError extends Error {
  status?: number;
}

function withStatus(error: Error, status: number): HttpError {
  (error as HttpError).status = status;
  return error;
}

async function unwrapResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let message = `Request failed with ${response.status}`;
    try {
      const payload = (await response.json()) as { error?: string };
      if (payload?.error) {
        message = payload.error;
      }
    } catch {
      // Ignore parse failures and keep fallback message.
    }
    if (response.status === 429) {
      const header = response.headers.get('Retry-After');
      throw withStatus(new Error(`${message} (rate limited, retry after ${header ?? 'unknown'}s)`), response.status);
    }
    throw withStatus(new Error(message), response.status);
  }

  return response.json() as Promise<T>;
}

function toWebSocketBaseUrl(input: string): string {
  if (input.startsWith('https://')) {
    return `wss://${input.slice('https://'.length)}`;
  }
  if (input.startsWith('http://')) {
    return `ws://${input.slice('http://'.length)}`;
  }
  return input;
}

// Both of these always route through the gateway now. The gateway's
// intent/presence proxies handle a caller that already has a resolved
// playerSecret (forwards it directly) just as well as one that only has a
// playerClaimToken (resolves it first) -- see proxyGatewayIntent /
// proxyGatewayPresence in cmd/gateway/main.go. The apps/web/app/api/realtime
// routes' POST handlers are deliberately local-dev-only (they 404 for any
// non-localhost request with "use the gateway match flow"), and there is no
// POST .../presence route under /api/realtime at all -- so whenever a caller
// here had a secret but no claim token, this used to build a URL under
// httpBaseUrl ("/api/realtime/matches/{id}/presence") that 404s
// unconditionally, breaking presence (and, via the equivalent intents
// branch, move submission) for every match reached through a path that
// resolves identity without a claim token, e.g. queue-matched pairing.
function buildIntentUrl(matchId: string): string {
  return `${gatewayBaseUrl}/matches/${matchId}/intents`;
}

function buildPresenceUrl(matchId: string): string {
  return `${gatewayBaseUrl}/matches/${matchId}/presence`;
}

function buildMatchFetchHeaders(): Headers {
  const headers = new Headers();
  const sides = ['white', 'black'] as const;
  for (const side of sides) {
    const identity = readStoredGuestIdentity(side);
    if (identity.guestId?.trim()) {
      headers.set(`x-chess404-${side}-guest-id`, identity.guestId.trim());
    }
    if (identity.sessionToken?.trim()) {
      headers.set(`x-chess404-${side}-session-token`, identity.sessionToken.trim());
    }
    if (identity.sessionSecret?.trim()) {
      headers.set(`x-chess404-${side}-session-secret`, identity.sessionSecret.trim());
    }
  }
  return headers;
}

function normalizeSecret(value?: string | null): string {
  return typeof value === 'string' ? value.trim() : '';
}

function normalizeBaseUrl(value?: string | null): string {
  return typeof value === 'string' ? value.trim().replace(/\/$/, '') : '';
}

function resolveWebSocketBaseUrl(): string | null {
  if (wsBaseUrl) {
    return wsBaseUrl;
  }

  const derivedFromHttp = deriveWebSocketBaseUrlFromHttpBase(httpBaseUrl);
  if (derivedFromHttp) {
    return derivedFromHttp;
  }

  return null;
}

function deriveWebSocketBaseUrlFromHttpBase(input: string): string | null {
  const normalized = normalizeBaseUrl(input);
  if (!normalized) {
    return null;
  }
  if (normalized.startsWith('https://')) {
    return normalized.replace(/^https:\/\//i, 'wss://').replace(/\/api(?:\/realtime)?$/i, '');
  }
  if (normalized.startsWith('http://')) {
    return normalized.replace(/^http:\/\//i, 'ws://').replace(/\/api(?:\/realtime)?$/i, '');
  }
  if (normalized.startsWith('/')) {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${protocol}//${window.location.host}${normalized.replace(/\/api(?:\/realtime)?$/i, '')}`;
  }
  return null;
}
