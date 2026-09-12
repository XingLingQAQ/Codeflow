package debate

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/codeflow/backend/internal/dbx"
)

// sqliteDebateStore persists Debate JSON documents keyed by id.
type sqliteDebateStore struct {
	db *sql.DB
}

func openSQLiteDebateStore(dbPath string) (*sqliteDebateStore, error) {
	if err := prepareDebateDBDir(dbPath); err != nil {
		return nil, err
	}
	db, err := dbx.Open(dbPath, dbx.WithMaxOpenConns(1)) // SQLite write serialization
	if err != nil {
		return nil, fmt.Errorf("open debate db: %w", err)
	}
	s := &sqliteDebateStore{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func prepareDebateDBDir(dbPath string) error {
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create debate db dir: %w", err)
		}
	}
	return nil
}

func (s *sqliteDebateStore) initSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS debates (
  id TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  flow_id TEXT NOT NULL,
  stage_id TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_debates_status ON debates(status);
CREATE INDEX IF NOT EXISTS idx_debates_flow ON debates(flow_id);
`)
	if err != nil {
		return fmt.Errorf("init debate schema: %w", err)
	}
	return nil
}

// Close closes the underlying database.
func (s *sqliteDebateStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// put upserts a debate document.
func (s *sqliteDebateStore) put(d *Debate) error {
	if d == nil || d.ID == "" {
		return fmt.Errorf("debate id is required")
	}
	payload, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("marshal debate: %w", err)
	}
	_, err = s.db.Exec(`
INSERT INTO debates (id, status, flow_id, stage_id, created_at, updated_at, payload)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  status=excluded.status,
  flow_id=excluded.flow_id,
  stage_id=excluded.stage_id,
  updated_at=excluded.updated_at,
  payload=excluded.payload
`, d.ID, string(d.Status), d.FlowID, d.StageID, d.CreatedAt, d.UpdatedAt, string(payload))
	if err != nil {
		return fmt.Errorf("put debate: %w", err)
	}
	return nil
}

// delete removes a debate by id.
func (s *sqliteDebateStore) delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM debates WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete debate: %w", err)
	}
	return nil
}

// loadAll returns every stored debate. Rows whose payload fails to decode are
// skipped so a single corrupted document cannot block startup.
func (s *sqliteDebateStore) loadAll() ([]*Debate, error) {
	rows, err := s.db.Query(`SELECT payload FROM debates`)
	if err != nil {
		return nil, fmt.Errorf("load debates: %w", err)
	}
	defer rows.Close()
	out := make([]*Debate, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var d Debate
		if err := json.Unmarshal([]byte(payload), &d); err != nil {
			continue
		}
		out = append(out, cloneDebate(&d))
	}
	return out, rows.Err()
}
