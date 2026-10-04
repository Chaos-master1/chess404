package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/envutil"
	"github.com/chess404/realtime/internal/httputil"
	"github.com/chess404/realtime/internal/matchmaking"
	"github.com/chess404/realtime/internal/metrics"
	"github.com/chess404/realtime/internal/rate_limit"
)

func main() {
	internalToken := matchmakingInternalServiceToken()
	envutil.WarnSharedInternalToken("matchmaking-service",
		os.Getenv("MATCHMAKING_INTERNAL_SERVICE_TOKEN"), "MATCHMAKING_INTERNAL_SERVICE_TOKEN",
		[]string{"PLATFORM_INTERNAL_SERVICE_TOKEN", "CHESS404_INTERNAL_SERVICE_TOKEN", "INTERNAL_SERVICE_TOKEN"})

	service, err := openMatchmakingService()
	if err != nil {
		log.Fatalf("failed to initialize matchmaking service: %v", err)
	}
	defer func() { _ = service.Close() }()
	rl, err := rate_limit.NewRateLimiter()
	if err != nil {
		log.Fatalf("failed to initialize rate limiter: %v", err)
	}

	mux := buildMatchmakingMux(service, internalToken)

	addr := httputil.ListenAddr("MATCHMAKING_ADDR", 8084)
	srv := &http.Server{
		Addr:              addr,
		Handler:           rate_limit.NewHeaderStrippingMiddleware("X-Powered-By")(httputil.WithRecovery(httputil.WithLogging("matchmaking-service", rate_limit.SecurityHeadersMiddleware(httputil.LimitBody(rate_limit.CSRFMiddleware(rate_limit.GlobalIPRateLimitMiddleware(rl, internalToken)(rate_limit.MiddlewareWithTrustedBypass(rl, rate_limit.DefaultQueueWindow, rate_limit.DefaultQueueLimit, internalToken)(mux)), nil, internalToken)))))),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		log.Printf("matchmaking-service listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("matchmaking-service shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	rl.Close()
}

func matchmakingTicketStoreSQLitePath() string {
	return httputil.EnvOrDefault("MATCHMAKING_TICKET_STORE_SQLITE_PATH", "data/matchmaking-tickets.sqlite")
}

func matchmakingTicketStoreRedisURL() string {
	return httputil.EnvOrDefault("MATCHMAKING_TICKET_STORE_REDIS_URL", "")
}

func matchmakingTicketStoreRedisKey() string {
	return httputil.EnvOrDefault("MATCHMAKING_TICKET_STORE_REDIS_KEY", "chess404:matchmaking:tickets")
}

func matchmakingMatchServiceURL() string {
	return resolveInternalServiceURL(
		httputil.EnvOrDefault("MATCH_SERVICE_INTERNAL_URL", ""),
		"http://match-service:8080",
	)
}

func matchmakingPlatformServiceURL() string {
	return resolveInternalServiceURL(
		httputil.EnvOrDefault("PLATFORM_SERVICE_INTERNAL_URL", ""),
		"http://platform-service:8080",
	)
}

// matchmakingInternalServiceToken is the FIRST token this service accepts
// from callers (rate-limit trusted bypass). The service-specific name comes
// first; shared envs remain accepted so the per-service token migration can
// proceed one service at a time.
func matchmakingInternalServiceToken() string {
	for _, name := range []string{"MATCHMAKING_INTERNAL_SERVICE_TOKEN", "PLATFORM_INTERNAL_SERVICE_TOKEN", "CHESS404_INTERNAL_SERVICE_TOKEN", "INTERNAL_SERVICE_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

// matchmakingInternalServiceTokens is the full accept set: every non-empty
// token env this service recognizes. Per-caller rotation (RUNBOOK.md,
// stage 4) stages distinct values per caller; the ticket-list auth must
// accept any of them, not just the first.
func matchmakingInternalServiceTokens() []string {
	var tokens []string
	for _, name := range []string{"MATCHMAKING_INTERNAL_SERVICE_TOKEN", "PLATFORM_INTERNAL_SERVICE_TOKEN", "CHESS404_INTERNAL_SERVICE_TOKEN", "INTERNAL_SERVICE_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			tokens = append(tokens, value)
		}
	}
	return tokens
}

// platformServiceCallerToken is the credential this service SENDS to
// platform-service (its accept list, in precedence order). Deliberately
// independent of the inbound chain: staging
// MATCHMAKING_INTERNAL_SERVICE_TOKEN here must not change what we send to
// platform-service.
func platformServiceCallerToken() string {
	for _, name := range []string{"PLATFORM_INTERNAL_SERVICE_TOKEN", "CHESS404_INTERNAL_SERVICE_TOKEN", "INTERNAL_SERVICE_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

// checkAccountRestriction consults the platform-service moderation store to
// determine whether the supplied account is currently banned or suspended.
// Returns (true, kind) when the account is restricted, (false, "") when it is
// clear, and (false, "") on transport error (fail-open is acceptable for
// matchmaking; the match-service will still enforce the restriction at the
// intent boundary).
func checkAccountRestriction(ctx context.Context, baseURL, token, accountID string) (bool, string) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" || strings.TrimSpace(baseURL) == "" || token == "" {
		return false, ""
	}
	target := strings.TrimRight(baseURL, "/") + "/api/platform/internal/account-restriction?accountId=" + url.QueryEscape(accountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		log.Printf("[matchmaking] WARN: failed to build restriction check request: %v", err)
		return false, ""
	}
	req.Header.Set("X-Chess404-Service-Token", token)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[matchmaking] WARN: restriction check transport error for %s: %v", accountID, err)
		return false, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[matchmaking] WARN: restriction check returned %d for %s", resp.StatusCode, accountID)
		return false, ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, ""
	}
	var payload struct {
		Restricted      bool   `json:"restricted"`
		RestrictionKind string `json:"restrictionKind"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, ""
	}
	return payload.Restricted, payload.RestrictionKind
}

// resolveInternalServiceURL normalizes the misconfigured-Railway-variable
// shapes production has actually shipped. It mirrors the gateway's resolver
// (gateway_util.go): unresolved "${{...}}" template text falls back to the
// default, and a hostname with no port (the trailing-colon shape Railway
// stores when a private-domain variable is set by hand) gets the default
// port appended. The previous version here DISCARDED any configured
// *.railway.internal URL and fell back to the compose-style default, which
// does not resolve on Railway -- matchmaking then dialed port 80 of the
// internal hostname, CreateMatch failed, and the queue silently deleted
// both paired tickets on every pairing attempt.
func resolveInternalServiceURL(explicit, fallback string) string {
	value := strings.TrimRight(strings.TrimSpace(explicit), "/")
	if value == "" {
		return strings.TrimRight(strings.TrimSpace(fallback), "/")
	}
	if strings.Contains(value, "${{") {
		return strings.TrimRight(strings.TrimSpace(fallback), "/")
	}
	// A bare "host:" gains the default port, and a scheme-only URL with no
	// port at all ("http://host") must too -- otherwise net/http dials port 80,
	// which nothing in this repo listens on.
	if strings.HasSuffix(value, ":") {
		value += "8080"
	} else if u, err := url.Parse(value); err == nil && u.Port() == "" && u.Host != "" {
		value += ":8080"
	}
	return value
}

// buildMatchmakingMux registers every HTTP route. Extracted from main so
// tests can exercise the REAL handlers (cancel-secret redaction, internal
// auth gating) via ServeHTTP instead of rebuilding approximations of them
// -- the same pattern buildMatchServiceMux uses in match-service.
func buildMatchmakingMux(service *matchmaking.Service, internalToken string) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.Handle("/metrics", metrics.Handler())

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ok",
			"service":   "matchmaking-service",
			"checkedAt": time.Now().UTC(),
			"stats":     service.Stats(),
		})
	})

	mux.HandleFunc("/api/queues/default", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"queue":        "rated",
			"status":       "open",
			"architecture": "region-aware-authoritative",
		})
	})

	mux.HandleFunc("/api/queues/snapshots", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		queueFilter := parseOptionalQueueName(r.URL.Query().Get("queue"))
		modeFilter := parseOptionalModeID(r.URL.Query().Get("modeId"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"snapshots": queueSnapshots(service, queueFilter, modeFilter),
			"checkedAt": time.Now().UTC(),
		})
	})

	mux.HandleFunc("/api/queues/tickets", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			guestID := strings.TrimSpace(r.URL.Query().Get("guestId"))
			accountID := strings.TrimSpace(r.URL.Query().Get("accountId"))
			if guestID != "" || accountID != "" {
				ticket, ok := service.FindActiveTicket(guestID, accountID)
				if !ok {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ticket":   matchmaking.WireTicket{Ticket: ticket},
					"snapshot": service.Snapshot(ticket.Queue, ticket.ModeID),
				})
				return
			}
			// Full ticket list exposes every guest ID and rating. Web clients
			// never see it (the proxy returns an empty list in production);
			// direct anonymous access to this port must not leak it either.
			if !hasInternalServiceAccess(r, internalToken) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "ticket list requires internal service access"})
				return
			}
			queue := parseQueueName(r.URL.Query().Get("queue"))
			modeID := parseModeID(r.URL.Query().Get("modeId"))
			// Even though this list is internal-only, it still projects every
			// ticket so no reader can ever observe the pairing reservation
			// through this binary, whatever their access level.
			listed := service.List(queue, modeID)
			wireList := make([]matchmaking.WireTicket, len(listed))
			for i, t := range listed {
				wireList[i] = matchmaking.WireTicket{Ticket: t}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tickets": wireList,
			})
		case http.MethodPost:
			var payload struct {
				Queue          string `json:"queue"`
				ModeID         string `json:"modeId"`
				GuestID        string `json:"guestId"`
				AccountID      string `json:"accountId"`
				DisplayName    string `json:"displayName"`
				Rating         int    `json:"rating"`
				ClockSeconds   int64  `json:"clockSeconds"`
				ClockIncrement int64  `json:"clockIncrement"`
			}
			if r.Body != nil {
				defer r.Body.Close()
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					log.Printf("[matchmaking] ERROR: invalid queue payload: %v", err)
					http.Error(w, `{"error":"invalid queue payload"}`, http.StatusBadRequest)
					return
				}
			}
			if payload.GuestID == "" {
				log.Printf("[matchmaking] ERROR: guestId is required")
				http.Error(w, `{"error":"guestId is required"}`, http.StatusBadRequest)
				return
			}
			if payload.Rating <= 0 {
				payload.Rating = 1200
			}
			modeID := parseModeID(payload.ModeID)
			queue := parseQueueName(payload.Queue)
			if queue == matchmaking.QueueRated && strings.TrimSpace(payload.AccountID) == "" {
				http.Error(w, `{"error":"rated queue requires an accountId"}`, http.StatusUnauthorized)
				return
			}

			if restricted, kind := checkAccountRestriction(r.Context(), matchmakingPlatformServiceURL(), platformServiceCallerToken(), payload.AccountID); restricted {
				log.Printf("[matchmaking] BLOCKED enqueue for account=%s restriction=%s", payload.AccountID, kind)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":           "account is " + kind + " and cannot enter the queue",
					"restrictionKind": kind,
				})
				return
			}

			log.Printf("[matchmaking] Enqueue request: guest=%s, queue=%s, mode=%s, rating=%d, clock=%d+%d",
				payload.GuestID, queue, modeID, payload.Rating, payload.ClockSeconds, payload.ClockIncrement)

			ticket, err := service.EnqueueWithAccount(
				queue,
				modeID,
				payload.GuestID,
				payload.Rating,
				payload.DisplayName,
				strings.TrimSpace(payload.AccountID),
				payload.ClockSeconds,
				payload.ClockIncrement,
			)
			if err != nil {
				var activeErr matchmaking.ActiveTicketError
				if errors.As(err, &activeErr) {
					log.Printf("[matchmaking] Guest %s already has active ticket %s", payload.GuestID, activeErr.Ticket.TicketID)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusConflict)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"error": err.Error(),
						// The requester supplied only a guestId (public in every snapshot and
						// directory), so this response must not re-reveal the cancel
						// credential: knowing a guestId must not grant ticket cancellation.
						"ticket":   matchmaking.WireTicket{Ticket: activeErr.Ticket},
						"snapshot": service.Snapshot(activeErr.Ticket.Queue, activeErr.Ticket.ModeID),
					})
					return
				}
				log.Printf("[matchmaking] ERROR: failed to enqueue guest %s: %v", payload.GuestID, err)
				http.Error(w, fmt.Sprintf(`{"error":"failed to persist queue ticket: %s"}`, err.Error()), http.StatusInternalServerError)
				return
			}

			log.Printf("[matchmaking] Created ticket %s for guest %s (status=%s, room=%s)",
				ticket.TicketID, ticket.GuestID, ticket.Status, ticket.AssignedRoom)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ticket":   ticket,
				"snapshot": service.Snapshot(ticket.Queue, ticket.ModeID),
			})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/queues/tickets/", func(w http.ResponseWriter, r *http.Request) {
		ticketID := strings.TrimPrefix(r.URL.Path, "/api/queues/tickets/")
		if ticketID == "" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			ticket, ok := service.Get(ticketID)
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			// The ticket ID is the only credential this read requires, and
			// ticket IDs travel through URLs and logs, so the requester may
			// be anyone. The cancel secret is issued exactly once, on the
			// POST create response, to the enqueuing client -- never here.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ticket":   matchmaking.WireTicket{Ticket: ticket},
				"snapshot": service.Snapshot(ticket.Queue, ticket.ModeID),
			})
		case http.MethodDelete:
			// Cancelling is a mutation on someone else's ticket otherwise:
			// require the cancel secret issued to the enqueuing client, or an
			// internal service token (moderation / service ops).
			//
			// Precedence matters: the web proxy injects its internal service
			// token into EVERY forwarded request, so a token alone cannot
			// mean "service caller" on the user path. A presented ticket
			// secret always demotes the request to the user path, where the
			// secret is verified against the stored one.
			ticketSecret := strings.TrimSpace(r.Header.Get("X-Chess404-Ticket-Secret"))
			if ticketSecret == "" {
				ticketSecret = strings.TrimSpace(r.URL.Query().Get("cancelSecret"))
			}
			if ticketSecret == "" && !hasInternalServiceAccess(r, internalToken) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "ticket cancel requires the ticket cancel secret"})
				return
			}
			var cancelled matchmaking.Ticket
			var ok bool
			var err error
			if ticketSecret != "" {
				cancelled, ok, err = service.Cancel(ticketID, ticketSecret)
			} else {
				cancelled, ok, err = service.CancelByService(ticketID)
			}
			if err != nil {
				if errors.Is(err, matchmaking.ErrCancelUnauthorized) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": "ticket cancel secret missing or wrong"})
					return
				}
				http.Error(w, `{"error":"failed to persist queue cancellation"}`, http.StatusInternalServerError)
				return
			}
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ticket":   matchmaking.WireTicket{Ticket: cancelled},
				"snapshot": service.Snapshot(cancelled.Queue, cancelled.ModeID),
			})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	return mux
}

func openMatchmakingService() (*matchmaking.Service, error) {
	matchServiceURL := matchmakingMatchServiceURL()
	log.Printf("[matchmaking] Initializing with match service URL: %s", matchServiceURL)

	serviceCreator := &httpMatchCreator{
		baseURL: matchServiceURL,
		client:  &http.Client{Timeout: 3 * time.Second},
	}

	var (
		service *matchmaking.Service
		err     error
	)
	backend := strings.ToLower(httputil.EnvOrDefault("MATCHMAKING_TICKET_STORE_BACKEND", "file"))
	log.Printf("[matchmaking] Using ticket store backend: %s", backend)

	switch backend {
	case "sqlite":
		service, err = matchmaking.NewSQLitePersistentService(matchmakingTicketStoreSQLitePath())
	case "redis":
		redisURL := matchmakingTicketStoreRedisURL()
		log.Printf("[matchmaking] Connecting to Redis at: %s", httputil.RedactURLCredentials(redisURL))
		service, err = matchmaking.NewRedisPersistentService(redisURL, matchmakingTicketStoreRedisKey())
	default:
		service, err = matchmaking.NewPersistentService(httputil.EnvOrDefault("MATCHMAKING_TICKET_STORE_PATH", "data/matchmaking-tickets.json"))
	}
	if err != nil {
		return nil, err
	}
	service.SetMatchCreator(serviceCreator)
	log.Printf("[matchmaking] Service initialized successfully")
	return service, nil
}

func parseQueueName(value string) matchmaking.QueueName {
	switch value {
	case string(matchmaking.QueueCasual):
		return matchmaking.QueueCasual
	default:
		return matchmaking.QueueRated
	}
}

func parseModeID(value string) contracts.MatchModeID {
	return contracts.NormalizeMatchModeID(value)
}

func parseOptionalQueueName(value string) matchmaking.QueueName {
	switch strings.TrimSpace(value) {
	case "":
		return ""
	default:
		return parseQueueName(value)
	}
}

func parseOptionalModeID(value string) contracts.MatchModeID {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return contracts.NormalizeMatchModeID(value)
}

func hasInternalServiceAccess(r *http.Request, token string) bool {
	// `token` is the first accepted token (kept for the not-configured
	// check and compatibility); any configured token env also authenticates,
	// so per-caller rotation can stage additional values.
	if strings.TrimSpace(token) == "" {
		return false
	}
	provided := strings.TrimSpace(r.Header.Get("X-Chess404-Service-Token"))
	for _, expected := range append(matchmakingInternalServiceTokens(), token) {
		if expected != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1 {
			return true
		}
	}
	return false
}

func queueSnapshots(service *matchmaking.Service, queueFilter matchmaking.QueueName, modeFilter contracts.MatchModeID) []matchmaking.QueueSnapshot {
	queues := []matchmaking.QueueName{matchmaking.QueueCasual, matchmaking.QueueRated}
	if queueFilter != "" {
		queues = []matchmaking.QueueName{queueFilter}
	}

	modes := []contracts.MatchModeID{contracts.MatchModeOpenCards, contracts.MatchModeHiddenCards}
	if modeFilter != "" {
		modes = []contracts.MatchModeID{modeFilter}
	}

	snapshots := make([]matchmaking.QueueSnapshot, 0, len(queues)*len(modes))
	for _, queue := range queues {
		for _, modeID := range modes {
			snapshots = append(snapshots, service.Snapshot(queue, modeID))
		}
	}
	return snapshots
}

type httpMatchCreator struct {
	baseURL string
	client  *http.Client
}

func (c *httpMatchCreator) CreateMatch(assignment matchmaking.MatchAssignment) error {
	if c == nil || strings.TrimSpace(c.baseURL) == "" {
		log.Printf("[matchmaking] ERROR: match service URL is not configured - cannot create match for room %s", assignment.RoomID)
		return fmt.Errorf("match service URL is not configured")
	}
	client := c.client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}

	clockSeconds := assignment.ClockSeconds
	if clockSeconds <= 0 {
		// Legacy assignments (or a direct service call) without a clock fall
		// back to the same default the queue normalizes to.
		clockSeconds = 600
	}
	increment := assignment.ClockIncrement
	if increment < 0 {
		increment = 0
	}

	payload := map[string]any{
		"matchId":           assignment.RoomID,
		"clockSeconds":      clockSeconds,
		"clockIncrement":    increment,
		"starterHandMode":   "starter_three",
		"queue":             string(assignment.Queue),
		"modeId":            string(assignment.ModeID),
		"whiteGuestId":      assignment.WhiteGuestID,
		"blackGuestId":      assignment.BlackGuestID,
		"whiteAccountId":    assignment.WhiteAccountID,
		"blackAccountId":    assignment.BlackAccountID,
		"whiteName":         assignment.WhiteName,
		"blackName":         assignment.BlackName,
		"whitePlayerSecret": assignment.WhitePlayerSecret,
		"blackPlayerSecret": assignment.BlackPlayerSecret,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[matchmaking] ERROR: failed to marshal match payload for room %s: %v", assignment.RoomID, err)
		return err
	}

	targetURL := c.baseURL + "/api/matches"
	log.Printf("[matchmaking] Creating match room %s at %s (white=%s, black=%s, queue=%s, mode=%s)",
		assignment.RoomID, targetURL, assignment.WhiteGuestID, assignment.BlackGuestID, assignment.Queue, assignment.ModeID)

	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[matchmaking] ERROR: failed to create HTTP request for room %s: %v", assignment.RoomID, err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.baseURL)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[matchmaking] ERROR: failed to send match creation request for room %s: %v", assignment.RoomID, err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := json.Marshal(payload)
		log.Printf("[matchmaking] ERROR: match service create failed for room %s with status %d, payload: %s", assignment.RoomID, resp.StatusCode, string(bodyBytes))
		return fmt.Errorf("match service create failed with status %d", resp.StatusCode)
	}

	log.Printf("[matchmaking] Successfully created match room %s", assignment.RoomID)
	return nil
}
