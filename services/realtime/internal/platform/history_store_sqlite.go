package platform

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type sqliteArchiveStore struct {
	db *sql.DB
}

func newSQLiteArchiveStore(path string) (*sqliteArchiveStore, error) {
	if path != "" && path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	store := &sqliteArchiveStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *sqliteArchiveStore) backend() string {
	return "sqlite"
}

func (s *sqliteArchiveStore) load() (map[string]MatchArchiveEntry, map[string]MatchArchivePrivateEntry, error) {
	rows, err := s.db.Query(`
		select match_id, entry_json, private_json
		from archives
	`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	entries := make(map[string]MatchArchiveEntry)
	private := make(map[string]MatchArchivePrivateEntry)

	for rows.Next() {
		var (
			matchID     string
			entryJSON   string
			privateJSON sql.NullString
		)
		if err := rows.Scan(&matchID, &entryJSON, &privateJSON); err != nil {
			return nil, nil, err
		}
		var entry MatchArchiveEntry
		if err := json.Unmarshal([]byte(entryJSON), &entry); err != nil {
			return nil, nil, err
		}
		entries[matchID] = entry

		if privateJSON.Valid && privateJSON.String != "" {
			var privateEntry MatchArchivePrivateEntry
			if err := json.Unmarshal([]byte(privateJSON.String), &privateEntry); err != nil {
				return nil, nil, err
			}
			private[matchID] = privateEntry
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	return entries, private, nil
}

const sqliteArchiveUpsertSQL = `
	insert into archives(match_id, entry_json, private_json)
	values(?, ?, ?)
	on conflict(match_id) do update set
		entry_json = excluded.entry_json,
		private_json = excluded.private_json
`

// sqliteArchiveUpsertArgs builds the arguments shared by the batch persist
// and the single-row upsert so the two write paths cannot drift apart.
func sqliteArchiveUpsertArgs(matchID string, entry MatchArchiveEntry, private *MatchArchivePrivateEntry) ([]any, error) {
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}

	var privateJSON any
	if private != nil {
		encodedPrivate, err := json.Marshal(*private)
		if err != nil {
			return nil, err
		}
		privateJSON = string(encodedPrivate)
	}

	return []any{matchID, string(entryJSON), privateJSON}, nil
}

func (s *sqliteArchiveStore) persist(entries map[string]MatchArchiveEntry, private map[string]MatchArchivePrivateEntry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	upsertStmt, err := tx.Prepare(sqliteArchiveUpsertSQL)
	if err != nil {
		return err
	}
	defer upsertStmt.Close()

	for matchID, entry := range entries {
		args, err := sqliteArchiveUpsertArgs(matchID, entry, privateEntryPtr(private, matchID))
		if err != nil {
			return err
		}
		if _, err := upsertStmt.Exec(args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// upsertOne writes exactly one row, so FlushMatch can make a new match
// durable without rewriting the other rows in the overlay.
func (s *sqliteArchiveStore) upsertOne(entry MatchArchiveEntry, private *MatchArchivePrivateEntry) error {
	args, err := sqliteArchiveUpsertArgs(entry.MatchID, entry, private)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(sqliteArchiveUpsertSQL, args...)
	return err
}

func (s *sqliteArchiveStore) delete(matchID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`delete from archives where match_id = ?`, matchID)
	return err
}

func (s *sqliteArchiveStore) close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// query* methods for the SQLite backend are not implemented — the store falls
// back to the in-memory map populated by load().

func (s *sqliteArchiveStore) queryGet(_ string) (MatchArchiveEntry, bool, error) {
	return MatchArchiveEntry{}, false, nil
}

func (s *sqliteArchiveStore) queryPrivate(_ string) (MatchArchivePrivateEntry, bool, error) {
	return MatchArchivePrivateEntry{}, false, nil
}

func (s *sqliteArchiveStore) queryList(_, _ int) ([]MatchArchiveEntry, error) {
	return nil, nil
}

func (s *sqliteArchiveStore) queryUnfinishedIDs(_ int) ([]string, error) {
	return nil, nil
}

func (s *sqliteArchiveStore) queryFinishedIDs(_ int) ([]string, error) {
	return nil, nil
}

func (s *sqliteArchiveStore) queryByGuest(_ string, _, _ int) ([]MatchArchiveEntry, error) {
	return nil, nil
}

func (s *sqliteArchiveStore) queryByAccount(_ string, _ []string, _, _ int) ([]MatchArchiveEntry, error) {
	return nil, nil
}

func (s *sqliteArchiveStore) queryStats() (MatchArchiveStats, error) {
	return MatchArchiveStats{}, nil
}

func (s *sqliteArchiveStore) init() error {
	_, _ = s.db.Exec(`PRAGMA journal_mode=WAL`)
	_, _ = s.db.Exec(`PRAGMA busy_timeout=5000`)
	_, err := s.db.Exec(`
		create table if not exists archives (
			match_id text primary key,
			entry_json text not null,
			private_json text
		)
	`)
	return err
}
