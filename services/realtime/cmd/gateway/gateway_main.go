package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chess404/realtime/internal/envutil"
	"github.com/chess404/realtime/internal/httputil"
	"github.com/chess404/realtime/internal/rate_limit"
)

// Service startup and HTTP server wiring.

func main() {
	envutil.Require("MATCH_SERVICE_INTERNAL_URL", "PLATFORM_SERVICE_INTERNAL_URL", "MATCHMAKING_SERVICE_INTERNAL_URL", "ALLOWED_ORIGINS")
	config := gatewayConfigFromEnv()
	// HTTP client used to call sibling backend services. The 10s
	// budget is comfortable for computer-mode match creation
	// (which involves card eval + opponent init + persist to
	// postgres + redis) and avoids spurious 502s on the first
	// request after a deploy (cold-start). Most calls finish
	// well under 1s; the headroom is for tail latency.
	client := httputil.NewHTTPClient(10 * time.Second)
	mux := buildGatewayMux(config, client)
	rl, err := rate_limit.NewRateLimiter()
	if err != nil {
		log.Fatalf("failed to initialize rate limiter: %v", err)
	}

	internalToken := gatewayInternalServiceToken()
	addr := httputil.ListenAddr("GATEWAY_ADDR", 8080)
	srv := &http.Server{
		Addr:              addr,
		Handler:           rate_limit.NewHeaderStrippingMiddleware("X-Powered-By")(httputil.WithRecovery(httputil.WithLogging("gateway", rate_limit.SecurityHeadersMiddleware(rate_limit.CSRFMiddleware(rate_limit.GlobalIPRateLimitMiddleware(rl, internalToken)(rate_limit.MiddlewareWithTrustedBypass(rl, rate_limit.DefaultAPIWindow, rate_limit.DefaultAPILimit, internalToken)(rate_limit.ContentTypeMiddleware(mux))), httputil.ParseAllowedOrigins(), internalToken))))),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		certFile := os.Getenv("TLS_CERT_FILE")
		keyFile := os.Getenv("TLS_KEY_FILE")
		if certFile != "" && keyFile != "" {
			log.Printf("gateway listening with TLS on %s", addr)
			if err := srv.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
				log.Fatalf("listen: %v", err)
			}
		} else {
			log.Printf("gateway listening on %s", addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("listen: %v", err)
			}
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("gateway shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	rl.Close()
}
