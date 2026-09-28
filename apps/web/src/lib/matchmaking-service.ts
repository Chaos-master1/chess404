import type { MatchModeId } from '@chess404/contracts';
import { DEFAULT_MATCH_MODE_ID } from '@chess404/contracts';

export type QueueName = 'casual' | 'rated';
export type TicketStatus = 'queued' | 'matched' | 'cancelled';

export interface QueueTicket {
  ticketId: string;
  guestId: string;
  displayName?: string;
  queue: QueueName;
  modeId?: MatchModeId;
  status: TicketStatus;
  rating: number;
  /** Normalized time control this seek was created with (pairing is same-clock only). */
  clockSeconds?: number;
  clockIncrement?: number;
  createdAt: string;
  updatedAt: string;
  matchedAt?: string;
  matchedWith?: string;
  seatColor?: 'white' | 'black';
  opponentName?: string;
  assignedRoom?: string;
  /** Only ever present on the POST /tickets create response for the enqueuing client. */
  cancelSecret?: string;
}

export interface QueueSnapshot {
  queue: QueueName;
  modeId?: MatchModeId;
  queuedCount: number;
  matchedCount: number;
  cancelledCount: number;
}

export interface QueueSnapshotResponse {
  snapshots: QueueSnapshot[];
  checkedAt?: string;
}

export interface EnqueueGuestAccountIdentity {
  accountId?: string;
  accountSessionToken?: string;
}

function cancelSecretStorageKey(ticketId: string): string {
  return `chess404.queue.cxl.${ticketId}`;
}

export interface EnqueueGuestClock {
  seconds: number;
  increment: number;
}

export async function enqueueGuest(
  guestId: string,
  queue: QueueName,
  modeId: MatchModeId,
  rating: number,
  displayName?: string,
  accountIdentity: EnqueueGuestAccountIdentity = {},
  clock?: EnqueueGuestClock,
): Promise<{ ticket: QueueTicket; snapshot: QueueSnapshot }> {
  const response = await fetch('/api/matchmaking/queues/tickets', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      guestId,
      queue,
      modeId,
      rating,
      displayName,
      accountId: accountIdentity.accountId,
      accountSessionToken: accountIdentity.accountSessionToken,
      clockSeconds: clock?.seconds,
      clockIncrement: clock?.increment,
    }),
  });
  const result = await unwrapResponse<{ ticket: QueueTicket; snapshot: QueueSnapshot }>(response);
  // The server issues a per-ticket cancel secret exactly once, on this
  // create response. Persist it keyed by ticket: DELETE later requires it.
  if (typeof window !== 'undefined' && result.ticket?.cancelSecret) {
    try {
      window.localStorage.setItem(cancelSecretStorageKey(result.ticket.ticketId), result.ticket.cancelSecret);
    } catch {
      // localStorage unavailable (private mode) -- cancel just won't carry
      // the secret and the server will refuse; join still works.
    }
  }
  return result;
}

export async function fetchQueueTickets(queue: QueueName, modeId: MatchModeId = DEFAULT_MATCH_MODE_ID): Promise<QueueTicket[]> {
  const params = new URLSearchParams({ queue, modeId });
  const response = await fetch(`/api/matchmaking/queues/tickets?${params.toString()}`, {
    method: 'GET',
    headers: {
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
  });
  const payload = await unwrapResponse<{ tickets?: QueueTicket[] }>(response);
  return payload.tickets ?? [];
}

export async function fetchTicket(ticketId: string): Promise<{ ticket: QueueTicket; snapshot: QueueSnapshot }> {
  const response = await fetch(`/api/matchmaking/queues/tickets/${ticketId}`, {
    method: 'GET',
    headers: {
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
  });
  return unwrapResponse(response);
}

export async function cancelTicket(ticketId: string): Promise<{ ticket: QueueTicket; snapshot: QueueSnapshot }> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };
  // The server refuses cancels without the ticket's cancel secret (the web
  // proxy injects its own service token, which only authorizes internal
  // callers, so the secret is what proves end-user ownership here).
  if (typeof window !== 'undefined') {
    const secret = window.localStorage.getItem(cancelSecretStorageKey(ticketId));
    if (secret) {
      headers['X-Chess404-Ticket-Secret'] = secret;
    }
  }
  const response = await fetch(`/api/matchmaking/queues/tickets/${ticketId}`, {
    method: 'DELETE',
    headers,
  });
  return unwrapResponse(response);
}

export class RateLimitError extends Error {
  retryAfter: number;
  constructor(message: string, retryAfter: number) {
    super(message);
    this.name = 'RateLimitError';
    this.retryAfter = retryAfter;
  }
}

async function unwrapResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let message = `Request failed with ${response.status}`;
    let retryAfter = 0;
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
      if (header) retryAfter = parseInt(header, 10) || 1;
      throw new RateLimitError(message, retryAfter || 1);
    }
    throw new Error(message);
  }
  return response.json() as Promise<T>;
}

export async function fetchQueueSnapshots(queue?: QueueName, modeId?: MatchModeId): Promise<QueueSnapshotResponse> {
  const params = new URLSearchParams();
  if (queue) {
    params.set('queue', queue);
  }
  if (modeId) {
    params.set('modeId', modeId);
  }
  const suffix = params.size > 0 ? `?${params.toString()}` : '';
  const response = await fetch(`/api/matchmaking/queues/snapshots${suffix}`, {
    method: 'GET',
    headers: {
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
  });
  const payload = await unwrapResponse<QueueSnapshotResponse>(response);
  return {
    snapshots: payload.snapshots ?? [],
    checkedAt: payload.checkedAt,
  };
}
