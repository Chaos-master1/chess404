package main

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/httputil"
	"github.com/chess404/realtime/internal/platform"
)

// Store path/URL resolution, store openers (SQLite/Postgres), and the anticheat retention loop.

func archivePath() string {
	if value := os.Getenv("MATCH_ARCHIVE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "match-archive.json")
}

func archiveSQLitePath() string {
	if value := os.Getenv("MATCH_ARCHIVE_SQLITE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "match-archive.sqlite")
}

func archivePostgresURL() string {
	return httputil.EnvOrDefault("MATCH_ARCHIVE_POSTGRES_URL", "")
}

func guestStorePath() string {
	if value := os.Getenv("GUEST_STORE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "guest-profiles.json")
}

func guestStoreSQLitePath() string {
	if value := os.Getenv("GUEST_STORE_SQLITE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "guest-profiles.sqlite")
}

func guestStorePostgresURL() string {
	return httputil.EnvOrDefault("GUEST_STORE_POSTGRES_URL", "")
}

func accountStorePath() string {
	if value := os.Getenv("ACCOUNT_STORE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "accounts.json")
}

func accountStoreSQLitePath() string {
	if value := os.Getenv("ACCOUNT_STORE_SQLITE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "accounts.sqlite")
}

func accountStorePostgresURL() string {
	return httputil.EnvOrDefault("ACCOUNT_STORE_POSTGRES_URL", "")
}

func friendshipStorePath() string {
	if value := os.Getenv("FRIEND_STORE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "friendships.json")
}

func friendshipStoreSQLitePath() string {
	if value := os.Getenv("FRIEND_STORE_SQLITE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "friendships.sqlite")
}

func friendshipStorePostgresURL() string {
	return httputil.EnvOrDefault("FRIEND_STORE_POSTGRES_URL", "")
}

func directChallengeStorePath() string {
	if value := os.Getenv("DIRECT_CHALLENGE_STORE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "direct_challenges.json")
}

func directChallengeStoreSQLitePath() string {
	if value := os.Getenv("DIRECT_CHALLENGE_STORE_SQLITE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "direct_challenges.sqlite")
}

func moderationStorePath() string {
	if value := os.Getenv("MODERATION_STORE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "moderation.json")
}

func moderationStoreSQLitePath() string {
	if value := os.Getenv("MODERATION_STORE_SQLITE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "moderation.sqlite")
}

func moderationStorePostgresURL() string {
	return httputil.EnvOrDefault("MODERATION_STORE_POSTGRES_URL", "")
}

func directChallengeStorePostgresURL() string {
	return httputil.EnvOrDefault("DIRECT_CHALLENGE_STORE_POSTGRES_URL", "")
}

func notificationStorePath() string {
	if value := os.Getenv("NOTIFICATION_STORE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "notifications.json")
}

func notificationStoreSQLitePath() string {
	if value := os.Getenv("NOTIFICATION_STORE_SQLITE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "notifications.sqlite")
}

func notificationStorePostgresURL() string {
	return httputil.EnvOrDefault("NOTIFICATION_STORE_POSTGRES_URL", "")
}

func accountEmailOutboxStorePath() string {
	if value := os.Getenv("ACCOUNT_EMAIL_OUTBOX_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "account-email-outbox.json")
}

func accountEmailOutboxSQLitePath() string {
	if value := os.Getenv("ACCOUNT_EMAIL_OUTBOX_SQLITE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "account-email-outbox.sqlite")
}

func accountEmailOutboxPostgresURL() string {
	return httputil.EnvOrDefault("ACCOUNT_EMAIL_OUTBOX_POSTGRES_URL", "")
}

func accountSecurityAuditStorePath() string {
	if value := os.Getenv("ACCOUNT_SECURITY_AUDIT_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "account-security-audit.json")
}

func accountSecurityAuditSQLitePath() string {
	if value := os.Getenv("ACCOUNT_SECURITY_AUDIT_SQLITE_PATH"); value != "" {
		return value
	}
	return filepath.Join("data", "account-security-audit.sqlite")
}

func accountSecurityAuditPostgresURL() string {
	return httputil.EnvOrDefault("ACCOUNT_SECURITY_AUDIT_POSTGRES_URL", "")
}

func matchClaimStoreRedisURL() string {
	return httputil.EnvOrDefault("MATCH_CLAIM_STORE_REDIS_URL", "")
}

func isRecoverableMatchStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "waiting", "active":
		return true
	default:
		return false
	}
}

func buildMatchSeatClaim(matchState contracts.MatchState, guestID, fallbackSecret string) (platform.MatchSeatClaim, bool) {
	seatColor := ""
	playerSecret := ""
	switch guestID {
	case matchState.WhiteGuestID:
		seatColor = "white"
		playerSecret = matchState.WhitePlayerSecret
	case matchState.BlackGuestID:
		seatColor = "black"
		playerSecret = matchState.BlackPlayerSecret
	default:
		return platform.MatchSeatClaim{}, false
	}
	if strings.TrimSpace(playerSecret) == "" {
		playerSecret = strings.TrimSpace(fallbackSecret)
	}
	return platform.MatchSeatClaim{
		MatchID:      matchState.MatchID,
		GuestID:      guestID,
		SeatColor:    seatColor,
		PlayerID:     guestID,
		PlayerSecret: playerSecret,
		Queue:        matchState.Queue,
		ModeID:       matchState.ModeID,
		WhiteGuestID: matchState.WhiteGuestID,
		BlackGuestID: matchState.BlackGuestID,
		WhiteName:    matchState.WhiteName,
		BlackName:    matchState.BlackName,
	}, true
}

func refreshStoredMatchClaim(
	archive *platform.MatchArchiveStore,
	claims *platform.MatchClaimStore,
	claim platform.MatchSeatClaim,
	fallbackSecret string,
) (platform.MatchSeatClaim, bool) {
	matchState, _, ok := archive.LoadMatch(claim.MatchID)
	if !ok || !isRecoverableMatchStatus(matchState.Status) {
		_ = claims.Delete(claim.MatchID, claim.GuestID)
		return platform.MatchSeatClaim{}, false
	}
	refreshed, ok := buildMatchSeatClaim(matchState, claim.GuestID, fallbackSecret)
	if !ok {
		_ = claims.Delete(claim.MatchID, claim.GuestID)
		return platform.MatchSeatClaim{}, false
	}
	refreshed.ClaimToken = claim.ClaimToken
	refreshed.ExpiresAt = claim.ExpiresAt
	return refreshed, true
}

func matchClaimStoreRedisKey() string {
	return httputil.EnvOrDefault("MATCH_CLAIM_STORE_REDIS_KEY", "chess404:platform:match-claims")
}

func matchClaimStoreTTL() time.Duration {
	seconds := platform.ParseListLimit(os.Getenv("MATCH_CLAIM_STORE_TTL_SECONDS"), int((12*time.Hour)/time.Second))
	if seconds <= 0 {
		seconds = int((12 * time.Hour) / time.Second)
	}
	return time.Duration(seconds) * time.Second
}

func moderationAdminConfigured() bool {
	return len(configuredModerationAdminHandles()) > 0 || len(configuredModerationAdminAccountIDs()) > 0
}

func configuredModerationAdminHandles() map[string]struct{} {
	return parseModerationAdminSet(os.Getenv("PLATFORM_ADMIN_HANDLES"), true)
}

func configuredModerationAdminAccountIDs() map[string]struct{} {
	return parseModerationAdminSet(os.Getenv("PLATFORM_ADMIN_ACCOUNT_IDS"), false)
}

func parseModerationAdminSet(value string, lowercase bool) map[string]struct{} {
	items := make(map[string]struct{})
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ',', ';', '\n', '\r', '\t', ' ':
			return true
		default:
			return false
		}
	}) {
		resolved := strings.TrimSpace(part)
		if lowercase {
			resolved = strings.ToLower(resolved)
		}
		if resolved == "" {
			continue
		}
		items[resolved] = struct{}{}
	}
	return items
}

func openGuestDirectory() (platform.GuestDirectory, error) {
	switch strings.ToLower(httputil.EnvOrDefault("GUEST_STORE_BACKEND", "file")) {
	case "sqlite":
		return platform.NewSQLiteGuestStore(guestStoreSQLitePath())
	case "postgres":
		return openPostgresGuestStore()
	default:
		return platform.NewGuestStore(guestStorePath())
	}
}

func openAccountStore() (platform.AccountDirectory, error) {
	switch strings.ToLower(httputil.EnvOrDefault("ACCOUNT_STORE_BACKEND", "file")) {
	case "sqlite":
		return platform.NewSQLiteAccountStore(accountStoreSQLitePath())
	case "postgres":
		return openPostgresAccountStore()
	default:
		return platform.NewAccountStore(accountStorePath())
	}
}

func openFriendshipStore() (platform.FriendshipDirectory, error) {
	switch strings.ToLower(httputil.EnvOrDefault("FRIEND_STORE_BACKEND", "file")) {
	case "sqlite":
		return platform.NewSQLiteFriendshipStore(friendshipStoreSQLitePath())
	case "postgres":
		return openPostgresFriendshipStore()
	default:
		return platform.NewFriendshipStore(friendshipStorePath())
	}
}

func openModerationStore() (platform.ModerationDirectory, error) {
	switch strings.ToLower(httputil.EnvOrDefault("MODERATION_STORE_BACKEND", "file")) {
	case "sqlite":
		return platform.NewSQLiteModerationStore(moderationStoreSQLitePath())
	case "postgres":
		return openPostgresModerationStore()
	default:
		return platform.NewModerationStore(moderationStorePath())
	}
}

func openDirectChallengeStore() (platform.DirectChallengeDirectory, error) {
	switch strings.ToLower(httputil.EnvOrDefault("DIRECT_CHALLENGE_STORE_BACKEND", "file")) {
	case "sqlite":
		return platform.NewSQLiteDirectChallengeStore(directChallengeStoreSQLitePath())
	case "postgres":
		return openPostgresDirectChallengeStore()
	default:
		return platform.NewDirectChallengeStore(directChallengeStorePath())
	}
}

func openNotificationStore() (platform.AccountNotificationDirectory, error) {
	switch strings.ToLower(httputil.EnvOrDefault("NOTIFICATION_STORE_BACKEND", "file")) {
	case "sqlite":
		return platform.NewSQLiteAccountNotificationStore(notificationStoreSQLitePath())
	case "postgres":
		return openPostgresNotificationStore()
	default:
		return platform.NewAccountNotificationStore(notificationStorePath())
	}
}

func openAccountEmailOutboxStore() (platform.AccountEmailOutboxDirectory, error) {
	switch strings.ToLower(httputil.EnvOrDefault("ACCOUNT_EMAIL_OUTBOX_BACKEND", "file")) {
	case "sqlite":
		return platform.NewSQLiteAccountEmailOutboxStore(accountEmailOutboxSQLitePath())
	case "postgres":
		return openPostgresAccountEmailOutboxStore()
	default:
		return platform.NewAccountEmailOutboxStore(accountEmailOutboxStorePath())
	}
}

func openAccountSecurityAuditStore() (platform.AccountSecurityAuditDirectory, error) {
	switch strings.ToLower(httputil.EnvOrDefault("ACCOUNT_SECURITY_AUDIT_BACKEND", "file")) {
	case "sqlite":
		return platform.NewSQLiteAccountSecurityAuditStore(accountSecurityAuditSQLitePath())
	case "postgres":
		return openPostgresAccountSecurityAuditStore()
	default:
		return platform.NewAccountSecurityAuditStore(accountSecurityAuditStorePath())
	}
}

func openAnticheatStore() (platform.AnticheatStore, error) {
	switch strings.ToLower(httputil.EnvOrDefault("ANTICHEAT_BACKEND", "memory")) {
	case "postgres":
		return openPostgresAnticheatStore()
	case "sqlite":
		return platform.NewSqliteAnticheatStore(anticheatSQLitePath())
	default:
		return platform.NewInMemoryAnticheatStore(), nil
	}
}

// openPostgresGuestStore opens the guest store, using the shared pool when
// PLATFORM_POSTGRES_URL is set, otherwise falling back to the per-store URL.
func openPostgresGuestStore() (platform.GuestDirectory, error) {
	if sharedPostgresPool != nil {
		return platform.NewPostgresGuestStoreWithDB(sharedPostgresPool)
	}
	return platform.NewPostgresGuestStore(guestStorePostgresURL())
}

func openPostgresAccountStore() (platform.AccountDirectory, error) {
	if sharedPostgresPool != nil {
		return platform.NewPostgresAccountStoreWithDB(sharedPostgresPool)
	}
	return platform.NewPostgresAccountStore(accountStorePostgresURL())
}

func openPostgresFriendshipStore() (platform.FriendshipDirectory, error) {
	if sharedPostgresPool != nil {
		return platform.NewPostgresFriendshipStoreWithDB(sharedPostgresPool)
	}
	return platform.NewPostgresFriendshipStore(friendshipStorePostgresURL())
}

func openPostgresModerationStore() (platform.ModerationDirectory, error) {
	if sharedPostgresPool != nil {
		return platform.NewPostgresModerationStoreWithDB(sharedPostgresPool)
	}
	return platform.NewPostgresModerationStore(moderationStorePostgresURL())
}

func openPostgresDirectChallengeStore() (platform.DirectChallengeDirectory, error) {
	if sharedPostgresPool != nil {
		return platform.NewPostgresDirectChallengeStoreWithDB(sharedPostgresPool)
	}
	return platform.NewPostgresDirectChallengeStore(directChallengeStorePostgresURL())
}

func openPostgresNotificationStore() (platform.AccountNotificationDirectory, error) {
	if sharedPostgresPool != nil {
		return platform.NewPostgresAccountNotificationStoreWithDB(sharedPostgresPool)
	}
	return platform.NewPostgresAccountNotificationStore(notificationStorePostgresURL())
}

func openPostgresAccountEmailOutboxStore() (platform.AccountEmailOutboxDirectory, error) {
	if sharedPostgresPool != nil {
		return platform.NewPostgresAccountEmailOutboxStoreWithDB(sharedPostgresPool)
	}
	return platform.NewPostgresAccountEmailOutboxStore(accountEmailOutboxPostgresURL())
}

func openPostgresAccountSecurityAuditStore() (platform.AccountSecurityAuditDirectory, error) {
	if sharedPostgresPool != nil {
		return platform.NewPostgresAccountSecurityAuditStoreWithDB(sharedPostgresPool)
	}
	return platform.NewPostgresAccountSecurityAuditStore(accountSecurityAuditPostgresURL())
}

func openPostgresAnticheatStore() (platform.AnticheatStore, error) {
	if sharedPostgresPool != nil {
		return platform.NewPostgresAnticheatStoreWithDB(sharedPostgresPool)
	}
	return platform.NewPostgresAnticheatStore(anticheatPostgresURL())
}

func runAnticheatRetentionLoop(store platform.AnticheatStore) {
	retentionDays := 30
	if raw := strings.TrimSpace(os.Getenv("ANTICHEAT_RETENTION_DAYS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			retentionDays = parsed
		}
	}
	interval := time.Duration(retentionDays) * 24 * time.Hour
	if interval < time.Hour {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
		removed, err := store.PruneAnalysesOlderThan(cutoff)
		if err != nil {
			log.Printf("platform:anticheat: retention prune failed: %v", err)
			continue
		}
		if removed > 0 {
			log.Printf("platform:anticheat: pruned %d analyses older than %s", removed, cutoff.Format(time.RFC3339))
		}
	}
}

func anticheatPostgresURL() string {
	return httputil.EnvOrDefault("ANTICHEAT_POSTGRES_URL", httputil.EnvOrDefault("PLATFORM_POSTGRES_URL", ""))
}

func anticheatSQLitePath() string {
	return httputil.EnvOrDefault("ANTICHEAT_SQLITE_PATH", "./data/anticheat.sqlite")
}

func openMatchClaimStore() (*platform.MatchClaimStore, error) {
	switch strings.ToLower(httputil.EnvOrDefault("MATCH_CLAIM_STORE_BACKEND", "memory")) {
	case "redis":
		return platform.NewRedisMatchClaimStoreWithTTL(matchClaimStoreRedisURL(), matchClaimStoreRedisKey(), matchClaimStoreTTL())
	default:
		return platform.NewMatchClaimStoreWithTTL(matchClaimStoreTTL()), nil
	}
}
