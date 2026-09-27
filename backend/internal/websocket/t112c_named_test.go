package websocket

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/runstore"
	gorillaws "github.com/gorilla/websocket"
)

// These three tests are the ones §28 T1.12.c names, and they pin the §15
// acceptance sentence — "断线期间事件全可回放；gap 明确进入快照恢复流程" — on the wire,
// over a real store, a real HTTP server and a real WebSocket client:
//
//   - TestReplayLiveBoundaryNoGap: the client comes back with a cursor (it missed
//     events while it was away) and the store is appended to while the replay is
//     paging, so the seam between "replayed" and "live" falls inside the stream.
//     The events above H must arrive from the live phase — read from the store by
//     cursor, with no wake-up at all — because an event appended after the
//     snapshot that fixed H, and in particular one appended after the replay's
//     last read, is exactly §27.4's forbidden silent gap if it is not delivered.
//   - TestSlowClientReconnect: a client that stops reading is closed with 1013
//     (Try Again Later, reason "slow consumer") rather than served a gap, without
//     dropping a single frame on the way; reconnecting from the last APPLIED
//     sequence receives every remaining sequence once, in order, and the
//     connection stays healthy — and a close that is not 1013 is never dressed up
//     as one.
//   - TestCursorExpiredSnapshot: a cursor below the retention floor ends the
//     subscription with the machine-readable recovery instruction
//     (cursor_expired, recovery:"snapshot", retention_floor), the client resumes
//     from the cursor a snapshot carries and gets the rest, and the server never
//     adopts a cursor for the client (invalid_cursor quotes the watermark and the
//     frame carries no new cursor).
//
// Every wait here is driven by a frame, a cursor or a channel — never by a sleep
// that decides the ORDER — so -count=3 cannot make them flap. The helpers this
// file adds are suffixed t112c/smallReadBuffer so they cannot collide with the
// older fixtures.

// ---------------------------------------------------------------------------
// §28 T1.12.c: TestReplayLiveBoundaryNoGap
// ---------------------------------------------------------------------------

// TestReplayLiveBoundaryNoGap is §15's "断线期间事件全可回放" at the boundary that
// makes it hard: the subscription starts at after=2 (the client applied 1 and 2
// before it was disconnected), two more events are appended while the replay is
// paging — one right after the read whose snapshot fixed H, one after the replay's
// LAST read, i.e. after the fetch had reached H and before the live phase started
// — and nothing ever wakes the connection (no deliverer is invoked, the poll
// interval is the only source).
//
// The assertions are the ones the two halves of §27.4 need:
//
//   - the replay carries the closed window and nothing else: every sequence above
//     the cursor up to H, never an already-applied sequence and never one above H;
//   - the live phase carries exactly the events above H, in order, so the client's
//     stream is after+1..last once each — no duplicate, no hole;
//   - H does not move while the replay runs: it is the number the snapshot read,
//     not the count the store happens to have.
func TestReplayLiveBoundaryNoGap(t *testing.T) {
	f := newReplayFixture(t)
	// The events that existed when the client came back. H is fixed at 5 by the
	// first read (the page and its watermark come from one SQL snapshot).
	for i := 0; i < 5; i++ {
		f.projectEvent(testProjectA)
	}
	// One append after the first read (the snapshot that fixed H) and one after
	// the second, which is the replay's last read: the second event is appended
	// after the fetch reached H and before the live phase started — the window a
	// server that "switches to live at the last fetched page" would drop.
	reader := &appendOnPage{
		EventReader: f.reader,
		appendAt:    map[int]int{1: 1, 2: 1},
		append:      func() runstore.Event { return f.projectEvent(testProjectA) },
	}
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       reader,
		Runs:         f.auth.Runs,
		PollInterval: 5 * time.Millisecond, // the live phase's guarantee, and its only delivery here
		PageLimit:    2,
	})
	conn := projectStream(t, srv, testProjectA)
	conn.subscribe(ResourceTypeProject, testProjectA, 2, "sub_1")

	subscribed := conn.expectFrame(MsgTypeSubscribed)
	assertJSONString(t, subscribed.Data, "client_request_id", "sub_1")
	assertJSONString(t, subscribed.Data, "scope", "project:"+testProjectA)

	started := conn.expectFrame(MsgTypeReplayStarted)
	if after := jsonNumber(t, started.Data, "after"); after != 2 {
		t.Fatalf("replay_started after = %d, want the 2 the client asked to resume from", after)
	}
	highWatermark := jsonNumber(t, started.Data, "high_watermark")
	if highWatermark != 5 {
		t.Fatalf("replay_started high_watermark = %d, want 5 (the store at the snapshot read)", highWatermark)
	}
	if floor := jsonNumber(t, started.Data, "retention_floor"); floor != 1 {
		t.Fatalf("replay_started retention_floor = %d, want 1 (nothing was trimmed)", floor)
	}

	var (
		replaySequences []int64
		liveSequences   []int64
		frames          int
		finished        bool
	)
	// Read until the last appended event (sequence 7) has reached the client. The
	// read deadline on every frame is what turns "the live phase never delivers"
	// into a failure with a message rather than a hang.
	for len(liveSequences) == 0 || liveSequences[len(liveSequences)-1] < 7 {
		frames++
		if frames > 64 {
			t.Fatalf("read %d frames without reaching sequence 7 (replay %v, live %v)",
				frames, replaySequences, liveSequences)
		}
		_, shape := conn.readRaw()
		switch shape.Type {
		case MsgTypeEvent:
			sequence := frameSequence(t, shape)
			assertJSONString(t, shape.Data, "scope", "project:"+testProjectA)
			if !finished {
				// The replay window: the cursor it was asked for is respected
				// (nothing already applied is re-sent) and H is a ceiling.
				if sequence <= 2 {
					t.Fatalf("the replay re-sent sequence %d, at or below the after=2 cursor", sequence)
				}
				if sequence > highWatermark {
					t.Fatalf("the replay carried sequence %d above its high watermark %d", sequence, highWatermark)
				}
				replaySequences = append(replaySequences, sequence)
				continue
			}
			if sequence <= highWatermark {
				t.Fatalf("the live phase delivered sequence %d, at or below the high watermark %d", sequence, highWatermark)
			}
			if len(liveSequences) > 0 && sequence != liveSequences[len(liveSequences)-1]+1 {
				t.Fatalf("the live phase delivered %d after %d: the live stream gapped",
					sequence, liveSequences[len(liveSequences)-1])
			}
			liveSequences = append(liveSequences, sequence)
		case MsgTypeReplayFinished:
			if finished {
				t.Fatal("a second replay_finished arrived on one subscription")
			}
			assertJSONString(t, shape.Data, "client_request_id", "sub_1")
			assertJSONNumber(t, shape.Data, "high_watermark", highWatermark)
			// next_after is the replay's own end, and where the live phase starts.
			assertJSONNumber(t, shape.Data, "next_after", replaySequences[len(replaySequences)-1])
			finished = true
		default:
			t.Fatalf("frame %q inside the subscription, want an event or replay_finished", shape.Type)
		}
	}

	// The appends really happened, and they are the events above H.
	appended := reader.appended()
	if len(appended) != 2 {
		t.Fatalf("the reader appended %d events, want 2 (the test did not cross the boundary)", len(appended))
	}
	if appended[0].ProjectSeq != 6 || appended[1].ProjectSeq != 7 {
		t.Fatalf("appended sequences = %d, %d, want 6 and 7 (both above H)", appended[0].ProjectSeq, appended[1].ProjectSeq)
	}

	// The replay's closed window: H is 5 and the client already had 2, so 3, 4, 5
	// once each, in order.
	if got := replaySequences; len(got) != 3 || got[0] != 3 || got[1] != 4 || got[2] != 5 {
		t.Fatalf("the replay delivered %v, want [3 4 5]", got)
	}
	// The live phase's open window: 6 and 7 once each, in order — including the
	// event appended after the replay's last read, which nothing but a by-cursor
	// read of the store could deliver (no delivery ever woke this subscription).
	if got := liveSequences; len(got) != 2 || got[0] != 6 || got[1] != 7 {
		t.Fatalf("the live phase delivered %v, want [6 7]", got)
	}
	// Every sequence of the scope from the cursor to the last append, exactly
	// once: the union of the two phases is one contiguous stream with no hole and
	// no duplicate.
	union := append(append([]int64(nil), replaySequences...), liveSequences...)
	for i, sequence := range union {
		if want := int64(3 + i); sequence != want {
			t.Fatalf("the client's stream is %v, which is not 3..7 once each: position %d is %d, want %d",
				union, i, sequence, want)
		}
	}
	if len(union) != 5 {
		t.Fatalf("the client received %d events, want 5 (3..7 once each)", len(union))
	}
}

// smallReadBufferDialer shrinks the client's TCP receive window, so a client that
// reads slowly fills the server's send buffer after a few dozen frames instead of
// hundreds of thousands.
func smallReadBufferDialer(dialer *gorillaws.Dialer) {
	dialer.NetDial = func(network, address string) (net.Conn, error) {
		raw, err := (&net.Dialer{Timeout: 5 * time.Second}).Dial(network, address)
		if err != nil {
			return nil, err
		}
		if tcp, ok := raw.(*net.TCPConn); ok {
			_ = tcp.SetReadBuffer(4096)
		}
		return raw, nil
	}
}

// ---------------------------------------------------------------------------
// §28 T1.12.c: TestSlowClientReconnect
// ---------------------------------------------------------------------------

// TestSlowClientReconnect is §27.4's slow-client rule read as a promise: the
// server may shed a client that cannot keep up, but only with 1013 (Try Again
// Later) and the reason "slow consumer" — because the alternative, dropping the
// frame, is the silent gap the same paragraph forbids. The client reconnects from
// the last sequence it APPLIED (not from the high watermark), receives the tail in
// order without a hole, and the connection is healthy again afterwards, which is
// what makes 1013 a "come back" rather than a failure.
//
// The negative half keeps the rule honest: a close that is NOT 1013 must not be
// readable as a slow-consumer shed, because the client's reconnect path keys on
// the code alone (ws.ts: 1013 → reconnect promptly and do not re-pair; any other
// code → the auth re-pair path).
func TestSlowClientReconnect(t *testing.T) {
	f := newReplayFixture(t)
	const total = 600
	last := appendBigProjectEvents(t, f.store, testProjectA, total, 4096, 1700000001000)
	if last.ProjectSeq != total {
		t.Fatalf("backlog ended at sequence %d, want %d", last.ProjectSeq, total)
	}

	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: 5 * time.Millisecond,
	})
	conn := dialStream(t, srv, "/projects/"+testProjectA+"/stream", smallReadBufferDialer)
	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")

	// A client that cannot keep up, not one that has gone away: it reads, applies
	// and sleeps. What it applies is what a real client would have persisted, and
	// its last sequence is the cursor the reconnect must use.
	type readResult struct {
		raw []byte
		err error
	}
	applied := make([]int64, 0, total)
	frames := make(chan readResult, 64)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(frames)
		for {
			_ = conn.conn.SetReadDeadline(time.Now().Add(20 * time.Second))
			_, data, err := conn.conn.ReadMessage()
			if err != nil {
				frames <- readResult{err: err}
				return
			}
			select {
			case frames <- readResult{raw: data}:
			case <-done:
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	var (
		closeErr   error
		readFrames int
	)
	for result := range frames {
		if result.err != nil {
			closeErr = result.err
			break
		}
		readFrames++
		if readFrames > total+16 {
			t.Fatalf("received %d frames without a close: the buffer never filled", readFrames)
		}
		var top struct {
			Type string `json:"type"`
			Data struct {
				Sequence int64 `json:"sequence"`
			} `json:"data"`
		}
		if err := json.Unmarshal(result.raw, &top); err != nil {
			t.Fatalf("server frame is not JSON: %v (%s)", err, result.raw)
		}
		if MessageType(top.Type) != MsgTypeEvent {
			continue
		}
		applied = append(applied, top.Data.Sequence)
	}
	if isTimeout(closeErr) {
		t.Fatalf("the server never closed the connection after %d frames: the client was left hanging", readFrames)
	}

	// The close the server chose, and the reason it gave: both halves of §27.4's
	// instruction, and 1013 is the code the client acts on.
	closeError, ok := closeErr.(*gorillaws.CloseError)
	if !ok {
		t.Fatalf("the connection ended with %v, want a WebSocket close error", closeErr)
	}
	if closeError.Code != gorillaws.CloseTryAgainLater {
		t.Fatalf("close code = %d, want %d (1013: reconnect from the last applied cursor)",
			closeError.Code, gorillaws.CloseTryAgainLater)
	}
	if !strings.Contains(closeError.Text, slowConsumerReason) {
		t.Fatalf("close reason = %q, want it to name a slow consumer (%q)", closeError.Text, slowConsumerReason)
	}
	if len(applied) == 0 {
		t.Fatal("the client applied no event before the close")
	}
	lastApplied := applied[len(applied)-1]
	if lastApplied >= total {
		t.Fatalf("the client applied every event (%d): the test did not exercise the tail", lastApplied)
	}
	// No frame was dropped on the way out: a server that shed the slow client
	// AFTER silently skipping a frame would show a hole here.
	for i, sequence := range applied {
		if want := int64(i + 1); sequence != want {
			t.Fatalf("the shed connection delivered %d at position %d, want %d: it dropped frames instead of closing",
				sequence, i, want)
		}
	}

	// Reconnect from the last applied cursor. The server replays everything above
	// it (the duplicates §27.4 allows reach the client as sequences it already
	// applied, which is why the check below is on the SET, not on the frames): the
	// frames it reads are a contiguous run starting at lastApplied+1, so the
	// connection breaks neither the order nor the "no hole" rule.
	reconnected := projectStream(t, srv, testProjectA)
	reconnected.subscribe(ResourceTypeProject, testProjectA, lastApplied, "sub_2")
	resumed := reconnected.drainReplay("sub_2", "project:"+testProjectA)
	if resumed.After != lastApplied {
		t.Fatalf("the reconnect resumed after %d, want the last applied cursor %d", resumed.After, lastApplied)
	}
	if resumed.HighWatermark != total {
		t.Fatalf("reconnected high_watermark = %d, want %d", resumed.HighWatermark, total)
	}
	if want := total - lastApplied; int64(len(resumed.Sequences)) != want {
		t.Fatalf("the reconnect delivered %d events, want %d (every sequence above the cursor)",
			len(resumed.Sequences), want)
	}
	if resumed.Sequences[0] != lastApplied+1 {
		t.Fatalf("the reconnect started at %d, want %d", resumed.Sequences[0], lastApplied+1)
	}
	// The client's set: what it applied before the shed, plus what it was replayed.
	// Together they are 1..total once each — a duplicate is allowed and does not
	// change the set; a hole would leave a sequence out.
	final := append(append([]int64(nil), applied...), resumed.Sequences...)
	if len(final) != total {
		t.Fatalf("the client holds %d sequences, want %d", len(final), total)
	}
	for i, sequence := range final {
		if want := int64(i + 1); sequence != want {
			t.Fatalf("the client's stream is not 1..%d once each: position %d is %d, want %d", total, i, sequence, want)
		}
	}

	// The reconnected socket is a working live subscription, not just a replay: a
	// delivered event reaches it.
	live := f.projectEvent(testProjectA)
	if err := NewHubDeliverer(srv.hub).Deliver(context.Background(), deliveryEvent(live)); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	reconnected.expectEventSequence("project:"+testProjectA, live.ProjectSeq)

	// 1013 is not a general-purpose close. A client that closes normally is not
	// answered with 1013, so "slow consumer" stays a statement about the server's
	// buffer and not about any drop at all.
	normal := projectStream(t, srv, testProjectA)
	normal.subscribe(ResourceTypeProject, testProjectA, live.ProjectSeq, "sub_3")
	normal.drainReplay("sub_3", "project:"+testProjectA)
	if err := normal.conn.WriteControl(
		gorillaws.CloseMessage,
		gorillaws.FormatCloseMessage(gorillaws.CloseNormalClosure, ""),
		time.Now().Add(5*time.Second),
	); err != nil {
		t.Fatalf("send a normal close: %v", err)
	}
	if err := normal.conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, _, err := normal.conn.ReadMessage(); err == nil {
		t.Fatal("the normal close was answered by a frame, want the close to end the connection")
	} else if gorillaws.IsCloseError(err, gorillaws.CloseTryAgainLater) {
		t.Fatalf("a normal close was answered with 1013: %v", err)
	}
}

// ---------------------------------------------------------------------------
// §28 T1.12.c: TestCursorExpiredSnapshot
// ---------------------------------------------------------------------------

// TestCursorExpiredSnapshot is §15's "gap 明确进入快照恢复流程" and §27.4's 410/422
// pair, end to end on one connection:
//
//   - a cursor below the retention floor is answered with cursor_expired carrying
//     the floor and the machine-readable recovery instruction ("snapshot"), and
//     the subscription ENDS — it is not continued from a cursor with a hole under
//     it, and nothing follows the frame on that scope;
//   - the boundary is exact: after = floor-1 is legal and replays the retained
//     history, so the 410 was a statement about that cursor and not a blanket
//     refusal (a server that expired everything after a trim would pass a weaker
//     test);
//   - the client resumes from the cursor a snapshot carries (the floor's own
//     position) and receives the rest, live;
//   - a cursor past the watermark is answered with invalid_cursor quoting the
//     watermark, and the frame hands over NO cursor: the server reports, the client
//     (T1.14) decides. Serving the watermark once the client chooses to adopt it
//     works normally.
func TestCursorExpiredSnapshot(t *testing.T) {
	f := newReplayFixture(t)
	// Retention has trimmed everything before 11: the scope counter is preset, so
	// the retained history starts at 11 and the floor is 11 (the technique
	// runstore's own replay tests use).
	presetScopeCounter(t, f.store, projectScopeName(testProjectA), 10)
	const retainedFrom = 11
	last := appendBigProjectEvents(t, f.store, testProjectA, 3, 16, 1700000002000)
	if last.ProjectSeq != 13 {
		t.Fatalf("retained history ended at %d, want 13", last.ProjectSeq)
	}
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: 5 * time.Millisecond,
	})
	conn := projectStream(t, srv, testProjectA)
	server := clientForSession(t, srv.hub, ProjectScopePrefix+testProjectA)

	// --- 410: the cursor points at history retention removed -------------------
	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_expired")
	subscribed := conn.expectFrame(MsgTypeSubscribed)
	assertJSONString(t, subscribed.Data, "client_request_id", "sub_expired")
	assertJSONString(t, subscribed.Data, "scope", "project:"+testProjectA)

	expired := conn.expectFrame(MsgTypeCursorExpired)
	assertJSONString(t, expired.Data, "client_request_id", "sub_expired")
	assertJSONString(t, expired.Data, "scope", "project:"+testProjectA)
	assertJSONString(t, expired.Data, "code", CodeCursorExpired)
	assertJSONString(t, expired.Data, "recovery", RecoverySnapshot)
	assertJSONNumber(t, expired.Data, "retention_floor", retainedFrom)
	// The frame carries the recovery instruction and the floor. What it must NOT
	// carry is a cursor: only the snapshot's owner may produce one.
	for _, key := range []string{"next_after", "high_watermark", "snapshot_url"} {
		if _, ok := expired.Data[key]; ok {
			t.Errorf("cursor_expired carries %q; §27.4 makes the client's snapshot the only source of a new cursor", key)
		}
	}

	// The subscription ends with the frame: the table entry is dropped by the
	// stopped goroutine's own defer, so an empty table proves the goroutine returned
	// and no live event can follow on a cursor with a hole under it.
	waitForSubscriptionCount(t, server, 0)

	// --- the exact boundary, from the legal side: after = floor-1 ---------------
	// The next frame this connection reads must be the NEXT subscription's
	// `subscribed`: a stray event or a second cursor frame for the ended one would
	// make this read fail. The retained history is exactly 11..13, so the 410 above
	// was about the cursor and not about the trim.
	conn.subscribe(ResourceTypeProject, testProjectA, retainedFrom-1, "sub_floor")
	atFloor := conn.drainReplay("sub_floor", "project:"+testProjectA)
	if atFloor.After != retainedFrom-1 {
		t.Fatalf("replay at the floor: after = %d, want %d", atFloor.After, retainedFrom-1)
	}
	if atFloor.RetentionFloor != retainedFrom {
		t.Fatalf("replay at the floor: retention_floor = %d, want %d", atFloor.RetentionFloor, retainedFrom)
	}
	if got := atFloor.Sequences; len(got) != 3 || got[0] != 11 || got[1] != 12 || got[2] != 13 {
		t.Fatalf("replay at the floor delivered %v, want [11 12 13]", got)
	}

	// --- the snapshot recovery: resume from the cursor the snapshot carries -----
	// A client that replaced its cache with a snapshot taken at the trim point
	// resumes from the floor.
	conn.subscribe(ResourceTypeProject, testProjectA, retainedFrom, "sub_snapshot")
	resumed := conn.drainReplay("sub_snapshot", "project:"+testProjectA)
	if resumed.After != retainedFrom || resumed.RetentionFloor != retainedFrom {
		t.Fatalf("snapshot resume = after %d floor %d, want both %d", resumed.After, resumed.RetentionFloor, retainedFrom)
	}
	if got := resumed.Sequences; len(got) != 2 || got[0] != 12 || got[1] != 13 {
		t.Fatalf("snapshot resume delivered %v, want [12 13]", got)
	}
	// It is a live subscription afterwards, not a replay that ended the scope.
	live := f.projectEvent(testProjectA)
	conn.expectEventSequence("project:"+testProjectA, live.ProjectSeq)
	if live.ProjectSeq != 14 {
		t.Fatalf("the live event is sequence %d, want 14", live.ProjectSeq)
	}

	// --- 422: the cursor names a sequence this scope never allocated ------------
	conn.subscribe(ResourceTypeProject, testProjectA, live.ProjectSeq+7, "sub_invalid")
	assertJSONString(t, conn.expectFrame(MsgTypeSubscribed).Data, "client_request_id", "sub_invalid")

	invalid := conn.expectFrame(MsgTypeInvalidCursor)
	assertJSONString(t, invalid.Data, "client_request_id", "sub_invalid")
	assertJSONString(t, invalid.Data, "scope", "project:"+testProjectA)
	assertJSONString(t, invalid.Data, "code", CodeInvalidCursor)
	assertJSONNumber(t, invalid.Data, "high_watermark", live.ProjectSeq)
	for _, key := range []string{"next_after", "recovery"} {
		if _, ok := invalid.Data[key]; ok {
			t.Errorf("invalid_cursor carries %q; the watermark is a report, not a cursor to adopt", key)
		}
	}
	waitForSubscriptionCount(t, server, 0)

	// The client adopts the watermark it was quoted (its own decision, T1.14's),
	// and the server serves that cursor normally: an empty replay at H, then live.
	conn.subscribe(ResourceTypeProject, testProjectA, live.ProjectSeq, "sub_adopted")
	adopted := conn.drainReplay("sub_adopted", "project:"+testProjectA)
	if adopted.After != live.ProjectSeq || len(adopted.Sequences) != 0 {
		t.Fatalf("adopting the watermark = after %d with %d events, want after %d and none",
			adopted.After, len(adopted.Sequences), live.ProjectSeq)
	}
	if adopted.HighWatermark != live.ProjectSeq {
		t.Fatalf("adopted high_watermark = %d, want %d", adopted.HighWatermark, live.ProjectSeq)
	}
	next := f.projectEvent(testProjectA)
	conn.expectEventSequence("project:"+testProjectA, next.ProjectSeq)
}
