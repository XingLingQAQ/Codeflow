package floweng

import (
	"context"
	"testing"
)

// A human enter gate on the next stage parks it on waiting_gate when the prior
// stage advances; the advance itself still succeeds. Approving the gate resumes.
func TestEnterGateParksNextStageWaiting(t *testing.T) {
	id := uniqTemplateID("enter_human_")
	def := CustomTemplate{
		ID: id,
		Stages: []CustomStage{
			{Type: StageTypeDesign, Name: "d", Canvas: "c",
				Gates: []CustomGate{{Phase: GatePhaseExit, Kind: GateKindAuto}}},
			{Type: StageTypeCoding, Name: "code", Canvas: "coding",
				Gates: []CustomGate{{Phase: GatePhaseEnter, Kind: GateKindHumanApproval}}},
		},
	}
	if err := RegisterTemplate(def); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = UnregisterTemplate(id) })

	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p", TemplateID: id})
	if err != nil {
		t.Fatal(err)
	}

	flow, err = e.Advance(context.Background(), flow.ID, &AdvanceRequest{})
	if err != nil {
		t.Fatalf("advancing design should succeed even with a downstream enter gate: %v", err)
	}
	if stageByType(flow, StageTypeDesign).Status != StageStatusDone {
		t.Fatal("design should be done (transition succeeded)")
	}
	coding := stageByType(flow, StageTypeCoding)
	if coding.Status != StageStatusWaitingGate {
		t.Fatalf("coding should park on its enter gate, got %s", coding.Status)
	}
	if !flowHasEvent(flow.Events, "gate.waiting") {
		t.Fatalf("expected gate.waiting event, got %v", flowEventTypes(flow.Events))
	}

	approved := true
	flow, err = e.DecideGate(context.Background(), flow.ID, coding.Gates[0].ID,
		&GateDecisionRequest{Approved: &approved, Reason: "ready"})
	if err != nil {
		t.Fatalf("approving enter gate: %v", err)
	}
	if stageByType(flow, StageTypeCoding).Status != StageStatusActive {
		t.Fatal("coding should be active after enter-gate approval")
	}
}

// An auto enter gate passes immediately and silently (no waiting_gate, no event).
func TestAutoEnterGatePassesSilently(t *testing.T) {
	id := uniqTemplateID("enter_auto_")
	def := CustomTemplate{
		ID: id,
		Stages: []CustomStage{
			{Type: StageTypeDesign, Name: "d", Canvas: "c",
				Gates: []CustomGate{{Phase: GatePhaseExit, Kind: GateKindAuto}}},
			{Type: StageTypeCoding, Name: "code", Canvas: "coding",
				Gates: []CustomGate{{Phase: GatePhaseEnter, Kind: GateKindAuto}}},
		},
	}
	if err := RegisterTemplate(def); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = UnregisterTemplate(id) })

	e := NewInMemoryEngine(nil)
	flow, _ := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p", TemplateID: id})
	flow, err := e.Advance(context.Background(), flow.ID, &AdvanceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	coding := stageByType(flow, StageTypeCoding)
	if coding.Status != StageStatusActive {
		t.Fatalf("auto enter gate should leave coding active, got %s", coding.Status)
	}
	if !coding.Gates[0].Passed {
		t.Fatal("auto enter gate should be marked passed")
	}
	if flowHasEvent(flow.Events, "gate.waiting") {
		t.Fatal("auto enter gate must not emit gate.waiting")
	}
}

// Looping back into a stage re-evaluates its enter gate, re-parking on waiting_gate.
func TestLoopReentryReparksEnterGate(t *testing.T) {
	id := uniqTemplateID("enter_loop_")
	def := CustomTemplate{
		ID: id,
		Stages: []CustomStage{
			{Type: StageTypeDesign, Name: "d", Canvas: "c",
				Gates: []CustomGate{{Phase: GatePhaseExit, Kind: GateKindAuto}}},
			{Type: StageTypeCoding, Name: "code", Canvas: "coding", Gates: []CustomGate{
				{Phase: GatePhaseEnter, Kind: GateKindHumanApproval},
				{Phase: GatePhaseExit, Kind: GateKindAuto},
			}},
			{Type: StageTypeReview, Name: "rev", Canvas: "review",
				Gates: []CustomGate{{Phase: GatePhaseExit, Kind: GateKindAuto}}},
		},
		Loops: []LoopEdge{{From: StageTypeReview, To: StageTypeCoding}},
	}
	if err := RegisterTemplate(def); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = UnregisterTemplate(id) })

	e := NewInMemoryEngine(nil)
	flow, _ := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p", TemplateID: id})

	// design -> coding parks on enter gate; approve to enter.
	flow, err := e.Advance(context.Background(), flow.ID, &AdvanceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	coding := stageByType(flow, StageTypeCoding)
	if coding.Status != StageStatusWaitingGate {
		t.Fatalf("coding should wait on enter gate, got %s", coding.Status)
	}
	approved := true
	flow, err = e.DecideGate(context.Background(), flow.ID, coding.Gates[0].ID, &GateDecisionRequest{Approved: &approved})
	if err != nil {
		t.Fatal(err)
	}
	// coding -> review.
	flow, err = e.Advance(context.Background(), flow.ID, &AdvanceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	review := stageByType(flow, StageTypeReview)
	coding = stageByType(flow, StageTypeCoding)
	if review.Status != StageStatusActive {
		t.Fatalf("review should be active, got %s", review.Status)
	}

	// Loop review -> coding; enter gate re-parks coding.
	flow, err = e.Loop(context.Background(), flow.ID, &LoopRequest{
		FromStageID: review.ID, ToStageID: coding.ID, Reason: "revisit",
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if stageByType(flow, StageTypeCoding).Status != StageStatusWaitingGate {
		t.Fatalf("coding should re-park on its enter gate after loop, got %s",
			stageByType(flow, StageTypeCoding).Status)
	}
	if !flowHasEvent(flow.Events, "flow.loop") {
		t.Fatal("expected flow.loop event")
	}
}

// Two human enter gates on one stage require both approvals before the stage
// becomes active (P2-3 fix: DecideGate re-runs applyEnterGates after approval).
func TestDualEnterGateRequiresBothApprovals(t *testing.T) {
	id := uniqTemplateID("dual_enter_")
	def := CustomTemplate{
		ID: id,
		Stages: []CustomStage{
			{Type: StageTypeDesign, Name: "d", Canvas: "c",
				Gates: []CustomGate{{Phase: GatePhaseExit, Kind: GateKindAuto}}},
			{Type: StageTypeCoding, Name: "code", Canvas: "coding", Gates: []CustomGate{
				{Phase: GatePhaseEnter, Kind: GateKindHumanApproval},
				{Phase: GatePhaseEnter, Kind: GateKindHumanApproval},
			}},
		},
	}
	if err := RegisterTemplate(def); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = UnregisterTemplate(id) })

	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p", TemplateID: id})
	if err != nil {
		t.Fatal(err)
	}
	// Advance design -> coding parks on the first enter gate.
	flow, err = e.Advance(context.Background(), flow.ID, &AdvanceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	coding := stageByType(flow, StageTypeCoding)
	if coding.Status != StageStatusWaitingGate {
		t.Fatalf("coding should be waiting_gate, got %s", coding.Status)
	}

	gate0 := coding.Gates[0]
	gate1 := coding.Gates[1]

	// Approve the first enter gate: stage must remain waiting_gate (second still unpassed).
	approved := true
	flow, err = e.DecideGate(context.Background(), flow.ID, gate0.ID,
		&GateDecisionRequest{Approved: &approved, Reason: "first ok"})
	if err != nil {
		t.Fatal(err)
	}
	coding = stageByType(flow, StageTypeCoding)
	if coding.Status != StageStatusWaitingGate {
		t.Fatalf("coding should still be waiting_gate after first approval, got %s", coding.Status)
	}
	if !coding.Gates[0].Passed {
		t.Fatal("first enter gate should be marked passed")
	}
	if coding.Gates[1].Passed {
		t.Fatal("second enter gate should still be unpassed")
	}

	// Approve the second enter gate: stage becomes active.
	flow, err = e.DecideGate(context.Background(), flow.ID, gate1.ID,
		&GateDecisionRequest{Approved: &approved, Reason: "second ok"})
	if err != nil {
		t.Fatal(err)
	}
	coding = stageByType(flow, StageTypeCoding)
	if coding.Status != StageStatusActive {
		t.Fatalf("coding should be active after both approvals, got %s", coding.Status)
	}
}
