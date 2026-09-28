package match

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

const (
	whiteTestSecret = "white-super-secret-value"
	blackTestSecret = "black-super-secret-value"
)

func newSecurityTestMatch(t *testing.T, service *Service, matchID string, now time.Time) {
	t.Helper()
	service.CreateMatch(contracts.CreateMatchRequest{
		MatchID:           matchID,
		WhiteGuestID:      "guest_white",
		BlackGuestID:      "guest_black",
		WhitePlayerSecret: whiteTestSecret,
		BlackPlayerSecret: blackTestSecret,
		// Hand-visibility assertions below assume the hidden-cards mode; an
		// unset mode normalizes to open_cards, where hands are public.
		ModeID: contracts.MatchModeHiddenCards,
	}, now)
}

func TestRedactPlayerSecretNeverIncludesCredentialMaterial(t *testing.T) {
	secret := "MixedCase-Bearer-Secret"
	if got := redactPlayerSecret(secret); got != "<redacted>" {
		t.Fatalf("expected a fixed redaction marker, got %q", got)
	}
	if got := redactPlayerSecret(""); got != "<empty>" {
		t.Fatalf("expected empty secret marker, got %q", got)
	}
}

// Seat secrets are bearer credentials for a seat: holding one lets you move,
// play cards and resign as that player. They must never be serialized into a
// client-facing snapshot. Previously GET /api/matches/{id} returned both,
// unauthenticated, which handed anyone full control of both seats.
func TestViewerSnapshotNeverExposesSeatSecrets(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	newSecurityTestMatch(t, service, "sec_leak", now)

	viewers := []struct {
		name     string
		playerID string
		secret   string
	}{
		{"spectator", "", ""},
		{"white seat", "guest_white", whiteTestSecret},
		{"black seat", "guest_black", blackTestSecret},
	}

	for _, v := range viewers {
		t.Run(v.name, func(t *testing.T) {
			resp, err := service.GetMatchForViewer("sec_leak", v.playerID, v.secret)
			if err != nil {
				t.Fatalf("GetMatchForViewer: %v", err)
			}
			encoded, err := json.Marshal(resp)
			if err != nil {
				t.Fatalf("marshal snapshot: %v", err)
			}
			for _, secret := range []string{whiteTestSecret, blackTestSecret} {
				if bytes.Contains(encoded, []byte(secret)) {
					t.Fatalf("serialized snapshot leaked seat secret %q", secret)
				}
			}
		})
	}
}

// Guest IDs are public in every snapshot, so claiming one must not be enough
// to be served that seat's view.
func TestGetMatchForViewerRejectsWrongSecret(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	newSecurityTestMatch(t, service, "sec_view", now)

	if _, err := service.GetMatchForViewer("sec_view", "guest_black", "not-the-secret"); err == nil {
		t.Fatal("expected rejection when claiming a seat with a wrong secret")
	}
}

// Same for the WebSocket path: Subscribe used to resolve the seat from
// playerID alone, so an attacker could read the opponent's guest ID from a
// spectator frame, reconnect as them, and stream their private hand.
func TestSubscribeRejectsWrongSecret(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	newSecurityTestMatch(t, service, "sec_sub", now)

	if _, _, _, err := service.Subscribe("sec_sub", "guest_black", "not-the-secret"); err == nil {
		t.Fatal("expected Subscribe to reject a seat claim with a wrong secret")
	}

	stream, unsubscribe, initial, err := service.Subscribe("sec_sub", "guest_black", blackTestSecret)
	if err != nil {
		t.Fatalf("Subscribe with the correct secret should succeed: %v", err)
	}
	defer unsubscribe()
	if stream == nil {
		t.Fatal("expected a snapshot stream")
	}
	if len(initial.Match.BlackHand) == 0 {
		t.Fatal("black seat should receive its own hand")
	}
	// The opposing hand ships as face-down stubs: the count must survive
	// (so the seat can see how many cards the opponent holds) but no card
	// identity may leak.
	if len(initial.Match.WhiteHand) == 0 {
		t.Fatal("black seat should still see the opposing hand count via face-down stubs")
	}
	for i, stub := range initial.Match.WhiteHand {
		if stub.ID != "" || stub.Name != "" || stub.Mechanic != "" || stub.Rarity != "" {
			t.Fatalf("opponent hand stub %d leaks card identity: %+v", i, stub)
		}
	}
	if initial.Match.WhitePlayerSecret != "" || initial.Match.BlackPlayerSecret != "" {
		t.Fatal("subscribe snapshot leaked a seat secret")
	}
}

// Matching a seat's public guest ID must not let a caller rewrite that seat's
// linked account. Rated finalization credits Elo by account ID, so this was a
// rating-theft vector against any live match.
func TestJoinMatchSeatRejectsIdentityRewriteWithoutSecret(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	newSecurityTestMatch(t, service, "sec_join", now)

	_, err := service.JoinMatchSeat("sec_join", contracts.JoinMatchSeatRequest{
		GuestID:   "guest_white",
		AccountID: "attacker_account",
	}, now)
	if err == nil {
		t.Fatal("expected seat rewrite without the seat secret to be rejected")
	}

	c := service.getMatchContainer("sec_join")
	c.mu.Lock()
	gotAccount := c.state.WhiteAccountID
	c.mu.Unlock()
	if gotAccount == "attacker_account" {
		t.Fatal("attacker rewrote the seat's linked account")
	}

	// The legitimate owner, presenting the seat secret, still succeeds.
	if _, err := service.JoinMatchSeat("sec_join", contracts.JoinMatchSeatRequest{
		GuestID:      "guest_white",
		AccountID:    "real_account",
		PlayerSecret: whiteTestSecret,
	}, now); err != nil {
		t.Fatalf("legitimate seat owner should still be able to join: %v", err)
	}
}

// requireIntentColor's auth order is security-relevant and pinned here:
//
//  1. If the playerID matches a seat's guest id, the secret is checked
//     against THAT seat -- an id claiming to be guest_b can NEVER
//     authenticate with white's secret, even though the caller holds a
//     valid (other seat's) credential. Guest ids are public; secrets are
//     not; the id chooses the seat, the secret must prove it.
//  2. If the id matches NO seat (legacy clients send placeholder ids),
//     the secret alone resolves the seat via the final fallback -- the
//     bearer credential identifies its owner.
//  3. A wrong or empty secret never authenticates, in any path.
//
// Changing any of this must be a conscious decision, not a refactor slip.
func TestRequireIntentColorSecretOnlyFallbackIsPinned(t *testing.T) {
	state := &contracts.MatchState{
		MatchID:           "pin_secret_fallback",
		WhiteGuestID:      "guest_w",
		BlackGuestID:      "guest_b",
		WhitePlayerSecret: "white-secret",
		BlackPlayerSecret: "black-secret",
	}

	// (1) Correct id + correct secret: resolves.
	color, err := requireIntentColor(state, "guest_w", "white-secret")
	if err != nil || color != "white" {
		t.Fatalf("expected white for correct id+secret, got %s err=%v", color, err)
	}

	// (1) A seat-matching id is checked against ITS OWN seat only: the
	// opponent's public guest id cannot ride white's secret.
	if _, err := requireIntentColor(state, "guest_b", "white-secret"); err == nil {
		t.Fatal("guest_b id must NOT authenticate with white's secret")
	}

	// (2) Placeholder id (matches no seat) + valid secret: resolves via the
	// secret-only fallback.
	color, err = requireIntentColor(state, "legacy-client-placeholder", "black-secret")
	if err != nil || color != "black" {
		t.Fatalf("placeholder id with valid secret must resolve via fallback, got %s err=%v", color, err)
	}
	color, err = requireIntentColor(state, "", "white-secret")
	if err != nil || color != "white" {
		t.Fatalf("empty id with valid secret resolves via fallback (pinned), got %s err=%v", color, err)
	}

	// (3) Wrong or empty secret: never resolves, in any path.
	if _, err := requireIntentColor(state, "guest_w", "black-secret"); err == nil {
		t.Fatal("wrong secret must be rejected even with a matching id")
	}
	if _, err := requireIntentColor(state, "guest_w", ""); err == nil {
		t.Fatal("empty secret must be rejected")
	}
	if _, err := requireIntentColor(state, "legacy-placeholder", "no-such-secret"); err == nil {
		t.Fatal("wrong secret must be rejected via the fallback path too")
	}
}
