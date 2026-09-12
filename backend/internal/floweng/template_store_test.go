package floweng

import (
	"context"
	"testing"
)

type recordingTemplateStore struct {
	FlowStore
	defs map[TemplateID]CustomTemplate
}

func newRecordingTemplateStore() *recordingTemplateStore {
	return &recordingTemplateStore{FlowStore: newMemoryStore(), defs: map[TemplateID]CustomTemplate{}}
}

func (s *recordingTemplateStore) PutTemplate(def CustomTemplate) error {
	s.defs[def.ID] = def
	return nil
}

func (s *recordingTemplateStore) ListTemplateDefinitions() ([]CustomTemplate, error) {
	out := make([]CustomTemplate, 0, len(s.defs))
	for _, def := range s.defs {
		out = append(out, def)
	}
	return out, nil
}

func (s *recordingTemplateStore) DeleteTemplate(id TemplateID) error {
	delete(s.defs, id)
	return nil
}

func TestSaveTemplatePersistsAndInstantiatesAgentBinding(t *testing.T) {
	store := newRecordingTemplateStore()
	previous := GetEngine()
	SetEngine(NewEngineWithStore(store, nil))
	id := uniqTemplateID("stored_contract_")
	t.Cleanup(func() {
		_ = DeleteTemplate(id)
		SetEngine(previous)
	})

	def := CustomTemplate{
		ID: id, Name: "Stored contract",
		Stages: []CustomStage{
			{
				Type: StageTypePlanning, Name: "规划", Canvas: "planning_board",
				AgentID: "planner-agent",
				Gates:   []CustomGate{{Phase: GatePhaseExit, Kind: GateKindHumanApproval}},
			},
		},
	}
	if err := SaveTemplate(def); err != nil {
		t.Fatal(err)
	}
	if got, ok := store.defs[id]; !ok || got.Stages[0].AgentID != "planner-agent" {
		t.Fatalf("template was not persisted: %#v", got)
	}

	flow, err := GetEngine().Create(context.Background(), &CreateFlowRequest{ProjectID: "p", TemplateID: id})
	if err != nil {
		t.Fatal(err)
	}
	if flow.Stages[0].AgentID != "planner-agent" || len(flow.Stages[0].Gates) != 1 {
		t.Fatalf("instance contract mismatch: %#v", flow.Stages[0])
	}

	if err := DeleteTemplate(id); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.defs[id]; ok {
		t.Fatal("deleted template remains in persistent store")
	}
}
