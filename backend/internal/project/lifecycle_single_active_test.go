package project

// T3.01.a group 2 acceptance tests for the project lifecycle.
//
// The rule "one active project flow per project" is the engine's (internal/
// floweng), but the project lifecycle is where it is easiest to break without
// touching the engine: restore used to look for a flow *with the record's
// session* and create one when it did not find it, which is exactly what the
// engine now refuses. These tests pin the four shapes the plan names for this
// step:
//
//   - restore with an active project flow that carries a different session than
//     the record naming it reuses that flow, creates nothing, does not fail, and
//     binds the project to the flow's own session;
//   - a restore that was interrupted after its flow was created but before
//     runtime_ready is re-run without creating a second flow;
//   - archiving ends both the active and the suspended flows of a project;
//   - a project with no active project flow still gets one created.
//
// The fixtures are the real ones the neighbouring lifecycle tests use: the
// in-memory journal project service, a real flow engine, and a recording
// session provisioner installed through the package-level hook, so these tests
// exercise the same code path a running server does. The archive test opens a
// real SQLite flow store, because a suspended flow only ever arrives through the
// store (the engine refuses to create a second project flow, and suspending is
// the migration's or a later step's job).

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/storage"
)

// installLifecycleProvisioner makes prov the process-wide session provisioner
// for the duration of one test.
func installLifecycleProvisioner(t *testing.T, prov *recoverySessionProvisioner) {
	t.Helper()
	SetSessionProvisioner(prov)
	t.Cleanup(func() { SetSessionProvisioner(nil) })
}

// newLifecycleProject creates a project row with the given flow/session
// bindings, without a flow being created by the creation coordinator: these
// tests set up the flow side themselves.
func newLifecycleProject(t *testing.T, svc *journalProjectService, projectID, flowID, sessionID string) *Project {
	t.Helper()
	p, err := svc.CreateProject(context.Background(), &ProjectCreateRequest{
		ID:               projectID,
		Title:            "Lifecycle " + projectID,
		DefaultFlowID:    flowID,
		DefaultSessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("create project %s: %v", projectID, err)
	}
	return p
}

// archiveLifecycleProject puts the project row in the state a restore starts
// from: archived, so the restore has real work to do.
func archiveLifecycleProject(t *testing.T, svc *journalProjectService, projectID string) {
	t.Helper()
	if err := svc.DeleteProject(context.Background(), projectID); err != nil {
		t.Fatalf("archive project %s: %v", projectID, err)
	}
}

// createActiveFlow creates one active project flow through the engine, with the
// given session, and returns it.
func createActiveFlow(t *testing.T, eng floweng.Engine, projectID, sessionID string) *floweng.Flow {
	t.Helper()
	flow, err := eng.Create(context.Background(), &floweng.CreateFlowRequest{
		ProjectID:  projectID,
		TemplateID: floweng.TemplateNewProject,
		SessionID:  sessionID,
		Kind:       floweng.FlowKindProject,
	})
	if err != nil {
		t.Fatalf("create project flow for %s: %v", projectID, err)
	}
	return flow
}

// newSQLiteLifecycleEngine opens a real store-backed engine and returns it with
// its store, so a test can also write the shapes the engine does not create
// (a suspended project flow).
func newSQLiteLifecycleEngine(t *testing.T) (*floweng.InMemoryEngine, *floweng.SQLiteFlowStore) {
	t.Helper()
	store, err := floweng.NewSQLiteFlowStore(filepath.Join(t.TempDir(), "floweng.db"))
	if err != nil {
		t.Fatalf("open flow store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return floweng.NewEngineWithStore(store, nil), store
}

// storeSuspendedProjectFlow writes a suspended project flow straight into the
// store, the shape the store migration leaves behind: a complete document with
// stages and events, parked, not a task flow.
func storeSuspendedProjectFlow(t *testing.T, store *floweng.SQLiteFlowStore, projectID string) *floweng.Flow {
	t.Helper()
	now := time.Now().UTC().Add(-time.Hour)
	flow := &floweng.Flow{
		ID:         uuid.NewString(),
		ProjectID:  projectID,
		TemplateID: floweng.TemplateNewProject,
		Status:     floweng.FlowStatusSuspended,
		Kind:       floweng.FlowKindProject,
		Stages: []floweng.Stage{{ID: uuid.NewString(), Type: floweng.StageTypeIdea, Name: "想法",
			Canvas: "intent", Status: floweng.StageStatusActive, Order: 0}},
		Artifacts: []floweng.Artifact{},
		Events:    []floweng.FlowEvent{{ID: uuid.NewString(), Type: "flow.created", Message: "created", Timestamp: now}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Put(flow); err != nil {
		t.Fatalf("store the suspended flow: %v", err)
	}
	return flow
}

// storedFlowCount counts the flows a project holds (any kind, any status).
func storedFlowCount(t *testing.T, eng floweng.Engine, projectID string) int {
	t.Helper()
	items, err := eng.List(context.Background(), projectID)
	if err != nil {
		t.Fatalf("list flows of %s: %v", projectID, err)
	}
	return len(items)
}

// TestRestoreReusesTheActiveProjectFlowOfAnotherSession is the case the old
// session-matching search got wrong: the project already has an active project
// flow, and the lifecycle record (or the project row) names a different session.
// The restore must adopt the flow of record — and therefore its session — not
// try to create a second flow, which the engine refuses and which made the
// startup recovery retry the same failing restore.
func TestRestoreReusesTheActiveProjectFlowOfAnotherSession(t *testing.T) {
	ctx := context.Background()
	projects := newJournalProjectService()
	flows := floweng.NewInMemoryEngine(nil)
	prov := newRecoverySessionProvisioner()
	installLifecycleProvisioner(t, prov)

	const projectID = "proj-reuse"
	// The project row names an old flow and an old session; the project is
	// archived, so the restore has real work to do.
	recordedSessionID := "session-recorded"
	prov.sessions[recordedSessionID] = &storage.Session{ID: recordedSessionID, Title: "recorded"}
	newLifecycleProject(t, projects, projectID, "flow-recorded", recordedSessionID)
	archiveLifecycleProject(t, projects, projectID)

	// The flow of record is active, belongs to the project, and runs on a
	// session of its own that neither the project row nor the record knows.
	flowSessionID := "session-of-the-flow"
	prov.sessions[flowSessionID] = &storage.Session{ID: flowSessionID, Title: "flow session"}
	flow := createActiveFlow(t, flows, projectID, flowSessionID)

	restored, err := RestoreProjectWithDefaultFlow(ctx, projects, flows, projectID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Flow.ID != flow.ID {
		t.Fatalf("restore did not reuse the flow of record: got %s want %s", restored.Flow.ID, flow.ID)
	}
	if got := storedFlowCount(t, flows, projectID); got != 1 {
		t.Fatalf("the project holds %d flows after the restore, want 1 (no second flow)", got)
	}
	if restored.Session.ID != flowSessionID {
		t.Fatalf("restored session = %s, want the flow's own %s", restored.Session.ID, flowSessionID)
	}
	p, err := projects.GetProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if p.DefaultFlowID != flow.ID || p.DefaultSessionID != flowSessionID {
		t.Fatalf("bindings = flow %s session %s, want %s/%s", p.DefaultFlowID, p.DefaultSessionID, flow.ID, flowSessionID)
	}
	if p.Status != StatusPlanning {
		t.Fatalf("restored project status = %s", p.Status)
	}
	record := projects.lifecycleRecords[lifecycleTestKey(projectOperationRestore, projectID)]
	if record == nil || record.State != lifecycleOperationCompleted {
		t.Fatalf("restore record = %+v, want completed", record)
	}
	if record.FlowID != flow.ID || record.SessionID != flowSessionID {
		t.Fatalf("record bindings = %s/%s, want %s/%s", record.FlowID, record.SessionID, flow.ID, flowSessionID)
	}
}

// TestRestoreRetryAfterInterruptionDoesNotCreateASecondFlow is the recovery
// loop's case: the first restore attempt created the project's flow and then
// died before runtime_ready, so the record still says "pending" while the flow
// exists. The second attempt must find and reuse it.
func TestRestoreRetryAfterInterruptionDoesNotCreateASecondFlow(t *testing.T) {
	ctx := context.Background()
	projects := newJournalProjectService()
	flows := floweng.NewInMemoryEngine(nil)
	prov := newRecoverySessionProvisioner()
	installLifecycleProvisioner(t, prov)

	const projectID = "proj-retry"
	const sessionID = "session-stale"
	prov.sessions[sessionID] = &storage.Session{ID: sessionID, Title: "stale"}
	newLifecycleProject(t, projects, projectID, "flow-stale", sessionID)
	archiveLifecycleProject(t, projects, projectID)

	// What the interrupted attempt left behind: an active project flow, and a
	// restore record still parked before runtime_ready.
	flow := createActiveFlow(t, flows, projectID, sessionID)
	projects.lifecycleRecords[lifecycleTestKey(projectOperationRestore, projectID)] = &LifecycleOperationRecord{
		Operation: projectOperationRestore,
		ProjectID: projectID,
		FlowID:    flow.ID,
		SessionID: flow.SessionID,
		State:     lifecycleOperationPending,
	}
	if got := storedFlowCount(t, flows, projectID); got != 1 {
		t.Fatalf("fixture holds %d flows, want 1", got)
	}

	// The startup recovery is what a restart runs.
	if err := RecoverIncompleteProjectLifecycles(ctx, projects, flows); err != nil {
		t.Fatalf("recover: %v", err)
	}
	if got := storedFlowCount(t, flows, projectID); got != 1 {
		t.Fatalf("the recovery created a second flow: %d flows", got)
	}
	listed, err := flows.List(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if listed[0].ID != flow.ID {
		t.Fatalf("the recovery discarded the interrupted flow: %s", listed[0].ID)
	}
	record := projects.lifecycleRecords[lifecycleTestKey(projectOperationRestore, projectID)]
	if record.State != lifecycleOperationCompleted {
		t.Fatalf("restore record state = %s, want completed", record.State)
	}
	p, err := projects.GetProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if p.DefaultFlowID != flow.ID || p.DefaultSessionID != flow.SessionID {
		t.Fatalf("bindings after the retry = %s/%s, want %s/%s",
			p.DefaultFlowID, p.DefaultSessionID, flow.ID, flow.SessionID)
	}
	if p.Status != StatusPlanning {
		t.Fatalf("recovered project status = %s", p.Status)
	}
}

// TestArchiveAbortsActiveAndSuspendedFlows pins archive as "the project has no
// runnable flow left": the active flow of record is aborted, and a suspended
// one is too — otherwise an archived project keeps a parked flow that a later
// resume could bring back. A terminal flow is history and is left alone.
func TestArchiveAbortsActiveAndSuspendedFlows(t *testing.T) {
	ctx := context.Background()
	projects := newJournalProjectService()
	flows, store := newSQLiteLifecycleEngine(t)
	installLifecycleProvisioner(t, newRecoverySessionProvisioner())

	const projectID = "proj-archive"
	active := createActiveFlow(t, flows, projectID, "session-active")
	parked := storeSuspendedProjectFlow(t, store, projectID)
	newLifecycleProject(t, projects, projectID, active.ID, active.SessionID)

	// A third flow that is already terminal: the archive must not touch it.
	task, err := flows.Create(ctx, &floweng.CreateFlowRequest{ProjectID: projectID, Kind: floweng.FlowKindTask})
	if err != nil {
		t.Fatalf("create task flow: %v", err)
	}
	terminal, err := flows.Abort(ctx, task.ID, "done earlier")
	if err != nil {
		t.Fatalf("abort task flow: %v", err)
	}
	terminalRevision, terminalEvents := terminal.Revision, len(terminal.Events)

	if err := ArchiveProjectAndAbortFlows(ctx, projects, flows, projectID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if got := storedFlowCount(t, flows, projectID); got != 3 {
		t.Fatalf("the project holds %d flows after the archive, want 3", got)
	}
	for _, want := range []struct{ id, why string }{
		{active.ID, "the active flow of record"},
		{parked.ID, "the suspended flow"},
	} {
		flow, err := flows.Get(ctx, want.id)
		if err != nil {
			t.Fatal(err)
		}
		if flow.Status != floweng.FlowStatusAborted {
			t.Fatalf("%s (%s) is %s after the archive, want aborted", want.id, want.why, flow.Status)
		}
		if !strings.Contains(joinEventTypes(flow.Events), "flow.aborted") {
			t.Fatalf("%s has no flow.aborted event: %v", want.id, joinEventTypes(flow.Events))
		}
	}
	kept, err := flows.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Status != floweng.FlowStatusAborted || kept.Revision != terminalRevision || len(kept.Events) != terminalEvents {
		t.Fatalf("the already-aborted flow was written again: status %s revision %d events %d, want aborted/%d/%d",
			kept.Status, kept.Revision, len(kept.Events), terminalRevision, terminalEvents)
	}
	p, err := projects.GetProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusArchived {
		t.Fatalf("project status after the archive = %s", p.Status)
	}
}

// TestRestoreCreatesAFlowWhenTheProjectHasNone keeps the ordinary path honest:
// with no active project flow to reuse, the restore creates exactly one, tagged
// as the project's flow of record.
func TestRestoreCreatesAFlowWhenTheProjectHasNone(t *testing.T) {
	ctx := context.Background()
	projects := newJournalProjectService()
	flows := floweng.NewInMemoryEngine(nil)
	prov := newRecoverySessionProvisioner()
	installLifecycleProvisioner(t, prov)

	const projectID = "proj-fresh"
	newLifecycleProject(t, projects, projectID, "", "")
	archiveLifecycleProject(t, projects, projectID)
	if got := storedFlowCount(t, flows, projectID); got != 0 {
		t.Fatalf("fixture holds %d flows, want none", got)
	}

	restored, err := RestoreProjectWithDefaultFlow(ctx, projects, flows, projectID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Flow == nil || restored.Flow.Status != floweng.FlowStatusActive {
		t.Fatalf("restored flow = %+v", restored.Flow)
	}
	if restored.Flow.Kind != floweng.FlowKindProject {
		t.Fatalf("the created flow is kind %q, want project", restored.Flow.Kind)
	}
	if got := storedFlowCount(t, flows, projectID); got != 1 {
		t.Fatalf("the project holds %d flows, want 1", got)
	}
	if restored.Flow.SessionID != restored.Session.ID {
		t.Fatalf("created flow session = %q, want %q", restored.Flow.SessionID, restored.Session.ID)
	}
	p, err := projects.GetProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if p.DefaultFlowID != restored.Flow.ID {
		t.Fatalf("bindings = %s, want the created %s", p.DefaultFlowID, restored.Flow.ID)
	}
}

// TestRestoreReusesFlowCreatedByAnInterruptedRestoreOfAWrongSession is the
// combination the two rules have to get right together: the interrupted attempt
// created its flow with the record's session, a later run of the record names a
// session that does not exist any more, and the flow of record must still be
// reused rather than replaced.
func TestRestoreReusesFlowCreatedByAnInterruptedRestoreOfAWrongSession(t *testing.T) {
	ctx := context.Background()
	projects := newJournalProjectService()
	flows := floweng.NewInMemoryEngine(nil)
	prov := newRecoverySessionProvisioner()
	installLifecycleProvisioner(t, prov)

	const projectID = "proj-drift"
	prov.sessions["session-old"] = &storage.Session{ID: "session-old", Title: "old"}
	newLifecycleProject(t, projects, projectID, "", "session-old")
	archiveLifecycleProject(t, projects, projectID)

	flow := createActiveFlow(t, flows, projectID, "session-old")
	// The record now names a session that the provisioner no longer has (the
	// user recreated the project's runtime in another session).
	projects.lifecycleRecords[lifecycleTestKey(projectOperationRestore, projectID)] = &LifecycleOperationRecord{
		Operation: projectOperationRestore,
		ProjectID: projectID,
		FlowID:    flow.ID,
		SessionID: "session-gone",
		State:     lifecycleOperationRuntimeReady,
	}

	restored, err := RestoreProjectWithDefaultFlow(ctx, projects, flows, projectID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Flow.ID != flow.ID {
		t.Fatalf("restore replaced the flow of record: got %s want %s", restored.Flow.ID, flow.ID)
	}
	if got := storedFlowCount(t, flows, projectID); got != 1 {
		t.Fatalf("the project holds %d flows, want 1", got)
	}
	if restored.Session.ID != "session-old" {
		t.Fatalf("restored session = %s, want the flow's own session-old", restored.Session.ID)
	}
}

// joinEventTypes renders a flow's event types for a failure message.
func joinEventTypes(events []floweng.FlowEvent) string {
	parts := make([]string, 0, len(events))
	for _, ev := range events {
		parts = append(parts, ev.Type)
	}
	return strings.Join(parts, ",")
}
