package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/chess404/realtime/internal/platform"
)

// registerGuestRoutes wires the guests endpoints.
func registerGuestRoutes(mux *http.ServeMux, archive *platform.MatchArchiveStore, guests platform.GuestDirectory, accounts platform.AccountDirectory) {
	mux.HandleFunc("/api/platform/guest-sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			GuestID       string `json:"guestId"`
			SessionSecret string `json:"sessionSecret"`
			SessionToken  string `json:"sessionToken"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				respondError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		}
		var session platform.GuestSession
		var err error
		switch {
		case strings.TrimSpace(payload.GuestID) != "" && strings.TrimSpace(payload.SessionToken) != "":
			session, err = guests.ResumeGuestByToken(payload.GuestID, payload.SessionToken)
			if err == platform.ErrUnauthorizedGuestSession && strings.TrimSpace(payload.SessionSecret) != "" {
				session, err = guests.EnsureGuest(payload.GuestID, payload.SessionSecret)
			}
		default:
			session, err = guests.EnsureGuest(payload.GuestID, payload.SessionSecret)
		}
		if err != nil {
			if err == platform.ErrUnauthorizedGuestSession {
				http.Error(w, `{"error":"unauthorized guest session"}`, http.StatusUnauthorized)
				return
			}
			log.Printf("ERROR: failed to create guest session for guestId=%q: %v", payload.GuestID, err)
			http.Error(w, `{"error":"failed to create guest session"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(session.IssuedView())
	})

	mux.HandleFunc("/api/platform/guest-results", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !requireInternalServiceRequest(w, r) {
			return
		}
		var payload struct {
			MatchID       string `json:"matchId"`
			GuestID       string `json:"guestId"`
			SessionSecret string `json:"sessionSecret"`
			SessionToken  string `json:"sessionToken"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid guest result payload"}`, http.StatusBadRequest)
				return
			}
		}
		session, err := resumeGuestFromPayload(guests, payload.GuestID, payload.SessionSecret, payload.SessionToken)
		if err != nil {
			switch err {
			case platform.ErrUnauthorizedGuestSession, os.ErrNotExist:
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			default:
				http.Error(w, `{"error":"unauthorized"}`, http.StatusBadRequest)
			}
			return
		}
		entry, ok := archive.Get(payload.MatchID)
		if !ok {
			http.Error(w, `{"error":"unknown match archive"}`, http.StatusBadRequest)
			return
		}
		winner := strings.TrimSpace(entry.Winner)
		if err := validateArchivedRatedResult(entry, payload.MatchID, winner); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		if session.Guest.GuestID != entry.WhiteGuestID && session.Guest.GuestID != entry.BlackGuestID {
			respondError(w, http.StatusForbidden, "guest does not own an archived rated seat")
			return
		}
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
		white, black, changed, err := guests.FinalizeMatch(payload.MatchID, entry.WhiteGuestID, entry.BlackGuestID, winner)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		var (
			whiteAccountProfile platform.PublicAccountProfile
			blackAccountProfile platform.PublicAccountProfile
			accountChanged      bool
		)
		_, whiteLinked := accounts.GetAccountByGuest(entry.WhiteGuestID)
		_, blackLinked := accounts.GetAccountByGuest(entry.BlackGuestID)
		if whiteLinked && blackLinked {
			if _, _, changed, err := finalizeLinkedAccounts(accounts, payload.MatchID, entry.WhiteGuestID, entry.BlackGuestID, winner, entry.Queue, entry.ModeID, whiteBefore, blackBefore); err != nil {
				respondError(w, http.StatusBadRequest, "failed to finalize linked account result")
				return
			} else {
				accountChanged = changed
			}
			if account, ok := accounts.GetAccountByGuest(entry.WhiteGuestID); ok {
				whiteAccountProfile = platform.BuildPublicAccountProfile(account, guests)
			}
			if account, ok := accounts.GetAccountByGuest(entry.BlackGuestID); ok {
				blackAccountProfile = platform.BuildPublicAccountProfile(account, guests)
			}
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"changed":      changed || accountChanged,
			"white":        white,
			"black":        black,
			"whiteAccount": whiteAccountProfile,
			"blackAccount": blackAccountProfile,
		})
	})

	mux.HandleFunc("/api/platform/guests", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"guests": guests.ListRecentGuests(platform.ParseListLimit(r.URL.Query().Get("limit"), 24)),
		})
	})

	mux.HandleFunc("/api/platform/guests/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		guestID := strings.TrimPrefix(r.URL.Path, "/api/platform/guests/")
		if guestID == "" {
			http.NotFound(w, r)
			return
		}
		guest, ok := guests.GetGuest(guestID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"guest": guest,
		})
	})
}
