package floweng

import (
	"context"
	"strings"
	"testing"
)

// Rejecting a gate whose OnFail is escalate_to_debate appends an extra
// gate.escalate_debate event carrying the gate id and reason (design §2).
func TestDecideGateRejectEscalateEmitsDebateEvent(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	flow.Stages[0].Gates = []Gate{{
		ID: "gate-esc", Phase: GatePhaseExit, Kind: GateKindHumanApproval, OnFail: GateOnFailEscalateDebate,
	}}
	if err := e.putRaw(flow); err != nil {
		t.Fatal(err)
	}

	rejected := false
	got, err := e.DecideGate(context.Background(), flow.ID, "gate-esc",
		&GateDecisionRequest{Approved: &rejected, Reason: "insufficient coverage"})
	if err != nil {
		t.Fatal(err)
	}
	ev := findFlowEvent(got.Events, "gate.escalate_debate")
	if ev == nil {
		t.Fatalf("expected gate.escalate_debate event, got %v", flowEventTypes(got.Events))
	}
	if !strings.Contains(ev.Message, "gate-esc") || !strings.Contains(ev.Message, "insufficient coverage") {
		t.Fatalf("escalate event should carry gate id and reason, got %q", ev.Message)
	}
	// stage still parks on waiting_gate (escalation does not unblock).
	if got.Stages[0].Status != StageStatusWaitingGate {
		t.Fatalf("stage should be waiting_gate after escalate reject, got %s", got.Stages[0].Status)
	}
}

// A rejected block-policy gate (default/empty OnFail) must not emit an escalate event.
func TestDecideGateRejectBlockNoDebateEvent(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	flow.Stages[0].Gates = []Gate{{ID: "gate-block", Phase: GatePhaseExit, Kind: GateKindHumanApproval}}
	if err := e.putRaw(flow); err != nil {
		t.Fatal(err)
	}

	rejected := false
	got, err := e.DecideGate(context.Background(), flow.ID, "gate-block",
		&GateDecisionRequest{Approved: &rejected, Reason: "no"})
	if err != nil {
		t.Fatal(err)
	}
	if !flowHasEvent(got.Events, "gate.rejected") {
		t.Fatalf("expected gate.rejected event, got %v", flowEventTypes(got.Events))
	}
	if flowHasEvent(got.Events, "gate.escalate_debate") {
		t.Fatalf("block-policy gate must not escalate, events=%v", flowEventTypes(got.Events))
	}
}

// A blocking exit gate with escalate policy surfaces the escalation hint in the
// Advance error (passExitGates wrapping style preserved).
func TestAdvanceBlockedEscalateGateMentionsEscalation(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	flow.Stages[0].Gates = []Gate{{
		ID: "gate-esc-exit", Phase: GatePhaseExit, Kind: GateKindHumanApproval, OnFail: GateOnFailEscalateDebate,
	}}
	if err := e.putRaw(flow); err != nil {
		t.Fatal(err)
	}

	_, err = e.Advance(context.Background(), flow.ID, &AdvanceRequest{})
	if err == nil {
		t.Fatal("expected block on escalate exit gate")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "escalat") {
		t.Fatalf("blocked-gate error should mention escalation, got %q", err.Error())
	}

	// A plain block gate error must NOT mention escalation.
	flow2, _ := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	flow2.Stages[0].Gates = []Gate{{ID: "g-plain", Phase: GatePhaseExit, Kind: GateKindHumanApproval}}
	if err := e.putRaw(flow2); err != nil {
		t.Fatal(err)
	}
	_, err = e.Advance(context.Background(), flow2.ID, &AdvanceRequest{})
	if err == nil {
		t.Fatal("expected block on plain human gate")
	}
	if strings.Contains(strings.ToLower(err.Error()), "escalat") {
		t.Fatalf("plain block gate error should not mention escalation, got %q", err.Error())
	}
}
