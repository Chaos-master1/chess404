package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/chess404/realtime/internal/httputil"
	"github.com/chess404/realtime/internal/platform"
)

// registerInternalRoutes wires the internal endpoints.
func registerInternalRoutes(mux *http.ServeMux, archive *platform.MatchArchiveStore, guests platform.GuestDirectory, accounts platform.AccountDirectory, moderation platform.ModerationDirectory) {
	mux.HandleFunc("/api/platform/internal/finalize-rated-match", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !requireInternalServiceRequest(w, r) {
			return
		}
		var payload struct {
			MatchID string `json:"matchId"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				respondError(w, http.StatusBadRequest, "invalid finalization payload")
				return
			}
		}
		response, status, err := finalizeArchivedRatedMatch(archive, guests, accounts, payload.MatchID)
		if err != nil {
			respondError(w, status, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("/api/platform/internal/account-restriction", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !requireInternalServiceRequest(w, r) {
			return
		}
		accountID := strings.TrimSpace(r.URL.Query().Get("accountId"))
		if accountID == "" {
			respondError(w, http.StatusBadRequest, "accountId is required")
			return
		}
		restriction, restricted := moderation.GetAccountRestriction(accountID)
		respondJSON(w, http.StatusOK, map[string]any{
			"restricted":      restricted,
			"restriction":     restriction,
			"restrictionKind": restriction.Kind,
			"restrictionId":   restriction.RestrictionID,
			"restrictionNote": restriction.Reason,
			"checkedAt":       httputil.NowUTC(),
		})
	})
}
