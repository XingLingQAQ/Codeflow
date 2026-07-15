package floweng

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
			{From: StageTypeReview, To: StageTypeDesign},
			{From: StageTypeCoding, To: StageTypePlanning},
		},
	},
}

// ListTemplates returns built-in template IDs.
func ListTemplates() []TemplateID {
	return []TemplateID{TemplateNewProject, TemplateImport}
}

func getTemplate(id TemplateID) (templateDef, bool) {
	if id == "" {
		id = TemplateNewProject
	}
	t, ok := builtinTemplates[id]
	return t, ok
}
