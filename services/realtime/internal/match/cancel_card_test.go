package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// Regression tests for a live-production deadlock: a player who abandons a
// pending multi-step card client-side had no way to clear the server's
// PendingCard, so every later play_card was rejected with "resolve the pending
// card target first" for the rest of the match (45 consecutive rejections
// observed in one session). cancel_card is the server-side escape hatch; these
// tests pin its authorization and state behavior.

func TestCancelCardClearsPendingWithoutConsumingCard(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	snapshot := createTestMatch(service, contracts.CreateMatchRequest{MatchID: "cancel_card_ok"}, now)
	cardID := cardIDByMechanic(t, snapshot.Match.WhiteHand, "freeze")

	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "play_card",
		MatchID:  "cancel_card_ok",
		PlayerID: "white_player",
		CardID:   cardID,
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("expected play_card to enter pending state, got %v", err)
	}

	result, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "cancel_card",
		MatchID:  "cancel_card_ok",
		PlayerID: "white_player",
		CardID:   cardID,
	}, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("expected cancel_card to clear the pending state, got %v", err)
	}
	if result.Match.PendingCard != nil {
		t.Fatalf("expected PendingCard to be cleared, got %#v", result.Match.PendingCard)
	}
	// A pending card is consumed from the hand when its target RESOLVES, not
	// when it is played -- cancelling must not touch the hand either way.
	if len(result.Match.WhiteHand) != len(snapshot.Match.WhiteHand) {
		t.Fatalf("expected the pending card to stay in the hand after cancel, got %d cards (had %d)", len(result.Match.WhiteHand), len(snapshot.Match.WhiteHand))
	}
}

func TestCancelCardUnblocksLaterCardPlay(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	snapshot := createTestMatch(service, contracts.CreateMatchRequest{MatchID: "cancel_card_replay"}, now)
	hand := snapshot.Match.WhiteHand
	if len(hand) < 2 {
		t.Fatalf("need at least two cards for this test, got %d", len(hand))
	}
	first := cardIDByMechanic(t, hand, "freeze")

	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "play_card", MatchID: "cancel_card_replay", PlayerID: "white_player", CardID: first,
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("expected first play_card to enter pending state, got %v", err)
	}
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "cancel_card", MatchID: "cancel_card_replay", PlayerID: "white_player",
	}, now.Add(2*time.Second)); err != nil {
		t.Fatalf("expected cancel_card to succeed, got %v", err)
	}

	// The exact production symptom: after cancelling, a NEW play_card must be
	// accepted again instead of "resolve the pending card target first".
	second := ""
	for _, c := range hand {
		if c.ID != first {
			second = c.ID
			break
		}
	}
	if second == "" {
		t.Fatal("no second card available to replay with")
	}
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "play_card", MatchID: "cancel_card_replay", PlayerID: "white_player", CardID: second,
	}, now.Add(3*time.Second)); err != nil {
		t.Fatalf("expected a later play_card to be accepted after cancel, got %v", err)
	}
}

func TestCancelCardRejectsWrongOwnerAndEmptyPending(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	snapshot := createTestMatch(service, contracts.CreateMatchRequest{MatchID: "cancel_card_auth"}, now)
	cardID := cardIDByMechanic(t, snapshot.Match.WhiteHand, "freeze")

	// Nothing pending yet -- cancelling must be rejected.
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "cancel_card", MatchID: "cancel_card_auth", PlayerID: "white_player",
	}, now.Add(time.Second)); err == nil {
		t.Fatal("expected cancel_card without a pending card to be rejected")
	}

	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "play_card", MatchID: "cancel_card_auth", PlayerID: "white_player", CardID: cardID,
	}, now.Add(2*time.Second)); err != nil {
		t.Fatalf("expected play_card to enter pending state, got %v", err)
	}

	// The opponent must not be able to cancel someone else's pending card.
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "cancel_card", MatchID: "cancel_card_auth", PlayerID: "black_player",
	}, now.Add(3*time.Second)); err == nil {
		t.Fatal("expected cancel_card for another player's pending card to be rejected")
	}

	// The pending card is untouched and still resolvable by its owner.
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type:     "select_target",
		MatchID:  "cancel_card_auth",
		PlayerID: "white_player",
		Target:   &contracts.Square{Row: 6, Col: 0},
	}, now.Add(4*time.Second)); err != nil {
		t.Fatalf("expected the pending card to still be resolvable by its owner, got %v", err)
	}
}

// Re-playing a card by the SAME player while their own pending card is armed
// must be an implicit abandon-and-switch, not a forever-deadlock: a client
// that dismisses a pending card without (successfully) sending cancel_card --
// old bundles, a dropped request -- used to leave the server pending armed
// for the rest of the match (44 consecutive "resolve the pending card target
// first" rejections observed live).
func TestReplaySamePlayerSwitchesPendingCardImplicitly(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	snapshot := createTestMatch(service, contracts.CreateMatchRequest{MatchID: "pending_switch"}, now)
	hand := snapshot.Match.WhiteHand
	if len(hand) < 2 {
		t.Fatalf("need at least two cards, got %d", len(hand))
	}
	first := cardIDByMechanic(t, hand, "freeze")
	second := ""
	for _, c := range hand {
		if c.ID != first {
			second = c.ID
			break
		}
	}

	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "play_card", MatchID: "pending_switch", PlayerID: "white_player", CardID: first,
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("expected first play_card to arm pending, got %v", err)
	}

	result, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "play_card", MatchID: "pending_switch", PlayerID: "white_player", CardID: second,
	}, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("expected same-player replay to switch the pending card, got %v", err)
	}
	if result.Match.PendingCard == nil {
		t.Fatal("expected the new card to be pending")
	}
	if got := cardIDByMechanic(t, result.Match.WhiteHand, result.Match.PendingCard.Mechanic); got != second {
		t.Fatalf("expected pending card to be %q, got %q", second, got)
	}
	if len(result.Match.WhiteHand) != len(hand) {
		t.Fatalf("expected hand unchanged (pending card not consumed), got %d (had %d)", len(result.Match.WhiteHand), len(hand))
	}
}
