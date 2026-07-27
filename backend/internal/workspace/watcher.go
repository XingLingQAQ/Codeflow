package workspace

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EventType classifies a filesystem change detected by a Watcher.
type EventType string

const (
	// EventCreated is emitted when a file appears under the watched root.
	EventCreated EventType = "created"
	// EventModified is emitted when a tracked file's size or mod time changes.
	EventModified EventType = "modified"
	// EventDeleted is emitted when a previously tracked file disappears.
	EventDeleted EventType = "deleted"
)

// Event is a single filesystem change under a watched root.
type Event struct {
	Type EventType `json:"type"`
	// Path is relative to the watched root, slash-separated.
	Path string `json:"path"`
	// Ts is when the change was observed (UTC).
	Ts time.Time `json:"ts"`
}

// Notifier receives workspace change events. The Watcher depends only on this
// interface so the core stays decoupled from the websocket package, mirroring
// floweng.EventNotifier. WSNotifier (ws_notifier.go) is the production adapter.
type Notifier interface {
	OnWorkspaceEvent(root string, ev Event)
}

// NotifierFunc adapts a plain function to Notifier.
type NotifierFunc func(root string, ev Event)

// OnWorkspaceEvent implements Notifier.
func (f NotifierFunc) OnWorkspaceEvent(root string, ev Event) { f(root, ev) }

// DefaultPollInterval is the fallback poll cadence when none is configured.
const DefaultPollInterval = 2 * time.Second

// watchIgnoredDirs are directory names never descended into while scanning.
var watchIgnoredDirs = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"dist":         {},
	"vendor":       {},
}

// Watcher polls a project root and reports created/modified/deleted files to a
// Notifier. It uses a single goroutine and standard-library polling (no fsnotify
// dependency). Writes performed through FSService are observed here on the next
// poll; the watcher is intentionally not coupled to Write.
type Watcher struct {
	svc      Service
	root     string
	interval time.Duration

	mu       sync.Mutex
	notifier Notifier
	started  bool
	stopped  bool
	stopCh   chan struct{}
	doneCh   chan struct{}

	// snapshot is owned exclusively by the poll goroutine after Start.
	snapshot map[string]fileMeta
}

type fileMeta struct {
	modTime time.Time
	size    int64
}

// WatcherOption configures a Watcher.
type WatcherOption func(*Watcher)

// WithInterval sets the poll interval (values <= 0 keep the default).
func WithInterval(d time.Duration) WatcherOption {
	return func(w *Watcher) {
		if d > 0 {
			w.interval = d
		}
	}
}

// WithNotifier registers the sink for change events.
func WithNotifier(n Notifier) WatcherOption {
	return func(w *Watcher) { w.notifier = n }
}

// NewWatcher creates a watcher for root. svc supplies path-sandbox safety
// (Resolve validates the root against allowedRoots and resolves symlinks); when
// nil an unrestricted FSService is used.
func NewWatcher(svc Service, root string, opts ...WatcherOption) *Watcher {
	if svc == nil {
		svc = NewFSService(nil)
	}
	w := &Watcher{
		svc:      svc,
		root:     root,
		interval: DefaultPollInterval,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// SetNotifier replaces the event sink (safe before or during Start).
func (w *Watcher) SetNotifier(n Notifier) {
	w.mu.Lock()
	w.notifier = n
	w.mu.Unlock()
}

// Start records an initial baseline snapshot then polls until ctx is cancelled
// or Stop is called. Pre-existing files do not emit events. The watcher is
// single-use: calling Start again (even after Stop) returns an error.
func (w *Watcher) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return fmt.Errorf("watcher already started")
	}
	w.started = true
	w.stopCh = make(chan struct{})
	w.doneCh = make(chan struct{})
	w.mu.Unlock()

	// Baseline before the goroutine starts; snapshot is single-owner thereafter.
	w.snapshot = w.scan()
	go w.loop(ctx)
	return nil
}

// Stop halts polling and waits for the goroutine to exit. Idempotent and safe to
// call even if Start was never called or the context already stopped the loop.
func (w *Watcher) Stop() {
	w.mu.Lock()
	if !w.started || w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	close(w.stopCh)
	done := w.doneCh
	w.mu.Unlock()
	<-done
}

func (w *Watcher) loop(ctx context.Context) {
	defer close(w.doneCh)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.poll()
		}
	}
}

// poll scans the tree and emits the diff against the previous snapshot.
func (w *Watcher) poll() {
	cur := w.scan()
	prev := w.snapshot
	now := time.Now().UTC()
	for path, meta := range cur {
		old, ok := prev[path]
		if !ok {
			w.emit(Event{Type: EventCreated, Path: path, Ts: now})
			continue
		}
		if !old.modTime.Equal(meta.modTime) || old.size != meta.size {
			w.emit(Event{Type: EventModified, Path: path, Ts: now})
		}
	}
	for path := range prev {
		if _, ok := cur[path]; !ok {
			w.emit(Event{Type: EventDeleted, Path: path, Ts: now})
		}
	}
	w.snapshot = cur
}

func (w *Watcher) emit(ev Event) {
	w.mu.Lock()
	n := w.notifier
	w.mu.Unlock()
	if n != nil {
		n.OnWorkspaceEvent(w.root, ev)
	}
}

// scan walks the watched tree and returns regular-file metadata keyed by
// project-relative path. A missing or disallowed root yields an empty map so the
// next poll reports previously known files as deleted (root-vanish semantics)
// and keeps watching for the root to reappear.
func (w *Watcher) scan() map[string]fileMeta {
	out := make(map[string]fileMeta)
	absRoot, err := w.svc.Resolve(w.root, "")
	if err != nil {
		return out
	}
	_ = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Unreadable entry: skip the subtree if it is a directory.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == absRoot {
			return nil
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return nil
		}
		rel = normalizeRel(rel)
		if d.IsDir() {
			if isIgnoredWatchDir(rel, d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		// Track only regular files. Symlinks are never followed (WalkDir does not
		// descend them) and are ignored to avoid escape and churn.
		if !info.Mode().IsRegular() {
			return nil
		}
		out[rel] = fileMeta{modTime: info.ModTime(), size: info.Size()}
		return nil
	})
	return out
}

// isIgnoredWatchDir reports whether a directory should be skipped during scans.
func isIgnoredWatchDir(rel, name string) bool {
	if _, ok := watchIgnoredDirs[name]; ok {
		return true
	}
	// Skip only the staging shadow tree, not all of .codeflow.
	return rel == ".codeflow/staging" || strings.HasPrefix(rel, ".codeflow/staging/")
}
