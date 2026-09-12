package project

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/storage"
)

const (
	projectOperationArchive = "archive_project"
	projectOperationRestore = "restore_project"

	lifecycleOperationPending         = "pending"
	lifecycleOperationProjectArchived = "project_archived"
	lifecycleOperationFlowsAborted    = "flows_aborted"
	lifecycleOperationRuntimeStopped  = "runtime_stopped"
	lifecycleOperationRuntimeReady    = "runtime_ready"
	lifecycleOperationCompleted       = "completed"
)

// LifecycleOperationRecord is the durable progress marker for recoverable
// archive and restore workflows.
type LifecycleOperationRecord struct {
	Operation     string
	ProjectID     string
	FlowID        string
	SessionID     string
	WorkspaceRoot string
	State         string
}

type LifecycleOperationStore interface {
	GetLifecycleOperation(ctx context.Context, operation, projectID string) (*LifecycleOperationRecord, error)
	ListIncompleteLifecycleOperations(ctx context.Context) ([]*LifecycleOperationRecord, error)
	SaveLifecycleOperation(ctx context.Context, record *LifecycleOperationRecord) error
}

// ProjectRuntimeCleanup stops process-local resources owned by a workspace.
// It is intentionally not invoked during startup recovery because watchers and
// dev-servers do not survive a process restart.
type ProjectRuntimeCleanup func(ctx context.Context, project *Project) error

var projectLifecycleMu sync.Mutex

// ArchiveProjectAndAbortFlows records a recoverable archive request. The
// project row, terminal Flows, and default Session remain retained so a restore
// can rebuild a runnable default Flow before the retention janitor purges data.
func ArchiveProjectAndAbortFlows(ctx context.Context, projects IProjectService, flows floweng.Engine, id string) error {
	return ArchiveProjectAndAbortFlowsWithCleanup(ctx, projects, flows, id, nil)
}

func ArchiveProjectAndAbortFlowsWithCleanup(ctx context.Context, projects IProjectService, flows floweng.Engine, id string, cleanup ProjectRuntimeCleanup) error {
	projectLifecycleMu.Lock()
	defer projectLifecycleMu.Unlock()
	return archiveProject(ctx, projects, flows, id, cleanup)
}

func archiveProject(ctx context.Context, projects IProjectService, flows floweng.Engine, id string, cleanup ProjectRuntimeCleanup) error {
	if projects == nil {
		return fmt.Errorf("project service is required")
	}
	if flows == nil {
		return fmt.Errorf("flow service is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("project id is required")
	}
	p, err := projects.GetProject(ctx, id)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("project not found")
	}

	record := &LifecycleOperationRecord{
		Operation:     projectOperationArchive,
		ProjectID:     id,
		FlowID:        p.DefaultFlowID,
		SessionID:     p.DefaultSessionID,
		WorkspaceRoot: p.WorkspaceRoot,
		State:         lifecycleOperationPending,
	}
	journal, durable := projects.(LifecycleOperationStore)
	if durable {
		if existing, err := journal.GetLifecycleOperation(ctx, projectOperationArchive, id); err != nil {
			return err
		} else if existing != nil && existing.State != lifecycleOperationCompleted {
			record = existing
		} else if err := journal.SaveLifecycleOperation(ctx, record); err != nil {
			return err
		}
	}

	if marker, ok := projects.(projectDeletionMarker); ok && p.BindingState != BindingStateDeleting && p.Status != StatusArchived {
		if _, err := marker.MarkProjectDeleting(ctx, id); err != nil {
			return err
		}
	}
	if p.Status != StatusArchived || p.BindingState != BindingStateArchived {
		if err := projects.DeleteProject(ctx, id); err != nil {
			return err
		}
	}
	record.State = lifecycleOperationProjectArchived
	if durable {
		if err := journal.SaveLifecycleOperation(ctx, record); err != nil {
			return err
		}
	}

	items, err := flows.List(ctx, id)
	if err != nil {
		return err
	}
	for _, f := range items {
		if f == nil || f.Status != floweng.FlowStatusActive {
			continue
		}
		if _, err := flows.Abort(ctx, f.ID, "project archived"); err != nil {
			return err
		}
	}
	record.State = lifecycleOperationFlowsAborted
	if durable {
		if err := journal.SaveLifecycleOperation(ctx, record); err != nil {
			return err
		}
	}

	if cleanup != nil {
		if err := cleanup(ctx, p); err != nil {
			return err
		}
	}
	record.State = lifecycleOperationRuntimeStopped
	if durable {
		if err := journal.SaveLifecycleOperation(ctx, record); err != nil {
			return err
		}
	}
	record.State = lifecycleOperationCompleted
	if durable {
		return journal.SaveLifecycleOperation(ctx, record)
	}
	return nil
}

// RestoreProjectWithDefaultFlow makes an archived project runnable again. A
// terminal default Flow is retained for history and replaced with one active
// Flow bound to the retained (or deterministically recreated) Session.
func RestoreProjectWithDefaultFlow(ctx context.Context, projects IProjectService, flows floweng.Engine, id string) (*ProjectCreation, error) {
	projectLifecycleMu.Lock()
	defer projectLifecycleMu.Unlock()
	return restoreProject(ctx, projects, flows, id)
}

func restoreProject(ctx context.Context, projects IProjectService, flows floweng.Engine, id string) (*ProjectCreation, error) {
	if projects == nil {
		return nil, fmt.Errorf("project service is required")
	}
	if flows == nil {
		return nil, fmt.Errorf("flow service is required")
	}
	p, err := projects.GetProject(ctx, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("project not found")
	}
	record := &LifecycleOperationRecord{
		Operation:     projectOperationRestore,
		ProjectID:     p.ID,
		FlowID:        p.DefaultFlowID,
		SessionID:     p.DefaultSessionID,
		WorkspaceRoot: p.WorkspaceRoot,
		State:         lifecycleOperationPending,
	}
	journal, durable := projects.(LifecycleOperationStore)
	if durable {
		if existing, err := journal.GetLifecycleOperation(ctx, projectOperationRestore, id); err != nil {
			return nil, err
		} else if existing != nil && existing.State != lifecycleOperationCompleted {
			record = existing
		} else if p.Status == StatusArchived {
			if err := journal.SaveLifecycleOperation(ctx, record); err != nil {
				return nil, err
			}
		}
	}

	session, err := ensureProjectSession(ctx, p, record)
	if err != nil {
		return nil, err
	}
	flow, err := ensureRunnableProjectFlow(ctx, flows, p.ID, record.FlowID, session.ID)
	if err != nil {
		return nil, err
	}
	record.FlowID = flow.ID
	record.SessionID = session.ID
	record.State = lifecycleOperationRuntimeReady
	if durable {
		if err := journal.SaveLifecycleOperation(ctx, record); err != nil {
			return nil, err
		}
	}

	p, err = projects.UpdateRuntimeBindings(ctx, p.ID, flow.ID, session.ID)
	if err != nil {
		return nil, err
	}
	p, err = projects.RestoreProject(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	record.State = lifecycleOperationCompleted
	if durable {
		if err := journal.SaveLifecycleOperation(ctx, record); err != nil {
			return nil, err
		}
	}
	return &ProjectCreation{Project: p, Flow: flow, Session: session}, nil
}

func ensureProjectSession(ctx context.Context, p *Project, record *LifecycleOperationRecord) (*storage.Session, error) {
	sessionID := record.SessionID
	if sessionID == "" {
		sessionID = p.DefaultSessionID
	}
	if provisioner := getSessionProvisioner(); provisioner != nil {
		if sessionID != "" {
			session, err := provisioner.GetSession(ctx, sessionID)
			if err != nil {
				return nil, err
			}
			if session != nil {
				return session, nil
			}
		}
		return provisioner.CreateDefaultSession(ctx, p.ID, p.Title+" session")
	}
	if sessionID == "" {
		sessionID = "project-" + p.ID
	}
	now := time.Now().UnixMilli()
	return &storage.Session{ID: sessionID, Title: p.Title + " session", CreatedAt: now, UpdatedAt: now}, nil
}

func ensureRunnableProjectFlow(ctx context.Context, flows floweng.Engine, projectID, preferredFlowID, sessionID string) (*floweng.Flow, error) {
	if preferredFlowID != "" {
		flow, err := flows.Get(ctx, preferredFlowID)
		if err != nil {
			return nil, err
		}
		if flow != nil && flow.Status == floweng.FlowStatusActive && flow.ProjectID == projectID && flow.SessionID == sessionID {
			return flow, nil
		}
	}
	items, err := flows.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, flow := range items {
		if flow != nil && flow.Status == floweng.FlowStatusActive && flow.SessionID == sessionID {
			return flow, nil
		}
	}
	return flows.Create(ctx, &floweng.CreateFlowRequest{ProjectID: projectID, TemplateID: floweng.TemplateNewProject, SessionID: sessionID})
}

// RecoverIncompleteProjectLifecycles finishes archive and restore operations
// before the server accepts requests.
func RecoverIncompleteProjectLifecycles(ctx context.Context, projects IProjectService, flows floweng.Engine) error {
	journal, ok := projects.(LifecycleOperationStore)
	if !ok {
		return nil
	}
	records, err := journal.ListIncompleteLifecycleOperations(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record == nil || strings.TrimSpace(record.ProjectID) == "" {
			return fmt.Errorf("invalid incomplete project lifecycle operation")
		}
		switch record.Operation {
		case projectOperationArchive:
			if err := archiveProject(ctx, projects, flows, record.ProjectID, nil); err != nil {
				return fmt.Errorf("recover project archive %s: %w", record.ProjectID, err)
			}
		case projectOperationRestore:
			if _, err := restoreProject(ctx, projects, flows, record.ProjectID); err != nil {
				return fmt.Errorf("recover project restore %s: %w", record.ProjectID, err)
			}
		default:
			return fmt.Errorf("unknown project lifecycle operation %q", record.Operation)
		}
	}
	return nil
}
