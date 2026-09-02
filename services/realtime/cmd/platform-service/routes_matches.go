package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/httputil"
	"github.com/chess404/realtime/internal/platform"
)

// registerMatchArchiveRoutes wires the matches endpoints.
func registerMatchArchiveRoutes(mux *http.ServeMux, archive *platform.MatchArchiveStore, accounts platform.AccountDirectory) {
	mux.HandleFunc("/api/platform/matches", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			// Only the gateway writes archive rows. This used to be gated by
			// "the snapshot must carry a seat secret", which stopped being a
			// check at all once match-service began redacting secrets from every
			// snapshot it hands out: the gateway then forwarded a redacted
			// snapshot and every single archive write failed with 400, so
			// nothing was ever archived -- no history, no replays, no results.
			if !requireInternalServiceRequest(w, r) {
				return
			}
			var snapshot contracts.MatchSnapshotResponse
			if r.Body != nil {
				defer r.Body.Close()
				r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
				if err := json.NewDecoder(r.Body).Decode(&snapshot); err != nil {
					http.Error(w, `{"error":"invalid match snapshot payload"}`, http.StatusBadRequest)
					return
				}
			}
			if strings.TrimSpace(snapshot.Match.MatchID) == "" {
				http.Error(w, `{"error":"matchId is required"}`, http.StatusBadRequest)
				return
			}
			if err := archive.Upsert(snapshot); err != nil {
				log.Printf("platform:match-sync: upsert failed matchID=%s err=%v", snapshot.Match.MatchID, err)
				http.Error(w, `{"error":"failed to persist match snapshot"}`, http.StatusInternalServerError)
				return
			}
			log.Printf("platform:match-sync: stored matchID=%s modeID=%s whiteGuestID=%s", snapshot.Match.MatchID, snapshot.Match.ModeID, snapshot.Match.WhiteGuestID)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":      "ok",
				"matchId":     snapshot.Match.MatchID,
				"persistedAt": httputil.NowUTC(),
			})
		case http.MethodGet:
			guestID := r.URL.Query().Get("guestId")
			accountID := r.URL.Query().Get("accountId")
			seasonID := strings.TrimSpace(r.URL.Query().Get("seasonId"))
			modeID := parseOptionalModeID(r.URL.Query().Get("modeId"))
			statusFilter := parseOptionalMatchStatus(r.URL.Query().Get("status"))
			var matches []platform.MatchArchiveEntry
			// A query scoped to one player is that player's own history, not the
			// public spectacle feed: it must include their vs-computer and
			// private-invite games, which the public predicate strips out.
			scoped := accountID != "" || guestID != ""
			if accountID != "" {
				account, ok := accounts.GetAccount(accountID)
				if ok {
					matches = archive.ListByAccount(account.AccountID, account.LinkedGuestIDs, platform.ParseListLimit(r.URL.Query().Get("limit"), 20))
				} else {
					matches = []platform.MatchArchiveEntry{}
				}
			} else if guestID != "" {
				matches = archive.ListByGuest(guestID, platform.ParseListLimit(r.URL.Query().Get("limit"), 20))
			} else {
				matches = archive.List(platform.ParseListLimit(r.URL.Query().Get("limit"), 20))
			}
			for i := range matches {
				matches[i] = enrichArchiveEntry(accounts, matches[i])
			}
			if seasonID != "" {
				matches = filterArchivedMatchesBySeason(matches, seasonID)
			}
			if modeID != "" {
				matches = filterArchivedMatchesByMode(matches, modeID)
			}
			if scoped {
				matches = filterScopedArchivedMatchesByStatus(matches, statusFilter)
			} else {
				matches = filterPublicArchivedMatchesByStatus(matches, statusFilter)
			}
			publicMatches := make([]platform.PublicMatchArchiveEntry, 0, len(matches))
			for _, match := range matches {
				publicMatches = append(publicMatches, platform.BuildPublicMatchArchiveEntry(match))
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"matches":          publicMatches,
				"selectedSeasonId": seasonID,
				"selectedModeId":   modeID,
				"selectedStatus":   resolvedPublicStatusFilter(statusFilter),
			})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/platform/matches/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		matchID := strings.TrimPrefix(r.URL.Path, "/api/platform/matches/")
		if matchID == "" {
			http.NotFound(w, r)
			return
		}
		entry, ok := archive.Get(matchID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		entry = enrichArchiveEntry(accounts, entry)
		// A participant may open their own finished game, including the
		// vs-computer and private ones the public replay rule excludes --
		// otherwise their own history lists matches they cannot open. Same
		// trust level as the guest-scoped list query above.
		viewerGuestID := strings.TrimSpace(r.URL.Query().Get("guestId"))
		ownGame := viewerGuestID != "" &&
			(viewerGuestID == strings.TrimSpace(entry.WhiteGuestID) || viewerGuestID == strings.TrimSpace(entry.BlackGuestID)) &&
			strings.EqualFold(strings.TrimSpace(entry.Status), "finished")
		if !ownGame && !platform.IsPublicReplayableMatch(entry) {
			respondError(w, http.StatusNotFound, "public replay is only available for finished matches")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(platform.BuildPublicMatchArchiveEntry(entry))
	})
}
