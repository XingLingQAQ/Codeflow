package snapshot

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/codeflow/backend/internal/agent"
	backendgit "github.com/codeflow/backend/internal/git"
	"github.com/codeflow/backend/internal/memory"
	"github.com/codeflow/backend/internal/samg"
)

// StateProvider captures and restores the external state that composes a snapshot.
// Capture produces recoverable JSON tokens (schema_version=1). Restore performs true
// module-specific recovery. Legacy digest-only tokens are rejected with ErrNotRestorable.
// Git hard reset remains opt-in via CODEFLOW_SNAPSHOT_ENABLE_GIT_RESTORE=true.
type StateProvider interface {
	CaptureGitState(ctx context.Context) (string, error)
	CaptureConversationState(ctx context.Context, sessionID string) (string, error)
	CaptureVectorState(ctx context.Context, sessionID string) (string, error)
	CaptureMemoryGraphState(ctx context.Context) (string, error)
	RestoreGitState(ctx context.Context, gitHash string) error
	RestoreConversationState(ctx context.Context, state string) error
	RestoreVectorState(ctx context.Context, pointer string) error
	RestoreMemoryGraphState(ctx context.Context, version string) error
}

type defaultStateProvider struct {
	gitManager backendgit.IGitManager
}

// NewDefaultStateProvider creates the default snapshot state provider.
func NewDefaultStateProvider() StateProvider {
	return &defaultStateProvider{gitManager: backendgit.NewGitManager(".")}
}

type conversationStatePayload struct {
	SessionID string                           `json:"session_id"`
	Trace     *agent.ConversationTraceResponse `json:"trace,omitempty"`
}

type vectorStatePayload struct {
	SessionID string              `json:"session_id"`
	Items     []memory.MemoryItem `json:"items"`
}

type graphStatePayload struct {
	Graph *samg.JsonLdGraph `json:"graph"`
}

func (p *defaultStateProvider) CaptureGitState(ctx context.Context) (string, error) {
	hash, err := p.gitManager.GetCurrentHash(ctx)
	if err != nil {
		return "", fmt.Errorf("capture git hash: %w", err)
	}
	return hash, nil
}

func (p *defaultStateProvider) CaptureConversationState(ctx context.Context, sessionID string) (string, error) {
	payload := conversationStatePayload{SessionID: strings.TrimSpace(sessionID)}
	if payload.SessionID != "" {
		trace, err := agent.GetAgentService().GetConversationTrace(ctx, payload.SessionID)
		if err != nil {
			return "", fmt.Errorf("capture conversation trace: %w", err)
		}
		payload.Trace = trace
	}
	return encodeRecoverable("conversation", payload)
}

func (p *defaultStateProvider) CaptureVectorState(ctx context.Context, sessionID string) (string, error) {
	resp, err := memory.GetMemoryService().List(ctx, &memory.MemoryListOptions{SessionID: sessionID, Limit: 10000})
	if err != nil {
		return "", fmt.Errorf("capture memory vector pointer: %w", err)
	}
	items := []memory.MemoryItem{}
	if resp != nil {
		items = resp.Items
	}
	payload := vectorStatePayload{
		SessionID: strings.TrimSpace(sessionID),
		Items:     items,
	}
	return encodeRecoverable("vector", payload)
}

func (p *defaultStateProvider) CaptureMemoryGraphState(ctx context.Context) (string, error) {
	svc := samg.GetSAMGService()
	graph, err := svc.ExportGraph(ctx)
	if err != nil {
		return "", fmt.Errorf("capture samg graph: %w", err)
	}
	return encodeRecoverable("graph", graphStatePayload{Graph: graph})
}

func (p *defaultStateProvider) RestoreGitState(ctx context.Context, gitHash string) error {
	if strings.TrimSpace(gitHash) == "" {
		return fmt.Errorf("git hash is empty")
	}
	if os.Getenv("CODEFLOW_SNAPSHOT_ENABLE_GIT_RESTORE") != "true" {
		return nil
	}
	return p.gitManager.Reset(ctx, gitHash, true)
}

func (p *defaultStateProvider) RestoreConversationState(ctx context.Context, state string) error {
	rec, err := parseRecoverableState("conversation", state)
	if err != nil {
		return err
	}
	payload, err := decodePayload[conversationStatePayload](rec)
	if err != nil {
		return err
	}
	// Empty capture (no session / no trace) is a successful no-op restore.
	if payload.Trace == nil && strings.TrimSpace(payload.SessionID) == "" {
		return nil
	}
	trace := payload.Trace
	if trace == nil {
		trace = &agent.ConversationTraceResponse{SessionID: payload.SessionID}
	}
	if strings.TrimSpace(trace.SessionID) == "" {
		trace.SessionID = payload.SessionID
	}
	return agent.GetAgentService().RestoreConversationTrace(ctx, trace.SessionID, trace)
}

func (p *defaultStateProvider) RestoreVectorState(ctx context.Context, pointer string) error {
	rec, err := parseRecoverableState("vector", pointer)
	if err != nil {
		return err
	}
	payload, err := decodePayload[vectorStatePayload](rec)
	if err != nil {
		return err
	}
	return memory.GetMemoryService().ReplaceItems(ctx, payload.SessionID, payload.Items)
}

func (p *defaultStateProvider) RestoreMemoryGraphState(ctx context.Context, version string) error {
	rec, err := parseRecoverableState("graph", version)
	if err != nil {
		return err
	}
	payload, err := decodePayload[graphStatePayload](rec)
	if err != nil {
		return err
	}
	_, err = samg.GetSAMGService().ReplaceGraph(ctx, payload.Graph)
	return err
}
