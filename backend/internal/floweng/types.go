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
	StageTypeIdea     StageType = "idea"
	StageTypeDesign   StageType = "design"
	StageTypePlanning StageType = "planning"
	StageTypeResearch StageType = "research"
	StageTypeCoding   StageType = "coding"
	StageTypeReview   StageType = "review"
	StageTypeSubmit   StageType = "submit"
	StageTypeImport   StageType = "import"
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

// ArtifactStatus tracks artifact freshness after loops.
type ArtifactStatus string

const (
	ArtifactStatusDraft    ArtifactStatus = "draft"
	ArtifactStatusApproved ArtifactStatus = "approved"
	ArtifactStatusStale    ArtifactStatus = "stale"
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
	Status     StageStatus `json:"status"`
	Optional   bool        `json:"optional"`
	SnapshotID string      `json:"snapshot_id,omitempty"`
	Gates      []Gate      `json:"gates,omitempty"`
	Order      int         `json:"order"`
}

// Gate is an enter/exit check on a stage.
type Gate struct {
	ID     string    `json:"id"`
	Phase  GatePhase `json:"phase"`
	Kind   GateKind  `json:"kind"`
	Passed bool      `json:"passed"`
}

// LoopEdge allows jumping from one stage type back to an earlier type.
type LoopEdge struct {
	From StageType `json:"from"`
	To   StageType `json:"to"`
}

// Artifact is a stage output reference (content stored elsewhere).
type Artifact struct {
	ID        string         `json:"id"`
	StageID   string         `json:"stage_id"`
	Type      string         `json:"type"`
	Version   int            `json:"version"`
	Status    ArtifactStatus `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
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
	// SkipExitGate forces auto gates through (human gates still block unless Approve used).
	Force bool `json:"force,omitempty"`
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

// Engine is the Flow state machine API.
type Engine interface {
	Create(ctx context.Context, req *CreateFlowRequest) (*Flow, error)
	Get(ctx context.Context, id string) (*Flow, error)
	List(ctx context.Context, projectID string) ([]*Flow, error)
	Advance(ctx context.Context, flowID string, req *AdvanceRequest) (*Flow, error)
	Skip(ctx context.Context, flowID string, req *SkipRequest) (*Flow, error)
	Loop(ctx context.Context, flowID string, req *LoopRequest) (*Flow, error)
	ListEvents(ctx context.Context, flowID string) ([]FlowEvent, error)
}
