package actions

import (
	"math/rand"
	"testing"
)

func TestSampleHandProducesTheRequestedSize(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	hand := SampleHand(rng, 5)
	if len(hand) != 5 {
		t.Fatalf("expected a hand of 5 cards, got %d", len(hand))
	}
}

// TestSampleHandMatchesRarityWeightsStatistically draws a large sample and
// checks each tier's observed frequency lands close to its known weight --
// a sanity check that the per-card weights (tier weight / cards in tier)
// were assembled correctly, not a rigorous statistical test.
func TestSampleHandMatchesRarityWeightsStatistically(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const draws = 200000
	hand := SampleHand(rng, draws)

	rarityOf := map[Mechanic]Rarity{}
	for _, c := range cardPool {
		rarityOf[c.mechanic] = c.rarity
	}

	counts := map[Rarity]int{}
	for _, c := range hand {
		counts[rarityOf[c.Mechanic]]++
	}

	for rarity, want := range rarityWeight {
		got := float64(counts[rarity]) / float64(draws)
		if diff := got - want; diff > 0.01 || diff < -0.01 {
			t.Errorf("rarity %s: observed frequency %.4f, expected %.4f (tolerance 0.01)", rarity, got, want)
		}
	}
}

func TestSampleHandNeverProducesAnUnknownMechanic(t *testing.T) {
	known := map[Mechanic]bool{}
	for _, c := range cardPool {
		known[c.mechanic] = true
	}
	rng := rand.New(rand.NewSource(7))
	for _, c := range SampleHand(rng, 5000) {
		if !known[c.Mechanic] {
			t.Fatalf("sampled an unknown mechanic %q", c.Mechanic)
		}
	}
}
