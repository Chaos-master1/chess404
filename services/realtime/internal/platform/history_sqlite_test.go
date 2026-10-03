package platform

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/chess404/realtime/internal/contracts"
)

func TestSQLiteMatchArchiveStoreUpsertAndReload(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "match-archive.sqlite")
	store, err := NewSQLiteMatchArchiveStore(storePath)
	if err != nil {
		t.Fatalf("expected sqlite archive store to initialize, got %v", err)
	}
	defer func() { _ = store.Close() }()

	now := time.Date(2026, 5, 6, 18, 0, 0, 0, time.UTC)
	snapshot := contracts.MatchSnapshotResponse{
		Match: contracts.MatchState{
			MatchID:      "sqlite_archive_test",
			RulesVersion: "v1-alpha-foundation",
			Status:       "finished",
			Winner:       "white",
			MoveHistory:  []string{"e4", "e5"},
			CreatedAt:    now,
			UpdatedAt:    now.Add(time.Minute),
		},
		ReplayHead: 2,
	}
	if err := store.Upsert(snapshot); err != nil {
		t.Fatalf("expected sqlite archive upsert to succeed, got %v", err)
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("expected sqlite archive flush to succeed, got %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("expected sqlite archive close to succeed, got %v", err)
	}

	reloaded, err := NewSQLiteMatchArchiveStore(storePath)
	if err != nil {
		t.Fatalf("expected sqlite archive store reload to succeed, got %v", err)
	}
	defer func() { _ = reloaded.Close() }()

	entry, ok := reloaded.Get("sqlite_archive_test")
	if !ok {
		t.Fatalf("expected sqlite archive entry to be reloadable")
	}
	if entry.MatchID != "sqlite_archive_test" || entry.MoveCount != 2 || entry.LastMove != "e5" || entry.Winner != "white" {
		t.Fatalf("unexpected sqlite archive entry %#v", entry)
	}
	if reloaded.Backend() != "sqlite" {
		t.Fatalf("expected sqlite archive backend, got %s", reloaded.Backend())
	}
}

func TestSQLiteMatchArchiveStoreLoadMatchRestoresPrivateState(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "match-archive.sqlite")
	store, err := NewSQLiteMatchArchiveStore(storePath)
	if err != nil {
		t.Fatalf("expected sqlite archive store to initialize, got %v", err)
	}
	defer func() { _ = store.Close() }()

	now := time.Date(2026, 5, 6, 18, 30, 0, 0, time.UTC)
	snapshot := contracts.MatchSnapshotResponse{
		Match: contracts.MatchState{
			MatchID:           "sqlite_private_restore",
			RulesVersion:      "v1-alpha-foundation",
			WhitePlayerSecret: "white-secret",
			BlackPlayerSecret: "black-secret",
			Status:            "active",
			Turn:              "black",
			Board:             make([][]*contracts.Piece, 8),
			MoveHistory:       []string{"e4"},
			CreatedAt:         now,
			UpdatedAt:         now,
			History: []contracts.PositionState{
				{Board: make([][]*contracts.Piece, 8), Turn: "white", MoveHistory: []string{}},
				{Board: make([][]*contracts.Piece, 8), Turn: "black", MoveHistory: []string{"e4"}},
			},
		},
		Events: []contracts.ResolvedEvent{
			{ID: "evt_1", MatchID: "sqlite_private_restore", Type: "match_started", At: now, Payload: map[string]any{"turn": "white"}},
			{ID: "evt_2", MatchID: "sqlite_private_restore", Type: "move_applied", At: now, Payload: map[string]any{"notation": "e4"}},
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
		t.Fatalf("expected sqlite archive upsert to succeed, got %v", err)
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("expected sqlite archive flush to succeed, got %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("expected sqlite archive close to succeed, got %v", err)
	}

	reloaded, err := NewSQLiteMatchArchiveStore(storePath)
	if err != nil {
		t.Fatalf("expected sqlite archive store reload to succeed, got %v", err)
	}
	defer func() { _ = reloaded.Close() }()

	match, events, ok := reloaded.LoadMatch("sqlite_private_restore")
	if !ok {
		t.Fatalf("expected sqlite private restore entry to be loadable")
	}
	if match.WhitePlayerSecret != "white-secret" || match.BlackPlayerSecret != "black-secret" {
		t.Fatalf("expected sqlite private secrets to persist, got %#v", match)
	}
	if len(match.History) != 2 || len(events) != 2 {
		t.Fatalf("expected sqlite private history and events to persist, got history=%d events=%d", len(match.History), len(events))
	}
}

// FlushMatch must make exactly the one match durable through the backend's
// single-row upsert, and it must not wait for Close or the background write
// loop: the create/join responses hand out claim tokens whose refresh reads
// the archive row, and the loop could lose that race.
func TestSQLiteMatchArchiveStoreFlushMatchIsDurableWithoutClose(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "match-archive.sqlite")

	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	seed, err := NewSQLiteMatchArchiveStore(storePath)
	if err != nil {
		t.Fatalf("expected sqlite archive store to initialize, got %v", err)
	}
	for i, id := range []string{"sqlite_flush_a", "sqlite_flush_b"} {
		if err := seed.Upsert(contracts.MatchSnapshotResponse{Match: contracts.MatchState{
			MatchID:      id,
			RulesVersion: "v1-alpha-foundation",
			Status:       "active",
			CreatedAt:    now,
			UpdatedAt:    now.Add(time.Duration(i) * time.Minute),
		}}); err != nil {
			t.Fatalf("expected seed upsert of %s to succeed, got %v", id, err)
		}
	}
	if err := seed.Flush(); err != nil {
		t.Fatalf("expected seed flush to succeed, got %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("expected seed close to succeed, got %v", err)
	}

	store, err := NewSQLiteMatchArchiveStore(storePath)
	if err != nil {
		t.Fatalf("expected sqlite archive store to initialize, got %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.Upsert(contracts.MatchSnapshotResponse{Match: contracts.MatchState{
		MatchID:      "sqlite_flush_b",
		RulesVersion: "v1-alpha-foundation",
		Status:       "finished",
		Winner:       "black",
		CreatedAt:    now,
		UpdatedAt:    now.Add(5 * time.Minute),
	}}); err != nil {
		t.Fatalf("expected dirty upsert to succeed, got %v", err)
	}
	if err := store.FlushMatch("sqlite_flush_b"); err != nil {
		t.Fatalf("expected FlushMatch to succeed, got %v", err)
	}

	// A second connection reads the committed row: durability must not have
	// waited for Close or the background loop.
	reader, err := NewSQLiteMatchArchiveStore(storePath)
	if err != nil {
		t.Fatalf("expected sqlite reader to open, got %v", err)
	}
	defer func() { _ = reader.Close() }()

	entry, ok := reader.Get("sqlite_flush_b")
	if !ok {
		t.Fatal("expected FlushMatch row to be durable before Close")
	}
	if entry.Status != "finished" || entry.Winner != "black" {
		t.Fatalf("expected flushed row to carry its new state, got %#v", entry)
	}
	if _, ok := reader.Get("sqlite_flush_a"); !ok {
		t.Fatal("expected sibling row to survive the single-row flush")
	}
}
