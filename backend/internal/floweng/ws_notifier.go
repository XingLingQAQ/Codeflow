package floweng

import (
	"time"

	"github.com/codeflow/backend/internal/websocket"
)

// WSNotifier publishes flow events on the global WebSocket hub.
type WSNotifier struct {
	Hub *websocket.Hub
}

// NewWSNotifier uses hub or falls back to websocket.GetHub().
func NewWSNotifier(hub *websocket.Hub) *WSNotifier {
	if hub == nil {
		hub = websocket.GetHub()
	}
	return &WSNotifier{Hub: hub}
}

// OnFlowEvent implements EventNotifier.
func (n *WSNotifier) OnFlowEvent(flow *Flow, event FlowEvent) {
	if n == nil || n.Hub == nil || flow == nil {
		return
	}
	msg := &websocket.Message{
		Type:    websocket.MessageType("flow_event"),
		Content: event.Type,
		Data: map[string]interface{}{
			"flow_id":     flow.ID,
			"project_id":  flow.ProjectID,
			"flow_status": string(flow.Status),
			"event_id":    event.ID,
			"event_type":  event.Type,
			"stage_id":    event.StageID,
			"message":     event.Message,
			"timestamp":   event.Timestamp.UTC().Format(time.RFC3339),
		},
	}
	// Topic fan-out: clients must subscribe to TopicFlowEvent (or project-scoped topic).
	n.Hub.BroadcastToTopic(websocket.TopicFlowEvent, msg)
	if flow.ProjectID != "" {
		n.Hub.BroadcastToTopic("flow:project:"+flow.ProjectID, msg)
	}
}
