import { describe, expect, it } from 'vitest';
import {
  SEAT_WAITING_LABEL,
  accountRatingForMode,
  resolveSeatIdentity,
} from './seat-identity';

describe('resolveSeatIdentity', () => {
  it('renders an honest waiting state with no rating for an empty seat', () => {
    const seat = resolveSeatIdentity({ waiting: true, isViewer: false });
    expect(seat).toEqual({ waiting: true, name: SEAT_WAITING_LABEL, rating: null });
  });

  it('prefers the account handle and mode rating for an account-backed opponent', () => {
    const seat = resolveSeatIdentity({
      waiting: false,
      isViewer: false,
      seatName: 'lazy-to-move',
      accountHandle: 'lazy-to-move',
      accountRating: 1216,
    });
    expect(seat).toEqual({ waiting: false, name: '@lazy-to-move', rating: 1216 });
  });

  it('shows Guest with no rating for a pure-guest opponent (never a generated name)', () => {
    const seat = resolveSeatIdentity({
      waiting: false,
      isViewer: false,
      seatName: 'Ivory Bishop 101',
      accountHandle: null,
    });
    expect(seat).toEqual({ waiting: false, name: 'Guest', rating: null });
  });

  it('hides "Guest <id>" bootstrap placeholders too', () => {
    const seat = resolveSeatIdentity({
      waiting: false,
      isViewer: false,
      seatName: 'Guest guest_314ab6d2',
    });
    expect(seat).toEqual({ waiting: false, name: 'Guest', rating: null });
  });

  it('trusts a real chosen seat name when no account lookup exists (queue-paired handle)', () => {
    const seat = resolveSeatIdentity({
      waiting: false,
      isViewer: false,
      seatName: 'lazy-to-move',
    });
    expect(seat).toEqual({ waiting: false, name: 'lazy-to-move', rating: null });
  });

  it('keeps the viewer profile on the viewer seat', () => {
    const seat = resolveSeatIdentity({
      waiting: false,
      isViewer: true,
      viewerProfileName: 'houssemdrira',
      viewerRating: 1216,
      seatName: 'lazy-to-move',
    });
    expect(seat).toEqual({ waiting: false, name: 'houssemdrira', rating: 1216 });
  });

  it('keeps the computer label and its advertised rating', () => {
    const seat = resolveSeatIdentity({
      waiting: false,
      isViewer: false,
      computerSeat: { name: 'Computer Medium', rating: 1600 },
    });
    expect(seat).toEqual({ waiting: false, name: 'Computer Medium', rating: 1600 });
  });

  it('falls back to the viewer profile name when the viewer seat has no profile yet', () => {
    const seat = resolveSeatIdentity({
      waiting: false,
      isViewer: true,
      viewerProfileName: null,
      seatName: 'Guest guest_314ab6d2',
    });
    expect(seat).toEqual({ waiting: false, name: 'Anonymous', rating: null });
  });
});

describe('accountRatingForMode', () => {
  it('uses the mode ladder only when it has rated games', () => {
    const profile = {
      rating: 1300,
      openCards: { rating: 1500, matchesPlayed: 4 },
      hiddenCards: { rating: 1450, matchesPlayed: 0 },
    };
    expect(accountRatingForMode(profile, 'open_cards')).toBe(1500);
    expect(accountRatingForMode(profile, 'hidden_cards')).toBe(1300); // empty ladder -> blended
    expect(accountRatingForMode(profile, null)).toBe(1300);
  });

  it('hides the rating when the profile has none at all', () => {
    expect(accountRatingForMode(null, 'open_cards')).toBeNull();
    expect(accountRatingForMode({}, 'open_cards')).toBeNull();
  });
});
