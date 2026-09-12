package project

import (
	"context"
	"testing"

	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/storage"
)

type journalProjectService struct {
	*InMemoryProjectService
	records          map[string]*CreateOperationRecord
	lifecycleRecords map[string]*LifecycleOperationRecord
}

func newJournalProjectService() *journalProjectService {
	return &journalProjectService{
		InMemoryProjectService: NewInMemoryProjectService(),
		records:                map[string]*CreateOperationRecord{},
		lifecycleRecords:       map[string]*LifecycleOperationRecord{},
	}
}

func lifecycleTestKey(operation, projectID string) string {
	return operation + ":" + projectID
}

func (s *journalProjectService) GetLifecycleOperation(_ context.Context, operation, projectID string) (*LifecycleOperationRecord, error) {
	if record := s.lifecycleRecords[lifecycleTestKey(operation, projectID)]; record != nil {
		copy := *record
		return &copy, nil
	}
	return nil, nil
}

func (s *journalProjectService) ListIncompleteLifecycleOperations(context.Context) ([]*LifecycleOperationRecord, error) {
	var result []*LifecycleOperationRecord
	for _, record := range s.lifecycleRecords {
		if record.State == lifecycleOperationCompleted {
			continue
		}
		copy := *record
		result = append(result, &copy)
	}
	return result, nil
}

func (s *journalProjectService) SaveLifecycleOperation(_ context.Context, record *LifecycleOperationRecord) error {
	copy := *record
	s.lifecycleRecords[lifecycleTestKey(record.Operation, record.ProjectID)] = &copy
	return nil
}

func (s *journalProjectService) GetCreateOperation(_ context.Context, key string) (*CreateOperationRecord, error) {
	if record := s.records[key]; record != nil {
		copy := *record
		return &copy, nil
	}
	return nil, nil
}

func (s *journalProjectService) ListIncompleteCreateOperations(context.Context) ([]*CreateOperationRecord, error) {
	var result []*CreateOperationRecord
	for key, record := range s.records {
		if !isResumableCreateOperationState(record.State) {
			continue
		}
		copy := *record
		copy.IdempotencyKey = key
		result = append(result, &copy)
	}
	return result, nil
}

func (s *journalProjectService) SaveCreateOperation(_ context.Context, key string, record *CreateOperationRecord) error {
	copy := *record
	copy.IdempotencyKey = key
	s.records[key] = &copy
	return nil
}

type recoverySessionProvisioner struct {
	sessions    map[string]*storage.Session
	createCalls int
	deleteCalls int
}

func newRecoverySessionProvisioner() *recoverySessionProvisioner {
	return &recoverySessionProvisioner{sessions: map[string]*storage.Session{}}
}

func (p *recoverySessionProvisioner) CreateDefaultSession(_ context.Context, projectID, title string) (*storage.Session, error) {
	p.createCalls++
	id := "project-" + projectID
	if session := p.sessions[id]; session != nil {
		copy := *session
		return &copy, nil
	}
	session := &storage.Session{ID: id, Title: title}
	p.sessions[id] = session
	copy := *session
	return &copy, nil
}

func (p *recoverySessionProvisioner) GetSession(_ context.Context, id string) (*storage.Session, error) {
	if session := p.sessions[id]; session != nil {
		copy := *session
		return &copy, nil
	}
	return nil, nil
}

func (p *recoverySessionProvisioner) DeleteSession(context.Context, string) error {
	p.deleteCalls++
	return nil
}

func TestRecoverIncompleteProjectCreationFromFlowCreated(t *testing.T) {
	ctx := context.Background()
	resetIdempotentCreationCache()
	t.Cleanup(resetIdempotentCreationCache)
	projects := newJournalProjectService()
	flows := floweng.NewInMemoryEngine(nil)
	provisioner := newRecoverySessionProvisioner()
	SetSessionProvisioner(provisioner)
	t.Cleanup(func() { SetSessionProvisioner(nil) })

	const (
		key       = "recover-flow-created"
		projectID = "project-recovery"
		sessionID = "project-project-recovery"
	)
	provisioner.sessions[sessionID] = &storage.Session{ID: sessionID, Title: "Recovered session"}
	flow, err := flows.Create(ctx, &floweng.CreateFlowRequest{
		ProjectID:  projectID,
		TemplateID: floweng.TemplateNewProject,
		SessionID:  sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	projects.records[key] = &CreateOperationRecord{
		IdempotencyKey: key,
		ProjectID:      projectID,
		FlowID:         flow.ID,
		SessionID:      sessionID,
		State:          createOperationFlowCreated,
		Title:          "Recovered project",
	}

	diagnostics, err := RecoverIncompleteProjectCreations(ctx, projects, flows)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("recovery diagnostics=%v", diagnostics)
	}

	created, err := projects.GetProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if created == nil || created.DefaultFlowID != flow.ID || created.DefaultSessionID != sessionID {
		t.Fatalf("recovered project has wrong bindings: %+v", created)
	}
	if projects.records[key].State != createOperationCompleted {
		t.Fatalf("operation state=%q", projects.records[key].State)
	}
	listed, err := flows.List(ctx, projectID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("flows=%d err=%v", len(listed), err)
	}
	if provisioner.createCalls != 0 || provisioner.deleteCalls != 0 {
		t.Fatalf("session create/delete calls=%d/%d", provisioner.createCalls, provisioner.deleteCalls)
	}
}

func TestRecoverIncompleteProjectCreationFromPendingCreatesResourcesOnce(t *testing.T) {
	ctx := context.Background()
	resetIdempotentCreationCache()
	t.Cleanup(resetIdempotentCreationCache)
	projects := newJournalProjectService()
	flows := floweng.NewInMemoryEngine(nil)
	provisioner := newRecoverySessionProvisioner()
	SetSessionProvisioner(provisioner)
	t.Cleanup(func() { SetSessionProvisioner(nil) })

	const (
		key       = "recover-pending"
		projectID = "project-pending"
	)
	projects.records[key] = &CreateOperationRecord{
		IdempotencyKey: key,
		ProjectID:      projectID,
		State:          createOperationPending,
		Title:          "Pending project",
	}

	diagnostics, err := RecoverIncompleteProjectCreations(ctx, projects, flows)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("recovery diagnostics=%v", diagnostics)
	}
	first, err := CreateProjectWithDefaultFlowIdempotent(ctx, projects, flows, &ProjectCreateRequest{Title: "ignored retry title"}, key)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateProjectWithDefaultFlowIdempotent(ctx, projects, flows, &ProjectCreateRequest{Title: "another retry title"}, key)
	if err != nil {
		t.Fatal(err)
	}

	if first.ID != projectID || second.ID != projectID || first.Flow.ID != second.Flow.ID || first.Session.ID != second.Session.ID {
		t.Fatalf("retry changed bindings: first=%+v second=%+v", first, second)
	}
	if provisioner.createCalls != 1 || provisioner.deleteCalls != 0 || len(provisioner.sessions) != 1 {
		t.Fatalf("session creates/deletes/count=%d/%d/%d", provisioner.createCalls, provisioner.deleteCalls, len(provisioner.sessions))
	}
	listedFlows, err := flows.List(ctx, projectID)
	if err != nil || len(listedFlows) != 1 {
		t.Fatalf("flows=%d err=%v", len(listedFlows), err)
	}
	listedProjects, err := projects.ListProjects(ctx, &ProjectListRequest{})
	if err != nil || listedProjects.Total != 1 {
		t.Fatalf("projects=%+v err=%v", listedProjects, err)
	}
	if projects.records[key].State != createOperationCompleted {
		t.Fatalf("operation state=%q", projects.records[key].State)
	}
}

func TestRecoverIncompleteProjectArchiveAndRestoreReusesRuntime(t *testing.T) {
	ctx := context.Background()
	projects := newJournalProjectService()
	flows := floweng.NewInMemoryEngine(nil)
	provisioner := newRecoverySessionProvisioner()
	SetSessionProvisioner(provisioner)
	t.Cleanup(func() { SetSessionProvisioner(nil) })

	const (
		projectID = "lifecycle-project"
		sessionID = "project-lifecycle-project"
	)
	provisioner.sessions[sessionID] = &storage.Session{ID: sessionID, Title: "Lifecycle session"}
	initialFlow, err := flows.Create(ctx, &floweng.CreateFlowRequest{
		ProjectID:  projectID,
		TemplateID: floweng.TemplateNewProject,
		SessionID:  sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := projects.CreateProject(ctx, &ProjectCreateRequest{
		ID:               projectID,
		Title:            "Lifecycle project",
		DefaultFlowID:    initialFlow.ID,
		DefaultSessionID: sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	projects.lifecycleRecords[lifecycleTestKey(projectOperationArchive, projectID)] = &LifecycleOperationRecord{
		Operation: projectOperationArchive,
		ProjectID: projectID,
		FlowID:    initialFlow.ID,
		SessionID: sessionID,
		State:     lifecycleOperationPending,
	}

	if err := RecoverIncompleteProjectLifecycles(ctx, projects, flows); err != nil {
		t.Fatal(err)
	}
	archived, err := projects.GetProject(ctx, created.ID)
	if err != nil || archived == nil || archived.Status != StatusArchived {
		t.Fatalf("archived project=%+v err=%v", archived, err)
	}
	aborted, err := flows.Get(ctx, initialFlow.ID)
	if err != nil || aborted.Status != floweng.FlowStatusAborted {
		t.Fatalf("archived flow=%+v err=%v", aborted, err)
	}
	if projects.lifecycleRecords[lifecycleTestKey(projectOperationArchive, projectID)].State != lifecycleOperationCompleted {
		t.Fatal("archive operation was not completed")
	}

	replacement, err := flows.Create(ctx, &floweng.CreateFlowRequest{
		ProjectID:  projectID,
		TemplateID: floweng.TemplateNewProject,
		SessionID:  sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	projects.lifecycleRecords[lifecycleTestKey(projectOperationRestore, projectID)] = &LifecycleOperationRecord{
		Operation: projectOperationRestore,
		ProjectID: projectID,
		FlowID:    replacement.ID,
		SessionID: sessionID,
		State:     lifecycleOperationRuntimeReady,
	}

	if err := RecoverIncompleteProjectLifecycles(ctx, projects, flows); err != nil {
		t.Fatal(err)
	}
	restored, err := projects.GetProject(ctx, projectID)
	if err != nil || restored == nil || restored.Status != StatusPlanning {
		t.Fatalf("restored project=%+v err=%v", restored, err)
	}
	if restored.DefaultFlowID != replacement.ID || restored.DefaultSessionID != sessionID {
		t.Fatalf("restored bindings=%+v", restored)
	}
	listed, err := flows.List(ctx, projectID)
	if err != nil || len(listed) != 2 {
		t.Fatalf("flows=%d err=%v", len(listed), err)
	}
	if provisioner.createCalls != 0 || provisioner.deleteCalls != 0 {
		t.Fatalf("session create/delete calls=%d/%d", provisioner.createCalls, provisioner.deleteCalls)
	}
	if projects.lifecycleRecords[lifecycleTestKey(projectOperationRestore, projectID)].State != lifecycleOperationCompleted {
		t.Fatal("restore operation was not completed")
	}
}
