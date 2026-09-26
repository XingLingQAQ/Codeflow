package hooks

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/run"
)

// ---------------------------------------------------------------------------
// 工具 hook payload
// ---------------------------------------------------------------------------

func strPtr(value string) *string { return &value }

func intPtr(value int) *int { return &value }

func toolIdentity() run.ExecutionIdentity {
	return run.ExecutionIdentity{
		ProjectID:       "project-1",
		RunID:           strPtr("run-1"),
		AttemptID:       strPtr("attempt-1"),
		AgentRevisionID: strPtr("revision-1"),
		Actor:           run.Actor{Type: run.ActorTypeAgent, ID: "agent-1", Source: "server"},
	}
}

func allowedPreToolPayload() ToolHookPayload {
	return ToolHookPayload{
		Identity:           toolIdentity(),
		EventID:            "event-1",
		ToolCallID:         "toolu_1",
		RequestFingerprint: "sha256:abc",
		Tool:               "read_file",
		Arguments:          json.RawMessage(`{"path":"src/x.go"}`),
	}
}

func TestToolHookPayloadValidateAccepts(t *testing.T) {
	pre := allowedPreToolPayload()
	if err := pre.Validate(HookPreToolUse); err != nil {
		t.Fatalf("pre payload rejected: %v", err)
	}

	post := pre
	post.EventID = "event-2"
	post.Arguments = nil // post may drop the arguments
	post.Result = &ToolHookResult{Status: ToolHookResultOK, ExitCode: intPtr(0), OutputRef: "spool/1"}
	if err := post.Validate(HookPostToolUse); err != nil {
		t.Fatalf("post payload rejected: %v", err)
	}

	// The result summary validates on its own too, for every closed status.
	if err := post.Result.Validate(); err != nil {
		t.Fatalf("result rejected: %v", err)
	}
	for _, status := range ToolHookResultStatuses {
		result := ToolHookResult{Status: status}
		if err := result.Validate(); err != nil {
			t.Errorf("result status %q rejected: %v", status, err)
		}
	}
}

func TestToolHookPayloadRejects(t *testing.T) {
	valid := allowedPreToolPayload()

	cases := []struct {
		name   string
		hook   HookType
		mutate func(*ToolHookPayload)
		field  string
	}{
		{name: "wrong hook type", hook: HookAfterExec, field: "hook"},
		{name: "empty event id", hook: HookPreToolUse, field: "EventID", mutate: func(p *ToolHookPayload) { p.EventID = "" }},
		{name: "blank event id", hook: HookPreToolUse, field: "EventID", mutate: func(p *ToolHookPayload) { p.EventID = "   " }},
		{name: "event id too long", hook: HookPreToolUse, field: "EventID", mutate: func(p *ToolHookPayload) { p.EventID = strings.Repeat("e", run.MaxIdentityIDLength+1) }},
		{name: "empty tool call id", hook: HookPreToolUse, field: "ToolCallID", mutate: func(p *ToolHookPayload) { p.ToolCallID = "" }},
		{name: "tool call id too long", hook: HookPreToolUse, field: "ToolCallID", mutate: func(p *ToolHookPayload) { p.ToolCallID = strings.Repeat("t", MaxToolCallIDBytes+1) }},
		{name: "empty fingerprint", hook: HookPreToolUse, field: "RequestFingerprint", mutate: func(p *ToolHookPayload) { p.RequestFingerprint = "" }},
		{name: "empty tool", hook: HookPreToolUse, field: "Tool", mutate: func(p *ToolHookPayload) { p.Tool = "" }},
		{name: "pre with a result", hook: HookPreToolUse, field: "Result", mutate: func(p *ToolHookPayload) { p.Result = &ToolHookResult{Status: ToolHookResultOK} }},
		{name: "pre without arguments", hook: HookPreToolUse, field: "Arguments", mutate: func(p *ToolHookPayload) { p.Arguments = nil }},
		{name: "pre with malformed arguments", hook: HookPreToolUse, field: "Arguments", mutate: func(p *ToolHookPayload) { p.Arguments = json.RawMessage(`{"path":`) }},
		{name: "post without a result", hook: HookPostToolUse, field: "Result", mutate: func(p *ToolHookPayload) { p.Result = nil }},
		{name: "post with an unknown result status", hook: HookPostToolUse, field: "Result.Status", mutate: func(p *ToolHookPayload) { p.Result = &ToolHookResult{Status: "maybe"} }},
		{name: "post with malformed arguments", hook: HookPostToolUse, field: "Arguments", mutate: func(p *ToolHookPayload) {
			p.Result = &ToolHookResult{Status: ToolHookResultOK}
			p.Arguments = json.RawMessage(`{"path":`)
		}},
		{name: "project id missing", hook: HookPreToolUse, field: "Identity.project_id", mutate: func(p *ToolHookPayload) { p.Identity.ProjectID = "" }},
		{name: "run id missing", hook: HookPreToolUse, field: "Identity.run_id", mutate: func(p *ToolHookPayload) { p.Identity.RunID = nil }},
		{name: "attempt id missing", hook: HookPreToolUse, field: "Identity.attempt_id", mutate: func(p *ToolHookPayload) { p.Identity.AttemptID = nil }},
		{name: "agent revision missing", hook: HookPreToolUse, field: "Identity.agent_revision_id", mutate: func(p *ToolHookPayload) { p.Identity.AgentRevisionID = nil }},
		{name: "actor type invalid", hook: HookPreToolUse, field: "Identity.actor.type", mutate: func(p *ToolHookPayload) { p.Identity.Actor.Type = "robot" }},
		{name: "actor id missing", hook: HookPreToolUse, field: "Identity.actor.id", mutate: func(p *ToolHookPayload) { p.Identity.Actor.ID = "" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := valid
			if tc.mutate != nil {
				tc.mutate(&payload)
			}
			err := payload.Validate(tc.hook)
			if err == nil {
				t.Fatalf("payload accepted, want rejection of %s", tc.field)
			}
			var perr *PayloadError
			if !errors.As(err, &perr) {
				t.Fatalf("error is not a *PayloadError: %v", err)
			}
			if perr.Field != tc.field {
				t.Fatalf("error field = %q, want %q (%v)", perr.Field, tc.field, err)
			}
			if perr.Hook != tc.hook {
				t.Errorf("error hook = %q, want %q", perr.Hook, tc.hook)
			}
		})
	}
}

// TestToolPayloadSentinels pins the errors.Is contract: which sentinel each
// class of rejection reports, so a caller can tell "absent" from "malformed"
// without string matching, and the run package's identity sentinels stay
// reachable through the wrapper.
func TestToolPayloadSentinels(t *testing.T) {
	missing := allowedPreToolPayload()
	missing.ToolCallID = ""
	err := missing.Validate(HookPreToolUse)
	if !errors.Is(err, ErrPayloadFieldMissing) {
		t.Errorf("empty ToolCallID: errors.Is(ErrPayloadFieldMissing) = false (%v)", err)
	}
	if errors.Is(err, ErrPayloadFieldInvalid) {
		t.Errorf("empty ToolCallID reported as invalid as well: %v", err)
	}

	invalid := allowedPreToolPayload()
	invalid.Arguments = json.RawMessage(`{"path":`)
	err = invalid.Validate(HookPreToolUse)
	if !errors.Is(err, ErrPayloadFieldInvalid) {
		t.Errorf("bad JSON arguments: errors.Is(ErrPayloadFieldInvalid) = false (%v)", err)
	}
	if errors.Is(err, ErrPayloadFieldMissing) {
		t.Errorf("bad JSON arguments reported as missing as well: %v", err)
	}

	mismatch := allowedPreToolPayload()
	err = mismatch.Validate(HookBeforeWrite)
	if !errors.Is(err, ErrPayloadHookMismatch) {
		t.Errorf("wrong hook: errors.Is(ErrPayloadHookMismatch) = false (%v)", err)
	}
	if errors.Is(err, ErrPayloadFieldInvalid) {
		t.Errorf("wrong hook reported as an invalid field as well: %v", err)
	}

	// The run package's identity sentinels stay reachable through the wrapper.
	missingIdentity := allowedPreToolPayload()
	missingIdentity.Identity.RunID = nil
	err = missingIdentity.Validate(HookPreToolUse)
	if !errors.Is(err, run.ErrMissingIdentityField) {
		t.Errorf("missing run_id: errors.Is(run.ErrMissingIdentityField) = false (%v)", err)
	}
	if !errors.Is(err, ErrPayloadFieldMissing) {
		t.Errorf("missing run_id: errors.Is(ErrPayloadFieldMissing) = false (%v)", err)
	}
	invalidIdentity := allowedPreToolPayload()
	invalidIdentity.Identity.ProjectID = strings.Repeat("p", run.MaxIdentityIDLength+1)
	err = invalidIdentity.Validate(HookPreToolUse)
	if !errors.Is(err, run.ErrInvalidIdentityField) {
		t.Errorf("over-long project_id: errors.Is(run.ErrInvalidIdentityField) = false (%v)", err)
	}
}

// TestPayloadErrorsDoNotLeakContent is the redaction guarantee: a rejected
// payload's error text names fields and reasons, never the arguments or result
// content, which would otherwise reach logs and audit.
func TestPayloadErrorsDoNotLeakContent(t *testing.T) {
	const secret = "sk-super-secret-token"

	// Malformed arguments: the raw bytes must not be echoed, not even the keys.
	badArguments := allowedPreToolPayload()
	badArguments.Arguments = json.RawMessage(`{"token":"` + secret + `"`)
	err := badArguments.Validate(HookPreToolUse)
	if err == nil {
		t.Fatal("payload with malformed arguments accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error text leaks the arguments: %v", err)
	}
	if strings.Contains(err.Error(), "token") {
		t.Fatalf("error text leaks argument keys: %v", err)
	}

	// A rejected result must not echo the output reference either.
	post := allowedPreToolPayload()
	post.Arguments = nil
	post.Result = &ToolHookResult{Status: "bogus", OutputRef: secret}
	err = post.Validate(HookPostToolUse)
	if err == nil {
		t.Fatal("post payload with a bogus status accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error text leaks the result: %v", err)
	}

	// The identity wrapper names the field and keeps the run error (field-only),
	// never the field's value.
	identity := allowedPreToolPayload()
	identity.Identity.ProjectID = ""
	err = identity.Validate(HookPreToolUse)
	if err == nil || !strings.Contains(err.Error(), "project_id") {
		t.Fatalf("identity rejection should name the field: %v", err)
	}
}

func TestToolCallIDLimitMatchesExecBackend(t *testing.T) {
	// execbackend.MaxProviderRefBytes is 256 and this package may not import
	// execbackend (it imports only the standard library). The number is pinned
	// here so the two cannot drift silently.
	if MaxToolCallIDBytes != 256 {
		t.Fatalf("MaxToolCallIDBytes = %d, want 256 (execbackend.MaxProviderRefBytes)", MaxToolCallIDBytes)
	}
	atLimit := allowedPreToolPayload()
	atLimit.ToolCallID = strings.Repeat("t", MaxToolCallIDBytes)
	if err := atLimit.Validate(HookPreToolUse); err != nil {
		t.Fatalf("a tool call id of exactly %d bytes was rejected: %v", MaxToolCallIDBytes, err)
	}
	overLimit := allowedPreToolPayload()
	overLimit.ToolCallID = strings.Repeat("t", MaxToolCallIDBytes+1)
	if err := overLimit.Validate(HookPreToolUse); err == nil {
		t.Fatalf("a tool call id of %d bytes was accepted", MaxToolCallIDBytes+1)
	}
}

func TestToolHookPayloadDedupeKey(t *testing.T) {
	payload := allowedPreToolPayload()
	pre := payload.DedupeKey(HookPreToolUse)
	if pre == "" {
		t.Fatal("dedupe key is empty for a validated payload")
	}
	if !strings.Contains(pre, payload.EventID) || !strings.Contains(pre, string(HookPreToolUse)) {
		t.Errorf("dedupe key %q must carry the hook type and the event id", pre)
	}
	// Same event and same hook: one trigger record.
	if again := payload.DedupeKey(HookPreToolUse); again != pre {
		t.Errorf("dedupe key is not stable: %q vs %q", again, pre)
	}
	// A different hook for the same event is a different record (pre and post of
	// one tool call are two different facts).
	if other := payload.DedupeKey(HookPostToolUse); other == pre {
		t.Errorf("pre and post share the dedupe key %q", pre)
	}
	// A different event is a different record.
	moved := payload
	moved.EventID = "event-9"
	if moved.DedupeKey(HookPreToolUse) == pre {
		t.Errorf("two events share the dedupe key %q", pre)
	}
	// No event id: no key at all, so an un-validated payload cannot collide.
	empty := payload
	empty.EventID = ""
	if key := empty.DedupeKey(HookPreToolUse); key != "" {
		t.Errorf("empty event id produced the key %q, want \"\"", key)
	}
	if key := empty.DedupeKey(""); key != "" {
		t.Errorf("empty hook type produced the key %q, want \"\"", key)
	}
}

// ---------------------------------------------------------------------------
// Run hook payload
// ---------------------------------------------------------------------------

func runIdentity() run.ExecutionIdentity {
	return run.ExecutionIdentity{
		ProjectID:       "project-1",
		RunID:           strPtr("run-1"),
		AttemptID:       strPtr("attempt-1"),
		AgentRevisionID: strPtr("revision-1"),
		Actor:           run.Actor{Type: run.ActorTypeSystem, ID: "scheduler", Source: "server"},
	}
}

func TestRunHookPayloadValidateAccepts(t *testing.T) {
	start := RunHookPayload{
		Identity:       runIdentity(),
		EventID:        "claimed-1",
		Status:         run.RunStatusStarting,
		PreviousStatus: run.RunStatusQueued,
	}
	if err := start.Validate(HookRunStart); err != nil {
		t.Fatalf("run start payload rejected: %v", err)
	}

	// Start may omit PreviousStatus (the producer did not record it).
	startNoPrevious := start
	startNoPrevious.PreviousStatus = ""
	if err := startNoPrevious.Validate(HookRunStart); err != nil {
		t.Fatalf("run start payload without PreviousStatus rejected: %v", err)
	}

	for _, terminal := range []run.RunStatus{
		run.RunStatusCompleted,
		run.RunStatusFailed,
		run.RunStatusCancelled,
		run.RunStatusExpired,
	} {
		finish := RunHookPayload{
			Identity:       runIdentity(),
			EventID:        "terminal-1",
			Status:         terminal,
			PreviousStatus: run.RunStatusRunning,
		}
		if err := finish.Validate(HookRunFinish); err != nil {
			t.Errorf("run finish payload for %q rejected: %v", terminal, err)
		}
	}

	// A terminal Run fact needs only the Run: attempt and revision may be gone.
	noAttempt := RunHookPayload{
		Identity: run.ExecutionIdentity{
			ProjectID: "project-1",
			RunID:     strPtr("run-1"),
			Actor:     run.Actor{Type: run.ActorTypeSystem, ID: "reaper"},
		},
		EventID: "terminal-2",
		Status:  run.RunStatusFailed,
	}
	if err := noAttempt.Validate(HookRunFinish); err != nil {
		t.Fatalf("run finish payload without attempt rejected: %v", err)
	}
}

func TestRunHookPayloadRejects(t *testing.T) {
	start := RunHookPayload{
		Identity: runIdentity(),
		EventID:  "claimed-1",
		Status:   run.RunStatusStarting,
	}
	finish := RunHookPayload{
		Identity: runIdentity(),
		EventID:  "terminal-1",
		Status:   run.RunStatusCompleted,
	}

	cases := []struct {
		name   string
		hook   HookType
		base   RunHookPayload
		mutate func(*RunHookPayload)
		field  string
	}{
		{name: "wrong hook type", hook: HookOnStream, base: start, field: "hook"},
		{name: "empty event id", hook: HookRunStart, base: start, field: "EventID", mutate: func(p *RunHookPayload) { p.EventID = "" }},
		{name: "unknown status", hook: HookRunStart, base: start, field: "Status", mutate: func(p *RunHookPayload) { p.Status = "halfway" }},
		{name: "start with a non-starting status", hook: HookRunStart, base: start, field: "Status", mutate: func(p *RunHookPayload) { p.Status = run.RunStatusRunning }},
		{name: "start without run", hook: HookRunStart, base: start, field: "Identity.run_id", mutate: func(p *RunHookPayload) { p.Identity.RunID = nil }},
		{name: "start without attempt", hook: HookRunStart, base: start, field: "AttemptID", mutate: func(p *RunHookPayload) { p.Identity.AttemptID = nil }},
		{name: "start without agent revision", hook: HookRunStart, base: start, field: "AgentRevisionID", mutate: func(p *RunHookPayload) { p.Identity.AgentRevisionID = nil }},
		{name: "finish with a non-terminal status", hook: HookRunFinish, base: finish, field: "Status", mutate: func(p *RunHookPayload) { p.Status = run.RunStatusRunning }},
		// completed/failed delegate to the terminal event's own requirement.
		{name: "finish without run", hook: HookRunFinish, base: finish, field: "Identity.run_id", mutate: func(p *RunHookPayload) { p.Identity.RunID = nil }},
		// cancelled/expired have no event type, so the hook applies the Run-level
		// rule itself and names the field "RunID".
		{name: "cancelled finish without run", hook: HookRunFinish, base: finish, field: "RunID", mutate: func(p *RunHookPayload) {
			p.Status = run.RunStatusCancelled
			p.Identity.RunID = nil
		}},
		{name: "finish without project", hook: HookRunFinish, base: finish, field: "Identity.project_id", mutate: func(p *RunHookPayload) { p.Identity.ProjectID = "" }},
		{name: "unknown previous status", hook: HookRunFinish, base: finish, field: "PreviousStatus", mutate: func(p *RunHookPayload) { p.PreviousStatus = "halfway" }},
		{name: "previous equals current", hook: HookRunFinish, base: finish, field: "PreviousStatus", mutate: func(p *RunHookPayload) { p.PreviousStatus = run.RunStatusCompleted }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := tc.base
			if tc.mutate != nil {
				tc.mutate(&payload)
			}
			err := payload.Validate(tc.hook)
			if err == nil {
				t.Fatalf("payload accepted, want rejection of %s", tc.field)
			}
			var perr *PayloadError
			if !errors.As(err, &perr) {
				t.Fatalf("error is not a *PayloadError: %v", err)
			}
			if perr.Field != tc.field {
				t.Fatalf("error field = %q, want %q (%v)", perr.Field, tc.field, err)
			}
			if perr.Hook != tc.hook {
				t.Errorf("error hook = %q, want %q", perr.Hook, tc.hook)
			}
		})
	}
}

// TestRunStartNeedsAnAttempt is the §28 rule "tool 必须有 attempt" applied to
// the run-start hook: it fires after the claim, so a payload without the attempt
// it belongs to is incomplete even though scheduler.claimed itself only names
// the Run.
func TestRunStartNeedsAnAttempt(t *testing.T) {
	payload := RunHookPayload{
		Identity: run.ExecutionIdentity{
			ProjectID: "project-1",
			RunID:     strPtr("run-1"),
			Actor:     run.Actor{Type: run.ActorTypeSystem, ID: "scheduler"},
		},
		EventID: "claimed-1",
		Status:  run.RunStatusStarting,
	}
	err := payload.Validate(HookRunStart)
	if err == nil {
		t.Fatal("run start accepted a payload with no attempt")
	}
	if !errors.Is(err, ErrPayloadFieldMissing) {
		t.Errorf("errors.Is(ErrPayloadFieldMissing) = false (%v)", err)
	}
	// The very same identity is fine for the claim event itself: the stricter
	// rule belongs to the hook, not to the event.
	if err := payload.Identity.ValidateFor(string(run.EventSchedulerClaimed)); err != nil {
		t.Errorf("identity is not valid for scheduler.claimed: %v", err)
	}
}

func TestRunTerminalEventType(t *testing.T) {
	cases := map[run.RunStatus]run.ExecutionEventType{
		run.RunStatusCompleted: run.EventRunCompleted,
		run.RunStatusFailed:    run.EventRunFailed,
	}
	for status, want := range cases {
		got, ok := RunTerminalEventType(status)
		if !ok || got != want {
			t.Errorf("RunTerminalEventType(%q) = %q/%v, want %q/true", status, got, ok, want)
		}
	}
	// cancelled and expired have no dedicated event in the closed enum yet.
	for _, status := range []run.RunStatus{run.RunStatusCancelled, run.RunStatusExpired, run.RunStatusRunning} {
		if got, ok := RunTerminalEventType(status); ok {
			t.Errorf("RunTerminalEventType(%q) = %q/true, want ok=false", status, got)
		}
	}
}

func TestRunHookPayloadDedupeKey(t *testing.T) {
	payload := RunHookPayload{
		Identity: runIdentity(),
		EventID:  "terminal-1",
		Status:   run.RunStatusCompleted,
	}
	finish := payload.DedupeKey(HookRunFinish)
	if finish == "" || !strings.Contains(finish, payload.EventID) {
		t.Fatalf("dedupe key %q must carry the event id", finish)
	}
	if payload.DedupeKey(HookRunStart) == finish {
		t.Error("run start and run finish share the dedupe key")
	}
	empty := payload
	empty.EventID = ""
	if key := empty.DedupeKey(HookRunFinish); key != "" {
		t.Errorf("empty event id produced the key %q", key)
	}
}

// TestPayloadIdentityIsServerFilled documents the trust rule as a test: the
// payload carries a run.ExecutionIdentity, the type the server builds from the
// authoritative Run/Attempt records — never an identity a backend self-reports.
func TestPayloadIdentityIsServerFilled(t *testing.T) {
	payload := allowedPreToolPayload()
	var _ run.ExecutionIdentity = payload.Identity
	if payload.Identity.ProjectID == "" || payload.Identity.RunID == nil {
		t.Fatal("fixture identity is not a full server-side identity")
	}
	// An identity that only names an actor (the shape a backend could self-report)
	// is refused: project_id is required. (An empty project_id is "invalid
	// (minLength 1)", not "missing": run's shape check comes first.)
	spoofed := allowedPreToolPayload()
	spoofed.Identity = run.ExecutionIdentity{Actor: run.Actor{Type: run.ActorTypeAgent, ID: "claude"}}
	err := spoofed.Validate(HookPreToolUse)
	if err == nil {
		t.Fatal("a self-reported actor-only identity was accepted")
	}
	if !errors.Is(err, run.ErrInvalidIdentityField) {
		t.Errorf("errors.Is(run.ErrInvalidIdentityField) = false (%v)", err)
	}
	if !strings.Contains(err.Error(), "project_id") {
		t.Errorf("rejection does not name project_id: %v", err)
	}
}

// TestPayloadValidateIsHookClosed pins that each payload contract only serves
// its two hook types: wiring a payload to another hook is a contract error, not
// a silent pass.
func TestPayloadValidateIsHookClosed(t *testing.T) {
	toolPayload := allowedPreToolPayload()
	runPayload := RunHookPayload{
		Identity: runIdentity(),
		EventID:  "claimed-1",
		Status:   run.RunStatusStarting,
	}
	for _, hook := range AllHookTypes() {
		err := toolPayload.Validate(hook)
		switch hook {
		case HookPreToolUse:
			if err != nil {
				t.Errorf("%s rejected a valid pre payload: %v", hook, err)
			}
		case HookPostToolUse:
			if !errors.Is(err, ErrPayloadFieldMissing) {
				t.Errorf("%s: a pre payload (no result) should be a missing Result, got %v", hook, err)
			}
		default:
			if !errors.Is(err, ErrPayloadHookMismatch) {
				t.Errorf("%s: errors.Is(ErrPayloadHookMismatch) = false (%v)", hook, err)
			}
		}

		err = runPayload.Validate(hook)
		switch hook {
		case HookRunStart:
			if err != nil {
				t.Errorf("%s rejected a valid start payload: %v", hook, err)
			}
		case HookRunFinish:
			if !errors.Is(err, ErrPayloadFieldInvalid) {
				t.Errorf("%s: a starting Run should be a non-terminal status rejection, got %v", hook, err)
			}
		default:
			if !errors.Is(err, ErrPayloadHookMismatch) {
				t.Errorf("%s: errors.Is(ErrPayloadHookMismatch) = false (%v)", hook, err)
			}
		}
	}
}
