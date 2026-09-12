package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/dbx"
)

// legacyAgentsDDL is the pre-migration schema exactly as initSchema created it
// before revision support, used to build "old database" fixtures by hand.
const legacyAgentsDDL = `
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
`

type agentRow struct {
	id        string
	name      string
	roleBase  string
	source    string
	enabled   int64
	payload   string
	updatedAt int64
}

type revisionRow struct {
	agentID   string
	revision  int64
	frozen    string
	source    string
	createdAt int64
}

type headRow struct {
	agentID   string
	head      int64
	updatedAt int64
}

func revisionTestAssets() []*AgentAsset {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	temp := 0.7
	maxTok := 4096
	return []*AgentAsset{
		{
			ID: "builtin-flow-conductor", Name: "Flow Conductor", Avatar: "🎯",
			Description: "orchestrates stages", Version: "1.0.0",
			Source: SourceBuiltin, RoleBase: RoleBaseMain,
			SystemPrompt: "You are the Flow Conductor.",
			StageTags:    []string{"planning", "coding"},
			Enabled:      true,
			CreatedAt:    base, UpdatedAt: base,
		},
		{
			ID: "user-禁用-agent", Name: "已停用 Agent \"quoted\"", Version: "0.3.1",
			Source: SourceUser, RoleBase: RoleBaseCoder,
			SystemPrompt: "多行\nprompt with 'quotes' and \"double\"",
			Binding: Binding{
				Model: "gpt-5", Channel: "default",
				Temperature: &temp, MaxTokens: &maxTok,
			},
			Mounts: Mounts{
				MCPTools: []string{"fs.read", "shell.exec"},
				Skills:   []string{"review"},
			},
			Enabled:   false,
			CreatedAt: base, UpdatedAt: base.Add(time.Hour),
		},
		{
			ID: "plugin-scout", Name: "Scout Plugin", Version: "2.0.0-rc.1",
			Source: SourcePlugin, RoleBase: RoleBaseSub,
			Stats:   Stats{UsageCount: 7, Score: 3.5},
			Enabled: true,
			CreatedAt: base, UpdatedAt: base.Add(2 * time.Hour),
		},
	}
}

// createLegacyAgentsDB builds a pre-migration database: legacy schema only,
// user_version untouched (0), one row per asset with payload = put() encoding.
func createLegacyAgentsDB(t *testing.T, dbPath string, assets []*AgentAsset) {
	t.Helper()
	db, err := dbx.Open(dbPath, dbx.WithMaxOpenConns(1))
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(legacyAgentsDDL); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	for _, a := range assets {
		payload, err := json.Marshal(a)
		if err != nil {
			t.Fatalf("marshal %s: %v", a.ID, err)
		}
		enabled := 0
		if a.Enabled {
			enabled = 1
		}
		if _, err := db.Exec(
			`INSERT INTO agents (id, name, role_base, source, enabled, payload, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
			a.ID, a.Name, string(a.RoleBase), string(a.Source), enabled,
			string(payload), a.UpdatedAt.UTC().UnixMilli(),
		); err != nil {
			t.Fatalf("insert legacy %s: %v", a.ID, err)
		}
	}
}

func readUserVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	return v
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name,
	).Scan(&n); err != nil {
		t.Fatalf("check table %s: %v", name, err)
	}
	return n == 1
}

func dumpAgentsRows(t *testing.T, db *sql.DB) []agentRow {
	t.Helper()
	rows, err := db.Query(
		`SELECT id, name, role_base, source, enabled, payload, updated_at FROM agents ORDER BY id`)
	if err != nil {
		t.Fatalf("dump agents: %v", err)
	}
	defer rows.Close()
	out := make([]agentRow, 0)
	for rows.Next() {
		var r agentRow
		if err := rows.Scan(&r.id, &r.name, &r.roleBase, &r.source, &r.enabled, &r.payload, &r.updatedAt); err != nil {
			t.Fatalf("scan agent row: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("dump agents: %v", err)
	}
	return out
}

func dumpRevisionRows(t *testing.T, db *sql.DB) []revisionRow {
	t.Helper()
	rows, err := db.Query(
		`SELECT agent_id, revision, frozen_config, source, created_at FROM agent_revisions ORDER BY agent_id, revision`)
	if err != nil {
		t.Fatalf("dump revisions: %v", err)
	}
	defer rows.Close()
	out := make([]revisionRow, 0)
	for rows.Next() {
		var r revisionRow
		if err := rows.Scan(&r.agentID, &r.revision, &r.frozen, &r.source, &r.createdAt); err != nil {
			t.Fatalf("scan revision row: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("dump revisions: %v", err)
	}
	return out
}

func dumpHeadRows(t *testing.T, db *sql.DB) []headRow {
	t.Helper()
	rows, err := db.Query(
		`SELECT agent_id, head_revision, updated_at FROM agent_revision_head ORDER BY agent_id`)
	if err != nil {
		t.Fatalf("dump heads: %v", err)
	}
	defer rows.Close()
	out := make([]headRow, 0)
	for rows.Next() {
		var r headRow
		if err := rows.Scan(&r.agentID, &r.head, &r.updatedAt); err != nil {
			t.Fatalf("scan head row: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("dump heads: %v", err)
	}
	return out
}

func mustUnmarshalAsset(t *testing.T, payload string) *AgentAsset {
	t.Helper()
	var a AgentAsset
	if err := json.Unmarshal([]byte(payload), &a); err != nil {
		t.Fatalf("unmarshal asset: %v", err)
	}
	return &a
}

// putAgentsRowOnly writes only the agents row (no revision append), the exact
// behavior put() had before T1.03.b wired revision appends into it. Fixtures
// that exercise appendRevisionTx in isolation use it to start from head=0.
func putAgentsRowOnly(t *testing.T, store *sqliteAgentStore, a *AgentAsset) {
	t.Helper()
	payload, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal %s: %v", a.ID, err)
	}
	enabled := 0
	if a.Enabled {
		enabled = 1
	}
	if _, err := store.db.Exec(
		`INSERT INTO agents (id, name, role_base, source, enabled, payload, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Name, string(a.RoleBase), string(a.Source), enabled,
		string(payload), a.UpdatedAt.UTC().UnixMilli(),
	); err != nil {
		t.Fatalf("insert row-only %s: %v", a.ID, err)
	}
}

// Entry 1: empty database. Open builds the full latest schema; there is
// nothing to backfill, so the revision tables stay empty.
func TestRevisionMigrationEmptyDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_empty.db")

	store, err := openSQLiteAgentStore(dbPath)
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	if got := readUserVersion(t, store.db); got != agentSchemaVersion {
		t.Fatalf("user_version=%d want %d", got, agentSchemaVersion)
	}
	for _, table := range []string{"agents", "agent_revisions", "agent_revision_head"} {
		if !tableExists(t, store.db, table) {
			t.Fatalf("table %s missing after empty-db open", table)
		}
	}
	if rows := dumpRevisionRows(t, store.db); len(rows) != 0 {
		t.Fatalf("revisions=%d want 0 on empty db", len(rows))
	}
	if rows := dumpHeadRows(t, store.db); len(rows) != 0 {
		t.Fatalf("heads=%d want 0 on empty db", len(rows))
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen: migration gate makes it a no-op.
	store2, err := openSQLiteAgentStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()
	if got := readUserVersion(t, store2.db); got != agentSchemaVersion {
		t.Fatalf("user_version after reopen=%d want %d", got, agentSchemaVersion)
	}
	if rows := dumpRevisionRows(t, store2.db); len(rows) != 0 {
		t.Fatalf("revisions after reopen=%d want 0", len(rows))
	}
}

// Entry 2: pre-migration database with data. Every existing agent gains
// revision 1 whose frozen config is byte-identical to its payload; the legacy
// agents rows, ids and Version labels are untouched.
func TestRevisionMigrationLegacyDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_legacy.db")
	assets := revisionTestAssets()
	createLegacyAgentsDB(t, dbPath, assets)

	legacy, err := dbx.Open(dbPath, dbx.WithMaxOpenConns(1))
	if err != nil {
		t.Fatal(err)
	}
	if got := readUserVersion(t, legacy); got != 0 {
		legacy.Close()
		t.Fatalf("legacy fixture user_version=%d want 0", got)
	}
	if tableExists(t, legacy, "agent_revisions") {
		legacy.Close()
		t.Fatal("legacy fixture should not have agent_revisions")
	}
	before := dumpAgentsRows(t, legacy)
	legacy.Close()

	store, err := openSQLiteAgentStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if got := readUserVersion(t, store.db); got != agentSchemaVersion {
		t.Fatalf("user_version=%d want %d", got, agentSchemaVersion)
	}

	// Negative assertion: migration must not modify existing rows or ids.
	after := dumpAgentsRows(t, store.db)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("agents rows changed by migration:\nbefore=%+v\nafter=%+v", before, after)
	}

	payloadByID := make(map[string]string, len(after))
	for _, r := range after {
		payloadByID[r.id] = r.payload
	}
	heads := dumpHeadRows(t, store.db)
	if len(heads) != len(assets) {
		t.Fatalf("head rows=%d want %d", len(heads), len(assets))
	}
	for _, a := range assets {
		payload := payloadByID[a.ID]
		if payload == "" {
			t.Fatalf("agent %s missing after migration", a.ID)
		}
		head, err := store.headRevision(a.ID)
		if err != nil {
			t.Fatalf("head of %s: %v", a.ID, err)
		}
		if head != 1 {
			t.Fatalf("head of %s=%d want 1", a.ID, head)
		}

		frozen, err := store.revisionAsset(a.ID, 1)
		if err != nil {
			t.Fatalf("revision 1 of %s: %v", a.ID, err)
		}
		// Field-by-field equality with the current asset document.
		if want := mustUnmarshalAsset(t, payload); !reflect.DeepEqual(want, frozen) {
			t.Fatalf("frozen config of %s differs from asset:\nwant=%+v\ngot=%+v", a.ID, want, frozen)
		}
		// Version display label and id preserved in the snapshot.
		if frozen.ID != a.ID || frozen.Version != a.Version {
			t.Fatalf("frozen id/version of %s = %s/%s", a.ID, frozen.ID, frozen.Version)
		}
		if frozen.Enabled != a.Enabled {
			t.Fatalf("frozen enabled of %s = %v want %v", a.ID, frozen.Enabled, a.Enabled)
		}
	}
	for _, r := range dumpRevisionRows(t, store.db) {
		if r.revision != 1 {
			t.Fatalf("revision of %s=%d want 1", r.agentID, r.revision)
		}
		if r.source != revisionSourceMigrationV1 {
			t.Fatalf("revision source of %s=%q want %q", r.agentID, r.source, revisionSourceMigrationV1)
		}
		// Snapshot bytes are the payload bytes; created_at reuses updated_at.
		if r.frozen != payloadByID[r.agentID] {
			t.Fatalf("frozen bytes of %s differ from payload", r.agentID)
		}
		want := mustUnmarshalAsset(t, payloadByID[r.agentID])
		if r.createdAt != want.UpdatedAt.UTC().UnixMilli() {
			t.Fatalf("created_at of %s=%d want %d", r.agentID, r.createdAt, want.UpdatedAt.UTC().UnixMilli())
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// The real entry point still loads the migrated database: same ids, same
	// Version labels, no builtin reseed on top of existing rows.
	reg, err := NewSQLiteAgentRegistry(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	list, err := reg.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != len(assets) {
		t.Fatalf("registry list=%d want %d (no reseed)", len(list), len(assets))
	}
	for _, a := range assets {
		got, err := reg.Get(context.Background(), a.ID)
		if err != nil {
			t.Fatalf("registry get %s: %v", a.ID, err)
		}
		if got.Version != a.Version || got.Name != a.Name || got.Enabled != a.Enabled {
			t.Fatalf("registry asset %s = %+v", a.ID, got)
		}
	}
}

// Entry 3: already-migrated database. Reopening is a no-op; revision and head
// rows stay byte-identical.
func TestRevisionMigrationReentrant(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_reopen.db")
	assets := revisionTestAssets()[:2]
	createLegacyAgentsDB(t, dbPath, assets)

	var prevRevisions []revisionRow
	var prevHeads []headRow
	var prevAgents []agentRow
	for i := 0; i < 3; i++ {
		store, err := openSQLiteAgentStore(dbPath)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if got := readUserVersion(t, store.db); got != agentSchemaVersion {
			store.Close()
			t.Fatalf("open %d user_version=%d want %d", i, got, agentSchemaVersion)
		}
		revs := dumpRevisionRows(t, store.db)
		heads := dumpHeadRows(t, store.db)
		agents := dumpAgentsRows(t, store.db)
		if len(revs) != len(assets) || len(heads) != len(assets) {
			store.Close()
			t.Fatalf("open %d revisions=%d heads=%d want %d each", i, len(revs), len(heads), len(assets))
		}
		if i > 0 {
			if !reflect.DeepEqual(prevRevisions, revs) {
				t.Fatalf("open %d changed revisions", i)
			}
			if !reflect.DeepEqual(prevHeads, heads) {
				t.Fatalf("open %d changed heads", i)
			}
			if !reflect.DeepEqual(prevAgents, agents) {
				t.Fatalf("open %d changed agents", i)
			}
		}
		prevRevisions, prevHeads, prevAgents = revs, heads, agents
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// appendRevisionTx is the write primitive T1.03.b wires into Update: each call
// appends head+1 inside the caller's transaction, earlier revisions stay
// frozen, and a rollback leaves no trace. The fixture writes the agents rows
// row-only so the first manual append starts from head=0.
func TestAppendRevisionTxPrimitive(t *testing.T) {
	store, err := openSQLiteAgentStore(filepath.Join(t.TempDir(), "agents_append.db"))
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	defer store.Close()

	base := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	a1 := &AgentAsset{
		ID: "user-one", Name: "One", Version: "0.1.0",
		Source: SourceUser, RoleBase: RoleBaseCoder,
		SystemPrompt: "v1 prompt", Enabled: true,
		CreatedAt: base, UpdatedAt: base,
	}
	a2 := &AgentAsset{
		ID: "user-two", Name: "Two", Version: "0.1.0",
		Source: SourceUser, RoleBase: RoleBaseSub, Enabled: true,
		CreatedAt: base, UpdatedAt: base,
	}
	putAgentsRowOnly(t, store, a1)
	putAgentsRowOnly(t, store, a2)

	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	rev, err := store.appendRevisionTx(tx, a1, "update")
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if rev != 1 {
		tx.Rollback()
		t.Fatalf("first append revision=%d want 1", rev)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	head, err := store.headRevision(a1.ID)
	if err != nil || head != 1 {
		t.Fatalf("head=%d err=%v want 1", head, err)
	}
	if head, err := store.headRevision(a2.ID); err != nil || head != 0 {
		t.Fatalf("a2 head=%d err=%v want 0 (no revisions)", head, err)
	}
	frozen1, err := store.revisionAsset(a1.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	payload1, _ := json.Marshal(a1)
	if want := mustUnmarshalAsset(t, string(payload1)); !reflect.DeepEqual(want, frozen1) {
		t.Fatalf("revision 1 frozen=%+v want %+v", frozen1, want)
	}

	// Second append: head moves to 2, revision 1 stays frozen.
	edited := cloneAgent(a1)
	edited.Name = "One Renamed"
	edited.Version = "0.2.0"
	edited.SystemPrompt = "v2 prompt"
	edited.UpdatedAt = base.Add(time.Hour)
	tx, err = store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	rev, err = store.appendRevisionTx(tx, edited, "update")
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if rev != 2 {
		tx.Rollback()
		t.Fatalf("second append revision=%d want 2", rev)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if head, err := store.headRevision(a1.ID); err != nil || head != 2 {
		t.Fatalf("head=%d err=%v want 2", head, err)
	}
	again1, err := store.revisionAsset(a1.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if again1.Name != "One" || again1.SystemPrompt != "v1 prompt" {
		t.Fatalf("revision 1 mutated: %+v", again1)
	}
	frozen2, err := store.revisionAsset(a1.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if frozen2.Name != "One Renamed" || frozen2.Version != "0.2.0" {
		t.Fatalf("revision 2 frozen=%+v", frozen2)
	}

	// Missing revision is a not-found error.
	if _, err := store.revisionAsset(a1.ID, 99); !errors.Is(err, ErrAgentAssetNotFound) {
		t.Fatalf("missing revision err=%v want ErrAgentAssetNotFound", err)
	}

	// Rollback leaves neither revision nor head behind.
	tx, err = store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.appendRevisionTx(tx, a2, "update"); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if head, err := store.headRevision(a2.ID); err != nil || head != 0 {
		t.Fatalf("a2 head after rollback=%d err=%v want 0", head, err)
	}
}

// Migration backfill and the T1.03.b append primitive compose: the migrated
// head=1 continues with revision 2 while revision 1 stays the migration snapshot.
func TestRevisionMigrationThenAppend(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_migrate_append.db")
	assets := revisionTestAssets()[:1]
	createLegacyAgentsDB(t, dbPath, assets)

	store, err := openSQLiteAgentStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if head, err := store.headRevision(assets[0].ID); err != nil || head != 1 {
		t.Fatalf("head after migration=%d err=%v want 1", head, err)
	}

	edited := cloneAgent(assets[0])
	edited.Name = "Renamed After Migration"
	edited.UpdatedAt = assets[0].UpdatedAt.Add(time.Hour)
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	rev, err := store.appendRevisionTx(tx, edited, "update")
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if rev != 2 {
		tx.Rollback()
		t.Fatalf("append after migration revision=%d want 2", rev)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if head, err := store.headRevision(assets[0].ID); err != nil || head != 2 {
		t.Fatalf("head=%d err=%v want 2", head, err)
	}
	revs := dumpRevisionRows(t, store.db)
	if len(revs) != 2 {
		t.Fatalf("revisions=%d want 2", len(revs))
	}
	payload, _ := json.Marshal(assets[0])
	if revs[0].frozen != string(payload) || revs[0].source != revisionSourceMigrationV1 {
		t.Fatalf("revision 1 changed: %+v", revs[0])
	}
	frozen2, err := store.revisionAsset(assets[0].ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if frozen2.Name != "Renamed After Migration" {
		t.Fatalf("revision 2 frozen=%+v", frozen2)
	}
}
