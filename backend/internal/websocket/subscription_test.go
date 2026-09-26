package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/api/middleware"
	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
	"github.com/gin-gonic/gin"
	gorillaws "github.com/gorilla/websocket"
)

// These tests cover subscription.go and the dispatch hub.go added for it: the
// ordered subscription as a whole, over a real event store, a real HTTP server and
// a real WebSocket client. The properties they exist to prove are the ones
// §20.3/§27.4 make load-bearing:
//
//   - the frame order of one subscription is exactly
//     subscribed → replay_started → 0..N events ≤ H → replay_finished → live > H,
//     and no live event ever carries a sequence at or below the replayed ones;
//   - a client that misses frames while the replay runs still receives a
//     contiguous sequence: the live phase reads the store by cursor, so a
//     delivery that is late or duplicated cannot create a hole;
//   - live events arrive both when a delivery wakes the subscription and when
//     nothing does (the poll interval is the guarantee, the wake-up is the
//     latency optimization);
//   - authorization is the connection's project: another project's resource and a
//     resource that does not exist are the same forbidden frame, and a connection
//     that is not a project stream can subscribe nothing at all;
//   - the two cursor failures end the subscription with the frame §27.4 names
//     (410 cursor_expired with retention_floor and recovery=snapshot, 422
//     invalid_cursor with high_watermark);
//   - a client that stops reading is closed with 1013 rather than served a gap,
//     and reconnecting from its last applied cursor loses nothing;
//   - subscriptions do not outlive their connection, and the legacy topic
//     protocol still works on the same connection.

// ---------------------------------------------------------------------------
// Fixtures: a real store, a real server, a real WebSocket client
// ---------------------------------------------------------------------------

// subscriptionServer is one httptest server over a hub with a replay backend.
type subscriptionServer struct {
	*httptest.Server
	hub *Hub
}

// newSubscriptionServer starts a server whose project stream route attaches
// connections to a hub carrying deps, plus a conversation stream route (a scope
// that is not a project) for the "not a project stream" rule.
func newSubscriptionServer(t *testing.T, deps ReplayDependencies) *subscriptionServer {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hub := NewHub()
	hub.SetReplayDependencies(deps)
	go hub.Run()

	policy := NewAccessPolicy(wsTestToken, []string{wsTestOrigin})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(ContextAccessPolicy, policy)
		c.Next()
	})
	router.GET("/projects/:id/stream", middleware.RequireAccessToken(wsTestToken), func(c *gin.Context) {
		// The route owns both protocols: the project stream scope and the legacy
		// topic of that same project (security_test.go registers its topics the
		// same way).
		HandleScopedWebSocket(hub, c, ProjectScopePrefix+c.Param("id"), "flow:project:"+c.Param("id"))
	})
	router.GET("/conversations/:sessionId/stream", middleware.RequireAccessToken(wsTestToken), func(c *gin.Context) {
		HandleScopedWebSocket(hub, c, c.Param("sessionId"))
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return &subscriptionServer{Server: server, hub: hub}
}

// frameReadTimeout bounds every frame read: a frame that never arrives is a
// failure with a message, never a hang.
const frameReadTimeout = 10 * time.Second

// wsTestConn is one test client of a subscriptionServer. It uses the browser
// authentication the workbench uses (the token in the subprotocol, an exact
// Origin) — the same way security_test.go connects.
type wsTestConn struct {
	t    *testing.T
	conn *gorillaws.Conn
}

// dialStream connects to path with the browser credentials.
func dialStream(t *testing.T, srv *subscriptionServer, path string, tune func(*gorillaws.Dialer)) *wsTestConn {
	t.Helper()
	dialer := *gorillaws.DefaultDialer
	dialer.Subprotocols = []string{middleware.WebSocketProtocolV1, "codeflow.token." + wsTestToken}
	if tune != nil {
		tune(&dialer)
	}
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + path
	conn, response, err := dialer.Dial(url, http.Header{"Origin": []string{wsTestOrigin}})
	if err != nil {
		t.Fatalf("dial %s: status=%v err=%v", path, statusCode(response), err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &wsTestConn{t: t, conn: conn}
}

// projectStream connects to projectID's stream.
func projectStream(t *testing.T, srv *subscriptionServer, projectID string) *wsTestConn {
	t.Helper()
	return dialStream(t, srv, "/projects/"+projectID+"/stream", nil)
}

// conversationStream connects to a conversation stream: a connection whose scope
// is a random session id rather than "project:<id>".
func conversationStream(t *testing.T, srv *subscriptionServer, sessionID string) *wsTestConn {
	t.Helper()
	return dialStream(t, srv, "/conversations/"+sessionID+"/stream", nil)
}

// sendJSON writes one client frame.
func (c *wsTestConn) sendJSON(value any) {
	c.t.Helper()
	if err := c.conn.WriteJSON(value); err != nil {
		c.t.Fatalf("write frame: %v", err)
	}
}

// sendRawFrame writes one client frame as raw bytes, so a test can send a frame
// the typed helpers would not build.
func (c *wsTestConn) sendRawFrame(raw string) {
	c.t.Helper()
	if err := c.conn.WriteMessage(gorillaws.TextMessage, []byte(raw)); err != nil {
		c.t.Fatalf("write raw frame: %v", err)
	}
}

// subscribe sends one §20.3 subscribe frame with every required field.
func (c *wsTestConn) subscribe(resourceType, resourceID string, after int64, clientRequestID string) {
	c.t.Helper()
	c.sendJSON(map[string]any{"type": "subscribe", "data": map[string]any{
		"resource_type":     resourceType,
		"resource_id":       resourceID,
		"after":             after,
		"client_request_id": clientRequestID,
	}})
}

// unsubscribe sends one new-protocol unsubscribe frame.
func (c *wsTestConn) unsubscribe(resourceType, resourceID, clientRequestID string) {
	c.t.Helper()
	c.sendJSON(map[string]any{"type": "unsubscribe", "data": map[string]any{
		"resource_type":     resourceType,
		"resource_id":       resourceID,
		"client_request_id": clientRequestID,
	}})
}

// subscribeTopic sends a legacy topic subscribe frame (the pre-T1.12 protocol).
func (c *wsTestConn) subscribeTopic(topic string) {
	c.t.Helper()
	c.sendJSON(map[string]any{"type": "subscribe", "data": map[string]any{"topic": topic}})
}

// readRaw reads one frame and returns its bytes with the decoded shape.
func (c *wsTestConn) readRaw() ([]byte, frameShape) {
	c.t.Helper()
	if err := c.conn.SetReadDeadline(time.Now().Add(frameReadTimeout)); err != nil {
		c.t.Fatalf("set read deadline: %v", err)
	}
	_, data, err := c.conn.ReadMessage()
	if err != nil {
		c.t.Fatalf("read frame: %v", err)
	}
	return data, decodeFrame(c.t, data)
}

// readRawMessage reads one frame as raw bytes, for frames that are not §20.3
// server frames (the legacy topic broadcast).
func (c *wsTestConn) readRawMessage() []byte {
	c.t.Helper()
	if err := c.conn.SetReadDeadline(time.Now().Add(frameReadTimeout)); err != nil {
		c.t.Fatalf("set read deadline: %v", err)
	}
	_, data, err := c.conn.ReadMessage()
	if err != nil {
		c.t.Fatalf("read frame: %v", err)
	}
	return data
}

// expectFrame reads the next frame and requires its type.
func (c *wsTestConn) expectFrame(want MessageType) frameShape {
	c.t.Helper()
	_, shape := c.readRaw()
	if shape.Type != want {
		c.t.Fatalf("frame type = %q, want %q (data %v)", shape.Type, want, shape.Data)
	}
	return shape
}

// replayOutcome is what one subscription's replay delivered.
type replayOutcome struct {
	After          int64
	HighWatermark  int64
	RetentionFloor int64
	Sequences      []int64
}

// drainReplay reads one subscription's replay window and checks §20.3's order:
// subscribed, replay_started, a contiguous run of events from after+1 up to at
// most the high watermark, replay_finished quoting the same watermark and the
// cursor the client must reconnect with.
func (c *wsTestConn) drainReplay(clientRequestID, wantScope string) replayOutcome {
	c.t.Helper()
	subscribed := c.expectFrame(MsgTypeSubscribed)
	assertJSONString(c.t, subscribed.Data, "client_request_id", clientRequestID)
	assertJSONString(c.t, subscribed.Data, "scope", wantScope)

	started := c.expectFrame(MsgTypeReplayStarted)
	assertJSONString(c.t, started.Data, "client_request_id", clientRequestID)
	assertJSONString(c.t, started.Data, "scope", wantScope)
	after := jsonNumber(c.t, started.Data, "after")
	highWatermark := jsonNumber(c.t, started.Data, "high_watermark")
	out := replayOutcome{
		After:          after,
		HighWatermark:  highWatermark,
		RetentionFloor: jsonNumber(c.t, started.Data, "retention_floor"),
	}

	cursor := after
	for {
		_, shape := c.readRaw()
		switch shape.Type {
		case MsgTypeEvent:
			assertJSONString(c.t, shape.Data, "scope", wantScope)
			sequence := frameSequence(c.t, shape)
			if sequence != cursor+1 {
				c.t.Fatalf("replay delivered sequence %d after %d: the replay window must be contiguous", sequence, cursor)
			}
			if sequence > highWatermark {
				c.t.Fatalf("replay delivered sequence %d above the high watermark %d", sequence, highWatermark)
			}
			cursor = sequence
			out.Sequences = append(out.Sequences, sequence)
		case MsgTypeReplayFinished:
			assertJSONString(c.t, shape.Data, "client_request_id", clientRequestID)
			assertJSONNumber(c.t, shape.Data, "high_watermark", highWatermark)
			assertJSONNumber(c.t, shape.Data, "next_after", cursor)
			return out
		default:
			c.t.Fatalf("frame %q inside the replay window, want an event or replay_finished", shape.Type)
		}
	}
}

// expectEventSequence reads the next frame and requires it to be an event with
// the given sequence on the given scope.
func (c *wsTestConn) expectEventSequence(wantScope string, wantSequence int64) frameShape {
	c.t.Helper()
	shape := c.expectFrame(MsgTypeEvent)
	assertJSONString(c.t, shape.Data, "scope", wantScope)
	if got := frameSequence(c.t, shape); got != wantSequence {
		c.t.Fatalf("event sequence = %d, want %d", got, wantSequence)
	}
	return shape
}

// jsonNumber returns data[key] as an integer, failing if it is not one.
func jsonNumber(t *testing.T, data map[string]json.RawMessage, key string) int64 {
	t.Helper()
	raw, ok := data[key]
	if !ok {
		t.Fatalf("data has no %q key (%v)", key, data)
	}
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("data[%q] = %s, which is not an integer", key, raw)
	}
	return value
}

// frameSequence returns an event frame's sequence.
func frameSequence(t *testing.T, shape frameShape) int64 {
	t.Helper()
	return jsonNumber(t, shape.Data, "sequence")
}

// frameScope returns an event frame's scope.
func frameScope(t *testing.T, shape frameShape) string {
	t.Helper()
	var scope string
	if err := json.Unmarshal(shape.Data["scope"], &scope); err != nil {
		t.Fatalf("frame has no scope: %v (%v)", err, shape.Data)
	}
	return scope
}

// clientForSession finds the server-side client of the connection whose scope is
// sessionID. The hub keeps one client per connection, and these tests use one
// connection per scope.
func clientForSession(t *testing.T, hub *Hub, sessionID string) *Client {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		for _, client := range hub.clients {
			if client.SessionID == sessionID {
				hub.mu.RUnlock()
				return client
			}
		}
		hub.mu.RUnlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no client with session %q is registered", sessionID)
	return nil
}

// waitForSubscriptionCount waits until the connection holds want subscriptions.
func waitForSubscriptionCount(t *testing.T, client *Client, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if client.SubscriptionCount() == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("connection holds %d subscriptions, want %d (%+v)", client.SubscriptionCount(), want, client.SubscriptionSnapshots())
}

// waitForClientCount waits until the hub has want registered clients.
func waitForClientCount(t *testing.T, hub *Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if hub.GetTotalClientCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("hub has %d clients, want %d", hub.GetTotalClientCount(), want)
}

// subscriptionPointers copies the connection's subscription table, so a test can
// watch the goroutines it started.
func subscriptionPointers(t *testing.T, client *Client) []*subscription {
	t.Helper()
	client.subMu.Lock()
	defer client.subMu.Unlock()
	out := make([]*subscription, 0, len(client.subs))
	for _, sub := range client.subs {
		out = append(out, sub)
	}
	if len(out) == 0 {
		t.Fatal("the connection holds no subscriptions")
	}
	return out
}

// appendBigProjectEvents appends count project-level events in ONE transaction,
// each with a payload of about payloadBytes, and returns the last event. The
// payload is what lets a slow client fill the server's buffer after a few dozen
// frames rather than a few hundred thousand.
func appendBigProjectEvents(t *testing.T, store *runstore.Store, projectID string, count, payloadBytes int, atMillis int64) runstore.Event {
	t.Helper()
	identity, err := json.Marshal(map[string]any{
		"project_id": projectID,
		"actor":      map[string]any{"type": "system", "id": "worker-1"},
	})
	if err != nil {
		t.Fatalf("marshal identity: %v", err)
	}
	payload, err := json.Marshal(map[string]any{"pad": strings.Repeat("x", payloadBytes)})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	var last runstore.Event
	err = store.WithTx(context.Background(), func(ctx context.Context, tx runstore.Tx) error {
		for i := 0; i < count; i++ {
			event, err := runstore.AppendEventTx(ctx, tx, runstore.EventInput{
				ProjectID:  projectID,
				Type:       string(run.EventServerRestart),
				OccurredAt: time.UnixMilli(atMillis + int64(i)),
				Identity:   identity,
				Payload:    payload,
			})
			if err != nil {
				return err
			}
			last = event
		}
		return nil
	})
	if err != nil {
		t.Fatalf("append %d events to %s: %v", count, projectID, err)
	}
	return last
}

// ---------------------------------------------------------------------------
// The frame order and the two live paths
// ---------------------------------------------------------------------------

// TestSubscriptionReplaysToHighWatermarkThenDeliversLiveEvents is §20.3's order
// end to end: replay up to the watermark read at subscribe time, then live events
// above it, delivered here by a wake-up (the poll interval is an hour, so nothing
// but the delivery can have delivered them).
func TestSubscriptionReplaysToHighWatermarkThenDeliversLiveEvents(t *testing.T) {
	f := newReplayFixture(t)
	for i := 0; i < 3; i++ {
		f.projectEvent(testProjectA)
	}
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: time.Hour,
		PageLimit:    2,
	})
	conn := projectStream(t, srv, testProjectA)
	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")

	out := conn.drainReplay("sub_1", "project:"+testProjectA)
	if out.HighWatermark != 3 || out.RetentionFloor != 1 {
		t.Fatalf("replay boundaries = hwm %d floor %d, want 3/1", out.HighWatermark, out.RetentionFloor)
	}
	if len(out.Sequences) != 3 {
		t.Fatalf("replay delivered %d events, want 3", len(out.Sequences))
	}

	// The live event is appended after replay_finished, so it is strictly above
	// the replayed window — and it arrives because the deliverer woke the
	// subscription, not because the poll interval elapsed.
	live := f.projectEvent(testProjectA)
	deliverer := NewHubDeliverer(srv.hub)
	if err := deliverer.Deliver(context.Background(), runstore.Delivery{
		OutboxID: "outbox-1",
		EventID:  live.ID,
		Event:    live,
	}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	shape := conn.expectEventSequence("project:"+testProjectA, live.ProjectSeq)
	assertJSONString(t, shape.Data, "id", live.ID)
}

// TestSubscriptionDeliversLiveEventsByPollingAlone covers the other half of
// §27.4's "缓冲或从 event store 后续读取": nothing wakes the subscription — no
// dispatcher is registered in this process yet — and the poll interval still
// delivers every event.
func TestSubscriptionDeliversLiveEventsByPollingAlone(t *testing.T) {
	f := newReplayFixture(t)
	f.projectEvent(testProjectA)
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: 5 * time.Millisecond,
	})
	conn := projectStream(t, srv, testProjectA)
	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")
	out := conn.drainReplay("sub_1", "project:"+testProjectA)
	if out.HighWatermark != 1 {
		t.Fatalf("high watermark = %d, want 1", out.HighWatermark)
	}

	first := f.projectEvent(testProjectA)
	second := f.projectEvent(testProjectA)
	conn.expectEventSequence("project:"+testProjectA, first.ProjectSeq)
	conn.expectEventSequence("project:"+testProjectA, second.ProjectSeq)
}

// TestSubscriptionReplayLiveBoundaryHasNoGap is the T1.12 acceptance assertion
// (§15: "断线期间事件全可回放"; §27.4: 重复允许、静默缺口不允许) under the
// condition that makes it hard: events are appended while the replay is running,
// so the boundary between "replayed" and "live" falls inside the stream.
//
// The appends are driven by the replay reads themselves (appendOnPage): the
// snapshot that fixes H is taken, an event is appended, and only then does the
// subscription read its next page — so the event above H is guaranteed to exist
// while the replay is still running, rather than depending on a goroutine winning
// a race.
//
// The client reads frames until the last appended event arrives and checks that
// the sequences it saw from after+1 to that event are contiguous — the replay's
// closed window and the live phase's open one meet exactly, with no hole and no
// duplicate.
func TestSubscriptionReplayLiveBoundaryHasNoGap(t *testing.T) {
	f := newReplayFixture(t)
	for i := 0; i < 5; i++ {
		f.projectEvent(testProjectA)
	}
	// One append after the first read (the snapshot that fixes H) and one after the
	// second page: the first event is above H while the replay runs, the second is
	// appended after the replay already passed its window.
	reader := &appendOnPage{
		EventReader: f.reader,
		appendAt:    map[int]int{1: 1, 2: 1},
		append:      func() runstore.Event { return f.projectEvent(testProjectA) },
	}
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       reader,
		Runs:         f.auth.Runs,
		PollInterval: 5 * time.Millisecond,
		PageLimit:    2,
	})
	conn := projectStream(t, srv, testProjectA)
	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")

	subscribed := conn.expectFrame(MsgTypeSubscribed)
	assertJSONString(t, subscribed.Data, "client_request_id", "sub_1")
	started := conn.expectFrame(MsgTypeReplayStarted)
	highWatermark := jsonNumber(t, started.Data, "high_watermark")

	// Wait for the appends to have happened, then read the tail of the replay and
	// the live events.
	deadline := time.Now().Add(5 * time.Second)
	for len(reader.appended()) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the appends did not happen: the replay did not read as expected")
		}
		time.Sleep(time.Millisecond)
	}
	newest := reader.appended()[len(reader.appended())-1]

	var (
		sequences []int64
		cursor    int64
		finished  bool
	)
	for {
		_, shape := conn.readRaw()
		switch shape.Type {
		case MsgTypeEvent:
			sequence := frameSequence(t, shape)
			if sequence != cursor+1 {
				t.Fatalf("received sequence %d after %d: a gap or a duplicate reached the client", sequence, cursor)
			}
			cursor = sequence
			sequences = append(sequences, sequence)
			if finished && sequence <= highWatermark {
				t.Fatalf("live event sequence %d is not above the high watermark %d", sequence, highWatermark)
			}
		case MsgTypeReplayFinished:
			if jsonNumber(t, shape.Data, "high_watermark") != highWatermark {
				t.Fatal("replay_finished quoted a different watermark than replay_started")
			}
			// The replay window is closed at H on both sides: it carried every event
			// up to the watermark and nothing above it. An event appended while the
			// replay ran belongs to the live phase even when the store hands it to a
			// replay read, because H was fixed by the snapshot the replay started
			// from.
			for _, sequence := range sequences {
				if sequence > highWatermark {
					t.Fatalf("the replay carried sequence %d above its high watermark %d", sequence, highWatermark)
				}
			}
			if sequences[len(sequences)-1] != highWatermark {
				t.Fatalf("the replay ended at sequence %d, want the high watermark %d",
					sequences[len(sequences)-1], highWatermark)
			}
			finished = true
		default:
			t.Fatalf("unexpected frame %q", shape.Type)
		}
		if cursor >= newest.ProjectSeq {
			break
		}
	}
	if len(sequences) != int(newest.ProjectSeq) {
		t.Fatalf("received %d events, want %d", len(sequences), newest.ProjectSeq)
	}
	if !finished {
		t.Fatal("replay_finished never arrived")
	}
	if highWatermark >= newest.ProjectSeq {
		t.Fatalf("the high watermark %d already covered the last event %d: the test did not cross the boundary",
			highWatermark, newest.ProjectSeq)
	}
	if highWatermark != 5 {
		t.Fatalf("the high watermark = %d, want the 5 events that existed when the replay read its snapshot", highWatermark)
	}
}

// appendOnPage wraps an EventReader and appends events after chosen read counts:
// appendAt[n] appends that many events after the n-th read. It makes "an event was
// appended while the replay was running" a fact of the test rather than a race
// between a goroutine and the server.
type appendOnPage struct {
	EventReader
	appendAt map[int]int
	append   func() runstore.Event

	mu     sync.Mutex
	reads  int
	events []runstore.Event
}

// ReplayProject implements EventReader, appending after the read it just served.
func (r *appendOnPage) ReplayProject(ctx context.Context, projectID string, after int64, limit int) (runstore.ReplayPage, error) {
	page, err := r.EventReader.ReplayProject(ctx, projectID, after, limit)
	r.afterRead()
	return page, err
}

// ReplayRun implements EventReader.
func (r *appendOnPage) ReplayRun(ctx context.Context, runID string, after int64, limit int) (runstore.ReplayPage, error) {
	page, err := r.EventReader.ReplayRun(ctx, runID, after, limit)
	r.afterRead()
	return page, err
}

// afterRead appends whatever this read count asks for. It runs on the
// subscription's goroutine, between two of its reads, so the append is ordered
// with the replay rather than with the test.
func (r *appendOnPage) afterRead() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	for i := 0; i < r.appendAt[r.reads]; i++ {
		r.events = append(r.events, r.append())
	}
}

// appended returns the events appended so far.
func (r *appendOnPage) appended() []runstore.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]runstore.Event(nil), r.events...)
}

// ---------------------------------------------------------------------------
// Two scopes, two cursors, and unsubscribe
// ---------------------------------------------------------------------------

// TestSubscriptionScopesHaveIndependentCursorsAndUnsubscribe runs a project and a
// run subscription on ONE connection: each pages on its own counter, neither
// receives the other's events, and unsubscribing one leaves the other running.
func TestSubscriptionScopesHaveIndependentCursorsAndUnsubscribe(t *testing.T) {
	f := newReplayFixture(t)
	f.projectEvent(testProjectA)       // project_seq 1
	f.runEvent(testProjectA, testRunA) // project_seq 2, run_seq 1
	f.projectEvent(testProjectA)       // project_seq 3
	f.runEvent(testProjectA, testRunA) // project_seq 4, run_seq 2
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: 5 * time.Millisecond,
	})
	conn := projectStream(t, srv, testProjectA)
	server := clientForSession(t, srv.hub, ProjectScopePrefix+testProjectA)

	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_project")
	projectReplay := conn.drainReplay("sub_project", "project:"+testProjectA)
	if projectReplay.HighWatermark != 4 || len(projectReplay.Sequences) != 4 {
		t.Fatalf("project replay = hwm %d, %d events, want 4/4", projectReplay.HighWatermark, len(projectReplay.Sequences))
	}

	conn.subscribe(ResourceTypeRun, testRunA, 0, "sub_run")
	runReplay := conn.drainReplay("sub_run", "run:"+testRunA)
	if runReplay.HighWatermark != 2 || len(runReplay.Sequences) != 2 {
		t.Fatalf("run replay = hwm %d, %d events, want 2/2", runReplay.HighWatermark, len(runReplay.Sequences))
	}

	// Both subscriptions are live, each with its own cursor.
	waitForSubscriptionCount(t, server, 2)
	snapshots := server.SubscriptionSnapshots()
	if len(snapshots) != 2 || snapshots[0].Scope != "project:"+testProjectA || snapshots[1].Scope != "run:"+testRunA {
		t.Fatalf("subscription table = %+v, want the project and the run scope", snapshots)
	}
	for _, snapshot := range snapshots {
		if snapshot.State != SubscriptionLive {
			t.Errorf("subscription %s state = %q, want %q", snapshot.Scope, snapshot.State, SubscriptionLive)
		}
	}

	// A project-only event and a run event: the run subscription must see only the
	// run event, with its own sequence. Different subscriptions interleave freely
	// (each one is ordered, the connection is not), so the test collects frames by
	// scope and checks each scope's own run.
	f.projectEvent(testProjectA)       // project_seq 5, no run
	f.runEvent(testProjectA, testRunA) // project_seq 6, run_seq 3

	perScope := collectUntil(t, conn, map[string]int64{
		"project:" + testProjectA: 6,
		"run:" + testRunA:         3,
	})
	assertContiguousFrom(t, perScope["project:"+testProjectA], 5)
	assertContiguousFrom(t, perScope["run:"+testRunA], 3)

	// Unsubscribe the run scope: its table entry disappears (which happens only
	// after its goroutine returned) and the project subscription keeps working.
	conn.unsubscribe(ResourceTypeRun, testRunA, "sub_run_2")
	waitForSubscriptionCount(t, server, 1)
	if got := server.SubscriptionSnapshots()[0].Scope; got != "project:"+testProjectA {
		t.Fatalf("remaining subscription = %q, want the project scope", got)
	}

	f.runEvent(testProjectA, testRunA) // project_seq 7, run_seq 4
	conn.expectEventSequence("project:"+testProjectA, 7)
}

// collectUntil reads event frames until every scope in want has delivered the
// sequence it is waiting for, and returns each scope's sequences in arrival order.
// It fails if any scope arrives out of order or if another frame type turns up.
func collectUntil(t *testing.T, c *wsTestConn, want map[string]int64) map[string][]int64 {
	t.Helper()
	out := make(map[string][]int64, len(want))
	remaining := make(map[string]int64, len(want))
	for scope, sequence := range want {
		remaining[scope] = sequence
	}
	for len(remaining) > 0 {
		_, shape := c.readRaw()
		if shape.Type != MsgTypeEvent {
			t.Fatalf("frame %q while waiting for events", shape.Type)
		}
		scope := frameScope(t, shape)
		sequence := frameSequence(t, shape)
		expected, ok := remaining[scope]
		if !ok {
			t.Fatalf("unexpected event %d on scope %s", sequence, scope)
		}
		out[scope] = append(out[scope], sequence)
		if sequence > expected {
			t.Fatalf("scope %s delivered %d, past the %d it was read for", scope, sequence, expected)
		}
		if sequence == expected {
			delete(remaining, scope)
		}
	}
	return out
}

// assertContiguousFrom fails unless sequences are start, start+1, … with nothing
// missing and nothing repeated.
func assertContiguousFrom(t *testing.T, sequences []int64, start int64) {
	t.Helper()
	if len(sequences) == 0 {
		t.Fatalf("no events from %d", start)
	}
	for i, sequence := range sequences {
		if sequence != start+int64(i) {
			t.Fatalf("sequences %v are not contiguous from %d", sequences, start)
		}
	}
}

// TestSubscriptionDuplicateSubscribeReplacesInPlace pins the answer to §27.4's
// reconnect path on a live socket: subscribing twice to the same scope stops the
// old subscription (the test waits for its goroutine to be gone) and starts the
// new one from the cursor the client names — one subscription per scope, no
// duplicate stream of the same event.
func TestSubscriptionDuplicateSubscribeReplacesInPlace(t *testing.T) {
	f := newReplayFixture(t)
	for i := 0; i < 3; i++ {
		f.projectEvent(testProjectA)
	}
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: time.Hour,
	})
	conn := projectStream(t, srv, testProjectA)
	server := clientForSession(t, srv.hub, ProjectScopePrefix+testProjectA)

	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")
	first := conn.drainReplay("sub_1", "project:"+testProjectA)
	if first.HighWatermark != 3 || len(first.Sequences) != 3 {
		t.Fatalf("first replay = hwm %d, %d events, want 3/3", first.HighWatermark, len(first.Sequences))
	}
	waitForSubscriptionCount(t, server, 1)
	old := subscriptionPointers(t, server)[0]

	// Reconnect on the same socket from cursor 1: the old subscription must be gone
	// (its goroutine finished before the new one sent anything) and the new one
	// replays 2 and 3 only.
	conn.subscribe(ResourceTypeProject, testProjectA, 1, "sub_2")
	second := conn.drainReplay("sub_2", "project:"+testProjectA)
	if second.After != 1 {
		t.Fatalf("replay_started after = %d, want the 1 the client asked for", second.After)
	}
	if len(second.Sequences) != 2 || second.Sequences[0] != 2 || second.Sequences[1] != 3 {
		t.Fatalf("second replay delivered %v, want [2 3]", second.Sequences)
	}
	select {
	case <-old.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the replaced subscription is still running")
	}

	waitForSubscriptionCount(t, server, 1)
	snapshots := server.SubscriptionSnapshots()
	if len(snapshots) != 1 || snapshots[0].ClientRequestID != "sub_2" || snapshots[0].Cursor != 3 {
		t.Fatalf("subscription table = %+v, want the sub_2 subscription at cursor 3", snapshots)
	}

	// The replacement is live: a delivered event reaches it.
	live := f.projectEvent(testProjectA)
	if err := NewHubDeliverer(srv.hub).Deliver(context.Background(), runstore.Delivery{EventID: live.ID, Event: live}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	conn.expectEventSequence("project:"+testProjectA, live.ProjectSeq)
}

// TestSubscriptionStaysSilentWhenStoppedMidRead pins that an unsubscribe is not
// answered by the subscription it stopped. The reader holds a live-phase read open
// until the test releases it, so the cancel lands while the read is in flight and
// the read comes back as context.Canceled; the stopped subscription must send
// nothing at all — answering "unavailable, retry" to a client that deliberately
// left the subscription would be a lie, and on a shared connection it would look
// exactly like a backend failure.
func TestSubscriptionStaysSilentWhenStoppedMidRead(t *testing.T) {
	f := newReplayFixture(t)
	f.projectEvent(testProjectA)
	reader := &blockingReader{EventReader: f.reader, entered: make(chan struct{}), gate: make(chan struct{})}
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       reader,
		Runs:         f.auth.Runs,
		PollInterval: 5 * time.Millisecond,
	})
	conn := projectStream(t, srv, testProjectA)
	server := clientForSession(t, srv.hub, ProjectScopePrefix+testProjectA)

	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")
	out := conn.drainReplay("sub_1", "project:"+testProjectA)
	if out.HighWatermark != 1 {
		t.Fatalf("high watermark = %d, want 1", out.HighWatermark)
	}

	// The live phase reaches the blocked read. The unsubscribe then cancels the
	// subscription while that read is still in flight.
	select {
	case <-reader.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the subscription never entered the blocked read")
	}
	conn.unsubscribe(ResourceTypeProject, testProjectA, "sub_1")
	close(reader.gate)

	// The table entry is dropped after the stopped subscription's goroutine has
	// finished enqueueing whatever it was going to enqueue, so the next frame on
	// this connection must be the `subscribed` of a NEW subscription: had the
	// stopped one answered its cancelled read, that frame would be here instead.
	waitForSubscriptionCount(t, server, 0)
	conn.subscribe(ResourceTypeProject, testProjectA, 1, "sub_2")
	after := conn.drainReplay("sub_2", "project:"+testProjectA)
	if after.After != 1 || len(after.Sequences) != 0 {
		t.Fatalf("subscription after the unsubscribe = after %d with %d events, want after 1 and none",
			after.After, len(after.Sequences))
	}
}

// blockingReader holds the read after the first one open until the test releases
// it, and reports the read's own context error afterwards. It makes "the
// subscription was cancelled while a read was in flight" a fact of the test rather
// than a race.
type blockingReader struct {
	EventReader
	entered chan struct{}
	gate    chan struct{}

	mu    sync.Mutex
	reads int
}

// ReplayProject implements EventReader.
func (r *blockingReader) ReplayProject(ctx context.Context, projectID string, after int64, limit int) (runstore.ReplayPage, error) {
	r.mu.Lock()
	r.reads++
	blocked := r.reads > 1
	r.mu.Unlock()
	if blocked {
		select {
		case <-r.entered:
		default:
			close(r.entered)
		}
		<-r.gate
		if err := ctx.Err(); err != nil {
			return runstore.ReplayPage{}, err
		}
	}
	return r.EventReader.ReplayProject(ctx, projectID, after, limit)
}

// ---------------------------------------------------------------------------
// Authorization
// ---------------------------------------------------------------------------

// TestSubscriptionAuthorizationIsBoundToTheConnectionProject is T1.12's first
// acceptance assertion on the wire: a connection to project A cannot subscribe
// project B, and cannot tell another project's run from a run that does not
// exist — the two answers are the same bytes.
func TestSubscriptionAuthorizationIsBoundToTheConnectionProject(t *testing.T) {
	f := newReplayFixture(t)
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: time.Hour,
	})
	conn := projectStream(t, srv, testProjectA)
	server := clientForSession(t, srv.hub, ProjectScopePrefix+testProjectA)

	conn.subscribe(ResourceTypeProject, testProjectB, 0, "sub_1")
	otherProjectBytes, otherProject := conn.readRaw()
	if otherProject.Type != MsgTypeForbidden {
		t.Fatalf("subscribing another project answered %q, want forbidden", otherProject.Type)
	}
	assertJSONString(t, otherProject.Data, "code", CodeForbidden)
	assertJSONString(t, otherProject.Data, "client_request_id", "sub_1")

	conn.subscribe(ResourceTypeRun, testRunNone, 0, "sub_1")
	missingRunBytes, missingRun := conn.readRaw()
	if missingRun.Type != MsgTypeForbidden {
		t.Fatalf("subscribing a missing run answered %q, want forbidden", missingRun.Type)
	}
	if !bytes.Equal(otherProjectBytes, missingRunBytes) {
		t.Fatalf("another project's run and a missing run answered differently:\n  %s\n  %s", otherProjectBytes, missingRunBytes)
	}
	for _, id := range []string{testProjectB, testRunB, testRunNone} {
		if bytes.Contains(otherProjectBytes, []byte(id)) {
			t.Errorf("forbidden frame leaks %s: %s", id, otherProjectBytes)
		}
	}

	// The connection's own run is granted on the same connection, so the refusals
	// above are about the resource and not about the connection being unusable.
	conn.subscribe(ResourceTypeRun, testRunA, 0, "sub_2")
	conn.drainReplay("sub_2", "run:"+testRunA)
	waitForSubscriptionCount(t, server, 1)

	// A connection whose scope is not a project (a conversation stream) can
	// subscribe nothing: the ordered protocol is not offered there.
	conversation := conversationStream(t, srv, "session-abc")
	conversation.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")
	assertJSONString(t, conversation.expectFrame(MsgTypeForbidden).Data, "code", CodeForbidden)
	conversation.subscribe(ResourceTypeRun, testRunA, 0, "sub_1")
	assertJSONString(t, conversation.expectFrame(MsgTypeForbidden).Data, "code", CodeForbidden)
	// And its unsubscribe is refused the same way, so the new protocol never
	// reaches a scope that could not have been subscribed.
	conversation.unsubscribe(ResourceTypeProject, testProjectA, "sub_2")
	assertJSONString(t, conversation.expectFrame(MsgTypeForbidden).Data, "code", CodeForbidden)
}

// ---------------------------------------------------------------------------
// Cursor failures
// ---------------------------------------------------------------------------

// TestSubscriptionCursorExpiredEndsTheSubscription covers §27.4's 410: the cursor
// points at history retention removed, so the only recovery is a snapshot. The
// frame carries the floor and the machine-readable recovery instruction, and the
// subscription ends rather than continuing from a cursor with a hole in it.
func TestSubscriptionCursorExpiredEndsTheSubscription(t *testing.T) {
	f := newReplayFixture(t)
	// Modelling trimmed history: the counter is preset, so the next event gets 5
	// and the retained history starts there (the same technique runstore's own
	// replay tests use).
	presetScopeCounter(t, f.store, projectScopeName(testProjectA), 4)
	f.projectEvent(testProjectA) // project_seq 5
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: time.Hour,
	})
	conn := projectStream(t, srv, testProjectA)
	server := clientForSession(t, srv.hub, ProjectScopePrefix+testProjectA)

	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")
	subscribed := conn.expectFrame(MsgTypeSubscribed)
	assertJSONString(t, subscribed.Data, "client_request_id", "sub_1")

	expired := conn.expectFrame(MsgTypeCursorExpired)
	assertJSONString(t, expired.Data, "client_request_id", "sub_1")
	assertJSONString(t, expired.Data, "scope", "project:"+testProjectA)
	assertJSONString(t, expired.Data, "code", CodeCursorExpired)
	assertJSONString(t, expired.Data, "recovery", RecoverySnapshot)
	assertJSONNumber(t, expired.Data, "retention_floor", 5)

	// The subscription is gone: the table entry is dropped by the goroutine's own
	// defer, so an empty table proves the goroutine returned.
	waitForSubscriptionCount(t, server, 0)
}

// TestSubscriptionInvalidCursorQuotesTheWatermark covers §27.4's 422: the cursor
// names a sequence the scope never allocated. The answer quotes the watermark so
// the client can correct its cursor, and the subscription ends.
func TestSubscriptionInvalidCursorQuotesTheWatermark(t *testing.T) {
	f := newReplayFixture(t)
	f.projectEvent(testProjectA)
	f.projectEvent(testProjectA)
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: time.Hour,
	})
	conn := projectStream(t, srv, testProjectA)
	server := clientForSession(t, srv.hub, ProjectScopePrefix+testProjectA)

	conn.subscribe(ResourceTypeProject, testProjectA, 99, "sub_1")
	assertJSONString(t, conn.expectFrame(MsgTypeSubscribed).Data, "client_request_id", "sub_1")

	invalid := conn.expectFrame(MsgTypeInvalidCursor)
	assertJSONString(t, invalid.Data, "client_request_id", "sub_1")
	assertJSONString(t, invalid.Data, "scope", "project:"+testProjectA)
	assertJSONString(t, invalid.Data, "code", CodeInvalidCursor)
	assertJSONNumber(t, invalid.Data, "high_watermark", 2)

	waitForSubscriptionCount(t, server, 0)
}

// ---------------------------------------------------------------------------
// Backend availability
// ---------------------------------------------------------------------------

// TestSubscriptionWithoutBackendAnswersUnavailable pins the answer of a server
// whose replay backend is not wired up (T1.04's job): an unavailable frame that
// tells the client to retry, never an invalid_request that blames it and never a
// subscription that nothing can serve.
func TestSubscriptionWithoutBackendAnswersUnavailable(t *testing.T) {
	srv := newSubscriptionServer(t, ReplayDependencies{})
	conn := projectStream(t, srv, testProjectA)
	server := clientForSession(t, srv.hub, ProjectScopePrefix+testProjectA)

	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")
	shape := conn.expectFrame(MsgTypeUnavailable)
	assertJSONString(t, shape.Data, "client_request_id", "sub_1")
	assertJSONString(t, shape.Data, "code", CodeBackendUnavailable)
	var retryable bool
	if err := json.Unmarshal(shape.Data["retryable"], &retryable); err != nil || !retryable {
		t.Fatalf("unavailable frame retryable = %s, want true", shape.Data["retryable"])
	}
	if _, ok := shape.Data["scope"]; ok {
		t.Error("unavailable frame carries a scope, but the subscription never started")
	}
	if server.SubscriptionCount() != 0 {
		t.Fatal("an unservable subscription was installed in the table")
	}
}

// TestSubscriptionOnAConversationStreamIsForbiddenEvenWithoutBackend: a
// connection that is not a project stream can never be served, so it is told
// forbidden before the backend is consulted. Answering it unavailable (retryable)
// while no backend is installed would send a client that is on the wrong route
// into a retry loop that can never succeed.
func TestSubscriptionOnAConversationStreamIsForbiddenEvenWithoutBackend(t *testing.T) {
	srv := newSubscriptionServer(t, ReplayDependencies{})
	conversation := conversationStream(t, srv, "session-abc")
	conversation.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")
	frame := conversation.expectFrame(MsgTypeForbidden)
	assertJSONString(t, frame.Data, "code", CodeForbidden)
	assertJSONString(t, frame.Data, "client_request_id", "sub_1")
}

// TestSubscriptionRunScopeWithoutLookupAnswersUnavailable is the same rule for a
// wiring gap rather than a missing backend: a reader but no run lookup. A run
// subscription must not be answered forbidden — that would tell the client its own
// run does not exist.
func TestSubscriptionRunScopeWithoutLookupAnswersUnavailable(t *testing.T) {
	f := newReplayFixture(t)
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		PollInterval: time.Hour,
	})
	conn := projectStream(t, srv, testProjectA)
	conn.subscribe(ResourceTypeRun, testRunA, 0, "sub_1")
	assertJSONString(t, conn.expectFrame(MsgTypeUnavailable).Data, "code", CodeBackendUnavailable)

	// A project subscription needs no lookup, so it still works.
	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_2")
	conn.drainReplay("sub_2", "project:"+testProjectA)
}

// TestSubscriptionRejectsFramesTheContractRefuses is the wire-level half of the
// tightened parser: a subscribe frame missing after or client_request_id is
// answered invalid_request (the contract lists all four data properties as
// required), and the subscription is not installed.
func TestSubscriptionRejectsFramesTheContractRefuses(t *testing.T) {
	f := newReplayFixture(t)
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: time.Hour,
	})
	conn := projectStream(t, srv, testProjectA)
	server := clientForSession(t, srv.hub, ProjectScopePrefix+testProjectA)

	cases := []struct {
		name string
		raw  string
	}{
		{"after missing", `{"type":"subscribe","data":{"resource_type":"project","resource_id":"` + testProjectA + `","client_request_id":"sub_1"}}`},
		{"client_request_id missing", `{"type":"subscribe","data":{"resource_type":"project","resource_id":"` + testProjectA + `","after":0}}`},
		{"case variant of resource_id", `{"type":"subscribe","data":{"resource_type":"project","RESOURCE_ID":"` + testProjectA + `","after":0,"client_request_id":"sub_1"}}`},
	}
	for _, tc := range cases {
		conn.sendRawFrame(tc.raw)
		shape := conn.expectFrame(MsgTypeInvalidRequest)
		assertJSONString(t, shape.Data, "code", CodeInvalidRequest)
		if bytes.Contains(shape.Data["reason"], []byte(testProjectA)) {
			t.Errorf("%s: invalid_request echoes the client's bytes", tc.name)
		}
	}
	if server.SubscriptionCount() != 0 {
		t.Fatal("a refused frame installed a subscription")
	}
}

// ---------------------------------------------------------------------------
// Slow clients
// ---------------------------------------------------------------------------

// TestSlowClientIsClosedWith1013AndReconnectsWithoutGap is §27.4's slow-client
// rule end to end: the client stops keeping up, the server's buffer fills, and the
// connection is closed with 1013 (Try Again Later) instead of dropping frames —
// because a dropped frame is a silent gap. The client then reconnects from its
// last applied cursor and receives the rest, contiguously.
func TestSlowClientIsClosedWith1013AndReconnectsWithoutGap(t *testing.T) {
	f := newReplayFixture(t)
	// A backlog big enough to overflow the server's send buffer and the client's
	// socket buffer, with payloads large enough that this takes a few hundred
	// frames rather than hundreds of thousands.
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
	// A small receive buffer on the client side: the client is not reading, so its
	// window closes after a few kilobytes and the server's buffer fills quickly.
	conn := dialStream(t, srv, "/projects/"+testProjectA+"/stream", func(dialer *gorillaws.Dialer) {
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
	})
	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")

	// Read the first frames, then slow down to a crawl: this is a client that
	// cannot keep up, not one that has gone away.
	type readResult struct {
		raw []byte
		err error
	}
	frames := make(chan readResult, 64)
	go func() {
		defer close(frames)
		for {
			// A deadline on the slow reader is what turns "the server never closed
			// the connection" from a hang into a failure: with the frames dropped
			// instead of the connection closed, this client simply stops receiving.
			_ = conn.conn.SetReadDeadline(time.Now().Add(15 * time.Second))
			_, data, err := conn.conn.ReadMessage()
			if err != nil {
				frames <- readResult{err: err}
				return
			}
			frames <- readResult{raw: data}
			time.Sleep(3 * time.Millisecond)
		}
	}()

	var (
		lastApplied int64
		closeErr    error
		seen        int
	)
	for result := range frames {
		if result.err != nil {
			closeErr = result.err
			break
		}
		seen++
		if seen > total+16 {
			t.Fatalf("received %d frames without the server closing: the buffer never filled", seen)
		}
		var top struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(result.raw, &top); err != nil {
			t.Fatalf("frame is not JSON: %v", err)
		}
		if MessageType(top.Type) != MsgTypeEvent {
			continue
		}
		var event struct {
			Sequence int64 `json:"sequence"`
		}
		if err := json.Unmarshal(top.Data, &event); err != nil {
			t.Fatalf("event frame has no sequence: %v", err)
		}
		lastApplied = event.Sequence
	}

	if isTimeout(closeErr) {
		t.Fatalf("the server never closed the connection after %d frames: the buffer filled but the client was left hanging", seen)
	}
	if !gorillaws.IsCloseError(closeErr, gorillaws.CloseTryAgainLater) {
		t.Fatalf("connection ended with %v, want a close with code %d (1013)", closeErr, gorillaws.CloseTryAgainLater)
	}
	if lastApplied == 0 {
		t.Fatal("the client applied no event before the close")
	}
	if lastApplied >= total {
		t.Fatalf("the client applied every event (%d): the test did not exercise the tail", lastApplied)
	}

	// Reconnect from the last applied cursor: everything after it must arrive,
	// contiguously, and nothing before it.
	reconnected := projectStream(t, srv, testProjectA)
	reconnected.subscribe(ResourceTypeProject, testProjectA, lastApplied, "sub_2")
	out := reconnected.drainReplay("sub_2", "project:"+testProjectA)
	if out.After != lastApplied {
		t.Fatalf("replay_started after = %d, want %d", out.After, lastApplied)
	}
	if out.HighWatermark != total {
		t.Fatalf("replay high watermark = %d, want %d", out.HighWatermark, total)
	}
	if len(out.Sequences) != int(total-lastApplied) {
		t.Fatalf("reconnected replay delivered %d events, want %d", len(out.Sequences), total-lastApplied)
	}
	if out.Sequences[0] != lastApplied+1 {
		t.Fatalf("first replayed sequence = %d, want %d", out.Sequences[0], lastApplied+1)
	}
}

// ---------------------------------------------------------------------------
// Lifecycle and the legacy protocol
// ---------------------------------------------------------------------------

// TestSubscriptionsStopWhenTheConnectionCloses proves subscriptions belong to
// their connection: when the socket goes away every subscription goroutine
// returns (the table entry is dropped by the goroutine's own defer and the
// subscription's done channel is closed), so nothing keeps reading the store for
// a client that is gone.
func TestSubscriptionsStopWhenTheConnectionCloses(t *testing.T) {
	f := newReplayFixture(t)
	for i := 0; i < 3; i++ {
		f.projectEvent(testProjectA)
	}
	f.runEvent(testProjectA, testRunA)
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: 5 * time.Millisecond,
	})
	baseline := runtime.NumGoroutine()

	conn := projectStream(t, srv, testProjectA)
	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_project")
	conn.drainReplay("sub_project", "project:"+testProjectA)
	conn.subscribe(ResourceTypeRun, testRunA, 0, "sub_run")
	conn.drainReplay("sub_run", "run:"+testRunA)

	server := clientForSession(t, srv.hub, ProjectScopePrefix+testProjectA)
	waitForSubscriptionCount(t, server, 2)
	subs := subscriptionPointers(t, server)

	if err := conn.conn.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	waitForClientCount(t, srv.hub, 0)
	waitForSubscriptionCount(t, server, 0)

	for _, sub := range subs {
		select {
		case <-sub.done:
		case <-time.After(5 * time.Second):
			t.Fatalf("subscription %s is still running after its connection closed", sub.scope)
		}
	}

	// The goroutine count is the second witness: the two subscription goroutines
	// are gone. The deadline allows the socket goroutines of the closed connection
	// to finish; a leaked subscription never settles.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := runtime.NumGoroutine(); got <= baseline+1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutines = %d, baseline was %d: a subscription leaked", runtime.NumGoroutine(), baseline)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestLegacyTopicSubscribeCoexistsWithOrderedSubscriptions pins that the dispatch
// split did not change the old protocol: a topic subscribe on the same connection
// still receives its topic broadcasts, while the ordered subscription on the same
// socket works as usual.
func TestLegacyTopicSubscribeCoexistsWithOrderedSubscriptions(t *testing.T) {
	f := newReplayFixture(t)
	f.projectEvent(testProjectA)
	srv := newSubscriptionServer(t, ReplayDependencies{
		Reader:       f.reader,
		Runs:         f.auth.Runs,
		PollInterval: 5 * time.Millisecond,
	})
	conn := projectStream(t, srv, testProjectA)

	topic := "flow:project:" + testProjectA
	conn.subscribeTopic(topic)
	waitForSubscriberCount(t, srv.hub, topic, 1)

	conn.subscribe(ResourceTypeProject, testProjectA, 0, "sub_1")
	out := conn.drainReplay("sub_1", "project:"+testProjectA)
	if out.HighWatermark != 1 {
		t.Fatalf("high watermark = %d, want 1", out.HighWatermark)
	}

	srv.hub.BroadcastToTopic(topic, &Message{Type: MessageType("flow_event"), Content: "flow.created"})
	// A legacy broadcast is a Message, not a §20.3 frame: it must arrive exactly as
	// the pre-T1.12 hub sent it.
	raw := conn.readRawMessage()
	var broadcast Message
	if err := json.Unmarshal(raw, &broadcast); err != nil {
		t.Fatalf("legacy broadcast is not a Message: %v (%s)", err, raw)
	}
	if broadcast.Type != MessageType("flow_event") || broadcast.Content != "flow.created" {
		t.Fatalf("legacy broadcast = %s, want a flow_event carrying flow.created", raw)
	}

	// The topic protocol is still refused for a topic outside the route's
	// allowlist, exactly as before.
	conn.subscribeTopic("flow:project:somewhere-else")
	if got := srv.hub.TopicSubscriberCount("flow:project:somewhere-else"); got != 0 {
		t.Fatalf("cross-project topic subscribed the client: %d", got)
	}
}
