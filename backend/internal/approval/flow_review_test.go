package approval_test

// Tests added by the main agent's acceptance review of T2.02.b. Each one pins a
// defect found in review and fails on the code as delivered:
//
//   - the low-risk whitelist contained programs that write files or run other
//     programs, and network tools were classified as plain reads;
//   - the risk was classified from Action.Command while the approval bound
//     FingerprintInput.Command, and nothing required the two to be the same argv;
//   - with parallel tool calls, consuming an auto-approved low-risk call released
//     a Run that was waiting for a different approval, and a low-risk call asked
//     while the Run waited was refused outright;
//   - ResolveDeniedTx released a waiting Run on any denied approval of that Run,
//     including an older one that had already been resolved.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/approval"
	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
)

// TestClassifyToolRiskWhitelistHasNoWriterOrRunner: "low" means auto-approved
// with nobody asked, so a program that can write a file, change system state,
// run another program or send data out must never be low.
func TestClassifyToolRiskWhitelistHasNoWriterOrRunner(t *testing.T) {
	notLow := []struct {
		name   string
		action approval.ToolAction
	}{
		{"uniq writes its second operand", approval.ToolAction{Command: []string{"uniq", "in.txt", "out.txt"}}},
		{"yq -i edits in place", approval.ToolAction{Command: []string{"yq", "-i", ".a = 1", "f.yaml"}}},
		{"tree -o writes a file", approval.ToolAction{Command: []string{"tree", "-o", "out.txt"}}},
		{"file -C writes a compiled magic file", approval.ToolAction{Command: []string{"file", "-C", "-m", "magic"}}},
		{"hostname NAME sets the host name", approval.ToolAction{Command: []string{"hostname", "evil"}}},
		{"date -s sets the clock", approval.ToolAction{Command: []string{"date", "-s", "2020-01-01"}}},
		{"printenv prints secrets from the environment", approval.ToolAction{Command: []string{"printenv"}}},
		{"git log --output= writes a file", approval.ToolAction{Command: []string{"git", "log", "--output=/tmp/x"}}},
		{"git diff --output writes a file", approval.ToolAction{Command: []string{"git", "diff", "--output", "x.patch"}}},
		{"git diff --ext-diff runs the configured diff program", approval.ToolAction{Command: []string{"git", "diff", "--ext-diff"}}},
		{"git show --textconv runs the configured filter", approval.ToolAction{Command: []string{"git", "show", "--textconv", "HEAD:a.bin"}}},
		{"rg --pre runs a program per file", approval.ToolAction{Command: []string{"rg", "--pre", "./evil.sh", "x"}}},
		{"rg --pre= runs a program per file", approval.ToolAction{Command: []string{"rg", "--pre=./evil.sh", "x"}}},
		{"web fetch sends data out", approval.ToolAction{Tool: "WebFetch"}},
		{"web search sends data out", approval.ToolAction{Tool: "web_search"}},
		{"fetch sends data out", approval.ToolAction{Tool: "fetch"}},
		{"http get sends data out", approval.ToolAction{Tool: "http_get"}},
		{"MCP resource read runs an external server", approval.ToolAction{Tool: "read_resource"}},
	}
	for _, tc := range notLow {
		t.Run(tc.name, func(t *testing.T) {
			risk, reasons := approval.ClassifyToolRisk(tc.action, approval.RiskConfig{})
			if risk == approval.RiskLow {
				t.Fatalf("risk = low (%v), want medium or high: a low action is approved with nobody asked", reasons)
			}
			if len(reasons) == 0 {
				t.Fatalf("no reason returned")
			}
		})
	}

	// Controls: the plain reads the table exists for stay low.
	stillLow := []approval.ToolAction{
		{Command: []string{"git", "status", "--porcelain"}},
		{Command: []string{"git", "log", "--oneline", "-5"}},
		{Command: []string{"git", "diff", "--output-indicator-new=+"}},
		{Command: []string{"rg", "-n", "needle", "src"}},
		{Command: []string{"rg", "--pre-glob", "*.gz", "needle"}},
		{Command: []string{"cat", "a.go"}},
		{Tool: "read_file"},
		{Tool: "Grep"},
	}
	for _, action := range stillLow {
		if risk, reasons := approval.ClassifyToolRisk(action, approval.RiskConfig{}); risk != approval.RiskLow {
			t.Errorf("%+v: risk = %s (%v), want low", action, risk, reasons)
		}
	}
}

// TestRequestCommandMustMatchTheBoundCommand: the service classifies the risk
// itself so a caller cannot name its own; that only holds if the command it
// classifies is the command the approval binds. A request whose Action.Command
// is not exactly FingerprintInput.Command is refused with nothing written —
// otherwise "ls" would be classified (low, auto-approved) while "npm test" is
// bound, and the grant would later be spent on npm test.
func TestRequestCommandMustMatchTheBoundCommand(t *testing.T) {
	cases := []struct {
		name   string
		action approval.ToolAction
	}{
		{"read-only command classified, runner bound", approval.ToolAction{Tool: "run_shell", Command: []string{"ls"}}},
		{"no command classified, a command bound", approval.ToolAction{Tool: "read_file"}},
		{"same program, different arguments", approval.ToolAction{Tool: "run_shell", Command: []string{"npm", "install"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fl := newFlow(t)
			fl.running()
			in := fl.toolRequest(t, "ap-mismatch", "tc_mismatch") // binds npm test
			in.Action = tc.action

			approvalsBefore := fl.f.countRows(t, "approvals")
			eventsBefore := fl.f.countRows(t, "events")
			_, err := fl.requestTool(in)
			if !errors.Is(err, approval.ErrInvalidApproval) {
				t.Fatalf("error = %v, want ErrInvalidApproval", err)
			}
			if got := fl.f.countRows(t, "approvals"); got != approvalsBefore {
				t.Errorf("approvals = %d, want %d (nothing written)", got, approvalsBefore)
			}
			if got := fl.f.countRows(t, "events"); got != eventsBefore {
				t.Errorf("events = %d, want %d (nothing written)", got, eventsBefore)
			}
			if got := fl.status(fxRun); got != run.RunStatusRunning {
				t.Errorf("run status = %s, want running", got)
			}
		})
	}
}

// lowRiskRequest is a read-only call for the fixture's run, with the command
// classified and the command bound being the same argv.
func (fl *flow) lowRiskRequest(t *testing.T, id, toolCallID string) approval.RequestToolInput {
	t.Helper()
	in := fl.toolRequest(t, id, toolCallID)
	in.Action = approval.ToolAction{Tool: "run_shell", Command: []string{"ls"}}
	in.FingerprintInput.ArgumentsHash = argumentsHash(t, "run_shell", `{"command":"ls"}`)
	in.FingerprintInput.Command = []string{"ls"}
	return in
}

// TestRequestLowRiskWhileWaitingIsAutoApproved: a backend may issue tool calls
// in parallel. While one call waits for a human, a read-only call of the same
// turn is still low risk: it is auto-approved and the Run keeps waiting for the
// call it is actually waiting for. Only a second call that itself needs a human
// is refused (S1).
func TestRequestLowRiskWhileWaitingIsAutoApproved(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	fl.mustRequestTool(fl.toolRequest(t, "ap-high", "tc_high"))
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Fatalf("run status = %s, want waiting_approval", got)
	}

	result, err := fl.requestTool(fl.lowRiskRequest(t, "ap-low", "tc_low"))
	if err != nil {
		t.Fatalf("low-risk request while waiting: %v", err)
	}
	if !result.AutoApproved || result.Approval.Status != approval.StatusApproved {
		t.Fatalf("AutoApproved = %v, status = %s; want an auto-approval", result.AutoApproved, result.Approval.Status)
	}
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Errorf("run status = %s, want still waiting_approval (for ap-high)", got)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalRequired)); got != 1 {
		t.Errorf("approval.required events = %d, want 1 (only ap-high's)", got)
	}
}

// TestConsumeLowRiskWhileWaitingKeepsTheRunWaiting: consuming an auto-approved
// low-risk call never releases a Run that waits for another approval — not
// while that approval is pending, and not after it was approved but before it
// was consumed. Only the approval the Run waits for releases it.
func TestConsumeLowRiskWhileWaitingKeepsTheRunWaiting(t *testing.T) {
	for _, decideFirst := range []bool{false, true} {
		name := "waited approval still pending"
		if decideFirst {
			name = "waited approval approved, not yet consumed"
		}
		t.Run(name, func(t *testing.T) {
			fl := newFlow(t)
			fl.running()
			lowIn := fl.lowRiskRequest(t, "ap-low", "tc_low")
			low := fl.mustRequestTool(lowIn).Approval // asked while running
			highIn := fl.toolRequest(t, "ap-high", "tc_high")
			high := fl.mustRequestTool(highIn).Approval
			if decideFirst {
				var err error
				high, err = fl.decide(approval.DecisionInput{
					ApprovalID: high.ID, ExpectedRevision: high.Revision, Approved: true,
					DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
					Now: fl.f.now.Add(30 * time.Second),
				})
				if err != nil {
					t.Fatalf("DecideTx: %v", err)
				}
			}

			result, err := fl.consume(fl.consumeInput(t, low, lowIn.FingerprintInput), nil)
			if err != nil {
				t.Fatalf("consume the low-risk call: %v", err)
			}
			if !result.Granted || result.RunMoved {
				t.Fatalf("Granted = %v, RunMoved = %v; want granted without moving the run", result.Granted, result.RunMoved)
			}
			if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
				t.Fatalf("run status = %s, want still waiting_approval (for ap-high)", got)
			}
			if got := fl.countEvents(fxRun, string(run.EventApprovalApproved)); got != 0 {
				t.Fatalf("approval.approved events = %d, want 0", got)
			}

			if !decideFirst {
				return
			}
			moved, err := fl.consume(fl.consumeInput(t, high, highIn.FingerprintInput), nil)
			if err != nil {
				t.Fatalf("consume the waited approval: %v", err)
			}
			if !moved.Granted || !moved.RunMoved {
				t.Fatalf("Granted = %v, RunMoved = %v; want the waited approval to release the run", moved.Granted, moved.RunMoved)
			}
			if got := fl.status(fxRun); got != run.RunStatusRunning {
				t.Errorf("run status = %s, want running", got)
			}
		})
	}
}

// TestConsumeRefusesAnApprovedApprovalTheRunIsNotWaitingFor: an approved,
// unconsumed tool approval that is not the one the Run waits for cannot
// release the Run. The state is produced by moving the Run out of
// waiting_approval without consuming ap-old (standing in for any path that does
// so), then asking ap-new, which the Run now waits for.
func TestConsumeRefusesAnApprovedApprovalTheRunIsNotWaitingFor(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	oldIn := fl.toolRequest(t, "ap-old", "tc_old")
	old := fl.mustRequestTool(oldIn).Approval
	old, err := fl.decide(approval.DecisionInput{
		ApprovalID: old.ID, ExpectedRevision: old.Revision, Approved: true,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(10 * time.Second),
	})
	if err != nil {
		t.Fatalf("DecideTx(ap-old): %v", err)
	}
	fl.moveRun(fxRun, run.RunStatusWaitingApproval, run.EventTrigger(string(run.EventApprovalApproved)))

	newIn := fl.toolRequest(t, "ap-new", "tc_new")
	newer := fl.mustRequestTool(newIn).Approval
	if _, err := fl.decide(approval.DecisionInput{
		ApprovalID: newer.ID, ExpectedRevision: newer.Revision, Approved: true,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(20 * time.Second),
	}); err != nil {
		t.Fatalf("DecideTx(ap-new): %v", err)
	}
	approvedBefore := fl.countEvents(fxRun, string(run.EventApprovalApproved))

	_, err = fl.consume(fl.consumeInput(t, old, oldIn.FingerprintInput), nil)
	if !errors.Is(err, approval.ErrApprovalRunNotConsumable) {
		t.Fatalf("consume ap-old error = %v, want ErrApprovalRunNotConsumable (the run waits for ap-new)", err)
	}
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Errorf("run status = %s, want still waiting_approval", got)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalApproved)); got != approvedBefore {
		t.Errorf("approval.approved events = %d, want %d", got, approvedBefore)
	}
	if got := fl.f.countRows(t, "approval_consumptions"); got != 0 {
		t.Errorf("receipts = %d, want 0", got)
	}
}

// TestResolveDeniedRefusesADenialTheRunIsNotWaitingFor: resolving an older,
// already resolved denial must not release a Run that now waits for a newer
// request; only the denial of the approval the Run waits for does.
func TestResolveDeniedRefusesADenialTheRunIsNotWaitingFor(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	backend := run.Actor{Type: run.ActorTypeSystem, ID: "backend"}
	reject := func(a approval.Approval, at time.Duration) {
		t.Helper()
		if _, err := fl.decide(approval.DecisionInput{
			ApprovalID: a.ID, ExpectedRevision: a.Revision, Approved: false,
			DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
			Now: fl.f.now.Add(at),
		}); err != nil {
			t.Fatalf("DecideTx(%s): %v", a.ID, err)
		}
	}

	first := fl.mustRequestTool(fl.toolRequest(t, "ap-first", "tc_first")).Approval
	reject(first, 10*time.Second)
	if _, _, err := fl.resolveDenied(first.ID, backend, fl.f.now.Add(20*time.Second)); err != nil {
		t.Fatalf("ResolveDeniedTx(ap-first): %v", err)
	}
	second := fl.mustRequestTool(fl.toolRequest(t, "ap-second", "tc_second")).Approval
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Fatalf("run status = %s, want waiting_approval (for ap-second)", got)
	}

	_, _, err := fl.resolveDenied(first.ID, backend, fl.f.now.Add(30*time.Second))
	if !errors.Is(err, approval.ErrApprovalConflict) {
		t.Fatalf("resolving the old denial: error = %v, want ErrApprovalConflict", err)
	}
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Fatalf("run status = %s, want still waiting_approval (ap-second is pending)", got)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalDenied)); got != 1 {
		t.Fatalf("approval.denied events = %d, want 1 (only ap-first's)", got)
	}

	// Control: the denial the Run waits for does release it.
	reject(second, 40*time.Second)
	if _, moved, err := fl.resolveDenied(second.ID, backend, fl.f.now.Add(50*time.Second)); err != nil {
		t.Fatalf("ResolveDeniedTx(ap-second): %v", err)
	} else if moved.To != run.RunStatusRunning {
		t.Errorf("transition to = %s, want running", moved.To)
	}
}

// TestResolveDeniedRefusesAGateDenial: a gate approval bound to a Run never put
// the Run into waiting_approval (it writes no approval.required), so its
// rejection cannot take the Run out of it either.
func TestResolveDeniedRefusesAGateDenial(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	fl.mustRequestTool(fl.toolRequest(t, "ap-tool", "tc_tool")) // the run waits for this

	var gate approval.Approval
	err := fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		var requestErr error
		gate, requestErr = approval.RequestApprovalTx(ctx, tx, approval.RequestApprovalInput{
			ID: "ap-gate", ProjectID: fl.f.projID, RunID: fl.f.runID,
			SubjectType: approval.SubjectGate, SubjectID: "stage-1", Risk: approval.RiskMedium,
			FingerprintInput: approval.FingerprintInput{
				ProjectID:     fl.f.projID,
				SubjectType:   approval.SubjectGate,
				SubjectID:     "stage-1",
				PolicyVersion: policy.RuleVersion,
			},
			PolicyVersion: policy.RuleVersion,
			RequestedAt:   fl.f.now, ExpiresAt: fl.f.now.Add(time.Hour),
		})
		return requestErr
	})
	if err != nil {
		t.Fatalf("RequestApprovalTx(gate): %v", err)
	}
	if _, err := fl.decide(approval.DecisionInput{
		ApprovalID: gate.ID, ExpectedRevision: gate.Revision, Approved: false,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(10 * time.Second),
	}); err != nil {
		t.Fatalf("DecideTx(gate): %v", err)
	}

	_, _, err = fl.resolveDenied(gate.ID, run.Actor{Type: run.ActorTypeSystem, ID: "backend"}, fl.f.now.Add(20*time.Second))
	if !errors.Is(err, approval.ErrApprovalConflict) {
		t.Fatalf("error = %v, want ErrApprovalConflict", err)
	}
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Errorf("run status = %s, want still waiting_approval (for ap-tool)", got)
	}
}

// TestDecidePendingRevisionMismatchIsAConflict pins the decision CAS on a row
// that is still pending. TestDecideStaleRevisionReturnsCurrentRow covers a row
// somebody already decided, which is refused before the revision is compared;
// here the row is pending but moved on (its deadline was extended, which
// migration 009 allows on a pending row and which bumps the revision), so a
// human answering the revision they saw must get the current row back and
// change nothing.
func TestDecidePendingRevisionMismatchIsAConflict(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	pending := fl.mustRequestTool(fl.toolRequest(t, "ap-cas", "tc_cas")).Approval

	if _, err := fl.f.store.DB().ExecContext(fl.f.ctx,
		`UPDATE approvals SET expires_at = expires_at + 60000, revision = revision + 1 WHERE id = ?`,
		pending.ID); err != nil {
		t.Fatalf("extend the pending approval's deadline: %v", err)
	}

	current, err := fl.decide(approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: true,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(time.Minute),
	})
	if !errors.Is(err, approval.ErrApprovalConflict) {
		t.Fatalf("error = %v, want ErrApprovalConflict", err)
	}
	if current.Revision != pending.Revision+1 || current.Status != approval.StatusPending {
		t.Fatalf("returned row = %s rev %d, want the current row (pending rev %d)",
			current.Status, current.Revision, pending.Revision+1)
	}
	if status, revision := fl.approvalStatus(pending.ID); status != approval.StatusPending || revision != pending.Revision+1 {
		t.Fatalf("stored = %s rev %d, want pending rev %d (unchanged)", status, revision, pending.Revision+1)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalDecided)); got != 0 {
		t.Fatalf("approval.decided events = %d, want 0", got)
	}

	// Answering the current revision succeeds.
	decided, err := fl.decide(approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: current.Revision, Approved: true,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(time.Minute),
	})
	if err != nil || decided.Status != approval.StatusApproved {
		t.Fatalf("decide at the current revision: %s, %v; want approved", decided.Status, err)
	}
}

// eventPayloads returns the decoded payloads of one run's events of a type, in
// run order.
func (fl *flow) eventPayloads(runID, eventType string) []map[string]any {
	fl.t.Helper()
	events, _, err := runstore.ListRunEvents(fl.f.ctx, fl.f.store.DB(), runID, 0, 1000)
	if err != nil {
		fl.t.Fatalf("list events of %s: %v", runID, err)
	}
	var out []map[string]any
	for _, e := range events {
		if e.Type != eventType {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			fl.t.Fatalf("decode %s payload: %v", eventType, err)
		}
		out = append(out, payload)
	}
	return out
}

// TestResolveDeniedReleasesTheRunForAStaleGrant is CA-4: an approved grant the
// waiting call can no longer use. The target content changed after the human
// approved, so ConsumeTx refuses (the old decision is unconsumable history,
// §27.2.3). Once the refusal reached the backend, ResolveDeniedTx returns the
// Run to running with reason "stale"; the old grant can never be spent
// afterwards, and the changed call can ask for a new approval.
func TestResolveDeniedReleasesTheRunForAStaleGrant(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	backend := run.Actor{Type: run.ActorTypeSystem, ID: "backend"}
	userDecides := func(a approval.Approval, at time.Duration) approval.Approval {
		t.Helper()
		decided, err := fl.decide(approval.DecisionInput{
			ApprovalID: a.ID, ExpectedRevision: a.Revision, Approved: true,
			DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
			Now: fl.f.now.Add(at),
		})
		if err != nil {
			t.Fatalf("DecideTx(%s): %v", a.ID, err)
		}
		return decided
	}

	oldIn := fl.toolRequest(t, "ap-old", "tc_old")
	old := userDecides(fl.mustRequestTool(oldIn).Approval, 10*time.Second)

	changed := oldIn.FingerprintInput
	changed.BaseManifestHash = "sha256:manifest-after-an-edit"
	if _, err := fl.consume(fl.consumeInput(t, old, changed), nil); !errors.Is(err, approval.ErrApprovalBindingChanged) {
		t.Fatalf("consume with changed target content: error = %v, want ErrApprovalBindingChanged", err)
	}
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Fatalf("run status = %s, want waiting_approval (still waiting for ap-old)", got)
	}

	_, moved, err := fl.resolveDenied(old.ID, backend, fl.f.now.Add(20*time.Second))
	if err != nil {
		t.Fatalf("ResolveDeniedTx(stale grant): %v", err)
	}
	if moved.From != run.RunStatusWaitingApproval || moved.To != run.RunStatusRunning {
		t.Fatalf("transition = %s -> %s, want waiting_approval -> running", moved.From, moved.To)
	}
	denied := fl.eventPayloads(fxRun, string(run.EventApprovalDenied))
	if len(denied) != 1 || denied[0]["reason"] != "stale" || denied[0]["approval_id"] != old.ID {
		t.Fatalf("approval.denied payloads = %v, want one with reason stale for %s", denied, old.ID)
	}
	if status, _ := fl.approvalStatus(old.ID); status != approval.StatusApproved {
		t.Fatalf("ap-old status = %s, want approved (the decision stays as history)", status)
	}

	// The old grant can never be spent now, not even with the original binding.
	if _, err := fl.consume(fl.consumeInput(t, old, oldIn.FingerprintInput), nil); !errors.Is(err, approval.ErrApprovalRunNotConsumable) {
		t.Fatalf("consume the stale grant after the denial: error = %v, want ErrApprovalRunNotConsumable", err)
	}
	if got := fl.f.countRows(t, "approval_consumptions"); got != 0 {
		t.Fatalf("receipts = %d, want 0", got)
	}

	// The changed call asks again and is approved and spent normally.
	newIn := fl.toolRequest(t, "ap-new", "tc_new")
	newIn.FingerprintInput.BaseManifestHash = changed.BaseManifestHash
	newer := userDecides(fl.mustRequestTool(newIn).Approval, 30*time.Second)
	result, err := fl.consume(fl.consumeInput(t, newer, newIn.FingerprintInput), nil)
	if err != nil || !result.Granted || !result.RunMoved {
		t.Fatalf("consume the new approval: granted=%v moved=%v err=%v; want granted and the run released",
			result.Granted, result.RunMoved, err)
	}
	if _, _, err := fl.resolveDenied(old.ID, backend, fl.f.now.Add(40*time.Second)); !errors.Is(err, approval.ErrApprovalConflict) {
		t.Fatalf("resolving the old grant again: error = %v, want ErrApprovalConflict", err)
	}
}

// TestResolveDeniedRefusesAPendingApproval: a request nobody answered yet is not
// a denial; the Run keeps waiting.
func TestResolveDeniedRefusesAPendingApproval(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	pending := fl.mustRequestTool(fl.toolRequest(t, "ap-pending", "tc_pending")).Approval
	_, _, err := fl.resolveDenied(pending.ID, run.Actor{Type: run.ActorTypeSystem, ID: "backend"}, fl.f.now.Add(time.Second))
	if !errors.Is(err, approval.ErrApprovalAlreadyDecided) {
		t.Fatalf("error = %v, want ErrApprovalAlreadyDecided (pending is not a denial)", err)
	}
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Fatalf("run status = %s, want waiting_approval", got)
	}
}

// TestDecideCommentIsRecorded: the decider's note lands in the approval.decided
// payload; an over-long note is refused before anything is written.
func TestDecideCommentIsRecorded(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	pending := fl.mustRequestTool(fl.toolRequest(t, "ap-note", "tc_note")).Approval
	in := approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: false,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(time.Minute),
	}

	tooLong := in
	tooLong.Comment = strings.Repeat("长", approval.MaxDecisionCommentRunes+1)
	if _, err := fl.decide(tooLong); !errors.Is(err, approval.ErrInvalidApproval) {
		t.Fatalf("over-long comment: error = %v, want ErrInvalidApproval", err)
	}
	if status, revision := fl.approvalStatus(pending.ID); status != approval.StatusPending || revision != pending.Revision {
		t.Fatalf("after a refused decision: %s rev %d, want pending rev %d", status, revision, pending.Revision)
	}

	in.Comment = "  不要在发布分支上跑安装脚本  "
	if _, err := fl.decide(in); err != nil {
		t.Fatalf("DecideTx: %v", err)
	}
	decided := fl.eventPayloads(fxRun, string(run.EventApprovalDecided))
	if len(decided) != 1 || decided[0]["comment"] != "不要在发布分支上跑安装脚本" || decided[0]["status"] != "rejected" {
		t.Fatalf("approval.decided payloads = %v, want one rejected decision carrying the trimmed comment", decided)
	}
}
