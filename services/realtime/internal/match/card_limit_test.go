package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

func testMatchContainer(t *testing.T, service *Service, matchID string) *matchContainer {
	t.Helper()
	c, ok := service.matches.Load(matchID)
	if !ok {
		t.Fatalf("expected match %q to be loaded", matchID)
	}
	return c
}

// Regression: the one-card-per-turn rule was enforced only by the browser UI
// (cardUsedBy); the server accepted unlimited play_card intents per turn from
// any API client. The server now consumes the card slot in removeCardFromHand
// -- the same moment the client's finishCardUse sets its flag -- and refuses
// further play_card intents that turn.
func TestOneCardPerTurnEnforcedServerSide(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 6, 12, 11, 0, 0, 0, time.UTC)
	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "card_limit",
		WhiteGuestID: "guest-white",
		BlackGuestID: "guest-black",
	}, now)

	c := testMatchContainer(t, service, "card_limit")
	c.mu.Lock()
	c.state.Turn = "white"
	radarOne := cardTemplateByMechanic("radar")
	radarTwo := cardTemplateByMechanic("radar")
	radarTwo.ID = radarOne.ID + "_second" // distinct IDs; templates share one
	c.state.WhiteHand = []contracts.GameCard{radarOne, radarTwo}
	c.mu.Unlock()

	hand := c.state.WhiteHand
	first := cardIDByMechanic(t, hand, "radar")

	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "play_card",
		MatchID:  "card_limit",
		PlayerID: "white_player",
		CardID:   first,
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("expected first card play to succeed, got %v", err)
	}

	second := cardIDByMechanic(t, c.state.WhiteHand, "radar")
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "play_card",
		MatchID:  "card_limit",
		PlayerID: "white_player",
		CardID:   second,
	}, now.Add(2*time.Second)); err == nil {
		t.Fatal("expected second card play in the same turn to be refused")
	}
}

// After a move flips the turn, the mover's slot is free again on their next
// turn -- the flag must not leak across turns.
func TestCardUsedFlagResetsAfterTurnFlip(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 6, 12, 11, 30, 0, 0, time.UTC)
	createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:      "card_limit_reset",
		WhiteGuestID: "guest-white",
		BlackGuestID: "guest-black",
	}, now)

	c := testMatchContainer(t, service, "card_limit_reset")
	c.mu.Lock()
	c.state.Turn = "white"
	c.state.WhiteHand = []contracts.GameCard{cardTemplateByMechanic("radar")}
	c.mu.Unlock()

	first := cardIDByMechanic(t, c.state.WhiteHand, "radar")
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "play_card",
		MatchID:  "card_limit_reset",
		PlayerID: "white_player",
		CardID:   first,
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("expected card play to succeed, got %v", err)
	}

	// A white move flips the turn (resets black's slot); then black's move
	// flips it back and resets white's slot for the new round -- the same
	// sequence a real game produces.
	whitePawnRow, whitePawnCol := 1, 4
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "make_move",
		MatchID:  "card_limit_reset",
		PlayerID: "white_player",
		From:     &contracts.Square{Row: whitePawnRow, Col: whitePawnCol},
		To:       &contracts.Square{Row: whitePawnRow + 2, Col: whitePawnCol},
	}, now.Add(2*time.Second)); err != nil {
		t.Fatalf("expected white move to succeed, got %v", err)
	}
	blackPawnRow, blackPawnCol := 6, 4
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "make_move",
		MatchID:  "card_limit_reset",
		PlayerID: "black_player",
		From:     &contracts.Square{Row: blackPawnRow, Col: blackPawnCol},
		To:       &contracts.Square{Row: blackPawnRow - 2, Col: blackPawnCol},
	}, now.Add(3*time.Second)); err != nil {
		t.Fatalf("expected black move to succeed, got %v", err)
	}

	// Give white another radar for the new round and play it: the slot must
	// have been reset when the turn returned to white.
	c.mu.Lock()
	c.state.WhiteHand = append(c.state.WhiteHand, cardTemplateByMechanic("radar"))
	c.mu.Unlock()

	next := c.state.WhiteHand[len(c.state.WhiteHand)-1]
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "play_card",
		MatchID:  "card_limit_reset",
		PlayerID: "white_player",
		CardID:   next.ID,
	}, now.Add(4*time.Second)); err != nil {
		t.Fatalf("expected card play on the new turn to succeed, got %v", err)
	}
}
