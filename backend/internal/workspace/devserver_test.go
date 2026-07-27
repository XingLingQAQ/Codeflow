package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// --- DetectScripts ---

func TestDetectScripts_Sorted(t *testing.T) {
	dir := t.TempDir()
	scripts := map[string]string{
		"dev":   "vite dev",
		"build": "tsc && vite build",
		"test":  "vitest",
		"lint":  "eslint .",
	}
	if err := writeTestPackageJSON(dir, scripts); err != nil {
		t.Fatal(err)
	}
	svc := NewFSService(nil)
	got, err := DetectScripts(svc, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 scripts, got %d", len(got))
	}
	want := []string{"build", "dev", "lint", "test"}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("script[%d] = %q, want %q", i, got[i].Name, name)
		}
	}
	if got[0].Command != "tsc && vite build" {
		t.Errorf("build command = %q, want %q", got[0].Command, "tsc && vite build")
	}
}

func TestDetectScripts_MissingPackageJSON(t *testing.T) {
	dir := t.TempDir()
	svc := NewFSService(nil)
	got, err := DetectScripts(svc, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestDetectScripts_NoScripts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewFSService(nil)
	got, err := DetectScripts(svc, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestDetectScripts_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{bad json`), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewFSService(nil)
	_, err := DetectScripts(svc, dir)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse package.json") {
		t.Errorf("error should mention parse, got: %v", err)
	}
}

// --- Ring buffer ---

func TestRingBuffer_Append_Tail(t *testing.T) {
	e := &devEntry{
		logBuf: make([]string, 0, 4),
		logCap: 4,
	}
	for i := 0; i < 3; i++ {
		e.appendLog(nthLine(i))
	}
	got := e.tailLog(0)
	if len(got) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(got))
	}
	for i, l := range got {
		if l != nthLine(i) {
			t.Errorf("line[%d] = %q, want %q", i, l, nthLine(i))
		}
	}
}

func TestRingBuffer_Overflow(t *testing.T) {
	cap := 4
	e := &devEntry{
		logBuf: make([]string, 0, cap),
		logCap: cap,
	}
	for i := 0; i < 10; i++ {
		e.appendLog(nthLine(i))
	}
	got := e.tailLog(0)
	if len(got) != cap {
		t.Fatalf("expected %d lines, got %d", cap, len(got))
	}
	for i, l := range got {
		want := nthLine(6 + i)
		if l != want {
			t.Errorf("line[%d] = %q, want %q", i, l, want)
		}
	}
}

func TestRingBuffer_TailN(t *testing.T) {
	e := &devEntry{
		logBuf: make([]string, 0, 10),
		logCap: 10,
	}
	for i := 0; i < 8; i++ {
		e.appendLog(nthLine(i))
	}
	got := e.tailLog(3)
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	for i, l := range got {
		want := nthLine(5 + i)
		if l != want {
			t.Errorf("line[%d] = %q, want %q", i, l, want)
		}
	}
}

func nthLine(n int) string {
	return strings.Repeat("x", n+1)
}

// --- DevServerManager ---

func echoScript() string {
	return "echo hello-devserver"
}

func longScript() string {
	if runtime.GOOS == "windows" {
		return "powershell -NoProfile -Command Start-Sleep -Seconds 120"
	}
	return "sleep 120"
}

func multiLineScript(n int) string {
	if runtime.GOOS == "windows" {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = "echo line" + nthLine(i)
		}
		return strings.Join(parts, " & ")
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "echo line" + nthLine(i)
	}
	return strings.Join(parts, " && ")
}

func newManagerWithRoot(t *testing.T, scripts map[string]string) (*DevServerManager, string) {
	t.Helper()
	dir := t.TempDir()
	if err := writeTestPackageJSON(dir, scripts); err != nil {
		t.Fatal(err)
	}
	svc := NewFSService(nil)
	mgr := NewDevServerManager(svc)
	t.Cleanup(mgr.Shutdown)
	return mgr, dir
}

func TestDevServer_StartEcho_ExitsWithLogs(t *testing.T) {
	mgr, dir := newManagerWithRoot(t, map[string]string{
		"echo": echoScript(),
	})
	h, err := mgr.Start(dir, "echo")
	if err != nil {
		t.Fatal(err)
	}
	if h.ID == "" || h.Root == "" || h.Script != "echo" {
		t.Errorf("bad handle: %+v", h)
	}
	if h.Status != DevServerRunning {
		t.Errorf("initial status = %q, want running", h.Status)
	}

	waitForExit(t, mgr, h.ID, 10*time.Second)

	list := mgr.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}
	if list[0].Status != DevServerExited {
		t.Errorf("status = %q, want exited", list[0].Status)
	}
	if list[0].ExitCode == nil || *list[0].ExitCode != 0 {
		t.Errorf("exit code = %v, want 0", list[0].ExitCode)
	}

	logs, err := mgr.Logs(h.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range logs {
		if strings.Contains(l, "hello-devserver") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'hello-devserver' in logs, got: %v", logs)
	}
}

func TestDevServer_StartScriptNotFound(t *testing.T) {
	mgr, dir := newManagerWithRoot(t, map[string]string{
		"echo": echoScript(),
	})
	_, err := mgr.Start(dir, "nonexistent")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestDevServer_StopKillsLongRunning(t *testing.T) {
	mgr, dir := newManagerWithRoot(t, map[string]string{
		"serve": longScript(),
	})
	h, err := mgr.Start(dir, "serve")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)

	if err := mgr.Stop(h.ID); err != nil {
		t.Fatal(err)
	}
	list := mgr.List()
	if len(list) != 0 {
		t.Errorf("expected empty list after stop, got %d", len(list))
	}
}

func TestDevServer_StopUnknownID(t *testing.T) {
	mgr := NewDevServerManager(NewFSService(nil))
	t.Cleanup(mgr.Shutdown)
	err := mgr.Stop("bogus")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got: %v", err)
	}
}

func TestDevServer_Idempotent(t *testing.T) {
	mgr, dir := newManagerWithRoot(t, map[string]string{
		"serve": longScript(),
	})
	h1, err := mgr.Start(dir, "serve")
	if err != nil {
		t.Fatal(err)
	}
	h2, err := mgr.Start(dir, "serve")
	if err != nil {
		t.Fatal(err)
	}
	if h1.ID != h2.ID {
		t.Errorf("idempotent start returned different IDs: %s vs %s", h1.ID, h2.ID)
	}
	if len(mgr.List()) != 1 {
		t.Errorf("expected 1 entry, got %d", len(mgr.List()))
	}
}

func TestDevServer_Cap(t *testing.T) {
	dir := t.TempDir()
	scripts := map[string]string{}
	for i := 0; i < MaxDevServers+2; i++ {
		name := "s" + nthLine(i)
		scripts[name] = longScript()
	}
	if err := writeTestPackageJSON(dir, scripts); err != nil {
		t.Fatal(err)
	}
	svc := NewFSService(nil)
	mgr := NewDevServerManager(svc)
	t.Cleanup(mgr.Shutdown)

	names := make([]string, 0, len(scripts))
	for name := range scripts {
		names = append(names, name)
	}
	sort.Strings(names)

	for i := 0; i < MaxDevServers; i++ {
		if _, err := mgr.Start(dir, names[i]); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
	}
	_, err := mgr.Start(dir, names[MaxDevServers])
	if err == nil || !strings.Contains(err.Error(), "limit reached") {
		t.Errorf("expected limit error, got: %v", err)
	}
}

func TestDevServer_ShutdownStopsAll(t *testing.T) {
	mgr, dir := newManagerWithRoot(t, map[string]string{
		"a": longScript(),
		"b": longScript(),
	})
	if _, err := mgr.Start(dir, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Start(dir, "b"); err != nil {
		t.Fatal(err)
	}
	if len(mgr.List()) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(mgr.List()))
	}
	mgr.Shutdown()
	if len(mgr.List()) != 0 {
		t.Errorf("expected empty list after shutdown, got %d", len(mgr.List()))
	}
}

func TestDevServer_RingBufferTruncation(t *testing.T) {
	dir := t.TempDir()
	cap := 5
	numLines := cap + 3
	if err := writeTestPackageJSON(dir, map[string]string{
		"flood": multiLineScript(numLines),
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewFSService(nil)
	mgr := NewDevServerManager(svc)
	mgr.logCap = cap
	t.Cleanup(mgr.Shutdown)

	h, err := mgr.Start(dir, "flood")
	if err != nil {
		t.Fatal(err)
	}
	waitForExit(t, mgr, h.ID, 10*time.Second)

	logs, err := mgr.Logs(h.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) > cap {
		t.Errorf("expected at most %d log lines, got %d", cap, len(logs))
	}
}

func TestDevServer_LogsUnknownID(t *testing.T) {
	mgr := NewDevServerManager(NewFSService(nil))
	t.Cleanup(mgr.Shutdown)
	_, err := mgr.Logs("bogus", 0)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got: %v", err)
	}
}

func TestDevServer_NilService(t *testing.T) {
	mgr := NewDevServerManager(nil)
	t.Cleanup(mgr.Shutdown)
	_, err := mgr.Start(t.TempDir(), "dev")
	if err == nil {
		t.Fatal("expected error with no package.json, got nil")
	}
}

// waitForExit polls until the dev-server exits or the timeout fires.
func waitForExit(t *testing.T, mgr *DevServerManager, id string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("dev-server %s did not exit within %v", id, timeout)
		default:
		}
		mgr.mu.Lock()
		e, ok := mgr.byID[id]
		if ok && e.handle.Status != DevServerRunning {
			mgr.mu.Unlock()
			return
		}
		mgr.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
}
