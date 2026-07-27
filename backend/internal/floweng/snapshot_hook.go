package floweng

import (
	"context"
	"fmt"
	"strings"

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

// DefaultSnapshotRestorer restores a stage's completion snapshot via
// ISnapshotService — the symmetric counterpart to DefaultSnapshotHook, used by
// the engine's Loop for snapshot-based rollback (design §4-5).
//
// Destructive git restore stays opt-in at the snapshot provider layer
// (CODEFLOW_SNAPSHOT_ENABLE_GIT_RESTORE=true, see internal/snapshot state
// provider). This restorer enables nothing destructive: it calls the service's
// Restore, which honors that gate, and requires no explicit RestoreOptions.
type DefaultSnapshotRestorer struct {
	svc snapshot.ISnapshotService
}

// NewDefaultSnapshotRestorer wraps a snapshot service. svc may be nil (no-op).
func NewDefaultSnapshotRestorer(svc snapshot.ISnapshotService) *DefaultSnapshotRestorer {
	return &DefaultSnapshotRestorer{svc: svc}
}

// RestoreStageSnapshot implements SnapshotRestorer. A nil restorer or nil
// service is a successful no-op (mirroring DefaultSnapshotHook). Both a service
// error and any partial restore failures (RestoreResult.Errors) are surfaced so
// the caller (Loop) aborts the rollback rather than proceeding on bad state.
func (h *DefaultSnapshotRestorer) RestoreStageSnapshot(ctx context.Context, flow *Flow, target *Stage, snapshotID string) error {
	if h == nil || h.svc == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := h.svc.Restore(ctx, snapshotID)
	if err != nil {
		return err
	}
	if res != nil && len(res.Errors) > 0 {
		return fmt.Errorf("snapshot %s restore incomplete: %s", snapshotID, strings.Join(res.Errors, "; "))
	}
	return nil
}
