package floweng

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codeflow/backend/internal/dbx"
)

// SQLiteFlowStore stores each Flow as a JSON document keyed by id.
//
// It is the legacy Flow's source of truth and, since T1.05.c, also the owner of
// that database's local outbox (flow_event_outbox): a Flow write and the
// timeline rows it newly produced commit together here, and a projector
// (internal/flowprojection) later replays them into the runtime database keyed
// by source_event_id. §19.3 / §27.1: the state and its local outbox share the
// source database transaction; the hop into the runtime database is a
// recoverable asynchronous projection, not a two-file commit (T3.01 moves the
// Flow into the runtime database and then shares one transaction).
type SQLiteFlowStore struct {
	db *sql.DB
}

// NewSQLiteFlowStore opens (or creates) a SQLite database at dbPath.
// Pass ":memory:" for an ephemeral private in-memory database.
//
// WithTxLock("immediate") is set because Put reads the stored document and then
// writes: under WAL a deferred read-then-write transaction can be refused with
// SQLITE_BUSY_SNAPSHOT (extended code 517) when another writer commits in
// between, and SQLite does not invoke the busy handler for that upgrade
// failure, so busy_timeout cannot help. Taking the write lock at BEGIN removes
// the failure instead of retrying it. Every other statement in this file is a
// single-statement Exec, whose semantics are unchanged.
func NewSQLiteFlowStore(dbPath string) (*SQLiteFlowStore, error) {
	if err := prepareFlowDBDir(dbPath); err != nil {
		return nil, err
	}
	db, err := dbx.Open(dbPath, dbx.WithMaxOpenConns(1), dbx.WithTxLock("immediate")) // SQLite write serialization
	if err != nil {
		return nil, fmt.Errorf("open floweng db: %w", err)
	}
	store := &SQLiteFlowStore{db: db}
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func prepareFlowDBDir(dbPath string) error {
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create floweng db dir: %w", err)
		}
	}
	return nil
}

// initSchema creates the Flow tables and the local event outbox. Everything is
// CREATE ... IF NOT EXISTS, so opening a database written by an older build adds
// the outbox table to it without touching the rows it already holds.
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
CREATE TABLE IF NOT EXISTS flow_templates (
  id TEXT PRIMARY KEY,
  payload TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);
-- Local outbox of the legacy Flow store (T1.05.c, §19.3/§27.1). Written in the
-- same transaction as the flows row it came from; read by the projector that
-- replays the timeline into the runtime database. Rows are facts: Delete of a
-- Flow does not remove them, and nothing in this package deletes them.
CREATE TABLE IF NOT EXISTS flow_event_outbox (
  seq                INTEGER PRIMARY KEY AUTOINCREMENT,   -- local append order
  source_event_id    TEXT    NOT NULL UNIQUE,             -- FlowEvent.ID
  flow_id            TEXT    NOT NULL,
  project_id         TEXT    NOT NULL,
  event_type         TEXT    NOT NULL,                    -- FlowEvent.Type (legacy vocabulary)
  stage_id           TEXT    NOT NULL DEFAULT '',
  message            TEXT    NOT NULL DEFAULT '',
  occurred_at        INTEGER NOT NULL,                    -- FlowEvent.Timestamp, Unix milliseconds UTC
  state              TEXT    NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','projected','dead_letter')),
  attempt_count      INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  next_attempt_at    INTEGER,
  last_error         TEXT,
  projected_event_id TEXT,
  created_at         INTEGER NOT NULL,
  projected_at       INTEGER,
  CHECK ((state = 'pending') = (next_attempt_at IS NOT NULL)),
  CHECK ((state = 'projected') = (projected_event_id IS NOT NULL AND projected_at IS NOT NULL))
);
-- The projector's claim scans "pending, due, no earlier pending row of the same
-- Flow"; these two partial indexes cover the two halves of it without indexing
-- the projected majority of the table.
CREATE INDEX IF NOT EXISTS idx_flow_event_outbox_flow_pending
  ON flow_event_outbox(flow_id, seq) WHERE state = 'pending';
CREATE INDEX IF NOT EXISTS idx_flow_event_outbox_due_pending
  ON flow_event_outbox(next_attempt_at, seq) WHERE state = 'pending';
`)
	if err != nil {
		return fmt.Errorf("init floweng schema: %w", err)
	}
	return nil
}

// PutTemplate persists a reusable custom template definition.
func (s *SQLiteFlowStore) PutTemplate(def CustomTemplate) error {
	payload, err := json.Marshal(def)
	if err != nil {
		return fmt.Errorf("marshal flow template: %w", err)
	}
	_, err = s.db.Exec(`
INSERT INTO flow_templates (id, payload, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(id) DO UPDATE SET payload=excluded.payload, updated_at=excluded.updated_at
`, string(def.ID), string(payload), time.Now().UTC().UnixMilli())
	if err != nil {
		return fmt.Errorf("put flow template: %w", err)
	}
	return nil
}

// ListTemplateDefinitions loads all reusable custom template definitions.
func (s *SQLiteFlowStore) ListTemplateDefinitions() ([]CustomTemplate, error) {
	rows, err := s.db.Query(`SELECT payload FROM flow_templates ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list flow templates: %w", err)
	}
	defer rows.Close()
	out := make([]CustomTemplate, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var def CustomTemplate
		if err := json.Unmarshal([]byte(payload), &def); err != nil {
			return nil, fmt.Errorf("unmarshal flow template: %w", err)
		}
		out = append(out, def)
	}
	return out, rows.Err()
}

// DeleteTemplate removes a persisted custom template definition.
func (s *SQLiteFlowStore) DeleteTemplate(id TemplateID) error {
	_, err := s.db.Exec(`DELETE FROM flow_templates WHERE id = ?`, string(id))
	if err != nil {
		return fmt.Errorf("delete flow template: %w", err)
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

// Put upserts a flow document and, in the same transaction, appends a local
// outbox row for every event this document produced for the first time.
//
// One transaction, not two: a flows row that committed while its timeline row
// was lost would be a Flow whose events can never be projected, and an outbox
// row without its Flow would be a fact that cannot be read back. Either both
// land or neither does (TestLegacyFlowPutAndOutboxRollbackTogether provokes both
// directions with triggers). The projection into the runtime database happens
// after the commit, keyed by source_event_id: §19.3's "迁移前的旧 Flow 在本源库
// 保存状态+local outbox，再按源 event ID 幂等投影到运行库，不承诺跨文件事务".
//
// Only events new to this document are queued. A Flow that existed before this
// build carries a timeline that was never projected, and it is not back-filled
// here: the full legacy migration is T3.01, and quietly replaying years of
// history from a Put would be a migration nobody asked for. The stored
// document's event ids are compared against the incoming ones; a document that
// cannot be read back (invalid JSON in payload) is an error rather than an
// empty set, because guessing "all of them are new" would duplicate a timeline
// and guessing "none of them are new" would drop one.
//
// Put does not change any other method. Get/List/Delete and the template
// methods behave exactly as before, and Delete deliberately leaves outbox rows
// in place: an event is a fact, and the projector may not have read it yet.
//
// The WebSocket notifier is NOT part of this path. It stays where it was — a
// best-effort notification fired by the engine before the store is even called
// — and it is not made reliable by moving it (see the plan's T1.05.c: "不能仅把
// notifier 移到 Put 后就声称可靠投递"). The reliable path is this outbox plus
// the projector; T1.12 moves the WS fan-out onto the runtime database's replay
// and delivery.
func (s *SQLiteFlowStore) Put(flow *Flow) error {
	if flow == nil || flow.ID == "" {
		return fmt.Errorf("flow id is required")
	}
	payload, err := json.Marshal(flow)
	if err != nil {
		return fmt.Errorf("marshal flow: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("put flow: begin: %w", err)
	}
	defer tx.Rollback()

	stored, err := loadStoredEventIDs(tx, flow.ID)
	if err != nil {
		return err
	}

	nowMS := time.Now().UTC().UnixMilli()
	if _, err := tx.Exec(`
INSERT INTO flows (id, project_id, template_id, status, payload, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  project_id=excluded.project_id,
  template_id=excluded.template_id,
  status=excluded.status,
  payload=excluded.payload,
  updated_at=excluded.updated_at
`, flow.ID, flow.ProjectID, string(flow.TemplateID), string(flow.Status), string(payload),
		flow.CreatedAt.UTC().UnixMilli(), flow.UpdatedAt.UTC().UnixMilli()); err != nil {
		return fmt.Errorf("put flow: %w", err)
	}

	for i := range flow.Events {
		ev := &flow.Events[i]
		if _, seen := stored[ev.ID]; seen {
			continue
		}
		if ev.ID == "" {
			// The engine always mints an id (uuid.New in appendEvent). An empty
			// id here is a programming error, not an empty fact: queueing it
			// would collapse every such event onto one outbox row and lose the
			// timeline, so refuse the whole write instead.
			return fmt.Errorf("put flow %s: event %d has an empty id", flow.ID, i)
		}
		if _, err := tx.Exec(`
INSERT INTO flow_event_outbox
  (source_event_id, flow_id, project_id, event_type, stage_id, message,
   occurred_at, state, attempt_count, next_attempt_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', 0, ?, ?)
ON CONFLICT(source_event_id) DO NOTHING
`, ev.ID, flow.ID, flow.ProjectID, ev.Type, ev.StageID, ev.Message,
			ev.Timestamp.UTC().UnixMilli(), nowMS, nowMS); err != nil {
			return fmt.Errorf("put flow %s: queue event %s: %w", flow.ID, ev.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("put flow: commit: %w", err)
	}
	return nil
}

// loadStoredEventIDs returns the event ids of the stored document of id, or an
// empty set when the row does not exist. It runs on the caller's transaction,
// so the set it returns is the one the caller is about to replace atomically.
//
// A payload that is not readable JSON is an error: Put must not guess which
// events are new (§27.4: a silent gap is worse than a failed write).
func loadStoredEventIDs(tx *sql.Tx, id string) (map[string]struct{}, error) {
	var payload string
	err := tx.QueryRow(`SELECT payload FROM flows WHERE id = ?`, id).Scan(&payload)
	if err == sql.ErrNoRows {
		return map[string]struct{}{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read stored flow %s: %w", id, err)
	}
	var stored struct {
		Events []struct {
			ID string `json:"id"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(payload), &stored); err != nil {
		return nil, fmt.Errorf("stored flow %s is not readable JSON: %w", id, err)
	}
	ids := make(map[string]struct{}, len(stored.Events))
	for _, ev := range stored.Events {
		ids[ev.ID] = struct{}{}
	}
	return ids, nil
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
