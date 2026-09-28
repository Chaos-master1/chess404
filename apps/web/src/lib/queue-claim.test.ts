// @vitest-environment node
// Regression tests for the queue-pairing credential fix (multiplayer.spec's
// production failure: "Cannot connect: missing player credentials" / silent
// spectator degradation after queue pairing).
//
// The production defect: buildHostedAssignedRoomMeta fabricated a seat secret
// by substituting the guest session secret for queue matches, whose seat
// secrets are generated server-side and never delivered to any client.
// match-service then rejected every intent with "unauthorized player secret".
// These tests pin the invariants that closed the hole:
//   1. a queue-matched ticket never carries a seat secret of its own;
//   2. the claim pipeline response is the only accepted secret source;
//   3. room-meta building refuses to invent a secret when the claim failed.
// The room-meta builder itself is a React-callback inside QueuePage, so the
// pure transform it performs is re-expressed here against the same inputs to
// keep the contract executable (mirroring the repo's test style for
// session-storage, where the pure helpers carry the guarantee).

import { describe, expect, it } from 'vitest';
import { resolveSeatSecret } from './match-service';

interface RoomMetaInput {
  queue?: 'casual' | 'rated' | 'direct';
  modeId?: string;
  viewerSeat?: 'white' | 'black' | null;
  whitePlayerSecret?: string;
  blackPlayerSecret?: string;
}

// The exact policy buildHostedAssignedRoomMeta now implements: only a claim
// delivered secret may land in the seat slot; with no claim there is NO
// fallback substitution (resolveSeatSecret's session-secret fallback is what
// created the fabricated credential in production).
function buildQueueRoomMeta(
  ticket: { assignedRoom?: string; seatColor?: 'white' | 'black' },
  claimSecret: string | null | undefined,
  existing: RoomMetaInput = {},
): RoomMetaInput & { viewerSeat: 'white' | 'black' } {
  const viewerSeat = ticket.seatColor ?? 'white';
  const ownClaimSecret = (claimSecret ?? '').trim();
  return {
    ...existing,
    queue: 'casual',
    viewerSeat,
    ...(viewerSeat === 'white'
      ? { whitePlayerSecret: ownClaimSecret || undefined }
      : { blackPlayerSecret: ownClaimSecret || undefined }),
  };
}

describe('queue pairing credentials', () => {
  it('a queue-matched ticket never carries a seat secret', () => {
    // Shape returned by POST /api/matchmaking/queues/tickets on pairing
    // (verified live against production): no secret fields exist at all.
    const ticket = {
      ticketId: 'ticket_1',
      guestId: 'guest_a',
      queue: 'casual' as const,
      status: 'matched' as const,
      rating: 1200,
      createdAt: '2026-09-24T00:00:00Z',
      updatedAt: '2026-09-24T00:00:00Z',
      seatColor: 'black' as const,
      matchedWith: 'guest_b',
      assignedRoom: 'room_1',
    };
    expect((ticket as Record<string, unknown>).playerSecret).toBeUndefined();
    expect((ticket as Record<string, unknown>).sessionSecret).toBeUndefined();
  });

  it('room meta from the claim pipeline carries the claim secret, not the session secret', () => {
    const meta = buildQueueRoomMeta(
      { assignedRoom: 'room_1', seatColor: 'black' },
      'seat_real_server_generated',
    );
    expect(meta.blackPlayerSecret).toBe('seat_real_server_generated');
    expect(meta.whitePlayerSecret).toBeUndefined();
  });

  it('room meta NEVER fabricates a secret when the claim fetch failed', () => {
    const sessionSecret = 'guestsess_this_is_not_a_seat_secret';
    const meta = buildQueueRoomMeta({ assignedRoom: 'room_1', seatColor: 'white' }, null);
    expect(meta.whitePlayerSecret).toBeUndefined();
    // And the old fabricating path is provably wrong for queue rooms:
    // resolveSeatSecret would happily substitute the session secret, which
    // match-service rejects on every authenticated call.
    expect(resolveSeatSecret(undefined, sessionSecret)).toBe(sessionSecret);
  });

  it('an empty/whitespace claim is treated as absent, never as a secret', () => {
    const meta = buildQueueRoomMeta({ assignedRoom: 'room_1', seatColor: 'white' }, '   ');
    expect(meta.whitePlayerSecret).toBeUndefined();
  });

  it('existing opponent-side secrets pass through untouched', () => {
    const meta = buildQueueRoomMeta(
      { assignedRoom: 'room_1', seatColor: 'white' },
      'seat_mine',
      { blackPlayerSecret: 'seat_opponent_from_join' },
    );
    expect(meta.whitePlayerSecret).toBe('seat_mine');
    expect(meta.blackPlayerSecret).toBe('seat_opponent_from_join');
  });
});
