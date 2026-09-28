package platform

import (
	"testing"

	"github.com/chess404/realtime/internal/contracts"
)

// The post-placement K-factor decay curve: fast convergence for new players,
// a stable, meaningful rating for established ones. These boundaries are the
// product contract -- changing them changes how fast everyone's rating moves.
func TestEloKFactorForGamesDecayBoundaries(t *testing.T) {
	cases := []struct {
		games int
		want  float64
	}{
		{0, defaultKFactorNovice},
		{5, defaultKFactorNovice},
		{29, defaultKFactorNovice},
		{30, defaultKFactorTours},
		{60, defaultKFactorTours},
		{119, defaultKFactorTours},
		{120, defaultKFactorEstablished},
		{500, defaultKFactorEstablished},
	}
	for _, tc := range cases {
		if got := eloKFactorForGames(tc.games); got != tc.want {
			t.Fatalf("eloKFactorForGames(%d) = %v, want %v", tc.games, got, tc.want)
		}
	}
}

// End-to-end through the account store: an established player (500 games) and
// a brand-new player (0 games) with equal ratings -- the novice's rating must
// move MORE than the established player's. That asymmetry is the whole point
// of the decay curve.
func TestEloDecayAppliedToEndToEndFinalization(t *testing.T) {
	store, err := NewAccountStore("")
	if err != nil {
		t.Fatalf("account store init: %v", err)
	}
	defer func() { _ = store.Close() }()

	// ClaimGuest does not seed stats; SyncGuestStats carries the guest's
	// MatchesPlayed into the account (the same flow production uses).
	whiteSession, err := store.ClaimGuest(GuestProfile{GuestID: "decay_white", Rating: 1200, MatchesPlayed: 500}, "decay_white")
	if err != nil {
		t.Fatalf("claim white: %v", err)
	}
	if _, _, err := store.SyncGuestStats(GuestProfile{GuestID: "decay_white", Rating: 1200, MatchesPlayed: 500}); err != nil {
		t.Fatalf("sync white: %v", err)
	}
	// 6 games: past placements (the seed helper only defaults 0-game guests
	// into placements) but still in the novice K band.
	blackSession, err := store.ClaimGuest(GuestProfile{GuestID: "decay_black", Rating: 1200, MatchesPlayed: 6}, "decay_black")
	if err != nil {
		t.Fatalf("claim black: %v", err)
	}
	if _, _, err := store.SyncGuestStats(GuestProfile{GuestID: "decay_black", Rating: 1200, MatchesPlayed: 6}); err != nil {
		t.Fatalf("sync black: %v", err)
	}

	white, black, changed, err := store.FinalizeMatch("decay_match_1", whiteSession.Account.AccountID, blackSession.Account.AccountID, "white", "rated", contracts.MatchModeOpenCards)
	if err != nil || !changed {
		t.Fatalf("finalize: %v changed=%v", err, changed)
	}

	whiteDelta := white.Rating - 1200
	blackDelta := 1200 - black.Rating
	if whiteDelta != 8 { // K=16, even fight: 16 * (1-0.5) = 8
		t.Fatalf("established player delta = %d, want 8 (K=16)", whiteDelta)
	}
	if blackDelta != 20 { // K=40: 40 * 0.5 = 20
		t.Fatalf("novice player delta = %d, want 20 (K=40)", blackDelta)
	}
}
