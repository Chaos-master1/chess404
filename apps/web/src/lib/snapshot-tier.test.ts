import { describe, expect, it } from 'vitest';
import { clampRunningClock, classifySnapshotTier, filterUnseenEventIds } from './snapshot-tier';

describe('classifySnapshotTier', () => {
  it('treats seq-less snapshots as fresh (bootstraps, legacy payloads)', () => {
    expect(classifySnapshotTier(null, 5)).toBe('fresh');
    expect(classifySnapshotTier(undefined, 5)).toBe('fresh');
    expect(classifySnapshotTier(0, 5)).toBe('fresh');
  });

  it('drops strictly older snapshots (the bounce: pre-move tick after the move)', () => {
    expect(classifySnapshotTier(4, 5)).toBe('stale');
    expect(classifySnapshotTier(1, 900)).toBe('stale');
  });

  it('classifies equal seq as cosmetic-only (per-second clock tick)', () => {
    expect(classifySnapshotTier(5, 5)).toBe('cosmetic');
    expect(classifySnapshotTier(42, 42)).toBe('cosmetic');
  });

  it('applies strictly newer snapshots fully', () => {
    expect(classifySnapshotTier(6, 5)).toBe('fresh');
    expect(classifySnapshotTier(1, 0)).toBe('fresh');
  });
});

describe('clampRunningClock', () => {
  // Live bug: the HTTP response for your own move still carried the
  // opponent's clock uncharged at your move's timestamp (same seqNum as the
  // per-second tick that had already charged it). If the tick's WS frame
  // landed first, the late response re-applied the higher uncharged value and
  // the visible clock ran down, then jumped back up.
  it('rejects a higher value for a running clock (out-of-order older build)', () => {
    const displayed = 295_000; // already interpolated ~5s down from 300s
    expect(clampRunningClock(300_000, displayed)).toBe(displayed);
  });

  it('accepts genuinely newer (lower) charged values', () => {
    expect(clampRunningClock(294_200, 295_000)).toBe(294_200);
  });

  it('keeps the display on an equal value', () => {
    expect(clampRunningClock(295_000, 295_000)).toBe(295_000);
  });
});

describe('filterUnseenEventIds', () => {
  it('passes through events without an ID', () => {
    const seen = new Set<string>();
    const events = [{ id: undefined, type: 'x' }, { type: 'y' }] as any[];
    expect(filterUnseenEventIds(events, seen)).toHaveLength(2);
    expect(seen.size).toBe(0);
  });

  it('skips already-seen IDs and records new ones', () => {
    const seen = new Set(['e1']);
    const events = [{ id: 'e1' }, { id: 'e2' }] as any[];
    const fresh = filterUnseenEventIds(events, seen);
    expect(fresh.map((e) => e.id)).toEqual(['e2']);
    expect(seen.has('e2')).toBe(true);
  });

  it('trims the seen-window so long matches cannot grow it unboundedly', () => {
    const seen = new Set<string>();
    for (let i = 0; i < 600; i++) {
      filterUnseenEventIds([{ id: `e${i}` } as any], seen);
    }
    expect(seen.size).toBeLessThanOrEqual(500);
    expect(seen.has('e599')).toBe(true);
  });
});
