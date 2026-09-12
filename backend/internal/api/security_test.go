package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSidecarHTTPAuthentication(t *testing.T) {
	server := NewServer(&Config{
		AuthToken:      testAuthToken,
		AllowedOrigins: []string{"http://localhost:3000"},
	})

	for name, tc := range map[string]struct {
		header string
		want   int
	}{
		"missing": {want: http.StatusUnauthorized},
		"wrong":   {header: "Bearer wrong-token", want: http.StatusUnauthorized},
		"valid":   {header: "Bearer " + testAuthToken, want: http.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			server.Router().ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
			if strings.Contains(w.Body.String(), testAuthToken) {
				t.Fatal("response leaked access token")
			}
		})
	}

	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("health status=%d want=%d", w.Code, http.StatusOK)
	}
}

func TestSidecarOriginAllowlistIsExact(t *testing.T) {
	server := NewServer(&Config{
		AuthToken:      testAuthToken,
		AllowedOrigins: []string{"http://localhost:3000"},
	})

	for name, tc := range map[string]struct {
		origin string
		want   int
	}{
		"allowed":          {origin: "http://localhost:3000", want: http.StatusOK},
		"wrong-port":       {origin: "http://localhost:3001", want: http.StatusForbidden},
		"prefix-confusion": {origin: "http://localhost:3000.attacker.invalid", want: http.StatusForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
			req.Header.Set("Authorization", "Bearer "+testAuthToken)
			req.Header.Set("Origin", tc.origin)
			server.Router().ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestSidecarConfigRejectsUnsafeTrustDefaults(t *testing.T) {
	cases := []Config{
		{Host: "0.0.0.0", AuthToken: testAuthToken, AllowedOrigins: []string{"http://localhost:3000"}},
		{Host: "127.0.0.1", AuthToken: "short", AllowedOrigins: []string{"http://localhost:3000"}},
		{Host: "127.0.0.1", AuthToken: "invalid token 0123456789abcdef012345", AllowedOrigins: []string{"http://localhost:3000"}},
		{Host: "127.0.0.1", AuthToken: testAuthToken, AllowedOrigins: []string{"*"}},
	}
	for i := range cases {
		if err := NewServer(&cases[i]).Validate(); err == nil {
			t.Fatalf("case %d unexpectedly accepted", i)
		}
	}

	remote := Config{
		Host:           "0.0.0.0",
		AuthToken:      testAuthToken,
		AllowedOrigins: []string{"https://codeflow.example"},
		AllowRemote:    true,
	}
	if err := NewServer(&remote).Validate(); err != nil {
		t.Fatalf("explicit remote configuration rejected: %v", err)
	}
}

func TestGeneratedTokenRotatesAcrossServerRestarts(t *testing.T) {
	first := NewServer(nil)
	second := NewServer(nil)
	if first.AuthToken() == second.AuthToken() {
		t.Fatal("separate server processes reused a generated token")
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer "+first.AuthToken())
	second.Router().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("old token status=%d want=%d", w.Code, http.StatusUnauthorized)
	}
}

func TestRunContextEmitsLoopbackHandshake(t *testing.T) {
	writes := make(chan []byte, 1)
	server := NewServer(&Config{
		Host:            "127.0.0.1",
		Port:            "0",
		AuthToken:       testAuthToken,
		AllowedOrigins:  []string{"http://localhost:3000"},
		HandshakeWriter: channelWriter{writes: writes},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.RunContext(ctx) }()

	var raw []byte
	select {
	case raw = <-writes:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("timed out waiting for sidecar handshake")
	}
	line := strings.TrimSpace(string(raw))
	const prefix = "CODEFLOW_HANDSHAKE:"
	if !strings.HasPrefix(line, prefix) {
		cancel()
		t.Fatalf("unexpected handshake %q", line)
	}
	var handshake struct {
		ProtocolVersion string `json:"protocol_version"`
		Host            string `json:"host"`
		Port            int    `json:"port"`
		Token           string `json:"token"`
		ExpiresAt       string `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &handshake); err != nil {
		cancel()
		t.Fatalf("decode handshake: %v", err)
	}
	if handshake.Host != "127.0.0.1" || handshake.Port == 0 || handshake.Token != testAuthToken ||
		handshake.ProtocolVersion != "1" || handshake.ExpiresAt != "process" {
		cancel()
		t.Fatalf("unexpected handshake: %+v", handshake)
	}

	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/v1/projects", handshake.Port), nil)
	req.Header.Set("Authorization", "Bearer "+testAuthToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("loopback request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("loopback request status=%d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}

type channelWriter struct {
	writes chan<- []byte
}

func (w channelWriter) Write(p []byte) (int, error) {
	copyOfP := append([]byte(nil), p...)
	w.writes <- copyOfP
	return len(p), nil
}
