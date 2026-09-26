// Wake-ups for the live phase (T1.12.b).
//
// §27.4 gives the live phase two ways to learn that its scope has new events: a
// wake-up from the delivery path, or reading the event store again. This file is
// the first half of that pair — a runstore.Deliverer that turns one delivery into
// "the subscriptions following this project (and this run) should read again".
//
// Two properties matter, and both come from the same decision — the delivery is a
// hint, never a payload:
//
//   - Deliver never forwards the delivered event to a connection. What the client
//     receives is read from the store by cursor (subscription.go:pump), so a
//     delivery that is late, duplicated or missing cannot create a gap or an
//     out-of-order frame. That is also why Deliver always returns nil: there is
//     nothing to retry. The outbox row is acknowledged because the wake-up it
//     asked for has happened; whether any subscription acted on it is not the
//     dispatcher's business.
//   - Deliver never blocks. It signals a buffered channel per subscription and
//     returns, so a slow client cannot slow the dispatcher down: the slow-client
//     rule (close 1013, subscription.go) lives in the write path, not here.
//
// T1.04 registers this as the dispatcher's destination. Until it does, the poll
// interval is what delivers live events, which is why the ordered subscription
// works today with no dispatcher at all.
package websocket

import (
	"context"

	"github.com/codeflow/backend/internal/runstore"
)

// HubDeliverer implements runstore.Deliverer for the WebSocket hub: one delivery
// wakes the subscriptions that follow the delivered event's scope.
type HubDeliverer struct {
	hub *Hub
}

// compile-time proof that the dispatcher can use this value as a destination.
var _ runstore.Deliverer = (*HubDeliverer)(nil)

// NewHubDeliverer returns the deliverer that wakes subscriptions on hub.
//
// A nil hub is accepted and makes every delivery a no-op, so a caller that has no
// hub yet (or a test that only wants the interface) cannot panic the dispatcher
// from inside a delivery.
func NewHubDeliverer(hub *Hub) *HubDeliverer {
	return &HubDeliverer{hub: hub}
}

// Deliver implements runstore.Deliverer.
//
// It wakes the project scope the event belongs to and, when the event has a run,
// the run scope too: a project subscriber follows project_seq and a run subscriber
// run_seq, and both may be waiting on the same event (§27.4: the same event is
// readable through both counters, each cursor advancing on its own).
//
// The event's own ids decide the scopes — never the outbox row's destination
// string, which is this process's own routing label and could name a scope that
// does not exist.
func (d *HubDeliverer) Deliver(_ context.Context, delivery runstore.Delivery) error {
	if d == nil || d.hub == nil {
		return nil
	}
	event := delivery.Event
	if event.ProjectID != "" {
		d.hub.wakeScope(Scope{Kind: ResourceTypeProject, ID: event.ProjectID}.String())
	}
	if event.RunID != nil && *event.RunID != "" {
		d.hub.wakeScope(Scope{Kind: ResourceTypeRun, ID: *event.RunID}.String())
	}
	return nil
}

// wakeScope is defined in subscription.go, next to the table it walks.
