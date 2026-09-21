package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// matchSecretResolvable is the subset of the match archive that the claim
// pipeline needs to decide whether a stored claim's secret is real.
type matchSecretResolvable struct {
	MatchID      string
	ModeID       string
	WhiteGuestID string
	BlackGuestID string
	Queue        string
}

// resolveMatchSeatSecret returns the plaintext secret of the seat owned by
// guestID on matchID. The match service redacts seat secrets from every
// snapshot it emits or persists, so this cannot be answered from the archive:
// it asks match-service directly over the internal, service-token-gated
// seat-secret endpoint. The guest's identity must already have been proven by
// a guest-session resume before this helper is called; this is pure
// service-to-server credential delivery for the proven owner.
func resolveMatchSeatSecret(matchID, guestID string) (string, error) {
	matchID = strings.TrimSpace(matchID)
	guestID = strings.TrimSpace(guestID)
	if matchID == "" || guestID == "" {
		return "", fmt.Errorf("matchId and guestId are required")
	}
	baseURL := strings.TrimSpace(resolveInternalServiceURLForPlatform("MATCH_SERVICE_INTERNAL_URL", "http://match-service:8080"))
	if baseURL == "" {
		return "", fmt.Errorf("match service URL is not configured")
	}
	token := configuredInternalServiceToken()
	if token == "" {
		return "", fmt.Errorf("internal service token is not configured")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	body, err := json.Marshal(map[string]string{"guestId": guestID})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/matches/"+matchID+"/seat-secret", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Chess404-Service-Token", token)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("match service returned status %d", resp.StatusCode)
	}
	var payload struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	secret := strings.TrimSpace(payload.Secret)
	if secret == "" {
		return "", fmt.Errorf("match service returned an empty seat secret")
	}
	return secret, nil
}

// seatSecretIsRedacted reports whether a snapshot-carried seat secret is
// actually usable. match-service replaces every secret with "<redacted>" in
// snapshots, and redactSeatSecrets writes "<redacted>" into the stored archive
// rows too, so claims built from archived state must never trust them.
func seatSecretIsRedacted(secret string) bool {
	trimmed := strings.TrimSpace(secret)
	return trimmed == "" || trimmed == redactedSeatSecretMarker
}

const redactedSeatSecretMarker = "<redacted>"

// resolveInternalServiceURLForPlatform mirrors the gateway's
// resolveInternalServiceURL handling for the misconfigured-Railway-variable
// shapes production has actually shipped: literal "${{...}}" template text and
// hostnames ending in a bare ":".
func resolveInternalServiceURLForPlatform(envKey, defaultURL string) string {
	u := strings.TrimSpace(os.Getenv(envKey))
	if u == "" {
		return defaultURL
	}
	if strings.Contains(u, "${{") {
		return defaultURL
	}
	if strings.HasSuffix(u, ":") {
		u += "8080"
	}
	return u
}

