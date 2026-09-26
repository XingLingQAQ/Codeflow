package run_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/codeflow/backend/internal/run"
)

// This file transcribes plan §21.1 row by row and then walks the whole
// cross-product of states × triggers, so that a row removed from, or a row
// added to, the package's table fails here instead of in production. The
// expectation table below uses literal wire strings (never the package's
// constants) on purpose: renaming a status or an event in the package must
// break this test, not silently follow along.

const (
	stQueued          = run.RunStatus("queued")
	stStarting        = run.RunStatus("starting")
	stRunning         = run.RunStatus("running")
	stWaitingApproval = run.RunStatus("waiting_approval")
	stPaused          = run.RunStatus("paused")
	stCancelling      = run.RunStatus("cancelling")
	stRecovering      = run.RunStatus("recovering")
	stCompleted       = run.RunStatus("completed")
	stFailed          = run.RunStatus("failed")
	stCancelled       = run.RunStatus("cancelled")
	stExpired         = run.RunStatus("expired")
)

// event builds an event trigger from a literal event type.
func event(name string) run.Trigger { return run.EventTrigger(name) }

// command builds a command trigger from a literal command name.
func command(name string) run.Trigger { return run.CommandTrigger(name) }

// conclusion builds a conclusion trigger from a literal conclusion name.
func conclusion(name string, terminal run.RunStatus) run.Trigger {
	return run.ConclusionTrigger(name, terminal)
}

// planRow is one positive case transcribed from §21.1: the row's current state,
// the row's command/event, and the row's target state.
type planRow struct {
	name    string
	from    run.RunStatus
	trigger run.Trigger
	want    run.RunStatus
}

// planRows is §21.1 verbatim, one entry per table row (a row that lists several
// states or two commands expands into one entry per combination).
var planRows = []planRow{
	{"queued + scheduler.claimed", stQueued, event("scheduler.claimed"), stStarting},
	{"starting + process.started", stStarting, event("process.started"), stRunning},
	{"running + approval.required", stRunning, event("approval.required"), stWaitingApproval},
	{"waiting_approval + approval.approved", stWaitingApproval, event("approval.approved"), stRunning},
	{"running + budget.soft_exceeded", stRunning, event("budget.soft_exceeded"), stRunning},
	{"running + checkpoint.acknowledged", stRunning, event("checkpoint.acknowledged"), stPaused},
	{"paused + run.resume", stPaused, command("run.resume"), stRunning},
	{"running + process.exited(0)", stRunning, run.ProcessExitedTrigger(0), stCompleted},
	{"running + process.exited(!=0)", stRunning, run.ProcessExitedTrigger(1), stFailed},
	{"queued + run.cancel", stQueued, command("run.cancel"), stCancelling},
	{"starting + run.cancel", stStarting, command("run.cancel"), stCancelling},
	{"running + run.cancel", stRunning, command("run.cancel"), stCancelling},
	{"waiting_approval + run.cancel", stWaitingApproval, command("run.cancel"), stCancelling},
	{"paused + run.cancel", stPaused, command("run.cancel"), stCancelling},
	{"queued + hard_deadline", stQueued, command("hard_deadline"), stCancelling},
	{"starting + hard_deadline", stStarting, command("hard_deadline"), stCancelling},
	{"running + hard_deadline", stRunning, command("hard_deadline"), stCancelling},
	{"waiting_approval + hard_deadline", stWaitingApproval, command("hard_deadline"), stCancelling},
	{"paused + hard_deadline", stPaused, command("hard_deadline"), stCancelling},
	{"cancelling + process.terminated", stCancelling, event("process.terminated"), stCancelled},
	{"cancelling + process_kill_failed", stCancelling, conclusion("process_kill_failed", ""), stRecovering},
	{"starting + server.restart", stStarting, event("server.restart"), stRecovering},
	{"running + server.restart", stRunning, event("server.restart"), stRecovering},
	{"waiting_approval + server.restart", stWaitingApproval, event("server.restart"), stRecovering},
	{"paused + server.restart", stPaused, event("server.restart"), stRecovering},
	{"cancelling + server.restart", stCancelling, event("server.restart"), stRecovering},
	{"recovering + process_verified", stRecovering, conclusion("process_verified", ""), stRunning},
	{"recovering + cleaned(failed)", stRecovering, conclusion("cleaned", stFailed), stFailed},
	{"recovering + cleaned(cancelled)", stRecovering, conclusion("cleaned", stCancelled), stCancelled},
	{"recovering + cleaned(expired)", stRecovering, conclusion("cleaned", stExpired), stExpired},
	// The queued-no-process path: a queued Run that is cancelled goes to
	// cancelling like every other cancel, and leaves it through the same
	// "cleaned" conclusion the recovery path uses, because no
	// process.terminated can ever arrive for a Run that never started.
	{"cancelling + cleaned(cancelled)", stCancelling, conclusion("cleaned", stCancelled), stCancelled},
	{"cancelling + cleaned(expired)", stCancelling, conclusion("cleaned", stExpired), stExpired},
	{"completed + merge", stCompleted, command("merge"), stCompleted},
}

// positiveExtras are further §21.1 rows exercised with a different exit code.
// They are positive cases, not extra table rows: the table has one non-zero
// exit row, and Rules()/AllowedTriggers must report one row per §21.1 row.
var positiveExtras = []planRow{
	{"running + process.exited(137)", stRunning, run.ProcessExitedTrigger(137), stFailed},
	{"running + process.exited(-1)", stRunning, run.ProcessExitedTrigger(-1), stFailed},
}

// legalPairs indexes planRows by state + trigger for the exhaustive sweeps.
func legalPairs() map[string]run.RunStatus {
	out := make(map[string]run.RunStatus, len(planRows))
	for _, r := range planRows {
		out[pairKey(r.from, r.trigger)] = r.want
	}
	return out
}

// pairKey names a (state, trigger) combination. RunStatus and Trigger both
// render through %s, which is all this test needs.
func pairKey(from run.RunStatus, trg run.Trigger) string {
	return fmt.Sprintf("%s|%s", from, trg)
}

// TestNextStatusPlanRows is the positive case of every §21.1 row.
func TestNextStatusPlanRows(t *testing.T) {
	for _, r := range append(append([]planRow{}, planRows...), positiveExtras...) {
		t.Run(r.name, func(t *testing.T) {
			got, err := run.NextStatus(r.from, r.trigger)
			if err != nil {
				t.Fatalf("NextStatus(%s, %s) = error %v, want %s", r.from, r.trigger, err, r.want)
			}
			if got != r.want {
				t.Fatalf("NextStatus(%s, %s) = %s, want %s", r.from, r.trigger, got, r.want)
			}
		})
	}
}

// TestNextStatusExhaustiveMatrix walks every state against every trigger the
// table speaks about. A pair is either a §21.1 row (it must move to the row's
// target) or it must be rejected with the Run left where it was; no third
// outcome is allowed.
func TestNextStatusExhaustiveMatrix(t *testing.T) {
	legal := legalPairs()
	triggers := run.AllTriggers()
	combos, accepted, rejected := 0, 0, 0
	for _, from := range run.RunStatuses {
		for _, trg := range triggers {
			combos++
			got, err := run.NextStatus(from, trg)
			want, isLegal := legal[pairKey(from, trg)]
			if isLegal {
				accepted++
				if err != nil {
					t.Errorf("NextStatus(%s, %s) = error %v, want %s (§21.1 row)", from, trg, err, want)
					continue
				}
				if got != want {
					t.Errorf("NextStatus(%s, %s) = %s, want %s (§21.1 row)", from, trg, got, want)
				}
				continue
			}
			rejected++
			if got != from {
				t.Errorf("NextStatus(%s, %s) = %s: a rejected trigger must leave the Run in %s", from, trg, got, from)
			}
			if err == nil {
				t.Errorf("NextStatus(%s, %s) = %s, nil error; the pair is not in §21.1 and must be rejected", from, trg, got)
				continue
			}
			var te *run.TransitionError
			if !errors.As(err, &te) {
				t.Errorf("NextStatus(%s, %s) error is %T, want *run.TransitionError", from, trg, err)
				continue
			}
			if !isKnownCode(te.Code) {
				t.Errorf("NextStatus(%s, %s) error code %q is not a §21.1 code", from, trg, te.Code)
			}
			if te.From != from || te.Trigger != trg {
				t.Errorf("NextStatus(%s, %s) error reports From=%s Trigger=%s, want the request that failed", from, trg, te.From, te.Trigger)
			}
		}
	}
	t.Logf("exhaustive matrix: %d combinations (states=%d, triggers=%d), %d accepted, %d rejected",
		combos, len(run.RunStatuses), len(triggers), accepted, rejected)
	if combos != len(run.RunStatuses)*len(triggers) {
		t.Fatalf("combos = %d, want %d", combos, len(run.RunStatuses)*len(triggers))
	}
	if rejected == 0 || accepted == 0 {
		t.Fatalf("accepted = %d, rejected = %d: the matrix must contain both", accepted, rejected)
	}
}

// TestNextStatusAllEventTypesRejectedExceptTriggers sweeps the whole closed
// event enum (without an exit code) over every state: only the nine event types
// §21.1 uses as triggers may be accepted, and process.exited without a known
// exit code must never be, because the target state depends on the code.
func TestNextStatusAllEventTypesRejectedExceptTriggers(t *testing.T) {
	legal := legalPairs()
	combos, accepted, rejected := 0, 0, 0
	for _, from := range run.RunStatuses {
		for _, et := range run.ExecutionEventTypes {
			combos++
			trg := run.EventTrigger(string(et))
			got, err := run.NextStatus(from, trg)
			want, isLegal := legal[pairKey(from, trg)]
			if isLegal {
				accepted++
				if err != nil || got != want {
					t.Errorf("NextStatus(%s, %s) = (%s, %v), want (%s, nil)", from, trg, got, err, want)
				}
				continue
			}
			rejected++
			if got != from {
				t.Errorf("NextStatus(%s, %s) = %s, want %s unchanged", from, trg, got, from)
			}
			if err == nil {
				t.Errorf("NextStatus(%s, %s) accepted an event type that is not a §21.1 trigger for %s", from, trg, from)
			}
		}
	}
	t.Logf("event-enum sweep: %d combinations (%d states × %d event types), %d accepted, %d rejected",
		combos, len(run.RunStatuses), len(run.ExecutionEventTypes), accepted, rejected)
}

// TestNextStatusMalformedTriggersRejected covers the requests a caller can get
// wrong: an unknown name, the wrong trigger kind for a known name, an exit code
// on a trigger that has none, a "cleaned" conclusion without its terminal, and
// a trigger with no kind at all.
func TestNextStatusMalformedTriggersRejected(t *testing.T) {
	withExit := func(t run.Trigger, code int) run.Trigger { t.ExitCode = &code; return t }
	malformed := []struct {
		name    string
		trigger run.Trigger
	}{
		{"unknown event", run.EventTrigger("run.started")},
		{"unknown event 2", run.EventTrigger("process.killed")},
		{"unknown command", run.CommandTrigger("run.pause")},
		{"unknown conclusion", run.ConclusionTrigger("assumed_ok", "")},
		{"event name as command", run.CommandTrigger("scheduler.claimed")},
		{"command name as event", run.EventTrigger("run.cancel")},
		{"command name as conclusion", run.ConclusionTrigger("run.resume", "")},
		{"conclusion name as event", run.EventTrigger("cleaned")},
		{"no kind", run.Trigger{Name: "scheduler.claimed"}},
		{"no name", run.Trigger{Kind: run.TriggerEvent}},
		{"exit code on a non-exit event", withExit(run.EventTrigger("scheduler.claimed"), 0)},
		{"exit code on a command", withExit(run.CommandTrigger("run.cancel"), 3)},
		{"cleaned without a terminal", run.ConclusionTrigger("cleaned", "")},
		{"cleaned with a non-terminal", run.ConclusionTrigger("cleaned", stRunning)},
		{"cleaned with a bogus terminal", run.ConclusionTrigger("cleaned", run.RunStatus("done"))},
		{"process_verified with a terminal", run.ConclusionTrigger("process_verified", stFailed)},
		{"process.exited without a code", run.EventTrigger("process.exited")},
		{"process.exited as a conclusion", run.ConclusionTrigger("process.exited", "")},
	}
	combos, rejected := 0, 0
	for _, from := range run.RunStatuses {
		for _, m := range malformed {
			combos++
			got, err := run.NextStatus(from, m.trigger)
			if got != from {
				t.Errorf("NextStatus(%s, %s) = %s, want %s unchanged", from, m.trigger, got, from)
			}
			if err == nil {
				t.Errorf("NextStatus(%s, %s) accepted a malformed trigger (%s)", from, m.trigger, m.name)
				continue
			}
			rejected++
			var te *run.TransitionError
			if !errors.As(err, &te) || te.Code != run.CodeInvalidTransition {
				t.Errorf("NextStatus(%s, %s) code = %v, want %s", from, m.trigger, err, run.CodeInvalidTransition)
			}
		}
	}
	t.Logf("malformed sweep: %d combinations (%d states × %d malformed triggers), %d rejected",
		combos, len(run.RunStatuses), len(malformed), rejected)
	if rejected != combos {
		t.Fatalf("rejected = %d, want %d (every malformed trigger must be rejected from every state)", rejected, combos)
	}
}

// TestNextStatusTerminalStatesStayPut proves no trigger can leave a terminal
// state, with the single exception of merge on completed, which is allowed but
// keeps the status at completed.
func TestNextStatusTerminalStatesStayPut(t *testing.T) {
	terminal := []run.RunStatus{stCompleted, stFailed, stCancelled, stExpired}
	merges := 0
	for _, from := range terminal {
		for _, trg := range run.AllTriggers() {
			got, err := run.NextStatus(from, trg)
			if got != from {
				t.Errorf("NextStatus(%s, %s) = %s: no terminal state may change", from, trg, got)
			}
			isMerge := from == stCompleted && trg.Kind == run.TriggerCommand && trg.Name == "merge"
			if isMerge {
				merges++
				if err != nil {
					t.Errorf("NextStatus(%s, %s) = error %v, want completed and no error", from, trg, err)
				}
				continue
			}
			if err == nil {
				t.Errorf("NextStatus(%s, %s) was accepted; only completed + merge is allowed from a terminal state", from, trg)
			}
		}
		if from != stCompleted && len(run.AllowedTriggers(from)) != 0 {
			t.Errorf("AllowedTriggers(%s) = %v, want none", from, run.AllowedTriggers(from))
		}
	}
	if merges != 1 {
		t.Fatalf("merge acceptances from completed = %d, want 1", merges)
	}
}

// TestNextStatusExitCodeBranches pins the two process.exited branches and the
// unknown-exit case.
func TestNextStatusExitCodeBranches(t *testing.T) {
	if got, err := run.NextStatus(stRunning, run.ProcessExitedTrigger(0)); err != nil || got != stCompleted {
		t.Fatalf("exit(0) from running = (%s, %v), want (completed, nil)", got, err)
	}
	for _, code := range []int{1, 2, 137, -1} {
		got, err := run.NextStatus(stRunning, run.ProcessExitedTrigger(code))
		if err != nil || got != stFailed {
			t.Fatalf("exit(%d) from running = (%s, %v), want (failed, nil)", code, got, err)
		}
	}
	unknown := run.EventTrigger("process.exited")
	got, err := run.NextStatus(stRunning, unknown)
	if got != stRunning || err == nil {
		t.Fatalf("process.exited without an exit code = (%s, %v), want (running, error)", got, err)
	}
	if code := errorCode(t, err); code != run.CodeInvalidTransition {
		t.Fatalf("unknown exit code reported %q, want %q", code, run.CodeInvalidTransition)
	}
	if code := run.FailureCodeFor(unknown); code != run.CodeInvalidTransition {
		t.Fatalf("FailureCodeFor(unknown exit) = %q, want %q", code, run.CodeInvalidTransition)
	}
	if code := run.FailureCodeFor(run.ProcessExitedTrigger(0)); code != run.CodeOutputPersistFailed {
		t.Fatalf("FailureCodeFor(exit 0) = %q, want %q", code, run.CodeOutputPersistFailed)
	}
	if code := run.FailureCodeFor(run.ProcessExitedTrigger(9)); code != run.CodeInvalidTransition {
		t.Fatalf("FailureCodeFor(exit 9) = %q, want %q (the plan writes \"—\" for a non-zero exit)", code, run.CodeInvalidTransition)
	}
}

// TestNextStatusBudgetSoftExceededIsASelfLoop pins §21.1's "record the warning,
// never fake a pause": the only accepted budget.soft_exceeded row keeps running.
func TestNextStatusBudgetSoftExceededIsASelfLoop(t *testing.T) {
	got, err := run.NextStatus(stRunning, event("budget.soft_exceeded"))
	if err != nil || got != stRunning {
		t.Fatalf("running + budget.soft_exceeded = (%s, %v), want (running, nil)", got, err)
	}
	for _, from := range run.RunStatuses {
		if from == stRunning {
			continue
		}
		if _, err := run.NextStatus(from, event("budget.soft_exceeded")); err == nil {
			t.Errorf("%s + budget.soft_exceeded was accepted; §21.1 only lists the running row", from)
		}
	}
}

// TestNextStatusFailureCodes pins the failure code §21.1 attaches to each
// rejected operation: a rejected trigger reports the code of its operation when
// the table gives exactly one, and invalid_transition otherwise.
func TestNextStatusFailureCodes(t *testing.T) {
	cases := []struct {
		name    string
		from    run.RunStatus
		trigger run.Trigger
		want    string
	}{
		{"cancel from a terminal state", stCompleted, command("run.cancel"), run.CodeForbidden},
		{"deadline from recovering", stRecovering, command("hard_deadline"), run.CodeForbidden},
		{"resume from failed", stFailed, command("run.resume"), run.CodeConflict},
		{"claim from running", stRunning, event("scheduler.claimed"), run.CodeBackendUnavailable},
		{"process.started from queued", stQueued, event("process.started"), run.CodeProcessStartFailed},
		{"approval.required from paused", stPaused, event("approval.required"), run.CodeApprovalPersistFailed},
		{"approval.approved from running", stRunning, event("approval.approved"), run.CodeApprovalInvalid},
		{"checkpoint from paused", stPaused, event("checkpoint.acknowledged"), run.CodeCapabilityUnavailable},
		{"exit(0) from starting", stStarting, run.ProcessExitedTrigger(0), run.CodeOutputPersistFailed},
		{"exit(1) from paused", stPaused, run.ProcessExitedTrigger(1), run.CodeInvalidTransition},
		{"process.terminated from running", stRunning, event("process.terminated"), run.CodeProcessKillFailed},
		{"kill-failed from running", stRunning, conclusion("process_kill_failed", ""), run.CodeProcessKillFailed},
		{"server.restart from completed", stCompleted, event("server.restart"), run.CodeRecoveryRequired},
		{"merge from running", stRunning, command("merge"), run.CodeInvalidTransition},
		{"budget.soft_exceeded from paused", stPaused, event("budget.soft_exceeded"), run.CodeInvalidTransition},
		{"cleaned(failed) from cancelling", stCancelling, conclusion("cleaned", stFailed), run.CodeInvalidTransition},
		{"consequence event as trigger", stRunning, event("run.failed"), run.CodeInvalidTransition},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := run.NextStatus(c.from, c.trigger)
			if got != c.from {
				t.Fatalf("NextStatus(%s, %s) = %s, want %s unchanged", c.from, c.trigger, got, c.from)
			}
			if err == nil {
				t.Fatalf("NextStatus(%s, %s) was accepted, want %s", c.from, c.trigger, c.want)
			}
			if code := errorCode(t, err); code != c.want {
				t.Fatalf("NextStatus(%s, %s) code = %q, want %q", c.from, c.trigger, code, c.want)
			}
			if got := run.FailureCodeFor(c.trigger); got != c.want {
				t.Fatalf("FailureCodeFor(%s) = %q, want %q", c.trigger, got, c.want)
			}
		})
	}
}

// TestNextStatusInvalidFromState rejects a state that is not a RunStatus at all.
func TestNextStatusInvalidFromState(t *testing.T) {
	for _, from := range []run.RunStatus{"", "Queued", "done", "running "} {
		got, err := run.NextStatus(from, event("scheduler.claimed"))
		if got != from || err == nil {
			t.Fatalf("NextStatus(%q, scheduler.claimed) = (%s, %v), want the state unchanged and an error", from, got, err)
		}
		if code := errorCode(t, err); code != run.CodeInvalidTransition {
			t.Fatalf("NextStatus(%q, ...) code = %q, want %q", from, code, run.CodeInvalidTransition)
		}
	}
}

// TestRulesExposeThePlanTable checks that Rules() is the §21.1 table as data:
// every row's representative trigger is accepted by NextStatus and lands on the
// row's target, and every row carries a condition and a known code.
func TestRulesExposeThePlanTable(t *testing.T) {
	rules := run.Rules()
	if len(rules) != len(planRows) {
		t.Fatalf("len(Rules()) = %d, want %d (§21.1 rows)", len(rules), len(planRows))
	}
	legal := legalPairs()
	for i, r := range rules {
		key := pairKey(r.From, r.Trigger)
		want, ok := legal[key]
		if !ok {
			t.Errorf("Rules()[%d] = %s + %s, which is not one of the transcribed §21.1 rows", i, r.From, r.Trigger)
			continue
		}
		if r.To != want {
			t.Errorf("Rules()[%d] (%s + %s) To = %s, want %s", i, r.From, r.Trigger, r.To, want)
		}
		if r.Condition == "" {
			t.Errorf("Rules()[%d] (%s + %s) has no condition text", i, r.From, r.Trigger)
		}
		if r.FailureCode != "" && !isKnownCode(r.FailureCode) {
			t.Errorf("Rules()[%d] failure code %q is not a §21.1 code", i, r.FailureCode)
		}
		got, err := run.NextStatus(r.From, r.Trigger)
		if err != nil || got != r.To {
			t.Errorf("NextStatus(%s, %s) = (%s, %v), but Rules() declares %s", r.From, r.Trigger, got, err, r.To)
		}
	}
	// The map used by FailureCodeFor must not drift from the table's codes: a
	// row's non-empty code is the code its operation reports when rejected.
	for _, r := range rules {
		if r.FailureCode == "" {
			continue
		}
		if got := run.FailureCodeFor(r.Trigger); got != r.FailureCode {
			t.Errorf("FailureCodeFor(%s) = %q, but Rules() row says %q", r.Trigger, got, r.FailureCode)
		}
	}
}

// TestAllowedTriggers checks the per-state trigger list used by UI and audit.
func TestAllowedTriggers(t *testing.T) {
	legal := legalPairs()
	counted := 0
	for _, from := range run.RunStatuses {
		allowed := run.AllowedTriggers(from)
		if len(allowed) == 0 && from != stFailed && from != stCancelled && from != stExpired {
			t.Errorf("AllowedTriggers(%s) is empty, want at least one trigger", from)
		}
		for _, trg := range allowed {
			counted++
			want, ok := legal[pairKey(from, trg)]
			if !ok {
				t.Errorf("AllowedTriggers(%s) contains %s, which is not a §21.1 row", from, trg)
				continue
			}
			if got, err := run.NextStatus(from, trg); err != nil || got != want {
				t.Errorf("NextStatus(%s, %s) = (%s, %v), want (%s, nil)", from, trg, got, err, want)
			}
		}
	}
	if counted != len(planRows) {
		t.Fatalf("AllowedTriggers covered %d rows, want %d", counted, len(planRows))
	}
	// The returned slice and its ExitCode pointers are the caller's to keep.
	allowed := run.AllowedTriggers(stRunning)
	if len(allowed) == 0 {
		t.Fatal("AllowedTriggers(running) is empty")
	}
	allowed[0].Name = "mutated"
	if again := run.AllowedTriggers(stRunning); again[0].Name == "mutated" {
		t.Fatal("AllowedTriggers returns shared state: mutating the result changed the next call")
	}
	for _, trg := range run.AllowedTriggers(stRunning) {
		if trg.ExitCode != nil {
			*trg.ExitCode = 99
			break
		}
	}
	for _, trg := range run.AllowedTriggers(stRunning) {
		if trg.ExitCode != nil && *trg.ExitCode == 99 {
			t.Fatal("AllowedTriggers shares ExitCode pointers between calls")
		}
	}
}

// TestAllTriggersVocabulary checks the canonical vocabulary itself.
func TestAllTriggersVocabulary(t *testing.T) {
	triggers := run.AllTriggers()
	if len(triggers) == 0 {
		t.Fatal("AllTriggers() is empty")
	}
	seen := map[string]bool{}
	for _, trg := range triggers {
		if !trg.Kind.Valid() {
			t.Errorf("AllTriggers() contains %s with an invalid kind", trg)
		}
		if trg.Name == "" {
			t.Errorf("AllTriggers() contains a trigger with no name: %s", trg)
		}
		if seen[trg.String()] {
			t.Errorf("AllTriggers() repeats %s", trg)
		}
		seen[trg.String()] = true
	}
	if len(triggers) != len(seen) {
		t.Fatalf("AllTriggers() has %d entries but %d distinct triggers", len(triggers), len(seen))
	}
	// Every event-kind trigger name must be a real event type; commands and
	// conclusions must be the §21.1 names, not event types.
	for _, trg := range triggers {
		switch trg.Kind {
		case run.TriggerEvent:
			if !run.ExecutionEventType(trg.Name).Valid() {
				t.Errorf("%s is not an execution event type", trg)
			}
		case run.TriggerCommand:
			if !contains(run.Commands, trg.Name) {
				t.Errorf("%s is not a §21.1 command", trg)
			}
			if run.ExecutionEventType(trg.Name).Valid() {
				t.Errorf("%s is an event type and must not be a command", trg)
			}
		case run.TriggerConclusion:
			if !contains(run.Conclusions, trg.Name) {
				t.Errorf("%s is not a §21.1 recoverer conclusion", trg)
			}
			if run.ExecutionEventType(trg.Name).Valid() {
				t.Errorf("%s is an event type and must not be a conclusion", trg)
			}
		}
	}
}

// TestTriggerString pins the rendering used in logs and error messages.
func TestTriggerString(t *testing.T) {
	cases := []struct {
		trigger run.Trigger
		want    string
	}{
		{run.EventTrigger("scheduler.claimed"), "event:scheduler.claimed"},
		{run.ProcessExitedTrigger(0), "event:process.exited(exit=0)"},
		{run.CommandTrigger("run.cancel"), "command:run.cancel"},
		{run.ConclusionTrigger("cleaned", stExpired), "conclusion:cleaned(expired)"},
		{run.Trigger{}, ":"},
	}
	for _, c := range cases {
		if got := c.trigger.String(); got != c.want {
			t.Errorf("Trigger.String() = %q, want %q", got, c.want)
		}
	}
}

// TestFailureCodesAreThePlanCodes checks the exported code set.
func TestFailureCodesAreThePlanCodes(t *testing.T) {
	want := map[string]bool{
		"invalid_transition": true, "backend_unavailable": true,
		"process_start_failed": true, "approval_persist_failed": true,
		"approval_invalid": true, "capability_unavailable": true,
		"conflict": true, "output_persist_failed": true, "forbidden": true,
		"process_kill_failed": true, "recovery_required": true,
		"base_changed": true, "merge_conflict": true, "guard_blocked": true,
	}
	if len(run.FailureCodes) != len(want) {
		t.Fatalf("len(FailureCodes) = %d, want %d", len(run.FailureCodes), len(want))
	}
	seen := map[string]bool{}
	for _, code := range run.FailureCodes {
		if !want[code] {
			t.Errorf("FailureCodes contains %q, which §21.1 does not name", code)
		}
		if seen[code] {
			t.Errorf("FailureCodes repeats %q", code)
		}
		seen[code] = true
	}
}

// TestRetryableStartFailure pins §15/§27.2: an automatic retry is allowed only
// for a start the provider never accepted and that began no side effect.
func TestRetryableStartFailure(t *testing.T) {
	cases := []struct {
		name string
		f    run.StartFailure
		want bool
	}{
		{"never accepted, nothing done", run.StartFailure{}, true},
		{"provider accepted the start", run.StartFailure{ProviderAccepted: true}, false},
		{"side effect began", run.StartFailure{SideEffectsStarted: true}, false},
		{"accepted and executed", run.StartFailure{ProviderAccepted: true, SideEffectsStarted: true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := run.RetryableStartFailure(c.f); got != c.want {
				t.Fatalf("RetryableStartFailure(%+v) = %v, want %v", c.f, got, c.want)
			}
		})
	}
}

// errorCode extracts the §21.1 code from a NextStatus error.
func errorCode(t *testing.T, err error) string {
	t.Helper()
	var te *run.TransitionError
	if !errors.As(err, &te) {
		t.Fatalf("error %v is %T, want *run.TransitionError", err, err)
	}
	return te.Code
}

// isKnownCode reports whether code is one of the exported §21.1 codes.
func isKnownCode(code string) bool {
	for _, c := range run.FailureCodes {
		if c == code {
			return true
		}
	}
	return false
}

// contains reports whether list holds value.
func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

// TestTransitionErrorRendering keeps the error message readable.
func TestTransitionErrorRendering(t *testing.T) {
	_, err := run.NextStatus(stCompleted, command("run.cancel"))
	if err == nil {
		t.Fatal("completed + run.cancel was accepted")
	}
	want := fmt.Sprintf("run: transition %s --%s--> not allowed: %s", stCompleted, command("run.cancel"), run.CodeForbidden)
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}
