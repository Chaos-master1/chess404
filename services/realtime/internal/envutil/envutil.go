package envutil

import (
	"fmt"
	"os"
	"strings"
)

func Require(keys ...string) {
	var missing []string
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "FATAL: missing required environment variables: %s\n", strings.Join(missing, ", "))
		os.Exit(1)
	}
}

func Optional(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// WarnSharedInternalToken logs a production warning when a service accepts
// the legacy SHARED internal token (one token value across all services).
// The shared token is the accepted-but-deprecated configuration: any single
// leaked proxy route then inherits internal-caller trust everywhere. The
// migration is per-service tokens (MATCH_INTERNAL_SERVICE_TOKEN,
// PLATFORM_INTERNAL_SERVICE_TOKEN, GATEWAY_INTERNAL_SERVICE_TOKEN); until
// every caller and callee is moved, the shared value must keep working --
// hence a warning, not a failure. Called once at service boot with the
// service's OWN specific token env name (if any) and the list of shared
// fallback names it accepts.
func WarnSharedInternalToken(serviceName, specificToken, specificEnv string, sharedEnvs []string) {
	specific := strings.TrimSpace(specificToken)
	if specific == "" {
		fmt.Fprintf(os.Stderr, "[security] %s: no service-specific token set (env %s); accepting shared internal token\n", serviceName, specificEnv)
		return
	}
	for _, name := range sharedEnvs {
		shared := strings.TrimSpace(os.Getenv(name))
		if shared != "" && shared == specific {
			fmt.Fprintf(os.Stderr, "[security] %s: the shared token env %s holds the SAME value as %s -- replace it with a per-service token (see RUNBOOK.md, internal service tokens)\n", serviceName, name, specificEnv)
			return
		}
	}
}
