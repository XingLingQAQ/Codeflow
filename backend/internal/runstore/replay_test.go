package runstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
)

// These tests cover replay.go: the paged replay with its snapshot boundaries.
// The properties they exist to prove are the ones §27.4 and §20.2 make
// load-bearing:
//
//   - the page and the high watermark / retention floor come from one snapshot,
//     so a client is never told about an event it cannot read and never handed
//     an event past the watermark it was given;
//   - a project replay pages on project_seq and a run replay on run_seq, and
//     the two counters are never mixed;
//   - after = 0 with floor = 1 is the legal first replay, after > high_watermark
//     is invalid_cursor, and after < floor-1 is cursor_expired;
//   - an empty scope answers with hwm = 0, floor = 1 and an empty page rather
//     than an error.

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// replayFixture is a store with a run and helpers that append events with
// distinct instants (so claim/replay order is the append order).
type replayFixture struct {
	t      *testing.T
	events *eventFixture
	store  *Store
}

func newReplayFixture(t *testing.T) *replayFixture {
	t.Helper()
	events := newEventFixture(t)
	return &replayFixture{t: t, events: events, store: events.store}
}

// appendProject appends a project-scoped event and returns it.
func (f *replayFixture) appendProject(t *testing.T) Event {
	t.Helper()
	return f.events.appendEvent(t, run.EventServerRestart, nil)
}

// appendRun appends a run-scoped event and returns it.
func (f *replayFixture) appendRun(t *testing.T) Event {
	t.Helper()
	return f.events.appendEvent(t, run.EventRunFailed, &f.events.runID)
}

// setProjectCounter forces the project's scope counter to value, which is how a
// test models history that retention already trimmed: the next event gets
// value+1 and the retained history starts there.
func (f *replayFixture) setProjectCounter(t *testing.T, value int64) {
	t.Helper()
	if _, err := f.store.DB().ExecContext(context.Background(), `
		INSERT INTO scope_counters (scope, value) VALUES (?, ?)
		ON CONFLICT (scope) DO UPDATE SET value = ?`,
		projectScope(eventProjectID), value, value); err != nil {
		t.Fatalf("set project counter to %d: %v", value, err)
	}
}

// ---------------------------------------------------------------------------
// Paging
// ---------------------------------------------------------------------------

// TestReplayProjectPagesOnProjectSequence walks a project timeline in pages of
// two and checks the cursor chain: next_after is the last sequence of the page,
// has_more is true until the stream ends, and the watermark is the same on
// every page because nothing is being appended.
func TestReplayProjectPagesOnProjectSequence(t *testing.T) {
	ctx := context.Background()
	f := newReplayFixture(t)

	appended := []Event{
		f.appendProject(t),
		f.appendRun(t),
		f.appendProject(t),
		f.appendRun(t),
		f.appendProject(t),
	}

	var (
		after   int64
		gotSeqs []int64
		gotIDs  []string
		pages   int
	)
	for {
		page, err := ReplayProject(ctx, f.store.DB(), eventProjectID, after, 2)
		if err != nil {
			t.Fatalf("ReplayProject(after=%d): %v", after, err)
		}
		pages++
		if page.HighWatermark != 5 {
			t.Errorf("page %d high_watermark = %d, want 5", pages, page.HighWatermark)
		}
		if page.RetentionFloor != 1 {
			t.Errorf("page %d retention_floor = %d, want 1 (nothing was trimmed)", pages, page.RetentionFloor)
		}
		for i, event := range page.Events {
			gotSeqs = append(gotSeqs, event.ProjectSeq)
			gotIDs = append(gotIDs, event.ID)
			if i > 0 && event.ProjectSeq != page.Events[i-1].ProjectSeq+1 {
				t.Errorf("page %d sequences are not consecutive: %v", pages, seqsOf(page.Events))
			}
		}
		if len(page.Events) > 2 {
			t.Errorf("page %d returned %d events, want at most the limit 2", pages, len(page.Events))
		}
		if page.HasMore {
			if len(page.Events) != 2 {
				t.Errorf("page %d has_more with %d events, want a full page", pages, len(page.Events))
			}
			if page.NextAfter != page.Events[len(page.Events)-1].ProjectSeq {
				t.Errorf("page %d next_after = %d, want the last sequence %d",
					pages, page.NextAfter, page.Events[len(page.Events)-1].ProjectSeq)
			}
			after = page.NextAfter
			continue
		}
		// The last page: the cursor reached the watermark.
		if page.NextAfter != 5 {
			t.Errorf("last page next_after = %d, want 5", page.NextAfter)
		}
		break
	}

	if want := []int64{1, 2, 3, 4, 5}; fmt.Sprint(gotSeqs) != fmt.Sprint(want) {
		t.Errorf("replayed project sequences = %v, want %v", gotSeqs, want)
	}
	for i, event := range appended {
		if gotIDs[i] != event.ID {
			t.Errorf("replayed event %d = %s, want %s", i, gotIDs[i], event.ID)
		}
	}
	if pages != 3 {
		t.Errorf("replay took %d pages, want 3", pages)
	}

	// after = the watermark is a legal, empty page: the client is caught up.
	empty, err := ReplayProject(ctx, f.store.DB(), eventProjectID, 5, 2)
	if err != nil {
		t.Fatalf("ReplayProject(after=watermark): %v", err)
	}
	if len(empty.Events) != 0 || empty.HasMore || empty.NextAfter != 5 {
		t.Errorf("page after the watermark = %+v, want an empty page with next_after=5", empty)
	}
	if empty.HighWatermark != 5 || empty.RetentionFloor != 1 {
		t.Errorf("boundaries after the watermark = hwm %d floor %d, want 5/1", empty.HighWatermark, empty.RetentionFloor)
	}
}

// TestReplayRunPagesOnRunSequence proves the run projection uses run_seq and not
// project_seq: the two events of the run are interleaved with project-only
// events, so a run replay that used the project counter would return the wrong
// rows and the wrong watermark.
func TestReplayRunPagesOnRunSequence(t *testing.T) {
	ctx := context.Background()
	f := newReplayFixture(t)

	f.appendProject(t)       // project 1
	first := f.appendRun(t)  // project 2, run 1
	f.appendProject(t)       // project 3
	second := f.appendRun(t) // project 4, run 2
	f.appendProject(t)       // project 5
	f.appendRun(t)           // project 6, run 3

	page, err := ReplayRun(ctx, f.store.DB(), f.events.runID, 0, 10)
	if err != nil {
		t.Fatalf("ReplayRun: %v", err)
	}
	if page.HighWatermark != 3 {
		t.Errorf("run high_watermark = %d, want 3 (the run counter, not the project's 6)", page.HighWatermark)
	}
	if page.RetentionFloor != 1 {
		t.Errorf("run retention_floor = %d, want 1", page.RetentionFloor)
	}
	if got := runSeqsOf(page.Events); fmt.Sprint(got) != fmt.Sprint([]int64{1, 2, 3}) {
		t.Errorf("run sequences = %v, want [1 2 3]", got)
	}
	if len(page.Events) != 3 || page.Events[0].ID != first.ID || page.Events[1].ID != second.ID {
		t.Errorf("run page = %+v, want the three run events in order", page.Events)
	}
	// The same rows keep their project_seq: the two views are projections of
	// one row, not two copies.
	if page.Events[0].ProjectSeq != 2 || page.Events[1].ProjectSeq != 4 {
		t.Errorf("project sequences of the run page = %d, %d, want 2, 4",
			page.Events[0].ProjectSeq, page.Events[1].ProjectSeq)
	}

	// A run cursor is a run sequence: after=1 returns the rest of the run and
	// nothing of the project-only events.
	rest, err := ReplayRun(ctx, f.store.DB(), f.events.runID, 1, 10)
	if err != nil {
		t.Fatalf("ReplayRun(after=1): %v", err)
	}
	if got := runSeqsOf(rest.Events); fmt.Sprint(got) != fmt.Sprint([]int64{2, 3}) {
		t.Errorf("run sequences after 1 = %v, want [2 3]", got)
	}
	if rest.HasMore {
		t.Error("run page has_more = true, want false")
	}
	if rest.NextAfter != 3 {
		t.Errorf("run next_after = %d, want 3", rest.NextAfter)
	}

	// A run cursor past the run's watermark is invalid even though the project
	// has more events: the scope decides, not the table.
	if _, err := ReplayRun(ctx, f.store.DB(), f.events.runID, 4, 10); !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("ReplayRun(after=4) error = %v, want ErrInvalidCursor", err)
	}

	// A different run has no events at all: hwm 0, floor 1, empty page.
	empty, err := ReplayRun(ctx, f.store.DB(), "run-does-not-exist", 0, 10)
	if err != nil {
		t.Fatalf("ReplayRun(unknown run): %v", err)
	}
	if len(empty.Events) != 0 || empty.HighWatermark != 0 || empty.RetentionFloor != 1 || empty.NextAfter != 0 {
		t.Errorf("unknown run page = %+v, want an empty page with hwm=0 floor=1", empty)
	}
}

// TestReplayEmptyScopeIsTheLegalFirstReplay pins the §20.2 first replay: a scope
// with no events answers after=0 with an empty page, hwm=0 and floor=1, not
// with an error.
func TestReplayEmptyScopeIsTheLegalFirstReplay(t *testing.T) {
	ctx := context.Background()
	f := newReplayFixture(t)

	page, err := ReplayProject(ctx, f.store.DB(), "p-never-written", 0, 0)
	if err != nil {
		t.Fatalf("ReplayProject on an empty scope: %v", err)
	}
	if len(page.Events) != 0 || page.HasMore || page.NextAfter != 0 {
		t.Errorf("empty scope page = %+v, want an empty page with next_after=0", page)
	}
	if page.HighWatermark != 0 {
		t.Errorf("empty scope high_watermark = %d, want 0", page.HighWatermark)
	}
	if page.RetentionFloor != 1 {
		t.Errorf("empty scope retention_floor = %d, want 1", page.RetentionFloor)
	}

	// after=0 is legal on an empty scope; anything past the watermark is not.
	if _, err := ReplayProject(ctx, f.store.DB(), "p-never-written", 1, 0); !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("ReplayProject(after=1) on an empty scope error = %v, want ErrInvalidCursor", err)
	}
}

// TestReplayLimitIsClampedToTheEventBounds pins the paging bounds the list
// reads already use: a limit <= 0 means the default, and a limit above the cap
// is clamped rather than refused (the rest is reachable through has_more).
func TestReplayLimitIsClampedToTheEventBounds(t *testing.T) {
	ctx := context.Background()
	f := newReplayFixture(t)

	const total = MaxEventLimit + 1
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		for i := 0; i < total; i++ {
			if _, err := AppendEventTx(ctx, tx, EventInput{
				ProjectID:  eventProjectID,
				Type:       string(run.EventServerRestart),
				OccurredAt: time.UnixMilli(1700000003000 + int64(i)),
				Identity:   eventIdentity(eventProjectID, "system", nil),
				Payload:    []byte(`{"i":` + fmt.Sprint(i) + `}`),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("append %d events: %v", total, err)
	}

	page, err := ReplayProject(ctx, f.store.DB(), eventProjectID, 0, 5000)
	if err != nil {
		t.Fatalf("ReplayProject(limit=5000): %v", err)
	}
	if len(page.Events) != MaxEventLimit {
		t.Errorf("page length = %d, want the %d cap", len(page.Events), MaxEventLimit)
	}
	if !page.HasMore {
		t.Error("has_more = false, want true (one event is past the cap)")
	}
	if page.HighWatermark != total {
		t.Errorf("high_watermark = %d, want %d", page.HighWatermark, total)
	}
	if page.NextAfter != MaxEventLimit {
		t.Errorf("next_after = %d, want %d", page.NextAfter, MaxEventLimit)
	}

	// The clamped page is still complete: paging on reaches the last event.
	rest, err := ReplayProject(ctx, f.store.DB(), eventProjectID, page.NextAfter, 5000)
	if err != nil {
		t.Fatalf("ReplayProject(after=cap): %v", err)
	}
	if len(rest.Events) != 1 || rest.Events[0].ProjectSeq != total || rest.HasMore {
		t.Errorf("rest = %v (has_more %v), want one event at %d", seqsOf(rest.Events), rest.HasMore, total)
	}

	// A non-positive limit means the default, not "no rows" and not "all rows".
	for _, limit := range []int{0, -5} {
		got, err := ReplayProject(ctx, f.store.DB(), eventProjectID, 0, limit)
		if err != nil {
			t.Fatalf("ReplayProject(limit=%d): %v", limit, err)
		}
		if len(got.Events) != DefaultEventLimit {
			t.Errorf("ReplayProject(limit=%d) returned %d events, want the default %d",
				limit, len(got.Events), DefaultEventLimit)
		}
	}
}

// ---------------------------------------------------------------------------
// Cursor rules
// ---------------------------------------------------------------------------

// TestReplayRejectsInvalidCursors pins the 422 side of §27.4: a negative cursor
// and a cursor past the watermark are both wrong positions, not empty answers.
func TestReplayRejectsInvalidCursors(t *testing.T) {
	ctx := context.Background()
	f := newReplayFixture(t)

	f.appendProject(t)
	f.appendProject(t)

	cases := []struct {
		name  string
		after int64
	}{
		{"negative", -1},
		{"past the watermark", 3},
		{"far past the watermark", 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReplayProject(ctx, f.store.DB(), eventProjectID, tc.after, 10)
			if !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("ReplayProject(after=%d) error = %v, want ErrInvalidCursor", tc.after, err)
			}
			if errors.Is(err, ErrCursorExpired) {
				t.Errorf("ReplayProject(after=%d) error = %v, want it not to be ErrCursorExpired", tc.after, err)
			}
		})
	}

	// after = watermark is valid (the client is caught up).
	if _, err := ReplayProject(ctx, f.store.DB(), eventProjectID, 2, 10); err != nil {
		t.Errorf("ReplayProject(after=watermark) error = %v, want a valid empty page", err)
	}
}

// TestReplayExpiredCursorIsReported is the 410 side of §27.4. The counter is
// preset to 4 (as if events 1-4 had been trimmed), so the first event appended
// afterwards is sequence 5 and the retention floor is 5. after = 4 is the last
// legal position before the retained history; anything below it is expired.
func TestReplayExpiredCursorIsReported(t *testing.T) {
	ctx := context.Background()
	f := newReplayFixture(t)

	f.setProjectCounter(t, 4)
	first := f.appendProject(t)
	if first.ProjectSeq != 5 {
		t.Fatalf("first retained event has project_seq %d, want 5 after the preset counter", first.ProjectSeq)
	}
	f.appendProject(t)

	page, err := ReplayProject(ctx, f.store.DB(), eventProjectID, 4, 10)
	if err != nil {
		t.Fatalf("ReplayProject(after=floor-1) error = %v, want it to be legal", err)
	}
	if page.RetentionFloor != 5 {
		t.Errorf("retention_floor = %d, want 5 (the oldest retained event)", page.RetentionFloor)
	}
	if page.HighWatermark != 6 {
		t.Errorf("high_watermark = %d, want 6", page.HighWatermark)
	}
	if got := seqsOf(page.Events); fmt.Sprint(got) != fmt.Sprint([]int64{5, 6}) {
		t.Errorf("page sequences = %v, want [5 6]", got)
	}
	if page.NextAfter != 6 || page.HasMore {
		t.Errorf("next_after=%d has_more=%v, want 6/false", page.NextAfter, page.HasMore)
	}

	// Everything below floor-1 is expired, and the error is distinct from an
	// invalid cursor: the client must replace its cache, not fix its number.
	for _, after := range []int64{0, 2, 3} {
		_, err := ReplayProject(ctx, f.store.DB(), eventProjectID, after, 10)
		if !errors.Is(err, ErrCursorExpired) {
			t.Errorf("ReplayProject(after=%d) error = %v, want ErrCursorExpired", after, err)
		}
		if errors.Is(err, ErrInvalidCursor) {
			t.Errorf("ReplayProject(after=%d) error = %v, want it not to be ErrInvalidCursor", after, err)
		}
	}

	// The watermark is still the ceiling: a cursor past it is invalid, not
	// expired.
	if _, err := ReplayProject(ctx, f.store.DB(), eventProjectID, 7, 10); !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("ReplayProject(after=7) error = %v, want ErrInvalidCursor", err)
	}
}

// TestReplayCursorErrorsCarryTheSnapshotBounds: a 410/422 answer must quote the
// numbers that decided it, so the two cursor errors return the page's boundaries
// (and no events) from the same statement instead of a zero page.
func TestReplayCursorErrorsCarryTheSnapshotBounds(t *testing.T) {
	ctx := context.Background()
	f := newReplayFixture(t)
	f.setProjectCounter(t, 4)
	f.appendProject(t) // 5
	f.appendProject(t) // 6

	page, err := ReplayProject(ctx, f.store.DB(), eventProjectID, 9, 10)
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("after past the watermark: %v, want ErrInvalidCursor", err)
	}
	if page.HighWatermark != 6 || page.RetentionFloor != 5 || page.NextAfter != 9 || len(page.Events) != 0 {
		t.Fatalf("invalid-cursor page = %+v, want watermark 6, floor 5, next_after 9, no events", page)
	}

	page, err = ReplayProject(ctx, f.store.DB(), eventProjectID, 1, 10)
	if !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("after below the floor: %v, want ErrCursorExpired", err)
	}
	if page.HighWatermark != 6 || page.RetentionFloor != 5 || page.NextAfter != 1 || len(page.Events) != 0 {
		t.Fatalf("expired-cursor page = %+v, want watermark 6, floor 5, next_after 1, no events", page)
	}
}

// TestReplayFullyTrimmedScopeExpiresOldCursors covers a scope whose counter has
// moved but which retains no event at all (retention trimmed the whole
// history). The floor must be watermark+1, not the "never written" value 1:
// otherwise after=0 would be accepted and answered with an empty page, and the
// client would go live believing it had missed nothing.
func TestReplayFullyTrimmedScopeExpiresOldCursors(t *testing.T) {
	ctx := context.Background()
	f := newReplayFixture(t)
	f.setProjectCounter(t, 4)

	page, err := ReplayProject(ctx, f.store.DB(), eventProjectID, 4, 10)
	if err != nil {
		t.Fatalf("ReplayProject(after=watermark) error = %v, want an empty legal page", err)
	}
	if page.HighWatermark != 4 || page.RetentionFloor != 5 {
		t.Fatalf("watermark/floor = %d/%d, want 4/5", page.HighWatermark, page.RetentionFloor)
	}
	if len(page.Events) != 0 || page.HasMore || page.NextAfter != 4 {
		t.Fatalf("page = %d events, has_more=%v, next_after=%d; want empty/false/4",
			len(page.Events), page.HasMore, page.NextAfter)
	}
	for _, after := range []int64{0, 1, 3} {
		if _, err := ReplayProject(ctx, f.store.DB(), eventProjectID, after, 10); !errors.Is(err, ErrCursorExpired) {
			t.Errorf("ReplayProject(after=%d) on a fully trimmed scope error = %v, want ErrCursorExpired", after, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Snapshot consistency
// ---------------------------------------------------------------------------

// TestReplayIsConsistentUnderConcurrentAppends is the reason the page and its
// boundaries come from one statement: while events are appended continuously,
// every replay must satisfy
//
//   - the page's sequences are strictly increasing and consecutive;
//   - the high watermark is at or after every sequence in the page (a page can
//     never contain an event the client was not told exists);
//   - a page that reports has_more=false ends exactly at the watermark (a
//     complete page cannot be followed by an event below the watermark);
//   - next_after is the last sequence of the page.
//
// A second statement for the watermark — before the page — breaks the second
// rule; after the page it breaks the third.
func TestReplayIsConsistentUnderConcurrentAppends(t *testing.T) {
	ctx := context.Background()
	f := newReplayFixture(t)

	const (
		writers   = 4
		perWriter = 25
	)
	var (
		wg      sync.WaitGroup
		stop    = make(chan struct{})
		replays int
	)

	// The appenders.
	wg.Add(writers)
	for w := 0; w < writers; w++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				_, err := f.events.tryAppendInput(EventInput{
					ProjectID:  eventProjectID,
					Type:       string(run.EventServerRestart),
					OccurredAt: time.UnixMilli(1700000002000 + int64(w*1000+i)),
					Identity:   eventIdentity(eventProjectID, "system", nil),
					Payload:    []byte(`{"w":` + fmt.Sprint(w) + `,"i":` + fmt.Sprint(i) + `}`),
				})
				if err != nil {
					t.Errorf("concurrent append: %v", err)
					return
				}
			}
		}(w)
	}

	// The replayer: read pages from the start while the appends land.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			page, err := ReplayProject(ctx, f.store.DB(), eventProjectID, 0, 16)
			if err != nil {
				t.Errorf("replay during appends: %v", err)
				return
			}
			replays++
			if page.HighWatermark < 0 || page.RetentionFloor != 1 {
				t.Errorf("boundaries = hwm %d floor %d, want a non-negative hwm and floor 1",
					page.HighWatermark, page.RetentionFloor)
			}
			for i, event := range page.Events {
				if event.ProjectSeq > page.HighWatermark {
					t.Errorf("page contains sequence %d above the high watermark %d",
						event.ProjectSeq, page.HighWatermark)
				}
				if i > 0 && event.ProjectSeq != page.Events[i-1].ProjectSeq+1 {
					t.Errorf("page sequences are not consecutive: %v", seqsOf(page.Events))
				}
				if event.ProjectSeq > int64(writers*perWriter) {
					t.Errorf("page contains sequence %d, past what was ever appended", event.ProjectSeq)
				}
			}
			if len(page.Events) > 0 {
				if page.NextAfter != page.Events[len(page.Events)-1].ProjectSeq {
					t.Errorf("next_after = %d, want the last sequence %d",
						page.NextAfter, page.Events[len(page.Events)-1].ProjectSeq)
				}
			} else if page.NextAfter != 0 {
				t.Errorf("empty page next_after = %d, want 0", page.NextAfter)
			}
			if !page.HasMore && len(page.Events) > 0 && page.NextAfter != page.HighWatermark {
				t.Errorf("a complete page ends at %d but the watermark is %d: the page and its watermark are not one snapshot",
					page.NextAfter, page.HighWatermark)
			}
		}
	}()

	wg.Wait()
	close(stop)
	<-done

	if replays == 0 {
		t.Fatal("no replay ran while the appends were landing")
	}

	// After the writers stopped, the whole stream is reachable by paging from 0
	// and the final page ends exactly at the watermark.
	total := writers * perWriter
	after := int64(0)
	seen := int64(0)
	for {
		page, err := ReplayProject(ctx, f.store.DB(), eventProjectID, after, 64)
		if err != nil {
			t.Fatalf("final replay(after=%d): %v", after, err)
		}
		if page.HighWatermark != int64(total) {
			t.Fatalf("final high_watermark = %d, want %d", page.HighWatermark, total)
		}
		if len(page.Events) == 0 {
			break
		}
		for i, event := range page.Events {
			if event.ProjectSeq != seen+int64(i)+1 {
				t.Fatalf("event %d has sequence %d, want %d", seen+int64(i), event.ProjectSeq, seen+int64(i)+1)
			}
		}
		seen += int64(len(page.Events))
		after = page.NextAfter
	}
	if seen != int64(total) {
		t.Errorf("final replay saw %d events, want %d", seen, total)
	}
	t.Logf("EVIDENCE replay consistency: %d concurrent replays, %d events", replays, total)
}

// TestReplayPageMatchesGetEvent pins the replay scanner against GetEvent: the
// two must return the same row field by field, so a column added to one and
// forgotten in the other cannot pass unnoticed.
func TestReplayPageMatchesGetEvent(t *testing.T) {
	ctx := context.Background()
	f := newReplayFixture(t)

	project := f.appendProject(t)
	runScoped := f.appendRun(t)

	for _, want := range []Event{project, runScoped} {
		page, err := ReplayProject(ctx, f.store.DB(), eventProjectID, want.ProjectSeq-1, 1)
		if err != nil {
			t.Fatalf("ReplayProject(after=%d): %v", want.ProjectSeq-1, err)
		}
		if len(page.Events) != 1 {
			t.Fatalf("page for sequence %d has %d events, want 1", want.ProjectSeq, len(page.Events))
		}
		got := page.Events[0]
		if got.ID != want.ID || got.ProjectID != want.ProjectID || got.ProjectSeq != want.ProjectSeq ||
			got.Type != want.Type || got.SchemaVersion != want.SchemaVersion ||
			!got.OccurredAt.Equal(want.OccurredAt) || got.PayloadHash != want.PayloadHash ||
			string(got.Identity) != string(want.Identity) || string(got.Payload) != string(want.Payload) {
			t.Errorf("replayed event = %+v, want %+v", got, want)
		}
		if (got.RunID == nil) != (want.RunID == nil) || (got.RunSeq == nil) != (want.RunSeq == nil) ||
			(got.AttemptID == nil) != (want.AttemptID == nil) {
			t.Errorf("replayed nullable columns differ from the stored event: %+v vs %+v", got, want)
		}
		if got.RunID != nil && *got.RunID != *want.RunID {
			t.Errorf("replayed run_id = %q, want %q", *got.RunID, *want.RunID)
		}
		if got.RunSeq != nil && *got.RunSeq != *want.RunSeq {
			t.Errorf("replayed run_seq = %d, want %d", *got.RunSeq, *want.RunSeq)
		}

		stored, err := GetEvent(ctx, f.store.DB(), want.ID)
		if err != nil {
			t.Fatalf("GetEvent(%s): %v", want.ID, err)
		}
		if eventFingerprint(got) != eventFingerprint(stored) {
			t.Errorf("replayed event differs from GetEvent:\n replay %s\n stored %s",
				eventFingerprint(got), eventFingerprint(stored))
		}
	}

	// A run replay returns the same row as the project replay, with the same
	// project_seq and the run's own sequence.
	page, err := ReplayRun(ctx, f.store.DB(), f.events.runID, 0, 1)
	if err != nil {
		t.Fatalf("ReplayRun: %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].ID != runScoped.ID || page.Events[0].ProjectSeq != runScoped.ProjectSeq {
		t.Errorf("run replay returned %+v, want the run event %s", page.Events, runScoped.ID)
	}
}

// eventFingerprint renders an event as one comparable string with the optional
// ids dereferenced: two Events describing the same row must compare equal even
// though their pointers were allocated separately.
func eventFingerprint(event Event) string {
	optional := func(id *string) string {
		if id == nil {
			return "<nil>"
		}
		return *id
	}
	seq := func(v *int64) string {
		if v == nil {
			return "<nil>"
		}
		return fmt.Sprint(*v)
	}
	return fmt.Sprintf("id=%s project=%s project_seq=%d run=%s run_seq=%s attempt=%s type=%s schema=%d at=%s identity=%s payload=%s hash=%s",
		event.ID, event.ProjectID, event.ProjectSeq, optional(event.RunID), seq(event.RunSeq),
		optional(event.AttemptID), event.Type, event.SchemaVersion,
		event.OccurredAt.UTC().Format(time.RFC3339Nano), event.Identity, event.Payload, event.PayloadHash)
}

// TestReplayReadsTakeAQuerier proves the replay works both through the pool and
// inside a caller's transaction, and that inside a transaction it sees that
// transaction's own uncommitted appends (which is what makes "snapshot and
// cursor in one transaction" usable by the API layer).
func TestReplayReadsTakeAQuerier(t *testing.T) {
	ctx := context.Background()
	f := newReplayFixture(t)

	f.appendProject(t)

	var inside ReplayPage
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, err := AppendEventTx(ctx, tx, EventInput{
			ProjectID:  eventProjectID,
			Type:       string(run.EventServerRestart),
			OccurredAt: time.UnixMilli(1700000002500),
			Identity:   eventIdentity(eventProjectID, "system", nil),
			Payload:    []byte(`{"inTx":true}`),
		}); err != nil {
			return err
		}
		var err error
		inside, err = ReplayProject(ctx, tx, eventProjectID, 0, 10)
		return err
	})
	if err != nil {
		t.Fatalf("ReplayProject inside WithTx: %v", err)
	}
	if got := seqsOf(inside.Events); fmt.Sprint(got) != fmt.Sprint([]int64{1, 2}) {
		t.Errorf("in-transaction replay sequences = %v, want [1 2]", got)
	}
	if inside.HighWatermark != 2 {
		t.Errorf("in-transaction high_watermark = %d, want 2", inside.HighWatermark)
	}

	// Through the pool, after the commit, the same answer.
	after, err := ReplayProject(ctx, f.store.DB(), eventProjectID, 0, 10)
	if err != nil {
		t.Fatalf("ReplayProject through the pool: %v", err)
	}
	if got := seqsOf(after.Events); fmt.Sprint(got) != fmt.Sprint([]int64{1, 2}) {
		t.Errorf("post-commit replay sequences = %v, want [1 2]", got)
	}

	// A transaction that rolls back leaves the replay unchanged, and the
	// counter it consumed is returned: a replayed page never shows a gap from
	// an undone append.
	sentinel := errors.New("roll back")
	err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, err := AppendEventTx(ctx, tx, EventInput{
			ProjectID:  eventProjectID,
			Type:       string(run.EventServerRestart),
			OccurredAt: time.UnixMilli(1700000002600),
			Identity:   eventIdentity(eventProjectID, "system", nil),
			Payload:    []byte(`{"rolledBack":true}`),
		}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx error = %v, want the sentinel", err)
	}
	again, err := ReplayProject(ctx, f.store.DB(), eventProjectID, 0, 10)
	if err != nil {
		t.Fatalf("ReplayProject after the rollback: %v", err)
	}
	if again.HighWatermark != 2 || len(again.Events) != 2 {
		t.Errorf("after the rollback: hwm=%d events=%d, want 2/2", again.HighWatermark, len(again.Events))
	}
}

// TestReplayReadsDoNotTakeTheWriteLock proves the choice made in replay.go:
// reads take a Querier and never open a BEGIN IMMEDIATE transaction, so a
// replay can neither block an append nor wait for one. The test holds a write
// transaction open in the background and replays while it is open: the replay
// must answer promptly (with the committed state) rather than queue behind the
// writer, which is what a read that took the write lock would do.
func TestReplayReadsDoNotTakeTheWriteLock(t *testing.T) {
	ctx := context.Background()
	f := newReplayFixture(t)
	f.appendProject(t)

	// A writer holds a write transaction open for a moment while a replay runs.
	started := make(chan struct{})
	release := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			if _, err := AppendEventTx(ctx, tx, EventInput{
				ProjectID:  eventProjectID,
				Type:       string(run.EventServerRestart),
				OccurredAt: time.UnixMilli(1700000002700),
				Identity:   eventIdentity(eventProjectID, "system", nil),
				Payload:    []byte(`{"held":true}`),
			}); err != nil {
				return err
			}
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	// The replay runs while the writer's transaction is open: it must not block
	// on the write lock (it reads the committed state, which is one event).
	done := make(chan ReplayPage, 1)
	go func() {
		page, err := ReplayProject(ctx, f.store.DB(), eventProjectID, 0, 10)
		if err != nil {
			t.Errorf("replay during a write transaction: %v", err)
		}
		done <- page
	}()
	select {
	case page := <-done:
		if page.HighWatermark != 1 {
			t.Errorf("replay during a write transaction saw hwm %d, want 1 (the committed state)", page.HighWatermark)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a replay blocked while a write transaction was open: reads must not take the write lock")
	}

	close(release)
	if err := <-writerDone; err != nil {
		t.Fatalf("writer transaction: %v", err)
	}

	// The append is visible to the next replay.
	page, err := ReplayProject(ctx, f.store.DB(), eventProjectID, 0, 10)
	if err != nil {
		t.Fatalf("ReplayProject after the writer committed: %v", err)
	}
	if page.HighWatermark != 2 || len(page.Events) != 2 {
		t.Errorf("after the commit: hwm=%d events=%d, want 2/2", page.HighWatermark, len(page.Events))
	}
}
