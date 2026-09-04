package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/chess404/realtime/internal/platform"
)

// registerModerationRoutes wires the moderation endpoints.
func registerModerationRoutes(mux *http.ServeMux, guests platform.GuestDirectory, accounts platform.AccountDirectory, friends platform.FriendshipDirectory, moderation platform.ModerationDirectory, challenges platform.DirectChallengeDirectory, notifications platform.AccountNotificationDirectory, securityAudit platform.AccountSecurityAuditDirectory) {
	mux.HandleFunc("/api/platform/moderation/overview", func(w http.ResponseWriter, r *http.Request) {
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
				http.Error(w, `{"error":"invalid moderation overview payload"}`, http.StatusBadRequest)
				return
			}
		}

		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}
		respondModerationOverview(w, guests, accounts, moderation, session.Account.AccountID)
	})

	mux.HandleFunc("/api/platform/moderation/blocks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID       string `json:"accountId"`
			SessionToken    string `json:"sessionToken"`
			TargetAccountID string `json:"targetAccountId"`
			Reason          string `json:"reason"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid account block payload"}`, http.StatusBadRequest)
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
		if _, err := moderation.BlockAccount(session.Account.AccountID, target.AccountID, payload.Reason); err != nil {
			writeModerationError(w, err)
			return
		}
		if err := friends.RemoveFriend(session.Account.AccountID, target.AccountID); err != nil && err != platform.ErrInvalidFriendRequest {
			http.Error(w, `{"error":"failed to remove social connection"}`, http.StatusInternalServerError)
			return
		}
		if err := challenges.PurgePair(session.Account.AccountID, target.AccountID); err != nil && err != platform.ErrInvalidDirectChallenge {
			http.Error(w, `{"error":"failed to purge pending direct challenges"}`, http.StatusInternalServerError)
			return
		}
		if err := notifications.PurgePair(session.Account.AccountID, target.AccountID); err != nil && err != platform.ErrInvalidAccountNotification {
			log.Printf("failed to purge notifications for blocked pair %s/%s: %v", session.Account.AccountID, target.AccountID, err)
		}
		respondModerationOverview(w, guests, accounts, moderation, session.Account.AccountID)
	})

	mux.HandleFunc("/api/platform/moderation/blocks/remove", func(w http.ResponseWriter, r *http.Request) {
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
				http.Error(w, `{"error":"invalid account unblock payload"}`, http.StatusBadRequest)
				return
			}
		}

		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}
		if err := moderation.UnblockAccount(session.Account.AccountID, payload.TargetAccountID); err != nil {
			writeModerationError(w, err)
			return
		}
		respondModerationOverview(w, guests, accounts, moderation, session.Account.AccountID)
	})

	mux.HandleFunc("/api/platform/moderation/reports", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID       string `json:"accountId"`
			SessionToken    string `json:"sessionToken"`
			TargetAccountID string `json:"targetAccountId"`
			Category        string `json:"category"`
			Details         string `json:"details"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid player report payload"}`, http.StatusBadRequest)
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
		if _, err := moderation.CreateReport(session.Account.AccountID, target.AccountID, payload.Category, payload.Details); err != nil {
			writeModerationError(w, err)
			return
		}
		respondModerationOverview(w, guests, accounts, moderation, session.Account.AccountID)
	})

	mux.HandleFunc("/api/platform/moderation/admin/overview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID    string `json:"accountId"`
			SessionToken string `json:"sessionToken"`
			Limit        int    `json:"limit"`
			Status       string `json:"status"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid moderation admin overview payload"}`, http.StatusBadRequest)
				return
			}
		}
		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}
		if !isModerationAdminAccount(session.Account) {
			writeModerationAdminAuthError(w)
			return
		}
		respondModerationAdminOverview(w, guests, accounts, moderation, session.Account.AccountID, payload.Limit, payload.Status)
	})

	mux.HandleFunc("/api/platform/moderation/admin/reports/resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID    string `json:"accountId"`
			SessionToken string `json:"sessionToken"`
			ReportID     string `json:"reportId"`
			Action       string `json:"action"`
			Restriction  string `json:"restriction"`
			Note         string `json:"note"`
			Limit        int    `json:"limit"`
			Status       string `json:"status"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid moderation admin resolution payload"}`, http.StatusBadRequest)
				return
			}
		}
		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}
		if !isModerationAdminAccount(session.Account) {
			writeModerationAdminAuthError(w)
			return
		}
		resolvedReport, _, err := moderation.ResolveReport(session.Account.AccountID, payload.ReportID, payload.Action, payload.Note)
		if err != nil {
			writeModerationError(w, err)
			return
		}
		switch strings.TrimSpace(strings.ToLower(payload.Restriction)) {
		case "", "none":
		case "clear":
			if err := moderation.ClearAccountRestriction(resolvedReport.TargetAccountID); err != nil && err != platform.ErrAccountRestrictionNotFound {
				writeModerationError(w, err)
				return
			}
		default:
			if _, err := moderation.SetAccountRestriction(session.Account.AccountID, resolvedReport.TargetAccountID, payload.Restriction, payload.Note, resolvedReport.ReportID); err != nil {
				writeModerationError(w, err)
				return
			}
		}
		recordAccountSecurityEvent(securityAudit, session.Account.AccountID, platform.AccountSecurityEventKindModeratorReviewRecorded, resolvedReport.ReportID)
		respondModerationAdminOverview(w, guests, accounts, moderation, session.Account.AccountID, payload.Limit, payload.Status)
	})
}
