package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/websocket"
)

// recordNotifier captures events from a Watcher in a race-safe way.
type recordNotifier struct {
	mu     sync.Mutex
	events []Event
	roots  []string
}

func (r *recordNotifier) OnWorkspaceEvent(root string, ev Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.roots = append(r.roots, root)
	r.mu.Unlock()
}

func (r *recordNotifier) has(typ EventType, path string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.Type == typ && e.Path == path {
			return true
		}
	}
	return false
}

func (r *recordNotifier) snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestWatcherDetectsCreateModifyDelete(t *testing.T) {
	root := t.TempDir()
	rec := &recordNotifier{}
	w := NewWatcher(nil, root, WithInterval(25*time.Millisecond), WithNotifier(rec))
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return rec.has(EventCreated, "a.txt") }, "created event")

	// Size differs from the baseline so the change is caught regardless of
	// filesystem mod-time resolution.
	if err := os.WriteFile(target, []byte("two-longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return rec.has(EventModified, "a.txt") }, "modified event")

	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return rec.has(EventDeleted, "a.txt") }, "deleted event")

	// The reported root is the watched root for every event.
	rec.mu.Lock()
	for _, got := range rec.roots {
		if got != root {
			rec.mu.Unlock()
			t.Fatalf("event root=%q want %q", got, root)
		}
	}
	rec.mu.Unlock()
}

func TestWatcherIgnoresStaging(t *testing.T) {
	root := t.TempDir()
	rec := &recordNotifier{}
	w := NewWatcher(nil, root, WithInterval(25*time.Millisecond), WithNotifier(rec))
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Write into the staging shadow tree; it must never emit an event.
	stagingFile := filepath.Join(root, ".codeflow", "staging", "src", "s.txt")
	if err := os.MkdirAll(filepath.Dir(stagingFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagingFile, []byte("staged"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A normal file gives a positive signal to synchronize on: once it is seen a
	// full poll cycle has run and any staging change would already have surfaced.
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return rec.has(EventCreated, "real.txt") }, "real.txt created")

	for _, e := range rec.snapshot() {
		if strings.HasPrefix(e.Path, ".codeflow/staging") {
			t.Fatalf("staging change leaked as event: %+v", e)
		}
	}
}

func TestWatcherRootVanishEmitsDeletes(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := &recordNotifier{}
	w := NewWatcher(nil, root, WithInterval(25*time.Millisecond), WithNotifier(rec))
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Add a file after baseline so we know polling is live.
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return rec.has(EventCreated, "new.txt") }, "new.txt created")

	// Remove the entire root: the watcher should report the tracked files as
	// deleted and not crash.
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return rec.has(EventDeleted, "keep.txt") }, "keep.txt deleted")
	waitFor(t, func() bool { return rec.has(EventDeleted, "new.txt") }, "new.txt deleted")
}

func TestWatcherStopIdempotent(t *testing.T) {
	root := t.TempDir()
	w := NewWatcher(nil, root, WithInterval(20*time.Millisecond))
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.Stop()
	w.Stop() // must not panic or block

	// The watcher is single-use; restart is rejected.
	if err := w.Start(context.Background()); err == nil {
		t.Fatal("restart should be rejected")
	}
}

func TestWatcherStopBeforeStart(t *testing.T) {
	w := NewWatcher(nil, t.TempDir())
	w.Stop() // no-op, must not panic or block
}

func TestWatcherContextCancelStopsGoroutine(t *testing.T) {
	root := t.TempDir()
	w := NewWatcher(nil, root, WithInterval(20*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()

	// The goroutine closes doneCh on exit; no leak.
	select {
	case <-w.doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher goroutine did not exit after context cancel")
	}
	w.Stop() // still safe after context cancellation
}

func TestWorkspaceTopicForRoot(t *testing.T) {
	a := WorkspaceTopicForRoot(filepath.Join("tmp", "project-a"))
	b := WorkspaceTopicForRoot(filepath.Join("tmp", "project-b"))
	if a == b {
		t.Fatal("distinct roots must map to distinct topics")
	}
	if a != WorkspaceTopicForRoot(filepath.Join("tmp", "project-a")) {
		t.Fatal("topic must be deterministic for the same root")
	}
	if !strings.HasPrefix(a, "workspace:root:") {
		t.Fatalf("unexpected topic format: %s", a)
	}
}

func TestWSNotifierDoesNotPanic(t *testing.T) {
	n := NewWSNotifier(nil) // nil hub falls back to the global hub
	if n.Hub == nil {
		t.Fatal("expected fallback hub")
	}
	n.OnWorkspaceEvent(t.TempDir(), Event{Type: EventCreated, Path: "a.txt", Ts: time.Now()})

	// nil receiver / nil hub are safe.
	var np *WSNotifier
	np.OnWorkspaceEvent("/x", Event{})
	(&WSNotifier{}).OnWorkspaceEvent("/x", Event{})
}

func TestWSNotifierBroadcastsToTopic(t *testing.T) {
	hub := websocket.NewHub() // BroadcastToTopic works without Run()
	client := &websocket.Client{ID: "ws-1", Send: make(chan []byte, 8)}
	hub.SubscribeTopic(client, websocket.TopicWorkspaceEvent)

	n := NewWSNotifier(hub)
	n.OnWorkspaceEvent(t.TempDir(), Event{Type: EventModified, Path: "main.go", Ts: time.Now()})

	select {
	case raw := <-client.Send:
		if len(raw) == 0 {
			t.Fatal("empty payload on subscriber channel")
		}
	default:
		t.Fatal("expected workspace event on subscriber channel")
	}
}
