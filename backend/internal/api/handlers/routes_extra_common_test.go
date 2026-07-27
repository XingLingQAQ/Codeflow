package handlers

// Shared helpers for the *_routes_extra_test.go HTTP handler tests. These tests
// exercise handlers over a minimal gin router (no experimental middleware, which
// only adds headers) and assert against the {success,data,error} envelope from
// common.go. Names use a distinct "rex" (routes-extra) prefix to avoid clashing
// with per-file helpers elsewhere in the package.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// rexEnvelope mirrors handlers.Response with Data kept raw for per-test decoding.
type rexEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

// rexRequest issues one request against r and returns the recorder. A non-nil
// body sets Content-Type: application/json. Extra headers are applied last.
func rexRequest(t *testing.T, r http.Handler, method, target string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, target, rdr)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, target, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// rexDecode unmarshals the response body into the standard envelope.
func rexDecode(t *testing.T, w *httptest.ResponseRecorder) rexEnvelope {
	t.Helper()
	var env rexEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (status=%d body=%s)", err, w.Code, w.Body.String())
	}
	return env
}

// rexData decodes the envelope, asserts the HTTP status and success flag, and
// unmarshals the data payload into out. Pass out=nil to only assert status.
func rexData(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, wantSuccess bool, out interface{}) rexEnvelope {
	t.Helper()
	if w.Code != wantStatus {
		t.Fatalf("status=%d want=%d body=%s", w.Code, wantStatus, w.Body.String())
	}
	env := rexDecode(t, w)
	if env.Success != wantSuccess {
		t.Fatalf("success=%v want=%v body=%s", env.Success, wantSuccess, w.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(env.Data, out); err != nil {
			t.Fatalf("decode data: %v body=%s", err, w.Body.String())
		}
	}
	return env
}

// rexMustJSON marshals v or fails the test.
func rexMustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
