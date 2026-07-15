package floweng

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// InMemoryEngine is an in-memory Flow engine (PR-8 baseline).
type InMemoryEngine struct {
	mu        sync.RWMutex
	flows     map[string]*Flow
	snapshots SnapshotCreator // optional
}

// NewInMemoryEngine creates an engine. snapshots may be nil (no auto snapshot).
func NewInMemoryEngine(snapshots SnapshotCreator) *InMemoryEngine {
	return &InMemoryEngine{
		flows:     make(map[string]*Flow),
		snapshots: snapshots,
	}
}

// SetSnapshotCreator attaches or replaces the stage-completion snapshot hook.
func (e *InMemoryEngine) SetSnapshotCreator(s SnapshotCreator) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.snapshots = s
}

// Create instantiates a Flow from a built-in template.
func (e *InMemoryEngine) Create(ctx context.Context, req *CreateFlowRequest) (*Flow, error) {
	if req == nil || req.ProjectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	tmpl, ok := getTemplate(req.TemplateID)
	if !ok {
		return nil, fmt.Errorf("unknown template_id %q", req.TemplateID)
	}

	now := time.Now().UTC()
	flow := &Flow{
		ID:         uuid.New().String(),
		ProjectID:  req.ProjectID,
		TemplateID: tmpl.ID,
		Status:     FlowStatusActive,
		Loops:      append([]LoopEdge(nil), tmpl.Loops...),
		Artifacts:  make([]Artifact, 0),
		Events:     make([]FlowEvent, 0),
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	stages := make([]Stage, 0, len(tmpl.Stages))
	for i, def := range tmpl.Stages {
		status := StageStatusPending
		if i == 0 {
			status = StageStatusActive
		}
		gates := make([]Gate, len(def.Gates))
		for gi, g := range def.Gates {
			gates[gi] = Gate{
				ID:     uuid.New().String(),
				Phase:  g.Phase,
				Kind:   g.Kind,
				Passed: false,
			}
		}
		stages = append(stages, Stage{
			ID:       uuid.New().String(),
			Type:     def.Type,
			Name:     def.Name,
			Canvas:   def.Canvas,
			Status:   status,
			Optional: def.Optional,
			Gates:    gates,
			Order:    i,
		})
	}
	flow.Stages = stages
	e.appendEvent(flow, "flow.created", "", fmt.Sprintf("created template=%s project=%s", tmpl.ID, req.ProjectID))

	e.mu.Lock()
	e.flows[flow.ID] = cloneFlow(flow)
	e.mu.Unlock()

	return cloneFlow(flow), nil
}

// Get returns a flow by id.
func (e *InMemoryEngine) Get(ctx context.Context, id string) (*Flow, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	f, ok := e.flows[id]
	if !ok {
		return nil, fmt.Errorf("flow not found: %s", id)
	}
	return cloneFlow(f), nil
}

// List returns flows, optionally filtered by projectID (empty = all).
func (e *InMemoryEngine) List(ctx context.Context, projectID string) ([]*Flow, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*Flow, 0)
	for _, f := range e.flows {
		if projectID == "" || f.ProjectID == projectID {
			out = append(out, cloneFlow(f))
		}
	}
	return out, nil
}

// Advance completes the active stage and activates the next non-skipped pending stage.
func (e *InMemoryEngine) Advance(ctx context.Context, flowID string, req *AdvanceRequest) (*Flow, error) {
	if req == nil {
		req = &AdvanceRequest{}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	flow, err := e.mustGetLocked(flowID)
	if err != nil {
		return nil, err
	}
	if flow.Status != FlowStatusActive {
		return nil, fmt.Errorf("flow is not active: %s", flow.Status)
	}

	activeIdx := activeStageIndex(flow)
	if activeIdx < 0 {
		return nil, fmt.Errorf("no active stage")
	}
	stage := &flow.Stages[activeIdx]

	if err := passExitGates(stage, req.Force); err != nil {
		return nil, err
	}

	// Optional snapshot on completion
	if e.snapshots != nil {
		sid, err := e.snapshots.CreateStageSnapshot(ctx, flow, stage, req.SessionID)
		if err != nil {
			return nil, fmt.Errorf("stage snapshot: %w", err)
		}
		stage.SnapshotID = sid
	}

	stage.Status = StageStatusDone
	e.appendEvent(flow, "stage.done", stage.ID, fmt.Sprintf("completed stage type=%s", stage.Type))

	// Activate next pending (skip already skipped)
	next := -1
	for i := activeIdx + 1; i < len(flow.Stages); i++ {
		if flow.Stages[i].Status == StageStatusPending {
			next = i
			break
		}
	}
	if next < 0 {
		flow.Status = FlowStatusCompleted
		e.appendEvent(flow, "flow.completed", "", "all stages finished")
	} else {
		flow.Stages[next].Status = StageStatusActive
		e.appendEvent(flow, "stage.active", flow.Stages[next].ID, fmt.Sprintf("activated type=%s", flow.Stages[next].Type))
	}

	flow.UpdatedAt = time.Now().UTC()
	e.flows[flow.ID] = cloneFlow(flow)
	return cloneFlow(flow), nil
}

// Skip skips an optional stage that is pending or active.
func (e *InMemoryEngine) Skip(ctx context.Context, flowID string, req *SkipRequest) (*Flow, error) {
	if req == nil || req.StageID == "" {
		return nil, fmt.Errorf("stage_id is required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	flow, err := e.mustGetLocked(flowID)
	if err != nil {
		return nil, err
	}
	if flow.Status != FlowStatusActive {
		return nil, fmt.Errorf("flow is not active: %s", flow.Status)
	}

	idx := stageIndexByID(flow, req.StageID)
	if idx < 0 {
		return nil, fmt.Errorf("stage not found: %s", req.StageID)
	}
	stage := &flow.Stages[idx]
	if !stage.Optional {
		return nil, fmt.Errorf("stage %s is not optional", stage.Type)
	}
	if stage.Status != StageStatusPending && stage.Status != StageStatusActive {
		return nil, fmt.Errorf("stage cannot be skipped from status %s", stage.Status)
	}

	wasActive := stage.Status == StageStatusActive
	stage.Status = StageStatusSkipped
	e.appendEvent(flow, "stage.skipped", stage.ID, fmt.Sprintf("skipped optional type=%s", stage.Type))

	if wasActive {
		// Activate next pending
		activated := false
		for i := idx + 1; i < len(flow.Stages); i++ {
			if flow.Stages[i].Status == StageStatusPending {
				flow.Stages[i].Status = StageStatusActive
				e.appendEvent(flow, "stage.active", flow.Stages[i].ID, fmt.Sprintf("activated type=%s", flow.Stages[i].Type))
				activated = true
				break
			}
		}
		if !activated {
			flow.Status = FlowStatusCompleted
			e.appendEvent(flow, "flow.completed", "", "all stages finished after skip")
		}
	}

	flow.UpdatedAt = time.Now().UTC()
	e.flows[flow.ID] = cloneFlow(flow)
	return cloneFlow(flow), nil
}

// Loop jumps back along an allowed template loop edge.
func (e *InMemoryEngine) Loop(ctx context.Context, flowID string, req *LoopRequest) (*Flow, error) {
	if req == nil || req.FromStageID == "" || req.ToStageID == "" {
		return nil, fmt.Errorf("from_stage_id and to_stage_id are required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	flow, err := e.mustGetLocked(flowID)
	if err != nil {
		return nil, err
	}
	if flow.Status != FlowStatusActive {
		return nil, fmt.Errorf("flow is not active: %s", flow.Status)
	}

	fromIdx := stageIndexByID(flow, req.FromStageID)
	toIdx := stageIndexByID(flow, req.ToStageID)
	if fromIdx < 0 || toIdx < 0 {
		return nil, fmt.Errorf("from/to stage not found")
	}
	if toIdx >= fromIdx {
		return nil, fmt.Errorf("loop target must be an earlier stage")
	}

	from := flow.Stages[fromIdx]
	to := flow.Stages[toIdx]
	if !loopAllowed(flow.Loops, from.Type, to.Type) {
		return nil, fmt.Errorf("loop %s → %s is not allowed", from.Type, to.Type)
	}
	if from.Status != StageStatusActive && from.Status != StageStatusDone {
		return nil, fmt.Errorf("loop from stage must be active or done")
	}

	// Mark artifacts on the reopened target and all downstream stages as stale.
	for i := range flow.Artifacts {
		artStageIdx := stageIndexByID(flow, flow.Artifacts[i].StageID)
		if artStageIdx >= toIdx {
			flow.Artifacts[i].Status = ArtifactStatusStale
		}
	}
	// Stages after target that were done/active/waiting become pending (re-work).
	for i := toIdx + 1; i < len(flow.Stages); i++ {
		st := flow.Stages[i].Status
		if st == StageStatusDone || st == StageStatusActive || st == StageStatusWaitingGate {
			flow.Stages[i].Status = StageStatusPending
			flow.Stages[i].SnapshotID = ""
			for gi := range flow.Stages[i].Gates {
				flow.Stages[i].Gates[gi].Passed = false
			}
		}
	}
	// Re-open target stage.
	flow.Stages[toIdx].Status = StageStatusActive
	flow.Stages[toIdx].SnapshotID = ""
	for gi := range flow.Stages[toIdx].Gates {
		flow.Stages[toIdx].Gates[gi].Passed = false
	}

	reason := req.Reason
	if reason == "" {
		reason = "loop"
	}
	e.appendEvent(flow, "flow.loop", to.ID, fmt.Sprintf("loop %s→%s reason=%s", from.Type, to.Type, reason))

	flow.UpdatedAt = time.Now().UTC()
	e.flows[flow.ID] = cloneFlow(flow)
	return cloneFlow(flow), nil
}

// ListEvents returns a copy of flow events.
func (e *InMemoryEngine) ListEvents(ctx context.Context, flowID string) ([]FlowEvent, error) {
	flow, err := e.Get(ctx, flowID)
	if err != nil {
		return nil, err
	}
	out := make([]FlowEvent, len(flow.Events))
	copy(out, flow.Events)
	return out, nil
}

// AttachArtifact records a draft artifact on a stage (used by tests / future stages).
func (e *InMemoryEngine) AttachArtifact(flowID, stageID, artType string) (*Artifact, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	flow, err := e.mustGetLocked(flowID)
	if err != nil {
		return nil, err
	}
	if stageIndexByID(flow, stageID) < 0 {
		return nil, fmt.Errorf("stage not found: %s", stageID)
	}
	art := Artifact{
		ID:        uuid.New().String(),
		StageID:   stageID,
		Type:      artType,
		Version:   1,
		Status:    ArtifactStatusDraft,
		CreatedAt: time.Now().UTC(),
	}
	flow.Artifacts = append(flow.Artifacts, art)
	flow.UpdatedAt = time.Now().UTC()
	e.flows[flow.ID] = cloneFlow(flow)
	return &art, nil
}

func (e *InMemoryEngine) mustGetLocked(id string) (*Flow, error) {
	f, ok := e.flows[id]
	if !ok {
		return nil, fmt.Errorf("flow not found: %s", id)
	}
	// work on a mutable clone; caller stores back
	return cloneFlow(f), nil
}

func (e *InMemoryEngine) appendEvent(flow *Flow, typ, stageID, msg string) {
	flow.Events = append(flow.Events, FlowEvent{
		ID:        uuid.New().String(),
		Type:      typ,
		StageID:   stageID,
		Message:   msg,
		Timestamp: time.Now().UTC(),
	})
}

func activeStageIndex(flow *Flow) int {
	for i := range flow.Stages {
		if flow.Stages[i].Status == StageStatusActive {
			return i
		}
	}
	return -1
}

func stageIndexByID(flow *Flow, id string) int {
	for i := range flow.Stages {
		if flow.Stages[i].ID == id {
			return i
		}
	}
	return -1
}

func loopAllowed(loops []LoopEdge, from, to StageType) bool {
	for _, e := range loops {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}

func passExitGates(stage *Stage, force bool) error {
	for i := range stage.Gates {
		g := &stage.Gates[i]
		if g.Phase != GatePhaseExit {
			continue
		}
		switch g.Kind {
		case GateKindAuto:
			g.Passed = true
		case GateKindHumanApproval, GateKindAgentCheck:
			if force {
				g.Passed = true
				continue
			}
			if !g.Passed {
				stage.Status = StageStatusWaitingGate
				return fmt.Errorf("stage %s blocked on %s gate %s", stage.Type, g.Kind, g.ID)
			}
		default:
			g.Passed = true
		}
	}
	return nil
}

func cloneFlow(f *Flow) *Flow {
	if f == nil {
		return nil
	}
	cp := *f
	cp.Stages = append([]Stage(nil), f.Stages...)
	for i := range cp.Stages {
		cp.Stages[i].Gates = append([]Gate(nil), f.Stages[i].Gates...)
	}
	cp.Loops = append([]LoopEdge(nil), f.Loops...)
	cp.Artifacts = append([]Artifact(nil), f.Artifacts...)
	cp.Events = append([]FlowEvent(nil), f.Events...)
	return &cp
}
