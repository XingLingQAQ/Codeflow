package approval_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/approval"
	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
)

// TestConsumeSuccessMovesRunBack is §21.1's waiting_approval
// --approval.approved--> running through the consumption path: after a human
// approval, the call is granted exactly once, one receipt exists, the Run
// resumes and exactly one approval.approved is written.
func TestConsumeSuccessMovesRunBack(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	pending := fl.mustRequestTool(fl.toolRequest(t, "ap-ok", "tc_ok")).Approval

	// A human approves; the Run is still waiting until the call is granted.
	approved, err := fl.decide(approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: true,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("DecideTx: %v", err)
	}
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Fatalf("run status after the decision = %s, want waiting_approval", got)
	}

	granted := 0
	result, err := fl.consume(fl.consumeInput(t, approved, fl.toolRequest(t, "ap-ok", "tc_ok").FingerprintInput),
		func(r approval.ConsumeResult) {
			if r.Granted {
				granted++
			}
		})
	if err != nil {
		t.Fatalf("ConsumeTx: %v", err)
	}
	if !result.Granted || !result.RunMoved {
		t.Fatalf("consume = granted %v moved %v, want both true", result.Granted, result.RunMoved)
	}
	if result.Transition.From != run.RunStatusWaitingApproval || result.Transition.To != run.RunStatusRunning {
		t.Errorf("transition = %s -> %s, want waiting_approval -> running", result.Transition.From, result.Transition.To)
	}
	if result.Consumption.IsZero() {
		t.Error("Consumption is zero, want the written receipt")
	}
	if got := fl.f.countRows(t, "approval_consumptions"); got != 1 {
		t.Errorf("receipts = %d, want 1", got)
	}
	if got := fl.status(fxRun); got != run.RunStatusRunning {
		t.Errorf("run status = %s, want running", got)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalApproved)); got != 1 {
		t.Errorf("approval.approved events = %d, want 1", got)
	}
	if granted != 1 {
		t.Errorf("the executor was released %d times, want 1", granted)
	}
}

// TestConsumeRefusalsWriteNothing is §27.2.3's re-validation as a table: every
// way the world can have changed since the decision is refused with its own
// sentinel, exactly zero rows are written, and the Run stays where it was.
func TestConsumeRefusalsWriteNothing(t *testing.T) {
	type mut struct {
		name string
		// want is the sentinel the refusal must carry.
		want error
		// change rewrites one thing about the world before consumption.
		change func(t *testing.T, fl *flow, in *approval.ConsumeInput)
	}
	cases := []mut{
		{"policy disallows", approval.ErrApprovalPolicyDenied, func(t *testing.T, fl *flow, in *approval.ConsumeInput) {
			in.Evaluator = policy.NewFailClosedEvaluator()
		}},
		{"no evaluator", approval.ErrApprovalPolicyDenied, func(t *testing.T, fl *flow, in *approval.ConsumeInput) {
			in.Evaluator = nil
		}},
		{"policy version changed", approval.ErrApprovalPolicyChanged, func(t *testing.T, fl *flow, in *approval.ConsumeInput) {
			in.Evaluator = &policy.StaticEvaluator{RuleVersion: "b5-newer", AllowedOperations: map[string]bool{
				policy.OperationProcessStart: true}}
		}},
		{"command changed", approval.ErrApprovalBindingChanged, func(t *testing.T, fl *flow, in *approval.ConsumeInput) {
			in.FingerprintInput.Command = []string{"npm", "test", "--verbose"}
		}},
		{"argument hash changed", approval.ErrApprovalBindingChanged, func(t *testing.T, fl *flow, in *approval.ConsumeInput) {
			in.FingerprintInput.ArgumentsHash = argumentsHash(t, "run_shell", `{"command":"npm run deploy"}`)
		}},
		{"target paths changed", approval.ErrApprovalBindingChanged, func(t *testing.T, fl *flow, in *approval.ConsumeInput) {
			in.FingerprintInput.TargetPaths = []string{"src/b.go"}
		}},
		{"base manifest changed", approval.ErrApprovalBindingChanged, func(t *testing.T, fl *flow, in *approval.ConsumeInput) {
			in.FingerprintInput.BaseManifestHash = "sha256:other-manifest"
		}},
		{"agent revision changed", approval.ErrApprovalBindingChanged, func(t *testing.T, fl *flow, in *approval.ConsumeInput) {
			in.FingerprintInput.AgentRevisionID = "ar-2"
		}},
		{"cwd changed", approval.ErrApprovalBindingChanged, func(t *testing.T, fl *flow, in *approval.ConsumeInput) {
			in.FingerprintInput.Cwd = "/workspace/p-1/sub"
		}},
		{"scope changed", approval.ErrApprovalBindingChanged, func(t *testing.T, fl *flow, in *approval.ConsumeInput) {
			in.Scope = []byte(`{"paths":["src/a.go","src/b.go"]}`)
		}},
		{"wrong tool call", approval.ErrApprovalNotConsumable, func(t *testing.T, fl *flow, in *approval.ConsumeInput) {
			in.ToolCallID = "tc_other"
		}},
		{"wrong attempt", approval.ErrApprovalAttemptMismatch, func(t *testing.T, fl *flow, in *approval.ConsumeInput) {
			in.AttemptID = fxOtherAtte
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fl := newFlow(t)
			fl.running()
			pending := fl.mustRequestTool(fl.toolRequest(t, "ap-ref", "tc_ref")).Approval
			approved, err := fl.decide(approval.DecisionInput{
				ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: true,
				DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
				Now: fl.f.now.Add(time.Minute),
			})
			if err != nil {
				t.Fatalf("DecideTx: %v", err)
			}

			in := fl.consumeInput(t, approved, fl.toolRequest(t, "ap-ref", "tc_ref").FingerprintInput)
			tc.change(t, fl, &in)

			before := map[string]int{
				"approval_consumptions": fl.f.countRows(t, "approval_consumptions"),
				"events":                fl.f.countRows(t, "events"),
			}
			_, err = fl.consume(in, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if !errors.Is(err, approval.ErrApprovalNoReceipt) {
				t.Errorf("error %v does not wrap ErrApprovalNoReceipt; every refusal must be the 'not granted' class", err)
			}
			for table, want := range before {
				if got := fl.f.countRows(t, table); got != want {
					t.Errorf("%s = %d after a refused consumption, want %d (zero writes)", table, got, want)
				}
			}
			if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
				t.Errorf("run status = %s after a refused consumption, want waiting_approval", got)
			}
			if status, _ := fl.approvalStatus(approved.ID); status != approval.StatusApproved {
				t.Errorf("approval status = %s, want still approved", status)
			}
		})
	}
}

// TestConsumeRefusesTerminalRun: a Run that ended cannot spend an approval it
// was granted before ("Run 退出后……已批准不能消费" is enforced here, since 009
// keeps the approved row as history).
func TestConsumeRefusesTerminalRun(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	pending := fl.mustRequestTool(fl.toolRequest(t, "ap-term", "tc_term")).Approval
	approved, err := fl.decide(approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: true,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("DecideTx: %v", err)
	}

	// waiting_approval --run.cancel--> cancelling --cleaned--> cancelled.
	fl.moveRun(fxRun, run.RunStatusWaitingApproval, run.CommandTrigger(run.CommandRunCancel))
	fl.moveRun(fxRun, run.RunStatusCancelling,
		run.ConclusionTrigger(run.ConclusionCleaned, run.RunStatusCancelled))

	receiptsBefore := fl.f.countRows(t, "approval_consumptions")
	_, err = fl.consume(fl.consumeInput(t, approved, fl.toolRequest(t, "ap-term", "tc_term").FingerprintInput), nil)
	if !errors.Is(err, approval.ErrApprovalRunNotConsumable) {
		t.Fatalf("error = %v, want ErrApprovalRunNotConsumable", err)
	}
	if got := fl.f.countRows(t, "approval_consumptions"); got != receiptsBefore {
		t.Errorf("receipts = %d after a refused consumption, want %d", got, receiptsBefore)
	}
}

// TestConsumeRefusesWhenRunWaitsForAnotherApproval: a Run waiting for a
// different pending tool call must not be released by consuming another
// approval.
func TestConsumeRefusesWhenRunWaitsForAnotherApproval(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	pending := fl.mustRequestTool(fl.toolRequest(t, "ap-wait", "tc_wait")).Approval
	approved, err := fl.decide(approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: true,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("DecideTx: %v", err)
	}

	// A second, still-pending tool approval for the same Run, inserted by hand:
	// the request path refuses this (S1), so the state can only arise from an
	// administrative write — exactly the case the check must survive.
	err = fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		_, insertErr := tx.ExecContext(ctx, `
			INSERT INTO approvals (id, project_id, run_id, attempt_id, subject_type, subject_id, risk,
			                       scope_json, fingerprint, fingerprint_input_json, status, policy_version,
			                       requested_at, expires_at, revision)
			SELECT 'ap-second', project_id, run_id, attempt_id, subject_type, 'tc_second', risk,
			       scope_json, fingerprint, fingerprint_input_json, 'pending', policy_version,
			       requested_at, expires_at, 1
			FROM approvals WHERE id = ?`, pending.ID)
		return insertErr
	})
	if err != nil {
		t.Fatalf("seed second pending approval: %v", err)
	}

	_, err = fl.consume(fl.consumeInput(t, approved, fl.toolRequest(t, "ap-wait", "tc_wait").FingerprintInput), nil)
	if !errors.Is(err, approval.ErrApprovalRunNotConsumable) {
		t.Fatalf("error = %v, want ErrApprovalRunNotConsumable (the run waits for ap-second)", err)
	}
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Errorf("run status = %s, want waiting_approval", got)
	}
	if got := fl.f.countRows(t, "approval_consumptions"); got != 0 {
		t.Errorf("receipts = %d, want 0", got)
	}
}

// TestConsumeRefusesExpiredGrant: an approved row that passed its deadline is
// refused at consumption — "过期授权拒绝" for the grant nobody swept.
func TestConsumeRefusesExpiredGrant(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	in := fl.toolRequest(t, "ap-late", "tc_late")
	in.ExpiresAt = fl.f.now.Add(time.Minute)
	pending := fl.mustRequestTool(in).Approval
	approved, err := fl.decide(approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: true,
		DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now: fl.f.now.Add(30 * time.Second),
	})
	if err != nil {
		t.Fatalf("DecideTx: %v", err)
	}

	consumeIn := fl.consumeInput(t, approved, in.FingerprintInput)
	consumeIn.Now = fl.f.now.Add(2 * time.Minute) // past the deadline
	_, err = fl.consume(consumeIn, nil)
	if !errors.Is(err, approval.ErrApprovalNotConsumable) {
		t.Fatalf("error = %v, want ErrApprovalNotConsumable", err)
	}
	if got := fl.f.countRows(t, "approval_consumptions"); got != 0 {
		t.Errorf("receipts = %d, want 0", got)
	}
	if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
		t.Errorf("run status = %s, want waiting_approval", got)
	}
}

// TestConsumeRefusesPendingAndNonToolSubjects covers the other statuses and the
// subject check: only an approved tool approval is spendable.
func TestConsumeRefusesPendingAndNonToolSubjects(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	pending := fl.mustRequestTool(fl.toolRequest(t, "ap-pend", "tc_pend")).Approval

	// Still pending: no decision yet.
	_, err := fl.consume(fl.consumeInput(t, pending, fl.toolRequest(t, "ap-pend", "tc_pend").FingerprintInput), nil)
	if !errors.Is(err, approval.ErrApprovalNotConsumable) {
		t.Fatalf("pending consumption error = %v, want ErrApprovalNotConsumable", err)
	}

	// A gate approval is never consumable as a tool call.
	var gate approval.Approval
	err = fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		var createErr error
		gate, createErr = approval.RequestApprovalTx(ctx, tx, approval.RequestApprovalInput{
			ID:          "ap-gate-c",
			ProjectID:   fl.f.projID,
			SubjectType: approval.SubjectGate,
			SubjectID:   "stage-1",
			Risk:        approval.RiskHigh,
			FingerprintInput: approval.FingerprintInput{
				ProjectID: fl.f.projID, SubjectType: approval.SubjectGate, SubjectID: "stage-1",
				PolicyVersion: policy.RuleVersion,
			},
			RequestedAt: fl.f.now, ExpiresAt: fl.f.now.Add(time.Hour), PolicyVersion: policy.RuleVersion,
		})
		return createErr
	})
	if err != nil {
		t.Fatalf("RequestApprovalTx(gate): %v", err)
	}
	_, err = fl.consume(approval.ConsumeInput{
		ApprovalID: gate.ID, ToolCallID: "stage-1", AttemptID: fxAttempt,
		FingerprintInput: approval.FingerprintInput{
			ProjectID: fl.f.projID, SubjectType: approval.SubjectTool, SubjectID: "stage-1",
			AgentRevisionID: "ar-1", ArgumentsHash: argumentsHash(t, "run_shell", `{}`),
			BaseManifestHash: "sha256:manifest", PolicyVersion: policy.RuleVersion,
		},
		Scope:     []byte(`{}`),
		Evaluator: policy.NewLocalEvaluator(),
		PolicyRequest: policy.Request{
			Operation: policy.OperationProcessStart,
		},
		Now: fl.f.now,
	}, nil)
	if !errors.Is(err, approval.ErrApprovalNotConsumable) {
		t.Fatalf("gate consumption error = %v, want ErrApprovalNotConsumable", err)
	}
	if got := fl.f.countRows(t, "approval_consumptions"); got != 0 {
		t.Errorf("receipts = %d, want 0", got)
	}
}

// TestConsumeTwiceIsRefused: the second consumption of the same approval and
// call is ErrAlreadyConsumed and writes no second receipt.
func TestConsumeTwiceIsRefused(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	low := fl.toolRequest(t, "ap-twice", "tc_twice")
	low.Action = approval.ToolAction{Tool: "run_shell", Command: []string{"ls"}}
	low.FingerprintInput.ArgumentsHash = argumentsHash(t, "run_shell", `{"command":"ls"}`)
	low.FingerprintInput.Command = []string{"ls"}
	auto := fl.mustRequestTool(low).Approval

	consumeIn := fl.consumeInput(t, auto, low.FingerprintInput)
	if _, err := fl.consume(consumeIn, nil); err != nil {
		t.Fatalf("first ConsumeTx: %v", err)
	}
	_, err := fl.consume(consumeIn, nil)
	if !errors.Is(err, approval.ErrAlreadyConsumed) {
		t.Fatalf("second ConsumeTx error = %v, want ErrAlreadyConsumed", err)
	}
	if got := fl.f.countRows(t, "approval_consumptions"); got != 1 {
		t.Errorf("receipts = %d, want 1", got)
	}
}

// TestConsumeConcurrentGrantsExactlyOne is §27.2.5's one-shot rule under real
// concurrency: two consumers race for the same approval and call, exactly one
// is Granted, one receipt exists, and a fake executor is released once.
//
// Run with -count=10: the race is timing-dependent, and a single run proves
// little.
func TestConsumeConcurrentGrantsExactlyOne(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	low := fl.toolRequest(t, "ap-race", "tc_race")
	low.Action = approval.ToolAction{Tool: "run_shell", Command: []string{"ls"}}
	low.FingerprintInput.ArgumentsHash = argumentsHash(t, "run_shell", `{"command":"ls"}`)
	low.FingerprintInput.Command = []string{"ls"}
	auto := fl.mustRequestTool(low).Approval

	input := fl.consumeInput(t, auto, low.FingerprintInput)
	var released int32
	run := func() {
		// Each goroutine uses its own WithTx, as two server requests would.
		_, err := fl.consume(input, func(r approval.ConsumeResult) {
			if r.Granted {
				atomic.AddInt32(&released, 1)
			}
		})
		if err != nil && !errors.Is(err, approval.ErrApprovalNoReceipt) {
			t.Errorf("consumer error = %v, want nil or ErrApprovalNoReceipt", err)
		}
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			run()
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(&released); got != 1 {
		t.Errorf("executor released %d times, want exactly 1", got)
	}
	if got := fl.f.countRows(t, "approval_consumptions"); got != 1 {
		t.Errorf("receipts = %d, want 1", got)
	}
}

// TestDecideAndInvalidateRaceIsCASDecided is the second concurrency case: a
// decision and a run-finish invalidation race for one pending row, exactly one
// wins, the loser reads the current fact, and the terminal state is never
// revived.
//
// Run with -count=10.
func TestDecideAndInvalidateRaceIsCASDecided(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	pending := fl.mustRequestTool(fl.toolRequest(t, "ap-cas", "tc_cas")).Approval

	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		outcome = make([]error, 2)
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, outcome[0] = fl.decide(approval.DecisionInput{
			ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: true,
			DecidedBy: "u-1", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
			Now: fl.f.now.Add(time.Minute),
		})
	}()
	go func() {
		defer wg.Done()
		<-start
		_, outcome[1] = fl.invalidate(fxRun, "run_cancelled")
	}()
	close(start)
	wg.Wait()

	status, _ := fl.approvalStatus(pending.ID)
	if status != approval.StatusApproved && status != approval.StatusInvalidated {
		t.Fatalf("status after the race = %s, want approved or invalidated", status)
	}
	// Exactly one write won: one approval.decided event beyond the request's
	// approval.required, and the loser reported a conflict or found nothing.
	if got := fl.countEvents(fxRun, string(run.EventApprovalDecided)); got != 1 {
		t.Errorf("approval.decided events = %d, want 1 (one winner)", got)
	}
	// Neither outcome may be an error class that suggests corruption; the
	// decision may legitimately lose (ErrApprovalAlreadyDecided or a conflict),
	// and the invalidator may legitimately find nothing.
	if outcome[0] != nil {
		if !errors.Is(outcome[0], approval.ErrApprovalAlreadyDecided) &&
			!errors.Is(outcome[0], approval.ErrApprovalConflict) {
			t.Errorf("the losing decision returned %v; want already-decided or conflict", outcome[0])
		}
	}
	if outcome[1] != nil {
		t.Errorf("invalidation returned %v, want nil (it either won or found nothing)", outcome[1])
	}
	// The row is terminal and stays terminal: a second decision attempt changes
	// nothing.
	_, _ = fl.decide(approval.DecisionInput{
		ApprovalID: pending.ID, ExpectedRevision: pending.Revision, Approved: status == approval.StatusInvalidated,
		DecidedBy: "u-2", Actor: run.Actor{Type: run.ActorTypeUser, ID: "u-2"},
		Now: fl.f.now.Add(2 * time.Minute),
	})
	statusAfter, revisionAfter := fl.approvalStatus(pending.ID)
	if statusAfter != status || revisionAfter != 2 {
		t.Errorf("after the extra decision attempt: %s rev %d, want %s rev 2 (the terminal fact is not revived)",
			statusAfter, revisionAfter, status)
	}
}

// TestConsumeOperationIdentityComesFromTheApproval: the policy request the
// evaluator sees is filled from the stored approval, so a caller cannot
// evaluate one operation and spend another's approval.
func TestConsumeOperationIdentityComesFromTheApproval(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	low := fl.toolRequest(t, "ap-ident", "tc_ident")
	low.FingerprintInput.Command = []string{"ls"}
	low.FingerprintInput.ArgumentsHash = argumentsHash(t, "run_shell", `{"command":"ls"}`)
	low.Action = approval.ToolAction{Tool: "run_shell", Command: []string{"ls"}}
	auto := fl.mustRequestTool(low).Approval

	recorder := &recordingEvaluator{allow: true}
	consumeIn := fl.consumeInput(t, auto, low.FingerprintInput)
	consumeIn.Evaluator = recorder
	consumeIn.PolicyRequest = policy.Request{
		// A caller trying to evaluate something else entirely. The identity
		// fields are overwritten from the stored row; Resource is the caller's
		// (left empty here, so it defaults to the subject).
		Operation: policy.OperationProcessStart,
		ProjectID: "some-other-project",
		RunID:     "some-other-run",
		ActorID:   "u-1",
	}
	if _, err := fl.consume(consumeIn, nil); err != nil {
		t.Fatalf("ConsumeTx: %v", err)
	}
	got := recorder.last
	if got.ProjectID != fl.f.projID || got.RunID != fxRun || got.AttemptID != fxAttempt {
		t.Errorf("evaluator saw project/run/attempt %s/%s/%s, want the stored approval's %s/%s/%s",
			got.ProjectID, got.RunID, got.AttemptID, fl.f.projID, fxRun, fxAttempt)
	}
	if got.ApprovalID != auto.ID || got.Fingerprint != auto.Fingerprint {
		t.Errorf("evaluator saw approval %s fingerprint %s, want %s / %s",
			got.ApprovalID, got.Fingerprint, auto.ID, auto.Fingerprint)
	}
	if got.Risk != string(approval.RiskLow) {
		t.Errorf("evaluator saw risk %q, want low", got.Risk)
	}
	if got.Resource != auto.SubjectID {
		t.Errorf("evaluator saw resource %q, want the subject %q (the default when the caller names none)",
			got.Resource, auto.SubjectID)
	}
}

// recordingEvaluator captures the request it was asked to judge.
type recordingEvaluator struct {
	allow bool
	last  policy.Request
}

func (e *recordingEvaluator) Evaluate(_ context.Context, req policy.Request) policy.Decision {
	e.last = req
	return policy.Decision{Allowed: e.allow, Reason: "recording", RuleVersion: policy.RuleVersion}
}

// TestConsumePolicyRequestDefaults pins PolicyRequestFor's defaults so the
// exported helper and the consumption path cannot drift.
func TestConsumePolicyRequestDefaults(t *testing.T) {
	stored := approval.Approval{
		ID: "ap-1", ProjectID: "p-1", RunID: "r-1", AttemptID: "a-1",
		Risk: approval.RiskHigh, Fingerprint: "sha256:" + strings.Repeat("a", 64),
		SubjectID: "tc_1",
	}
	got := approval.PolicyRequestFor(policy.Request{ActorID: "u-1"}, stored)
	if got.Operation != policy.OperationProcessStart {
		t.Errorf("Operation = %q, want %q", got.Operation, policy.OperationProcessStart)
	}
	if got.Resource != "tc_1" {
		t.Errorf("Resource = %q, want the subject id", got.Resource)
	}
	if got.ProjectID != "p-1" || got.RunID != "r-1" || got.AttemptID != "a-1" {
		t.Errorf("identity = %s/%s/%s, want the stored fields", got.ProjectID, got.RunID, got.AttemptID)
	}
}
