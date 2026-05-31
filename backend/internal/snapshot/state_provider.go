package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/codeflow/backend/internal/agent"
	backendgit "github.com/codeflow/backend/internal/git"
	"github.com/codeflow/backend/internal/memory"
	"github.com/codeflow/backend/internal/samg"
)

// StateProvider captures and restores the external state that composes a snapshot.
// The default provider captures real observable state and keeps restore non-destructive
// unless a safer module-specific restore implementation is injected.
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

func (p *defaultStateProvider) CaptureGitState(ctx context.Context) (string, error) {
	hash, err := p.gitManager.GetCurrentHash(ctx)
	if err != nil {
		return "", fmt.Errorf("capture git hash: %w", err)
	}
	return hash, nil
}

func (p *defaultStateProvider) CaptureConversationState(ctx context.Context, sessionID string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return digestState("conversation", map[string]string{"session_id": ""}), nil
	}
	trace, err := agent.GetAgentService().GetConversationTrace(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("capture conversation trace: %w", err)
	}
	return digestState("conversation", trace), nil
}

func (p *defaultStateProvider) CaptureVectorState(ctx context.Context, sessionID string) (string, error) {
	resp, err := memory.GetMemoryService().List(ctx, &memory.MemoryListOptions{SessionID: sessionID, Limit: 10000})
	if err != nil {
		return "", fmt.Errorf("capture memory vector pointer: %w", err)
	}
	return digestState("vector", resp), nil
}

func (p *defaultStateProvider) CaptureMemoryGraphState(ctx context.Context) (string, error) {
	svc := samg.GetSAMGService()
	stats, err := svc.GetStats(ctx)
	if err != nil {
		return "", fmt.Errorf("capture samg stats: %w", err)
	}
	graph, err := svc.ExportGraph(ctx)
	if err != nil {
		return "", fmt.Errorf("capture samg graph: %w", err)
	}
	return digestState("graph", struct {
		Stats interface{} `json:"stats"`
		Graph interface{} `json:"graph"`
	}{Stats: stats, Graph: graph}), nil
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
	return validateStateToken("conversation", state)
}

func (p *defaultStateProvider) RestoreVectorState(ctx context.Context, pointer string) error {
	return validateStateToken("vector", pointer)
}

func (p *defaultStateProvider) RestoreMemoryGraphState(ctx context.Context, version string) error {
	return validateStateToken("graph", version)
}

func digestState(prefix string, data interface{}) string {
	raw, err := json.Marshal(data)
	if err != nil {
		raw = []byte(fmt.Sprintf("%#v", data))
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%s:%x", prefix, sum[:12])
}

func validateStateToken(prefix, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s state is empty", prefix)
	}
	if !strings.HasPrefix(value, prefix+":") {
		return fmt.Errorf("invalid %s state token", prefix)
	}
	return nil
}
