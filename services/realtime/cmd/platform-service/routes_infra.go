package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/chess404/realtime/internal/metrics"
	"github.com/chess404/realtime/internal/platform"
)

// registerInfraRoutes wires the infra endpoints.
func registerInfraRoutes(mux *http.ServeMux, archive *platform.MatchArchiveStore, guests platform.GuestDirectory, accounts platform.AccountDirectory, friends platform.FriendshipDirectory, moderation platform.ModerationDirectory, claims *platform.MatchClaimStore) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		if pingable, ok := accounts.(interface{ Ping() error }); ok {
			if err := pingable.Ping(); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("database unavailable"))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if pingable, ok := accounts.(interface{ Ping() error }); ok {
			if err := pingable.Ping(); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("database unavailable"))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.Handle("/metrics", metrics.Handler())

	mux.HandleFunc("/api/platform/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		trustedResultFinalization := configuredInternalServiceToken() != ""
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"cardChessFlagship":       true,
			"publicBetaReady":         envBool("CHESS404_PUBLIC_BETA_READY", false) && trustedResultFinalization,
			"freeCorePlay":            true,
			"serverAuthoritative":     true,
			"publicSnapshotsSafe":     true,
			"deterministicReplays":    true,
			"queueTruth":              true,
			"modeSeparation":          true,
			"trustedFinalization":     trustedResultFinalization,
			"guestPlay":               true,
			"rankedRequiresID":        true,
			"accountRegistration":     true,
			"profiles":                true,
			"ratings":                 true,
			"matchHistory":            true,
			"friends":                 true,
			"friendChallenges":        true,
			"inbox":                   true,
			"presence":                true,
			"moderation":              true,
			"moderationAdmin":         moderationAdminConfigured(),
			"emailVerification":       true,
			"passwordReset":           true,
			"authEmailDelivery":       true,
			"authEmailDispatch":       configuredAccountEmailDeliveryProvider() != "disabled",
			"accountSecurityActivity": true,
			"fairPlayTelemetry":       false,
			"seasons":                 true,
			"achievements":            false,
			"dailyChallenges":         false,
			"cardAcademy":             false,
			"botPractice":             false,
			"puzzles":                 false,
			"postGameReview":          false,
			"tournaments":             false,
			"supporterCosmetics":      false,
		})
	})

	mux.HandleFunc("/api/platform/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":              "ok",
			"service":             "platform-service",
			"checkedAt":           time.Now().UTC(),
			"archiveBackend":      archive.Backend(),
			"guestStoreBackend":   guests.Backend(),
			"accountStoreBackend": accounts.Backend(),
			"claimStoreBackend":   claims.Backend(),
			"claimLeaseSeconds":   claims.TTLSeconds(),
			// The status page reads archive/accounts/claims/guests as required
			// objects. Omitting them threw a null dereference in the client and
			// took the whole /status route down with it.
			"archive":  archive.Stats(),
			"accounts": accounts.Stats(),
			"claims":   claims.Stats(),
			"guests":   guests.Stats(),
		})
	})
}
