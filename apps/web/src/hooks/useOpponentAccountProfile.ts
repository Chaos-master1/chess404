'use client';

import React from 'react';
import { fetchAccount, type AccountProfile } from '../lib/platform-service';

/**
 * Resolves the public account profile for a match seat's account ID.
 *
 * Match snapshots carry only `whiteAccountId`/`blackAccountId` — no handle,
 * no rating. This fetches the public profile once per account and caches the
 * result for the page session, so the seat card can show a real @handle and
 * rating the moment the opponent joins, without any reload.
 *
 * Guest seats have no accountId: the hook returns null and the seat card
 * renders "Guest" with no rating (see lib/seat-identity.ts).
 */
export function useOpponentAccountProfile(accountId: string | null | undefined): AccountProfile | null {
  const [profile, setProfile] = React.useState<AccountProfile | null>(null);

  // Page-session cache keyed by accountId: a rematch against the same player
  // and a rematch flip reuse the first fetch instead of hammering the public
  // endpoint on every snapshot.
  const cacheRef = React.useRef<Map<string, AccountProfile>>(new Map());
  const inflightRef = React.useRef<Map<string, Promise<AccountProfile | null>>>(new Map());

  React.useEffect(() => {
    const id = accountId?.trim();
    if (!id) {
      setProfile(null);
      return;
    }
    const cached = cacheRef.current.get(id);
    if (cached) {
      setProfile(cached);
      return;
    }
    setProfile(null);
    let cancelled = false;
    let inflight = inflightRef.current.get(id);
    if (!inflight) {
      inflight = fetchAccount(id)
        .then((resolved) => {
          cacheRef.current.set(id, resolved);
          inflightRef.current.delete(id);
          return resolved;
        })
        .catch(() => {
          // Public profile lookup failed (offline blip, restriction): the seat
          // degrades to name-only. Retry on the next accountId change/mount.
          inflightRef.current.delete(id);
          return null;
        });
      inflightRef.current.set(id, inflight);
    }
    void inflight.then((resolved) => {
      if (!cancelled && resolved) {
        setProfile(resolved);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [accountId]);

  return profile;
}
