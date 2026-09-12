package floweng

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestExportBuiltinNewProject(t *testing.T) {
	data, err := ExportTemplateJSON(TemplateNewProject)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var ct CustomTemplate
	if err := json.Unmarshal(data, &ct); err != nil {
		t.Fatalf("unmarshal exported JSON: %v", err)
	}
	if ct.ID != TemplateNewProject {
		t.Fatalf("id=%s want %s", ct.ID, TemplateNewProject)
	}
	if len(ct.Stages) != 7 {
		t.Fatalf("stages=%d want 7", len(ct.Stages))
	}
	wantTypes := []StageType{
		StageTypeIdea, StageTypeDesign, StageTypePlanning, StageTypeResearch,
		StageTypeCoding, StageTypeReview, StageTypeSubmit,
	}
	for i, want := range wantTypes {
		if ct.Stages[i].Type != want {
			t.Errorf("stage[%d].Type=%s want %s", i, ct.Stages[i].Type, want)
		}
	}
	if len(ct.Loops) != 3 {
		t.Fatalf("loops=%d want 3", len(ct.Loops))
	}
	// Each stage should carry its exit gate.
	for i, s := range ct.Stages {
		if len(s.Gates) == 0 {
			t.Errorf("stage[%d] (%s) exported with no gates", i, s.Type)
		}
	}
}

func TestExportCustomRoundTrip(t *testing.T) {
	id := uniqTemplateID("export_rt_")
	orig := CustomTemplate{
		ID: id,
		Stages: []CustomStage{
			{Type: StageTypeDesign, Name: "d", Canvas: "c", Gates: []CustomGate{
				{Phase: GatePhaseExit, Kind: GateKindAgentCheck, OnFail: GateOnFailEscalateDebate,
					Config: map[string]string{"threshold": "0.9"}},
			}},
			{Type: StageTypeCoding, Name: "code", Canvas: "coding", Optional: true},
			{Type: StageTypeReview, Name: "rev", Canvas: "review"},
		},
		Loops: []LoopEdge{{From: StageTypeReview, To: StageTypeDesign}},
	}
	if err := RegisterTemplate(orig); err != nil {
		t.Fatal(err)
	}

	data, err := ExportTemplateJSON(id)
	if err != nil {
		t.Fatal(err)
	}

	if err := UnregisterTemplate(id); err != nil {
		t.Fatal(err)
	}
	if _, ok := DescribeTemplate(id); ok {
		t.Fatal("template should be gone after unregister")
	}

	gotID, err := ImportTemplateJSON(data)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	t.Cleanup(func() { _ = UnregisterTemplate(gotID) })
	if gotID != id {
		t.Fatalf("imported id=%s want %s", gotID, id)
	}

	before, _ := DescribeTemplate(id)
	origInfo := describeCustomTemplate(orig)
	if !reflect.DeepEqual(before, origInfo) {
		t.Fatalf("round-tripped TemplateInfo differs:\ngot  %+v\nwant %+v", before, origInfo)
	}
}

func TestImportInvalidJSON(t *testing.T) {
	if _, err := ImportTemplateJSON([]byte(`{not json`)); err == nil {
		t.Fatal("expected error on invalid JSON")
	}
}

func TestImportFailsValidation(t *testing.T) {
	bad := CustomTemplate{
		ID:     uniqTemplateID("import_bad_"),
		Stages: []CustomStage{{Type: StageTypeDesign}, {Type: StageTypeCoding}},
		Loops:  []LoopEdge{{From: StageTypeDesign, To: StageTypeCoding}},
	}
	data, _ := json.Marshal(bad)
	if _, err := ImportTemplateJSON(data); err == nil {
		t.Fatal("expected validation error for downstream loop")
	}
	if containsTemplateID(ListTemplates(), bad.ID) {
		t.Fatal("failed import must not register the template")
	}
}

func TestImportBuiltinIDRejected(t *testing.T) {
	ct := CustomTemplate{
		ID:     TemplateNewProject,
		Stages: []CustomStage{{Type: StageTypeIdea}},
	}
	data, _ := json.Marshal(ct)
	if _, err := ImportTemplateJSON(data); err == nil {
		t.Fatal("import with builtin id should be rejected")
	}
}

func TestExportUnknownID(t *testing.T) {
	if _, err := ExportTemplateJSON("nonexistent"); err == nil {
		t.Fatal("expected error for unknown template id")
	}
}

func TestExportDeterministic(t *testing.T) {
	a, err := ExportTemplateJSON(TemplateNewProject)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ExportTemplateJSON(TemplateNewProject)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two exports of the same template should produce byte-identical JSON")
	}
}

// describeCustomTemplate builds a TemplateInfo from a CustomTemplate for
// comparison (mirrors what DescribeTemplate returns after registration).
func describeCustomTemplate(ct CustomTemplate) *TemplateInfo {
	info := &TemplateInfo{
		ID: ct.ID, Name: ct.Name, Description: ct.Description, Source: "custom",
		Loops: append([]LoopEdge(nil), ct.Loops...),
	}
	for _, s := range ct.Stages {
		brief := StageBrief{
			Type: s.Type, Name: s.Name, Canvas: s.Canvas, AgentID: s.AgentID, Optional: s.Optional,
			Gates: append([]CustomGate(nil), s.Gates...),
		}
		info.Stages = append(info.Stages, brief)
	}
	return info
}
