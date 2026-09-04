package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/httputil"
	"github.com/chess404/realtime/internal/platform"
)

// Upstream fetch helpers, error formatting, payload decoding, secret redaction.

func ensureGatewayPrivateGuestSession(config GatewayConfig, client *http.Client, identity GatewayGuestIdentity, r *http.Request) (*platform.GuestSession, int, error) {
	log.Printf("gw:guest-bootstrap: starting guestID=%q sessionSecret=%s platformServiceURL=%q",
		identity.GuestID, redactSecret(identity.SessionSecret), config.PlatformServiceURL)
	session, errMessage := bootstrapGuestSessionForSide(config, client, &identity, r)
	if session != nil {
		log.Printf("gw:guest-bootstrap: ok guestID=%q accountID=%q", session.Guest.GuestID, accountIDOf(session))
		return session, http.StatusOK, nil
	}
	log.Printf("gw:guest-bootstrap: FAILED errMessage=%q", errMessage)
	if errMessage == "" {
		return nil, http.StatusBadRequest, errors.New("failed to bootstrap guest session")
	}
	if strings.Contains(strings.ToLower(errMessage), "unauthorized") {
		return nil, http.StatusUnauthorized, errors.New(errMessage)
	}
	if strings.Contains(strings.ToLower(errMessage), "unknown guest") {
		return nil, http.StatusNotFound, errors.New(errMessage)
	}
	return nil, http.StatusBadGateway, errors.New(errMessage)
}

// accountIDOf safely returns the account ID from a session, or
// "<guest-only>" if the session is a guest without an account.
func accountIDOf(s *platform.GuestSession) string {
	if s == nil {
		return "<nil>"
	}
	if s.SessionToken != "" {
		// SessionToken is set when the guest has an associated
		// account session; we don't have the account ID
		// directly on GuestSession so just say "linked".
		return "<linked-account>"
	}
	return "<guest-only>"
}

// redactSecret keeps bearer credentials out of logs entirely. A prefix is
// still credential material and can make a brute-force or log-correlation
// attack easier, so non-empty values have a single fixed representation.
func redactSecret(s string) string {
	if s == "" {
		return "<empty>"
	}
	return "<redacted>"
}

func ensureGatewayPrivateAccountSession(config GatewayConfig, client *http.Client, identity *GatewayAccountIdentity, guestSession *platform.GuestSession, r *http.Request) (*platform.AccountSession, int, error) {
	if identity == nil || strings.TrimSpace(identity.AccountID) == "" {
		return nil, http.StatusOK, nil
	}
	session, errMessage := bootstrapAccountSessionForSide(config, client, identity, guestSession, r)
	if session != nil {
		return session, http.StatusOK, nil
	}
	if errMessage == "" {
		return nil, http.StatusBadGateway, errors.New("failed to bootstrap account session")
	}
	if strings.Contains(strings.ToLower(errMessage), "unauthorized") {
		return nil, http.StatusUnauthorized, errors.New(errMessage)
	}
	return nil, http.StatusBadGateway, errors.New(errMessage)
}

func proxyGatewayIntent(w http.ResponseWriter, r *http.Request, config GatewayConfig, client *http.Client, matchID string) {
	if !isValidPathParam(matchID) {
		httputil.WriteError(w, http.StatusBadRequest, "invalid match id")
		return
	}
	var req contracts.ApplyIntentRequest
	if r.Body != nil {
		defer r.Body.Close()
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	req.Intent.MatchID = matchID
	// A claim token is a one-time bootstrap into playerID+playerSecret. Resolve
	// it if either durable field is absent: a client with an empty player ID and
	// a stale secret is not authenticated yet. When both values are present the
	// claim must be left untouched, because a presence/intent race must not
	// consume the one-time token that accompanies an otherwise valid secret.
	if strings.TrimSpace(req.Intent.PlayerClaimToken) != "" && (strings.TrimSpace(req.Intent.PlayerID) == "" || strings.TrimSpace(req.Intent.PlayerSecret) == "") {
		claim, errMessage := resolveGatewayClaimByToken(config, client, matchID, strings.TrimSpace(req.Intent.PlayerClaimToken), r)
		if errMessage != "" {
			httputil.WriteError(w, http.StatusUnauthorized, errMessage)
			return
		}
		req.Intent.PlayerID = claim.PlayerID
		req.Intent.PlayerSecret = claim.PlayerSecret
		req.Intent.PlayerClaimToken = ""
	}

	result := fetchGatewayJSONRequest(r, client, http.MethodPost, config.MatchServiceURL+"/api/matches/"+matchID+"/intents", req)
	if result.Error != "" && result.StatusCode == 0 {
		httputil.WriteError(w, http.StatusBadGateway, result.Error)
		return
	}
	httputil.WriteJSON(w, statusOrDefault(result.StatusCode, http.StatusBadGateway), result.Payload)
}

func proxyGatewayPresence(w http.ResponseWriter, r *http.Request, config GatewayConfig, client *http.Client, matchID string) {
	if !isValidPathParam(matchID) {
		httputil.WriteError(w, http.StatusBadRequest, "invalid match id")
		return
	}
	var req contracts.MatchPresenceRequest
	if r.Body != nil {
		defer r.Body.Close()
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	// See the matching intent path: replace incomplete identity fields with the
	// claim exactly once, but leave a complete playerID+secret untouched.
	if strings.TrimSpace(req.PlayerClaimToken) != "" && (strings.TrimSpace(req.PlayerID) == "" || strings.TrimSpace(req.PlayerSecret) == "") {
		claim, errMessage := resolveGatewayClaimByToken(config, client, matchID, strings.TrimSpace(req.PlayerClaimToken), r)
		if errMessage != "" {
			httputil.WriteError(w, http.StatusUnauthorized, errMessage)
			return
		}
		req.PlayerID = claim.PlayerID
		req.PlayerSecret = claim.PlayerSecret
		req.PlayerClaimToken = ""
	}

	result := fetchGatewayJSONRequest(r, client, http.MethodPost, config.MatchServiceURL+"/api/matches/"+matchID+"/presence", req)
	if result.Error != "" && result.StatusCode == 0 {
		httputil.WriteError(w, http.StatusBadGateway, result.Error)
		return
	}
	if result.StatusCode == http.StatusNoContent {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	httputil.WriteJSON(w, statusOrDefault(result.StatusCode, http.StatusBadGateway), result.Payload)
}

func fetchGatewayJSON(r *http.Request, client *http.Client, url string) GatewayServiceHealth {
	return fetchGatewayJSONRequest(r, client, http.MethodGet, url, nil)
}

func fetchGatewayJSONRequest(r *http.Request, client *http.Client, method, url string, payload any) GatewayServiceHealth {
	var ctx context.Context
	if r != nil {
		ctx = r.Context()
	} else {
		ctx = context.Background()
	}
	return fetchGatewayJSONRequestWithContext(ctx, client, method, url, payload)
}

func fetchGatewayJSONRequestWithContext(ctx context.Context, client *http.Client, method, url string, payload any) GatewayServiceHealth {
	var body *bytes.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return GatewayServiceHealth{URL: url, Error: err.Error()}
		}
		body = bytes.NewReader(encoded)
	} else {
		body = bytes.NewReader(nil)
	}

	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return GatewayServiceHealth{URL: url, Error: err.Error()}
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token := gatewayInternalServiceToken(); token != "" {
		request.Header.Set("X-Chess404-Service-Token", token)
	}
	// Set the Origin header on the outgoing request to match the public
	// origin of the incoming request. The destination service's CSRF
	// middleware compares the Origin against its allow-list, so it needs a
	// clean origin (no path) to match. Without this, server-to-server
	// POSTs from the gateway arrive with no Origin and are rejected with
	// 403 (CSRF check failed: origin header required). Note: the browser
	// does not send Origin for same-origin requests (only Referer with a
	// path), and Referer-with-path does not equal the bare origin in the
	// allow-list, so we always reconstruct the origin from the source
	// request's host information.
	if source := sourceRequestFromContext(ctx); source != nil {
		if origin := reconstructPublicOrigin(source); origin != "" {
			request.Header.Set("Origin", origin)
		}
	}

	response, err := client.Do(request)
	if err != nil {
		return GatewayServiceHealth{URL: url, Error: err.Error()}
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNoContent {
		return GatewayServiceHealth{
			URL:        url,
			Healthy:    true,
			StatusCode: response.StatusCode,
		}
	}

	var responsePayload any
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&responsePayload); err != nil {
		return GatewayServiceHealth{
			URL:        url,
			StatusCode: response.StatusCode,
			Error:      fmt.Sprintf("invalid json: %v", err),
		}
	}

	return GatewayServiceHealth{
		URL:        url,
		Healthy:    response.StatusCode >= 200 && response.StatusCode < 300,
		StatusCode: response.StatusCode,
		Payload:    responsePayload,
	}
}

func gatewayErrorMessage(status GatewayServiceHealth, fallback string) string {
	if payload, ok := status.Payload.(map[string]any); ok {
		if message, ok := payload["error"].(string); ok && message != "" {
			return message
		}
	}
	return fallback
}

// formatUpstreamError returns a human-readable error that includes
// the upstream status code and (if the body is non-JSON or missing
// the "error" field) the raw response excerpt. Used for 502/504
// responses so the browser sees what actually failed, not a generic
// fallback. The message is intentionally verbose; the frontend shows
// it in the error banner.
func formatUpstreamError(status GatewayServiceHealth, fallback string) string {
	if payload, ok := status.Payload.(map[string]any); ok {
		if message, ok := payload["error"].(string); ok && message != "" {
			return fmt.Sprintf("%s (upstream status %d)", message, status.StatusCode)
		}
	}
	return fmt.Sprintf("%s (upstream status %d)", fallback, status.StatusCode)
}

func statusOrDefault(statusCode int, fallback int) int {
	if statusCode == 0 {
		return fallback
	}
	return statusCode
}

func decodeGatewayPayload[T any](payload any) (T, error) {
	var decoded T
	raw, err := json.Marshal(payload)
	if err != nil {
		return decoded, err
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return decoded, err
	}
	return decoded, nil
}

func bootstrapMessage(status GatewaySystemStatus) string {
	if status.Status == "ok" {
		return "Gateway online. Match, platform, and matchmaking services are ready."
	}

	problems := make([]string, 0, len(status.Services))
	for name, service := range status.Services {
		if !service.Healthy {
			problems = append(problems, name)
		}
	}

	if len(problems) == 0 {
		return "Gateway online."
	}
	return "Gateway online, but some backend services are degraded: " + strings.Join(problems, ", ")
}

func gatewayInternalServiceToken() string {
	for _, name := range []string{"GATEWAY_INTERNAL_SERVICE_TOKEN", "PLATFORM_INTERNAL_SERVICE_TOKEN", "CHESS404_INTERNAL_SERVICE_TOKEN", "INTERNAL_SERVICE_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func gatewayConfigFromEnv() GatewayConfig {
	return GatewayConfig{
		MatchServiceURL:       resolveInternalServiceURL("MATCH_SERVICE_INTERNAL_URL", "http://match-service:8080"),
		PlatformServiceURL:    resolveInternalServiceURL("PLATFORM_SERVICE_INTERNAL_URL", "http://platform-service:8080"),
		MatchmakingServiceURL: resolveInternalServiceURL("MATCHMAKING_SERVICE_INTERNAL_URL", "http://matchmaking-service:8080"),
	}
}

// resolveInternalServiceURL returns the value of envKey, falling back to
// defaultURL if the env var is missing, blank, or contains an unresolved
// Railway template (${{...}}). When the env var is set to a hostname-only
// Railway internal URL (e.g., "http://match-service.railway.internal:" with
// a trailing colon but no port), the function appends ":8080" so the
// resulting URL is valid. Services in this repo listen on port 8080.
func resolveInternalServiceURL(envKey string, defaultURL string) string {
	u := strings.TrimSpace(os.Getenv(envKey))
	if u == "" {
		return defaultURL
	}
	// Unresolved Railway template references (e.g., when the env var was
	// set to "${{match-service.RAILWAY_PRIVATE_DOMAIN}}" but the
	// referenced variable does not exist on the project). Using the literal
	// template as a URL would fail with a confusing connection error.
	if strings.Contains(u, "${{") {
		return defaultURL
	}
	// Hostname with no port (e.g., "http://match-service.railway.internal:"
	// from a misconfigured Railway variable). Append the default port so
	// the URL is valid.
	if strings.HasSuffix(u, ":") {
		u += "8080"
	}
	return u
}

// sanitizeSeatClaim returns the claim with all fields the frontend
// needs to connect to the match WebSocket. The PlayerSecret IS
// included (it IS the human's session secret, which the human
// already has); omitting it would prevent the frontend from
// authenticating the WebSocket. ClaimToken is also included for
// cases where the platform-service issues a separate per-match
// claim token.
func sanitizeSeatClaim(claim *GatewaySeatClaim) *GatewaySeatClaim {
	if claim == nil {
		return nil
	}
	return &GatewaySeatClaim{
		MatchID:      claim.MatchID,
		GuestID:      claim.GuestID,
		SeatColor:    claim.SeatColor,
		PlayerID:     claim.PlayerID,
		PlayerSecret: claim.PlayerSecret,
		ClaimToken:   claim.ClaimToken,
		ExpiresAt:    claim.ExpiresAt,
		Queue:        claim.Queue,
		ModeID:       claim.ModeID,
		WhiteGuestID: claim.WhiteGuestID,
		BlackGuestID: claim.BlackGuestID,
		WhiteName:    claim.WhiteName,
		BlackName:    claim.BlackName,
		Status:       claim.Status,
	}
}
