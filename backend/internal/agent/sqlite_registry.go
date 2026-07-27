package agent

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// sqliteAgentStore persists AgentAsset JSON documents.
type sqliteAgentStore struct {
	db *sql.DB
}

func openSQLiteAgentStore(dbPath string) (*sqliteAgentStore, error) {
	conn, err := buildAgentSQLiteConnString(dbPath)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", conn)
	if err != nil {
		return nil, fmt.Errorf("open agent db: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &sqliteAgentStore{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func buildAgentSQLiteConnString(dbPath string) (string, error) {
	if dbPath == "" || dbPath == ":memory:" {
		return "file:agent_mem?mode=memory&cache=shared&_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", nil
	}
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create agent db dir: %w", err)
		}
	}
	return fmt.Sprintf("file:%s?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", filepath.ToSlash(dbPath)), nil
}

func (s *sqliteAgentStore) initSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS agents (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  role_base TEXT NOT NULL,
  source TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  payload TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agents_name ON agents(name);
CREATE INDEX IF NOT EXISTS idx_agents_role ON agents(role_base);
`)
	if err != nil {
		return fmt.Errorf("init agent schema: %w", err)
	}
	return nil
}

func (s *sqliteAgentStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *sqliteAgentStore) put(a *AgentAsset) error {
	if a == nil || a.ID == "" {
		return fmt.Errorf("agent id required")
	}
	payload, err := json.Marshal(a)
	if err != nil {
		return err
	}
	enabled := 0
	if a.Enabled {
		enabled = 1
	}
	_, err = s.db.Exec(`
INSERT INTO agents (id, name, role_base, source, enabled, payload, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name,
  role_base=excluded.role_base,
  source=excluded.source,
  enabled=excluded.enabled,
  payload=excluded.payload,
  updated_at=excluded.updated_at
`, a.ID, a.Name, string(a.RoleBase), string(a.Source), enabled, string(payload), a.UpdatedAt.UTC().UnixMilli())
	return err
}

func (s *sqliteAgentStore) delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM agents WHERE id = ?`, id)
	return err
}

func (s *sqliteAgentStore) loadAll() ([]*AgentAsset, error) {
	rows, err := s.db.Query(`SELECT payload FROM agents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*AgentAsset, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var a AgentAsset
		if err := json.Unmarshal([]byte(payload), &a); err != nil {
			return nil, err
		}
		out = append(out, cloneAgent(&a))
	}
	return out, rows.Err()
}

// NewSQLiteAgentRegistry opens a durable agent registry at dbPath.
// Loads existing rows; seeds builtins only when the database is empty.
func NewSQLiteAgentRegistry(dbPath string) (*InMemoryAgentRegistry, error) {
	store, err := openSQLiteAgentStore(dbPath)
	if err != nil {
		return nil, err
	}
	r := &InMemoryAgentRegistry{agents: make(map[string]*AgentAsset), store: store}
	loaded, err := store.loadAll()
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	for _, a := range loaded {
		r.agents[a.ID] = a
	}
	if len(r.agents) == 0 {
		r.seedBuiltins()
		for _, a := range r.agents {
			if err := store.put(a); err != nil {
				_ = store.Close()
				return nil, err
			}
		}
	}
	return r, nil
}
