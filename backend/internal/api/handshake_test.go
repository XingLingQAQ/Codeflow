package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// The controlled handshake fixtures mirror the exact wire format emitted by
// Server.RunContext ("CODEFLOW_HANDSHAKE:{json}\n"). Tokens in fixtures are
// test placeholders; a real process token is never committed.
var handshakeFixtureFiles = []string{
	"valid.line",
	"valid_min_token_min_port.line",
	"valid_localhost_max_port.line",
}

type handshakePayload struct {
	ProtocolVersion string `json:"protocol_version"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Token           string `json:"token"`
	ExpiresAt       string `json:"expires_at"`
	ProcessStartID  string `json:"process_start_id"`
}

func loadHandshakeFixture(t *testing.T, name string) (string, handshakePayload) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "handshake", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	line := strings.TrimRight(string(raw), "\r\n")
	const prefix = "CODEFLOW_HANDSHAKE:"
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("fixture %s missing %q prefix", name, prefix)
	}
	var payload handshakePayload
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &payload); err != nil {
		t.Fatalf("fixture %s does not decode: %v", name, err)
	}
	return line, payload
}

// assertHandshakeShape mirrors the validation rules the Rust desktop parser
// enforces, so a fixture that drifts from the consumer contract fails here.
func assertHandshakeShape(t *testing.T, name string, h handshakePayload) {
	t.Helper()
	if h.ProtocolVersion != "1" {
		t.Fatalf("%s: protocol_version=%q want %q", name, h.ProtocolVersion, "1")
	}
	if h.Host != "127.0.0.1" && h.Host != "localhost" {
		t.Fatalf("%s: host %q outside loopback whitelist", name, h.Host)
	}
	if h.Port < 1 || h.Port > 65535 {
		t.Fatalf("%s: port %d out of range", name, h.Port)
	}
	if !validOpaqueToken(h.Token) {
		t.Fatalf("%s: token fails producer charset/length rules", name)
	}
	if h.ExpiresAt == "" {
		t.Fatalf("%s: expires_at empty", name)
	}
	if h.ProcessStartID == "" {
		t.Fatalf("%s: process_start_id empty", name)
	}
}

func TestHandshakeFixturesCoverSchemaAndBoundaries(t *testing.T) {
	ports := map[int]bool{}
	minTokenLen := 1 << 30
	seenStartIDs := map[string]bool{}
	for _, name := range handshakeFixtureFiles {
		_, payload := loadHandshakeFixture(t, name)
		assertHandshakeShape(t, name, payload)
		if strings.Contains(payload.Token, "codeflow-test-token") {
			t.Fatalf("%s: fixture embeds the shared test token instead of a placeholder", name)
		}
		if seenStartIDs[payload.ProcessStartID] {
			t.Fatalf("%s: duplicate process_start_id %q", name, payload.ProcessStartID)
		}
		seenStartIDs[payload.ProcessStartID] = true
		ports[payload.Port] = true
		if len(payload.Token) < minTokenLen {
			minTokenLen = len(payload.Token)
		}
	}
	// Boundary coverage: lowest/highest port and the minimum accepted token
	// length (32, matching validOpaqueToken) each appear in some fixture.
	if !ports[1] || !ports[65535] {
		t.Fatalf("fixtures miss port boundaries, got %v", ports)
	}
	if minTokenLen != 32 {
		t.Fatalf("fixtures miss minimum token length, got %d", minTokenLen)
	}
}

func TestRunContextHandshakeMatchesFixtureSchema(t *testing.T) {
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
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("server shutdown: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("server did not stop")
		}
	}()

	var raw []byte
	select {
	case raw = <-writes:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for sidecar handshake")
	}
	line := strings.TrimSpace(string(raw))
	const prefix = "CODEFLOW_HANDSHAKE:"
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("unexpected handshake %q", line)
	}

	// The emitted document must carry exactly the fixture key set: additive
	// schema changes stay consumer-compatible only while both sides agree on
	// the field inventory.
	_, fixture := loadHandshakeFixture(t, "valid.line")
	fixtureKeys := keysOfHandshake(t, "valid.line")
	var emitted map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &emitted); err != nil {
		t.Fatalf("decode emitted handshake: %v", err)
	}
	emittedKeys := make([]string, 0, len(emitted))
	for k := range emitted {
		emittedKeys = append(emittedKeys, k)
	}
	sort.Strings(emittedKeys)
	if strings.Join(emittedKeys, ",") != strings.Join(fixtureKeys, ",") {
		t.Fatalf("emitted keys %v differ from fixture keys %v", emittedKeys, fixtureKeys)
	}

	// Stable fields match the fixture; volatile fields are process-specific.
	if emitted["protocol_version"] != fixture.ProtocolVersion ||
		emitted["host"] != fixture.Host ||
		emitted["expires_at"] != fixture.ExpiresAt {
		t.Fatalf("stable fields drifted from fixture: %v", emitted)
	}
	if emitted["token"] != testAuthToken {
		t.Fatal("handshake did not carry the configured process token")
	}
	if port, ok := emitted["port"].(float64); !ok || port < 1 || port > 65535 {
		t.Fatalf("emitted port out of range: %v", emitted["port"])
	}
	if emitted["process_start_id"] != server.ProcessStartID() {
		t.Fatalf("handshake process_start_id %v != server id %q",
			emitted["process_start_id"], server.ProcessStartID())
	}
}

func keysOfHandshake(t *testing.T, name string) []string {
	t.Helper()
	line, _ := loadHandshakeFixture(t, name)
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "CODEFLOW_HANDSHAKE:")), &payload); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	keys := make([]string, 0, len(payload))
	for k := range payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var processStartIDPattern = regexp.MustCompile(`^[0-9a-f]+-[A-Za-z0-9_-]{22}$`)

func TestProcessStartIDStableWithinProcess(t *testing.T) {
	server := NewServer(nil)
	id := server.ProcessStartID()
	if !processStartIDPattern.MatchString(id) {
		t.Fatalf("process start id %q does not match expected shape", id)
	}
	if got := server.ProcessStartID(); got != id {
		t.Fatalf("process start id changed within process: %q -> %q", id, got)
	}
}

func TestProcessStartIDChangesAcrossRestarts(t *testing.T) {
	first := NewServer(nil)
	second := NewServer(nil)
	if first.ProcessStartID() == "" || second.ProcessStartID() == "" {
		t.Fatal("process start id must not be empty")
	}
	if first.ProcessStartID() == second.ProcessStartID() {
		t.Fatalf("separate server instances reused process start id %q", first.ProcessStartID())
	}
}
