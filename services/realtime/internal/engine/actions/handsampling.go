package actions

import (
	"fmt"
	"math/rand"
)

// Fair-play hidden-hand sampling: the engine must never be given the
// opponent's actual hand (an explicit, locked-in design constraint for
// this rebuild -- "Play fair: search over plausible hands"). Instead, when
// a search needs to reason about what the opponent might do, it samples a
// PLAUSIBLE hand from public information (hand size, which is always
// visible) and the game's own draw distribution, then aggregates results
// across several such samples (Perfect-Information Monte Carlo -- solve
// each sample with full information, then combine). This file provides the
// sampling primitive; engine/search is what actually runs PIMC.
//
// Deliberately NOT modeled: reconstructing the draw sequence from
// RNGSeed+FullMoveNum (deterministicCardIndex, cards.go:554) -- the engine
// has access to RNGSeed on the wire exactly like a cheating human player
// would, and exploiting it is precisely what "fair play" rules out. This
// package never reads RNGSeed for sampling purposes, only handSize.

// Rarity is a card's rarity tier, matching packages/game-core/src/cards.json's
// rarity field. This package mirrors only the (mechanic, rarity) pairs
// sampling needs, not the cosmetic fields (name, color, icon, description).
type Rarity string

const (
	RarityTrash     Rarity = "trash"
	RarityCommon    Rarity = "common"
	RarityRare      Rarity = "rare"
	RarityEpic      Rarity = "epic"
	RarityLegendary Rarity = "legendary"
)

// rarityWeight is the draw probability mass for an entire tier -- read
// directly from the live client's "DROP RATES" panel (Trash 5%, Common
// 40%, Rare 30%, Epic 20%, Legendary 5%), matching packages/game-core's
// RARITY_WEIGHTS. A specific card's draw probability is this tier weight
// divided evenly among every card sharing that tier.
var rarityWeight = map[Rarity]float64{
	RarityTrash:     0.05,
	RarityCommon:    0.40,
	RarityRare:      0.30,
	RarityEpic:      0.20,
	RarityLegendary: 0.05,
}

// cardPool mirrors packages/game-core/src/cards.json's (mechanic, rarity)
// pairs exactly -- all 37 mechanics, including the 30 this package doesn't
// model as Actions (see the package comment in action.go). Fair-play
// sampling must draw from the COMPLETE pool the opponent could actually
// hold, not just the subset this engine currently knows how to search
// over -- a sampled card whose mechanic has no candidate proposer
// (candidates.go's generateCardActions returns nil for it) simply
// contributes zero actions, exactly as if the engine chose not to search
// that branch, never a crash or a silently-dropped card.
var cardPool = []struct {
	mechanic Mechanic
	rarity   Rarity
}{
	{"freeze", RarityCommon}, {"shield", RarityCommon}, {"sniper", RarityEpic},
	{"badsniper", RarityTrash}, {"promote", RarityCommon}, {"demote", RarityTrash},
	{"promotehim", RarityTrash}, {"demotehim", RarityRare}, {"teleport", RarityRare},
	{"jump", RarityCommon}, {"doublemove_diff", RarityRare}, {"doublemove_same", RarityRare},
	{"swapme", RarityCommon}, {"swapus", RarityRare}, {"swaphim", RarityRare},
	{"borrow", RarityEpic}, {"mindcontrol", RarityLegendary}, {"parasite", RarityEpic},
	{"clone", RarityEpic}, {"lavaground", RarityRare}, {"fog_village", RarityCommon},
	{"invisible", RarityRare}, {"unabomber", RarityEpic}, {"halffuse", RarityCommon},
	{"fullfusion", RarityRare}, {"fortress", RarityEpic}, {"reverse", RarityEpic},
	{"undo", RarityEpic}, {"mirror", RarityRare}, {"fakepiece", RarityRare},
	{"blackhole", RarityEpic}, {"smallsacrifice", RarityCommon}, {"bigsacrifice", RarityEpic},
	{"gambler", RarityTrash}, {"radar", RarityRare}, {"cheater", RarityRare},
	{"joker", RarityRare},
}

// cardWeights[i] is cardPool[i]'s exact per-card draw probability (its
// tier's weight divided by how many cards share that tier) -- precomputed
// once so SampleHand doesn't recompute tier counts on every call.
var cardWeights []float64

func init() {
	tierCounts := map[Rarity]int{}
	for _, c := range cardPool {
		tierCounts[c.rarity]++
	}
	cardWeights = make([]float64, len(cardPool))
	for i, c := range cardPool {
		cardWeights[i] = rarityWeight[c.rarity] / float64(tierCounts[c.rarity])
	}
}

// SampleHand draws handSize cards independently from the rarity-weighted
// pool. internal/match's own draws are independent and effectively
// unlimited ("infinite draws", cards.go's deterministicCardIndex has no
// finite deck to deplete), so with-replacement sampling matches the
// reference's actual model rather than approximating a finite-deck one.
// Sampled card IDs are synthetic ("sampled_0", "sampled_1", ...) -- they
// never need to match a real card's ID, only its mechanic.
func SampleHand(rng *rand.Rand, handSize int) Hand {
	hand := make(Hand, handSize)
	for i := 0; i < handSize; i++ {
		hand[i] = CardInstance{ID: fmt.Sprintf("sampled_%d", i), Mechanic: sampleOneMechanic(rng)}
	}
	return hand
}

func sampleOneMechanic(rng *rand.Rand) Mechanic {
	r := rng.Float64()
	var cumulative float64
	for i, w := range cardWeights {
		cumulative += w
		if r < cumulative {
			return cardPool[i].mechanic
		}
	}
	return cardPool[len(cardPool)-1].mechanic // float rounding fallback
}
