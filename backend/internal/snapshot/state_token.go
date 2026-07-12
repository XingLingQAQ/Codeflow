package snapshot

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrNotRestorable indicates a captured state token cannot be truly restored
// (legacy digest-only tokens, unsupported storage, etc.).
var ErrNotRestorable = errors.New("snapshot state not restorable")

const recoverableSchemaVersion = 1

// RecoverableState is the PR-4 restorable state envelope stored in snapshot fields.
// Legacy digest-only tokens used "kind:hex" and are rejected on restore.
type RecoverableState struct {
	SchemaVersion int             `json:"schema_version"`
	Kind          string          `json:"kind"`
	Digest        string          `json:"digest"`
	Storage       string          `json:"storage"` // inline | blob
	Payload       json.RawMessage `json:"payload,omitempty"`
	BlobRef       string          `json:"blob_ref,omitempty"`
}

func encodeRecoverable(kind string, payload interface{}) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal %s payload: %w", kind, err)
	}
	sum := sha256.Sum256(raw)
	state := RecoverableState{
		SchemaVersion: recoverableSchemaVersion,
		Kind:          kind,
		Digest:        fmt.Sprintf("sha256:%x", sum),
		Storage:       "inline",
		Payload:       raw,
	}
	out, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("marshal recoverable %s state: %w", kind, err)
	}
	return string(out), nil
}

func parseRecoverableState(kind, value string) (*RecoverableState, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("%s state is empty", kind)
	}

	// Legacy digest-only tokens: "conversation:<hex>", "vector:<hex>", "graph:<hex>".
	if isLegacyDigestToken(kind, value) {
		return nil, fmt.Errorf("%w: legacy digest-only %s token", ErrNotRestorable, kind)
	}

	var state RecoverableState
	if err := json.Unmarshal([]byte(value), &state); err != nil {
		if strings.HasPrefix(value, kind+":") {
			return nil, fmt.Errorf("%w: legacy digest-only %s token", ErrNotRestorable, kind)
		}
		return nil, fmt.Errorf("invalid %s state token: %w", kind, err)
	}

	if state.SchemaVersion < 1 {
		return nil, fmt.Errorf("%w: unsupported schema_version %d", ErrNotRestorable, state.SchemaVersion)
	}
	if state.Kind != "" && state.Kind != kind {
		return nil, fmt.Errorf("invalid %s state token: kind mismatch (got %q)", kind, state.Kind)
	}
	if state.Kind == "" {
		state.Kind = kind
	}

	storage := state.Storage
	if storage == "" {
		storage = "inline"
	}
	if storage != "inline" {
		return nil, fmt.Errorf("%w: storage %q not supported in PR-4", ErrNotRestorable, storage)
	}
	if len(state.Payload) == 0 && state.BlobRef != "" {
		return nil, fmt.Errorf("%w: blob storage not supported in PR-4", ErrNotRestorable)
	}

	if state.Digest != "" && len(state.Payload) > 0 {
		sum := sha256.Sum256(state.Payload)
		expected := fmt.Sprintf("sha256:%x", sum)
		if state.Digest != expected {
			return nil, fmt.Errorf("%s state digest mismatch", kind)
		}
	}

	return &state, nil
}

func decodePayload[T any](state *RecoverableState) (T, error) {
	var zero T
	if state == nil {
		return zero, fmt.Errorf("recoverable state is nil")
	}
	if len(state.Payload) == 0 {
		// empty payload is valid (empty capture)
		return zero, nil
	}
	var out T
	if err := json.Unmarshal(state.Payload, &out); err != nil {
		return zero, fmt.Errorf("decode %s payload: %w", state.Kind, err)
	}
	return out, nil
}

func isLegacyDigestToken(kind, value string) bool {
	prefix := kind + ":"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	// JSON recoverable tokens start with '{'
	if strings.HasPrefix(strings.TrimSpace(value), "{") {
		return false
	}
	rest := value[len(prefix):]
	if rest == "" {
		return true
	}
	for _, r := range rest {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}
