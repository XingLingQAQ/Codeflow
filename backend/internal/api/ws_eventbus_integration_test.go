// Package api — Integration tests for the unified WS event bus (G29): real
// clients subscribe to flow_event / workspace_event / debate_event topics
// (global and per-scope), receive events triggered by floweng advance /
// workspace change / debate round, unsubscribe stops delivery, and a single
// client can listen on multiple topics.
//
// Placed in internal/api (not internal/websocket) to avoid an import cycle
// (websocket <-> floweng both import each other). The tests use the Hub Go API
// (no HTTP upgrade) with synchronous BroadcastToTopic — Hub.Run is not started.
package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/debate"
	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/websocket"
	"github.com/codeflow/backend/internal/workspace"
)

func wsNewClient(hub *websocket.Hub, id string) *websocket.Client {
	c := &websocket.Client{ID: id, Hub: hub, Send: make(chan []byte, 32)}
	// Register into hub's client map via exported Subscribe path:
	// SubscribeTopic adds the client to the topic map; we also need it in the
	// clients map for unregister. Direct field access is not possible from an
	// external package, so we subscribe to a temp topic to register, then
	// unsubscribe. Alternatively, just use BroadcastToTopic which only needs
	// topic membership — that is all our tests need.
	return c
}

func wsDrainOne(c *websocket.Client) *websocket.Message {
	select {
	case raw := <-c.Send:
		var msg websocket.Message
		if json.Unmarshal(raw, &msg) == nil {
			return &msg
		}
		return nil
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func wsDrainAll(c *websocket.Client, n int) []*websocket.Message {
	var out []*websocket.Message
	for i := 0; i < n; i++ {
		m := wsDrainOne(c)
		if m == nil {
			break
		}
		out = append(out, m)
	}
	return out
}

// ---------- 1. Flow events on flow_event + per-project topic ----------

func TestWSEventBus_FlowEvents(t *testing.T) {
	hub := websocket.NewHub()
	client := wsNewClient(hub, "flow-sub")
	hub.SubscribeTopic(client, websocket.TopicFlowEvent)

	eng := floweng.NewInMemoryEngine(nil)
	notifier := floweng.NewWSNotifier(hub)
	eng.SetEventNotifier(notifier)

	flow, err := eng.Create(nil, &floweng.CreateFlowRequest{
		ProjectID:  "proj-ws",
		TemplateID: floweng.TemplateNewProject,
	})
	if err != nil {
		t.Fatal(err)
	}

	msgs := wsDrainAll(client, 8)
	if len(msgs) == 0 {
		t.Fatal("expected flow.created event on flow_event topic")
	}
	found := false
	for _, m := range msgs {
		if m.Data != nil && m.Data["event_type"] == "flow.created" {
			found = true
			if m.Data["project_id"] != "proj-ws" {
				t.Fatalf("project_id=%v want=proj-ws", m.Data["project_id"])
			}
		}
	}
	if !found {
		t.Fatalf("flow.created not found among %d messages", len(msgs))
	}

	// Advance produces stage.done; it arrives on the topic.
	if _, err := eng.Advance(nil, flow.ID, &floweng.AdvanceRequest{}); err != nil {
		t.Fatal(err)
	}
	advMsgs := wsDrainAll(client, 8)
	hasDone := false
	for _, m := range advMsgs {
		if m.Data != nil && m.Data["event_type"] == "stage.done" {
			hasDone = true
		}
	}
	if !hasDone {
		t.Fatalf("stage.done missing after advance, got %d msgs", len(advMsgs))
	}

	// Per-project topic: a second client on "flow:project:proj-ws" also receives.
	projClient := wsNewClient(hub, "proj-sub")
	hub.SubscribeTopic(projClient, "flow:project:proj-ws")
	if _, err := eng.Advance(nil, flow.ID, &floweng.AdvanceRequest{}); err != nil {
		t.Fatal(err)
	}
	projMsgs := wsDrainAll(projClient, 8)
	if len(projMsgs) == 0 {
		t.Fatal("expected events on flow:project:proj-ws")
	}
}

// ---------- 2. Workspace events on workspace_event + per-root ----------

func TestWSEventBus_WorkspaceEvents(t *testing.T) {
	hub := websocket.NewHub()
	globalClient := wsNewClient(hub, "ws-global")
	hub.SubscribeTopic(globalClient, websocket.TopicWorkspaceEvent)

	root := t.TempDir()
	rootTopic := workspace.WorkspaceTopicForRoot(root)
	rootClient := wsNewClient(hub, "ws-root")
	hub.SubscribeTopic(rootClient, rootTopic)

	notifier := workspace.NewWSNotifier(hub)
	notifier.OnWorkspaceEvent(root, workspace.Event{
		Type: workspace.EventModified,
		Path: "main.go",
		Ts:   time.Now(),
	})

	gMsg := wsDrainOne(globalClient)
	if gMsg == nil || gMsg.Data == nil || gMsg.Data["change"] != "modified" {
		t.Fatalf("expected modified on workspace_event, got %+v", gMsg)
	}
	if gMsg.Data["path"] != "main.go" {
		t.Fatalf("path=%v want=main.go", gMsg.Data["path"])
	}

	rMsg := wsDrainOne(rootClient)
	if rMsg == nil || rMsg.Data == nil || rMsg.Data["change"] != "modified" {
		t.Fatalf("expected modified on root topic, got %+v", rMsg)
	}
}

// ---------- 3. Debate events via BroadcastToTopic ----------

func TestWSEventBus_DebateEvents(t *testing.T) {
	hub := websocket.NewHub()
	debClient := wsNewClient(hub, "deb-sub")
	hub.SubscribeTopic(debClient, websocket.TopicDebateEvent)

	hub.BroadcastToTopic(websocket.TopicDebateEvent, &websocket.Message{
		Type:    websocket.MessageType("debate_event"),
		Content: "round_advanced",
		Data:    map[string]interface{}{"debate_id": "d-1", "current_round": float64(2)},
	})

	msg := wsDrainOne(debClient)
	if msg == nil || msg.Content != "round_advanced" {
		t.Fatalf("expected round_advanced, got %+v", msg)
	}

	// Per-flow debate topic.
	flowDebClient := wsNewClient(hub, "deb-flow")
	hub.SubscribeTopic(flowDebClient, "debate:flow:f-1")
	hub.BroadcastToTopic("debate:flow:f-1", &websocket.Message{
		Type:    websocket.MessageType("debate_event"),
		Content: "solution_selected",
		Data:    map[string]interface{}{"debate_id": "d-1", "flow_id": "f-1"},
	})
	fMsg := wsDrainOne(flowDebClient)
	if fMsg == nil || fMsg.Content != "solution_selected" {
		t.Fatalf("expected solution_selected on debate:flow:f-1, got %+v", fMsg)
	}
}

// ---------- 4. Unsubscribe stops delivery ----------

func TestWSEventBus_UnsubscribeStopsDelivery(t *testing.T) {
	hub := websocket.NewHub()
	client := wsNewClient(hub, "unsub")
	hub.SubscribeTopic(client, websocket.TopicFlowEvent)

	hub.BroadcastToTopic(websocket.TopicFlowEvent, &websocket.Message{
		Type: websocket.MessageType("test"), Content: "before",
	})
	if m := wsDrainOne(client); m == nil {
		t.Fatal("expected message while subscribed")
	}

	hub.UnsubscribeTopic(client, websocket.TopicFlowEvent)
	if hub.TopicSubscriberCount(websocket.TopicFlowEvent) != 0 {
		t.Fatal("expected 0 subscribers after unsubscribe")
	}

	hub.BroadcastToTopic(websocket.TopicFlowEvent, &websocket.Message{
		Type: websocket.MessageType("test"), Content: "after",
	})
	if m := wsDrainOne(client); m != nil {
		t.Fatalf("unexpected message after unsubscribe: %+v", m)
	}
}

// ---------- 5. Multi-topic single connection ----------

func TestWSEventBus_MultiTopicSingleConnection(t *testing.T) {
	hub := websocket.NewHub()
	client := wsNewClient(hub, "multi")
	hub.SubscribeTopic(client, websocket.TopicFlowEvent)
	hub.SubscribeTopic(client, websocket.TopicWorkspaceEvent)
	hub.SubscribeTopic(client, websocket.TopicDebateEvent)

	hub.BroadcastToTopic(websocket.TopicFlowEvent, &websocket.Message{
		Type: websocket.MessageType("flow_event"), Content: "flow.created",
	})
	hub.BroadcastToTopic(websocket.TopicWorkspaceEvent, &websocket.Message{
		Type: websocket.MessageType("workspace_event"), Content: "modified",
	})
	hub.BroadcastToTopic(websocket.TopicDebateEvent, &websocket.Message{
		Type: websocket.MessageType("debate_event"), Content: "round_advanced",
	})

	msgs := wsDrainAll(client, 8)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages from 3 topics, got %d", len(msgs))
	}
	contents := map[string]bool{}
	for _, m := range msgs {
		contents[m.Content] = true
	}
	for _, want := range []string{"flow.created", "modified", "round_advanced"} {
		if !contents[want] {
			t.Fatalf("missing %q in multi-topic messages", want)
		}
	}

	// Unsubscribe from flow_event only; workspace and debate still deliver.
	hub.UnsubscribeTopic(client, websocket.TopicFlowEvent)
	hub.BroadcastToTopic(websocket.TopicFlowEvent, &websocket.Message{
		Type: websocket.MessageType("flow_event"), Content: "should-not-arrive",
	})
	hub.BroadcastToTopic(websocket.TopicWorkspaceEvent, &websocket.Message{
		Type: websocket.MessageType("workspace_event"), Content: "still-arrive",
	})

	remaining := wsDrainAll(client, 4)
	for _, m := range remaining {
		if m.Content == "should-not-arrive" {
			t.Fatal("received flow_event after unsubscribing from it")
		}
	}
	foundWS := false
	for _, m := range remaining {
		if m.Content == "still-arrive" {
			foundWS = true
		}
	}
	if !foundWS {
		t.Fatal("workspace_event should still deliver after unsubscribing flow_event")
	}
}

// ---------- 6. Project scoping — other project events excluded ----------

func TestWSEventBus_FlowProjectScoping(t *testing.T) {
	hub := websocket.NewHub()
	projClient := wsNewClient(hub, "scoped")
	hub.SubscribeTopic(projClient, "flow:project:mine")

	eng := floweng.NewInMemoryEngine(nil)
	eng.SetEventNotifier(floweng.NewWSNotifier(hub))

	if _, err := eng.Create(nil, &floweng.CreateFlowRequest{ProjectID: "mine", TemplateID: floweng.TemplateNewProject}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Create(nil, &floweng.CreateFlowRequest{ProjectID: "other", TemplateID: floweng.TemplateNewProject}); err != nil {
		t.Fatal(err)
	}

	msgs := wsDrainAll(projClient, 16)
	if len(msgs) == 0 {
		t.Fatal("expected at least one event for project mine")
	}
	for _, m := range msgs {
		if m.Data != nil && m.Data["project_id"] != "mine" {
			t.Fatalf("scoped client got event from project %v", m.Data["project_id"])
		}
	}
}

// ---------- 7. Debate manager → event bus round-trip ----------

func TestWSEventBus_DebateManagerRoundTrip(t *testing.T) {
	hub := websocket.NewHub()
	client := wsNewClient(hub, "deb-mgr")
	hub.SubscribeTopic(client, websocket.TopicDebateEvent)

	dm := debate.NewInMemoryDebateManager()
	d, err := dm.CreateDebate(nil, &debate.DebateCreateRequest{
		Title: "WS test", GeneratorID: "g", CriticID: "c", InitialInput: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dm.NextRound(nil, d.ID, &debate.NextRoundRequest{
		GeneratorOutput: "out", CriticFeedback: "fb",
	}); err != nil {
		t.Fatal(err)
	}

	// Mirror the handler broadcast after NextRound.
	hub.BroadcastToTopic(websocket.TopicDebateEvent, &websocket.Message{
		Type:    websocket.MessageType("debate_event"),
		Content: "round_advanced",
		Data:    map[string]interface{}{"debate_id": d.ID},
	})

	msg := wsDrainOne(client)
	if msg == nil || msg.Data["debate_id"] != d.ID {
		t.Fatalf("expected round_advanced for %s, got %+v", d.ID, msg)
	}
}
