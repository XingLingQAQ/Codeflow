package floweng

// Binding resolution and the legacy binding_id backfill (T3.01.a group 3).
//
// A Flow records the workspace binding it runs on (binding_id), so the runtime
// database of T3.01.b can prove which directory a flow's work belonged to even
// after a project is rebound. The engine does not know the project package —
// project already imports floweng, and the import cannot go back — so the
// binding of "the project this flow belongs to" is provided by the startup
// code, which owns both.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// BindingResolver answers "which workspace binding is this project's current
// primary one?". It is implemented by the project service and injected into the
// engine at startup (SetBindingResolver).
//
// The contract:
//
//   - A non-empty id is the project's active primary binding.
//   - ("", nil) means the project has no primary binding right now. It is not
//     an error: a Flow records the empty binding, and a backfill reports the
//     project instead of failing.
//   - A non-nil error means the answer could not be produced. Flow creation
//     fails and writes nothing (fail closed); a backfill stops and returns the
//     error, keeping what it already wrote so the next startup can continue.
type BindingResolver interface {
	ActivePrimaryBindingID(ctx context.Context, projectID string) (string, error)
}

// SetBindingResolver attaches or replaces the binding resolver. It is called by
// the startup code after the project service exists. Without one, flows are
// created with an empty binding_id, which is what every caller before T3.01.a
// got and what tests that do not care about bindings keep getting.
func (e *InMemoryEngine) SetBindingResolver(r BindingResolver) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.bindings = r
}

// resolveBinding asks the resolver for the project's primary binding. It reads
// the resolver under e.mu and then calls it outside the lock, because the
// resolver is external code (the project service) and may not re-enter the
// engine. A nil resolver returns an empty binding, which is "unknown", not an
// error.
func (e *InMemoryEngine) resolveBinding(ctx context.Context, projectID string) (string, error) {
	e.mu.Lock()
	resolver := e.bindings
	e.mu.Unlock()
	if resolver == nil {
		return "", nil
	}
	return resolver.ActivePrimaryBindingID(ctx, projectID)
}

// FlowBindingBackfillMigration is the receipt name of the binding backfill.
// Unlike the store migrations, this receipt is not written by opening a
// database: the resolver only becomes available when the startup code installs
// it, so the backfill runs as an explicit step (BackfillFlowBindings) and
// records its own receipt when it actually changed something.
const FlowBindingBackfillMigration = "t301a_binding_id_backfill"

// flowBindingBackfillNote is the honest caveat of the binding receipt: only
// flows that are still running are bound now. A completed or aborted flow was
// bound to whatever the project's binding was at the time, and that is not
// knowable from the database, so those stay empty.
const flowBindingBackfillNote = "binding_id is filled for unfinished (active/suspended) flows only, from the project's current primary binding. Finished (completed/aborted) flows were bound to the binding in force at their time, which is not recorded, and stay empty."

// FlowBindingBackfilledFlow is one flow the binding backfill filled in.
type FlowBindingBackfilledFlow struct {
	FlowID     string `json:"flow_id"`
	ProjectID  string `json:"project_id"`
	BindingID  string `json:"binding_id"`
	Revision   int64  `json:"revision"`
	FlowStatus string `json:"status"`
}

// FlowBindingUnresolvedProject is one project the backfill could not bind: it
// has no active primary binding, so every bound-so-far flow of it keeps an empty
// binding_id. It is a normal state (an unbound project), not an error.
type FlowBindingUnresolvedProject struct {
	ProjectID string   `json:"project_id"`
	FlowIDs   []string `json:"flow_ids"`
	Reason    string   `json:"reason"`
}

// FlowBindingBackfillReceipt is the receipt of the binding backfill, recorded
// in flow_store_migrations once per database (the flow store's own receipt
// table — the only durable record this package owns).
type FlowBindingBackfillReceipt struct {
	Note       string                         `json:"note"`
	Backfilled []FlowBindingBackfilledFlow    `json:"backfilled"`
	Unresolved []FlowBindingUnresolvedProject `json:"unresolved"`
}

// BackfillFlowBindings fills binding_id on the stored flows that are still
// unfinished and do not have one, and returns the receipt of what it did.
//
// Only flows with status active or suspended are considered: those are the
// flows that may still run work on a binding, so binding them to the project's
// current primary binding is meaningful and correct. A completed or aborted
// flow ran on the binding in force at its time, which the database does not
// record; it stays empty rather than being pointed at a binding it never used.
//
// One resolver call is made per project, not per flow, and a project with no
// primary binding is listed in the receipt instead of failing. A resolver error
// stops the backfill and is returned: everything written before it stays
// written, and the next startup continues from there (the same flows are found
// again because their binding_id is still empty).
//
// Each changed flow is written through writeFlowDocument: the document gains
// its binding_id member, the mirror column is filled from it, and the revision
// moves by one. As with the template-revision backfill, UpdatedAt is preserved
// (a system migration is not user activity) and no event or outbox row is
// produced (nothing happened to the flow). Calling it again after a successful
// run writes nothing, and records nothing.
func (e *InMemoryEngine) BackfillFlowBindings(ctx context.Context) (*FlowBindingBackfillReceipt, error) {
	// The resolver is external code (the project service) and is called outside
	// e.mu; the scan that decides what to process is under it. Each write below
	// re-reads the flow under the lock and re-checks the condition, so a flow
	// that moved between the scan and the write (finished, or bound by another
	// caller) is left alone rather than overwritten.
	e.mu.Lock()
	if e.bindings == nil {
		e.mu.Unlock()
		return nil, fmt.Errorf("backfill flow bindings: no binding resolver is attached")
	}
	resolver := e.bindings
	flows, err := e.store.List("")
	if err != nil {
		e.mu.Unlock()
		return nil, fmt.Errorf("backfill flow bindings: list flows: %w", err)
	}
	e.mu.Unlock()

	receipt := &FlowBindingBackfillReceipt{Note: flowBindingBackfillNote}
	byProject := map[string][]string{}
	projectIDs := []string{}
	for _, f := range flows {
		if f == nil || f.BindingID != "" {
			continue
		}
		if f.Status != FlowStatusActive && f.Status != FlowStatusSuspended {
			continue
		}
		if _, seen := byProject[f.ProjectID]; !seen {
			projectIDs = append(projectIDs, f.ProjectID)
		}
		byProject[f.ProjectID] = append(byProject[f.ProjectID], f.ID)
	}
	sort.Strings(projectIDs)

	for _, projectID := range projectIDs {
		group := byProject[projectID]
		sort.Strings(group)
		bindingID, err := resolver.ActivePrimaryBindingID(ctx, projectID)
		if err != nil {
			if err := e.recordBindingBackfillReceipt(receipt); err != nil {
				return receipt, err
			}
			return receipt, fmt.Errorf("backfill flow bindings: project %s: %w", projectID, err)
		}
		if bindingID == "" {
			receipt.Unresolved = append(receipt.Unresolved, FlowBindingUnresolvedProject{
				ProjectID: projectID,
				FlowIDs:   group,
				Reason:    "the project has no active primary binding",
			})
			continue
		}
		e.mu.Lock()
		for _, flowID := range group {
			flow, err := e.store.Get(flowID)
			if err != nil {
				e.mu.Unlock()
				return receipt, fmt.Errorf("backfill flow bindings: flow %s: %w", flowID, err)
			}
			if flow.BindingID != "" || (flow.Status != FlowStatusActive && flow.Status != FlowStatusSuspended) {
				continue
			}
			flow.BindingID = bindingID
			if err := e.putPreservingUpdatedAt(flow); err != nil {
				e.mu.Unlock()
				return receipt, fmt.Errorf("backfill flow bindings: flow %s: %w", flowID, err)
			}
			receipt.Backfilled = append(receipt.Backfilled, FlowBindingBackfilledFlow{
				FlowID:     flow.ID,
				ProjectID:  flow.ProjectID,
				BindingID:  bindingID,
				Revision:   flow.Revision,
				FlowStatus: string(flow.Status),
			})
		}
		e.mu.Unlock()
	}
	if err := e.recordBindingBackfillReceipt(receipt); err != nil {
		return receipt, err
	}
	return receipt, nil
}

// putPreservingUpdatedAt writes the flow through the store without stamping a
// new UpdatedAt and without queuing an outbox row for events the store has not
// seen. The durable store has a preserving path for migrations
// (writeFlowDocumentWith); a memoryStore write does neither of those things
// anyway, so this branches only for the durable store. The caller holds e.mu.
func (e *InMemoryEngine) putPreservingUpdatedAt(flow *Flow) error {
	if store, ok := e.store.(*SQLiteFlowStore); ok {
		tx, err := store.db.Begin()
		if err != nil {
			return fmt.Errorf("backfill flow bindings: begin: %w", err)
		}
		defer tx.Rollback()
		if err := writeFlowDocumentWith(tx, flow, flowWriteOptions{skipOutbox: true}); err != nil {
			return err
		}
		return tx.Commit()
	}
	return e.store.Put(flow)
}

// recordBindingBackfillReceipt records what the backfill did in
// flow_store_migrations, once. It is a best-effort durable record: a store
// without the table (the memory store) has nowhere to keep it, and the returned
// receipt is the answer either way. The receipt is written on every run that
// changed something, replacing nothing — the table's primary key is the name,
// and a second successful run changes nothing and writes nothing.
func (e *InMemoryEngine) recordBindingBackfillReceipt(receipt *FlowBindingBackfillReceipt) error {
	if receipt == nil || (len(receipt.Backfilled) == 0 && len(receipt.Unresolved) == 0) {
		return nil
	}
	store, ok := e.store.(*SQLiteFlowStore)
	if !ok {
		return nil
	}
	body, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("backfill flow bindings: encode receipt: %w", err)
	}
	if _, err := store.db.Exec(`
INSERT INTO flow_store_migrations (name, applied_at, receipt) VALUES (?, ?, ?)
ON CONFLICT(name) DO UPDATE SET applied_at=excluded.applied_at, receipt=excluded.receipt`,
		FlowBindingBackfillMigration, time.Now().UTC().UnixMilli(), string(body)); err != nil {
		return fmt.Errorf("backfill flow bindings: record receipt: %w", err)
	}
	return nil
}

// FindAppliedBindingBackfill returns the receipt of the last binding backfill,
// or nil when none has run. It is the read-only half of the record above.
func (s *SQLiteFlowStore) FindAppliedBindingBackfill() (*FlowStoreMigration, error) {
	return s.FindAppliedStoreMigration(FlowBindingBackfillMigration)
}
