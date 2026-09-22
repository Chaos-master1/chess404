package platform

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

// Regression tests for deterministic MatchArchiveStore shutdown.
//
// Root cause: Close() signalled the writeLoop but never joined it. A deferred
// t.TempDir() removal could therefore race the loop's final SQLite/WAL work,
// producing the sporadic "TempDir cleanup failed" failures seen on CI-style
// runs of the platform-service packages.

func archiveSnapshotFor(matchID string, now time.Time) contracts.MatchSnapshotResponse {
	return contracts.MatchSnapshotResponse{
		Match: contracts.MatchState{
			MatchID:      matchID,
			RulesVersion: "v1-alpha-foundation",
			Status:       "finished",
			Winner:       "white",
			MoveHistory:  []string{"e4", "e5"},
			CreatedAt:    now,
			UpdatedAt:    now.Add(time.Minute),
		},
		ReplayHead: 2,
	}
}

// CloseTwiceMustNotDeadlockOrPanic guards the join-once semantics: the second
// Close waits on loopDone, which only the writeLoop closes. A double Close
// (explicit call plus test defer, or two defers) must complete rather than
// deadlock or re-close the channel.
func TestArchiveStoreCloseTwiceMustNotDeadlockOrPanic(t *testing.T) {
	store, err := NewSQLiteMatchArchiveStore(filepath.Join(t.TempDir(), "a.sqlite"))
	if err != nil {
		t.Fatalf("expected store to initialize, got %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = store.Close()
		_ = store.Close()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("double Close deadlocked or panicked")
	}
}

// CloseJoinsBackgroundWriter proves the ordering contract: Close must not
// return while a writeLoop persist can still be in flight. It swaps the
// store's persistence for a slow fake, fires a background persist, then
// requires that Close only returns after the in-flight persist completed.
func TestArchiveStoreCloseJoinsBackgroundWriter(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSQLiteMatchArchiveStore(filepath.Join(dir, "a.sqlite"))
	if err != nil {
		t.Fatalf("expected store to initialize, got %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	if err := store.Upsert(archiveSnapshotFor("close_join_probe", now)); err != nil {
		t.Fatalf("expected upsert to succeed, got %v", err)
	}

	// Swap in a slow fake so a persist in flight is observable.
	persistStarted := make(chan struct{})
	persistDone := make(chan struct{})
	slow := &blockingPersistBackend{delegate: store.store, started: persistStarted, done: persistDone}
	store.mu.Lock()
	store.store = slow
	store.mu.Unlock()

	// Schedule a background persist, wait until it is genuinely inside the
	// slow persist call, then let Close proceed against it.
	go func() {
		_ = store.Flush()
	}()
	<-persistStarted

	closeErr := make(chan error, 1)
	go func() {
		closeErr <- store.Close()
	}()

	// Give Close a moment; it must block until the in-flight persist
	// finishes. Then release the persist and require Close to complete.
	select {
	case err := <-closeErr:
		t.Fatalf("Close returned while a persist was still in flight: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(persistDone)
	select {
	case err := <-closeErr:
		if err != nil {
			t.Fatalf("expected Close to succeed, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close never returned after the in-flight persist completed")
	}
	<-persistDone // Close joined: this is already closed iff ordering held.
}

// blockingPersistBackend wraps a real backend and blocks every persist until
// done is closed, exposing the in-flight window to the ordering test.
type blockingPersistBackend struct {
	delegate archivePersistence
	started  chan struct{}
	done     chan struct{}
	once     sync.Once
}

func (b *blockingPersistBackend) persist(entries map[string]MatchArchiveEntry, private map[string]MatchArchivePrivateEntry) error {
	b.once.Do(func() { close(b.started) })
	<-b.done
	return b.delegate.persist(entries, private)
}

func (b *blockingPersistBackend) backend() string { return b.delegate.backend() }

func (b *blockingPersistBackend) load() (map[string]MatchArchiveEntry, map[string]MatchArchivePrivateEntry, error) {
	return b.delegate.load()
}

func (b *blockingPersistBackend) queryGet(matchID string) (MatchArchiveEntry, bool, error) {
	return b.delegate.queryGet(matchID)
}

func (b *blockingPersistBackend) queryPrivate(matchID string) (MatchArchivePrivateEntry, bool, error) {
	return b.delegate.queryPrivate(matchID)
}

func (b *blockingPersistBackend) queryList(limit, offset int) ([]MatchArchiveEntry, error) {
	return b.delegate.queryList(limit, offset)
}

func (b *blockingPersistBackend) queryUnfinishedIDs(limit int) ([]string, error) {
	return b.delegate.queryUnfinishedIDs(limit)
}

func (b *blockingPersistBackend) queryFinishedIDs(limit int) ([]string, error) {
	return b.delegate.queryFinishedIDs(limit)
}

func (b *blockingPersistBackend) queryByGuest(guestID string, limit, offset int) ([]MatchArchiveEntry, error) {
	return b.delegate.queryByGuest(guestID, limit, offset)
}

func (b *blockingPersistBackend) queryByAccount(accountID string, linkedGuestIDs []string, limit, offset int) ([]MatchArchiveEntry, error) {
	return b.delegate.queryByAccount(accountID, linkedGuestIDs, limit, offset)
}

func (b *blockingPersistBackend) queryStats() (MatchArchiveStats, error) {
	return b.delegate.queryStats()
}

func (b *blockingPersistBackend) close() error { return b.delegate.close() }

// CloseThenTempDirCleanupIsDeterministic is the end-user-visible property:
// Upsert (schedules the async write), Close, then let t.TempDir remove the
// directory — the removal must always succeed because Close joined the
// writer. Runs under -race in the suite.
func TestArchiveStoreCloseThenTempDirCleanupIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "a.sqlite")
	store, err := NewSQLiteMatchArchiveStore(storePath)
	if err != nil {
		t.Fatalf("expected store to initialize, got %v", err)
	}
	now := time.Date(2026, 9, 22, 12, 30, 0, 0, time.UTC)
	if err := store.Upsert(archiveSnapshotFor("close_cleanup_probe", now)); err != nil {
		t.Fatalf("expected upsert to succeed, got %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("expected close to succeed, got %v", err)
	}
	// t.TempDir cleanup now races nothing: the writer is joined. Reload from
	// the same file to prove the final persist landed before Close returned.
	reloaded, err := NewSQLiteMatchArchiveStore(storePath)
	if err != nil {
		t.Fatalf("expected reload to succeed, got %v", err)
	}
	defer func() { _ = reloaded.Close() }()
	if entry, ok := reloaded.Get("close_cleanup_probe"); !ok || entry.MoveCount != 2 {
		t.Fatalf("expected persisted entry to survive close+reload, got ok=%v entry=%#v", ok, entry)
	}
}
