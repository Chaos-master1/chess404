package match

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/logging"
	"github.com/chess404/realtime/internal/metrics"
)

const (
	rulesVersion                 = "v1-alpha-foundation"
	defaultClock                 = int64(10 * 60 * 1000)
	maxHandSize                  = 10
	drawFromRound                = 8
	drawEveryRounds              = 3
	presenceHeartbeatTimeout     = 25 * time.Second
	disconnectGracePeriod        = 45 * time.Second
	disconnectGraceBothPeriod    = 2 * time.Minute
	disconnectGraceBoth          = "both"
	maxIntentsPerSecondPerPlayer = 10
	matchMapShards               = 32
)

// computerOpponentImpl selects the vs-computer brain: "search" (default)
// uses the rebuilt engine stack for chess moves (v1 keeps cards), "v1" is
// the legacy heuristic opponent kept as an instant rollback switch.
var computerOpponentImpl = strings.ToLower(strings.TrimSpace(func() string {
	if v := os.Getenv("COMPUTER_OPPONENT"); v != "" {
		return v
	}
	return "search"
}()))

type matchShard struct {
	mu      sync.RWMutex
	matches map[string]*matchContainer
}

var (
	ErrMatchNotFound     = errors.New("match not found")
	ErrMatchSeatFull     = errors.New("match has no open seats")
	ErrMatchJoinFinished = errors.New("match is finished")
	// ErrUnauthorizedSeatClaim is returned when a caller matches a seat's guest
	// ID but cannot prove ownership with that seat's player secret.
	ErrUnauthorizedSeatClaim = errors.New("unauthorized seat claim")
	ErrStaleClientState      = errors.New("client state is stale; refresh from latest snapshot")
)

type matchContainer struct {
	mu       sync.Mutex
	state    *contracts.MatchState
	events   []contracts.ResolvedEvent
	presence *matchPresenceState
	subs     map[chan contracts.MatchSnapshotResponse]string
	seqNum   int64
	computer computerOpponent
}

func newMatchContainer(state *contracts.MatchState, events []contracts.ResolvedEvent, presence *matchPresenceState) *matchContainer {
	return &matchContainer{
		state:    state,
		events:   events,
		presence: presence,
		subs:     make(map[chan contracts.MatchSnapshotResponse]string),
	}
}

type computerMoveTask struct {
	c   *matchContainer
	now time.Time
}

type matchMap struct {
	shards [matchMapShards]*matchShard
}

func newMatchMap() *matchMap {
	mm := &matchMap{}
	for i := 0; i < matchMapShards; i++ {
		mm.shards[i] = &matchShard{matches: make(map[string]*matchContainer)}
	}
	return mm
}

func (mm *matchMap) shardFor(matchID string) *matchShard {
	h := sha256.Sum256([]byte(matchID))
	idx := int(h[0]) % matchMapShards
	return mm.shards[idx]
}

func (mm *matchMap) Load(matchID string) (*matchContainer, bool) {
	s := mm.shardFor(matchID)
	s.mu.RLock()
	c, ok := s.matches[matchID]
	s.mu.RUnlock()
	return c, ok
}

func (mm *matchMap) Store(matchID string, c *matchContainer) {
	s := mm.shardFor(matchID)
	s.mu.Lock()
	s.matches[matchID] = c
	s.mu.Unlock()
}

func (mm *matchMap) Delete(matchID string) {
	s := mm.shardFor(matchID)
	s.mu.Lock()
	delete(s.matches, matchID)
	s.mu.Unlock()
}

func (mm *matchMap) Len() int {
	total := 0
	for i := 0; i < matchMapShards; i++ {
		mm.shards[i].mu.RLock()
		total += len(mm.shards[i].matches)
		mm.shards[i].mu.RUnlock()
	}
	return total
}

func (mm *matchMap) Range(fn func(matchID string, c *matchContainer) bool) {
	for i := 0; i < matchMapShards; i++ {
		mm.shards[i].mu.RLock()
		for id, c := range mm.shards[i].matches {
			if !fn(id, c) {
				mm.shards[i].mu.RUnlock()
				return
			}
		}
		mm.shards[i].mu.RUnlock()
	}
}

func (mm *matchMap) RangeLocked(fn func(matchID string, c *matchContainer)) {
	for i := 0; i < matchMapShards; i++ {
		mm.shards[i].mu.Lock()
		for id, c := range mm.shards[i].matches {
			fn(id, c)
		}
		mm.shards[i].mu.Unlock()
	}
}

type Service struct {
	mu               sync.Mutex
	matches          *matchMap
	archive          MatchArchiver
	store            MatchStore
	broadcaster      Broadcaster
	stopCh           chan struct{}
	authTokens       map[string]authTokenEntry
	tokenStore       TokenStore
	Log              *logging.Logger
	computerCh       chan computerMoveTask
	computerWorkerWg sync.WaitGroup

	// instanceID tags every snapshot this process publishes to the shared
	// broadcaster so relayRedisBroadcasts can recognize and skip its own
	// process's publishes -- without it, a process that both mutates a match
	// and relays for it would deliver every broadcast to its local
	// subscribers twice.
	instanceID string

	relayMu      sync.Mutex
	relayStarted map[string]bool

	// Deferred persistence/publish queue (see persist_queue.go): the Upstash
	// save and cross-instance publish run on background workers with strict
	// per-match ordering, instead of blocking every mutation under c.mu.
	persistMu      sync.Mutex
	persistCond    *sync.Cond
	persistQueues  map[string][]*asyncPersistJob
	persistOwned   map[string]bool
	persistStopped bool
	persistWG      sync.WaitGroup
}

type authTokenEntry struct {
	PlayerID     string
	PlayerSecret string
	ExpiresAt    time.Time
}

type matchPresenceState struct {
	WhiteLastSeenAt         time.Time
	BlackLastSeenAt         time.Time
	WhiteConnected          bool
	BlackConnected          bool
	DisconnectGraceFor      string
	DisconnectGraceDeadline *time.Time
	WhiteLastIntentAt       time.Time
	BlackLastIntentAt       time.Time
	WhiteTokens             float64
	WhiteLastRefill         time.Time
	BlackTokens             float64
	BlackLastRefill         time.Time
}

type MatchArchiver interface {
	Upsert(snapshot contracts.MatchSnapshotResponse) error
}

type MatchArchiveLoader interface {
	MatchArchiver
	LoadMatch(matchID string) (contracts.MatchState, []contracts.ResolvedEvent, bool)
}

type MatchArchiveBootstrapper interface {
	MatchArchiveLoader
	ListUnfinishedMatchIDs(limit int) []string
}

type ServiceStats struct {
	LoadedMatches     int `json:"loadedMatches"`
	ActiveMatches     int `json:"activeMatches"`
	FinishedMatches   int `json:"finishedMatches"`
	SubscriberCount   int `json:"subscriberCount"`
	BufferedEventSets int `json:"bufferedEventSets"`
}

func NewService() *Service {
	return NewServiceWithArchive(nil)
}

func NewServiceWithArchive(archive MatchArchiver) *Service {
	return NewServiceWithStoreAndBroadcaster(archive, NewMemoryMatchStore(), NoopBroadcaster{})
}

func NewServiceWithStoreAndBroadcaster(archive MatchArchiver, store MatchStore, broadcaster Broadcaster) *Service {
	return NewServiceWithStoreBroadcasterAndTokenStore(archive, store, broadcaster, nil)
}

func NewServiceWithStoreBroadcasterAndTokenStore(archive MatchArchiver, store MatchStore, broadcaster Broadcaster, tokenStore TokenStore) *Service {
	service := &Service{
		matches:      newMatchMap(),
		archive:      archive,
		store:        store,
		broadcaster:  broadcaster,
		stopCh:       make(chan struct{}),
		authTokens:   make(map[string]authTokenEntry),
		tokenStore:   tokenStore,
		Log:          logging.New("match-service"),
		computerCh:   make(chan computerMoveTask, 100),
		instanceID:   newInstanceID(),
		relayStarted: make(map[string]bool),
	}
	if loader, ok := archive.(MatchArchiveBootstrapper); ok {
		service.restoreArchivedMatchesLocked(loader)
	}

	go service.startBroadcaster()
	go service.startGC()
	go service.cleanupAuthTokensLoop()
	service.startPersistWorkers()
	numWorkers := runtime.NumCPU()
	if numWorkers < 2 {
		numWorkers = 2
	}
	for i := 0; i < numWorkers; i++ {
		service.computerWorkerWg.Add(1)
		go service.computerWorker()
	}
	service.Log.Info("computer worker pool started", "workers", numWorkers)

	return service
}

func newInstanceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "inst-" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return "inst-" + hex.EncodeToString(b)
}

func (s *Service) getMatchContainer(matchID string) *matchContainer {
	c, _ := s.matches.Load(matchID)
	return c
}

func (s *Service) GetMatch(matchID string) (contracts.MatchSnapshotResponse, error) {
	s.mu.Lock()
	c, ok := s.ensureMatchLoadedLocked(matchID)
	s.mu.Unlock()
	if !ok {
		return contracts.MatchSnapshotResponse{}, ErrMatchNotFound
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	return buildSnapshotWithPresence(c.state, s.ensurePresenceStateLocked(c, now), len(c.events), nil, now), nil
}

// GetMatchForViewer returns the snapshot as a specific viewer is allowed to
// see it: seat secrets stripped, and the opponent's hand / private card state
// hidden unless the caller proves seat ownership with a valid player secret.
//
// An empty playerID is treated as a spectator. A non-empty playerID with a
// secret that does not match the seat is rejected outright rather than being
// silently downgraded, so a caller cannot probe for a seat by guessing.
func (s *Service) GetMatchForViewer(matchID, playerID, playerSecret string) (contracts.MatchSnapshotResponse, error) {
	s.mu.Lock()
	c, ok := s.ensureMatchLoadedLocked(matchID)
	s.mu.Unlock()
	if !ok {
		return contracts.MatchSnapshotResponse{}, ErrMatchNotFound
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	viewerColor := ""
	if strings.TrimSpace(playerID) != "" {
		color, err := requireIntentColor(c.state, strings.TrimSpace(playerID), strings.TrimSpace(playerSecret))
		if err != nil {
			return contracts.MatchSnapshotResponse{}, err
		}
		viewerColor = color
	}

	now := time.Now().UTC()
	base := buildSnapshotWithPresence(c.state, s.ensurePresenceStateLocked(c, now), len(c.events), nil, now)
	return contracts.MatchSnapshotResponse{
		Match:        filterStateForColor(base.Match, viewerColor),
		ReplayHead:   base.ReplayHead,
		ReplayFrames: base.ReplayFrames,
		Events:       filterEventsForColor(base.Events, viewerColor),
	}, nil
}

// ResolveSeatSecret returns the plaintext secret of the seat owned by guestID
// on matchID. It exists for server-to-server credential delivery: queue-matched
// rooms are created with server-generated seat secrets that never reach either
// player, so the platform's match-claim pipeline needs a trusted way to hand
// the seated player their real credential. The endpoint that exposes this is
// gated on the shared internal service token, and every snapshot the match
// service emits is still fully redacted -- only this call path, between
// services that already trust each other, sees the plaintext.
func (s *Service) ResolveSeatSecret(matchID, guestID string) (string, error) {
	guestID = strings.TrimSpace(guestID)
	if guestID == "" {
		return "", errors.New("guestId is required")
	}

	s.mu.Lock()
	c, ok := s.ensureMatchLoadedLocked(matchID)
	s.mu.Unlock()
	if !ok {
		return "", ErrMatchNotFound
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == nil {
		return "", ErrMatchNotFound
	}

	// Computer matches have one human seat and one engine seat. The engine
	// "player" (guestID "computer") must never be claimable, but the HUMAN
	// seat is a real seat backed by the creator's guest session -- refusing it
	// broke the whole credential chain for computer matches: platform match
	// claims, gateway bootstrap fallbacks, and WS auth-token issuance all
	// funnel through here, so the client ended up with no working secret,
	// every presence heartbeat 400'd, and the WebSocket never connected.
	seatColor := ""
	switch {
	case strings.EqualFold(guestID, strings.TrimSpace(c.state.WhiteGuestID)):
		seatColor = "white"
	case strings.EqualFold(guestID, strings.TrimSpace(c.state.BlackGuestID)):
		seatColor = "black"
	default:
		return "", ErrUnauthorizedSeatClaim
	}
	if c.state.ModeID == contracts.MatchModeComputer && strings.EqualFold(strings.TrimSpace(seatOwnerGuestID(c.state, seatColor)), "computer") {
		return "", errors.New("computer engine seat cannot be claimed")
	}

	secret := ""
	if seatColor == "white" {
		secret = strings.TrimSpace(c.state.WhitePlayerSecret)
	} else {
		secret = strings.TrimSpace(c.state.BlackPlayerSecret)
	}
	if secret == "" {
		return "", errors.New("seat has no player secret configured")
	}
	return secret, nil
}

// seatOwnerGuestID returns the guest that owns the given seat color in state.
func seatOwnerGuestID(state *contracts.MatchState, seatColor string) string {
	if state == nil {
		return ""
	}
	if seatColor == "white" {
		return strings.TrimSpace(state.WhiteGuestID)
	}
	return strings.TrimSpace(state.BlackGuestID)
}

func (s *Service) HeartbeatPresence(matchID string, req contracts.MatchPresenceRequest, now time.Time) error {
	s.mu.Lock()
	c, ok := s.ensureMatchLoadedLocked(matchID)
	s.mu.Unlock()
	if !ok {
		return ErrMatchNotFound
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.Status == "finished" {
		return nil
	}

	color, err := requireIntentColor(c.state, strings.TrimSpace(req.PlayerID), strings.TrimSpace(req.PlayerSecret))
	if err != nil {
		return err
	}

	presence := s.ensurePresenceStateLocked(c, now)
	presenceHeartbeat(presence, color, now)
	return nil
}

func (s *Service) MarkDisconnected(matchID string, playerID string, playerSecret string, now time.Time) error {
	s.mu.Lock()
	c, ok := s.ensureMatchLoadedLocked(matchID)
	s.mu.Unlock()
	if !ok || c.state.Status == "finished" {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	color, err := requireIntentColor(c.state, strings.TrimSpace(playerID), strings.TrimSpace(playerSecret))
	if err != nil {
		return err
	}

	presence := s.ensurePresenceStateLocked(c, now)
	if color == "white" {
		if c.state.WhiteGuestID == "computer" || !presence.WhiteConnected {
			return nil
		}
		presence.WhiteLastSeenAt = time.Time{}
		presence.WhiteConnected = false
	} else {
		if c.state.BlackGuestID == "computer" || !presence.BlackConnected {
			return nil
		}
		presence.BlackLastSeenAt = time.Time{}
		presence.BlackConnected = false
	}

	snapshot := buildSnapshotWithPresence(c.state, presence, len(c.events), nil, now)
	s.broadcastLocked(c, snapshot)
	return nil
}

// redactPlayerSecret keeps bearer credentials out of logs entirely. A prefix
// is credential material too, so use one fixed marker for every non-empty
// value. Empty string remains distinguishable for configuration diagnostics.
func redactPlayerSecret(s string) string {
	if s == "" {
		return "<empty>"
	}
	return "<redacted>"
}

// Subscribe attaches a snapshot stream for a viewer. playerSecret must prove
// ownership of the seat identified by playerID; without it a caller could pass
// any opponent's guest ID (which is public in every snapshot) and be served
// that seat's private hand for the rest of the match.
func (s *Service) Subscribe(matchID string, playerID string, playerSecret string) (<-chan contracts.MatchSnapshotResponse, func(), contracts.MatchSnapshotResponse, error) {
	s.mu.Lock()
	c, ok := s.ensureMatchLoadedLocked(matchID)
	s.mu.Unlock()
	if !ok {
		return nil, nil, contracts.MatchSnapshotResponse{}, ErrMatchNotFound
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.subs == nil {
		c.subs = make(map[chan contracts.MatchSnapshotResponse]string)
	}

	const maxSubscribersPerMatch = 50
	if len(c.subs) >= maxSubscribersPerMatch {
		return nil, nil, contracts.MatchSnapshotResponse{}, errors.New("max subscribers reached for match")
	}

	// Resolve the seat through the same constant-time secret check the intent
	// path uses. Identity alone is not sufficient: guest IDs are public.
	playerColor := ""
	if strings.TrimSpace(playerID) != "" {
		color, err := requireIntentColor(c.state, strings.TrimSpace(playerID), strings.TrimSpace(playerSecret))
		if err != nil {
			return nil, nil, contracts.MatchSnapshotResponse{}, err
		}
		playerColor = color
	}

	ch := make(chan contracts.MatchSnapshotResponse, 128)
	c.subs[ch] = playerColor

	now := time.Now().UTC()
	baseInitial := buildSnapshotWithPresence(c.state, s.ensurePresenceStateLocked(c, now), len(c.events), c.events, now)
	initial := contracts.MatchSnapshotResponse{
		Match:      filterStateForColor(baseInitial.Match, playerColor),
		ReplayHead: baseInitial.ReplayHead,
		Events:     filterEventsForColor(baseInitial.Events, playerColor),
	}

	unsubscribe := func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if _, present := c.subs[ch]; present {
			delete(c.subs, ch)
			close(ch)
		}
	}

	return ch, unsubscribe, initial, nil
}

func (s *Service) ensureMatchLoadedLocked(matchID string) (*matchContainer, bool) {
	if c, ok := s.matches.Load(matchID); ok {
		return c, true
	}

	restored, events, presence, ok := s.resolveMatchStateLocked(matchID)
	if !ok {
		return nil, false
	}

	if len(restored.History) == 0 {
		restored.History = []contracts.PositionState{capturePositionState(&restored)}
	}

	return s.loadMatchContainerLocked(matchID, restored, events, presence), true
}

func (s *Service) restoreArchivedMatchesLocked(loader MatchArchiveBootstrapper) {
	for _, matchID := range loader.ListUnfinishedMatchIDs(0) {
		if _, ok := s.matches.Load(matchID); ok {
			continue
		}
		// resolveMatchStateLocked prefers Redis when it has fresher data for
		// an ID the archive already told us is unfinished; it falls back to
		// this same archive row when Redis has nothing (TTL'd out, or the
		// match predates Redis being wired at all).
		restored, events, presence, ok := s.resolveMatchStateLocked(matchID)
		if !ok {
			continue
		}
		if len(restored.History) == 0 {
			restored.History = []contracts.PositionState{capturePositionState(&restored)}
		}
		s.loadMatchContainerLocked(matchID, restored, events, presence)
	}
}

// resolveMatchStateLocked tries the shared Redis store before the archive.
// Redis is the hot cross-instance layer written on every mutation
// (saveToRedis) with a short TTL; the archive (Postgres/SQLite/file) is
// written on the same cadence but is slower, and for the file/sqlite backends
// is not shared across instances at all. Preferring Redis means an instance
// that never handled this match's mutations still sees the latest state
// instead of a potentially-stale or entirely local-only archive row.
func (s *Service) resolveMatchStateLocked(matchID string) (contracts.MatchState, []contracts.ResolvedEvent, *matchPresenceState, bool) {
	if restored, events, presence, ok := s.hydrateFromRedisLocked(matchID); ok {
		return restored, events, presence, true
	}

	loader, ok := s.archive.(MatchArchiveLoader)
	if !ok {
		return contracts.MatchState{}, nil, nil, false
	}
	restored, events, ok := loader.LoadMatch(matchID)
	if !ok {
		return contracts.MatchState{}, nil, nil, false
	}
	return restored, events, nil, true
}

// hydrateFromRedisLocked rebuilds match state from a single Redis read.
// SaveState stores the full contracts.MatchSnapshotResponse -- Match (board,
// hands, seat secrets, position history) plus Events -- so LoadState alone is
// sufficient; the separate SaveHistory/SaveEvents keys and the hashed
// SaveSecrets key are not read here (SaveSecrets stores an HMAC, not the
// plaintext, so it cannot authenticate a caller-supplied secret and is not
// usable for this purpose). LoadPresence is read separately because presence
// (connection/heartbeat/rate-limit state) is not part of MatchState at all.
func (s *Service) hydrateFromRedisLocked(matchID string) (contracts.MatchState, []contracts.ResolvedEvent, *matchPresenceState, bool) {
	if s.store == nil {
		return contracts.MatchState{}, nil, nil, false
	}

	var snapshot contracts.MatchSnapshotResponse
	if err := s.store.LoadState(matchID, &snapshot); err != nil || snapshot.Match.MatchID == "" {
		return contracts.MatchState{}, nil, nil, false
	}

	var presence *matchPresenceState
	if data, err := s.store.LoadPresence(matchID); err == nil && len(data) > 0 {
		var p matchPresenceState
		if json.Unmarshal(data, &p) == nil {
			presence = &p
		}
	}

	return snapshot.Match, snapshot.Events, presence, true
}

func (s *Service) loadMatchContainerLocked(matchID string, restored contracts.MatchState, events []contracts.ResolvedEvent, presence *matchPresenceState) *matchContainer {
	// Restore SeenClientMoveIDs from Redis store if available
	if s.store != nil {
		if data, err := s.store.LoadSeenClientMoveIDs(matchID); err == nil && len(data) > 0 {
			var ids []string
			if json.Unmarshal(data, &ids) == nil {
				restored.SeenClientMoveIDs = ids
			}
		}
	}
	if presence == nil {
		presence = newRecoveredMatchPresenceState(&restored)
	}
	c := newMatchContainer(&restored, append([]contracts.ResolvedEvent{}, events...), presence)
	if s.store != nil {
		if seq, err := s.store.LoadSeq(matchID); err == nil {
			c.seqNum = seq
		}
	}
	s.matches.Store(matchID, c)

	// This instance did not create the match (CreateMatch stores directly,
	// bypassing this function), so it has no other way to learn about future
	// mutations made elsewhere. Relay Redis broadcasts into this container's
	// local subscribers so a spectator or player whose connection landed on
	// this instance still sees a live match being played on another one.
	// CreateMatch subscribes too, for the same reason in the other direction.
	s.ensureRedisRelay(matchID)

	return c
}

const authTokenTTL = 5 * time.Minute

func (s *Service) CreateAuthToken(playerID, playerSecret string, now time.Time) string {
	raw := make([]byte, 16)
	var token string
	if _, err := rand.Read(raw); err != nil {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s_%s_%d", playerID, playerSecret, now.UnixNano())))
		token = "at_" + hex.EncodeToString(h[:16])
	} else {
		token = "at_" + hex.EncodeToString(raw)
	}
	entry := authTokenEntry{
		PlayerID:     playerID,
		PlayerSecret: playerSecret,
		ExpiresAt:    now.Add(authTokenTTL),
	}
	if s.tokenStore != nil {
		if err := s.tokenStore.Create(token, entry, authTokenTTL); err != nil {
			s.Log.Error("failed to store auth token in redis, falling back to memory", "error", err)
		} else {
			return token
		}
	}
	s.mu.Lock()
	s.authTokens[token] = entry
	s.mu.Unlock()
	return token
}

func (s *Service) ResolveAuthToken(token string) (string, string, bool) {
	if token == "" {
		return "", "", false
	}
	if s.tokenStore != nil {
		entry, ok, err := s.tokenStore.Resolve(token)
		if err != nil {
			s.Log.Error("failed to resolve auth token from redis, falling back to memory", "error", err)
		} else if ok {
			return entry.PlayerID, entry.PlayerSecret, true
		} else if !ok && err == nil {
			return "", "", false
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.authTokens[token]
	if !ok {
		return "", "", false
	}
	if time.Now().After(entry.ExpiresAt) {
		delete(s.authTokens, token)
		return "", "", false
	}
	delete(s.authTokens, token)
	return entry.PlayerID, entry.PlayerSecret, true
}

func (s *Service) cleanupAuthTokensLoop() {
	if s.tokenStore != nil {
		return
	}
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			for token, entry := range s.authTokens {
				if now.After(entry.ExpiresAt) {
					delete(s.authTokens, token)
				}
			}
			s.mu.Unlock()
		}
	}
}

func (s *Service) Stats() ServiceStats {
	stats := ServiceStats{}
	s.matches.Range(func(_ string, c *matchContainer) bool {
		c.mu.Lock()
		stats.LoadedMatches++
		stats.BufferedEventSets++
		if c.state.Status == "finished" {
			stats.FinishedMatches++
		} else {
			stats.ActiveMatches++
		}
		stats.SubscriberCount += len(c.subs)
		c.mu.Unlock()
		return true
	})
	return stats
}

func (s *Service) Close() {
	close(s.stopCh)
	// Drain deferred persistence so a redeploy does not lose the tail of
	// in-flight writes. Workers exit once the queues are empty; jobs that
	// arrive after this point run inline (queueCommit handles that).
	s.persistMu.Lock()
	s.persistStopped = true
	s.persistCond.Broadcast()
	s.persistMu.Unlock()
	s.persistWG.Wait()
}

func (s *Service) persistSnapshot(snapshot contracts.MatchSnapshotResponse) {
	if s.archive == nil {
		return
	}
	persisted := snapshot
	persisted.Match.WhiteConnected = false
	persisted.Match.BlackConnected = false
	persisted.Match.DisconnectGraceFor = ""
	persisted.Match.DisconnectGraceDeadline = nil
	if err := s.archive.Upsert(persisted); err != nil {
		s.Log.Error("failed to persist snapshot", "matchId", snapshot.Match.MatchID, "error", err)
	}
}

// saveToRedis queues the Redis snapshot save as deferred IO. It is
// deliberately cheap: no network call runs on the mutation path (see
// persist_queue.go for the ordering invariants). The full snapshot --
// including seat secrets -- is still what gets stored, so
// hydrateFromRedisLocked can rebuild a container with intent auth working;
// that is a direct point-to-point Redis write, not a broadcast, same trust
// tier as the archive.
//
// All components go out as ONE pipelined round trip (SaveSnapshotAtomic).
// The previous six sequential Save* calls put a ~6xWAN-RTT floor on every
// move; deferring the round trip entirely removes the rest of the ~1s
// intent-to-board latency measured in production on 2026-09-25.
func (s *Service) saveToRedis(snapshot contracts.MatchSnapshotResponse, presence *matchPresenceState) {
	s.queueSave(snapshot, presence)
}

// flushCommit persists the archive upsert + Redis snapshot save INLINE.
// Use only where durability must precede the call returning: match creation
// (a hydrate on another connection must find the match) and terminal states
// (the final state must never be overtaken by a queued older write).
func (s *Service) flushCommit(persistSnap contracts.MatchSnapshotResponse, presence *matchPresenceState) {
	s.persistSnapshot(persistSnap)
	s.drainMatchPersist(persistSnap.Match.MatchID)
	if b := s.buildRedisSaveBundle(persistSnap, presence); b != nil {
		s.runRedisSave(persistSnap.Match.MatchID, b)
	}
}

// redisBroadcastEnvelope wraps a published snapshot with the id of the
// instance that produced it, so relayRedisBroadcasts can recognize and skip
// its own process's publishes.
type redisBroadcastEnvelope struct {
	OriginInstanceID string                          `json:"originInstanceId"`
	Snapshot         contracts.MatchSnapshotResponse `json:"snapshot"`
}

// publishToRedis marshals the redacted envelope and queues the pub/sub
// write as deferred IO (no network on the mutation path; see
// persist_queue.go). Local subscribers are already served synchronously by
// the caller (deliverToSubscribersLocked).
func (s *Service) publishToRedis(matchID string, snapshot contracts.MatchSnapshotResponse) {
	s.queuePublish(matchID, s.buildPublishPayload(snapshot))
}

// ensureRedisRelay subscribes this instance to cross-instance broadcasts for
// matchID, once per matchID per process. Only called for matches reached
// through the hydrate path (loadMatchContainerLocked) -- a match created
// locally via CreateMatch never subscribes to its own channel, which is what
// keeps a single instance from receiving and re-delivering its own
// broadcasts a second time.
func (s *Service) ensureRedisRelay(matchID string) {
	if s.broadcaster == nil {
		return
	}
	if _, ok := s.broadcaster.(NoopBroadcaster); ok {
		return
	}

	s.relayMu.Lock()
	if s.relayStarted == nil {
		s.relayStarted = make(map[string]bool)
	}
	if s.relayStarted[matchID] {
		s.relayMu.Unlock()
		return
	}
	s.relayStarted[matchID] = true
	s.relayMu.Unlock()

	ch := s.broadcaster.Subscribe(matchID)
	if ch == nil {
		s.relayMu.Lock()
		delete(s.relayStarted, matchID)
		s.relayMu.Unlock()
		return
	}
	go s.relayRedisBroadcasts(matchID, ch)
}

func (s *Service) relayRedisBroadcasts(matchID string, ch <-chan []byte) {
	for data := range ch {
		var envelope redisBroadcastEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			s.Log.Error("redis relay: failed to unmarshal broadcast envelope", "matchId", matchID, "error", err)
			continue
		}
		if envelope.OriginInstanceID == s.instanceID {
			continue
		}
		s.deliverRelayedSnapshot(matchID, envelope.Snapshot)
	}
	s.relayMu.Lock()
	delete(s.relayStarted, matchID)
	s.relayMu.Unlock()
}

// deliverRelayedSnapshot pushes a snapshot produced by another instance to
// this instance's local subscribers only. It does not republish (that would
// create an infinite relay loop across instances) and does not mint a new
// seq (the snapshot already carries the seq its origin assigned via
// nextSeqNum) -- it only advances the local cache so ApplyIntent's staleness
// check reflects the latest known global sequence even on instances that
// never produced a broadcast themselves.
func (s *Service) deliverRelayedSnapshot(matchID string, snapshot contracts.MatchSnapshotResponse) {
	c, ok := s.matches.Load(matchID)
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if snapshot.SeqNum > c.seqNum {
		c.seqNum = snapshot.SeqNum
	}
	deliverToSubscribersLocked(c, snapshot)
}

// nextSeqNum assigns the sequence number for an outgoing broadcast. It uses
// the shared store's atomic counter so seq numbers are globally monotonic
// across every instance broadcasting for this match, not just this process --
// MemoryMatchStore implements the same counter locally for single-instance/
// test runs, so this is a safe default in every configuration.
func (s *Service) nextSeqNum(c *matchContainer) int64 {
	if s.store != nil {
		if seq, err := s.store.IncSeq(c.state.MatchID); err == nil {
			c.seqNum = seq
			return seq
		}
		s.Log.Error("failed to increment seq via store, falling back to local counter", "matchId", c.state.MatchID)
	}
	c.seqNum++
	return c.seqNum
}

func (s *Service) broadcastLocked(c *matchContainer, snapshot contracts.MatchSnapshotResponse) {
	snapshot.SeqNum = s.nextSeqNum(c)

	// Strip replay frames from periodic broadcasts to reduce bandwidth.
	// Replay frames are still sent on initial Subscribe and via ApplyIntent
	// so clients can resync on reconnect.
	snapshot.ReplayFrames = nil

	// Local delivery FIRST (in-memory, no IO): the acting client sees its
	// move in the same instant the mutation commits. The cross-instance
	// publish is deferred to the background queue.
	deliverToSubscribersLocked(c, snapshot)
	s.queuePublish(c.state.MatchID, s.buildPublishPayload(snapshot))
}

// broadcastLockedNoSeqBump delivers a snapshot without minting a new seq --
// use it only when nothing about the authoritative game state (board, hands,
// turn, status) actually changed, e.g. the once-a-second tick that exists
// purely to keep the visible clock countdown smooth for connected viewers.
// Reusing c.seqNum instead of calling nextSeqNum matters because ApplyIntent
// rejects a move whenever the client's expectedSeqNum is behind the current
// seq (staleness protection against acting on a board the client hasn't
// seen yet) -- if this per-second cosmetic tick bumped that same counter,
// any client whose WS delivery of that tick lagged its own next move click
// by even a few tens of milliseconds would have its perfectly valid move
// rejected as "stale", purely because of a broadcast that changed nothing
// the move's legality depended on. That was happening in production: a
// steady trickle of 409s roughly once every 10-30s of active play, each one
// self-recovering (via the client's post-409 resync) but still visibly
// failing every time it happened.
func (s *Service) broadcastLockedNoSeqBump(c *matchContainer, snapshot contracts.MatchSnapshotResponse) {
	snapshot.SeqNum = c.seqNum
	snapshot.ReplayFrames = nil
	deliverToSubscribersLocked(c, snapshot)
	s.queuePublish(c.state.MatchID, s.buildPublishPayload(snapshot))
}

func deliverToSubscribersLocked(c *matchContainer, snapshot contracts.MatchSnapshotResponse) {
	if len(c.subs) == 0 {
		return
	}

	cachedWhite := snapshot
	cachedWhite.Match = filterStateForColor(snapshot.Match, "white")
	cachedWhite.Events = filterEventsForColor(snapshot.Events, "white")
	cachedBlack := snapshot
	cachedBlack.Match = filterStateForColor(snapshot.Match, "black")
	cachedBlack.Events = filterEventsForColor(snapshot.Events, "black")
	cachedSpec := snapshot
	cachedSpec.Match = filterStateForColor(snapshot.Match, "")
	cachedSpec.Events = filterEventsForColor(snapshot.Events, "")

	// Collect the channels first: a drop removes its channel from c.subs, and
	// mutating a map while ranging it is exactly the kind of subtle bug this
	// path used to have (the drop left the dead channel IN the map -- every
	// later broadcast panicked into recover for that client forever, the slot
	// counted against the per-match subscriber cap, and the eventual
	// unsubscribe() closed the already-closed channel, an unrecovered panic on
	// a hijacked-connection goroutine that could take down the process).
	type subPush struct {
		ch    chan contracts.MatchSnapshotResponse
		snap  contracts.MatchSnapshotResponse
	}
	pushes := make([]subPush, 0, len(c.subs))
	for ch, color := range c.subs {
		switch color {
		case "white":
			pushes = append(pushes, subPush{ch: ch, snap: cachedWhite})
		case "black":
			pushes = append(pushes, subPush{ch: ch, snap: cachedBlack})
		default:
			pushes = append(pushes, subPush{ch: ch, snap: cachedSpec})
		}
	}
	for _, p := range pushes {
		pushSnapshot(c, p.ch, p.snap)
	}
}

// pushSnapshot delivers one snapshot to one subscriber. The caller must hold
// c.mu so a drop can remove the dead channel from c.subs in the same critical
// section. Removal-before-close is what keeps the subscribers map authoritative:
// after a drop the channel no longer exists as a subscriber, its buffer slot is
// freed for a new viewer, and no later broadcast can touch the closed channel.
func pushSnapshot(c *matchContainer, ch chan contracts.MatchSnapshotResponse, snapshot contracts.MatchSnapshotResponse) {
	select {
	case ch <- snapshot:
	default:
		metrics.PushSnapshotDrops.Inc()
		log.Printf("pushSnapshot: dropping event seq=%d for channel %p (buffer full) — forcing client resync", snapshot.SeqNum, ch)
		if c != nil && c.subs != nil {
			delete(c.subs, ch)
		}
		close(ch)
	}
}

func (s *Service) startBroadcaster() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case now, ok := <-ticker.C:
			if !ok {
				return
			}
			s.collectAndBroadcast(now.UTC())
		}
	}
}

const broadcastConcurrency = 20

func (s *Service) startGC() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case now, ok := <-ticker.C:
			if !ok {
				return
			}
			s.gcFinishedMatches(now.UTC())
		}
	}
}

func (s *Service) collectAndBroadcast(now time.Time) {
	sem := make(chan struct{}, broadcastConcurrency)
	var wg sync.WaitGroup

	// Snapshot the container set before fanning out. Acquiring the broadcast
	// semaphore inside Range would block while holding a shard RLock, so one
	// slow WebSocket write would stall match creation on that whole shard.
	containers := make([]*matchContainer, 0, s.matches.Len())
	s.matches.Range(func(_ string, c *matchContainer) bool {
		containers = append(containers, c)
		return true
	})

	for _, c := range containers {
		sem <- struct{}{}
		wg.Add(1)
		go func(mc *matchContainer) {
			defer func() {
				<-sem
				wg.Done()
			}()
			s.processMatchBroadcast(mc, now)
		}(c)
	}

	wg.Wait()
}

func (s *Service) processMatchBroadcast(c *matchContainer, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state.Status == "finished" {
		return
	}

	presence := s.ensurePresenceStateLocked(c, now)

	recentCutoff := now.Add(-presenceHeartbeatTimeout)
	hasRecentActivity := (!presence.WhiteLastSeenAt.IsZero() && presence.WhiteLastSeenAt.After(recentCutoff)) ||
		(!presence.BlackLastSeenAt.IsZero() && presence.BlackLastSeenAt.After(recentCutoff))
	if hasRecentActivity {
		timeoutEvents := syncClockForMutation(c.state, now)
		if len(timeoutEvents) > 0 {
			c.events = append(c.events, timeoutEvents...)
			s.broadcastLocked(c, buildSnapshotWithPresence(c.state, presence, len(c.events), timeoutEvents, now))
		}
		s.persistSnapshot(buildSnapshot(c.state, len(c.events), c.events, now))
		if len(timeoutEvents) > 0 {
			return
		}
	}

	runtimeEvents := evaluatePresenceRuntime(c.state, presence, now)
	if len(runtimeEvents) > 0 {
		c.events = append(c.events, runtimeEvents...)
		s.persistSnapshot(buildSnapshot(c.state, len(c.events), c.events, now))
		s.broadcastLocked(c, buildSnapshotWithPresence(c.state, presence, len(c.events), runtimeEvents, now))
		return
	}
	if len(c.subs) == 0 {
		return
	}
	// Nothing authoritative changed this tick (no timeout, no presence
	// runtime event) -- this broadcast exists only so connected clients see
	// the clock keep counting down. Must not advance seqNum; see
	// broadcastLockedNoSeqBump.
	s.broadcastLockedNoSeqBump(c, buildSnapshotWithPresence(c.state, presence, len(c.events), nil, now))
}

func (s *Service) computerWorker() {
	defer s.computerWorkerWg.Done()
	for {
		select {
		case <-s.stopCh:
			return
		case task := <-s.computerCh:
			task.c.mu.Lock()
			s.autoPlayComputerDepthLimited(task.c, task.now, 0)
			s.ensureComputerMadeProgressLocked(task.c, task.now)
			task.c.mu.Unlock()
		}
	}
}

func (s *Service) gcFinishedMatches(now time.Time) {
	const finishedMatchTTL = 30 * time.Minute
	const waitingMatchTTL = 30 * time.Minute

	// Collect first, delete after Range returns. Range holds the shard's
	// RLock across the callback, and Delete takes that same shard's write
	// lock -- calling Delete from inside Range self-deadlocks the goroutine
	// and leaves the shard mutex permanently held, which wedges every
	// Load/Store/Range on that shard for the lifetime of the process.
	var stale []string
	var zombies []string
	s.matches.Range(func(matchID string, c *matchContainer) bool {
		c.mu.Lock()
		status := c.state.Status
		updatedAt := c.state.UpdatedAt
		c.mu.Unlock()

		switch status {
		case "finished":
			if now.Sub(updatedAt) >= finishedMatchTTL {
				stale = append(stale, matchID)
			}
		case "waiting":
			if now.Sub(updatedAt) >= waitingMatchTTL {
				stale = append(stale, matchID)
			}
		case "active":
			// An ACTIVE match with no connected player for long past the
			// disconnect grace is a zombie: both players are gone and nothing
			// will ever finalize it. Previously these were evicted while
			// still "active", so the archived row stayed active forever and
			// clogged the public watch/replay feed. Finalize them as draws
			// (abandon) instead, exactly like the reconcile path does on
			// restart.
			//
			// Presence-gated, NOT wall-clock idle: Untimed matches and long
			// thinks legitimately sit without mutations, so UpdatedAt alone
			// proves nothing about liveness. A connected player's presence
			// heartbeat keeps the match alive regardless of how stale the
			// UpdatedAt timestamp is.
			const activeAbandonTTL = 10 * time.Minute
			if now.Sub(updatedAt) >= activeAbandonTTL && s.zombiePresenceLocked(c, now) {
				stale = append(stale, matchID)
				zombies = append(zombies, matchID)
			}
		}
		return true
	})

	for _, matchID := range zombies {
		s.finalizeAbandonedMatch(matchID, now)
	}
	for _, matchID := range stale {
		s.matches.Delete(matchID)
	}
}

// zombiePresenceLocked reports whether an active match looks fully abandoned:
// neither seat has been heard from within the presence heartbeat window. A
// container with no presence state at all predates presence tracking (or was
// never resumed through a presence-bearing path), so the historical wall-clock
// zombie rule still applies to it -- that was the regression this branch was
// built to clean up.
//
// Long thinks and untimed games are exactly what this guard protects: they
// refresh nothing, but a live player's heartbeat keeps both the tick loop and
// this GC away from the match.
func (s *Service) zombiePresenceLocked(c *matchContainer, now time.Time) bool {
	if c.presence == nil {
		return true
	}
	cutoff := now.Add(-presenceHeartbeatTimeout)
	whiteAlive := c.presence.WhiteLastSeenAt.After(cutoff)
	blackAlive := c.presence.BlackLastSeenAt.After(cutoff)
	if c.state.WhiteGuestID == "computer" {
		whiteAlive = true
	}
	if c.state.BlackGuestID == "computer" {
		blackAlive = true
	}
	return !whiteAlive && !blackAlive
}

// finalizeAbandonedMatch marks a zombie active match as a draw-abandon and
// persists the finished state. Caller must NOT hold c.mu.
func (s *Service) finalizeAbandonedMatch(matchID string, now time.Time) {
	c := s.getMatchContainer(matchID)
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.state.Status != "active" {
		c.mu.Unlock()
		return
	}
	markMatchFinished(c.state, "draw", "abandon", now)
	finishEvents := []contracts.ResolvedEvent{
		makeEvent(matchID, "match_finished", now, "system", map[string]any{
			"result":        "abandon",
			"winner":        "draw",
			"disconnected":  disconnectGraceBoth,
		}),
	}
	c.events = append(c.events, finishEvents...)
	snapshot := buildSnapshotWithPresence(c.state, c.presence, len(c.events), finishEvents, now)
	persistSnap := buildSnapshot(c.state, len(c.events), c.events, now)
	// flushCommit and broadcastLocked run while STILL HOLDING c.mu, exactly
	// like every other call site (JoinMatchSeat, ApplyIntent, the computer
	// worker). The previous version unlocked first: broadcastLocked ->
	// deliverToSubscribersLocked then read c.subs and c.seqNum without the
	// lock, racing a concurrently reconnecting player's Subscribe/ApplyIntent
	// -- a concurrent map read+write is a fatal runtime panic that would take
	// down the whole match-service process. Neither call re-enters c.mu (the
	// persist workers never take it; see persist_queue.go), so holding it
	// across both is safe and matches the established invariant.
	s.flushCommit(persistSnap, c.presence)
	s.broadcastLocked(c, snapshot)
	c.mu.Unlock()
}
