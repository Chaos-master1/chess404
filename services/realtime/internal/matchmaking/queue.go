package matchmaking

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

type QueueName string

const (
	QueueCasual QueueName = "casual"
	QueueRated  QueueName = "rated"
)

const (
	defaultQueuedTTL = 10 * time.Minute
	// defaultQueueClockSeconds is the time control every queue match gets
	// unless the client explicitly picked one of the allowed controls.
	defaultQueueClockSeconds int64 = 600
	// maxQueueClockIncrementSeconds caps the increment a client can request;
	// the UI only offers 0 and 10 today, but the server tolerates more.
	maxQueueClockIncrementSeconds int64 = 30
	// Matched tickets must outlive create + claim + client-claim-retry:
	// the paired guest navigates, claims a seat (with up to 3 retries), and
	// only then stops polling the ticket. A short window here silently
	// deletes the ticket under a matched player; the recovery path then
	// 404s and the client clears its state with no explanation.
	defaultMatchedRecoveryTTL = 15 * time.Minute
	// A pairing reservation must outlive the creator's HTTP budget (~3s) by
	// a wide margin, but must NOT survive a process crash for long: the
	// cleanup loop rolls stale pairing tickets back to queued after this.
	defaultPairingRecoveryTTL = 60 * time.Second
	defaultCancelledTicketTTL = 30 * time.Second
	defaultMaxRatingDiff      = 400
)

// allowedQueueClockSeconds lists the time controls the queue offers. Anything
// else (including 0 from clients that predate the picker) normalizes to the
// default so a malformed payload can never inject an arbitrary clock into a
// paired match.
var allowedQueueClockSeconds = map[int64]bool{
	300: true,
	600: true,
	900: true,
	1800: true,
	3600: true,
}

func normalizeClockSeconds(value int64) int64 {
	if allowedQueueClockSeconds[value] {
		return value
	}
	return defaultQueueClockSeconds
}

func normalizeClockIncrement(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > maxQueueClockIncrementSeconds {
		return maxQueueClockIncrementSeconds
	}
	return value
}

type TicketStatus string

const (
	StatusQueued    TicketStatus = "queued"
	// StatusPairing is the reservation state between "opponent found" and
	// "match room created". EnqueueWithAccount releases s.mu while the
	// creator's HTTP call runs, so concurrent readers see this state; it
	// counts as active for the guest (no second enqueue can race in) but is
	// never returned to clients as a terminal result. A crash or a wedged
	// creator leaves a pairing ticket behind; recoverStalePairingsLocked
	// rolls those back to queued.
	StatusPairing  TicketStatus = "pairing"
	StatusMatched  TicketStatus = "matched"
	StatusCancelled TicketStatus = "cancelled"
)

type Ticket struct {
	TicketID     string                `json:"ticketId"`
	GuestID      string                `json:"guestId"`
	AccountID    string                `json:"accountId,omitempty"`
	DisplayName  string                `json:"displayName,omitempty"`
	Queue        QueueName             `json:"queue"`
	ModeID       contracts.MatchModeID `json:"modeId,omitempty"`
	// ClockSeconds/ClockIncrement are the normalized time control this seek
	// was created with. Pairing requires exact equality so nobody ever gets
	// dropped into a match at a clock they did not pick.
	ClockSeconds   int64                 `json:"clockSeconds,omitempty"`
	ClockIncrement int64                 `json:"clockIncrement,omitempty"`
	Status       TicketStatus          `json:"status"`
	Rating       int                   `json:"rating"`
	CreatedAt    time.Time             `json:"createdAt"`
	UpdatedAt    time.Time             `json:"updatedAt"`
	MatchedAt    *time.Time            `json:"matchedAt,omitempty"`
	MatchedWith  string                `json:"matchedWith,omitempty"`
	SeatColor    string                `json:"seatColor,omitempty"`
	OpponentName string                `json:"opponentName,omitempty"`
	AssignedRoom string                `json:"assignedRoom,omitempty"`
	// CancelSecret authorizes DELETE /tickets/{id}: only the enqueuing
	// client (which received it at create time) or an internal service can
	// cancel. Never serialized in List output.
	CancelSecret string                `json:"cancelSecret,omitempty"`
}

type QueueSnapshot struct {
	Queue          QueueName             `json:"queue"`
	ModeID         contracts.MatchModeID `json:"modeId,omitempty"`
	QueuedCount    int                   `json:"queuedCount"`
	MatchedCount   int                   `json:"matchedCount"`
	CancelledCount int                   `json:"cancelledCount"`
}

type Service struct {
	mu                  sync.Mutex
	store               ticketStore
	tickets             map[string]Ticket
	// inFlight guards the pairing critical section: a guest mid-CreateMatch
	// must not re-enter Enqueue (or be enqueued from a second request) until
	// the current pairing resolves. Keyed by guestID.
	inFlight            map[string]bool
	creator             MatchCreator
	now                 func() time.Time
	queuedTTL             time.Duration
	matchedRecoveryTTL    time.Duration
	pairingRecoveryTTL    time.Duration
	cancelledTicketTTL    time.Duration
	cleanupStopCh         chan struct{}
}

type MatchAssignment struct {
	Queue             QueueName
	ModeID            contracts.MatchModeID
	ClockSeconds      int64
	ClockIncrement    int64
	RoomID            string
	WhiteGuestID      string
	BlackGuestID      string
	WhiteAccountID    string
	BlackAccountID    string
	WhiteName         string
	BlackName         string
	WhitePlayerSecret string
	BlackPlayerSecret string
}

type MatchCreator interface {
	CreateMatch(assignment MatchAssignment) error
}

var ErrGuestAlreadyQueued = errors.New("guest already has an active queue ticket")

var ErrCancelUnauthorized = errors.New("ticket cancel secret missing or wrong")

type ActiveTicketError struct {
	Ticket Ticket
}

func (e ActiveTicketError) Error() string {
	return ErrGuestAlreadyQueued.Error()
}

type ServiceStats struct {
	Backend      string        `json:"backend"`
	TotalTickets int           `json:"totalTickets"`
	Casual       QueueSnapshot `json:"casual"`
	Rated        QueueSnapshot `json:"rated"`
}

func NewService() *Service {
	return newService(nil)
}

func NewPersistentService(path string) (*Service, error) {
	return newPersistentService(newFileTicketStore(path))
}

func NewSQLitePersistentService(path string) (*Service, error) {
	store, err := newSQLiteTicketStore(path)
	if err != nil {
		return nil, err
	}
	return newPersistentService(store)
}

func NewRedisPersistentService(redisURL, key string) (*Service, error) {
	store, err := newRedisTicketStore(redisURL, key)
	if err != nil {
		return nil, err
	}
	return newPersistentService(store)
}

func newPersistentService(store ticketStore) (*Service, error) {
	service := newService(store)
	if err := service.loadLocked(); err != nil {
		_ = store.close()
		return nil, err
	}
	return service, nil
}

func newService(store ticketStore) *Service {
	s := &Service{
		store:              store,
		tickets:            make(map[string]Ticket),
		inFlight:           make(map[string]bool),
		now:                time.Now,
		queuedTTL:          queuedTTLFromEnv(),
		matchedRecoveryTTL: defaultMatchedRecoveryTTL,
		pairingRecoveryTTL: defaultPairingRecoveryTTL,
		cancelledTicketTTL: defaultCancelledTicketTTL,
		cleanupStopCh:      make(chan struct{}),
	}
	s.startCleanupLoop()
	return s
}

// queuedTTLFromEnv lets operators tune how long a queued ticket stays
// matchable. Shorter TTLs reduce the window for ghost pairings against
// abandoned tickets (the opponent is gone either way); the cleanup loop only
// prunes every 5 minutes, so expiry granularity is bounded by that too.
func queuedTTLFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("MATCHMAKING_QUEUED_TTL_SECONDS"))
	if secs, err := strconv.Atoi(raw); err == nil && secs >= 30 {
		return time.Duration(secs) * time.Second
	}
	return defaultQueuedTTL
}

func (s *Service) startCleanupLoop() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-s.cleanupStopCh:
				return
			case <-ticker.C:
				s.mu.Lock()
				s.recoverStalePairingsLocked(s.nowUTC())
				s.pruneExpiredLocked(s.nowUTC())
				s.mu.Unlock()
			}
		}
	}()
}

func (s *Service) SetMatchCreator(creator MatchCreator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creator = creator
}

func (s *Service) Close() error {
	close(s.cleanupStopCh)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store == nil {
		return nil
	}
	return s.store.close()
}

func (s *Service) Backend() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store == nil {
		return "memory"
	}
	return s.store.backend()
}

func (s *Service) Enqueue(queue QueueName, modeID contracts.MatchModeID, guestID string, rating int, displayName string) (Ticket, error) {
	return s.EnqueueWithAccount(queue, modeID, guestID, rating, displayName, "", 0, 0)
}

// EnqueueWithAccount creates (or idempotently returns) the guest's queue
// ticket, pairing with a waiting opponent when one is available.
//
// Pairing is TWO-PHASE. The previous version held s.mu across
// creator.CreateMatch -- a WAN HTTP call measured at ~2s -- which was correct
// against TOCTOU double-pairing but froze every other queue operation: every
// Get/List/Snapshot/Cancel from every client blocked for the whole create.
// Now:
//
//  1. (under s.mu) find the opponent, mint the room + seat secrets, and
//     RESERVE both tickets as StatusPairing. findMatchCandidateLocked only
//     considers StatusQueued tickets, so the reservation itself is the
//     double-pairing guard -- no second enqueue can claim the same waiter.
//  2. (s.mu RELEASED) the creator's HTTP call runs. Every other queue
//     operation proceeds normally; reserved tickets read as "queued" to
//     clients (publicView), so the wire contract is unchanged.
//  3. (under s.mu) promote both tickets to matched -- but only if they are
//     STILL pairing (a stale-pairing recovery or a cancel may have touched
//     them while the lock was released). On creator failure, roll the
//     reservation back to queued so a waiting opponent simply keeps waiting.
func (s *Service) EnqueueWithAccount(queue QueueName, modeID contracts.MatchModeID, guestID string, rating int, displayName, accountID string, clockSeconds, clockIncrement int64) (Ticket, error) {
	s.mu.Lock()

	now := s.nowUTC()
	s.recoverStalePairingsLocked(now)
	if s.pruneExpiredLocked(now) {
		if err := s.persistLocked(); err != nil {
			s.mu.Unlock()
			return Ticket{}, err
		}
	}

	modeID = normalizeModeID(modeID)
	clockSeconds = normalizeClockSeconds(clockSeconds)
	clockIncrement = normalizeClockIncrement(clockIncrement)
	if active, ok := s.findActiveTicketForGuestLocked(guestID); ok {
		if active.Status == StatusPairing && active.Queue == queue && normalizeModeID(active.ModeID) == modeID {
			// A pairing reservation is already in flight for this guest
			// (re-entrant or duplicated enqueue): idempotent return, no
			// second pairing and no credential re-issue.
			s.mu.Unlock()
			return active.publicView(), nil
		}
		if active.Status == StatusMatched && active.Queue == queue && normalizeModeID(active.ModeID) == modeID {
			// Re-joining the SAME lane while still marked matched means the
			// guest is done with that room (finished, left, or never joined):
			// release the stale ticket and pair fresh. This must stay narrow:
			// claims live in the platform store independently of tickets, so
			// cancelling the ticket never disturbs a live match, and a
			// different-lane request still 409s below.
			active.Status = StatusCancelled
			active.UpdatedAt = now
			s.tickets[active.TicketID] = active
			if err := s.persistLocked(); err != nil {
				s.mu.Unlock()
				return Ticket{}, err
			}
		} else if active.Queue == queue && normalizeModeID(active.ModeID) == modeID {
			// Idempotent re-join, but the stored ticket carries the cancel
			// secret issued to the ORIGINAL create call. Returning it verbatim
			// would hand that credential to any caller who knows a public
			// guestId -- re-issue instead, matching the "issued exactly once,
			// to the enqueuing client" contract (a re-enqueuer IS enqueuing).
			active.CancelSecret = "cxl_" + randomToken(16)
			active.UpdatedAt = now
			s.tickets[active.TicketID] = active
			if err := s.persistLocked(); err != nil {
				s.mu.Unlock()
				return Ticket{}, err
			}
			// Re-join response: the caller just re-enqueued, so re-issuing
			// the cancel credential to THIS caller is correct.
			reissued := active
			s.mu.Unlock()
			return reissued, nil
		} else {
			activeErr := ActiveTicketError{Ticket: active.publicView()}
			s.mu.Unlock()
			return Ticket{}, activeErr
		}
	}

	ticket := Ticket{
		TicketID:       "ticket_" + randomToken(6),
		GuestID:        guestID,
		AccountID:      accountID,
		DisplayName:    normalizeDisplayName(displayName, guestID),
		Queue:          queue,
		ModeID:         modeID,
		ClockSeconds:   clockSeconds,
		ClockIncrement: clockIncrement,
		Status:         StatusQueued,
		Rating:         rating,
		CreatedAt:      now,
		UpdatedAt:      now,
		CancelSecret:   "cxl_" + randomToken(16),
	}

	opponent, found := s.findMatchCandidateLocked(queue, modeID, guestID, rating, clockSeconds, clockIncrement)
	if !found {
		s.tickets[ticket.TicketID] = ticket
		if err := s.persistLocked(); err != nil {
			delete(s.tickets, ticket.TicketID)
			s.mu.Unlock()
			return Ticket{}, err
		}
		s.mu.Unlock()
		// The create response is the ONE legitimate issuer of the cancel
		// secret (see redactTicketCancelSecret in the HTTP layer): return the
		// issued ticket itself, not the redacted projection.
		return ticket, nil
	}

	// Belt-and-braces against re-entrant enqueues from the same guest while
	// the pairing is mid-flight (the pairing reservation is the real guard).
	// NOTE: no defer for the delete -- s.mu is released during phase 2, so
	// the delete must happen under the lock explicitly on every exit below
	// (the old defer was only safe because one blanket defer held s.mu for
	// the whole function).
	s.inFlight[guestID] = true

	matchedAt := now
	roomID := "room_" + randomToken(5)

	whiteGuest, blackGuest := opponent.GuestID, guestID
	whiteAccount, blackAccount := opponent.AccountID, ticket.AccountID
	whiteName, blackName := normalizeDisplayName(opponent.DisplayName, opponent.GuestID), ticket.DisplayName

	b := make([]byte, 1)
	if _, err := rand.Read(b); err == nil && b[0]%2 == 0 {
		whiteGuest, blackGuest = blackGuest, whiteGuest
		whiteAccount, blackAccount = blackAccount, whiteAccount
		whiteName, blackName = blackName, whiteName
	}

	assignment := MatchAssignment{
		Queue:             queue,
		ModeID:            modeID,
		ClockSeconds:      clockSeconds,
		ClockIncrement:    clockIncrement,
		RoomID:            roomID,
		WhiteGuestID:      whiteGuest,
		BlackGuestID:      blackGuest,
		WhiteAccountID:    whiteAccount,
		BlackAccountID:    blackAccount,
		WhiteName:         whiteName,
		BlackName:         blackName,
		WhitePlayerSecret: "seat_" + randomToken(12),
		BlackPlayerSecret: "seat_" + randomToken(12),
	}

	// Phase 1: reserve both tickets as pairing while still under the lock.
	ticket.Status = StatusPairing
	ticket.AssignedRoom = roomID
	ticket.UpdatedAt = now
	opponent.Status = StatusPairing
	opponent.AssignedRoom = roomID
	opponent.UpdatedAt = now
	s.tickets[ticket.TicketID] = ticket
	s.tickets[opponent.TicketID] = opponent
	if err := s.persistLocked(); err != nil {
		delete(s.tickets, ticket.TicketID)
		delete(s.tickets, opponent.TicketID)
		s.mu.Unlock()
		return Ticket{}, err
	}

	if s.creator == nil {
		// No creator wired (unit tests): promote inline, semantics unchanged.
		s.promotePairingLocked(ticket.TicketID, opponent.TicketID, assignment, matchedAt)
		result := s.tickets[ticket.TicketID]
		delete(s.inFlight, guestID)
		s.mu.Unlock()
		return result.publicView(), nil
	}

	// Phase 2: create the room with s.mu RELEASED -- other queue operations
	// proceed while the WAN create runs.
	s.mu.Unlock()
	createErr := s.creator.CreateMatch(assignment)

	// Phase 3: promote or roll back under the lock.
	s.mu.Lock()
	delete(s.inFlight, guestID)
	result, err := s.completePairingLocked(ticket.TicketID, opponent.TicketID, assignment, matchedAt, createErr)
	s.mu.Unlock()
	return result, err
}

// promotePairingLocked transitions a live pairing reservation to matched for
// both seats. Returns false when the reservation no longer exists in the
// pairing state (rolled back by stale-pairing recovery, or cancelled by the
// guest while the creator call was running) -- the caller then leaves the
// created room alone; match-service's zombie GC finalizes empty rooms.
func (s *Service) promotePairingLocked(ticketID, opponentID string, assignment MatchAssignment, matchedAt time.Time) bool {
	ticket, okT := s.tickets[ticketID]
	opponent, okO := s.tickets[opponentID]
	if !okT || !okO || ticket.Status != StatusPairing || opponent.Status != StatusPairing {
		return false
	}
	promote := func(t *Ticket, matchedWith string) {
		t.Status = StatusMatched
		t.MatchedAt = &matchedAt
		t.MatchedWith = matchedWith
		if assignment.WhiteGuestID == t.GuestID {
			t.SeatColor = "white"
			t.OpponentName = assignment.BlackName
		} else {
			t.SeatColor = "black"
			t.OpponentName = assignment.WhiteName
		}
		t.AssignedRoom = assignment.RoomID
		t.UpdatedAt = matchedAt
	}
	promote(&ticket, opponent.GuestID)
	promote(&opponent, ticket.GuestID)
	s.tickets[ticketID] = ticket
	s.tickets[opponentID] = opponent
	if err := s.persistLocked(); err != nil {
		log.Printf("matchmaking: failed to persist promotion for room %s: %v", assignment.RoomID, err)
	}
	return true
}

// completePairingLocked finishes a two-phase pairing after the creator call:
// failure rolls the reservation back to queued, success promotes both seats.
func (s *Service) completePairingLocked(ticketID, opponentID string, assignment MatchAssignment, matchedAt time.Time, createErr error) (Ticket, error) {
	if createErr != nil {
		t, _ := s.tickets[ticketID]
		o, _ := s.tickets[opponentID]
		// Roll back to QUEUED, not deleted: the opponent may have been
		// waiting and polling for minutes and must simply keep waiting. If
		// the room was half-created despite the error it holds no ticket and
		// the match-service zombie GC finalizes it as an abandon draw.
		s.rollbackPairingLocked(t, o, s.nowUTC())
		return Ticket{}, createErr
	}
	if !s.promotePairingLocked(ticketID, opponentID, assignment, matchedAt) {
		log.Printf("matchmaking: pairing reservation for room %s vanished before promotion (recovered or cancelled mid-create); leaving the room to zombie GC", assignment.RoomID)
		if t, ok := s.tickets[ticketID]; ok {
			return t.publicView(), nil
		}
		return Ticket{}, nil
	}
	t := s.tickets[ticketID]
	return t.publicView(), nil
}

func (s *Service) Get(ticketID string) (Ticket, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pruneExpiredLocked(s.nowUTC()) {
		if err := s.persistLocked(); err != nil {
			log.Printf("warning: failed to persist after pruning: %v", err)
		}
	}
	ticket, ok := s.tickets[ticketID]
	return ticket, ok
}

func (s *Service) FindActiveTicket(guestID, accountID string) (Ticket, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pruneExpiredLocked(s.nowUTC()) {
		if err := s.persistLocked(); err != nil {
			log.Printf("warning: failed to persist after pruning: %v", err)
		}
	}
	return s.findActiveTicketLocked(guestID, accountID)
}

// Cancel marks a queued ticket cancelled. The caller must present the
// per-ticket cancel secret issued at enqueue time.
func (s *Service) Cancel(ticketID, cancelSecret string) (Ticket, bool, error) {
	return s.cancelWithAuth(ticketID, cancelSecret, false)
}

// CancelByService cancels a queued ticket from a trusted internal caller
// (moderation, service ops). No per-ticket secret is required.
func (s *Service) CancelByService(ticketID string) (Ticket, bool, error) {
	return s.cancelWithAuth(ticketID, "", true)
}

func (s *Service) cancelWithAuth(ticketID, cancelSecret string, serviceCaller bool) (Ticket, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.nowUTC()
	if s.pruneExpiredLocked(now) {
		if err := s.persistLocked(); err != nil {
			return Ticket{}, false, err
		}
	}
	ticket, ok := s.tickets[ticketID]
	if !ok {
		return Ticket{}, false, nil
	}
	if ticket.Status != StatusQueued {
		return ticket, false, nil
	}
	if !serviceCaller && (cancelSecret != ticket.CancelSecret || ticket.CancelSecret == "") {
		return ticket, false, ErrCancelUnauthorized
	}
	ticket.Status = StatusCancelled
	ticket.UpdatedAt = now
	if err := s.persistLocked(); err != nil {
		return Ticket{}, false, err
	}
	s.tickets[ticketID] = ticket
	return ticket, true, nil
}

func (s *Service) Snapshot(queue QueueName, modeID contracts.MatchModeID) QueueSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pruneExpiredLocked(s.nowUTC()) {
		if err := s.persistLocked(); err != nil {
			log.Printf("warning: failed to persist after pruning: %v", err)
		}
	}

	modeID = normalizeModeID(modeID)
	snapshot := QueueSnapshot{Queue: queue, ModeID: modeID}
	for _, ticket := range s.tickets {
		if ticket.Queue != queue {
			continue
		}
		if normalizeModeID(ticket.ModeID) != modeID {
			continue
		}
		switch ticket.Status {
		case StatusQueued:
			snapshot.QueuedCount++
		case StatusMatched:
			snapshot.MatchedCount++
		case StatusCancelled:
			snapshot.CancelledCount++
		}
	}
	return snapshot
}

func (s *Service) List(queue QueueName, modeID contracts.MatchModeID) []Ticket {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pruneExpiredLocked(s.nowUTC()) {
		if err := s.persistLocked(); err != nil {
			log.Printf("warning: failed to persist after pruning: %v", err)
		}
	}

	modeID = normalizeModeID(modeID)
	items := make([]Ticket, 0)
	for _, ticket := range s.tickets {
		if queue != "" && ticket.Queue != queue {
			continue
		}
		if modeID != "" && normalizeModeID(ticket.ModeID) != modeID {
			continue
		}
		items = append(items, ticket)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	for i := range items {
		items[i].CancelSecret = ""
	}
	return items
}

func (s *Service) Stats() ServiceStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pruneExpiredLocked(s.nowUTC()) {
		if err := s.persistLocked(); err != nil {
			log.Printf("warning: failed to persist after pruning: %v", err)
		}
	}

	stats := ServiceStats{
		Backend:      "memory",
		TotalTickets: len(s.tickets),
		Casual:       QueueSnapshot{Queue: QueueCasual},
		Rated:        QueueSnapshot{Queue: QueueRated},
	}
	if s.store != nil {
		stats.Backend = s.store.backend()
	}
	for _, ticket := range s.tickets {
		var snapshot *QueueSnapshot
		switch ticket.Queue {
		case QueueCasual:
			snapshot = &stats.Casual
		case QueueRated:
			snapshot = &stats.Rated
		default:
			continue
		}
		switch ticket.Status {
		case StatusQueued:
			snapshot.QueuedCount++
		case StatusMatched:
			snapshot.MatchedCount++
		case StatusCancelled:
			snapshot.CancelledCount++
		}
	}
	return stats
}

// rollbackPairingLocked reverts tickets reserved for a failed match creation
// back to the queued state (new UpdatedAt so the queued TTL restarts) instead
// of deleting them -- deleting a ticket the guest is actively polling turned
// one transient create failure into a silent kick out of the queue.
// Defensive: only tickets still in the pairing/reserved shape are touched --
// a ticket already promoted (or re-paired elsewhere by stale-pairing
// recovery) is left alone.
func (s *Service) rollbackPairingLocked(ticket, opponent Ticket, now time.Time) {
	requeued := false
	for i := range s.tickets {
		t := s.tickets[i]
		if t.TicketID != ticket.TicketID && t.TicketID != opponent.TicketID {
			continue
		}
		if t.Status != StatusPairing || t.SeatColor != "" || t.MatchedAt != nil {
			continue
		}
		t.Status = StatusQueued
		t.AssignedRoom = ""
		t.UpdatedAt = now
		s.tickets[i] = t
		requeued = true
	}
	if requeued {
		if err := s.persistLocked(); err != nil {
			log.Printf("failed to persist after CreateMatch rollback: %v", err)
		}
	}
}

func (s *Service) findMatchCandidateLocked(queue QueueName, modeID contracts.MatchModeID, guestID string, rating int, clockSeconds, clockIncrement int64) (Ticket, bool) {
	candidates := make([]Ticket, 0)
	now := s.nowUTC()
	for _, ticket := range s.tickets {
		if ticket.Queue != queue || normalizeModeID(ticket.ModeID) != modeID || ticket.Status != StatusQueued || ticket.GuestID == guestID {
			continue
		}
		// Time control must match exactly. A 5+0 seek must never steal a
		// 30+0 waiter just because the rating gap is small; the clock is
		// part of the lane identity for pairing purposes.
		if normalizeClockSeconds(ticket.ClockSeconds) != clockSeconds || normalizeClockIncrement(ticket.ClockIncrement) != clockIncrement {
			continue
		}
		diff := ticket.Rating - rating
		if diff < 0 {
			diff = -diff
		}
		expansion := int(now.Sub(ticket.CreatedAt).Seconds() / 30) * 50
		maxDiff := defaultMaxRatingDiff + expansion
		if diff > maxDiff {
			continue
		}
		candidates = append(candidates, ticket)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})
	if len(candidates) == 0 {
		return Ticket{}, false
	}
	return candidates[0], true
}

func (s *Service) findActiveTicketForGuestLocked(guestID string) (Ticket, bool) {
	return s.findActiveTicketLocked(guestID, "")
}

func (s *Service) findActiveTicketLocked(guestID, accountID string) (Ticket, bool) {
	guestID = strings.TrimSpace(guestID)
	accountID = strings.TrimSpace(accountID)
	var latest Ticket
	found := false
	for _, ticket := range s.tickets {
		if ticket.Status == StatusCancelled {
			continue
		}
		if guestID != "" && ticket.GuestID != guestID {
			continue
		}
		if guestID == "" && accountID != "" && strings.TrimSpace(ticket.AccountID) != accountID {
			continue
		}
		if guestID == "" && accountID == "" {
			continue
		}
		if !found || ticket.UpdatedAt.After(latest.UpdatedAt) {
			latest = ticket
			found = true
		}
	}
	return latest, found
}

// publicView is the client-safe projection of a ticket: the cancel secret is
// stripped everywhere EXCEPT the create/re-join response path, which re-issues
// it explicitly before returning (see EnqueueWithAccount). Pairing
// reservations read as queued so the wire contract never exposes the internal
// two-phase state.
func (t Ticket) publicView() Ticket {
	out := t
	out.CancelSecret = ""
	if out.Status == StatusPairing {
		out.Status = StatusQueued
		out.AssignedRoom = ""
	}
	return out
}

func randomToken(bytesCount int) string {
	buf := make([]byte, bytesCount)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}

func normalizeDisplayName(displayName, fallback string) string {
	value := displayName
	if value == "" {
		value = fallback
	}
	if value == "" {
		return "Player"
	}
	return value
}

func (s *Service) loadLocked() error {
	if s.store == nil {
		return nil
	}
	tickets, err := s.store.load()
	if err != nil {
		return err
	}
	if tickets == nil {
		tickets = make(map[string]Ticket)
	}
	for ticketID, ticket := range tickets {
		ticket.ModeID = normalizeModeID(ticket.ModeID)
		tickets[ticketID] = ticket
	}
	s.tickets = tickets
	if s.pruneExpiredLocked(s.nowUTC()) {
		return s.persistLocked()
	}
	return nil
}

func (s *Service) nowUTC() time.Time {
	if s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

// recoverStalePairingsLocked rolls pairing reservations that outlived
// pairingRecoveryTTL back to queued. Called before every prune sweep: a
// process crash (or a hung creator) mid-pairing must not wedge the two
// waiting guests out of the queue -- their tickets simply resume seeking, and
// a room half-created by the old attempt holds no live ticket and gets
// finalized by match-service's zombie GC.
func (s *Service) recoverStalePairingsLocked(now time.Time) bool {
	changed := false
	for ticketID, ticket := range s.tickets {
		if ticket.Status != StatusPairing {
			continue
		}
		if ticket.UpdatedAt.Add(s.pairingRecoveryTTL).After(now) {
			continue
		}
		log.Printf("matchmaking: rolling back stale pairing ticket %s (guest=%s room=%s, paired=%s ago)",
			ticketID, ticket.GuestID, ticket.AssignedRoom, now.Sub(ticket.UpdatedAt).Round(time.Second))
		ticket.Status = StatusQueued
		ticket.AssignedRoom = ""
		ticket.UpdatedAt = now
		s.tickets[ticketID] = ticket
		changed = true
	}
	if changed {
		if err := s.persistLocked(); err != nil {
			log.Printf("matchmaking: failed to persist stale-pairing rollback: %v", err)
		}
	}
	return changed
}

func (s *Service) pruneExpiredLocked(now time.Time) bool {
	changed := false
	for ticketID, ticket := range s.tickets {
		if !s.ticketRecoverableLocked(ticket, now) {
			if ticket.Status == StatusMatched && strings.TrimSpace(ticket.AssignedRoom) != "" {
				// A matched ticket pointing at a live room should have been
				// consumed by the claim flow. Seeing this log means a guest
				// was matched but never completed the handoff -- worth
				// surfacing rather than silently dropping.
				log.Printf("matchmaking: pruning expired MATCHED ticket %s (guest=%s room=%s matched=%s ago)",
					ticket.TicketID, ticket.GuestID, ticket.AssignedRoom, now.Sub(ticket.UpdatedAt).Round(time.Second))
			}
			delete(s.tickets, ticketID)
			changed = true
		}
	}
	return changed
}

func (s *Service) ticketRecoverableLocked(ticket Ticket, now time.Time) bool {
	switch ticket.Status {
	case StatusQueued:
		return ticket.UpdatedAt.Add(s.queuedTTL).After(now)
	case StatusPairing:
		// A LIVE reservation is always recoverable (never pruned). A STALE
		// one -- older than pairingRecoveryTTL, i.e. the creator call or the
		// process died mid-flight -- is also "recoverable" from the prune
		// loop's perspective, but recoverStalePairingsLocked first ROLLS it
		// back to queued so both waiting guests simply resume seeking.
		return true
	case StatusMatched:
		return ticket.UpdatedAt.Add(s.matchedRecoveryTTL).After(now)
	case StatusCancelled:
		return ticket.UpdatedAt.Add(s.cancelledTicketTTL).After(now)
	default:
		return false
	}
}

func (s *Service) persistLocked() error {
	if s.store == nil {
		return nil
	}
	return s.store.persist(s.tickets)
}

func normalizeModeID(modeID contracts.MatchModeID) contracts.MatchModeID {
	return contracts.NormalizeMatchModeID(string(modeID))
}
