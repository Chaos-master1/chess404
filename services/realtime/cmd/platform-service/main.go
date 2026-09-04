package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chess404/realtime/internal/envutil"
	"github.com/chess404/realtime/internal/httputil"
	"github.com/chess404/realtime/internal/platform"
	"github.com/chess404/realtime/internal/rate_limit"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// sharedPostgresPool is the single *sql.DB shared by all Postgres-backed
// stores in this service. Created once at startup; avoids the previous
// pattern of 10+ independent pools exhausting Postgres connections.
var sharedPostgresPool *sql.DB

func initPostgresPool() {
	rawURL := strings.TrimSpace(os.Getenv("PLATFORM_POSTGRES_URL"))
	if rawURL == "" {
		// When PLATFORM_POSTGRES_URL is unset, each store falls back to its
		// per-store env var and opens its own pool (backward compat).
		return
	}
	db, err := sql.Open("pgx", rawURL)
	if err != nil {
		log.Fatalf("failed to open shared Postgres pool: %v", err)
	}
	platform.ConfigurePostgresPool(db, 25, 5)
	sharedPostgresPool = db
	log.Printf("platform: shared Postgres pool opened")
}

var accountsCache = platform.NewLeaderboardCache(10 * time.Second)

func main() {
	envutil.Require("ALLOWED_ORIGINS")
	initPostgresPool()
	archive, err := openArchiveStore()
	if err != nil {
		log.Fatalf("failed to initialize archive store: %v", err)
	}
	defer func() { _ = archive.Close() }()
	guests, err := openGuestDirectory()
	if err != nil {
		log.Fatalf("failed to initialize guest store: %v", err)
	}
	defer func() { _ = guests.Close() }()
	accounts, err := openAccountStore()
	if err != nil {
		log.Fatalf("failed to initialize account store: %v", err)
	}
	defer func() { _ = accounts.Close() }()
	friends, err := openFriendshipStore()
	if err != nil {
		log.Fatalf("failed to initialize friendship store: %v", err)
	}
	defer func() { _ = friends.Close() }()
	moderation, err := openModerationStore()
	if err != nil {
		log.Fatalf("failed to initialize moderation store: %v", err)
	}
	defer func() { _ = moderation.Close() }()
	challenges, err := openDirectChallengeStore()
	if err != nil {
		log.Fatalf("failed to initialize direct challenge store: %v", err)
	}
	defer func() { _ = challenges.Close() }()
	notifications, err := openNotificationStore()
	if err != nil {
		log.Fatalf("failed to initialize notification store: %v", err)
	}
	defer func() { _ = notifications.Close() }()
	emailOutbox, err := openAccountEmailOutboxStore()
	if err != nil {
		log.Fatalf("failed to initialize account email outbox: %v", err)
	}
	defer func() { _ = emailOutbox.Close() }()
	emailSender, err := openAccountEmailSender()
	if err != nil {
		log.Fatalf("failed to initialize account email delivery: %v", err)
	}
	dispatcherContext, cancelEmailDispatch := context.WithCancel(context.Background())
	defer cancelEmailDispatch()
	newAccountEmailDispatcher(emailOutbox, emailSender, time.Now).Start(dispatcherContext)
	securityAudit, err := openAccountSecurityAuditStore()
	if err != nil {
		log.Fatalf("failed to initialize account security audit store: %v", err)
	}
	defer func() { _ = securityAudit.Close() }()
	claims, err := openMatchClaimStore()
	if err != nil {
		log.Fatalf("failed to initialize match claim store: %v", err)
	}
	defer func() { _ = claims.Close() }()
	anticheatStore, err := openAnticheatStore()
	if err != nil {
		log.Fatalf("failed to initialize anticheat store: %v", err)
	}
	defer func() { _ = anticheatStore.Close() }()
	mux := buildPlatformMux(archive, guests, accounts, friends, moderation, challenges, notifications, emailOutbox, securityAudit, claims, anticheatStore)
	rl, err := rate_limit.NewRateLimiter()
	if err != nil {
		log.Fatalf("failed to initialize rate limiter: %v", err)
	}

	go runAnticheatRetentionLoop(anticheatStore)

	internalToken := configuredInternalServiceToken()
	addr := httputil.ListenAddr("PLATFORM_ADDR", 8083)
	srv := &http.Server{
		Addr: addr,
		// CORS middleware wraps CSRF so that even CSRF-rejected responses
		// carry the proper Access-Control-Allow-* headers. Otherwise the
		// browser reports "blocked by CORS policy" on legitimate cross-origin
		// POSTs whose Origin happens to mismatch the same-origin self check.
		Handler:           rate_limit.NewHeaderStrippingMiddleware("X-Powered-By")(httputil.WithRecovery(httputil.WithLogging("platform-service", rate_limit.SecurityHeadersMiddleware(httputil.LimitBody(withCORS(rate_limit.CSRFMiddleware(rate_limit.GlobalIPRateLimitMiddleware(rl, internalToken)(rate_limit.MiddlewareWithTrustedBypass(rl, rate_limit.DefaultAPIWindow, rate_limit.DefaultAPILimit, internalToken)(mux)), httputil.ParseAllowedOrigins(), internalToken))))))),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		log.Printf("platform-service listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("platform-service shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	rl.Close()
}

func withCORS(next http.Handler) http.Handler {
	allowed := httputil.ParseAllowedOrigins()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || httputil.IsOriginAllowed(origin, allowed) {
			if origin == "" && len(allowed) > 0 {
				origin = allowed[0]
			}
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Vary", "Origin")
		} else {
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, DELETE, PUT")
		// Allow the custom identity headers used by the web client (x-chess404-{white|black}-{guest-id|session-token}).
		// Session secrets are transmitted via HttpOnly cookies, not headers.
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-Chess404-White-Guest-Id, X-Chess404-White-Session-Token, X-Chess404-Black-Guest-Id, X-Chess404-Black-Session-Token")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Type, X-Request-Id")
		w.Header().Set("Access-Control-Max-Age", "600")
		// API responses are per-account and often authenticated (sessions,
		// friends, notifications, moderation). Vary is Origin only, so a shared
		// cache marking these "public" would serve one user's authenticated
		// payload to another.
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type jsonContentTypeWriter struct {
	http.ResponseWriter
}

func (w *jsonContentTypeWriter) WriteHeader(code int) {
	if strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		w.Header().Set("Content-Type", "application/json")
	}
	w.ResponseWriter.WriteHeader(code)
}

func respondError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		log.Printf("error encoding JSON response: %v", err)
	}
}

func respondJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("error encoding JSON response: %v", err)
	}
}

func configuredInternalServiceToken() string {
	for _, name := range []string{"PLATFORM_INTERNAL_SERVICE_TOKEN", "CHESS404_INTERNAL_SERVICE_TOKEN", "INTERNAL_SERVICE_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func envBool(name string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func requireInternalServiceRequest(w http.ResponseWriter, r *http.Request) bool {
	expected := configuredInternalServiceToken()
	if expected == "" {
		respondError(w, http.StatusNotFound, "not found")
		return false
	}

	provided := strings.TrimSpace(r.Header.Get("X-Chess404-Service-Token"))
	if provided == "" {
		const prefix = "Bearer "
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(auth, prefix) {
			provided = strings.TrimSpace(strings.TrimPrefix(auth, prefix))
		}
	}
	if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func buildPlatformMux(archive *platform.MatchArchiveStore, guests platform.GuestDirectory, accounts platform.AccountDirectory, friends platform.FriendshipDirectory, moderation platform.ModerationDirectory, challenges platform.DirectChallengeDirectory, notifications platform.AccountNotificationDirectory, emailOutbox platform.AccountEmailOutboxDirectory, securityAudit platform.AccountSecurityAuditDirectory, claims *platform.MatchClaimStore, anticheatStore platform.AnticheatStore) http.Handler {
	mux := http.NewServeMux()
	registerAnticheatRoutes(mux, anticheatStore)
	authThrottle := newPlatformAuthThrottle(time.Now)
	registerInfraRoutes(mux, archive, guests, accounts, friends, moderation, claims)
	registerGuestRoutes(mux, archive, guests, accounts)
	registerMatchClaimRoutes(mux, archive, guests, claims)
	registerMatchArchiveRoutes(mux, archive, accounts)
	registerAccountAuthRoutes(mux, guests, accounts, moderation, emailOutbox, securityAudit, authThrottle)
	registerInboxRoutes(mux, guests, accounts, moderation, notifications)
	registerFriendRoutes(mux, guests, accounts, friends, moderation, notifications)
	registerModerationRoutes(mux, guests, accounts, friends, moderation, challenges, notifications, securityAudit)
	registerChallengeRoutes(mux, guests, accounts, friends, moderation, challenges, notifications)
	registerAccountRoutes(mux, archive, guests, accounts, moderation)
	registerInternalRoutes(mux, archive, guests, accounts, moderation)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(&jsonContentTypeWriter{ResponseWriter: w}, r)
	})
}
