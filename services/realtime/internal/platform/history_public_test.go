package platform

import "testing"

// Regression tests for the public Watch feed showing matches it never
// should have: private invite games (queue=="direct"), vs-computer games
// (modeId=="computer"), and aborted games (finishReason=="abort"). Before
// this fix, IsPublicReplayableMatch checked only Status=="finished" -- no
// queue/mode/finish-reason filtering at all -- so every private match,
// every computer match, and every trivially aborted game (often zero
// moves played) appeared in the public replay list once finished.
// IsPublicLiveSpectateMatch already excluded direct/private games but not
// computer games, so a computer match queued through a non-direct lane
// still showed up as a live spectate entry.

func TestIsPublicReplayableMatchExcludesPrivateMatches(t *testing.T) {
	entry := MatchArchiveEntry{MatchID: "m1", Status: "finished", Queue: "direct", Winner: "white"}
	if IsPublicReplayableMatch(entry) {
		t.Fatal("expected a private (queue=direct) finished match to be excluded from the public replay feed")
	}
}

func TestIsPublicReplayableMatchExcludesComputerMatches(t *testing.T) {
	entry := MatchArchiveEntry{MatchID: "m2", Status: "finished", Queue: "casual", ModeID: "computer", Winner: "white"}
	if IsPublicReplayableMatch(entry) {
		t.Fatal("expected a vs-computer finished match to be excluded from the public replay feed")
	}
}

func TestIsPublicReplayableMatchExcludesAbortedGames(t *testing.T) {
	entry := MatchArchiveEntry{MatchID: "m3", Status: "finished", Queue: "casual", Winner: "aborted", FinishReason: "abort"}
	if IsPublicReplayableMatch(entry) {
		t.Fatal("expected an aborted game to be excluded from the public replay feed")
	}
}

func TestIsPublicReplayableMatchIncludesRealFinishedGames(t *testing.T) {
	entry := MatchArchiveEntry{MatchID: "m4", Status: "finished", Queue: "casual", Winner: "white", FinishReason: "checkmate"}
	if !IsPublicReplayableMatch(entry) {
		t.Fatal("expected a real finished human-vs-human game to remain visible in the public replay feed")
	}
}

func TestIsPublicLiveSpectateMatchExcludesComputerMatches(t *testing.T) {
	entry := MatchArchiveEntry{MatchID: "m5", Status: "active", Queue: "casual", ModeID: "computer"}
	if IsPublicLiveSpectateMatch(entry) {
		t.Fatal("expected a live vs-computer match to be excluded from the public watch feed")
	}
}

func TestIsPublicLiveSpectateMatchIncludesRealActiveGames(t *testing.T) {
	entry := MatchArchiveEntry{MatchID: "m6", Status: "active", Queue: "casual", ModeID: "open_cards", WhiteGuestID: "guest_w", BlackGuestID: "guest_b"}
	if !IsPublicLiveSpectateMatch(entry) {
		t.Fatal("expected a real live human-vs-human game to remain visible in the public watch feed")
	}
}

// Match creation is a cheap public call, so unclaimed rooms accumulate. They
// have no game to watch and used to crowd real games out of the feed: a live
// production check found 33 of 34 "watchable" matches were empty rooms.
func TestIsPublicLiveSpectateMatchExcludesUnclaimedSeats(t *testing.T) {
	for name, entry := range map[string]MatchArchiveEntry{
		"no seats":  {MatchID: "m7", Status: "active", Queue: "casual", ModeID: "open_cards"},
		"white only": {MatchID: "m8", Status: "active", Queue: "casual", ModeID: "open_cards", WhiteGuestID: "guest_w"},
		"black only": {MatchID: "m9", Status: "active", Queue: "casual", ModeID: "open_cards", BlackGuestID: "guest_b"},
	} {
		if IsPublicLiveSpectateMatch(entry) {
			t.Fatalf("expected a match with %s to stay out of the public watch feed", name)
		}
	}
}
