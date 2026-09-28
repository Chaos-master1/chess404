package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// ResolveSeatSecret is the trusted internal path the platform match-claim
// pipeline and the gateway bootstrap use to hand a seated player their real
// credential. In computer mode the black seat belongs to the engine: its
// secret must never be resolvable. The HUMAN seat is a real seat -- refusing
// it broke the whole credential chain (claim secrets, WS auth tokens,
// presence heartbeats) for every computer match. The engine seat keeps its
// protection while the human seat resolves normally.
func TestResolveSeatSecretComputerModeAllowsHumanSeatOnly(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

	humanSecret := "human-computer-match-secret"
	service.CreateComputerMatch(contracts.CreateMatchRequest{
		MatchID:      "comp_resolve",
		ModeID:       contracts.MatchModeComputer,
		WhiteGuestID: "guest_human",
		// Mirror the real gateway create call: only the human seat's ID is
		// sent; CreateMatch stamps the other seat with the engine identity
		// and a server-generated engine secret.
		WhitePlayerSecret: humanSecret,
		ClockSeconds:      600,
	}, now)

	// The engine seat must never be claimable by anyone.
	if _, err := service.ResolveSeatSecret("comp_resolve", "computer"); err == nil {
		t.Fatal("engine seat secret must not be resolvable in computer mode")
	}

	// The human seat resolves -- this is the credential the client needs for
	// WS auth and presence.
	secret, err := service.ResolveSeatSecret("comp_resolve", "guest_human")
	if err != nil {
		t.Fatalf("human seat in a computer match must resolve: %v", err)
	}
	if secret != humanSecret {
		t.Fatalf("resolved secret mismatch: got %q want %q", secret, humanSecret)
	}

	// Unknown guests stay unauthorized.
	if _, err := service.ResolveSeatSecret("comp_resolve", "guest_stranger"); err == nil {
		t.Fatal("unrelated guest must not resolve a seat secret")
	}
}

// A human-vs-human match resolves both seats exactly as before the computer
// split (regression guard on the seat lookup itself).
func TestResolveSeatSecretStandardMatchResolvesBothSeats(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	newSecurityTestMatch(t, service, "std_resolve", now)

	for _, tc := range []struct {
		guest  string
		secret string
	}{
		{"guest_white", whiteTestSecret},
		{"guest_black", blackTestSecret},
	} {
		got, err := service.ResolveSeatSecret("std_resolve", tc.guest)
		if err != nil {
			t.Fatalf("seat %s should resolve: %v", tc.guest, err)
		}
		if got != tc.secret {
			t.Fatalf("seat %s secret mismatch: got %q want %q", tc.guest, got, tc.secret)
		}
	}
}
