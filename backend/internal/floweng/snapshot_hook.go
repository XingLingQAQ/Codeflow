package floweng

import (
	"context"
	"fmt"

	"github.com/codeflow/backend/internal/snapshot"
)

// DefaultSnapshotHook creates snapshots via ISnapshotService on stage completion.
type DefaultSnapshotHook struct {
	svc snapshot.ISnapshotService
}

// NewDefaultSnapshotHook wraps a snapshot service. svc may be nil (no-op).
func NewDefaultSnapshotHook(svc snapshot.ISnapshotService) *DefaultSnapshotHook {
	return &DefaultSnapshotHook{svc: svc}
}

// CreateStageSnapshot implements SnapshotCreator.
func (h *DefaultSnapshotHook) CreateStageSnapshot(ctx context.Context, flow *Flow, stage *Stage, sessionID string) (string, error) {
	if h == nil || h.svc == nil {
		return "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	desc := fmt.Sprintf("floweng stage done flow=%s stage=%s type=%s", flow.ID, stage.ID, stage.Type)
	snap, err := h.svc.Create(ctx, &snapshot.SnapshotCreateRequest{
		Description: desc,
		SessionID:   sessionID,
		Tags:        []string{"floweng", string(stage.Type), flow.ID},
	})
	if err != nil {
		return "", err
	}
	return snap.ID, nil
}
