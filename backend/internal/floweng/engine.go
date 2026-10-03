package floweng

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// InMemoryEngine is the Flow state machine. Despite the historical name it can
// back onto any FlowStore (memory default, or SQLite via NewSQLiteEngine).
type InMemoryEngine struct {
	mu         sync.Mutex
	store      FlowStore
	snapshots  SnapshotCreator       // optional
	restorer   SnapshotRestorer      // optional
	notifier   EventNotifier         // optional
	guard      ExecutionGuard        // optional
	escalation GateEscalationHandler // optional
	bindings   BindingResolver       // optional (T3.01.a group 3)
}

// NewInMemoryEngine creates an engine with an in-process memory store.
// snapshots may be nil (no auto snapshot on advance).
func NewInMemoryEngine(snapshots SnapshotCreator) *InMemoryEngine {
	store := newMemoryStore()
	ensureBuiltinRevisions(store)
	return &InMemoryEngine{
		store:     store,
		snapshots: snapshots,
	}
}

// NewEngineWithStore creates an engine on a custom store (tests / composition).
func NewEngineWithStore(store FlowStore, snapshots SnapshotCreator) *InMemoryEngine {
	if store == nil {
		store = newMemoryStore()
	}
	ensureBuiltinRevisions(store)
	return &InMemoryEngine{store: store, snapshots: snapshots}
}

// ensureBuiltinRevisions freezes the current code definition of every built-in
// template into the store when the store keeps template revisions, so a flow
// created through an in-process engine names the same revision a durable engine
// would (T3.01.a group 3). The durable store does its own seeding when the
// database is opened; this call is then a no-op (the content already matches),
// so both paths agree and neither writes twice.
func ensureBuiltinRevisions(store FlowStore) {
	rs, ok := store.(TemplateRevisionStore)
	if !ok {
		return
	}
	ids := make([]TemplateID, 0, len(builtinTemplates))
	for id := range builtinTemplates {
		ids = append(ids, id)
	}
	sortTemplateIDs(ids)
	for _, id := range ids {
		_, _ = rs.EnsureTemplateRevision(builtinTemplates[id], TemplateRevisionSourceBuiltin)
	}
}

// NewSQLiteEngine opens a durable engine at dbPath (e.g. data/floweng.db).
func NewSQLiteEngine(dbPath string, snapshots SnapshotCreator) (*InMemoryEngine, error) {
	store, err := NewSQLiteFlowStore(dbPath)
	if err != nil {
		return nil, err
	}
	defs, err := store.ListTemplateDefinitions()
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	for _, def := range defs {
		if err := RegisterTemplate(def); err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("load flow template %s: %w", def.ID, err)
		}
	}
	return NewEngineWithStore(store, snapshots), nil
}

// Close releases durable resources when the underlying store supports it.
func (e *InMemoryEngine) Close() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	type closer interface{ Close() error }
	if c, ok := e.store.(closer); ok {
		return c.Close()
	}
	return nil
}

// SetSnapshotCreator attaches or replaces the stage-completion snapshot hook.
func (e *InMemoryEngine) SetSnapshotCreator(s SnapshotCreator) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.snapshots = s
}

// SetSnapshotRestorer attaches or replaces the loop-time snapshot restore hook.
func (e *InMemoryEngine) SetSnapshotRestorer(r SnapshotRestorer) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.restorer = r
}

// SetEventNotifier attaches or replaces the event bus hook (e.g. WebSocket).
func (e *InMemoryEngine) SetEventNotifier(n EventNotifier) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.notifier = n
}

// SetExecutionGuard attaches or replaces the stage-transition lock (design §4).
// When set, Advance and Loop refuse to run while the guard reports busy.
func (e *InMemoryEngine) SetExecutionGuard(g ExecutionGuard) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.guard = g
}

// SetGateEscalationHandler attaches or replaces the callback invoked when a
// gate with on_fail=escalate_to_debate is rejected. The handler receives cloned
// data and runs synchronously after the decision is persisted and e.mu released;
// errors are its own concern.
func (e *InMemoryEngine) SetGateEscalationHandler(h GateEscalationHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.escalation = h
}

// Create instantiates a Flow from a built-in template.
//
// T3.01.a group 2 adds the kind and the one-active-project-flow rule. Kind
// defaults to FlowKindProject, so every caller that predates this step creates
// what it always created. A project may hold exactly one active project flow: a
// second Create for the same project is refused with ErrActiveProjectFlowExists,
// without writing anything, and the caller learns the id of the flow that holds
// the slot. Task flows are not limited — a project may hold any number of them —
// but a task flow's ParentProjectFlowID, when given, must name a Flow of the
// same project whose kind is project.
func (e *InMemoryEngine) Create(ctx context.Context, req *CreateFlowRequest) (*Flow, error) {
	if req == nil || req.ProjectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	kind := req.Kind
	if kind == "" {
		kind = FlowKindProject
	}
	if !kind.valid() {
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	tmpl, ok := getTemplate(req.TemplateID)
	if !ok {
		return nil, fmt.Errorf("unknown template_id %q", req.TemplateID)
	}

	now := time.Now().UTC()
	flow := &Flow{
		ID:         uuid.New().String(),
		ProjectID:  req.ProjectID,
		SessionID:  req.SessionID,
		TemplateID: tmpl.ID,
		Status:     FlowStatusActive,
		// T3.01.a: a flow created from a template is the project's flow of
		// record unless the caller asks for a task flow. Revision is not set
		// here: the store owns it.
		Kind:                kind,
		ParentProjectFlowID: req.ParentProjectFlowID,
		Loops:               append([]LoopEdge(nil), tmpl.Loops...),
		Artifacts:           make([]Artifact, 0),
		Events:              make([]FlowEvent, 0),
		CreatedAt:           now,
		UpdatedAt:           now,
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
				OnFail: g.OnFail,
				Config: cloneStringMap(g.Config),
				Passed: false,
			}
		}
		stages = append(stages, Stage{
			ID:       uuid.New().String(),
			Type:     def.Type,
			Name:     def.Name,
			Canvas:   def.Canvas,
			AgentID:  def.AgentID,
			Status:   status,
			Optional: def.Optional,
			Gates:    gates,
			Order:    i,
		})
	}
	flow.Stages = stages

	// The template revision and the workspace binding are facts about the flow
	// that the caller cannot supply and the store does not know: the revision is
	// whatever the template's history says right now, and the binding is
	// whatever the resolver says the project's primary binding is (T3.01.a
	// group 3). Both are resolved before e.mu is taken, and a resolver error
	// fails the Create before anything is written (fail closed).
	revision := int64(0)
	if rs, ok := e.store.(TemplateRevisionStore); ok {
		latest, err := rs.LatestTemplateRevision(tmpl.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve template revision for %s: %w", tmpl.ID, err)
		}
		revision = latest
	}
	flow.TemplateRevision = revision
	bindingID, err := e.resolveBinding(ctx, req.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("resolve binding for project %s: %w", req.ProjectID, err)
	}
	flow.BindingID = bindingID

	e.mu.Lock()
	defer e.mu.Unlock()

	if kind == FlowKindProject {
		// The check and the write share e.mu, so two Creates racing for the
		// same project's slot are serialized here and the store's partial
		// unique index (or memoryStore's map scan) remains a backstop that
		// never fires in a single engine.
		if existing, err := e.activeProjectFlowLocked(req.ProjectID, ""); err != nil {
			return nil, err
		} else if existing != nil {
			return nil, activeProjectFlowError(existing.ID)
		}
	} else if req.ParentProjectFlowID != "" {
		parent, err := e.store.Get(req.ParentProjectFlowID)
		if err != nil {
			return nil, fmt.Errorf("parent_project_flow_id %s: %w", req.ParentProjectFlowID, err)
		}
		if parent.Kind != FlowKindProject {
			return nil, fmt.Errorf("parent_project_flow_id %s is a %s flow, want a project flow", req.ParentProjectFlowID, parent.Kind)
		}
		if parent.ProjectID != req.ProjectID {
			return nil, fmt.Errorf("parent_project_flow_id %s belongs to project %s, not %s", req.ParentProjectFlowID, parent.ProjectID, req.ProjectID)
		}
	}

	e.appendEvent(flow, "flow.created", "", fmt.Sprintf("created template=%s project=%s", tmpl.ID, req.ProjectID))
	// Evaluate enter gates on the initially-active first stage: an unpassed
	// human/agent enter gate starts the flow on waiting_gate.
	e.applyEnterGates(flow, 0)

	if err := e.store.Put(flow); err != nil {
		return nil, err
	}
	return cloneFlow(flow), nil
}

// activeProjectFlowLocked returns the project's active project flow, or nil when
// the project has none. excludeID is skipped, which is what lets Resume ask
// "is anyone else active" about a flow that is not active yet and would keep
// Create idempotent if it were ever asked about the flow it is writing.
//
// The store is scanned rather than filtered by a query because both stores
// return every flow of the project from List and the number of flows a project
// holds is small; the scan is inside e.mu, and the engine is the only writer of
// both stores. The scan is deterministic: the store's own order is kept, so a
// project that somehow holds two active project flows (a database a later build
// wrote, a store that does not hold the invariant) names the same one on every
// call — List orders by updated_at DESC, which is the flow the project page
// shows.
func (e *InMemoryEngine) activeProjectFlowLocked(projectID, excludeID string) (*Flow, error) {
	items, err := e.store.List(projectID)
	if err != nil {
		return nil, err
	}
	for _, f := range items {
		if f == nil || f.ID == excludeID {
			continue
		}
		if f.Kind == FlowKindProject && f.Status == FlowStatusActive {
			return f, nil
		}
	}
	return nil, nil
}

// Resume returns a suspended flow to active (T3.01.a, §28). A suspended flow is
// a project flow that lost the project's active slot (the store migration
// suspends the extras an older database holds; a later step suspends explicitly
// when a project switches flows), so resuming it means taking the slot back:
// a project whose slot is already held by another flow cannot resume one, and
// the error names the flow that holds it.
//
// A flow that is not suspended is refused with ErrFlowNotSuspended — resuming an
// active flow is a no-op nobody asked for and resuming a terminal one would
// resurrect it.
func (e *InMemoryEngine) Resume(ctx context.Context, flowID string) (*Flow, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	flow, err := e.store.Get(flowID)
	if err != nil {
		return nil, err
	}
	if flow.Status != FlowStatusSuspended {
		return nil, fmt.Errorf("%w: %s is %s", ErrFlowNotSuspended, flowID, flow.Status)
	}
	if existing, err := e.activeProjectFlowLocked(flow.ProjectID, flowID); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, activeProjectFlowError(existing.ID)
	}

	flow.Status = FlowStatusActive
	e.appendEvent(flow, "flow.resumed", "", "resumed suspended flow")
	flow.UpdatedAt = time.Now().UTC()
	if err := e.store.Put(flow); err != nil {
		return nil, err
	}
	return cloneFlow(flow), nil
}

// Get returns a flow by id.
func (e *InMemoryEngine) Get(ctx context.Context, id string) (*Flow, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.store.Get(id)
}

// List returns flows, optionally filtered by projectID (empty = all).
func (e *InMemoryEngine) List(ctx context.Context, projectID string) ([]*Flow, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.store.List(projectID)
}

// ListByStatus filters flows by optional projectID and status.
func (e *InMemoryEngine) ListByStatus(ctx context.Context, projectID string, status FlowStatus) ([]*Flow, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	all, err := e.store.List(projectID)
	if err != nil {
		return nil, err
	}
	if status == "" {
		return all, nil
	}
	out := make([]*Flow, 0, len(all))
	for _, f := range all {
		if f != nil && f.Status == status {
			out = append(out, f)
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

	flow, err := e.store.Get(flowID)
	if err != nil {
		return nil, err
	}
	if err := requireActiveFlow(flow); err != nil {
		return nil, err
	}

	activeIdx := activeStageIndex(flow)
	if activeIdx < 0 {
		return nil, fmt.Errorf("no active stage")
	}
	stage := &flow.Stages[activeIdx]
	if req.ExpectedStageID != "" && stage.ID != req.ExpectedStageID {
		return nil, fmt.Errorf("stage is not active: %s", req.ExpectedStageID)
	}

	if err := e.checkExecutionGuard(); err != nil {
		return nil, err
	}

	if err := passExitGates(stage); err != nil {
		// persist waiting_gate status
		flow.UpdatedAt = time.Now().UTC()
		_ = e.store.Put(flow)
		return nil, err
	}

	if e.snapshots != nil {
		sid, err := e.snapshots.CreateStageSnapshot(ctx, flow, stage, req.SessionID)
		if err != nil {
			return nil, fmt.Errorf("stage snapshot: %w", err)
		}
		stage.SnapshotID = sid
	}

	stage.Status = StageStatusDone
	for i := range flow.Artifacts {
		if flow.Artifacts[i].StageID == stage.ID && flow.Artifacts[i].Status == ArtifactStatusDraft {
			flow.Artifacts[i].Status = ArtifactStatusApproved
			e.appendEvent(flow, "artifact.approved", stage.ID, fmt.Sprintf("artifact %s approved on stage completion", flow.Artifacts[i].ID))
		}
	}
	e.appendEvent(flow, "stage.done", stage.ID, fmt.Sprintf("completed stage type=%s", stage.Type))

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
		e.applyEnterGates(flow, next)
	}

	flow.UpdatedAt = time.Now().UTC()
	if err := e.store.Put(flow); err != nil {
		return nil, err
	}
	return cloneFlow(flow), nil
}

// Skip skips an optional stage that is pending or active.
func (e *InMemoryEngine) Skip(ctx context.Context, flowID string, req *SkipRequest) (*Flow, error) {
	if req == nil || req.StageID == "" {
		return nil, fmt.Errorf("stage_id is required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	flow, err := e.store.Get(flowID)
	if err != nil {
		return nil, err
	}
	if err := requireActiveFlow(flow); err != nil {
		return nil, err
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
		activated := false
		for i := idx + 1; i < len(flow.Stages); i++ {
			if flow.Stages[i].Status == StageStatusPending {
				flow.Stages[i].Status = StageStatusActive
				e.appendEvent(flow, "stage.active", flow.Stages[i].ID, fmt.Sprintf("activated type=%s", flow.Stages[i].Type))
				e.applyEnterGates(flow, i)
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
	if err := e.store.Put(flow); err != nil {
		return nil, err
	}
	return cloneFlow(flow), nil
}

// Loop jumps back along an allowed template loop edge.
func (e *InMemoryEngine) Loop(ctx context.Context, flowID string, req *LoopRequest) (*Flow, error) {
	if req == nil || req.FromStageID == "" || req.ToStageID == "" {
		return nil, fmt.Errorf("from_stage_id and to_stage_id are required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	flow, err := e.store.Get(flowID)
	if err != nil {
		return nil, err
	}
	if err := requireActiveFlow(flow); err != nil {
		return nil, err
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

	if err := e.checkExecutionGuard(); err != nil {
		return nil, err
	}

	// Snapshot-based rollback (design §4-5): restore the target stage's
	// completion snapshot before mutating any flow state. A restore failure
	// aborts the loop with the flow left untouched.
	if e.restorer != nil {
		if snapID := flow.Stages[toIdx].SnapshotID; snapID != "" {
			if err := e.restorer.RestoreStageSnapshot(ctx, flow, &flow.Stages[toIdx], snapID); err != nil {
				return nil, fmt.Errorf("restore stage snapshot: %w", err)
			}
			e.appendEvent(flow, "flow.restored", to.ID, fmt.Sprintf("restored snapshot %s for %s", snapID, to.Type))
		}
	}

	for i := range flow.Artifacts {
		artStageIdx := stageIndexByID(flow, flow.Artifacts[i].StageID)
		if artStageIdx >= toIdx {
			flow.Artifacts[i].Status = ArtifactStatusStale
		}
	}
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
	e.applyEnterGates(flow, toIdx)

	flow.UpdatedAt = time.Now().UTC()
	if err := e.store.Put(flow); err != nil {
		return nil, err
	}
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

// DecideGate approves or rejects a gate on a flow stage.
func (e *InMemoryEngine) DecideGate(ctx context.Context, flowID, gateID string, req *GateDecisionRequest) (*Flow, error) {
	if gateID == "" {
		return nil, fmt.Errorf("gate_id is required")
	}
	if req == nil || req.Approved == nil {
		return nil, fmt.Errorf("approved is required")
	}

	// Keep the state transition, event append, persistence, handler selection,
	// and callback snapshots atomic. Dispatch only after releasing e.mu so a
	// handler can safely re-enter the engine and a slow handler does not stall
	// unrelated flow operations. The callback itself remains synchronous from
	// the DecideGate caller's perspective.
	e.mu.Lock()
	result, escalation, err := e.decideGateLocked(flowID, gateID, req)
	e.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if escalation != nil {
		escalation.handler.OnGateEscalation(ctx, escalation.flow, escalation.stage, escalation.gate, escalation.reason)
	}
	return result, nil
}

// gateEscalationDispatch is a fully detached callback invocation captured while
// e.mu is held. None of its mutable arguments share state with the engine or
// with one another.
type gateEscalationDispatch struct {
	handler GateEscalationHandler
	flow    *Flow
	stage   *Stage
	gate    *Gate
	reason  string
}

// decideGateLocked applies and persists a decision and prepares any escalation
// callback for post-unlock dispatch. The caller must hold e.mu.
func (e *InMemoryEngine) decideGateLocked(flowID, gateID string, req *GateDecisionRequest) (*Flow, *gateEscalationDispatch, error) {
	flow, err := e.store.Get(flowID)
	if err != nil {
		return nil, nil, err
	}
	if err := requireActiveFlow(flow); err != nil {
		return nil, nil, err
	}
	found := false
	escalatedStageIdx := -1
	for si := range flow.Stages {
		for gi := range flow.Stages[si].Gates {
			g := &flow.Stages[si].Gates[gi]
			if g.ID != gateID {
				continue
			}
			found = true
			stage := &flow.Stages[si]
			if stage.Status != StageStatusActive && stage.Status != StageStatusWaitingGate {
				return nil, nil, fmt.Errorf("gate stage is not active: %s", stage.Status)
			}
			if *req.Approved {
				g.Passed = true
				if stage.Status == StageStatusWaitingGate {
					stage.Status = StageStatusActive
				}
				e.appendEvent(flow, "gate.approved", stage.ID, fmt.Sprintf("gate %s approved: %s", gateID, req.Reason))
				e.applyEnterGates(flow, si)
			} else {
				g.Passed = false
				stage.Status = StageStatusWaitingGate
				e.appendEvent(flow, "gate.rejected", stage.ID, fmt.Sprintf("gate %s rejected: %s", gateID, req.Reason))
				if g.OnFail == GateOnFailEscalateDebate {
					e.appendEvent(flow, "gate.escalate_debate", stage.ID,
						fmt.Sprintf("gate %s escalate to debate: %s", gateID, req.Reason))
					escalatedStageIdx = si
				}
			}
		}
	}
	if !found {
		return nil, nil, fmt.Errorf("gate not found: %s", gateID)
	}
	flow.UpdatedAt = time.Now().UTC()
	if err := e.store.Put(flow); err != nil {
		return nil, nil, err
	}

	result := cloneFlow(flow)
	if e.escalation == nil || escalatedStageIdx < 0 {
		return result, nil, nil
	}

	stageCopy := cloneStage(flow.Stages[escalatedStageIdx])
	var gateCopy Gate
	for i := range stageCopy.Gates {
		if stageCopy.Gates[i].ID == gateID {
			gateCopy = cloneGate(stageCopy.Gates[i])
			break
		}
	}
	return result, &gateEscalationDispatch{
		handler: e.escalation,
		flow:    cloneFlow(flow),
		stage:   &stageCopy,
		gate:    &gateCopy,
		reason:  req.Reason,
	}, nil
}

// Abort terminates an active or suspended flow. A suspended flow has nothing
// running, but aborting it is how it is discarded, so the operation is allowed
// on it exactly as it is on an active one (T3.01.a group 2).
func (e *InMemoryEngine) Abort(ctx context.Context, flowID, reason string) (*Flow, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	flow, err := e.store.Get(flowID)
	if err != nil {
		return nil, err
	}
	if flow.Status != FlowStatusActive && flow.Status != FlowStatusSuspended {
		return nil, fmt.Errorf("flow is not active: %s", flow.Status)
	}
	flow.Status = FlowStatusAborted
	if reason == "" {
		reason = "aborted"
	}
	e.appendEvent(flow, "flow.aborted", "", reason)
	// clear active stages
	for i := range flow.Stages {
		if flow.Stages[i].Status == StageStatusActive || flow.Stages[i].Status == StageStatusWaitingGate {
			flow.Stages[i].Status = StageStatusPending
		}
	}
	flow.UpdatedAt = time.Now().UTC()
	if err := e.store.Put(flow); err != nil {
		return nil, err
	}
	return cloneFlow(flow), nil
}

// AttachArtifact records a draft artifact on a stage (contentRef optional).
// The author is left unspecified; use AttachArtifactBy to record it.
func (e *InMemoryEngine) AttachArtifact(ctx context.Context, flowID, stageID, artType, contentRef string) (*Artifact, error) {
	return e.AttachArtifactBy(ctx, flowID, stageID, artType, contentRef, "")
}

// AttachArtifactBy records a draft artifact and its author. createdBy must be
// "" (unspecified), ArtifactCreatorAgent, or ArtifactCreatorUser.
func (e *InMemoryEngine) AttachArtifactBy(ctx context.Context, flowID, stageID, artType, contentRef, createdBy string) (*Artifact, error) {
	switch createdBy {
	case "", ArtifactCreatorAgent, ArtifactCreatorUser:
	default:
		return nil, fmt.Errorf("invalid created_by %q (want %q, %q, or empty)", createdBy, ArtifactCreatorAgent, ArtifactCreatorUser)
	}
	if artType == "" {
		artType = "generic"
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	flow, err := e.store.Get(flowID)
	if err != nil {
		return nil, err
	}
	if err := requireActiveFlow(flow); err != nil {
		return nil, err
	}
	if stageIndexByID(flow, stageID) < 0 {
		return nil, fmt.Errorf("stage not found: %s", stageID)
	}
	// A content reference identifies an immutable artifact payload. Returning
	// the existing record makes client retries safe when the first response was
	// lost after persistence.
	if contentRef != "" {
		for i := len(flow.Artifacts) - 1; i >= 0; i-- {
			existing := flow.Artifacts[i]
			if existing.StageID == stageID && existing.Type == artType && existing.ContentRef == contentRef && existing.Status != ArtifactStatusStale {
				return &existing, nil
			}
		}
	}
	ver := 1
	for _, a := range flow.Artifacts {
		if a.StageID == stageID && a.Type == artType {
			ver++
		}
	}
	art := Artifact{
		ID:         uuid.New().String(),
		StageID:    stageID,
		Type:       artType,
		Version:    ver,
		Status:     ArtifactStatusDraft,
		CreatedBy:  createdBy,
		ContentRef: contentRef,
		CreatedAt:  time.Now().UTC(),
	}
	flow.Artifacts = append(flow.Artifacts, art)
	e.appendEvent(flow, "artifact.created", stageID, fmt.Sprintf("artifact type=%s v=%d", artType, ver))
	flow.UpdatedAt = time.Now().UTC()
	if err := e.store.Put(flow); err != nil {
		return nil, err
	}
	return &art, nil
}

// Delete removes a flow.
func (e *InMemoryEngine) Delete(ctx context.Context, flowID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.store.Delete(flowID)
}

// SetArtifactStatus updates an artifact's status.
func (e *InMemoryEngine) SetArtifactStatus(ctx context.Context, flowID, artifactID string, status ArtifactStatus) (*Artifact, error) {
	switch status {
	case ArtifactStatusDraft, ArtifactStatusApproved, ArtifactStatusStale:
	default:
		return nil, fmt.Errorf("invalid artifact status %q", status)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	flow, err := e.store.Get(flowID)
	if err != nil {
		return nil, err
	}
	if err := requireActiveFlow(flow); err != nil {
		return nil, err
	}
	var out *Artifact
	for i := range flow.Artifacts {
		if flow.Artifacts[i].ID == artifactID {
			flow.Artifacts[i].Status = status
			out = &flow.Artifacts[i]
			e.appendEvent(flow, "artifact.status", flow.Artifacts[i].StageID, fmt.Sprintf("artifact %s -> %s", artifactID, status))
			break
		}
	}
	if out == nil {
		return nil, fmt.Errorf("artifact not found: %s", artifactID)
	}
	flow.UpdatedAt = time.Now().UTC()
	if err := e.store.Put(flow); err != nil {
		return nil, err
	}
	cp := *out
	return &cp, nil
}

// putRaw is used by tests to inject stage/gate fixtures.
func (e *InMemoryEngine) putRaw(flow *Flow) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.store.Put(flow)
}

func (e *InMemoryEngine) appendEvent(flow *Flow, typ, stageID, msg string) {
	ev := FlowEvent{
		ID:        uuid.New().String(),
		Type:      typ,
		StageID:   stageID,
		Message:   msg,
		Timestamp: time.Now().UTC(),
	}
	flow.Events = append(flow.Events, ev)
	if e.notifier != nil {
		// clone flow view for external bus without sharing mutable stages slice
		e.notifier.OnFlowEvent(cloneFlow(flow), ev)
	}
}

// requireActiveFlow refuses an operation that only makes sense on a running
// flow. A suspended flow is a parked one: its stages, gates and artifacts are
// frozen until it is resumed, so the stage machine and the artifact writes
// refuse it exactly as they refuse a completed or aborted flow (T3.01.a group 2,
// §28). The wording is the pool's: the handlers already map "not active" to 409.
func requireActiveFlow(flow *Flow) error {
	if flow.Status != FlowStatusActive {
		return fmt.Errorf("flow is not active: %s", flow.Status)
	}
	return nil
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

func passExitGates(stage *Stage) error {
	for i := range stage.Gates {
		g := &stage.Gates[i]
		if g.Phase != GatePhaseExit {
			continue
		}
		switch g.Kind {
		case GateKindAuto:
			g.Passed = true
		case GateKindHumanApproval, GateKindAgentCheck:
			if !g.Passed {
				stage.Status = StageStatusWaitingGate
				if g.OnFail == GateOnFailEscalateDebate {
					return fmt.Errorf("stage %s blocked on %s gate %s (escalation to debate suggested)", stage.Type, g.Kind, g.ID)
				}
				return fmt.Errorf("stage %s blocked on %s gate %s", stage.Type, g.Kind, g.ID)
			}
		default:
			g.Passed = true
		}
	}
	return nil
}

// checkExecutionGuard fails the caller when an ExecutionGuard reports busy.
// Callers hold e.mu; the guard must not re-enter the engine.
func (e *InMemoryEngine) checkExecutionGuard() error {
	if e.guard == nil {
		return nil
	}
	if busy, reason := e.guard.Busy(); busy {
		if reason == "" {
			reason = "busy"
		}
		return fmt.Errorf("stage transition locked: %s", reason)
	}
	return nil
}

// applyEnterGates evaluates enter-phase gates on a stage that has just been
// activated. Auto enter gates pass immediately; the first unpassed human/agent
// enter gate parks the stage on waiting_gate and emits gate.waiting. The
// transition that activated the stage is unaffected (it still succeeded).
func (e *InMemoryEngine) applyEnterGates(flow *Flow, idx int) {
	if idx < 0 || idx >= len(flow.Stages) {
		return
	}
	stage := &flow.Stages[idx]
	if stage.Status != StageStatusActive {
		return
	}
	if g := firstBlockingEnterGate(stage); g != nil {
		stage.Status = StageStatusWaitingGate
		e.appendEvent(flow, "gate.waiting", stage.ID,
			fmt.Sprintf("stage %s waiting on %s enter gate %s", stage.Type, g.Kind, g.ID))
	}
}

// firstBlockingEnterGate passes auto enter gates and returns the first unpassed
// human/agent enter gate (nil if none blocks the stage).
func firstBlockingEnterGate(stage *Stage) *Gate {
	for i := range stage.Gates {
		g := &stage.Gates[i]
		if g.Phase != GatePhaseEnter {
			continue
		}
		switch g.Kind {
		case GateKindAuto:
			g.Passed = true
		case GateKindHumanApproval, GateKindAgentCheck:
			if !g.Passed {
				return g
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
		cp.Stages[i] = cloneStage(cp.Stages[i])
	}
	cp.Loops = append([]LoopEdge(nil), f.Loops...)
	for i := range cp.Loops {
		cp.Loops[i].extras = cloneExtras(f.Loops[i].extras)
	}
	cp.Artifacts = append([]Artifact(nil), f.Artifacts...)
	for i := range cp.Artifacts {
		cp.Artifacts[i].extras = cloneExtras(f.Artifacts[i].extras)
	}
	cp.Events = append([]FlowEvent(nil), f.Events...)
	for i := range cp.Events {
		cp.Events[i].extras = cloneExtras(f.Events[i].extras)
	}
	cp.extras = cloneExtras(f.extras)
	return &cp
}

func cloneStage(stage Stage) Stage {
	stage.Gates = append([]Gate(nil), stage.Gates...)
	for i := range stage.Gates {
		stage.Gates[i] = cloneGate(stage.Gates[i])
	}
	stage.extras = cloneExtras(stage.extras)
	return stage
}

func cloneGate(gate Gate) Gate {
	gate.Config = cloneStringMap(gate.Config)
	gate.extras = cloneExtras(gate.extras)
	return gate
}

// cloneExtras copies a preserved-members map (nil stays nil), values included,
// so a copy of a document shares nothing with the document it came from: adding
// or removing an extra on the copy, or changing one of its bytes, leaves the
// original as it was. See legacy_json.go for what the map holds.
func cloneExtras(m map[string]json.RawMessage) map[string]json.RawMessage {
	if m == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(m))
	for k, v := range m {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}

// cloneStringMap returns a shallow copy of m (nil stays nil) so callers never
// share a gate Config map with engine-internal state.
func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
