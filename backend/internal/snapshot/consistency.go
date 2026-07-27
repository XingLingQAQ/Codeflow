package snapshot

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ProviderConsistency holds the comparison result for a single provider's state.
type ProviderConsistency struct {
	Kind          string `json:"kind"`
	Match         bool   `json:"match"`
	StoredDigest  string `json:"stored_digest"`
	CurrentDigest string `json:"current_digest"`
	Detail        string `json:"detail,omitempty"`
}

// ConsistencyReport is the result of VerifyConsistency.
type ConsistencyReport struct {
	SnapshotID string                `json:"snapshot_id"`
	CheckedAt  time.Time             `json:"checked_at"`
	Results    []ProviderConsistency `json:"results"`
	Consistent bool                  `json:"consistent"`
}

// VerifyConsistency re-captures the current live state for each provider and
// compares it against the state recorded in the given snapshot. This answers
// "does the current live state match what snapshot X recorded?" — the intended
// use is calling it right after a Restore to confirm the restore produced the
// expected state.
//
// For recoverable tokens (conversation, vector, graph) the comparison is by
// digest (sha256 of the payload). For git the comparison is a direct hash
// equality check. Providers whose stored token is not recoverable (legacy
// digest-only tokens, unsupported storage, etc.) are reported with a Detail
// explanation and excluded from the aggregate Consistent flag.
//
// Consistent is true when every comparable provider matches and at least one
// provider was comparable. If all providers are non-comparable (e.g. all
// captures failed due to context cancellation), Consistent is false.
func (s *InMemorySnapshotService) VerifyConsistency(ctx context.Context, snapshotID string) (*ConsistencyReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.RLock()
	snap, ok := s.snapshots[snapshotID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("snapshot not found: %s", snapshotID)
	}

	report := &ConsistencyReport{
		SnapshotID: snapshotID,
		CheckedAt:  time.Now(),
	}

	report.Results = append(report.Results,
		s.verifyGitConsistency(ctx, snap),
		s.verifyTokenConsistency(ctx, "conversation", snap.ConversationState,
			func() (string, error) { return s.provider.CaptureConversationState(ctx, snap.SessionID) }),
		s.verifyTokenConsistency(ctx, "vector", snap.VectorPointer,
			func() (string, error) { return s.provider.CaptureVectorState(ctx, snap.SessionID) }),
		s.verifyTokenConsistency(ctx, "graph", snap.MemoryGraphVersion,
			func() (string, error) { return s.provider.CaptureMemoryGraphState(ctx) }),
	)

	report.Consistent = true
	comparable := 0
	for _, r := range report.Results {
		if r.Detail != "" {
			continue
		}
		comparable++
		if !r.Match {
			report.Consistent = false
		}
	}
	if comparable == 0 {
		report.Consistent = false
	}

	return report, nil
}

func (s *InMemorySnapshotService) verifyGitConsistency(ctx context.Context, snap *Snapshot) ProviderConsistency {
	result := ProviderConsistency{Kind: "git", StoredDigest: snap.GitHash}
	current, err := s.provider.CaptureGitState(ctx)
	if err != nil {
		result.Detail = fmt.Sprintf("re-capture failed: %v", err)
		return result
	}
	result.CurrentDigest = current
	result.Match = strings.TrimSpace(result.StoredDigest) == strings.TrimSpace(result.CurrentDigest)
	return result
}

func (s *InMemorySnapshotService) verifyTokenConsistency(ctx context.Context, kind, storedToken string, captureFn func() (string, error)) ProviderConsistency {
	result := ProviderConsistency{Kind: kind}

	stored, err := parseRecoverableState(kind, storedToken)
	if err != nil {
		result.Detail = fmt.Sprintf("stored token not parseable: %v", err)
		return result
	}
	result.StoredDigest = stored.Digest

	currentToken, err := captureFn()
	if err != nil {
		result.Detail = fmt.Sprintf("re-capture failed: %v", err)
		return result
	}
	current, err := parseRecoverableState(kind, currentToken)
	if err != nil {
		result.Detail = fmt.Sprintf("re-captured token not parseable: %v", err)
		return result
	}
	result.CurrentDigest = current.Digest
	result.Match = result.StoredDigest == result.CurrentDigest
	return result
}
