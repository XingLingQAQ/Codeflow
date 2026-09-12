// Package handlers - Workspace file-watch API (experimental).
package handlers

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/codeflow/backend/internal/websocket"
	"github.com/codeflow/backend/internal/workspace"
)

// Workspace watch tuning. A client-supplied poll cadence is clamped into
// [watchIntervalMinMs, watchIntervalMaxMs]; maxActiveWatches caps the number of
// concurrent polling goroutines per process so a client cannot spawn unbounded
// watchers (excess create requests get 409).
const (
	watchIntervalMinMs     = 250
	watchIntervalMaxMs     = 60000
	watchIntervalDefaultMs = 2000
	maxActiveWatches       = 16
)

// watchEntry is one active workspace watcher plus the plumbing to stop it.
// Fields are set once at creation and treated as immutable thereafter; the
// registry mutex guards map membership, not these fields.
type watchEntry struct {
	id         string
	root       string // resolved absolute project root
	topic      string // per-root websocket fan-out topic
	intervalMs int
	createdAt  time.Time
	watcher    *workspace.Watcher
	cancel     context.CancelFunc
}

// watchView is the JSON shape returned for a watch.
type watchView struct {
	WatchID    string    `json:"watch_id"`
	Root       string    `json:"root"`
	Topic      string    `json:"topic"`
	IntervalMs int       `json:"interval_ms"`
	CreatedAt  time.Time `json:"created_at"`
}

func (e *watchEntry) view() watchView {
	return watchView{
		WatchID:    e.id,
		Root:       e.root,
		Topic:      e.topic,
		IntervalMs: e.intervalMs,
		CreatedAt:  e.createdAt,
	}
}

// watchRegistryT tracks active watchers by id and by resolved root. The by-root
// index enforces idempotency: a second watch of the same resolved root returns
// the existing entry instead of spawning a duplicate poller.
type watchRegistryT struct {
	mu     sync.Mutex
	byID   map[string]*watchEntry
	byRoot map[string]*watchEntry
}

var watchRegistry = &watchRegistryT{
	byID:   make(map[string]*watchEntry),
	byRoot: make(map[string]*watchEntry),
}

// clampWatchInterval clamps a millisecond interval into the accepted band,
// substituting the default for non-positive values.
func clampWatchInterval(ms int) int {
	if ms <= 0 {
		ms = watchIntervalDefaultMs
	}
	if ms < watchIntervalMinMs {
		return watchIntervalMinMs
	}
	if ms > watchIntervalMaxMs {
		return watchIntervalMaxMs
	}
	return ms
}

type createWatchBody struct {
	Root       string `json:"root"`
	ProjectID  string `json:"project_id"`
	IntervalMs int    `json:"interval_ms"`
}

// CreateWorkspaceWatch handles POST /api/v1/workspace/watch.
// Body {root?, interval_ms?}; root is also honored from the X-Codeflow-Workspace-Root
// header or ?root= query with the usual precedence. It starts a polling watcher
// that broadcasts created/modified/deleted events on the per-root websocket topic.
// Idempotent per resolved root: an existing watch is returned with 200 instead of
// spawning a duplicate; 201 is returned for a freshly created watch.
func CreateWorkspaceWatch(c *gin.Context) {
	if !workspace.HasService() {
		respondError(c, http.StatusServiceUnavailable, "workspace service not available")
		return
	}
	var body createWatchBody
	if err := c.ShouldBindJSON(&body); err != nil {
		// Empty body is allowed when root arrives via header/query.
		if c.Request.ContentLength > 0 {
			respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
	}
	if body.ProjectID != "" && c.GetHeader("X-Codeflow-Project-ID") == "" {
		c.Request.Header.Set("X-Codeflow-Project-ID", body.ProjectID)
	}
	root := workspaceRootFromRequest(c, body.Root)
	if root == "" {
		respondError(c, http.StatusBadRequest, "root is required (query root= or header X-Codeflow-Workspace-Root)")
		return
	}
	// Validate + canonicalize the root through the same sandbox path every
	// workspace op uses. Resolve(root, ".") returns the absolute, symlink-resolved
	// directory, which keys idempotency and derives the fan-out topic.
	svc := workspace.GetService()
	absRoot, err := svc.Resolve(root, ".")
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	interval := clampWatchInterval(body.IntervalMs)

	// Reserve a registry slot atomically: dedup by resolved root and enforce the
	// per-process cap under the lock, but start the poller (which does disk I/O)
	// outside it.
	watchRegistry.mu.Lock()
	if existing, ok := watchRegistry.byRoot[absRoot]; ok {
		view := existing.view()
		watchRegistry.mu.Unlock()
		respondOK(c, view)
		return
	}
	if len(watchRegistry.byID) >= maxActiveWatches {
		watchRegistry.mu.Unlock()
		respondError(c, http.StatusConflict, "active workspace watch limit reached")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	entry := &watchEntry{
		id:         uuid.NewString(),
		root:       absRoot,
		topic:      workspace.WorkspaceTopicForRoot(absRoot),
		intervalMs: interval,
		createdAt:  time.Now().UTC(),
		watcher: workspace.NewWatcher(svc, absRoot,
			workspace.WithInterval(time.Duration(interval)*time.Millisecond),
			workspace.WithNotifier(workspace.NewWSNotifier(websocket.GetHub())),
		),
		cancel: cancel,
	}
	watchRegistry.byID[entry.id] = entry
	watchRegistry.byRoot[absRoot] = entry
	watchRegistry.mu.Unlock()

	if err := entry.watcher.Start(ctx); err != nil {
		watchRegistry.mu.Lock()
		delete(watchRegistry.byID, entry.id)
		delete(watchRegistry.byRoot, entry.root)
		watchRegistry.mu.Unlock()
		cancel()
		respondInternalError(c, "start workspace watch", err)
		return
	}
	respondCreated(c, entry.view())
}

// ListWorkspaceWatches handles GET /api/v1/workspace/watches.
func ListWorkspaceWatches(c *gin.Context) {
	if !workspace.HasService() {
		respondError(c, http.StatusServiceUnavailable, "workspace service not available")
		return
	}
	watchRegistry.mu.Lock()
	items := make([]watchView, 0, len(watchRegistry.byID))
	for _, e := range watchRegistry.byID {
		items = append(items, e.view())
	}
	watchRegistry.mu.Unlock()
	respondOK(c, gin.H{"items": items, "total": len(items)})
}

type deleteWatchBody struct {
	WatchID string `json:"watch_id"`
}

// DeleteWorkspaceWatch handles DELETE /api/v1/workspace/watch?id=.
// The id is also accepted as JSON {watch_id}. It stops the poller and removes the
// registry entry; an unknown id returns 404.
func DeleteWorkspaceWatch(c *gin.Context) {
	if !workspace.HasService() {
		respondError(c, http.StatusServiceUnavailable, "workspace service not available")
		return
	}
	id := strings.TrimSpace(c.Query("id"))
	if id == "" && c.Request.ContentLength > 0 {
		var body deleteWatchBody
		if err := c.ShouldBindJSON(&body); err != nil {
			respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
			return
		}
		id = strings.TrimSpace(body.WatchID)
	}
	if id == "" {
		respondError(c, http.StatusBadRequest, "id is required (query id= or JSON watch_id)")
		return
	}
	watchRegistry.mu.Lock()
	entry, ok := watchRegistry.byID[id]
	if ok {
		delete(watchRegistry.byID, id)
		delete(watchRegistry.byRoot, entry.root)
	}
	watchRegistry.mu.Unlock()
	if !ok {
		respondError(c, http.StatusNotFound, "watch not found")
		return
	}
	// Cancel the context the registry owns, then join the poll goroutine so no
	// watcher outlives its registry entry.
	entry.cancel()
	entry.watcher.Stop()
	respondOK(c, gin.H{"watch_id": id, "stopped": true})
}

// ShutdownWorkspaceWatches stops all active workspace watchers and clears the
// registry. Safe to call during graceful server shutdown; idempotent (a second
// call is a no-op). Watcher goroutines are joined via Stop so none outlive the
// call.
func ShutdownWorkspaceWatches() {
	watchRegistry.mu.Lock()
	entries := make([]*watchEntry, 0, len(watchRegistry.byID))
	for _, e := range watchRegistry.byID {
		entries = append(entries, e)
	}
	watchRegistry.byID = make(map[string]*watchEntry)
	watchRegistry.byRoot = make(map[string]*watchEntry)
	watchRegistry.mu.Unlock()
	for _, e := range entries {
		e.cancel()
		e.watcher.Stop()
	}
}

// StopWorkspaceWatchesForRoot stops the process-local watcher owned by root.
// At most one exists because the registry is idempotent per canonical root.
func StopWorkspaceWatchesForRoot(root string) int {
	root = strings.TrimSpace(root)
	if root == "" {
		return 0
	}
	watchRegistry.mu.Lock()
	entry, ok := watchRegistry.byRoot[root]
	if ok {
		delete(watchRegistry.byID, entry.id)
		delete(watchRegistry.byRoot, root)
	}
	watchRegistry.mu.Unlock()
	if !ok {
		return 0
	}
	entry.cancel()
	entry.watcher.Stop()
	return 1
}
