package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/platform"
)

// registerChallengeRoutes wires the challenges endpoints.
func registerChallengeRoutes(mux *http.ServeMux, guests platform.GuestDirectory, accounts platform.AccountDirectory, friends platform.FriendshipDirectory, moderation platform.ModerationDirectory, challenges platform.DirectChallengeDirectory, notifications platform.AccountNotificationDirectory) {
	mux.HandleFunc("/api/platform/challenges/overview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID    string `json:"accountId"`
			SessionToken string `json:"sessionToken"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid challenge overview payload"}`, http.StatusBadRequest)
				return
			}
		}

		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}

		respondChallengeOverview(w, guests, accounts, challenges, session.Account.AccountID)
	})

	mux.HandleFunc("/api/platform/challenges/eligibility", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID       string `json:"accountId"`
			SessionToken    string `json:"sessionToken"`
			TargetAccountID string `json:"targetAccountId"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid challenge eligibility payload"}`, http.StatusBadRequest)
				return
			}
		}

		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}
		target, ok := accounts.GetAccount(strings.TrimSpace(payload.TargetAccountID))
		if !ok {
			http.Error(w, `{"error":"unknown target account"}`, http.StatusNotFound)
			return
		}
		if err := requireAccountInteractionAllowed(moderation, session.Account.AccountID, target.AccountID); err != nil {
			writeModerationError(w, err)
			return
		}
		if !friends.AreFriends(session.Account.AccountID, target.AccountID) {
			http.Error(w, `{"error":"direct challenges require an accepted friendship"}`, http.StatusForbidden)
			return
		}
		if err := challenges.CanCreateChallenge(session.Account.AccountID, target.AccountID); err != nil {
			switch err {
			case platform.ErrInvalidDirectChallenge:
				http.Error(w, `{"error":"invalid direct challenge"}`, http.StatusBadRequest)
			case platform.ErrDirectChallengeAlreadyExists:
				http.Error(w, `{"error":"a pending direct challenge already exists for this friend"}`, http.StatusConflict)
			default:
				http.Error(w, `{"error":"failed to validate direct challenge"}`, http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"viewer": platform.BuildPublicAccountProfile(session.Account, guests),
			"target": platform.BuildPublicAccountProfile(target, guests),
		})
	})

	mux.HandleFunc("/api/platform/challenges", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID       string                `json:"accountId"`
			SessionToken    string                `json:"sessionToken"`
			TargetAccountID string                `json:"targetAccountId"`
			MatchID         string                `json:"matchId"`
			ModeID          contracts.MatchModeID `json:"modeId"`
			ClockSeconds    int64                 `json:"clockSeconds"`
			ChallengerSeat  string                `json:"challengerSeat"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid challenge create payload"}`, http.StatusBadRequest)
				return
			}
		}

		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}
		target, ok := accounts.GetAccount(strings.TrimSpace(payload.TargetAccountID))
		if !ok {
			http.Error(w, `{"error":"unknown target account"}`, http.StatusNotFound)
			return
		}
		if err := requireAccountInteractionAllowed(moderation, session.Account.AccountID, target.AccountID); err != nil {
			writeModerationError(w, err)
			return
		}
		if !friends.AreFriends(session.Account.AccountID, target.AccountID) {
			http.Error(w, `{"error":"direct challenges require an accepted friendship"}`, http.StatusForbidden)
			return
		}
		challenge, err := challenges.CreateChallenge(session.Account.AccountID, target.AccountID, payload.MatchID, payload.ModeID, payload.ClockSeconds, payload.ChallengerSeat)
		if err != nil {
			switch err {
			case platform.ErrInvalidDirectChallenge:
				http.Error(w, `{"error":"invalid direct challenge"}`, http.StatusBadRequest)
			case platform.ErrDirectChallengeAlreadyExists:
				http.Error(w, `{"error":"a pending direct challenge already exists for this friend"}`, http.StatusConflict)
			default:
				http.Error(w, `{"error":"failed to create direct challenge"}`, http.StatusInternalServerError)
			}
			return
		}
		if _, err := notifications.CreateNotification(target.AccountID, session.Account.AccountID, platform.AccountNotificationKindDirectChallengeReceived, platform.AccountNotificationOptions{
			ChallengeID:    challenge.ChallengeID,
			MatchID:        challenge.MatchID,
			ModeID:         challenge.ModeID,
			ChallengerSeat: challenge.ChallengerSeat,
		}); err != nil {
			log.Printf("failed to create direct challenge notification for %s -> %s: %v", session.Account.AccountID, target.AccountID, err)
		}

		writeDirectChallengeView(w, guests, accounts, challenge, session.Account.AccountID)
	})

	mux.HandleFunc("/api/platform/challenges/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/platform/challenges/")
		if strings.TrimSpace(path) == "" {
			http.NotFound(w, r)
			return
		}
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		challengeID := strings.TrimSpace(parts[0])
		action := strings.TrimSpace(parts[1])
		if challengeID == "" {
			http.NotFound(w, r)
			return
		}

		switch action {
		case "view":
			var payload struct {
				AccountID    string `json:"accountId"`
				SessionToken string `json:"sessionToken"`
			}
			if r.Body != nil {
				defer r.Body.Close()
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					http.Error(w, `{"error":"invalid challenge view payload"}`, http.StatusBadRequest)
					return
				}
			}
			session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
			if !ok {
				return
			}
			challenge, ok := challenges.GetChallenge(challengeID)
			if !ok {
				http.Error(w, `{"error":"direct challenge not found"}`, http.StatusNotFound)
				return
			}
			if challenge.ChallengerAccountID != session.Account.AccountID && challenge.TargetAccountID != session.Account.AccountID {
				http.Error(w, `{"error":"unauthorized direct challenge"}`, http.StatusForbidden)
				return
			}
			if err := requireAccountInteractionAllowed(moderation, challenge.ChallengerAccountID, challenge.TargetAccountID); err != nil {
				writeModerationError(w, err)
				return
			}
			writeDirectChallengeView(w, guests, accounts, challenge, session.Account.AccountID)
		case "respond":
			var payload struct {
				AccountID    string `json:"accountId"`
				SessionToken string `json:"sessionToken"`
				Accept       bool   `json:"accept"`
			}
			if r.Body != nil {
				defer r.Body.Close()
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					http.Error(w, `{"error":"invalid challenge response payload"}`, http.StatusBadRequest)
					return
				}
			}
			session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
			if !ok {
				return
			}
			if existingChallenge, ok := challenges.GetChallenge(challengeID); ok {
				if err := requireAccountInteractionAllowed(moderation, existingChallenge.ChallengerAccountID, existingChallenge.TargetAccountID); err != nil {
					writeModerationError(w, err)
					return
				}
			}
			challenge, err := challenges.RespondToChallenge(session.Account.AccountID, challengeID, payload.Accept)
			if err != nil {
				switch err {
				case platform.ErrDirectChallengeNotFound:
					http.Error(w, `{"error":"direct challenge not found"}`, http.StatusNotFound)
				case platform.ErrUnauthorizedDirectChallenge:
					http.Error(w, `{"error":"unauthorized direct challenge"}`, http.StatusForbidden)
				case platform.ErrInvalidDirectChallenge:
					http.Error(w, `{"error":"invalid direct challenge"}`, http.StatusBadRequest)
				case platform.ErrDirectChallengeNotPending:
					http.Error(w, `{"error":"direct challenge is no longer pending"}`, http.StatusConflict)
				default:
					http.Error(w, `{"error":"failed to update direct challenge"}`, http.StatusInternalServerError)
				}
				return
			}
			notificationKind := platform.AccountNotificationKindDirectChallengeDeclined
			if payload.Accept {
				notificationKind = platform.AccountNotificationKindDirectChallengeAccepted
			}
			if _, err := notifications.CreateNotification(challenge.ChallengerAccountID, session.Account.AccountID, notificationKind, platform.AccountNotificationOptions{
				ChallengeID:    challenge.ChallengeID,
				MatchID:        challenge.MatchID,
				ModeID:         challenge.ModeID,
				ChallengerSeat: challenge.ChallengerSeat,
			}); err != nil {
				log.Printf("failed to create challenge response notification for %s -> %s: %v", session.Account.AccountID, challenge.ChallengerAccountID, err)
			}
			writeDirectChallengeView(w, guests, accounts, challenge, session.Account.AccountID)
		case "cancel":
			var payload struct {
				AccountID    string `json:"accountId"`
				SessionToken string `json:"sessionToken"`
			}
			if r.Body != nil {
				defer r.Body.Close()
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					http.Error(w, `{"error":"invalid challenge cancel payload"}`, http.StatusBadRequest)
					return
				}
			}
			session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
			if !ok {
				return
			}
			if existingChallenge, ok := challenges.GetChallenge(challengeID); ok {
				if err := requireAccountInteractionAllowed(moderation, existingChallenge.ChallengerAccountID, existingChallenge.TargetAccountID); err != nil {
					writeModerationError(w, err)
					return
				}
			}
			challenge, err := challenges.CancelChallenge(session.Account.AccountID, challengeID)
			if err != nil {
				switch err {
				case platform.ErrDirectChallengeNotFound:
					http.Error(w, `{"error":"direct challenge not found"}`, http.StatusNotFound)
				case platform.ErrUnauthorizedDirectChallenge:
					http.Error(w, `{"error":"unauthorized direct challenge"}`, http.StatusForbidden)
				case platform.ErrInvalidDirectChallenge:
					http.Error(w, `{"error":"invalid direct challenge"}`, http.StatusBadRequest)
				case platform.ErrDirectChallengeNotPending:
					http.Error(w, `{"error":"direct challenge is no longer pending"}`, http.StatusConflict)
				default:
					http.Error(w, `{"error":"failed to cancel direct challenge"}`, http.StatusInternalServerError)
				}
				return
			}
			if _, err := notifications.CreateNotification(challenge.TargetAccountID, session.Account.AccountID, platform.AccountNotificationKindDirectChallengeCanceled, platform.AccountNotificationOptions{
				ChallengeID:    challenge.ChallengeID,
				MatchID:        challenge.MatchID,
				ModeID:         challenge.ModeID,
				ChallengerSeat: challenge.ChallengerSeat,
			}); err != nil {
				log.Printf("failed to create challenge cancellation notification for %s -> %s: %v", session.Account.AccountID, challenge.TargetAccountID, err)
			}
			writeDirectChallengeView(w, guests, accounts, challenge, session.Account.AccountID)
		default:
			http.NotFound(w, r)
		}
	})
}
