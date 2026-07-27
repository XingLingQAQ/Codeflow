package floweng

import (
	"context"
	"testing"
)

// Gate.Config flows template -> instance and is deep-copied, so mutating a
// returned flow's gate config cannot corrupt engine-internal state.
func TestGateConfigPlumbedAndDeepCopied(t *testing.T) {
	id := uniqTemplateID("cfg_")
	def := CustomTemplate{
		ID: id,
		Stages: []CustomStage{
			{Type: StageTypeDesign, Name: "d", Canvas: "c", Gates: []CustomGate{
				{Phase: GatePhaseExit, Kind: GateKindAgentCheck,
					Config: map[string]string{"threshold": "0.8", "approver": "lead"}},
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
	cfg := flow.Stages[0].Gates[0].Config
	if cfg["threshold"] != "0.8" || cfg["approver"] != "lead" {
		t.Fatalf("gate config not plumbed to instance: %+v", cfg)
	}

	// Mutate the returned flow's config; engine-internal state must be isolated.
	cfg["threshold"] = "MUTATED"
	cfg["injected"] = "x"

	got, err := e.Get(context.Background(), flow.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotCfg := got.Stages[0].Gates[0].Config
	if gotCfg["threshold"] != "0.8" {
		t.Fatalf("internal gate config mutated via returned flow: %+v", gotCfg)
	}
	if _, ok := gotCfg["injected"]; ok {
		t.Fatalf("injected key leaked into internal state: %+v", gotCfg)
	}
}

// A caller mutating its source Config map after RegisterTemplate must not affect
// templates already stored or flows created from them (registry deep-copies).
func TestGateConfigTemplateNotAliased(t *testing.T) {
	id := uniqTemplateID("cfg_alias_")
	srcCfg := map[string]string{"threshold": "0.5"}
	def := CustomTemplate{
		ID: id,
		Stages: []CustomStage{
			{Type: StageTypeDesign, Name: "d", Canvas: "c", Gates: []CustomGate{
				{Phase: GatePhaseExit, Kind: GateKindAgentCheck, Config: srcCfg},
			}},
			{Type: StageTypeCoding, Name: "code", Canvas: "coding"},
		},
	}
	if err := RegisterTemplate(def); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = UnregisterTemplate(id) })

	// Post-registration mutation of the caller's map must not leak in.
	srcCfg["threshold"] = "9.9"
	srcCfg["sneaky"] = "1"

	e := NewInMemoryEngine(nil)
	flow, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p", TemplateID: id})
	if err != nil {
		t.Fatal(err)
	}
	cfg := flow.Stages[0].Gates[0].Config
	if cfg["threshold"] != "0.5" {
		t.Fatalf("template config aliased to caller's map: %+v", cfg)
	}
	if _, ok := cfg["sneaky"]; ok {
		t.Fatalf("caller's post-register mutation leaked into template: %+v", cfg)
	}
}
