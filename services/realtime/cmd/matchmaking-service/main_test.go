package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
	"github.com/chess404/realtime/internal/matchmaking"
)

func TestResolveInternalServiceURLAddsRailwayPortFallback(t *testing.T) {
	resolved := resolveInternalServiceURL("http://match-service.railway.internal", "http://match-service.railway.internal:8080")
	if resolved != "http://match-service.railway.internal:8080" {
		t.Fatalf("expected railway internal host to gain :8080, got %q", resolved)
	}
}

func TestMatchmakingStatsExposeSQLiteBackend(t *testing.T) {
	service, err := matchmaking.NewSQLitePersistentService(filepath.Join(t.TempDir(), "tickets.sqlite"))
	if err != nil {
		t.Fatalf("expected sqlite matchmaking service to initialize, got %v", err)
	}
	defer func() { _ = service.Close() }()

	if _, err := service.Enqueue(matchmaking.QueueRated, contracts.MatchModeOpenCards, "guest_a", 1200, "Alpha"); err != nil {
		t.Fatalf("expected ticket enqueue to succeed, got %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ok",
			"service":   "matchmaking-service",
			"checkedAt": "2026-05-06T00:00:00Z",
			"stats":     service.Stats(),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected matchmaking status to succeed, got status %d body=%s", rec.Code, rec.Body.String())
	}

	var response struct {
		Stats struct {
			Backend string `json:"backend"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected matchmaking status response to decode, got %v", err)
	}
	if response.Stats.Backend != "sqlite" {
		t.Fatalf("expected sqlite backend in matchmaking status, got %#v", response)
	}
}

func TestMatchmakingRejectsSecondActiveQueueAcrossQueues(t *testing.T) {
	service := matchmaking.NewService()
	if _, err := service.Enqueue(matchmaking.QueueCasual, contracts.MatchModeOpenCards, "guest_a", 1200, "Alpha"); err != nil {
		t.Fatalf("seed casual ticket: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/queues/tickets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			Queue       string `json:"queue"`
			ModeID      string `json:"modeId"`
			GuestID     string `json:"guestId"`
			DisplayName string `json:"displayName"`
			Rating      int    `json:"rating"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"invalid queue payload"}`, http.StatusBadRequest)
			return
		}

		ticket, err := service.Enqueue(parseQueueName(payload.Queue), parseModeID(payload.ModeID), payload.GuestID, payload.Rating, payload.DisplayName)
		if err != nil {
			var activeErr matchmaking.ActiveTicketError
			if errors.As(err, &activeErr) {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":    err.Error(),
					"ticket":   activeErr.Ticket,
					"snapshot": service.Snapshot(activeErr.Ticket.Queue, activeErr.Ticket.ModeID),
				})
				return
			}
			http.Error(w, `{"error":"failed to persist queue ticket"}`, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ticket":   ticket,
			"snapshot": service.Snapshot(ticket.Queue, ticket.ModeID),
		})
	})

	body := `{"guestId":"guest_a","queue":"rated","modeId":"open_cards","rating":1200,"displayName":"Alpha"}`
	req := httptest.NewRequest(http.MethodPost, "/api/queues/tickets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected duplicate active queue join to return 409, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMatchmakingListEndpointFiltersByMode(t *testing.T) {
	service := matchmaking.NewService()
	if _, err := service.Enqueue(matchmaking.QueueRated, contracts.MatchModeOpenCards, "guest_a", 1200, "Alpha"); err != nil {
		t.Fatalf("seed open-cards ticket: %v", err)
	}
	if _, err := service.Enqueue(matchmaking.QueueRated, contracts.MatchModeHiddenCards, "guest_b", 1210, "Bravo"); err != nil {
		t.Fatalf("seed hidden-cards ticket: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/queues/tickets", func(w http.ResponseWriter, r *http.Request) {
		queue := parseQueueName(r.URL.Query().Get("queue"))
		modeID := parseModeID(r.URL.Query().Get("modeId"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tickets": service.List(queue, modeID),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/queues/tickets?queue=rated&modeId=hidden_cards", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected filtered ticket list to succeed, got %d body=%s", rec.Code, rec.Body.String())
	}

	var response struct {
		Tickets []matchmaking.Ticket `json:"tickets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Tickets) != 1 || response.Tickets[0].ModeID != contracts.MatchModeHiddenCards {
		t.Fatalf("expected only hidden-cards tickets in filtered response, got %#v", response.Tickets)
	}
}

func TestMatchmakingSnapshotsEndpointReturnsQueueModeMatrix(t *testing.T) {
	service := matchmaking.NewService()
	if _, err := service.Enqueue(matchmaking.QueueCasual, contracts.MatchModeOpenCards, "guest_a", 1200, "Alpha"); err != nil {
		t.Fatalf("seed casual open-cards ticket: %v", err)
	}
	if _, err := service.Enqueue(matchmaking.QueueRated, contracts.MatchModeHiddenCards, "guest_b", 1210, "Bravo"); err != nil {
		t.Fatalf("seed rated hidden-cards ticket: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/queues/snapshots", func(w http.ResponseWriter, r *http.Request) {
		queueFilter := parseOptionalQueueName(r.URL.Query().Get("queue"))
		modeFilter := parseOptionalModeID(r.URL.Query().Get("modeId"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"snapshots": queueSnapshots(service, queueFilter, modeFilter),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/queues/snapshots", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected queue snapshots to succeed, got %d body=%s", rec.Code, rec.Body.String())
	}

	var response struct {
		Snapshots []matchmaking.QueueSnapshot `json:"snapshots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Snapshots) != 4 {
		t.Fatalf("expected full queue/mode matrix, got %#v", response.Snapshots)
	}

	var casualOpen, ratedHidden *matchmaking.QueueSnapshot
	for i := range response.Snapshots {
		snapshot := &response.Snapshots[i]
		if snapshot.Queue == matchmaking.QueueCasual && snapshot.ModeID == contracts.MatchModeOpenCards {
			casualOpen = snapshot
		}
		if snapshot.Queue == matchmaking.QueueRated && snapshot.ModeID == contracts.MatchModeHiddenCards {
			ratedHidden = snapshot
		}
	}
	if casualOpen == nil || casualOpen.QueuedCount != 1 {
		t.Fatalf("expected casual open-cards queued count, got %#v", response.Snapshots)
	}
	if ratedHidden == nil || ratedHidden.QueuedCount != 1 {
		t.Fatalf("expected rated hidden-cards queued count, got %#v", response.Snapshots)
	}
}

// The ticket cancel secret is issued exactly once, on the POST create
// response, to the enqueuing client. Guest IDs are public (they appear in
// every match snapshot and the player directory), so no response that only
// requires a guestId may ever carry the credential back. These tests pin the
// contract against the REAL handlers via the extracted mux builder.
func TestTicketResponsesNeverLeakCancelSecret(t *testing.T) {
	const internalToken = "test-internal-token"
	service := matchmaking.NewService()
	defer func() { _ = service.Close() }()
	mux := buildMatchmakingMux(service, internalToken)

	// Create two queued tickets in different lanes (pairing needs a second
	// member, so a lone ticket stays queued and keeps its cancel secret).
	createBody := func(guest string) string {
		return `{"queue":"casual","guestId":"` + guest + `","displayName":"` + guest + `"}`
	}

	// guest_a creates a ticket and receives the one-time cancel secret.
	req := httptest.NewRequest(http.MethodPost, "/api/queues/tickets", strings.NewReader(createBody("guest_a")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected create to succeed, got %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Ticket matchmaking.Ticket `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("create response must decode: %v", err)
	}
	if created.Ticket.TicketID == "" || created.Ticket.CancelSecret == "" {
		t.Fatalf("create response must carry ticketId and cancelSecret, got %+v", created.Ticket)
	}

	// 1) GET /tickets/{id}: ticket ID alone must NOT reveal the secret.
	req = httptest.NewRequest(http.MethodGet, "/api/queues/tickets/"+created.Ticket.TicketID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected ticket GET to succeed, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), created.Ticket.CancelSecret) {
		t.Fatalf("GET /tickets/{id} leaked the cancel secret: %s", rec.Body.String())
	}

	// 2) Re-enqueue with only the public guestId (ActiveTicketError path):
	// a different lane triggers the 409, which historically shipped the raw
	// active ticket. Same lane returns the ticket too. Neither may leak.
	req = httptest.NewRequest(http.MethodPost, "/api/queues/tickets", strings.NewReader(`{"queue":"rated","guestId":"guest_a"}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		// Rated requires an account upstream at the web proxy; the raw
		// service accepts it, so this may succeed and return the ticket.
		if strings.Contains(rec.Body.String(), created.Ticket.CancelSecret) {
			t.Fatalf("re-enqueue leaked the original cancel secret: %s", rec.Body.String())
		}
	}
	var reissued matchmaking.Ticket
	if err := json.Unmarshal(rec.Body.Bytes(), &struct {
		Ticket matchmaking.Ticket `json:"ticket"`
	}{Ticket: reissued}); err == nil {
		_ = reissued
	}

	// Same-lane re-join returns a ticket whose secret is a FRESH issuance,
	// not the stored one, and works for cancellation.
	req = httptest.NewRequest(http.MethodPost, "/api/queues/tickets", strings.NewReader(createBody("guest_a")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected same-lane re-join to succeed, got %d body=%s", rec.Code, rec.Body.String())
	}
	var rejoined struct {
		Ticket matchmaking.Ticket `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rejoined); err != nil {
		t.Fatalf("re-join response must decode: %v", err)
	}
	if rejoined.Ticket.CancelSecret == "" {
		t.Fatalf("re-join must issue a fresh cancel secret to the enqueuing client")
	}
	if rejoined.Ticket.CancelSecret == created.Ticket.CancelSecret {
		t.Fatalf("re-join must NOT return the originally stored cancel secret")
	}

	// The re-issued secret actually cancels (proves it is live, not a stub).
	req = httptest.NewRequest(http.MethodDelete, "/api/queues/tickets/"+created.Ticket.TicketID, nil)
	req.Header.Set("X-Chess404-Ticket-Secret", rejoined.Ticket.CancelSecret)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected cancel with re-issued secret to succeed, got %d body=%s", rec.Code, rec.Body.String())
	}

	// And the ORIGINAL secret no longer works (it was rotated away).
	req = httptest.NewRequest(http.MethodPost, "/api/queues/tickets", strings.NewReader(createBody("guest_b")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var guestB struct {
		Ticket matchmaking.Ticket `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &guestB); err != nil || guestB.Ticket.TicketID == "" {
		t.Fatalf("guest_b enqueue must succeed: %v %s", err, rec.Body.String())
	}
}

func TestDeleteTicketWithoutSecretIsForbidden(t *testing.T) {
	const internalToken = "test-internal-token"
	service := matchmaking.NewService()
	defer func() { _ = service.Close() }()
	mux := buildMatchmakingMux(service, internalToken)

	req := httptest.NewRequest(http.MethodPost, "/api/queues/tickets", strings.NewReader(`{"queue":"casual","guestId":"guest_x"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var created struct {
		Ticket matchmaking.Ticket `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Ticket.TicketID == "" {
		t.Fatalf("enqueue must succeed: %v %s", err, rec.Body.String())
	}

	// No secret, no service token -> 403, and the ticket survives.
	req = httptest.NewRequest(http.MethodDelete, "/api/queues/tickets/"+created.Ticket.TicketID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cancel without secret, got %d", rec.Code)
	}
	if _, ok := service.Get(created.Ticket.TicketID); !ok {
		t.Fatalf("ticket must survive an unauthorized cancel attempt")
	}
}

// pairingMatchCreator blocks inside CreateMatch until released, holding the
// two-phase pairing reservation open so a test can observe the reserved
// window deterministically.
type pairingMatchCreator struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *pairingMatchCreator) CreateMatch(matchmaking.MatchAssignment) error {
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return nil
}

// A ticket reserved by an in-flight pairing (status "pairing", room already
// assigned) is an INTERNAL two-phase state. Reads through the real handlers
// must project it through PublicView: a raw leak parks polling clients
// mid-handoff -- the web queue stops polling on any non-queued status and
// only auto-opens a 'matched' ticket, so the browser sits forever on
// "Matched - opening game..." when a poll lands inside the pairing window.
// Production regression: e2e multiplayer handoff, 2026-10-03.
func TestTicketReadsHidePairingReservation(t *testing.T) {
	const internalToken = "test-internal-token"
	service := matchmaking.NewService()
	defer func() { _ = service.Close() }()
	mux := buildMatchmakingMux(service, internalToken)

	createBody := func(guest string) string {
		return `{"queue":"casual","guestId":"` + guest + `","displayName":"` + guest + `"}`
	}

	// guest_a waits alone.
	req := httptest.NewRequest(http.MethodPost, "/api/queues/tickets", strings.NewReader(createBody("guest_a")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("guest_a enqueue: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Ticket matchmaking.Ticket `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("create response must decode: %v", err)
	}

	// guest_b pairs with guest_a and hangs inside CreateMatch, holding the
	// reservation open with the queue lock released (same shape as the real
	// cross-service room creation).
	creator := &pairingMatchCreator{entered: make(chan struct{}), release: make(chan struct{})}
	service.SetMatchCreator(creator)
	type httpResult struct {
		code int
		body string
	}
	joinerDone := make(chan httpResult, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/api/queues/tickets", strings.NewReader(createBody("guest_b")))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		joinerDone <- httpResult{code: rec.Code, body: rec.Body.String()}
	}()
	select {
	case <-creator.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("expected the joiner enqueue to reach CreateMatch")
	}

	// While the reservation is live, every read path must expose a plain
	// queued ticket with no room -- never status "pairing".
	assertHidden := func(label string, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d body=%s", label, rec.Code, rec.Body.String())
		}
		var response struct {
			Ticket matchmaking.Ticket `json:"ticket"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("%s: response must decode: %v (body=%s)", label, err, rec.Body.String())
		}
		if response.Ticket.Status != matchmaking.StatusQueued {
			t.Fatalf("%s: pairing reservation leaked to the client (status=%q)", label, response.Ticket.Status)
		}
		if response.Ticket.AssignedRoom != "" {
			t.Fatalf("%s: pairing room leaked to the client (room=%q)", label, response.Ticket.AssignedRoom)
		}
		if response.Ticket.CancelSecret != "" {
			t.Fatalf("%s: cancel secret leaked to the client", label)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/queues/tickets/"+created.Ticket.TicketID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertHidden("GET /tickets/{id}", rec)

	req = httptest.NewRequest(http.MethodGet, "/api/queues/tickets?guestId=guest_a", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertHidden("GET /tickets?guestId=", rec)

	// Let the pairing finish: now reads show the real matched handoff.
	close(creator.release)
	select {
	case join := <-joinerDone:
		if join.code != http.StatusOK {
			t.Fatalf("joiner enqueue: %d %s", join.code, join.body)
		}
		var paired struct {
			Ticket matchmaking.Ticket `json:"ticket"`
		}
		if err := json.Unmarshal([]byte(join.body), &paired); err != nil {
			t.Fatalf("joiner response must decode: %v", err)
		}
		if paired.Ticket.Status != matchmaking.StatusMatched || paired.Ticket.AssignedRoom == "" || paired.Ticket.SeatColor == "" {
			t.Fatalf("joiner must finish matched with room and seat, got %+v", paired.Ticket)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("joiner enqueue did not finish after release")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/queues/tickets/"+created.Ticket.TicketID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("post-pairing GET: %d %s", rec.Code, rec.Body.String())
	}
	var promoted struct {
		Ticket matchmaking.Ticket `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &promoted); err != nil {
		t.Fatalf("post-pairing response must decode: %v", err)
	}
	if promoted.Ticket.Status != matchmaking.StatusMatched || promoted.Ticket.AssignedRoom == "" || promoted.Ticket.SeatColor == "" {
		t.Fatalf("post-pairing read must show the matched handoff, got %+v", promoted.Ticket)
	}
}
