package floweng

import (
	"context"
	"testing"
)

// A flow whose first stage has an unpassed human enter gate starts on
// waiting_gate at Create time (design: enter gates run when a stage activates,
// including the initially-active stage 0). Approving resumes to active.
func TestCreateFirstStageEnterGateWaits(t *testing.T) {
	id := uniqTemplateID("create_enter_")
	def := CustomTemplate{
		ID: id,
		Stages: []CustomStage{
			{Type: StageTypeDesign, Name: "d", Canvas: "c", Gates: []CustomGate{
				{Phase: GatePhaseEnter, Kind: GateKindHumanApproval},
			}},
			{Type: StageTypeCoding, Name: "code", Canvas: "coding"},
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
	if flow.Stages[0].Status != StageStatusWaitingGate {
		t.Fatalf("first stage should start waiting_gate, got %s", flow.Stages[0].Status)
	}
	if !flowHasEvent(flow.Events, "gate.waiting") {
		t.Fatalf("expected gate.waiting event at create, got %v", flowEventTypes(flow.Events))
	}

	approved := true
	flow, err = e.DecideGate(context.Background(), flow.ID, flow.Stages[0].Gates[0].ID,
		&GateDecisionRequest{Approved: &approved, Reason: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if flow.Stages[0].Status != StageStatusActive {
		t.Fatalf("first stage should be active after approval, got %s", flow.Stages[0].Status)
	}
}

// A built-in template (no enter gates) still starts with its first stage active.
func TestCreateFirstStageNoEnterGateActive(t *testing.T) {
	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p", TemplateID: TemplateNewProject})
	if err != nil {
		t.Fatal(err)
	}
	if flow.Stages[0].Status != StageStatusActive {
		t.Fatalf("first stage should be active without an enter gate, got %s", flow.Stages[0].Status)
	}
	if flowHasEvent(flow.Events, "gate.waiting") {
		t.Fatal("no enter gate should mean no gate.waiting at create")
	}
}
