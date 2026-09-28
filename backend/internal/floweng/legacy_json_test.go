package floweng

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// Tests for the Flow document codec (T3.01.a, §28 "不能丢未识别但仍需兼容的字
// 段"): a stored document keeps every member this build does not recognize, a
// document with nothing extra encodes exactly as it did before, and the fixtures
// under testdata/legacy_flows round-trip. The rules themselves are documented in
// legacy_json.go.

const legacyFixtureDir = "testdata/legacy_flows"

// readLegacyFixture returns one fixture's bytes, verbatim.
func readLegacyFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(legacyFixtureDir, name+".json"))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return body
}

// The shadow types below repeat the fields of the document types with the same
// JSON tags and no methods. They are the reference for "what encoding/json
// would have written before this codec existed", and they deliberately do NOT
// carry an extras map: a shadow encodes only known fields, which is exactly the
// point of comparing against it.

type shadowFlow struct {
	ID                  string           `json:"id"`
	ProjectID           string           `json:"project_id"`
	SessionID           string           `json:"session_id,omitempty"`
	TemplateID          TemplateID       `json:"template_id"`
	Status              FlowStatus       `json:"status"`
	Kind                FlowKind         `json:"kind"`
	Revision            int64            `json:"revision"`
	BindingID           string           `json:"binding_id,omitempty"`
	TemplateRevision    int64            `json:"template_revision,omitempty"`
	ParentProjectFlowID string           `json:"parent_project_flow_id,omitempty"`
	Stages              []shadowStage    `json:"stages"`
	Loops               []shadowLoop     `json:"loops"`
	Artifacts           []shadowArtifact `json:"artifacts"`
	Events              []shadowEvent    `json:"events"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

type shadowStage struct {
	ID         string       `json:"id"`
	Type       StageType    `json:"type"`
	Name       string       `json:"name"`
	Canvas     string       `json:"canvas"`
	AgentID    string       `json:"agent_id,omitempty"`
	Status     StageStatus  `json:"status"`
	Optional   bool         `json:"optional"`
	SnapshotID string       `json:"snapshot_id,omitempty"`
	Gates      []shadowGate `json:"gates,omitempty"`
	Order      int          `json:"order"`
}

type shadowGate struct {
	ID     string            `json:"id"`
	Phase  GatePhase         `json:"phase"`
	Kind   GateKind          `json:"kind"`
	OnFail GateOnFail        `json:"on_fail,omitempty"`
	Config map[string]string `json:"config,omitempty"`
	Passed bool              `json:"passed"`
}

type shadowArtifact struct {
	ID         string         `json:"id"`
	StageID    string         `json:"stage_id"`
	Type       string         `json:"type"`
	Version    int            `json:"version"`
	Status     ArtifactStatus `json:"status"`
	CreatedBy  string         `json:"created_by,omitempty"`
	ContentRef string         `json:"content_ref,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

type shadowEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	StageID   string    `json:"stage_id,omitempty"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

type shadowLoop struct {
	From StageType `json:"from"`
	To   StageType `json:"to"`
}

// shadowOf converts a document to its shadow, field by field, so the compiler
// (not a test) is what notices a field being added to one side only.
func shadowOf(f *Flow) shadowFlow {
	sh := shadowFlow{
		ID: f.ID, ProjectID: f.ProjectID, SessionID: f.SessionID, TemplateID: f.TemplateID,
		Status: f.Status, Kind: f.Kind, Revision: f.Revision, BindingID: f.BindingID,
		TemplateRevision: f.TemplateRevision, ParentProjectFlowID: f.ParentProjectFlowID,
		CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt,
	}
	for _, s := range f.Stages {
		ss := shadowStage{
			ID: s.ID, Type: s.Type, Name: s.Name, Canvas: s.Canvas, AgentID: s.AgentID,
			Status: s.Status, Optional: s.Optional, SnapshotID: s.SnapshotID, Order: s.Order,
		}
		for _, g := range s.Gates {
			ss.Gates = append(ss.Gates, shadowGate{
				ID: g.ID, Phase: g.Phase, Kind: g.Kind, OnFail: g.OnFail, Config: g.Config, Passed: g.Passed,
			})
		}
		sh.Stages = append(sh.Stages, ss)
	}
	for _, l := range f.Loops {
		sh.Loops = append(sh.Loops, shadowLoop{From: l.From, To: l.To})
	}
	for _, a := range f.Artifacts {
		sh.Artifacts = append(sh.Artifacts, shadowArtifact{
			ID: a.ID, StageID: a.StageID, Type: a.Type, Version: a.Version, Status: a.Status,
			CreatedBy: a.CreatedBy, ContentRef: a.ContentRef, CreatedAt: a.CreatedAt,
		})
	}
	for _, e := range f.Events {
		sh.Events = append(sh.Events, shadowEvent{
			ID: e.ID, Type: e.Type, StageID: e.StageID, Message: e.Message, Timestamp: e.Timestamp,
		})
	}
	return sh
}

// sampleFlow is a fully populated document: every known field of every type has
// a non-zero value, plus one preserved member on each of the six node types
// (gate extras live on the stage's gates and are reached through the stage
// codec). It is the input of most tests here.
func sampleFlow() *Flow {
	ts := time.Date(2026, 9, 28, 10, 30, 0, 0, time.UTC)
	f := &Flow{
		ID:                  "11111111-1111-4111-8111-111111111111",
		ProjectID:           "proj-sample",
		SessionID:           "sess-sample",
		TemplateID:          TemplateNewProject,
		Status:              FlowStatusActive,
		Kind:                FlowKindProject,
		Revision:            3,
		BindingID:           "binding-1",
		TemplateRevision:    5,
		ParentProjectFlowID: "parent-flow-1",
		Stages: []Stage{
			{
				ID: "22222222-2222-4222-8222-222222222222", Type: StageTypeIdea, Name: "想法",
				Canvas: "intent", AgentID: "agent-idea", Status: StageStatusDone, Optional: true,
				SnapshotID: "snap-1",
				Gates: []Gate{{
					ID: "33333333-3333-4333-8333-333333333333", Phase: GatePhaseExit, Kind: GateKindAgentCheck,
					OnFail: GateOnFailEscalateDebate, Config: map[string]string{"approver": "user"}, Passed: true,
				}},
				Order: 0,
			},
		},
		Loops: []LoopEdge{{From: StageTypeCoding, To: StageTypeDesign}},
		Artifacts: []Artifact{{
			ID: "44444444-4444-4444-8444-444444444444", StageID: "22222222-2222-4222-8222-222222222222",
			Type: "intent_doc", Version: 2, Status: ArtifactStatusApproved,
			CreatedBy: ArtifactCreatorUser, ContentRef: "content://x", CreatedAt: ts,
		}},
		Events: []FlowEvent{{
			ID: "55555555-5555-4555-8555-555555555555", Type: "flow.created",
			StageID: "22222222-2222-4222-8222-222222222222", Message: "created", Timestamp: ts,
		}},
		CreatedAt: ts,
		UpdatedAt: ts,
	}
	f.extras = map[string]json.RawMessage{
		"flow_future": json.RawMessage(`{"a":[1,{"b":null}]}`),
		"Status":      json.RawMessage(`"a case variant"`),
		"big_number":  json.RawMessage(`12345678901234567890.5`),
	}
	f.Stages[0].extras = map[string]json.RawMessage{"stage_future": json.RawMessage(`"s"`)}
	f.Stages[0].Gates[0].extras = map[string]json.RawMessage{"gate_future": json.RawMessage(`[1,2]`)}
	f.Loops[0].extras = map[string]json.RawMessage{"loop_future": json.RawMessage(`true`)}
	f.Artifacts[0].extras = map[string]json.RawMessage{"artifact_future": json.RawMessage(`{"k":"v"}`)}
	f.Events[0].extras = map[string]json.RawMessage{"event_future": json.RawMessage(`"e"`)}
	return f
}

// knownOnlyFlow returns a document with no preserved members at all: a deep
// copy of sampleFlow with every extras map emptied. It is the input for the
// byte-identity tests, whose subject is the known half of the encoding.
func knownOnlyFlow(t *testing.T) *Flow {
	t.Helper()
	f := CloneFlowDocument(sampleFlow())
	f.extras = nil
	for i := range f.Stages {
		f.Stages[i].extras = nil
		for gi := range f.Stages[i].Gates {
			f.Stages[i].Gates[gi].extras = nil
		}
	}
	for i := range f.Loops {
		f.Loops[i].extras = nil
	}
	for i := range f.Artifacts {
		f.Artifacts[i].extras = nil
	}
	for i := range f.Events {
		f.Events[i].extras = nil
	}
	return f
}

// TestLegacyFlowJSONMatchesStandardMarshal is the byte-identity property: a
// document without extras encodes exactly the way encoding/json encodes the
// same value as a struct with the same fields and no methods. If a known field
// were dropped, reordered or lost its omitempty, this fails.
func TestLegacyFlowJSONMatchesStandardMarshal(t *testing.T) {
	cases := []struct {
		name string
		flow *Flow
	}{
		{"fully populated", knownOnlyFlow(t)},
		{"empty document", &Flow{}},
		{"engine-shaped", func() *Flow {
			f, err := NewInMemoryEngine(nil).Create(t.Context(), &CreateFlowRequest{ProjectID: "p"})
			if err != nil {
				t.Fatal(err)
			}
			return f
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.flow.extras) != 0 {
				t.Fatalf("case carries extras; it is meant to be an extras-free document")
			}
			got, err := json.Marshal(tc.flow)
			if err != nil {
				t.Fatalf("marshal flow: %v", err)
			}
			want, err := json.Marshal(shadowOf(tc.flow))
			if err != nil {
				t.Fatalf("marshal shadow: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("flow encoding differs from the standard encoding of the same fields:\n got %s\nwant %s", got, want)
			}
		})
	}

	// The parts of sampleFlow do carry extras, so each one is compared with them
	// stripped: the known half alone must still match the shadow's encoding.
	t.Run("parts match their own standard encodings", func(t *testing.T) {
		f := sampleFlow()
		sh := shadowOf(f)
		checks := []struct {
			name string
			doc  any
			want []byte
		}{
			{"stage", stripExtras(t, "stage", f), mustMarshal(t, sh.Stages[0])},
			{"gate", stripExtras(t, "gate", f), mustMarshal(t, sh.Stages[0].Gates[0])},
			{"artifact", stripExtras(t, "artifact", f), mustMarshal(t, sh.Artifacts[0])},
			{"event", stripExtras(t, "event", f), mustMarshal(t, sh.Events[0])},
			{"loop", stripExtras(t, "loop", f), mustMarshal(t, sh.Loops[0])},
		}
		for _, c := range checks {
			got, err := json.Marshal(c.doc)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, c.want) {
				t.Fatalf("%s encoding differs:\n got %s\nwant %s", c.name, got, c.want)
			}
		}
	})
}

// stripExtras returns the sample part named by name with its extras removed, so
// it can be compared against the shadow encoding.
func stripExtras(t *testing.T, name string, f *Flow) any {
	t.Helper()
	switch name {
	case "stage":
		s := f.Stages[0]
		s.extras = nil
		for i := range s.Gates {
			s.Gates[i].extras = nil
		}
		return s
	case "gate":
		g := f.Stages[0].Gates[0]
		g.extras = nil
		return g
	case "artifact":
		a := f.Artifacts[0]
		a.extras = nil
		return a
	case "event":
		e := f.Events[0]
		e.extras = nil
		return e
	case "loop":
		l := f.Loops[0]
		l.extras = nil
		return l
	}
	t.Fatalf("unknown part %s", name)
	return nil
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// TestLegacyFlowJSONMatchesStandardUnmarshal is the decoding property: a
// canonical document (no extras) decodes to the same value the standard decoder
// would produce for the same fields. The reference is a second decode through
// the shadow type.
func TestLegacyFlowJSONMatchesStandardUnmarshal(t *testing.T) {
	body := mustMarshal(t, shadowOf(sampleFlow()))

	var got Flow
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode flow: %v", err)
	}
	if len(got.extras) != 0 {
		t.Fatalf("a canonical document produced %d extras: %v", len(got.extras), got.extras)
	}
	var want shadowFlow
	if err := json.Unmarshal(body, &want); err != nil {
		t.Fatalf("decode shadow: %v", err)
	}
	if !reflect.DeepEqual(shadowOf(&got), want) {
		t.Fatalf("decoded flow differs from the standard decode:\n got %+v\nwant %+v", shadowOf(&got), want)
	}
}

// TestLegacyFlowJSONCarriesUnknownFields is the §28 rule itself: members this
// build does not know survive a decode/encode round trip byte for byte, at every
// level of the document, including values that would lose precision through a
// float64.
func TestLegacyFlowJSONCarriesUnknownFields(t *testing.T) {
	body := readLegacyFixture(t, "unknown_fields")

	var got Flow
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	// "Status" is a case variant of the known "status" key. It must not touch
	// the field, and it must not be lost either.
	if got.Status != FlowStatusActive {
		t.Fatalf("status = %q, want the canonical status (a case variant must not win, and must not be dropped)", got.Status)
	}
	if raw, ok := got.extras["Status"]; !ok {
		t.Fatalf("the case variant key was dropped: extras=%v", got.extras)
	} else if !bytes.Contains(raw, []byte("case variant")) {
		t.Fatalf(`extras["Status"] = %s, want the fixture's value`, raw)
	}

	// "Order" is a case variant of "order" on the second stage, and "Version" of
	// "version" on the artifact. Go's JSON decoding matches keys
	// case-insensitively, so without the codec's exact-key rule the value 99
	// would have landed in Stage.Order.
	if got.Stages[1].Order != 1 {
		t.Fatalf("stage order = %d, want the canonical order 1 (the case variant must not win)", got.Stages[1].Order)
	}
	if _, ok := got.Stages[1].extras["Order"]; !ok {
		t.Fatalf("stage case variant dropped: %v", got.Stages[1].extras)
	}
	if got.Artifacts[0].Version != 1 {
		t.Fatalf("artifact version = %d, want the canonical 1", got.Artifacts[0].Version)
	}
	if _, ok := got.Artifacts[0].extras["Version"]; !ok {
		t.Fatalf("artifact case variant dropped: %v", got.Artifacts[0].extras)
	}

	// Every unknown value came back as the bytes the document held, including
	// the members that are nested, empty, unicode-escaped, or numbers the
	// float64 the standard decoder would use cannot hold.
	want := []extraLevel{
		{"flow", map[string]json.RawMessage{
			"Status":          json.RawMessage(`"this case variant must stay an extra and must not become the flow status"`),
			"big_number":      json.RawMessage(`12345678901234567890.5`),
			"future_flow_key": json.RawMessage(`{"nested": {"deep": [1, 2, {"three": true}]}, "empty_obj": {}, "empty_arr": []}`),
			"unicode_escape":  json.RawMessage(`"中文 \"quoted\" \\backslash\\ é"`),
		}, nil},
		{"stage 1b1b1b1b-1b1b-4b1b-8b1b-1b1b1b1b1b1b", map[string]json.RawMessage{
			"future_stage_key": json.RawMessage(`"kept verbatim"`),
			"stage_array":      json.RawMessage(`[1, 2, 3]`),
		}, nil},
		{"gate 2c2c2c2c-2c2c-4c2c-8c2c-2c2c2c2c2c2c", map[string]json.RawMessage{
			"Gate":            json.RawMessage(`"gate-level case variant stays an extra"`),
			"future_gate_key": json.RawMessage(`[{"a": 1}, null, "x"]`),
			"precise":         json.RawMessage(`0.1000000000000000055511151231257827`),
		}, nil},
		{"stage 3d3d3d3d-3d3d-4d3d-8d3d-3d3d3d3d3d3d", map[string]json.RawMessage{
			"Order": json.RawMessage(`99`),
		}, nil},
		{"loop edge", map[string]json.RawMessage{
			"future_loop_key": json.RawMessage(`{"reason": "quality gate"}`),
		}, nil},
		{"artifact 5f5f5f5f-5f5f-4f5f-8f5f-5f5f5f5f5f5f", map[string]json.RawMessage{
			"Version":             json.RawMessage(`99`),
			"future_artifact_key": json.RawMessage(`{"checksum": "sha256:abc", "size": 12345}`),
		}, nil},
		{"event 6a6a6a6a-6a6a-4a6a-8a6a-6a6a6a6a6a6a", map[string]json.RawMessage{
			"future_event_key": json.RawMessage(`{"trace_id": "trace-1", "spans": [{"name": "create", "ms": 12.5}]}`),
		}, nil},
	}
	for _, l := range want {
		first, ok := findExtraLevel(t, &got, l.name)
		if !ok {
			t.Fatalf("%s: no such node in the decoded document", l.name)
		}
		for key, wantRaw := range l.first {
			gotRaw, ok := first[key]
			if !ok {
				t.Fatalf("%s: unknown member %q was dropped (kept: %v)", l.name, key, keysOf(first))
			}
			if !bytes.Equal(gotRaw, wantRaw) {
				t.Fatalf("%s: unknown member %q changed\n got %s\nwant %s", l.name, key, gotRaw, wantRaw)
			}
		}
		if len(first) != len(l.first) {
			t.Fatalf("%s: kept %d members %v, want %d %v", l.name, len(first), keysOf(first), len(l.first), keysOf(l.first))
		}
	}

	encoded, err := json.Marshal(&got)
	if err != nil {
		t.Fatalf("encode flow: %v", err)
	}
	// The values come out of the encoding with the digits and the characters
	// they had. json.Marshal compacts what a MarshalJSON returns, so a value
	// written with spaces comes back without them; the two members that matter
	// for fidelity — a number longer than a float64, a nested object — are
	// checked for exactly what must survive, not for the spelling of a space.
	for _, raw := range []string{
		`12345678901234567890.5`,
		`0.1000000000000000055511151231257827`,
		`{"trace_id":"trace-1","spans":[{"name":"create","ms":12.5}]}`,
		`"中文 \"quoted\" \\backslash\\ é"`,
		`"future_stage_key":"kept verbatim"`,
	} {
		if !bytes.Contains(encoded, []byte(raw)) {
			t.Fatalf("re-encoded document lost %s\nencoded: %s", raw, encoded)
		}
	}

	// Extras are appended in key order, so the encoding is deterministic.
	again, err := json.Marshal(&got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, again) {
		t.Fatal("encoding the same document twice produced different bytes")
	}

	// A second round trip is stable: the same bytes, and nothing lost on the way
	// back in. What the first pass did to the preserved values — removing the
	// insignificant whitespace json.Marshal removes from every MarshalJSON
	// result — must not happen a second time, and no key may disappear.
	var twice Flow
	if err := json.Unmarshal(encoded, &twice); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	thrice, err := json.Marshal(&twice)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, thrice) {
		t.Fatalf("round trip is not stable:\n once %s\ntwice %s", encoded, thrice)
	}
	if !reflect.DeepEqual(shadowOf(&got), shadowOf(&twice)) {
		t.Fatalf("known fields changed on round trip:\n once %+v\ntwice %+v", shadowOf(&got), shadowOf(&twice))
	}
	// Every level keeps the same members with the same values. The comparison is
	// by value where the first pass rewrote the bytes — json.Compact removes
	// whitespace inside a nested value, which Go's RawMessage does not protect
	// (go.dev/issue/67551) — and by bytes where it did not.
	for _, c := range extraLevels(&got, &twice) {
		if len(c.first) != len(c.second) {
			t.Fatalf("%s: extra count changed on round trip:\n once %v\ntwice %v", c.name, keysOf(c.first), keysOf(c.second))
		}
		for key, want := range c.first {
			gotValue, ok := c.second[key]
			if !ok {
				t.Fatalf("%s: extra %q disappeared on round trip", c.name, key)
			}
			if !sameJSONValue(t, want, gotValue) {
				t.Fatalf("%s: extra %q changed on round trip:\n once %s\ntwice %s", c.name, key, want, gotValue)
			}
			if !hasInsignificantWhitespace(want) && !bytes.Equal(want, gotValue) {
				t.Fatalf("%s: extra %q was respelled on round trip:\n once %s\ntwice %s", c.name, key, want, gotValue)
			}
		}
	}
}

// hasInsignificantWhitespace reports whether a raw JSON value is written with
// whitespace a compactor would remove.
func hasInsignificantWhitespace(raw json.RawMessage) bool {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return false
	}
	return !bytes.Equal(compact.Bytes(), raw)
}

// sameJSONValue reports whether two raw values carry the same JSON value. It is
// used where the codec's byte-level promise is not the question — after a pass
// through json.Marshal, which normalises the whitespace and the escaping of the
// bytes a MarshalJSON returned (see legacy_json.go) — and where the question is
// whether anything was lost.
func sameJSONValue(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		t.Fatalf("unmarshal %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	return reflect.DeepEqual(va, vb)
}

// findExtraLevel returns the preserved members of the node named by name ("flow",
// "stage <id>", "gate <id>", "artifact <id>", "event <id>", "loop edge").
func findExtraLevel(t *testing.T, f *Flow, name string) (map[string]json.RawMessage, bool) {
	t.Helper()
	switch {
	case name == "flow":
		return f.extras, true
	case name == "loop edge":
		if len(f.Loops) != 1 {
			return nil, false
		}
		return f.Loops[0].extras, true
	case strings.HasPrefix(name, "stage "):
		id := strings.TrimPrefix(name, "stage ")
		for i := range f.Stages {
			if f.Stages[i].ID == id {
				return f.Stages[i].extras, true
			}
		}
	case strings.HasPrefix(name, "gate "):
		id := strings.TrimPrefix(name, "gate ")
		for si := range f.Stages {
			for gi := range f.Stages[si].Gates {
				if f.Stages[si].Gates[gi].ID == id {
					return f.Stages[si].Gates[gi].extras, true
				}
			}
		}
	case strings.HasPrefix(name, "artifact "):
		id := strings.TrimPrefix(name, "artifact ")
		for i := range f.Artifacts {
			if f.Artifacts[i].ID == id {
				return f.Artifacts[i].extras, true
			}
		}
	case strings.HasPrefix(name, "event "):
		id := strings.TrimPrefix(name, "event ")
		for i := range f.Events {
			if f.Events[i].ID == id {
				return f.Events[i].extras, true
			}
		}
	}
	return nil, false
}

// keysOf lists the keys of a preserved-members map, in map order (for messages).
func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// sortedKeysOf lists the keys of a string-keyed map in order (for messages).
func sortedKeysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedAnyKeys lists the keys of a decoded JSON object in order.
func sortedAnyKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// fixtureKeys collects every member name a document uses, at every level, so a
// test can say "this fixture has no such key anywhere" without searching for a
// substring of the file.
func fixtureKeys(body []byte) (map[string]bool, error) {
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch node := v.(type) {
		case map[string]any:
			for k, child := range node {
				keys[k] = true
				walk(child)
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(doc)
	return keys, nil
}

// TestLegacyFlowJSONAliasesMirrorFields pins each alias to its document type by
// reflection, because the compiler only sees the aliases the codec converts
// directly: stageJSON is built field by field (its Gates member is raw), so a
// stage field added to Stage and to stageJSON is checked here instead.
func TestLegacyFlowJSONAliasesMirrorFields(t *testing.T) {
	type pair struct {
		name  string
		doc   any
		alias any
	}
	pairs := []pair{
		{"Flow", Flow{}, flowJSON{}},
		{"Stage", Stage{}, stageJSON{}},
		{"Gate", Gate{}, gateJSON{}},
		{"Artifact", Artifact{}, artifactJSON{}},
		{"FlowEvent", FlowEvent{}, flowEventJSON{}},
		{"LoopEdge", LoopEdge{}, loopEdgeJSON{}},
	}
	for _, p := range pairs {
		doc := reflect.TypeOf(p.doc)
		alias := reflect.TypeOf(p.alias)
		if doc.NumField() != alias.NumField() {
			t.Errorf("%s: %d fields, %s has %d", p.name, doc.NumField(), alias.Name(), alias.NumField())
			continue
		}
		for i := 0; i < doc.NumField(); i++ {
			df, af := doc.Field(i), alias.Field(i)
			if df.Name != af.Name {
				t.Errorf("%s: field %d is %s in the type and %s in %s", p.name, i, df.Name, af.Name, alias.Name())
				continue
			}
			if df.Tag != af.Tag {
				t.Errorf("%s.%s: tag %q vs %q", p.name, df.Name, df.Tag, af.Tag)
			}
			if df.Type != af.Type && df.Name != "Gates" {
				t.Errorf("%s.%s: type %s vs %s", p.name, df.Name, df.Type, af.Type)
			}
		}
	}
}

// TestLegacyFlowJSONKnownKeysMatchTags pins the known-key sets to the struct
// tags, in both directions: a tag without a key would drop the member it names,
// and a key without a tag could never reach a field.
func TestLegacyFlowJSONKnownKeysMatchTags(t *testing.T) {
	cases := []struct {
		name  string
		typ   reflect.Type
		known map[string]bool
	}{
		{"Flow", reflect.TypeOf(flowJSON{}), flowKnownMembers()},
		{"Stage", reflect.TypeOf(stageJSON{}), stageKnownMembers()},
		{"Gate", reflect.TypeOf(gateJSON{}), gateKnownMembers()},
		{"Artifact", reflect.TypeOf(artifactJSON{}), artifactKnownMembers()},
		{"FlowEvent", reflect.TypeOf(flowEventJSON{}), flowEventKnownMembers()},
		{"LoopEdge", reflect.TypeOf(loopEdgeJSON{}), loopEdgeKnownMembers()},
	}
	for _, c := range cases {
		tags := map[string]bool{}
		for i := 0; i < c.typ.NumField(); i++ {
			f := c.typ.Field(i)
			if f.Tag == "" {
				continue
			}
			tag := strings.Split(string(f.Tag), `"`)[1]
			if tag == "-" {
				continue
			}
			tags[strings.Split(tag, ",")[0]] = true
		}
		for tag := range tags {
			if !c.known[tag] {
				t.Errorf("%s: tag %q is not a known key", c.name, tag)
			}
		}
		for key := range c.known {
			if !tags[key] {
				t.Errorf("%s: known key %q has no field", c.name, key)
			}
		}
	}
}

// TestLegacyFlowJSONDefaults covers the two defaults a stored document does not
// carry itself, and the one value that is refused.
func TestLegacyFlowJSONDefaults(t *testing.T) {
	t.Run("missing kind and revision mean project at revision 1", func(t *testing.T) {
		var f Flow
		if err := json.Unmarshal(readLegacyFixture(t, "v0_minimal"), &f); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if f.Kind != FlowKindProject {
			t.Fatalf("kind = %q, want %q", f.Kind, FlowKindProject)
		}
		if f.Revision != 1 {
			t.Fatalf("revision = %d, want 1", f.Revision)
		}
	})

	t.Run("an explicit revision 0 reads as 1", func(t *testing.T) {
		var f Flow
		if err := json.Unmarshal([]byte(`{"id":"f","revision":0}`), &f); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if f.Revision != 1 {
			t.Fatalf("revision = %d, want 1", f.Revision)
		}
	})

	t.Run("a task flow keeps its kind", func(t *testing.T) {
		var f Flow
		if err := json.Unmarshal([]byte(`{"id":"f","kind":"task","revision":2}`), &f); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if f.Kind != FlowKindTask || f.Revision != 2 {
			t.Fatalf("kind=%q revision=%d", f.Kind, f.Revision)
		}
	})

	t.Run("an unknown kind is refused, not guessed", func(t *testing.T) {
		var f Flow
		err := json.Unmarshal([]byte(`{"id":"f","kind":"saga"}`), &f)
		if err == nil {
			t.Fatal("an unknown kind was accepted")
		}
		if !strings.Contains(err.Error(), "saga") {
			t.Fatalf("error does not name the kind: %v", err)
		}
	})

	t.Run("a negative revision is refused", func(t *testing.T) {
		var f Flow
		if err := json.Unmarshal([]byte(`{"id":"f","revision":-1}`), &f); err == nil {
			t.Fatal("a negative revision was accepted")
		}
	})
}

// flowMembers reads a document body the way the Flow decoder does, so a test
// case can ask the reader itself what it thinks of a body.
func flowMembers(body []byte) error {
	_, err := parseObjectMembers(body)
	return err
}

// stageDocument reads a stage body the way the Stage decoder does.
func stageDocument(body []byte) error {
	var s Stage
	return loadStageDocument(body, &s)
}

// gateDocument reads a gate body the way the stage decoder reads one.
func gateDocument(body []byte) error {
	var g Gate
	return loadGateDocument(body, &g)
}

// TestLegacyFlowJSONRejectsUnreadableDocuments covers the shapes the codec must
// refuse rather than guess at: a duplicate key in one object, a body that is not
// one object, and content after the object (the Decoder.More() trap).
func TestLegacyFlowJSONRejectsUnreadableDocuments(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
		// decode runs the rule the case is about. A document's own members are
		// read by parseObjectMembers; a member of a nested object is read by
		// that object's type, so a duplicate inside a stage is the stage
		// decoder's to refuse (and its own body is that object alone).
		decode func(body []byte) error
		// readerBody, when set, is the inner object the reader is asked about.
		readerBody string
	}{
		{"duplicate key", string(readLegacyFixture(t, "duplicate_key")), "duplicate JSON key", flowMembers, ""},
		{"not an object", `[]`, "not a JSON object", flowMembers, ""},
		{"not JSON", `not json`, "read JSON object", flowMembers, ""},
		{"trailing content", `{"id":"a"}{"id":"b"}`, "trailing content", flowMembers, ""},
		{"trailing brace", `{"id":"a"}}`, "trailing content", flowMembers, ""},
		{"trailing whitespace and value", `{"id":"a"} 1`, "trailing content", flowMembers, ""},
		{"duplicate key inside a stage", `{"stages":[{"id":"a","id":"b"}]}`, "duplicate JSON key",
			stageDocument, `{"id":"a","id":"b"}`},
		{"duplicate key inside a gate", `{"stages":[{"gates":[{"passed":true,"passed":false}]}]}`, "duplicate JSON key",
			gateDocument, `{"passed":true,"passed":false}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// DecodeFlowDocument first: the entry point a caller uses must
			// refuse the whole document.
			var f Flow
			if err := DecodeFlowDocument([]byte(c.body), &f); err == nil {
				t.Fatalf("DecodeFlowDocument accepted: %s", c.body)
			}
			// And the rule's own reader refuses the level the case is about, for
			// the reason named. It is asked separately because json.Unmarshal
			// validates the whole input with checkValid before any custom
			// decoder runs — so for a body that is not valid JSON at all the
			// error DecodeFlowDocument surfaces is encoding/json's, while the
			// reader's own error says what was actually wrong.
			at := c.body
			if c.readerBody != "" {
				at = c.readerBody
			}
			if err := c.decode([]byte(at)); err == nil {
				t.Fatalf("the reader accepted: %s", at)
			} else if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("reader error %q does not mention %q", err, c.want)
			}
		})
	}

	t.Run("a case variant next to the canonical key is an extra, not a duplicate", func(t *testing.T) {
		// "status" and "Status" are two different keys, so the document is
		// readable: the canonical one is the field and the variant is carried
		// (TestLegacyFlowJSONCarriesUnknownFields checks the same rule against
		// the fixture). A document that spelled one key twice is the hazard, and
		// the loop above covers that case.
		var f Flow
		if err := DecodeFlowDocument([]byte(`{"status":"active","Status":"done"}`), &f); err != nil {
			t.Fatalf("two differently spelled keys must both be readable: %v", err)
		}
		if f.Status != FlowStatusActive {
			t.Fatalf("status = %q, want the canonical value", f.Status)
		}
		if string(f.extras["Status"]) != `"done"` {
			t.Fatalf(`extras["Status"] = %s`, f.extras["Status"])
		}
	})

	t.Run("trailing whitespace is not content", func(t *testing.T) {
		var f Flow
		if err := DecodeFlowDocument([]byte("{\"id\":\"a\",\"kind\":\"project\"}\n\t "), &f); err != nil {
			t.Fatalf("a document with trailing whitespace was refused: %v", err)
		}
		if f.ID != "a" {
			t.Fatalf("id = %q", f.ID)
		}
	})

	t.Run("nested unreadable gate names its position", func(t *testing.T) {
		var f Flow
		err := DecodeFlowDocument([]byte(`{"stages":[{"gates":[{"id":"a"},{"id":"b","id":"c"}]}]}`), &f)
		if err == nil {
			t.Fatal("accepted a stage with an unreadable gate")
		}
		if !strings.Contains(err.Error(), "gate 1") {
			t.Fatalf("error does not name the gate index: %v", err)
		}
	})

	t.Run("a nil destination is refused rather than panicking", func(t *testing.T) {
		if err := DecodeFlowDocument([]byte(`{"id":"a"}`), nil); err == nil {
			t.Fatal("decode into nil succeeded")
		}
	})
}

// TestLegacyFlowJSONPreservesUnknownValuesByteForByte states the byte-level
// promise of the codec, measured rather than assumed: what the decoder preserves
// are the document's own bytes (no float64 round trip, no reformatting), and
// what the encoder then writes is those bytes — unless json.Marshal's own
// normalisation of a MarshalJSON result rewrites them, which it does for
// insignificant whitespace and for '<', '>' and '&'. Values that mean what they
// say are therefore byte-identical across the codec, and the rest are compared by
// value.
func TestLegacyFlowJSONPreservesUnknownValuesByteForByte(t *testing.T) {
	body := readLegacyFixture(t, "unknown_fields")

	var got Flow
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	t.Run("decoding keeps the document's bytes", func(t *testing.T) {
		// The fixture's awkward values, exactly as the file spells them. The two
		// that mean something special — the number longer than a float64 and the
		// nested object written with spaces — prove no parsing happened.
		want := map[string]string{
			"Status":          `"this case variant must stay an extra and must not become the flow status"`,
			"big_number":      `12345678901234567890.5`,
			"future_flow_key": `{"nested": {"deep": [1, 2, {"three": true}]}, "empty_obj": {}, "empty_arr": []}`,
			"unicode_escape":  `"中文 \"quoted\" \\backslash\\ é"`,
		}
		if len(got.extras) != len(want) {
			t.Fatalf("flow extras = %v, want %v", keysOf(got.extras), sortedKeysOf(want))
		}
		for key, wantRaw := range want {
			if string(got.extras[key]) != wantRaw {
				t.Fatalf("extras[%q]:\n got %s\nwant %s", key, got.extras[key], wantRaw)
			}
		}
		gate := got.Stages[0].Gates[0]
		if string(gate.extras["precise"]) != `0.1000000000000000055511151231257827` {
			t.Fatalf("gate extras[precise] = %s", gate.extras["precise"])
		}
		if string(gate.extras["future_gate_key"]) != `[{"a": 1}, null, "x"]` {
			t.Fatalf("gate extras[future_gate_key] = %s", gate.extras["future_gate_key"])
		}
	})

	t.Run("the preserved number is not the float64 of it", func(t *testing.T) {
		// Why the two constants above are exact: through the float64 an
		// interface{} would use, the first would print as ...68000 and the
		// second as 0.1.
		var asFloat float64
		if err := json.Unmarshal(got.extras["big_number"], &asFloat); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%v", asFloat) == "1.2345678901234568e+19" {
			t.Fatal("the test value does not survive a float64, so it cannot prove anything")
		}
	})

	t.Run("encoding writes them through", func(t *testing.T) {
		encoded, err := json.Marshal(&got)
		if err != nil {
			t.Fatal(err)
		}
		for _, raw := range []string{
			`12345678901234567890.5`,
			`0.1000000000000000055511151231257827`,
			`[{"a":1},null,"x"]`, // compacted by json.Marshal, value intact
			`{"nested":{"deep":[1,2,{"three":true}]},"empty_obj":{},"empty_arr":[]}`,
			`"中文 \"quoted\" \\backslash\\ é"`,
			`"future_stage_key":"kept verbatim"`,
		} {
			if !bytes.Contains(encoded, []byte(raw)) {
				t.Fatalf("encoding lost %s:\n%s", raw, encoded)
			}
		}
	})
}

// TestLegacyFlowJSONClonesExtras proves the copy handed out by the package is a
// deep copy of the preserved members too: editing an extra on the copy cannot
// reach the original, at every level.
func TestLegacyFlowJSONClonesExtras(t *testing.T) {
	orig := sampleFlow()
	cp := CloneFlowDocument(orig)

	// Change every extra on the copy, through the maps the clone owns.
	cp.extras["flow_future"] = json.RawMessage(`"changed"`)
	cp.extras["flow_only_on_copy"] = json.RawMessage(`1`)
	delete(cp.extras, "big_number")
	cp.Stages[0].extras["stage_future"] = json.RawMessage(`"changed"`)
	cp.Stages[0].Gates[0].extras["gate_future"] = json.RawMessage(`"changed"`)
	cp.Loops[0].extras["loop_future"] = json.RawMessage(`"changed"`)
	cp.Artifacts[0].extras["artifact_future"] = json.RawMessage(`"changed"`)
	cp.Events[0].extras["event_future"] = json.RawMessage(`"changed"`)

	want := sampleFlow()
	if !reflect.DeepEqual(orig.extras, want.extras) {
		t.Fatalf("cloning changed the original's extras:\n got %v\nwant %v", orig.extras, want.extras)
	}
	levels := []struct {
		name string
		got  map[string]json.RawMessage
		want map[string]json.RawMessage
	}{
		{"stem", orig.extras, want.extras},
		{"stage", orig.Stages[0].extras, want.Stages[0].extras},
		{"gate", orig.Stages[0].Gates[0].extras, want.Stages[0].Gates[0].extras},
		{"loop", orig.Loops[0].extras, want.Loops[0].extras},
		{"artifact", orig.Artifacts[0].extras, want.Artifacts[0].extras},
		{"event", orig.Events[0].extras, want.Events[0].extras},
	}
	for _, l := range levels {
		if !reflect.DeepEqual(l.got, l.want) {
			t.Fatalf("the original's %s extras changed through the copy:\n got %v\nwant %v", l.name, l.got, l.want)
		}
	}

	// Changing the bytes of one value must not reach the original either.
	cp2 := CloneFlowDocument(orig)
	raw := cp2.extras["big_number"]
	for i := range raw {
		raw[i] = '0'
	}
	if !reflect.DeepEqual(orig.extras["big_number"], want.extras["big_number"]) {
		t.Fatalf("editing a copied value changed the original: %s", orig.extras["big_number"])
	}
}

// TestLegacyGateHasNoJSONMethods pins the constraint that keeps the flattened
// gate rows of api/handlers a gate *row*: a struct that embeds Gate must not
// inherit a JSON method, or encoding/json would treat the whole row as a gate
// and serialize only the gate.
func TestLegacyGateHasNoJSONMethods(t *testing.T) {
	type gateRow struct {
		StageID   string    `json:"stage_id"`
		StageType StageType `json:"stage_type"`
		Gate
	}
	row := gateRow{StageID: "s1", StageType: StageTypeIdea,
		Gate: Gate{ID: "g1", Phase: GatePhaseExit, Kind: GateKindAuto, Passed: true}}

	data, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal gate row: %v", err)
	}
	for _, want := range []string{`"stage_id":"s1"`, `"stage_type":"idea"`, `"id":"g1"`, `"passed":true`} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("gate row lost %s: %s", want, data)
		}
	}

	var back gateRow
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("decode gate row: %v", err)
	}
	if back.StageID != "s1" || back.StageType != StageTypeIdea || back.Gate.ID != "g1" {
		t.Fatalf("gate row did not round trip: %+v", back)
	}
}

// TestLegacyFlowFixturesRoundTrip walks every fixture: it must decode, re-encode
// and decode again to the same document, and the encoding must be stable. The
// fixtures are the plan's migration inputs, so this is the test that says the
// codec can read what the plan says has to be read.
func TestLegacyFlowFixturesRoundTrip(t *testing.T) {
	for _, name := range []string{"v0_minimal", "current", "unknown_fields"} {
		t.Run(name, func(t *testing.T) {
			body := readLegacyFixture(t, name)

			var first Flow
			if err := json.Unmarshal(body, &first); err != nil {
				t.Fatalf("decode: %v", err)
			}
			encoded, err := json.Marshal(&first)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}

			var second Flow
			if err := json.Unmarshal(encoded, &second); err != nil {
				t.Fatalf("re-decode: %v", err)
			}
			reencoded, err := json.Marshal(&second)
			if err != nil {
				t.Fatalf("re-encode: %v", err)
			}
			if !bytes.Equal(encoded, reencoded) {
				t.Fatalf("round trip is not stable:\n once %s\ntwice %s", encoded, reencoded)
			}
			if !reflect.DeepEqual(shadowOf(&first), shadowOf(&second)) {
				t.Fatalf("known fields changed on round trip:\n once %+v\ntwice %+v", shadowOf(&first), shadowOf(&second))
			}
			// Compared per level, by value: a preserved value may lose the
			// whitespace a formatter gave it (json.Marshal compacts what a
			// MarshalJSON returns) but must never lose its key, its precision or
			// the fidelity of a string.
			for _, c := range extraLevels(&first, &second) {
				if len(c.first) != len(c.second) {
					t.Fatalf("%s extras changed on round trip: %v then %v", c.name, keysOf(c.first), keysOf(c.second))
				}
				for key, want := range c.first {
					gotValue, ok := c.second[key]
					if !ok {
						t.Fatalf("%s: extra %q disappeared on round trip", c.name, key)
					}
					if !sameJSONValue(t, want, gotValue) {
						t.Fatalf("%s: extra %q changed on round trip:\n once %s\ntwice %s", c.name, key, want, gotValue)
					}
				}
			}
		})
	}
}

// extraLevel pairs the preserved members of one node of two documents, so a
// test can compare them and name the node that differs.
type extraLevel struct {
	name          string
	first, second map[string]json.RawMessage
}

// extraLevels lists the extras of every level of two documents, in document
// order, paired position by position.
func extraLevels(a, b *Flow) []extraLevel {
	levels := []extraLevel{{"flow", a.extras, b.extras}}
	pair := func(name string, first, second map[string]json.RawMessage, ok bool) {
		if ok {
			levels = append(levels, extraLevel{name, first, second})
		}
	}
	for i := range a.Stages {
		if i >= len(b.Stages) {
			break
		}
		pair("stage "+a.Stages[i].ID, a.Stages[i].extras, b.Stages[i].extras, true)
		for gi := range a.Stages[i].Gates {
			if gi >= len(b.Stages[i].Gates) {
				break
			}
			pair("gate "+a.Stages[i].Gates[gi].ID,
				a.Stages[i].Gates[gi].extras, b.Stages[i].Gates[gi].extras, true)
		}
	}
	for i := range a.Artifacts {
		if i >= len(b.Artifacts) {
			break
		}
		pair("artifact "+a.Artifacts[i].ID, a.Artifacts[i].extras, b.Artifacts[i].extras, true)
	}
	for i := range a.Events {
		if i >= len(b.Events) {
			break
		}
		pair("event "+a.Events[i].ID, a.Events[i].extras, b.Events[i].extras, true)
	}
	for i := range a.Loops {
		if i >= len(b.Loops) {
			break
		}
		pair("loop edge", a.Loops[i].extras, b.Loops[i].extras, true)
	}
	return levels
}

// TestLegacyFlowCurrentFixtureIsTheDocumentWeWrite proves current.json is not a
// hand-written approximation: it is byte for byte what this build writes for the
// document, so a later change to the document shape shows up here as a fixture
// that no longer matches.
func TestLegacyFlowCurrentFixtureIsTheDocumentWeWrite(t *testing.T) {
	fixture := readLegacyFixture(t, "current")

	var f Flow
	if err := json.Unmarshal(fixture, &f); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(f.extras) != 0 {
		t.Fatalf("current.json carries extras, so it is not a document this build wrote: %v", f.extras)
	}
	encoded, err := json.Marshal(&f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(fixture), encoded) {
		t.Fatalf("current.json is not what this build writes:\nfixture %s\n wrote %s", fixture, encoded)
	}
}

// TestLegacyFlowV0FixtureIsTheOldestShape pins what v0_minimal.json is: the
// members a document written before T1.05 (the snapshot/created_by generation)
// and before T3.01 has, and none of the ones it does not. It is the input of the
// SQLite upgrade test, so its shape is asserted rather than assumed — a fixture
// that quietly grew a "revision" or a "snapshot_id" would make that test prove
// nothing about old rows.
func TestLegacyFlowV0FixtureIsTheOldestShape(t *testing.T) {
	body := readLegacyFixture(t, "v0_minimal")
	// The fixture is read through the same decoder the stores use, so a key it
	// gained later could not hide in a corner of the document: the keys present
	// are the keys of the object, at every level.
	keys, err := fixtureKeys(body)
	if err != nil {
		t.Fatalf("read fixture keys: %v", err)
	}
	// Members that appear nowhere in the document. "kind" is not one of them —
	// a gate has had a kind since the first build — which is why the flow-level
	// member set is checked separately below.
	for _, absent := range []string{
		"session_id", "revision", "binding_id", "template_revision", "parent_project_flow_id",
		"snapshot_id", "on_fail", "config", "created_by", "content_ref",
	} {
		if keys[absent] {
			t.Fatalf("v0_minimal.json has a %q member, which the earliest shape does not have", absent)
		}
	}
	// The flow-level member set, so the fixture cannot gain a member silently
	// either. "kind" is a member of a gate (its kind), not of the flow, which is
	// why this asks the top-level object rather than the whole file.
	var top map[string]any
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatalf("decode top level: %v", err)
	}
	wantKeys := []string{"artifacts", "created_at", "events", "id", "loops", "project_id",
		"stages", "status", "template_id", "updated_at"}
	if gotKeys := sortedAnyKeys(top); !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("v0_minimal.json members changed:\n got %v\nwant %v", gotKeys, wantKeys)
	}
	for _, key := range []string{"kind", "revision", "session_id", "binding_id", "template_revision", "parent_project_flow_id"} {
		if _, present := top[key]; present {
			t.Fatalf("v0_minimal.json has a flow-level %q, which the earliest shape does not have", key)
		}
	}
	// The gates of that shape carry no on_fail and no config, and its stages and
	// artifacts carry no members added since. Those are member sets too.
	stages := top["stages"].([]any)
	firstStage := stages[0].(map[string]any)
	if gotStageKeys := sortedAnyKeys(firstStage); !reflect.DeepEqual(gotStageKeys,
		[]string{"canvas", "gates", "id", "name", "optional", "order", "status", "type"}) {
		t.Fatalf("v0_minimal.json's first stage members changed: %v", gotStageKeys)
	}
	firstGate := firstStage["gates"].([]any)[0].(map[string]any)
	if gotGateKeys := sortedAnyKeys(firstGate); !reflect.DeepEqual(gotGateKeys,
		[]string{"id", "kind", "passed", "phase"}) {
		t.Fatalf("v0_minimal.json's first gate members changed: %v", gotGateKeys)
	}
	firstArtifact := top["artifacts"].([]any)[0].(map[string]any)
	if gotArtifactKeys := sortedAnyKeys(firstArtifact); !reflect.DeepEqual(gotArtifactKeys,
		[]string{"created_at", "id", "stage_id", "status", "type", "version"}) {
		t.Fatalf("v0_minimal.json's first artifact members changed: %v", gotArtifactKeys)
	}

	var f Flow
	if err := json.Unmarshal(body, &f); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.SessionID != "" || f.BindingID != "" || f.TemplateRevision != 0 || f.ParentProjectFlowID != "" ||
		f.Stages[0].SnapshotID != "" || f.Stages[0].Gates[0].OnFail != "" ||
		f.Stages[0].Gates[0].Config != nil || f.Artifacts[0].CreatedBy != "" || f.Artifacts[0].ContentRef != "" {
		t.Fatalf("a member the earliest shape lacks decoded to a non-zero value: %+v", f)
	}
	// The two members a flow-level document does not carry still mean what they
	// mean for every stored document: project, revision 1.
	if f.Kind != FlowKindProject || f.Revision != 1 {
		t.Fatalf("kind=%q revision=%d, want project at revision 1", f.Kind, f.Revision)
	}
	if len(f.extras) != 0 || len(f.Stages[0].extras) != 0 || len(f.Artifacts[0].extras) != 0 {
		t.Fatal("the earliest shape carries no extras")
	}
}
