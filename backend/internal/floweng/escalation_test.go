package floweng

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// captureEscalationHandler records all escalation calls for assertions.
type captureEscalationHandler struct {
	mu    sync.Mutex
	calls []escalationCall
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

func newEscalationTestFlow(t *testing.T, e *InMemoryEngine, projectID, gateID string) *Flow {
	t.Helper()
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: projectID})
	if err != nil {
		t.Fatal(err)
	}
	flow.Stages[0].Gates = []Gate{{
		ID: gateID, Phase: GatePhaseExit, Kind: GateKindHumanApproval, OnFail: GateOnFailEscalateDebate,
		Config: map[string]string{"scope": "test"},
	}}
	if err := e.putRaw(flow); err != nil {
		t.Fatal(err)
	}
	return flow
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

// The flow, stage, and gate callback values are mutually detached as well as
// detached from engine state.
func TestEscalationHandlerCloneArgumentsAreIndependent(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow := newEscalationTestFlow(t, e, "p-independent", "gate-independent")

	var callbackErr error
	e.SetGateEscalationHandler(GateEscalationFunc(
		func(ctx context.Context, callbackFlow *Flow, stage *Stage, gate *Gate, reason string) {
			callbackFlow.Stages[0].Status = StageStatusDone
			callbackFlow.Stages[0].Gates[0].Passed = true
			callbackFlow.Stages[0].Gates[0].Config["scope"] = "flow"
			if stage.Status != StageStatusWaitingGate || stage.Gates[0].Passed || stage.Gates[0].Config["scope"] != "test" {
				callbackErr = fmt.Errorf("mutating flow callback argument changed stage argument")
				return
			}
			stage.Gates[0].Passed = true
			stage.Gates[0].Config["scope"] = "stage"
			if gate.Passed || gate.Config["scope"] != "test" {
				callbackErr = fmt.Errorf("mutating stage callback argument changed gate argument")
			}
		},
	))

	rejected := false
	if _, err := e.DecideGate(context.Background(), flow.ID, "gate-independent",
		&GateDecisionRequest{Approved: &rejected, Reason: "clone"}); err != nil {
		t.Fatal(err)
	}
	if callbackErr != nil {
		t.Fatal(callbackErr)
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

// A handler may call back into the same engine. This deadlocked when callbacks
// ran under e.mu.
func TestEscalationHandlerMayReenterEngine(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow := newEscalationTestFlow(t, e, "p-reentrant", "gate-reentrant")

	callbackDone := make(chan error, 1)
	e.SetGateEscalationHandler(GateEscalationFunc(
		func(ctx context.Context, callbackFlow *Flow, stage *Stage, gate *Gate, reason string) {
			got, err := e.Get(ctx, callbackFlow.ID)
			if err == nil && got.Stages[0].Status != StageStatusWaitingGate {
				err = fmt.Errorf("persisted stage status=%s want %s", got.Stages[0].Status, StageStatusWaitingGate)
			}
			if err == nil {
				last := len(got.Events) - 1
				if last < 1 || got.Events[last-1].Type != "gate.rejected" || got.Events[last].Type != "gate.escalate_debate" {
					err = fmt.Errorf("persisted escalation event order=%v", flowEventTypes(got.Events))
				}
			}
			callbackDone <- err
		},
	))

	rejected := false
	decisionDone := make(chan error, 1)
	go func() {
		_, err := e.DecideGate(context.Background(), flow.ID, "gate-reentrant",
			&GateDecisionRequest{Approved: &rejected, Reason: "reenter"})
		decisionDone <- err
	}()

	select {
	case err := <-callbackDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("reentrant escalation callback deadlocked")
	}
	select {
	case err := <-decisionDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("DecideGate did not return after reentrant callback")
	}
}

// A slow callback still delays its own DecideGate call (preserving synchronous
// public behavior), but it must not retain e.mu and stall unrelated flows.
func TestSlowEscalationHandlerDoesNotBlockUnrelatedFlow(t *testing.T) {
	e := NewInMemoryEngine(nil)
	escalated := newEscalationTestFlow(t, e, "p-slow", "gate-slow")
	unrelated, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p-unrelated"})
	if err != nil {
		t.Fatal(err)
	}

	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseCallback) }) }
	t.Cleanup(release)
	e.SetGateEscalationHandler(GateEscalationFunc(
		func(ctx context.Context, flow *Flow, stage *Stage, gate *Gate, reason string) {
			close(callbackStarted)
			<-releaseCallback
		},
	))

	rejected := false
	decisionDone := make(chan error, 1)
	go func() {
		_, err := e.DecideGate(context.Background(), escalated.ID, "gate-slow",
			&GateDecisionRequest{Approved: &rejected, Reason: "slow"})
		decisionDone <- err
	}()
	select {
	case <-callbackStarted:
	case <-time.After(time.Second):
		t.Fatal("slow escalation callback did not start")
	}

	getDone := make(chan error, 1)
	go func() {
		_, err := e.Get(context.Background(), unrelated.ID)
		getDone <- err
	}()
	select {
	case err := <-getDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("slow escalation callback blocked unrelated flow operation")
	}

	select {
	case err := <-decisionDone:
		t.Fatalf("DecideGate returned before synchronous callback finished: %v", err)
	default:
	}
	release()
	select {
	case err := <-decisionDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("DecideGate did not return after slow callback completed")
	}
}

// Concurrent handler replacement and escalation exercises the post-unlock
// dispatch capture under the race detector.
func TestEscalationHandlerConcurrentSetAndDecide(t *testing.T) {
	e := NewInMemoryEngine(nil)
	const n = 32
	flows := make([]*Flow, n)
	for i := range flows {
		flows[i] = newEscalationTestFlow(t, e, "p-race", fmt.Sprintf("gate-race-%d", i))
	}

	var wg sync.WaitGroup
	for i := range flows {
		i := i
		wg.Add(2)
		go func() {
			defer wg.Done()
			e.SetGateEscalationHandler(GateEscalationFunc(
				func(context.Context, *Flow, *Stage, *Gate, string) {},
			))
		}()
		go func() {
			defer wg.Done()
			rejected := false
			_, _ = e.DecideGate(context.Background(), flows[i].ID, fmt.Sprintf("gate-race-%d", i),
				&GateDecisionRequest{Approved: &rejected, Reason: "race"})
		}()
	}
	wg.Wait()
}
