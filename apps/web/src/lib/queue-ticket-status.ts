// Queue ticket wire statuses the matchmaking service can send.
//
// 'pairing' is the service's INTERNAL two-phase reservation state: the
// opponent was found and the room is being created cross-service, but the
// promotion to 'matched' has not happened yet. The service projects it away
// (PublicView), but any deploy skew -- or a regression there -- would hand it
// to clients raw, and historically that parked browsers forever: the queue
// stopped polling on any non-queued status and the auto-open effect only
// fires on 'matched', so a poll that landed inside the pairing window left
// the player on "Matched - opening game..." with no retry (production,
// 2026-10-03). Treat it as a still-active seek everywhere status is branched
// on, and the handoff self-heals regardless of server version.

export type QueueTicketStatus = 'queued' | 'pairing' | 'matched' | 'cancelled';

/** True while the ticket still belongs in the seeking/polling loop. */
export function isSeekingStatus(status: string | null | undefined): boolean {
  return status === 'queued' || status === 'pairing';
}

/**
 * Status as the queue card should present it: the raw internal reservation
 * state is rendered as plain 'queued' (same badge, same cancel affordance),
 * because the card promises not to expose raw internal ticket details.
 */
export function displayTicketStatus(status: string | null | undefined): string {
  if (status === 'pairing') {
    return 'queued';
  }
  return status ?? 'idle';
}
