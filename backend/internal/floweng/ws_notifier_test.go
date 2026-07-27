package floweng

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/websocket"
)

type captureNotifier struct {
	events []FlowEvent
}

func (c *captureNotifier) OnFlowEvent(flow *Flow, event FlowEvent) {
	c.events = append(c.events, event)
}

func TestEventNotifierOnCreate(t *testing.T) {
	cap := &captureNotifier{}
	e := NewInMemoryEngine(nil)
	e.SetEventNotifier(cap)
	if _, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"}); err != nil {
		t.Fatal(err)
	}
	if len(cap.events) == 0 || cap.events[0].Type != "flow.created" {
		t.Fatalf("events=%+v", cap.events)
	}
}

func TestWSNotifierDoesNotPanic(t *testing.T) {
	// ensure hub starts
	hub := websocket.GetHub()
	n := NewWSNotifier(hub)
	e := NewInMemoryEngine(nil)
	e.SetEventNotifier(n)
	if _, err := e.Create(context.Background(), &CreateFlowRequest{ProjectID: "ws"}); err != nil {
		t.Fatal(err)
	}
	// give hub a tick
	time.Sleep(20 * time.Millisecond)
}

// OnFlowEvent must fan out to both the global flow-event topic and the
// project-scoped topic. BroadcastToTopic delivers synchronously, so a hub that
// is not running still routes to fake subscribed clients.
func TestWSNotifierFansOutToFlowAndProjectTopics(t *testing.T) {
	hub := websocket.NewHub()
	const projectID = "proj-fanout"

	flowClient := &websocket.Client{ID: "flow-topic-client", Hub: hub, Send: make(chan []byte, 4)}
	projClient := &websocket.Client{ID: "proj-topic-client", Hub: hub, Send: make(chan []byte, 4)}
	hub.SubscribeTopic(flowClient, websocket.TopicFlowEvent)
	hub.SubscribeTopic(projClient, "flow:project:"+projectID)

	n := NewWSNotifier(hub)
	flow := &Flow{ID: "flow-1", ProjectID: projectID, Status: FlowStatusActive}
	n.OnFlowEvent(flow, FlowEvent{ID: "e1", Type: "flow.created", Message: "created", Timestamp: time.Now().UTC()})

	// Global flow-event topic: decode and verify the payload carries the event.
	select {
	case raw := <-flowClient.Send:
		var msg websocket.Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("decode flow-topic message: %v", err)
		}
		if msg.Data["event_type"] != "flow.created" || msg.Data["project_id"] != projectID {
			t.Fatalf("flow-topic payload mismatch: %+v", msg.Data)
		}
	default:
		t.Fatal("expected a message on the flow-event topic")
	}

	// Project-scoped topic must also receive it.
	select {
	case raw := <-projClient.Send:
		if len(raw) == 0 {
			t.Fatal("empty project-topic payload")
		}
	default:
		t.Fatal("expected a message on the project-scoped topic")
	}
}
