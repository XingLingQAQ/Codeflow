package floweng

import (
	"context"
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
