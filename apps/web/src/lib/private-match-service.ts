import type { MatchModeId, MatchSnapshotMessage, PieceColor } from '@chess404/contracts';
import { DEFAULT_MATCH_MODE_ID } from '@chess404/contracts';
import type { GuestSession, MatchSeatClaim } from './platform-service';
import { writeStoredGuestIdentity } from './session-storage';

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

export async function createPrivateMatch(input: {
  identity: PrivateMatchIdentity;
  queue?: 'direct' | 'casual' | 'rated';
  modeId?: MatchModeId;
  clockSeconds?: number;
  preferredSeat?: PieceColor;
  difficulty?: string;
}): Promise<PrivateMatchAccessResponse> {
  const response = await fetch('/api/gateway/private-matches', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      guest: {
        guestId: input.identity.guestId,
        sessionSecret: input.identity.sessionSecret,
        sessionToken: input.identity.sessionToken,
      },
      account: input.identity.accountId ? {
        accountId: input.identity.accountId,
        sessionToken: input.identity.accountSessionToken,
      } : undefined,
      queue: input.queue ?? 'direct',
      modeId: input.modeId ?? DEFAULT_MATCH_MODE_ID,
      difficulty: input.difficulty ?? '',
      clockSeconds: input.clockSeconds ?? 600,
      preferredSeat: input.preferredSeat ?? 'white',
    }),
  });

  return persistResolvedGuestSession(await unwrapResponse<PrivateMatchAccessResponse>(response));
}

export async function joinPrivateMatch(input: {
  matchId: string;
  identity: PrivateMatchIdentity;
  preferredSeat?: PieceColor;
}): Promise<PrivateMatchAccessResponse> {
  const response = await fetch(`/api/gateway/private-matches/${encodeURIComponent(input.matchId)}/join`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      guest: {
        guestId: input.identity.guestId,
        sessionSecret: input.identity.sessionSecret,
        sessionToken: input.identity.sessionToken,
      },
      account: input.identity.accountId ? {
        accountId: input.identity.accountId,
        sessionToken: input.identity.accountSessionToken,
      } : undefined,
      preferredSeat: input.preferredSeat,
    }),
  });

  return persistResolvedGuestSession(await unwrapResponse<PrivateMatchAccessResponse>(response));
}

export async function rematchPrivateMatch(input: {
  matchId: string;
  identity: PrivateMatchIdentity;
  clockSeconds?: number;
}): Promise<PrivateMatchAccessResponse> {
  const response = await fetch(`/api/gateway/private-matches/${encodeURIComponent(input.matchId)}/rematch`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      guest: {
        guestId: input.identity.guestId,
        sessionSecret: input.identity.sessionSecret,
        sessionToken: input.identity.sessionToken,
      },
      account: input.identity.accountId ? {
        accountId: input.identity.accountId,
        sessionToken: input.identity.accountSessionToken,
      } : undefined,
      clockSeconds: input.clockSeconds ?? 600,
    }),
  });

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
      throw new Error(`${message} (rate limited, retry after ${header ?? 'unknown'}s)`);
    }
    throw new Error(message);
  }

  return response.json() as Promise<T>;
}
