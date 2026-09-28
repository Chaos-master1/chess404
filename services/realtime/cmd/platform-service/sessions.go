package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/chess404/realtime/internal/httputil"
	"github.com/chess404/realtime/internal/platform"
)

// Account-auth config, security-event audit, session fingerprints, and guest resume.

func accountAuthPreviewEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(httputil.EnvOrDefault("ACCOUNT_AUTH_EXPOSE_PREVIEW_TOKENS", "false")))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func accountAuthPublicBaseURL() string {
	return httputil.EnvOrDefault("ACCOUNT_AUTH_PUBLIC_BASE_URL", "")
}

func recordAccountSecurityEvent(store platform.AccountSecurityAuditDirectory, accountID, kind, detail string) {
	if store == nil || strings.TrimSpace(accountID) == "" {
		return
	}
	if _, err := store.RecordEvent(platform.AccountSecurityEventRequest{
		AccountID: accountID,
		Kind:      kind,
		Detail:    detail,
	}); err != nil {
		log.Printf("failed to record account security event %s for %s: %v", kind, accountID, err)
	}
}

func sessionTokenFingerprint(token string) string {
	resolved := strings.TrimSpace(token)
	if resolved == "" {
		return ""
	}
	if len(resolved) <= 12 {
		return resolved
	}
	return fmt.Sprintf("%s...%s", resolved[:8], resolved[len(resolved)-4:])
}

func openArchiveStore() (*platform.MatchArchiveStore, error) {
	switch strings.ToLower(httputil.EnvOrDefault("MATCH_ARCHIVE_BACKEND", "file")) {
	case "sqlite":
		return platform.NewSQLiteMatchArchiveStore(archiveSQLitePath())
	case "postgres":
		if sharedPostgresPool != nil {
			return platform.NewPostgresMatchArchiveStoreWithDB(sharedPostgresPool)
		}
		return platform.NewPostgresMatchArchiveStore(archivePostgresURL())
	default:
		return platform.NewMatchArchiveStore(archivePath())
	}
}

func resolvePrimaryGuestID(account platform.AccountProfile) string {
	if resolved := strings.TrimSpace(account.PrimaryGuestID); resolved != "" {
		return resolved
	}
	for _, guestID := range account.LinkedGuestIDs {
		if resolved := strings.TrimSpace(guestID); resolved != "" {
			return resolved
		}
	}
	return ""
}

// renameLinkedGuestToHandle keeps the guest row linked to an account in
// sync with the account identity: the match-creation chain names players
// from the guest displayName, so a signed-in player whose guest still
// carried a generated placeholder ("Ivory Bishop 101") would show that
// placeholder in every match instead of their handle. Best-effort: the
// guest row may not exist yet, and a rename failure must never block
// authentication.
func renameLinkedGuestToHandle(guests platform.GuestDirectory, account platform.AccountProfile) {
	guestID := strings.TrimSpace(account.PrimaryGuestID)
	handle := strings.TrimSpace(account.Handle)
	if guestID == "" || handle == "" {
		return
	}
	if _, err := guests.RenameGuest(guestID, handle); err != nil && err != os.ErrNotExist {
		log.Printf("[platform] WARN: could not rename guest %s to handle %s: %v", guestID, handle, err)
	}
}

func resumeGuestFromPayload(guests platform.GuestDirectory, guestID, sessionSecret, sessionToken string) (platform.GuestSession, error) {
	resolvedGuestID := strings.TrimSpace(guestID)
	if resolvedGuestID == "" {
		return platform.GuestSession{}, os.ErrInvalid
	}
	resolvedSecret := strings.TrimSpace(sessionSecret)
	resolvedToken := strings.TrimSpace(sessionToken)
	if resolvedToken != "" {
		session, err := guests.ResumeGuestByToken(resolvedGuestID, resolvedToken)
		if err == nil {
			return session, nil
		}
		if err != platform.ErrUnauthorizedGuestSession || resolvedSecret == "" {
			return platform.GuestSession{}, err
		}
	}
	return guests.ResumeGuest(resolvedGuestID, resolvedSecret)
}
