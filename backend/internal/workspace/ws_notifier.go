package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"time"

	"github.com/codeflow/backend/internal/websocket"
)

// WSNotifier publishes workspace change events on the global WebSocket hub. It
// is the production Notifier adapter; the Watcher itself never imports websocket
// (mirrors floweng.WSNotifier). Wiring lives in main and is out of this package.
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

// OnWorkspaceEvent implements Notifier by fanning the event out on the global
// TopicWorkspaceEvent topic and on the per-root topic.
func (n *WSNotifier) OnWorkspaceEvent(root string, ev Event) {
	if n == nil || n.Hub == nil {
		return
	}
	msg := &websocket.Message{
		Type:    websocket.MessageType(websocket.TopicWorkspaceEvent),
		Content: string(ev.Type),
		Data: map[string]interface{}{
			"root":      root,
			"path":      ev.Path,
			"change":    string(ev.Type),
			"timestamp": ev.Ts.UTC().Format(time.RFC3339),
		},
	}
	n.Hub.BroadcastToTopic(websocket.TopicWorkspaceEvent, msg)
	n.Hub.BroadcastToTopic(WorkspaceTopicForRoot(root), msg)
}

// WorkspaceTopicForRoot builds the per-root fan-out topic. Filesystem roots
// contain separators, spaces and drive letters, so the cleaned absolute path is
// hashed. Mirrors floweng's "flow:project:{id}" convention as
// "workspace:root:{hash}".
func WorkspaceTopicForRoot(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	return "workspace:root:" + hex.EncodeToString(sum[:8])
}
