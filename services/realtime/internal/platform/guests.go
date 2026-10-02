package platform

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

var ErrUnauthorizedGuestSession = errors.New("unauthorized guest session")

const defaultGuestSessionTTL = 24 * time.Hour

type GuestProfile struct {
	GuestID           string    `json:"guestId"`
	DisplayName       string    `json:"displayName"`
	Rating            int       `json:"rating"`
	// Per-mode guest ladders (open/hidden). 0 = no games in that mode yet;
	// the first rated game in a mode seeds the ladder from the blended
	// rating so returning guests do not restart at the default. Mirrors the
	// account-side modeRatings split (open and hidden genuinely reward
	// different skills); computer matches stay unrated.
	RatingOpen        int       `json:"ratingOpen,omitempty"`
	RatingHidden      int       `json:"ratingHidden,omitempty"`
	MatchesPlayed     int       `json:"matchesPlayed"`
	Wins              int       `json:"wins"`
	Losses            int       `json:"losses"`
	Draws             int       `json:"draws"`
	PlacementsRemaining int     `json:"placementsRemaining"`
	CreatedAt         time.Time `json:"createdAt"`
	LastSeenAt        time.Time `json:"lastSeenAt"`
}

type GuestSession struct {
	Guest         GuestProfile `json:"guest"`
	SessionSecret string       `json:"sessionSecret"`
	SessionToken  string       `json:"sessionToken,omitempty"`
	ExpiresAt     time.Time    `json:"expiresAt,omitempty"`
}

type GuestPrivateState struct {
	SessionSecret    string    `json:"sessionSecret,omitempty"`
	SessionToken     string    `json:"sessionToken,omitempty"`
	SessionExpiresAt time.Time `json:"sessionExpiresAt,omitempty"`
}

type GuestStore struct {
	mu               sync.Mutex
	path             string
	entries          map[string]GuestProfile
	private          map[string]GuestPrivateState
	finalizedMatches map[string]string
}

type guestStoreFile struct {
	Guests           map[string]GuestProfile      `json:"guests"`
	Private          map[string]GuestPrivateState `json:"private,omitempty"`
	FinalizedMatches map[string]string            `json:"finalizedMatches,omitempty"`
}

type GuestStoreStats struct {
	GuestCount          int `json:"guestCount"`
	FinalizedMatchCount int `json:"finalizedMatchCount"`
	RankedPlayers       int `json:"rankedPlayers"`
}

func (s *GuestStore) Backend() string {
	return "file"
}

func (s *GuestStore) Close() error {
	return nil
}

func NewGuestStore(path string) (*GuestStore, error) {
	store := &GuestStore{
		path:             path,
		entries:          make(map[string]GuestProfile),
		private:          make(map[string]GuestPrivateState),
		finalizedMatches: make(map[string]string),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *GuestStore) EnsureGuest(guestID, sessionSecret string) (GuestSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if guestID != "" {
		if entry, ok := s.entries[guestID]; ok {
			privateState := s.private[guestID]
			resolvedSecret := strings.TrimSpace(privateState.SessionSecret)
			switch {
			case resolvedSecret == "":
				resolvedSecret = firstNonEmpty(sessionSecret, "guestsess_"+randomToken(12))
				privateState.SessionSecret = resolvedSecret
			case subtle.ConstantTimeCompare([]byte(strings.TrimSpace(sessionSecret)), []byte(resolvedSecret)) != 1:
				return GuestSession{}, ErrUnauthorizedGuestSession
			}
			privateState = renewGuestPrivateState(privateState, now)
			s.private[guestID] = privateState
			entry.LastSeenAt = now
			s.entries[guestID] = entry
			return buildGuestSession(entry, privateState), s.persistLocked()
		}
	}

	if guestID == "" {
		guestID = "guest_" + randomToken(6)
	}
	if strings.TrimSpace(sessionSecret) == "" {
		sessionSecret = "guestsess_" + randomToken(12)
	}

	entry := GuestProfile{
		GuestID:             guestID,
		DisplayName:         generateGuestName(len(s.entries) + 1),
		Rating:              1200,
		PlacementsRemaining: defaultPlacementMatches,
		CreatedAt:           now,
		LastSeenAt:          now,
	}
	s.entries[guestID] = entry
	privateState := renewGuestPrivateState(GuestPrivateState{SessionSecret: strings.TrimSpace(sessionSecret)}, now)
	s.private[guestID] = privateState
	return buildGuestSession(entry, privateState), s.persistLocked()
}

func (s *GuestStore) IssueGuestSession(guestID string) (GuestSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	guestID = strings.TrimSpace(guestID)
	if guestID == "" {
		return GuestSession{}, os.ErrInvalid
	}

	entry, ok := s.entries[guestID]
	if !ok {
		return GuestSession{}, os.ErrNotExist
	}

	now := time.Now().UTC()
	privateState := s.private[guestID]
	if strings.TrimSpace(privateState.SessionSecret) == "" {
		privateState.SessionSecret = "guestsess_" + randomToken(12)
	}
	privateState = renewGuestPrivateState(privateState, now)
	entry.LastSeenAt = now
	s.entries[guestID] = entry
	s.private[guestID] = privateState
	if err := s.persistLocked(); err != nil {
		return GuestSession{}, err
	}

	return buildGuestSession(entry, privateState), nil
}

func (s *GuestStore) ResumeGuest(guestID, sessionSecret string) (GuestSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	guestID = strings.TrimSpace(guestID)
	if guestID == "" {
		return GuestSession{}, os.ErrInvalid
	}

	entry, ok := s.entries[guestID]
	if !ok {
		return GuestSession{}, os.ErrNotExist
	}

	privateState := s.private[guestID]
	resolvedSecret := strings.TrimSpace(privateState.SessionSecret)
	if resolvedSecret == "" {
		return GuestSession{}, ErrUnauthorizedGuestSession
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(sessionSecret)), []byte(resolvedSecret)) != 1 {
		return GuestSession{}, ErrUnauthorizedGuestSession
	}

	entry.LastSeenAt = time.Now().UTC()
	s.entries[guestID] = entry
	privateState = renewGuestPrivateState(privateState, entry.LastSeenAt)
	s.private[guestID] = privateState
	if err := s.persistLocked(); err != nil {
		return GuestSession{}, err
	}

	return buildGuestSession(entry, privateState), nil
}

func (s *GuestStore) ResumeGuestByToken(guestID, sessionToken string) (GuestSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	guestID = strings.TrimSpace(guestID)
	if guestID == "" {
		return GuestSession{}, os.ErrInvalid
	}

	entry, ok := s.entries[guestID]
	if !ok {
		return GuestSession{}, os.ErrNotExist
	}

	privateState := s.private[guestID]
	if !guestSessionTokenValid(privateState, sessionToken, time.Now().UTC()) {
		return GuestSession{}, ErrUnauthorizedGuestSession
	}

	now := time.Now().UTC()
	entry.LastSeenAt = now
	s.entries[guestID] = entry
	privateState = renewGuestPrivateState(privateState, now)
	s.private[guestID] = privateState
	if err := s.persistLocked(); err != nil {
		return GuestSession{}, err
	}

	return buildGuestSession(entry, privateState), nil
}

// guestModeKey maps a match mode to the GuestProfile ladder field it feeds.
// Computer matches return "" (unrated for ladder purposes, like accounts).
func guestModeKey(modeID contracts.MatchModeID) string {
	switch contracts.NormalizeMatchModeID(string(modeID)) {
	case contracts.MatchModeHiddenCards:
		return "hidden"
	case contracts.MatchModeOpenCards:
		return "open"
	default:
		return ""
	}
}

func guestLadderRating(profile GuestProfile, modeKey string) int {
	if modeKey == "hidden" {
		return profile.RatingHidden
	}
	return profile.RatingOpen
}

func setGuestLadderRating(profile *GuestProfile, modeKey string, rating int) {
	if modeKey == "hidden" {
		profile.RatingHidden = rating
		return
	}
	profile.RatingOpen = rating
}

// applyGuestModeElo updates both guests' per-mode ladder alongside the
// blended one. First game in a mode seeds from the blended rating (a
// 1500-blended guest must not restart at the 1200 default); Elo uses the
// same K-factor and floor as the blended update. Returns false when the
// match's mode has no ladder (computer, unknown).
func applyGuestModeElo(white, black *GuestProfile, modeID contracts.MatchModeID, winner string, kFactor float64) bool {
	modeKey := guestModeKey(modeID)
	if modeKey == "" {
		return false
	}
	whiteMode := guestLadderRating(*white, modeKey)
	blackMode := guestLadderRating(*black, modeKey)
	if whiteMode <= 0 {
		whiteMode = white.Rating
		if whiteMode <= 0 {
			whiteMode = defaultEloStartRating
		}
	}
	if blackMode <= 0 {
		blackMode = black.Rating
		if blackMode <= 0 {
			blackMode = defaultEloStartRating
		}
	}
	newWhite, newBlack := ApplyEloMatchResultWithK(whiteMode, blackMode, winner, kFactor, defaultEloMinRating)
	setGuestLadderRating(white, modeKey, newWhite)
	setGuestLadderRating(black, modeKey, newBlack)
	return true
}

// RenameGuest replaces a guest's display name. It exists so the guest row
// linked to an account can carry the account handle as its name (the
// match-creation chain uses the guest displayName); generating playful
// placeholder names for guests is fine, but a signed-in player must never
// show one.
func (s *GuestStore) RenameGuest(guestID, displayName string) (GuestProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	guestID = strings.TrimSpace(guestID)
	displayName = strings.TrimSpace(displayName)
	if guestID == "" || displayName == "" {
		return GuestProfile{}, os.ErrInvalid
	}
	entry, ok := s.entries[guestID]
	if !ok {
		return GuestProfile{}, os.ErrNotExist
	}
	entry.DisplayName = displayName
	entry.LastSeenAt = time.Now().UTC()
	s.entries[guestID] = entry
	if err := s.persistLocked(); err != nil {
		return GuestProfile{}, err
	}
	return entry, nil
}

func (s *GuestStore) FinalizeMatch(matchID, whiteGuestID, blackGuestID, winner string, modeID contracts.MatchModeID) (GuestProfile, GuestProfile, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	white, ok := s.entries[whiteGuestID]
	if !ok {
		return GuestProfile{}, GuestProfile{}, false, os.ErrNotExist
	}
	black, ok := s.entries[blackGuestID]
	if !ok {
		return GuestProfile{}, GuestProfile{}, false, os.ErrNotExist
	}
	if matchID == "" {
		return GuestProfile{}, GuestProfile{}, false, os.ErrInvalid
	}
	if _, alreadyFinalized := s.finalizedMatches[matchID]; alreadyFinalized {
		return white, black, false, nil
	}

	now := time.Now().UTC()
	// Per-seat K factors: placements outrank everything, otherwise the K
	// decays with games played (same curve as the account ladder -- see
	// eloKFactorForGames).
	whiteK := eloKFactorForGames(white.MatchesPlayed)
	blackK := eloKFactorForGames(black.MatchesPlayed)
	if white.PlacementsRemaining > 0 {
		whiteK = defaultPlacementEloKFactor
	}
	if black.PlacementsRemaining > 0 {
		blackK = defaultPlacementEloKFactor
	}
	newWhite, _ := ApplyEloMatchResultWithK(white.Rating, black.Rating, winner, whiteK, defaultEloMinRating)
	_, newBlack := ApplyEloMatchResultWithK(white.Rating, black.Rating, winner, blackK, defaultEloMinRating)
	applyGuestModeElo(&white, &black, modeID, winner, whiteK)
	switch winner {
	case "white":
		white.Rating = newWhite
		black.Rating = newBlack
		white.Wins++
		black.Losses++
	case "black":
		white.Rating = newWhite
		black.Rating = newBlack
		black.Wins++
		white.Losses++
	case "draw":
		white.Rating = newWhite
		black.Rating = newBlack
		white.Draws++
		black.Draws++
	default:
		return GuestProfile{}, GuestProfile{}, false, os.ErrInvalid
	}
	white.MatchesPlayed++
	black.MatchesPlayed++
	if white.PlacementsRemaining > 0 {
		white.PlacementsRemaining--
	}
	if black.PlacementsRemaining > 0 {
		black.PlacementsRemaining--
	}

	white.LastSeenAt = now
	black.LastSeenAt = now
	s.entries[whiteGuestID] = white
	s.entries[blackGuestID] = black
	s.finalizedMatches[matchID] = winner
	return white, black, true, s.persistLocked()
}

func (s *GuestStore) ListGuests(limit int) []GuestProfile {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]GuestProfile, 0, len(s.entries))
	for _, guest := range s.entries {
		items = append(items, guest)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Rating == items[j].Rating {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].Rating > items[j].Rating
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func (s *GuestStore) GetGuest(guestID string) (GuestProfile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	guest, ok := s.entries[guestID]
	return guest, ok
}

func (s *GuestStore) ListRecentGuests(limit int) []GuestProfile {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]GuestProfile, 0, len(s.entries))
	for _, guest := range s.entries {
		items = append(items, guest)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].LastSeenAt.After(items[j].LastSeenAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func (s *GuestStore) Stats() GuestStoreStats {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats := GuestStoreStats{
		GuestCount:          len(s.entries),
		FinalizedMatchCount: len(s.finalizedMatches),
	}
	for _, guest := range s.entries {
		if guest.MatchesPlayed > 0 {
			stats.RankedPlayers++
		}
	}
	return stats
}

func (s *GuestStore) load() error {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var wrapped guestStoreFile
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Guests != nil {
		s.entries = wrapped.Guests
		if wrapped.Private != nil {
			s.private = wrapped.Private
		}
		if wrapped.FinalizedMatches != nil {
			s.finalizedMatches = wrapped.FinalizedMatches
		}
		return nil
	}
	var entries map[string]GuestProfile
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	s.entries = entries
	return nil
}

func (s *GuestStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(guestStoreFile{
		Guests:           s.entries,
		Private:          s.private,
		FinalizedMatches: s.finalizedMatches,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isGeneratedGuestName(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || trimmed == "Anonymous" {
		return true
	}
	parts := strings.Split(trimmed, " ")
	if len(parts) == 3 {
		if _, err := strconv.Atoi(parts[2]); err == nil {
			return true
		}
	}
	return false
}

func sanitizeGuestDisplayName(name string) string {
	if isGeneratedGuestName(name) {
		return "Anonymous"
	}
	return name
}

func generateGuestName(_ int) string {
	return "Anonymous"
}

func randomToken(bytesCount int) string {
	buf := make([]byte, bytesCount)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func renewGuestPrivateState(state GuestPrivateState, now time.Time) GuestPrivateState {
	state.SessionSecret = strings.TrimSpace(state.SessionSecret)
	state.SessionToken = strings.TrimSpace(state.SessionToken)
	if state.SessionToken == "" {
		state.SessionToken = "guesttok_" + randomToken(18)
	}
	state.SessionExpiresAt = now.Add(defaultGuestSessionTTL).UTC()
	return state
}

func buildGuestSession(entry GuestProfile, privateState GuestPrivateState) GuestSession {
	entry.DisplayName = sanitizeGuestDisplayName(entry.DisplayName)
	return GuestSession{
		Guest:         entry,
		SessionSecret: strings.TrimSpace(privateState.SessionSecret),
		SessionToken:  strings.TrimSpace(privateState.SessionToken),
		ExpiresAt:     privateState.SessionExpiresAt.UTC(),
	}
}

// PublicGuestSession is the sanitized JSON view of a guest session, suitable
// for read endpoints. It strips the long-lived session secret and opaque
// session token, exposing only the public profile and expiry. Issue-time
// endpoints (login, register, resume-rotates) must construct the
// IssuedGuestSession envelope below to deliver credentials to the caller.
type PublicGuestSession struct {
	Guest     GuestProfile `json:"guest"`
	ExpiresAt time.Time    `json:"expiresAt,omitempty"`
}

// IssuedGuestSession is the JSON envelope returned at credential-issue time.
// It embeds the public view plus the credentials the caller needs to keep
// using the session. It must only be used at login / register /
// resume-rotates endpoints.
type IssuedGuestSession struct {
	Guest         GuestProfile `json:"guest"`
	SessionSecret string       `json:"sessionSecret"`
	SessionToken  string       `json:"sessionToken,omitempty"`
	ExpiresAt     time.Time    `json:"expiresAt,omitempty"`
}

// PublicView returns the sanitized session for read endpoints.
func (s GuestSession) PublicView() PublicGuestSession {
	return PublicGuestSession{
		Guest:     s.Guest,
		ExpiresAt: s.ExpiresAt,
	}
}

// IssuedView returns the credential-bearing session for issue-time endpoints.
func (s GuestSession) IssuedView() IssuedGuestSession {
	return IssuedGuestSession{
		Guest:         s.Guest,
		SessionSecret: s.SessionSecret,
		SessionToken:  s.SessionToken,
		ExpiresAt:     s.ExpiresAt,
	}
}

func guestSessionTokenValid(state GuestPrivateState, sessionToken string, now time.Time) bool {
	resolvedToken := strings.TrimSpace(state.SessionToken)
	if resolvedToken == "" || strings.TrimSpace(sessionToken) == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(sessionToken)), []byte(resolvedToken)) != 1 {
		return false
	}
	if state.SessionExpiresAt.IsZero() || !state.SessionExpiresAt.After(now) {
		return false
	}
	return true
}
