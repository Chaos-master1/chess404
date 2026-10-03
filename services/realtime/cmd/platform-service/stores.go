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

// isRecoverableMatchStatus answers "may this match still be resumed?".
// A LIVENESS question -- used by the "which game am I in?" lookup, which must
// never hand back a finished room.
func isRecoverableMatchStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "waiting", "active":
		return true
	default:
		return false
	}
}

// isReadableMatchStatus reports statuses for which a guest who has already
// proven seat ownership may still READ the match.
//
// This is deliberately NOT the liveness predicate above ("waiting"/"active"
// only). Reusing that one for reads conflated two unrelated questions and made
// finished games unreadable to the people who played them: the claims route
// refused the claim, the web layer then had no verified seat to scope the
// snapshot with, and its public-spectator gate requires status==active -- so
// both players got a 404 for their own finished game while match-service served
// it happily (observed live: a completed match returned 200 from
// match-service and 404 from web, with the client retrying for 34 minutes).
//
// "finished" is included on purpose: the final position, clocks and result are
// the player's own data. Granting read access cannot grant write access -- every
// mutating intent calls ensureActive() (match_actions.go, cards_play.go,
// cards_target_select.go), which rejects anything that is not "active".
//
// "aborted" and unknown statuses stay excluded: an aborted game is never
// persisted to the archive at all (history.go deletes the row on abort), so
// there is nothing legitimate to read.
func isReadableMatchStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "waiting", "active", "finished":
		return true
	default:
		return false
	}
}

func buildMatchSeatClaim(matchState contracts.MatchState, guestID, fallbackSecret string) (platform.MatchSeatClaim, bool) {
	claim, ok := buildMatchSeatClaimFromSnapshot(matchState, guestID, fallbackSecret)
	if !ok {
		return platform.MatchSeatClaim{}, false
	}
	// The authoritative seat secret lives only inside match-service: it
	// redacts seat secrets from every snapshot it emits or persists, so the
	// archive row carries either a "<redacted>" placeholder or an empty seat
	// secret. Resolving unconditionally (not only when the snapshot value is
	// redacted) matters because buildMatchSeatClaimFromSnapshot substitutes
	// the guest session secret when the archived seat secret is empty -- and
	// a claim holding the guest secret authenticates to HTTP but fails the
	// WS seat check with "unauthorized player secret". If the resolver is
	// unreachable, keep whatever the snapshot/fallback produced (best
	// effort) rather than failing the claim outright.
	if secret, err := resolveMatchSeatSecret(matchState.MatchID, guestID); err == nil {
		claim.PlayerSecret = secret
	} else {
		log.Printf("seat secret resolve failed for match=%s guest=%s: %v", matchState.MatchID, guestID, err)
	}
	return claim, true
}

func buildMatchSeatClaimFromSnapshot(matchState contracts.MatchState, guestID, fallbackSecret string) (platform.MatchSeatClaim, bool) {
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
	// NOTE: no fallback substitution here. An empty archived seat secret
	// must stay empty so buildMatchSeatClaim can resolve the authoritative
	// credential from match-service; masking it with the guest session
	// secret produced claims that passed HTTP but failed the WS seat check.
	_ = fallbackSecret
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

// isLikelyArchiveOutage reports whether the archive backend is currently in
// a state where LoadMatch's ok=false is plausibly TRANSIENT (backend closed
// mid-shutdown, IO failure) rather than a genuine "no such match". The
// signals available are process-wide and deliberately conservative: this
// only gates whether refreshStoredMatchClaim deletes a claim on a miss, so
// a false "outage" merely delays cleanup of a dead claim until TTL pruning;
// a false "healthy" burns the claim exactly like the pre-fix code.
// Tests in this package override this probe to simulate an outage.
var isLikelyArchiveOutage = platform.ArchiveBackendDegraded

// allowsStatus is the caller's own rule for "may this stored claim survive a
// refresh against an archived status?" -- isReadableMatchStatus for an
// ownership check on a named match, isRecoverableMatchStatus for the "which
// match am I currently in?" lookup. Passing the rule in keeps each route
// stating its own policy at its own call site.
func refreshStoredMatchClaim(
	archive *platform.MatchArchiveStore,
	claims *platform.MatchClaimStore,
	claim platform.MatchSeatClaim,
	fallbackSecret string,
	allowsStatus func(string) bool,
) (platform.MatchSeatClaim, bool) {
	matchState, _, ok := archive.LoadMatch(claim.MatchID)
	if !ok {
		// Distinguish "the archive has no recoverable row for this match"
		// (permanent -- the match finished or never existed, so the claim
		// is dead and may be consumed) from "the archive backend is
		// temporarily unreadable" (transient -- LoadMatch swallows backend
		// errors into ok=false). A transient outage used to delete the
		// claim, so the seat's single-use claim token burned on the first
		// retry storm and the seat 404'd forever. Archive outages are
		// bounded (see isLikelyArchiveOutage), so erring toward keeping
		// the claim is safe: the next prune cycle reaps genuinely dead
		// claims when their TTL expires.
		if !isLikelyArchiveOutage() {
			_ = claims.Delete(claim.MatchID, claim.GuestID)
		}
		return platform.MatchSeatClaim{}, false
	}
	if !allowsStatus(matchState.Status) {
		// The caller's rule rejected this status, so the claim is consumed. For
		// a named match that means an aborted (or unrecognised) row -- a merely
		// FINISHED match must not land here, because the owner still needs the
		// claim to read the final state and dropping it is what stranded
		// clients in a 404 retry loop. For the liveness lookup, a finished room
		// is consumed on purpose: "resume my game" must never return a
		// completed one.
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
	// The refresh is resolver-authoritative: a real seat secret (resolved
	// above) or a legacy archived real secret wins; placeholder/empty values
	// must NOT be patched with the guest session secret -- match-service
	// rejects it on the WS seat check, which is exactly the failure this
	// pipeline exists to prevent.
	_ = fallbackSecret
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
