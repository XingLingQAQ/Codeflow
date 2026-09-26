package run_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/codeflow/backend/internal/run"
)

// TestExecutionEventTypesMatchOpenAPI requires the ExecutionEvent.type enum of
// backend/docs/openapi.yaml (the source of the generated frontend types) to
// equal the Go set, in order. The OpenAPI copy of the enum had no parity check
// before contract amendment CA-1 and would have drifted silently.
func TestExecutionEventTypesMatchOpenAPI(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Enum []string `yaml:"enum"`
				} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	event, ok := doc.Components.Schemas["ExecutionEvent"]
	if !ok {
		t.Fatal("openapi.yaml has no components.schemas.ExecutionEvent")
	}
	got := event.Properties["type"].Enum
	if len(got) != len(run.ExecutionEventTypes) {
		t.Fatalf("OpenAPI ExecutionEvent.type has %d values, run.ExecutionEventTypes has %d: %v", len(got), len(run.ExecutionEventTypes), got)
	}
	for i, et := range run.ExecutionEventTypes {
		if got[i] != string(et) {
			t.Fatalf("OpenAPI ExecutionEvent.type[%d] = %q, want %q (same order as the schema)", i, got[i], et)
		}
	}
}

// schemaDir is where the frozen wire contracts live, relative to
// backend/internal/run.
const schemaDir = "../../schemas"

// identityFixtureDir is the fixture corpus validated by
// schemas/validate-fixtures.mjs; the identity cases must behave the same in Go.
const identityFixtureDir = "../../schemas/fixtures"

// TestExecutionEventTypesMatchSchema reads the closed enum out of
// schemas/execution-event.schema.json and requires the Go set to equal it in
// both directions and in order, so a new event type cannot be added to one side
// only.
func TestExecutionEventTypesMatchSchema(t *testing.T) {
	var schema struct {
		Properties struct {
			Type struct {
				Enum []string `json:"enum"`
			} `json:"type"`
		} `json:"properties"`
	}
	readSchema(t, "execution-event.schema.json", &schema)
	schemaEnum := schema.Properties.Type.Enum
	if len(schemaEnum) == 0 {
		t.Fatal("execution-event.schema.json has no type enum")
	}
	got := make([]string, 0, len(run.ExecutionEventTypes))
	for _, et := range run.ExecutionEventTypes {
		got = append(got, string(et))
	}
	if len(got) != len(schemaEnum) {
		t.Fatalf("ExecutionEventTypes has %d entries, schema enum has %d", len(got), len(schemaEnum))
	}
	for i := range got {
		if got[i] != schemaEnum[i] {
			t.Errorf("ExecutionEventTypes[%d] = %q, schema enum[%d] = %q", i, got[i], i, schemaEnum[i])
		}
		if !run.ExecutionEventType(got[i]).Valid() {
			t.Errorf("ExecutionEventType(%q).Valid() = false", got[i])
		}
	}
	for _, et := range run.ExecutionEventTypes {
		if run.ExecutionEventType(string(et) + " ").Valid() {
			t.Errorf("ExecutionEventType(%q).Valid() = true, want false", string(et)+" ")
		}
	}
	if run.ExecutionEventType("run.started").Valid() {
		t.Error(`ExecutionEventType("run.started").Valid() = true, want false`)
	}
}

// TestExecutionIdentityMatchesSchema reads the identity schema's property
// names, required list, actor enum and length limits, and requires the Go
// struct's JSON tags and constants to agree with them.
func TestExecutionIdentityMatchesSchema(t *testing.T) {
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type      json.RawMessage `json:"type"`
			MinLength int             `json:"minLength"`
			MaxLength int             `json:"maxLength"`
			Enum      []string        `json:"enum"`
			Required  []string        `json:"required"`
			Props     map[string]struct {
				Enum      []string `json:"enum"`
				MinLength int      `json:"minLength"`
				MaxLength int      `json:"maxLength"`
			} `json:"properties"`
		} `json:"properties"`
	}
	readSchema(t, "execution-identity.schema.json", &schema)

	wantProps := []string{"project_id", "actor", "run_id", "attempt_id", "agent_revision_id"}
	gotProps := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		gotProps = append(gotProps, name)
	}
	sort.Strings(gotProps)
	sort.Strings(wantProps)
	if len(gotProps) != len(wantProps) {
		t.Fatalf("schema properties = %v, want %v", gotProps, wantProps)
	}
	for i := range gotProps {
		if gotProps[i] != wantProps[i] {
			t.Fatalf("schema properties = %v, want %v", gotProps, wantProps)
		}
	}

	// The JSON tags of ExecutionIdentity must be exactly the schema properties.
	raw, err := json.Marshal(run.ExecutionIdentity{
		ProjectID: "p", RunID: strPtr("r"), AttemptID: strPtr("a"), AgentRevisionID: strPtr("ar"),
		Actor: run.Actor{Type: run.ActorTypeUser, ID: "u", Source: "desktop"},
	})
	if err != nil {
		t.Fatalf("marshal identity: %v", err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal marshalled identity: %v", err)
	}
	if len(wire) != len(wantProps) {
		t.Fatalf("marshalled identity keys = %v, want %v", wire, wantProps)
	}
	for _, name := range wantProps {
		if _, ok := wire[name]; !ok {
			t.Errorf("marshalled identity has no %q key (JSON tag mismatch)", name)
		}
	}
	// actor's own shape.
	var actorWire map[string]json.RawMessage
	if err := json.Unmarshal(wire["actor"], &actorWire); err != nil {
		t.Fatalf("unmarshal actor: %v", err)
	}
	for _, name := range []string{"type", "id", "source"} {
		if _, ok := actorWire[name]; !ok {
			t.Errorf("marshalled actor has no %q key", name)
		}
	}

	// required: project_id + actor, exactly.
	wantRequired := []string{"actor", "project_id"}
	gotRequired := append([]string(nil), schema.Required...)
	sort.Strings(gotRequired)
	if len(gotRequired) != len(wantRequired) {
		t.Fatalf("schema required = %v, want %v", gotRequired, wantRequired)
	}
	for i := range gotRequired {
		if gotRequired[i] != wantRequired[i] {
			t.Fatalf("schema required = %v, want %v", gotRequired, wantRequired)
		}
	}
	if schema.Properties["actor"].Required == nil {
		t.Fatal("schema actor has no required list")
	}
	actorRequired := append([]string(nil), schema.Properties["actor"].Required...)
	sort.Strings(actorRequired)
	if len(actorRequired) != 2 || actorRequired[0] != "id" || actorRequired[1] != "type" {
		t.Fatalf("schema actor.required = %v, want [id type]", actorRequired)
	}

	// actor.type enum equals the Go set.
	actorEnum := append([]string(nil), schema.Properties["actor"].Props["type"].Enum...)
	sort.Strings(actorEnum)
	gotActorTypes := make([]string, 0, len(run.ActorTypes))
	for _, at := range run.ActorTypes {
		gotActorTypes = append(gotActorTypes, string(at))
	}
	sort.Strings(gotActorTypes)
	if len(actorEnum) != len(gotActorTypes) {
		t.Fatalf("actor.type enum = %v, Go ActorTypes = %v", actorEnum, gotActorTypes)
	}
	for i := range actorEnum {
		if actorEnum[i] != gotActorTypes[i] {
			t.Fatalf("actor.type enum = %v, Go ActorTypes = %v", actorEnum, gotActorTypes)
		}
	}

	// length limits: project_id/actor.id and every optional id are 1..128,
	// actor.source is 128.
	for _, name := range []string{"project_id", "run_id", "attempt_id", "agent_revision_id"} {
		p := schema.Properties[name]
		if p.MinLength != 1 || p.MaxLength != run.MaxIdentityIDLength {
			t.Errorf("%s limits = %d..%d, want 1..%d", name, p.MinLength, p.MaxLength, run.MaxIdentityIDLength)
		}
	}
	if got := schema.Properties["actor"].Props["id"].MaxLength; got != run.MaxIdentityIDLength {
		t.Errorf("actor.id maxLength = %d, want %d", got, run.MaxIdentityIDLength)
	}
	if got := schema.Properties["actor"].Props["source"].MaxLength; got != run.MaxActorSourceLength {
		t.Errorf("actor.source maxLength = %d, want %d", got, run.MaxActorSourceLength)
	}
}

// TestRequiredIdentityTable pins the requirement of every one of the 15 event
// types, including the two Run-less families and the queued-stage claim.
func TestRequiredIdentityTable(t *testing.T) {
	want := map[run.ExecutionEventType]run.IdentityRequirement{
		"approval.approved":       {Run: true},
		"approval.decided":        {},
		"approval.required":       {Run: true},
		"budget.soft_exceeded":    {Run: true},
		"budget.warning":          {Run: true},
		"checkpoint.acknowledged": {Run: true, Attempt: true, AgentRevision: true},
		"merge.completed":         {Run: true},
		"process.exited":          {Run: true, Attempt: true, AgentRevision: true},
		"process.started":         {Run: true, Attempt: true, AgentRevision: true},
		"process.terminated":      {Run: true, Attempt: true, AgentRevision: true},
		"run.cancel_requested":    {Run: true},
		"run.cancelled":           {Run: true},
		"run.completed":           {Run: true},
		"run.expired":             {Run: true},
		"run.failed":              {Run: true},
		"run.reattached":          {Run: true, Attempt: true, AgentRevision: true},
		"run.recovering":          {Run: true},
		"run.resumed":             {Run: true, Attempt: true, AgentRevision: true},
		"scheduler.claimed":       {Run: true},
		"server.restart":          {},
		"tool.requested":          {Run: true, Attempt: true, AgentRevision: true},
	}
	if len(want) != len(run.ExecutionEventTypes) {
		t.Fatalf("the test table covers %d event types, the enum has %d", len(want), len(run.ExecutionEventTypes))
	}
	for _, et := range run.ExecutionEventTypes {
		w, ok := want[et]
		if !ok {
			t.Errorf("no expected requirement for event type %q", et)
			continue
		}
		got, err := run.RequiredIdentity(string(et))
		if err != nil {
			t.Errorf("RequiredIdentity(%q) = error %v", et, err)
			continue
		}
		if got != w {
			t.Errorf("RequiredIdentity(%q) = %+v, want %+v", et, got, w)
		}
	}
	// Every event type the table lists must be in the enum, and the exported
	// view must carry a reason for each row.
	rules := run.IdentityRules()
	if len(rules) != len(run.ExecutionEventTypes) {
		t.Fatalf("IdentityRules() has %d rows, want %d", len(rules), len(run.ExecutionEventTypes))
	}
	for i, r := range rules {
		if !r.Event.Valid() {
			t.Errorf("IdentityRules()[%d] event %q is not in the enum", i, r.Event)
		}
		if r.Reason == "" {
			t.Errorf("IdentityRules()[%d] (%s) has no reason", i, r.Event)
		}
		if r.Requirement != want[r.Event] {
			t.Errorf("IdentityRules()[%d] (%s) = %+v, want %+v", i, r.Event, r.Requirement, want[r.Event])
		}
	}
	// Unknown event types are an error, not "nothing required".
	if _, err := run.RequiredIdentity("run.started"); err == nil {
		t.Error("RequiredIdentity(\"run.started\") = nil error, want a rejection")
	} else if !errors.Is(err, run.ErrUnknownEventType) {
		t.Errorf("RequiredIdentity(\"run.started\") error %v, want ErrUnknownEventType", err)
	}
	if _, err := run.RequiredIdentity(""); err == nil {
		t.Error("RequiredIdentity(\"\") = nil error, want a rejection")
	}
}

// TestValidateForQueuedStageNeedsNoAttempt covers §28's "a queued-stage event
// may have no attempt" and "a tool event must have one".
func TestValidateForQueuedStageNeedsNoAttempt(t *testing.T) {
	queued := run.ExecutionIdentity{
		ProjectID: "p_123",
		RunID:     strPtr("run_88"),
		Actor:     run.Actor{Type: run.ActorTypeSystem, ID: "scheduler"},
	}
	if err := queued.ValidateFor("scheduler.claimed"); err != nil {
		t.Fatalf("scheduler.claimed without an attempt = %v, want nil", err)
	}
	err := queued.ValidateFor("tool.requested")
	if err == nil {
		t.Fatal("tool.requested without an attempt was accepted")
	}
	var ie *run.IdentityError
	if !errors.As(err, &ie) {
		t.Fatalf("error is %T, want *run.IdentityError", err)
	}
	if ie.Field != "attempt_id" || !ie.Missing {
		t.Fatalf("error = %+v, want a missing attempt_id", ie)
	}
	if !errors.Is(err, run.ErrMissingIdentityField) {
		t.Fatalf("error %v does not match ErrMissingIdentityField", err)
	}
	// Filling the attempt in is enough; the agent revision is still missing.
	withAttempt := queued
	withAttempt.AttemptID = strPtr("att_1")
	err = withAttempt.ValidateFor("tool.requested")
	if !errors.As(err, &ie) || ie.Field != "agent_revision_id" || !ie.Missing {
		t.Fatalf("error = %v (%+v), want a missing agent_revision_id", err, ie)
	}
	withAttempt.AgentRevisionID = strPtr("ar_12")
	if err := withAttempt.ValidateFor("tool.requested"); err != nil {
		t.Fatalf("complete execution identity = %v, want nil", err)
	}
}

// TestValidateShape checks the shape rules of the identity schema.
func TestValidateShape(t *testing.T) {
	ok := run.ExecutionIdentity{
		ProjectID: "p_123",
		Actor:     run.Actor{Type: run.ActorTypeUser, ID: "local-user", Source: "desktop"},
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("minimal identity = %v, want nil", err)
	}
	long := make([]byte, run.MaxIdentityIDLength+1)
	for i := range long {
		long[i] = 'a'
	}
	cases := []struct {
		name  string
		id    run.ExecutionIdentity
		field string
	}{
		{"empty project", run.ExecutionIdentity{Actor: run.Actor{Type: run.ActorTypeUser, ID: "u"}}, "project_id"},
		{"blank project", run.ExecutionIdentity{ProjectID: "   ", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u"}}, "project_id"},
		{"project too long", run.ExecutionIdentity{ProjectID: string(long), Actor: run.Actor{Type: run.ActorTypeUser, ID: "u"}}, "project_id"},
		{"no actor type", run.ExecutionIdentity{ProjectID: "p", Actor: run.Actor{ID: "u"}}, "actor.type"},
		{"bad actor type", run.ExecutionIdentity{ProjectID: "p", Actor: run.Actor{Type: run.ActorType("robot"), ID: "u"}}, "actor.type"},
		{"no actor id", run.ExecutionIdentity{ProjectID: "p", Actor: run.Actor{Type: run.ActorTypeUser}}, "actor.id"},
		{"blank actor id", run.ExecutionIdentity{ProjectID: "p", Actor: run.Actor{Type: run.ActorTypeUser, ID: " "}}, "actor.id"},
		{"actor id too long", run.ExecutionIdentity{ProjectID: "p", Actor: run.Actor{Type: run.ActorTypeUser, ID: string(long)}}, "actor.id"},
		{"actor source too long", run.ExecutionIdentity{ProjectID: "p", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u", Source: string(long)}}, "actor.source"},
		{"empty run id", run.ExecutionIdentity{ProjectID: "p", RunID: strPtr(""), Actor: run.Actor{Type: run.ActorTypeUser, ID: "u"}}, "run_id"},
		{"run id too long", run.ExecutionIdentity{ProjectID: "p", RunID: strPtr(string(long)), Actor: run.Actor{Type: run.ActorTypeUser, ID: "u"}}, "run_id"},
		{"attempt id too long", run.ExecutionIdentity{ProjectID: "p", AttemptID: strPtr(string(long)), Actor: run.Actor{Type: run.ActorTypeUser, ID: "u"}}, "attempt_id"},
		{"revision id too long", run.ExecutionIdentity{ProjectID: "p", AgentRevisionID: strPtr(string(long)), Actor: run.Actor{Type: run.ActorTypeUser, ID: "u"}}, "agent_revision_id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.id.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want a rejection of %s", c.field)
			}
			var ie *run.IdentityError
			if !errors.As(err, &ie) {
				t.Fatalf("error is %T, want *run.IdentityError", err)
			}
			if ie.Field != c.field {
				t.Fatalf("error field = %q, want %q", ie.Field, c.field)
			}
			if ie.Missing {
				t.Fatalf("error = %+v, want Missing=false for a shape problem", ie)
			}
			if !errors.Is(err, run.ErrInvalidIdentityField) {
				t.Fatalf("error %v does not match ErrInvalidIdentityField", err)
			}
			// ValidateFor must report the same field, plus the event type.
			err = c.id.ValidateFor("run.completed")
			if !errors.As(err, &ie) || ie.Field != c.field || ie.EventType != "run.completed" {
				t.Fatalf("ValidateFor error = %v (%+v), want field %q of run.completed", err, ie, c.field)
			}
		})
	}
	// actor.source at the limit is fine.
	atLimit := ok
	atLimit.Actor.Source = string(long[:run.MaxActorSourceLength])
	if err := atLimit.Validate(); err != nil {
		t.Fatalf("actor.source at the limit = %v, want nil", err)
	}
}

// TestValidateForRequiredFields walks every event type with a Run-only identity
// and with a full execution identity, so the whole table is exercised through
// the validator, not just through RequiredIdentity.
func TestValidateForRequiredFields(t *testing.T) {
	runOnly := run.ExecutionIdentity{
		ProjectID: "p_123",
		RunID:     strPtr("run_88"),
		Actor:     run.Actor{Type: run.ActorTypeSystem, ID: "scheduler"},
	}
	execution := runOnly
	execution.AttemptID = strPtr("att_1")
	execution.AgentRevisionID = strPtr("ar_12")
	for _, et := range run.ExecutionEventTypes {
		req, err := run.RequiredIdentity(string(et))
		if err != nil {
			t.Fatalf("RequiredIdentity(%q): %v", et, err)
		}
		if err := execution.ValidateFor(string(et)); err != nil {
			t.Errorf("full execution identity for %q = %v, want nil", et, err)
		}
		err = runOnly.ValidateFor(string(et))
		switch {
		case !req.Attempt && !req.AgentRevision:
			if err != nil {
				t.Errorf("Run-only identity for %q = %v, want nil", et, err)
			}
		default:
			var ie *run.IdentityError
			if !errors.As(err, &ie) {
				t.Errorf("Run-only identity for %q = %v, want a missing-field rejection", et, err)
				continue
			}
			wantField := "attempt_id"
			if !req.Attempt {
				wantField = "agent_revision_id"
			}
			if ie.Field != wantField || !ie.Missing {
				t.Errorf("Run-only identity for %q reported %+v, want missing %s", et, ie, wantField)
			}
		}
	}
	// A Run-less identity is accepted for the two Run-less families and
	// rejected everywhere run_id is required.
	gate := run.ExecutionIdentity{
		ProjectID: "b7f1d0a2-3c4e-4f5a-8b6c-9d0e1f2a3b4c",
		Actor:     run.Actor{Type: run.ActorTypeUser, ID: "local-user", Source: "desktop"},
	}
	for _, et := range []string{"approval.decided", "server.restart"} {
		if err := gate.ValidateFor(et); err != nil {
			t.Errorf("Run-less identity for %q = %v, want nil", et, err)
		}
	}
	for _, et := range run.ExecutionEventTypes {
		if et == "approval.decided" || et == "server.restart" {
			continue
		}
		err := gate.ValidateFor(string(et))
		var ie *run.IdentityError
		if !errors.As(err, &ie) || ie.Field != "run_id" || !ie.Missing {
			t.Errorf("Run-less identity for %q = %v (%+v), want a missing run_id", et, err, ie)
		}
	}
}

// TestIdentityFixturesAgainstSchema parses the identity fixtures of
// schemas/fixtures: the valid ones must be accepted for an event type whose
// requirements they satisfy, and the invalid ones must be rejected, so the Go
// validator and the JSON Schema agree on the corpus that
// schemas/validate-fixtures.mjs checks.
func TestIdentityFixturesAgainstSchema(t *testing.T) {
	valid := []struct {
		file  string
		event string
	}{
		{"identity.agent-run.json", "tool.requested"},
		{"identity.user-gate-no-run.json", "approval.decided"},
	}
	for _, c := range valid {
		t.Run("valid/"+c.file, func(t *testing.T) {
			raw := readFixture(t, filepath.Join(identityFixtureDir, "valid", c.file))
			var id run.ExecutionIdentity
			if err := json.Unmarshal(raw, &id); err != nil {
				t.Fatalf("unmarshal %s: %v", c.file, err)
			}
			if err := id.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if err := id.ValidateFor(c.event); err != nil {
				t.Fatalf("ValidateFor(%q) = %v, want nil", c.event, err)
			}
			// Round-trip: the marshalled form must keep exactly the schema keys
			// and re-parse to the same identity.
			out, err := json.Marshal(id)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var keys map[string]json.RawMessage
			if err := json.Unmarshal(out, &keys); err != nil {
				t.Fatalf("unmarshal round-trip: %v", err)
			}
			for name := range keys {
				switch name {
				case "project_id", "actor", "run_id", "attempt_id", "agent_revision_id":
				default:
					t.Errorf("round-trip emitted %q, which the schema does not allow", name)
				}
			}
			var again run.ExecutionIdentity
			if err := json.Unmarshal(out, &again); err != nil {
				t.Fatalf("unmarshal round-trip: %v", err)
			}
			if again.ProjectID != id.ProjectID || again.Actor != id.Actor {
				t.Errorf("round-trip changed project_id/actor: %+v -> %+v", id, again)
			}
		})
	}
	invalid := []string{
		"identity.missing-actor.json",
		"identity.missing-project-id.json",
		"identity.project-id-too-long.json",
		"identity.unknown-actor-type.json",
	}
	for _, file := range invalid {
		t.Run("invalid/"+file, func(t *testing.T) {
			raw := readFixture(t, filepath.Join(identityFixtureDir, "invalid", file))
			var id run.ExecutionIdentity
			if err := json.Unmarshal(raw, &id); err != nil {
				// A shape the Go type cannot even hold is a rejection too.
				return
			}
			if err := id.Validate(); err == nil {
				t.Fatalf("Validate() = nil for %s, want a rejection", file)
			}
			if err := id.ValidateFor("tool.requested"); err == nil {
				t.Fatalf("ValidateFor() = nil for %s, want a rejection", file)
			}
		})
	}
}

// TestValidateForEventTypeOrdering keeps the error contract stable: an unknown
// event type is reported before the shape, and the reported field names the
// event type itself.
func TestValidateForEventTypeOrdering(t *testing.T) {
	empty := run.ExecutionIdentity{}
	err := empty.ValidateFor("not.an.event")
	if err == nil {
		t.Fatal("ValidateFor with an unknown event type = nil, want a rejection")
	}
	var ie *run.IdentityError
	if !errors.As(err, &ie) {
		t.Fatalf("error is %T, want *run.IdentityError", err)
	}
	if ie.Field != "event_type" || ie.EventType != "not.an.event" {
		t.Fatalf("error = %+v, want field event_type of not.an.event", ie)
	}
	if !errors.Is(err, run.ErrUnknownEventType) {
		t.Fatalf("error %v does not match ErrUnknownEventType", err)
	}
	if errors.Is(err, run.ErrMissingIdentityField) || errors.Is(err, run.ErrInvalidIdentityField) {
		t.Fatalf("error %v must only match ErrUnknownEventType", err)
	}
}

// readSchema decodes a schema file into out, failing the test if it is missing.
func readSchema(t *testing.T, name string, out any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(schemaDir, name))
	if err != nil {
		t.Fatalf("read schema %s: %v", name, err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("parse schema %s: %v", name, err)
	}
}

// readFixture reads one fixture file.
func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return raw
}

// strPtr returns a pointer to s, for the nullable identity fields.
func strPtr(s string) *string { return &s }
