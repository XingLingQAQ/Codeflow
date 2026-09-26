// Ordered subscription frames for the scoped WebSocket stream (T1.12.a).
//
// §20.3 fixes the client's first frame and the server's answer order:
//
//	{"type":"subscribe","data":{"resource_type":"run","resource_id":"run_88",
//	 "after":41,"client_request_id":"sub_1"}}
//
//	subscribed → replay_started → 0..N events → replay_finished → live events
//
// and §27.4 fixes what the replay is built on: the page and the snapshot
// boundaries (high watermark H, retention floor F) must come from ONE SQL
// snapshot (runstore.ReplayProject/ReplayRun do exactly that read), the response
// is projected on the subscription's own counter — project_seq for a project
// subscription, run_seq for a run subscription, never mixed — after < F-1 is 410
// cursor_expired (only a snapshot can recover) and after > H is 422
// invalid_cursor.
//
// This file is the server-side vocabulary of that protocol and nothing else:
//
//   - parsing the subscribe frame (§20.3) strictly, so that a case variant of a
//     wire key is a different key rather than the same field;
//   - authorizing it against the project the connection already belongs to
//     (T1.12's AUTH scope: a connection to project A can never subscribe
//     project B, and cannot even learn whether B's run exists);
//   - reading one replay page through the event store and projecting it onto the
//     wire shape of schemas/execution-event.schema.json;
//   - encoding the control frames as []byte, ready to write to a connection.
//
// It deliberately does not touch the hub. T1.12.a ships the blocks; T1.12.b
// wires them into a connection (replay to H, buffer/back-read > H, close 1013
// for a slow client) and T1.14 renders them. Nothing here writes to a socket,
// spawns a goroutine or takes a lock, so the old topic-subscribe path in hub.go
// keeps its behaviour unchanged.
package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
)

// Message types of the ordered-subscription protocol (§20.3). They extend the
// hub's MessageType vocabulary, but no frame here travels through Message.Data:
// every frame has its own fixed struct, so a map key rename cannot leak into the
// protocol.
const (
	// MsgTypeSubscribed acknowledges an authorized subscription: the first frame
	// of the §20.3 order.
	MsgTypeSubscribed MessageType = "subscribed"
	// MsgTypeReplayStarted opens the replay window (after → high watermark) and
	// states the retention floor the read was made against.
	MsgTypeReplayStarted MessageType = "replay_started"
	// MsgTypeEvent carries one ExecutionEvent of
	// schemas/execution-event.schema.json.
	MsgTypeEvent MessageType = "event"
	// MsgTypeReplayFinished closes the replay: everything at or below
	// high_watermark was delivered, and live events follow with a sequence
	// greater than it (§27.4).
	MsgTypeReplayFinished MessageType = "replay_finished"
	// MsgTypeForbidden is the only answer to a subscription outside the
	// connection's project. It is identical whether the resource belongs to
	// another project or does not exist at all (§20.3: no sensitive information
	// beyond existence — and this frame does not even give that).
	MsgTypeForbidden MessageType = "forbidden"
	// MsgTypeCursorExpired reports after < retention_floor-1 (§27.4 → 410): the
	// history the cursor points at is gone and only a snapshot can recover.
	MsgTypeCursorExpired MessageType = "cursor_expired"
	// MsgTypeInvalidCursor reports after > high_watermark (§27.4 → 422): the
	// cursor names a sequence this scope never allocated.
	MsgTypeInvalidCursor MessageType = "invalid_cursor"
	// MsgTypeInvalidRequest reports a frame the parser refused. Its reason is a
	// fixed phrase, never the client's bytes.
	MsgTypeInvalidRequest MessageType = "invalid_request"
)

// Error codes carried by the error frames: the cursor reason names of §27.4
// (cursor_expired / invalid_cursor), the error-envelope code forbidden (§20.4
// clarification: the 13 envelope codes are the vocabulary of error frames too),
// and invalid_request for a frame the parser refused.
const (
	CodeForbidden      = "forbidden"
	CodeCursorExpired  = "cursor_expired"
	CodeInvalidCursor  = "invalid_cursor"
	CodeInvalidRequest = "invalid_request"
)

// RecoverySnapshot is the machine-readable recovery instruction of a
// cursor_expired frame: replace the local cache with a snapshot and resume from
// the cursor that snapshot carries (§27.4). The snapshot URL itself is
// T1.12.b/T1.14's decision; this file does not invent one.
const RecoverySnapshot = "snapshot"

// Fixed messages of the error frames. They are constants, not format strings:
// an error frame must not be a channel for anything the client sent, and the
// forbidden message in particular must read the same for "belongs to another
// project" and "does not exist".
const (
	forbiddenMessage     = "subscription is not authorized for this connection"
	cursorExpiredMessage = "cursor is below the retention floor; replace the local cache with a snapshot"
	invalidCursorMessage = "cursor is above the scope high watermark"
)

// MaxSubscribeFrameBytes bounds one control frame (§27.4: control frames 64 KiB).
// A frame of exactly this size is accepted; one byte more is refused.
const MaxSubscribeFrameBytes = 64 * 1024

// Field length limits of a subscribe frame: the resource id is 1..128 bytes (the
// same bound the schema puts on project_id/run_id/scope) and the optional
// client_request_id is at most 128 bytes.
const (
	maxSubscribeResourceIDBytes      = 128
	maxSubscribeClientRequestIDBytes = 128
)

// Resource types a subscription may name (§20.3's closed enum). Scope.Kind uses
// the same two values, so one vocabulary covers the frame and the grant.
const (
	ResourceTypeProject = "project"
	ResourceTypeRun     = "run"
)

// ErrInvalidSubscribe is the sentinel for a frame that is not a usable subscribe
// request: a size overrun, malformed or truncated JSON, trailing content, an
// unknown or duplicated key, a key that is not the exact lowercase wire name, a
// bad resource_type/resource_id/after/client_request_id, or a missing type/data.
// The caller answers with an invalid_request frame.
var ErrInvalidSubscribe = errors.New("websocket: invalid subscribe frame")

// ErrForbidden is the sentinel for a subscription this connection may not make:
// a project other than its own, or a run that is not in its own project. It is
// returned unwrapped and with no details on purpose — the answer to "another
// project's run" and to "no such run" must be the same value and the same text,
// or the error itself would be an existence oracle.
var ErrForbidden = errors.New("websocket: subscription is not authorized for this connection")

// Fixed reasons of a rejected frame. They are a closed set of phrases: an
// invalid_request frame carries a class, never the offending text (§20.3), so
// the parser records a reason plus, at most, a wire field name of this file's own
// vocabulary.
const (
	ReasonFrameTooLarge          = "frame_too_large"
	ReasonNotJSONObject          = "not_json_object"
	ReasonUnknownField           = "unknown_field"
	ReasonDuplicateField         = "duplicate_field"
	ReasonWrongFrameType         = "wrong_frame_type"
	ReasonInvalidData            = "invalid_data"
	ReasonInvalidResourceType    = "invalid_resource_type"
	ReasonInvalidResourceID      = "invalid_resource_id"
	ReasonInvalidAfter           = "invalid_after"
	ReasonInvalidClientRequestID = "invalid_client_request_id"
	ReasonUnusableFrame          = "unusable_frame"
)

// SubscribeRequest is a parsed, still-unauthorized §20.3 subscribe frame.
//
// After is the client's last applied sequence on the scope's own counter; 0
// means "from the beginning of the retained history" (§20.2: after=0 with
// retention_floor=1 is the legal first replay). ClientRequestID is optional in
// the plan's frame (the §20.3 example sends "sub_1"); every server frame echoes
// it so the client can correlate the answer, and an empty value simply means the
// client asked for no correlation.
type SubscribeRequest struct {
	ResourceType    string
	ResourceID      string
	After           int64
	ClientRequestID string
}

// SubscribeError is one parse failure: a fixed reason (safe to put in a frame)
// plus the field class that failed, for logs.
//
// It deliberately holds no copy of the input. Field is a wire name from this
// file's own vocabulary ("type", "data", "data.resource_id", ...) or empty, so
// even a log line cannot echo a key the client invented.
type SubscribeError struct {
	Reason string
	Field  string
}

// Error implements error.
func (e *SubscribeError) Error() string {
	if e == nil {
		return ErrInvalidSubscribe.Error()
	}
	if e.Field == "" {
		return "websocket: invalid subscribe frame: " + e.Reason
	}
	return "websocket: invalid subscribe frame: " + e.Reason + " (field " + e.Field + ")"
}

// Unwrap makes every *SubscribeError match errors.Is(err, ErrInvalidSubscribe).
func (e *SubscribeError) Unwrap() error { return ErrInvalidSubscribe }

// SubscribeErrorReason returns the fixed reason phrase to put in an
// invalid_request frame for err. Anything that is not a *SubscribeError (nil, or
// an error from somewhere else) becomes ReasonUnusableFrame, so the frame never
// carries foreign error text.
func SubscribeErrorReason(err error) string {
	var se *SubscribeError
	if errors.As(err, &se) && se.Reason != "" {
		return se.Reason
	}
	return ReasonUnusableFrame
}

// ParseSubscribeFrame decodes one §20.3 subscribe frame strictly.
//
// Strictness is the point. encoding/json matches field names case-insensitively,
// so a plain json.Unmarshal into a struct tagged `resource_id` accepts
// "RESOURCE_ID" — and DisallowUnknownFields does not stop it, because the
// case-variant is a match, not an unknown field. A key that is not the exact
// lowercase wire name is a different key on the wire, and accepting it would let
// a client smuggle a second spelling of the same field past every later
// allowlist check. This parser therefore walks the token stream itself and
// compares keys byte for byte, refusing unknown keys, duplicated keys and case
// variants alike.
//
// It also refuses, all wrapping ErrInvalidSubscribe:
//
//   - a frame over MaxSubscribeFrameBytes (64 KiB, §27.4 control-frame bound);
//   - anything that is not one JSON object: truncated JSON, a bare value, or a
//     second value / trailing garbage after the object (checked by reading one
//     more token and requiring io.EOF — dec.More() reports false for a stray
//     '}' and would silently drop it);
//   - a top-level key other than "type" and "data", or a data key other than
//     resource_type/resource_id/after/client_request_id;
//   - a missing or non-"subscribe" type, a missing or non-object data;
//   - resource_type outside {project, run}; resource_id that is empty, over 128
//     bytes, has surrounding whitespace, or contains the scope separator ':'
//     (the wire scope is "project:<id>" / "run:<id>" per §20.2, and an id that
//     contains ':' could not be represented in it);
//   - after that is not a non-negative JSON integer (absent means 0; a decimal,
//     a string, null, a boolean or a value that overflows int64 is refused);
//   - client_request_id longer than 128 bytes or not a string.
func ParseSubscribeFrame(raw []byte) (SubscribeRequest, error) {
	if len(raw) == 0 {
		return SubscribeRequest{}, &SubscribeError{Reason: ReasonNotJSONObject}
	}
	if len(raw) > MaxSubscribeFrameBytes {
		return SubscribeRequest{}, &SubscribeError{Reason: ReasonFrameTooLarge}
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	// UseNumber keeps `after` as its literal digits: decoding into float64 would
	// turn a 2^53-sized integer into a different integer and 1.5 into a value
	// that has to be guessed at.
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil || !isDelim(tok, '{') {
		return SubscribeRequest{}, &SubscribeError{Reason: ReasonNotJSONObject}
	}

	var (
		req       SubscribeRequest
		frameType string
		typeSeen  bool
		dataSeen  bool
	)
	for dec.More() {
		key, err := objectKey(dec)
		if err != nil {
			return SubscribeRequest{}, err
		}
		switch key {
		case "type":
			if typeSeen {
				return SubscribeRequest{}, &SubscribeError{Reason: ReasonDuplicateField, Field: "type"}
			}
			typeSeen = true
			if err := dec.Decode(&frameType); err != nil {
				return SubscribeRequest{}, malformedOr(&SubscribeError{Reason: ReasonWrongFrameType, Field: "type"}, err)
			}
		case "data":
			if dataSeen {
				return SubscribeRequest{}, &SubscribeError{Reason: ReasonDuplicateField, Field: "data"}
			}
			dataSeen = true
			if err := parseSubscribeData(dec, &req); err != nil {
				return SubscribeRequest{}, err
			}
		default:
			// The key is not echoed: it is client text.
			return SubscribeRequest{}, &SubscribeError{Reason: ReasonUnknownField}
		}
	}
	if err := consumeObjectEnd(dec); err != nil {
		return SubscribeRequest{}, err
	}
	if err := requireFrameEnd(dec); err != nil {
		return SubscribeRequest{}, err
	}
	if !typeSeen || frameType != string(MsgTypeSubscribe) {
		return SubscribeRequest{}, &SubscribeError{Reason: ReasonWrongFrameType, Field: "type"}
	}
	if !dataSeen {
		return SubscribeRequest{}, &SubscribeError{Reason: ReasonInvalidData, Field: "data"}
	}
	return req, nil
}

// parseSubscribeData decodes the data object of a subscribe frame into req. The
// caller has just read the "data" key; this function consumes the object itself
// (including its closing brace).
func parseSubscribeData(dec *json.Decoder, req *SubscribeRequest) error {
	tok, err := dec.Token()
	if err != nil || !isDelim(tok, '{') {
		return &SubscribeError{Reason: ReasonInvalidData, Field: "data"}
	}

	var (
		resourceTypeSeen    bool
		resourceIDSeen      bool
		afterSeen           bool
		clientRequestIDSeen bool
	)
	for dec.More() {
		key, err := objectKey(dec)
		if err != nil {
			return err
		}
		switch key {
		case "resource_type":
			if resourceTypeSeen {
				return &SubscribeError{Reason: ReasonDuplicateField, Field: "data.resource_type"}
			}
			resourceTypeSeen = true
			var value string
			if err := dec.Decode(&value); err != nil {
				return &SubscribeError{Reason: ReasonInvalidResourceType, Field: "data.resource_type"}
			}
			if value != ResourceTypeProject && value != ResourceTypeRun {
				return &SubscribeError{Reason: ReasonInvalidResourceType, Field: "data.resource_type"}
			}
			req.ResourceType = value
		case "resource_id":
			if resourceIDSeen {
				return &SubscribeError{Reason: ReasonDuplicateField, Field: "data.resource_id"}
			}
			resourceIDSeen = true
			var value string
			if err := dec.Decode(&value); err != nil {
				return malformedOr(&SubscribeError{Reason: ReasonInvalidResourceID, Field: "data.resource_id"}, err)
			}
			if err := checkResourceID(value); err != nil {
				return err
			}
			req.ResourceID = value
		case "after":
			if afterSeen {
				return &SubscribeError{Reason: ReasonDuplicateField, Field: "data.after"}
			}
			afterSeen = true
			var value any
			if err := dec.Decode(&value); err != nil {
				return malformedOr(&SubscribeError{Reason: ReasonInvalidAfter, Field: "data.after"}, err)
			}
			after, err := checkAfter(value)
			if err != nil {
				return err
			}
			req.After = after
		case "client_request_id":
			if clientRequestIDSeen {
				return &SubscribeError{Reason: ReasonDuplicateField, Field: "data.client_request_id"}
			}
			clientRequestIDSeen = true
			var value string
			if err := dec.Decode(&value); err != nil {
				return malformedOr(&SubscribeError{Reason: ReasonInvalidClientRequestID, Field: "data.client_request_id"}, err)
			}
			if len(value) > maxSubscribeClientRequestIDBytes {
				return &SubscribeError{Reason: ReasonInvalidClientRequestID, Field: "data.client_request_id"}
			}
			req.ClientRequestID = value
		default:
			// The key is not echoed: it is client text.
			return &SubscribeError{Reason: ReasonUnknownField, Field: "data"}
		}
	}
	if err := consumeObjectEnd(dec); err != nil {
		return err
	}
	if !resourceTypeSeen {
		return &SubscribeError{Reason: ReasonInvalidResourceType, Field: "data.resource_type"}
	}
	if !resourceIDSeen {
		return &SubscribeError{Reason: ReasonInvalidResourceID, Field: "data.resource_id"}
	}
	// after and client_request_id are optional: an absent after means 0 (the
	// legal first replay of §20.2), an absent client_request_id means no
	// correlation.
	return nil
}

// malformedOr keeps a JSON-level failure from being reported as a bad value. A
// truncated frame ("…","resource_id":"run_88) makes json.Decoder return
// io.ErrUnexpectedEOF, which would otherwise be classified as "the resource id is
// invalid" — a misleading reason for a frame that is simply incomplete. The
// caller's field-level reason stands only when the value itself decoded; anything
// else is a malformed frame.
func malformedOr(fieldReason *SubscribeError, err error) error {
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrNoProgress) {
		return &SubscribeError{Reason: ReasonNotJSONObject}
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return &SubscribeError{Reason: ReasonNotJSONObject}
	}
	return fieldReason
}

// checkResourceID enforces the resource id rules: 1..128 bytes, no surrounding
// whitespace, and no ':' (which would make the id impossible to express in the
// wire scope "project:<id>" / "run:<id>").
func checkResourceID(value string) error {
	if value == "" || len(value) > maxSubscribeResourceIDBytes {
		return &SubscribeError{Reason: ReasonInvalidResourceID, Field: "data.resource_id"}
	}
	if strings.TrimSpace(value) != value {
		return &SubscribeError{Reason: ReasonInvalidResourceID, Field: "data.resource_id"}
	}
	if strings.Contains(value, ":") {
		return &SubscribeError{Reason: ReasonInvalidResourceID, Field: "data.resource_id"}
	}
	return nil
}

// checkAfter accepts a JSON integer >= 0 and nothing else. value is what
// json.Decoder produced with UseNumber: a json.Number for every JSON number, and
// something else (string, bool, nil, map, slice) for everything else.
func checkAfter(value any) (int64, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, &SubscribeError{Reason: ReasonInvalidAfter, Field: "data.after"}
	}
	after, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil {
		// A decimal ("41.0"), an exponent ("1e3") or an int64 overflow.
		return 0, &SubscribeError{Reason: ReasonInvalidAfter, Field: "data.after"}
	}
	if after < 0 {
		return 0, &SubscribeError{Reason: ReasonInvalidAfter, Field: "data.after"}
	}
	return after, nil
}

// isDelim reports whether tok is the expected JSON delimiter.
func isDelim(tok json.Token, want json.Delim) bool {
	delim, ok := tok.(json.Delim)
	return ok && delim == want
}

// objectKey reads the next key of an object being walked. Inside an object the
// token stream only yields strings for keys, so anything else is malformed JSON.
func objectKey(dec *json.Decoder) (string, error) {
	tok, err := dec.Token()
	if err != nil {
		return "", &SubscribeError{Reason: ReasonNotJSONObject}
	}
	key, ok := tok.(string)
	if !ok {
		return "", &SubscribeError{Reason: ReasonNotJSONObject}
	}
	return key, nil
}

// consumeObjectEnd reads the closing brace of the object just walked.
func consumeObjectEnd(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil || !isDelim(tok, '}') {
		return &SubscribeError{Reason: ReasonNotJSONObject}
	}
	return nil
}

// requireFrameEnd refuses a second JSON value or trailing garbage after the
// frame. dec.More() must not be used here: it reports false when the next byte is
// a closing '}' or ']', so `{...}}` would pass and the stray delimiter would be
// dropped without a trace (the same trap runstore.requireJSONEnd documents). The
// check therefore reads one more token and requires io.EOF.
func requireFrameEnd(dec *json.Decoder) error {
	if _, err := dec.Token(); err != io.EOF {
		return &SubscribeError{Reason: ReasonNotJSONObject}
	}
	return nil
}

// Scope is the resource a subscription is about, in the shape the wire's "scope"
// field uses: "project:<project_id>" or "run:<run_id>" (§20.2).
type Scope struct {
	// Kind is ResourceTypeProject or ResourceTypeRun.
	Kind string
	// ID is the project or run id the subscription follows.
	ID string
}

// String renders the scope the way the wire does (§20.2/§27.3: "run:run_88").
//
// An unknown kind or an empty id renders as the empty string: only a Grant built
// by Authorize reaches a frame, and a scope that is not project/run must never be
// minted into one.
func (s Scope) String() string {
	switch s.Kind {
	case ResourceTypeProject, ResourceTypeRun:
		if s.ID == "" {
			return ""
		}
		return s.Kind + ":" + s.ID
	default:
		return ""
	}
}

// Grant is what an authorized subscription may read: the project the connection
// belongs to, and the one scope it follows inside that project.
//
// Both facts live on the value on purpose. The scope decides the read and the
// sequence projection; ProjectID is the authorization fact every mapped event is
// checked against, so a reader that returns a row from somewhere else is caught
// here rather than sent to the client (§27.4: the server derives the scope from
// the authoritative resource and does not re-interpret a cursor under a filter
// the client supplied).
type Grant struct {
	ProjectID string
	Scope     Scope
}

// RunLookup answers "which project owns this run, if a run with this id exists".
//
// found=false is a fact about the store, not an authorization decision. A
// database failure is reported as an error instead, so a transient failure can
// never be mistaken for "no such run" and silently become a forbidden answer the
// client would read as "that run is not yours".
type RunLookup func(ctx context.Context, runID string) (projectID string, found bool, err error)

// RunstoreRunLookup resolves runs through the runtime library: GetRun, with a
// missing row reported as (found=false, nil) and every other failure passed on.
func RunstoreRunLookup(q runstore.Querier) RunLookup {
	return func(ctx context.Context, runID string) (string, bool, error) {
		r, err := runstore.GetRun(ctx, q, runID)
		if errors.Is(err, runstore.ErrNotFound) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		return r.ProjectID, true, nil
	}
}

// Authorizer decides whether a connection may subscribe the frame's resource.
//
// connectionProjectID is the project the route already verified for this
// connection (api/handlers/projects.go:StreamProjectEvents passes
// "project:"+id); it is the only project the connection may ever read.
type Authorizer struct {
	// Runs resolves a run id to its project. Required for resource_type=run; a
	// project subscription needs no lookup.
	Runs RunLookup
}

// Authorize grants the scope the connection may follow, or refuses it.
//
// The rules (§28 T1.12.a: "鉴权 project/run，授权 grant 限 scope"):
//
//   - resource_type=project: resource_id must equal connectionProjectID;
//   - resource_type=run: the run must exist AND its project_id must equal
//     connectionProjectID.
//
// Everything else returns ErrForbidden — unwrapped, with no detail, and the same
// value for every refusal, so "that run belongs to another project" and "that run
// does not exist" are indistinguishable to the client (§20.3: an invalid resource
// gets forbidden and no sensitive information beyond existence).
//
// A RunLookup failure is NOT forbidden: it is wrapped and returned as a
// distinguishable internal error, because the caller must close the connection or
// retry rather than tell the client its own run does not exist.
func (a Authorizer) Authorize(ctx context.Context, connectionProjectID string, req SubscribeRequest) (Grant, error) {
	switch req.ResourceType {
	case ResourceTypeProject:
		if req.ResourceID == "" || req.ResourceID != connectionProjectID {
			return Grant{}, ErrForbidden
		}
		return Grant{
			ProjectID: connectionProjectID,
			Scope:     Scope{Kind: ResourceTypeProject, ID: req.ResourceID},
		}, nil

	case ResourceTypeRun:
		if req.ResourceID == "" {
			return Grant{}, ErrForbidden
		}
		if a.Runs == nil {
			// A wiring bug, not an authorization decision: fail closed, but make
			// it visible instead of answering forbidden.
			return Grant{}, errors.New("websocket: authorize run subscription: no run lookup is configured")
		}
		projectID, found, err := a.Runs(ctx, req.ResourceID)
		if err != nil {
			return Grant{}, fmt.Errorf("websocket: authorize run subscription: %w", err)
		}
		if !found || projectID == "" || projectID != connectionProjectID {
			return Grant{}, ErrForbidden
		}
		return Grant{
			ProjectID: projectID,
			Scope:     Scope{Kind: ResourceTypeRun, ID: req.ResourceID},
		}, nil

	default:
		return Grant{}, ErrForbidden
	}
}

// EventReader is the store-side read the replay is built on. It is an interface
// so this package depends on two methods rather than on *runstore.Store, and so
// a caller can supply its own reader.
type EventReader interface {
	// ReplayProject reads one page of a project timeline on project_seq.
	ReplayProject(ctx context.Context, projectID string, after int64, limit int) (runstore.ReplayPage, error)
	// ReplayRun reads one page of a run timeline on run_seq.
	ReplayRun(ctx context.Context, runID string, after int64, limit int) (runstore.ReplayPage, error)
}

// runstoreEventReader adapts a runstore handle (or one of its transactions) to
// EventReader.
type runstoreEventReader struct {
	q runstore.Querier
}

// RunstoreEventReader reads replay pages through q.
//
// The page and its two boundaries come from ONE SQL statement inside runstore,
// which is what §27.4's "快照和 cursor 同一个 SQL snapshot" requires; this adapter
// adds no second read that could break that guarantee.
func RunstoreEventReader(q runstore.Querier) EventReader { return runstoreEventReader{q: q} }

// ReplayProject implements EventReader.
func (r runstoreEventReader) ReplayProject(ctx context.Context, projectID string, after int64, limit int) (runstore.ReplayPage, error) {
	return runstore.ReplayProject(ctx, r.q, projectID, after, limit)
}

// ReplayRun implements EventReader.
func (r runstoreEventReader) ReplayRun(ctx context.Context, runID string, after int64, limit int) (runstore.ReplayPage, error) {
	return runstore.ReplayRun(ctx, r.q, runID, after, limit)
}

// WireEvent is one ExecutionEvent as the wire carries it
// (schemas/execution-event.schema.json, §20.2).
//
// Every field is a property of that schema and there are no others, so an
// accidental key cannot appear on the wire;
// TestWireEventJSONKeysMatchExecutionEventSchema compares this struct's tags with
// the schema's properties/required in both directions.
//
// RunID carries no omitempty: a project-level event (no Run) travels as
// `"run_id":null`, which the schema's nullable type allows. A stable key set is
// worth more to the front end (T1.14) than a shorter frame.
type WireEvent struct {
	ID            string          `json:"id"`
	Scope         string          `json:"scope"`
	ProjectID     string          `json:"project_id"`
	RunID         *string         `json:"run_id"`
	Sequence      int64           `json:"sequence"`
	Type          string          `json:"type"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    string          `json:"occurred_at"`
	Identity      json.RawMessage `json:"identity"`
	Payload       json.RawMessage `json:"payload"`
}

// occurredAtLayout is the wire instant format: RFC3339 UTC with a fixed
// millisecond field ("2026-09-06T08:00:00.000Z").
//
// §20.2's example prints second precision ("2026-09-06T08:00:00Z"); both are
// RFC3339 UTC and the schema is `format: date-time`, which accepts either, so
// T1.12.a has to choose one (the choice is pinned by
// TestWireEventOccurredAtIsRFC3339UTCWithMilliseconds). The store keeps Unix
// milliseconds (§27.1), and a fixed millisecond field prints exactly what was
// stored: time.RFC3339 would round every instant down to the second (two events a
// millisecond apart would show the same time), while RFC3339Nano trims trailing
// zeros and would print two different shapes for one contract.
const occurredAtLayout = "2006-01-02T15:04:05.000Z07:00"

// ToWireEvent projects one stored event onto the wire under the grant that
// authorized the subscription.
//
// Sequence is projected by the subscription's scope and never mixed (§27.4): a
// project grant reads ProjectSeq, a run grant reads RunSeq. The scope field is
// the GRANT's scope, not the event's own — a project subscriber that receives a
// run's event sees "project:<id>" with the run id in run_id, so the two counters
// can never be confused for one another.
//
// It refuses, rather than sends, an event that does not belong to the grant:
// another project's event, a run event whose RunID is not the granted run, a
// project-level event (no Run) under a run grant, a run event with no run_seq, a
// sequence below 1 (the schema's minimum), an unknown type, an unexpected
// schema_version, an empty id, a zero occurred_at, or identity/payload that are
// not valid JSON. All of those are unreachable while the store's own validation
// holds, and each would be a protocol violation on the wire if it did happen, so
// the mapping fails loudly instead of emitting a frame the client would have to
// defend against.
func ToWireEvent(g Grant, ev runstore.Event) (WireEvent, error) {
	if g.ProjectID == "" || g.Scope.String() == "" {
		return WireEvent{}, fmt.Errorf("map event %s: grant is not a usable scope", ev.ID)
	}
	if ev.ID == "" {
		return WireEvent{}, errors.New("map event: event has no id")
	}
	if ev.ProjectID != g.ProjectID {
		return WireEvent{}, fmt.Errorf("map event %s: event belongs to another project than the subscription", ev.ID)
	}
	if !run.ExecutionEventType(ev.Type).Valid() {
		return WireEvent{}, fmt.Errorf("map event %s: %q is not an execution event type", ev.ID, ev.Type)
	}
	if ev.SchemaVersion != runstore.EventSchemaVersion {
		return WireEvent{}, fmt.Errorf("map event %s: schema_version %d is not %d", ev.ID, ev.SchemaVersion, runstore.EventSchemaVersion)
	}
	if ev.OccurredAt.IsZero() {
		return WireEvent{}, fmt.Errorf("map event %s: event has no occurred_at", ev.ID)
	}
	if len(ev.Identity) == 0 || !json.Valid(ev.Identity) {
		return WireEvent{}, fmt.Errorf("map event %s: identity is not valid JSON", ev.ID)
	}
	if len(ev.Payload) == 0 || !json.Valid(ev.Payload) {
		return WireEvent{}, fmt.Errorf("map event %s: payload is not valid JSON", ev.ID)
	}

	var sequence int64
	switch g.Scope.Kind {
	case ResourceTypeProject:
		sequence = ev.ProjectSeq
	case ResourceTypeRun:
		// A run subscription may only ever see the granted run's events. Both
		// halves are checked: RunID says whose event it is, RunSeq says where it
		// sits on that run's counter.
		if ev.RunID == nil || *ev.RunID != g.Scope.ID {
			return WireEvent{}, fmt.Errorf("map event %s: event is not part of the subscribed run", ev.ID)
		}
		if ev.RunSeq == nil {
			return WireEvent{}, fmt.Errorf("map event %s: event has no run sequence", ev.ID)
		}
		sequence = *ev.RunSeq
	default:
		return WireEvent{}, fmt.Errorf("map event %s: grant kind %q is not a scope", ev.ID, g.Scope.Kind)
	}
	if sequence < 1 {
		return WireEvent{}, fmt.Errorf("map event %s: sequence %d is below the schema minimum 1", ev.ID, sequence)
	}

	return WireEvent{
		ID:            ev.ID,
		Scope:         g.Scope.String(),
		ProjectID:     ev.ProjectID,
		RunID:         ev.RunID,
		Sequence:      sequence,
		Type:          ev.Type,
		SchemaVersion: ev.SchemaVersion,
		OccurredAt:    ev.OccurredAt.UTC().Format(occurredAtLayout),
		Identity:      ev.Identity,
		Payload:       ev.Payload,
	}, nil
}

// ReplayBatch is one replay page in wire form together with the boundaries that
// were read with it.
//
// NextAfter is the cursor the client holds after this page: the last delivered
// sequence, or the request's after when the page was empty. HasMore reports that
// the page was cut short by the limit rather than by the end of the retained
// history, so the caller keeps paging until it is false and only then sends
// replay_finished.
type ReplayBatch struct {
	Events         []WireEvent
	NextAfter      int64
	HasMore        bool
	HighWatermark  int64
	RetentionFloor int64
}

// ReadReplay reads one page of the granted scope and projects it onto the wire.
//
// limit follows the event store's rule (<= 0 means the default 200, above 1000 is
// clamped). The cursor rules are the store's and are passed through unchanged:
// errors.Is(err, runstore.ErrInvalidCursor) is the 422 invalid_cursor case
// (after > high watermark) and errors.Is(err, runstore.ErrCursorExpired) the 410
// cursor_expired case (after < retention_floor-1); the caller answers with
// InvalidCursorFrame/CursorExpiredFrame and, for the expired cursor, enters the
// snapshot recovery flow (§27.4).
//
// On the two cursor errors the returned batch carries no events but does carry
// HighWatermark, RetentionFloor and NextAfter=after: the store reads them in the
// same statement that judged the cursor, so InvalidCursorFrame and
// CursorExpiredFrame quote the numbers that decided the answer.
func ReadReplay(ctx context.Context, r EventReader, g Grant, after int64, limit int) (ReplayBatch, error) {
	if r == nil {
		return ReplayBatch{}, errors.New("websocket: read replay: no event reader")
	}
	if g.ProjectID == "" || g.Scope.String() == "" {
		return ReplayBatch{}, errors.New("websocket: read replay: grant is not a usable scope")
	}

	var (
		page runstore.ReplayPage
		err  error
	)
	switch g.Scope.Kind {
	case ResourceTypeProject:
		page, err = r.ReplayProject(ctx, g.Scope.ID, after, limit)
	case ResourceTypeRun:
		page, err = r.ReplayRun(ctx, g.Scope.ID, after, limit)
	default:
		return ReplayBatch{}, fmt.Errorf("websocket: read replay: grant kind %q is not a scope", g.Scope.Kind)
	}
	if err != nil {
		if errors.Is(err, runstore.ErrInvalidCursor) || errors.Is(err, runstore.ErrCursorExpired) {
			return ReplayBatch{
				NextAfter:      page.NextAfter,
				HighWatermark:  page.HighWatermark,
				RetentionFloor: page.RetentionFloor,
			}, err
		}
		return ReplayBatch{}, err
	}

	batch := ReplayBatch{
		NextAfter:      page.NextAfter,
		HasMore:        page.HasMore,
		HighWatermark:  page.HighWatermark,
		RetentionFloor: page.RetentionFloor,
	}
	if len(page.Events) == 0 {
		return batch, nil
	}
	batch.Events = make([]WireEvent, 0, len(page.Events))
	for _, ev := range page.Events {
		wire, err := ToWireEvent(g, ev)
		if err != nil {
			return ReplayBatch{}, err
		}
		batch.Events = append(batch.Events, wire)
	}
	return batch, nil
}

// frameEnvelope is the one shape every server frame has: {"type":…,"data":{…}}
// (§20.3). Data is a fixed struct per frame — never a map[string]interface{} —
// so a frame's key set is a compile-time fact pinned by the tests.
type frameEnvelope struct {
	Type MessageType `json:"type"`
	Data any         `json:"data"`
}

// encodeFrame marshals one frame.
//
// The fallback is unreachable for every payload this file builds: they are fixed
// structs of strings, int64s and RawMessages that ToWireEvent proved are valid
// JSON. It exists because these constructors are called from T1.12.b's write
// path, where a panic would take down the process: if an encoder bug ever did
// make a frame unmarshalable, the connection gets a fixed invalid_request frame
// (which says nothing about the input) instead.
func encodeFrame(t MessageType, data any) []byte {
	raw, err := json.Marshal(frameEnvelope{Type: t, Data: data})
	if err != nil {
		return []byte(`{"type":"invalid_request","data":{"client_request_id":"","code":"invalid_request","reason":"frame_encoding_failed"}}`)
	}
	return raw
}

// subscribedData is the data object of a subscribed frame.
type subscribedData struct {
	ClientRequestID string `json:"client_request_id"`
	Scope           string `json:"scope"`
	ResourceType    string `json:"resource_type"`
	ResourceID      string `json:"resource_id"`
	ProjectID       string `json:"project_id"`
}

// SubscribedFrame acknowledges a granted subscription (first frame of the §20.3
// order). It repeats the authorized resource so the client can prove the
// connection acknowledged the subscription it asked for — including the project,
// which is what makes cross-project isolation visible on the client side.
func SubscribedFrame(clientRequestID string, g Grant) []byte {
	return encodeFrame(MsgTypeSubscribed, subscribedData{
		ClientRequestID: clientRequestID,
		Scope:           g.Scope.String(),
		ResourceType:    g.Scope.Kind,
		ResourceID:      g.Scope.ID,
		ProjectID:       g.ProjectID,
	})
}

// replayStartedData is the data object of a replay_started frame.
type replayStartedData struct {
	ClientRequestID string `json:"client_request_id"`
	Scope           string `json:"scope"`
	After           int64  `json:"after"`
	HighWatermark   int64  `json:"high_watermark"`
	RetentionFloor  int64  `json:"retention_floor"`
}

// ReplayStartedFrame opens the replay window: after → high_watermark on the
// subscription's own counter, with the retention floor the read was made against.
// §27.4: the client switches to live events only after replay_finished, and every
// live event must carry a sequence greater than high_watermark.
func ReplayStartedFrame(clientRequestID, scope string, after, highWatermark, retentionFloor int64) []byte {
	return encodeFrame(MsgTypeReplayStarted, replayStartedData{
		ClientRequestID: clientRequestID,
		Scope:           scope,
		After:           after,
		HighWatermark:   highWatermark,
		RetentionFloor:  retentionFloor,
	})
}

// EventFrame wraps one wire event. The data object IS the execution event: its
// key set is the schema's (TestFrameShapes compares it with
// schemas/execution-event.schema.json's properties).
func EventFrame(event WireEvent) []byte {
	return encodeFrame(MsgTypeEvent, event)
}

// replayFinishedData is the data object of a replay_finished frame.
type replayFinishedData struct {
	ClientRequestID string `json:"client_request_id"`
	Scope           string `json:"scope"`
	HighWatermark   int64  `json:"high_watermark"`
	NextAfter       int64  `json:"next_after"`
}

// ReplayFinishedFrame closes the replay. next_after is the cursor the client
// holds afterwards — the last delivered sequence, or the subscription's after when
// nothing was delivered — i.e. exactly the value it must reconnect with.
func ReplayFinishedFrame(clientRequestID, scope string, highWatermark, nextAfter int64) []byte {
	return encodeFrame(MsgTypeReplayFinished, replayFinishedData{
		ClientRequestID: clientRequestID,
		Scope:           scope,
		HighWatermark:   highWatermark,
		NextAfter:       nextAfter,
	})
}

// forbiddenData is the data object of a forbidden frame.
type forbiddenData struct {
	ClientRequestID string `json:"client_request_id"`
	Code            string `json:"code"`
	Message         string `json:"message"`
}

// ForbiddenFrame refuses a subscription the connection may not make. It carries
// the client_request_id and two fixed strings and nothing else: no resource id,
// no project id, no reason — so the frame for "another project's run" and for "no
// such run" is the same bytes (§20.3).
func ForbiddenFrame(clientRequestID string) []byte {
	return encodeFrame(MsgTypeForbidden, forbiddenData{
		ClientRequestID: clientRequestID,
		Code:            CodeForbidden,
		Message:         forbiddenMessage,
	})
}

// cursorExpiredData is the data object of a cursor_expired frame.
type cursorExpiredData struct {
	ClientRequestID string `json:"client_request_id"`
	Scope           string `json:"scope"`
	Code            string `json:"code"`
	Message         string `json:"message"`
	Recovery        string `json:"recovery"`
	RetentionFloor  int64  `json:"retention_floor"`
}

// CursorExpiredFrame reports after < retention_floor-1 (§27.4 → 410): the events
// the cursor points at are gone, and the machine-readable answer is
// "recovery":"snapshot" — replace the local cache with a snapshot and resume from
// the cursor that snapshot carries. retention_floor states how much history this
// database still has. The snapshot URL is deliberately absent: T1.12.b/T1.14 own
// the snapshot endpoint and this file does not invent a URL for it.
func CursorExpiredFrame(clientRequestID, scope string, retentionFloor int64) []byte {
	return encodeFrame(MsgTypeCursorExpired, cursorExpiredData{
		ClientRequestID: clientRequestID,
		Scope:           scope,
		Code:            CodeCursorExpired,
		Message:         cursorExpiredMessage,
		Recovery:        RecoverySnapshot,
		RetentionFloor:  retentionFloor,
	})
}

// invalidCursorData is the data object of an invalid_cursor frame.
type invalidCursorData struct {
	ClientRequestID string `json:"client_request_id"`
	Scope           string `json:"scope"`
	Code            string `json:"code"`
	Message         string `json:"message"`
	HighWatermark   int64  `json:"high_watermark"`
}

// InvalidCursorFrame reports after > high_watermark (§27.4 → 422): the cursor
// names a sequence this scope never allocated, so it is a wrong position and not
// an empty answer. high_watermark is the largest sequence the scope has, which is
// what the client needs to correct the cursor it built.
func InvalidCursorFrame(clientRequestID, scope string, highWatermark int64) []byte {
	return encodeFrame(MsgTypeInvalidCursor, invalidCursorData{
		ClientRequestID: clientRequestID,
		Scope:           scope,
		Code:            CodeInvalidCursor,
		Message:         invalidCursorMessage,
		HighWatermark:   highWatermark,
	})
}

// invalidRequestData is the data object of an invalid_request frame.
type invalidRequestData struct {
	ClientRequestID string `json:"client_request_id"`
	Code            string `json:"code"`
	Reason          string `json:"reason"`
}

// InvalidRequestFrame answers a frame ParseSubscribeFrame refused. reason must be
// one of the fixed phrases of this file (SubscribeErrorReason produces them); the
// frame carries no copy of the client's bytes, and no scope — a rejected frame
// never got far enough to have one.
func InvalidRequestFrame(clientRequestID, reason string) []byte {
	return encodeFrame(MsgTypeInvalidRequest, invalidRequestData{
		ClientRequestID: clientRequestID,
		Code:            CodeInvalidRequest,
		Reason:          reason,
	})
}
