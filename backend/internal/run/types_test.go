package run_test

import (
	"encoding/json"
	"testing"

	"github.com/codeflow/backend/internal/run"
)

// TestTaskKindValid pins the tasks.kind value set. Every constant must be
// valid; the empty value and near-miss strings must not be, so that a typo
// fails here rather than at the SQL CHECK in a later layer.
func TestTaskKindValid(t *testing.T) {
	for _, k := range run.TaskKinds {
		if !k.Valid() {
			t.Errorf("TaskKind(%q).Valid() = false, want true", k)
		}
	}
	for _, k := range []run.TaskKind{"", "Code", "documents", "manual ", "codex"} {
		if k.Valid() {
			t.Errorf("TaskKind(%q).Valid() = true, want false", k)
		}
	}
}

func TestTaskStatusValid(t *testing.T) {
	for _, s := range run.TaskStatuses {
		if !s.Valid() {
			t.Errorf("TaskStatus(%q).Valid() = false, want true", s)
		}
	}
	for _, s := range []run.TaskStatus{"", "READY", "waiting-review", "failed_internal", "completed "} {
		if s.Valid() {
			t.Errorf("TaskStatus(%q).Valid() = true, want false", s)
		}
	}
}

func TestRunStatusValid(t *testing.T) {
	for _, s := range run.RunStatuses {
		if !s.Valid() {
			t.Errorf("RunStatus(%q).Valid() = false, want true", s)
		}
	}
	for _, s := range []run.RunStatus{"", "Queued", "waitingapproval", "done", "running "} {
		if s.Valid() {
			t.Errorf("RunStatus(%q).Valid() = true, want false", s)
		}
	}
}

func TestAttemptStatusValid(t *testing.T) {
	for _, s := range run.AttemptStatuses {
		if !s.Valid() {
			t.Errorf("AttemptStatus(%q).Valid() = false, want true", s)
		}
	}
	for _, s := range []run.AttemptStatus{"", "Exited", "killed", "exited "} {
		if s.Valid() {
			t.Errorf("AttemptStatus(%q).Valid() = true, want false", s)
		}
	}
}

func TestRefKindValid(t *testing.T) {
	for _, k := range run.RefKinds {
		if !k.Valid() {
			t.Errorf("RefKind(%q).Valid() = false, want true", k)
		}
	}
	for _, k := range []run.RefKind{"", "Flow", "project", "agentrevision", "agent_revisions"} {
		if k.Valid() {
			t.Errorf("RefKind(%q).Valid() = true, want false", k)
		}
	}
}

func TestProjectRefStateValid(t *testing.T) {
	for _, s := range run.ProjectRefStates {
		if !s.Valid() {
			t.Errorf("ProjectRefState(%q).Valid() = false, want true", s)
		}
	}
	for _, s := range []run.ProjectRefState{"", "Active", "deleted", "archived "} {
		if s.Valid() {
			t.Errorf("ProjectRefState(%q).Valid() = true, want false", s)
		}
	}
}

// TestTaskStatusIsTerminal pins §27.2.7: completed/cancelled never reopen,
// while failed stays retryable through an explicit retry command.
func TestTaskStatusIsTerminal(t *testing.T) {
	want := map[run.TaskStatus]bool{
		run.TaskStatusReady:         false,
		run.TaskStatusQueued:        false,
		run.TaskStatusRunning:       false,
		run.TaskStatusWaitingReview: false,
		run.TaskStatusCompleted:     true,
		run.TaskStatusFailed:        false,
		run.TaskStatusCancelled:     true,
	}
	if len(want) != len(run.TaskStatuses) {
		t.Fatalf("test covers %d statuses, package defines %d", len(want), len(run.TaskStatuses))
	}
	for s := range want {
		if got := s.IsTerminal(); got != want[s] {
			t.Errorf("TaskStatus(%q).IsTerminal() = %v, want %v", s, got, want[s])
		}
	}
	if run.TaskStatus("bogus").IsTerminal() {
		t.Error(`TaskStatus("bogus").IsTerminal() = true, want false`)
	}
}

// TestRunStatusIsTerminal pins the four Run terminal states of §21.1.
func TestRunStatusIsTerminal(t *testing.T) {
	want := map[run.RunStatus]bool{
		run.RunStatusQueued:          false,
		run.RunStatusStarting:        false,
		run.RunStatusRunning:         false,
		run.RunStatusWaitingApproval: false,
		run.RunStatusPaused:          false,
		run.RunStatusCancelling:      false,
		run.RunStatusRecovering:      false,
		run.RunStatusCompleted:       true,
		run.RunStatusFailed:          true,
		run.RunStatusCancelled:       true,
		run.RunStatusExpired:         true,
	}
	if len(want) != len(run.RunStatuses) {
		t.Fatalf("test covers %d statuses, package defines %d", len(want), len(run.RunStatuses))
	}
	terminals := 0
	for s := range want {
		if got := s.IsTerminal(); got != want[s] {
			t.Errorf("RunStatus(%q).IsTerminal() = %v, want %v", s, got, want[s])
		}
		if want[s] {
			terminals++
		}
	}
	if terminals != 4 {
		t.Errorf("terminal Run states = %d, want exactly 4", terminals)
	}
	if run.RunStatus("bogus").IsTerminal() {
		t.Error(`RunStatus("bogus").IsTerminal() = true, want false`)
	}
}

// TestAttemptStatusIsTerminalAndActive pins both predicates and the invariant
// they must satisfy for the partial unique index to mean "one active attempt
// per run": active and terminal are disjoint, and every status is exactly one
// of the two.
func TestAttemptStatusIsTerminalAndActive(t *testing.T) {
	wantTerminal := map[run.AttemptStatus]bool{
		run.AttemptStatusStarting:   false,
		run.AttemptStatusRunning:    false,
		run.AttemptStatusExited:     true,
		run.AttemptStatusTerminated: true,
		run.AttemptStatusAbandoned:  true,
	}
	wantActive := map[run.AttemptStatus]bool{
		run.AttemptStatusStarting:   true,
		run.AttemptStatusRunning:    true,
		run.AttemptStatusExited:     false,
		run.AttemptStatusTerminated: false,
		run.AttemptStatusAbandoned:  false,
	}
	if len(wantTerminal) != len(run.AttemptStatuses) {
		t.Fatalf("test covers %d statuses, package defines %d", len(wantTerminal), len(run.AttemptStatuses))
	}
	for s := range wantTerminal {
		if got := s.IsTerminal(); got != wantTerminal[s] {
			t.Errorf("AttemptStatus(%q).IsTerminal() = %v, want %v", s, got, wantTerminal[s])
		}
		if got := s.IsActive(); got != wantActive[s] {
			t.Errorf("AttemptStatus(%q).IsActive() = %v, want %v", s, got, wantActive[s])
		}
		if s.IsTerminal() && s.IsActive() {
			t.Errorf("AttemptStatus(%q) is both terminal and active", s)
		}
		if !s.IsTerminal() && !s.IsActive() {
			t.Errorf("AttemptStatus(%q) is neither terminal nor active", s)
		}
	}
	if run.AttemptStatus("bogus").IsTerminal() {
		t.Error(`AttemptStatus("bogus").IsTerminal() = true, want false`)
	}
	if run.AttemptStatus("bogus").IsActive() {
		t.Error(`AttemptStatus("bogus").IsActive() = true, want false`)
	}
}

// TestEnumSlicesMatchPackageSet guards the exported list used by runstore's
// schema tests: if a constant is added to one of these slices without being
// declared (or vice versa), the SQL/Go set comparison in schema_test.go would
// silently pass over a wrong set.
func TestEnumSlicesMatchPackageSet(t *testing.T) {
	seen := map[run.TaskStatus]bool{}
	for _, s := range run.TaskStatuses {
		if seen[s] {
			t.Errorf("TaskStatuses contains %q twice", s)
		}
		seen[s] = true
		if !s.Valid() {
			t.Errorf("TaskStatuses contains invalid %q", s)
		}
	}
	runSeen := map[run.RunStatus]bool{}
	for _, s := range run.RunStatuses {
		if runSeen[s] {
			t.Errorf("RunStatuses contains %q twice", s)
		}
		runSeen[s] = true
		if !s.Valid() {
			t.Errorf("RunStatuses contains invalid %q", s)
		}
	}
	attemptSeen := map[run.AttemptStatus]bool{}
	for _, s := range run.AttemptStatuses {
		if attemptSeen[s] {
			t.Errorf("AttemptStatuses contains %q twice", s)
		}
		attemptSeen[s] = true
		if !s.Valid() {
			t.Errorf("AttemptStatuses contains invalid %q", s)
		}
	}
	for _, k := range run.TaskKinds {
		if !k.Valid() {
			t.Errorf("TaskKinds contains invalid %q", k)
		}
	}
	for _, k := range run.RefKinds {
		if !k.Valid() {
			t.Errorf("RefKinds contains invalid %q", k)
		}
	}
	for _, s := range run.ProjectRefStates {
		if !s.Valid() {
			t.Errorf("ProjectRefStates contains invalid %q", s)
		}
	}
}

// TestBudgetUnknownFieldsStayAbsent is the §27.1 rule that an unknown budget is
// nil and must never be rendered as 0: a client that receives
// {"tokens":0,"wall_time_seconds":0} would believe the run is budgeted at zero
// and is allowed to be killed immediately.
func TestBudgetUnknownFieldsStayAbsent(t *testing.T) {
	got, err := json.Marshal(run.Budget{})
	if err != nil {
		t.Fatalf("marshal empty budget: %v", err)
	}
	if string(got) != "{}" {
		t.Errorf("empty Budget marshalled as %s, want {} (unknown must not become 0)", got)
	}

	var decoded run.Budget
	if err := json.Unmarshal([]byte(`{"wall_time_seconds":1800,"tokens":50000}`), &decoded); err != nil {
		t.Fatalf("unmarshal plan §20.1 budget: %v", err)
	}
	if decoded.WallTimeSeconds == nil || *decoded.WallTimeSeconds != 1800 {
		t.Errorf("wall_time_seconds = %v, want 1800", decoded.WallTimeSeconds)
	}
	if decoded.Tokens == nil || *decoded.Tokens != 50000 {
		t.Errorf("tokens = %v, want 50000", decoded.Tokens)
	}
	if decoded.CostLimitMinor != nil {
		t.Errorf("cost_limit_minor = %v, want nil for an absent field", *decoded.CostLimitMinor)
	}

	round, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal decoded budget: %v", err)
	}
	if string(round) != `{"wall_time_seconds":1800,"tokens":50000}` {
		t.Errorf("round-trip = %s, want the §20.1 subset", round)
	}
}

// TestBudgetExplicitZeroIsPreserved is the other half of the rule: an
// explicitly requested zero limit is a real limit and must survive, distinct
// from nil.
func TestBudgetExplicitZeroIsPreserved(t *testing.T) {
	var decoded run.Budget
	if err := json.Unmarshal([]byte(`{"tokens":0}`), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Tokens == nil {
		t.Fatal("tokens = nil, want an explicit 0")
	}
	if *decoded.Tokens != 0 {
		t.Errorf("tokens = %d, want 0", *decoded.Tokens)
	}
	if decoded.WallTimeSeconds != nil {
		t.Errorf("wall_time_seconds = %v, want nil", *decoded.WallTimeSeconds)
	}
}
