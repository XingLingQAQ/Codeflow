package websocket

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
)

// live_test.go covers live.go: the deliverer that turns a dispatcher delivery
// into a wake-up of the subscriptions it concerns.
//
// The property that matters is a negative one: a delivery may only wake the
// subscriptions of its own project and its own run. Waking too much is a
// performance bug; NOT waking is not a correctness bug at all, because the poll
// interval guarantees delivery either way (§27.4: live events are read from the
// event store by cursor; a wake-up is only a latency optimization). So the test
// pins the boundary with a poll interval long enough that a subscription which
// was woken by the wrong delivery has no other way to deliver — and, for the same
// reason, a subscription that IS woken delivers immediately.

// longPollInterval is long enough that nothing in these tests can be delivered by
// polling: every frame a test sees arrived because a delivery woke the
// subscription.
const longPollInterval = time.Hour

// deliveryEvent builds a stored-event value for a delivery: the deliverer only
// reads the identity fields, so the fixture events are used as they come from the
// store.
func deliveryEvent(event runstore.Event) runstore.Delivery {
	return runstore.Delivery{
		OutboxID: "outbox-" + event.ID,
		EventID:  event.ID,
		Event:    event,
	}
}

// TestHubDelivererWakesOnlyItsOwnScopes is the boundary of the wake-up: a
// delivery for project A's run wakes the run subscription and the project
// subscription of project A, and nothing else — not project B's stream, not the
// other run of project A.
func TestHubDelivererWakesOnlyItsOwnScopes(t *testing.T) {
	f := newReplayFixture(t)
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: longPollInterval,
	})

	projectA := projectStream(t, srv, testProjectA)
	projectA.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")
	projectA.drainReplay("sub_1", "project:"+testProjectA)

	runA := projectStream(t, srv, testProjectA)
	runA.subscribe(ResourceTypeRun, testRunA, 0, "sub_1")
	runA.drainReplay("sub_1", "run:"+testRunA)

	projectB := projectStream(t, srv, testProjectB)
	projectB.subscribe(ResourceTypeProject, testProjectB, 0, "sub_1")
	projectB.drainReplay("sub_1", "project:"+testProjectB)

	runB := projectStream(t, srv, testProjectB)
	runB.subscribe(ResourceTypeRun, testRunB, 0, "sub_1")
	runB.drainReplay("sub_1", "run:"+testRunB)

	// A run event of project A's run: project A (the project stream) and run A must
	// receive it, and neither of project B's connections may.
	event := f.runEvent(testProjectA, testRunA)
	deliverer := NewHubDeliverer(srv.hub)
	if err := deliverer.Deliver(context.Background(), deliveryEvent(event)); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	projectA.expectEventSequence("project:"+testProjectA, event.ProjectSeq)
	if event.RunSeq == nil {
		t.Fatal("the fixture run event has no run sequence")
	}
	runA.expectEventSequence("run:"+testRunA, *event.RunSeq)
	expectNoFrame(t, projectB)
	expectNoFrame(t, runB)
}

// TestHubDelivererWakesProjectSubscriptionsForRunlessEvents is the project-level
// half: an event with no run (a project fact such as a legacy flow event) wakes
// the project's subscriptions and nothing that is scoped to a run.
func TestHubDelivererWakesProjectSubscriptionsForRunlessEvents(t *testing.T) {
	f := newReplayFixture(t)
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: longPollInterval,
	})

	projectA := projectStream(t, srv, testProjectA)
	projectA.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")
	projectA.drainReplay("sub_1", "project:"+testProjectA)

	runA := projectStream(t, srv, testProjectA)
	runA.subscribe(ResourceTypeRun, testRunA, 0, "sub_1")
	runA.drainReplay("sub_1", "run:"+testRunA)

	event := f.projectEvent(testProjectA)
	if event.RunID != nil {
		t.Fatalf("the fixture event carries run %q, but this test is about a runless event", *event.RunID)
	}
	if err := NewHubDeliverer(srv.hub).Deliver(context.Background(), deliveryEvent(event)); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	projectA.expectEventSequence("project:"+testProjectA, event.ProjectSeq)
	expectNoFrame(t, runA)
}

// TestHubDelivererIsSafeWithoutASubscriberOrAHub pins the two cases the
// dispatcher will hit in production: a delivery for a project nobody is watching
// (the common case — every event is delivered, most have no subscriber), and a
// hub that was never constructed. Both are no-ops, and neither panics.
func TestHubDelivererIsSafeWithoutASubscriberOrAHub(t *testing.T) {
	f := newReplayFixture(t)
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: longPollInterval,
	})
	// A connection to project B, so the hub is not empty while project A's events
	// are delivered.
	projectB := projectStream(t, srv, testProjectB)
	projectB.subscribe(ResourceTypeProject, testProjectB, 0, "sub_1")
	projectB.drainReplay("sub_1", "project:"+testProjectB)

	for _, deliverer := range []*HubDeliverer{NewHubDeliverer(srv.hub), NewHubDeliverer(nil), nil} {
		for _, event := range []runstore.Event{
			f.projectEvent(testProjectA),
			f.runEvent(testProjectA, testRunA),
			// An event with no identity at all: the deliverer must not assume the
			// event it is handed is well formed.
			{Type: string(run.EventServerRestart)},
		} {
			if err := deliverer.Deliver(context.Background(), deliveryEvent(event)); err != nil {
				t.Fatalf("Deliver(%+v) = %v, want nil", event, err)
			}
		}
	}
	expectNoFrame(t, projectB)
}

// TestHubDelivererDoesNotWaitForTheSubscription pins the deliverer's contract with
// the dispatcher: Deliver returns as soon as it has signalled, whether or not the
// subscription has done anything about it. A subscription that is still in its
// replay must not hold the dispatcher.
func TestHubDelivererDoesNotWaitForTheSubscription(t *testing.T) {
	f := newReplayFixture(t)
	for i := 0; i < 3; i++ {
		f.projectEvent(testProjectA)
	}
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: longPollInterval,
	})
	conn := projectStream(t, srv, testProjectA)
	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")

	// Deliver while the subscription is between subscribed and replay_finished.
	event := f.projectEvent(testProjectA)
	done := make(chan error, 1)
	go func() { done <- NewHubDeliverer(srv.hub).Deliver(context.Background(), deliveryEvent(event)) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Deliver: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Deliver blocked on a subscription")
	}

	// The subscription still delivers everything, in order: the replay window
	// (which may already include the event the delivery announced, because it was
	// appended before the snapshot was read) and then a fresh live event.
	out := conn.drainReplay("sub_1", "project:"+testProjectA)
	assertContiguousFrom(t, out.Sequences, 1)
	if out.HighWatermark < 3 {
		t.Fatalf("high watermark = %d, want at least the 3 events appended before the subscribe", out.HighWatermark)
	}

	live := f.projectEvent(testProjectA)
	if err := NewHubDeliverer(srv.hub).Deliver(context.Background(), deliveryEvent(live)); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	conn.expectEventSequence("project:"+testProjectA, live.ProjectSeq)
}

// expectNoFrame fails if a frame arrives within the window. The window is short
// because the check is a negative one: the subscription could only deliver by
// polling, and its poll interval is an hour.
//
// It is a TERMINAL check: gorilla treats a read deadline as a permanent error for
// the connection, so nothing may be read from this connection afterwards.
func expectNoFrame(t *testing.T, c *wsTestConn) {
	t.Helper()
	if err := c.conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, raw, err := c.conn.ReadMessage()
	if err == nil {
		var shape struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(raw, &shape)
		t.Fatalf("unexpected frame %s (type %s)", raw, shape.Type)
	}
	if !isTimeout(err) {
		t.Fatalf("connection ended while waiting for silence: %v", err)
	}
}

// isTimeout reports whether err is a read deadline expiry.
func isTimeout(err error) bool {
	type timeout interface{ Timeout() bool }
	if err == nil {
		return false
	}
	if t, ok := err.(timeout); ok {
		return t.Timeout()
	}
	return false
}
