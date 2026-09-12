package agent

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/codeflow/backend/internal/dbx"
)

// sqliteAgentStore persists AgentAsset JSON documents.
type sqliteAgentStore struct {
	db *sql.DB
}

func openSQLiteAgentStore(dbPath string) (*sqliteAgentStore, error) {
	if err := prepareAgentDBDir(dbPath); err != nil {
		return nil, err
	}
	db, err := dbx.Open(dbPath, dbx.WithMaxOpenConns(1))
	if err != nil {
		return nil, fmt.Errorf("open agent db: %w", err)
	}
	s := &sqliteAgentStore{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.migrateSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func prepareAgentDBDir(dbPath string) error {
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create agent db dir: %w", err)
		}
	}
	return nil
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

// Revision model (T1.03.a, I-20/I-52): every agent asset gets immutable,
// numbered revisions so run records can bind to the exact configuration in
// effect at run time. The editable Version string stays a display label and
// the legacy agents table (columns, ids, row contents) is left untouched.
//
// agent_revisions holds the snapshots:
//   agent_id      agents.id of the snapshotted asset. No FOREIGN KEY is
//                 declared: T1.03.b delete removes the agents row and the
//                 head pointer but retains revision rows as audit facts, and
//                 a constraint here would preempt that retention.
//   revision      1-based, monotonically increasing per agent.
//   frozen_config JSON snapshot of the full AgentAsset, identical encoding to
//                 agents.payload (id/name/avatar/description/version/source/
//                 role_base/system_prompt/binding/mounts/stage_tags/stats/
//                 enabled/created_at/updated_at).
//   source        writer tag: revisionSourceMigrationV1 for the backfilled
//                 first revision, revisionSourceSeed for the builtin seed
//                 path, revisionSourceUpdate for registry writes (T1.03.b).
//   created_at    unix millis; backfilled rows reuse agents.updated_at, the
//                 last modification time of the snapshotted asset.
//
// agent_revision_head is the per-agent head pointer. It is a separate table
// (not an agents column) because the legacy agents table structure must not
// change. (agent_id, revision) is the revisions primary key, so head lookups
// and per-agent scans need no extra index.
const (
	revisionSourceMigrationV1 = "migration_v1"
	revisionSourceSeed        = "seed"
	revisionSourceUpdate      = "update"
	agentSchemaVersion        = 1
)

// agentMigration is one PRAGMA user_version-gated schema step. All statements
// plus the version stamp commit in a single transaction, so a crash cannot
// leave a stamped-but-unapplied (or applied-but-unstamped) database.
type agentMigration struct {
	version    int
	statements []string
}

var agentSchemaMigrations = []agentMigration{
	{
		version: 1,
		statements: []string{
			`CREATE TABLE IF NOT EXISTS agent_revisions (
  agent_id TEXT NOT NULL,
  revision INTEGER NOT NULL,
  frozen_config TEXT NOT NULL,
  source TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (agent_id, revision)
);`,
			`CREATE TABLE IF NOT EXISTS agent_revision_head (
  agent_id TEXT PRIMARY KEY,
  head_revision INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);`,
			// First revision for every agent that existed before revisions
			// did: frozen_config copies the payload column verbatim, so the
			// snapshot is byte-identical to the current asset document.
			`INSERT INTO agent_revisions (agent_id, revision, frozen_config, source, created_at)
SELECT id, 1, payload, 'migration_v1', updated_at FROM agents;`,
			`INSERT INTO agent_revision_head (agent_id, head_revision, updated_at)
SELECT id, 1, updated_at FROM agents;`,
		},
	},
}

// migrateSchema applies every migration newer than the stored user_version.
// Empty databases, pre-migration databases and already-migrated databases all
// converge: the version gate makes re-entry a no-op, and existing rows keep
// their values and ids because the steps only add tables and copy data.
func (s *sqliteAgentStore) migrateSchema() error {
	var current int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("read agent user_version: %w", err)
	}
	for _, m := range agentSchemaMigrations {
		if m.version <= 0 {
			return fmt.Errorf("invalid agent migration version %d", m.version)
		}
		if m.version <= current {
			continue
		}
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin agent migration %d: %w", m.version, err)
		}
		for _, stmt := range m.statements {
			if _, err := tx.Exec(stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply agent migration %d: %w", m.version, err)
			}
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, m.version)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("stamp agent user_version %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("commit agent migration %d: %w", m.version, err)
		}
		current = m.version
	}
	return nil
}

// headRevision returns the current head revision of agentID, or 0 when the
// agent has no revisions yet.
func (s *sqliteAgentStore) headRevision(agentID string) (int, error) {
	var head int
	err := s.db.QueryRow(
		`SELECT head_revision FROM agent_revision_head WHERE agent_id = ?`, agentID,
	).Scan(&head)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read agent revision head: %w", err)
	}
	return head, nil
}

// revisionAsset loads the frozen config of one immutable revision.
func (s *sqliteAgentStore) revisionAsset(agentID string, revision int) (*AgentAsset, error) {
	var frozen string
	err := s.db.QueryRow(
		`SELECT frozen_config FROM agent_revisions WHERE agent_id = ? AND revision = ?`,
		agentID, revision,
	).Scan(&frozen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s revision %d", ErrAgentAssetNotFound, agentID, revision)
	}
	if err != nil {
		return nil, fmt.Errorf("read agent revision: %w", err)
	}
	var a AgentAsset
	if err := json.Unmarshal([]byte(frozen), &a); err != nil {
		return nil, fmt.Errorf("decode frozen config: %w", err)
	}
	return &a, nil
}

// appendRevisionTx appends a new immutable revision of a inside tx and moves
// the head pointer to it. This is the T1.03.b write primitive: the caller
// opens the transaction that also updates the agents row, so payload, head
// and revision commit or roll back together. Returns the new revision number.
func (s *sqliteAgentStore) appendRevisionTx(tx *sql.Tx, a *AgentAsset, source string) (int, error) {
	if tx == nil {
		return 0, fmt.Errorf("append agent revision: tx is nil")
	}
	if a == nil || a.ID == "" {
		return 0, fmt.Errorf("agent id required")
	}
	frozen, err := json.Marshal(a)
	if err != nil {
		return 0, err
	}
	var head int
	err = tx.QueryRow(
		`SELECT head_revision FROM agent_revision_head WHERE agent_id = ?`, a.ID,
	).Scan(&head)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("read agent revision head: %w", err)
	}
	next := head + 1
	created := a.UpdatedAt.UTC().UnixMilli()
	if _, err := tx.Exec(
		`INSERT INTO agent_revisions (agent_id, revision, frozen_config, source, created_at)
VALUES (?, ?, ?, ?, ?)`,
		a.ID, next, string(frozen), source, created,
	); err != nil {
		return 0, fmt.Errorf("insert agent revision %d: %w", next, err)
	}
	if _, err := tx.Exec(
		`INSERT INTO agent_revision_head (agent_id, head_revision, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(agent_id) DO UPDATE SET
  head_revision=excluded.head_revision,
  updated_at=excluded.updated_at`,
		a.ID, next, created,
	); err != nil {
		return 0, fmt.Errorf("move agent revision head %d: %w", next, err)
	}
	return next, nil
}

func (s *sqliteAgentStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// upsertAgentRowTx writes only the agents row of a inside tx. put pairs it
// with appendRevisionTx so the row and its new revision commit together.
func (s *sqliteAgentStore) upsertAgentRowTx(tx *sql.Tx, a *AgentAsset) error {
	payload, err := json.Marshal(a)
	if err != nil {
		return err
	}
	enabled := 0
	if a.Enabled {
		enabled = 1
	}
	_, err = tx.Exec(`
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

// put persists a and appends its immutable revision in one transaction: the
// agents row upsert and the revision append commit together or not at all,
// so any non-nil error means zero writes. The configuration mutations
// (Create, Update) reach this path and therefore advance the head; the
// writer tag for these edits is revisionSourceUpdate (T1.03.b).
func (s *sqliteAgentStore) put(a *AgentAsset) error {
	return s.putWithRevision(a, revisionSourceUpdate)
}

// putStats persists only the agents row of a, without appending a revision
// (T1.03.c stats-path narrowing). A revision is the immutable lineage of the
// asset configuration; Stats.UsageCount/Stats.Score are telemetry counters,
// not configuration, so IncrementUsage/SetScore reach this path and the head
// stays put. The row write is still transactional, so a non-nil error means
// zero writes, and the frozen_config column keeps carrying whatever stats
// values were in effect when each revision was appended.
func (s *sqliteAgentStore) putStats(a *AgentAsset) error {
	if a == nil || a.ID == "" {
		return fmt.Errorf("agent id required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin agent stats put tx: %w", err)
	}
	// Rollback after Commit returns sql.ErrTxDone, a documented no-op, so
	// the error return below still leaves zero writes.
	defer func() { _ = tx.Rollback() }()
	if err := s.upsertAgentRowTx(tx, a); err != nil {
		return fmt.Errorf("upsert agent row: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit agent stats put tx: %w", err)
	}
	return nil
}

// seedPut is the put variant for the builtin seed path: the first revision
// of a seeded asset is tagged revisionSourceSeed (T1.03.b).
func (s *sqliteAgentStore) seedPut(a *AgentAsset) error {
	return s.putWithRevision(a, revisionSourceSeed)
}

func (s *sqliteAgentStore) putWithRevision(a *AgentAsset, source string) error {
	if a == nil || a.ID == "" {
		return fmt.Errorf("agent id required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin agent put tx: %w", err)
	}
	// Rollback after Commit returns sql.ErrTxDone, a documented no-op, so
	// every error return below still leaves zero writes.
	defer func() { _ = tx.Rollback() }()
	if err := s.upsertAgentRowTx(tx, a); err != nil {
		return fmt.Errorf("upsert agent row: %w", err)
	}
	if _, err := s.appendRevisionTx(tx, a, source); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit agent put tx: %w", err)
	}
	return nil
}

// delete removes the agents row and the head pointer in one transaction but
// keeps every agent_revisions row: revision history is an audit fact that
// outlives the asset, so revisionAsset stays valid after deletion (T1.03.b).
func (s *sqliteAgentStore) delete(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin agent delete tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM agents WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete agent row: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM agent_revision_head WHERE agent_id = ?`, id); err != nil {
		return fmt.Errorf("delete agent revision head: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit agent delete tx: %w", err)
	}
	return nil
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
			if err := store.seedPut(a); err != nil {
				_ = store.Close()
				return nil, err
			}
		}
	}
	return r, nil
}
