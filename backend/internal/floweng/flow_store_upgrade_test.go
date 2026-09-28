package floweng

// Tests for the store half of T3.01.a: the document columns (kind, revision,
// binding_id, template_revision, parent_project_flow_id), the revision the store
// assigns, the mirror check that keeps a stored document and its columns from
// disagreeing, and the upgrade of a database written before any of this existed.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// upgradeFixtureFlow is testLegacyFlow with a twist of its own: the flow
// documents in the tests below are moved through SQLite and the engine, so an
// event has to be carried (appendEvent mints a fresh id and putRaw rejects the
// document otherwise) and none of the tests may depend on a random id.
func upgradeFixtureFlow(id, projectID string) *Flow {
	return testLegacyFlow(id, projectID, testLegacyEvent(id+"-ev", "flow.created", "", "created"))
}

// upgradeStoredRow reads the payload and updated_at of a flows row so a test can
// prove bytes were not rewritten, reporting "no row" instead of failing.
func upgradeStoredRow(t *testing.T, s *SQLiteFlowStore, id string) (payload string, updatedAt int64) {
	t.Helper()
	err := s.db.QueryRow(`SELECT payload, updated_at FROM flows WHERE id = ?`, id).Scan(&payload, &updatedAt)
	if err == sql.ErrNoRows {
		return "", 0
	}
	if err != nil {
		t.Fatalf("read flows row %s: %v", id, err)
	}
	return payload, updatedAt
}

// newUpgradeTestStore opens a store at a fresh path and returns it with the path.
func newUpgradeTestStore(t *testing.T) (*SQLiteFlowStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "floweng.db")
	store, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

// legacySchemaDDL is the schema of the flows, flow_templates and
// flow_event_outbox tables exactly as the build before T3.01.a created them,
// copied from sqlite_store.go at that commit. The upgrade test runs it to build
// a database of the old shape, so "opening an older database" is tested against
// the real thing rather than a guess at it.
const legacySchemaDDL = `
CREATE TABLE flows (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  template_id TEXT NOT NULL,
  status TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX idx_flows_project ON flows(project_id);
CREATE INDEX idx_flows_status ON flows(status);
CREATE TABLE flow_templates (
  id TEXT PRIMARY KEY,
  payload TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE flow_event_outbox (
  seq                INTEGER PRIMARY KEY AUTOINCREMENT,
  source_event_id    TEXT    NOT NULL UNIQUE,
  flow_id            TEXT    NOT NULL,
  project_id         TEXT    NOT NULL,
  event_type         TEXT    NOT NULL,
  stage_id           TEXT    NOT NULL DEFAULT '',
  message            TEXT    NOT NULL DEFAULT '',
  occurred_at        INTEGER NOT NULL,
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
CREATE INDEX idx_flow_event_outbox_flow_pending
  ON flow_event_outbox(flow_id, seq) WHERE state = 'pending';
CREATE INDEX idx_flow_event_outbox_due_pending
  ON flow_event_outbox(next_attempt_at, seq) WHERE state = 'pending';
`

// columnsOf returns the column names of a table, and every column's type,
// not-null flag and default, so a test can compare the migrated table with the
// table a fresh database creates.
func columnsOf(t *testing.T, s *SQLiteFlowStore, table string) map[string]string {
	t.Helper()
	rows, err := s.db.Query(`SELECT name, type, "notnull", COALESCE(dflt_value, '<none>') FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("read columns of %s: %v", table, err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, typ, dflt string
		var notNull int
		if err := rows.Scan(&name, &typ, &notNull, &dflt); err != nil {
			t.Fatalf("scan column of %s: %v", table, err)
		}
		out[name] = fmt.Sprintf("%s|notnull=%d|default=%s", typ, notNull, dflt)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read columns of %s: %v", table, err)
	}
	return out
}

// mirrorOf reads the flow columns of one row.
func mirrorOf(t *testing.T, s *SQLiteFlowStore, id string) flowMirror {
	t.Helper()
	var m flowMirror
	err := s.db.QueryRow(`SELECT id, kind, revision FROM flows WHERE id = ?`, id).
		Scan(&m.rowID, &m.kind, &m.revision)
	if err != nil {
		t.Fatalf("read mirror of %s: %v", id, err)
	}
	return m
}

// TestFlowStoreSchemaHasTheDocumentColumns pins the five columns T3.01.a adds,
// with the defaults and constraints the next steps depend on: the partial unique
// index of group 2 is built on kind, and revision is the counter T3.01.b
// compares.
func TestFlowStoreSchemaHasTheDocumentColumns(t *testing.T) {
	store, _ := newUpgradeTestStore(t)
	cols := columnsOf(t, store, "flows")

	want := map[string]string{
		"kind":                   "TEXT|notnull=1|default='project'",
		"revision":               "INTEGER|notnull=1|default=1",
		"binding_id":             "TEXT|notnull=0|default=<none>",
		"template_revision":      "INTEGER|notnull=0|default=<none>",
		"parent_project_flow_id": "TEXT|notnull=0|default=<none>",
	}
	for name, wantSpec := range want {
		got, ok := cols[name]
		if !ok {
			t.Fatalf("flows has no %s column; columns are %v", name, sortedSpecKeys(cols))
		}
		if got != wantSpec {
			t.Fatalf("flows.%s = %s, want %s", name, got, wantSpec)
		}
	}

	t.Run("kind only accepts the two kinds", func(t *testing.T) {
		_, err := store.db.Exec(`INSERT INTO flows
			(id, project_id, template_id, status, payload, created_at, updated_at, kind, revision)
			VALUES ('bad-kind', 'p', 'new_project', 'active', '{}', 0, 0, 'saga', 1)`)
		if err == nil {
			t.Fatal("a third kind was accepted")
		}
		if !strings.Contains(err.Error(), "CHECK constraint failed") {
			t.Fatalf("error is not the check constraint: %v", err)
		}
	})

	t.Run("revision starts at one", func(t *testing.T) {
		_, err := store.db.Exec(`INSERT INTO flows
			(id, project_id, template_id, status, payload, created_at, updated_at, kind, revision)
			VALUES ('bad-rev', 'p', 'new_project', 'active', '{}', 0, 0, 'project', 0)`)
		if err == nil {
			t.Fatal("revision 0 was accepted")
		}
	})
}

func sortedSpecKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestFlowStoreAssignsRevision is the revision rule: a new Flow is stored at 1,
// every later Put at the stored value plus one, both stores agree, and the value
// the store just stored is written back into the caller's document — which is
// what makes the engine's returned copy carry it.
func TestFlowStoreAssignsRevision(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		store, _ := newUpgradeTestStore(t)
		flow := upgradeFixtureFlow("flow-rev", "proj-rev")

		if err := store.Put(flow); err != nil {
			t.Fatalf("first put: %v", err)
		}
		if flow.Revision != 1 {
			t.Fatalf("after the first Put the document has revision %d, want 1", flow.Revision)
		}
		if got := mirrorOf(t, store, flow.ID); got.revision != 1 || got.kind != "project" {
			t.Fatalf("row after the first Put: %+v", got)
		}

		for want := int64(2); want <= 4; want++ {
			flow.UpdatedAt = flow.UpdatedAt.Add(time.Second)
			if err := store.Put(flow); err != nil {
				t.Fatalf("put %d: %v", want, err)
			}
			if flow.Revision != want {
				t.Fatalf("after Put %d the document has revision %d", want, flow.Revision)
			}
			if got := mirrorOf(t, store, flow.ID).revision; got != want {
				t.Fatalf("row revision after Put %d = %d", want, got)
			}
			loaded, err := store.Get(flow.ID)
			if err != nil {
				t.Fatalf("get after Put %d: %v", want, err)
			}
			if loaded.Revision != want {
				t.Fatalf("loaded revision after Put %d = %d", want, loaded.Revision)
			}
		}

		t.Run("reopening does not move the counter", func(t *testing.T) {
			// The document is written twice (so the stored revision is 2) and
			// then written through a second connection to the same file. If
			// opening a database reseeded anything, this write would come out
			// as 1 or 2 instead of the stored value plus one.
			path := filepath.Join(t.TempDir(), "rev.db")
			first, err := NewSQLiteFlowStore(path)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := first.Put(flow); err != nil {
					t.Fatal(err)
				}
			}
			if flow.Revision != 2 {
				t.Fatalf("revision after two writes = %d, want 2", flow.Revision)
			}
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			second, err := NewSQLiteFlowStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			if err := second.Put(flow); err != nil {
				t.Fatal(err)
			}
			if flow.Revision != 3 {
				t.Fatalf("revision after reopening and writing = %d, want 3", flow.Revision)
			}
		})
	})

	t.Run("memory store applies the same rule", func(t *testing.T) {
		store := newMemoryStore()
		flow := upgradeFixtureFlow("flow-rev-mem", "proj-rev")
		for want := int64(1); want <= 3; want++ {
			if err := store.Put(flow); err != nil {
				t.Fatalf("put %d: %v", want, err)
			}
			if flow.Revision != want {
				t.Fatalf("after Put %d the document has revision %d", want, flow.Revision)
			}
			got, err := store.Get(flow.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Revision != want {
				t.Fatalf("stored revision after Put %d = %d", want, got.Revision)
			}
		}
	})

	t.Run("the engine returns the revision the store stored", func(t *testing.T) {
		for name, store := range map[string]FlowStore{
			"memory": newMemoryStore(),
			"sqlite": func() FlowStore {
				s, _ := newUpgradeTestStore(t)
				return s
			}(),
		} {
			t.Run(name, func(t *testing.T) {
				eng := NewEngineWithStore(store, nil)
				flow, err := eng.Create(context.Background(), &CreateFlowRequest{ProjectID: "proj-rev-eng"})
				if err != nil {
					t.Fatalf("create: %v", err)
				}
				if flow.Revision != 1 {
					t.Fatalf("created flow revision = %d, want 1", flow.Revision)
				}
				if flow.Kind != FlowKindProject {
					t.Fatalf("created flow kind = %q, want project", flow.Kind)
				}
				advanced, err := eng.Advance(context.Background(), flow.ID, &AdvanceRequest{})
				if err != nil {
					t.Fatalf("advance: %v", err)
				}
				if advanced.Revision != 2 {
					t.Fatalf("revision after one advance = %d, want 2", advanced.Revision)
				}
				loaded, err := eng.Get(context.Background(), flow.ID)
				if err != nil {
					t.Fatal(err)
				}
				if loaded.Revision != 2 {
					t.Fatalf("stored revision after one advance = %d, want 2", loaded.Revision)
				}
			})
		}
	})
}

// TestFlowStoreRejectsAKindItCannotStore pins the store's half of the document
// invariant: a Flow that arrives without a kind is stored as a project flow
// (which is what its rows have always meant), and one that arrives with a kind
// this build does not know is refused rather than written into a row whose
// column would then disagree with its document.
func TestFlowStoreRejectsAKindItCannotStore(t *testing.T) {
	store, _ := newUpgradeTestStore(t)

	t.Run("no kind is stored as project", func(t *testing.T) {
		flow := upgradeFixtureFlow("flow-nokind", "proj-nokind")
		if err := store.Put(flow); err != nil {
			t.Fatalf("put: %v", err)
		}
		if flow.Kind != FlowKindProject {
			t.Fatalf("the store left the document with kind %q", flow.Kind)
		}
		if got := mirrorOf(t, store, flow.ID); got.kind != "project" {
			t.Fatalf("row kind = %q", got.kind)
		}
		loaded, err := store.Get(flow.ID)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Kind != FlowKindProject {
			t.Fatalf("loaded kind = %q", loaded.Kind)
		}
	})

	t.Run("an unknown kind is refused", func(t *testing.T) {
		flow := upgradeFixtureFlow("flow-badkind", "proj-badkind")
		flow.Kind = FlowKind("saga")
		err := store.Put(flow)
		if err == nil {
			t.Fatal("a Flow with an unknown kind was stored")
		}
		if !strings.Contains(err.Error(), "saga") {
			t.Fatalf("error does not name the kind: %v", err)
		}
		var n int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM flows WHERE id = ?`, flow.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatal("the refused Put wrote a row")
		}
	})
}

// TestFlowStoreKeepsPayloadAndColumnsConsistent is the mirror rule: the payload
// is the document and the columns are its copy, so a row where the two disagree
// is reported instead of read. Such a row can only be written by something that
// did not go through Put, which is exactly what the test does.
func TestFlowStoreKeepsPayloadAndColumnsConsistent(t *testing.T) {
	store, _ := newUpgradeTestStore(t)
	flow := upgradeFixtureFlow("flow-mirror", "proj-mirror")
	if err := store.Put(flow); err != nil {
		t.Fatal(err)
	}
	tamper := func(column, value string) {
		t.Helper()
		if _, err := store.db.Exec(`UPDATE flows SET `+column+` = ? WHERE id = ?`, value, flow.ID); err != nil {
			t.Fatalf("tamper with %s: %v", column, err)
		}
	}

	t.Run("a kind that disagrees with the document", func(t *testing.T) {
		tamper("kind", "task")
		defer tamper("kind", "project")

		_, err := store.Get(flow.ID)
		if err == nil {
			t.Fatal("Get returned a row whose kind disagrees with its document")
		}
		if !strings.Contains(err.Error(), "flow-mirror") || !strings.Contains(err.Error(), "kind") {
			t.Fatalf("error does not name the flow and the column: %v", err)
		}
		if _, err := store.List("proj-mirror"); err == nil {
			t.Fatal("List returned a row whose kind disagrees with its document")
		} else if !strings.Contains(err.Error(), "flow-mirror") {
			t.Fatalf("list error does not name the flow: %v", err)
		}
	})

	t.Run("a revision that disagrees with the document", func(t *testing.T) {
		tamper("revision", "99")
		defer tamper("revision", "1")

		_, err := store.Get(flow.ID)
		if err == nil {
			t.Fatal("Get returned a row whose revision disagrees with its document")
		}
		if !strings.Contains(err.Error(), "revision") {
			t.Fatalf("error does not name the revision: %v", err)
		}
	})
}

// TestFlowStoreMirrorsTheNewColumns covers the other half of a write: the values
// a document carries reach their columns, empty ones as NULL.
func TestFlowStoreMirrorsTheNewColumns(t *testing.T) {
	store, _ := newUpgradeTestStore(t)
	flow := upgradeFixtureFlow("flow-mirror-cols", "proj-mirror-cols")
	flow.Kind = FlowKindTask
	flow.BindingID = "binding-9"
	flow.TemplateRevision = 4
	flow.ParentProjectFlowID = "flow-parent"
	if err := store.Put(flow); err != nil {
		t.Fatal(err)
	}

	var (
		kind, binding, parent sql.NullString
		tmplRev               sql.NullInt64
	)
	err := store.db.QueryRow(`SELECT kind, binding_id, template_revision, parent_project_flow_id
		FROM flows WHERE id = ?`, flow.ID).Scan(&kind, &binding, &tmplRev, &parent)
	if err != nil {
		t.Fatal(err)
	}
	if kind.String != "task" || binding.String != "binding-9" || tmplRev.Int64 != 4 || parent.String != "flow-parent" {
		t.Fatalf("mirrored columns = %v %v %v %v", kind, binding, tmplRev, parent)
	}

	t.Run("empty values are NULL", func(t *testing.T) {
		plain := upgradeFixtureFlow("flow-nulls", "proj-mirror-cols")
		if err := store.Put(plain); err != nil {
			t.Fatal(err)
		}
		var (
			binding, parent sql.NullString
			tmplRev         sql.NullInt64
		)
		if err := store.db.QueryRow(`SELECT binding_id, template_revision, parent_project_flow_id
			FROM flows WHERE id = ?`, plain.ID).Scan(&binding, &tmplRev, &parent); err != nil {
			t.Fatal(err)
		}
		if binding.Valid || tmplRev.Valid || parent.Valid {
			t.Fatalf("empty values were stored as %v %v %v, want NULL", binding, tmplRev, parent)
		}
	})
}

// TestFlowStoreUpgradeFromPreT301Database is the migration test: a database
// written by the build before T3.01.a — its tables created from the DDL of that
// build, its rows inserted by hand — opens on this build, gains the columns,
// keeps every document byte for byte, keeps its outbox untouched, and can be
// opened again without repeating anything.
func TestFlowStoreUpgradeFromPreT301Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	store, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	// Rebuild the database as the earlier build left it: drop what this build
	// created and create the old tables instead.
	for _, table := range []string{"flows", "flow_templates", "flow_event_outbox"} {
		if _, err := store.db.Exec(`DROP TABLE ` + table); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}
	if _, err := store.db.Exec(legacySchemaDDL); err != nil {
		t.Fatalf("create the old schema: %v", err)
	}

	// One document in the oldest shape this build can still read, one written by
	// the build just before this one (session_id, snapshot_id, config and
	// created_by present, no kind or revision), and the outbox rows that belong
	// to them.
	v0 := readLegacyFixture(t, "v0_minimal")
	preT301 := []byte(`{
	  "id": "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
	  "project_id": "proj-legacy",
	  "session_id": "sess-legacy",
	  "template_id": "new_project",
	  "status": "completed",
	  "stages": [{
	    "id": "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
	    "type": "idea", "name": "想法", "canvas": "intent", "agent_id": "agent-1",
	    "status": "done", "optional": false, "snapshot_id": "snap-legacy",
	    "gates": [{"id": "ffffffff-ffff-4fff-8fff-ffffffffffff", "phase": "exit",
	      "kind": "human_approval", "on_fail": "escalate_to_debate",
	      "config": {"approver": "user"}, "passed": true}],
	    "order": 0
	  }],
	  "loops": [],
	  "artifacts": [{"id": "12121212-1212-4121-8121-121212121212",
	    "stage_id": "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", "type": "intent_doc",
	    "version": 1, "status": "approved", "created_by": "user",
	    "content_ref": "content://intent/v1", "created_at": "2025-06-01T00:00:00Z"}],
	  "events": [{"id": "13131313-1313-4131-8131-131313131313", "type": "flow.created",
	    "message": "created", "timestamp": "2025-06-01T00:00:00Z"}],
	  "created_at": "2025-06-01T00:00:00Z",
	  "updated_at": "2025-06-01T01:00:00Z"
	}`)

	insertLegacyRow := func(id, projectID, status string, payload []byte) {
		t.Helper()
		if _, err := store.db.Exec(`
INSERT INTO flows (id, project_id, template_id, status, payload, created_at, updated_at)
VALUES (?, ?, 'new_project', ?, ?, 1735689600000, 1735689600000)`,
			id, projectID, status, string(payload)); err != nil {
			t.Fatalf("insert legacy row %s: %v", id, err)
		}
	}
	insertLegacyRow("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "proj-legacy", "active", v0)
	insertLegacyRow("dddddddd-dddd-4ddd-8ddd-dddddddddddd", "proj-legacy", "completed", preT301)

	if _, err := store.db.Exec(`
INSERT INTO flow_event_outbox
  (source_event_id, flow_id, project_id, event_type, stage_id, message,
   occurred_at, state, attempt_count, next_attempt_at, created_at)
VALUES ('ffffffff-ffff-4fff-8fff-ffffffffffff', 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'proj-legacy',
        'flow.created', '', 'created', 1735689600000, 'pending', 0, 1735689600000, 1735689600000)`); err != nil {
		t.Fatalf("insert legacy outbox row: %v", err)
	}
	outboxBefore := legacyOutboxCount(t, store)
	if outboxBefore != 1 {
		t.Fatalf("outbox rows before the upgrade = %d", outboxBefore)
	}
	payloadBefore := string(v0)
	updatedAtBefore := int64(1735689600000)
	// Leave the write lock so the reopen below is a real second connection.
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Open the old database with this build.
	upgraded, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatalf("open an older database: %v", err)
	}
	defer upgraded.Close()

	t.Run("the columns were added", func(t *testing.T) {
		cols := columnsOf(t, upgraded, "flows")
		for _, name := range []string{"kind", "revision", "binding_id", "template_revision", "parent_project_flow_id"} {
			if _, ok := cols[name]; !ok {
				t.Fatalf("%s was not added: %v", name, sortedSpecKeys(cols))
			}
		}
	})

	t.Run("every old row is a project flow at revision 1", func(t *testing.T) {
		rows, err := upgraded.db.Query(`SELECT id, kind, revision FROM flows ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		seen := 0
		for rows.Next() {
			var id, kind string
			var revision int64
			if err := rows.Scan(&id, &kind, &revision); err != nil {
				t.Fatal(err)
			}
			seen++
			if kind != "project" || revision != 1 {
				t.Fatalf("row %s = kind %q revision %d, want project at revision 1", id, kind, revision)
			}
		}
		if seen != 2 {
			t.Fatalf("rows = %d, want the 2 that were there", seen)
		}
	})

	t.Run("the documents read back unchanged", func(t *testing.T) {
		loaded, err := upgraded.Get("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
		if err != nil {
			t.Fatalf("get v0 document: %v", err)
		}
		if loaded.Kind != FlowKindProject || loaded.Revision != 1 {
			t.Fatalf("v0 document decoded as kind %q revision %d", loaded.Kind, loaded.Revision)
		}
		if loaded.ProjectID != "proj-legacy" || loaded.Status != FlowStatusActive {
			t.Fatalf("v0 document changed: %+v", loaded)
		}
		if loaded.Stages[1].ID != "dddddddd-dddd-4ddd-8ddd-dddddddddddd" {
			t.Fatalf("v0 stages changed: %+v", loaded.Stages)
		}

		list, err := upgraded.List("proj-legacy")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("list returned %d flows, want 2", len(list))
		}
		for _, f := range list {
			if f.Kind != FlowKindProject || f.Revision != 1 {
				t.Fatalf("listed flow %s = kind %q revision %d", f.ID, f.Kind, f.Revision)
			}
		}

		readBack, err := upgraded.Get("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
		if err != nil {
			t.Fatalf("get the pre-T3.01 document: %v", err)
		}
		if readBack.SessionID != "sess-legacy" || readBack.Stages[0].SnapshotID != "snap-legacy" {
			t.Fatalf("members added before T3.01 were lost: %+v", readBack)
		}
		if readBack.Stages[0].Gates[0].OnFail != GateOnFailEscalateDebate ||
			readBack.Stages[0].Gates[0].Config["approver"] != "user" {
			t.Fatalf("gate members were lost: %+v", readBack.Stages[0].Gates[0])
		}
		if readBack.Artifacts[0].CreatedBy != ArtifactCreatorUser ||
			readBack.Artifacts[0].ContentRef != "content://intent/v1" {
			t.Fatalf("artifact members were lost: %+v", readBack.Artifacts[0])
		}
	})

	t.Run("the payload is the bytes it was", func(t *testing.T) {
		payload, updatedAt := upgradeStoredRow(t, upgraded, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
		if payload != payloadBefore {
			t.Fatalf("opening the database rewrote the payload:\n before %s\n  after %s", payloadBefore, payload)
		}
		if updatedAt != updatedAtBefore {
			t.Fatalf("opening the database changed updated_at: %d -> %d", updatedAtBefore, updatedAt)
		}
	})

	t.Run("opening wrote no outbox row", func(t *testing.T) {
		if got := legacyOutboxCount(t, upgraded); got != outboxBefore {
			t.Fatalf("outbox rows after opening an old database = %d, want %d", got, outboxBefore)
		}
	})

	t.Run("opening again changes nothing", func(t *testing.T) {
		second, err := NewSQLiteFlowStore(path)
		if err != nil {
			t.Fatalf("second open: %v", err)
		}
		defer second.Close()
		payload, _ := upgradeStoredRow(t, second, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
		if payload != payloadBefore {
			t.Fatal("the second open rewrote the payload")
		}
		if got := legacyOutboxCount(t, second); got != outboxBefore {
			t.Fatalf("outbox rows after the second open = %d", got)
		}
		if got := mirrorOf(t, second, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"); got.kind != "project" || got.revision != 1 {
			t.Fatalf("the second open changed the row: %+v", got)
		}
	})

	t.Run("the migrated table matches a fresh one", func(t *testing.T) {
		fresh, _ := newUpgradeTestStore(t)
		migrated, freshCols := columnsOf(t, upgraded, "flows"), columnsOf(t, fresh, "flows")
		if len(migrated) != len(freshCols) {
			t.Fatalf("column counts differ: migrated %v, fresh %v", sortedSpecKeys(migrated), sortedSpecKeys(freshCols))
		}
		for name, wantSpec := range freshCols {
			if got, ok := migrated[name]; !ok || got != wantSpec {
				t.Fatalf("column %s: migrated %q, fresh %q", name, migrated[name], wantSpec)
			}
		}
	})

	t.Run("a document written after the upgrade is a revision 2", func(t *testing.T) {
		loaded, err := upgraded.Get("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
		if err != nil {
			t.Fatal(err)
		}
		loaded.Status = FlowStatusAborted
		if err := upgraded.Put(loaded); err != nil {
			t.Fatalf("put after upgrade: %v", err)
		}
		if loaded.Revision != 2 {
			t.Fatalf("revision after the first write = %d, want 2", loaded.Revision)
		}
		if got := legacyOutboxCount(t, upgraded); got != outboxBefore {
			t.Fatalf("the write queued a row for history that was already there: %d -> %d", outboxBefore, got)
		}
	})
}

// TestFlowStoreUpgradePreservesUnknownFieldsThroughSqlite is the end-to-end
// version of the codec rule: a document with members this build does not know
// goes in, comes back out of SQLite with them, is advanced by the engine, and
// still has them — including the members on objects the advance did not touch.
func TestFlowStoreUpgradePreservesUnknownFieldsThroughSqlite(t *testing.T) {
	store, _ := newUpgradeTestStore(t)
	body := readLegacyFixture(t, "unknown_fields")

	var flow Flow
	if err := json.Unmarshal(body, &flow); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if err := store.Put(&flow); err != nil {
		t.Fatalf("put: %v", err)
	}
	if flow.Revision != 1 {
		t.Fatalf("revision after the first Put = %d", flow.Revision)
	}

	t.Run("the preserved members survive a round trip through the database", func(t *testing.T) {
		loaded, err := store.Get(flow.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertSameExtras(t, &flow, loaded)
		if string(loaded.extras["big_number"]) != `12345678901234567890.5` {
			t.Fatalf("the high-precision number changed: %s", loaded.extras["big_number"])
		}
		if loaded.Status != FlowStatusActive {
			t.Fatalf("the case variant won over the canonical status: %q", loaded.Status)
		}
		if len(loaded.Stages) != 2 || loaded.Stages[1].Order != 1 {
			t.Fatalf("the case variant won over the canonical order: %+v", loaded.Stages[1])
		}

		// List goes through the same decode and the same mirror check, so what
		// it returns must carry the members too.
		list, err := store.List(flow.ProjectID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(list) != 1 {
			t.Fatalf("list returned %d flows, want 1", len(list))
		}
		assertSameExtras(t, &flow, list[0])
	})

	t.Run("the outbox has only the events this document already had", func(t *testing.T) {
		// The fixture's event is new to the database, so it is queued once; the
		// point is that decoding and re-encoding the document did not invent or
		// lose one.
		if got := legacyOutboxCount(t, store); got != len(flow.Events) {
			t.Fatalf("outbox rows = %d, want one per event (%d)", got, len(flow.Events))
		}
	})

	t.Run("an engine operation keeps them and moves the revision", func(t *testing.T) {
		eng := NewEngineWithStore(store, nil)

		// The fixture's active stage has an exit gate that has not passed, so the
		// engine is asked to decide it — a normal operation that rewrites the
		// document — and the advance then succeeds. Both write a revision, which
		// is why the advance lands on 3.
		before, err := eng.Get(context.Background(), flow.ID)
		if err != nil {
			t.Fatal(err)
		}
		active := activeStage(before)
		if active == nil {
			t.Fatal("the fixture has no active stage")
		}
		approved := true
		decided, err := eng.DecideGate(context.Background(), flow.ID,
			active.Gates[0].ID, &GateDecisionRequest{Approved: &approved, Reason: "ok"})
		if err != nil {
			t.Fatalf("decide gate: %v", err)
		}
		if decided.Revision != 2 {
			t.Fatalf("revision after deciding a gate = %d, want 2", decided.Revision)
		}

		advanced, err := eng.Advance(context.Background(), flow.ID, &AdvanceRequest{})
		if err != nil {
			t.Fatalf("advance: %v", err)
		}
		if advanced.Revision != 3 {
			t.Fatalf("revision after advance = %d, want 3", advanced.Revision)
		}
		if advanced.Kind != FlowKindProject {
			t.Fatalf("kind after advance = %q", advanced.Kind)
		}

		// The objects the advance did not touch kept every member, and the
		// objects it did touch — the active stage and the flow itself — kept the
		// members that are not the fields the engine writes.
		loaded, err := eng.Get(context.Background(), flow.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertSameExtras(t, &flow, loaded)
		if loaded.Stages[0].extras["future_stage_key"] == nil ||
			loaded.Stages[0].Gates[0].extras["future_gate_key"] == nil {
			t.Fatalf("an untouched stage lost its members: %+v", loaded.Stages[0])
		}
		if loaded.Artifacts[0].extras["future_artifact_key"] == nil {
			t.Fatalf("the artifact lost its members: %+v", loaded.Artifacts[0])
		}
		if loaded.Loops[0].extras["future_loop_key"] == nil {
			t.Fatalf("the loop edge lost its members: %+v", loaded.Loops[0])
		}
		if len(loaded.Events) < len(flow.Events) {
			t.Fatal("the advance lost events")
		}
		if level, ok := findExtraLevel(t, loaded, "event "+flow.Events[0].ID); !ok ||
			level["future_event_key"] == nil {
			t.Fatalf("the original event lost its members: %v", level)
		}

		// The new events are queued, the old ones are not queued twice.
		if got, want := legacyOutboxCount(t, store), len(loaded.Events); got != want {
			t.Fatalf("outbox rows after the advance = %d, want %d (one per event, none repeated)", got, want)
		}
	})
}

// assertSameExtras fails if the preserved members of two documents differ, at
// every level. A member the fixture writes without insignificant whitespace and
// without an HTML-escapable character must come back byte for byte; the rest are
// compared by value, which is the boundary the codec documents (see
// legacy_json.go).
func assertSameExtras(t *testing.T, want, got *Flow) {
	t.Helper()
	compare := func(name, key string, wantRaw json.RawMessage, second map[string]json.RawMessage) {
		t.Helper()
		gotRaw, ok := second[key]
		if !ok {
			t.Fatalf("%s member %q disappeared", name, key)
		}
		if bytes.Equal(wantRaw, gotRaw) {
			return
		}
		normalised := hasInsignificantWhitespace(wantRaw) || bytes.ContainsAny(wantRaw, "<>&")
		if normalised && sameJSONValue(t, wantRaw, gotRaw) {
			return
		}
		t.Fatalf("%s member %q changed:\n got %s\nwant %s", name, key, gotRaw, wantRaw)
	}

	if len(want.extras) != len(got.extras) {
		t.Fatalf("flow members changed: %v -> %v", keysOf(want.extras), keysOf(got.extras))
	}
	for key, wantRaw := range want.extras {
		compare("flow", key, wantRaw, got.extras)
	}
	for _, c := range extraLevels(want, got) {
		if len(c.first) != len(c.second) {
			t.Fatalf("%s members changed: %v -> %v", c.name, keysOf(c.first), keysOf(c.second))
		}
		for key, wantRaw := range c.first {
			compare(c.name, key, wantRaw, c.second)
		}
	}
}
