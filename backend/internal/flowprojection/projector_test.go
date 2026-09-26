package flowprojection

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/floweng"
)

// Tests for the legacy Flow projector (T1.05.c).
//
// The Source is the real thing (a file-backed *floweng.SQLiteFlowStore), so the
// tests exercise the actual claim rule and the actual compare-and-set marks
// instead of a mock that agrees with the projector about what they mean. The
// Target is a fake, because its job is the runtime database's and that database
// is not this package's.

// ---------- clock ----------

// testClock is a movable clock: no test sleeps for a backoff.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock { return &testClock{now: time.Now().UTC()} }

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// Sync moves the clock to the real present. Outbox rows are created due at the
// real write time, so a clock frozen before them would see nothing as due.
func (c *testClock) Sync() { c.Set(time.Now().UTC()) }

func (c *testClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// ---------- fixture ----------

type fixture struct {
	t     *testing.T
	dir   string
	store *floweng.SQLiteFlowStore
	clk   *testClock
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	// One temp dir per test, not per store: a test that reopens the database
	// (the restart case) must find the same file, and closing a store must not
	// leave its directory behind for the next one to inherit.
	dir := t.TempDir()
	store, err := floweng.NewSQLiteFlowStore(filepath.Join(dir, "floweng.db"))
	if err != nil {
		t.Fatalf("open legacy store: %v", err)
	}
	f := &fixture{t: t, dir: dir, store: store, clk: newTestClock()}
	// Close whatever the fixture currently holds: after a reopen that is the new
	// handle, and an unclosed handle keeps the file busy for the TempDir cleanup.
	t.Cleanup(func() {
		if f.store != nil {
			_ = f.store.Close()
		}
	})
	return f
}

// reopen closes the store and opens it again, for the restart case.
func (f *fixture) reopen() {
	f.t.Helper()
	if err := f.store.Close(); err != nil {
		f.t.Fatalf("close store: %v", err)
	}
	store, err := floweng.NewSQLiteFlowStore(filepath.Join(f.dir, "floweng.db"))
	if err != nil {
		f.t.Fatalf("reopen store: %v", err)
	}
	f.store = store
}

// seed writes a Flow whose timeline is exactly the given event types, and
// returns the source event ids in order. The clock is re-synced afterwards so
// the rows are due.
func (f *fixture) seed(flowID string, types ...string) []string {
	f.t.Helper()
	now := time.Now().UTC()
	events := make([]floweng.FlowEvent, 0, len(types))
	ids := make([]string, 0, len(types))
	for i, typ := range types {
		id := fmt.Sprintf("%s-ev-%d", flowID, i+1)
		ids = append(ids, id)
		events = append(events, floweng.FlowEvent{
			ID:        id,
			Type:      typ,
			StageID:   "stage-1",
			Message:   "message for " + id,
			Timestamp: now.Add(time.Duration(i) * time.Millisecond),
		})
	}
	flow := &floweng.Flow{
		ID:         flowID,
		ProjectID:  "proj-" + flowID,
		TemplateID: floweng.TemplateNewProject,
		Status:     floweng.FlowStatusActive,
		Stages:     []floweng.Stage{},
		Artifacts:  []floweng.Artifact{},
		Events:     events,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := f.store.Put(flow); err != nil {
		f.t.Fatalf("seed %s: %v", flowID, err)
	}
	f.clk.Sync()
	return ids
}

// assertProjectedAs asks the store itself which runtime event id it recorded for
// a source event: marking the same event with a different id must be refused,
// and the refusal names the recorded one.
func (f *fixture) assertProjectedAs(sourceEventID, projectedID string) {
	f.t.Helper()
	err := f.store.MarkLegacyEventProjected(sourceEventID, projectedID+"-different", f.clk.Now())
	if err == nil {
		f.t.Fatalf("%s: the store accepted a second projected id", sourceEventID)
	}
	if !strings.Contains(err.Error(), "already projected as "+projectedID) {
		f.t.Fatalf("%s: recorded id is not %s: %v", sourceEventID, projectedID, err)
	}
}

// ---------- fake target ----------

// fakeTarget stands in for the runtime database. It keeps the same promise the
// real adapter must: one runtime event per SourceEventID, the same id on every
// call.
type fakeTarget struct {
	mu        sync.Mutex
	calls     []string          // source event ids, in call order
	stored    map[string]string // source event id -> runtime event id
	nextID    int
	failures  map[string]int  // source event id -> failures still to return
	permanent map[string]bool // source event id -> fail permanently
	panics    map[string]any  // source event id -> panic value
}

func newFakeTarget() *fakeTarget {
	return &fakeTarget{
		stored:    map[string]string{},
		failures:  map[string]int{},
		permanent: map[string]bool{},
		panics:    map[string]any{},
	}
}

func (f *fakeTarget) Project(_ context.Context, ev floweng.LegacyOutboxEvent) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ev.SourceEventID)
	if v, ok := f.panics[ev.SourceEventID]; ok {
		panic(v)
	}
	if f.permanent[ev.SourceEventID] {
		return "", &PermanentError{Err: errors.New("target rejects source event " + ev.SourceEventID)}
	}
	if f.failures[ev.SourceEventID] > 0 {
		f.failures[ev.SourceEventID]--
		return "", errors.New("target is unavailable for " + ev.SourceEventID)
	}
	if id, ok := f.stored[ev.SourceEventID]; ok {
		return id, nil // the same source event keeps its runtime identity
	}
	f.nextID++
	id := fmt.Sprintf("runtime-%d", f.nextID)
	f.stored[ev.SourceEventID] = id
	return id, nil
}

func (f *fakeTarget) callsFor(sourceEventID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, id := range f.calls {
		if id == sourceEventID {
			n++
		}
	}
	return n
}

func (f *fakeTarget) storedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.stored)
}

// ---------- recording / fault-injecting source wrapper ----------

// recordingSource embeds the real store (so the claim rule and the CAS marks are
// the ones under test) and records every write-back, plus optional injected
// failures.
type recordingSource struct {
	*floweng.SQLiteFlowStore

	mu             sync.Mutex
	projectedMarks []string // "source:projectedID"
	retries        []string // "source:error"
	deaths         []string // "source:error"
	failMarks      int      // how many MarkLegacyEventProjected calls to refuse
	failDue        error    // when set, DueLegacyEvents fails
}

func (s *recordingSource) MarkLegacyEventProjected(sourceEventID, projectedEventID string, now time.Time) error {
	s.mu.Lock()
	if s.failMarks > 0 {
		s.failMarks--
		s.mu.Unlock()
		return errors.New("injected mark failure")
	}
	s.projectedMarks = append(s.projectedMarks, sourceEventID+":"+projectedEventID)
	s.mu.Unlock()
	return s.SQLiteFlowStore.MarkLegacyEventProjected(sourceEventID, projectedEventID, now)
}

func (s *recordingSource) MarkLegacyEventRetry(sourceEventID, lastError string, nextAttemptAt time.Time) error {
	s.mu.Lock()
	s.retries = append(s.retries, sourceEventID+":"+lastError)
	s.mu.Unlock()
	return s.SQLiteFlowStore.MarkLegacyEventRetry(sourceEventID, lastError, nextAttemptAt)
}

func (s *recordingSource) MarkLegacyEventDead(sourceEventID, lastError string) error {
	s.mu.Lock()
	s.deaths = append(s.deaths, sourceEventID+":"+lastError)
	s.mu.Unlock()
	return s.SQLiteFlowStore.MarkLegacyEventDead(sourceEventID, lastError)
}

func (s *recordingSource) DueLegacyEvents(now time.Time, limit int) ([]floweng.LegacyOutboxEvent, error) {
	if s.failDue != nil {
		return nil, s.failDue
	}
	return s.SQLiteFlowStore.DueLegacyEvents(now, limit)
}

func (s *recordingSource) lastRetry() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.retries) == 0 {
		return ""
	}
	return s.retries[len(s.retries)-1]
}

func (s *recordingSource) retryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.retries)
}

func (s *recordingSource) recordedProjections() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.projectedMarks...)
}

// newProjector builds a projector over the wrapper with a small, explicit
// configuration so the assertions below can name the numbers.
func newTestProjector(t *testing.T, src Source, target Target, clk *testClock, opts Options) *Projector {
	t.Helper()
	opts.Source = src
	opts.Target = target
	if opts.Clock == nil {
		opts.Clock = clk.Now
	}
	if opts.BatchSize == 0 {
		opts.BatchSize = DefaultBatchSize
	}
	if opts.BackoffBase == 0 {
		opts.BackoffBase = time.Second
	}
	if opts.BackoffMax == 0 {
		opts.BackoffMax = time.Minute
	}
	if opts.MaxAttempts == 0 {
		opts.MaxAttempts = DefaultMaxAttempts
	}
	p, err := New(opts)
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	return p
}

// ---------- tests ----------

// TestProjectOnceProjectsEveryDueEventOnce is the happy path: every event of
// every Flow reaches the Target exactly once, in local order, and the rows are
// marked projected with the id the Target returned.
func TestProjectOnceProjectsEveryDueEventOnce(t *testing.T) {
	f := newFixture(t)
	src := &recordingSource{SQLiteFlowStore: f.store}
	target := newFakeTarget()
	p := newTestProjector(t, src, target, f.clk, Options{})

	ids := f.seed("flow-a", "flow.created", "stage.done", "stage.active")
	if pending, dead, err := f.store.LegacyOutboxBacklog(); err != nil || pending != 3 || dead != 0 {
		t.Fatalf("backlog before = %d/%d err=%v", pending, dead, err)
	}

	// One round hands out one event per Flow: a Flow's timeline is projected in
	// local order, so the claim only ever offers a head. For a single Flow that
	// is one event per round.
	for round, id := range ids {
		report, err := p.ProjectOnce(context.Background())
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if report != (Report{Due: 1, Projected: 1}) {
			t.Fatalf("round %d = %+v, want due=1 projected=1", round, report)
		}
		if n := target.callsFor(id); n != 1 {
			t.Fatalf("%s was projected %d times, want 1", id, n)
		}
		f.assertProjectedAs(id, target.stored[id])
	}
	if got := target.calls; len(got) != len(ids) {
		t.Fatalf("target calls = %v, want the three events", got)
	} else {
		for i := range ids {
			if got[i] != ids[i] {
				t.Fatalf("target calls = %v, want %v in order", got, ids)
			}
		}
	}
	if pending, dead, err := f.store.LegacyOutboxBacklog(); err != nil || pending != 0 || dead != 0 {
		t.Fatalf("backlog after = %d/%d err=%v, want 0/0", pending, dead, err)
	}
	if want := len(src.recordedProjections()); want != 3 {
		t.Fatalf("recorded marks = %v", src.recordedProjections())
	}

	// A second round finds nothing: the marks moved the rows out of the queue.
	report, err := p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("second round: %v", err)
	}
	if report != (Report{}) {
		t.Fatalf("second round = %+v, want an empty report", report)
	}
	if len(target.calls) != 3 {
		t.Fatalf("the second round called the target again: %v", target.calls)
	}
}

// TestRestartResumesTheQueue: the outbox lives in the source database, so a
// projector that is restarted (a new store handle and a new Projector) picks up
// exactly the events the previous one did not finish, and never re-projects the
// ones it marked.
func TestRestartResumesTheQueue(t *testing.T) {
	f := newFixture(t)
	src := &recordingSource{SQLiteFlowStore: f.store}
	target := newFakeTarget()
	p := newTestProjector(t, src, target, f.clk, Options{})

	ids := f.seed("flow-restart", "flow.created", "stage.done")
	if _, err := p.ProjectOnce(context.Background()); err != nil {
		t.Fatalf("round before restart: %v", err)
	}
	if target.callsFor(ids[0]) != 1 || target.callsFor(ids[1]) != 0 {
		t.Fatalf("before the restart: %v", target.calls)
	}

	// Restart: a new handle on the same file, a new source wrapper and a new
	// projector, exactly as a process restart would build them.
	f.reopen()
	src2 := &recordingSource{SQLiteFlowStore: f.store}
	p2 := newTestProjector(t, src2, target, f.clk, Options{})
	report, err := p2.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round after restart: %v", err)
	}
	if report != (Report{Due: 1, Projected: 1}) {
		t.Fatalf("after restart = %+v, want due=1 projected=1", report)
	}
	if target.callsFor(ids[0]) != 1 {
		t.Fatalf("the restart re-projected an already projected event: %v", target.calls)
	}
	if target.callsFor(ids[1]) != 1 {
		t.Fatalf("the restart did not project the remaining event: %v", target.calls)
	}
	f.assertProjectedAs(ids[1], target.stored[ids[1]])
	if pending, dead, err := f.store.LegacyOutboxBacklog(); err != nil || pending != 0 || dead != 0 {
		t.Fatalf("backlog = %d/%d err=%v, want 0/0", pending, dead, err)
	}
}

// TestProjectOnceBatchSizeBoundsTheRound: one round claims at most BatchSize
// events, and the next round continues where it stopped.
func TestProjectOnceBatchSizeBoundsTheRound(t *testing.T) {
	f := newFixture(t)
	src := &recordingSource{SQLiteFlowStore: f.store}
	target := newFakeTarget()
	p := newTestProjector(t, src, target, f.clk, Options{BatchSize: 1})

	ids := f.seed("flow-batch", "flow.created", "stage.done")
	report, err := p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if report != (Report{Due: 1, Projected: 1}) {
		t.Fatalf("round 1 = %+v, want due=1 projected=1", report)
	}
	if len(target.calls) != 1 || target.calls[0] != ids[0] {
		t.Fatalf("round 1 calls = %v, want [%s]", target.calls, ids[0])
	}
	report, err = p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if report != (Report{Due: 1, Projected: 1}) {
		t.Fatalf("round 2 = %+v, want due=1 projected=1", report)
	}
	if len(target.calls) != 2 || target.calls[1] != ids[1] {
		t.Fatalf("round 2 calls = %v, want the second event next", target.calls)
	}
}

// TestCrashBetweenTargetAndMarkReplaysWithoutDuplicating is the crash window the
// whole design is built around: the Target committed, the projector died before
// marking the row. The row stays pending, the next round projects the same
// SourceEventID, and the Target's own idempotency turns that into the same
// runtime event — the design is at-least-once delivery plus a target that
// de-duplicates on the source event id, not a two-phase commit.
func TestCrashBetweenTargetAndMarkReplaysWithoutDuplicating(t *testing.T) {
	f := newFixture(t)
	src := &recordingSource{SQLiteFlowStore: f.store, failMarks: 1}
	target := newFakeTarget()
	p := newTestProjector(t, src, target, f.clk, Options{})

	ids := f.seed("flow-crash", "flow.created")
	source := ids[0]

	// Round 1: the Target succeeds (the runtime event exists), the mark fails —
	// exactly the state a process crash in between would leave behind.
	report, err := p.ProjectOnce(context.Background())
	if err == nil {
		t.Fatal("a failed mark was reported as success")
	}
	if !strings.Contains(err.Error(), "injected mark failure") {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Projected != 0 || report.Retried != 0 || report.DeadLettered != 0 {
		t.Fatalf("report = %+v, want nothing counted as an outcome", report)
	}
	if target.callsFor(source) != 1 {
		t.Fatalf("target calls after round 1 = %d, want 1", target.callsFor(source))
	}
	if pending, _, err := f.store.LegacyOutboxBacklog(); err != nil || pending != 1 {
		t.Fatalf("the row must still be pending after the failed mark: pending=%d err=%v", pending, err)
	}

	// Round 2: the same event is projected again, the Target answers with the
	// same runtime id, and the mark lands.
	report, err = p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if report != (Report{Due: 1, Projected: 1}) {
		t.Fatalf("round 2 = %+v, want due=1 projected=1", report)
	}
	if target.callsFor(source) != 2 {
		t.Fatalf("target calls after round 2 = %d, want 2 (a replay)", target.callsFor(source))
	}
	if target.storedCount() != 1 {
		t.Fatalf("the target holds %d runtime events, want 1", target.storedCount())
	}
	f.assertProjectedAs(source, target.stored[source])
	if pending, dead, err := f.store.LegacyOutboxBacklog(); err != nil || pending != 0 || dead != 0 {
		t.Fatalf("backlog after recovery = %d/%d err=%v, want 0/0", pending, dead, err)
	}
}

// TestRetryBackoffGrowsAndTheRowComesBack proves the retry path and the backoff
// growth with an injected clock: after the n-th failure the row is due again at
// now + min(base*2^(n-1), max).
func TestRetryBackoffGrowsAndTheRowComesBack(t *testing.T) {
	f := newFixture(t)
	src := &recordingSource{SQLiteFlowStore: f.store}
	target := newFakeTarget()
	p := newTestProjector(t, src, target, f.clk, Options{BackoffBase: time.Second, BackoffMax: time.Minute})

	ids := f.seed("flow-backoff", "flow.created")
	source := ids[0]
	target.failures[source] = 2 // fails twice, then succeeds

	start := f.clk.Now()
	report, err := p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if report != (Report{Due: 1, Retried: 1}) {
		t.Fatalf("round 1 = %+v, want due=1 retried=1", report)
	}
	if got := src.lastRetry(); !strings.HasPrefix(got, source+":") || !strings.Contains(got, "unavailable") {
		t.Fatalf("recorded retry = %q", got)
	}

	// Still inside the first backoff window (1s): not due.
	f.clk.Set(start.Add(time.Second - time.Millisecond))
	if report, err = p.ProjectOnce(context.Background()); err != nil {
		t.Fatalf("round 1b: %v", err)
	} else if report != (Report{}) {
		t.Fatalf("a row inside its backoff window was handed out: %+v", report)
	}

	// The window closes: due again, and the second failure doubles the wait.
	f.clk.Set(start.Add(time.Second))
	if report, err = p.ProjectOnce(context.Background()); err != nil {
		t.Fatalf("round 2: %v", err)
	} else if report != (Report{Due: 1, Retried: 1}) {
		t.Fatalf("round 2 = %+v, want due=1 retried=1", report)
	}
	f.clk.Set(start.Add(time.Second + 2*time.Second - time.Millisecond))
	if report, err = p.ProjectOnce(context.Background()); err != nil {
		t.Fatalf("round 2b: %v", err)
	} else if report != (Report{}) {
		t.Fatalf("the doubled backoff window did not hold: %+v", report)
	}

	// Third attempt succeeds.
	f.clk.Set(start.Add(time.Second + 2*time.Second))
	report, err = p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 3: %v", err)
	}
	if report != (Report{Due: 1, Projected: 1}) {
		t.Fatalf("round 3 = %+v, want due=1 projected=1", report)
	}
	if target.callsFor(source) != 3 {
		t.Fatalf("target calls = %d, want 3", target.callsFor(source))
	}
	f.assertProjectedAs(source, target.stored[source])
}

// TestPermanentErrorDeadLettersAtOnce: a failure retrying cannot fix spends no
// budget, and the replayed failure text stays a prefix of what the Target said.
func TestPermanentErrorDeadLettersAtOnce(t *testing.T) {
	f := newFixture(t)
	src := &recordingSource{SQLiteFlowStore: f.store}
	target := newFakeTarget()
	p := newTestProjector(t, src, target, f.clk, Options{})

	ids := f.seed("flow-permanent", "flow.created")
	source := ids[0]
	target.permanent[source] = true

	report, err := p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if report != (Report{Due: 1, DeadLettered: 1}) {
		t.Fatalf("report = %+v, want due=1 dead_lettered=1 (no retry budget spent)", report)
	}
	if src.retryCount() != 0 {
		t.Fatalf("a permanent failure was retried: %v", src.retries)
	}
	if pending, dead, err := f.store.LegacyOutboxBacklog(); err != nil || pending != 0 || dead != 1 {
		t.Fatalf("backlog = %d/%d err=%v, want 0/1", pending, dead, err)
	}

	// A dead row is not handed out again, so the round after is empty.
	f.clk.Add(time.Hour)
	report, err = p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if report != (Report{}) {
		t.Fatalf("round 2 = %+v, want empty", report)
	}
	if target.callsFor(source) != 1 {
		t.Fatalf("target calls = %d, want 1", target.callsFor(source))
	}
}

// TestAttemptsExhaustedDeadLettersAndTheFlowMovesOn: the attempt budget is spent
// one failure at a time, the last failure dead-letters the row, and that release
// is what lets the Flow's next event be projected.
func TestAttemptsExhaustedDeadLettersAndTheFlowMovesOn(t *testing.T) {
	f := newFixture(t)
	src := &recordingSource{SQLiteFlowStore: f.store}
	target := newFakeTarget()
	p := newTestProjector(t, src, target, f.clk, Options{BackoffBase: 10 * time.Millisecond, MaxAttempts: 2})

	ids := f.seed("flow-exhaust", "flow.created", "stage.done")
	first, second := ids[0], ids[1]
	target.failures[first] = 100 // always fails until the budget runs out

	report, err := p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if report != (Report{Due: 1, Retried: 1}) {
		t.Fatalf("round 1 = %+v, want due=1 retried=1 (one attempt left)", report)
	}
	f.clk.Add(10 * time.Millisecond)
	report, err = p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if report != (Report{Due: 1, DeadLettered: 1}) {
		t.Fatalf("round 2 = %+v, want due=1 dead_lettered=1 (budget spent)", report)
	}
	if pending, dead, err := f.store.LegacyOutboxBacklog(); err != nil || pending != 1 || dead != 1 {
		t.Fatalf("backlog = %d/%d err=%v, want 1/1", pending, dead, err)
	}

	// The dead head no longer blocks the Flow.
	report, err = p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 3: %v", err)
	}
	if report != (Report{Due: 1, Projected: 1}) {
		t.Fatalf("round 3 = %+v, want due=1 projected=1 (the next event must not be blocked)", report)
	}
	if target.stored[second] == "" {
		t.Fatalf("the second event was not projected: %v", target.calls)
	}
	f.assertProjectedAs(second, target.stored[second])
}

// TestPerFlowOrderIsPreservedAndOtherFlowsProgress is the ordering rule at the
// projector level: a Flow whose head is failing does not get any of its later
// events into the runtime timeline, this round or the next; other Flows are
// unaffected.
func TestPerFlowOrderIsPreservedAndOtherFlowsProgress(t *testing.T) {
	f := newFixture(t)
	src := &recordingSource{SQLiteFlowStore: f.store}
	target := newFakeTarget()
	p := newTestProjector(t, src, target, f.clk, Options{BackoffBase: time.Second})

	a := f.seed("flow-A", "flow.created", "stage.done", "stage.active")
	b := f.seed("flow-B", "flow.created")
	target.failures[a[0]] = 100 // A's head always fails

	report, err := p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if report != (Report{Due: 2, Retried: 1, Projected: 1}) {
		t.Fatalf("round 1 = %+v, want due=2 retried=1 projected=1", report)
	}
	if target.stored[b[0]] == "" {
		t.Fatalf("flow B was not projected while flow A's head failed: %v", target.calls)
	}
	for _, id := range []string{a[1], a[2]} {
		if target.callsFor(id) != 0 {
			t.Fatalf("%s was projected although %s has not been: %v", id, a[0], target.calls)
		}
	}

	// A round inside the backoff window does not hand out A at all, so A's later
	// events cannot slip through either.
	report, err = p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if report != (Report{}) {
		t.Fatalf("round 2 = %+v, want empty", report)
	}

	// Well past the backoff window, A's head is offered again and fails again;
	// A's later events still wait.
	f.clk.Add(time.Second)
	before := target.callsFor(a[0])
	report, err = p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 3: %v", err)
	}
	if report != (Report{Due: 1, Retried: 1}) {
		t.Fatalf("round 3 = %+v, want due=1 retried=1", report)
	}
	if target.callsFor(a[0]) != before+1 {
		t.Fatalf("A's head was not retried")
	}
	for _, id := range []string{a[1], a[2]} {
		if target.callsFor(id) != 0 {
			t.Fatalf("%s overtook %s: %v", id, a[0], target.calls)
		}
	}

	// Once the head succeeds, the rest follow, in order, one round at a time:
	// each round's claim offers only the Flow's head, so the next event becomes
	// due only after the previous one has been projected.
	target.mu.Lock()
	delete(target.failures, a[0])
	consumed := len(target.calls)
	target.mu.Unlock()
	var tail []string
	for round := 0; round < 3; round++ {
		f.clk.Add(time.Minute) // well past every backoff window
		if _, err := p.ProjectOnce(context.Background()); err != nil {
			t.Fatalf("follow-up round %d: %v", round, err)
		}
		target.mu.Lock()
		tail = append(tail, target.calls[consumed:]...)
		consumed = len(target.calls)
		target.mu.Unlock()
	}
	if strings.Join(tail, ",") != strings.Join([]string{a[0], a[1], a[2]}, ",") {
		t.Fatalf("flow A was not projected in order: %v (all calls: %v)", tail, target.calls)
	}
	if pending, dead, err := f.store.LegacyOutboxBacklog(); err != nil || pending != 0 || dead != 0 {
		t.Fatalf("backlog = %d/%d err=%v, want 0/0", pending, dead, err)
	}
}

// TestTargetPanicIsOneFailedAttempt: a panicking Target spends one attempt, the
// runtime timeline is not told anything, and only the panic value's type is
// recorded — never its content, which may be anything the Target happened to be
// holding.
func TestTargetPanicIsOneFailedAttempt(t *testing.T) {
	f := newFixture(t)
	src := &recordingSource{SQLiteFlowStore: f.store}
	target := newFakeTarget()
	p := newTestProjector(t, src, target, f.clk, Options{MaxAttempts: 3})

	ids := f.seed("flow-panic", "flow.created")
	source := ids[0]
	const secret = "SECRET-PAYLOAD-VALUE"
	target.panics[source] = secret

	report, err := p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("a panic must not escape ProjectOnce as an error: %v", err)
	}
	if report != (Report{Due: 1, Retried: 1}) {
		t.Fatalf("report = %+v, want due=1 retried=1", report)
	}
	got := src.lastRetry()
	if !strings.Contains(got, "panicked") || !strings.Contains(got, "string") {
		t.Fatalf("the recorded failure does not name the panic type: %q", got)
	}
	if strings.Contains(got, secret) {
		t.Fatalf("the panic value leaked into the outbox: %q", got)
	}
	if pending, _, err := f.store.LegacyOutboxBacklog(); err != nil || pending != 1 {
		t.Fatalf("the event must stay pending: pending=%d err=%v", pending, err)
	}

	// A Target that panics on every call exhausts its budget instead of looping
	// forever.
	f.clk.Add(time.Minute)
	if _, err := p.ProjectOnce(context.Background()); err != nil {
		t.Fatalf("round 2: %v", err)
	}
	f.clk.Add(time.Minute)
	report, err = p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 3: %v", err)
	}
	if report != (Report{Due: 1, DeadLettered: 1}) {
		t.Fatalf("round 3 = %+v, want due=1 dead_lettered=1", report)
	}
}

// TestRunKeepsGoingThroughPanicsAndStopsOnContext: Run is a background loop. A
// panicking Target must not take it down, a failing round must not stop it, and
// ctx is the only thing that does.
func TestRunKeepsGoingThroughPanicsAndStopsOnContext(t *testing.T) {
	f := newFixture(t)
	src := &recordingSource{SQLiteFlowStore: f.store}
	target := newFakeTarget()
	// The real clock: Run is driven by wall time (the fixture clock is frozen,
	// which is what makes the deterministic rounds deterministic).
	p := newTestProjector(t, src, target, f.clk, Options{
		Clock:       time.Now,
		BackoffBase: time.Millisecond, BackoffMax: 5 * time.Millisecond, MaxAttempts: 1000,
	})

	ids := f.seed("flow-run", "flow.created")
	// A panic value that cannot collect garbage: this loop is hammered from
	// another goroutine, and a finalizer on a large allocation would be one more
	// variable in the middle of it.
	type runPanic struct{ reason string }
	target.panics[ids[0]] = runPanic{reason: "target is not ready"}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx, 10*time.Millisecond) }()

	deadline := time.Now().Add(5 * time.Second)
	for target.callsFor(ids[0]) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := target.callsFor(ids[0]); n < 3 {
		cancel()
		<-done
		t.Fatalf("Run stopped after %d panicking rounds, want the loop to continue", n)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}

	// A round error (the Source itself refusing) is logged and survived too.
	src.failDue = errors.New("database is locked")
	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := make(chan error, 1)
	go func() { done2 <- p.Run(ctx2, 10*time.Millisecond) }()
	time.Sleep(60 * time.Millisecond)
	cancel2()
	select {
	case err := <-done2:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after a failing round and a cancelled context")
	}
}

// TestRunDrainsAFlowBurstWithoutWaitingPerEvent: the claim returns one head per
// Flow, so a Flow with five queued events needs five rounds. Run must chain
// those rounds while they keep projecting instead of sleeping a full interval
// between them; with a one-hour interval the burst still lands at once.
func TestRunDrainsAFlowBurstWithoutWaitingPerEvent(t *testing.T) {
	f := newFixture(t)
	target := newFakeTarget()
	p := newTestProjector(t, f.store, target, f.clk, Options{Clock: time.Now})

	f.seed("flow-burst", "flow.created", "stage.active", "stage.done", "stage.active", "flow.completed")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx, time.Hour) }()

	deadline := time.Now().Add(5 * time.Second)
	for target.storedCount() < 5 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v, want context.Canceled", err)
	}
	if n := target.storedCount(); n != 5 {
		t.Fatalf("projected %d of 5 events of one Flow before a one-hour interval elapsed, want all 5", n)
	}
}

// TestRunRejectsNonPositiveInterval and the New validation together pin the
// configuration contract: a rejected option set yields an error and no
// projector, because a nil Target discovered at the first event is a crash.
func TestRunRejectsNonPositiveInterval(t *testing.T) {
	f := newFixture(t)
	src := &recordingSource{SQLiteFlowStore: f.store}
	p := newTestProjector(t, src, newFakeTarget(), f.clk, Options{})
	if err := p.Run(context.Background(), 0); err == nil {
		t.Fatal("Run accepted a zero interval")
	}
	if err := p.Run(context.Background(), -time.Second); err == nil {
		t.Fatal("Run accepted a negative interval")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Run(ctx, time.Millisecond); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run with a cancelled context returned %v, want context.Canceled", err)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	f := newFixture(t)
	src := &recordingSource{SQLiteFlowStore: f.store}
	target := newFakeTarget()
	cases := []struct {
		name string
		opts Options
	}{
		{"nil Source", Options{Target: target}},
		{"nil Target", Options{Source: src}},
		{"negative BatchSize", Options{Source: src, Target: target, BatchSize: -1}},
		{"negative BackoffBase", Options{Source: src, Target: target, BackoffBase: -time.Second}},
		{"negative BackoffMax", Options{Source: src, Target: target, BackoffMax: -time.Second}},
		{"negative MaxAttempts", Options{Source: src, Target: target, MaxAttempts: -1}},
		{"BackoffMax below BackoffBase", Options{Source: src, Target: target, BackoffBase: time.Minute, BackoffMax: time.Second}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.opts); err == nil {
				t.Fatalf("New accepted %s", tc.name)
			}
		})
	}

	p, err := New(Options{Source: src, Target: target})
	if err != nil {
		t.Fatalf("New with defaults: %v", err)
	}
	if p.batchSize != DefaultBatchSize || p.backoffBase != DefaultBackoffBase ||
		p.backoffMax != DefaultBackoffMax || p.maxAttempts != DefaultMaxAttempts {
		t.Fatalf("defaults not applied: %+v", p)
	}
	if p.clock == nil {
		t.Fatal("Clock default not applied")
	}
}

// TestProjectOnceStopsOnSourceErrorAndKeepsWhatWasDecided: only the Source can
// make the round an error. Rows already decided keep their outcome, the round
// stops, and the next round picks up the rest.
func TestProjectOnceStopsOnSourceErrorAndKeepsWhatWasDecided(t *testing.T) {
	f := newFixture(t)
	src := &recordingSource{SQLiteFlowStore: f.store}
	target := newFakeTarget()
	p := newTestProjector(t, src, target, f.clk, Options{})

	ids := f.seed("flow-dberr", "flow.created", "stage.done")

	// The head is projected first, so the round that fails has something already
	// decided to preserve.
	report, err := p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if report != (Report{Due: 1, Projected: 1}) {
		t.Fatalf("round 1 = %+v, want due=1 projected=1", report)
	}

	// The next mark fails: the round is an error, the projected head keeps its
	// outcome, and the second event stays queued.
	src.failMarks = 1
	report, err = p.ProjectOnce(context.Background())
	if err == nil {
		t.Fatal("a Source failure did not surface as an error")
	}
	if report != (Report{Due: 1}) {
		t.Fatalf("report = %+v, want the failed round to have decided nothing", report)
	}
	if pending, _, err2 := f.store.LegacyOutboxBacklog(); err2 != nil || pending != 1 {
		t.Fatalf("pending = %d err=%v, want 1 (the second event waits)", pending, err2)
	}

	// The next round finishes the job; the already-projected event is not
	// touched again.
	report, err = p.ProjectOnce(context.Background())
	if err != nil {
		t.Fatalf("round 3: %v", err)
	}
	if report != (Report{Due: 1, Projected: 1}) {
		t.Fatalf("round 3 = %+v", report)
	}
	if target.callsFor(ids[0]) != 1 {
		t.Fatalf("the projected event was sent again: %v", target.calls)
	}
	if pending, dead, err := f.store.LegacyOutboxBacklog(); err != nil || pending != 0 || dead != 0 {
		t.Fatalf("backlog = %d/%d err=%v", pending, dead, err)
	}

	// A claim that fails is an error too.
	src.failDue = errors.New("database is locked")
	if _, err := p.ProjectOnce(context.Background()); err == nil {
		t.Fatal("a failing claim did not surface as an error")
	}
}
