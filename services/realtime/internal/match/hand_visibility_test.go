package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// Regression tests for hand-visibility filtering. A seated player must see
// HOW MANY cards the opponent holds (face-down stubs, as in the local game)
// but never WHICH cards; radar must genuinely reveal the opposing hand for
// its holder; a spectator (color "") must see no hands at all.

func handVisibilityTestState() contracts.MatchState {
	whiteCard := contracts.GameCard{ID: "w1", Name: "Freeze", Mechanic: "freeze", Type: "spell", Rarity: "common"}
	blackCardA := contracts.GameCard{ID: "b1", Name: "Shield", Mechanic: "shield", Type: "spell", Rarity: "rare"}
	blackCardB := contracts.GameCard{ID: "b2", Name: "Sniper", Mechanic: "sniper", Type: "spell", Rarity: "epic"}
	return contracts.MatchState{
		MatchID:           "handvis",
		Status:            "active",
		Turn:              "white",
		WhiteHand:         []contracts.GameCard{whiteCard},
		BlackHand:         []contracts.GameCard{blackCardA, blackCardB},
		WhitePlayerSecret: "white-secret",
		BlackPlayerSecret: "black-secret",
	}
}

func TestFilterStateForColorStubsOpponentHand(t *testing.T) {
	state := handVisibilityTestState()
	filtered := filterStateForColor(state, "white")

	if len(filtered.BlackHand) != 2 {
		t.Fatalf("expected white to see 2 face-down stubs for black, got %d", len(filtered.BlackHand))
	}
	for i, card := range filtered.BlackHand {
		if card.ID != "" || card.Name != "" || card.Mechanic != "" || card.Rarity != "" {
			t.Fatalf("stub %d leaks card identity: %+v", i, card)
		}
	}
	if len(filtered.WhiteHand) != 1 || filtered.WhiteHand[0].ID != "w1" {
		t.Fatalf("white's own hand must be delivered intact, got %+v", filtered.WhiteHand)
	}

	mirror := filterStateForColor(state, "black")
	if len(mirror.WhiteHand) != 1 {
		t.Fatalf("expected black to see 1 face-down stub for white, got %d", len(mirror.WhiteHand))
	}
	if mirror.WhiteHand[0].ID != "" || mirror.WhiteHand[0].Name != "" {
		t.Fatalf("stub leaks card identity: %+v", mirror.WhiteHand[0])
	}
	if len(mirror.BlackHand) != 2 || mirror.BlackHand[0].ID != "b1" {
		t.Fatalf("black's own hand must be delivered intact, got %+v", mirror.BlackHand)
	}
}

func TestFilterStateForColorRadarRevealsOpponentHand(t *testing.T) {
	state := handVisibilityTestState()
	state.RadarRevealFor = "white"

	filtered := filterStateForColor(state, "white")
	if len(filtered.BlackHand) != 2 || filtered.BlackHand[0].ID != "b1" || filtered.BlackHand[1].ID != "b2" {
		t.Fatalf("radar must deliver black's real hand to white, got %+v", filtered.BlackHand)
	}

	// A radar flag for white must not leak black's hand to a spectator.
	spec := filterStateForColor(state, "")
	if len(spec.BlackHand) != 0 || len(spec.WhiteHand) != 0 {
		t.Fatalf("spectator must never receive hands, got white=%d black=%d", len(spec.WhiteHand), len(spec.BlackHand))
	}
}

func TestFilterStateForColorSpectatorSeesNoHands(t *testing.T) {
	state := handVisibilityTestState()
	filtered := filterStateForColor(state, "")
	if len(filtered.WhiteHand) != 0 || len(filtered.BlackHand) != 0 {
		t.Fatalf("spectator must see no hands, got white=%d black=%d", len(filtered.WhiteHand), len(filtered.BlackHand))
	}
	if filtered.WhitePlayerSecret != "" || filtered.BlackPlayerSecret != "" {
		t.Fatalf("spectator view must not carry seat secrets")
	}
}

// The service-level snapshot path must agree with the low-level filter.
func TestServiceSnapshotStubsOpponentHand(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	created := service.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           "handvis_live",
		ModeID:            contracts.MatchModeComputer,
		Difficulty:        "medium",
		WhiteGuestID:      "guest_white",
		WhitePlayerSecret: "white-secret",
	}, now)

	filtered := FilterSnapshotForColor(created, "white")
	if len(filtered.Match.BlackHand) != len(created.Match.BlackHand) {
		t.Fatalf("white should see %d face-down stubs, got %d", len(created.Match.BlackHand), len(filtered.Match.BlackHand))
	}
	for i, card := range filtered.Match.BlackHand {
		if card.ID != "" || card.Name != "" || card.Mechanic != "" {
			t.Fatalf("stub %d leaks card identity: %+v", i, card)
		}
	}
}
