// Package websocket - WebSocket service for real-time conversation streaming
package websocket

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/api/middleware"
	backendhooks "github.com/codeflow/backend/internal/hooks"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// ContextAccessPolicy is the gin context key used by the API server to pass
// the validated sidecar trust policy to websocket handlers.
const ContextAccessPolicy = "codeflow.websocket.access_policy"

// MessageType 消息类型
type MessageType string

const (
	MsgTypeText        MessageType = "text"
	MsgTypeToolCall    MessageType = "tool_call"
	MsgTypeToolResult  MessageType = "tool_result"
	MsgTypeThinking    MessageType = "thinking"
	MsgTypeError       MessageType = "error"
	MsgTypePing        MessageType = "ping"
	MsgTypePong        MessageType = "pong"
	MsgTypeSubscribe   MessageType = "subscribe"
	MsgTypeUnsubscribe MessageType = "unsubscribe"
)

// Message WebSocket消息
type Message struct {
	Type      MessageType            `json:"type"`
	SessionID string                 `json:"session_id,omitempty"`
	AgentID   string                 `json:"agent_id,omitempty"`
	Content   string                 `json:"content,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp int64                  `json:"timestamp"`
}

// Client WebSocket客户端
type Client struct {
	ID        string
	Conn      *websocket.Conn
	SessionID string
	// topics are optional fan-out keys (e.g. "flow_event"); independent of SessionID.
	topics        map[string]struct{}
	Send          chan []byte
	Hub           *Hub
	mu            sync.Mutex
	allowedTopics map[string]struct{}
}

// AccessPolicy carries the process identity and the exact browser/topic
// allowlists authorized for websocket connections.
type AccessPolicy struct {
	token          string
	allowedOrigins map[string]struct{}
}

// NewAccessPolicy creates the immutable process identity and Origin policy.
// Resource topics are supplied separately by the route that owns the scope.
func NewAccessPolicy(token string, origins []string) *AccessPolicy {
	policy := &AccessPolicy{
		token:          token,
		allowedOrigins: make(map[string]struct{}, len(origins)),
	}
	for _, origin := range origins {
		policy.allowedOrigins[origin] = struct{}{}
	}
	return policy
}

// OriginAllowed requires an exact configured Origin value.
func (p *AccessPolicy) OriginAllowed(origin string) bool {
	if p == nil || origin == "" {
		return false
	}
	_, ok := p.allowedOrigins[origin]
	return ok
}

func (p *AccessPolicy) authorized(r *http.Request) bool {
	return p != nil && middleware.AccessTokenMatches(r, p.token)
}

// Hub WebSocket连接管理中心
type Hub struct {
	mu         sync.RWMutex
	clients    map[string]*Client
	sessions   map[string]map[string]*Client // sessionID -> clientID -> client
	topics     map[string]map[string]*Client // topic -> clientID -> client
	register   chan *Client
	unregister chan *Client
	broadcast  chan *Message
}

// NewHub 创建Hub
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		sessions:   make(map[string]map[string]*Client),
		topics:     make(map[string]map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *Message, 256),
	}
}

// Run 运行Hub
func (h *Hub) Run() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID] = client
			if client.SessionID != "" {
				if _, ok := h.sessions[client.SessionID]; !ok {
					h.sessions[client.SessionID] = make(map[string]*Client)
				}
				h.sessions[client.SessionID][client.ID] = client
			}
			h.mu.Unlock()
			log.Printf("[WS] Client %s connected (session: %s)", client.ID, client.SessionID)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.ID]; ok {
				delete(h.clients, client.ID)
				if client.SessionID != "" {
					if sessionClients, ok := h.sessions[client.SessionID]; ok {
						delete(sessionClients, client.ID)
						if len(sessionClients) == 0 {
							delete(h.sessions, client.SessionID)
						}
					}
				}
				for topic := range client.topics {
					if topicClients, ok := h.topics[topic]; ok {
						delete(topicClients, client.ID)
						if len(topicClients) == 0 {
							delete(h.topics, topic)
						}
					}
				}
				close(client.Send)
			}
			h.mu.Unlock()
			log.Printf("[WS] Client %s disconnected", client.ID)

		case msg := <-h.broadcast:
			h.broadcastMessage(msg)

		case <-ticker.C:
			h.pingAll()
		}
	}
}

// broadcastMessage 广播消息
func (h *Hub) broadcastMessage(msg *Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[WS] Failed to marshal message: %v", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	if msg.SessionID != "" {
		// 发送给特定会话的所有客户端
		if sessionClients, ok := h.sessions[msg.SessionID]; ok {
			for _, client := range sessionClients {
				select {
				case client.Send <- data:
				default:
					// 缓冲区满，跳过
				}
			}
		}
	} else {
		// 广播给所有客户端
		for _, client := range h.clients {
			select {
			case client.Send <- data:
			default:
			}
		}
	}
}

// pingAll 发送心跳
func (h *Hub) pingAll() {
	msg := &Message{
		Type:      MsgTypePing,
		Timestamp: time.Now().UnixMilli(),
	}
	data, _ := json.Marshal(msg)

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		select {
		case client.Send <- data:
		default:
		}
	}
}

// BroadcastToSession 向特定会话广播
func (h *Hub) BroadcastToSession(sessionID string, msg *Message) {
	msg.SessionID = sessionID
	msg.Timestamp = time.Now().UnixMilli()
	h.broadcast <- msg
}

// BroadcastAll 向所有客户端广播
func (h *Hub) BroadcastAll(msg *Message) {
	msg.Timestamp = time.Now().UnixMilli()
	h.broadcast <- msg
}

// TopicFlowEvent is the hub topic for floweng lifecycle events.
const TopicFlowEvent = "flow_event"

// TopicDebateEvent is the hub topic for debate lifecycle updates.
const TopicDebateEvent = "debate_event"

// TopicWorkspaceEvent is the hub topic for workspace file-change events
// (created/modified/deleted). Per-root fan-out uses workspace.WorkspaceTopicForRoot.
const TopicWorkspaceEvent = "workspace_event"

// BroadcastToTopic sends a message only to clients subscribed to topic.
// SessionID on the message is ignored for routing (topic fan-out only).
func (h *Hub) BroadcastToTopic(topic string, msg *Message) {
	if h == nil || topic == "" || msg == nil {
		return
	}
	msg.Timestamp = time.Now().UnixMilli()
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[WS] Failed to marshal topic message: %v", err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, client := range h.topics[topic] {
		select {
		case client.Send <- data:
		default:
		}
	}
}

// SubscribeTopic registers a client for a topic (no-op if already subscribed).
func (h *Hub) SubscribeTopic(client *Client, topic string) {
	if h == nil || client == nil {
		return
	}
	topic = normalizeTopic(topic)
	if topic == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if client.topics == nil {
		client.topics = make(map[string]struct{})
	}
	client.topics[topic] = struct{}{}
	if _, ok := h.topics[topic]; !ok {
		h.topics[topic] = make(map[string]*Client)
	}
	h.topics[topic][client.ID] = client
}

// UnsubscribeTopic removes a client from a topic.
func (h *Hub) UnsubscribeTopic(client *Client, topic string) {
	if h == nil || client == nil {
		return
	}
	topic = normalizeTopic(topic)
	if topic == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if client.topics != nil {
		delete(client.topics, topic)
	}
	if topicClients, ok := h.topics[topic]; ok {
		delete(topicClients, client.ID)
		if len(topicClients) == 0 {
			delete(h.topics, topic)
		}
	}
}

// TopicSubscriberCount returns how many clients listen on topic.
func (h *Hub) TopicSubscriberCount(topic string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.topics[normalizeTopic(topic)])
}

func normalizeTopic(topic string) string {
	return strings.TrimSpace(topic)
}

func topicFromMessage(msg *Message) string {
	if msg == nil {
		return ""
	}
	if msg.Data != nil {
		if t, ok := msg.Data["topic"].(string); ok {
			return normalizeTopic(t)
		}
	}
	return normalizeTopic(msg.Content)
}

// GetSessionClientCount 获取会话客户端数量
func (h *Hub) GetSessionClientCount(sessionID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if sessionClients, ok := h.sessions[sessionID]; ok {
		return len(sessionClients)
	}
	return 0
}

// GetTotalClientCount 获取总客户端数量
func (h *Hub) GetTotalClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		policy, _ := r.Context().Value(accessPolicyContextKey{}).(*AccessPolicy)
		return policy != nil && policy.OriginAllowed(r.Header.Get("Origin"))
	},
}

type accessPolicyContextKey struct{}

// HandleScopedWebSocket upgrades a connection whose resource scope has already
// been authorized by its HTTP route. The client cannot change that scope or
// subscribe to a topic outside the exact route-owned allowlist.
func HandleScopedWebSocket(hub *Hub, c *gin.Context, scopeID string, allowedTopics ...string) {
	policyValue, ok := c.Get(ContextAccessPolicy)
	policy, policyOK := policyValue.(*AccessPolicy)
	if !ok || !policyOK || !policy.authorized(c.Request) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "error": "unauthorized"})
		return
	}
	if !policy.OriginAllowed(c.GetHeader("Origin")) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "error": "origin not allowed"})
		return
	}
	if scopeID == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "error": "missing websocket scope"})
		return
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), accessPolicyContextKey{}, policy))

	responseHeader := http.Header{}
	if middleware.WebSocketProtocolTokenMatches(c.Request, policy.token) {
		responseHeader.Set("Sec-WebSocket-Protocol", middleware.WebSocketProtocolV1)
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, responseHeader)
	if err != nil {
		log.Printf("[WS] Upgrade failed: %v", err)
		return
	}

	client := &Client{
		ID:            generateClientID(),
		Conn:          conn,
		SessionID:     scopeID,
		Send:          make(chan []byte, 256),
		Hub:           hub,
		allowedTopics: topicSet(allowedTopics),
	}

	hub.register <- client

	go client.writePump()
	go client.readPump()
}

// readPump 读取消息
func (c *Client) readPump() {
	defer func() {
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(512 * 1024) // 512KB
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, data, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[WS] Read error: %v", err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if !c.bindMessageScope(&msg) {
			continue
		}

		c.handleMessage(&msg)
	}
}

// writePump 写入消息
func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			c.mu.Lock()
			err := c.Conn.WriteMessage(websocket.TextMessage, message)
			c.mu.Unlock()

			if err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage 处理客户端消息
func (c *Client) handleMessage(msg *Message) {
	switch msg.Type {
	case MsgTypeText:
		if backendhooks.HasHookManager() {
			if _, err := backendhooks.GetHookManager().HookOnUserInputSubmitted(context.Background(), msg.Content); err != nil {
				log.Printf("[WARN] websocket user-input-submitted hook failed: session=%s err=%v", msg.SessionID, err)
			}
		}
	case MsgTypePong:
		// 心跳响应，不需要处理
	case MsgTypeSubscribe:
		// Topic subscribe: {type:subscribe, data:{topic:"flow_event"}} or content as topic.
		if topic := topicFromMessage(msg); topic != "" && c.topicAllowed(topic) {
			c.Hub.SubscribeTopic(c, topic)
		}
	case MsgTypeUnsubscribe:
		if topic := topicFromMessage(msg); topic != "" {
			c.Hub.UnsubscribeTopic(c, topic)
		}
	}
	notifyMessageCompleteHook(msg)
}

func (c *Client) topicAllowed(topic string) bool {
	if c == nil {
		return false
	}
	_, ok := c.allowedTopics[normalizeTopic(topic)]
	return ok
}

func (c *Client) bindMessageScope(msg *Message) bool {
	if c == nil || msg == nil || c.SessionID == "" {
		return false
	}
	if msg.SessionID != "" && msg.SessionID != c.SessionID {
		return false
	}
	msg.SessionID = c.SessionID
	return true
}

func topicSet(topics []string) map[string]struct{} {
	out := make(map[string]struct{}, len(topics))
	for _, topic := range topics {
		if normalized := normalizeTopic(topic); normalized != "" {
			out[normalized] = struct{}{}
		}
	}
	return out
}

func notifyMessageCompleteHook(msg *Message) {
	if msg == nil || !backendhooks.HasHookManager() {
		return
	}
	if _, err := backendhooks.GetHookManager().Trigger(context.Background(), backendhooks.HookOnMessageComplete, msg); err != nil {
		log.Printf("[WARN] websocket message-complete hook failed: session=%s err=%v", msg.SessionID, err)
	}
}

// SendMessage 发送消息给客户端
func (c *Client) SendMessage(msg *Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	select {
	case c.Send <- data:
	default:
	}
}

// generateClientID 生成客户端ID
func generateClientID() string {
	return time.Now().Format("20060102150405") + "-" + randomString(8)
}

// randomString 生成随机字符串
func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
		time.Sleep(time.Nanosecond)
	}
	return string(b)
}

// 全局Hub实例
var defaultHub *Hub
var hubOnce sync.Once

// GetHub 获取Hub实例
func GetHub() *Hub {
	hubOnce.Do(func() {
		defaultHub = NewHub()
		go defaultHub.Run()
	})
	return defaultHub
}
