package floweng

// Tests for the store half of T3.01.a group 2: the one-time data migration that
// brings a database written before the one-active-project-flow rule into line
// with it, the receipt it leaves behind, and the partial unique index that keeps
// it that way.
//
// The fixture is the real thing, not a guess at it: a database whose flows table
// is (re)created with the schema the build before group 1 created, plus the
// columns group 1 added — the exact shape an existing installation has — filled
// with rows written by hand, the way an older build wrote them.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/dbx"
)

// flowDocumentsTableDDL is the schema of a database written before T3.01.a
// group 2 but after group 1: group 1's document columns are there, group 2's
// index and receipt table are not. That is the shape an installation upgraded by
// the previous commit has, and the one the migration has to repair. It is built
// from legacySchemaDDL (the pre-group-1 tables) plus group 1's ADD COLUMNs, so
// the two upgrade tests cannot drift apart.
func flowDocumentsTableDDL(t *testing.T, db *sql.DB) string {
	t.Helper()
	ddl := strings.Replace(legacySchemaDDL, `CREATE TABLE flows (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  template_id TEXT NOT NULL,
  status TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);`, `CREATE TABLE flows (
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
);`, 1)
	if ddl == legacySchemaDDL {
		t.Fatal("the pre-group-2 flows table could not be built from legacySchemaDDL: the two have drifted apart")
	}
	return ddl
}

// migrationSeed is one hand-written legacy row: a document in the old shape
// plus the two timestamps the choice between several active flows is made on.
// The document's own created_at/updated_at carry the same instants, because the
// engine reads those while the migration reads the columns.
type migrationSeed struct {
	id        string
	projectID string
	status    string
	createdAt time.Time
	updatedAt time.Time
}

// migrationPayload renders one legacy document. It deliberately uses a plain
// map: the fixture is what an older build wrote, not what this build's codec
// would write, and it carries a member this build does not know
// (project_next_build_key) so the migration can be shown not to lose it.
func migrationPayload(t *testing.T, seed migrationSeed) []byte {
	t.Helper()
	stageID := seed.id + "-stage"
	doc := map[string]any{
		"id":          seed.id,
		"project_id":  seed.projectID,
		"template_id": "new_project",
		"status":      seed.status,
		"stages": []any{map[string]any{
			"id": stageID, "type": "idea", "name": "想法 " + seed.id, "canvas": "intent",
			"status": "active", "optional": false, "order": 0,
			"gates": []any{map[string]any{
				"id": seed.id + "-gate", "phase": "exit", "kind": "human_approval", "passed": false,
				"future_gate_key": "kept-" + seed.id,
			}},
		}},
		"loops": []any{},
		"artifacts": []any{map[string]any{
			"id": seed.id + "-art", "stage_id": stageID, "type": "intent_doc", "version": 1,
			"status": "draft", "content_ref": "content://" + seed.id, "created_at": seed.createdAt,
		}},
		"events": []any{map[string]any{
			"id": seed.id + "-ev", "type": "flow.created", "message": "created",
			"timestamp": seed.createdAt,
		}},
		"created_at":             seed.createdAt,
		"updated_at":             seed.updatedAt,
		"project_next_build_key": "kept-" + seed.id,
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode legacy document %s: %v", seed.id, err)
	}
	return body
}

// newLegacyGroup2Database builds a database of the shape this step upgrades and
// returns its path with the payload bytes of every row. The store it returns is
// closed: the test is about opening the database on this build.
func newLegacyGroup2Database(t *testing.T, seeds []migrationSeed) (string, map[string][]byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")
	store, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	for _, table := range []string{"flows", "flow_templates", "flow_event_outbox", "flow_store_migrations"} {
		if _, err := store.db.Exec(`DROP TABLE ` + table); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}
	if _, err := store.db.Exec(flowDocumentsTableDDL(t, store.db)); err != nil {
		t.Fatalf("create the older schema: %v", err)
	}

	payloads := make(map[string][]byte, len(seeds))
	for _, seed := range seeds {
		payload := migrationPayload(t, seed)
		payloads[seed.id] = payload
		if _, err := store.db.Exec(`
INSERT INTO flows (id, project_id, template_id, status, payload, created_at, updated_at, kind, revision)
VALUES (?, ?, 'new_project', ?, ?, ?, ?, 'project', 1)`,
			seed.id, seed.projectID, seed.status, string(payload),
			seed.createdAt.UnixMilli(), seed.updatedAt.UnixMilli()); err != nil {
			t.Fatalf("insert legacy row %s: %v", seed.id, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return path, payloads
}

// migrationFixtureSeeds is the four projects the rule has to get right, plus one
// that is already fine.
//
//	proj-a  three active flows, all three timestamps different
//	proj-b  two active flows with the same updated_at, different created_at
//	proj-c  two active flows with the same updated_at and created_at, so the id decides
//	proj-d  exactly one active flow, plus a completed and an aborted one
func migrationFixtureSeeds() []migrationSeed {
	at := func(ms int64) time.Time { return time.UnixMilli(ms).UTC() }
	return []migrationSeed{
		// proj-a: the newest updated_at wins, the other two are suspended
		// oldest-first.
		{id: "a-old", projectID: "proj-a", status: "active", createdAt: at(1_000), updatedAt: at(2_000)},
		{id: "a-mid", projectID: "proj-a", status: "active", createdAt: at(1_500), updatedAt: at(3_000)},
		{id: "a-new", projectID: "proj-a", status: "active", createdAt: at(1_100), updatedAt: at(4_000)},
		{id: "a-done", projectID: "proj-a", status: "completed", createdAt: at(500), updatedAt: at(5_000)},

		// proj-b: updated_at ties, so the greater created_at wins.
		{id: "b-x", projectID: "proj-b", status: "active", createdAt: at(6_000), updatedAt: at(9_000)},
		{id: "b-y", projectID: "proj-b", status: "active", createdAt: at(7_000), updatedAt: at(9_000)},

		// proj-c: both timestamps tie, so the smallest id wins.
		{id: "c-1", projectID: "proj-c", status: "active", createdAt: at(11_000), updatedAt: at(12_000)},
		{id: "c-2", projectID: "proj-c", status: "active", createdAt: at(11_000), updatedAt: at(12_000)},

		// proj-d: nothing to do.
		{id: "d-1", projectID: "proj-d", status: "active", createdAt: at(21_000), updatedAt: at(22_000)},
		{id: "d-done", projectID: "proj-d", status: "completed", createdAt: at(20_000), updatedAt: at(23_000)},
		{id: "d-aborted", projectID: "proj-d", status: "aborted", createdAt: at(19_000), updatedAt: at(24_000)},
	}
}

// singleActiveFlowMigrationWant is what the fixture above must produce.
var singleActiveFlowMigrationWant = map[string][]string{
	// project -> the flows that must be suspended, in receipt order
	"proj-a": {"a-old", "a-mid"},
	"proj-b": {"b-x"},
	"proj-c": {"c-2"},
	"proj-d": {},
}
var singleActiveFlowMigrationPrimary = map[string]string{
	"proj-a": "a-new",
	"proj-b": "b-y",
	"proj-c": "c-1",
	"proj-d": "d-1",
}

// TestSingleActiveProjectFlowMigration is the migration: a database holding
// several active project flows opens on this build, every project is left with
// exactly one active project flow — the one the rule chooses — the others are
// suspended with everything they carried intact and one flow.suspended event
// queued for the projector, the receipt says what happened, and opening again
// changes nothing.
func TestSingleActiveProjectFlowMigration(t *testing.T) {
	seeds := migrationFixtureSeeds()
	path, payloadsBefore := newLegacyGroup2Database(t, seeds)

	// One outbox row is already there, for a flow the migration does not touch.
	// Its bytes and its seq must survive the migration untouched.
	unrelatedEventID := "d-done" + "-ev"
	prepare, err := dbx.Open(path, dbx.WithMaxOpenConns(1))
	if err != nil {
		t.Fatalf("open for seeding: %v", err)
	}
	if _, err := prepare.Exec(`
INSERT INTO flow_event_outbox
  (source_event_id, flow_id, project_id, event_type, stage_id, message,
   occurred_at, state, attempt_count, next_attempt_at, created_at)
VALUES (?, 'd-done', 'proj-d', 'flow.created', '', 'created', 1, 'pending', 0, 1, 1)`, unrelatedEventID); err != nil {
		t.Fatalf("seed outbox row: %v", err)
	}
	if err := prepare.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatalf("open an older database: %v", err)
	}
	defer upgraded.Close()

	// The outbox must hold the seeded row plus one row per suspended flow, and
	// the suspended rows must name the flow.suspended events the documents
	// carry.
	seededRow, ok := outboxRow(t, upgraded, unrelatedEventID)
	if !ok {
		t.Fatal("the seeded outbox row disappeared")
	}

	t.Run("every project keeps one active project flow, the one the rule chooses", func(t *testing.T) {
		for projectID, primary := range singleActiveFlowMigrationPrimary {
			rows := activeProjectFlowsOf(t, upgraded, projectID)
			if len(rows) != 1 || rows[0] != primary {
				t.Fatalf("%s active project flows = %v, want exactly [%s]", projectID, rows, primary)
			}
		}
	})

	t.Run("the other active project flows are suspended, not aborted and not task flows", func(t *testing.T) {
		for projectID, suspended := range singleActiveFlowMigrationWant {
			got := suspendedFlowsOf(t, upgraded, projectID)
			if strings.Join(got, ",") != strings.Join(suspended, ",") {
				t.Fatalf("%s suspended = %v, want %v", projectID, got, suspended)
			}
		}
		// The completed and aborted flows kept their status: the migration only
		// ever parks an active one.
		for id, want := range map[string]string{"a-done": "completed", "d-done": "completed", "d-aborted": "aborted"} {
			flow, err := upgraded.Get(id)
			if err != nil {
				t.Fatalf("get %s: %v", id, err)
			}
			if string(flow.Status) != want {
				t.Fatalf("%s status = %q, want %q", id, flow.Status, want)
			}
		}
	})

	t.Run("a suspended flow carries its stages, artifacts and events unchanged", func(t *testing.T) {
		flow, err := upgraded.Get("a-mid")
		if err != nil {
			t.Fatalf("get suspended flow: %v", err)
		}
		before := decodeSeedDocument(t, payloadsBefore["a-mid"])
		if len(flow.Stages) != 1 || len(before.Stages) != 1 {
			t.Fatalf("stages: %d now, %d before", len(flow.Stages), len(before.Stages))
		}
		nowStages, _ := json.Marshal(flow.Stages)
		beforeStages, _ := json.Marshal(before.Stages)
		if string(nowStages) != string(beforeStages) {
			t.Fatalf("stages changed:\n now %s\nwas %s", nowStages, beforeStages)
		}
		if len(flow.Artifacts) != len(before.Artifacts) || flow.Artifacts[0].ContentRef != before.Artifacts[0].ContentRef {
			t.Fatalf("artifacts changed: %+v", flow.Artifacts)
		}
		if len(flow.Loops) != len(before.Loops) {
			t.Fatalf("loops changed: %+v", flow.Loops)
		}
		// Exactly one event was added, it is the suspension, and it names both
		// the reason and the flow that took the slot.
		if len(flow.Events) != len(before.Events)+1 {
			t.Fatalf("events = %v, want the original plus flow.suspended", flowEventTypes(flow.Events))
		}
		ev := flow.Events[len(flow.Events)-1]
		if ev.Type != "flow.suspended" {
			t.Fatalf("the added event is %q, want flow.suspended", ev.Type)
		}
		if !strings.Contains(ev.Message, suspendedProjectFlowReason) || !strings.Contains(ev.Message, "a-new") {
			t.Fatalf("the suspension event does not explain itself: %q", ev.Message)
		}
		if ev.ID == "" {
			t.Fatal("the suspension event has no id, so it cannot be projected")
		}
		if ev.Timestamp.IsZero() {
			t.Fatal("the suspension event has no timestamp")
		}
	})

	t.Run("nothing but the written members moved", func(t *testing.T) {
		// The write is one ordinary store write: the status leaves active, one
		// event is appended, updated_at moves, and the document now names the
		// kind and revision it is stored with (group 1's rule for every
		// document this build writes). The group-3 template-revision backfill
		// then writes each flow once more (it is written at open time, after
		// the group-2 migration), which additionally sets template_revision and
		// moves the revision to 3. Everything else — including the member this
		// build does not know — is what the older build wrote.
		written := map[string]bool{"status": true, "revision": true, "kind": true, "events": true, "updated_at": true, "template_revision": true}
		for _, id := range []string{"a-old", "a-mid", "b-x", "c-2"} {
			nowPayload, _ := upgradeStoredRow(t, upgraded, id)
			beforeMembers := membersOf(t, payloadsBefore[id])
			nowMembers := membersOf(t, []byte(nowPayload))
			for key := range nowMembers {
				if written[key] {
					continue
				}
				if _, was := beforeMembers[key]; !was {
					t.Fatalf("%s gained a member %q the older build never wrote", id, key)
				}
			}
			for key, want := range beforeMembers {
				if written[key] {
					continue
				}
				got, ok := nowMembers[key]
				if !ok {
					t.Fatalf("%s lost member %q", id, key)
				}
				// The document is re-encoded by this build's codec, which is
				// what keeps an unknown member: it writes the known fields in
				// its own order, so the comparison is by value, and the round
				// trip rule (see legacy_json.go) is what the byte-for-byte case
				// above the loop tests.
				if !sameJSONValue(t, want, got) {
					t.Fatalf("%s member %q changed:\n now %s\nwas %s", id, key, got, want)
				}
			}
			if string(nowMembers["project_next_build_key"]) != string(beforeMembers["project_next_build_key"]) {
				t.Fatalf("%s lost the member this build does not know", id)
			}
			// The unknown member nested inside a stage's gate survived too.
			flow, err := upgraded.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if len(flow.Stages) != 1 || len(flow.Stages[0].Gates) != 1 {
				t.Fatalf("%s stage/gates changed: %+v", id, flow.Stages)
			}
			if got := string(flow.Stages[0].Gates[0].extras["future_gate_key"]); got != `"kept-`+id+`"` {
				t.Fatalf("%s lost the gate member this build does not know: %q", id, got)
			}
			if flow.Stages[0].Gates[0].ID != id+"-gate" || flow.Stages[0].Gates[0].Phase != GatePhaseExit {
				t.Fatalf("%s gate changed: %+v", id, flow.Stages[0].Gates[0])
			}
			if string(nowMembers["kind"]) != `"project"` {
				t.Fatalf("%s kind = %s, want project", id, nowMembers["kind"])
			}
			if string(nowMembers["status"]) != `"suspended"` {
				t.Fatalf("%s status = %s, want suspended", id, nowMembers["status"])
			}
			if string(nowMembers["template_revision"]) != "1" {
				t.Fatalf("%s template_revision = %s, want 1 (the template's migration-time revision)",
					id, nowMembers["template_revision"])
			}
		}
	})

	t.Run("the untouched rows keep their members and their updated_at", func(t *testing.T) {
		// The template-revision backfill writes these rows once (they carry no
		// revision), so only the members that backfill writes may differ:
		// template_revision, the revision counter, and kind (the migration never
		// touched these rows, so they never got group 1's kind member until the
		// backfill re-encoded them). In particular updated_at — the tiebreak the
		// group-2 migration above used to choose each project's flow of record —
		// is exactly what the older build stored.
		for _, id := range []string{"a-new", "b-y", "c-1", "d-1", "a-done", "d-done", "d-aborted"} {
			payload, updatedAt := upgradeStoredRow(t, upgraded, id)
			assertFlowPayloadPreservedExcept(t, payloadsBefore[id], []byte(payload), "revision", "template_revision", "kind")
			doc := decodeSeedDocument(t, payloadsBefore[id])
			if updatedAt != doc.UpdatedAt.UnixMilli() {
				t.Fatalf("row %s updated_at = %d, want the stored %d", id, updatedAt, doc.UpdatedAt.UnixMilli())
			}
		}
	})

	t.Run("a suspended flow was written twice: the migration and the backfill", func(t *testing.T) {
		for _, id := range []string{"a-old", "a-mid", "b-x", "c-2"} {
			mirror := mirrorOf(t, upgraded, id)
			if mirror.revision != 3 {
				t.Fatalf("%s revision = %d, want 3 (2 for the suspension, 3 for the template-revision backfill)", id, mirror.revision)
			}
			if mirror.kind != "project" {
				t.Fatalf("%s kind = %q, want project", id, mirror.kind)
			}
		}
		for _, id := range []string{"a-new", "b-y", "c-1", "d-1"} {
			if got := mirrorOf(t, upgraded, id).revision; got != 2 {
				t.Fatalf("%s revision = %d, want 2 (the template-revision backfill only)", id, got)
			}
		}
	})

	t.Run("every old row gained a template revision and kept its updated_at", func(t *testing.T) {
		// The backfill must not move updated_at: it is the tiebreak the group-2
		// migration above chose each project's flow of record by, and what the UI
		// sorts by, so it must stay what the older build wrote. The rows the
		// migration suspended are the documented exception — the suspension is
		// user-visible activity from an earlier accepted step and stamped a new
		// time — and what the backfill must leave alone there is that stamp. The
		// suspension writes the flow and the flow.suspended event with the same
		// instant in one transaction, so comparing the two shows whether anything
		// after it moved the time.
		for _, seed := range seeds {
			flow, err := upgraded.Get(seed.id)
			if err != nil {
				t.Fatal(err)
			}
			if flow.TemplateID == "" {
				continue
			}
			if flow.TemplateRevision != 1 {
				t.Fatalf("%s template_revision = %d, want 1", seed.id, flow.TemplateRevision)
			}
			if flow.Status == FlowStatusSuspended {
				ev := flow.Events[len(flow.Events)-1]
				if ev.Type != "flow.suspended" {
					t.Fatalf("%s last event = %q, want flow.suspended", seed.id, ev.Type)
				}
				if !flow.UpdatedAt.Equal(ev.Timestamp) {
					t.Fatalf("%s updated_at = %v but its suspension was written at %v: something after the suspension moved it",
						seed.id, flow.UpdatedAt, ev.Timestamp)
				}
				continue
			}
			if flow.UpdatedAt.UnixMilli() != seed.updatedAt.UnixMilli() {
				t.Fatalf("%s updated_at = %v, want the stored %v", seed.id, flow.UpdatedAt, seed.updatedAt)
			}
		}
	})

	t.Run("each suspension queued exactly one outbox row", func(t *testing.T) {
		rows := allOutboxRows(t, upgraded)
		if len(rows) != 1+4 {
			t.Fatalf("outbox rows = %d, want the seeded one plus four (%v)", len(rows), rows)
		}
		if rows[0].sourceEventID != unrelatedEventID || rows[0].seq != seededRow.seq {
			t.Fatalf("the seeded outbox row moved: %+v", rows[0])
		}
		for _, id := range []string{"a-old", "a-mid", "b-x", "c-2"} {
			flow, err := upgraded.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			ev := flow.Events[len(flow.Events)-1]
			row, ok := outboxRow(t, upgraded, ev.ID)
			if !ok {
				t.Fatalf("no outbox row for the suspension event of %s", id)
			}
			if row.flowID != id || row.eventType != "flow.suspended" || row.state != "pending" {
				t.Fatalf("outbox row for %s = %+v", id, row)
			}
			if row.message != ev.Message {
				t.Fatalf("outbox message for %s = %q, want %q", id, row.message, ev.Message)
			}
		}
	})

	t.Run("the receipt says which project kept which flow and what was parked", func(t *testing.T) {
		applied, err := upgraded.FindAppliedSingleActiveProjectFlowMigration()
		if err != nil {
			t.Fatalf("read receipt: %v", err)
		}
		if applied == nil {
			t.Fatal("the migration recorded no receipt")
		}
		if applied.Name != singleActiveProjectFlowMigration {
			t.Fatalf("receipt name = %q", applied.Name)
		}
		if applied.AppliedAt.IsZero() || applied.AppliedAt.After(time.Now()) {
			t.Fatalf("receipt applied_at = %v", applied.AppliedAt)
		}
		var receipt SingleActiveProjectFlowReceipt
		if err := json.Unmarshal([]byte(applied.Receipt), &receipt); err != nil {
			t.Fatalf("decode receipt: %v (%s)", err, applied.Receipt)
		}
		if receipt.Reason != suspendedProjectFlowReason {
			t.Fatalf("receipt reason = %q", receipt.Reason)
		}
		if len(receipt.Projects) != 3 {
			t.Fatalf("receipt projects = %d, want the 3 that had extras: %s", len(receipt.Projects), applied.Receipt)
		}
		byProject := map[string]SingleActiveProjectFlowProject{}
		for _, p := range receipt.Projects {
			byProject[p.ProjectID] = p
		}
		for projectID, wantSuspended := range singleActiveFlowMigrationWant {
			if len(wantSuspended) == 0 {
				if _, found := byProject[projectID]; found {
					t.Fatalf("%s needed no migration but is in the receipt", projectID)
				}
				continue
			}
			entry, found := byProject[projectID]
			if !found {
				t.Fatalf("%s is missing from the receipt: %s", projectID, applied.Receipt)
			}
			if entry.PrimaryFlowID != singleActiveFlowMigrationPrimary[projectID] {
				t.Fatalf("%s primary = %q, want %q", projectID, entry.PrimaryFlowID, singleActiveFlowMigrationPrimary[projectID])
			}
			var ids []string
			for _, s := range entry.Suspended {
				ids = append(ids, s.FlowID)
				if s.PrimaryFlowID != entry.PrimaryFlowID {
					t.Fatalf("%s suspended entry names %q, want the primary %q", projectID, s.PrimaryFlowID, entry.PrimaryFlowID)
				}
			}
			if strings.Join(ids, ",") != strings.Join(wantSuspended, ",") {
				t.Fatalf("%s suspended in receipt = %v, want %v", projectID, ids, wantSuspended)
			}
		}
	})

	t.Run("the partial unique index exists and takes effect", func(t *testing.T) {
		var indexSQL string
		err := upgraded.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`,
			oneActiveProjectFlowIndexName).Scan(&indexSQL)
		if err == sql.ErrNoRows {
			t.Fatal("the one-active-project-flow index was not created")
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(indexSQL, "WHERE kind='project' AND status='active'") {
			t.Fatalf("the index is not partial over the project/active rows: %s", indexSQL)
		}
		// A second active project flow for a migrated project is refused by the
		// database itself.
		if _, err := upgraded.db.Exec(`
INSERT INTO flows (id, project_id, template_id, status, payload, created_at, updated_at, kind, revision)
VALUES ('intruder', 'proj-a', 'new_project', 'active', '{}', 0, 0, 'project', 1)`); err == nil {
			t.Fatal("the index let a second active project flow in")
		}
		// The same insert in any other shape is accepted: a task flow, a
		// suspended project flow, another project, and a flow of a project that
		// has no active project flow.
		for _, stmt := range []string{
			`INSERT INTO flows (id, project_id, template_id, status, payload, created_at, updated_at, kind, revision)
			 VALUES ('task-1', 'proj-a', 'new_project', 'active', '{}', 0, 0, 'task', 1)`,
			`INSERT INTO flows (id, project_id, template_id, status, payload, created_at, updated_at, kind, revision)
			 VALUES ('susp-1', 'proj-a', 'new_project', 'suspended', '{}', 0, 0, 'project', 1)`,
			`INSERT INTO flows (id, project_id, template_id, status, payload, created_at, updated_at, kind, revision)
			 VALUES ('other-1', 'proj-zzz', 'new_project', 'active', '{}', 0, 0, 'project', 1)`,
		} {
			if _, err := upgraded.db.Exec(stmt); err != nil {
				t.Fatalf("the index refused a row it must allow: %v", err)
			}
		}
	})

	t.Run("opening again suspends nothing, queues nothing and keeps the receipt", func(t *testing.T) {
		before, err := upgraded.FindAppliedSingleActiveProjectFlowMigration()
		if err != nil {
			t.Fatal(err)
		}
		outboxBefore := len(allOutboxRows(t, upgraded))
		revisionsBefore := map[string]int64{}
		for _, seed := range seeds {
			revisionsBefore[seed.id] = mirrorOf(t, upgraded, seed.id).revision
		}
		if err := upgraded.Close(); err != nil {
			t.Fatal(err)
		}

		second, err := NewSQLiteFlowStore(path)
		if err != nil {
			t.Fatalf("second open: %v", err)
		}
		defer second.Close()

		after, err := second.FindAppliedSingleActiveProjectFlowMigration()
		if err != nil {
			t.Fatal(err)
		}
		if after == nil || before == nil || after.Receipt != before.Receipt || !after.AppliedAt.Equal(before.AppliedAt) {
			t.Fatalf("the receipt changed on reopen:\n was %+v\n now %+v", before, after)
		}
		if got := len(allOutboxRows(t, second)); got != outboxBefore {
			t.Fatalf("outbox rows after the second open = %d, want %d", got, outboxBefore)
		}
		for id, want := range revisionsBefore {
			if got := mirrorOf(t, second, id).revision; got != want {
				t.Fatalf("%s revision after the second open = %d, want %d", id, got, want)
			}
			flow, err := second.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			wantSuspensions := 0
			if containsString(singleActiveFlowMigrationWant[flow.ProjectID], id) {
				wantSuspensions = 1
			}
			if got := countEventType(flow.Events, "flow.suspended"); got != wantSuspensions {
				t.Fatalf("%s has %d flow.suspended events after the second open, want %d: %v",
					id, got, wantSuspensions, flowEventTypes(flow.Events))
			}
		}
		for projectID, primary := range singleActiveFlowMigrationPrimary {
			rows := activeProjectFlowsOf(t, second, projectID)
			if len(rows) != 1 || rows[0] != primary {
				t.Fatalf("%s active after the second open = %v", projectID, rows)
			}
		}
	})
}

// TestSingleActiveProjectFlowMigrationLeavesACleanDatabaseAlone is the other
// half of "runs at most once": a database that already satisfies the rule is not
// touched at all — no revision, no event, no outbox row, no receipt — and it
// still gains the index.
func TestSingleActiveProjectFlowMigrationLeavesACleanDatabaseAlone(t *testing.T) {
	seeds := []migrationSeed{
		{id: "solo-1", projectID: "proj-solo", status: "active", createdAt: time.UnixMilli(1_000).UTC(), updatedAt: time.UnixMilli(2_000).UTC()},
		{id: "solo-done", projectID: "proj-solo", status: "completed", createdAt: time.UnixMilli(1_000).UTC(), updatedAt: time.UnixMilli(3_000).UTC()},
		{id: "solo-aborted", projectID: "proj-solo", status: "aborted", createdAt: time.UnixMilli(1_000).UTC(), updatedAt: time.UnixMilli(4_000).UTC()},
	}
	path, payloadsBefore := newLegacyGroup2Database(t, seeds)

	store, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	for _, seed := range seeds {
		payload, updatedAt := upgradeStoredRow(t, store, seed.id)
		// The group-2 migration leaves this database alone entirely. The
		// group-3 template-revision backfill writes each row once (none of
		// them carries a revision yet), so only template_revision, the
		// revision counter and kind — the member group 1 introduced, which
		// nothing had reason to add to these rows until now — may differ;
		// updated_at must not move.
		assertFlowPayloadPreservedExcept(t, payloadsBefore[seed.id], []byte(payload), "revision", "template_revision", "kind")
		doc := decodeSeedDocument(t, payloadsBefore[seed.id])
		if updatedAt != doc.UpdatedAt.UnixMilli() {
			t.Fatalf("%s updated_at = %d, want the stored %d", seed.id, updatedAt, doc.UpdatedAt.UnixMilli())
		}
		if got := mirrorOf(t, store, seed.id).revision; got != 2 {
			t.Fatalf("%s revision = %d, want 2 (the template-revision backfill)", seed.id, got)
		}
		flow, err := store.Get(seed.id)
		if err != nil {
			t.Fatal(err)
		}
		if flow.TemplateRevision != 1 {
			t.Fatalf("%s template_revision = %d, want 1", seed.id, flow.TemplateRevision)
		}
	}
	if got := len(allOutboxRows(t, store)); got != 0 {
		t.Fatalf("outbox rows = %d, want none", got)
	}
	applied, err := store.FindAppliedSingleActiveProjectFlowMigration()
	if err != nil {
		t.Fatal(err)
	}
	if applied != nil {
		t.Fatalf("a database with nothing to fix recorded a migration: %+v", applied)
	}
	var indexSQL string
	if err := store.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`,
		oneActiveProjectFlowIndexName).Scan(&indexSQL); err != nil {
		t.Fatalf("the index was not created: %v", err)
	}
}

// TestMigratedSuspendedFlowResumesOrAbortsWithTheEngine ties the migration to
// the engine: the flow the migration parked can be resumed once the project's
// slot is free, and aborting it is allowed while it is parked.
func TestMigratedSuspendedFlowResumesOrAbortsWithTheEngine(t *testing.T) {
	seeds := []migrationSeed{
		{id: "m-1", projectID: "proj-m", status: "active", createdAt: time.UnixMilli(1_000).UTC(), updatedAt: time.UnixMilli(1_000).UTC()},
		{id: "m-2", projectID: "proj-m", status: "active", createdAt: time.UnixMilli(2_000).UTC(), updatedAt: time.UnixMilli(2_000).UTC()},
	}
	path, _ := newLegacyGroup2Database(t, seeds)
	store, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	eng := NewEngineWithStore(store, nil)
	ctx := context.Background()

	parked, err := eng.Get(ctx, "m-1")
	if err != nil {
		t.Fatal(err)
	}
	if parked.Status != FlowStatusSuspended {
		t.Fatalf("m-1 is %s, want suspended", parked.Status)
	}
	if _, err := eng.Resume(ctx, parked.ID); err == nil {
		t.Fatal("resumed a flow while the project's slot was taken")
	} else if !strings.Contains(err.Error(), "m-2") {
		t.Fatalf("the refusal does not name the flow holding the slot: %v", err)
	}
	if _, err := eng.Advance(ctx, parked.ID, &AdvanceRequest{}); err == nil {
		t.Fatal("a suspended flow advanced")
	}
	if _, err := eng.Abort(ctx, "m-2", "free the slot"); err != nil {
		t.Fatalf("abort the primary: %v", err)
	}
	resumed, err := eng.Resume(ctx, parked.ID)
	if err != nil {
		t.Fatalf("resume after the slot was freed: %v", err)
	}
	if resumed.Status != FlowStatusActive || !flowHasEvent(resumed.Events, "flow.resumed") {
		t.Fatalf("resumed flow = status %s events %v", resumed.Status, flowEventTypes(resumed.Events))
	}
	if got := mirrorOf(t, store, parked.ID).revision; got != 4 {
		// revision 2 was the migration's write, 3 the template-revision
		// backfill, 4 the resume.
		t.Fatalf("m-1 revision = %d, want 4", got)
	}
}

// TestTemplateRevisionBackfillDoesNotDisturbThePrimaryChoice pins the ordering
// between the two things that happen on the first open of an old database: the
// group-2 migration chooses each project's flow of record by the updated_at the
// old build wrote, and the group-3 template-revision backfill rewrites every
// flow. If the backfill stamped a fresh time — or if the two ran in the other
// order — the choice would be made on times the user never produced, and the
// flow the user last worked on could lose its slot to one they did not.
//
// The fixture is built so the two orderings disagree: p-older has the later
// created_at and the earlier updated_at, so "most recently updated" picks
// p-newer while "most recently created" would pick p-older. A backfill that
// moved updated_at to now would make the tiebreak meaningless; the test reads
// both flows back afterwards and requires the migration's choice and both
// original times to be exactly what the old build stored.
func TestTemplateRevisionBackfillDoesNotDisturbThePrimaryChoice(t *testing.T) {
	seeds := []migrationSeed{
		// The later updated_at wins — despite the earlier created_at, which is
		// the point: the rule is "most recently updated", not "most recent".
		{id: "p-older", projectID: "proj-pick", status: "active",
			createdAt: time.UnixMilli(9_000).UTC(), updatedAt: time.UnixMilli(10_000).UTC()},
		{id: "p-newer", projectID: "proj-pick", status: "active",
			createdAt: time.UnixMilli(1_000).UTC(), updatedAt: time.UnixMilli(20_000).UTC()},
	}
	path, payloadsBefore := newLegacyGroup2Database(t, seeds)

	store, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatalf("open an older database: %v", err)
	}
	defer store.Close()

	// The first open ran the group-2 migration and then the backfill. The
	// project must be left with the flow the original updated_at chose.
	active := activeProjectFlowsOf(t, store, "proj-pick")
	if len(active) != 1 || active[0] != "p-newer" {
		t.Fatalf("proj-pick active project flows after the first open = %v, want exactly [p-newer] (the greatest original updated_at)", active)
	}
	if suspended := suspendedFlowsOf(t, store, "proj-pick"); len(suspended) != 1 || suspended[0] != "p-older" {
		t.Fatalf("proj-pick suspended = %v, want exactly [p-older]", suspended)
	}

	// Both flows kept the updated_at the old build wrote, on the row and in the
	// document. p-newer was never written by the migration (it kept its slot),
	// but the backfill did write it, so this is the assertion that the backfill
	// is what preserves it; p-older's migration write moved the time, so what
	// the backfill must not touch there is what the migration stamped, and the
	// suspension event carries that same instant.
	flowNewer, err := store.Get("p-newer")
	if err != nil {
		t.Fatal(err)
	}
	wantNewer := decodeSeedDocument(t, payloadsBefore["p-newer"]).UpdatedAt
	if !flowNewer.UpdatedAt.Equal(wantNewer) {
		t.Fatalf("p-newer updated_at = %v, want the stored %v: the backfill moved a time the migration chose by",
			flowNewer.UpdatedAt, wantNewer)
	}
	_, rowNewerMS := upgradeStoredRow(t, store, "p-newer")
	if rowNewerMS != wantNewer.UnixMilli() {
		t.Fatalf("p-newer updated_at column = %d, want the stored %d", rowNewerMS, wantNewer.UnixMilli())
	}

	flowOlder, err := store.Get("p-older")
	if err != nil {
		t.Fatal(err)
	}
	suspension := flowOlder.Events[len(flowOlder.Events)-1]
	if suspension.Type != "flow.suspended" {
		t.Fatalf("p-older last event = %q, want flow.suspended", suspension.Type)
	}
	if !flowOlder.UpdatedAt.Equal(suspension.Timestamp) {
		t.Fatalf("p-older updated_at = %v but its suspension was written at %v: something after the suspension moved it",
			flowOlder.UpdatedAt, suspension.Timestamp)
	}
	_, rowOlderMS := upgradeStoredRow(t, store, "p-older")
	if rowOlderMS != suspension.Timestamp.UnixMilli() {
		t.Fatalf("p-older updated_at column = %d, but the suspension was written at %d",
			rowOlderMS, suspension.Timestamp.UnixMilli())
	}

	// Both gained the revision the backfill fills in, which is what proves it
	// actually ran (otherwise "updated_at did not move" would hold vacuously).
	for _, id := range []string{"p-newer", "p-older"} {
		flow, err := store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if flow.TemplateRevision != 1 {
			t.Fatalf("%s template_revision = %d, want 1 (the backfill ran)", id, flow.TemplateRevision)
		}
	}
}

// --- fixture helpers -------------------------------------------------------

// assertFlowPayloadPreservedExcept fails when the two payloads differ in any
// member other than the named ones. It is what the upgrade tests use instead of
// a byte comparison once a backfill legitimately rewrites a row: the members
// the backfill does not touch must still be exactly what the older build wrote,
// and every other member must be there.
//
// A member the older document did not have may appear on the new one only when
// it carries the zero value the backfill writes (kind/revision/template_revision):
// the "changed" set names members whose presence and value may differ.
func assertFlowPayloadPreservedExcept(t *testing.T, before, after []byte, changed ...string) {
	t.Helper()
	changedSet := map[string]bool{}
	for _, k := range changed {
		changedSet[k] = true
	}
	beforeMembers := membersOf(t, before)
	afterMembers := membersOf(t, after)
	for key, beforeRaw := range beforeMembers {
		if changedSet[key] {
			continue
		}
		afterRaw, ok := afterMembers[key]
		if !ok {
			if pruneEmptyJSONValue(t, beforeRaw) == nil {
				// The member only held an empty value, which the codec's
				// omitempty drops when it re-encodes: absent, not lost.
				continue
			}
			t.Fatalf("member %q was lost; before %s, after %s", key, before, after)
		}
		if !preservedJSONValueEqual(t, beforeRaw, afterRaw) {
			t.Fatalf("member %q changed:\n before %s\n  after %s", key, beforeRaw, afterRaw)
		}
	}
	for key, afterRaw := range afterMembers {
		if changedSet[key] {
			continue
		}
		if _, existed := beforeMembers[key]; !existed && pruneEmptyJSONValue(t, afterRaw) != nil {
			t.Fatalf("member %q was gained (value %s); before %s", key, afterRaw, before)
		}
	}
}

// preservedJSONValueEqual compares two encoded members by value after pruning
// values the codec itself would not write back: a member encoded with omitempty
// whose value is empty (a stage's "gates": [], which this fixture carries)
// disappears when this build re-encodes the document, at any nesting depth.
// Everything else must be deeply equal.
func preservedJSONValueEqual(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	return reflect.DeepEqual(pruneEmptyJSONValue(t, a), pruneEmptyJSONValue(t, b))
}

// pruneEmptyJSONValue decodes one encoded value and drops empty members
// recursively, returning nil for a value that is empty altogether (an empty
// object, an empty array, JSON null). A non-empty value keeps every member with
// its decoded value, so two sides compare equal exactly when they differ only in
// empty members the codec's omitempty would not have written.
func pruneEmptyJSONValue(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode value %s: %v", raw, err)
	}
	return pruneEmptyJSON(v)
}

func pruneEmptyJSON(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			if p := pruneEmptyJSON(item); p != nil {
				out[k] = p
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]any, 0, len(val))
		for _, item := range val {
			out = append(out, pruneEmptyJSON(item))
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case nil:
		return nil
	default:
		return v
	}
}

// decodeSeedDocument decodes one of the hand-written legacy payloads with this
// build's codec, so a test can compare what the migration left against what the
// document said.
func decodeSeedDocument(t *testing.T, payload []byte) *Flow {
	t.Helper()
	var flow Flow
	if err := json.Unmarshal(payload, &flow); err != nil {
		t.Fatalf("decode seeded payload: %v", err)
	}
	return &flow
}

func membersOf(t *testing.T, payload []byte) map[string]json.RawMessage {
	t.Helper()
	var members map[string]json.RawMessage
	if err := json.Unmarshal(payload, &members); err != nil {
		t.Fatalf("read members of %s: %v", payload, err)
	}
	return members
}

func keysOfRaw(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// activeProjectFlowsOf lists the ids of a project's active project flows, in the
// order the store returns them.
func activeProjectFlowsOf(t *testing.T, s *SQLiteFlowStore, projectID string) []string {
	t.Helper()
	rows, err := s.db.Query(`SELECT id FROM flows WHERE project_id = ? AND kind = 'project' AND status = 'active' ORDER BY updated_at DESC, id`,
		projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// suspendedFlowsOf lists the ids of a project's suspended project flows.
func suspendedFlowsOf(t *testing.T, s *SQLiteFlowStore, projectID string) []string {
	t.Helper()
	rows, err := s.db.Query(`SELECT id FROM flows WHERE project_id = ? AND kind = 'project' AND status = 'suspended' ORDER BY created_at, id`,
		projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// outboxRow reads one outbox row by source event id. All fields are read, so a
// caller compares what it needs without another query.
func outboxRow(t *testing.T, s *SQLiteFlowStore, eventID string) (legacyOutboxRow, bool) {
	t.Helper()
	rows := readLegacyOutboxRows(t, s, "source_event_id = ?", eventID)
	if len(rows) == 0 {
		return legacyOutboxRow{}, false
	}
	return rows[0], true
}

func allOutboxRows(t *testing.T, s *SQLiteFlowStore) []legacyOutboxRow {
	t.Helper()
	return readLegacyOutboxRows(t, s, "")
}

func countEventType(events []FlowEvent, typ string) int {
	n := 0
	for _, ev := range events {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
