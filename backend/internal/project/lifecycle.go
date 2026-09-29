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

	// Archiving ends every flow the project could still be running: the active
	// ones drive project work, and the suspended ones are parked flows that a
	// restore could resume. Leaving a suspended flow behind would mean an
	// archived project still holds a flow of record that one Resume away (after
	// the active slot frees) becomes runnable. Terminal flows are history and
	// stay as they are.
	items, err := flows.List(ctx, id)
	if err != nil {
		return err
	}
	for _, f := range items {
		if f == nil || (f.Status != floweng.FlowStatusActive && f.Status != floweng.FlowStatusSuspended) {
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

	// The project's flow of record decides the session, not the other way round
	// (T3.01.a group 2): when the restore reuses a flow that already carries a
	// session, the runtime bindings must point at *that* session, or the
	// restored project would name a flow and a session that do not belong
	// together. The record is adopted before ensureProjectSession so the
	// provisioner is asked for the session the flow runs on. A flow with no
	// session leaves today's logic in place.
	if existing, err := findActiveProjectFlow(ctx, flows, p.ID, record.FlowID); err != nil {
		return nil, err
	} else if existing != nil && existing.SessionID != "" {
		record.FlowID = existing.ID
		record.SessionID = existing.SessionID
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
	// A reused flow keeps its own session: updateRuntimeBindings must name the
	// session the flow runs on, not the one record happened to carry.
	if flow.SessionID != "" {
		record.SessionID = flow.SessionID
	} else {
		record.SessionID = session.ID
	}
	record.State = lifecycleOperationRuntimeReady
	if durable {
		if err := journal.SaveLifecycleOperation(ctx, record); err != nil {
			return nil, err
		}
	}

	p, err = projects.UpdateRuntimeBindings(ctx, p.ID, flow.ID, record.SessionID)
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

// ensureRunnableProjectFlow returns the project's active project flow, creating
// one only when the project has none.
//
// T3.01.a makes "one active project flow per project" a rule of the engine, and
// this function is the restore path that has to live with it. Before this step
// the search matched on the session, so a project whose stored flow carried a
// different session than the one the record named was answered with a second
// flow — which the engine now refuses, and which made
// RecoverIncompleteProjectLifecycles retry the same failing restore on every
// start. The flow of record is a property of the project, not of a session, so
// the search is by kind and status: the first (and, under the rule, only) active
// project flow is reused whatever session it carries, and the session follows
// the flow rather than the other way round.
//
// The store lists a project's flows by updated_at DESC, so the flow picked here
// is the first active project flow the project page shows. preferredFlowID is
// still consulted first, but only as a preference among active project flows of
// this project: a stored id that is terminal, or of another project, is stale
// and does not decide anything.
func ensureRunnableProjectFlow(ctx context.Context, flows floweng.Engine, projectID, preferredFlowID, sessionID string) (*floweng.Flow, error) {
	flow, err := findActiveProjectFlow(ctx, flows, projectID, preferredFlowID)
	if err != nil {
		return nil, err
	}
	if flow != nil {
		return flow, nil
	}
	return flows.Create(ctx, &floweng.CreateFlowRequest{
		ProjectID:  projectID,
		TemplateID: floweng.TemplateNewProject,
		SessionID:  sessionID,
		Kind:       floweng.FlowKindProject,
	})
}

// findActiveProjectFlow returns the active project flow ensureRunnableProjectFlow
// would reuse, or nil when the project has none. preferredFlowID is consulted
// first, but only as a preference: an id that names nothing, a terminal flow or
// another project's flow does not decide anything. A missing preferred flow is
// not an error (the id is a stale record, not a broken invariant), while a store
// that cannot answer is.
func findActiveProjectFlow(ctx context.Context, flows floweng.Engine, projectID, preferredFlowID string) (*floweng.Flow, error) {
	if preferredFlowID != "" {
		flow, err := flows.Get(ctx, preferredFlowID)
		if err == nil {
			if isActiveProjectFlow(flow, projectID) {
				return flow, nil
			}
		} else if !isNotFoundError(err) {
			// A stored preferred id whose flow was deleted is stale, not fatal:
			// the list below is the answer. Anything else is a real failure.
			return nil, err
		}
	}
	items, err := flows.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, flow := range items {
		if isActiveProjectFlow(flow, projectID) {
			return flow, nil
		}
	}
	return nil, nil
}

// isActiveProjectFlow reports whether flow is the active project flow of
// projectID: a flow document that exists, belongs to the project, is active and
// is the project's flow of record (kind=project). A flow stored before T3.01
// decodes as FlowKindProject, so an older database's active flow qualifies.
func isActiveProjectFlow(flow *floweng.Flow, projectID string) bool {
	return flow != nil &&
		flow.ProjectID == projectID &&
		flow.Status == floweng.FlowStatusActive &&
		flow.Kind == floweng.FlowKindProject
}

// isNotFoundError reports whether err is the store's "flow not found". The
// engine has no not-found sentinel yet (the handlers classify the same string),
// so the check is local to this file and to the one place that has to tell a
// stale stored id from a broken store.
func isNotFoundError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not found")
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
