package guard

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestExemptionConcurrentGrantClearCheck exercises the exemption map under
// concurrent grant/clear/isExempt/list to prove the engine lock is race-free.
// Run with -race.
func TestExemptionConcurrentGrantClearCheck(t *testing.T) {
	e := NewEngine(nil, nil)
	const workers = 8
	const iters = 250
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			path := filepath.Join("proj", fmt.Sprintf("f%d.go", id%4))
			for i := 0; i < iters; i++ {
				switch i % 4 {
				case 0:
					_ = e.GrantExemption(Exemption{
						Path:      path,
						Rules:     []RuleID{RuleStackedNaming},
						ExpiresAt: time.Now().UTC().Add(time.Minute),
					})
				case 1:
					_ = e.isExempt(path, RuleStackedNaming)
				case 2:
					_ = e.ListExemptions()
				case 3:
					_ = e.ClearExemption(path)
				}
			}
		}(w)
	}
	wg.Wait()
	// Engine stays usable after concurrent churn.
	_ = e.ListExemptions()
}

// TestExemptionExpiryHonoredAndPruned verifies that an expired exemption is not
// honored by isExempt and that ListExemptions drops expired entries while
// retaining valid ones.
func TestExemptionExpiryHonoredAndPruned(t *testing.T) {
	ctx := context.Background()

	// (a) isExempt must not honor an expired exemption.
	e := NewEngine(nil, nil)
	stacked := filepath.Join("proj", "utils2.go")
	e.GrantExemption(Exemption{
		Path:      stacked,
		Rules:     []RuleID{RuleStackedNaming},
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	})
	if e.isExempt(stacked, RuleStackedNaming) {
		t.Fatal("expired exemption must not be active")
	}
	if err := e.BeforeWrite(ctx, stacked, []byte("package p\n")); err == nil {
		t.Fatal("expired exemption should not bypass stacked_naming")
	}

	// (b) ListExemptions prunes expired entries and keeps valid ones.
	e2 := NewEngine(nil, nil)
	expired := filepath.Join("proj", "old2.go")
	valid := filepath.Join("proj", "new2.go")
	e2.GrantExemption(Exemption{Path: expired, ExpiresAt: time.Now().UTC().Add(-time.Minute)})
	e2.GrantExemption(Exemption{Path: valid, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	listed := e2.ListExemptions()
	if len(listed) != 1 {
		t.Fatalf("expected only valid exemption after prune, got %d: %+v", len(listed), listed)
	}
	if listed[0].Path != filepath.Clean(valid) {
		t.Fatalf("expected valid exemption retained, got %q", listed[0].Path)
	}
	// Second call is stable (expired entry already removed from the map).
	if again := e2.ListExemptions(); len(again) != 1 {
		t.Fatalf("expected stable list after prune, got %d", len(again))
	}
}
