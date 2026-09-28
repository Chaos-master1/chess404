package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Per-caller rotation (RUNBOOK.md stage 4) stages distinct tokens for the
// gateway, the web proxy, and match-service; the internal route auth must
// accept ANY configured value, not just the first in the chain.
func TestRequireInternalServiceRequestAcceptsAnyConfiguredToken(t *testing.T) {
	t.Setenv("PLATFORM_INTERNAL_SERVICE_TOKEN", "platform-specific")
	t.Setenv("CHESS404_INTERNAL_SERVICE_TOKEN", "shared")

	for _, provided := range []string{"platform-specific", "shared"} {
		req := httptest.NewRequest(http.MethodPost, "/api/platform/internal/finalize-rated-match", nil)
		req.Header.Set("X-Chess404-Service-Token", provided)
		rec := httptest.NewRecorder()
		if !requireInternalServiceRequest(rec, req) {
			t.Fatalf("expected token %q to authenticate", provided)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected no error response for token %q, got %d", provided, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/platform/internal/finalize-rated-match", nil)
	req.Header.Set("X-Chess404-Service-Token", "wrong-token")
	rec := httptest.NewRecorder()
	if requireInternalServiceRequest(rec, req) {
		t.Fatal("expected a wrong token to be rejected")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong token, got %d", rec.Code)
	}

	empty := httptest.NewRequest(http.MethodPost, "/api/platform/internal/finalize-rated-match", nil)
	rec = httptest.NewRecorder()
	if requireInternalServiceRequest(rec, empty) {
		t.Fatal("expected a missing token to be rejected")
	}
}

// The platform -> match-service hop must send match-service's chain even
// when the platform-specific token is also set, so match-service can rotate
// independently.
func TestMatchServiceCallerTokenPrefersMatchSpecificEnv(t *testing.T) {
	t.Setenv("MATCH_INTERNAL_SERVICE_TOKEN", "match-specific")
	t.Setenv("PLATFORM_INTERNAL_SERVICE_TOKEN", "platform-specific")

	if got := matchServiceCallerToken(); got != "match-specific" {
		t.Fatalf("matchServiceCallerToken() = %q, want match-specific", got)
	}
	if got := matchServiceCallerToken(); got == "platform-specific" {
		t.Fatal("outbound match-service credential must not be the platform-specific token")
	}
}

func TestInternalServiceTokensCollectsEveryConfiguredValue(t *testing.T) {
	t.Setenv("PLATFORM_INTERNAL_SERVICE_TOKEN", "platform-specific")
	t.Setenv("CHESS404_INTERNAL_SERVICE_TOKEN", "shared")

	tokens := internalServiceTokens()
	if len(tokens) != 2 || tokens[0] != "platform-specific" || tokens[1] != "shared" {
		t.Fatalf("internalServiceTokens() = %v, want [platform-specific shared]", tokens)
	}
}
