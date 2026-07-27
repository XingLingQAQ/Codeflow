package floweng

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/snapshot"
)

// fakeSnapshotService is a minimal ISnapshotService seam that records the id
// passed to Restore and returns a scripted result/error.
type fakeSnapshotService struct {
	calls      int
	restoreID  string
	restoreErr error
	result     *snapshot.RestoreResult
}

func (f *fakeSnapshotService) Create(ctx context.Context, req *snapshot.SnapshotCreateRequest) (*snapshot.Snapshot, error) {
	return &snapshot.Snapshot{ID: "created"}, nil
}

func (f *fakeSnapshotService) List(ctx context.Context, opts *snapshot.SnapshotListOptions) (*snapshot.SnapshotListResponse, error) {
	return &snapshot.SnapshotListResponse{}, nil
}

func (f *fakeSnapshotService) Get(ctx context.Context, id string) (*snapshot.Snapshot, error) {
	return &snapshot.Snapshot{ID: id}, nil
}

func (f *fakeSnapshotService) Restore(ctx context.Context, id string) (*snapshot.RestoreResult, error) {
	f.calls++
	f.restoreID = id
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	if f.result != nil {
		return f.result, nil
	}
	return &snapshot.RestoreResult{SnapshotID: id}, nil
}

func (f *fakeSnapshotService) Delete(ctx context.Context, id string) error { return nil }

// The restorer invokes the service's Restore with the exact snapshot id.
func TestDefaultSnapshotRestorerInvokesServiceWithID(t *testing.T) {
	fake := &fakeSnapshotService{}
	r := NewDefaultSnapshotRestorer(fake)
	if err := r.RestoreStageSnapshot(context.Background(), &Flow{ID: "f"}, &Stage{ID: "s"}, "snap-xyz"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("Restore calls=%d want 1", fake.calls)
	}
	if fake.restoreID != "snap-xyz" {
		t.Fatalf("restored id=%q want snap-xyz", fake.restoreID)
	}
}

// A service error propagates to the caller (so Loop aborts).
func TestDefaultSnapshotRestorerPropagatesError(t *testing.T) {
	fake := &fakeSnapshotService{restoreErr: fmt.Errorf("snapshot not found")}
	r := NewDefaultSnapshotRestorer(fake)
	err := r.RestoreStageSnapshot(context.Background(), &Flow{ID: "f"}, &Stage{ID: "s"}, "missing")
	if err == nil {
		t.Fatal("expected the service error to propagate")
	}
	if !strings.Contains(err.Error(), "snapshot not found") {
		t.Fatalf("error=%q want it to mention the service failure", err.Error())
	}
}

// Partial restore failures reported in RestoreResult.Errors are surfaced.
func TestDefaultSnapshotRestorerSurfacesPartialErrors(t *testing.T) {
	fake := &fakeSnapshotService{result: &snapshot.RestoreResult{
		SnapshotID: "snap-1",
		Errors:     []string{"git restore failed: boom"},
	}}
	r := NewDefaultSnapshotRestorer(fake)
	err := r.RestoreStageSnapshot(context.Background(), &Flow{ID: "f"}, &Stage{ID: "s"}, "snap-1")
	if err == nil {
		t.Fatal("expected partial restore errors to surface as an error")
	}
	if !strings.Contains(err.Error(), "git restore failed: boom") {
		t.Fatalf("error=%q want it to include the partial failure", err.Error())
	}
}

// A nil service (or nil restorer) is a successful no-op, matching DefaultSnapshotHook.
func TestDefaultSnapshotRestorerNilServiceNoOp(t *testing.T) {
	r := NewDefaultSnapshotRestorer(nil)
	if err := r.RestoreStageSnapshot(context.Background(), &Flow{ID: "f"}, &Stage{ID: "s"}, "snap"); err != nil {
		t.Fatalf("nil service should no-op, got %v", err)
	}
	var nilRestorer *DefaultSnapshotRestorer
	if err := nilRestorer.RestoreStageSnapshot(context.Background(), &Flow{ID: "f"}, &Stage{ID: "s"}, "snap"); err != nil {
		t.Fatalf("nil restorer should no-op, got %v", err)
	}
}

// DefaultSnapshotRestorer satisfies the SnapshotRestorer interface.
func TestDefaultSnapshotRestorerImplementsInterface(t *testing.T) {
	var _ SnapshotRestorer = NewDefaultSnapshotRestorer(nil)
}
