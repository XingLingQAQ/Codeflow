package floweng

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteFlowStore stores each Flow as a JSON document keyed by id.
type SQLiteFlowStore struct {
	db *sql.DB
}

// NewSQLiteFlowStore opens (or creates) a SQLite database at dbPath.
// Pass ":memory:" for ephemeral tests (shared cache).
func NewSQLiteFlowStore(dbPath string) (*SQLiteFlowStore, error) {
	conn, err := buildFlowSQLiteConnString(dbPath)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", conn)
	if err != nil {
		return nil, fmt.Errorf("open floweng db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite write serialization
	store := &SQLiteFlowStore{db: db}
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func buildFlowSQLiteConnString(dbPath string) (string, error) {
	if dbPath == "" || dbPath == ":memory:" {
		return "file:floweng_mem?mode=memory&cache=shared&_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", nil
	}
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create floweng db dir: %w", err)
		}
	}
	return fmt.Sprintf("file:%s?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", filepath.ToSlash(dbPath)), nil
}

func (s *SQLiteFlowStore) initSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS flows (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  template_id TEXT NOT NULL,
  status TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_flows_project ON flows(project_id);
CREATE INDEX IF NOT EXISTS idx_flows_status ON flows(status);
`)
	if err != nil {
		return fmt.Errorf("init floweng schema: %w", err)
	}
	return nil
}

// Close closes the underlying database.
func (s *SQLiteFlowStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Put upserts a flow document.
func (s *SQLiteFlowStore) Put(flow *Flow) error {
	if flow == nil || flow.ID == "" {
		return fmt.Errorf("flow id is required")
	}
	payload, err := json.Marshal(flow)
	if err != nil {
		return fmt.Errorf("marshal flow: %w", err)
	}
	_, err = s.db.Exec(`
INSERT INTO flows (id, project_id, template_id, status, payload, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  project_id=excluded.project_id,
  template_id=excluded.template_id,
  status=excluded.status,
  payload=excluded.payload,
  updated_at=excluded.updated_at
`, flow.ID, flow.ProjectID, string(flow.TemplateID), string(flow.Status), string(payload),
		flow.CreatedAt.UTC().UnixMilli(), flow.UpdatedAt.UTC().UnixMilli())
	if err != nil {
		return fmt.Errorf("put flow: %w", err)
	}
	return nil
}

// Get loads a flow by id.
func (s *SQLiteFlowStore) Get(id string) (*Flow, error) {
	var payload string
	err := s.db.QueryRow(`SELECT payload FROM flows WHERE id = ?`, id).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("flow not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get flow: %w", err)
	}
	var flow Flow
	if err := json.Unmarshal([]byte(payload), &flow); err != nil {
		return nil, fmt.Errorf("unmarshal flow: %w", err)
	}
	return cloneFlow(&flow), nil
}

// List returns flows, optionally filtered by projectID.
func (s *SQLiteFlowStore) List(projectID string) ([]*Flow, error) {
	var rows *sql.Rows
	var err error
	if projectID == "" {
		rows, err = s.db.Query(`SELECT payload FROM flows ORDER BY updated_at DESC`)
	} else {
		rows, err = s.db.Query(`SELECT payload FROM flows WHERE project_id = ? ORDER BY updated_at DESC`, projectID)
	}
	if err != nil {
		return nil, fmt.Errorf("list flows: %w", err)
	}
	defer rows.Close()

	out := make([]*Flow, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var flow Flow
		if err := json.Unmarshal([]byte(payload), &flow); err != nil {
			return nil, fmt.Errorf("unmarshal flow: %w", err)
		}
		out = append(out, cloneFlow(&flow))
	}
	return out, rows.Err()
}

// Delete removes a flow by id.
func (s *SQLiteFlowStore) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM flows WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete flow: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("flow not found: %s", id)
	}
	return nil
}
