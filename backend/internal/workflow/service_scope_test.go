package workflow

// Tests for the scope-consuming read paths (T13.01.b / E-01): GetOverview,
// GetTimeline, and GetReplay must serve exactly the objects ResolveProjectScope
// authorizes. Foreign sessions are rejected before any trace read; audit
// entries are filtered by project-visible objects first and only then
// intersected with the requested session; an empty project yields empty
// collections instead of the global agent list; audit content and agent
// sessions never feed back into the project's session set.
//
// The A/B/empty projects/planner fakes come from scope_test.go; agents and
// audit are the real in-memory services so trace/audit behavior is the
// production one.

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/codeflow/backend/internal/agent"
	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/floweng"
)

// recordingAgentService wraps the real in-memory agent service and records
// every GetConversationTrace call, proving rejected replays never read traces.
type recordingAgentService struct {
	agent.IAgentService
	traceCalls []string
}

func (r *recordingAgentService) GetConversationTrace(ctx context.Context, sessionID string) (*agent.ConversationTraceResponse, error) {
	r.traceCalls = append(r.traceCalls, sessionID)
	return r.IAgentService.GetConversationTrace(ctx, sessionID)
}

type scopedServiceFixture struct {
	svc    *Service
	agents *recordingAgentService
	audit  *audit.AuditService
}

func newScopedServiceFixture() *scopedServiceFixture {
	scopeFixture := newScopeFixture()
	agentSvc := agent.NewInMemoryAgentService()
	for _, ag := range []*agent.Agent{
		{ID: "agent-a1", Name: "A Default", SessionID: "sess-a-default"},
		{ID: "agent-a2", Name: "A Task", SessionID: "sess-a-task"},
		{ID: "agent-b1", Name: "B Default", SessionID: "sess-b-default"},
		{ID: "agent-shared", Name: "Shared", SessionID: "sess-shared"},
		{ID: "agent-nosession", Name: "No Session"},
	} {
		agentSvc.RegisterAgent(ag)
	}
	recording := &recordingAgentService{IAgentService: agentSvc}
	auditSvc := audit.NewAuditService(audit.NewMemoryStorage())
	return &scopedServiceFixture{
		svc:    NewService(scopeFixture.deps.Projects, scopeFixture.deps.Planner, recording, auditSvc),
		agents: recording,
		audit:  auditSvc,
	}
}

// logAudit records one entry with a caller-controlled trace and resource.
func (f *scopedServiceFixture) logAudit(t *testing.T, id string, trace *audit.AuditTrace, resource audit.AuditResource) {
	t.Helper()
	ctx := context.Background()
	if trace != nil {
		ctx = audit.ContextWithTrace(ctx, trace)
	}
	if err := f.audit.Log(ctx, &audit.AuditLogEntry{
		ID:        id,
		Timestamp: 1_000_000,
		EventType: audit.EventApproval,
		Severity:  audit.SeverityInfo,
		Actor:     audit.AuditActor{ID: "sys", Type: "system", Name: "sys"},
		Resource:  resource,
		Action:    "checkpoint",
		Outcome:   audit.OutcomeSuccess,
	}); err != nil {
		t.Fatalf("log audit %s: %v", id, err)
	}
}

func auditIDsOf(entries []audit.AuditLogEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return ids
}

func TestGetReplayRejectsForeignSession(t *testing.T) {
	f := newScopedServiceFixture()
	ctx := context.Background()

	// B has a real trace a naive read path would happily return.
	traceID := f.agents.IAgentService.StartTrace("sess-b-default", "agent-b1", "b_tool", nil)
	f.agents.IAgentService.EndTrace(traceID, "b output", "completed")

	for _, sessionID := range []string{"sess-b-default", "sess-b-plan", "sess-b-task", "sess-shared", "sess-never-existed"} {
		replay, err := f.svc.GetReplay(ctx, "proj-a", sessionID)
		if err == nil {
			t.Fatalf("%s: expected rejection", sessionID)
		}
		if !errors.Is(err, ErrSessionNotInProject) {
			t.Fatalf("%s: err=%v must match ErrSessionNotInProject", sessionID, err)
		}
		var mismatch *SessionNotInProjectError
		if !errors.As(err, &mismatch) {
			t.Fatalf("%s: err=%v must be *SessionNotInProjectError", sessionID, err)
		}
		if mismatch.ProjectID != "proj-a" || mismatch.SessionID != sessionID {
			t.Fatalf("mismatch carries (%q,%q) want (proj-a,%q)", mismatch.ProjectID, mismatch.SessionID, sessionID)
		}
		if replay != nil {
			t.Fatalf("%s: replay must be nil on rejection, got %+v", sessionID, replay)
		}
	}
	if len(f.agents.traceCalls) != 0 {
		t.Fatalf("trace fetched for foreign sessions: %v", f.agents.traceCalls)
	}
}

func TestGetReplayFetchesTraceForAuthorizedSession(t *testing.T) {
	f := newScopedServiceFixture()
	ctx := context.Background()

	traceID := f.agents.IAgentService.StartTrace("sess-a-task", "agent-a2", "summarize", map[string]interface{}{
		"project_id": "proj-a",
		"plan_id":    "plan-a1",
		"task_id":    "task-a1",
	})
	f.agents.IAgentService.EndTrace(traceID, "done", "completed")

	replay, err := f.svc.GetReplay(ctx, "proj-a", "sess-a-task")
	if err != nil {
		t.Fatalf("GetReplay: %v", err)
	}
	if replay.SessionID != "sess-a-task" {
		t.Fatalf("SessionID=%q want sess-a-task", replay.SessionID)
	}
	if replay.Trace == nil || replay.Summary.TraceCount != 1 {
		t.Fatalf("expected the authorized trace, Trace=%+v TraceCount=%d", replay.Trace, replay.Summary.TraceCount)
	}
	if !reflect.DeepEqual(f.agents.traceCalls, []string{"sess-a-task"}) {
		t.Fatalf("traceCalls=%v want [sess-a-task]", f.agents.traceCalls)
	}
}

func TestEmptyProjectReturnsEmptyCollections(t *testing.T) {
	f := newScopedServiceFixture()
	ctx := context.Background()

	overview, err := f.svc.GetOverview(ctx, "proj-empty")
	if err != nil {
		t.Fatalf("GetOverview: %v", err)
	}
	if len(overview.Plans) != 0 || len(overview.Tasks) != 0 || len(overview.SessionIDs) != 0 {
		t.Fatalf("empty project must have no plans/tasks/sessions, got %+v", overview)
	}
	// Explicitly forbidden: falling back to the global agent list when the
	// project has no sessions (old loadRelevantAgents branch).
	if len(overview.Agents) != 0 || overview.Summary.AgentCount != 0 {
		t.Fatalf("empty project must not expose global agents, got %+v", overview.Agents)
	}

	timeline, err := f.svc.GetTimeline(ctx, "proj-empty")
	if err != nil {
		t.Fatalf("GetTimeline: %v", err)
	}
	if len(timeline.SessionIDs) != 0 {
		t.Fatalf("empty project session ids=%v", timeline.SessionIDs)
	}
	for _, ev := range timeline.Events {
		if ev.Type == "agent" || ev.Type == "audit" || ev.SessionID != "" || ev.AgentID != "" {
			t.Fatalf("empty project timeline leaked %+v", ev)
		}
	}

	replay, err := f.svc.GetReplay(ctx, "proj-empty", "")
	if err != nil {
		t.Fatalf("GetReplay: %v", err)
	}
	if replay.SessionID != "" || len(replay.Events) != 0 || len(replay.Agents) != 0 || replay.Trace != nil {
		t.Fatalf("empty project replay must be empty, got %+v", replay)
	}
	if len(f.agents.traceCalls) != 0 {
		t.Fatalf("empty project must not fetch any trace, got %v", f.agents.traceCalls)
	}
}

func TestWorkflowScopeFiltersAuditAndTraceTogether(t *testing.T) {
	f := newScopedServiceFixture()
	ctx := context.Background()

	// Visible to proj-a: by resource ID, by authorized session only, and by
	// resource while carrying a session no authority binds to A.
	f.logAudit(t, "a-res", nil, audit.AuditResource{Type: "project", ID: "proj-a"})
	f.logAudit(t, "a-sess", &audit.AuditTrace{SessionID: "sess-a-task"}, audit.AuditResource{Type: "tool", ID: "tool-x"})
	f.logAudit(t, "a-foreign-session", &audit.AuditTrace{ProjectID: "proj-a", SessionID: "sess-audit-only"},
		audit.AuditResource{Type: "project", ID: "proj-a"})
	// Never visible to proj-a: B's entries, however they are keyed.
	f.logAudit(t, "b-res", &audit.AuditTrace{ProjectID: "proj-b", SessionID: "sess-b-default"},
		audit.AuditResource{Type: "project", ID: "proj-b"})
	f.logAudit(t, "b-sess", &audit.AuditTrace{SessionID: "sess-b-default"}, audit.AuditResource{Type: "tool", ID: "tool-y"})

	overview, err := f.svc.GetOverview(ctx, "proj-a")
	if err != nil {
		t.Fatalf("GetOverview: %v", err)
	}
	if overview.Summary.AuditCount != 3 {
		t.Fatalf("AuditCount=%d want 3 (a-res, a-sess, a-foreign-session)", overview.Summary.AuditCount)
	}
	// The audit-carried foreign session must not widen the session set.
	assertIDs(t, "SessionIDs", overview.SessionIDs,
		[]string{"sess-a-default", "sess-a-plan", "sess-a-task", "sess-a-task2"})

	timeline, err := f.svc.GetTimeline(ctx, "proj-a")
	if err != nil {
		t.Fatalf("GetTimeline: %v", err)
	}
	auditEvents := make(map[string]bool)
	for _, ev := range timeline.Events {
		if ev.Type != "audit" {
			continue
		}
		auditEvents[ev.AuditID] = true
		if ev.SessionID == "sess-b-default" {
			t.Fatalf("timeline leaked B audit session via %+v", ev)
		}
	}
	// a-foreign-session is A's own entry (resource match); it stays visible,
	// but its unauthorized session must not widen SessionIDs (checked above).
	for _, id := range []string{"a-res", "a-sess", "a-foreign-session"} {
		if !auditEvents[id] {
			t.Fatalf("timeline missing audit event %s: %v", id, auditEvents)
		}
	}
	for _, id := range []string{"b-res", "b-sess"} {
		if auditEvents[id] {
			t.Fatalf("timeline leaked B audit event %s", id)
		}
	}

	// Replay intersects project-visible entries with the requested session:
	// only a-sess survives; B entries can never reach the intersection.
	replay, err := f.svc.GetReplay(ctx, "proj-a", "sess-a-task")
	if err != nil {
		t.Fatalf("GetReplay: %v", err)
	}
	if got := auditIDsOf(replay.AuditEntries); !reflect.DeepEqual(got, []string{"a-sess"}) {
		t.Fatalf("replay audit=%v want [a-sess]", got)
	}
	if replay.Summary.AuditCount != 1 {
		t.Fatalf("replay AuditCount=%d want 1", replay.Summary.AuditCount)
	}

	// Audit content alone can never authorize a session for replay.
	if _, err := f.svc.GetReplay(ctx, "proj-a", "sess-audit-only"); !errors.Is(err, ErrSessionNotInProject) {
		t.Fatalf("sess-audit-only replay err=%v want ErrSessionNotInProject", err)
	}
}

func TestGetReplayDefaultSessionWithoutTraceReturnsEmptyTrace(t *testing.T) {
	f := newScopedServiceFixture()
	ctx := context.Background()

	// Traces exist elsewhere (B's default, A's task session); the defaulted
	// session itself has none and must not fall back to either.
	bTrace := f.agents.IAgentService.StartTrace("sess-b-default", "agent-b1", "b_tool", nil)
	f.agents.IAgentService.EndTrace(bTrace, "b output", "completed")
	aTrace := f.agents.IAgentService.StartTrace("sess-a-task", "agent-a2", "a_tool", nil)
	f.agents.IAgentService.EndTrace(aTrace, "a output", "completed")

	replay, err := f.svc.GetReplay(ctx, "proj-a", "")
	if err != nil {
		t.Fatalf("GetReplay: %v", err)
	}
	if replay.SessionID != "sess-a-default" {
		t.Fatalf("SessionID=%q want sess-a-default (first authorized session)", replay.SessionID)
	}
	if replay.Trace != nil || replay.Summary.TraceCount != 0 {
		t.Fatalf("default session without trace must yield empty trace, got Trace=%+v TraceCount=%d",
			replay.Trace, replay.Summary.TraceCount)
	}
	if !reflect.DeepEqual(f.agents.traceCalls, []string{"sess-a-default"}) {
		t.Fatalf("traceCalls=%v want exactly [sess-a-default]", f.agents.traceCalls)
	}

	explicit, err := f.svc.GetReplay(ctx, "proj-a", "sess-a-default")
	if err != nil {
		t.Fatalf("GetReplay explicit default: %v", err)
	}
	if explicit.Trace != nil || explicit.Summary.TraceCount != 0 {
		t.Fatalf("explicit default session must also yield empty trace, got %+v", explicit.Trace)
	}
}

func TestValidProjectViewKeepsAllAuthorizedData(t *testing.T) {
	f := newScopedServiceFixture()
	ctx := context.Background()

	overview, err := f.svc.GetOverview(ctx, "proj-a")
	if err != nil {
		t.Fatalf("GetOverview: %v", err)
	}
	if len(overview.Plans) != 2 || len(overview.Tasks) != 3 {
		t.Fatalf("plans/tasks shrank: %d plans, %d tasks", len(overview.Plans), len(overview.Tasks))
	}
	agents := make([]string, 0, len(overview.Agents))
	for _, ag := range overview.Agents {
		agents = append(agents, ag.ID)
	}
	sort.Strings(agents)
	assertIDs(t, "Agents", agents, []string{"agent-a1", "agent-a2"})
	if overview.Summary.PlanCount != 2 || overview.Summary.TaskCount != 3 ||
		overview.Summary.AgentCount != 2 || overview.Summary.SessionCount != 4 {
		t.Fatalf("summary degraded: %+v", overview.Summary)
	}

	timeline, err := f.svc.GetTimeline(ctx, "proj-a")
	if err != nil {
		t.Fatalf("GetTimeline: %v", err)
	}
	eventIDs := make(map[string]bool)
	for _, ev := range timeline.Events {
		eventIDs[ev.ID] = true
	}
	for _, id := range []string{
		"project-proj-a", "plan-plan-a1", "plan-plan-a2",
		"task-task-a1", "task-task-a2", "task-task-a3",
		"agent-agent-a1", "agent-agent-a2",
	} {
		if !eventIDs[id] {
			t.Fatalf("timeline missing %s", id)
		}
	}

	traceID := f.agents.IAgentService.StartTrace("sess-a-task", "agent-a2", "summarize", nil)
	f.agents.IAgentService.EndTrace(traceID, "done", "completed")
	replay, err := f.svc.GetReplay(ctx, "proj-a", "sess-a-task")
	if err != nil {
		t.Fatalf("GetReplay: %v", err)
	}
	if replay.Trace == nil || len(replay.Agents) == 0 {
		t.Fatalf("authorized replay lost its trace/agents: %+v", replay)
	}
}

func TestFlowengEventsStayInProjectScope(t *testing.T) {
	eng := swapFlowengEngine(t)
	f := newScopedServiceFixture()
	ctx := context.Background()

	flowA, err := eng.Create(ctx, &floweng.CreateFlowRequest{ProjectID: "proj-a", TemplateID: floweng.TemplateNewProject})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Advance(ctx, flowA.ID, &floweng.AdvanceRequest{}); err != nil {
		t.Fatal(err)
	}
	flowB, err := eng.Create(ctx, &floweng.CreateFlowRequest{ProjectID: "proj-b", TemplateID: floweng.TemplateNewProject})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Advance(ctx, flowB.ID, &floweng.AdvanceRequest{}); err != nil {
		t.Fatal(err)
	}

	timeline, err := f.svc.GetTimeline(ctx, "proj-a")
	if err != nil {
		t.Fatalf("GetTimeline: %v", err)
	}
	flowengTimeline := 0
	for _, ev := range timeline.Events {
		if ev.Lane != "floweng" {
			continue
		}
		flowengTimeline++
		if ev.ProjectID != "proj-a" {
			t.Fatalf("timeline floweng event from %q leaked into proj-a: %+v", ev.ProjectID, ev)
		}
	}
	if flowengTimeline == 0 {
		t.Fatal("expected proj-a floweng events in timeline")
	}

	replay, err := f.svc.GetReplay(ctx, "proj-a", "")
	if err != nil {
		t.Fatalf("GetReplay: %v", err)
	}
	flowengReplay := 0
	for _, ev := range replay.Events {
		if ev.Lane != "floweng" {
			continue
		}
		flowengReplay++
		if ev.ProjectID != "proj-a" || ev.Evidence != "flow:"+flowA.ID {
			t.Fatalf("replay floweng event leaked from another project: %+v", ev)
		}
	}
	if flowengReplay == 0 {
		t.Fatal("expected proj-a floweng events in replay")
	}
}
