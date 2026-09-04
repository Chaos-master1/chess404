package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/chess404/realtime/internal/platform"
)

// registerFriendRoutes wires the friends endpoints.
func registerFriendRoutes(mux *http.ServeMux, guests platform.GuestDirectory, accounts platform.AccountDirectory, friends platform.FriendshipDirectory, moderation platform.ModerationDirectory, notifications platform.AccountNotificationDirectory) {
	mux.HandleFunc("/api/platform/friends/overview", func(w http.ResponseWriter, r *http.Request) {
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
				http.Error(w, `{"error":"invalid friend overview payload"}`, http.StatusBadRequest)
				return
			}
		}

		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}

		respondFriendOverview(w, guests, accounts, friends, session.Account.AccountID)
	})

	mux.HandleFunc("/api/platform/friends/requests", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID    string `json:"accountId"`
			SessionToken string `json:"sessionToken"`
			TargetHandle string `json:"targetHandle"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid friend request payload"}`, http.StatusBadRequest)
				return
			}
		}

		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}
		target, ok := findAccountByHandle(accounts, payload.TargetHandle)
		if !ok {
			http.Error(w, `{"error":"unknown target handle"}`, http.StatusNotFound)
			return
		}
		if err := requireAccountInteractionAllowed(moderation, session.Account.AccountID, target.AccountID); err != nil {
			writeModerationError(w, err)
			return
		}
		request, err := friends.SendRequest(session.Account.AccountID, target.AccountID)
		if err != nil {
			switch err {
			case platform.ErrInvalidFriendRequest:
				http.Error(w, `{"error":"invalid friend request"}`, http.StatusBadRequest)
			case platform.ErrAlreadyFriends:
				http.Error(w, `{"error":"accounts are already friends"}`, http.StatusConflict)
			case platform.ErrFriendRequestAlreadyExists:
				http.Error(w, `{"error":"friend request already exists"}`, http.StatusConflict)
			default:
				http.Error(w, `{"error":"failed to send friend request"}`, http.StatusInternalServerError)
			}
			return
		}
		notificationKind := platform.AccountNotificationKindFriendRequestReceived
		if request.Status == platform.FriendRequestStatusAccepted {
			notificationKind = platform.AccountNotificationKindFriendRequestAccepted
		}
		if _, err := notifications.CreateNotification(target.AccountID, session.Account.AccountID, notificationKind, platform.AccountNotificationOptions{
			FriendRequestID: request.RequestID,
		}); err != nil {
			log.Printf("failed to create friend request notification for %s -> %s: %v", session.Account.AccountID, target.AccountID, err)
		}

		respondFriendOverview(w, guests, accounts, friends, session.Account.AccountID)
	})

	mux.HandleFunc("/api/platform/friends/requests/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/respond") {
			http.NotFound(w, r)
			return
		}
		requestID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/platform/friends/requests/"), "/respond")
		if strings.TrimSpace(requestID) == "" {
			http.NotFound(w, r)
			return
		}
		var payload struct {
			AccountID    string `json:"accountId"`
			SessionToken string `json:"sessionToken"`
			Accept       bool   `json:"accept"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid friend response payload"}`, http.StatusBadRequest)
				return
			}
		}

		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}
		requestOverview := friends.ListOverview(session.Account.AccountID)
		requestTargetAccountID := findFriendRequestCounterpartyAccountID(requestOverview, requestID, session.Account.AccountID)
		if requestTargetAccountID == "" {
			http.Error(w, `{"error":"friend request not found"}`, http.StatusNotFound)
			return
		}
		if err := requireAccountInteractionAllowed(moderation, session.Account.AccountID, requestTargetAccountID); err != nil {
			writeModerationError(w, err)
			return
		}
		request, err := friends.RespondToRequest(session.Account.AccountID, requestID, payload.Accept)
		if err != nil {
			switch err {
			case platform.ErrFriendRequestNotFound:
				http.Error(w, `{"error":"friend request not found"}`, http.StatusNotFound)
			case platform.ErrUnauthorizedFriendRequest:
				http.Error(w, `{"error":"unauthorized friend request"}`, http.StatusForbidden)
			case platform.ErrInvalidFriendRequest:
				http.Error(w, `{"error":"invalid friend request"}`, http.StatusBadRequest)
			default:
				http.Error(w, `{"error":"failed to update friend request"}`, http.StatusInternalServerError)
			}
			return
		}
		if payload.Accept {
			if _, err := notifications.CreateNotification(requestTargetAccountID, session.Account.AccountID, platform.AccountNotificationKindFriendRequestAccepted, platform.AccountNotificationOptions{
				FriendRequestID: request.RequestID,
			}); err != nil {
				log.Printf("failed to create friend acceptance notification for %s -> %s: %v", session.Account.AccountID, requestTargetAccountID, err)
			}
		}

		respondFriendOverview(w, guests, accounts, friends, session.Account.AccountID)
	})

	mux.HandleFunc("/api/platform/friends/remove", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID       string `json:"accountId"`
			SessionToken    string `json:"sessionToken"`
			FriendAccountID string `json:"friendAccountId"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid remove friend payload"}`, http.StatusBadRequest)
				return
			}
		}

		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}
		if err := friends.RemoveFriend(session.Account.AccountID, payload.FriendAccountID); err != nil {
			switch err {
			case platform.ErrInvalidFriendRequest:
				http.Error(w, `{"error":"invalid friend removal"}`, http.StatusBadRequest)
			default:
				http.Error(w, `{"error":"failed to remove friend"}`, http.StatusInternalServerError)
			}
			return
		}

		respondFriendOverview(w, guests, accounts, friends, session.Account.AccountID)
	})
}
