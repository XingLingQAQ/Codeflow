package workflow

// Project scope resolution for the workflow views (E-01 / T13.01).
//
// ResolveProjectScope computes the verified set of plan/task/session/agent IDs
// that authoritatively belong to one project. It is the single authorization
// source later consumed by loadProjectSnapshot/GetOverview/GetTimeline/GetReplay
// (T13.01.b): nothing outside these sets may be read through a project view.
//
// Authoritative inclusion rules (actual association model, see T13.01.a receipt):
//   - Sessions: project.DefaultSessionID (server-assigned at creation) plus the
//     plan/task session bindings. planner.Plan and planner.Task have no dedicated
//     session_id column; the only existing association channel is their Metadata
//     ("session_id"/"sessionId"/"workflow_session_id"/"workflowSessionId" — the
//     same keys planner.taskSessionID and the current workflow view treat as the
//     binding). Project-level Metadata session keys are deliberately NOT included:
//     the contract whitelists DefaultSessionID and plan/task bindings only.
//   - Agents: an agent belongs to the project iff its Agent.SessionID is in the
//     authorized session set (agent has no task/project field). An empty session
//     set yields an empty agent set — the previous "no sessions -> all agents"
//     fallback is explicitly forbidden, and agents never add sessions back.
//   - Audit content, the URL session_id parameter, and the global agent list can
//     never widen the sets. The optional audit dependency is accepted for wiring
//     symmetry with the workflow Service and is never consulted for membership,
//     so a nil audit service changes nothing (neither widens nor shrinks).
//
// The three outcomes are distinguishable: project not found (ErrProjectNotFound),
// explicit session not in project (ErrSessionNotInProject carrying the IDs), and
// a valid project with no authorized objects (scope.Empty == true, nil error).

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/codeflow/backend/internal/agent"
	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
)

// ErrSessionNotInProject matches (via errors.Is) any *SessionNotInProjectError:
// the caller supplied an explicit session that is not authorized for the project.
var ErrSessionNotInProject = errors.New("workflow session not in project scope")

// SessionNotInProjectError reports an explicit session (e.g. a replay URL
// session_id) that does not belong to the requested project's authorized set.
type SessionNotInProjectError struct {
	ProjectID string
	SessionID string
}

func (e *SessionNotInProjectError) Error() string {
	return fmt.Sprintf("workflow session %q is not authorized for project %q", e.SessionID, e.ProjectID)
}

// Is makes errors.Is(err, ErrSessionNotInProject) true for any instance.
func (e *SessionNotInProjectError) Is(target error) bool {
	return target == ErrSessionNotInProject
}

// ProjectScope is the verified authorization set of one project. All slices are
// sorted, deduplicated, and non-nil (possibly empty).
type ProjectScope struct {
	ProjectID  string
	PlanIDs    []string
	TaskIDs    []string
	SessionIDs []string
	AgentIDs   []string
	// Empty is true when the project exists but has no authorized plans, tasks,
	// sessions, or agents — a valid outcome, not an error.
	Empty bool
}

// HasSession reports whether sessionID is in the project's authorized set.
func (s *ProjectScope) HasSession(sessionID string) bool {
	if s == nil {
		return false
	}
	for _, id := range s.SessionIDs {
		if id == sessionID {
			return true
		}
	}
	return false
}

// ScopeOptions carries caller-supplied narrowing input. SessionID is validated
// against the authorized set; it is never added to it.
type ScopeOptions struct {
	SessionID string
}

// ScopeAuditQuerier is the subset of *audit.AuditService the workflow Service
// already wires. *audit.AuditService satisfies it.
type ScopeAuditQuerier interface {
	Query(ctx context.Context, query *audit.AuditQuery) (*audit.AuditQueryResult, error)
}

// ScopeDependencies mirrors the injectable shape of the workflow Service
// constructor so consumers can pass the same collaborators without adaptation.
// Projects and Planner are required; Agents and Audit are optional (nil yields
// an empty agent set / is never consulted, respectively).
type ScopeDependencies struct {
	Projects project.IProjectService
	Planner  planner.IPlanner
	Agents   agent.IAgentService
	Audit    ScopeAuditQuerier
}

// ResolveProjectScope verifies projectID and returns its authorized scope.
// When opts.SessionID is non-empty it must already belong to the project,
// otherwise a *SessionNotInProjectError is returned and no scope is produced.
func ResolveProjectScope(ctx context.Context, deps ScopeDependencies, projectID string, opts ScopeOptions) (*ProjectScope, error) {
	if deps.Projects == nil {
		return nil, fmt.Errorf("workflow project service unavailable")
	}
	if deps.Planner == nil {
		return nil, fmt.Errorf("workflow planner service unavailable")
	}

	proj, err := deps.Projects.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if proj == nil {
		return nil, ErrProjectNotFound
	}

	plans, err := deps.Projects.GetProjectPlans(ctx, projectID)
	if err != nil {
		return nil, err
	}

	tasks := make([]planner.Task, 0)
	for _, plan := range plans {
		result, err := deps.Planner.ListTasks(ctx, plan.ID, &planner.TaskListRequest{})
		if err != nil {
			return nil, err
		}
		if result != nil {
			tasks = append(tasks, result.Tasks...)
		}
	}

	sessionIDs := collectScopeSessionIDs(proj, plans, tasks)
	agentIDs, err := collectScopeAgentIDs(ctx, deps.Agents, sessionIDs)
	if err != nil {
		return nil, err
	}

	scope := &ProjectScope{
		ProjectID:  proj.ID,
		PlanIDs:    sortedUniqueIDs(planIDs(plans)),
		TaskIDs:    sortedUniqueIDs(taskIDs(tasks)),
		SessionIDs: sessionIDs,
		AgentIDs:   agentIDs,
	}
	scope.Empty = len(scope.PlanIDs) == 0 && len(scope.TaskIDs) == 0 &&
		len(scope.SessionIDs) == 0 && len(scope.AgentIDs) == 0

	if requested := strings.TrimSpace(opts.SessionID); requested != "" && !scope.HasSession(requested) {
		return nil, &SessionNotInProjectError{ProjectID: proj.ID, SessionID: requested}
	}
	return scope, nil
}

// collectScopeSessionIDs unions the only authoritative session bindings:
// project.DefaultSessionID plus plan/task Metadata session keys. Audit entries,
// caller-supplied IDs, and agent sessions are not consulted.
func collectScopeSessionIDs(proj *project.Project, plans []planner.Plan, tasks []planner.Task) []string {
	sessions := appendUnique(make([]string, 0), proj.DefaultSessionID)
	for _, plan := range plans {
		sessions = appendMetadataSessionIDs(sessions, plan.Metadata)
	}
	for _, task := range tasks {
		sessions = appendMetadataSessionIDs(sessions, task.Metadata)
	}
	sort.Strings(sessions)
	return sessions
}

// collectScopeAgentIDs keeps only agents whose session is authorized for the
// project. An empty session set returns an empty set — never the global list.
func collectScopeAgentIDs(ctx context.Context, agents agent.IAgentService, sessionIDs []string) ([]string, error) {
	ids := make([]string, 0)
	if agents == nil || len(sessionIDs) == 0 {
		return ids, nil
	}
	result, err := agents.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	if result == nil {
		// 没有列表就没有可授权的 Agent：返回空集合，绝不回退到全局列表。
		return ids, nil
	}
	sessionSet := make(map[string]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		sessionSet[sessionID] = struct{}{}
	}
	for _, ag := range result.Agents {
		if _, ok := sessionSet[ag.SessionID]; ok {
			ids = appendUnique(ids, ag.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func planIDs(plans []planner.Plan) []string {
	ids := make([]string, 0, len(plans))
	for _, plan := range plans {
		ids = append(ids, plan.ID)
	}
	return ids
}

func taskIDs(tasks []planner.Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func sortedUniqueIDs(ids []string) []string {
	out := appendUnique(make([]string, 0, len(ids)), ids...)
	sort.Strings(out)
	return out
}
