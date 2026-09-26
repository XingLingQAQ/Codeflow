package flowprojection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
)

// LegacyFlowSourceStore is the source_store name of the legacy Flow database in
// the run store's legacy_event_sources table and in the projected payload.
const LegacyFlowSourceStore = "floweng"

// legacyFlowActorID is the system actor that authors every projected legacy
// Flow event (contract amendment CA-2, plan §27.1).
const legacyFlowActorID = "legacy-flow-projector"

// ErrUnknownProject is what a ProjectRefResolver returns (wrapped) for a
// project the legacy project store does not know. The event can never be
// projected — the run store's events reference project_refs — so the Target
// reports it as a PermanentError and the projector dead-letters the row.
var ErrUnknownProject = errors.New("flowprojection: legacy project not found")

// ProjectRefResolver returns the verified snapshot of a legacy project.
//
// The run store's events.project_id references project_refs (§27.1: the
// runtime keeps read-only snapshots of legacy projects instead of a cross-file
// foreign key), so an event of a project can only be appended once the project
// has a snapshot row. The resolver reads the legacy project store; the Target
// upserts its answer in the same run-store transaction as the event. Production
// wiring (T1.04) supplies the project service's snapshot; tests supply a fake.
type ProjectRefResolver func(ctx context.Context, projectID string) (run.ProjectRef, error)

// RunstoreTarget is the production Target: it appends each legacy Flow event
// to the run store as one legacy.flow_event (CA-2), exactly once per source
// event.
//
// The de-duplication is the run store's (runstore.ProjectLegacyEventTx keyed by
// source_store + source_event_id), and it happens inside the same run-store
// transaction that allocates the project sequence. That is what makes the
// projector's crash window harmless: when the Target call committed but the
// local outbox mark did not, the next round calls Project again and gets the
// same event back — no second event, no second sequence number, no second
// outbox row.
//
// The event it writes: type legacy.flow_event, project-scoped (no run_id),
// occurred_at = the Flow event's own timestamp, actor {system,
// legacy-flow-projector, floweng}, payload {source_store, source_event_id,
// source_type (the legacy event name), flow_id, stage_id, message}.
type RunstoreTarget struct {
	store        *runstore.Store
	projectRef   ProjectRefResolver
	destinations func(projectID string) []string
}

// NewRunstoreTarget builds the run-store Target. store and projectRef are
// required. destinations, when non-nil, names the run-store outbox
// destinations to queue for a newly projected event of a project (a
// re-projection of an existing event queues nothing).
func NewRunstoreTarget(store *runstore.Store, projectRef ProjectRefResolver, destinations func(projectID string) []string) (*RunstoreTarget, error) {
	if store == nil {
		return nil, errors.New("flowprojection: NewRunstoreTarget needs a run store")
	}
	if projectRef == nil {
		return nil, errors.New("flowprojection: NewRunstoreTarget needs a project ref resolver")
	}
	return &RunstoreTarget{store: store, projectRef: projectRef, destinations: destinations}, nil
}

// legacyFlowPayload is the payload of a projected legacy Flow event.
type legacyFlowPayload struct {
	SourceStore   string `json:"source_store"`
	SourceEventID string `json:"source_event_id"`
	SourceType    string `json:"source_type"`
	FlowID        string `json:"flow_id"`
	StageID       string `json:"stage_id"`
	Message       string `json:"message"`
}

// Project implements Target.
//
// The project snapshot is resolved before the run-store transaction opens, so
// the run store's write lock is never held while the legacy project store is
// read. The snapshot is then upserted in the same transaction as the event.
//
// Error classes: an event the run store refuses on its face (blank source id,
// an input AppendEventTx rejects, a source id already projected as a different
// project or type) or a project the legacy store does not know is wrapped in
// PermanentError, because no retry can change the answer and the projector
// should dead-letter the row where an operator can see it. Everything else — a
// busy database, a closed store, a resolver that could not reach the legacy
// store, a lost projection race — is returned as is, so the projector backs
// off and retries.
func (t *RunstoreTarget) Project(ctx context.Context, ev floweng.LegacyOutboxEvent) (string, error) {
	ref, err := t.projectRef(ctx, ev.ProjectID)
	if err != nil {
		if errors.Is(err, ErrUnknownProject) {
			return "", &PermanentError{Err: err}
		}
		return "", fmt.Errorf("resolve legacy project %s: %w", ev.ProjectID, err)
	}
	if ref.ProjectID != ev.ProjectID {
		return "", &PermanentError{Err: fmt.Errorf("project ref resolver answered project %q for %q", ref.ProjectID, ev.ProjectID)}
	}

	identity, err := json.Marshal(run.ExecutionIdentity{
		ProjectID: ev.ProjectID,
		Actor:     run.Actor{Type: run.ActorTypeSystem, ID: legacyFlowActorID, Source: LegacyFlowSourceStore},
	})
	if err != nil {
		return "", &PermanentError{Err: fmt.Errorf("encode identity of legacy flow event %s: %w", ev.SourceEventID, err)}
	}
	payload, err := json.Marshal(legacyFlowPayload{
		SourceStore:   LegacyFlowSourceStore,
		SourceEventID: ev.SourceEventID,
		SourceType:    ev.Type,
		FlowID:        ev.FlowID,
		StageID:       ev.StageID,
		Message:       ev.Message,
	})
	if err != nil {
		return "", &PermanentError{Err: fmt.Errorf("encode payload of legacy flow event %s: %w", ev.SourceEventID, err)}
	}
	var destinations []string
	if t.destinations != nil {
		destinations = t.destinations(ev.ProjectID)
	}
	in := runstore.EventInput{
		ProjectID:    ev.ProjectID,
		Type:         string(run.EventLegacyFlowEvent),
		OccurredAt:   ev.OccurredAt,
		Identity:     identity,
		Payload:      payload,
		Destinations: destinations,
	}
	src := runstore.LegacySource{Store: LegacyFlowSourceStore, EventID: ev.SourceEventID}

	var eventID string
	err = t.store.WithTx(ctx, func(ctx context.Context, tx runstore.Tx) error {
		if err := runstore.UpsertProjectRef(ctx, tx, ref); err != nil {
			return err
		}
		event, _, err := runstore.ProjectLegacyEventTx(ctx, tx, src, in)
		if err != nil {
			return err
		}
		eventID = event.ID
		return nil
	})
	if err != nil {
		if errors.Is(err, runstore.ErrInvalidLegacySource) ||
			errors.Is(err, runstore.ErrInvalidEvent) ||
			errors.Is(err, runstore.ErrInvalidRecord) ||
			errors.Is(err, runstore.ErrLegacySourceConflict) {
			return "", &PermanentError{Err: err}
		}
		return "", err
	}
	return eventID, nil
}
