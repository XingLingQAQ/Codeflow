package workflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/codeflow/backend/internal/agent"
	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
)

var ErrProjectNotFound = errors.New("workflow project not found")

type WorkflowSummary struct {
	ProjectCount    int `json:"project_count"`
	PlanCount       int `json:"plan_count"`
	TaskCount       int `json:"task_count"`
	CompletedTasks  int `json:"completed_tasks"`
	InProgressTasks int `json:"in_progress_tasks"`
	BlockedTasks    int `json:"blocked_tasks"`
	PendingTasks    int `json:"pending_tasks"`
	AgentCount      int `json:"agent_count"`
	SessionCount    int `json:"session_count"`
	AuditCount      int `json:"audit_count"`
	Progress        int `json:"progress"`
	// Floweng runtime (G14 enrichment)
	FlowCount          int `json:"flow_count"`
	ActiveFlowCount    int `json:"active_flow_count"`
	CompletedFlowCount int `json:"completed_flow_count"`
	AbortedFlowCount   int `json:"aborted_flow_count"`
}

type WorkflowOverview struct {
	Project     *project.Project     `json:"project"`
	Plans       []planner.Plan       `json:"plans"`
	Tasks       []planner.Task       `json:"tasks"`
	Agents      []agent.Agent        `json:"agents"`
	SessionIDs  []string             `json:"session_ids"`
	LatestAudit *audit.AuditLogEntry `json:"latest_audit,omitempty"`
	Summary     WorkflowSummary      `json:"summary"`
}

type WorkflowTimeline struct {
	ProjectID  string                  `json:"project_id"`
	SessionIDs []string                `json:"session_ids"`
	Events     []WorkflowTimelineEvent `json:"events"`
	Summary    WorkflowSummary         `json:"summary"`
}

type WorkflowReplay struct {
	ProjectID    string                `json:"project_id"`
	SessionID    string                `json:"session_id,omitempty"`
	Events       []WorkflowReplayEvent `json:"events"`
	Trace        *agent.CallTrace      `json:"trace,omitempty"`
	Agents       []agent.Agent         `json:"agents,omitempty"`
	AuditEntries []audit.AuditLogEntry `json:"audit_entries,omitempty"`
	Summary      WorkflowReplaySummary `json:"summary"`
}

type WorkflowReplaySummary struct {
	EventCount int `json:"event_count"`
	TraceCount int `json:"trace_count"`
	AuditCount int `json:"audit_count"`
}

type WorkflowTimelineEvent struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Lane      string `json:"lane"`
	Title     string `json:"title"`
	Detail    string `json:"detail,omitempty"`
	Status    string `json:"status,omitempty"`
	Source    string `json:"source,omitempty"`
	Timestamp int64  `json:"timestamp"`

	ProjectID string `json:"project_id,omitempty"`
	PlanID    string `json:"plan_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	AuditID   string `json:"audit_id,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
}

type WorkflowReplayEvent struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Lane      string `json:"lane"`
	Speaker   string `json:"speaker"`
	Message   string `json:"message"`
	Evidence  string `json:"evidence,omitempty"`
	Status    string `json:"status,omitempty"`
	Timestamp int64  `json:"timestamp"`

	ProjectID string `json:"project_id,omitempty"`
	PlanID    string `json:"plan_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	AuditID   string `json:"audit_id,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
}

type Service struct {
	projects project.IProjectService
	planner  planner.IPlanner
	agents   agent.IAgentService
	audit    *audit.AuditService
}

func NewService(projects project.IProjectService, plannerSvc planner.IPlanner, agentSvc agent.IAgentService, auditSvc *audit.AuditService) *Service {
	return &Service{
		projects: projects,
		planner:  plannerSvc,
		agents:   agentSvc,
		audit:    auditSvc,
	}
}

func (s *Service) GetOverview(ctx context.Context, projectID string) (*WorkflowOverview, error) {
	snapshot, err := s.loadProjectSnapshot(ctx, projectID)
	if err != nil {
		return nil, err
	}

	summary := buildWorkflowSummary(snapshot)
	enrichSummaryWithFlows(ctx, projectID, &summary)

	return &WorkflowOverview{
		Project:     snapshot.project,
		Plans:       snapshot.plans,
		Tasks:       snapshot.tasks,
		Agents:      snapshot.agents,
		SessionIDs:  snapshot.sessionIDs,
		LatestAudit: snapshot.latestAudit,
		Summary:     summary,
	}, nil
}

func enrichSummaryWithFlows(ctx context.Context, projectID string, summary *WorkflowSummary) {
	if summary == nil {
		return
	}
	eng := floweng.GetEngine()
	if eng == nil {
		return
	}
	flows, err := eng.List(ctx, projectID)
	if err != nil {
		return
	}
	summary.FlowCount = len(flows)
	for _, f := range flows {
		if f == nil {
			continue
		}
		switch f.Status {
		case floweng.FlowStatusActive:
			summary.ActiveFlowCount++
		case floweng.FlowStatusCompleted:
			summary.CompletedFlowCount++
		case floweng.FlowStatusAborted:
			summary.AbortedFlowCount++
		}
	}
}

func (s *Service) GetTimeline(ctx context.Context, projectID string) (*WorkflowTimeline, error) {
	snapshot, err := s.loadProjectSnapshot(ctx, projectID)
	if err != nil {
		return nil, err
	}

	events := buildTimelineEvents(snapshot)
	// G14: merge floweng runtime events so observation is not planner/audit-only.
	events = append(events, collectFlowengTimelineEvents(ctx, projectID)...)
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp < events[j].Timestamp
	})
	summary := buildWorkflowSummary(snapshot)
	enrichSummaryWithFlows(ctx, projectID, &summary)
	return &WorkflowTimeline{
		ProjectID:  projectID,
		SessionIDs: snapshot.sessionIDs,
		Events:     events,
		Summary:    summary,
	}, nil
}

// collectFlowengTimelineEvents adapts floweng FlowEvent → WorkflowTimelineEvent (query adapter).
func collectFlowengTimelineEvents(ctx context.Context, projectID string) []WorkflowTimelineEvent {
	eng := floweng.GetEngine()
	if eng == nil {
		return nil
	}
	flows, err := eng.List(ctx, projectID)
	if err != nil || len(flows) == 0 {
		return nil
	}
	out := make([]WorkflowTimelineEvent, 0)
	for _, flow := range flows {
		if flow == nil {
			continue
		}
		for _, ev := range flow.Events {
			out = append(out, WorkflowTimelineEvent{
				ID:        ev.ID,
				Type:      ev.Type,
				Lane:      "floweng",
				Title:     ev.Type,
				Detail:    ev.Message,
				Status:    string(flow.Status),
				Source:    "floweng",
				Timestamp: ev.Timestamp.UnixMilli(),
				ProjectID: projectID,
			})
		}
	}
	return out
}

func (s *Service) GetReplay(ctx context.Context, projectID, requestedSessionID string) (*WorkflowReplay, error) {
	snapshot, err := s.loadProjectSnapshot(ctx, projectID)
	if err != nil {
		return nil, err
	}

	sessionID := requestedSessionID
	if sessionID == "" && len(snapshot.sessionIDs) > 0 {
		sessionID = snapshot.sessionIDs[0]
	}

	var traceResp *agent.ConversationTraceResponse
	if s.agents != nil && sessionID != "" {
		traceResp, err = s.agents.GetConversationTrace(ctx, sessionID)
		if err != nil {
			return nil, err
		}
	}

	auditEntries, err := s.loadReplayAuditEntries(ctx, snapshot, sessionID)
	if err != nil {
		return nil, err
	}

	replayEvents, traceCount := buildReplayEvents(traceResp, auditEntries)
	return &WorkflowReplay{
		ProjectID:    projectID,
		SessionID:    sessionID,
		Events:       replayEvents,
		Trace:        nilIfTraceResponseNil(traceResp),
		Agents:       replayAgents(traceResp, snapshot.agents),
		AuditEntries: auditEntries,
		Summary: WorkflowReplaySummary{
			EventCount: len(replayEvents),
			TraceCount: traceCount,
			AuditCount: len(auditEntries),
		},
	}, nil
}

type projectSnapshot struct {
	project      *project.Project
	plans        []planner.Plan
	tasks        []planner.Task
	agents       []agent.Agent
	sessionIDs   []string
	auditEntries []audit.AuditLogEntry
	latestAudit  *audit.AuditLogEntry
}

func (s *Service) loadProjectSnapshot(ctx context.Context, projectID string) (*projectSnapshot, error) {
	if s.projects == nil {
		return nil, fmt.Errorf("workflow project service unavailable")
	}
	if s.planner == nil {
		return nil, fmt.Errorf("workflow planner service unavailable")
	}

	proj, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if proj == nil {
		return nil, ErrProjectNotFound
	}

	plans, err := s.projects.GetProjectPlans(ctx, projectID)
	if err != nil {
		return nil, err
	}

	tasks := make([]planner.Task, 0)
	for _, plan := range plans {
		result, err := s.planner.ListTasks(ctx, plan.ID, &planner.TaskListRequest{})
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, result.Tasks...)
	}

	auditEntries, latestAudit, err := s.loadRelevantAuditEntries(ctx, proj, plans, tasks)
	if err != nil {
		return nil, err
	}

	sessionIDs := collectSessionIDs(proj, plans, tasks, auditEntries)
	agents, err := s.loadRelevantAgents(ctx, sessionIDs)
	if err != nil {
		return nil, err
	}
	for _, ag := range agents {
		if ag.SessionID != "" {
			sessionIDs = appendUnique(sessionIDs, ag.SessionID)
		}
	}

	return &projectSnapshot{
		project:      proj,
		plans:        plans,
		tasks:        tasks,
		agents:       agents,
		sessionIDs:   sessionIDs,
		auditEntries: auditEntries,
		latestAudit:  latestAudit,
	}, nil
}

func (s *Service) loadRelevantAuditEntries(ctx context.Context, proj *project.Project, plans []planner.Plan, tasks []planner.Task) ([]audit.AuditLogEntry, *audit.AuditLogEntry, error) {
	if s.audit == nil {
		return nil, nil, nil
	}

	result, err := s.audit.Query(ctx, &audit.AuditQuery{Limit: 10000})
	if err != nil {
		return nil, nil, err
	}

	idSet := map[string]struct{}{proj.ID: {}}
	for _, plan := range plans {
		idSet[plan.ID] = struct{}{}
	}
	for _, task := range tasks {
		idSet[task.ID] = struct{}{}
	}

	entries := make([]audit.AuditLogEntry, 0)
	var latest *audit.AuditLogEntry
	for _, entry := range result.Entries {
		if !isRelevantAuditEntry(entry, idSet) {
			continue
		}
		entries = append(entries, entry)
		entryCopy := entry
		if latest == nil || normalizeTimestamp(entry.Timestamp) > normalizeTimestamp(latest.Timestamp) {
			latest = &entryCopy
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return normalizeTimestamp(entries[i].Timestamp) < normalizeTimestamp(entries[j].Timestamp)
	})
	return entries, latest, nil
}

func (s *Service) loadRelevantAgents(ctx context.Context, sessionIDs []string) ([]agent.Agent, error) {
	if s.agents == nil {
		return nil, nil
	}
	result, err := s.agents.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	if len(sessionIDs) == 0 {
		return result.Agents, nil
	}

	sessionSet := make(map[string]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		sessionSet[sessionID] = struct{}{}
	}

	filtered := make([]agent.Agent, 0)
	for _, ag := range result.Agents {
		if _, ok := sessionSet[ag.SessionID]; ok {
			filtered = append(filtered, ag)
		}
	}
	return filtered, nil
}

func (s *Service) loadReplayAuditEntries(ctx context.Context, snapshot *projectSnapshot, sessionID string) ([]audit.AuditLogEntry, error) {
	if s.audit == nil {
		return nil, nil
	}
	if sessionID == "" {
		return snapshot.auditEntries, nil
	}

	result, err := s.audit.Query(ctx, &audit.AuditQuery{Limit: 10000})
	if err != nil {
		return nil, err
	}

	entries := make([]audit.AuditLogEntry, 0)
	for _, entry := range result.Entries {
		if auditEntrySessionID(entry) != sessionID {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func buildWorkflowSummary(snapshot *projectSnapshot) WorkflowSummary {
	summary := WorkflowSummary{
		ProjectCount: 1,
		PlanCount:    len(snapshot.plans),
		TaskCount:    len(snapshot.tasks),
		AgentCount:   len(snapshot.agents),
		SessionCount: len(snapshot.sessionIDs),
		AuditCount:   len(snapshot.auditEntries),
		Progress:     snapshot.project.Progress,
	}
	for _, task := range snapshot.tasks {
		switch task.Status {
		case planner.TaskStatusCompleted:
			summary.CompletedTasks++
		case planner.TaskStatusInProgress:
			summary.InProgressTasks++
		case planner.TaskStatusBlocked:
			summary.BlockedTasks++
		default:
			summary.PendingTasks++
		}
	}
	return summary
}

func buildTimelineEvents(snapshot *projectSnapshot) []WorkflowTimelineEvent {
	events := make([]WorkflowTimelineEvent, 0, 1+len(snapshot.plans)+len(snapshot.tasks)+len(snapshot.agents)+len(snapshot.auditEntries))
	events = append(events, WorkflowTimelineEvent{
		ID:        "project-" + snapshot.project.ID,
		Type:      "project",
		Lane:      "PROJECT",
		Title:     snapshot.project.Title,
		Detail:    strings.TrimSpace(snapshot.project.Description),
		Status:    string(snapshot.project.Status),
		Source:    "project",
		Timestamp: normalizeTimestamp(snapshot.project.UpdatedAt),
		ProjectID: snapshot.project.ID,
	})

	for _, plan := range snapshot.plans {
		events = append(events, WorkflowTimelineEvent{
			ID:        "plan-" + plan.ID,
			Type:      "plan",
			Lane:      "PLAN",
			Title:     plan.Title,
			Detail:    strings.TrimSpace(plan.Description),
			Status:    plan.Status,
			Source:    "plan",
			Timestamp: normalizeTimestamp(plan.UpdatedAt),
			ProjectID: snapshot.project.ID,
			PlanID:    plan.ID,
		})
	}

	for _, task := range snapshot.tasks {
		events = append(events, WorkflowTimelineEvent{
			ID:        "task-" + task.ID,
			Type:      "task",
			Lane:      "TASK",
			Title:     task.Title,
			Detail:    strings.TrimSpace(task.Description),
			Status:    string(task.Status),
			Source:    "task",
			Timestamp: normalizeTimestamp(task.UpdatedAt),
			ProjectID: snapshot.project.ID,
			PlanID:    task.PlanID,
			TaskID:    task.ID,
		})
	}

	for _, ag := range snapshot.agents {
		events = append(events, WorkflowTimelineEvent{
			ID:        "agent-" + ag.ID,
			Type:      "agent",
			Lane:      "AGENT",
			Title:     ag.Name,
			Detail:    fmt.Sprintf("%s · %s", ag.Role, ag.Model),
			Status:    string(ag.Status),
			Source:    "agent",
			Timestamp: normalizeTimestamp(ag.LastActiveAt),
			ProjectID: snapshot.project.ID,
			SessionID: ag.SessionID,
			AgentID:   ag.ID,
		})
	}

	for _, entry := range snapshot.auditEntries {
		events = append(events, WorkflowTimelineEvent{
			ID:        "audit-" + entry.ID,
			Type:      "audit",
			Lane:      "AUDIT",
			Title:     entry.Action,
			Detail:    describeAuditEntry(entry),
			Status:    string(entry.Outcome),
			Source:    "audit",
			Timestamp: normalizeTimestamp(entry.Timestamp),
			ProjectID: firstNonEmpty(auditProjectID(entry), snapshot.project.ID),
			PlanID:    auditPlanID(entry),
			TaskID:    auditTaskID(entry),
			SessionID: auditEntrySessionID(entry),
			AgentID:   auditEntryAgentID(entry),
			AuditID:   entry.ID,
		})
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].Timestamp == events[j].Timestamp {
			return events[i].ID < events[j].ID
		}
		return events[i].Timestamp < events[j].Timestamp
	})
	return events
}

func buildReplayEvents(traceResp *agent.ConversationTraceResponse, auditEntries []audit.AuditLogEntry) ([]WorkflowReplayEvent, int) {
	events := make([]WorkflowReplayEvent, 0)
	traceCount := 0
	agentNames := make(map[string]string)
	if traceResp != nil {
		for _, ag := range traceResp.Agents {
			agentNames[ag.ID] = ag.Name
		}
		if traceResp.Trace != nil {
			flattenTrace(traceResp.Trace, traceResp.SessionID, agentNames, &events, &traceCount)
		}
	}

	for _, entry := range auditEntries {
		events = append(events, WorkflowReplayEvent{
			ID:        "audit-" + entry.ID,
			Type:      "audit",
			Lane:      "AUDIT",
			Speaker:   firstNonEmpty(entry.Actor.Name, entry.Actor.Type, "audit"),
			Message:   describeAuditEntry(entry),
			Evidence:  fmt.Sprintf("audit:%s", entry.ID),
			Status:    string(entry.Outcome),
			Timestamp: normalizeTimestamp(entry.Timestamp),
			ProjectID: auditProjectID(entry),
			PlanID:    auditPlanID(entry),
			TaskID:    auditTaskID(entry),
			SessionID: auditEntrySessionID(entry),
			AgentID:   auditEntryAgentID(entry),
			AuditID:   entry.ID,
		})
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].Timestamp == events[j].Timestamp {
			return events[i].ID < events[j].ID
		}
		return events[i].Timestamp < events[j].Timestamp
	})
	return events, traceCount
}

func flattenTrace(trace *agent.CallTrace, sessionID string, agentNames map[string]string, events *[]WorkflowReplayEvent, traceCount *int) {
	if trace == nil {
		return
	}
	*traceCount += 1
	*events = append(*events, WorkflowReplayEvent{
		ID:        "trace-" + trace.ID,
		Type:      "trace",
		Lane:      strings.ToUpper(string(trace.AgentRole)),
		Speaker:   firstNonEmpty(agentNames[trace.AgentID], string(trace.AgentRole), trace.AgentID),
		Message:   firstNonEmpty(trace.Output, trace.ToolName),
		Evidence:  fmt.Sprintf("trace:%s", trace.ID),
		Status:    trace.Status,
		Timestamp: normalizeTimestamp(trace.StartTime),
		ProjectID: firstNonEmpty(trace.ProjectID, inputString(trace.Input, "project_id", "projectId")),
		PlanID:    firstNonEmpty(trace.PlanID, inputString(trace.Input, "plan_id", "planId")),
		TaskID:    firstNonEmpty(trace.TaskID, inputString(trace.Input, "task_id", "taskId")),
		SessionID: firstNonEmpty(trace.SessionID, sessionID),
		AgentID:   trace.AgentID,
		TraceID:   trace.ID,
	})
	for _, child := range trace.Children {
		flattenTrace(child, sessionID, agentNames, events, traceCount)
	}
}

func collectSessionIDs(proj *project.Project, plans []planner.Plan, tasks []planner.Task, auditEntries []audit.AuditLogEntry) []string {
	sessions := make([]string, 0)
	sessions = appendMetadataSessionIDs(sessions, proj.Metadata)
	for _, plan := range plans {
		sessions = appendMetadataSessionIDs(sessions, plan.Metadata)
	}
	for _, task := range tasks {
		sessions = appendMetadataSessionIDs(sessions, task.Metadata)
	}
	for _, entry := range auditEntries {
		sessions = appendUnique(sessions, auditEntrySessionID(entry))
	}
	sort.Strings(sessions)
	return sessions
}

func appendMetadataSessionIDs(values []string, metadata map[string]interface{}) []string {
	if metadata == nil {
		return values
	}
	return appendUnique(values,
		mapString(metadata, "session_id", "sessionId", "workflow_session_id", "workflowSessionId"),
	)
}

func appendUnique(values []string, candidates ...string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		values = append(values, candidate)
		seen[candidate] = struct{}{}
	}
	return values
}

func replayAgents(traceResp *agent.ConversationTraceResponse, fallback []agent.Agent) []agent.Agent {
	if traceResp != nil && len(traceResp.Agents) > 0 {
		return traceResp.Agents
	}
	return fallback
}

func nilIfTraceResponseNil(traceResp *agent.ConversationTraceResponse) *agent.CallTrace {
	if traceResp == nil {
		return nil
	}
	return traceResp.Trace
}

func describeAuditEntry(entry audit.AuditLogEntry) string {
	resourceLabel := firstNonEmpty(entry.Resource.Name, entry.Resource.Type, entry.Resource.ID)
	return strings.TrimSpace(fmt.Sprintf("%s %s %s", entry.Action, resourceLabel, entry.Outcome))
}

func isRelevantAuditEntry(entry audit.AuditLogEntry, ids map[string]struct{}) bool {
	if _, ok := ids[entry.Resource.ID]; ok {
		return true
	}
	for _, value := range []string{auditProjectID(entry), auditPlanID(entry), auditTaskID(entry)} {
		if _, ok := ids[value]; ok {
			return true
		}
	}
	return false
}

func auditProjectID(entry audit.AuditLogEntry) string {
	return firstNonEmpty(
		traceString(entry.Trace, func(t *audit.AuditTrace) string { return t.ProjectID }),
		mapString(entry.Details, "project_id", "projectId"),
	)
}

func auditPlanID(entry audit.AuditLogEntry) string {
	return firstNonEmpty(
		traceString(entry.Trace, func(t *audit.AuditTrace) string { return t.PlanID }),
		mapString(entry.Details, "plan_id", "planId"),
	)
}

func auditTaskID(entry audit.AuditLogEntry) string {
	return firstNonEmpty(
		traceString(entry.Trace, func(t *audit.AuditTrace) string { return t.TaskID }),
		mapString(entry.Details, "task_id", "taskId"),
	)
}

func auditEntrySessionID(entry audit.AuditLogEntry) string {
	return firstNonEmpty(
		traceString(entry.Trace, func(t *audit.AuditTrace) string { return t.SessionID }),
		entry.Actor.SessionID,
		mapString(entry.Details, "session_id", "sessionId"),
	)
}

func auditEntryAgentID(entry audit.AuditLogEntry) string {
	return firstNonEmpty(
		traceString(entry.Trace, func(t *audit.AuditTrace) string { return t.AgentID }),
		mapString(entry.Details, "agent_id", "agentId"),
	)
}

func traceString(trace *audit.AuditTrace, selector func(*audit.AuditTrace) string) string {
	if trace == nil {
		return ""
	}
	return selector(trace)
}

func mapString(values map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if values == nil {
			return ""
		}
		if value, ok := values[key]; ok {
			switch typed := value.(type) {
			case string:
				return typed
			case fmt.Stringer:
				return typed.String()
			default:
				return fmt.Sprintf("%v", typed)
			}
		}
	}
	return ""
}

func inputString(values map[string]interface{}, keys ...string) string {
	return mapString(values, keys...)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeTimestamp(value int64) int64 {
	switch {
	case value > 1_000_000_000_000_000:
		return value / 1_000_000
	case value > 1_000_000_000_000:
		return value
	default:
		return value * 1000
	}
}

var defaultWorkflowService *Service

func GetService() *Service {
	if defaultWorkflowService == nil {
		defaultWorkflowService = NewService(
			project.GetProjectService(),
			planner.GetPlanner(),
			agent.GetAgentService(),
			audit.GetAuditService(),
		)
	}
	return defaultWorkflowService
}

func SetService(svc *Service) {
	defaultWorkflowService = svc
}
