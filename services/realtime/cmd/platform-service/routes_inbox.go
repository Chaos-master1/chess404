package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/chess404/realtime/internal/platform"
)

// registerInboxRoutes wires the inbox endpoints.
func registerInboxRoutes(mux *http.ServeMux, guests platform.GuestDirectory, accounts platform.AccountDirectory, moderation platform.ModerationDirectory, notifications platform.AccountNotificationDirectory) {
	mux.HandleFunc("/api/platform/inbox/overview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID    string `json:"accountId"`
			SessionToken string `json:"sessionToken"`
			Limit        int    `json:"limit"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid inbox overview payload"}`, http.StatusBadRequest)
				return
			}
		}
		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}
		respondNotificationOverview(w, guests, accounts, notifications, session.Account.AccountID, payload.Limit)
	})

	mux.HandleFunc("/api/platform/inbox/read", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID      string `json:"accountId"`
			SessionToken   string `json:"sessionToken"`
			NotificationID string `json:"notificationId"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid inbox read payload"}`, http.StatusBadRequest)
				return
			}
		}
		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}
		if _, err := notifications.MarkRead(session.Account.AccountID, payload.NotificationID); err != nil {
			writeNotificationError(w, err)
			return
		}
		respondNotificationOverview(w, guests, accounts, notifications, session.Account.AccountID, 48)
	})

	mux.HandleFunc("/api/platform/inbox/read-all", func(w http.ResponseWriter, r *http.Request) {
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
				http.Error(w, `{"error":"invalid inbox read-all payload"}`, http.StatusBadRequest)
				return
			}
		}
		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}
		if _, err := notifications.MarkAllRead(session.Account.AccountID); err != nil {
			writeNotificationError(w, err)
			return
		}
		respondNotificationOverview(w, guests, accounts, notifications, session.Account.AccountID, 48)
	})

	mux.HandleFunc("/api/platform/inbox/stream", func(w http.ResponseWriter, r *http.Request) {
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
				http.Error(w, `{"error":"invalid inbox stream payload"}`, http.StatusBadRequest)
				return
			}
		}
		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}
		flusher, streamOK := w.(http.Flusher)
		if !streamOK {
			http.Error(w, `{"error":"streaming unsupported"}`, http.StatusInternalServerError)
			return
		}
		events, cancel := notifications.Subscribe(session.Account.AccountID, 32)
		defer cancel()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		_, _ = fmt.Fprint(w, ": connected\n\n")
		flusher.Flush()

		heartbeat := time.NewTicker(25 * time.Second)
		defer heartbeat.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if err := writeAccountNotificationStreamEvent(w, flusher, event); err != nil {
					return
				}
			case <-heartbeat.C:
				_, _ = fmt.Fprint(w, ": keep-alive\n\n")
				flusher.Flush()
			}
		}
	})
}
