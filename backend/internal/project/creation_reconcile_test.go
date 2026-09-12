package project

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/storage"
)

func newReconcileFixture(t *testing.T) (*journalProjectService, floweng.Engine, *recoverySessionProvisioner) {
	t.Helper()
	resetIdempotentCreationCache()
	projects := newJournalProjectService()
	flows := floweng.NewInMemoryEngine(nil)
	provisioner := newRecoverySessionProvisioner()
	SetSessionProvisioner(provisioner)
	t.Cleanup(func() {
		SetSessionProvisioner(nil)
		resetIdempotentCreationCache()
	})
	return projects, flows, provisioner
}

func assertProjection(t *testing.T, projection *CreateOperationProjection, state string, missing []string) {
	t.Helper()
	if projection == nil {
		t.Fatalf("projection is nil, want state %q", state)
	}
	if projection.State != state {
		t.Fatalf("projection state=%q want %q (missing=%v reasons=%v)", projection.State, state, projection.MissingSteps, projection.Reasons)
	}
	if fmt.Sprint(projection.MissingSteps) != fmt.Sprint(missing) {
		t.Fatalf("missing steps=%v want %v", projection.MissingSteps, missing)
	}
	if state == CreateOperationProjectionDegraded && len(projection.Reasons) != len(missing) {
		t.Fatalf("degraded projection reasons=%v, want one explanation per missing step", projection.Reasons)
	}
}

func TestReconcileCreateOperationProjectionStates(t *testing.T) {
	ctx := context.Background()

	t.Run("pending operation misses project flow session", func(t *testing.T) {
		projects, flows, _ := newReconcileFixture(t)
		record := &CreateOperationRecord{IdempotencyKey: "k", ProjectID: "p1", State: createOperationPending}
		projection, err := ReconcileCreateOperation(ctx, projects, flows, record)
		if err != nil {
			t.Fatal(err)
		}
		assertProjection(t, projection, CreateOperationProjectionDegraded, []string{CreateOperationStepProject, CreateOperationStepFlow, CreateOperationStepSession})
	})

	t.Run("session created misses project flow", func(t *testing.T) {
		projects, flows, provisioner := newReconcileFixture(t)
		provisioner.sessions["s1"] = &storage.Session{ID: "s1", Title: "s1"}
		record := &CreateOperationRecord{IdempotencyKey: "k", ProjectID: "p1", SessionID: "s1", State: createOperationSessionCreated}
		projection, err := ReconcileCreateOperation(ctx, projects, flows, record)
		if err != nil {
			t.Fatal(err)
		}
		assertProjection(t, projection, CreateOperationProjectionDegraded, []string{CreateOperationStepProject, CreateOperationStepFlow})
	})

	t.Run("flow created misses only project", func(t *testing.T) {
		projects, flows, provisioner := newReconcileFixture(t)
		provisioner.sessions["s1"] = &storage.Session{ID: "s1", Title: "s1"}
		flow, err := flows.Create(ctx, &floweng.CreateFlowRequest{ProjectID: "p1", TemplateID: floweng.TemplateNewProject, SessionID: "s1"})
		if err != nil {
			t.Fatal(err)
		}
		record := &CreateOperationRecord{IdempotencyKey: "k", ProjectID: "p1", SessionID: "s1", FlowID: flow.ID, State: createOperationFlowCreated}
		projection, err := ReconcileCreateOperation(ctx, projects, flows, record)
		if err != nil {
			t.Fatal(err)
		}
		assertProjection(t, projection, CreateOperationProjectionDegraded, []string{CreateOperationStepProject})
		if !strings.Contains(strings.Join(projection.Reasons, ";"), "p1") {
			t.Fatalf("degraded reasons do not name the missing project: %v", projection.Reasons)
		}
	})

	t.Run("lost session degrades with the session step named", func(t *testing.T) {
		projects, flows, _ := newReconcileFixture(t)
		flow, err := flows.Create(ctx, &floweng.CreateFlowRequest{ProjectID: "p1", TemplateID: floweng.TemplateNewProject, SessionID: "lost"})
		if err != nil {
			t.Fatal(err)
		}
		record := &CreateOperationRecord{IdempotencyKey: "k", ProjectID: "p1", SessionID: "lost", FlowID: flow.ID, State: createOperationFlowCreated}
		projection, err := ReconcileCreateOperation(ctx, projects, flows, record)
		if err != nil {
			t.Fatal(err)
		}
		assertProjection(t, projection, CreateOperationProjectionDegraded, []string{CreateOperationStepProject, CreateOperationStepSession})
		if !strings.Contains(strings.Join(projection.Reasons, ";"), "lost") {
			t.Fatalf("degraded reasons do not name the lost session: %v", projection.Reasons)
		}
	})

	t.Run("completed operation is ready", func(t *testing.T) {
		projects, flows, _ := newReconcileFixture(t)
		if _, err := CreateProjectWithDefaultFlowIdempotent(ctx, projects, flows, &ProjectCreateRequest{Title: "ready"}, "ready-key"); err != nil {
			t.Fatal(err)
		}
		record, err := projects.GetCreateOperation(ctx, "ready-key")
		if err != nil {
			t.Fatal(err)
		}
		projection, err := ReconcileCreateOperation(ctx, projects, flows, record)
		if err != nil {
			t.Fatal(err)
		}
		assertProjection(t, projection, CreateOperationProjectionReady, nil)
	})

	t.Run("replay continues a degraded operation without duplicating resources", func(t *testing.T) {
		projects, flows, provisioner := newReconcileFixture(t)
		provisioner.sessions["s1"] = &storage.Session{ID: "s1", Title: "s1"}
		flow, err := flows.Create(ctx, &floweng.CreateFlowRequest{ProjectID: "p1", TemplateID: floweng.TemplateNewProject, SessionID: "s1"})
		if err != nil {
			t.Fatal(err)
		}
		req := &ProjectCreateRequest{Title: "Continued"}
		projects.records["cont-key"] = &CreateOperationRecord{
			IdempotencyKey:        "cont-key",
			ProjectID:             "p1",
			SessionID:             "s1",
			FlowID:                flow.ID,
			State:                 createOperationFlowCreated,
			Title:                 "Continued",
			NormalizedRequestHash: mustHash(t, req),
		}

		created, err := CreateProjectWithDefaultFlowIdempotent(ctx, projects, flows, req, "cont-key")
		if err != nil {
			t.Fatal(err)
		}
		if created.ID != "p1" || created.Flow.ID != flow.ID || created.Session.ID != "s1" {
			t.Fatalf("replay did not reuse the recorded resources: %+v", created)
		}
		if provisioner.createCalls != 0 {
			t.Fatalf("existing session was recreated: %d calls", provisioner.createCalls)
		}
		listed, err := flows.List(ctx, "p1")
		if err != nil || len(listed) != 1 {
			t.Fatalf("flows=%d err=%v", len(listed), err)
		}
		stored, err := projects.GetCreateOperation(ctx, "cont-key")
		if err != nil {
			t.Fatal(err)
		}
		if stored.State != createOperationCompleted {
			t.Fatalf("operation state=%q", stored.State)
		}
		projection, err := ReconcileCreateOperation(ctx, projects, flows, stored)
		if err != nil {
			t.Fatal(err)
		}
		assertProjection(t, projection, CreateOperationProjectionReady, nil)
	})
}

func TestReconcileCreateOperationProjectionFailed(t *testing.T) {
	ctx := context.Background()

	t.Run("project bound to a flow that no longer exists", func(t *testing.T) {
		projects, flows, provisioner := newReconcileFixture(t)
		provisioner.sessions["s1"] = &storage.Session{ID: "s1", Title: "s1"}
		flow, err := flows.Create(ctx, &floweng.CreateFlowRequest{ProjectID: "p1", TemplateID: floweng.TemplateNewProject, SessionID: "s1"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := projects.CreateProject(ctx, &ProjectCreateRequest{ID: "p1", Title: "Bound", DefaultFlowID: flow.ID, DefaultSessionID: "s1"}); err != nil {
			t.Fatal(err)
		}
		if err := flows.Delete(ctx, flow.ID); err != nil {
			t.Fatal(err)
		}
		record := &CreateOperationRecord{IdempotencyKey: "fail-key", ProjectID: "p1", SessionID: "s1", FlowID: flow.ID, State: createOperationFlowCreated, Title: "Bound"}

		projection, err := ReconcileCreateOperation(ctx, projects, flows, record)
		if err != nil {
			t.Fatal(err)
		}
		assertProjection(t, projection, CreateOperationProjectionFailed, nil)
		if !strings.Contains(strings.Join(projection.Reasons, ";"), flow.ID) {
			t.Fatalf("failed reasons do not name the missing flow: %v", projection.Reasons)
		}

		projects.records["fail-key"] = record
		_, err = CreateProjectWithDefaultFlowIdempotent(ctx, projects, flows, &ProjectCreateRequest{Title: "Bound"}, "fail-key")
		var reconcileErr *ProjectCreateReconcileError
		if !errors.As(err, &reconcileErr) {
			t.Fatalf("replay error=%v, want *ProjectCreateReconcileError", err)
		}
		if reconcileErr.IdempotencyKey != "fail-key" || reconcileErr.ProjectID != "p1" {
			t.Fatalf("reconcile error identity=%q/%q", reconcileErr.IdempotencyKey, reconcileErr.ProjectID)
		}
		if reconcileErr.Projection == nil || reconcileErr.Projection.State != CreateOperationProjectionFailed {
			t.Fatalf("reconcile error projection=%+v", reconcileErr.Projection)
		}
	})

	t.Run("project bound to different resources", func(t *testing.T) {
		projects, flows, provisioner := newReconcileFixture(t)
		provisioner.sessions["s1"] = &storage.Session{ID: "s1", Title: "s1"}
		flow, err := flows.Create(ctx, &floweng.CreateFlowRequest{ProjectID: "p1", TemplateID: floweng.TemplateNewProject, SessionID: "s1"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := projects.CreateProject(ctx, &ProjectCreateRequest{ID: "p1", Title: "Bound", DefaultFlowID: "other-flow", DefaultSessionID: "s1"}); err != nil {
			t.Fatal(err)
		}
		record := &CreateOperationRecord{IdempotencyKey: "k", ProjectID: "p1", SessionID: "s1", FlowID: flow.ID, State: createOperationFlowCreated, Title: "Bound"}

		projection, err := ReconcileCreateOperation(ctx, projects, flows, record)
		if err != nil {
			t.Fatal(err)
		}
		assertProjection(t, projection, CreateOperationProjectionFailed, nil)
		joined := strings.Join(projection.Reasons, ";")
		if !strings.Contains(joined, "other-flow") || !strings.Contains(joined, flow.ID) {
			t.Fatalf("failed reasons do not explain the binding mismatch: %v", projection.Reasons)
		}
	})
}

func TestReplaySameKeySameHashReusesOriginalResources(t *testing.T) {
	ctx := context.Background()
	resetIdempotentCreationCache()
	t.Cleanup(resetIdempotentCreationCache)
	dbPath := filepath.Join(t.TempDir(), "project.db")
	svc, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteProjectService failed: %v", err)
	}
	flows := floweng.NewInMemoryEngine(nil)
	provisioner := newRecoverySessionProvisioner()
	SetSessionProvisioner(provisioner)
	t.Cleanup(func() { SetSessionProvisioner(nil) })

	req := &ProjectCreateRequest{Title: "Replay me", Tags: []string{"a", "b"}, Metadata: map[string]interface{}{"owner": "qa"}}
	created, err := CreateProjectWithDefaultFlowIdempotent(ctx, svc, flows, req, "replay-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopening bypasses the in-process success cache, so the replay exercises
	// the durable journal path and must still reuse the original resources.
	svc2, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer svc2.Close()
	replayed, err := CreateProjectWithDefaultFlowIdempotent(ctx, svc2, flows, req, "replay-key")
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != created.ID || replayed.Flow.ID != created.Flow.ID || replayed.Session.ID != created.Session.ID {
		t.Fatalf("replay returned different resources: %+v vs %+v", replayed, created)
	}
	listed, err := svc2.ListProjects(ctx, &ProjectListRequest{})
	if err != nil || listed.Total != 1 {
		t.Fatalf("projects=%+v err=%v", listed, err)
	}
	listedFlows, err := flows.List(ctx, created.ID)
	if err != nil || len(listedFlows) != 1 {
		t.Fatalf("flows=%d err=%v", len(listedFlows), err)
	}
	if provisioner.createCalls != 1 || len(provisioner.sessions) != 1 {
		t.Fatalf("session creates=%d count=%d, want exactly one", provisioner.createCalls, len(provisioner.sessions))
	}

	record, projection, err := GetCreateOperationProjection(ctx, svc2, flows, "replay-key")
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.State != createOperationCompleted {
		t.Fatalf("record=%+v", record)
	}
	assertProjection(t, projection, CreateOperationProjectionReady, nil)
}

func TestReconcileProjectionStableAcrossReopenAndRecovery(t *testing.T) {
	ctx := context.Background()
	resetIdempotentCreationCache()
	t.Cleanup(resetIdempotentCreationCache)
	dbPath := filepath.Join(t.TempDir(), "project.db")
	svc, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteProjectService failed: %v", err)
	}
	flows := floweng.NewInMemoryEngine(nil)
	provisioner := newRecoverySessionProvisioner()
	SetSessionProvisioner(provisioner)
	t.Cleanup(func() { SetSessionProvisioner(nil) })

	// Hand-construct an operation interrupted after flow_created: session and
	// flow are durable, the project was never persisted.
	const (
		key       = "interrupted-key"
		projectID = "interrupted-project"
		sessionID = "project-interrupted-project"
	)
	provisioner.sessions[sessionID] = &storage.Session{ID: sessionID, Title: "Interrupted session"}
	flow, err := flows.Create(ctx, &floweng.CreateFlowRequest{ProjectID: projectID, TemplateID: floweng.TemplateNewProject, SessionID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	req := &ProjectCreateRequest{Title: "Interrupted"}
	if _, err := svc.db.Exec(`
		INSERT INTO project_operations (idempotency_key, project_id, flow_id, session_id, title, workspace_root, normalized_request_hash, operation, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, NULL, ?, 'create_project', ?, 1000, 1000)
	`, key, projectID, flow.ID, sessionID, "Interrupted", mustHash(t, req), createOperationFlowCreated); err != nil {
		t.Fatalf("seed interrupted operation failed: %v", err)
	}

	// The projection is derived from the durable record and the live stores,
	// so it is stable across service restarts.
	assertDegradedProject := func(t *testing.T, svc *SQLiteProjectService) {
		t.Helper()
		record, projection, err := GetCreateOperationProjection(ctx, svc, flows, key)
		if err != nil {
			t.Fatal(err)
		}
		if record == nil || record.State != createOperationFlowCreated {
			t.Fatalf("record=%+v", record)
		}
		assertProjection(t, projection, CreateOperationProjectionDegraded, []string{CreateOperationStepProject})
		if !strings.Contains(strings.Join(projection.Reasons, ";"), projectID) {
			t.Fatalf("degraded reasons do not name the project: %v", projection.Reasons)
		}
	}
	assertDegradedProject(t, svc)
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	svc2, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	assertDegradedProject(t, svc2)

	// Recovery completes the hashed operation from its missing step: there is
	// no client payload to conflict with, and existing resources are reused.
	diagnostics, err := RecoverIncompleteProjectCreations(ctx, svc2, flows)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("recovery diagnostics=%v", diagnostics)
	}
	project, err := svc2.GetProject(ctx, projectID)
	if err != nil || project == nil {
		t.Fatalf("recovered project=%+v err=%v", project, err)
	}
	if project.DefaultFlowID != flow.ID || project.DefaultSessionID != sessionID {
		t.Fatalf("recovered bindings=%+v", project)
	}
	listedFlows, err := flows.List(ctx, projectID)
	if err != nil || len(listedFlows) != 1 {
		t.Fatalf("flows=%d err=%v", len(listedFlows), err)
	}
	if provisioner.createCalls != 0 {
		t.Fatalf("session recreated during recovery: %d calls", provisioner.createCalls)
	}
	record, projection, err := GetCreateOperationProjection(ctx, svc2, flows, key)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != createOperationCompleted {
		t.Fatalf("operation state=%q", record.State)
	}
	assertProjection(t, projection, CreateOperationProjectionReady, nil)
	if err := svc2.Close(); err != nil {
		t.Fatal(err)
	}

	svc3, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("second reopen failed: %v", err)
	}
	defer svc3.Close()
	_, projection, err = GetCreateOperationProjection(ctx, svc3, flows, key)
	if err != nil {
		t.Fatal(err)
	}
	assertProjection(t, projection, CreateOperationProjectionReady, nil)
}
