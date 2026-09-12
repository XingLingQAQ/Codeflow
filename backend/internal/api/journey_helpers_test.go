// Package api - Shared helpers for the T0.06 user-journey acceptance tests.
//
// The TestJourney_* cases (T0.06.c) consume five helpers defined here:
//   - fakeClock: injectable, concurrency-safe time source (Now/Advance/Until/Since).
//   - faultPoints: countable fault injection consumed only through Hit.
//   - manifest verification: copyJourneyFixture copies a fixture from
//     testdata/journeys into t.TempDir() and verifies every copied file
//     against the SHA-256 list in manifest.json before use.
//   - newJourneyServer: minimal real API server (genuine gin router plus the
//     four core services on in-memory / :memory: stores).
//   - logCapture: thread-safe capture attachable to any *log.Logger.
//
// Helpers never seed fake events or rows: a freshly started journey server
// must observe empty stores on the success path.
package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/config"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
	"github.com/codeflow/backend/internal/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// fakeClock
// ---------------------------------------------------------------------------

// fakeClock is an injectable, mutex-guarded time source. It never consults
// the wall clock, so journeys decide exactly how time moves.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

// Now reports the current fake time.
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the fake time by d (negative moves it backwards) and returns
// the new value.
func (c *fakeClock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}

// Until returns t - Now (negative once Now is past t).
func (c *fakeClock) Until(t time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return t.Sub(c.now)
}

// Since returns Now - t.
func (c *fakeClock) Since(t time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now.Sub(t)
}

func TestJourneyHelpersFakeClock(t *testing.T) {
	start := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	assert.Equal(t, start, clock.Now())

	// Advance moves Now forward and returns the new value.
	got := clock.Advance(90 * time.Second)
	assert.Equal(t, start.Add(90*time.Second), got)
	assert.Equal(t, got, clock.Now())

	// Until/Since are measured against the fake Now, not the wall clock.
	deadline := start.Add(5 * time.Minute)
	assert.Equal(t, 210*time.Second, clock.Until(deadline))
	assert.Equal(t, 90*time.Second, clock.Since(start))

	// Negative durations move the clock backwards deterministically.
	clock.Advance(-30 * time.Second)
	assert.Equal(t, start.Add(60*time.Second), clock.Now())

	// Concurrent writers must not lose or double-count an advance: the
	// final reading is exactly start + offset + workers*steps regardless
	// of interleaving.
	const workers = 8
	const advancesPerWorker = 250
	const step = time.Millisecond
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < advancesPerWorker; j++ {
				clock.Advance(step)
				_ = clock.Now()
			}
		}()
	}
	wg.Wait()
	expected := start.Add(60*time.Second + workers*advancesPerWorker*step)
	assert.Equal(t, expected, clock.Now())
}

// ---------------------------------------------------------------------------
// faultPoints
// ---------------------------------------------------------------------------

// faultPoint is one armed injection: err fires on the next `remaining` Hits.
type faultPoint struct {
	err       error
	remaining int
	hits      int
}

// faultPoints is a concurrency-safe registry of named injection points. A
// fault is consumed only when Hit is called with its name, so a test can
// prove an injected failure actually fired (and detect ones that never did).
type faultPoints struct {
	mu     sync.Mutex
	points map[string]*faultPoint
}

func newFaultPoints() *faultPoints {
	return &faultPoints{points: make(map[string]*faultPoint)}
}

// Arm registers err to be returned by the next `times` Hit(name) calls.
// Re-arming the same name replaces the previous point.
func (fp *faultPoints) Arm(name string, times int, err error) {
	if times < 1 {
		panic("faultPoints.Arm: times must be >= 1")
	}
	if err == nil {
		panic("faultPoints.Arm: err must be non-nil")
	}
	fp.mu.Lock()
	defer fp.mu.Unlock()
	fp.points[name] = &faultPoint{err: err, remaining: times}
}

// ArmOnce registers a single-shot fault.
func (fp *faultPoints) ArmOnce(name string, err error) {
	fp.Arm(name, 1, err)
}

// Hit consumes one armed occurrence of name and returns its error. It
// returns nil when name is not armed or its count is exhausted.
func (fp *faultPoints) Hit(name string) error {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	p, ok := fp.points[name]
	if !ok || p.remaining == 0 {
		return nil
	}
	p.remaining--
	p.hits++
	return p.err
}

// Hits reports how many times name has actually fired.
func (fp *faultPoints) Hits(name string) int {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if p, ok := fp.points[name]; ok {
		return p.hits
	}
	return 0
}

// Unconsumed returns the sorted names of armed points that have not been
// fully consumed, so a test can fail on faults that never fired.
func (fp *faultPoints) Unconsumed() []string {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	var names []string
	for name, p := range fp.points {
		if p.remaining > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func TestJourneyHelpersFaultPoints(t *testing.T) {
	fp := newFaultPoints()
	boom := errors.New("boom")

	// Unarmed point: Hit returns nil and records nothing.
	assert.NoError(t, fp.Hit("never-armed"))
	assert.Zero(t, fp.Hits("never-armed"))
	assert.Empty(t, fp.Unconsumed())

	// One-shot fault: first Hit consumes it, later Hits pass through.
	fp.ArmOnce("flush", boom)
	assert.ErrorIs(t, fp.Hit("flush"), boom)
	assert.NoError(t, fp.Hit("flush"), "one-shot fault must be exhausted after one hit")
	assert.Equal(t, 1, fp.Hits("flush"))
	assert.Empty(t, fp.Unconsumed())

	// Counted fault: fires exactly N times, then passes through.
	fp.Arm("write", 2, boom)
	assert.ErrorIs(t, fp.Hit("write"), boom)
	assert.ErrorIs(t, fp.Hit("write"), boom)
	assert.NoError(t, fp.Hit("write"))
	assert.Equal(t, 2, fp.Hits("write"))
	assert.Empty(t, fp.Unconsumed())

	// Negative: an armed-but-never-hit point is detected, proving a fault
	// is consumed only through Hit.
	fp.ArmOnce("never-hit", boom)
	assert.Equal(t, []string{"never-hit"}, fp.Unconsumed())
	assert.Zero(t, fp.Hits("never-hit"))
	assert.ErrorIs(t, fp.Hit("never-hit"), boom)
	assert.Empty(t, fp.Unconsumed())

	// Concurrent consumption of a counted fault fires exactly N times in
	// total, never more.
	fp.Arm("parallel", 50, boom)
	var wg sync.WaitGroup
	var fired atomic.Int64
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if fp.Hit("parallel") != nil {
					fired.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int64(50), fired.Load())
	assert.Equal(t, 50, fp.Hits("parallel"))
	assert.Empty(t, fp.Unconsumed())
}

// ---------------------------------------------------------------------------
// journey manifest verification
// ---------------------------------------------------------------------------

const journeysTestdataDir = "testdata/journeys"

type journeyManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type journeyManifestFixture struct {
	Name  string                `json:"name"`
	Path  string                `json:"path"`
	Kind  string                `json:"kind"`
	Files []journeyManifestFile `json:"files"`
}

type journeyManifest struct {
	SchemaVersion int                      `json:"schema_version"`
	StepID        string                   `json:"step_id"`
	PlanVersion   string                   `json:"plan_version"`
	HashAlgorithm string                   `json:"hash_algorithm"`
	Fixtures      []journeyManifestFixture `json:"fixtures"`
}

// loadJourneyManifest reads and validates testdata/journeys/manifest.json.
func loadJourneyManifest(t *testing.T) *journeyManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(journeysTestdataDir, "manifest.json"))
	require.NoError(t, err, "read journeys manifest")
	var m journeyManifest
	require.NoError(t, json.Unmarshal(data, &m), "parse journeys manifest")
	require.Equal(t, 1, m.SchemaVersion, "unsupported manifest schema_version")
	require.Equal(t, "sha256", m.HashAlgorithm, "unsupported manifest hash algorithm")
	require.NotEmpty(t, m.Fixtures, "manifest lists no fixtures")
	return &m
}

// fixtureByName finds a fixture entry by name.
func (m *journeyManifest) fixtureByName(name string) (journeyManifestFixture, error) {
	for _, fx := range m.Fixtures {
		if fx.Name == name {
			return fx, nil
		}
	}
	return journeyManifestFixture{}, fmt.Errorf("journey fixture %q not in manifest", name)
}

// verifyJourneyFiles checks every manifest entry against the tree rooted at
// root, comparing byte size first and then SHA-256. It returns the first
// mismatch found.
func verifyJourneyFiles(root string, files []journeyManifestFile) error {
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
		if err != nil {
			return fmt.Errorf("journey fixture file %s: %w", f.Path, err)
		}
		if int64(len(data)) != f.Bytes {
			return fmt.Errorf("journey fixture file %s: size %d, manifest says %d", f.Path, len(data), f.Bytes)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != f.SHA256 {
			return fmt.Errorf("journey fixture file %s: sha256 %s, manifest says %s", f.Path, got, f.SHA256)
		}
	}
	return nil
}

// verifyNoExtraFiles fails when the tree at root holds a file the manifest
// does not list (manifest drift or stray writes) or a non-regular file.
func verifyNoExtraFiles(root string, files []journeyManifestFile) error {
	listed := make(map[string]struct{}, len(files))
	for _, f := range files {
		listed[filepath.Clean(filepath.FromSlash(f.Path))] = struct{}{}
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("journey fixture: non-regular file %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if _, ok := listed[filepath.Clean(rel)]; !ok {
			return fmt.Errorf("journey fixture: file %s not listed in manifest", rel)
		}
		return nil
	})
}

// copyJourneyFixture copies the named fixture from testdata/journeys into a
// fresh t.TempDir() and verifies the copy against the manifest SHA-256 list
// before handing it over. The returned directory is the only tree a journey
// may mutate; the source under testdata must stay byte-identical.
func copyJourneyFixture(t *testing.T, name string) string {
	t.Helper()
	m := loadJourneyManifest(t)
	fx, err := m.fixtureByName(name)
	require.NoError(t, err)
	src := filepath.Join(journeysTestdataDir, filepath.FromSlash(fx.Path))
	require.NoError(t, verifyNoExtraFiles(src, fx.Files), "fixture source %s drifted from manifest", name)
	dst := filepath.Join(t.TempDir(), filepath.FromSlash(fx.Path))
	require.NoError(t, copyJourneyDir(src, dst))
	require.NoError(t, verifyJourneyFiles(dst, fx.Files), "fixture copy %s failed manifest verification", name)
	return dst
}

// copyJourneyDir recursively copies regular files from src to dst.
func copyJourneyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("journey fixture: refusing to copy non-regular file %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func TestJourneyHelpersManifest(t *testing.T) {
	m := loadJourneyManifest(t)
	require.Len(t, m.Fixtures, 3, "manifest must list the three T0.06.a fixtures")
	assert.Equal(t, "T0.06.a", m.StepID)

	for _, name := range []string{"project-go-small", "project-node-small", "project-no-git"} {
		fx, err := m.fixtureByName(name)
		require.NoError(t, err)
		require.NotEmpty(t, fx.Files)

		// The source tree must match the manifest exactly.
		src := filepath.Join(journeysTestdataDir, filepath.FromSlash(fx.Path))
		require.NoError(t, verifyJourneyFiles(src, fx.Files), "source fixture %s drifted from manifest", name)
		require.NoError(t, verifyNoExtraFiles(src, fx.Files), "source fixture %s holds unlisted files", name)

		// The copy handed to journeys verifies against the manifest too.
		dir := copyJourneyFixture(t, name)
		require.NoError(t, verifyJourneyFiles(dir, fx.Files))

		// The copy is writable and isolated from the source tree.
		probe := filepath.Join(dir, "JOURNEY-WRITE-PROBE")
		require.NoError(t, os.WriteFile(probe, []byte("probe"), 0o644))
		_, statErr := os.Stat(filepath.Join(src, "JOURNEY-WRITE-PROBE"))
		assert.True(t, os.IsNotExist(statErr), "writing to the copy must not touch the source tree")
	}

	_, err := m.fixtureByName("does-not-exist")
	assert.Error(t, err, "unknown fixture names must be rejected")
}

func TestJourneyHelpersManifestDetectsTampering(t *testing.T) {
	dir := copyJourneyFixture(t, "project-no-git")
	m := loadJourneyManifest(t)
	fx, err := m.fixtureByName("project-no-git")
	require.NoError(t, err)

	// Flip one byte in the copy: verification must fail with the file named.
	target := filepath.Join(dir, "src", "hello.txt")
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	data[0] ^= 0xFF
	require.NoError(t, os.WriteFile(target, data, 0o644))
	err = verifyJourneyFiles(dir, fx.Files)
	require.Error(t, err, "tampered fixture must fail manifest verification")
	assert.Contains(t, err.Error(), "src/hello.txt")

	// Truncation changes the size and must fail before any hash compare.
	require.NoError(t, os.WriteFile(target, data[:len(data)-1], 0o644))
	err = verifyJourneyFiles(dir, fx.Files)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "size")

	// A file the manifest does not list must be detected.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("x"), 0o644))
	err = verifyNoExtraFiles(dir, fx.Files)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stray.txt")
}

// ---------------------------------------------------------------------------
// journeyServer
// ---------------------------------------------------------------------------

// newJourneyServer assembles the minimal real API server used by journey
// tests: the genuine gin router from NewServer plus the four core services
// backed by in-memory / :memory: stores, following the package's existing
// E2E wiring convention. It deliberately seeds no hooks, events, or domain
// rows — success-path journeys must observe empty stores and create any
// data they assert on through the real API.
func newJourneyServer(t *testing.T) *httptest.Server {
	t.Helper()

	snapshotSvc := snapshot.NewInMemorySnapshotService()
	snapshot.SetSnapshotService(snapshotSvc)

	configSvc, err := config.NewSQLiteConfigService(":memory:")
	require.NoError(t, err, "create journey config service")
	config.SetConfigService(configSvc)

	plannerSvc, err := planner.NewSQLitePlanner(":memory:")
	require.NoError(t, err, "create journey planner service")
	planner.SetPlanner(plannerSvc)

	projectSvc, err := project.NewSQLiteProjectService(":memory:")
	require.NoError(t, err, "create journey project service")
	project.SetProjectService(projectSvc)

	server := NewServer(&Config{
		Port:            "0",
		AuthToken:       testAuthToken,
		AllowedOrigins:  []string{"http://localhost:3000"},
		EnableDebugMode: true,
	})
	ts := httptest.NewServer(authenticatedTestHandler(server.Router()))
	t.Cleanup(func() {
		snapshot.SetSnapshotService(nil)
		config.SetConfigService(nil)
		planner.SetPlanner(nil)
		project.SetProjectService(nil)
		_ = configSvc.Close()
		_ = plannerSvc.Close()
		_ = projectSvc.Close()
	})
	return ts
}

func TestJourneyHelpersServer(t *testing.T) {
	ts := newJourneyServer(t)
	defer ts.Close()
	client := ts.Client()

	// Health endpoint answers a real HTTP round trip without auth.
	resp, err := client.Get(ts.URL + "/health")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), "healthy")

	// Wired service route works through the authenticated wrapper, and the
	// store is empty: no fake rows or events are pre-filled.
	resp, err = client.Get(ts.URL + "/api/v1/snapshots")
	require.NoError(t, err)
	var list map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&list))
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, float64(0), list["total"], "fresh journey server must not pre-fill fake data")

	// Negative: a wrong bearer token is rejected by the real middleware,
	// proving auth is enforced rather than stubbed out.
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/snapshots", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer definitely-wrong-token")
	resp, err = client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// logCapture
// ---------------------------------------------------------------------------

// logCapture is a thread-safe io.Writer that records log output for later
// assertions. Attach it to any *log.Logger; the returned restore function
// hands the logger its original writer back.
type logCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func newLogCapture() *logCapture {
	return &logCapture{}
}

func (c *logCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *logCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// Lines returns the captured output split into lines, without a trailing
// empty element.
func (c *logCapture) Lines() []string {
	text := c.String()
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(text, "\n"), "\n")
}

// Contains reports whether the captured output holds substr.
func (c *logCapture) Contains(substr string) bool {
	return strings.Contains(c.String(), substr)
}

// Attach swaps logger's output to this capture and returns a restore
// function that puts the original writer back.
func (c *logCapture) Attach(logger *log.Logger) func() {
	prev := logger.Writer()
	logger.SetOutput(c)
	return func() { logger.SetOutput(prev) }
}

func TestJourneyHelpersLogCapture(t *testing.T) {
	logger := log.New(io.Discard, "journey ", log.Lmsgprefix)
	capture := newLogCapture()
	restore := capture.Attach(logger)

	logger.Print("first line")
	assert.True(t, capture.Contains("first line"))
	assert.Equal(t, []string{"journey first line"}, capture.Lines())

	// Concurrent writes from 16 goroutines are serialized and all land.
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			logger.Printf("worker-%02d", i)
		}(i)
	}
	wg.Wait()
	assert.Len(t, capture.Lines(), 17)
	assert.True(t, capture.Contains("worker-07"))

	// Restore detaches the capture: later output is not recorded.
	restore()
	logger.Print("after restore")
	assert.False(t, capture.Contains("after restore"))
	assert.Len(t, capture.Lines(), 17)
}
