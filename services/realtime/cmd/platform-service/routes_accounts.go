package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/chess404/realtime/internal/platform"
)

// registerAccountRoutes wires the accounts endpoints.
func registerAccountRoutes(mux *http.ServeMux, archive *platform.MatchArchiveStore, guests platform.GuestDirectory, accounts platform.AccountDirectory, moderation platform.ModerationDirectory) {
	mux.HandleFunc("/api/platform/accounts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		limit := platform.ParseListLimit(r.URL.Query().Get("limit"), 24)
		sortMode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))
		seasonID := strings.TrimSpace(r.URL.Query().Get("seasonId"))
		modeID := parseOptionalModeID(r.URL.Query().Get("modeId"))
		query := normalizeAccountQuery(r.URL.Query().Get("query"))

		cacheKey := fmt.Sprintf("%s|%s|%s|%s|%d", sortMode, seasonID, modeID, query, limit)
		if cached, ok := accountsCache.Get(cacheKey); ok {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			_ = json.NewEncoder(w).Encode(cached)
			return
		}

		accountItems := filterAccountsByQuery(accounts.ListAccounts(limit), query)
		seasonOptions := platform.BuildAvailableSeasonOptionsForMode(accountItems, modeID)
		accountsList := make([]platform.PublicAccountProfile, 0, len(accountItems))
		for _, account := range accountItems {
			profile := platform.BuildPublicAccountProfileForSeasonAndMode(account, guests, seasonID, modeID)
			if seasonID != "" && profile.SelectedSeason == nil {
				continue
			}
			if modeID != "" && profile.MatchesPlayed == 0 {
				continue
			}
			accountsList = append(accountsList, profile)
		}
		if sortMode == "rating" {
			if seasonID != "" {
				platform.SortPublicAccountsBySelectedSeason(accountsList)
			} else {
				platform.SortPublicAccountsByRating(accountsList)
			}
		}
		if limit > 0 && len(accountsList) > limit {
			accountsList = accountsList[:limit]
		}
		summary := platform.BuildAccountLeaderboardSummary(accountsList, seasonID, modeID)
		result := map[string]any{
			"accounts":         accountsList,
			"seasons":          seasonOptions,
			"summary":          summary,
			"selectedSeasonId": seasonID,
			"selectedModeId":   modeID,
			"selectedQuery":    query,
		}
		accountsCache.Set(cacheKey, result)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "MISS")
		_ = json.NewEncoder(w).Encode(result)
	})

	mux.HandleFunc("/api/platform/accounts/by-guest/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		guestID := strings.TrimPrefix(r.URL.Path, "/api/platform/accounts/by-guest/")
		if guestID == "" {
			http.NotFound(w, r)
			return
		}
		account, ok := accounts.GetAccountByGuest(guestID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		seasonID := strings.TrimSpace(r.URL.Query().Get("seasonId"))
		modeID := parseOptionalModeID(r.URL.Query().Get("modeId"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account": platform.BuildDetailedPublicAccountProfileForSeasonAndMode(account, guests, seasonID, modeID),
		})
	})

	mux.HandleFunc("/api/platform/accounts/by-handle/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handlePath := strings.TrimPrefix(r.URL.Path, "/api/platform/accounts/by-handle/")
		if handlePath == "" {
			http.NotFound(w, r)
			return
		}
		resolvedHandle, err := url.PathUnescape(handlePath)
		if err != nil {
			http.Error(w, `{"error":"invalid account handle"}`, http.StatusBadRequest)
			return
		}
		account, ok := findAccountByHandle(accounts, resolvedHandle)
		if !ok {
			http.NotFound(w, r)
			return
		}
		seasonID := strings.TrimSpace(r.URL.Query().Get("seasonId"))
		modeID := parseOptionalModeID(r.URL.Query().Get("modeId"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account": platform.BuildDetailedPublicAccountProfileForSeasonAndMode(account, guests, seasonID, modeID),
		})
	})

	mux.HandleFunc("/api/platform/accounts/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		accountID := strings.TrimPrefix(r.URL.Path, "/api/platform/accounts/")
		if accountID == "" {
			http.NotFound(w, r)
			return
		}
		account, ok := accounts.GetAccount(accountID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		seasonID := strings.TrimSpace(r.URL.Query().Get("seasonId"))
		modeID := parseOptionalModeID(r.URL.Query().Get("modeId"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account": platform.BuildDetailedPublicAccountProfileForSeasonAndMode(account, guests, seasonID, modeID),
		})
	})

	mux.HandleFunc("/api/platform/account-results", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !requireInternalServiceRequest(w, r) {
			return
		}
		var payload struct {
			MatchID      string `json:"matchId"`
			AccountID    string `json:"accountId"`
			SessionToken string `json:"sessionToken"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid account result payload"}`, http.StatusBadRequest)
				return
			}
		}
		accountSession, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}

		entry, ok := archive.Get(payload.MatchID)
		if !ok {
			http.Error(w, `{"error":"unknown match archive"}`, http.StatusBadRequest)
			return
		}
		entry = enrichArchiveEntry(accounts, entry)

		whiteBefore, ok := guests.GetGuest(entry.WhiteGuestID)
		if !ok {
			http.Error(w, `{"error":"unknown white guest"}`, http.StatusBadRequest)
			return
		}
		blackBefore, ok := guests.GetGuest(entry.BlackGuestID)
		if !ok {
			http.Error(w, `{"error":"unknown black guest"}`, http.StatusBadRequest)
			return
		}
		winner := strings.TrimSpace(entry.Winner)
		if err := validateArchivedRatedResult(entry, payload.MatchID, winner); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !accountOwnsGuest(accountSession.Account, entry.WhiteGuestID) && !accountOwnsGuest(accountSession.Account, entry.BlackGuestID) {
			respondError(w, http.StatusForbidden, "account does not own an archived rated seat")
			return
		}
		white, black, guestChanged, err := guests.FinalizeMatch(payload.MatchID, entry.WhiteGuestID, entry.BlackGuestID, winner)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		whiteAccount, blackAccount, accountChanged, err := finalizeLinkedAccounts(accounts, payload.MatchID, entry.WhiteGuestID, entry.BlackGuestID, winner, entry.Queue, entry.ModeID, whiteBefore, blackBefore)
		if err != nil {
			respondError(w, http.StatusBadRequest, "failed to finalize account result")
			return
		}

		respondJSON(w, http.StatusOK, map[string]any{
			"changed":      guestChanged || accountChanged,
			"white":        white,
			"black":        black,
			"whiteAccount": platform.BuildPublicAccountProfile(whiteAccount, guests),
			"blackAccount": platform.BuildPublicAccountProfile(blackAccount, guests),
		})
	})

	mux.HandleFunc("/api/platform/rankings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"players": guests.ListGuests(platform.ParseListLimit(r.URL.Query().Get("limit"), 20)),
		})
	})
}
