// Package floweng is the Flow execution engine (stage state machine).
// Observation/timeline remains in internal/workflow; this package owns runtime truth.
package floweng

import (
	"context"
	"time"
)

// FlowStatus is the lifecycle of a Flow instance.
type FlowStatus string

const (
	FlowStatusActive    FlowStatus = "active"
	FlowStatusCompleted FlowStatus = "completed"
	FlowStatusAborted   FlowStatus = "aborted"
)

// StageStatus is the lifecycle of a Stage within a Flow.
type StageStatus string

const (
	StageStatusPending     StageStatus = "pending"
	StageStatusActive      StageStatus = "active"
	StageStatusWaitingGate StageStatus = "waiting_gate"
	StageStatusDone        StageStatus = "done"
	StageStatusSkipped     StageStatus = "skipped"
)

// StageType identifies built-in or plugin stage kinds.
type StageType string

const (
	StageTypeIdea       StageType = "idea"
	StageTypeDesign     StageType = "design"
	StageTypePlanning   StageType = "planning"
	StageTypeResearch   StageType = "research"
	StageTypeCoding     StageType = "coding"
	StageTypeReview     StageType = "review"
	StageTypeSubmit     StageType = "submit"
	StageTypeImport     StageType = "import"
	StageTypeComprehend StageType = "comprehension"
)

// GateKind is how a gate is evaluated.
type GateKind string

const (
	GateKindAuto          GateKind = "auto"
	GateKindHumanApproval GateKind = "human_approval"
	GateKindAgentCheck    GateKind = "agent_check"
)

// GatePhase is when the gate runs.
type GatePhase string

const (
	GatePhaseEnter GatePhase = "enter"
	GatePhaseExit  GatePhase = "exit"
)

// GateOnFail is the policy applied when a gate blocks or is rejected (design §2).
type GateOnFail string

const (
	// GateOnFailBlock halts the stage until the gate passes. It is the
	// zero-value semantics: an empty OnFail is treated as block.
	GateOnFailBlock GateOnFail = "block"
	// GateOnFailEscalateDebate additionally signals that a debate should be
	// opened to resolve the block; the stage still halts on waiting_gate.
	GateOnFailEscalateDebate GateOnFail = "escalate_to_debate"
)

// ArtifactStatus tracks artifact freshness after loops.
type ArtifactStatus string

const (
	ArtifactStatusDraft    ArtifactStatus = "draft"
	ArtifactStatusApproved ArtifactStatus = "approved"
	ArtifactStatusStale    ArtifactStatus = "stale"
)

// Artifact author values for Artifact.CreatedBy (design §2 created_by).
const (
	ArtifactCreatorAgent = "agent"
	ArtifactCreatorUser  = "user"
)

// TemplateID names a built-in or registered flow template.
type TemplateID string

const (
	TemplateNewProject TemplateID = "new_project"
	TemplateImport     TemplateID = "import_project"
)

// Flow is a running workflow instance for a project.
type Flow struct {
	ID         string      `json:"id"`
	ProjectID  string      `json:"project_id"`
	SessionID  string      `json:"session_id,omitempty"`
	TemplateID TemplateID  `json:"template_id"`
	Status     FlowStatus  `json:"status"`
	Stages     []Stage     `json:"stages"`
	Loops      []LoopEdge  `json:"loops"`
	Artifacts  []Artifact  `json:"artifacts"`
	Events     []FlowEvent `json:"events"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// Stage is one step in a Flow.
type Stage struct {
	ID         string      `json:"id"`
	Type       StageType   `json:"type"`
	Name       string      `json:"name"`
	Canvas     string      `json:"canvas"`
	AgentID    string      `json:"agent_id,omitempty"`
	Status     StageStatus `json:"status"`
	Optional   bool        `json:"optional"`
	SnapshotID string      `json:"snapshot_id,omitempty"`
	Gates      []Gate      `json:"gates,omitempty"`
	Order      int         `json:"order"`
}

// Gate is an enter/exit check on a stage.
type Gate struct {
	ID     string     `json:"id"`
	Phase  GatePhase  `json:"phase"`
	Kind   GateKind   `json:"kind"`
	OnFail GateOnFail `json:"on_fail,omitempty"`
	// Config carries gate parameters (e.g. test pass-rate threshold, approver).
	Config map[string]string `json:"config,omitempty"`
	Passed bool              `json:"passed"`
}

// LoopEdge allows jumping from one stage type back to an earlier type.
type LoopEdge struct {
	From StageType `json:"from"`
	To   StageType `json:"to"`
}

// Artifact is a stage output reference (content stored elsewhere).
type Artifact struct {
	ID      string         `json:"id"`
	StageID string         `json:"stage_id"`
	Type    string         `json:"type"`
	Version int            `json:"version"`
	Status  ArtifactStatus `json:"status"`
	// CreatedBy records the author: "agent", "user", or "" when unspecified.
	CreatedBy string `json:"created_by,omitempty"`
	// ContentRef is an optional storage pointer (path, blob id, URI).
	ContentRef string    `json:"content_ref,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// FlowEvent is an append-only audit/timeline entry for observation adapters.
type FlowEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	StageID   string    `json:"stage_id,omitempty"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// CreateFlowRequest creates a Flow from a template.
type CreateFlowRequest struct {
	ProjectID  string     `json:"project_id" binding:"required"`
	TemplateID TemplateID `json:"template_id"`
	SessionID  string     `json:"session_id,omitempty"` // optional: snapshot association
}

// AdvanceRequest completes the active stage and moves forward.
type AdvanceRequest struct {
	SessionID string `json:"session_id,omitempty"`
	// Force is retained for API compatibility. It never bypasses human-approval
	// or agent-check gates; automatic gates pass as part of normal evaluation.
	Force bool `json:"force,omitempty"`
	// ExpectedStageID binds an API advance to the stage named in its route.
	// It is internal-only and checked atomically with the state transition.
	ExpectedStageID string `json:"-"`
}

// SkipRequest skips an optional stage.
type SkipRequest struct {
	StageID string `json:"stage_id" binding:"required"`
}

// LoopRequest jumps back along an allowed loop edge.
type LoopRequest struct {
	FromStageID string `json:"from_stage_id" binding:"required"`
	ToStageID   string `json:"to_stage_id" binding:"required"`
	Reason      string `json:"reason,omitempty"`
}

// SnapshotCreator is optional; when set, stage completion creates a snapshot.
type SnapshotCreator interface {
	CreateStageSnapshot(ctx context.Context, flow *Flow, stage *Stage, sessionID string) (snapshotID string, err error)
}

// SnapshotRestorer is optional; when set, Loop restores the target stage's
// completion snapshot before rolling flow state back (design §4-5 回跳基于阶段快照).
// A restore error aborts the loop with the flow left unchanged.
type SnapshotRestorer interface {
	RestoreStageSnapshot(ctx context.Context, flow *Flow, target *Stage, snapshotID string) error
}

// EventNotifier is optional; receives each appended flow event for buses (WS, etc.).
type EventNotifier interface {
	OnFlowEvent(flow *Flow, event FlowEvent)
}

// ExecutionGuard is optional; when set, stage transitions (Advance/Loop) refuse
// to run while a write/git transaction is in flight (design §4 阶段切换持锁).
// Busy reports whether a transition is currently disallowed and, if so, a
// human-readable reason surfaced in the returned error.
type ExecutionGuard interface {
	Busy() (busy bool, reason string)
}

// GateDecisionRequest approves or rejects a gate.
type GateDecisionRequest struct {
	Approved *bool  `json:"approved" binding:"required"`
	Reason   string `json:"reason,omitempty"`
}

// Engine is the Flow state machine API.
type Engine interface {
	Create(ctx context.Context, req *CreateFlowRequest) (*Flow, error)
	Get(ctx context.Context, id string) (*Flow, error)
	List(ctx context.Context, projectID string) ([]*Flow, error)
	// ListByStatus filters by project (optional) and status (optional empty = any).
	ListByStatus(ctx context.Context, projectID string, status FlowStatus) ([]*Flow, error)
	Advance(ctx context.Context, flowID string, req *AdvanceRequest) (*Flow, error)
	Skip(ctx context.Context, flowID string, req *SkipRequest) (*Flow, error)
	Loop(ctx context.Context, flowID string, req *LoopRequest) (*Flow, error)
	ListEvents(ctx context.Context, flowID string) ([]FlowEvent, error)
	// DecideGate sets human/agent gate passed flag (approve/reject).
	DecideGate(ctx context.Context, flowID, gateID string, req *GateDecisionRequest) (*Flow, error)
	// Abort marks a flow aborted (terminal).
	Abort(ctx context.Context, flowID, reason string) (*Flow, error)
	// AttachArtifact records a draft artifact on a stage (contentRef optional storage pointer).
	AttachArtifact(ctx context.Context, flowID, stageID, artType, contentRef string) (*Artifact, error)
	// SetArtifactStatus updates artifact status (draft/approved/stale).
	SetArtifactStatus(ctx context.Context, flowID, artifactID string, status ArtifactStatus) (*Artifact, error)
	// Delete removes a flow document.
	Delete(ctx context.Context, flowID string) error
}
