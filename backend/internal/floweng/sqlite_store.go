package floweng

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
//
// T3.01.a adds the document columns of the Flow (kind, revision, binding_id,
// template_revision, parent_project_flow_id) to the CREATE and then, in
// addFlowColumns, to any flows table that predates them. The payload stays the
// document of record and these columns stay its mirror, exactly like status:
// they exist so the partial unique index below can be built on kind without
// reading JSON, and so a revision can be compared inside a transaction.
//
// The partial unique index idx_flows_one_active_project is group 2's rule "one
// active project flow per project" expressed where it cannot be raced: the
// engine checks the rule under its own lock, and this index is what keeps a
// database written by a concurrent process — or a caller that goes straight to
// the store — from holding two. It is created after the migration below, which
// is what makes an older database able to satisfy it.
func (s *SQLiteFlowStore) initSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS flows (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  template_id TEXT NOT NULL,
  status TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  kind TEXT NOT NULL DEFAULT 'project' CHECK (kind IN ('project','task')),
  revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
  binding_id TEXT,
  template_revision INTEGER,
  parent_project_flow_id TEXT
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
-- Receipts of the one-time, in-place data migrations this store has run
-- (currently t301a_single_active_project_flow). A migration that has a row here
-- is never run twice, so opening a database cannot repeat its effects. The
-- receipt column keeps the JSON report of what that run changed.
CREATE TABLE IF NOT EXISTS flow_store_migrations (
  name       TEXT PRIMARY KEY,
  applied_at INTEGER NOT NULL,
  receipt    TEXT NOT NULL
);
`)
	if err != nil {
		return fmt.Errorf("init floweng schema: %w", err)
	}
	if err := s.addFlowColumns(); err != nil {
		return err
	}
	// The migration comes before the index, and the order is the point: an
	// existing database may hold several active project flows, and the index can
	// only be created on one that does not. On a database this build has already
	// opened, the migration is a receipt lookup and creates the index in the same
	// statement it always did.
	if err := s.migrateSingleActiveProjectFlow(); err != nil {
		return err
	}
	if err := s.createSingleActiveProjectFlowIndex(); err != nil {
		return err
	}
	return nil
}

// createSingleActiveProjectFlowIndex creates the index that enforces
// T3.01.a group 2's rule: one active project flow per project. It is partial, so
// it constrains only the rows that are a project's flow of record and drive its
// stages: any number of task flows, completed/aborted/suspended project flows
// and flows of other projects are unconstrained, and a project with no active
// project flow at all (archived, or mid-restore) is fine. The first active
// project flow of a project can always be inserted; a second meets a UNIQUE
// failure that Put reports as ErrActiveProjectFlowExists.
//
// It runs on its own, after the migration, so a database that still violates the
// rule fails here with a message that says which rule and why, rather than
// somewhere inside a schema script.
func (s *SQLiteFlowStore) createSingleActiveProjectFlowIndex() error {
	if _, err := s.db.Exec(
		`CREATE UNIQUE INDEX IF NOT EXISTS ` + oneActiveProjectFlowIndexName +
			` ON flows(project_id) WHERE kind='project' AND status='active'`,
	); err != nil {
		return fmt.Errorf("create %s (a database with several active project flows in one project must be migrated first): %w",
			oneActiveProjectFlowIndexName, err)
	}
	return nil
}

// addFlowColumns adds the T3.01.a document columns to a flows table written by
// an earlier build. It is idempotent: the columns that are already there are
// left alone, so opening the same database twice is not an error.
//
// The whole migration is one transaction, and it writes nothing but column
// definitions. In particular it does not rewrite a single payload, and it does
// not queue a single outbox row: the existing rows keep the documents they were
// written with, and a Flow's timeline stays exactly as long as it was. They get
// kind='project' and revision=1 from the column defaults, which is what those
// documents mean — every document written before this build is a project flow,
// and no stored document has ever counted a write.
//
// ALTER TABLE ADD COLUMN with a non-constant default is refused by SQLite, but a
// constant default (1, 'project') is allowed and is applied to the existing
// rows, which is why the defaults carry the backfill. The CHECK constraints of
// the new columns are copied from the CREATE above so a database created by
// either path ends up with the same table; SQLite accepts them on ADD COLUMN as
// long as the default satisfies them.
func (s *SQLiteFlowStore) addFlowColumns() error {
	existing, err := s.flowColumns()
	if err != nil {
		return err
	}
	additions := []struct{ name, ddl string }{
		{"kind", `ALTER TABLE flows ADD COLUMN kind TEXT NOT NULL DEFAULT 'project' CHECK (kind IN ('project','task'))`},
		{"revision", `ALTER TABLE flows ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)`},
		{"binding_id", `ALTER TABLE flows ADD COLUMN binding_id TEXT`},
		{"template_revision", `ALTER TABLE flows ADD COLUMN template_revision INTEGER`},
		{"parent_project_flow_id", `ALTER TABLE flows ADD COLUMN parent_project_flow_id TEXT`},
	}
	missing := make([]string, 0, len(additions))
	for _, a := range additions {
		if !existing[a.name] {
			missing = append(missing, a.ddl)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("migrate flows columns: begin: %w", err)
	}
	defer tx.Rollback()
	for _, ddl := range missing {
		if _, err := tx.Exec(ddl); err != nil {
			return fmt.Errorf("migrate flows columns: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate flows columns: commit: %w", err)
	}
	return nil
}

// flowColumns returns the column names of the flows table as PRAGMA
// table_info(flows) reports them.
func (s *SQLiteFlowStore) flowColumns() (map[string]bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(flows)`)
	if err != nil {
		return nil, fmt.Errorf("read flows columns: %w", err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var (
			cid       int
			name      string
			typ       string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			return nil, fmt.Errorf("read flows columns: %w", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read flows columns: %w", err)
	}
	return cols, nil
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
//
// T3.01.a assigns the document's revision here, in the same transaction that
// writes the document: 1 for a Flow the table does not hold yet, and the stored
// row's revision plus one for every later Put. The value is written back into
// the caller's Flow, so the copy the engine returns carries the revision that
// was just stored rather than the one it arrived with. The incoming Revision is
// deliberately not compared against the stored one — this is not
// compare-and-set, and a caller cannot lose a write to a stale revision here.
// CAS on the revision (an expected_revision guard for the runtime database) is
// T3.01.b, and it is why dbx.WithTxLock("immediate") is set on this store.
//
// Kind, binding_id, template_revision and parent_project_flow_id are mirrored
// into their columns from the document, the way status already is. Empty ones
// are written as NULL, so "not set" stays distinguishable from a value.
//
// A write that would leave the project with two active project flows meets
// idx_flows_one_active_project and comes back as ErrActiveProjectFlowExists —
// no transaction is left half-written, and the id of the flow that holds the
// slot is in the error.
func (s *SQLiteFlowStore) Put(flow *Flow) error {
	if flow == nil || flow.ID == "" {
		return fmt.Errorf("flow id is required")
	}
	if err := normalizeFlowDocument(flow); err != nil {
		return fmt.Errorf("put flow: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("put flow: begin: %w", err)
	}
	defer tx.Rollback()

	if err := writeFlowDocument(tx, flow); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("put flow: commit: %w", err)
	}
	return nil
}

// writeFlowDocument writes one Flow inside the caller's transaction: it assigns
// the document's revision from the stored row, upserts the row and its mirror
// columns, and queues every event the stored document did not already have into
// flow_event_outbox. The payload, the row and the outbox rows therefore land
// together or not at all, which is the guarantee Put documents.
//
// It is a plain function on *sql.Tx, not a method, because two callers with
// different lifetimes need exactly this sequence: Put, in a transaction that
// lasts one write, and the one-time store migration, in a transaction that
// rewrites many rows and records its receipt. Duplicating the sequence would
// mean two places that decide what a stored Flow is, and the migration's
// suspended flows would be the first documents written by the copy.
//
// The revision is written back into the caller's document, so the copy the
// caller holds carries the revision that was just stored.
func writeFlowDocument(tx *sql.Tx, flow *Flow) error {
	stored, err := loadStoredEventIDs(tx, flow.ID)
	if err != nil {
		return err
	}
	revision, err := nextRevision(tx, flow)
	if err != nil {
		return err
	}
	flow.Revision = revision

	payload, err := json.Marshal(flow)
	if err != nil {
		return fmt.Errorf("marshal flow: %w", err)
	}

	nowMS := time.Now().UTC().UnixMilli()
	if _, err := tx.Exec(`
INSERT INTO flows (id, project_id, template_id, status, payload, created_at, updated_at,
                   kind, revision, binding_id, template_revision, parent_project_flow_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  project_id=excluded.project_id,
  template_id=excluded.template_id,
  status=excluded.status,
  payload=excluded.payload,
  updated_at=excluded.updated_at,
  kind=excluded.kind,
  revision=excluded.revision,
  binding_id=excluded.binding_id,
  template_revision=excluded.template_revision,
  parent_project_flow_id=excluded.parent_project_flow_id
`, flow.ID, flow.ProjectID, string(flow.TemplateID), string(flow.Status), string(payload),
		flow.CreatedAt.UTC().UnixMilli(), flow.UpdatedAt.UTC().UnixMilli(),
		string(flow.Kind), revision, nullableText(flow.BindingID),
		nullableInt(flow.TemplateRevision), nullableText(flow.ParentProjectFlowID)); err != nil {
		if id, ok := conflictingActiveProjectFlowID(tx, flow, err); ok {
			return activeProjectFlowError(id)
		}
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
	return nil
}

// conflictingActiveProjectFlowID reports whether a failed flows write failed
// because of idx_flows_one_active_project, and if so the id of the flow that
// holds the slot. Returns ok=false for every other error, which must stay
// whatever it was.
//
// Two spellings can be recognised. On this build the unique index reports itself
// by the columns it is declared on — "UNIQUE constraint failed:
// flows.project_id" — because SQLite names the indexed columns, not the index.
// A database created by a build that used the constraint form (a UNIQUE ...
// WHERE constraint, where SQLite does report the constraint name) explains
// itself by the index/constraint name instead, so that spelling is accepted too.
// Both checks are specific: an unrelated UNIQUE failure, or a CHECK/NOT NULL
// failure, is not misreported as this rule.
//
// The holder's id is not in the driver's error, so it is read back. The
// statement that failed was rolled back on its own (SQLite's default ABORT
// conflict resolution aborts the statement, not the transaction), and the row
// that blocks the write was committed before this transaction began, so the
// read sees the holder.
func conflictingActiveProjectFlowID(tx *sql.Tx, flow *Flow, err error) (string, bool) {
	if err == nil || flow == nil {
		return "", false
	}
	msg := err.Error()
	named := strings.Contains(msg, oneActiveProjectFlowIndexName)
	if !named && !(strings.Contains(msg, "UNIQUE constraint failed") && strings.Contains(msg, "flows.project_id")) {
		return "", false
	}
	var existing string
	q := `SELECT id FROM flows WHERE project_id = ? AND kind = 'project' AND status = 'active' AND id <> ?`
	if err := tx.QueryRow(q, flow.ProjectID, flow.ID).Scan(&existing); err == nil && existing != "" {
		return existing, true
	}
	// The rule is still the reason even when the holder cannot be named;
	// naming the project is the honest answer.
	return "another active project flow of " + flow.ProjectID, true
}

// nextRevision returns the revision the document being written must carry: 1
// when the table does not hold the Flow yet, and the stored revision plus one
// otherwise. It runs on the caller's transaction, so the number it returns is
// the one the caller is about to store, with no window for another writer to
// take it first (memoryStore.Put applies the same rule to its own map).
//
// A row whose revision is not a revision (below 1) is an error rather than a
// silent reset, in the same spirit as loadStoredEventIDs' unreadable payload:
// guessing what such a row meant would number the documents wrongly.
func nextRevision(tx *sql.Tx, flow *Flow) (int64, error) {
	var stored sql.NullInt64
	err := tx.QueryRow(`SELECT revision FROM flows WHERE id = ?`, flow.ID).Scan(&stored)
	if err == sql.ErrNoRows {
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read stored flow %s revision: %w", flow.ID, err)
	}
	if !stored.Valid || stored.Int64 < 1 {
		return 0, fmt.Errorf("stored flow %s has revision %v, which is not a revision", flow.ID, stored)
	}
	return stored.Int64 + 1, nil
}

// nullableText maps "" to SQL NULL, so an unset mirror column is not stored as
// an empty string.
func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullableInt maps 0 to SQL NULL (0 means "unknown" for template_revision).
func nullableInt(n int64) any {
	if n == 0 {
		return nil
	}
	return n
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

// flowMirror is the part of a flows row that duplicates the document. rowID is
// "" when the row was not selected; Get and List compare it against the
// document they just decoded.
type flowMirror struct {
	rowID    string
	kind     string
	revision int64
}

// check reports whether the document agrees with its columns. The payload is
// the document of record and the columns are its mirror, so a disagreement
// means one of the two was written by something that did not go through Put —
// reading either one would be reading a value the Flow does not have. It is
// reported, naming the flow, instead of quietly picked.
func (m flowMirror) check(flow *Flow) error {
	if m.rowID == "" {
		return nil
	}
	if string(flow.Kind) != m.kind {
		return fmt.Errorf("flow %s: payload kind %q does not match flows.kind %q", flow.ID, flow.Kind, m.kind)
	}
	if flow.Revision != m.revision {
		return fmt.Errorf("flow %s: payload revision %d does not match flows.revision %d", flow.ID, flow.Revision, m.revision)
	}
	return nil
}

// Get loads a flow by id.
func (s *SQLiteFlowStore) Get(id string) (*Flow, error) {
	var (
		payload string
		mirror  flowMirror
	)
	err := s.db.QueryRow(`SELECT id, payload, kind, revision FROM flows WHERE id = ?`, id).
		Scan(&mirror.rowID, &payload, &mirror.kind, &mirror.revision)
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
	if err := mirror.check(&flow); err != nil {
		return nil, fmt.Errorf("get flow: %w", err)
	}
	return cloneFlow(&flow), nil
}

// List returns flows, optionally filtered by projectID.
func (s *SQLiteFlowStore) List(projectID string) ([]*Flow, error) {
	var rows *sql.Rows
	var err error
	if projectID == "" {
		rows, err = s.db.Query(`SELECT id, payload, kind, revision FROM flows ORDER BY updated_at DESC`)
	} else {
		rows, err = s.db.Query(`SELECT id, payload, kind, revision FROM flows WHERE project_id = ? ORDER BY updated_at DESC`, projectID)
	}
	if err != nil {
		return nil, fmt.Errorf("list flows: %w", err)
	}
	defer rows.Close()

	out := make([]*Flow, 0)
	for rows.Next() {
		var (
			payload string
			mirror  flowMirror
		)
		if err := rows.Scan(&mirror.rowID, &payload, &mirror.kind, &mirror.revision); err != nil {
			return nil, err
		}
		var flow Flow
		if err := json.Unmarshal([]byte(payload), &flow); err != nil {
			return nil, fmt.Errorf("unmarshal flow: %w", err)
		}
		if err := mirror.check(&flow); err != nil {
			return nil, fmt.Errorf("list flows: %w", err)
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
