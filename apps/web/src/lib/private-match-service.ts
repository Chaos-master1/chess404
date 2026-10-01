import type { MatchModeId, MatchSnapshotMessage, PieceColor } from '@chess404/contracts';
import { DEFAULT_MATCH_MODE_ID } from '@chess404/contracts';
import type { GuestSession, MatchSeatClaim } from './platform-service';
import { clearStoredGuestIdentity, writeStoredGuestIdentity } from './session-storage';

export interface PrivateMatchIdentity {
  guestId?: string;
  sessionSecret?: string;
  sessionToken?: string;
  accountId?: string;
  accountSessionToken?: string;
}

export interface PrivateMatchAccessResponse {
  matchId: string;
  seatColor: PieceColor;
  waitingForOpponent: boolean;
  snapshot: MatchSnapshotMessage;
  claim?: MatchSeatClaim;
  // The gateway may mint a replacement guest session when the browser submits
  // an expired one. Persisting this resolved identity prevents the new room
  // from being created for a guest the browser can no longer authenticate as.
  guestSession?: GuestSession;
}

// Shared POST helper for the three private-match access calls. The platform
// refuses stale/expired stored credentials with 401; when the caller supplied
// credentials that were rejected, clear the poisoned local identity and retry
// once as a fresh visitor so returning players are re-minted a guest instead
// of being locked out of the room (and so the retry cannot mint a phantom
// second identity for someone already holding a valid session).
async function fetchPrivateMatchAccess(
  path: string,
  payload: Record<string, unknown>,
  identity: PrivateMatchIdentity,
): Promise<Response> {
  const send = (ident: PrivateMatchIdentity) => fetch(path, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      ...payload,
      guest: {
        guestId: ident.guestId,
        sessionSecret: ident.sessionSecret,
        sessionToken: ident.sessionToken,
      },
      account: ident.accountId ? {
        accountId: ident.accountId,
        sessionToken: ident.accountSessionToken,
      } : undefined,
    }),
  });

  let response = await send(identity);
  const suppliedCredentials = !!(identity.guestId || identity.sessionSecret || identity.sessionToken);
  if (response.status === 401 && suppliedCredentials) {
    clearStoredGuestIdentity('white');
    response = await send({});
  }
  return response;
}

export async function createPrivateMatch(input: {
  identity: PrivateMatchIdentity;
  queue?: 'direct' | 'casual' | 'rated';
  modeId?: MatchModeId;
  clockSeconds?: number;
  preferredSeat?: PieceColor;
  difficulty?: string;
}): Promise<PrivateMatchAccessResponse> {
  const response = await fetchPrivateMatchAccess('/api/gateway/private-matches', {
    queue: input.queue ?? 'direct',
    modeId: input.modeId ?? DEFAULT_MATCH_MODE_ID,
    difficulty: input.difficulty ?? '',
    clockSeconds: input.clockSeconds ?? 600,
    preferredSeat: input.preferredSeat ?? 'white',
  }, input.identity);

  return persistResolvedGuestSession(await unwrapResponse<PrivateMatchAccessResponse>(response));
}

export async function joinPrivateMatch(input: {
  matchId: string;
  identity: PrivateMatchIdentity;
  preferredSeat?: PieceColor;
}): Promise<PrivateMatchAccessResponse> {
  const response = await fetchPrivateMatchAccess(
    `/api/gateway/private-matches/${encodeURIComponent(input.matchId)}/join`,
    { preferredSeat: input.preferredSeat },
    input.identity,
  );

  return persistResolvedGuestSession(await unwrapResponse<PrivateMatchAccessResponse>(response));
}

export async function rematchPrivateMatch(input: {
  matchId: string;
  identity: PrivateMatchIdentity;
  clockSeconds?: number;
  difficulty?: string;
}): Promise<PrivateMatchAccessResponse> {
  const response = await fetchPrivateMatchAccess(
    `/api/gateway/private-matches/${encodeURIComponent(input.matchId)}/rematch`,
    { clockSeconds: input.clockSeconds ?? 600, difficulty: input.difficulty ?? '' },
    input.identity,
  );

  return persistResolvedGuestSession(await unwrapResponse<PrivateMatchAccessResponse>(response));
}

function persistResolvedGuestSession(result: PrivateMatchAccessResponse): PrivateMatchAccessResponse {
  const session = result.guestSession;
  if (!session?.guest?.guestId || !session.sessionSecret) return result;

  // Private-room entry points are driven by the browser's primary guest
  // identity.  Seat colour describes the room assignment, not a second local
  // account, so a joiner assigned Black still persists this as its White slot.
  writeStoredGuestIdentity('white', session.guest.guestId, session.sessionSecret, {
    sessionToken: session.sessionToken ?? null,
    sessionExpiresAt: session.expiresAt ?? null,
  });
  return result;
}

// Callers key retry/UX decisions on the HTTP status (e.g. the match facade
// separates "gone room" from "network down" via err.status). Without the
// status a definitive 404 looked identical to an offline blip.
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
      // Keep fallback message.
    }
    if (response.status === 429) {
      const header = response.headers.get('Retry-After');
      throw withStatus(new Error(`${message} (rate limited, retry after ${header ?? 'unknown'}s)`), response.status);
    }
    throw withStatus(new Error(message), response.status);
  }

  return response.json() as Promise<T>;
}
