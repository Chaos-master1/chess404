// Seat identity policy (shared by the match nav hook and the seat cards).
//
// The invite room is created BEFORE the opponent exists, so at that moment
// there is no name and no rating to show. The old UI filled that hole with
// junk: "Anonymous" and a default 1200 rating. The policy here is:
//
// - Empty seat in a waiting room  -> "Waiting for opponent", no rating.
// - Account-backed seat           -> @handle + the account's rating for the
//                                    match's mode (never a borrowed number).
// - Seat with a real chosen name  -> that name, no rating. Queue-paired
//                                    account players may lack accountId in
//                                    the snapshot, but the server renames
//                                    their linked guest to the handle, so
//                                    the name IS their identity.
// - Pure guest seat               -> "Guest", no rating. Generated guest
//                                    display names ("Ivory Bishop 101") and
//                                    legacy 1200 defaults are never shown,
//                                    matching the formatPlayerLabel policy in
//                                    lib/display.ts (only account handles are
//                                    real identity).
// - The viewer's own seat         -> local profile name/rating as before.
// - Computer seats                -> "Computer <difficulty>" + its rating.

export interface MatchSeatMeta {
  whiteGuestId?: string;
  blackGuestId?: string;
  whiteAccountId?: string;
  blackAccountId?: string;
  whiteName?: string;
  blackName?: string;
}

export const SEAT_WAITING_LABEL = 'Waiting for opponent';

export interface SeatIdentityInput {
  /** Seat has no guest yet while the room status is "waiting". */
  waiting: boolean;
  /** This browser owns the seat (shows the local profile, not server meta). */
  isViewer: boolean;
  viewerProfileName?: string | null;
  viewerRating?: number | null;
  /** Seat display name from the server snapshot (guest names may be generated). */
  seatName?: string | null;
  /** Resolved public account for the seat, when the seat is account-backed. */
  accountHandle?: string | null;
  accountRating?: number | null;
  /** Computer-mode override ("Computer Medium" etc.) supplied by the nav hook. */
  computerSeat?: { name: string; rating: number } | null;
}

export interface SeatIdentity {
  waiting: boolean;
  name: string;
  /** null = hide the rating line entirely (waiting / guest / unknown). */
  rating: number | null;
}

export function resolveSeatIdentity(input: SeatIdentityInput): SeatIdentity {
  if (input.waiting) {
    return { waiting: true, name: SEAT_WAITING_LABEL, rating: null };
  }
  if (input.computerSeat) {
    return { waiting: false, name: input.computerSeat.name, rating: input.computerSeat.rating };
  }
  if (input.isViewer) {
    // Same placeholder rule as the opponent seat: a stale generated name
    // burned into the snapshot must not override the viewer's real profile.
    const profileName = input.viewerProfileName?.trim();
    const seatName = input.seatName?.trim();
    const name = profileName
      || (seatName && !isPlaceholderSeatName(seatName) ? seatName : null)
      || 'Anonymous';
    return { waiting: false, name, rating: input.viewerRating ?? null };
  }
  const handle = input.accountHandle?.trim();
  if (handle) {
    return { waiting: false, name: `@${handle}`, rating: input.accountRating ?? null };
  }
  // No account lookup available: a non-generated seat name is still a real
  // identity (server renamed account-linked guests to their handle, or the
  // player chose a custom name). Generated/placeholder names are not.
  const seatName = input.seatName?.trim();
  if (seatName && !isPlaceholderSeatName(seatName)) {
    return { waiting: false, name: seatName, rating: null };
  }
  return { waiting: false, name: 'Guest', rating: null };
}

const generatedSeatNamePattern = /^[A-Z][a-z]+ [A-Z][a-z]+ \d{1,4}$/;
export function isPlaceholderSeatName(name: string): boolean {
  // "Ivory Bishop 101"-style generated guest names and "Guest <id>"
  // bootstrap placeholders are not identities.
  return generatedSeatNamePattern.test(name) || /^Guest /.test(name);
}

/**
 * Rating to display for an ACCOUNT in the match's mode: the mode's own ladder
 * when it has rated games, falling back to the blended account rating (same
 * fallback rule as guestRatingForMode for guests). Empty ladder + no blended
 * rating -> null (hide, never invent).
 */
export function accountRatingForMode(
  profile: { rating?: number; openCards?: { rating: number; matchesPlayed: number }; hiddenCards?: { rating: number; matchesPlayed: number } } | null | undefined,
  modeId?: string | null,
): number | null {
  if (!profile) {
    return null;
  }
  if (modeId === 'open_cards' && profile.openCards && profile.openCards.matchesPlayed > 0) {
    return profile.openCards.rating;
  }
  if (modeId === 'hidden_cards' && profile.hiddenCards && profile.hiddenCards.matchesPlayed > 0) {
    return profile.hiddenCards.rating;
  }
  return profile.rating ?? null;
}
