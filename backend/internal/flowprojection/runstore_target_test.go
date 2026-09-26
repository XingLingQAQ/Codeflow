package flowprojection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
)

// End-to-end tests of the legacy Flow bridge (T1.05.c): a real Flow engine on a
// file-backed floweng.db, the projector, and a real run store on codeflow.db.
// Nothing is faked except an injected failure of the local outbox mark, which
// is how the crash window between the two databases is reproduced.

type bridgeFixture struct {
	t      *testing.T
	flows  *floweng.SQLiteFlowStore
	engine *floweng.InMemoryEngine
	runs   *runstore.Store
}

func newBridgeFixture(t *testing.T) *bridgeFixture {
	t.Helper()
	dir := t.TempDir()
	flows, err := floweng.NewSQLiteFlowStore(filepath.Join(dir, "floweng.db"))
	if err != nil {
		t.Fatalf("open floweng.db: %v", err)
	}
	t.Cleanup(func() { _ = flows.Close() })
	runs, _, err := runstore.OpenStore(context.Background(), filepath.Join(dir, "codeflow.db"))
	if err != nil {
		t.Fatalf("open codeflow.db: %v", err)
	}
	t.Cleanup(func() { _ = runs.Close() })
	return &bridgeFixture{t: t, flows: flows, engine: floweng.NewEngineWithStore(flows, nil), runs: runs}
}

// flowTimeline creates a Flow for projectID and advances it once, returning the
// Flow's own event list (the order the legacy store recorded it in).
func (f *bridgeFixture) flowTimeline(projectID string) []floweng.FlowEvent {
	f.t.Helper()
	ctx := context.Background()
	flow, err := f.engine.Create(ctx, &floweng.CreateFlowRequest{ProjectID: projectID, TemplateID: floweng.TemplateNewProject})
	if err != nil {
		f.t.Fatalf("create flow: %v", err)
	}
	advanced, err := f.engine.Advance(ctx, flow.ID, &floweng.AdvanceRequest{})
	if err != nil {
		f.t.Fatalf("advance flow: %v", err)
	}
	if len(advanced.Events) < 2 {
		f.t.Fatalf("flow timeline has %d events, want at least create + advance", len(advanced.Events))
	}
	return advanced.Events
}

// knownProjects is a fake ProjectRefResolver over a fixed set of legacy
// projects: a project outside the set is ErrUnknownProject.
func knownProjects(ids ...string) ProjectRefResolver {
	known := make(map[string]bool, len(ids))
	for _, id := range ids {
		known[id] = true
	}
	return func(_ context.Context, projectID string) (run.ProjectRef, error) {
		if !known[projectID] {
			return run.ProjectRef{}, fmt.Errorf("%w: %s", ErrUnknownProject, projectID)
		}
		now := time.Now().UTC()
		return run.ProjectRef{
			ProjectID:    projectID,
			SnapshotHash: "sha256:" + projectID,
			State:        run.ProjectRefStateActive,
			CapturedAt:   now,
			VerifiedAt:   now,
		}, nil
	}
}

func (f *bridgeFixture) target(projects ...string) *RunstoreTarget {
	f.t.Helper()
	target, err := NewRunstoreTarget(f.runs, knownProjects(projects...),
		func(projectID string) []string { return []string{"ws:project:" + projectID} })
	if err != nil {
		f.t.Fatalf("NewRunstoreTarget: %v", err)
	}
	return target
}

// drain runs projection rounds until nothing is due any more.
func drain(t *testing.T, p *Projector) {
	t.Helper()
	for i := 0; i < 50; i++ {
		report, err := p.ProjectOnce(context.Background())
		if err != nil {
			t.Fatalf("ProjectOnce: %v", err)
		}
		if report.Due == 0 {
			return
		}
	}
	t.Fatal("projection did not drain in 50 rounds")
}

func projectedEvents(t *testing.T, runs *runstore.Store, projectID string) []runstore.Event {
	t.Helper()
	page, err := runstore.ReplayProject(context.Background(), runs.DB(), projectID, 0, runstore.MaxEventLimit)
	if err != nil {
		t.Fatalf("ReplayProject(%s): %v", projectID, err)
	}
	return page.Events
}

// TestLegacyFlowTimelineReachesTheRunStoreOnce is the bridge's happy path: every
// event the Flow engine recorded arrives in the run store once, in the Flow's
// own order, as legacy.flow_event with the legacy name in payload.source_type,
// a system actor, no run, and the Flow's own timestamps.
func TestLegacyFlowTimelineReachesTheRunStoreOnce(t *testing.T) {
	f := newBridgeFixture(t)
	timeline := f.flowTimeline("proj-bridge")

	p, err := New(Options{Source: f.flows, Target: f.target("proj-bridge")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	drain(t, p)

	got := projectedEvents(t, f.runs, "proj-bridge")
	if len(got) != len(timeline) {
		t.Fatalf("run store holds %d projected events, want the %d of the Flow timeline", len(got), len(timeline))
	}
	for i, ev := range got {
		if ev.ProjectSeq != int64(i+1) {
			t.Errorf("event %d has project_seq %d, want %d (gap-free)", i, ev.ProjectSeq, i+1)
		}
		if ev.Type != string(run.EventLegacyFlowEvent) || ev.RunID != nil {
			t.Errorf("event %d = type %q run %v, want legacy.flow_event without a run", i, ev.Type, ev.RunID)
		}
		var payload legacyFlowPayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Fatalf("event %d payload: %v", i, err)
		}
		want := timeline[i]
		if payload.SourceEventID != want.ID || payload.SourceType != want.Type || payload.SourceStore != LegacyFlowSourceStore {
			t.Errorf("event %d payload = %+v, want source %s type %s", i, payload, want.ID, want.Type)
		}
		if !ev.OccurredAt.Equal(want.Timestamp.UTC().Truncate(time.Millisecond)) {
			t.Errorf("event %d occurred_at = %s, want the Flow event's own %s", i, ev.OccurredAt, want.Timestamp)
		}
		var identity run.ExecutionIdentity
		if err := json.Unmarshal(ev.Identity, &identity); err != nil {
			t.Fatalf("event %d identity: %v", i, err)
		}
		if identity.Actor.Type != run.ActorTypeSystem || identity.Actor.ID != legacyFlowActorID || identity.ProjectID != "proj-bridge" {
			t.Errorf("event %d identity = %+v, want the system projector actor on proj-bridge", i, identity)
		}
	}
	pending, dead, err := f.flows.LegacyOutboxBacklog()
	if err != nil || pending != 0 || dead != 0 {
		t.Fatalf("local outbox backlog = %d pending / %d dead (err %v), want 0/0", pending, dead, err)
	}
}

// TestLegacyFlowCrashBetweenDatabasesDoesNotDuplicate reproduces the one window
// the two-database design leaves open: the run store committed the projection,
// the local outbox mark did not. The next round must re-project into the same
// run-store event — no second event, no second sequence number, no second
// outbox row — and then mark the local row.
func TestLegacyFlowCrashBetweenDatabasesDoesNotDuplicate(t *testing.T) {
	f := newBridgeFixture(t)
	timeline := f.flowTimeline("proj-crash")
	src := &recordingSource{SQLiteFlowStore: f.flows, failMarks: 1}
	p, err := New(Options{Source: src, Target: f.target("proj-crash")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := p.ProjectOnce(context.Background()); err == nil {
		t.Fatal("the first round must report the injected mark failure")
	}
	drain(t, p)

	got := projectedEvents(t, f.runs, "proj-crash")
	if len(got) != len(timeline) {
		t.Fatalf("run store holds %d events after the crash window, want exactly %d", len(got), len(timeline))
	}
	for i, ev := range got {
		if ev.ProjectSeq != int64(i+1) {
			t.Errorf("event %d has project_seq %d, want %d: the replay must not consume a sequence", i, ev.ProjectSeq, i+1)
		}
	}
	var outboxRows int
	if err := f.runs.DB().QueryRow(`SELECT COUNT(*) FROM outbox`).Scan(&outboxRows); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outboxRows != len(timeline) {
		t.Errorf("run-store outbox rows = %d, want %d (one per event, none for the replay)", outboxRows, len(timeline))
	}
	// The first source event was projected twice (once before the failed mark,
	// once after) and must map to one run-store event.
	first, found, err := runstore.LegacyEventFor(context.Background(), f.runs.DB(),
		runstore.LegacySource{Store: LegacyFlowSourceStore, EventID: timeline[0].ID})
	if err != nil || !found || first != got[0].ID {
		t.Fatalf("mapping of the replayed source event = (%q, %v, %v), want %q", first, found, err, got[0].ID)
	}
}

// TestRunstoreTargetClassifiesErrors: a request the run store refuses on its
// face is a PermanentError (dead-letter it), so is a source id reused for
// another project and a project the legacy store does not know; a closed store
// is a plain error (retry).
func TestRunstoreTargetClassifiesErrors(t *testing.T) {
	f := newBridgeFixture(t)
	target := f.target("proj-a", "proj-b")
	ctx := context.Background()
	base := floweng.LegacyOutboxEvent{
		SourceEventID: "src-1", FlowID: "flow-1", ProjectID: "proj-a",
		Type: "flow.created", OccurredAt: time.Now().UTC(),
	}

	if _, err := target.Project(ctx, base); err != nil {
		t.Fatalf("valid projection: %v", err)
	}

	var permanent *PermanentError
	reused := base
	reused.ProjectID = "proj-b"
	if _, err := target.Project(ctx, reused); !errors.As(err, &permanent) || !errors.Is(err, runstore.ErrLegacySourceConflict) {
		t.Errorf("source id reused for another project = %v, want a PermanentError wrapping ErrLegacySourceConflict", err)
	}

	noTime := base
	noTime.SourceEventID = "src-2"
	noTime.OccurredAt = time.Time{}
	if _, err := target.Project(ctx, noTime); !errors.As(err, &permanent) || !errors.Is(err, runstore.ErrInvalidEvent) {
		t.Errorf("event without occurred_at = %v, want a PermanentError wrapping ErrInvalidEvent", err)
	}

	unknown := base
	unknown.SourceEventID = "src-4"
	unknown.ProjectID = "proj-gone"
	if _, err := target.Project(ctx, unknown); !errors.As(err, &permanent) || !errors.Is(err, ErrUnknownProject) {
		t.Errorf("event of a project the legacy store does not know = %v, want a PermanentError wrapping ErrUnknownProject", err)
	}

	if _, err := NewRunstoreTarget(f.runs, nil, nil); err == nil {
		t.Error("NewRunstoreTarget without a project ref resolver must be refused")
	}

	blank := base
	blank.SourceEventID = ""
	if _, err := target.Project(ctx, blank); !errors.As(err, &permanent) || !errors.Is(err, runstore.ErrInvalidLegacySource) {
		t.Errorf("blank source id = %v, want a PermanentError wrapping ErrInvalidLegacySource", err)
	}

	if err := f.runs.Close(); err != nil {
		t.Fatalf("close run store: %v", err)
	}
	transient := base
	transient.SourceEventID = "src-3"
	_, err := target.Project(ctx, transient)
	if err == nil || errors.As(err, &permanent) {
		t.Errorf("projection into a closed store = %v, want a retryable (non-permanent) error", err)
	}
}
