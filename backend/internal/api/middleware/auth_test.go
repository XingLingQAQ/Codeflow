package middleware

import (
	"net/http"
	"testing"
)

const authTestToken = "0123456789abcdef0123456789abcdef"

func TestAccessTokenMatchesBearer(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/api/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer "+authTestToken)
	if !AccessTokenMatches(req, authTestToken) {
		t.Fatal("expected bearer token to authenticate")
	}
	req.Header.Set("Authorization", "bearer "+authTestToken)
	if !AccessTokenMatches(req, authTestToken) {
		t.Fatal("authentication scheme must be case-insensitive")
	}
}

func TestWebSocketProtocolTokenMatches(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/stream", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Protocol", WebSocketProtocolV1+", codeflow.token."+authTestToken)
	if !WebSocketProtocolTokenMatches(req, authTestToken) {
		t.Fatal("expected websocket subprotocol token to authenticate")
	}

	req.Header.Set("Sec-WebSocket-Protocol", "codeflow.token."+authTestToken)
	if WebSocketProtocolTokenMatches(req, authTestToken) {
		t.Fatal("token protocol without stable protocol must be rejected")
	}
	req.Header.Set("Sec-WebSocket-Protocol", WebSocketProtocolV1+", codeflow.token.wrong")
	if WebSocketProtocolTokenMatches(req, authTestToken) {
		t.Fatal("wrong websocket token must be rejected")
	}
}
