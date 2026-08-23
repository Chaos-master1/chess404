package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// TestReverseDoesNotResurrectAStalePendingCard is a regression test for a
// production correctness bug xgauntlet's E0 cross-engine gauntlet found: a
// card that had already fully resolved (target selected, card removed from
// hand, PendingCard cleared) came back as "pending" again several plies
// later, right after a "reverse" card -- and since applyPlayCard rejects ANY
// new play_card while ANY PendingCard is set, regardless of whose, this
// silently disabled card play for BOTH sides for the rest of the match.
//
// Root cause: every select-target-resolving mechanic's case calls
// replaceLastHistorySnapshot mid-case, immediately after mutating the board
// but BEFORE the switch's shared tail clears PendingCard and removes the
// card from hand (match_cards.go, ~line 1200). So the history slot for that
// turn was frozen with the card still "pending" and still in hand. "reverse"
// restores a history slot two half-moves back (applyPlayCard's "reverse"
// case) -- landing on exactly that frozen, stale-pending slot resurrects an
// already-fully-resolved card as if it had never been played.
//
// Sequence: white moves (pushes history[1]), black plays and resolves
// "freeze" (mutates history[1] in place via replaceLastHistorySnapshot),
// black moves (pushes history[2], freezing history[1] permanently), white
// plays "reverse" -- restoring exactly history[1], the slot freeze mutated.
func TestReverseDoesNotResurrectAStalePendingCard(t *testing.T) {
	service := NewService()
	defer service.Close()
	now := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	snapshot := createTestMatch(service, contracts.CreateMatchRequest{MatchID: "reverse_stale_pending"}, now)

	// White: e2-e4.
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "make_move", MatchID: "reverse_stale_pending", PlayerID: "white_player",
		From: &contracts.Square{Row: 1, Col: 4}, To: &contracts.Square{Row: 3, Col: 4},
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("expected white's opening move to succeed, got %v", err)
	}

	// Black: play and fully resolve "freeze" on a white piece.
	freezeCardID := cardIDByMechanic(t, snapshot.Match.BlackHand, "freeze")
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "play_card", MatchID: "reverse_stale_pending", PlayerID: "black_player", CardID: freezeCardID,
	}, now.Add(2*time.Second)); err != nil {
		t.Fatalf("expected freeze play_card to succeed, got %v", err)
	}
	afterFreeze, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "select_target", MatchID: "reverse_stale_pending", PlayerID: "black_player",
		Target: &contracts.Square{Row: 0, Col: 1}, // white knight, b1
	}, now.Add(3*time.Second))
	if err != nil {
		t.Fatalf("expected freeze target selection to succeed, got %v", err)
	}
	if afterFreeze.Match.PendingCard != nil {
		t.Fatalf("expected freeze to be fully resolved, got PendingCard=%#v", afterFreeze.Match.PendingCard)
	}

	// Black: b8-c6, ending black's turn and freezing history[1] in place.
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "make_move", MatchID: "reverse_stale_pending", PlayerID: "black_player",
		From: &contracts.Square{Row: 7, Col: 1}, To: &contracts.Square{Row: 5, Col: 2},
	}, now.Add(4*time.Second)); err != nil {
		t.Fatalf("expected black's move to succeed, got %v", err)
	}

	// White: reverse, restoring history[1] -- exactly the slot freeze mutated.
	reverseCardID := cardIDByMechanic(t, snapshot.Match.WhiteHand, "reverse")
	afterReverse, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "play_card", MatchID: "reverse_stale_pending", PlayerID: "white_player", CardID: reverseCardID,
	}, now.Add(5*time.Second))
	if err != nil {
		t.Fatalf("expected reverse to succeed, got %v", err)
	}
	if afterReverse.Match.PendingCard != nil {
		t.Fatalf("reverse resurrected a stale PendingCard from freeze's already-resolved turn: %#v", afterReverse.Match.PendingCard)
	}

	// Prove the fix actually matters, not just that nothing broke: cards
	// must still be playable by EITHER side afterward. applyPlayCard
	// rejects ANY play_card while ANY PendingCard is set, regardless of
	// whose -- so before this fix, a resurrected PendingCard would block
	// every future card play for the rest of the match, not just the color
	// it nominally belonged to. "reverse" itself doesn't flip Turn (cards
	// never do), so it's still white's move.
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "make_move", MatchID: "reverse_stale_pending", PlayerID: "white_player",
		From: &contracts.Square{Row: 0, Col: 6}, To: &contracts.Square{Row: 2, Col: 5}, // Ng1-f3
	}, now.Add(6*time.Second)); err != nil {
		t.Fatalf("expected white's move after reverse to succeed, got %v", err)
	}
	sniperCardID := cardIDByMechanic(t, afterReverse.Match.BlackHand, "sniper")
	if _, err := applyTestIntent(service, contracts.PlayerIntent{
		Type: "play_card", MatchID: "reverse_stale_pending", PlayerID: "black_player", CardID: sniperCardID,
	}, now.Add(7*time.Second)); err != nil {
		t.Fatalf("expected black to still be able to play a card after white's reverse, got %v", err)
	}
}
