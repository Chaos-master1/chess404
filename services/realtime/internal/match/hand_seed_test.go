package match

import (
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// Opening-deal determinism contract:
//
//   - Hands are derived from the match's SERVER-CHOSEN RNGSeed (clients
//     cannot pin it -- see chooseSeed: a client-pinned seed would let a
//     hidden-cards player compute the opponent's hand and future draws).
//   - The two seats draw from differently-seeded streams of one match seed
//     (black's stream is offset), so the hands differ within a match.
//   - Replays reproduce hands by reusing the STORED seed, so the deal for a
//     given seed must be stable across service instances.
func TestOpeningHandsDeterministicPerStoredSeed(t *testing.T) {
	seed := int64(4242)
	white := starterHandForSeed("starter_three", seed, "white")
	black := starterHandForSeed("starter_three", seed, "black")

	whiteAgain := starterHandForSeed("starter_three", seed, "white")
	blackAgain := starterHandForSeed("starter_three", seed, "black")

	if handsSignature(white) != handsSignature(whiteAgain) {
		t.Fatalf("same seed must reproduce white's hand: %+v vs %+v", white, whiteAgain)
	}
	if handsSignature(black) != handsSignature(blackAgain) {
		t.Fatalf("same seed must reproduce black's hand: %+v vs %+v", black, blackAgain)
	}
	if handsSignature(white) == handsSignature(black) {
		t.Fatal("white and black must draw from differently-seeded streams")
	}
}

func TestClientSuppliedSeedIsIgnored(t *testing.T) {
	now := time.Date(2026, 5, 5, 8, 5, 0, 0, time.UTC)

	service := NewService()
	a := createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:         "seed_client_a",
		ModeID:          contracts.MatchModeHiddenCards,
		StarterHandMode: "starter_three",
		Seed:            31337,
	}, now)
	b := createTestMatch(service, contracts.CreateMatchRequest{
		MatchID:         "seed_client_b",
		ModeID:          contracts.MatchModeHiddenCards,
		StarterHandMode: "starter_three",
		Seed:            31337,
	}, now.Add(time.Second))

	// A client pinning the same seed twice must NOT get the same hands --
	// otherwise the seed becomes a hidden-information oracle.
	if handsSignature(a.Match.WhiteHand) == handsSignature(b.Match.WhiteHand) {
		t.Fatal("client-pinned seed leaked into the deal: same request seed produced identical hands")
	}
}

func handsSignature(hand []contracts.GameCard) string {
	sig := ""
	for _, c := range hand {
		sig += c.ID + "|"
	}
	return sig
}
