package floweng

import (
	"encoding/json"
	"fmt"
)

// ExportTemplateJSON resolves a builtin or custom template by id and returns
// its JSON representation using the public CustomTemplate shape. The output is
// indented for readability and deterministic across calls (struct field order is
// fixed; map keys are sorted by encoding/json). Design §7: 模板导出/导入 JSON.
func ExportTemplateJSON(id TemplateID) ([]byte, error) {
	td, ok := getTemplate(id)
	if !ok {
		return nil, fmt.Errorf("template not found: %s", id)
	}
	return json.MarshalIndent(templateDefToCustom(td), "", "  ")
}

// ImportTemplateJSON unmarshals a CustomTemplate from JSON and registers it via
// RegisterTemplate. All validation and builtin-collision rules apply. Returns
// the registered template id on success.
func ImportTemplateJSON(data []byte) (TemplateID, error) {
	var ct CustomTemplate
	if err := json.Unmarshal(data, &ct); err != nil {
		return "", fmt.Errorf("invalid template JSON: %w", err)
	}
	if err := RegisterTemplate(ct); err != nil {
		return "", err
	}
	return ct.ID, nil
}

// templateDefToCustom converts an internal templateDef to the public
// CustomTemplate shape for export (builtins and custom alike).
func templateDefToCustom(td templateDef) CustomTemplate {
	ct := CustomTemplate{
		ID:    td.ID,
		Loops: append([]LoopEdge(nil), td.Loops...),
	}
	for _, sd := range td.Stages {
		cs := CustomStage{
			Type:     sd.Type,
			Name:     sd.Name,
			Canvas:   sd.Canvas,
			Optional: sd.Optional,
		}
		for _, g := range sd.Gates {
			cs.Gates = append(cs.Gates, CustomGate{
				Phase:  g.Phase,
				Kind:   g.Kind,
				OnFail: g.OnFail,
				Config: cloneStringMap(g.Config),
			})
		}
		ct.Stages = append(ct.Stages, cs)
	}
	return ct
}
