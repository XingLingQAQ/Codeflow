package guard

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codeflow/backend/internal/dbx"
)

// sqliteExemptionStore persists temporary path exemptions across restarts.
type sqliteExemptionStore struct {
	db *sql.DB
}

func openSQLiteExemptionStore(dbPath string) (*sqliteExemptionStore, error) {
	if err := prepareExemptionDBDir(dbPath); err != nil {
		return nil, err
	}
	db, err := dbx.Open(dbPath, dbx.WithMaxOpenConns(1))
	if err != nil {
		return nil, fmt.Errorf("open guard exemption db: %w", err)
	}
	s := &sqliteExemptionStore{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func prepareExemptionDBDir(dbPath string) error {
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create guard exemption db dir: %w", err)
		}
	}
	return nil
}

func (s *sqliteExemptionStore) initSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS exemptions (
  path TEXT PRIMARY KEY,
  expires_at INTEGER NOT NULL,
  payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_exemptions_expires ON exemptions(expires_at);
CREATE TABLE IF NOT EXISTS exemption_requests (
  id TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_exreq_status ON exemption_requests(status);
`)
	if err != nil {
		return fmt.Errorf("init guard exemption schema: %w", err)
	}
	return nil
}

func (s *sqliteExemptionStore) putRequest(req ExemptionRequest) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("exemption store not open")
	}
	return putExemptionRequest(s.db, req)
}

type exemptionStoreExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func putExemptionRequest(exec exemptionStoreExecer, req ExemptionRequest) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = exec.Exec(`
INSERT INTO exemption_requests (id, status, payload, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  status=excluded.status,
  payload=excluded.payload
`, req.ID, string(req.Status), string(payload), req.CreatedAt.UTC().UnixMilli())
	return err
}

func (s *sqliteExemptionStore) loadAllRequests() ([]ExemptionRequest, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT payload FROM exemption_requests ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ExemptionRequest, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var req ExemptionRequest
		if err := json.Unmarshal([]byte(payload), &req); err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, rows.Err()
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
	return putExemption(s.db, ex)
}

func putExemption(exec exemptionStoreExecer, ex Exemption) error {
	if ex.Path == "" {
		return fmt.Errorf("exemption path required")
	}
	payload, err := json.Marshal(ex)
	if err != nil {
		return err
	}
	_, err = exec.Exec(`
INSERT INTO exemptions (path, expires_at, payload)
VALUES (?, ?, ?)
ON CONFLICT(path) DO UPDATE SET
  expires_at=excluded.expires_at,
  payload=excluded.payload
`, ex.Path, ex.ExpiresAt.UTC().UnixMilli(), string(payload))
	return err
}

// approveRequest atomically persists the active exemption and the approved
// request. Neither row is visible unless both writes succeed.
func (s *sqliteExemptionStore) approveRequest(req ExemptionRequest, ex Exemption) (err error) {
	if s == nil || s.db == nil {
		return fmt.Errorf("exemption store not open")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = putExemption(tx, ex); err != nil {
		return fmt.Errorf("persist approved exemption: %w", err)
	}
	if err = putExemptionRequest(tx, req); err != nil {
		return fmt.Errorf("persist approved exemption request: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit approved exemption request: %w", err)
	}
	return nil
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
