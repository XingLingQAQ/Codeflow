package websocket

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/api/middleware"
	"github.com/gin-gonic/gin"
	gorillaws "github.com/gorilla/websocket"
)

const (
	wsTestToken  = "0123456789abcdef0123456789abcdef"
	wsTestOrigin = "http://localhost:3000"
)

func TestWebSocketAuthenticationAndOrigin(t *testing.T) {
	server, _ := newScopedWSTestServer(t, "session-1")
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream"

	_, response, err := gorillaws.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{wsTestOrigin}})
	if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token response=%v err=%v", statusCode(response), err)
	}
	_ = response.Body.Close()

	badOriginHeader := http.Header{
		"Origin":        []string{"http://localhost:3001"},
		"Authorization": []string{"Bearer " + wsTestToken},
	}
	_, response, err = gorillaws.DefaultDialer.Dial(wsURL, badOriginHeader)
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("bad origin response=%v err=%v", statusCode(response), err)
	}
	_ = response.Body.Close()

	validHeader := http.Header{
		"Origin":        []string{wsTestOrigin},
		"Authorization": []string{"Bearer " + wsTestToken},
	}
	conn, response, err := gorillaws.DefaultDialer.Dial(wsURL, validHeader)
	if err != nil {
		t.Fatalf("valid bearer websocket rejected: status=%v err=%v", statusCode(response), err)
	}
	_ = conn.Close()
}

func TestWebSocketBrowserSubprotocolAndScopedTopics(t *testing.T) {
	server, hub := newScopedWSTestServer(t, "project:mine", "flow:project:mine")
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/stream"
	dialer := *gorillaws.DefaultDialer
	dialer.Subprotocols = []string{middleware.WebSocketProtocolV1, "codeflow.token." + wsTestToken}
	conn, response, err := dialer.Dial(wsURL, http.Header{"Origin": []string{wsTestOrigin}})
	if err != nil {
		t.Fatalf("subprotocol websocket rejected: status=%v err=%v", statusCode(response), err)
	}
	defer conn.Close()
	if conn.Subprotocol() != middleware.WebSocketProtocolV1 {
		t.Fatalf("selected protocol=%q", conn.Subprotocol())
	}

	if err := conn.WriteJSON(Message{
		Type:      MsgTypeSubscribe,
		SessionID: "project:other",
		Data:      map[string]interface{}{"topic": "flow:project:mine"},
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(25 * time.Millisecond)
	if got := hub.TopicSubscriberCount("flow:project:mine"); got != 0 {
		t.Fatalf("cross-scope message subscribed client: %d", got)
	}

	if err := conn.WriteJSON(Message{
		Type: MsgTypeSubscribe,
		Data: map[string]interface{}{"topic": "flow:project:other"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Message{
		Type: MsgTypeSubscribe,
		Data: map[string]interface{}{"topic": "flow:project:mine"},
	}); err != nil {
		t.Fatal(err)
	}
	waitForSubscriberCount(t, hub, "flow:project:mine", 1)
	if got := hub.TopicSubscriberCount("flow:project:other"); got != 0 {
		t.Fatalf("cross-project topic subscribed client: %d", got)
	}
}

func newScopedWSTestServer(t *testing.T, scopeID string, topics ...string) (*httptest.Server, *Hub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hub := NewHub()
	go hub.Run()
	policy := NewAccessPolicy(wsTestToken, []string{wsTestOrigin})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(ContextAccessPolicy, policy)
		c.Next()
	})
	router.GET("/stream", middleware.RequireAccessToken(wsTestToken), func(c *gin.Context) {
		HandleScopedWebSocket(hub, c, scopeID, topics...)
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server, hub
}

func waitForSubscriberCount(t *testing.T, hub *Hub, topic string, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if hub.TopicSubscriberCount(topic) == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("topic %q subscribers=%d want=%d", topic, hub.TopicSubscriberCount(topic), want)
}

func statusCode(response *http.Response) interface{} {
	if response == nil {
		return nil
	}
	return response.StatusCode
}
