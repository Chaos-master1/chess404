package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/chess404/realtime/internal/platform"
)

// registerAccountAuthRoutes wires the account auth endpoints.
func registerAccountAuthRoutes(mux *http.ServeMux, guests platform.GuestDirectory, accounts platform.AccountDirectory, moderation platform.ModerationDirectory, emailOutbox platform.AccountEmailOutboxDirectory, securityAudit platform.AccountSecurityAuditDirectory, authThrottle *platformAuthThrottle) {
	mux.HandleFunc("/api/platform/accounts/claim", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			GuestID       string `json:"guestId"`
			SessionSecret string `json:"sessionSecret"`
			SessionToken  string `json:"sessionToken"`
			Handle        string `json:"handle"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid account claim payload"}`, http.StatusBadRequest)
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
			case os.ErrInvalid:
				http.Error(w, `{"error":"guestId is required"}`, http.StatusBadRequest)
			default:
				http.Error(w, `{"error":"failed to resume guest session"}`, http.StatusBadRequest)
			}
			return
		}

		accountSession, err := accounts.ClaimGuest(session.Guest, payload.Handle)
		if err != nil {
			switch err {
			case platform.ErrInvalidAccountHandle:
				http.Error(w, `{"error":"invalid account handle"}`, http.StatusBadRequest)
			case platform.ErrAccountHandleTaken:
				http.Error(w, `{"error":"account handle already taken"}`, http.StatusConflict)
			default:
				http.Error(w, `{"error":"failed to claim account"}`, http.StatusInternalServerError)
			}
			return
		}
		recordAccountSecurityEvent(securityAudit, accountSession.Account.AccountID, platform.AccountSecurityEventKindAccountClaimed, accountSession.Account.Handle)
		accountsCache.Invalidate()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(accountSession)
	})

	mux.HandleFunc("/api/platform/account-sessions", func(w http.ResponseWriter, r *http.Request) {
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
				http.Error(w, `{"error":"invalid account session payload"}`, http.StatusBadRequest)
				return
			}
		}

		session, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken)
		if !ok {
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(session)
	})

	mux.HandleFunc("/api/platform/account-sessions/overview", func(w http.ResponseWriter, r *http.Request) {
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
				http.Error(w, `{"error":"invalid account session overview payload"}`, http.StatusBadRequest)
				return
			}
		}

		if _, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken); !ok {
			return
		}
		overview, err := accounts.ListAccountSessions(payload.AccountID, payload.SessionToken)
		if err != nil {
			writeAccountSessionError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(overview.PublicView(payload.SessionToken))
	})

	mux.HandleFunc("/api/platform/account-security/overview", func(w http.ResponseWriter, r *http.Request) {
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
				http.Error(w, `{"error":"invalid account security overview payload"}`, http.StatusBadRequest)
				return
			}
		}
		if _, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken); !ok {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(securityAudit.ListOverview(payload.AccountID, payload.Limit))
	})

	mux.HandleFunc("/api/platform/account-sessions/revoke", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID    string `json:"accountId"`
			SessionToken string `json:"sessionToken"`
			RevokeToken  string `json:"revokeToken"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid account session revoke payload"}`, http.StatusBadRequest)
				return
			}
		}
		if strings.TrimSpace(payload.RevokeToken) == "" {
			http.Error(w, `{"error":"revokeToken is required"}`, http.StatusBadRequest)
			return
		}
		if _, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken); !ok {
			return
		}
		if err := accounts.RevokeAccountSession(payload.AccountID, payload.SessionToken, payload.RevokeToken); err != nil {
			writeAccountSessionError(w, err)
			return
		}
		recordAccountSecurityEvent(securityAudit, payload.AccountID, platform.AccountSecurityEventKindSessionRevoked, sessionTokenFingerprint(payload.RevokeToken))
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/api/platform/account-sessions/revoke-others", func(w http.ResponseWriter, r *http.Request) {
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
				http.Error(w, `{"error":"invalid account session revoke-others payload"}`, http.StatusBadRequest)
				return
			}
		}
		if _, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken); !ok {
			return
		}
		overview, err := accounts.ListAccountSessions(payload.AccountID, payload.SessionToken)
		if err != nil {
			writeAccountSessionError(w, err)
			return
		}
		if err := accounts.RevokeOtherAccountSessions(payload.AccountID, payload.SessionToken); err != nil {
			writeAccountSessionError(w, err)
			return
		}
		revokedCount := len(overview.Sessions) - 1
		if revokedCount < 0 {
			revokedCount = 0
		}
		recordAccountSecurityEvent(securityAudit, payload.AccountID, platform.AccountSecurityEventKindOtherSessionsRevoked, fmt.Sprintf("%d", revokedCount))
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/api/platform/account-presence", func(w http.ResponseWriter, r *http.Request) {
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
				http.Error(w, `{"error":"invalid account presence payload"}`, http.StatusBadRequest)
				return
			}
		}

		if _, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken); !ok {
			return
		}
		session, err := accounts.TouchPresence(payload.AccountID, payload.SessionToken)
		if err != nil {
			writeAccountSessionError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(session)
	})

	mux.HandleFunc("/api/platform/account-auth/credentials", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID    string `json:"accountId"`
			SessionToken string `json:"sessionToken"`
			Email        string `json:"email"`
			Password     string `json:"password"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid account auth payload"}`, http.StatusBadRequest)
				return
			}
		}
		if allowed, retryAfter := authThrottle.allowCredentialSetup(r, payload.AccountID); !allowed {
			writeAuthRateLimitError(w, retryAfter)
			return
		}
		if _, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken); !ok {
			return
		}

		session, err := accounts.EnablePasswordLogin(payload.AccountID, payload.SessionToken, payload.Email, payload.Password)
		if err != nil {
			switch err {
			case platform.ErrUnauthorizedAccountSession:
				http.Error(w, `{"error":"unauthorized account session"}`, http.StatusUnauthorized)
			case platform.ErrInvalidAccountEmail:
				http.Error(w, `{"error":"invalid account email"}`, http.StatusBadRequest)
			case platform.ErrAccountEmailTaken:
				http.Error(w, `{"error":"account email already taken"}`, http.StatusConflict)
			case platform.ErrInvalidAccountPassword:
				http.Error(w, `{"error":"invalid account password"}`, http.StatusBadRequest)
			case os.ErrNotExist:
				http.Error(w, `{"error":"unknown account"}`, http.StatusNotFound)
			case os.ErrInvalid:
				http.Error(w, `{"error":"accountId is required"}`, http.StatusBadRequest)
			default:
				http.Error(w, `{"error":"failed to enable account login"}`, http.StatusInternalServerError)
			}
			return
		}
		recordAccountSecurityEvent(securityAudit, session.Account.AccountID, platform.AccountSecurityEventKindPasswordLoginEnabled, payload.Email)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(session)
	})

	mux.HandleFunc("/api/platform/account-auth/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Handle        string `json:"handle"`
			Email         string `json:"email"`
			Password      string `json:"password"`
			GuestID       string `json:"guestId"`
			SessionSecret string `json:"sessionSecret"`
			SessionToken  string `json:"sessionToken"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid account registration payload"}`, http.StatusBadRequest)
				return
			}
		}
		if allowed, retryAfter := authThrottle.allowRegistration(r, payload.Handle, payload.Email); !allowed {
			writeAuthRateLimitError(w, retryAfter)
			return
		}

		var guestSession platform.GuestSession
		var err error
		if strings.TrimSpace(payload.GuestID) != "" || strings.TrimSpace(payload.SessionSecret) != "" || strings.TrimSpace(payload.SessionToken) != "" {
			guestSession, err = resumeGuestFromPayload(guests, payload.GuestID, payload.SessionSecret, payload.SessionToken)
		} else {
			guestSession, err = guests.EnsureGuest("", "")
		}
		if err != nil {
			switch err {
			case platform.ErrUnauthorizedGuestSession:
				http.Error(w, `{"error":"unauthorized guest session"}`, http.StatusUnauthorized)
			case os.ErrNotExist:
				http.Error(w, `{"error":"unknown guest session"}`, http.StatusNotFound)
			default:
				http.Error(w, `{"error":"failed to restore guest session"}`, http.StatusInternalServerError)
			}
			return
		}

		accountSession, err := accounts.RegisterGuestAccount(guestSession.Guest, payload.Handle, payload.Email, payload.Password)
		if err != nil {
			writeAccountAuthError(w, err)
			return
		}
		if restriction, restricted := moderation.GetAccountRestriction(accountSession.Account.AccountID); restricted {
			_ = accounts.LogoutAccount(accountSession.Account.AccountID, accountSession.SessionToken)
			writeAccountRestrictionError(w, restriction)
			return
		}
		recordAccountSecurityEvent(securityAudit, accountSession.Account.AccountID, platform.AccountSecurityEventKindAccountClaimed, accountSession.Account.Handle)
		recordAccountSecurityEvent(securityAudit, accountSession.Account.AccountID, platform.AccountSecurityEventKindPasswordLoginEnabled, strings.TrimSpace(payload.Email))

		var (
			overview        platform.AccountAuthOverview
			delivery        *platform.AccountEmailDelivery
			previewToken    string
			verificationExp time.Time
		)
		challenge, verificationErr := accounts.StartEmailVerification(accountSession.Account.AccountID, accountSession.SessionToken)
		if verificationErr == nil {
			accountProfile, _ := accounts.GetAccount(accountSession.Account.AccountID)
			if queued, queueErr := emailOutbox.QueueDelivery(platform.BuildAccountEmailVerificationDelivery(accountProfile, challenge, accountAuthPublicBaseURL())); queueErr == nil {
				delivery = &queued
			}
			recordAccountSecurityEvent(securityAudit, accountSession.Account.AccountID, platform.AccountSecurityEventKindEmailVerificationRequested, challenge.Email)
			previewToken = challenge.Token
			verificationExp = challenge.ExpiresAt
		} else if verificationErr != platform.ErrAccountEmailAlreadyVerified {
			writeAccountAuthError(w, verificationErr)
			return
		}

		overview, err = accounts.GetAccountAuthOverview(accountSession.Account.AccountID, accountSession.SessionToken)
		if err != nil {
			writeAccountAuthError(w, err)
			return
		}

		response := map[string]any{
			"account":  accountSession,
			"guest":    guestSession,
			"overview": overview,
		}
		if delivery != nil {
			response["delivery"] = delivery
		}
		if !verificationExp.IsZero() {
			response["requestedVerification"] = true
			response["expiresAt"] = verificationExp
		}
		if accountAuthPreviewEnabled() && strings.TrimSpace(previewToken) != "" {
			response["previewToken"] = previewToken
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})

	mux.HandleFunc("/api/platform/account-auth/overview", func(w http.ResponseWriter, r *http.Request) {
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
				http.Error(w, `{"error":"invalid account auth overview payload"}`, http.StatusBadRequest)
				return
			}
		}

		if _, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken); !ok {
			return
		}
		overview, err := accounts.GetAccountAuthOverview(payload.AccountID, payload.SessionToken)
		if err != nil {
			writeAccountAuthError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(overview)
	})

	mux.HandleFunc("/api/platform/email-outbox/overview", func(w http.ResponseWriter, r *http.Request) {
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
				http.Error(w, `{"error":"invalid email outbox overview payload"}`, http.StatusBadRequest)
				return
			}
		}
		if _, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken); !ok {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(emailOutbox.ListOverview(payload.AccountID, payload.Limit))
	})

	mux.HandleFunc("/api/platform/account-auth/email-verification/request", func(w http.ResponseWriter, r *http.Request) {
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
				http.Error(w, `{"error":"invalid account verification payload"}`, http.StatusBadRequest)
				return
			}
		}
		if allowed, retryAfter := authThrottle.allowEmailVerification(r, payload.AccountID); !allowed {
			writeAuthRateLimitError(w, retryAfter)
			return
		}
		if _, ok := resumeAllowedAccountSessionOrWrite(w, accounts, moderation, payload.AccountID, payload.SessionToken); !ok {
			return
		}
		challenge, err := accounts.StartEmailVerification(payload.AccountID, payload.SessionToken)
		if err != nil {
			writeAccountAuthError(w, err)
			return
		}
		accountProfile, _ := accounts.GetAccount(payload.AccountID)
		delivery, err := emailOutbox.QueueDelivery(platform.BuildAccountEmailVerificationDelivery(accountProfile, challenge, accountAuthPublicBaseURL()))
		if err != nil {
			http.Error(w, `{"error":"failed to queue account verification email"}`, http.StatusInternalServerError)
			return
		}
		overview, err := accounts.GetAccountAuthOverview(payload.AccountID, payload.SessionToken)
		if err != nil {
			writeAccountAuthError(w, err)
			return
		}
		response := map[string]any{
			"overview":  overview,
			"requested": true,
			"email":     challenge.Email,
			"expiresAt": challenge.ExpiresAt,
			"delivery":  delivery,
		}
		recordAccountSecurityEvent(securityAudit, payload.AccountID, platform.AccountSecurityEventKindEmailVerificationRequested, challenge.Email)
		if accountAuthPreviewEnabled() {
			response["previewToken"] = challenge.Token
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})

	mux.HandleFunc("/api/platform/account-auth/email-verification/confirm", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID string `json:"accountId"`
			Token     string `json:"token"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid email verification confirmation payload"}`, http.StatusBadRequest)
				return
			}
		}
		if allowed, retryAfter := authThrottle.allowEmailVerification(r, payload.AccountID); !allowed {
			writeAuthRateLimitError(w, retryAfter)
			return
		}
		overview, err := accounts.VerifyEmail(payload.AccountID, payload.Token)
		if err != nil {
			writeAccountAuthError(w, err)
			return
		}
		recordAccountSecurityEvent(securityAudit, overview.AccountID, platform.AccountSecurityEventKindEmailVerified, overview.Email)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(overview)
	})

	mux.HandleFunc("/api/platform/account-auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Identifier string `json:"identifier"`
			Password   string `json:"password"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid account login payload"}`, http.StatusBadRequest)
				return
			}
		}
		if allowed, retryAfter := authThrottle.allowLogin(r, payload.Identifier); !allowed {
			writeAuthRateLimitError(w, retryAfter)
			return
		}

		accountSession, err := accounts.LoginWithPassword(payload.Identifier, payload.Password)
		if err != nil {
			switch err {
			case platform.ErrUnauthorizedAccountCredentials:
				http.Error(w, `{"error":"unauthorized account credentials"}`, http.StatusUnauthorized)
			default:
				http.Error(w, `{"error":"failed to sign in"}`, http.StatusInternalServerError)
			}
			return
		}
		if restriction, restricted := moderation.GetAccountRestriction(accountSession.Account.AccountID); restricted {
			_ = accounts.LogoutAccount(accountSession.Account.AccountID, accountSession.SessionToken)
			writeAccountRestrictionError(w, restriction)
			return
		}
		recordAccountSecurityEvent(securityAudit, accountSession.Account.AccountID, platform.AccountSecurityEventKindPasswordLoginSucceeded, accountSession.Account.Handle)
		guestSession, err := guests.IssueGuestSession(resolvePrimaryGuestID(accountSession.Account))
		if err != nil {
			http.Error(w, `{"error":"failed to restore account guest session"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account": accountSession,
			"guest":   guestSession,
		})
	})

	mux.HandleFunc("/api/platform/account-auth/password-reset/request", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Identifier string `json:"identifier"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid password reset request payload"}`, http.StatusBadRequest)
				return
			}
		}
		if allowed, retryAfter := authThrottle.allowPasswordReset(r, payload.Identifier); !allowed {
			writeAuthRateLimitError(w, retryAfter)
			return
		}
		challenge, err := accounts.StartPasswordReset(payload.Identifier)
		if err != nil {
			writeAccountAuthError(w, err)
			return
		}
		var delivery *platform.AccountEmailDelivery
		if strings.TrimSpace(challenge.Token) != "" {
			accountProfile, _ := accounts.GetAccount(challenge.AccountID)
			queued, queueErr := emailOutbox.QueueDelivery(platform.BuildAccountPasswordResetDelivery(accountProfile, challenge, accountAuthPublicBaseURL()))
			if queueErr == nil {
				delivery = &queued
			}
		}
		response := map[string]any{
			"requested": challenge.Requested,
		}
		if delivery != nil {
			response["delivery"] = delivery
		}
		if challenge.Requested && strings.TrimSpace(challenge.AccountID) != "" {
			recordAccountSecurityEvent(securityAudit, challenge.AccountID, platform.AccountSecurityEventKindPasswordResetRequested, challenge.Email)
		}
		if accountAuthPreviewEnabled() && strings.TrimSpace(challenge.Token) != "" {
			response["previewToken"] = challenge.Token
			response["previewAccountId"] = challenge.AccountID
			response["email"] = challenge.Email
			response["expiresAt"] = challenge.ExpiresAt
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})

	mux.HandleFunc("/api/platform/account-auth/password-reset/confirm", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			AccountID string `json:"accountId"`
			Token     string `json:"token"`
			Password  string `json:"password"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, `{"error":"invalid password reset confirmation payload"}`, http.StatusBadRequest)
				return
			}
		}
		if allowed, retryAfter := authThrottle.allowPasswordReset(r, payload.AccountID); !allowed {
			writeAuthRateLimitError(w, retryAfter)
			return
		}

		accountSession, err := accounts.ResetPassword(payload.AccountID, payload.Token, payload.Password)
		if err != nil {
			writeAccountAuthError(w, err)
			return
		}
		recordAccountSecurityEvent(securityAudit, accountSession.Account.AccountID, platform.AccountSecurityEventKindPasswordResetCompleted, accountSession.Account.Handle)
		guestSession, err := guests.IssueGuestSession(resolvePrimaryGuestID(accountSession.Account))
		if err != nil {
			http.Error(w, `{"error":"failed to restore account guest session"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account": accountSession,
			"guest":   guestSession,
		})
	})

	mux.HandleFunc("/api/platform/account-auth/logout", func(w http.ResponseWriter, r *http.Request) {
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
				http.Error(w, `{"error":"invalid account logout payload"}`, http.StatusBadRequest)
				return
			}
		}

		if err := accounts.LogoutAccount(payload.AccountID, payload.SessionToken); err != nil {
			switch err {
			case platform.ErrUnauthorizedAccountSession:
				http.Error(w, `{"error":"unauthorized account session"}`, http.StatusUnauthorized)
			case os.ErrNotExist:
				http.Error(w, `{"error":"unknown account"}`, http.StatusNotFound)
			case os.ErrInvalid:
				http.Error(w, `{"error":"accountId is required"}`, http.StatusBadRequest)
			default:
				http.Error(w, `{"error":"failed to sign out"}`, http.StatusInternalServerError)
			}
			return
		}
		recordAccountSecurityEvent(securityAudit, payload.AccountID, platform.AccountSecurityEventKindSessionSignedOut, sessionTokenFingerprint(payload.SessionToken))

		w.WriteHeader(http.StatusNoContent)
	})
}
