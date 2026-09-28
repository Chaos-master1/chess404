package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Stage 4 of the per-service token migration (RUNBOOK.md): the gateway sends
// each backend the credential that backend expects, chosen per destination,
// not one global token. This is what allows each backend to hold a distinct
// token without breaking the gateway.
func TestGatewayOutboundTokensArePerTarget(t *testing.T) {
	received := map[string]string{}
	newBackend := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			received[name] = r.Header.Get("X-Chess404-Service-Token")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		}))
	}
	matchServer := newBackend("match")
	defer matchServer.Close()
	platformServer := newBackend("platform")
	defer platformServer.Close()
	matchmakingServer := newBackend("matchmaking")
	defer matchmakingServer.Close()

	t.Setenv("MATCH_SERVICE_INTERNAL_URL", matchServer.URL)
	t.Setenv("PLATFORM_SERVICE_INTERNAL_URL", platformServer.URL)
	t.Setenv("MATCHMAKING_SERVICE_INTERNAL_URL", matchmakingServer.URL)
	t.Setenv("MATCH_INTERNAL_SERVICE_TOKEN", "match-specific")
	t.Setenv("PLATFORM_INTERNAL_SERVICE_TOKEN", "platform-specific")
	t.Setenv("MATCHMAKING_INTERNAL_SERVICE_TOKEN", "matchmaking-specific")
	t.Setenv("GATEWAY_INTERNAL_SERVICE_TOKEN", "gateway-specific")

	client := &http.Client{}
	for _, tc := range []struct {
		name string
		url  string
		want string
	}{
		{"match", matchServer.URL + "/api/matches/m1/intents", "match-specific"},
		{"platform", platformServer.URL + "/api/platform/guest-sessions", "platform-specific"},
		{"matchmaking", matchmakingServer.URL + "/api/queues/tickets", "matchmaking-specific"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := fetchGatewayJSONRequestWithContext(context.Background(), client, http.MethodPost, tc.url, map[string]string{})
			if !result.Healthy {
				t.Fatalf("expected healthy upstream call to %s, got: %v", tc.url, result)
			}
			if got := received[tc.name]; got != tc.want {
				t.Fatalf("%s received token %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// The gateway's INBOUND credential (web -> gateway) is separate from every
// outbound chain: a token intended for a backend hop must not authenticate
// the web caller, and vice versa.
func TestGatewayInboundTokenIsDistinctFromOutboundChains(t *testing.T) {
	t.Setenv("GATEWAY_INTERNAL_SERVICE_TOKEN", "gateway-specific")
	t.Setenv("MATCH_INTERNAL_SERVICE_TOKEN", "match-specific")
	t.Setenv("PLATFORM_INTERNAL_SERVICE_TOKEN", "platform-specific")

	if got := gatewayInternalServiceToken(); got != "gateway-specific" {
		t.Fatalf("gatewayInternalServiceToken() = %q, want gateway-specific", got)
	}
	if got := gatewayMatchServiceCallerToken(); got != "match-specific" {
		t.Fatalf("gatewayMatchServiceCallerToken() = %q, want match-specific", got)
	}
	if got := gatewayPlatformServiceCallerToken(); got != "platform-specific" {
		t.Fatalf("gatewayPlatformServiceCallerToken() = %q, want platform-specific", got)
	}
}
