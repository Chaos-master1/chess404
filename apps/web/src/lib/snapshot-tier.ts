// Shared classification for authoritative snapshot ordering (bounce fix).
//
// Snapshots arrive over multiple transport paths (WS broadcast, HTTP intent
// response, poll) and can arrive out of order: the once-a-second clock tick
// generated BEFORE a move can be delivered AFTER the move's broadcast, and an
// intent response races its own WS frame. Applying every snapshot
// unconditionally repainted the pre-move board -- the visible
// "piece snaps back, then the move plays" bounce.
//
// Tiers:
//   - "stale": strictly older than the newest applied seq -> drop entirely.
//   - "cosmetic": EQUAL to the newest applied seq. This is the once-a-second
//     clock tick, which deliberately does not bump the seq (seqnum_tick
//     no-bump fix). It may update clocks and terminal state, but never board,
//     turn, hands, pending cards or identity -- and a naive `<=` guard here
//     would freeze every visible clock.
//   - "fresh": strictly newer, or seq-less (older snapshots/bootstraps) ->
//     full apply.
export type SnapshotTier = 'stale' | 'cosmetic' | 'fresh';

export function classifySnapshotTier(
  incomingSeqNum: number | null | undefined,
  lastAppliedSeqNum: number,
): SnapshotTier {
  const seq = typeof incomingSeqNum === 'number' && incomingSeqNum > 0 ? incomingSeqNum : 0;
  if (seq === 0) return 'fresh';
  if (seq < lastAppliedSeqNum) return 'stale';
  if (seq === lastAppliedSeqNum) return 'cosmetic';
  return 'fresh';
}

// Sliding-window event de-dup by stable event ID. Join and intent responses
// replay the tail of the event log, so without this a resync re-fired draw
// banners and sounds for already-applied events. IDs are trusted from the
// server; events without an ID are passed through.
export function filterUnseenEventIds(
  events: { id?: string }[] | undefined,
  seen: Set<string>,
): { id?: string }[] {
  const fresh: { id?: string }[] = [];
  for (const ev of events ?? []) {
    if (!ev?.id) {
      fresh.push(ev);
      continue;
    }
    if (seen.has(ev.id)) continue;
    seen.add(ev.id);
    fresh.push(ev);
  }
  if (seen.size > 500) {
    for (const id of Array.from(seen).slice(0, seen.size - 200)) seen.delete(id);
  }
  return fresh;
}
