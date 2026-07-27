// Package snapshot - integrity & state-token spec tests.
//
// These tests target state_token.go (the RecoverableState envelope: encode →
// parse → decode round-trip, digest integrity, schema/kind/storage skew) and
// the provider-level integrity guarantee that a tampered token is refused
// without mutating live state. They complement snapshot_test.go, which already
// covers the happy-path true-restore cycle per provider.
package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/memory"
)

// mutateState decodes a valid recoverable token, lets fn tweak the envelope,
// then re-marshals it. Because json.RawMessage payloads round-trip verbatim,
// the original digest stays valid unless fn changes the payload.
func mutateState(t *testing.T, base string, fn func(*RecoverableState)) string {
	t.Helper()
	var st RecoverableState
	if err := json.Unmarshal([]byte(base), &st); err != nil {
		t.Fatalf("mutateState: unmarshal base token: %v", err)
	}
	fn(&st)
	out, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("mutateState: marshal mutated token: %v", err)
	}
	return string(out)
}

// tamperRecoverablePayload rewrites the payload bytes while leaving the original
// digest in place, simulating in-place corruption of a stored token. Shared with
// snapshot_restore_spec_test.go.
func tamperRecoverablePayload(t *testing.T, token string) string {
	t.Helper()
	return mutateState(t, token, func(st *RecoverableState) {
		st.Payload = json.RawMessage(`{"tampered":"corruption-marker"}`)
	})
}

func validVectorToken(t *testing.T) string {
	t.Helper()
	token, err := encodeRecoverable("vector", vectorStatePayload{SessionID: "base-session", Items: []memory.MemoryItem{}})
	if err != nil {
		t.Fatalf("encodeRecoverable: %v", err)
	}
	return token
}

func TestStateTokenRoundTripInline(t *testing.T) {
	payload := vectorStatePayload{SessionID: "sess-round-trip", Items: []memory.MemoryItem{}}
	token, err := encodeRecoverable("vector", payload)
	if err != nil {
		t.Fatalf("encodeRecoverable: %v", err)
	}

	var env RecoverableState
	if err := json.Unmarshal([]byte(token), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.SchemaVersion != recoverableSchemaVersion {
		t.Fatalf("schema_version = %d, want %d", env.SchemaVersion, recoverableSchemaVersion)
	}
	if env.Kind != "vector" {
		t.Fatalf("kind = %q, want vector", env.Kind)
	}
	if env.Storage != "inline" {
		t.Fatalf("storage = %q, want inline", env.Storage)
	}
	if !strings.HasPrefix(env.Digest, "sha256:") {
		t.Fatalf("digest = %q, want sha256: prefix", env.Digest)
	}

	state, err := parseRecoverableState("vector", token)
	if err != nil {
		t.Fatalf("parseRecoverableState: %v", err)
	}
	got, err := decodePayload[vectorStatePayload](state)
	if err != nil {
		t.Fatalf("decodePayload: %v", err)
	}
	if got.SessionID != "sess-round-trip" {
		t.Fatalf("round-trip session id = %q, want sess-round-trip", got.SessionID)
	}
}

func TestStateTokenEmptyRejected(t *testing.T) {
	if _, err := parseRecoverableState("vector", "   "); err == nil {
		t.Fatal("expected error for empty token")
	} else if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty-token error, got %v", err)
	}
}

func TestStateTokenCorruptedJSONRejected(t *testing.T) {
	// Truncated JSON that is neither a legacy token nor a valid envelope.
	_, err := parseRecoverableState("vector", `{"schema_version":1,"kind":"vector"`)
	if err == nil {
		t.Fatal("expected error for corrupted token")
	}
	if !strings.Contains(err.Error(), "invalid") && !errors.Is(err, ErrNotRestorable) {
		t.Fatalf("expected invalid-token error, got %v", err)
	}
}

func TestStateTokenDigestTamperRejected(t *testing.T) {
	tampered := tamperRecoverablePayload(t, validVectorToken(t))
	_, err := parseRecoverableState("vector", tampered)
	if err == nil {
		t.Fatal("tampered payload with stale digest must be rejected")
	}
	if !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("expected digest mismatch, got %v", err)
	}
}

// TestStateTokenMissingDigestRejected verifies that a token carrying a payload
// but an empty digest is rejected. encodeRecoverable always emits a digest, so
// no legitimate producer creates empty-digest tokens; rejecting them closes the
// integrity bypass where a fully-rewritten payload with the digest field cleared
// would have been silently accepted.
func TestStateTokenMissingDigestRejected(t *testing.T) {
	tok := mutateState(t, validVectorToken(t), func(st *RecoverableState) {
		st.Payload = json.RawMessage(`{"session_id":"rewritten","items":[]}`)
		st.Digest = ""
	})
	_, err := parseRecoverableState("vector", tok)
	if err == nil {
		t.Fatal("payload with empty digest should be rejected")
	}
	if !strings.Contains(err.Error(), "missing digest") {
		t.Fatalf("expected 'missing digest' in error, got %v", err)
	}
}

func TestStateTokenSchemaVersionZeroRejected(t *testing.T) {
	tok := mutateState(t, validVectorToken(t), func(st *RecoverableState) { st.SchemaVersion = 0 })
	_, err := parseRecoverableState("vector", tok)
	if !errors.Is(err, ErrNotRestorable) {
		t.Fatalf("schema_version 0 should be ErrNotRestorable, got %v", err)
	}
}

// TestStateTokenFutureSchemaVersionRejected verifies that parseRecoverableState
// rejects tokens with a schema_version newer than the current supported version,
// preventing silent misinterpretation of future-format tokens.
func TestStateTokenFutureSchemaVersionRejected(t *testing.T) {
	tok := mutateState(t, validVectorToken(t), func(st *RecoverableState) {
		st.SchemaVersion = recoverableSchemaVersion + 1
	})
	_, err := parseRecoverableState("vector", tok)
	if !errors.Is(err, ErrNotRestorable) {
		t.Fatalf("future schema_version should be ErrNotRestorable, got %v", err)
	}
	if !strings.Contains(err.Error(), "unsupported schema_version") {
		t.Fatalf("expected 'unsupported schema_version' in error, got %v", err)
	}
}

func TestStateTokenKindMismatchRejected(t *testing.T) {
	// A vector token parsed as if it were a conversation token.
	_, err := parseRecoverableState("conversation", validVectorToken(t))
	if err == nil || !strings.Contains(err.Error(), "kind mismatch") {
		t.Fatalf("expected kind mismatch, got %v", err)
	}
}

func TestStateTokenEmptyKindDefaultsToRequested(t *testing.T) {
	tok := mutateState(t, validVectorToken(t), func(st *RecoverableState) { st.Kind = "" })
	state, err := parseRecoverableState("vector", tok)
	if err != nil {
		t.Fatalf("empty kind should default, got err %v", err)
	}
	if state.Kind != "vector" {
		t.Fatalf("empty kind should default to requested kind, got %q", state.Kind)
	}
}

func TestStateTokenUnsupportedStorageRejected(t *testing.T) {
	tok := mutateState(t, validVectorToken(t), func(st *RecoverableState) { st.Storage = "blob" })
	_, err := parseRecoverableState("vector", tok)
	if !errors.Is(err, ErrNotRestorable) {
		t.Fatalf("non-inline storage should be ErrNotRestorable, got %v", err)
	}
}

func TestStateTokenBlobRefRejected(t *testing.T) {
	tok := mutateState(t, validVectorToken(t), func(st *RecoverableState) {
		st.Payload = nil
		st.Digest = ""
		st.BlobRef = "blob://some-ref"
	})
	_, err := parseRecoverableState("vector", tok)
	if !errors.Is(err, ErrNotRestorable) {
		t.Fatalf("blob_ref storage should be ErrNotRestorable, got %v", err)
	}
}

func TestStateTokenLegacyDigestVariantsRejected(t *testing.T) {
	cases := []struct {
		name  string
		token string
	}{
		{"empty-rest", "vector:"},
		{"hex-mixed-case", "vector:DEADbeef0123"},
		{"non-hex-rest", "vector:not-a-hex-token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseRecoverableState("vector", tc.token)
			if !errors.Is(err, ErrNotRestorable) {
				t.Fatalf("legacy token %q should be ErrNotRestorable, got %v", tc.token, err)
			}
		})
	}
}

func TestDecodePayloadNilState(t *testing.T) {
	if _, err := decodePayload[vectorStatePayload](nil); err == nil {
		t.Fatal("expected error decoding nil state")
	}
}

func TestDecodePayloadEmptyPayloadIsZeroValue(t *testing.T) {
	state := &RecoverableState{SchemaVersion: 1, Kind: "vector", Storage: "inline"}
	got, err := decodePayload[vectorStatePayload](state)
	if err != nil {
		t.Fatalf("empty payload should decode to zero value, got %v", err)
	}
	if got.SessionID != "" || len(got.Items) != 0 {
		t.Fatalf("expected zero value payload, got %+v", got)
	}
}

func TestDecodePayloadWrongShapeRejected(t *testing.T) {
	state := &RecoverableState{
		SchemaVersion: 1,
		Kind:          "vector",
		Storage:       "inline",
		Payload:       json.RawMessage(`"a-json-string-not-an-object"`),
	}
	if _, err := decodePayload[vectorStatePayload](state); err == nil {
		t.Fatal("expected decode error for wrong payload shape")
	}
}

// TestRestoreRefusesTamperedVectorAndPreservesState is the end-to-end integrity
// guarantee: a tampered vector token is refused by the real provider AND the
// live memory state is left untouched (no partial application before the
// integrity check fires).
func TestRestoreRefusesTamperedVectorAndPreservesState(t *testing.T) {
	ctx := context.Background()
	prevMem := memory.GetMemoryService()
	memSvc := memory.NewInMemoryService()
	memory.SetMemoryService(memSvc)
	t.Cleanup(func() { memory.SetMemoryService(prevMem) })

	sessionID := "snap-tamper-vector"
	if _, err := memSvc.Create(ctx, &memory.MemoryItemCreateRequest{
		Content:   "seed content",
		Type:      memory.MemoryTypeSTM,
		SessionID: sessionID,
		Source:    memory.SourceUser,
	}); err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	provider := NewDefaultStateProvider()
	token, err := provider.CaptureVectorState(ctx, sessionID)
	if err != nil {
		t.Fatalf("CaptureVectorState: %v", err)
	}

	// Mutate live state away from the captured snapshot (now 2 items).
	if _, err := memSvc.Create(ctx, &memory.MemoryItemCreateRequest{
		Content:   "mutated content",
		Type:      memory.MemoryTypeSTM,
		SessionID: sessionID,
		Source:    memory.SourceAssistant,
	}); err != nil {
		t.Fatalf("mutate Create: %v", err)
	}

	tampered := tamperRecoverablePayload(t, token)
	err = provider.RestoreVectorState(ctx, tampered)
	if err == nil {
		t.Fatal("restore accepted a tampered vector token — integrity check missing")
	}
	if !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("expected digest mismatch, got %v", err)
	}

	// Live state must be unchanged by the refused restore.
	list, err := memSvc.List(ctx, &memory.MemoryListOptions{SessionID: sessionID, Limit: 100})
	if err != nil {
		t.Fatalf("List after refused restore: %v", err)
	}
	if list.Total != 2 {
		t.Fatalf("tampered restore must not mutate state; want 2 items, got %d", list.Total)
	}
}
