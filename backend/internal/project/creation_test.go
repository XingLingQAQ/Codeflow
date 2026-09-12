package project

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/storage"
)

type failingCreateProjectService struct {
	*InMemoryProjectService
}

type recordingSessionProvisioner struct {
	created *storage.Session
	deleted string
}

func (p *recordingSessionProvisioner) CreateDefaultSession(context.Context, string, string) (*storage.Session, error) {
	p.created = &storage.Session{ID: "session-test", Title: "session"}
	return p.created, nil
}

func (p *recordingSessionProvisioner) GetSession(_ context.Context, id string) (*storage.Session, error) {
	if p.created != nil && p.created.ID == id {
		return p.created, nil
	}
	return nil, nil
}

func (p *recordingSessionProvisioner) DeleteSession(_ context.Context, id string) error {
	p.deleted = id
	return nil
}

func (s *failingCreateProjectService) CreateProject(context.Context, *ProjectCreateRequest) (*Project, error) {
	return nil, errors.New("project persistence unavailable")
}

func TestCreateProjectWithDefaultFlowReturnsRunnableSevenStageFlow(t *testing.T) {
	projects := NewInMemoryProjectService()
	flows := floweng.NewInMemoryEngine(nil)

	created, err := CreateProjectWithDefaultFlow(context.Background(), projects, flows, &ProjectCreateRequest{
		Title: "  Checkout rewrite  ",
		Tags:  []string{" frontend ", "frontend", "payments"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Flow == nil || created.Flow.ProjectID != created.ID {
		t.Fatalf("project and flow are not bound: %+v", created)
	}
	if created.Title != "Checkout rewrite" {
		t.Fatalf("title=%q", created.Title)
	}
	if len(created.Tags) != 2 || created.Tags[0] != "frontend" || created.Tags[1] != "payments" {
		t.Fatalf("tags=%v", created.Tags)
	}
	if created.Flow.TemplateID != floweng.TemplateNewProject || created.Flow.Status != floweng.FlowStatusActive {
		t.Fatalf("unexpected flow: %+v", created.Flow)
	}
	if len(created.Flow.Stages) != 7 || created.Flow.Stages[0].Type != floweng.StageTypeIdea || created.Flow.Stages[0].Status != floweng.StageStatusActive {
		t.Fatalf("initial stages are not runnable: %+v", created.Flow.Stages)
	}
}

func TestCreateProjectWithDefaultFlowRemovesFlowWhenProjectPersistenceFails(t *testing.T) {
	projects := &failingCreateProjectService{InMemoryProjectService: NewInMemoryProjectService()}
	flows := floweng.NewInMemoryEngine(nil)

	_, err := CreateProjectWithDefaultFlow(context.Background(), projects, flows, &ProjectCreateRequest{Title: "will fail"})
	if err == nil {
		t.Fatal("expected project persistence failure")
	}
	var creationErr *ProjectCreationError
	if !errors.As(err, &creationErr) || creationErr.Phase != "project persistence" || creationErr.CleanupErr != nil {
		t.Fatalf("unexpected error: %#v", err)
	}

	listed, listErr := projects.ListProjects(context.Background(), &ProjectListRequest{})
	if listErr != nil {
		t.Fatal(listErr)
	}
	if listed.Total != 0 {
		t.Fatalf("partial project remained: %+v", listed.Projects)
	}
	remainingFlows, listErr := flows.List(context.Background(), "")
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(remainingFlows) != 0 {
		t.Fatalf("orphan flow remained: %+v", remainingFlows)
	}
}

func TestCreateProjectRemovesSessionWhenFlowCreationFails(t *testing.T) {
	projects := NewInMemoryProjectService()
	flows := floweng.NewEngineWithStore(failingFlowStoreForProject{}, nil)
	provisioner := &recordingSessionProvisioner{}
	SetSessionProvisioner(provisioner)
	t.Cleanup(func() { SetSessionProvisioner(nil) })
	_, err := CreateProjectWithDefaultFlow(context.Background(), projects, flows, &ProjectCreateRequest{Title: "flow fails"})
	if err == nil {
		t.Fatal("expected flow failure")
	}
	if provisioner.deleted != provisioner.created.ID {
		t.Fatalf("deleted session=%q want %q", provisioner.deleted, provisioner.created.ID)
	}
}

type failingFlowStoreForProject struct{}

func (failingFlowStoreForProject) Put(*floweng.Flow) error              { return errors.New("flow unavailable") }
func (failingFlowStoreForProject) Get(string) (*floweng.Flow, error)    { return nil, nil }
func (failingFlowStoreForProject) List(string) ([]*floweng.Flow, error) { return nil, nil }
func (failingFlowStoreForProject) Delete(string) error                  { return nil }

func TestCreateProjectWithDefaultFlowRejectsBlankTitleBeforeProvisioning(t *testing.T) {
	projects := NewInMemoryProjectService()
	flows := floweng.NewInMemoryEngine(nil)

	_, err := CreateProjectWithDefaultFlow(context.Background(), projects, flows, &ProjectCreateRequest{Title: " \t "})
	if !errors.Is(err, ErrInvalidProjectCreate) {
		t.Fatalf("error=%v want ErrInvalidProjectCreate", err)
	}
	remaining, listErr := flows.List(context.Background(), "")
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(remaining) != 0 {
		t.Fatalf("invalid request provisioned flows: %+v", remaining)
	}
}

func TestCreateProjectWithDefaultFlowBindsSessionAndRoot(t *testing.T) {
	root := t.TempDir()
	projects := NewInMemoryProjectService()
	projects.SetAllowedWorkspaceRoots([]string{root})
	flows := floweng.NewInMemoryEngine(nil)
	created, err := CreateProjectWithDefaultFlow(context.Background(), projects, flows, &ProjectCreateRequest{Title: "bound", WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := CanonicalizeWorkspaceRoot(root, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if created.DefaultFlowID != created.Flow.ID || created.DefaultSessionID != created.Session.ID {
		t.Fatalf("bindings not persisted: project=%+v flow=%+v session=%+v", created.Project, created.Flow, created.Session)
	}
	if created.BindingState != BindingStateBound || created.WorkspaceRoot != canonicalRoot {
		t.Fatalf("workspace binding=%+v", created.Project)
	}
	if created.Flow.SessionID != created.Session.ID {
		t.Fatalf("flow session=%q want %q", created.Flow.SessionID, created.Session.ID)
	}
}

func TestCanonicalizeWorkspaceRootRejectsOutsideAndSymlink(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	if _, err := CanonicalizeWorkspaceRoot(outside, []string{allowed}); err == nil {
		t.Fatal("outside root accepted")
	}
	if _, err := os.Stat(allowed); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalizeWorkspaceRootRejectsUnconfiguredBindings(t *testing.T) {
	if _, err := CanonicalizeWorkspaceRoot(t.TempDir(), nil); err == nil {
		t.Fatal("workspace root accepted without a server allow-list")
	}
}

func TestCreateProjectWithDefaultFlowIdempotency(t *testing.T) {
	resetIdempotentCreationCache()
	t.Cleanup(resetIdempotentCreationCache)
	projects := NewInMemoryProjectService()
	flows := floweng.NewInMemoryEngine(nil)
	one, err := CreateProjectWithDefaultFlowIdempotent(context.Background(), projects, flows, &ProjectCreateRequest{Title: "retry"}, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	two, err := CreateProjectWithDefaultFlowIdempotent(context.Background(), projects, flows, &ProjectCreateRequest{Title: "different"}, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if one.ID != two.ID || one.Flow.ID != two.Flow.ID {
		t.Fatalf("retry created a second resource: one=%s/%s two=%s/%s", one.ID, one.Flow.ID, two.ID, two.Flow.ID)
	}
	items, _ := flows.List(context.Background(), one.ID)
	if len(items) != 1 {
		t.Fatalf("flow count=%d", len(items))
	}
}

func TestArchiveProjectAndAbortFlowsIsRecoverable(t *testing.T) {
	projects := NewInMemoryProjectService()
	flows := floweng.NewInMemoryEngine(nil)
	created, err := CreateProjectWithDefaultFlow(context.Background(), projects, flows, &ProjectCreateRequest{Title: "recoverable"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ArchiveProjectAndAbortFlows(context.Background(), projects, flows, created.ID); err != nil {
		t.Fatal(err)
	}
	archived, err := projects.GetProject(context.Background(), created.ID)
	if err != nil || archived == nil || archived.Status != StatusArchived || archived.BindingState != BindingStateArchived {
		t.Fatalf("archived project=%+v err=%v", archived, err)
	}
	aborted, err := flows.Get(context.Background(), created.Flow.ID)
	if err != nil || aborted == nil || aborted.Status != floweng.FlowStatusAborted {
		t.Fatalf("aborted flow=%+v err=%v", aborted, err)
	}
	restored, err := RestoreProjectWithDefaultFlow(context.Background(), projects, flows, created.ID)
	if err != nil || restored.Status != StatusPlanning {
		t.Fatalf("restored project=%+v err=%v", restored, err)
	}
	if restored.Flow.ID == created.Flow.ID || restored.Flow.Status != floweng.FlowStatusActive {
		t.Fatalf("restored flow=%+v original=%+v", restored.Flow, created.Flow)
	}
	if restored.Session.ID != created.Session.ID || restored.DefaultFlowID != restored.Flow.ID {
		t.Fatalf("restored bindings=%+v", restored)
	}
}

func TestConcurrentCreateProjectIdempotency(t *testing.T) {
	resetIdempotentCreationCache()
	t.Cleanup(resetIdempotentCreationCache)
	projects := NewInMemoryProjectService()
	flows := floweng.NewInMemoryEngine(nil)
	const count = 8
	ids := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			created, err := CreateProjectWithDefaultFlowIdempotent(context.Background(), projects, flows, &ProjectCreateRequest{Title: "concurrent"}, "concurrent-key")
			if err != nil {
				errs <- err
				return
			}
			ids <- created.ID
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	close(ids)
	var want string
	for id := range ids {
		if want == "" {
			want = id
		}
		if id != want {
			t.Fatalf("idempotent requests returned %q and %q", want, id)
		}
	}
	listed, err := projects.ListProjects(context.Background(), &ProjectListRequest{})
	if err != nil || listed.Total != 1 {
		t.Fatalf("projects=%+v err=%v", listed, err)
	}
}
