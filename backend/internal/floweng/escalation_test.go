package floweng

import (
	"context"
	"sync"
	"testing"
)

// captureEscalationHandler records all escalation calls for assertions.
type captureEscalationHandler struct {
	mu     sync.Mutex
	calls  []escalationCall
}

type escalationCall struct {
	flowID  string
	stageID string
	gateID  string
	reason  string
}

func (h *captureEscalationHandler) OnGateEscalation(ctx context.Context, flow *Flow, stage *Stage, gate *Gate, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, escalationCall{
		flowID:  flow.ID,
		stageID: stage.ID,
		gateID:  gate.ID,
		reason:  reason,
	})
}

func (h *captureEscalationHandler) lastCall() (escalationCall, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.calls) == 0 {
		return escalationCall{}, false
	}
	return h.calls[len(h.calls)-1], true
}

func (h *captureEscalationHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.calls)
}

// Rejecting an escalate gate invokes the handler with the right flow/stage/gate
// ids and reason.
func TestEscalationHandlerCalledOnReject(t *testing.T) {
	e := NewInMemoryEngine(nil)
	handler := &captureEscalationHandler{}
	e.SetGateEscalationHandler(handler)

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
	flow, err = e.DecideGate(context.Background(), flow.ID, "gate-esc",
		&GateDecisionRequest{Approved: &rejected, Reason: "insufficient"})
	if err != nil {
		t.Fatal(err)
	}
	if handler.count() != 1 {
		t.Fatalf("handler calls=%d want 1", handler.count())
	}
	call, _ := handler.lastCall()
	if call.flowID != flow.ID {
		t.Fatalf("handler flow=%q want %q", call.flowID, flow.ID)
	}
	if call.stageID != flow.Stages[0].ID {
		t.Fatalf("handler stage=%q want %q", call.stageID, flow.Stages[0].ID)
	}
	if call.gateID != "gate-esc" {
		t.Fatalf("handler gate=%q want gate-esc", call.gateID)
	}
	if call.reason != "insufficient" {
		t.Fatalf("handler reason=%q want insufficient", call.reason)
	}
}

// A plain block-policy rejection must NOT invoke the handler.
func TestEscalationHandlerNotCalledOnBlockReject(t *testing.T) {
	e := NewInMemoryEngine(nil)
	handler := &captureEscalationHandler{}
	e.SetGateEscalationHandler(handler)

	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	flow.Stages[0].Gates = []Gate{{
		ID: "gate-block", Phase: GatePhaseExit, Kind: GateKindHumanApproval,
	}}
	if err := e.putRaw(flow); err != nil {
		t.Fatal(err)
	}

	rejected := false
	if _, err := e.DecideGate(context.Background(), flow.ID, "gate-block",
		&GateDecisionRequest{Approved: &rejected, Reason: "no"}); err != nil {
		t.Fatal(err)
	}
	if handler.count() != 0 {
		t.Fatalf("block reject should not invoke escalation handler, calls=%d", handler.count())
	}
}

// An approval must NOT invoke the handler.
func TestEscalationHandlerNotCalledOnApprove(t *testing.T) {
	e := NewInMemoryEngine(nil)
	handler := &captureEscalationHandler{}
	e.SetGateEscalationHandler(handler)

	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	flow.Stages[0].Gates = []Gate{{
		ID: "gate-esc-app", Phase: GatePhaseExit, Kind: GateKindHumanApproval, OnFail: GateOnFailEscalateDebate,
	}}
	if err := e.putRaw(flow); err != nil {
		t.Fatal(err)
	}

	approved := true
	if _, err := e.DecideGate(context.Background(), flow.ID, "gate-esc-app",
		&GateDecisionRequest{Approved: &approved, Reason: "ok"}); err != nil {
		t.Fatal(err)
	}
	if handler.count() != 0 {
		t.Fatalf("approval should not invoke escalation handler, calls=%d", handler.count())
	}
}

// The handler receives cloned data: mutating it must not affect engine state.
func TestEscalationHandlerReceivesClonedData(t *testing.T) {
	e := NewInMemoryEngine(nil)
	e.SetGateEscalationHandler(GateEscalationFunc(
		func(ctx context.Context, flow *Flow, stage *Stage, gate *Gate, reason string) {
			flow.Status = FlowStatusAborted
			stage.Status = StageStatusDone
			gate.Passed = true
		},
	))

	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	flow.Stages[0].Gates = []Gate{{
		ID: "gate-clone", Phase: GatePhaseExit, Kind: GateKindHumanApproval, OnFail: GateOnFailEscalateDebate,
	}}
	if err := e.putRaw(flow); err != nil {
		t.Fatal(err)
	}

	rejected := false
	if _, err := e.DecideGate(context.Background(), flow.ID, "gate-clone",
		&GateDecisionRequest{Approved: &rejected, Reason: "test"}); err != nil {
		t.Fatal(err)
	}

	got, err := e.Get(context.Background(), flow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != FlowStatusActive {
		t.Fatalf("handler mutated internal flow status: %s", got.Status)
	}
	if got.Stages[0].Status != StageStatusWaitingGate {
		t.Fatalf("handler mutated internal stage status: %s", got.Stages[0].Status)
	}
	if got.Stages[0].Gates[0].Passed {
		t.Fatal("handler mutated internal gate passed flag")
	}
}

// A nil handler (default) must not panic.
func TestEscalationHandlerNilNoPanic(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	flow.Stages[0].Gates = []Gate{{
		ID: "gate-nil-esc", Phase: GatePhaseExit, Kind: GateKindHumanApproval, OnFail: GateOnFailEscalateDebate,
	}}
	if err := e.putRaw(flow); err != nil {
		t.Fatal(err)
	}
	rejected := false
	if _, err := e.DecideGate(context.Background(), flow.ID, "gate-nil-esc",
		&GateDecisionRequest{Approved: &rejected, Reason: "no handler"}); err != nil {
		t.Fatal(err)
	}
}
