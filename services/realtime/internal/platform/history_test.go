package platform

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// newTestArchiveStore returns a store whose writeLoop is joined before the
// test's temp dir is removed. The store persists asynchronously (writeCh ->
// writeLoop -> archive file), so a test that walks away from it lets that
// write land while t.TempDir cleanup is deleting the directory -- the
// sporadic "RemoveAll cleanup: directory not empty" failures on CI. Closing
// through t.Cleanup runs before TempDir's own cleanup (registered first).
func newTestArchiveStore(t *testing.T, path string) *MatchArchiveStore {
	t.Helper()
	store, err := NewMatchArchiveStore(path)
	if err != nil {
		t.Fatalf("expected archive store to initialize, got %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestMatchArchiveStoreUpsertAndReload(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "match-archive.json")
	store := newTestArchiveStore(t, storePath)

	now := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	snapshot := contracts.MatchSnapshotResponse{
		Match: contracts.MatchState{
			MatchID:      "archive_test",
			RulesVersion: "v1-alpha-foundation",
			Status:       "finished",
			Winner:       "white",
			FinishReason: "checkmate",
			MoveHistory:  []string{"e4", "e5"},
			CreatedAt:    now,
			UpdatedAt:    now.Add(time.Minute),
		},
		ReplayHead: 2,
	}
	if err := store.Upsert(snapshot); err != nil {
		t.Fatalf("expected upsert to succeed, got %v", err)
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("expected archive flush to succeed, got %v", err)
	}

	reloaded := newTestArchiveStore(t, storePath)
	entry, ok := reloaded.Get("archive_test")
	if !ok {
		t.Fatalf("expected archive entry to be reloadable")
	}
	if entry.MatchID != "archive_test" || entry.MoveCount != 2 || entry.LastMove != "e5" || entry.Winner != "white" || entry.FinishReason != "checkmate" {
		t.Fatalf("unexpected archive entry %#v", entry)
	}
}

func TestMatchArchiveStorePreservesPlayerMetadata(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "match-archive.json")
	store := newTestArchiveStore(t, storePath)

	now := time.Date(2026, 5, 6, 11, 0, 0, 0, time.UTC)
	snapshot := contracts.MatchSnapshotResponse{
		Match: contracts.MatchState{
			MatchID:        "archive_players",
			RulesVersion:   "v1-alpha-foundation",
			Queue:          "rated",
			ModeID:         contracts.MatchModeHiddenCards,
			WhiteGuestID:   "guest_white",
			BlackGuestID:   "guest_black",
			WhiteAccountID: "acct_white",
			BlackAccountID: "acct_black",
			WhiteName:      "Aurora Bishop 101",
			BlackName:      "Velvet Queen 202",
			Status:         "active",
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	if err := store.Upsert(snapshot); err != nil {
		t.Fatalf("expected upsert to succeed, got %v", err)
	}

	entry, ok := store.Get("archive_players")
	if !ok {
		t.Fatalf("expected archive entry lookup to succeed")
	}
	if entry.WhiteGuestID != "guest_white" || entry.BlackGuestID != "guest_black" {
		t.Fatalf("expected guest ids to persist, got %#v", entry)
	}
	if entry.WhiteAccountID != "acct_white" || entry.BlackAccountID != "acct_black" {
		t.Fatalf("expected account ids to persist, got %#v", entry)
	}
	if entry.WhiteName != "Aurora Bishop 101" || entry.BlackName != "Velvet Queen 202" {
		t.Fatalf("expected guest names to persist, got %#v", entry)
	}
	if entry.Queue != "rated" {
		t.Fatalf("expected queue metadata to persist, got %#v", entry)
	}
	if entry.ModeID != contracts.MatchModeHiddenCards {
		t.Fatalf("expected mode metadata to persist, got %#v", entry)
	}
}

// FlushMatch is the create/join critical path: it must make ONE match durable
// without rewriting the rest of the overlay. The file backend rewrites its
// whole file per persist and therefore does not implement the single-row
// fast path -- this test pins the documented fallback, where persist still
// receives every entry so a one-row flush can never erase siblings.
func TestMatchArchiveStoreFlushMatchPreservesOtherRows(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "match-archive.json")
	store := newTestArchiveStore(t, storePath)

	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	for i, id := range []string{"flush_a", "flush_b", "flush_c"} {
		if err := store.Upsert(contracts.MatchSnapshotResponse{Match: contracts.MatchState{
			MatchID:      id,
			RulesVersion: "v1-alpha-foundation",
			Status:       "active",
			CreatedAt:    now,
			UpdatedAt:    now.Add(time.Duration(i) * time.Minute),
		}}); err != nil {
			t.Fatalf("expected upsert of %s to succeed, got %v", id, err)
		}
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("expected initial flush to succeed, got %v", err)
	}

	// Re-upsert one row as finished, then flush ONLY that row.
	if err := store.Upsert(contracts.MatchSnapshotResponse{Match: contracts.MatchState{
		MatchID:      "flush_b",
		RulesVersion: "v1-alpha-foundation",
		Status:       "finished",
		Winner:       "white",
		CreatedAt:    now,
		UpdatedAt:    now.Add(10 * time.Minute),
	}}); err != nil {
		t.Fatalf("expected dirty upsert to succeed, got %v", err)
	}
	if err := store.FlushMatch("flush_b"); err != nil {
		t.Fatalf("expected FlushMatch to succeed, got %v", err)
	}
	// An unknown id must be a silent no-op, not a full rewrite or an error.
	if err := store.FlushMatch("flush_missing"); err != nil {
		t.Fatalf("expected FlushMatch of an unknown match to be a no-op, got %v", err)
	}

	reloaded := newTestArchiveStore(t, storePath)
	for _, id := range []string{"flush_a", "flush_b", "flush_c"} {
		if _, ok := reloaded.Get(id); !ok {
			t.Fatalf("expected %s to survive a sibling's FlushMatch", id)
		}
	}
	entry, _ := reloaded.Get("flush_b")
	if entry.Status != "finished" || entry.Winner != "white" {
		t.Fatalf("expected the flushed row to carry its new state, got %#v", entry)
	}
}

// singleRowFakeStore is a backend whose persist() upserts only what it is
// given, like SQLite and Postgres. It records which write path FlushMatch took.
type singleRowFakeStore struct {
	*freshnessFakeStore
	upserted  []string
	persisted int
}

// slowUpserterFakeStore makes upsertOne take real time and records peak
// concurrency, so tests can prove concurrent FlushMatch calls overlap instead
// of serializing on the overlay lock.
type slowUpserterFakeStore struct {
	*freshnessFakeStore
	delay       time.Duration
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
	upserts     atomic.Int32
	onUpsert    func() // optional hook, runs after the delay
}

func (f *slowUpserterFakeStore) upsertOne(entry MatchArchiveEntry, _ *MatchArchivePrivateEntry) error {
	cur := f.inFlight.Add(1)
	for {
		max := f.maxInFlight.Load()
		if cur <= max || f.maxInFlight.CompareAndSwap(max, cur) {
			break
		}
	}
	time.Sleep(f.delay)
	f.inFlight.Add(-1)
	f.upserts.Add(1)
	if f.onUpsert != nil {
		f.onUpsert()
	}
	return nil
}

func (f *singleRowFakeStore) upsertOne(entry MatchArchiveEntry, _ *MatchArchivePrivateEntry) error {
	f.upserted = append(f.upserted, entry.MatchID)
	return nil
}

func (f *singleRowFakeStore) persist(map[string]MatchArchiveEntry, map[string]MatchArchivePrivateEntry) error {
	f.persisted++
	return nil
}

// FlushMatch must prefer the backend's single-row upsert when it has one:
// the whole point is that game creation stops rewriting the process-lifetime
// overlay. Backends without it keep the full-write fallback.
func TestFlushMatchPrefersSingleRowUpserter(t *testing.T) {
	fake := &singleRowFakeStore{freshnessFakeStore: &freshnessFakeStore{
		rows:     map[string]MatchArchiveEntry{},
		privates: map[string]MatchArchivePrivateEntry{},
	}}
	store, err := newMatchArchiveStore(fake)
	if err != nil {
		t.Fatalf("expected archive store to initialize, got %v", err)
	}
	defer func() { _ = store.Close() }()

	// Seed the overlay directly: Upsert would poke the background write loop
	// and make the persist counter nondeterministic.
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	store.mu.Lock()
	store.entries["flush_single"] = MatchArchiveEntry{
		MatchID:   "flush_single",
		Status:    "active",
		UpdatedAt: now,
	}
	store.dirty["flush_single"] = struct{}{}
	store.mu.Unlock()

	if err := store.FlushMatch("flush_single"); err != nil {
		t.Fatalf("expected FlushMatch to succeed, got %v", err)
	}
	if len(fake.upserted) != 1 || fake.upserted[0] != "flush_single" {
		t.Fatalf("expected exactly one single-row upsert for flush_single, got %v", fake.upserted)
	}
	if fake.persisted != 0 {
		t.Fatalf("expected FlushMatch to skip the full persist, got %d calls", fake.persisted)
	}
}

// FlushMatch on single-row backends must write OUTSIDE the overlay lock: a
// shared managed Postgres measured ~450ms per single-row upsert in production,
// and holding the global lock across it serialized every concurrent match
// creation behind one write at a time (a 909ms -> 9.7s burst-20 staircase).
func TestFlushMatchWritesConcurrentlyForUpserterBackends(t *testing.T) {
	fake := &slowUpserterFakeStore{
		freshnessFakeStore: &freshnessFakeStore{
			rows:     map[string]MatchArchiveEntry{},
			privates: map[string]MatchArchivePrivateEntry{},
		},
		delay: 80 * time.Millisecond,
	}
	store, err := newMatchArchiveStore(fake)
	if err != nil {
		t.Fatalf("expected archive store to initialize, got %v", err)
	}
	defer func() { _ = store.Close() }()

	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	store.mu.Lock()
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("burst_match_%d", i)
		store.entries[id] = MatchArchiveEntry{MatchID: id, Status: "active", UpdatedAt: now}
		store.gens[id] = 1
		store.dirty[id] = struct{}{}
	}
	store.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := store.FlushMatch(fmt.Sprintf("burst_match_%d", i)); err != nil {
				t.Errorf("expected FlushMatch %d to succeed, got %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	if got := fake.upserts.Load(); got != 6 {
		t.Fatalf("expected 6 single-row upserts, got %d", got)
	}
	if fake.maxInFlight.Load() < 2 {
		t.Fatalf("expected concurrent FlushMatch DB writes to overlap, peak in-flight was %d", fake.maxInFlight.Load())
	}
	store.mu.Lock()
	remaining := len(store.dirty)
	store.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("expected all flushed rows to leave the dirty set, %d remain", remaining)
	}
}

// If the row changes while its flush write is in flight, the flush must NOT
// clear the dirty flag: the newer version still needs to reach the backend.
func TestFlushMatchKeepsDirtyWhenRowChangesMidFlush(t *testing.T) {
	fake := &slowUpserterFakeStore{
		freshnessFakeStore: &freshnessFakeStore{
			rows:     map[string]MatchArchiveEntry{},
			privates: map[string]MatchArchivePrivateEntry{},
		},
		delay: 40 * time.Millisecond,
	}
	store, err := newMatchArchiveStore(fake)
	if err != nil {
		t.Fatalf("expected archive store to initialize, got %v", err)
	}
	defer func() { _ = store.Close() }()

	now := time.Date(2026, 10, 4, 12, 30, 0, 0, time.UTC)
	store.mu.Lock()
	store.entries["gen_guard"] = MatchArchiveEntry{MatchID: "gen_guard", Status: "active", UpdatedAt: now}
	store.gens["gen_guard"] = 7
	store.dirty["gen_guard"] = struct{}{}
	store.mu.Unlock()

	fake.onUpsert = func() {
		store.mu.Lock()
		store.entries["gen_guard"] = MatchArchiveEntry{MatchID: "gen_guard", Status: "finished", UpdatedAt: now.Add(time.Minute)}
		store.gens["gen_guard"] = 8
		store.mu.Unlock()
	}

	if err := store.FlushMatch("gen_guard"); err != nil {
		t.Fatalf("expected FlushMatch to succeed, got %v", err)
	}
	store.mu.Lock()
	_, stillDirty := store.dirty["gen_guard"]
	store.mu.Unlock()
	if !stillDirty {
		t.Fatal("expected dirty to remain after the row changed mid-flush")
	}
}

// The write loop's one-by-one drain for single-row backends must write every
// dirty row exactly once and clear only rows that did not change mid-flight.
func TestPersistDirtyOneByOneDrainsAndGuardsGenerations(t *testing.T) {
	fake := &slowUpserterFakeStore{
		freshnessFakeStore: &freshnessFakeStore{
			rows:     map[string]MatchArchiveEntry{},
			privates: map[string]MatchArchivePrivateEntry{},
		},
		delay: time.Millisecond,
	}
	store, err := newMatchArchiveStore(fake)
	if err != nil {
		t.Fatalf("expected archive store to initialize, got %v", err)
	}
	defer func() { _ = store.Close() }()

	now := time.Date(2026, 10, 4, 13, 0, 0, 0, time.UTC)
	store.mu.Lock()
	for _, id := range []string{"drain_a", "drain_b", "drain_c"} {
		store.entries[id] = MatchArchiveEntry{MatchID: id, Status: "active", UpdatedAt: now}
		store.gens[id] = 1
		store.dirty[id] = struct{}{}
	}
	store.mu.Unlock()

	store.persistDirtyOneByOne(fake)

	if got := fake.upserts.Load(); got != 3 {
		t.Fatalf("expected 3 upserts, got %d", got)
	}
	store.mu.Lock()
	remaining := len(store.dirty)
	store.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("expected the dirty set to drain, %d rows remain", remaining)
	}
}

func TestMatchArchiveStoreListByGuest(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "match-archive.json")
	store := newTestArchiveStore(t, storePath)

	base := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	snapshots := []contracts.MatchSnapshotResponse{
		{
			Match: contracts.MatchState{
				MatchID:      "guest_match_1",
				RulesVersion: "v1-alpha-foundation",
				WhiteGuestID: "guest_focus",
				BlackGuestID: "guest_other",
				CreatedAt:    base,
				UpdatedAt:    base.Add(2 * time.Minute),
			},
		},
		{
			Match: contracts.MatchState{
				MatchID:      "guest_match_2",
				RulesVersion: "v1-alpha-foundation",
				WhiteGuestID: "guest_else",
				BlackGuestID: "guest_focus",
				CreatedAt:    base,
				UpdatedAt:    base.Add(5 * time.Minute),
			},
		},
		{
			Match: contracts.MatchState{
				MatchID:      "guest_match_3",
				RulesVersion: "v1-alpha-foundation",
				WhiteGuestID: "guest_else",
				BlackGuestID: "guest_other",
				CreatedAt:    base,
				UpdatedAt:    base.Add(8 * time.Minute),
			},
		},
	}

	for _, snapshot := range snapshots {
		if err := store.Upsert(snapshot); err != nil {
			t.Fatalf("expected upsert to succeed, got %v", err)
		}
	}

	matches := store.ListByGuest("guest_focus", 10)
	if len(matches) != 2 {
		t.Fatalf("expected 2 guest matches, got %d", len(matches))
	}
	if matches[0].MatchID != "guest_match_2" || matches[1].MatchID != "guest_match_1" {
		t.Fatalf("expected guest matches sorted by recency, got %#v", matches)
	}
}

func TestMatchArchiveStoreListByAccountIncludesLinkedGuestFallback(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "match-archive.json")
	store := newTestArchiveStore(t, storePath)

	base := time.Date(2026, 5, 6, 12, 30, 0, 0, time.UTC)
	snapshots := []contracts.MatchSnapshotResponse{
		{
			Match: contracts.MatchState{
				MatchID:        "account_match_direct",
				RulesVersion:   "v1-alpha-foundation",
				WhiteGuestID:   "guest_focus",
				BlackGuestID:   "guest_other",
				WhiteAccountID: "acct_focus",
				CreatedAt:      base,
				UpdatedAt:      base.Add(2 * time.Minute),
			},
		},
		{
			Match: contracts.MatchState{
				MatchID:      "account_match_legacy",
				RulesVersion: "v1-alpha-foundation",
				WhiteGuestID: "guest_else",
				BlackGuestID: "guest_focus",
				CreatedAt:    base,
				UpdatedAt:    base.Add(5 * time.Minute),
			},
		},
		{
			Match: contracts.MatchState{
				MatchID:      "account_match_other",
				RulesVersion: "v1-alpha-foundation",
				WhiteGuestID: "guest_else",
				BlackGuestID: "guest_other",
				CreatedAt:    base,
				UpdatedAt:    base.Add(8 * time.Minute),
			},
		},
	}

	for _, snapshot := range snapshots {
		if err := store.Upsert(snapshot); err != nil {
			t.Fatalf("expected upsert to succeed, got %v", err)
		}
	}

	matches := store.ListByAccount("acct_focus", []string{"guest_focus"}, 10)
	if len(matches) != 2 {
		t.Fatalf("expected 2 account matches, got %d", len(matches))
	}
	if matches[0].MatchID != "account_match_legacy" || matches[1].MatchID != "account_match_direct" {
		t.Fatalf("expected account matches sorted by recency, got %#v", matches)
	}
}

func TestMatchArchiveStorePreservesReplayFrames(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "match-archive.json")
	store := newTestArchiveStore(t, storePath)

	now := time.Date(2026, 5, 6, 13, 0, 0, 0, time.UTC)
	snapshot := contracts.MatchSnapshotResponse{
		Match: contracts.MatchState{
			MatchID:      "replay_archive",
			RulesVersion: "v1-alpha-foundation",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		ReplayHead: 1,
		ReplayFrames: []contracts.ReplayFrame{
			{Index: 0, Turn: "white", Board: make([][]*contracts.Piece, 8)},
			{Index: 1, Turn: "black", Board: make([][]*contracts.Piece, 8), MoveHistory: []string{"e4"}},
		},
	}

	if err := store.Upsert(snapshot); err != nil {
		t.Fatalf("expected upsert to succeed, got %v", err)
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("expected archive flush to succeed, got %v", err)
	}

	reloaded := newTestArchiveStore(t, storePath)
	entry, ok := reloaded.Get("replay_archive")
	if !ok {
		t.Fatalf("expected replay archive entry to exist after reload")
	}
	if len(entry.Snapshot.ReplayFrames) != 2 || entry.Snapshot.ReplayFrames[1].Turn != "black" {
		t.Fatalf("expected replay frames to persist, got %#v", entry.Snapshot.ReplayFrames)
	}
}

func TestMatchArchiveStoreLoadMatchRestoresPrivateState(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "match-archive.json")
	store := newTestArchiveStore(t, storePath)

	now := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	snapshot := contracts.MatchSnapshotResponse{
		Match: contracts.MatchState{
			MatchID:           "private_restore",
			RulesVersion:      "v1-alpha-foundation",
			WhitePlayerSecret: "white-secret",
			BlackPlayerSecret: "black-secret",
			Status:            "active",
			Turn:              "black",
			Board:             make([][]*contracts.Piece, 8),
			Moved:             []string{"1-4"},
			MoveHistory:       []string{"e4"},
			CreatedAt:         now,
			UpdatedAt:         now,
			History: []contracts.PositionState{
				{Board: make([][]*contracts.Piece, 8), Turn: "white", MoveHistory: []string{}},
				{Board: make([][]*contracts.Piece, 8), Turn: "black", MoveHistory: []string{"e4"}},
			},
		},
		ReplayHead: 2,
		Events: []contracts.ResolvedEvent{
			{ID: "evt_1", MatchID: "private_restore", Type: "match_started", At: now, Payload: map[string]any{"turn": "white"}},
			{ID: "evt_2", MatchID: "private_restore", Type: "move_applied", At: now, Payload: map[string]any{"notation": "e4"}},
		},
	}
	for i := range snapshot.Match.Board {
		snapshot.Match.Board[i] = make([]*contracts.Piece, 8)
	}
	for i := range snapshot.Match.History {
		for row := range snapshot.Match.History[i].Board {
			snapshot.Match.History[i].Board[row] = make([]*contracts.Piece, 8)
		}
	}

	if err := store.Upsert(snapshot); err != nil {
		t.Fatalf("expected archive upsert to succeed, got %v", err)
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("expected archive flush to succeed, got %v", err)
	}

	reloaded := newTestArchiveStore(t, storePath)
	match, events, ok := reloaded.LoadMatch("private_restore")
	if !ok {
		t.Fatalf("expected private restore entry to be loadable")
	}
	if match.WhitePlayerSecret != "white-secret" || match.BlackPlayerSecret != "black-secret" {
		t.Fatalf("expected seat secrets to persist privately, got %#v", match)
	}
	if len(match.History) != 2 || len(events) != 2 {
		t.Fatalf("expected private history and events to persist, got history=%d events=%d", len(match.History), len(events))
	}
}

func TestMatchArchiveStoreStatsReflectQueuesAndStatuses(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "match-archive.json")
	store := newTestArchiveStore(t, storePath)

	now := time.Date(2026, 5, 6, 15, 0, 0, 0, time.UTC)
	snapshots := []contracts.MatchSnapshotResponse{
		{Match: contracts.MatchState{MatchID: "rated_finished", RulesVersion: "v1", Queue: "rated", Status: "finished", CreatedAt: now, UpdatedAt: now}},
		{Match: contracts.MatchState{MatchID: "casual_active", RulesVersion: "v1", Queue: "casual", Status: "active", CreatedAt: now, UpdatedAt: now}},
		{Match: contracts.MatchState{MatchID: "direct_active", RulesVersion: "v1", Status: "active", CreatedAt: now, UpdatedAt: now}},
	}
	for _, snapshot := range snapshots {
		if err := store.Upsert(snapshot); err != nil {
			t.Fatalf("expected archive upsert to succeed, got %v", err)
		}
	}

	stats := store.Stats()
	if stats.TotalMatches != 3 || stats.FinishedMatches != 1 || stats.ActiveMatches != 2 {
		t.Fatalf("unexpected archive status counts %#v", stats)
	}
	if stats.RatedMatches != 1 || stats.CasualMatches != 1 || stats.DirectMatches != 1 {
		t.Fatalf("unexpected archive queue counts %#v", stats)
	}
}
