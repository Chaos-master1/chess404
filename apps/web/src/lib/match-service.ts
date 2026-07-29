import type { MatchModeId, MatchPresenceRequest, MatchSnapshotMessage, PlayerIntent } from '@chess404/contracts';
import { DEFAULT_MATCH_MODE_ID } from '@chess404/contracts';
import { readStoredGuestIdentity } from './session-storage';

const gatewayBaseUrl = '/api/gateway';
let httpBaseUrl = '/api/realtime';
let wsBaseUrl = '';
const MATCH_POLL_INTERVAL_MS = 750;
const MATCH_POLL_RETRY_INTERVAL_MS = 900;

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

export async function fetchMatch(matchId: string, signal?: AbortSignal): Promise<MatchSnapshotMessage> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 15000);
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
  window.localStorage.setItem(key, JSON.stringify({
    ...meta,
    modeId: meta.modeId ?? DEFAULT_MATCH_MODE_ID,
  }));
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
      try {
        const snapshot = await fetchMatch(matchId);
        if (!disposed) {
          if (snapshot.seqNum) recordMatchSeqNum(matchId, snapshot.seqNum);
          handlers.onSnapshot(snapshot);
          handlers.onStatusChange?.('connected');
        }
      } catch {
        if (!disposed) {
          handlers.onStatusChange?.('reconnecting');
        }
      } finally {
        schedulePoll();
      }
    }, delay);
  };

  const maxReconnectAttempts = 10;
  const scheduleReconnect = () => {
    if (disposed) {
      return;
    }
    if (reconnectAttempt >= maxReconnectAttempts) {
      console.warn('max reconnect attempts reached, falling back to polling');
      handlers.onStatusChange?.('connected');
      schedulePoll(0);
      return;
    }
    if (typeof navigator !== 'undefined' && !navigator.onLine) {
      console.log('offline — waiting for network before reconnecting');
      handlers.onStatusChange?.('reconnecting');
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
    if (playerIdentity?.playerClaimToken?.trim()) {
      authPromise = Promise.resolve({ claimToken: playerIdentity.playerClaimToken!.trim() });
    } else if (playerIdentity?.playerId?.trim() && playerIdentity?.playerSecret?.trim()) {
      authPromise = fetchAuthToken(matchId, playerIdentity.playerId.trim(), playerIdentity.playerSecret.trim())
        .then(token => ({ claimToken: token }));
    } else {
      console.log('Spectate mode: no player identity — using polling');
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
            handlers.onStatusChange?.('connected');
            return;
          }
          if (msg.type === 'auth.error') {
            nextSocket.close();
            handlers.onStatusChange?.('disconnected');
            return;
          }
          if (!authReceived) return;
          if (msg.type === 'match.snapshot' && msg.payload) {
            const snapshot = msg.payload;
            if (snapshot.seqNum && lastSeqNum > 0 && snapshot.seqNum > lastSeqNum + 1) {
              console.warn(`seqNum gap detected: ${lastSeqNum} -> ${snapshot.seqNum}, refetching`);
              fetchMatch(matchId).then(fullSnapshot => {
                if (!disposed) handlers.onSnapshot(fullSnapshot);
              }).catch(() => {});
            }
            if (snapshot.seqNum) {
              lastSeqNum = snapshot.seqNum;
              recordMatchSeqNum(matchId, snapshot.seqNum);
            }
            handlers.onSnapshot(snapshot);
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
        if (!disposed) scheduleReconnect();
      });
    }).catch(() => {
      if (!disposed) schedulePoll(0);
    });
  };

  connect();

  const manualRetry = () => {
    if (disposed) return;
    reconnectAttempt = 0;
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
      handlers.onStatusChange?.('disconnected');
      if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
        socket.close();
      }
    },
    retry: manualRetry,
  };
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
      throw new Error(`${message} (rate limited, retry after ${header ?? 'unknown'}s)`);
    }
    throw new Error(message);
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
