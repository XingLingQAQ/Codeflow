package websocket

import (
	"context"
	"testing"

	backendhooks "github.com/codeflow/backend/internal/hooks"
)

func TestClientHandleMessageTriggersMessageCompleteHook(t *testing.T) {
	mgr := backendhooks.NewHookManager()
	previous := backendhooks.GetHookManager()
	backendhooks.SetHookManager(mgr)
	t.Cleanup(func() {
		backendhooks.SetHookManager(previous)
	})

	var payload *Message
	err := mgr.Register(backendhooks.HookConfig{Name: "message-complete", Type: backendhooks.HookOnMessageComplete, Enabled: true}, func(ctx context.Context, value interface{}) (interface{}, error) {
		msg, ok := value.(*Message)
		if !ok {
			t.Fatalf("expected Message payload, got %#v", value)
		}
		payload = msg
		return value, nil
	})
	if err != nil {
		t.Fatalf("register hook: %v", err)
	}

	client := &Client{ID: "client-1", Hub: NewHub(), SessionID: "session-1"}
	client.handleMessage(&Message{Type: MsgTypeText, Content: "done", SessionID: "session-1"})

	if payload == nil || payload.Content != "done" || payload.SessionID != "session-1" {
		t.Fatalf("unexpected complete payload: %#v", payload)
	}
}

func TestTopicSubscribeAndBroadcast(t *testing.T) {
	hub := NewHub()
	client := &Client{ID: "c-topic", Hub: hub, Send: make(chan []byte, 4)}
	hub.clients[client.ID] = client

	client.handleMessage(&Message{
		Type: MsgTypeSubscribe,
		Data: map[string]interface{}{"topic": TopicFlowEvent},
	})
	if hub.TopicSubscriberCount(TopicFlowEvent) != 1 {
		t.Fatalf("expected 1 subscriber, got %d", hub.TopicSubscriberCount(TopicFlowEvent))
	}

	hub.BroadcastToTopic(TopicFlowEvent, &Message{Type: MessageType("flow_event"), Content: "flow.created"})
	select {
	case raw := <-client.Send:
		if len(raw) == 0 {
			t.Fatal("empty payload")
		}
	default:
		t.Fatal("expected topic message on client send channel")
	}

	client.handleMessage(&Message{
		Type:    MsgTypeUnsubscribe,
		Content: TopicFlowEvent,
	})
	if hub.TopicSubscriberCount(TopicFlowEvent) != 0 {
		t.Fatalf("expected 0 subscribers after unsubscribe, got %d", hub.TopicSubscriberCount(TopicFlowEvent))
	}
}

func TestClientHandleMessageTriggersUserInputSubmittedHook(t *testing.T) {
	mgr := backendhooks.NewHookManager()
	previous := backendhooks.GetHookManager()
	backendhooks.SetHookManager(mgr)
	t.Cleanup(func() {
		backendhooks.SetHookManager(previous)
	})

	var payload string
	err := mgr.Register(backendhooks.HookConfig{Name: "user-input", Type: backendhooks.HookOnUserInputSubmitted, Enabled: true}, func(ctx context.Context, value interface{}) (interface{}, error) {
		input, ok := value.(string)
		if !ok {
			t.Fatalf("expected string payload, got %#v", value)
		}
		payload = input
		return []backendhooks.MemoryMatch{{Content: "related", Similarity: 0.9, Source: "test"}}, nil
	})
	if err != nil {
		t.Fatalf("register hook: %v", err)
	}

	client := &Client{ID: "client-1", Hub: NewHub(), SessionID: "session-1"}
	client.handleMessage(&Message{Type: MsgTypeText, Content: "hello hook", SessionID: "session-1"})

	if payload != "hello hook" {
		t.Fatalf("expected submitted input payload, got %q", payload)
	}
}
