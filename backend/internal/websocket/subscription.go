// Ordered subscriptions: replay to H, live after H (T1.12.b).
//
// replay.go is the protocol's vocabulary — parse a frame, authorize it, read one
// page, encode one frame. This file is the machinery that turns those blocks into
// one subscription on one connection, and it is where §20.3's order and §27.4's
// recovery rules are actually enforced:
//
//		subscribed → replay_started → 0..N events (sequence ≤ H) → replay_finished
//		→ live events (sequence > H)
//
//	  - H (the high watermark) is read once, in the same SQL snapshot as the first
//	    page, and the replay never sends an event above it. An event appended while
//	    the replay runs belongs to the live phase and is read there, by cursor, so
//	    the boundary between the two phases is a number rather than a moment in
//	    time.
//	  - Live events are always read from the event store by cursor, never forwarded
//	    from a delivery. The store is the only thing that knows the sequence order,
//	    so a delivery that arrives out of order, twice, or not at all cannot create
//	    a gap in what the client sees; a wake-up only makes the next read happen
//	    sooner (§27.4: 缓冲或从 event store 后续读取 — this is the "后续读取" half,
//	    chosen over buffering because it has no buffer to overflow).
//	  - Every frame of one subscription is enqueued by one goroutine, in order.
//	    Different subscriptions of a connection may interleave; one subscription
//	    never does.
//	  - The enqueue is non-blocking. A full buffer means the client is not keeping
//	    up, and the connection is closed with 1013 (Try Again Later) so the client
//	    reconnects from its last applied cursor. Dropping the frame instead would
//	    be a silent gap, which §27.4 forbids; blocking would stall the hub.
//	  - Each subscription owns a cursor on its scope's own counter, so one
//	    connection may follow a project and a run at the same time without the two
//	    counters ever meeting (§27.4: 不混用两种计数).
//
// The backend is injected (ReplayDependencies) rather than opened here: runstore
// is wired into the process by T1.04, and until that happens an ordered
// subscription is answered with an unavailable frame instead of being accepted
// and then starved.
package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/runstore"
)

// DefaultReplayPollInterval is the live phase's fallback read cadence.
//
// It exists because wake-ups are an optimization, not a guarantee: the dispatcher
// that would call HubDeliverer is wired by T1.04, and a delivery may be delayed
// or lost (the outbox retries it). With a poll interval the client receives every
// event regardless — later, never never.
const DefaultReplayPollInterval = time.Second

// Replay page bounds. The store owns them (runstore.DefaultEventLimit /
// MaxEventLimit); they are restated here as the defaults this package applies
// when a caller configures nothing.
const (
	DefaultReplayPageLimit = runstore.DefaultEventLimit
	MaxReplayPageLimit     = runstore.MaxEventLimit
)

// ReplayDependencies is everything a connection needs to serve an ordered
// subscription: the event-store read, the run lookup authorization uses, and the
// two knobs of the live phase.
//
// It is injected per hub rather than held in a package-level singleton so a test
// can give one hub its own store, and so the composition root decides when the
// server has a store at all.
type ReplayDependencies struct {
	// Reader reads replay pages. Required: without it no ordered subscription is
	// served (the answer is an unavailable frame).
	Reader EventReader
	// Runs resolves a run id to its project. Required for a run subscription; a
	// project subscription needs no lookup. A missing lookup fails closed as a
	// wiring error (unavailable), never as forbidden.
	Runs RunLookup
	// PollInterval is how often the live phase re-reads the store when no
	// delivery woke it. <= 0 means DefaultReplayPollInterval.
	PollInterval time.Duration
	// PageLimit is the replay page size. <= 0 means DefaultReplayPageLimit; above
	// MaxReplayPageLimit it is clamped, exactly as the store clamps it.
	PageLimit int
}

// normalized applies the defaults and the store's own clamp, so every reader of
// the dependencies sees usable numbers.
func (d ReplayDependencies) normalized() ReplayDependencies {
	if d.PollInterval <= 0 {
		d.PollInterval = DefaultReplayPollInterval
	}
	switch {
	case d.PageLimit <= 0:
		d.PageLimit = DefaultReplayPageLimit
	case d.PageLimit > MaxReplayPageLimit:
		d.PageLimit = MaxReplayPageLimit
	}
	return d
}

// configured reports whether a subscription can be served at all.
func (d ReplayDependencies) configured() bool { return d.Reader != nil }

// SetReplayDependencies installs the ordered-subscription backend on this hub.
//
// This is T1.04's call: the hub never opens a database, and a hub that has not
// been given one answers every ordered subscription with an unavailable frame
// (retryable) rather than accepting a subscription nothing can serve.
func (h *Hub) SetReplayDependencies(deps ReplayDependencies) {
	if h == nil {
		return
	}
	deps = deps.normalized()
	h.mu.Lock()
	h.replay = deps
	h.mu.Unlock()
}

// ReplayDependencies returns the installed backend (the zero value when none is
// installed). It is what the subscription path reads on every new subscription,
// so a backend installed after a connection was accepted still serves it.
func (h *Hub) ReplayDependencies() ReplayDependencies {
	if h == nil {
		return ReplayDependencies{}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.replay
}

// SetReplayDependencies installs the backend on the process-wide hub (GetHub()).
//
// It exists for the composition root, which has one store and one hub: the route
// api/handlers/projects.go:StreamProjectEvents attaches connections to GetHub(),
// so a backend installed anywhere else would never be seen. Tests build their own
// hub and call the method on it.
func SetReplayDependencies(deps ReplayDependencies) {
	GetHub().SetReplayDependencies(deps)
}

// ProjectScopePrefix is the SessionID prefix the project stream route puts on a
// connection ("project:<id>", api/handlers/projects.go:StreamProjectEvents).
const ProjectScopePrefix = "project:"

// connectionProjectID returns the project this connection already belongs to, as
// the route that upgraded it recorded it.
//
// ok=false means the connection is not a project stream — an agent conversation
// (a random session id) or a debate ("debate:<id>"). The ordered-subscription
// protocol is offered on project streams only, so such a connection can name no
// resource it would be allowed to follow, and every new-protocol frame it sends
// is answered forbidden.
func (c *Client) connectionProjectID() (string, bool) {
	if c == nil {
		return "", false
	}
	id, found := strings.CutPrefix(c.SessionID, ProjectScopePrefix)
	if !found || id == "" {
		return "", false
	}
	return id, true
}

// subscriptionState is where one subscription is in the §20.3 order.
//
// It is exposed through SubscriptionSnapshots so a test — and, later, a client
// that wants to know whether a replay is still running — can tell "replaying"
// from "live" without inferring it from timing.
type subscriptionState string

const (
	// SubscriptionReplaying: subscribed and replay_started were sent; the frames
	// up to the high watermark are still being delivered.
	SubscriptionReplaying subscriptionState = "replaying"
	// SubscriptionLive: replay_finished was sent; only events above the high
	// watermark follow.
	SubscriptionLive subscriptionState = "live"
	// SubscriptionEnded: the subscription stopped — a cursor frame ended it, the
	// client unsubscribed, or the connection went away. It is removed from the
	// connection's table as it ends.
	SubscriptionEnded subscriptionState = "ended"
)

// subscription is one ordered subscription of one connection: the grant that
// authorized it, its own cursor on the scope's counter, and the goroutine that
// delivers it.
//
// After newSubscription the goroutine is the only writer of cursor and state; the
// mutex exists so the connection (and a test) can read them.
type subscription struct {
	client          *Client
	deps            ReplayDependencies
	grant           Grant
	scope           string
	clientRequestID string
	after           int64

	ctx    context.Context
	cancel context.CancelFunc
	// wake carries "the store may have more events for this scope". It holds one
	// signal: a second one while the first is pending adds nothing, because the
	// reader reads everything past its cursor in one pass.
	wake chan struct{}
	// done is closed when the goroutine has returned. stop() waits on it, which is
	// what lets a replacement (or a disconnect) guarantee that no further frame of
	// this subscription can be enqueued.
	done chan struct{}

	mu     sync.Mutex
	cursor int64
	state  subscriptionState
}

// newSubscription builds a subscription in the replaying state with its cursor at
// the client's `after`. The goroutine is not started yet.
func (c *Client) newSubscription(deps ReplayDependencies, grant Grant, req SubscribeRequest) *subscription {
	ctx, cancel := context.WithCancel(context.Background())
	return &subscription{
		client:          c,
		deps:            deps,
		grant:           grant,
		scope:           grant.Scope.String(),
		clientRequestID: req.ClientRequestID,
		after:           req.After,
		ctx:             ctx,
		cancel:          cancel,
		wake:            make(chan struct{}, 1),
		done:            make(chan struct{}),
		cursor:          req.After,
		state:           SubscriptionReplaying,
	}
}

// signal tells the subscription that its scope may have more events. It never
// blocks: a pending signal is as good as a new one.
func (s *subscription) signal() {
	if s == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// stop cancels the subscription and waits for its goroutine to return. It is
// idempotent: cancel and a closed done channel are both safe to observe twice.
func (s *subscription) stop() {
	if s == nil {
		return
	}
	s.cancel()
	<-s.done
}

// Cursor is the last sequence this subscription enqueued.
func (s *subscription) Cursor() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursor
}

// State is where the subscription is in the §20.3 order.
func (s *subscription) State() subscriptionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *subscription) setCursor(cursor int64) {
	s.mu.Lock()
	s.cursor = cursor
	s.mu.Unlock()
}

func (s *subscription) setState(state subscriptionState) {
	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
}

// run is the subscription's goroutine: it writes every frame of this
// subscription, in order, and returns when the subscription ends.
//
// It is the only writer of this subscription's frames. subscribed is sent here
// rather than by the caller so that the goroutine's first frame is the first
// frame, with no window in which another goroutine could interleave one.
func (s *subscription) run() {
	defer close(s.done)
	defer s.cancel()
	// Deferred last, so it runs first: the entry leaves the table before done is
	// closed, and a caller that waited on done never sees a stale entry.
	// forgetSubscription only removes this exact pointer, so a replacement
	// installed meanwhile is left alone.
	defer s.client.forgetSubscription(s)

	if !s.send(SubscribedFrame(s.clientRequestID, s.grant)) {
		s.setState(SubscriptionEnded)
		return
	}
	if !s.replay() {
		s.setState(SubscriptionEnded)
		return
	}
	s.live()
	s.setState(SubscriptionEnded)
}

// send enqueues one frame of this subscription, in order.
//
// A false return means the subscription must stop: the client's buffer is full
// (a slow client, closed with 1013 rather than served a gap) or the connection is
// already gone.
func (s *subscription) send(frame []byte) bool {
	if s.client.enqueueFrame(frame) {
		return true
	}
	s.client.closeSlowConsumer()
	return false
}

// replay delivers the closed window: the events above `after` and at or below the
// high watermark read with the first page, then replay_finished.
//
// It reports whether the subscription should continue into the live phase.
func (s *subscription) replay() bool {
	batch, err := ReadReplay(s.ctx, s.deps.Reader, s.grant, s.after, s.deps.PageLimit)
	if err != nil {
		s.endOnReadError(batch, err)
		return false
	}
	// The watermark is fixed here, by the same SQL snapshot that produced the
	// first page: everything at or below it is the replay, everything above it is
	// live. A later read cannot move it.
	highWatermark := batch.HighWatermark
	if !s.send(ReplayStartedFrame(s.clientRequestID, s.scope, s.after, highWatermark, batch.RetentionFloor)) {
		return false
	}

	cursor := s.after
	for {
		truncated := false
		for _, event := range batch.Events {
			if event.Sequence > highWatermark {
				// Appended after the snapshot that fixed H: not part of this
				// window. The live phase reads it from the store by cursor, so it
				// is delivered once and in order rather than here.
				truncated = true
				break
			}
			if event.Sequence <= cursor {
				continue
			}
			if !s.send(EventFrame(event)) {
				return false
			}
			cursor = event.Sequence
		}
		if truncated || !batch.HasMore || cursor >= highWatermark {
			break
		}
		next, err := ReadReplay(s.ctx, s.deps.Reader, s.grant, cursor, s.deps.PageLimit)
		if err != nil {
			s.setCursor(cursor)
			s.endOnReadError(next, err)
			return false
		}
		if len(next.Events) == 0 && next.NextAfter <= cursor {
			// Unreachable with the store's own reads (a page is strictly above the
			// cursor, and has_more implies a full page). Stopping here keeps a
			// future reader bug from spinning this loop forever.
			break
		}
		batch = next
	}

	s.setCursor(cursor)
	if !s.send(ReplayFinishedFrame(s.clientRequestID, s.scope, highWatermark, cursor)) {
		return false
	}
	return true
}

// live delivers the open window: everything above the cursor, for as long as the
// subscription lives.
//
// The loop is driven by wake-ups (HubDeliverer, once T1.04 registers it) and by
// the poll interval, which is the guarantee: without any wake-up at all the
// client still receives every event, one poll interval later.
func (s *subscription) live() {
	s.setState(SubscriptionLive)
	ticker := time.NewTicker(s.deps.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
		if !s.pump() {
			return
		}
	}
}

// pump reads everything past the cursor and enqueues it in order. It reports
// whether the subscription should keep running.
//
// The read is by cursor and the cursor only moves forward, so a delivery that
// arrives twice, out of order, or not at all cannot make this loop emit a
// duplicate or skip a sequence: the store is the order, not the delivery.
func (s *subscription) pump() bool {
	for {
		cursor := s.Cursor()
		batch, err := ReadReplay(s.ctx, s.deps.Reader, s.grant, cursor, s.deps.PageLimit)
		if err != nil {
			s.endOnReadError(batch, err)
			return false
		}
		for _, event := range batch.Events {
			if event.Sequence <= cursor {
				// Defensive: a page is strictly above the cursor. Skipping keeps a
				// reader that returned an already-delivered event from producing a
				// duplicate frame.
				continue
			}
			if !s.send(EventFrame(event)) {
				return false
			}
			cursor = event.Sequence
			s.setCursor(cursor)
		}
		if !batch.HasMore {
			return true
		}
	}
}

// endOnReadError answers a failed read and ends the subscription.
//
// The two cursor errors are the client's to act on (§27.4): 410 cursor_expired
// carries the retention floor and the snapshot recovery instruction, 422
// invalid_cursor carries the watermark. Both end the subscription, because
// continuing to stream live events from a cursor that names a hole is exactly the
// silent gap the protocol forbids. Any other failure is the server's, so the
// client is told backend_unavailable (retryable) and keeps its cursor.
//
// The cancellation case comes first: a subscription stopped by an unsubscribe or
// by its connection going away has no client left to answer, and its in-flight
// read comes back as context.Canceled. Answering that with an unavailable frame
// would send a "retry" to a connection that deliberately left the subscription —
// and on a shared connection it would be indistinguishable from a real backend
// failure.
func (s *subscription) endOnReadError(batch ReplayBatch, err error) {
	if s.ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	switch {
	case errors.Is(err, runstore.ErrCursorExpired):
		s.send(CursorExpiredFrame(s.clientRequestID, s.scope, batch.RetentionFloor))
	case errors.Is(err, runstore.ErrInvalidCursor):
		s.send(InvalidCursorFrame(s.clientRequestID, s.scope, batch.HighWatermark))
	default:
		log.Printf("[WS] subscription %s read failed: %v", s.scope, err)
		s.send(UnavailableFrame(s.clientRequestID))
	}
}

// startSubscription handles one new-protocol subscribe frame, from parse to a
// running goroutine.
//
// It is called from the connection's read goroutine, so the frames it writes
// itself (invalid_request, forbidden, unavailable) are enqueued before the
// subscription goroutine starts and cannot race with it.
func (c *Client) startSubscription(raw []byte) {
	req, err := ParseSubscribeFrame(raw)
	if err != nil {
		// The correlation id is empty: the frame never parsed far enough to have
		// one, and the reason is a fixed phrase that says what was wrong with the
		// shape (never a copy of the client's bytes).
		c.enqueueFrame(InvalidRequestFrame("", SubscribeErrorReason(err)))
		return
	}
	projectID, ok := c.connectionProjectID()
	if !ok {
		// Not a project stream: the ordered protocol is not offered here, so every
		// resource is refused with the same frame an unauthorized resource gets.
		// This is decided before the backend is consulted: a missing backend is
		// retryable, and a connection that can never be served must not be told
		// to retry.
		c.enqueueFrame(ForbiddenFrame(req.ClientRequestID))
		return
	}
	deps := c.Hub.ReplayDependencies()
	if !deps.configured() {
		c.enqueueFrame(UnavailableFrame(req.ClientRequestID))
		return
	}
	grant, err := (Authorizer{Runs: deps.Runs}).Authorize(context.Background(), projectID, req)
	switch {
	case errors.Is(err, ErrForbidden):
		c.enqueueFrame(ForbiddenFrame(req.ClientRequestID))
		return
	case err != nil:
		// A lookup failure is not an authorization decision: answering forbidden
		// would tell the client its own run does not exist (§20.3).
		c.enqueueFrame(UnavailableFrame(req.ClientRequestID))
		return
	}

	sub := c.newSubscription(deps, grant, req)
	if old := c.replaceSubscription(sub); old != nil {
		// A duplicate subscribe on the same scope replaces the old subscription.
		// That is the reconnect path of §27.4 — a client resumes from its last
		// applied cursor without tearing down the socket — and the wait is what
		// makes it safe to read: after it, no frame of the old subscription can be
		// enqueued, so every frame of this scope from here on belongs to the new
		// subscription and follows its `subscribed` in order. (Frames the old
		// subscription had already enqueued are still delivered first; they carry
		// sequences the client has applied, which §27.4 allows as duplicates.)
		old.stop()
	}
	go sub.run()
}

// stopSubscriptionFrame handles one new-protocol unsubscribe frame: it stops the
// connection's subscription for that scope, if any.
//
// There is no answer frame — the protocol's vocabulary has none, and inventing one
// would be a new server frame type this card is not allowed to add. A frame that
// names a scope this connection does not follow is a no-op rather than an error:
// unsubscribe is idempotent, and a scope that was never granted cannot be in the
// table, so the answer cannot become an existence oracle either.
func (c *Client) stopSubscriptionFrame(raw []byte) {
	req, err := ParseUnsubscribeFrame(raw)
	if err != nil {
		c.enqueueFrame(InvalidRequestFrame("", SubscribeErrorReason(err)))
		return
	}
	if _, ok := c.connectionProjectID(); !ok {
		c.enqueueFrame(ForbiddenFrame(req.ClientRequestID))
		return
	}
	scope := req.Scope().String()
	if scope == "" {
		// Unreachable: the parser refuses a resource type outside the enum.
		c.enqueueFrame(InvalidRequestFrame(req.ClientRequestID, ReasonInvalidResourceType))
		return
	}
	if sub := c.takeSubscription(scope); sub != nil {
		sub.stop()
	}
}

// handleSubscriptionFrame routes one raw client frame to the ordered-subscription
// protocol, and reports whether it took the frame. The legacy
// decode-into-Message path runs only for the frames it did not take, so the old
// topic protocol keeps its behaviour byte for byte.
func (c *Client) handleSubscriptionFrame(raw []byte) bool {
	kind, ok := probeSubscriptionFrame(raw)
	if !ok {
		return false
	}
	switch kind {
	case MsgTypeSubscribe:
		c.startSubscription(raw)
	case MsgTypeUnsubscribe:
		c.stopSubscriptionFrame(raw)
	}
	return true
}

// probeSubscriptionFrame reports whether raw is an ordered-subscription frame — a
// subscribe or unsubscribe whose data object names a resource_type — and which of
// the two it is.
//
// The probe decides only which parser runs, never what is authorized. It compares
// the key case-insensitively on purpose: a case variant of resource_type is not
// the new protocol's key, but treating it as an old topic frame would answer it
// with silence, while the strict parser answers it with invalid_request. Old topic
// frames never carry such a key (their data has `topic`), so nothing that used to
// work changes path.
func probeSubscriptionFrame(raw []byte) (MessageType, bool) {
	var envelope struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", false
	}
	var kind MessageType
	switch MessageType(envelope.Type) {
	case MsgTypeSubscribe, MsgTypeUnsubscribe:
		kind = MessageType(envelope.Type)
	default:
		return "", false
	}
	if len(envelope.Data) == 0 || !dataNamesResourceType(envelope.Data) {
		return "", false
	}
	return kind, true
}

// dataNamesResourceType reports whether a JSON object carries a resource_type key,
// compared without regard to case. A data value that is not an object (a string, an
// array, a number) does not name one.
func dataNamesResourceType(data json.RawMessage) bool {
	dec := json.NewDecoder(bytes.NewReader(data))
	token, err := dec.Token()
	if err != nil || !isDelim(token, '{') {
		return false
	}
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return false
		}
		key, ok := token.(string)
		if !ok {
			return false
		}
		if strings.EqualFold(key, "resource_type") {
			return true
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return false
		}
	}
	return false
}

// messageNamesResource reports whether a decoded message carries a resource_type
// key. It is the guard on the legacy path: a frame that names a resource must
// never be handled as a topic subscription, whatever brought it here.
func messageNamesResource(msg *Message) bool {
	if msg == nil || msg.Data == nil {
		return false
	}
	for key := range msg.Data {
		if strings.EqualFold(key, "resource_type") {
			return true
		}
	}
	return false
}

// replaceSubscription installs sub as the connection's subscription for its scope
// and returns the subscription it replaced, if any.
func (c *Client) replaceSubscription(sub *subscription) *subscription {
	if c == nil || sub == nil {
		return nil
	}
	c.subMu.Lock()
	defer c.subMu.Unlock()
	if c.subs == nil {
		c.subs = make(map[string]*subscription)
	}
	old := c.subs[sub.scope]
	c.subs[sub.scope] = sub
	return old
}

// takeSubscription removes and returns the connection's subscription for scope.
func (c *Client) takeSubscription(scope string) *subscription {
	if c == nil || scope == "" {
		return nil
	}
	c.subMu.Lock()
	defer c.subMu.Unlock()
	sub := c.subs[scope]
	if sub != nil {
		delete(c.subs, scope)
	}
	return sub
}

// forgetSubscription drops sub from the table if it is still the one installed
// there. A subscription that was replaced by a newer one leaves the newer entry
// alone — the table holds subscriptions, not history.
func (c *Client) forgetSubscription(sub *subscription) {
	if c == nil || sub == nil {
		return
	}
	c.subMu.Lock()
	defer c.subMu.Unlock()
	if c.subs[sub.scope] == sub {
		delete(c.subs, sub.scope)
	}
}

// stopAllSubscriptions ends every subscription of this connection and waits for
// the goroutines to return. It is called when the connection goes away: the
// subscriptions belong to the connection, and one that outlived its socket would
// keep a cursor and a store read alive for nobody.
func (c *Client) stopAllSubscriptions() {
	if c == nil {
		return
	}
	c.subMu.Lock()
	subs := make([]*subscription, 0, len(c.subs))
	for _, sub := range c.subs {
		subs = append(subs, sub)
	}
	c.subs = nil
	c.subMu.Unlock()

	for _, sub := range subs {
		sub.stop()
	}
}

// signalScope wakes this connection's subscription for scope, if it has one.
func (c *Client) signalScope(scope string) {
	if c == nil || scope == "" {
		return
	}
	c.subMu.Lock()
	sub := c.subs[scope]
	c.subMu.Unlock()
	if sub != nil {
		sub.signal()
	}
}

// SubscriptionSnapshot is a read-only view of one ordered subscription.
type SubscriptionSnapshot struct {
	Scope           string
	ClientRequestID string
	ProjectID       string
	ResourceType    string
	ResourceID      string
	// After is the cursor the client asked to resume from.
	After int64
	// Cursor is the last sequence enqueued for this scope.
	Cursor int64
	State  subscriptionState
}

// SubscriptionSnapshots returns one snapshot per ordered subscription this
// connection holds, ordered by scope. It is the subscription table made readable
// for tests and for diagnostics; the subscriptions themselves are only reachable
// through this method.
func (c *Client) SubscriptionSnapshots() []SubscriptionSnapshot {
	if c == nil {
		return nil
	}
	c.subMu.Lock()
	subs := make([]*subscription, 0, len(c.subs))
	for _, sub := range c.subs {
		subs = append(subs, sub)
	}
	c.subMu.Unlock()

	out := make([]SubscriptionSnapshot, 0, len(subs))
	for _, sub := range subs {
		out = append(out, SubscriptionSnapshot{
			Scope:           sub.scope,
			ClientRequestID: sub.clientRequestID,
			ProjectID:       sub.grant.ProjectID,
			ResourceType:    sub.grant.Scope.Kind,
			ResourceID:      sub.grant.Scope.ID,
			After:           sub.after,
			Cursor:          sub.Cursor(),
			State:           sub.State(),
		})
	}
	sortSnapshots(out)
	return out
}

// SubscriptionCount returns how many ordered subscriptions this connection holds.
func (c *Client) SubscriptionCount() int {
	if c == nil {
		return 0
	}
	c.subMu.Lock()
	defer c.subMu.Unlock()
	return len(c.subs)
}

// sortSnapshots orders snapshots by scope so a caller (and a test) sees a stable
// list. It is a tiny insertion sort: the table holds a handful of subscriptions.
func sortSnapshots(snapshots []SubscriptionSnapshot) {
	for i := 1; i < len(snapshots); i++ {
		for j := i; j > 0 && snapshots[j].Scope < snapshots[j-1].Scope; j-- {
			snapshots[j], snapshots[j-1] = snapshots[j-1], snapshots[j]
		}
	}
}

// wakeScope wakes every subscription on scope across the hub's connections.
//
// It copies the client list before touching any connection: a hub lock held while
// a connection lock is taken would order the two, and this path has no need to.
func (h *Hub) wakeScope(scope string) {
	if h == nil || scope == "" {
		return
	}
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for _, client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	for _, client := range clients {
		client.signalScope(scope)
	}
}

// wakeScopes wakes every subscription on any of scopes.
func (h *Hub) wakeScopes(scopes ...string) {
	for _, scope := range scopes {
		h.wakeScope(scope)
	}
}
