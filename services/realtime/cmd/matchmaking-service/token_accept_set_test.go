package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The full ticket list is the internal-only endpoint; per-caller rotation
// stages distinct tokens for the gateway and web proxy, so the accept set
// must cover every configured value.
func TestHasInternalServiceAccessAcceptsAnyConfiguredToken(t *testing.T) {
	t.Setenv("MATCHMAKING_INTERNAL_SERVICE_TOKEN", "matchmaking-specific")
	t.Setenv("PLATFORM_INTERNAL_SERVICE_TOKEN", "platform-specific")

	req := httptest.NewRequest(http.MethodGet, "/api/queues/tickets", nil)
	req.Header.Set("X-Chess404-Service-Token", "platform-specific")
	if !hasInternalServiceAccess(req, matchmakingInternalServiceToken()) {
		t.Fatal("expected the platform-caller token to authenticate")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/queues/tickets", nil)
	req.Header.Set("X-Chess404-Service-Token", "matchmaking-specific")
	if !hasInternalServiceAccess(req, matchmakingInternalServiceToken()) {
		t.Fatal("expected the matchmaking-specific token to authenticate")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/queues/tickets", nil)
	req.Header.Set("X-Chess404-Service-Token", "wrong")
	if hasInternalServiceAccess(req, matchmakingInternalServiceToken()) {
		t.Fatal("expected a wrong token to be rejected")
	}
}

func TestHasInternalServiceAccessRejectsWhenUnconfigured(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/queues/tickets", nil)
	req.Header.Set("X-Chess404-Service-Token", "anything")
	if hasInternalServiceAccess(req, "") {
		t.Fatal("expected no access when no token is configured")
	}
}

// The restriction-check call to platform-service uses the platform chain,
// never the matchmaking-specific token.
func TestPlatformServiceCallerTokenIgnoresMatchmakingSpecific(t *testing.T) {
	t.Setenv("MATCHMAKING_INTERNAL_SERVICE_TOKEN", "matchmaking-specific")
	t.Setenv("PLATFORM_INTERNAL_SERVICE_TOKEN", "platform-specific")

	if got := platformServiceCallerToken(); got != "platform-specific" {
		t.Fatalf("platformServiceCallerToken() = %q, want platform-specific", got)
	}
}
