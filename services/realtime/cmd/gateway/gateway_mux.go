package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/httputil"
	"github.com/chess404/realtime/internal/metrics"
	"github.com/chess404/realtime/internal/platform"
)

// Route table, source-request middleware, and public-origin reconstruction.

func buildGatewayMux(config GatewayConfig, client *http.Client) http.Handler {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"status":    "ok",
			"service":   "gateway",
			"checkedAt": time.Now().UTC(),
		})
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"status":    "ok",
			"service":   "gateway",
			"checkedAt": time.Now().UTC(),
		})
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		httputil.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	mux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) {
		httputil.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	mux.HandleFunc("/api/system/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		httputil.WriteJSON(w, http.StatusOK, collectGatewayStatus(config, client, r))
	})

	mux.Handle("/metrics", metrics.Handler())

	mux.HandleFunc("/api/session/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var payload GatewayBootstrapPayload
		var request GatewayBootstrapRequest
		if r.Method == http.MethodPost {
			if r.Body != nil {
				defer r.Body.Close()
				r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					httputil.WriteError(w, http.StatusBadRequest, "invalid bootstrap payload")
					return
				}
			}
			payload = buildGatewayBootstrapPayload(config, client, request, r)
		} else {
			payload = buildGatewayBootstrapPayload(config, client, GatewayBootstrapRequest{}, r)
		}

		// Set HttpOnly cookies for account session tokens as defense-in-depth.
		// TODO: Migrate to cookie-based auth and remove tokens from JSON body.
		// The JSON response still carries the tokens for JS usage until the
		// frontend and backend are migrated to read/write auth via cookies.
		if payload.AccountSessions != nil {
			for side, session := range map[string]*platform.AccountSession{
				"white": payload.AccountSessions.White,
				"black": payload.AccountSessions.Black,
			} {
				if session != nil && session.SessionToken != "" {
					http.SetCookie(w, &http.Cookie{
						Name:     "session_token_" + side,
						Value:    session.SessionToken,
						Path:     "/",
						HttpOnly: true,
						Secure:   true,
						SameSite: http.SameSiteStrictMode,
						Expires:  session.ExpiresAt,
					})
				}
			}
		}
		if payload.GuestSessions != nil {
			for side, session := range map[string]*platform.GuestSession{
				"white": payload.GuestSessions.White,
				"black": payload.GuestSessions.Black,
			} {
				if session != nil {
					http.SetCookie(w, &http.Cookie{
						Name:     "session_secret_" + side,
						Value:    session.SessionSecret,
						Path:     "/",
						HttpOnly: true,
						Secure:   true,
						SameSite: http.SameSiteStrictMode,
					})
					// Strip the secret only when the caller already proved it holds
					// this identity. For a first-time visitor THIS request is the
					// initial session creation, and the cookie is HttpOnly, so
					// stripping here left the browser with a guest id it could not
					// authenticate with: registration failed on "unauthorized guest
					// session", and every reload minted a fresh pair of guests
					// because the resume attempt could never succeed.
					if bootstrapResumedSuppliedGuest(request, side, session.Guest.GuestID) {
						session.SessionSecret = ""
						session.SessionToken = ""
					}
				}
			}
		}

		httputil.WriteJSON(w, http.StatusOK, contracts.Envelope{
			Type:    "gateway.bootstrap",
			Payload: payload,
		})
	})

	mux.HandleFunc("/api/private-matches", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var payload GatewayPrivateMatchRequest
		if r.Body != nil {
			defer r.Body.Close()
			r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				httputil.WriteError(w, http.StatusBadRequest, "invalid private match payload")
				return
			}
		}
		response, statusCode, err := createGatewayPrivateMatch(config, client, payload, r)
		if err != nil {
			httputil.WriteError(w, statusCode, err.Error())
			return
		}
		httputil.WriteJSON(w, http.StatusCreated, response)
	})

	mux.HandleFunc("/api/matches/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/matches/")
		if path == "" {
			httputil.WriteError(w, http.StatusNotFound, "match id required")
			return
		}
		parts := strings.Split(path, "/")
		if len(parts) != 2 || (parts[1] != "intents" && parts[1] != "presence") {
			httputil.WriteError(w, http.StatusNotFound, "route not found")
			return
		}
		if !isValidPathParam(parts[0]) {
			httputil.WriteError(w, http.StatusBadRequest, "invalid match id")
			return
		}
		if parts[1] == "intents" {
			proxyGatewayIntent(w, r, config, client, parts[0])
			return
		}
		proxyGatewayPresence(w, r, config, client, parts[0])
	})

	mux.HandleFunc("/api/private-matches/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/private-matches/")
		if path == "" {
			httputil.WriteError(w, http.StatusNotFound, "match id required")
			return
		}
		parts := strings.Split(path, "/")
		if len(parts) != 2 || (parts[1] != "join" && parts[1] != "rematch") {
			httputil.WriteError(w, http.StatusNotFound, "route not found")
			return
		}
		var payload GatewayPrivateMatchRequest
		if r.Body != nil {
			defer r.Body.Close()
			r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				httputil.WriteError(w, http.StatusBadRequest, "invalid private match join payload")
				return
			}
		}
		if !isValidPathParam(parts[0]) {
			httputil.WriteError(w, http.StatusBadRequest, "invalid match id")
			return
		}
		var (
			response   GatewayPrivateMatchResponse
			statusCode int
			err        error
		)
		if parts[1] == "join" {
			response, statusCode, err = joinGatewayPrivateMatch(config, client, parts[0], payload, r)
		} else {
			response, statusCode, err = rematchGatewayPrivateMatch(config, client, parts[0], payload, r)
		}
		if err != nil {
			httputil.WriteError(w, statusCode, err.Error())
			return
		}
		httputil.WriteJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("/api/challenges", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var payload GatewayDirectChallengeRequest
		if r.Body != nil {
			defer r.Body.Close()
			r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				httputil.WriteError(w, http.StatusBadRequest, "invalid direct challenge payload")
				return
			}
		}
		response, statusCode, err := createGatewayDirectChallenge(config, client, payload, r)
		if err != nil {
			httputil.WriteError(w, statusCode, err.Error())
			return
		}
		httputil.WriteJSON(w, http.StatusCreated, response)
	})

	mux.HandleFunc("/api/challenges/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/challenges/")
		parts := strings.Split(path, "/")
		if len(parts) != 2 || parts[1] != "accept" || strings.TrimSpace(parts[0]) == "" {
			httputil.WriteError(w, http.StatusNotFound, "route not found")
			return
		}
		var payload GatewayDirectChallengeAcceptRequest
		if r.Body != nil {
			defer r.Body.Close()
			r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				httputil.WriteError(w, http.StatusBadRequest, "invalid challenge accept payload")
				return
			}
		}
		if !isValidPathParam(parts[0]) {
			httputil.WriteError(w, http.StatusBadRequest, "invalid challenge id")
			return
		}
		response, statusCode, err := acceptGatewayDirectChallenge(config, client, parts[0], payload, r)
		if err != nil {
			httputil.WriteError(w, statusCode, err.Error())
			return
		}
		httputil.WriteJSON(w, http.StatusOK, response)
	})

	return sourceRequestMiddleware(mux)
}

// sourceRequestKey is the context key under which the gateway stores the
// incoming *http.Request for the lifetime of a single request. Downstream
// helpers (e.g., fetchGatewayJSONRequestWithContext) read it back to forward
// the browser's Origin/Referer headers to backend services, so their CSRF
// middleware can validate the request against its allow-list.
type sourceRequestKey struct{}

func withSourceRequest(ctx context.Context, r *http.Request) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, sourceRequestKey{}, r)
}

func sourceRequestFromContext(ctx context.Context) *http.Request {
	if ctx == nil {
		return nil
	}
	r, _ := ctx.Value(sourceRequestKey{}).(*http.Request)
	return r
}

// sourceRequestMiddleware wraps a handler so every request's source is
// available in its context. This lets the gateway's outgoing HTTP calls
// (which use context.Background today) read back the original incoming
// request to forward headers like Origin/Referer.
func sourceRequestMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := withSourceRequest(r.Context(), r)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// reconstructPublicOrigin returns the public-facing origin (scheme + host)
// for an incoming request, taking reverse-proxy headers into account. This
// mirrors the logic the CSRF middleware uses in
// internal/rate_limit.trustedSelfOrigin, applied to the source request
// rather than the current request.
//
// The browser sends an Origin header only for cross-origin requests, and
// the Referer it sends for same-origin POSTs includes the request path
// (e.g., https://example.com/play), which does not match a bare origin in
// a CSRF allow-list. Reconstructing the origin from the source's host
// information gives the gateway a clean origin to forward to backend
// services.
func reconstructPublicOrigin(r *http.Request) string {
	if r == nil {
		return ""
	}
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		// Browser sent Origin (e.g., cross-origin POST). Parse it and
		// return just scheme://host[:port] so the destination's CSRF
		// allow-list (which contains bare origins) can match.
		if u, err := url.Parse(origin); err == nil && u.Scheme != "" && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	host := r.Host
	scheme := "https://"
	if r.TLS == nil {
		scheme = "http://"
		if forwardedProto := firstForwardedValue(r.Header, "X-Forwarded-Proto"); forwardedProto != "" {
			scheme = forwardedProto + "://"
		}
		if forwardedHost := firstForwardedValue(r.Header, "X-Forwarded-Host"); forwardedHost != "" {
			host = forwardedHost
		}
	}
	if host == "" {
		return ""
	}
	return scheme + host
}

func firstForwardedValue(h http.Header, name string) string {
	raw := h.Get(name)
	if raw == "" {
		return ""
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			return part
		}
	}
	return ""
}
