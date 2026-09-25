package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/codeflow/backend/internal/audit"
	"github.com/gin-gonic/gin"
)

// ContextAuthenticated is set on requests which passed the sidecar bearer
// token check. It is intentionally a boolean marker, not the token itself.
const ContextAuthenticated = "codeflow.authenticated"

// SidecarUserActorID 与 ActorSourceSidecarToken 标记 sidecar token 认证确立的
// 操作者身份（T0.10.c）：持有进程级 token 的调用方是本地桌面操作者，归因
// type=user。agent/integration 身份只能由服务端可信来源（执行上下文、
// webhook 验签）注入，绝不取自客户端请求头。
const (
	SidecarUserActorID      = "sidecar-user"
	ActorSourceSidecarToken = "sidecar-token"
)

const (
	WebSocketProtocolV1       = "codeflow.v1"
	webSocketTokenProtocolTag = "codeflow.token."
)

// TokenMatches compares two opaque access tokens without leaking the first
// mismatching byte through comparison timing.
func TokenMatches(got, expected string) bool {
	if got == "" || expected == "" {
		return false
	}
	gotHash := sha256.Sum256([]byte(got))
	wantHash := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) == 1
}

// BearerTokenMatches validates the exact Authorization header format used by
// the sidecar. Hashing both values first keeps the comparison fixed length.
func BearerTokenMatches(header, expected string) bool {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || expected == "" {
		return false
	}
	return TokenMatches(parts[1], expected)
}

// WebSocketProtocolTokenMatches authenticates browser WebSocket clients. The
// browser offers a stable protocol plus a token-bearing protocol; the server
// selects only the stable protocol so the token is not echoed in the response.
func WebSocketProtocolTokenMatches(r *http.Request, expected string) bool {
	if r == nil || !headerContainsToken(r.Header, "Connection", "upgrade") ||
		!headerContainsToken(r.Header, "Upgrade", "websocket") {
		return false
	}

	hasProtocol := false
	hasToken := false
	for _, value := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, protocol := range strings.Split(value, ",") {
			protocol = strings.TrimSpace(protocol)
			switch {
			case protocol == WebSocketProtocolV1:
				hasProtocol = true
			case strings.HasPrefix(protocol, webSocketTokenProtocolTag):
				hasToken = hasToken || TokenMatches(strings.TrimPrefix(protocol, webSocketTokenProtocolTag), expected)
			}
		}
	}
	return hasProtocol && hasToken
}

// AccessTokenMatches accepts the native Authorization header or the browser
// WebSocket subprotocol handshake.
func AccessTokenMatches(r *http.Request, expected string) bool {
	return r != nil && (BearerTokenMatches(r.Header.Get("Authorization"), expected) ||
		WebSocketProtocolTokenMatches(r, expected))
}

// RequireAccessToken protects an HTTP route group with the process-local
// sidecar token. CORS preflight is allowed to proceed; the actual request
// still has to carry the token.
func RequireAccessToken(expected string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}
		if !AccessTokenMatches(c.Request, expected) {
			c.AbortWithStatusJSON(401, gin.H{
				"success": false,
				"error":   "unauthorized",
			})
			return
		}
		c.Set(ContextAuthenticated, true)
		// 认证通过即注入可信操作者身份（T0.10.c）：下游审计/策略记录从这里取
		// actor，客户端经 X-Agent-ID 等请求头自报的身份不会被采信为 actor。
		c.Request = c.Request.WithContext(audit.WithActor(c.Request.Context(), audit.Actor{
			Type:   audit.ActorTypeUser,
			ID:     SidecarUserActorID,
			Source: ActorSourceSidecarToken,
		}))
		c.Next()
	}
}

func headerContainsToken(header http.Header, name, expected string) bool {
	for _, value := range header.Values(name) {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), expected) {
				return true
			}
		}
	}
	return false
}
