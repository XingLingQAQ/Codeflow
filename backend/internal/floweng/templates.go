package floweng

import (
	"fmt"
	"sort"
	"sync"
)

// stageDef is a template stage before IDs are assigned.
type stageDef struct {
	Type     StageType
	Name     string
	Canvas   string
	Optional bool
	Gates    []Gate
}

// templateDef is a built-in flow template.
type templateDef struct {
	ID     TemplateID
	Stages []stageDef
	Loops  []LoopEdge
}

func autoExitGate() []Gate {
	return []Gate{{
		ID:     "", // filled at instance time
		Phase:  GatePhaseExit,
		Kind:   GateKindAuto,
		Passed: false,
	}}
}

var builtinTemplates = map[TemplateID]templateDef{
	TemplateNewProject: {
		ID: TemplateNewProject,
		Stages: []stageDef{
			{Type: StageTypeIdea, Name: "想法提出", Canvas: "intent", Gates: autoExitGate()},
			{Type: StageTypeDesign, Name: "设计", Canvas: "design_doc", Gates: autoExitGate()},
			{Type: StageTypePlanning, Name: "规划", Canvas: "planning_board", Gates: autoExitGate()},
			{Type: StageTypeResearch, Name: "调研", Canvas: "deep_search", Optional: true, Gates: autoExitGate()},
			{Type: StageTypeCoding, Name: "编码", Canvas: "coding", Gates: autoExitGate()},
			{Type: StageTypeReview, Name: "Review/Debug", Canvas: "review", Gates: autoExitGate()},
			{Type: StageTypeSubmit, Name: "提交", Canvas: "submit", Gates: autoExitGate()},
		},
		Loops: []LoopEdge{
			{From: StageTypeReview, To: StageTypeCoding},
			{From: StageTypeReview, To: StageTypeDesign},
			{From: StageTypeCoding, To: StageTypePlanning},
		},
	},
	TemplateImport: {
		ID: TemplateImport,
		Stages: []stageDef{
			{Type: StageTypeImport, Name: "导入", Canvas: "import_pipeline", Gates: autoExitGate()},
			{Type: StageTypeComprehend, Name: "理解", Canvas: "comprehension", Gates: autoExitGate()},
			{Type: StageTypePlanning, Name: "规划", Canvas: "planning_board", Gates: autoExitGate()},
			{Type: StageTypeResearch, Name: "调研", Canvas: "deep_search", Optional: true, Gates: autoExitGate()},
			{Type: StageTypeCoding, Name: "编码", Canvas: "coding", Gates: autoExitGate()},
			{Type: StageTypeReview, Name: "Review/Debug", Canvas: "review", Gates: autoExitGate()},
			{Type: StageTypeSubmit, Name: "提交", Canvas: "submit", Gates: autoExitGate()},
		},
		Loops: []LoopEdge{
			{From: StageTypeReview, To: StageTypeCoding},
			{From: StageTypeReview, To: StageTypePlanning},
			{From: StageTypeCoding, To: StageTypePlanning},
		},
	},
}

// ListTemplates returns built-in template IDs first, then registered custom IDs
// (sorted for deterministic ordering).
func ListTemplates() []TemplateID {
	out := []TemplateID{TemplateNewProject, TemplateImport}
	customMu.RLock()
	ids := make([]TemplateID, 0, len(customTemplates))
	for id := range customTemplates {
		ids = append(ids, id)
	}
	customMu.RUnlock()
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return append(out, ids...)
}

// TemplateInfo is a public view of a built-in template.
type TemplateInfo struct {
	ID     TemplateID   `json:"id"`
	Stages []StageBrief `json:"stages"`
	Loops  []LoopEdge   `json:"loops"`
}

// StageBrief describes a template stage without instance IDs.
type StageBrief struct {
	Type     StageType `json:"type"`
	Name     string    `json:"name"`
	Canvas   string    `json:"canvas"`
	Optional bool      `json:"optional"`
}

// DescribeTemplate returns a public template description.
func DescribeTemplate(id TemplateID) (*TemplateInfo, bool) {
	t, ok := getTemplate(id)
	if !ok {
		return nil, false
	}
	info := &TemplateInfo{ID: t.ID, Loops: append([]LoopEdge(nil), t.Loops...)}
	for _, s := range t.Stages {
		info.Stages = append(info.Stages, StageBrief{
			Type: s.Type, Name: s.Name, Canvas: s.Canvas, Optional: s.Optional,
		})
	}
	return info, true
}

// ListTemplateInfos returns all built-in template descriptions.
func ListTemplateInfos() []TemplateInfo {
	ids := ListTemplates()
	out := make([]TemplateInfo, 0, len(ids))
	for _, id := range ids {
		if info, ok := DescribeTemplate(id); ok {
			out = append(out, *info)
		}
	}
	return out
}

func getTemplate(id TemplateID) (templateDef, bool) {
	if id == "" {
		id = TemplateNewProject
	}
	if t, ok := builtinTemplates[id]; ok {
		return t, true
	}
	customMu.RLock()
	defer customMu.RUnlock()
	t, ok := customTemplates[id]
	return t, ok
}

// customTemplates holds plugin-registered templates, keyed by ID. Built-in IDs
// take precedence and can never be overwritten. Stored templateDefs are treated
// as immutable after publication: RegisterTemplate always assigns a freshly
// built value, so getTemplate may return an aliased snapshot safely.
var (
	customMu        sync.RWMutex
	customTemplates = map[TemplateID]templateDef{}
)

// CustomGate is the public gate spec accepted by RegisterTemplate.
type CustomGate struct {
	Phase  GatePhase         `json:"phase"`
	Kind   GateKind          `json:"kind"`
	OnFail GateOnFail        `json:"on_fail,omitempty"`
	Config map[string]string `json:"config,omitempty"`
}

// CustomStage is the public stage spec accepted by RegisterTemplate.
type CustomStage struct {
	Type     StageType    `json:"type"`
	Name     string       `json:"name"`
	Canvas   string       `json:"canvas"`
	Optional bool         `json:"optional"`
	Gates    []CustomGate `json:"gates,omitempty"`
}

// CustomTemplate is a plugin-provided flow template (design §3.3, §7).
type CustomTemplate struct {
	ID     TemplateID    `json:"id"`
	Stages []CustomStage `json:"stages"`
	Loops  []LoopEdge    `json:"loops,omitempty"`
}

func (c CustomTemplate) toTemplateDef() templateDef {
	td := templateDef{ID: c.ID, Loops: append([]LoopEdge(nil), c.Loops...)}
	for _, s := range c.Stages {
		sd := stageDef{Type: s.Type, Name: s.Name, Canvas: s.Canvas, Optional: s.Optional}
		for _, g := range s.Gates {
			sd.Gates = append(sd.Gates, Gate{Phase: g.Phase, Kind: g.Kind, OnFail: g.OnFail, Config: cloneStringMap(g.Config)})
		}
		td.Stages = append(td.Stages, sd)
	}
	return td
}

// RegisterTemplate registers a custom flow template for use with Create and the
// Describe/List APIs. It rejects an empty ID or a built-in ID collision, then
// validates structure via validateTemplateDef. Registering an existing custom ID
// replaces it (update semantics). Safe for concurrent use.
func RegisterTemplate(def CustomTemplate) error {
	if def.ID == "" {
		return fmt.Errorf("template id is required")
	}
	if _, isBuiltin := builtinTemplates[def.ID]; isBuiltin {
		return fmt.Errorf("cannot override built-in template %s", def.ID)
	}
	td := def.toTemplateDef()
	if err := validateTemplateDef(td); err != nil {
		return err
	}
	customMu.Lock()
	defer customMu.Unlock()
	customTemplates[def.ID] = td
	return nil
}

// UnregisterTemplate removes a custom template. Built-in templates cannot be
// unregistered. Returns an error if the id is unknown among custom templates.
func UnregisterTemplate(id TemplateID) error {
	if _, isBuiltin := builtinTemplates[id]; isBuiltin {
		return fmt.Errorf("cannot unregister built-in template %s", id)
	}
	customMu.Lock()
	defer customMu.Unlock()
	if _, ok := customTemplates[id]; !ok {
		return fmt.Errorf("template not found: %s", id)
	}
	delete(customTemplates, id)
	return nil
}

// validateTemplateDef enforces the editor/plugin constraints from design §7:
// at least one stage, a non-optional first stage (a flow must start somewhere),
// every loop edge referencing existing stages and pointing upstream, and gates
// carrying valid phase/kind/on_fail values.
func validateTemplateDef(t templateDef) error {
	if len(t.Stages) == 0 {
		return fmt.Errorf("template %s: must have at least one stage", t.ID)
	}
	if t.Stages[0].Optional {
		return fmt.Errorf("template %s: first stage %s must not be optional", t.ID, t.Stages[0].Type)
	}
	for si := range t.Stages {
		for gi := range t.Stages[si].Gates {
			if err := validateGate(t.Stages[si].Gates[gi]); err != nil {
				return fmt.Errorf("template %s stage %s: %w", t.ID, t.Stages[si].Type, err)
			}
		}
	}
	for _, edge := range t.Loops {
		fromIdx := stageTypeIndex(t.Stages, edge.From)
		toIdx := stageTypeIndex(t.Stages, edge.To)
		if fromIdx < 0 {
			return fmt.Errorf("template %s: loop edge from unknown stage %s", t.ID, edge.From)
		}
		if toIdx < 0 {
			return fmt.Errorf("template %s: loop edge to unknown stage %s", t.ID, edge.To)
		}
		if fromIdx <= toIdx {
			return fmt.Errorf("template %s: loop edge %s→%s must point upstream (to an earlier stage)", t.ID, edge.From, edge.To)
		}
	}
	return nil
}

func validateGate(g Gate) error {
	switch g.Phase {
	case GatePhaseEnter, GatePhaseExit:
	default:
		return fmt.Errorf("invalid gate phase %q", g.Phase)
	}
	switch g.Kind {
	case GateKindAuto, GateKindHumanApproval, GateKindAgentCheck:
	default:
		return fmt.Errorf("invalid gate kind %q", g.Kind)
	}
	switch g.OnFail {
	case "", GateOnFailBlock, GateOnFailEscalateDebate:
	default:
		return fmt.Errorf("invalid gate on_fail %q", g.OnFail)
	}
	return nil
}

// stageTypeIndex returns the first index whose stage has the given type, or -1.
func stageTypeIndex(stages []stageDef, typ StageType) int {
	for i := range stages {
		if stages[i].Type == typ {
			return i
		}
	}
	return -1
}
