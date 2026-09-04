package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/platform"
)

// registerMatchClaimRoutes wires the match claims endpoints.
func registerMatchClaimRoutes(mux *http.ServeMux, archive *platform.MatchArchiveStore, guests platform.GuestDirectory, claims *platform.MatchClaimStore) {
	mux.HandleFunc("/api/platform/match-claims", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			MatchID       string `json:"matchId"`
			GuestID       string `json:"guestId"`
			SessionSecret string `json:"sessionSecret"`
			SessionToken  string `json:"sessionToken"`
			SeatColor     string `json:"seatColor,omitempty"`
			PlayerSecret  string `json:"playerSecret,omitempty"`
			WhiteGuestID  string `json:"whiteGuestId,omitempty"`
			BlackGuestID  string `json:"blackGuestId,omitempty"`
			WhiteName     string `json:"whiteName,omitempty"`
			BlackName     string `json:"blackName,omitempty"`
			Queue         string `json:"queue,omitempty"`
			ModeID        string `json:"modeId,omitempty"`
			MatchStatus   string `json:"matchStatus,omitempty"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid match claim payload"}`, http.StatusBadRequest)
				return
			}
		}

		session, err := resumeGuestFromPayload(guests, payload.GuestID, payload.SessionSecret, payload.SessionToken)
		if err != nil {
			switch err {
			case platform.ErrUnauthorizedGuestSession:
				http.Error(w, `{"error":"unauthorized guest session"}`, http.StatusUnauthorized)
			case os.ErrNotExist:
				http.Error(w, `{"error":"unknown guest"}`, http.StatusNotFound)
			default:
				http.Error(w, `{"error":"failed to resume guest session"}`, http.StatusBadRequest)
			}
			return
		}

		if claim, ok := claims.Get(payload.MatchID, session.Guest.GuestID); ok {
			claim, ok = refreshStoredMatchClaim(archive, claims, claim, session.SessionSecret)
			if !ok {
				http.Error(w, `{"error":"unknown active match claim"}`, http.StatusNotFound)
				return
			}
			if strings.TrimSpace(claim.PlayerSecret) == "" {
				claim.PlayerSecret = session.SessionSecret
			}
			if err := claims.Put(claim); err == nil {
				if renewedClaim, renewed := claims.Get(payload.MatchID, session.Guest.GuestID); renewed {
					claim = renewedClaim
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(claim.IssuedView())
			return
		}

		matchState, _, archiveOk := archive.LoadMatch(payload.MatchID)
		if archiveOk {
			if !isRecoverableMatchStatus(matchState.Status) {
				http.Error(w, `{"error":"match is no longer active"}`, http.StatusNotFound)
				return
			}
			claim, ok := buildMatchSeatClaim(matchState, session.Guest.GuestID, session.SessionSecret)
			if !ok {
				http.Error(w, `{"error":"guest does not own a seat in this match"}`, http.StatusForbidden)
				return
			}
			if err := claims.Put(claim); err != nil {
				http.Error(w, `{"error":"failed to cache match claim"}`, http.StatusInternalServerError)
				return
			}
			if storedClaim, ok := claims.Get(claim.MatchID, claim.GuestID); ok {
				claim = storedClaim
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(claim.IssuedView())
			return
		}

		if payload.SeatColor == "" || payload.MatchStatus == "" {
			http.Error(w, `{"error":"unknown match archive"}`, http.StatusNotFound)
			return
		}
		if !isRecoverableMatchStatus(payload.MatchStatus) {
			http.Error(w, `{"error":"match is no longer active"}`, http.StatusNotFound)
			return
		}
		seatColor := strings.ToLower(strings.TrimSpace(payload.SeatColor))
		if seatColor != "white" && seatColor != "black" {
			http.Error(w, `{"error":"guest does not own a seat in this match"}`, http.StatusForbidden)
			return
		}
		playerSecret := strings.TrimSpace(payload.PlayerSecret)
		if playerSecret == "" {
			playerSecret = session.SessionSecret
		}
		var ownerGuestID string
		if seatColor == "white" {
			ownerGuestID = payload.WhiteGuestID
		} else {
			ownerGuestID = payload.BlackGuestID
		}
		if session.Guest.GuestID != ownerGuestID {
			http.Error(w, `{"error":"guest does not own a seat in this match"}`, http.StatusForbidden)
			return
		}
		claim := platform.MatchSeatClaim{
			MatchID:      payload.MatchID,
			GuestID:      session.Guest.GuestID,
			SeatColor:    seatColor,
			PlayerID:     session.Guest.GuestID,
			PlayerSecret: playerSecret,
			Queue:        strings.TrimSpace(payload.Queue),
			ModeID:       contracts.MatchModeID(strings.TrimSpace(payload.ModeID)),
			WhiteGuestID: strings.TrimSpace(payload.WhiteGuestID),
			BlackGuestID: strings.TrimSpace(payload.BlackGuestID),
			WhiteName:    strings.TrimSpace(payload.WhiteName),
			BlackName:    strings.TrimSpace(payload.BlackName),
		}
		if err := claims.Put(claim); err != nil {
			http.Error(w, `{"error":"failed to cache match claim"}`, http.StatusInternalServerError)
			return
		}
		if storedClaim, ok := claims.Get(claim.MatchID, claim.GuestID); ok {
			claim = storedClaim
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(claim.IssuedView())
	})

	mux.HandleFunc("/api/platform/match-claims/resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			MatchID    string `json:"matchId"`
			ClaimToken string `json:"claimToken"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid match claim resolve payload"}`, http.StatusBadRequest)
				return
			}
		}
		if strings.TrimSpace(payload.MatchID) == "" || strings.TrimSpace(payload.ClaimToken) == "" {
			http.Error(w, `{"error":"matchId and claimToken are required"}`, http.StatusBadRequest)
			return
		}

		claim, ok := claims.GetByToken(payload.MatchID, payload.ClaimToken)
		if !ok {
			http.Error(w, `{"error":"unknown room claim token"}`, http.StatusNotFound)
			return
		}
		claim, ok = refreshStoredMatchClaim(archive, claims, claim, claim.PlayerSecret)
		if !ok {
			http.Error(w, `{"error":"unknown room claim token"}`, http.StatusNotFound)
			return
		}
		if err := claims.Put(claim); err == nil {
			if renewedClaim, renewed := claims.Get(payload.MatchID, claim.GuestID); renewed {
				claim = renewedClaim
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(claim.IssuedView())
	})

	mux.HandleFunc("/api/platform/match-claims/active", func(w http.ResponseWriter, r *http.Request) {
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
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid active match claim payload"}`, http.StatusBadRequest)
				return
			}
		}

		session, err := resumeGuestFromPayload(guests, payload.GuestID, payload.SessionSecret, payload.SessionToken)
		if err != nil {
			switch err {
			case platform.ErrUnauthorizedGuestSession:
				http.Error(w, `{"error":"unauthorized guest session"}`, http.StatusUnauthorized)
			case os.ErrNotExist:
				http.Error(w, `{"error":"unknown guest"}`, http.StatusNotFound)
			default:
				http.Error(w, `{"error":"failed to resume guest session"}`, http.StatusBadRequest)
			}
			return
		}

		for retries := 0; retries < 3; retries++ {
			claim, ok := claims.FindByGuest(session.Guest.GuestID)
			if !ok {
				http.Error(w, `{"error":"no active match claim"}`, http.StatusNotFound)
				return
			}
			claim, ok = refreshStoredMatchClaim(archive, claims, claim, session.SessionSecret)
			if !ok {
				continue
			}
			if strings.TrimSpace(claim.PlayerSecret) == "" {
				claim.PlayerSecret = session.SessionSecret
			}
			if err := claims.Put(claim); err == nil {
				if renewedClaim, renewed := claims.Get(claim.MatchID, claim.GuestID); renewed {
					claim = renewedClaim
				}
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(claim.IssuedView())
			return
		}
		http.Error(w, `{"error":"failed to refresh match claim"}`, http.StatusInternalServerError)
	})
}
