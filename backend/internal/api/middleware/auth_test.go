package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

const authTestToken = "0123456789abcdef0123456789abcdef"

// newAuthTestRouter mounts RequireAccessToken on a route so a case can assert
// the real middleware response (401) instead of a bare predicate.
func newAuthTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/projects", RequireAccessToken(authTestToken), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	return router
}

// TestQueryTokenNotAccepted pins the rule that credentials never ride in the
// query string: only the Authorization header and the WebSocket subprotocol
// handshake are accepted, so a leaked URL (logs, referrer, history) is inert.
func TestQueryTokenNotAccepted(t *testing.T) {
	router := newAuthTestRouter(t)

	for name, rawURL := range map[string]string{
		"query-token":        "/api/v1/projects?token=" + authTestToken,
		"query-access-token": "/api/v1/projects?access_token=" + authTestToken,
		"query-both":         "/api/v1/projects?token=" + authTestToken + "&access_token=" + authTestToken,
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, rawURL, nil))
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d want=%d body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
			}
		})
	}

	// Sanity: the same token over the header channel still authenticates, so
	// the 401s above are about the channel, not about a broken token.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects?token="+authTestToken, nil)
	req.Header.Set("Authorization", "Bearer "+authTestToken)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("header channel status=%d want=%d", w.Code, http.StatusOK)
	}
}

// TestPlainHTTPCannotUseWebSocketProtocolToken pins that the subprotocol
// channel is only reachable through a real upgrade handshake: a plain GET
// carrying the token subprotocol must not authenticate, and an Upgrade header
// without Connection: upgrade must not either.
func TestPlainHTTPCannotUseWebSocketProtocolToken(t *testing.T) {
	router := newAuthTestRouter(t)
	protocols := WebSocketProtocolV1 + ", codeflow.token." + authTestToken

	for name, headers := range map[string]map[string]string{
		"subprotocol-without-upgrade": {"Sec-WebSocket-Protocol": protocols},
		"upgrade-without-connection":  {"Upgrade": "websocket", "Sec-WebSocket-Protocol": protocols},
		"connection-without-upgrade":  {"Connection": "Upgrade", "Sec-WebSocket-Protocol": protocols},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			router.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d want=%d body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
			}
		})
	}

	// The same header set on a real handshake does authenticate (predicate
	// level: gin cannot perform the upgrade in this router).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Protocol", protocols)
	if !AccessTokenMatches(req, authTestToken) {
		t.Fatal("complete websocket handshake headers must authenticate")
	}
}

// TestRotatedTokenRejectsOld complements the server-restart rotation test in
// api/security_test.go at the middleware level: a token bound to one process
// must not authenticate against another.
func TestRotatedTokenRejectsOld(t *testing.T) {
	const rotatedToken = "fedcba9876543210fedcba9876543210"

	for name, tc := range map[string]struct {
		bound   string
		present string
		want    int
	}{
		"old-token-against-new-middleware": {bound: rotatedToken, present: authTestToken, want: http.StatusUnauthorized},
		"new-token-against-old-middleware": {bound: authTestToken, present: rotatedToken, want: http.StatusUnauthorized},
		"matching-token":                   {bound: rotatedToken, present: rotatedToken, want: http.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.GET("/api/v1/projects", RequireAccessToken(tc.bound), func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"success": true})
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
			req.Header.Set("Authorization", "Bearer "+tc.present)
			router.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}

	// The websocket channel rotates too: the previous process's token
	// subprotocol must not authenticate against the new process.
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/stream", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Protocol", WebSocketProtocolV1+", codeflow.token."+authTestToken)
	if AccessTokenMatches(req, rotatedToken) {
		t.Fatal("rotated middleware accepted the previous process token")
	}
}

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
