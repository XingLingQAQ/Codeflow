package floweng

// Tests for T3.01.a group 3, part two: the binding_id backfill and the binding
// resolution Create performs. The backfill runs after startup wiring, walks the
// unfinished flows, writes each project's current primary binding into them
// (revision + 1, no event, no outbox row), reports the projects that have no
// binding instead of failing, and stops cleanly on a resolver error so the next
// startup can continue.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// atMS renders a fixture timestamp in the same shape migrationFixtureSeeds
// uses, so the two seed tables cannot drift.
func atMS(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// stubBindingResolver is a BindingResolver with per-project answers, so a test
// can drive "has a binding", "no binding" and "the lookup failed" without a
// project service.
type stubBindingResolver struct {
	bindings map[string]string
	errs     map[string]error
	calls    []string
}

func (r *stubBindingResolver) ActivePrimaryBindingID(ctx context.Context, projectID string) (string, error) {
	r.calls = append(r.calls, projectID)
	if err := r.errs[projectID]; err != nil {
		return "", err
	}
	return r.bindings[projectID], nil
}

// bindingSeeds is one active flow per project plus the statuses the backfill
// must distinguish: the flow of proj-bound that was suspended, and the finished
// flows that must stay unbound.
var bindingSeeds = []migrationSeed{
	{id: "bf-active", projectID: "proj-bound", status: "active", createdAt: atMS(1_000), updatedAt: atMS(1_000)},
	{id: "bf-suspended", projectID: "proj-bound", status: "suspended", createdAt: atMS(900), updatedAt: atMS(900)},
	{id: "bf-done", projectID: "proj-bound", status: "completed", createdAt: atMS(800), updatedAt: atMS(800)},
	{id: "bf-aborted", projectID: "proj-bound", status: "aborted", createdAt: atMS(700), updatedAt: atMS(700)},
	{id: "bf-unbound", projectID: "proj-no-binding", status: "active", createdAt: atMS(600), updatedAt: atMS(600)},
}

// flowBindingOf reads one flow's binding_id from the document and the column,
// with the row's revision. The column is nullable (an empty binding is stored
// as NULL), so it scans through sql.NullString.
func flowBindingOf(t *testing.T, s *SQLiteFlowStore, id string) (doc string, column string, revision int64) {
	t.Helper()
	var nullable sql.NullString
	if err := s.db.QueryRow(`SELECT binding_id, revision FROM flows WHERE id = ?`, id).
		Scan(&nullable, &revision); err != nil {
		t.Fatalf("read flows row %s: %v", id, err)
	}
	flow, err := s.Get(id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return flow.BindingID, nullable.String, revision
}

func TestBackfillFlowBindingsOnSQLite(t *testing.T) {
	ctx := context.Background()
	path, _ := newLegacyGroup2Database(t, bindingSeeds)

	store, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatalf("open the older database: %v", err)
	}
	defer store.Close()
	// Opening ran the migration and the template-revision backfill already;
	// what this test measures is the binding backfill that startup wiring runs.
	engine := NewEngineWithStore(store, nil)

	resolver := &stubBindingResolver{
		bindings: map[string]string{"proj-bound": "binding-1"},
		errs:     map[string]error{},
	}
	engine.SetBindingResolver(resolver)

	// The rows already carry a revision from the open-time backfills; read them
	// so "revision + 1" below is relative to what the store had.
	_, _, beforeUnfinished := flowBindingOf(t, store, "bf-active")
	_, _, beforeFinished := flowBindingOf(t, store, "bf-done")
	outboxBefore := legacyOutboxCount(t, store)

	receipt, err := engine.BackfillFlowBindings(ctx)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}

	// The unfinished, unbound flows got the project's binding, in both the
	// document and the mirror column, each by exactly one revision.
	for _, id := range []string{"bf-active", "bf-suspended"} {
		doc, column, revision := flowBindingOf(t, store, id)
		if doc != "binding-1" || column != "binding-1" {
			t.Fatalf("%s binding = %q (document) / %q (column), want binding-1", id, doc, column)
		}
		if revision != beforeUnfinished+1 {
			t.Fatalf("%s revision = %d, want %d (+1)", id, revision, beforeUnfinished+1)
		}
	}

	// The finished flows were not touched: binding stays empty, revision stays.
	for _, id := range []string{"bf-done", "bf-aborted"} {
		doc, column, revision := flowBindingOf(t, store, id)
		if doc != "" || column != "" {
			t.Fatalf("%s binding = %q / %q, want empty (finished flows are not bound)", id, doc, column)
		}
		if revision != beforeFinished {
			t.Fatalf("%s revision = %d, want %d (untouched)", id, revision, beforeFinished)
		}
	}

	// The project with no binding is in the receipt, not an error; its flow
	// stays empty and its revision does not move.
	if len(receipt.Unresolved) != 1 || receipt.Unresolved[0].ProjectID != "proj-no-binding" {
		t.Fatalf("unresolved = %+v, want proj-no-binding", receipt.Unresolved)
	}
	if got := receipt.Unresolved[0].FlowIDs; len(got) != 1 || got[0] != "bf-unbound" {
		t.Fatalf("unresolved flows = %v, want [bf-unbound]", got)
	}
	doc, column, _ := flowBindingOf(t, store, "bf-unbound")
	if doc != "" || column != "" {
		t.Fatalf("bf-unbound binding = %q / %q, want empty", doc, column)
	}
	if len(receipt.Backfilled) != 2 {
		t.Fatalf("backfilled = %+v, want exactly the two unfinished flows", receipt.Backfilled)
	}
	for _, rec := range receipt.Backfilled {
		if rec.BindingID != "binding-1" || rec.ProjectID != "proj-bound" {
			t.Fatalf("receipt entry = %+v", rec)
		}
	}

	// Nothing was queued for the projector: the backfill wrote no events.
	if got := legacyOutboxCount(t, store); got != outboxBefore {
		t.Fatalf("outbox rows after the backfill = %d, want %d (unchanged)", got, outboxBefore)
	}

	// The receipt is durable, and it says what the run did.
	applied, err := store.FindAppliedBindingBackfill()
	if err != nil {
		t.Fatal(err)
	}
	if applied == nil {
		t.Fatal("the binding backfill recorded no receipt")
	}
	if applied.Name != FlowBindingBackfillMigration {
		t.Fatalf("receipt name = %q", applied.Name)
	}
	var recorded FlowBindingBackfillReceipt
	if err := json.Unmarshal([]byte(applied.Receipt), &recorded); err != nil {
		t.Fatalf("decode receipt: %v (%s)", err, applied.Receipt)
	}
	if len(recorded.Backfilled) != 2 || len(recorded.Unresolved) != 1 || recorded.Note == "" {
		t.Fatalf("recorded receipt = %+v", recorded)
	}

	// Running it again is idempotent: nothing to write, nothing changes, and
	// only the still-unbound project is asked about.
	callsBefore := len(resolver.calls)
	receipt2, err := engine.BackfillFlowBindings(ctx)
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if len(receipt2.Backfilled) != 0 {
		t.Fatalf("second backfill wrote %+v, want nothing", receipt2.Backfilled)
	}
	if len(receipt2.Unresolved) != 1 {
		t.Fatalf("second backfill unresolved = %+v", receipt2.Unresolved)
	}
	if len(resolver.calls) != callsBefore+1 {
		t.Fatalf("second backfill made %d resolver calls, want one per unbound project", len(resolver.calls)-callsBefore)
	}
	for _, id := range []string{"bf-active", "bf-suspended"} {
		_, _, revision := flowBindingOf(t, store, id)
		if revision != beforeUnfinished+1 {
			t.Fatalf("%s revision after the second backfill = %d, want %d", id, revision, beforeUnfinished+1)
		}
	}
	if got := legacyOutboxCount(t, store); got != outboxBefore {
		t.Fatalf("outbox rows after the second backfill = %d", got)
	}
}

func TestBackfillFlowBindingsOnMemoryStore(t *testing.T) {
	ctx := context.Background()
	engine := NewInMemoryEngine(nil)

	// A flow created before the resolver was wired has no binding, which is the
	// state the backfill exists to repair. The resolver is attached only now.
	active, err := engine.Create(ctx, &CreateFlowRequest{ProjectID: "proj-m"})
	if err != nil {
		t.Fatal(err)
	}
	done := testLegacyFlow("mem-done", "proj-m", testLegacyEvent("mem-done-ev", "flow.created", "", "created"))
	done.Status = FlowStatusCompleted
	if err := engine.store.Put(done); err != nil {
		t.Fatal(err)
	}
	if active.BindingID != "" {
		t.Fatalf("fixture error: the flow was created with binding %q", active.BindingID)
	}
	activeRevision := active.Revision

	engine.SetBindingResolver(&stubBindingResolver{
		bindings: map[string]string{"proj-m": "binding-m"},
		errs:     map[string]error{},
	})
	receipt, err := engine.BackfillFlowBindings(ctx)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if len(receipt.Backfilled) != 1 || receipt.Backfilled[0].FlowID != active.ID {
		t.Fatalf("backfilled = %+v, want the active flow only", receipt.Backfilled)
	}
	got, err := engine.Get(ctx, active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.BindingID != "binding-m" {
		t.Fatalf("active binding = %q, want binding-m", got.BindingID)
	}
	if got.Revision != activeRevision+1 {
		t.Fatalf("active revision = %d, want %d", got.Revision, activeRevision+1)
	}
	stillDone, err := engine.Get(ctx, "mem-done")
	if err != nil {
		t.Fatal(err)
	}
	if stillDone.BindingID != "" {
		t.Fatalf("completed flow binding = %q, want empty", stillDone.BindingID)
	}

	// Repeat: nothing more to write.
	receipt2, err := engine.BackfillFlowBindings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt2.Backfilled) != 0 {
		t.Fatalf("second backfill = %+v, want nothing", receipt2.Backfilled)
	}
}

func TestBackfillFlowBindingsStopsOnResolverErrorAndResumes(t *testing.T) {
	ctx := context.Background()
	seeds := []migrationSeed{
		{id: "be-a", projectID: "proj-a", status: "active", createdAt: atMS(1_000), updatedAt: atMS(1_000)},
		{id: "be-b", projectID: "proj-b", status: "active", createdAt: atMS(1_000), updatedAt: atMS(1_000)},
	}
	path, _ := newLegacyGroup2Database(t, seeds)
	store, err := NewSQLiteFlowStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	engine := NewEngineWithStore(store, nil)

	failure := errors.New("binding store unavailable")
	resolver := &stubBindingResolver{
		// proj-a sorts before proj-b, so proj-a's flow is written first and
		// survives the failure on proj-b.
		bindings: map[string]string{"proj-a": "binding-a", "proj-b": "binding-b"},
		errs:     map[string]error{"proj-b": failure},
	}
	engine.SetBindingResolver(resolver)
	outboxBefore := legacyOutboxCount(t, store)

	receipt, err := engine.BackfillFlowBindings(ctx)
	if err == nil {
		t.Fatal("the backfill returned no error on a resolver failure")
	}
	if !errors.Is(err, failure) {
		t.Fatalf("backfill error = %v, want the resolver's error", err)
	}
	if receipt == nil || len(receipt.Backfilled) != 1 || receipt.Backfilled[0].FlowID != "be-a" {
		t.Fatalf("partial receipt = %+v, want the flow written before the failure", receipt)
	}
	// The written work stays, and the receipt of it is durable.
	docA, colA, revA := flowBindingOf(t, store, "be-a")
	if docA != "binding-a" || colA != "binding-a" {
		t.Fatalf("be-a binding = %q / %q, want binding-a", docA, colA)
	}
	_ = revA
	docB, colB, _ := flowBindingOf(t, store, "be-b")
	if docB != "" || colB != "" {
		t.Fatalf("be-b binding = %q / %q, want empty (stopped before it)", docB, colB)
	}
	applied, err := store.FindAppliedBindingBackfill()
	if err != nil {
		t.Fatal(err)
	}
	if applied == nil {
		t.Fatal("the failed run recorded no receipt of what it wrote")
	}

	// The next startup succeeds and only finishes the remaining flow: the
	// written one is not written again.
	resolver.errs = map[string]error{}
	_, _, revABeforeRetry := flowBindingOf(t, store, "be-a")
	receipt2, err := engine.BackfillFlowBindings(ctx)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(receipt2.Backfilled) != 1 || receipt2.Backfilled[0].FlowID != "be-b" {
		t.Fatalf("retry backfilled = %+v, want be-b only", receipt2.Backfilled)
	}
	docB2, colB2, _ := flowBindingOf(t, store, "be-b")
	if docB2 != "binding-b" || colB2 != "binding-b" {
		t.Fatalf("be-b after retry = %q / %q", docB2, colB2)
	}
	_, _, revAAfterRetry := flowBindingOf(t, store, "be-a")
	if revAAfterRetry != revABeforeRetry {
		t.Fatalf("be-a was written again on the retry: revision %d -> %d", revABeforeRetry, revAAfterRetry)
	}
	if got := legacyOutboxCount(t, store); got != outboxBefore {
		t.Fatalf("outbox rows = %d, want %d (no events were queued)", got, outboxBefore)
	}
}

func TestBackfillFlowBindingsWithoutResolverFailsClosed(t *testing.T) {
	engine := NewInMemoryEngine(nil)
	if _, err := engine.BackfillFlowBindings(context.Background()); err == nil {
		t.Fatal("the backfill ran without a resolver")
	}
}

func TestCreateRecordsTemplateRevisionAndBinding(t *testing.T) {
	ctx := context.Background()

	t.Run("latest revision and the resolver's binding", func(t *testing.T) {
		for name, store := range newRevisionStoreCases(t) {
			t.Run(name, func(t *testing.T) {
				engine := NewEngineWithStore(store, nil)
				resolver := &stubBindingResolver{
					bindings: map[string]string{"proj-create": "binding-create", "proj-create-custom": "binding-create"},
					errs:     map[string]error{},
				}
				engine.SetBindingResolver(resolver)

				// A flow from the builtin template records the builtin's latest
				// revision.
				flow, err := engine.Create(ctx, &CreateFlowRequest{ProjectID: "proj-create"})
				if err != nil {
					t.Fatal(err)
				}
				latest, err := store.LatestTemplateRevision(flow.TemplateID)
				if err != nil {
					t.Fatal(err)
				}
				if flow.TemplateRevision != latest || latest < 1 {
					t.Fatalf("flow template_revision = %d, want the store's latest %d", flow.TemplateRevision, latest)
				}
				if flow.BindingID != "binding-create" {
					t.Fatalf("flow binding_id = %q, want binding-create", flow.BindingID)
				}
				// The stored document carries both facts.
				stored, err := engine.Get(ctx, flow.ID)
				if err != nil {
					t.Fatal(err)
				}
				if stored.TemplateRevision != latest || stored.BindingID != "binding-create" {
					t.Fatalf("stored flow = revision %d binding %q", stored.TemplateRevision, stored.BindingID)
				}

				// A custom template saved after another appends revision 2;
				// a flow created now records 2, not 1. It goes to a second
				// project because the first project's slot is taken.
				customID := uniqTemplateID("create_rev_")
				def := customRevisionFixture(customID)
				if err := store.PutTemplate(def); err != nil {
					t.Fatal(err)
				}
				changed := def
				changed.Description = "second revision"
				if err := store.PutTemplate(changed); err != nil {
					t.Fatal(err)
				}
				RegisterTemplate(changed)
				t.Cleanup(func() { customMu.Lock(); delete(customTemplates, customID); customMu.Unlock() })
				customFlow, err := engine.Create(ctx, &CreateFlowRequest{ProjectID: "proj-create-custom", TemplateID: customID})
				if err != nil {
					t.Fatal(err)
				}
				if customFlow.TemplateRevision != 2 {
					t.Fatalf("custom flow template_revision = %d, want 2", customFlow.TemplateRevision)
				}
				if customFlow.BindingID != "binding-create" {
					t.Fatalf("custom flow binding_id = %q, want binding-create", customFlow.BindingID)
				}
			})
		}
	})

	t.Run("an empty resolver answer leaves the binding empty", func(t *testing.T) {
		engine := NewInMemoryEngine(nil)
		engine.SetBindingResolver(&stubBindingResolver{
			bindings: map[string]string{},
			errs:     map[string]error{},
		})
		flow, err := engine.Create(ctx, &CreateFlowRequest{ProjectID: "proj-unbound"})
		if err != nil {
			t.Fatal(err)
		}
		if flow.BindingID != "" {
			t.Fatalf("binding_id = %q, want empty", flow.BindingID)
		}
	})

	t.Run("a resolver error fails Create and writes nothing", func(t *testing.T) {
		failure := errors.New("lookup exploded")
		for name, store := range newRevisionStoreCases(t) {
			t.Run(name, func(t *testing.T) {
				engine := NewEngineWithStore(store, nil)
				engine.SetBindingResolver(&stubBindingResolver{
					bindings: map[string]string{},
					errs:     map[string]error{"proj-err": failure},
				})
				before, err := store.List("")
				if err != nil {
					t.Fatal(err)
				}
				_, err = engine.Create(ctx, &CreateFlowRequest{ProjectID: "proj-err"})
				if err == nil {
					t.Fatal("Create succeeded despite a resolver failure")
				}
				if !errors.Is(err, failure) {
					t.Fatalf("Create error = %v, want the resolver's error", err)
				}
				after, err := store.List("")
				if err != nil {
					t.Fatal(err)
				}
				if len(after) != len(before) {
					t.Fatalf("Create wrote %d flow(s) despite failing", len(after)-len(before))
				}
			})
		}
	})

	t.Run("no resolver leaves the binding empty", func(t *testing.T) {
		engine := NewInMemoryEngine(nil)
		flow, err := engine.Create(ctx, &CreateFlowRequest{ProjectID: "proj-none"})
		if err != nil {
			t.Fatal(err)
		}
		if flow.BindingID != "" {
			t.Fatalf("binding_id = %q, want empty when no resolver is attached", flow.BindingID)
		}
	})
}

func TestRefusedCreateWritesNothingAndKeepsTheTemplateRevision(t *testing.T) {
	// The revision and the binding are resolved before the active-project
	// check; a Create that then loses the race for the slot must not have
	// written anything. This pins the fail-closed ordering on the error path
	// most likely to run in production.
	ctx := context.Background()
	engine := NewInMemoryEngine(nil)
	first, err := engine.Create(ctx, &CreateFlowRequest{ProjectID: "proj-race"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := engine.List(ctx, "proj-race")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Create(ctx, &CreateFlowRequest{ProjectID: "proj-race"}); err == nil {
		t.Fatal("a second active project flow was created")
	} else if !strings.Contains(err.Error(), first.ID) {
		t.Fatalf("the refusal does not name the holder: %v", err)
	}
	after, err := engine.List(ctx, "proj-race")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("the refused Create wrote %d flow(s)", len(after)-len(before))
	}
	// The builtin revision seeding is idempotent: the refused Create appended
	// no revision either.
	rs, ok := engine.store.(TemplateRevisionStore)
	if !ok {
		t.Fatal("the memory store does not keep revisions")
	}
	for _, id := range builtinTemplateIDs() {
		latest, err := rs.LatestTemplateRevision(id)
		if err != nil {
			t.Fatal(err)
		}
		if latest != 1 {
			t.Fatalf("builtin %s revision = %d after a refused Create, want 1", id, latest)
		}
	}
}

// builtinTemplateIDs returns the built-in ids in a stable order for tests.
func builtinTemplateIDs() []TemplateID {
	ids := make([]TemplateID, 0, len(builtinTemplates))
	for id := range builtinTemplates {
		ids = append(ids, id)
	}
	sortTemplateIDs(ids)
	return ids
}
