package workflow

// Tests for G14 second half: GetReplay merges floweng flow events into the replay
// stream (lane "floweng"), timestamp-ordered and project-scoped, and stays safe
// when the engine is absent or a project has no flows. Mirrors the timeline
// bridge test pattern (swap the process-wide engine, restore on cleanup).

import (
	"context"
	"testing"

	"github.com/codeflow/backend/internal/agent"
	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
)

// newReplayTestService wires a Service over in-memory deps. The floweng engine is
// process-global and swapped separately by each test.
func newReplayTestService() *Service {
	return NewService(
		project.NewInMemoryProjectService(),
		planner.NewInMemoryPlanner(),
		agent.NewInMemoryAgentService(),
		audit.NewAuditService(audit.NewMemoryStorage()),
	)
}

func swapFlowengEngine(t *testing.T) *floweng.InMemoryEngine {
	t.Helper()
	prev := floweng.GetEngine()
	eng := floweng.NewInMemoryEngine(nil)
	floweng.SetEngine(eng)
	t.Cleanup(func() { floweng.SetEngine(prev) })
	return eng
}

// logProjectAudit records an audit entry relevant to projectID (Resource.ID match)
// with a caller-controlled timestamp (Log preserves a non-zero Timestamp).
func logProjectAudit(t *testing.T, s *Service, ctx context.Context, id, projectID string, ts int64) {
	t.Helper()
	if err := s.audit.Log(ctx, &audit.AuditLogEntry{
		ID:        id,
		Timestamp: ts,
		EventType: audit.EventApproval,
		Severity:  audit.SeverityInfo,
		Actor:     audit.AuditActor{ID: "sys", Type: "system", Name: "sys"},
		Resource:  audit.AuditResource{Type: "project", ID: projectID},
		Action:    "checkpoint",
		Outcome:   audit.OutcomeSuccess,
	}); err != nil {
		t.Fatalf("log audit %s: %v", id, err)
	}
}

func TestCollectFlowengReplayEvents(t *testing.T) {
	eng := swapFlowengEngine(t)
	ctx := context.Background()

	flow, err := eng.Create(ctx, &floweng.CreateFlowRequest{
		ProjectID:  "proj-replay",
		TemplateID: floweng.TemplateNewProject,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Advance(ctx, flow.ID, &floweng.AdvanceRequest{}); err != nil {
		t.Fatal(err)
	}

	events := collectFlowengReplayEvents(ctx, "proj-replay")
	if len(events) < 2 {
		t.Fatalf("expected floweng replay events, got %d", len(events))
	}
	for _, e := range events {
		if e.Lane != "floweng" || e.Speaker != "floweng" {
			t.Fatalf("bad replay event lane/speaker: %+v", e)
		}
		if e.ProjectID != "proj-replay" {
			t.Fatalf("project id: %+v", e)
		}
		if e.Evidence != "flow:"+flow.ID {
			t.Fatalf("evidence=%q want flow:%s", e.Evidence, flow.ID)
		}
		if e.Timestamp <= 0 {
			t.Fatalf("expected positive timestamp, got %d", e.Timestamp)
		}
	}

	// Other project → empty (project-scoped via engine List).
	if got := collectFlowengReplayEvents(ctx, "other"); len(got) != 0 {
		t.Fatalf("expected empty for other project, got %d", len(got))
	}

	// Engine absent → nil (HasEngine guard, no lazy construction side effect).
	floweng.SetEngine(nil)
	if got := collectFlowengReplayEvents(ctx, "proj-replay"); got != nil {
		t.Fatalf("expected nil when engine absent, got %d events", len(got))
	}
}

func TestGetReplayMergesFlowengEvents(t *testing.T) {
	eng := swapFlowengEngine(t)
	s := newReplayTestService()
	ctx := context.Background()

	proj, err := s.projects.CreateProject(ctx, &project.ProjectCreateRequest{Title: "Replay Target"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Two audit entries straddling "now": early normalizes to ~1e9ms, late to 5e12ms,
	// while floweng events are real (~now) and land strictly between them.
	logProjectAudit(t, s, ctx, "early", proj.ID, 1_000_000)                // ns → ~1e9 ms
	logProjectAudit(t, s, ctx, "late", proj.ID, 5_000_000_000_000_000_000) // ns → 5e12 ms

	flow, err := eng.Create(ctx, &floweng.CreateFlowRequest{ProjectID: proj.ID, TemplateID: floweng.TemplateNewProject})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Advance(ctx, flow.ID, &floweng.AdvanceRequest{}); err != nil {
		t.Fatal(err)
	}
	// A flow in an unrelated project must never surface in this project's replay.
	otherFlow, err := eng.Create(ctx, &floweng.CreateFlowRequest{ProjectID: "other-proj", TemplateID: floweng.TemplateNewProject})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Advance(ctx, otherFlow.ID, &floweng.AdvanceRequest{}); err != nil {
		t.Fatal(err)
	}

	replay, err := s.GetReplay(ctx, proj.ID, "")
	if err != nil {
		t.Fatalf("GetReplay: %v", err)
	}
	evs := replay.Events
	if len(evs) < 3 {
		t.Fatalf("expected audit+floweng events, got %d: %+v", len(evs), evs)
	}

	// Globally timestamp-ordered (proves interleaving across lanes).
	for i := 1; i < len(evs); i++ {
		if evs[i-1].Timestamp > evs[i].Timestamp {
			t.Fatalf("events not timestamp-ordered at %d: %d > %d", i, evs[i-1].Timestamp, evs[i].Timestamp)
		}
	}
	// Early audit first, late audit last, floweng events sandwiched between.
	if evs[0].Lane != "AUDIT" || evs[0].ID != "audit-early" {
		t.Fatalf("first event=%+v want early audit", evs[0])
	}
	last := evs[len(evs)-1]
	if last.Lane != "AUDIT" || last.ID != "audit-late" {
		t.Fatalf("last event=%+v want late audit", last)
	}

	flowengCount := 0
	for _, e := range evs {
		if e.Lane != "floweng" {
			continue
		}
		flowengCount++
		if e.ProjectID != proj.ID {
			t.Fatalf("floweng event project=%q want=%q", e.ProjectID, proj.ID)
		}
		if e.Evidence != "flow:"+flow.ID {
			t.Fatalf("floweng event evidence=%q want flow:%s (other-project flow leaked?)", e.Evidence, flow.ID)
		}
	}
	if flowengCount == 0 {
		t.Fatal("expected floweng-lane events interleaved in replay")
	}
	if replay.Summary.EventCount != len(evs) {
		t.Fatalf("summary EventCount=%d want=%d", replay.Summary.EventCount, len(evs))
	}
}

func TestGetReplayEmptyFlowsUnaffected(t *testing.T) {
	eng := swapFlowengEngine(t)
	s := newReplayTestService()
	ctx := context.Background()

	proj, err := s.projects.CreateProject(ctx, &project.ProjectCreateRequest{Title: "Empty Flows"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	logProjectAudit(t, s, ctx, "only", proj.ID, 2_000_000)

	// Engine is wired and non-empty, but only for a different project.
	otherFlow, err := eng.Create(ctx, &floweng.CreateFlowRequest{ProjectID: "somewhere-else", TemplateID: floweng.TemplateNewProject})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Advance(ctx, otherFlow.ID, &floweng.AdvanceRequest{}); err != nil {
		t.Fatal(err)
	}

	replay, err := s.GetReplay(ctx, proj.ID, "")
	if err != nil {
		t.Fatalf("GetReplay: %v", err)
	}
	for _, e := range replay.Events {
		if e.Lane == "floweng" {
			t.Fatalf("unexpected floweng event for empty-flow project: %+v", e)
		}
	}
	if len(replay.Events) != 1 || replay.Events[0].ID != "audit-only" {
		t.Fatalf("expected single audit event, got %+v", replay.Events)
	}
	if replay.Summary.EventCount != 1 {
		t.Fatalf("EventCount=%d want=1", replay.Summary.EventCount)
	}
}
