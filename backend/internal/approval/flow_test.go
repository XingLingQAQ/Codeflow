package approval_test

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

// The tests in this file drive the T2.02.b lifecycle over the T2.02.a fixture:
// the Run of the fixture is queued, so every case that needs `running` first
// moves it there through the production transition, exactly as the scheduler
// will.

// flow is the per-test handle on the lifecycle under test.
type flow struct {
	t *testing.T
	f *fixture
}

func newFlow(t *testing.T) *flow {
	t.Helper()
	return &flow{t: t, f: newFixture(t)}
}

// moveRun pushes a run through a transition in its own transaction.
func (fl *flow) moveRun(runID string, from run.RunStatus, trigger run.Trigger) run.Run {
	fl.t.Helper()
	var out run.Run
	err := fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		current, err := runstore.GetRun(ctx, tx, runID)
		if err != nil {
			return err
		}
		result, err := runstore.TransitionRunTx(ctx, tx, runstore.TransitionInput{
			RunID:            runID,
			ExpectedRevision: current.Revision,
			ExpectedStatus:   from,
			Trigger:          trigger,
			At:               fl.f.now.Add(time.Second),
			Actor:            run.Actor{Type: run.ActorTypeSystem, ID: "test"},
			AttemptID:        ptrString(fxAttempt),
			AgentRevisionID:  ptrString(current.AgentRevisionID),
			Destinations:     []string{"audit:export"},
		})
		if err != nil {
			return err
		}
		out = result.Run
		return nil
	})
	if err != nil {
		fl.t.Fatalf("move run %s from %s: %v", runID, from, err)
	}
	return out
}

// running moves the fixture's run to running (queued -> starting -> running).
func (fl *flow) running() {
	fl.t.Helper()
	fl.moveRun(fxRun, run.RunStatusQueued, run.Trigger{Kind: run.TriggerEvent, Name: string(run.EventSchedulerClaimed)})
	fl.moveRun(fxRun, run.RunStatusStarting, run.Trigger{Kind: run.TriggerEvent, Name: string(run.EventProcessStarted)})
}

// status reads one run's status.
func (fl *flow) status(runID string) run.RunStatus {
	fl.t.Helper()
	var status run.RunStatus
	if err := fl.f.store.DB().QueryRowContext(fl.f.ctx,
		`SELECT status FROM runs WHERE id = ?`, runID).Scan(&status); err != nil {
		fl.t.Fatalf("read run %s status: %v", runID, err)
	}
	return status
}

// eventTypes returns the types of one run's events, in run_seq order.
func (fl *flow) eventTypes(runID string) []string {
	fl.t.Helper()
	events, _, err := runstore.ListRunEvents(fl.f.ctx, fl.f.store.DB(), runID, 0, 1000)
	if err != nil {
		fl.t.Fatalf("list events of %s: %v", runID, err)
	}
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Type)
	}
	return out
}

// countEvents counts one run's events of a type.
func (fl *flow) countEvents(runID, eventType string) int {
	fl.t.Helper()
	n := 0
	for _, typ := range fl.eventTypes(runID) {
		if typ == eventType {
			n++
		}
	}
	return n
}

// toolRequest builds the tool request a caller makes for the fixture's run.
// The identity is spelled the way the caller believes it; each test then breaks
// exactly one field it wants refused.
func (fl *flow) toolRequest(t *testing.T, id, toolCallID string) approval.RequestToolInput {
	t.Helper()
	f := fl.f
	return approval.RequestToolInput{
		ID:              id,
		ProjectID:       f.projID,
		RunID:           f.runID,
		AttemptID:       f.attempt,
		AgentRevisionID: "ar-1",
		SubjectID:       toolCallID,
		Action: approval.ToolAction{
			Tool:    "run_shell",
			Command: []string{"npm", "test"},
		},
		FingerprintInput: approval.FingerprintInput{
			ProjectID:        f.projID,
			SubjectType:      approval.SubjectTool,
			SubjectID:        toolCallID,
			AgentRevisionID:  "ar-1",
			ArgumentsHash:    argumentsHash(t, "run_shell", `{"command":"npm test"}`),
			Command:          []string{"npm", "test"},
			Cwd:              "/workspace/p-1",
			TargetPaths:      []string{"src/a.go"},
			BaseManifestHash: "sha256:manifest",
			PolicyVersion:    policy.RuleVersion,
		},
		Scope:         []byte(`{"paths":["src/a.go"]}`),
		PolicyVersion: policy.RuleVersion,
		RequestedAt:   f.now,
		ExpiresAt:     f.now.Add(time.Hour),
	}
}

// requestTool calls RequestToolApprovalTx in its own transaction.
func (fl *flow) requestTool(in approval.RequestToolInput) (approval.RequestToolResult, error) {
	fl.t.Helper()
	var out approval.RequestToolResult
	err := fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		var requestErr error
		out, requestErr = approval.RequestToolApprovalTx(ctx, tx, in)
		return requestErr
	})
	return out, err
}

// mustRequestTool is requestTool for the cases where success is the premise.
func (fl *flow) mustRequestTool(in approval.RequestToolInput) approval.RequestToolResult {
	fl.t.Helper()
	out, err := fl.requestTool(in)
	if err != nil {
		fl.t.Fatalf("RequestToolApprovalTx(%s): %v", in.ID, err)
	}
	return out
}

// decide calls DecideTx in its own transaction, using exactly the pattern
// DecideTx's doc comment documents: RefusalWrote(err) means the refusal wrote
// the expiry as the outcome, so the closure commits and the error is returned
// to the test afterwards.
func (fl *flow) decide(in approval.DecisionInput) (approval.Approval, error) {
	fl.t.Helper()
	var (
		out       approval.Approval
		decideErr error
	)
	err := fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		out, decideErr = approval.DecideTx(ctx, tx, in)
		if approval.RefusalWrote(decideErr) {
			return nil
		}
		return decideErr
	})
	if err != nil {
		return out, err
	}
	return out, decideErr
}

// expire calls ExpireDueTx in its own transaction.
func (fl *flow) expire(projectID string, now time.Time) ([]approval.Approval, error) {
	fl.t.Helper()
	var out []approval.Approval
	err := fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		var expireErr error
		out, expireErr = approval.ExpireDueTx(ctx, tx, projectID, now)
		return expireErr
	})
	return out, err
}

// resolveDenied calls ResolveDeniedTx in its own transaction.
func (fl *flow) resolveDenied(id string, actor run.Actor, now time.Time) (approval.Approval, run.Transition, error) {
	fl.t.Helper()
	var (
		outApproval approval.Approval
		outMove     run.Transition
	)
	err := fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		var resolveErr error
		outApproval, outMove, resolveErr = approval.ResolveDeniedTx(ctx, tx, id, actor, now)
		return resolveErr
	})
	return outApproval, outMove, err
}

// invalidate calls InvalidatePendingForRunTx in its own transaction.
func (fl *flow) invalidate(runID, reason string) ([]approval.Approval, error) {
	fl.t.Helper()
	var out []approval.Approval
	err := fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		var invalidateErr error
		out, invalidateErr = approval.InvalidatePendingForRunTx(ctx, tx, runID, reason, fl.f.now.Add(2*time.Second))
		return invalidateErr
	})
	return out, err
}

// consume calls ConsumeTx with a permissive policy evaluator and the request's
// own fingerprint, in its own transaction.
func (fl *flow) consume(in approval.ConsumeInput, grant func(approval.ConsumeResult)) (approval.ConsumeResult, error) {
	fl.t.Helper()
	var out approval.ConsumeResult
	err := fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		var consumeErr error
		out, consumeErr = approval.ConsumeTx(ctx, tx, in)
		return consumeErr
	})
	if err == nil && grant != nil {
		grant(out)
	}
	return out, err
}

// consumeInput is what a correct consumer presents for the given approval: the
// same binding the request was made with, the same scope, and a permissive
// evaluator. `fp` is passed by value so a test can mutate exactly the field it
// wants refused.
func (fl *flow) consumeInput(t *testing.T, stored approval.Approval, fp approval.FingerprintInput) approval.ConsumeInput {
	t.Helper()
	f := fl.f
	return approval.ConsumeInput{
		ApprovalID:       stored.ID,
		ToolCallID:       stored.SubjectID,
		AttemptID:        stored.AttemptID,
		FingerprintInput: fp,
		Scope:            []byte(`{"paths":["src/a.go"]}`),
		Evaluator:        policy.NewLocalEvaluator(),
		PolicyRequest: policy.Request{
			Operation: policy.OperationProcessStart,
			ActorID:   "u-1",
		},
		Now: f.now.Add(time.Minute),
	}
}

// approvalStatus reads one approval row's status and revision.
func (fl *flow) approvalStatus(id string) (approval.Status, int64) {
	fl.t.Helper()
	var (
		status   approval.Status
		revision int64
	)
	if err := fl.f.store.DB().QueryRowContext(fl.f.ctx,
		`SELECT status, revision FROM approvals WHERE id = ?`, id).Scan(&status, &revision); err != nil {
		fl.t.Fatalf("read approval %s: %v", id, err)
	}
	return status, revision
}

func ptrString(s string) *string { return &s }

// ---------------------------------------------------------------------------
// Request
// ---------------------------------------------------------------------------

// TestRequestLowRiskAutoApproves proves §15 T2.02's "低风险不弹窗": a read-only
// call is decided by the service itself, the Run keeps running, exactly one
// approval.decided event is written and no approval.required ever appears.
func TestRequestLowRiskAutoApproves(t *testing.T) {
	fl := newFlow(t)
	fl.running()

	in := fl.toolRequest(t, "ap-low", "tc_low")
	in.Action = approval.ToolAction{Tool: "run_shell", Command: []string{"ls", "-la"}}
	in.FingerprintInput.ArgumentsHash = argumentsHash(t, "run_shell", `{"command":"ls -la"}`)
	in.FingerprintInput.Command = []string{"ls", "-la"}

	result := fl.mustRequestTool(in)
	if !result.AutoApproved {
		t.Fatal("AutoApproved = false, want true for a read-only command")
	}
	if result.Risk != approval.RiskLow {
		t.Fatalf("Risk = %s, want low", result.Risk)
	}
	if result.Approval.Status != approval.StatusApproved {
		t.Fatalf("stored status = %s, want approved", result.Approval.Status)
	}
	if result.Approval.DecidedBy != "system:policy" {
		t.Errorf("DecidedBy = %q, want system:policy", result.Approval.DecidedBy)
	}
	if result.Approval.Revision != 2 {
		t.Errorf("revision = %d, want 2 (created pending, then decided)", result.Approval.Revision)
	}
	if got := fl.status(fxRun); got != run.RunStatusRunning {
		t.Errorf("run status = %s, want running (a low-risk call must not interrupt)", got)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalDecided)); got != 1 {
		t.Errorf("approval.decided events = %d, want 1", got)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalRequired)); got != 0 {
		t.Errorf("approval.required events = %d, want 0", got)
	}

	// The auto-approved row is consumable, and consumption does not move a Run
	// that never stopped.
	consumed, err := fl.consume(fl.consumeInput(t, result.Approval, in.FingerprintInput), nil)
	if err != nil {
		t.Fatalf("ConsumeTx: %v", err)
	}
	if !consumed.Granted || consumed.RunMoved {
		t.Fatalf("consume = granted %v, moved %v; want granted and not moved", consumed.Granted, consumed.RunMoved)
	}
	if got := fl.status(fxRun); got != run.RunStatusRunning {
		t.Errorf("run status after consuming a low-risk approval = %s, want running", got)
	}
}

// TestRequestHighRiskWaits proves §21.1's running --approval.required-->
// waiting_approval: a script-running command creates a pending approval, moves
// the Run, and writes exactly one approval.required.
func TestRequestHighRiskWaits(t *testing.T) {
	fl := newFlow(t)
	fl.running()

	result := fl.mustRequestTool(fl.toolRequest(t, "ap-high", "tc_high"))
	if result.AutoApproved {
		t.Fatal("AutoApproved = true, want false for npm test")
	}
	if result.Risk != approval.RiskHigh {
		t.Fatalf("Risk = %s, want high", result.Risk)
	}
	if result.Approval.Status != approval.StatusPending {
		t.Fatalf("stored status = %s, want pending", result.Approval.Status)
	}
	if result.Approval.Revision != 1 {
		t.Errorf("revision = %d, want 1", result.Approval.Revision)
	}
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Errorf("run status = %s, want waiting_approval", got)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalRequired)); got != 1 {
		t.Errorf("approval.required events = %d, want 1", got)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalDecided)); got != 0 {
		t.Errorf("approval.decided events = %d, want 0 before a decision", got)
	}

	// The approval.required payload carries what a reviewer needs.
	events, _, err := runstore.ListRunEvents(fl.f.ctx, fl.f.store.DB(), fxRun, 0, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var found bool
	for _, e := range events {
		if e.Type != string(run.EventApprovalRequired) {
			continue
		}
		found = true
		var payload map[string]any
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			t.Fatalf("decode approval.required payload: %v", err)
		}
		if payload["approval_id"] != "ap-high" || payload["tool_call_id"] != "tc_high" || payload["risk"] != "high" {
			t.Errorf("approval.required payload = %v, want approval_id/tool_call_id/risk", payload)
		}
	}
	if !found {
		t.Fatal("no approval.required event found")
	}
}

// TestRequestSecondWhileWaitingIsRefused is the S1 rule: a Run that already
// waits for a tool approval refuses a second request, writing nothing — no
// approval row, no event, no second transition (§28 T2.02.b).
func TestRequestSecondWhileWaitingIsRefused(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	fl.mustRequestTool(fl.toolRequest(t, "ap-1st", "tc_1st"))

	approvalsBefore := fl.f.countRows(t, "approvals")
	eventsBefore := fl.f.countRows(t, "events")

	_, err := fl.requestTool(fl.toolRequest(t, "ap-2nd", "tc_2nd"))
	if !errors.Is(err, approval.ErrApprovalNotRunning) {
		t.Fatalf("second request error = %v, want ErrApprovalNotRunning", err)
	}
	if got := fl.f.countRows(t, "approvals"); got != approvalsBefore {
		t.Errorf("approvals = %d after a refused request, want %d (nothing written)", got, approvalsBefore)
	}
	if got := fl.f.countRows(t, "events"); got != eventsBefore {
		t.Errorf("events = %d after a refused request, want %d", got, eventsBefore)
	}
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Errorf("run status = %s, want still waiting_approval", got)
	}
}

// TestRequestIdentityMismatchIsRefused: the service derives the subject from
// the authoritative rows, so a caller that claims another attempt, agent
// revision or project is refused with nothing written (§27.4).
func TestRequestIdentityMismatchIsRefused(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(*approval.RequestToolInput)
		want  error
		field string
	}{
		{"attempt", func(in *approval.RequestToolInput) { in.AttemptID = "a-other" },
			approval.ErrApprovalIdentityMismatch, "a-other"},
		{"project", func(in *approval.RequestToolInput) { in.ProjectID = fxOther },
			approval.ErrApprovalIdentityMismatch, fxOther},
		{"agent revision", func(in *approval.RequestToolInput) { in.AgentRevisionID = "ar-other" },
			approval.ErrAgentRevisionMismatch, "ar-other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fl := newFlow(t)
			fl.running()
			in := fl.toolRequest(t, "ap-id", "tc_id")
			tc.mut(&in)

			_, err := fl.requestTool(in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("error %q does not name %q", err, tc.field)
			}
			if got := fl.f.countRows(t, "approvals"); got != 0 {
				t.Errorf("approvals = %d after a refused request, want 0", got)
			}
			if got := fl.status(fxRun); got != run.RunStatusRunning {
				t.Errorf("run status = %s, want running (no transition happened)", got)
			}
		})
	}
}

// TestRequestRefusedWhenRunNotRunning: a queued Run cannot request a tool
// approval.
func TestRequestRefusedWhenRunNotRunning(t *testing.T) {
	fl := newFlow(t)
	// The fixture's run is queued.
	_, err := fl.requestTool(fl.toolRequest(t, "ap-queued", "tc_queued"))
	if !errors.Is(err, approval.ErrApprovalNotRunning) {
		t.Fatalf("error = %v, want ErrApprovalNotRunning", err)
	}
	if got := fl.f.countRows(t, "approvals"); got != 0 {
		t.Errorf("approvals = %d, want 0", got)
	}
}

// TestRequestGateApprovalHasNoRunAndNoTransition: a gate or merge approval is
// created pending without moving a Run and without approval.required.
func TestRequestGateApprovalHasNoRunAndNoTransition(t *testing.T) {
	fl := newFlow(t)
	fl.running()

	var stored approval.Approval
	err := fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		var createErr error
		stored, createErr = approval.RequestApprovalTx(ctx, tx, approval.RequestApprovalInput{
			ID:          "ap-gate",
			ProjectID:   fl.f.projID,
			SubjectType: approval.SubjectGate,
			SubjectID:   "stage-build",
			Risk:        approval.RiskMedium,
			FingerprintInput: approval.FingerprintInput{
				ProjectID:     fl.f.projID,
				SubjectType:   approval.SubjectGate,
				SubjectID:     "stage-build",
				PolicyVersion: policy.RuleVersion,
			},
			RequestedAt:   fl.f.now,
			ExpiresAt:     fl.f.now.Add(time.Hour),
			PolicyVersion: policy.RuleVersion,
		})
		return createErr
	})
	if err != nil {
		t.Fatalf("RequestApprovalTx: %v", err)
	}
	if stored.Status != approval.StatusPending || stored.RunID != "" {
		t.Fatalf("stored = %s run %q, want pending with no run", stored.Status, stored.RunID)
	}
	if got := fl.status(fxRun); got != run.RunStatusRunning {
		t.Errorf("run status = %s, want running (a gate request moves nothing)", got)
	}
	// Only the two events of moving the Run to running exist; the gate request
	// itself wrote none.
	if got := fl.f.countRows(t, "events"); got != 2 {
		t.Errorf("project events = %d, want 2 (the two transitions; a gate request writes none)", got)
	}
}

// TestRequestToolRejectsGatePathAndViceVersa: the two request functions cover
// disjoint subject types.
func TestRequestToolRejectsGatePathAndViceVersa(t *testing.T) {
	fl := newFlow(t)
	fl.running()

	err := fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		_, gateErr := approval.RequestApprovalTx(ctx, tx, approval.RequestApprovalInput{
			ID:          "ap-x",
			ProjectID:   fl.f.projID,
			SubjectType: approval.SubjectTool,
			SubjectID:   "tc_x",
			Risk:        approval.RiskHigh,
			RequestedAt: fl.f.now,
			ExpiresAt:   fl.f.now.Add(time.Hour),
		})
		return gateErr
	})
	if !errors.Is(err, approval.ErrInvalidApproval) {
		t.Fatalf("RequestApprovalTx with a tool subject = %v, want ErrInvalidApproval", err)
	}
}

// ---------------------------------------------------------------------------
// Decide
// ---------------------------------------------------------------------------

// TestDecideWritesDecisionEvent: both answers write approval.decided in the
// same transaction, bump the revision, and record the decider — and the Run is
// not moved by the decision itself.
func TestDecideWritesDecisionEvent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		approved bool
		want     approval.Status
	}{
		{"approve", true, approval.StatusApproved},
		{"reject", false, approval.StatusRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fl := newFlow(t)
			fl.running()
			pending := fl.mustRequestTool(fl.toolRequest(t, "ap-d", "tc_d")).Approval

			decided, err := fl.decide(approval.DecisionInput{
				ApprovalID:       pending.ID,
				ExpectedRevision: pending.Revision,
				Approved:         tc.approved,
				DecidedBy:        "u-42",
				Actor:            run.Actor{Type: run.ActorTypeUser, ID: "u-42"},
				Now:              fl.f.now.Add(time.Minute),
			})
			if err != nil {
				t.Fatalf("DecideTx: %v", err)
			}
			if decided.Status != tc.want {
				t.Fatalf("status = %s, want %s", decided.Status, tc.want)
			}
			if decided.Revision != pending.Revision+1 {
				t.Errorf("revision = %d, want %d", decided.Revision, pending.Revision+1)
			}
			if decided.DecidedBy != "u-42" {
				t.Errorf("DecidedBy = %q, want u-42", decided.DecidedBy)
			}
			if !decided.DecidedAt.Equal(fl.f.now.Add(time.Minute)) {
				t.Errorf("DecidedAt = %s, want the caller's clock", decided.DecidedAt)
			}
			if got := fl.countEvents(fxRun, string(run.EventApprovalDecided)); got != 1 {
				t.Errorf("approval.decided events = %d, want 1", got)
			}
			// projectEvents counts this project's whole stream (the fixture
			// seeds one run): claimed, started, approval.required, decided.
			if got := fl.f.countRows(t, "events"); got != 4 {
				t.Errorf("project events = %d, want 4 (claimed, started, required, decided)", got)
			}
			if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
				t.Errorf("run status = %s, want waiting_approval (the decision alone moves nothing)", got)
			}
		})
	}
}

// TestDecideStaleRevisionReturnsCurrentRow: a decision based on an old revision
// is refused, the stored fact is returned, and nothing is overwritten
// (§27.2.5: the loser reads the current state).
func TestDecideStaleRevisionReturnsCurrentRow(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	pending := fl.mustRequestTool(fl.toolRequest(t, "ap-race", "tc_race")).Approval

	first, err := fl.decide(approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: true,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("first DecideTx: %v", err)
	}

	second, err := fl.decide(approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: false,
		DecidedBy: "u-2", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-2"},
		Now: fl.f.now.Add(2 * time.Minute),
	})
	if !errors.Is(err, approval.ErrApprovalAlreadyDecided) {
		t.Fatalf("second decision error = %v, want ErrApprovalAlreadyDecided", err)
	}
	if second.Status != approval.StatusApproved || second.DecidedBy != "u-1" {
		t.Fatalf("second decision read back %s by %q, want the first decision (approved by u-1)",
			second.Status, second.DecidedBy)
	}
	status, revision := fl.approvalStatus(pending.ID)
	if status != approval.StatusApproved || revision != first.Revision {
		t.Errorf("stored = %s rev %d, want approved rev %d (not overwritten)", status, revision, first.Revision)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalDecided)); got != 1 {
		t.Errorf("approval.decided events = %d, want 1", got)
	}
}

// TestDecideRepeatIsIdempotent: the same caller repeating the same answer gets
// the same row back without a second event or revision bump.
func TestDecideRepeatIsIdempotent(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	pending := fl.mustRequestTool(fl.toolRequest(t, "ap-idem", "tc_idem")).Approval

	in := approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: true,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(time.Minute),
	}
	first, err := fl.decide(in)
	if err != nil {
		t.Fatalf("first DecideTx: %v", err)
	}
	again, err := fl.decide(in)
	if err != nil {
		t.Fatalf("repeated DecideTx: %v", err)
	}
	if again.Revision != first.Revision || !again.DecidedAt.Equal(first.DecidedAt) {
		t.Errorf("repeat changed the row: rev %d -> %d, at %s -> %s",
			first.Revision, again.Revision, first.DecidedAt, again.DecidedAt)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalDecided)); got != 1 {
		t.Errorf("approval.decided events = %d, want 1", got)
	}
}

// TestDecideAfterExpiryExpiresAndCannotApprove: a decision at or past the
// deadline turns the row expired in the same transaction and answers "expired"
// — never "approved" ("过期授权拒绝", §28 T2.02.b history-keeping).
func TestDecideAfterExpiryExpiresAndCannotApprove(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	in := fl.toolRequest(t, "ap-exp", "tc_exp")
	in.ExpiresAt = fl.f.now.Add(time.Minute)
	pending := fl.mustRequestTool(in).Approval

	decided, err := fl.decide(approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: true,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(time.Minute), // exactly at expires_at: the deadline is exclusive
	})
	if !errors.Is(err, approval.ErrApprovalExpired) {
		t.Fatalf("error = %v, want ErrApprovalExpired", err)
	}
	if decided.Status != approval.StatusExpired {
		t.Fatalf("status = %s, want expired", decided.Status)
	}
	if decided.DecidedBy != "system:expiry" {
		t.Errorf("DecidedBy = %q, want system:expiry", decided.DecidedBy)
	}
	status, _ := fl.approvalStatus(pending.ID)
	if status != approval.StatusExpired {
		t.Errorf("stored status = %s, want expired (the approval can never be granted)", status)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalDecided)); got != 1 {
		t.Errorf("approval.decided events = %d, want 1", got)
	}
}

// TestDecideExpiryIsAnOutcomeRollbackLosesIt pins the must-commit contract
// from both directions: RefusalWrote is true and ExpiredApproval carries the
// expired row; committing keeps the expiry; and — the fact the marker exists to
// prevent — a caller that rolls back on the error loses the expiry and leaves
// the row pending, which is why the marker, not errors.Is, is the branch.
func TestDecideExpiryIsAnOutcomeRollbackLosesIt(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	in := fl.toolRequest(t, "ap-must-commit", "tc_mc")
	in.ExpiresAt = fl.f.now.Add(time.Minute)
	pending := fl.mustRequestTool(in).Approval

	decision := approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: true,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(2 * time.Minute),
	}

	// Wrong pattern: return the error, so the transaction rolls back.
	err := fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		_, err := approval.DecideTx(ctx, tx, decision)
		return err // the caller did not check RefusalWrote
	})
	if !errors.Is(err, approval.ErrApprovalExpired) {
		t.Fatalf("rollback attempt error = %v, want ErrApprovalExpired", err)
	}
	if status, revision := fl.approvalStatus(pending.ID); status != approval.StatusPending || revision != pending.Revision {
		t.Fatalf("after the rolled-back decision: %s rev %d, want pending rev %d (the rollback lost the expiry)",
			status, revision, pending.Revision)
	}

	// Right pattern: RefusalWrote says the write is the outcome; commit it.
	stored, err := fl.decide(decision)
	if !errors.Is(err, approval.ErrApprovalExpired) {
		t.Fatalf("error = %v, want ErrApprovalExpired", err)
	}
	if !approval.RefusalWrote(err) {
		t.Fatal("RefusalWrote = false, want true: the expiry was written and must be committed")
	}
	fromErr, ok := approval.ExpiredApproval(err)
	if !ok {
		t.Fatal("ExpiredApproval returned false, want the expired row")
	}
	if fromErr.ID != pending.ID || fromErr.Status != approval.StatusExpired {
		t.Errorf("ExpiredApproval = %s %s, want %s expired", fromErr.ID, fromErr.Status, pending.ID)
	}
	if stored.Status != approval.StatusExpired || stored.DecidedBy != "system:expiry" {
		t.Fatalf("committed decision = %s by %q, want expired by system:expiry", stored.Status, stored.DecidedBy)
	}
	if status, _ := fl.approvalStatus(pending.ID); status != approval.StatusExpired {
		t.Errorf("stored status = %s, want expired", status)
	}

	// Every other refusal reports false: RefusalWrote is exactly ErrApprovalExpired.
	if approval.RefusalWrote(approval.ErrApprovalConflict) || approval.RefusalWrote(nil) {
		t.Error("RefusalWrote is true for a non-expiry error, want false")
	}
	if _, ok := approval.ExpiredApproval(approval.ErrApprovalConflict); ok {
		t.Error("ExpiredApproval accepted a non-expiry error")
	}
}

// TestDecideRefusesMissingInput covers the pre-SQL refusals: no decision
// without an id, a revision, a clock or an accountable actor.
func TestDecideRefusesMissingInput(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	pending := fl.mustRequestTool(fl.toolRequest(t, "ap-in", "tc_in")).Approval
	base := approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: true,
		Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"}, Now: fl.f.now.Add(time.Minute),
	}
	cases := []struct {
		name string
		mut  func(*approval.DecisionInput)
	}{
		{"no id", func(in *approval.DecisionInput) { in.ApprovalID = "" }},
		{"no revision", func(in *approval.DecisionInput) { in.ExpectedRevision = 0 }},
		{"no clock", func(in *approval.DecisionInput) { in.Now = time.Time{} }},
		{"no actor type", func(in *approval.DecisionInput) { in.Actor.Type = "" }},
		{"no actor id", func(in *approval.DecisionInput) { in.Actor.ID = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.mut(&in)
			if _, err := fl.decide(in); !errors.Is(err, approval.ErrInvalidApproval) {
				t.Fatalf("error = %v, want ErrInvalidApproval", err)
			}
			status, revision := fl.approvalStatus(pending.ID)
			if status != approval.StatusPending || revision != pending.Revision {
				t.Errorf("stored = %s rev %d after a refused decision, want untouched pending rev %d",
					status, revision, pending.Revision)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Expire / invalidate / resolve
// ---------------------------------------------------------------------------

// TestExpireDueExpiresOnlyPastDeadline: the sweep expires the due row and
// leaves an approved row alone even past its deadline — an approved decision is
// terminal and is never rewritten; its deadline is enforced at consumption.
func TestExpireDueExpiresOnlyPastDeadline(t *testing.T) {
	fl := newFlow(t)
	fl.running()

	due := fl.toolRequest(t, "ap-due", "tc_due")
	due.ExpiresAt = fl.f.now.Add(time.Minute)
	fl.mustRequestTool(due)

	// An already-approved gate approval with the same deadline: S1 forbids a
	// second pending tool approval on the Run, and a gate is exactly the
	// subject that can be approved independently.
	var gate approval.Approval
	err := fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		var createErr error
		gate, createErr = approval.RequestApprovalTx(ctx, tx, approval.RequestApprovalInput{
			ID:          "ap-gate-due",
			ProjectID:   fl.f.projID,
			SubjectType: approval.SubjectGate,
			SubjectID:   "stage-build",
			Risk:        approval.RiskMedium,
			FingerprintInput: approval.FingerprintInput{
				ProjectID:     fl.f.projID,
				SubjectType:   approval.SubjectGate,
				SubjectID:     "stage-build",
				PolicyVersion: policy.RuleVersion,
			},
			RequestedAt:   fl.f.now,
			ExpiresAt:     fl.f.now.Add(time.Minute),
			PolicyVersion: policy.RuleVersion,
		})
		return createErr
	})
	if err != nil {
		t.Fatalf("RequestApprovalTx(gate): %v", err)
	}
	approvedGate, err := fl.decide(approval.DecisionInput{
		ApprovalID: gate.ID, ExpectedRevision: gate.Revision, Approved: true,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(30 * time.Second),
	})
	if err != nil {
		t.Fatalf("DecideTx(gate): %v", err)
	}

	expired, err := fl.expire(fl.f.projID, fl.f.now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("ExpireDueTx: %v", err)
	}
	if len(expired) != 1 || expired[0].ID != due.ID {
		t.Fatalf("expired = %v, want only the pending tool approval %s", expired, due.ID)
	}
	if expired[0].Status != approval.StatusExpired || expired[0].DecidedBy != "system:expiry" {
		t.Errorf("expired row = %s by %q, want expired by system:expiry",
			expired[0].Status, expired[0].DecidedBy)
	}
	status, revision := fl.approvalStatus(approvedGate.ID)
	if status != approval.StatusApproved || revision != approvedGate.Revision {
		t.Errorf("approved gate = %s rev %d, want approved rev %d unchanged",
			status, revision, approvedGate.Revision)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalDecided)); got != 1 {
		t.Errorf("run approval.decided events = %d, want 1 (only the expiry)", got)
	}
	// claimed + started + approval.required + gate decision + expiry.
	if got := fl.f.countRows(t, "events"); got != 5 {
		t.Errorf("project events = %d, want 5 (two transitions, required, the gate decision and the expiry)", got)
	}
}

// TestInvalidatePendingForRun: the Run-finish path turns every pending approval
// invalidated, each with its own event, and leaves approved rows alone —
// though a terminal Run can no longer consume them.
func TestInvalidatePendingForRun(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	pending := fl.mustRequestTool(fl.toolRequest(t, "ap-inv", "tc_inv")).Approval

	invalidated, err := fl.invalidate(fxRun, "run_cancelled")
	if err != nil {
		t.Fatalf("InvalidatePendingForRunTx: %v", err)
	}
	if len(invalidated) != 1 || invalidated[0].ID != pending.ID {
		t.Fatalf("invalidated = %v, want the one pending approval", invalidated)
	}
	if invalidated[0].Status != approval.StatusInvalidated || invalidated[0].DecidedBy != "system:run" {
		t.Errorf("invalidated row = %s by %q, want invalidated by system:run",
			invalidated[0].Status, invalidated[0].DecidedBy)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalDecided)); got != 1 {
		t.Errorf("approval.decided events = %d, want 1", got)
	}
	// Idempotent: a second call finds nothing pending and writes nothing.
	second, err := fl.invalidate(fxRun, "run_failed")
	if err != nil {
		t.Fatalf("second InvalidatePendingForRunTx: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second invalidation returned %d rows, want 0", len(second))
	}
	// claimed + started + approval.required + the invalidation's approval.decided.
	if got := fl.f.countRows(t, "events"); got != 4 {
		t.Errorf("project events after the second invalidation = %d, want 4", got)
	}
}

// TestResolveDeniedReturnsRunToRunning is CA-3's chain: decide "no", the
// refusal is delivered to the backend, then the Run returns to running with
// approval.denied — and a repeated call is idempotent.
func TestResolveDeniedReturnsRunToRunning(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	pending := fl.mustRequestTool(fl.toolRequest(t, "ap-deny", "tc_deny")).Approval

	if _, err := fl.decide(approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: false,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("DecideTx: %v", err)
	}
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Fatalf("run status after rejection = %s, want waiting_approval until delivered", got)
	}

	_, moved, err := fl.resolveDenied(pending.ID, run.Actor{Type: run.ActorTypeSystem, ID: "backend"}, fl.f.now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("ResolveDeniedTx: %v", err)
	}
	if moved.From != run.RunStatusWaitingApproval || moved.To != run.RunStatusRunning {
		t.Errorf("transition = %s -> %s, want waiting_approval -> running", moved.From, moved.To)
	}
	if got := fl.status(fxRun); got != run.RunStatusRunning {
		t.Errorf("run status = %s, want running", got)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalDenied)); got != 1 {
		t.Errorf("approval.denied events = %d, want 1", got)
	}

	// Idempotent: the Run is no longer waiting, so there is nothing to move.
	_, _, err = fl.resolveDenied(pending.ID, run.Actor{Type: run.ActorTypeSystem, ID: "backend"}, fl.f.now.Add(3*time.Minute))
	if !errors.Is(err, approval.ErrApprovalConflict) {
		t.Fatalf("second ResolveDeniedTx error = %v, want ErrApprovalConflict", err)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalDenied)); got != 1 {
		t.Errorf("approval.denied events = %d after the repeat, want 1", got)
	}
}

// TestResolveDeniedExpiryChain: ExpireDueTx -> deliver -> ResolveDeniedTx is
// the same chain for a deadline that passed.
func TestResolveDeniedExpiryChain(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	in := fl.toolRequest(t, "ap-chain", "tc_chain")
	in.ExpiresAt = fl.f.now.Add(time.Minute)
	pending := fl.mustRequestTool(in).Approval

	expired, err := fl.expire(fl.f.projID, fl.f.now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("ExpireDueTx: %v", err)
	}
	if len(expired) != 1 || expired[0].ID != pending.ID {
		t.Fatalf("expired = %v, want the one pending approval", expired)
	}
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Fatalf("run status after expiry = %s, want waiting_approval until delivered", got)
	}
	if _, _, err := fl.resolveDenied(pending.ID, run.Actor{Type: run.ActorTypeSystem, ID: "backend"}, fl.f.now.Add(3*time.Minute)); err != nil {
		t.Fatalf("ResolveDeniedTx: %v", err)
	}
	if got := fl.status(fxRun); got != run.RunStatusRunning {
		t.Errorf("run status = %s, want running", got)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalDenied)); got != 1 {
		t.Errorf("approval.denied events = %d, want 1", got)
	}
}

// TestExpireDueLeavesApprovedAlone: an approved row is terminal; the sweep must
// not touch it, and its past deadline is enforced at consumption instead.
func TestExpireDueLeavesApprovedAlone(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	low := fl.toolRequest(t, "ap-low2", "tc_low2")
	low.Action = approval.ToolAction{Tool: "run_shell", Command: []string{"ls"}}
	low.FingerprintInput.ArgumentsHash = argumentsHash(t, "run_shell", `{"command":"ls"}`)
	low.FingerprintInput.Command = []string{"ls"}
	low.ExpiresAt = fl.f.now.Add(time.Minute)
	approved := fl.mustRequestTool(low).Approval

	expired, err := fl.expire(fl.f.projID, fl.f.now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("ExpireDueTx: %v", err)
	}
	if len(expired) != 0 {
		t.Errorf("ExpireDueTx expired %d rows, want none (an approved decision is final)", len(expired))
	}
	status, revision := fl.approvalStatus(approved.ID)
	if status != approval.StatusApproved || revision != approved.Revision {
		t.Errorf("approved row = %s rev %d, want approved rev %d unchanged", status, revision, approved.Revision)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalDecided)); got != 1 {
		t.Errorf("approval.decided events = %d, want 1 (only the auto-approval)", got)
	}
}
