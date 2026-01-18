// Package websocket - WebSocket service for real-time conversation streaming
package websocket

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// MessageType 消息类型
type MessageType string

const (
	MsgTypeText       MessageType = "text"
	MsgTypeToolCall   MessageType = "tool_call"
	MsgTypeToolResult MessageType = "tool_result"
	MsgTypeThinking   MessageType = "thinking"
	MsgTypeError      MessageType = "error"
	MsgTypePing       MessageType = "ping"
	MsgTypePong       MessageType = "pong"
	MsgTypeSubscribe  MessageType = "subscribe"
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
	Send      chan []byte
	Hub       *Hub
	mu        sync.Mutex
}

// Hub WebSocket连接管理中心
type Hub struct {
	mu         sync.RWMutex
	clients    map[string]*Client
	sessions   map[string]map[string]*Client // sessionID -> clientID -> client
	register   chan *Client
	unregister chan *Client
	broadcast  chan *Message
}

// NewHub 创建Hub
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		sessions:   make(map[string]map[string]*Client),
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
		return true // 允许所有来源（生产环境应限制）
	},
}

// HandleWebSocket 处理WebSocket连接
func HandleWebSocket(hub *Hub, c *gin.Context) {
	sessionID := c.Param("sessionId")

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WS] Upgrade failed: %v", err)
		return
	}

	client := &Client{
		ID:        generateClientID(),
		Conn:      conn,
		SessionID: sessionID,
		Send:      make(chan []byte, 256),
		Hub:       hub,
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
	case MsgTypePong:
		// 心跳响应，不需要处理
	case MsgTypeSubscribe:
		// 订阅会话
		if msg.SessionID != "" && msg.SessionID != c.SessionID {
			c.Hub.mu.Lock()
			// 从旧会话移除
			if c.SessionID != "" {
				if sessionClients, ok := c.Hub.sessions[c.SessionID]; ok {
					delete(sessionClients, c.ID)
				}
			}
			// 添加到新会话
			c.SessionID = msg.SessionID
			if _, ok := c.Hub.sessions[c.SessionID]; !ok {
				c.Hub.sessions[c.SessionID] = make(map[string]*Client)
			}
			c.Hub.sessions[c.SessionID][c.ID] = c
			c.Hub.mu.Unlock()
		}
	case MsgTypeUnsubscribe:
		// 取消订阅
		c.Hub.mu.Lock()
		if c.SessionID != "" {
			if sessionClients, ok := c.Hub.sessions[c.SessionID]; ok {
				delete(sessionClients, c.ID)
			}
			c.SessionID = ""
		}
		c.Hub.mu.Unlock()
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
