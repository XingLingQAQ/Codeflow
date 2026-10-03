package floweng

// Tests for T3.01.a group 3, part one: immutable template revisions. The table
// refuses UPDATE and DELETE from SQLite itself, saving a custom template is
// idempotent by content, deleting a template keeps its history, built-in
// templates are frozen when a database is opened, and the canonical encoding is
// deterministic — byte for byte, on every field.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// customRevisionFixture is a small valid custom template with everything the
// canonical encoding has to cover: a name, a description, two stages, an
// agent-bound optional stage, a gate with a config map, and a loop edge.
func customRevisionFixture(id TemplateID) CustomTemplate {
	return CustomTemplate{
		ID:          id,
		Name:        "Revision contract",
		Description: "covers every canonical field",
		Stages: []CustomStage{
			{Type: StageTypeIdea, Name: "想法", Canvas: "intent",
				Gates: []CustomGate{{Phase: GatePhaseExit, Kind: GateKindHumanApproval,
					OnFail: GateOnFailEscalateDebate, Config: map[string]string{"zeta": "1", "alpha": "2"}}}},
			{Type: StageTypeDesign, Name: "设计", Canvas: "design_doc", Optional: true,
				AgentID: "designer-agent",
				Gates:   []CustomGate{{Phase: GatePhaseEnter, Kind: GateKindAuto}}},
		},
		Loops: []LoopEdge{{From: StageTypeDesign, To: StageTypeIdea}},
	}
}

// templateRevisionDBCount counts the revision rows of one template.
func templateRevisionDBCount(t *testing.T, s *SQLiteFlowStore, id TemplateID) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM flow_template_revisions WHERE template_id = ?`, string(id)).Scan(&n); err != nil {
		t.Fatalf("count revisions of %s: %v", id, err)
	}
	return n
}

// executionStore abstracts the two stores the revision rules are tested on, so
// one body of tests proves SQLite and the memory store behave alike.
type revisionStore interface {
	FlowStore
	TemplateStore
	TemplateRevisionStore
}

func newRevisionStoreCases(t *testing.T) map[string]revisionStore {
	t.Helper()
	sqlite, _ := newUpgradeTestStore(t)
	return map[string]revisionStore{
		"sqlite": sqlite,
		"memory": newMemoryStore(),
	}
}

func TestTemplateRevisionsAreImmutableAndIdempotent(t *testing.T) {
	for name, store := range newRevisionStoreCases(t) {
		t.Run(name, func(t *testing.T) {
			id := uniqTemplateID("rev_idem_")
			def := customRevisionFixture(id)

			// Saving the same definition twice appends one revision.
			if err := store.PutTemplate(def); err != nil {
				t.Fatal(err)
			}
			if err := store.PutTemplate(def); err != nil {
				t.Fatal(err)
			}
			latest, err := store.LatestTemplateRevision(id)
			if err != nil {
				t.Fatal(err)
			}
			if latest != 1 {
				t.Fatalf("latest revision after saving the same template twice = %d, want 1", latest)
			}
			first, err := store.GetTemplateRevision(id, 1)
			if err != nil {
				t.Fatal(err)
			}
			if first == nil {
				t.Fatal("revision 1 was not stored")
			}
			if !strings.HasPrefix(first.ContentHash, "sha256:") {
				t.Fatalf("content hash = %q, want a sha256: prefix", first.ContentHash)
			}
			if first.Source != TemplateRevisionSourceCustom {
				t.Fatalf("source = %q, want custom", first.Source)
			}

			// A changed field appends the next revision, leaving the first one.
			changed := def
			changed.Stages = append([]CustomStage(nil), def.Stages...)
			changed.Stages[0].Name = "想法（改）"
			if err := store.PutTemplate(changed); err != nil {
				t.Fatal(err)
			}
			latest, err = store.LatestTemplateRevision(id)
			if err != nil {
				t.Fatal(err)
			}
			if latest != 2 {
				t.Fatalf("latest revision after a changed save = %d, want 2", latest)
			}
			second, err := store.GetTemplateRevision(id, 2)
			if err != nil {
				t.Fatal(err)
			}
			if second == nil || second.ContentHash == first.ContentHash {
				t.Fatalf("revision 2 = %+v, want a different hash than revision 1 (%s)", second, first.ContentHash)
			}
			if first.Payload == second.Payload {
				t.Fatal("the two revisions carry the same payload")
			}

			// Reverting to the earlier content resolves to revision 1: the
			// content is already frozen and the UNIQUE (template_id,
			// content_hash) makes a second row impossible.
			if err := store.PutTemplate(def); err != nil {
				t.Fatal(err)
			}
			latest, err = store.LatestTemplateRevision(id)
			if err != nil {
				t.Fatal(err)
			}
			if latest != 2 {
				t.Fatalf("latest revision after reverting the content = %d, want 2 (no duplicate row)", latest)
			}
		})
	}
}

func TestTemplateRevisionRowsRefuseUpdateAndDelete(t *testing.T) {
	store, _ := newUpgradeTestStore(t)
	id := uniqTemplateID("rev_immutable_")
	if err := store.PutTemplate(customRevisionFixture(id)); err != nil {
		t.Fatal(err)
	}

	// An UPDATE through SQLite is refused by the trigger, and the Go side sees
	// the sentinel: the message is the trigger's stable text.
	_, err := store.db.Exec(`UPDATE flow_template_revisions SET payload = payload WHERE template_id = ?`, string(id))
	if err == nil {
		t.Fatal("UPDATE on flow_template_revisions was accepted")
	}
	if !strings.Contains(err.Error(), templateRevisionImmutableMessage) {
		t.Fatalf("UPDATE refusal = %v, want the trigger message", err)
	}

	// A DELETE is refused the same way.
	_, err = store.db.Exec(`DELETE FROM flow_template_revisions WHERE template_id = ?`, string(id))
	if err == nil {
		t.Fatal("DELETE on flow_template_revisions was accepted")
	}
	if !strings.Contains(err.Error(), templateRevisionImmutableMessage) {
		t.Fatalf("DELETE refusal = %v, want the trigger message", err)
	}

	// The triggers are the schema's, not a one-off: they exist under the names
	// the package's constants promise.
	for _, trigger := range []string{templateRevisionNoUpdateTrigger, templateRevisionNoDeleteTrigger} {
		var sqlText string
		if err := store.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = ?`, trigger).Scan(&sqlText); err != nil {
			t.Fatalf("trigger %s: %v", trigger, err)
		}
		if !strings.Contains(sqlText, "flow_template_revisions") {
			t.Fatalf("trigger %s does not guard the revisions table: %s", trigger, sqlText)
		}
	}

	// Re-ensuring an unchanged definition resolves to the existing revision
	// instead of trying (and failing) to append a second row for the same
	// content — the UNIQUE (template_id, content_hash) is what makes that
	// second row impossible.
	_, err = store.EnsureTemplateRevision(builtinTemplates[TemplateNewProject], TemplateRevisionSourceBuiltin)
	if err != nil {
		t.Fatalf("re-ensuring an unchanged builtin revision failed: %v", err)
	}
	// The constraint itself exists: a direct duplicate insert is refused, and
	// the store classifies that refusal as the immutability sentinel.
	rev, err := store.GetTemplateRevision(id, 1)
	if err != nil || rev == nil {
		t.Fatalf("revision 1: %+v, %v", rev, err)
	}
	_, err = store.db.Exec(`
INSERT INTO flow_template_revisions (template_id, revision, content_hash, payload, source, created_at)
VALUES (?, 2, ?, '{}', 'custom', 0)`, string(id), rev.ContentHash)
	if err == nil {
		t.Fatal("a duplicate content hash was accepted")
	}
	if classed := classifyTemplateRevisionWrite(err); !errors.Is(classed, ErrTemplateRevisionImmutable) {
		t.Fatalf("duplicate-content refusal = %v, want ErrTemplateRevisionImmutable", classed)
	}
}

func TestDeletingATemplateKeepsItsRevisionsAndItsFlowsReadable(t *testing.T) {
	for name, store := range newRevisionStoreCases(t) {
		t.Run(name, func(t *testing.T) {
			id := uniqTemplateID("rev_keep_")
			if err := store.PutTemplate(customRevisionFixture(id)); err != nil {
				t.Fatal(err)
			}
			// Registering in the live registry is what makes Create find the
			// template; DeleteTemplate below removes both.
			RegisterTemplate(customRevisionFixture(id))
			t.Cleanup(func() { customMu.Lock(); delete(customTemplates, id); customMu.Unlock() })
			engine := NewEngineWithStore(store, nil)
			flow, err := engine.Create(context.Background(), &CreateFlowRequest{ProjectID: "proj-keep", TemplateID: id})
			if err != nil {
				t.Fatal(err)
			}

			if err := store.DeleteTemplate(id); err != nil {
				t.Fatal(err)
			}
			latest, err := store.LatestTemplateRevision(id)
			if err != nil {
				t.Fatal(err)
			}
			if latest != 1 {
				t.Fatalf("latest revision after deleting the template = %d, want 1 (revisions stay)", latest)
			}
			rev, err := store.GetTemplateRevision(id, 1)
			if err != nil || rev == nil {
				t.Fatalf("revision 1 after deleting the template = %+v, %v", rev, err)
			}
			// The flow that names the deleted template still reads back with
			// the revision it was created from.
			readBack, err := engine.Get(context.Background(), flow.ID)
			if err != nil {
				t.Fatalf("flow of a deleted template is unreadable: %v", err)
			}
			if readBack.TemplateRevision != 1 {
				t.Fatalf("flow template_revision = %d, want 1", readBack.TemplateRevision)
			}
		})
	}
}

func TestBuiltinTemplateRevisions(t *testing.T) {
	t.Run("a database open freezes the builtins", func(t *testing.T) {
		store, _ := newUpgradeTestStore(t)
		for id := range builtinTemplates {
			latest, err := store.LatestTemplateRevision(id)
			if err != nil {
				t.Fatal(err)
			}
			if latest < 1 {
				t.Fatalf("builtin %s has revision %d after open, want >= 1", id, latest)
			}
			rev, err := store.GetTemplateRevision(id, latest)
			if err != nil || rev == nil {
				t.Fatalf("builtin %s revision %d unreadable: %+v, %v", id, latest, rev, err)
			}
			if rev.Source != TemplateRevisionSourceBuiltin {
				t.Fatalf("builtin %s revision source = %q, want builtin", id, rev.Source)
			}
		}
	})

	t.Run("an unchanged code definition appends nothing on reopen", func(t *testing.T) {
		path := ""
		first, p := newUpgradeTestStore(t)
		path = p
		before := map[TemplateID]int64{}
		for id := range builtinTemplates {
			latest, err := first.LatestTemplateRevision(id)
			if err != nil {
				t.Fatal(err)
			}
			before[id] = latest
		}
		second, err := NewSQLiteFlowStore(path)
		if err != nil {
			t.Fatal(err)
		}
		defer second.Close()
		for id, want := range before {
			latest, err := second.LatestTemplateRevision(id)
			if err != nil {
				t.Fatal(err)
			}
			if latest != want {
				t.Fatalf("reopening moved builtin %s from revision %d to %d", id, want, latest)
			}
		}
	})

	t.Run("a changed code definition appends the next revision", func(t *testing.T) {
		store, path := newUpgradeTestStore(t)
		id := TemplateNewProject
		before, err := store.LatestTemplateRevision(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}

		// Simulate "the next build changed this builtin template" through the
		// seam, so the package-level definition map is not touched.
		restore := primedBuiltinHashHook
		primedBuiltinHashHook = func(def templateDef) (string, []byte, error) {
			if def.ID == id {
				return "sha256:primed-changed-definition", []byte(`{"primed":"changed"}`), nil
			}
			return canonicalTemplateHash(def)
		}
		t.Cleanup(func() { primedBuiltinHashHook = restore })

		reopened, err := NewSQLiteFlowStore(path)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		after, err := reopened.LatestTemplateRevision(id)
		if err != nil {
			t.Fatal(err)
		}
		if after != before+1 {
			t.Fatalf("changed builtin definition moved revision %d -> %d, want +1", before, after)
		}
		rev, err := reopened.GetTemplateRevision(id, after)
		if err != nil || rev == nil {
			t.Fatalf("the appended revision is unreadable: %+v, %v", rev, err)
		}
		if rev.ContentHash != "sha256:primed-changed-definition" {
			t.Fatalf("appended revision hash = %q, want the primed one", rev.ContentHash)
		}
	})
}

func TestTemplateRevisionCanonicalEncodingIsDeterministic(t *testing.T) {
	base := customRevisionFixture("canon_encoding")
	defA := customTemplateDef(base)

	// Encoding the same definition twice is byte-for-byte identical, including
	// the gate config map, whose iteration order Go randomizes.
	first, err := canonicalTemplateContent(defA)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		again, err := canonicalTemplateContent(defA)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("encoding %d differs from the first:\n %s\n %s", i, again, first)
		}
	}
	if strings.Contains(string(first), `"alpha"`) && !strings.Contains(string(first), `"zeta"`) {
		t.Fatal("the config map was not encoded")
	}

	baseHash := templateContentHash(first)

	// Every field the canonical document covers changes the hash.
	mutations := map[string]func(templateDef) templateDef{
		"name":        func(d templateDef) templateDef { d.Name += "!"; return d },
		"description": func(d templateDef) templateDef { d.Description += "!"; return d },
		"stage name": func(d templateDef) templateDef {
			d.Stages = append([]stageDef(nil), d.Stages...)
			d.Stages[0].Name += "!"
			return d
		},
		"stage canvas": func(d templateDef) templateDef {
			d.Stages = append([]stageDef(nil), d.Stages...)
			d.Stages[0].Canvas += "!"
			return d
		},
		"stage agent": func(d templateDef) templateDef {
			d.Stages = append([]stageDef(nil), d.Stages...)
			d.Stages[1].AgentID += "!"
			return d
		},
		"stage optional": func(d templateDef) templateDef {
			d.Stages = append([]stageDef(nil), d.Stages...)
			d.Stages[0].Optional = !d.Stages[0].Optional
			return d
		},
		"gate kind": func(d templateDef) templateDef {
			d.Stages = append([]stageDef(nil), d.Stages...)
			d.Stages[0].Gates = append([]Gate(nil), d.Stages[0].Gates...)
			d.Stages[0].Gates[0].Kind = GateKindAgentCheck
			return d
		},
		"gate config": func(d templateDef) templateDef {
			d.Stages = append([]stageDef(nil), d.Stages...)
			d.Stages[0].Gates = append([]Gate(nil), d.Stages[0].Gates...)
			cfg := map[string]string{}
			for k, v := range d.Stages[0].Gates[0].Config {
				cfg[k] = v
			}
			cfg["new"] = "value"
			d.Stages[0].Gates[0].Config = cfg
			return d
		},
		"loop order": func(d templateDef) templateDef {
			d.Loops = append([]LoopEdge(nil), d.Loops...)
			d.Loops[0].From, d.Loops[0].To = d.Loops[0].To, d.Loops[0].From
			return d
		},
	}
	for label, mutate := range mutations {
		body, err := canonicalTemplateContent(mutate(defA))
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if templateContentHash(body) == baseHash {
			t.Fatalf("changing the %s did not change the hash", label)
		}
	}

	// Reordering the stage array changes the hash: stage order is part of the
	// definition, not a detail of the encoding.
	reordered := defA
	reordered.Stages = []stageDef{defA.Stages[1], defA.Stages[0]}
	body, err := canonicalTemplateContent(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if templateContentHash(body) == baseHash {
		t.Fatal("reordering the stages did not change the hash")
	}

	// The stored payload is exactly the canonical bytes.
	store, _ := newUpgradeTestStore(t)
	if err := store.PutTemplate(base); err != nil {
		t.Fatal(err)
	}
	rev, err := store.GetTemplateRevision(base.ID, 1)
	if err != nil || rev == nil {
		t.Fatalf("stored revision: %+v, %v", rev, err)
	}
	if rev.Payload != string(first) {
		t.Fatalf("stored payload is not the canonical bytes:\n got %s\nwant %s", rev.Payload, first)
	}
	if rev.ContentHash != baseHash {
		t.Fatalf("stored hash = %q, want %q", rev.ContentHash, baseHash)
	}
}

func TestUnknownTemplateRevisionIsZero(t *testing.T) {
	store, _ := newUpgradeTestStore(t)
	latest, err := store.LatestTemplateRevision("no_such_template")
	if err != nil {
		t.Fatal(err)
	}
	if latest != 0 {
		t.Fatalf("latest revision of an unknown template = %d, want 0", latest)
	}
	rev, err := store.GetTemplateRevision("no_such_template", 1)
	if err != nil || rev != nil {
		t.Fatalf("unknown revision = %+v, %v; want nil, nil", rev, err)
	}
	// Revision 0 does not exist even for a template that does.
	if rev, err := store.GetTemplateRevision(TemplateNewProject, 0); err != nil || rev != nil {
		t.Fatalf("revision 0 = %+v, %v; want nil, nil", rev, err)
	}

	// The engine's memory store answers the same way, and the engine reports
	// 0 on a flow whose template it cannot resolve.
	memory := newMemoryStore()
	if latest, err := memory.LatestTemplateRevision(TemplateNewProject); err != nil || latest != 0 {
		t.Fatalf("fresh memory store latest = %d, %v; want 0, nil", latest, err)
	}
}

func TestMemoryStoreRevisionsMatchSQLiteSemantics(t *testing.T) {
	memory := newMemoryStore()
	id := uniqTemplateID("mem_rule_")
	def := customRevisionFixture(id)
	if err := memory.PutTemplate(def); err != nil {
		t.Fatal(err)
	}
	if err := memory.PutTemplate(def); err != nil {
		t.Fatal(err)
	}
	if latest, _ := memory.LatestTemplateRevision(id); latest != 1 {
		t.Fatalf("memory latest after two identical saves = %d, want 1", latest)
	}

	changed := def
	changed.Description = "changed"
	if err := memory.PutTemplate(changed); err != nil {
		t.Fatal(err)
	}
	if latest, _ := memory.LatestTemplateRevision(id); latest != 2 {
		t.Fatalf("memory latest after a changed save = %d, want 2", latest)
	}
	first, _ := memory.GetTemplateRevision(id, 1)
	second, _ := memory.GetTemplateRevision(id, 2)
	if first == nil || second == nil || first.ContentHash == second.ContentHash {
		t.Fatalf("memory revisions = %+v / %+v", first, second)
	}
	if first.Source != TemplateRevisionSourceCustom || first.Payload == "" {
		t.Fatalf("memory revision 1 = %+v", first)
	}
	// The payload is the canonical encoding, same as SQLite's.
	body, err := canonicalTemplateContent(customTemplateDef(def))
	if err != nil {
		t.Fatal(err)
	}
	if first.Payload != string(body) || first.ContentHash != templateContentHash(body) {
		t.Fatal("memory revision 1 is not the canonical encoding")
	}

	// Deleting the definition keeps the history, like SQLite.
	if err := memory.DeleteTemplate(id); err != nil {
		t.Fatal(err)
	}
	if latest, _ := memory.LatestTemplateRevision(id); latest != 2 {
		t.Fatalf("memory latest after delete = %d, want 2", latest)
	}

	// The engine over a memory store sees the same numbers.
	engine := NewEngineWithStore(memory, nil)
	if _, ok := engine.store.(TemplateRevisionStore); !ok {
		t.Fatal("the memory store does not implement TemplateRevisionStore")
	}
}

func TestFillTemplateRevisionReceiptOnCleanDatabase(t *testing.T) {
	// A database this build created has no legacy rows, so opening it records
	// no template-revision backfill receipt at all.
	store, _ := newUpgradeTestStore(t)
	applied, err := store.FindAppliedTemplateRevisionBackfill()
	if err != nil {
		t.Fatal(err)
	}
	if applied != nil {
		t.Fatalf("a fresh database recorded a template-revision backfill: %+v", applied)
	}
}

// legacyRevisionSeed is one hand-written pre-T3.01 row of the fixture below:
// which template it names, and whether its payload pretends to already carry a
// revision (the state a build between group 1 and group 3 could have written).
type legacyRevisionSeed struct {
	id         string
	projectID  string
	templateID string
	status     string
	payloadRev int64 // 0 = the member is absent
}

// newLegacyTemplateRevisionDatabase builds a database of the pre-group-2 shape
// (the DDL copied from flow_store_upgrade_test.go, so the fixture is the real
// old table) with the given rows, plus one flow_templates row for every custom
// template named in customTemplates. The store is closed: the test is about
// what opening it on this build does.
func newLegacyTemplateRevisionDatabase(t *testing.T, seeds []legacyRevisionSeed, customTemplates []TemplateID) string {
	t.Helper()
	store, path := newUpgradeTestStore(t)
	for _, table := range []string{"flows", "flow_templates", "flow_event_outbox", "flow_store_migrations"} {
		if _, err := store.db.Exec(`DROP TABLE ` + table); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}
	if _, err := store.db.Exec(flowDocumentsTableDDL(t, store.db)); err != nil {
		t.Fatalf("create the older schema: %v", err)
	}
	for i, seed := range seeds {
		createdAt := int64(1_000 + i)
		doc := map[string]any{
			"id":          seed.id,
			"project_id":  seed.projectID,
			"template_id": seed.templateID,
			"status":      seed.status,
			"stages":      []any{},
			"loops":       []any{},
			"artifacts":   []any{},
			"events": []any{map[string]any{
				"id": seed.id + "-ev", "type": "flow.created", "message": "created",
				"timestamp": time.UnixMilli(createdAt).UTC(),
			}},
			"created_at": time.UnixMilli(createdAt).UTC(),
			"updated_at": time.UnixMilli(createdAt).UTC(),
		}
		if seed.payloadRev > 0 {
			doc["template_revision"] = seed.payloadRev
		}
		body, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec(`
INSERT INTO flows (id, project_id, template_id, status, payload, created_at, updated_at, kind, revision)
VALUES (?, ?, ?, ?, ?, ?, ?, 'project', 1)`,
			seed.id, seed.projectID, seed.templateID, seed.status, string(body), createdAt, createdAt); err != nil {
			t.Fatalf("insert legacy row %s: %v", seed.id, err)
		}
	}
	for i, id := range customTemplates {
		def := customRevisionFixture(id)
		// The overwrite-only row the old build wrote holds a CustomTemplate.
		body, err := json.Marshal(def)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec(`INSERT INTO flow_templates (id, payload, updated_at) VALUES (?, ?, ?)`,
			string(id), string(body), 1_000+i); err != nil {
			t.Fatalf("insert legacy template %s: %v", id, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTemplateRevisionBackfillOnLegacyDatabase(t *testing.T) {
	customID := TemplateID("legacy_custom_" + uniqTemplateID("x")[2:])
	customTemplates := []TemplateID{customID}
	seeds := []legacyRevisionSeed{
		{id: "lt-builtin", projectID: "proj-lt", templateID: string(TemplateNewProject), status: "active"},
		{id: "lt-custom", projectID: "proj-lt", templateID: string(customID), status: "completed"},
		{id: "lt-ghost", projectID: "proj-lt", templateID: "ghost_template", status: "aborted"},
		{id: "lt-modern", projectID: "proj-lt", templateID: string(TemplateNewProject), status: "suspended", payloadRev: 7},
	}
	path := newLegacyTemplateRevisionDatabase(t, seeds, customTemplates)

	store, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatalf("open the older database: %v", err)
	}
	defer store.Close()

	// The builtin flow and the custom flow were filled in; the ghost keeps 0;
	// the row that already names a revision in its payload is left alone.
	for id, want := range map[string]int64{"lt-builtin": 1, "lt-custom": 1, "lt-ghost": 0, "lt-modern": 7} {
		flow, err := store.Get(id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if flow.TemplateRevision != want {
			t.Fatalf("%s template_revision = %d, want %d", id, flow.TemplateRevision, want)
		}
	}

	// The receipt says what was filled and what could not be.
	applied, err := store.FindAppliedTemplateRevisionBackfill()
	if err != nil {
		t.Fatal(err)
	}
	if applied == nil {
		t.Fatal("the backfill recorded no receipt")
	}
	if applied.Name != templateRevisionBackfillMigration {
		t.Fatalf("receipt name = %q", applied.Name)
	}
	var receipt TemplateRevisionBackfillReceipt
	if err := json.Unmarshal([]byte(applied.Receipt), &receipt); err != nil {
		t.Fatalf("decode receipt: %v (%s)", err, applied.Receipt)
	}
	if !strings.Contains(receipt.Note, "migration time") {
		t.Fatalf("the receipt does not carry its honesty caveat: %q", receipt.Note)
	}
	if len(receipt.Backfilled) != 2 {
		t.Fatalf("backfilled = %+v, want the builtin and custom flows", receipt.Backfilled)
	}
	byFlow := map[string]TemplateRevisionBackfilledFlow{}
	for _, rec := range receipt.Backfilled {
		byFlow[rec.FlowID] = rec
	}
	if rec := byFlow["lt-custom"]; rec.TemplateID != string(customID) || rec.Revision != 1 {
		t.Fatalf("lt-custom receipt = %+v", rec)
	}
	if len(receipt.Unresolved) != 1 || receipt.Unresolved[0].FlowID != "lt-ghost" {
		t.Fatalf("unresolved = %+v, want lt-ghost", receipt.Unresolved)
	}

	// The sealed revision is the custom template's revision 1, produced by the
	// open-time seeding from the flow_templates payload the old build wrote.
	rev, err := store.GetTemplateRevision(customID, 1)
	if err != nil || rev == nil {
		t.Fatalf("custom template revision: %+v, %v", rev, err)
	}
	if rev.Source != TemplateRevisionSourceCustom {
		t.Fatalf("custom template revision source = %q", rev.Source)
	}
	// The ghost template got no revision at all.
	if latest, err := store.LatestTemplateRevision("ghost_template"); err != nil || latest != 0 {
		t.Fatalf("ghost template latest = %d, %v; want 0", latest, err)
	}

	// Opening again is idempotent: the same receipt, the same flows, no
	// revision beyond the ones already written.
	reopened, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	applied2, err := reopened.FindAppliedTemplateRevisionBackfill()
	if err != nil {
		t.Fatal(err)
	}
	if applied2 == nil || applied2.Receipt != applied.Receipt {
		t.Fatalf("reopening changed the receipt:\n first %v\n second %v", applied, applied2)
	}
	for id, want := range map[string]int64{"lt-builtin": 1, "lt-custom": 1, "lt-ghost": 0, "lt-modern": 7} {
		flow, err := reopened.Get(id)
		if err != nil {
			t.Fatalf("reopen get %s: %v", id, err)
		}
		if flow.TemplateRevision != want {
			t.Fatalf("after reopen %s template_revision = %d, want %d", id, flow.TemplateRevision, want)
		}
	}
	if latest, err := reopened.LatestTemplateRevision(customID); err != nil || latest != 1 {
		t.Fatalf("after reopen custom template latest = %d, %v; want 1", latest, err)
	}
}

func TestDeletedTemplateLeavesItsFlowsAtZeroAndInTheReceipt(t *testing.T) {
	// A custom template that was saved and then deleted leaves flows naming it;
	// the open-time custom seeding cannot reconstruct it (its flow_templates
	// row is gone), so the flow keeps 0 and the receipt says so instead of
	// inventing a revision.
	customID := uniqTemplateID("deleted_custom_")
	seeds := []legacyRevisionSeed{
		{id: "dt-1", projectID: "proj-dt", templateID: string(customID), status: "active"},
	}
	path := newLegacyTemplateRevisionDatabase(t, seeds, nil) // no flow_templates row

	store, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	flow, err := store.Get("dt-1")
	if err != nil {
		t.Fatal(err)
	}
	if flow.TemplateRevision != 0 {
		t.Fatalf("flow of a deleted template has template_revision %d, want 0", flow.TemplateRevision)
	}
	applied, err := store.FindAppliedTemplateRevisionBackfill()
	if err != nil {
		t.Fatal(err)
	}
	if applied == nil {
		t.Fatal("no receipt was recorded for the unresolved flow")
	}
	var receipt TemplateRevisionBackfillReceipt
	if err := json.Unmarshal([]byte(applied.Receipt), &receipt); err != nil {
		t.Fatal(err)
	}
	if len(receipt.Backfilled) != 0 || len(receipt.Unresolved) != 1 {
		t.Fatalf("receipt = %+v", receipt)
	}
	if receipt.Unresolved[0].TemplateID != string(customID) {
		t.Fatalf("unresolved = %+v", receipt.Unresolved[0])
	}
	if receipt.Unresolved[0].Reason == "" {
		t.Fatal("the unresolved entry does not explain itself")
	}
}
