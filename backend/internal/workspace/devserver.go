package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/policy"
)

// ScriptInfo describes one npm-style script detected in package.json.
type ScriptInfo struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// DetectScripts reads <root>/package.json through the workspace Resolve sandbox
// and returns the "scripts" entries as a sorted slice. A missing package.json
// is not an error (many roots are not Node projects); malformed JSON is.
func DetectScripts(svc Service, root string) ([]ScriptInfo, error) {
	if svc == nil {
		svc = NewFSService(nil)
	}
	abs, err := svc.Resolve(root, "package.json")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parse package.json: %w", err)
	}
	if len(pkg.Scripts) == 0 {
		return nil, nil
	}
	out := make([]ScriptInfo, 0, len(pkg.Scripts))
	for name, cmd := range pkg.Scripts {
		out = append(out, ScriptInfo{Name: name, Command: cmd})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// DevServerStatus is the lifecycle state of a managed dev-server process.
type DevServerStatus string

const (
	DevServerRunning DevServerStatus = "running"
	DevServerExited  DevServerStatus = "exited"
	DevServerFailed  DevServerStatus = "failed"
)

// DevServerHandle is the public view of one managed dev-server process.
type DevServerHandle struct {
	ID        string          `json:"id"`
	Root      string          `json:"root"`
	Script    string          `json:"script"`
	Command   string          `json:"command"`
	StartedAt time.Time       `json:"started_at"`
	Status    DevServerStatus `json:"status"`
	ExitCode  *int            `json:"exit_code,omitempty"`
}

// MaxDevServers is the process-wide cap on concurrent managed dev-servers.
const MaxDevServers = 8

// DefaultLogLines is the ring-buffer capacity for captured output.
const DefaultLogLines = 2000

// stopGracePeriod is how long Stop waits after kill before giving up.
const stopGracePeriod = 5 * time.Second

// devEntry is the internal bookkeeping for one managed process.
type devEntry struct {
	handle  DevServerHandle
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	doneCh  chan struct{}
	logMu   sync.Mutex
	logBuf  []string
	logCap  int
	logHead int // ring-buffer write position
	logSize int // number of valid entries (<= logCap)
}

// appendLog adds a line to the ring buffer (caller need not hold logMu).
func (e *devEntry) appendLog(line string) {
	e.logMu.Lock()
	if e.logSize < e.logCap {
		e.logBuf = append(e.logBuf, line)
		e.logSize++
	} else {
		e.logBuf[e.logHead] = line
	}
	e.logHead = (e.logHead + 1) % e.logCap
	e.logMu.Unlock()
}

// tailLog returns the most recent n lines (or all if n <= 0).
func (e *devEntry) tailLog(n int) []string {
	e.logMu.Lock()
	defer e.logMu.Unlock()
	total := e.logSize
	if n <= 0 || n > total {
		n = total
	}
	if total == 0 {
		return nil
	}
	out := make([]string, n)
	start := (e.logHead - n + e.logCap) % e.logCap
	for i := 0; i < n; i++ {
		out[i] = e.logBuf[(start+i)%e.logCap]
	}
	return out
}

// DevServerManager manages dev-server processes for workspace roots.
type DevServerManager struct {
	svc    Service
	logCap int

	mu     sync.Mutex
	byID   map[string]*devEntry
	byKey  map[string]*devEntry // key = root + "\x00" + script
	nextID int
}

// NewDevServerManager creates a manager. svc provides Resolve for path safety;
// nil falls back to an unrestricted FSService.
func NewDevServerManager(svc Service) *DevServerManager {
	if svc == nil {
		svc = NewFSService(nil)
	}
	return &DevServerManager{
		svc:    svc,
		logCap: DefaultLogLines,
		byID:   make(map[string]*devEntry),
		byKey:  make(map[string]*devEntry),
	}
}

func devKey(root, script string) string { return root + "\x00" + script }

// Start launches a dev-server process for the given script in root. The script
// command is executed directly (not via npm run) so tests work without an npm
// installation. On Windows the command runs through "cmd /c"; on other
// platforms through "sh -c". Start is idempotent per (root, script): a second
// call returns the existing handle. Returns an error when the per-process cap
// is reached.
func (m *DevServerManager) Start(root, script string) (*DevServerHandle, error) {
	return m.StartContext(context.Background(), root, script)
}

// StartContext launches a dev server with the caller's project/agent trace.
func (m *DevServerManager) StartContext(ctx context.Context, root, script string) (*DevServerHandle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	absRoot, err := m.svc.Resolve(root, ".")
	if err != nil {
		return nil, err
	}

	scripts, err := DetectScripts(m.svc, absRoot)
	if err != nil {
		return nil, fmt.Errorf("detect scripts: %w", err)
	}
	var command string
	for _, s := range scripts {
		if s.Name == script {
			command = s.Command
			break
		}
	}
	if command == "" {
		return nil, fmt.Errorf("script %q not found in package.json", script)
	}
	trace := audit.TraceFromContext(ctx)
	policyReq := policy.Request{Operation: policy.OperationProcessStart, Resource: command}
	if trace != nil {
		policyReq.ProjectID, policyReq.AgentID = trace.ProjectID, trace.AgentID
	}
	decision := policy.EvaluateBoundary(ctx, policyReq)
	if err := policy.DenialError(decision); err != nil {
		return nil, err
	}

	key := devKey(absRoot, script)

	m.mu.Lock()
	if existing, ok := m.byKey[key]; ok {
		h := existing.handle
		m.mu.Unlock()
		return &h, nil
	}
	if len(m.byID) >= MaxDevServers {
		m.mu.Unlock()
		return nil, fmt.Errorf("dev-server limit reached (%d)", MaxDevServers)
	}
	m.nextID++
	id := fmt.Sprintf("dev-%d", m.nextID)

	ctx, cancel := context.WithCancel(ctx)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Dir = absRoot

	entry := &devEntry{
		handle: DevServerHandle{
			ID:        id,
			Root:      absRoot,
			Script:    script,
			Command:   command,
			StartedAt: time.Now().UTC(),
			Status:    DevServerRunning,
		},
		cmd:    cmd,
		cancel: cancel,
		doneCh: make(chan struct{}),
		logBuf: make([]string, 0, m.logCap),
		logCap: m.logCap,
	}

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		m.mu.Unlock()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout // merge stderr into the same pipe

	if err := cmd.Start(); err != nil {
		cancel()
		m.mu.Unlock()
		return nil, fmt.Errorf("start process: %w", err)
	}

	m.byID[id] = entry
	m.byKey[key] = entry
	m.mu.Unlock()

	go m.supervise(entry, cmd, pipe)

	h := entry.handle
	return &h, nil
}

// supervise reads combined output and waits for process exit.
func (m *DevServerManager) supervise(entry *devEntry, cmd *exec.Cmd, pipe interface{ Read([]byte) (int, error) }) {
	defer close(entry.doneCh)

	buf := make([]byte, 4096)
	var partial string
	for {
		n, err := pipe.Read(buf)
		if n > 0 {
			chunk := partial + string(buf[:n])
			lines := strings.Split(chunk, "\n")
			partial = lines[len(lines)-1]
			for _, line := range lines[:len(lines)-1] {
				entry.appendLog(strings.TrimRight(line, "\r"))
			}
		}
		if err != nil {
			if partial != "" {
				entry.appendLog(strings.TrimRight(partial, "\r"))
			}
			break
		}
	}

	waitErr := cmd.Wait()

	m.mu.Lock()
	if waitErr != nil {
		entry.handle.Status = DevServerFailed
	} else {
		entry.handle.Status = DevServerExited
	}
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		entry.handle.ExitCode = &code
	}
	m.mu.Unlock()
}

// killProcessTree kills a process and all its descendants. On Windows this uses
// taskkill /t /f to kill the whole tree (exec.CommandContext only terminates the
// direct child, leaving grandchildren like npm-spawned servers orphaned). On
// Unix, context cancellation sends SIGKILL which is sufficient.
func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		kill := exec.Command("taskkill", "/t", "/f", "/pid", fmt.Sprintf("%d", cmd.Process.Pid))
		_ = kill.Run()
	} else {
		_ = cmd.Process.Kill()
	}
}

// stopEntry kills the process tree, waits for the supervisor goroutine, and
// returns. Separated from Stop so Shutdown can share it.
//
// On Windows the ordering matters: killProcessTree (taskkill /t /f) must run
// BEFORE cancel, because cancel triggers CommandContext's TerminateProcess on
// the direct child only — killing cmd.exe before taskkill can enumerate its
// children orphans grandchild processes (e.g. the actual server). With tree-kill
// first, all descendants are terminated while the process tree is still intact.
func stopEntry(e *devEntry) {
	killProcessTree(e.cmd)
	e.cancel()
	select {
	case <-e.doneCh:
	case <-time.After(stopGracePeriod):
	}
}

// Stop terminates a running dev-server. It kills the entire process tree (on
// Windows via taskkill /t /f, on Unix via SIGKILL), waits for the supervisor
// goroutine to drain, and removes the entry. Unknown IDs return an error;
// already-exited processes are cleaned up without error.
func (m *DevServerManager) Stop(id string) error {
	m.mu.Lock()
	entry, ok := m.byID[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("dev-server not found: %s", id)
	}
	delete(m.byID, id)
	delete(m.byKey, devKey(entry.handle.Root, entry.handle.Script))
	m.mu.Unlock()

	stopEntry(entry)
	return nil
}

// StopRoot terminates every tracked dev-server owned by root. It is idempotent
// and returns the number of entries removed.
func (m *DevServerManager) StopRoot(root string) int {
	root = filepath.Clean(root)
	m.mu.Lock()
	entries := make([]*devEntry, 0)
	for id, entry := range m.byID {
		entryRoot := filepath.Clean(entry.handle.Root)
		matches := entryRoot == root
		if runtime.GOOS == "windows" {
			matches = strings.EqualFold(entryRoot, root)
		}
		if !matches {
			continue
		}
		delete(m.byID, id)
		delete(m.byKey, devKey(entry.handle.Root, entry.handle.Script))
		entries = append(entries, entry)
	}
	m.mu.Unlock()
	for _, entry := range entries {
		stopEntry(entry)
	}
	return len(entries)
}

// Logs returns the most recent n lines of combined stdout/stderr for the given
// dev-server. Pass n <= 0 for all buffered lines.
func (m *DevServerManager) Logs(id string, n int) ([]string, error) {
	m.mu.Lock()
	entry, ok := m.byID[id]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("dev-server not found: %s", id)
	}
	return entry.tailLog(n), nil
}

// List returns a snapshot of all tracked dev-server handles.
func (m *DevServerManager) List() []DevServerHandle {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]DevServerHandle, 0, len(m.byID))
	for _, e := range m.byID {
		out = append(out, e.handle)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Shutdown stops all running dev-servers and waits for them to drain. Safe to
// call multiple times. Designed to be deferred in main.go so no goroutine
// outlives the server — addressing the watch-registry P2-1 finding proactively.
func (m *DevServerManager) Shutdown() {
	m.mu.Lock()
	entries := make([]*devEntry, 0, len(m.byID))
	for _, e := range m.byID {
		entries = append(entries, e)
	}
	m.byID = make(map[string]*devEntry)
	m.byKey = make(map[string]*devEntry)
	m.mu.Unlock()

	for _, e := range entries {
		stopEntry(e)
	}
}

// writeTestPackageJSON is a test helper that writes a package.json with the
// given scripts map into dir.
func writeTestPackageJSON(dir string, scripts map[string]string) error {
	pkg := struct {
		Name    string            `json:"name"`
		Scripts map[string]string `json:"scripts"`
	}{
		Name:    "test-project",
		Scripts: scripts,
	}
	data, err := json.Marshal(pkg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "package.json"), data, 0o644)
}
