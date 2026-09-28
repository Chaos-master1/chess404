package main

import (
	"testing"
)

func TestInternalTokenMatchesAny(t *testing.T) {
	expected := []string{"match-specific", "shared"}
	if !internalTokenMatchesAny("match-specific", expected) {
		t.Fatal("expected the specific token to authenticate")
	}
	if !internalTokenMatchesAny("shared", expected) {
		t.Fatal("expected the shared token to authenticate")
	}
	if internalTokenMatchesAny("wrong", expected) {
		t.Fatal("expected a wrong token to be rejected")
	}
	if internalTokenMatchesAny("", expected) {
		t.Fatal("expected an empty token to be rejected")
	}
	if internalTokenMatchesAny("match-specific", nil) {
		t.Fatal("expected an empty accept set to reject everything")
	}
}

// Staging MATCH_INTERNAL_SERVICE_TOKEN must make match-service accept its
// own specific token from callers.
func TestInternalServiceTokensIncludesMatchSpecific(t *testing.T) {
	t.Setenv("MATCH_INTERNAL_SERVICE_TOKEN", "match-specific")
	t.Setenv("CHESS404_INTERNAL_SERVICE_TOKEN", "shared")

	tokens := internalServiceTokens()
	if len(tokens) != 2 || tokens[0] != "match-specific" || tokens[1] != "shared" {
		t.Fatalf("internalServiceTokens() = %v, want [match-specific shared]", tokens)
	}
	if !internalTokenMatchesAny("match-specific", tokens) {
		t.Fatal("expected the match-specific token to be accepted")
	}
}

// The outbound credential for platform-service is independent of the
// inbound chain: MATCH_INTERNAL_SERVICE_TOKEN must never be SENT to
// platform-service, and with only MATCH staged the outbound credential is
// empty (finalization then skips, rather than failing auth downstream).
func TestPlatformServiceCallerTokenIgnoresMatchSpecific(t *testing.T) {
	t.Setenv("MATCH_INTERNAL_SERVICE_TOKEN", "match-specific")
	t.Setenv("PLATFORM_INTERNAL_SERVICE_TOKEN", "platform-specific")
	if got := platformServiceCallerToken(); got != "platform-specific" {
		t.Fatalf("platformServiceCallerToken() = %q, want platform-specific", got)
	}

	t.Setenv("PLATFORM_INTERNAL_SERVICE_TOKEN", "")
	if got := platformServiceCallerToken(); got != "" {
		t.Fatalf("platformServiceCallerToken() with only MATCH staged = %q, want empty", got)
	}
}
