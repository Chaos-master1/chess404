package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	matchTarget, err := url.Parse("http://127.0.0.1:8082")
	if err != nil {
		log.Fatalf("invalid match target: %v", err)
	}
	platformTarget, err := url.Parse("http://127.0.0.1:8083")
	if err != nil {
		log.Fatalf("invalid platform target: %v", err)
	}
	matchmakingTarget, err := url.Parse("http://127.0.0.1:8084")
	if err != nil {
		log.Fatalf("invalid matchmaking target: %v", err)
	}
	gatewayTarget, err := url.Parse("http://127.0.0.1:8090")
	if err != nil {
		log.Fatalf("invalid gateway target: %v", err)
	}

	matchProxy := httputil.NewSingleHostReverseProxy(matchTarget)
	platformProxy := httputil.NewSingleHostReverseProxy(platformTarget)
	matchmakingProxy := httputil.NewSingleHostReverseProxy(matchmakingTarget)
	gatewayProxy := httputil.NewSingleHostReverseProxy(gatewayTarget)

	// Configure timeouts and error handling
	errorHandler := func(name string) func(http.ResponseWriter, *http.Request, error) {
		return func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("proxy:%s error for %s %s: %v", name, r.Method, r.URL.Path, err)
			http.Error(w, `{"error":"upstream unavailable"}`, http.StatusBadGateway)
		}
	}
	matchProxy.ErrorHandler = errorHandler("match")
	platformProxy.ErrorHandler = errorHandler("platform")
	matchmakingProxy.ErrorHandler = errorHandler("matchmaking")
	gatewayProxy.ErrorHandler = errorHandler("gateway")

	mux := http.NewServeMux()

	// Direct health check for Render
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"render-proxy"}`))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Unified routing
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		isWebSocket := strings.EqualFold(r.Header.Get("Upgrade"), "websocket")

		// 1. WebSocket or live match rooms -> match-service
		if isWebSocket || strings.HasPrefix(path, "/api/matches") {
			matchProxy.ServeHTTP(w, r)
			return
		}

		// 2. Platform endpoints (accounts, profiles, history, rankings) -> platform-service
		if strings.HasPrefix(path, "/api/platform") {
			platformProxy.ServeHTTP(w, r)
			return
		}

		// 3. Matchmaking endpoints (queues, tickets) -> matchmaking-service
		if strings.HasPrefix(path, "/api/matchmaking") {
			matchmakingProxy.ServeHTTP(w, r)
			return
		}

		// 4. Everything else -> gateway (bootstrap, auth, challenges, system status)
		gatewayProxy.ServeHTTP(w, r)
	})

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("Render unified proxy listening on 0.0.0.0:%s", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
}
