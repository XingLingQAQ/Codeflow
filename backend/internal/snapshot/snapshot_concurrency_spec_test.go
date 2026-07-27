// Package snapshot - concurrency & -race spec tests.
//
// Coverage targets NOT in snapshot_test.go:
//   - Concurrent Create/Restore/List/Get/Delete under -race
//   - Restore does not hold the write lock during provider work (no deadlock)
package snapshot

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Counting provider — stateless except atomic call counters, race-safe
// ---------------------------------------------------------------------------

type countingStateProvider struct {
	captures int64
	restores int64
}

func (p *countingStateProvider) CaptureGitState(context.Context) (string, error) {
	atomic.AddInt64(&p.captures, 1)
	return "git-hash", nil
}
func (p *countingStateProvider) CaptureConversationState(context.Context, string) (string, error) {
	atomic.AddInt64(&p.captures, 1)
	return "conversation-token", nil
}
func (p *countingStateProvider) CaptureVectorState(context.Context, string) (string, error) {
	atomic.AddInt64(&p.captures, 1)
	return "vector-token", nil
}
func (p *countingStateProvider) CaptureMemoryGraphState(context.Context) (string, error) {
	atomic.AddInt64(&p.captures, 1)
	return "graph-token", nil
}
func (p *countingStateProvider) RestoreGitState(context.Context, string) error {
	atomic.AddInt64(&p.restores, 1)
	return nil
}
func (p *countingStateProvider) RestoreConversationState(context.Context, string) error {
	atomic.AddInt64(&p.restores, 1)
	return nil
}
func (p *countingStateProvider) RestoreVectorState(context.Context, string) error {
	atomic.AddInt64(&p.restores, 1)
	return nil
}
func (p *countingStateProvider) RestoreMemoryGraphState(context.Context, string) error {
	atomic.AddInt64(&p.restores, 1)
	return nil
}

// ---------------------------------------------------------------------------
// Blocking provider — blocks inside RestoreGitState on a channel
// ---------------------------------------------------------------------------

type blockingRestoreProvider struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *blockingRestoreProvider) CaptureGitState(context.Context) (string, error) {
	return "git-hash", nil
}
func (p *blockingRestoreProvider) CaptureConversationState(context.Context, string) (string, error) {
	return "conversation-token", nil
}
func (p *blockingRestoreProvider) CaptureVectorState(context.Context, string) (string, error) {
	return "vector-token", nil
}
func (p *blockingRestoreProvider) CaptureMemoryGraphState(context.Context) (string, error) {
	return "graph-token", nil
}
func (p *blockingRestoreProvider) RestoreGitState(context.Context, string) error {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return nil
}
func (p *blockingRestoreProvider) RestoreConversationState(context.Context, string) error {
	return nil
}
func (p *blockingRestoreProvider) RestoreVectorState(context.Context, string) error {
	return nil
}
func (p *blockingRestoreProvider) RestoreMemoryGraphState(context.Context, string) error {
	return nil
}

// ===========================================================================
// Concurrent operations under -race
// ===========================================================================

func TestConcurrentSnapshotOperationsAreRaceFree(t *testing.T) {
	provider := &countingStateProvider{}
	svc := NewInMemorySnapshotServiceWithProvider(provider)
	ctx := context.Background()

	const workers = 8
	const iters = 40

	var (
		wg    sync.WaitGroup
		idsMu sync.Mutex
		ids   []string
	)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				snap, err := svc.Create(ctx, &SnapshotCreateRequest{
					Description: "concurrent",
					SessionID:   "s",
				})
				if err != nil || snap == nil {
					continue
				}
				idsMu.Lock()
				ids = append(ids, snap.ID)
				idsMu.Unlock()

				_, _ = svc.Restore(ctx, snap.ID)
				_, _ = svc.Get(ctx, snap.ID)
				_, _ = svc.List(ctx, &SnapshotListOptions{Limit: 5})
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < workers*iters; i++ {
			idsMu.Lock()
			var id string
			if len(ids) > 0 {
				id = ids[len(ids)-1]
			}
			idsMu.Unlock()
			if id != "" {
				_ = svc.Delete(ctx, id)
			}
		}
	}()

	wg.Wait()

	if _, err := svc.List(ctx, &SnapshotListOptions{Limit: 10}); err != nil {
		t.Fatalf("List after concurrency: %v", err)
	}
	if atomic.LoadInt64(&provider.captures) == 0 {
		t.Fatal("expected capture calls to have run")
	}
	if atomic.LoadInt64(&provider.restores) == 0 {
		t.Fatal("expected restore calls to have run")
	}
}

// ===========================================================================
// Restore does not hold write lock during provider work
// ===========================================================================

// TestRestoreDoesNotHoldWriteLockDuringProviderWork starts a Restore that
// blocks inside a provider method. While blocked, a concurrent Create must
// succeed within a short timeout, proving the service releases its lock before
// calling providers. If the implementation ever regresses to holding the lock
// across provider calls, this test detects the resulting deadlock.
func TestRestoreDoesNotHoldWriteLockDuringProviderWork(t *testing.T) {
	provider := &blockingRestoreProvider{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc := NewInMemorySnapshotServiceWithProvider(provider)
	ctx := context.Background()

	pre, err := svc.Create(ctx, &SnapshotCreateRequest{Description: "pre-restore"})
	if err != nil {
		t.Fatalf("Create pre-restore: %v", err)
	}

	restoreDone := make(chan struct{})
	go func() {
		defer close(restoreDone)
		_, _ = svc.Restore(ctx, pre.ID)
	}()

	select {
	case <-provider.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("restore never reached provider work")
	}

	createDone := make(chan struct{})
	go func() {
		defer close(createDone)
		_, _ = svc.Create(ctx, &SnapshotCreateRequest{Description: "concurrent-create"})
	}()

	select {
	case <-createDone:
	case <-time.After(2 * time.Second):
		close(provider.release)
		<-restoreDone
		t.Fatal("Create blocked while Restore was in provider work — restore holds the write lock")
	}

	close(provider.release)
	<-restoreDone
}
