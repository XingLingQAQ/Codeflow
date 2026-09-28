package floweng

// The Flow document manifest (T3.01.a, §28 "为 Flow/Stage/Artifact 映射记录原
// ID/顺序").
//
// T3.01.b copies legacy Flows into the runtime database. Two things have to
// survive that copy: the identifiers the legacy documents minted, because
// observations, artifacts, snapshots and outbox rows all refer to them, and the
// order the arrays had, because it is the order a Flow's stages run in and the
// order its timeline is read back in. A manifest is the checklist for that: it
// lists every id with the position it was found at, for the Flow and for each
// of its parts.
//
// It is deliberately a pure, deterministic view. It reads no clock, mints no
// id, sorts nothing: FlowManifest(f) called twice on one document returns the
// same lists, and a document copied from a manifest-driven walk has exactly
// these ids in exactly these positions. Comparing the manifest of the source
// document with the manifest of the copy is the check T3.01.b runs.

// FlowDocumentManifest is the identities and positions of one Flow document,
// in document order.
type FlowDocumentManifest struct {
	// FlowID is the flow's own id.
	FlowID string `json:"flow_id"`
	// ProjectID is the project the flow belongs to; a copy must keep the
	// document in the same project it came from.
	ProjectID string `json:"project_id"`
	// Stages lists the stages in array order.
	Stages []StageManifest `json:"stages"`
	// Gates lists every stage's gates, each row naming the stage it belongs to.
	Gates []GateManifest `json:"gates"`
	// Artifacts lists the artifacts in array order, each row naming its stage.
	Artifacts []ArtifactManifest `json:"artifacts"`
	// Events lists the timeline in array order, which is also the order the
	// store queues new events in.
	Events []EventManifest `json:"events"`
	// Loops lists the flow's loop edges in array order. They have no id of
	// their own, so the position is what identifies them.
	Loops []LoopManifest `json:"loops"`
}

// StageManifest is one stage's identity and position.
type StageManifest struct {
	// ID is Stage.ID.
	ID string `json:"id"`
	// Index is the stage's position in Flow.Stages.
	Index int `json:"index"`
	// Order is Stage.Order, the running order the engine honours. It is kept
	// next to Index because the two can disagree in a hand-edited document and
	// a copy must not silently renumber either one.
	Order int `json:"order"`
}

// GateManifest is one gate's identity and position within its stage.
type GateManifest struct {
	// ID is Gate.ID.
	ID string `json:"id"`
	// StageID is the id of the stage the gate sits on. The manifest is flat on
	// purpose: a gate id is unique across the flow only through its stage.
	StageID string `json:"stage_id"`
	// StageIndex is that stage's position in Flow.Stages.
	StageIndex int `json:"stage_index"`
	// Index is the gate's position in its stage's Gates.
	Index int `json:"index"`
}

// ArtifactManifest is one artifact's identity and position.
type ArtifactManifest struct {
	// ID is Artifact.ID.
	ID string `json:"id"`
	// StageID is the artifact's stage reference, which may name a stage that is
	// no longer in the flow; it is copied as it stands.
	StageID string `json:"stage_id"`
	// Index is the artifact's position in Flow.Artifacts.
	Index int `json:"index"`
}

// EventManifest is one timeline entry's identity and position.
type EventManifest struct {
	// ID is FlowEvent.ID, the key the local outbox and the projector use.
	ID string `json:"id"`
	// Index is the event's position in Flow.Events.
	Index int `json:"index"`
}

// LoopManifest is one loop edge's position. A loop edge is a (from, to) pair
// with no id, so its index is its identity in the manifest.
type LoopManifest struct {
	// Index is the edge's position in Flow.Loops.
	Index int `json:"index"`
	// From and To are the edge's endpoints.
	From StageType `json:"from"`
	To   StageType `json:"to"`
}

// FlowManifest lists the ids and positions of every part of a Flow document.
// A nil Flow yields a manifest with no entries rather than an error: "there is
// nothing to copy" is a fact a caller can act on.
func FlowManifest(f *Flow) FlowDocumentManifest {
	if f == nil {
		return FlowDocumentManifest{
			Stages:    []StageManifest{},
			Gates:     []GateManifest{},
			Artifacts: []ArtifactManifest{},
			Events:    []EventManifest{},
			Loops:     []LoopManifest{},
		}
	}
	m := FlowDocumentManifest{
		FlowID:    f.ID,
		ProjectID: f.ProjectID,
		Stages:    make([]StageManifest, 0, len(f.Stages)),
		Gates:     make([]GateManifest, 0),
		Artifacts: make([]ArtifactManifest, 0, len(f.Artifacts)),
		Events:    make([]EventManifest, 0, len(f.Events)),
		Loops:     make([]LoopManifest, 0, len(f.Loops)),
	}
	for si := range f.Stages {
		stage := &f.Stages[si]
		m.Stages = append(m.Stages, StageManifest{ID: stage.ID, Index: si, Order: stage.Order})
		for gi := range stage.Gates {
			m.Gates = append(m.Gates, GateManifest{
				ID:         stage.Gates[gi].ID,
				StageID:    stage.ID,
				StageIndex: si,
				Index:      gi,
			})
		}
	}
	for i := range f.Artifacts {
		m.Artifacts = append(m.Artifacts, ArtifactManifest{
			ID:      f.Artifacts[i].ID,
			StageID: f.Artifacts[i].StageID,
			Index:   i,
		})
	}
	for i := range f.Events {
		m.Events = append(m.Events, EventManifest{ID: f.Events[i].ID, Index: i})
	}
	for i := range f.Loops {
		m.Loops = append(m.Loops, LoopManifest{Index: i, From: f.Loops[i].From, To: f.Loops[i].To})
	}
	return m
}
