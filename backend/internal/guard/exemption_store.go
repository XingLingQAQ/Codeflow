package guard

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// sqliteExemptionStore persists temporary path exemptions across restarts.
type sqliteExemptionStore struct {
	db *sql.DB
}

func openSQLiteExemptionStore(dbPath string) (*sqliteExemptionStore, error) {
	conn, err := buildExemptionSQLiteConnString(dbPath)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", conn)
	if err != nil {
		return nil, fmt.Errorf("open guard exemption db: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &sqliteExemptionStore{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func buildExemptionSQLiteConnString(dbPath string) (string, error) {
	if dbPath == "" || dbPath == ":memory:" {
		return "file:guard_exemptions_mem?mode=memory&cache=shared&_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", nil
	}
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create guard exemption db dir: %w", err)
		}
	}
	return fmt.Sprintf("file:%s?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", filepath.ToSlash(dbPath)), nil
}

func (s *sqliteExemptionStore) initSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS exemptions (
  path TEXT PRIMARY KEY,
  expires_at INTEGER NOT NULL,
  payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_exemptions_expires ON exemptions(expires_at);
`)
	if err != nil {
		return fmt.Errorf("init guard exemption schema: %w", err)
	}
	return nil
}

func (s *sqliteExemptionStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *sqliteExemptionStore) put(ex Exemption) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("exemption store not open")
	}
	if ex.Path == "" {
		return fmt.Errorf("exemption path required")
	}
	payload, err := json.Marshal(ex)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
INSERT INTO exemptions (path, expires_at, payload)
VALUES (?, ?, ?)
ON CONFLICT(path) DO UPDATE SET
  expires_at=excluded.expires_at,
  payload=excluded.payload
`, ex.Path, ex.ExpiresAt.UTC().UnixMilli(), string(payload))
	return err
}

func (s *sqliteExemptionStore) delete(path string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM exemptions WHERE path = ?`, path)
	return err
}

func (s *sqliteExemptionStore) loadActive(now time.Time) ([]Exemption, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	// Drop expired rows opportunistically so the table stays small.
	if _, err := s.db.Exec(`DELETE FROM exemptions WHERE expires_at > 0 AND expires_at < ?`, now.UTC().UnixMilli()); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT payload FROM exemptions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Exemption, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var ex Exemption
		if err := json.Unmarshal([]byte(payload), &ex); err != nil {
			return nil, err
		}
		out = append(out, ex)
	}
	return out, rows.Err()
}
