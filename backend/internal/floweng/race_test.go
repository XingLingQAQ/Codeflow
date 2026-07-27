package floweng

import (
	"context"
	"sync"
	"testing"
)

// P2-4 race fix: Create now holds e.mu before reading e.notifier (via
// appendEvent). This test exercises concurrent Create + SetEventNotifier to
// prove no data race on the notifier field under -race.
func TestCreateAndSetEventNotifierConcurrent(t *testing.T) {
	e := NewInMemoryEngine(nil)
	const n = 32
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = e.Create(context.Background(), &CreateFlowRequest{ProjectID: "p"})
		}()
		go func() {
			defer wg.Done()
			e.SetEventNotifier(&captureNotifier{})
		}()
	}
	wg.Wait()
}
