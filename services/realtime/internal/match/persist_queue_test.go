package match

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/chess404/realtime/internal/contracts"
	"github.com/redis/go-redis/v9"
)

// The async persist queue (persist_queue.go) moved the Upstash save + publish
// off the mutation path (~1000ms intent latency in production). These tests
// pin the guarantees that make that safe: local WS delivery precedes the
// save, bursts coalesce into one latest-wins write, terminal states land on
// the store inline, Close drains in-flight writes, and saves per match are
// strictly ordered.

// gatedStore counts SaveSnapshotAtomic calls, optionally blocks them (only
// once armed -- match creation must never block, so arm AFTER create), and
// can observe every persisted state via onSave.
type gatedStore struct {
	MemoryMatchStore
	armed        atomic.Bool
	entered      chan struct{}
	release      chan struct{}
	releaseOnceDo sync.Once
	saves        atomic.Int64
	onSaveMu     sync.Mutex
	onSave       func(state []byte)
}

func newGatedStore() *gatedStore {
	return &gatedStore{MemoryMatchStore: *NewMemoryMatchStore()}
}

// armGate makes the NEXT and all subsequent saves block until release.
// entered is buffered so a wedged save never deadlocks service shutdown
// during cleanup.
func (g *gatedStore) armGate() {
	g.entered = make(chan struct{}, 1)
	g.release = make(chan struct{})
	g.armed.Store(true)
}

// releaseOnce closes the gate exactly once (safe from test body + cleanup).
func (g *gatedStore) releaseOnce() {
	g.releaseOnceDo.Do(func() {
		if g.release != nil {
			close(g.release)
		}
	})
}

func (g *gatedStore) SaveSnapshotAtomic(matchID string, state []byte, secretWhite, secretBlack string, history, events, presence, seenIDs []byte) error {
	g.saves.Add(1)
	g.onSaveMu.Lock()
	hook := g.onSave
	g.onSaveMu.Unlock()
	if hook != nil {
		hook(state)
	}
	if g.armed.Load() {
		g.entered <- struct{}{}
		<-g.release
	}
	return g.MemoryMatchStore.SaveSnapshotAtomic(matchID, state, secretWhite, secretBlack, history, events, presence, seenIDs)
}

func newGatedService(t *testing.T, store *gatedStore) *Service {
	t.Helper()
	svc := NewServiceWithStoreAndBroadcaster(nil, store, nil)
	t.Cleanup(svc.Close)
	return svc
}

// openMove returns a legal one-step pawn advance for the given side and
// file, so tests can build arbitrary-length legal games.
func openMove(i int, white bool) (contracts.Square, contracts.Square) {
	rowFrom, rowTo := 6, 5
	if white {
		rowFrom, rowTo = 1, 2
	}
	return contracts.Square{Row: rowFrom, Col: i % 8}, contracts.Square{Row: rowTo, Col: i % 8}
}

func moveIntent(matchID, player string, from, to contracts.Square) contracts.PlayerIntent {
	return contracts.PlayerIntent{
		Type: "make_move", MatchID: matchID, PlayerID: player,
		From: &from, To: &to,
	}
}

// The mutation must return (and the subscriber must have received the
// broadcast) even while the store write is wedged. This is the behavior the
// whole queue exists for.
func TestApplyIntentReturnsBeforeStoreSaveCompletes(t *testing.T) {
	store := newGatedStore()
	svc := newGatedService(t, store)

	now := time.Now().UTC()
	createTestMatch(svc, contracts.CreateMatchRequest{MatchID: "gate_1"}, now)
	store.armGate()
	t.Cleanup(store.releaseOnce)

	stream, unsub, _, err := svc.Subscribe("gate_1", "white_player", "white-secret")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer unsub()
	// Subscribe delivers an initial snapshot into the buffered channel;
	// drain it so the move broadcast below is the next visible message.
	select {
	case <-stream:
	default:
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		gFrom, gTo := openMove(0, true)
		if _, err := applyTestIntent(svc, moveIntent("gate_1", "white_player", gFrom, gTo), now.Add(time.Second)); err != nil {
			t.Errorf("move intent: %v", err)
		}
	}()

	// The save is wedged. ApplyIntent and the local broadcast must still
	// have completed well within this budget.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ApplyIntent blocked on the store save; the async queue is not engaged")
	}
	select {
	case snap := <-stream:
		if len(snap.Match.MoveHistory) == 0 {
			t.Fatal("expected local broadcast to carry the move")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("local subscriber never received the broadcast")
	}

	// Un-wedge and confirm the queued save still lands.
	store.releaseOnce()
	if !svc.persistQueueWait(5 * time.Second) {
		t.Fatal("persist queue never drained")
	}
	if store.saves.Load() == 0 {
		t.Fatal("expected the deferred save to reach the store")
	}
}

// A burst of moves on one match must coalesce into deferred saves carrying
// the final position (not one WAN round trip per move).
func TestPersistQueueCoalescesBurstIntoLatestState(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisURL := "redis://" + redisServer.Addr() + "/0"
	store, err := NewRedisMatchStore(redisURL, "test:coalesce")
	if err != nil {
		t.Fatalf("new redis match store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	svc := NewServiceWithStoreAndBroadcaster(nil, store, nil)
	t.Cleanup(svc.Close)

	now := time.Now().UTC()
	createTestMatch(svc, contracts.CreateMatchRequest{MatchID: "coalesce_1"}, now)

	// e2e4 e7e5 g1f3 b8c6 -- every move legal at its turn.
	opening := []struct {
		player       string
		from, to     contracts.Square
	}{
		{"white_player", contracts.Square{Row: 1, Col: 4}, contracts.Square{Row: 3, Col: 4}},
		{"black_player", contracts.Square{Row: 6, Col: 4}, contracts.Square{Row: 4, Col: 4}},
		{"white_player", contracts.Square{Row: 0, Col: 6}, contracts.Square{Row: 2, Col: 5}},
		{"black_player", contracts.Square{Row: 7, Col: 1}, contracts.Square{Row: 5, Col: 2}},
	}
	for i, mv := range opening {
		intent := contracts.PlayerIntent{
			Type: "make_move", MatchID: "coalesce_1", PlayerID: mv.player,
			From: &contracts.Square{Row: mv.from.Row, Col: mv.from.Col},
			To:   &contracts.Square{Row: mv.to.Row, Col: mv.to.Col},
		}
		if _, err := applyTestIntent(svc, intent, now.Add(time.Duration(i+1)*time.Second)); err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
	}
	if !svc.persistQueueWait(5 * time.Second) {
		t.Fatal("persist queue never drained")
	}

	var stored contracts.MatchSnapshotResponse
	if err := store.LoadState("coalesce_1", &stored); err != nil {
		t.Fatalf("load state: %v", err)
	}
	if len(stored.Match.MoveHistory) != len(opening) {
		t.Fatalf("expected stored state to carry all %d moves, got %d", len(opening), len(stored.Match.MoveHistory))
	}
}

// A finished match must be readable from the store the moment the finishing
// intent returns: terminal states flush inline (after draining older queued
// writes), never through the queue.
func TestTerminalStateFlushesInlineToStore(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisURL := "redis://" + redisServer.Addr() + "/0"
	store, err := NewRedisMatchStore(redisURL, "test:terminal")
	if err != nil {
		t.Fatalf("new redis match store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	svc := NewServiceWithStoreAndBroadcaster(nil, store, nil)
	t.Cleanup(svc.Close)

	now := time.Now().UTC()
	createTestMatch(svc, contracts.CreateMatchRequest{MatchID: "terminal_1"}, now)
	if _, err := applyTestIntent(svc, contracts.PlayerIntent{
		Type: "resign", MatchID: "terminal_1", PlayerID: "white_player",
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("resign: %v", err)
	}

	// No queue wait here on purpose: durability must already hold.
	var snap contracts.MatchSnapshotResponse
	if err := store.LoadState("terminal_1", &snap); err != nil {
		t.Fatalf("finished match not on the store immediately after ApplyIntent: %v", err)
	}
	if snap.Match.Status != "finished" || snap.Match.Winner != "black" {
		t.Fatalf("expected finished/black on the store, got %s/%s", snap.Match.Status, snap.Match.Winner)
	}
}

// A terminal flush must not be overtaken by an older queued write: after a
// burst of moves and a resign, the store ends on the finished state.
func TestQueuedWritesCannotOvertakeTerminalFlush(t *testing.T) {
	store := newGatedStore()
	svc := newGatedService(t, store)

	now := time.Now().UTC()
	createTestMatch(svc, contracts.CreateMatchRequest{MatchID: "overtake_1"}, now)

	// Saturate the queue with legal pawn moves, then finish inline.
	for i := 0; i < 4; i++ {
		wFrom, wTo := openMove(i, true)
		bFrom, bTo := openMove(i, false)
		if _, err := applyTestIntent(svc, moveIntent("overtake_1", "white_player", wFrom, wTo), now.Add(time.Duration(2*i+1)*time.Second)); err != nil {
			t.Fatalf("white move %d: %v", i, err)
		}
		if _, err := applyTestIntent(svc, moveIntent("overtake_1", "black_player", bFrom, bTo), now.Add(time.Duration(2*i+2)*time.Second)); err != nil {
			t.Fatalf("black move %d: %v", i, err)
		}
	}
	if _, err := applyTestIntent(svc, contracts.PlayerIntent{
		Type: "resign", MatchID: "overtake_1", PlayerID: "white_player",
	}, now.Add(60 * time.Second)); err != nil {
		t.Fatalf("resign: %v", err)
	}
	if !svc.persistQueueWait(5 * time.Second) {
		t.Fatal("persist queue never drained")
	}

	var stored contracts.MatchSnapshotResponse
	if err := store.LoadState("overtake_1", &stored); err != nil {
		t.Fatalf("load state: %v", err)
	}
	if stored.Match.Status != "finished" || stored.Match.Winner != "black" {
		t.Fatalf("queued writes overtook the terminal flush: status=%s winner=%s", stored.Match.Status, stored.Match.Winner)
	}
}

// Close must drain in-flight and queued saves so a redeploy loses nothing.
func TestCloseDrainsQueuedSaves(t *testing.T) {
	store := newGatedStore()
	svc := NewServiceWithStoreAndBroadcaster(nil, store, nil)

	now := time.Now().UTC()
	createTestMatch(svc, contracts.CreateMatchRequest{MatchID: "close_1"}, now)
	store.armGate()
	wFrom, wTo := openMove(0, true)
	if _, err := applyTestIntent(svc, moveIntent("close_1", "white_player", wFrom, wTo), now.Add(time.Second)); err != nil {
		t.Fatalf("move intent: %v", err)
	}

	// Wait until the worker is inside the wedged save, then Close.
	select {
	case <-store.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("deferred save never started")
	}
	t.Cleanup(store.releaseOnce)
	closed := make(chan struct{})
	go func() {
		svc.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned while a save was still in flight")
	case <-time.After(300 * time.Millisecond):
	}

	store.releaseOnce()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close never drained the in-flight save")
	}
	if store.saves.Load() == 0 {
		t.Fatal("expected the drained save to reach the store")
	}
}

// Saves for one match must be strictly ordered even under a burst: the store
// must never persist an older state after a newer one.
func TestPersistQueueSavesAreOrderedPerMatch(t *testing.T) {
	store := newGatedStore()
	svc := newGatedService(t, store)

	var mu sync.Mutex
	var lengths []int
	store.onSaveMu.Lock()
	store.onSave = func(state []byte) {
		var snap contracts.MatchSnapshotResponse
		if json.Unmarshal(state, &snap) == nil {
			mu.Lock()
			lengths = append(lengths, len(snap.Match.MoveHistory))
			mu.Unlock()
		}
	}
	store.onSaveMu.Unlock()

	now := time.Now().UTC()
	createTestMatch(svc, contracts.CreateMatchRequest{MatchID: "order_1"}, now)
	for i := 0; i < 4; i++ {
		wFrom, wTo := openMove(i, true)
		bFrom, bTo := openMove(i, false)
		if _, err := applyTestIntent(svc, moveIntent("order_1", "white_player", wFrom, wTo), now.Add(time.Duration(2*i+1)*time.Second)); err != nil {
			t.Fatalf("white move %d: %v", i, err)
		}
		if _, err := applyTestIntent(svc, moveIntent("order_1", "black_player", bFrom, bTo), now.Add(time.Duration(2*i+2)*time.Second)); err != nil {
			t.Fatalf("black move %d: %v", i, err)
		}
	}
	if !svc.persistQueueWait(5 * time.Second) {
		t.Fatal("persist queue never drained")
	}

	mu.Lock()
	defer mu.Unlock()
	for i := 1; i < len(lengths); i++ {
		if lengths[i] < lengths[i-1] {
			t.Fatalf("save order regressed by move count: %v", lengths)
		}
	}
	if len(lengths) == 0 || lengths[len(lengths)-1] != 8 {
		t.Fatalf("expected the final save to carry all 8 moves, got %v", lengths)
	}
}

var _ = redis.Nil // keep the import anchored if helpers shift
