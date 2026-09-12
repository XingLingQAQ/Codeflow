package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codeflow/backend/internal/dbx"
)

// agentRuntimeStore is deliberately small so the normal in-memory service can
// remain the explicit test fake while production uses the same behavior with
// an atomic SQLite state snapshot.
type agentRuntimeStore interface {
	Load(context.Context) (map[string]*Agent, map[string][]*AgentLog, map[string]*Conversation, map[string]*CallTrace, error)
	Save(context.Context, map[string]*Agent, map[string][]*AgentLog, map[string]*Conversation, map[string]*CallTrace) error
	Close() error
}

type sqliteAgentRuntimeStore struct{ db *sql.DB }

type agentRuntimeState struct {
	Agents        map[string]*Agent         `json:"agents"`
	Logs          map[string][]*AgentLog    `json:"logs"`
	Conversations map[string]*Conversation  `json:"conversations"`
	Traces        map[string]*CallTrace     `json:"traces"`
}

// NewSQLiteAgentService creates the durable conversation/trace service. The
// database may be shared with SessionStorage, which keeps session, message,
// conversation and trace records in one durable data boundary.
func NewSQLiteAgentService(dbPath string) (*InMemoryAgentService, error) {
	store, err := openSQLiteAgentRuntimeStore(dbPath)
	if err != nil {
		return nil, err
	}
	agents, logs, conversations, traces, err := store.Load(context.Background())
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	svc := NewInMemoryAgentService()
	svc.store = store
	svc.agents = agents
	svc.logs = logs
	svc.conversations = conversations
	svc.traces = traces
	return svc, nil
}

func openSQLiteAgentRuntimeStore(dbPath string) (*sqliteAgentRuntimeStore, error) {
	if dbPath == "" {
		return nil, errors.New("agent runtime db path is required")
	}
	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return nil, fmt.Errorf("create agent runtime db dir: %w", err)
		}
	}
	db, err := dbx.Open(dbPath, dbx.WithSynchronous("NORMAL"))
	if err != nil {
		return nil, fmt.Errorf("open agent runtime db: %w", err)
	}
	store := &sqliteAgentRuntimeStore{db: db}
	if _, err := db.Exec(`
		PRAGMA foreign_keys = ON;
		CREATE TABLE IF NOT EXISTS agent_runtime_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			state_json TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		);`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init agent runtime schema: %w", err)
	}
	var quick string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&quick); err != nil || quick != "ok" {
		_ = db.Close()
		if err == nil {
			err = fmt.Errorf("quick_check returned %q", quick)
		}
		return nil, fmt.Errorf("agent runtime integrity check failed: %w", err)
	}
	return store, nil
}

func (s *sqliteAgentRuntimeStore) Load(ctx context.Context) (map[string]*Agent, map[string][]*AgentLog, map[string]*Conversation, map[string]*CallTrace, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, "SELECT state_json FROM agent_runtime_state WHERE id = 1").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return make(map[string]*Agent), make(map[string][]*AgentLog), make(map[string]*Conversation), make(map[string]*CallTrace), nil
	}
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load agent runtime state: %w", err)
	}
	var state agentRuntimeState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("decode agent runtime state: %w", err)
	}
	if state.Agents == nil { state.Agents = make(map[string]*Agent) }
	if state.Logs == nil { state.Logs = make(map[string][]*AgentLog) }
	if state.Conversations == nil { state.Conversations = make(map[string]*Conversation) }
	if state.Traces == nil { state.Traces = make(map[string]*CallTrace) }
	for _, conv := range state.Conversations {
		if conv != nil { conv.TraceRoot = rebindTraceTree(conv.TraceRoot, state.Traces) }
	}
	return state.Agents, state.Logs, state.Conversations, state.Traces, nil
}

func rebindTraceTree(node *CallTrace, traces map[string]*CallTrace) *CallTrace {
	if node == nil { return nil }
	if stored, ok := traces[node.ID]; ok && stored != nil {
		stored.ParentID = node.ParentID
		stored.Children = node.Children
		node = stored
	}
	for i, child := range node.Children { node.Children[i] = rebindTraceTree(child, traces) }
	return node
}

func (s *sqliteAgentRuntimeStore) Save(ctx context.Context, agents map[string]*Agent, logs map[string][]*AgentLog, conversations map[string]*Conversation, traces map[string]*CallTrace) error {
	state := agentRuntimeState{Agents: agents, Logs: logs, Conversations: conversations, Traces: traces}
	raw, err := json.Marshal(state)
	if err != nil { return fmt.Errorf("encode agent runtime state: %w", err) }
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil { return fmt.Errorf("begin agent runtime state transaction: %w", err) }
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_runtime_state (id, state_json, updated_at) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET state_json = excluded.state_json, updated_at = excluded.updated_at`, string(raw), time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("save agent runtime state: %w", err)
	}
	if err := tx.Commit(); err != nil { return fmt.Errorf("commit agent runtime state: %w", err) }
	return nil
}

func (s *sqliteAgentRuntimeStore) Close() error { return s.db.Close() }
