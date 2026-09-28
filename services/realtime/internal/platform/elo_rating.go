package platform

import "math"

const (
	defaultEloKFactor          = 32.0
	defaultPlacementEloKFactor = 64.0
	defaultPlacementMatches    = 5
	defaultEloStartRating      = 1200
	defaultEloMinRating        = 100
	// Post-placement K-factor decay (FIDE-style): new players converge fast,
	// established players converge slowly so an established rating means
	// something. Boundaries are games PLAYED (inclusive thresholds).
	defaultKFactorEstablished = 16.0
	defaultKFactorTours       = 24.0
	defaultKFactorNovice      = 40.0
	defaultKFactorGamesNovice = 30
	defaultKFactorGamesTours  = 120
)

// ApplyEloMatchResult updates the two ratings according to the standard
// Elo formula with a 32-point K-factor, returning the new ratings.
//
//	winner: "white" | "black" | "draw"
//
// The K-factor and minimum rating are exposed for callers that need to vary
// them (account finalization uses a stricter floor).
func ApplyEloMatchResult(whiteRating, blackRating int, winner string) (int, int) {
	return ApplyEloMatchResultWithK(whiteRating, blackRating, winner, defaultEloKFactor, defaultEloMinRating)
}

// eloKFactorForGames returns the post-placement K factor for a player with
// the given number of games played (placements already consumed). The curve:
// K=40 for the first 30 games (fast convergence off a provisional rating),
// K=24 up to 120 games, K=16 beyond -- so climbing early is quick but the
// rating of an established player is stable and meaningful. Placement games
// (K=64) are handled separately by the caller before this helper is used.
func eloKFactorForGames(matchesPlayed int) float64 {
	switch {
	case matchesPlayed < defaultKFactorGamesNovice:
		return defaultKFactorNovice
	case matchesPlayed < defaultKFactorGamesTours:
		return defaultKFactorTours
	default:
		return defaultKFactorEstablished
	}
}

func ApplyEloMatchResultWithK(whiteRating, blackRating int, winner string, kFactor float64, minRating int) (int, int) {
	if whiteRating <= 0 {
		whiteRating = defaultEloStartRating
	}
	if blackRating <= 0 {
		blackRating = defaultEloStartRating
	}
	whiteR := float64(whiteRating)
	blackR := float64(blackRating)
	whiteExpected := 1.0 / (1.0 + math.Pow(10, (blackR-whiteR)/400.0))
	blackExpected := 1.0 - whiteExpected

	var newWhite, newBlack int
	switch winner {
	case "white":
		newWhite = int(math.Round(whiteR + kFactor*(1.0-whiteExpected)))
		newBlack = int(math.Round(blackR + kFactor*(0.0-blackExpected)))
	case "black":
		newBlack = int(math.Round(blackR + kFactor*(1.0-blackExpected)))
		newWhite = int(math.Round(whiteR + kFactor*(0.0-whiteExpected)))
	case "draw":
		newWhite = int(math.Round(whiteR + kFactor*(0.5-whiteExpected)))
		newBlack = int(math.Round(blackR + kFactor*(0.5-blackExpected)))
	default:
		return whiteRating, blackRating
	}

	if newBlack < minRating {
		newBlack = minRating
	}
	if newWhite < minRating {
		newWhite = minRating
	}
	return newWhite, newBlack
}
