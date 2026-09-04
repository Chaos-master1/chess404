package main

import (
	"net/http"
	"os"
	"strings"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/platform"
)

// Archive enrichment, filtering, and account/guest query helpers.

func enrichArchiveEntry(accounts platform.AccountDirectory, entry platform.MatchArchiveEntry) platform.MatchArchiveEntry {
	if accounts == nil {
		return entry
	}
	if entry.WhiteAccountID == "" {
		if account, ok := accounts.GetAccountByGuest(entry.WhiteGuestID); ok {
			entry.WhiteAccountID = account.AccountID
			if entry.WhiteAccountHandle == "" {
				entry.WhiteAccountHandle = account.Handle
			}
		}
	} else if entry.WhiteAccountHandle == "" {
		if account, ok := accounts.GetAccount(entry.WhiteAccountID); ok {
			entry.WhiteAccountHandle = account.Handle
		}
	}
	if entry.BlackAccountID == "" {
		if account, ok := accounts.GetAccountByGuest(entry.BlackGuestID); ok {
			entry.BlackAccountID = account.AccountID
			if entry.BlackAccountHandle == "" {
				entry.BlackAccountHandle = account.Handle
			}
		}
	} else if entry.BlackAccountHandle == "" {
		if account, ok := accounts.GetAccount(entry.BlackAccountID); ok {
			entry.BlackAccountHandle = account.Handle
		}
	}
	if entry.Snapshot.Match.WhiteAccountID == "" {
		entry.Snapshot.Match.WhiteAccountID = entry.WhiteAccountID
	}
	if entry.Snapshot.Match.BlackAccountID == "" {
		entry.Snapshot.Match.BlackAccountID = entry.BlackAccountID
	}
	return entry
}

func filterArchivedMatchesBySeason(matches []platform.MatchArchiveEntry, seasonID string) []platform.MatchArchiveEntry {
	if seasonID == "" {
		return matches
	}
	filtered := make([]platform.MatchArchiveEntry, 0, len(matches))
	for _, entry := range matches {
		playedAt := entry.UpdatedAt
		if playedAt.IsZero() {
			playedAt = entry.CreatedAt
		}
		if playedAt.UTC().Format("2006-01") == seasonID {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func filterArchivedMatchesByMode(matches []platform.MatchArchiveEntry, modeID contracts.MatchModeID) []platform.MatchArchiveEntry {
	filtered := make([]platform.MatchArchiveEntry, 0, len(matches))
	for _, entry := range matches {
		if contracts.NormalizeMatchModeID(string(entry.ModeID)) != modeID {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func filterArchivedMatchesByStatus(matches []platform.MatchArchiveEntry, status string) []platform.MatchArchiveEntry {
	filtered := make([]platform.MatchArchiveEntry, 0, len(matches))
	for _, entry := range matches {
		entryStatus := strings.TrimSpace(entry.Status)

		// When filtering for "active", exclude matches that are actually finished
		// (have a winner or finish reason) even if their status field says "active"
		if status == "active" {
			isActuallyFinished := entry.Winner != "" || entry.FinishReason != ""
			if isActuallyFinished {
				continue
			}
			// Only include matches with status "active" that aren't finished
			if entryStatus != "active" {
				continue
			}
		} else if status == "finished" {
			// When filtering for "finished", include matches that:
			// 1. Have status "finished", OR
			// 2. Have a winner/finishReason (actually finished) even if status is still "active"
			isActuallyFinished := entry.Winner != "" || entry.FinishReason != ""
			if entryStatus != "finished" && !isActuallyFinished {
				continue
			}
		} else {
			// For other status filters, use exact match
			if entryStatus != status {
				continue
			}
		}

		filtered = append(filtered, entry)
	}
	return filtered
}

func resolvedPublicStatusFilter(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "active":
		return "active"
	default:
		return "finished"
	}
}

// filterScopedArchivedMatchesByStatus is the player's-own-history variant: it
// filters by status only, and still drops aborted games (usually zero moves).
func filterScopedArchivedMatchesByStatus(matches []platform.MatchArchiveEntry, status string) []platform.MatchArchiveEntry {
	wanted := "finished"
	if resolvedPublicStatusFilter(status) == "active" {
		wanted = "active"
	}
	filtered := filterArchivedMatchesByStatus(matches, wanted)
	kept := make([]platform.MatchArchiveEntry, 0, len(filtered))
	for _, entry := range filtered {
		if strings.EqualFold(strings.TrimSpace(entry.FinishReason), "abort") {
			continue
		}
		if strings.TrimSpace(entry.MatchID) == "" {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

func filterPublicArchivedMatchesByStatus(matches []platform.MatchArchiveEntry, status string) []platform.MatchArchiveEntry {
	if resolvedPublicStatusFilter(status) == "active" {
		filtered := filterArchivedMatchesByStatus(matches, "active")
		public := make([]platform.MatchArchiveEntry, 0, len(filtered))
		for _, entry := range filtered {
			if platform.IsPublicLiveSpectateMatch(entry) {
				public = append(public, entry)
			}
		}
		return public
	}

	filtered := filterArchivedMatchesByStatus(matches, "finished")
	public := make([]platform.MatchArchiveEntry, 0, len(filtered))
	for _, entry := range filtered {
		if platform.IsPublicReplayableMatch(entry) {
			public = append(public, entry)
		}
	}
	return public
}

func finalizeArchivedRatedMatch(
	archive *platform.MatchArchiveStore,
	guests platform.GuestDirectory,
	accounts platform.AccountDirectory,
	matchID string,
) (map[string]any, int, error) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return nil, http.StatusBadRequest, errGuestResult("match id is required")
	}
	entry, ok := archive.Get(matchID)
	if !ok {
		return nil, http.StatusBadRequest, errGuestResult("unknown match archive")
	}
	entry = enrichArchiveEntry(accounts, entry)
	winner := strings.TrimSpace(entry.Winner)
	if err := validateArchivedRatedResult(entry, matchID, winner); err != nil {
		return nil, http.StatusBadRequest, err
	}

	whiteBefore, ok := guests.GetGuest(entry.WhiteGuestID)
	if !ok {
		return nil, http.StatusBadRequest, errGuestResult("unknown white guest")
	}
	blackBefore, ok := guests.GetGuest(entry.BlackGuestID)
	if !ok {
		return nil, http.StatusBadRequest, errGuestResult("unknown black guest")
	}

	white, black, guestChanged, err := guests.FinalizeMatch(matchID, entry.WhiteGuestID, entry.BlackGuestID, winner)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}

	var (
		whiteAccountProfile platform.PublicAccountProfile
		blackAccountProfile platform.PublicAccountProfile
		accountChanged      bool
	)
	_, whiteLinked := accounts.GetAccountByGuest(entry.WhiteGuestID)
	_, blackLinked := accounts.GetAccountByGuest(entry.BlackGuestID)
	if whiteLinked && blackLinked {
		whiteAccount, blackAccount, changed, err := finalizeLinkedAccounts(accounts, matchID, entry.WhiteGuestID, entry.BlackGuestID, winner, entry.Queue, entry.ModeID, whiteBefore, blackBefore)
		if err != nil {
			return nil, http.StatusBadRequest, errGuestResult("failed to finalize linked account result")
		}
		accountChanged = changed
		whiteAccountProfile = platform.BuildPublicAccountProfile(whiteAccount, guests)
		blackAccountProfile = platform.BuildPublicAccountProfile(blackAccount, guests)
	}

	return map[string]any{
		"changed":      guestChanged || accountChanged,
		"white":        white,
		"black":        black,
		"whiteAccount": whiteAccountProfile,
		"blackAccount": blackAccountProfile,
	}, http.StatusOK, nil
}

func validateArchivedRatedResult(entry platform.MatchArchiveEntry, matchID, winner string) error {
	if entry.Queue != "rated" {
		return errGuestResult("only rated matches can finalize guest results")
	}
	if entry.Status != "finished" {
		return errGuestResult("only finished matches can finalize guest results")
	}
	if entry.MatchID != matchID {
		return errGuestResult("match archive mismatch")
	}
	if entry.Winner != winner {
		return errGuestResult("winner does not match archived result")
	}
	return nil
}

func accountOwnsGuest(account platform.AccountProfile, guestID string) bool {
	if account.PrimaryGuestID == guestID {
		return true
	}
	for _, linkedGuestID := range account.LinkedGuestIDs {
		if linkedGuestID == guestID {
			return true
		}
	}
	return false
}

func finalizeLinkedAccounts(
	accounts platform.AccountDirectory,
	matchID, whiteGuestID, blackGuestID, winner, queue string,
	modeID contracts.MatchModeID,
	whiteBefore, blackBefore platform.GuestProfile,
) (platform.AccountProfile, platform.AccountProfile, bool, error) {
	if _, _, err := accounts.SyncGuestStats(whiteBefore); err != nil {
		return platform.AccountProfile{}, platform.AccountProfile{}, false, err
	}
	if _, _, err := accounts.SyncGuestStats(blackBefore); err != nil {
		return platform.AccountProfile{}, platform.AccountProfile{}, false, err
	}
	whiteAccount, ok := accounts.GetAccountByGuest(whiteGuestID)
	if !ok {
		return platform.AccountProfile{}, platform.AccountProfile{}, false, os.ErrNotExist
	}
	blackAccount, ok := accounts.GetAccountByGuest(blackGuestID)
	if !ok {
		return platform.AccountProfile{}, platform.AccountProfile{}, false, os.ErrNotExist
	}
	finalWhite, finalBlack, changed, err := accounts.FinalizeMatch(matchID, whiteAccount.AccountID, blackAccount.AccountID, winner, queue, modeID)
	if err != nil {
		return platform.AccountProfile{}, platform.AccountProfile{}, false, err
	}
	if changed {
		accountsCache.Invalidate()
	}
	return finalWhite, finalBlack, changed, nil
}

type errGuestResult string

func (e errGuestResult) Error() string {
	return string(e)
}

func parseOptionalModeID(raw string) contracts.MatchModeID {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	return contracts.NormalizeMatchModeID(raw)
}

func parseOptionalMatchStatus(raw string) string {
	switch strings.TrimSpace(raw) {
	case "active", "finished":
		return strings.TrimSpace(raw)
	default:
		return ""
	}
}

func normalizeAccountQuery(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func filterAccountsByQuery(accounts []platform.AccountProfile, query string) []platform.AccountProfile {
	if query == "" {
		return accounts
	}
	filtered := make([]platform.AccountProfile, 0, len(accounts))
	for _, account := range accounts {
		if strings.Contains(strings.ToLower(strings.TrimSpace(account.Handle)), query) {
			filtered = append(filtered, account)
		}
	}
	return filtered
}

func findAccountByHandle(accounts platform.AccountDirectory, handle string) (platform.AccountProfile, bool) {
	return accounts.FindAccountByHandle(handle)
}
