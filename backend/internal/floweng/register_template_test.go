package floweng

import (
	"context"
	"sync"
	"testing"
)

// validCustomTemplate returns a minimal well-formed custom template: a
// non-optional first stage and a single upstream loop edge.
func validCustomTemplate(id TemplateID) CustomTemplate {
	return CustomTemplate{
		ID: id,
		Stages: []CustomStage{
			{Type: StageTypeDesign, Name: "设计", Canvas: "design_doc",
				Gates: []CustomGate{{Phase: GatePhaseExit, Kind: GateKindAuto}}},
			{Type: StageTypeCoding, Name: "编码", Canvas: "coding"},
			{Type: StageTypeReview, Name: "Review", Canvas: "review"},
		},
		Loops: []LoopEdge{{From: StageTypeReview, To: StageTypeCoding}},
	}
}

func TestRegisterTemplateCreateAndList(t *testing.T) {
	id := uniqTemplateID("custom_bugfix_")
	if err := RegisterTemplate(validCustomTemplate(id)); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() { _ = UnregisterTemplate(id) })

	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p", TemplateID: id})
	if err != nil {
		t.Fatalf("create from custom template: %v", err)
	}
	if flow.TemplateID != id || len(flow.Stages) != 3 {
		t.Fatalf("unexpected flow: template=%s stages=%d", flow.TemplateID, len(flow.Stages))
	}
	if flow.Stages[0].Status != StageStatusActive || flow.Stages[0].Type != StageTypeDesign {
		t.Fatalf("first stage should be active design, got %s/%s", flow.Stages[0].Type, flow.Stages[0].Status)
	}

	if !containsTemplateID(ListTemplates(), id) {
		t.Fatal("custom id missing from ListTemplates")
	}
	if _, ok := DescribeTemplate(id); !ok {
		t.Fatal("DescribeTemplate should resolve custom id")
	}
	found := false
	for _, info := range ListTemplateInfos() {
		if info.ID == id {
			found = true
			if len(info.Stages) != 3 || len(info.Loops) != 1 {
				t.Fatalf("custom info shape wrong: stages=%d loops=%d", len(info.Stages), len(info.Loops))
			}
		}
	}
	if !found {
		t.Fatal("custom id missing from ListTemplateInfos")
	}
}

// OnFail is carried from the custom template spec into the instantiated gate.
func TestRegisterTemplatePlumbsOnFail(t *testing.T) {
	id := uniqTemplateID("custom_escalate_")
	def := CustomTemplate{
		ID: id,
		Stages: []CustomStage{
			{Type: StageTypeDesign, Name: "d", Canvas: "c",
				Gates: []CustomGate{{Phase: GatePhaseExit, Kind: GateKindHumanApproval, OnFail: GateOnFailEscalateDebate}}},
			{Type: StageTypeCoding, Name: "code", Canvas: "coding"},
		},
	}
	if err := RegisterTemplate(def); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() { _ = UnregisterTemplate(id) })

	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p", TemplateID: id})
	if err != nil {
		t.Fatal(err)
	}
	g := flow.Stages[0].Gates[0]
	if g.OnFail != GateOnFailEscalateDebate {
		t.Fatalf("OnFail not plumbed to instance gate, got %q", g.OnFail)
	}
	if g.ID == "" {
		t.Fatal("instance gate should be assigned an id")
	}
}

func TestRegisterTemplateRejections(t *testing.T) {
	cases := []struct {
		name string
		def  CustomTemplate
	}{
		{"empty id", CustomTemplate{Stages: []CustomStage{{Type: StageTypeDesign}}}},
		{"builtin collision", CustomTemplate{ID: TemplateNewProject, Stages: []CustomStage{{Type: StageTypeIdea}}}},
		{"empty stages", CustomTemplate{ID: uniqTemplateID("empty_")}},
		{"optional first stage", CustomTemplate{ID: uniqTemplateID("optfirst_"),
			Stages: []CustomStage{{Type: StageTypeResearch, Optional: true}}}},
		{"downstream loop", CustomTemplate{ID: uniqTemplateID("downstream_"),
			Stages: []CustomStage{{Type: StageTypeDesign}, {Type: StageTypeCoding}},
			Loops:  []LoopEdge{{From: StageTypeDesign, To: StageTypeCoding}}}},
		{"loop missing stage", CustomTemplate{ID: uniqTemplateID("missing_"),
			Stages: []CustomStage{{Type: StageTypeDesign}, {Type: StageTypeCoding}},
			Loops:  []LoopEdge{{From: StageTypeReview, To: StageTypeDesign}}}},
		{"invalid gate kind", CustomTemplate{ID: uniqTemplateID("badgate_"),
			Stages: []CustomStage{{Type: StageTypeDesign,
				Gates: []CustomGate{{Phase: GatePhaseExit, Kind: GateKind("weird")}}}}}},
		{"invalid gate phase", CustomTemplate{ID: uniqTemplateID("badphase_"),
			Stages: []CustomStage{{Type: StageTypeDesign,
				Gates: []CustomGate{{Phase: GatePhase("whenever"), Kind: GateKindAuto}}}}}},
		{"invalid on_fail", CustomTemplate{ID: uniqTemplateID("badonfail_"),
			Stages: []CustomStage{{Type: StageTypeDesign,
				Gates: []CustomGate{{Phase: GatePhaseExit, Kind: GateKindAuto, OnFail: GateOnFail("nuke")}}}}}},
	}
	for _, tc := range cases {
		if err := RegisterTemplate(tc.def); err == nil {
			// Defensive cleanup should a bad case slip through.
			_ = UnregisterTemplate(tc.def.ID)
			t.Errorf("%s: expected rejection, got nil error", tc.name)
		}
	}
}

func TestUnregisterTemplate(t *testing.T) {
	id := uniqTemplateID("custom_unreg_")
	if err := RegisterTemplate(validCustomTemplate(id)); err != nil {
		t.Fatal(err)
	}
	if err := UnregisterTemplate(id); err != nil {
		t.Fatalf("unregister custom: %v", err)
	}
	if containsTemplateID(ListTemplates(), id) {
		t.Fatal("custom id should be absent after unregister")
	}
	if _, ok := DescribeTemplate(id); ok {
		t.Fatal("DescribeTemplate should fail after unregister")
	}
	if err := UnregisterTemplate(id); err == nil {
		t.Fatal("unregistering an unknown id should error")
	}
	if err := UnregisterTemplate(TemplateNewProject); err == nil {
		t.Fatal("unregistering a built-in template must be rejected")
	}
}

// Built-in templates must satisfy the same validation rules as custom ones.
func TestBuiltinTemplatesValidate(t *testing.T) {
	if len(builtinTemplates) == 0 {
		t.Fatal("expected built-in templates")
	}
	for id, td := range builtinTemplates {
		if err := validateTemplateDef(td); err != nil {
			t.Errorf("built-in template %s failed validation: %v", id, err)
		}
	}
}

// RegisterTemplate + Create + list APIs must be safe under concurrency (-race).
func TestRegisterTemplateConcurrent(t *testing.T) {
	const n = 16
	e := NewInMemoryEngine(nil)
	ids := make([]TemplateID, n)
	for i := range ids {
		ids[i] = uniqTemplateID("conc_")
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_ = UnregisterTemplate(id)
		}
	})

	errCh := make(chan error, 2*n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id TemplateID) {
			defer wg.Done()
			if err := RegisterTemplate(validCustomTemplate(id)); err != nil {
				errCh <- err
				return
			}
			if _, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p", TemplateID: id}); err != nil {
				errCh <- err
			}
		}(ids[i])
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ListTemplates()
			_ = ListTemplateInfos()
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent op failed: %v", err)
	}
}
