package approval_test

// T2.02.c group 1 (plan §28 T2.02.c): the four service-level named tests of the
// approval lifecycle, each driving the real T2.02.b API over a migrated
// database with the caller's own clock.
//
//	TestApprovalFingerprintChanged  §27.2.3 参数、目标内容或策略改变则原决定成为
//	                                不可消费历史，新建 approval（含 CA-4 的 stale 链）
//	TestApprovalConsumedOnce        §27.2.5 同一 tool_call_id 最多一次授权消费
//	TestCancelApproveRace           §27.2.5/§27.2.6 并发 cancel/approve 只有一个有效执行结果
//	TestApprovalExpiryFakeClock     §15 T2.02 "过期授权拒绝"，全程假时钟，不真实等待
//
// Every test uses the same fake executor: a counter bumped exactly in the
// ConsumeTx call whose transaction returned Granted=true, and never by a
// refusal — §28 T2.02.c's "失败后不得有工具副作用" as one number to read.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/approval"
	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
)

// fakeExecutor stands in for the tool an approval authorizes. It counts, and
// only counts: one call per ConsumeTx result with Granted=true (whose
// transaction therefore committed), nothing on any refusal.
type fakeExecutor struct {
	mu    sync.Mutex
	calls int
}

func (x *fakeExecutor) granted() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.calls++
}

func (x *fakeExecutor) count() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.calls
}

// consumeAndRun spends an approval the way the executing path does: one
// transaction, and only a Granted result releases the fake executor. The
// callback runs after the transaction committed, so a refusal can never bump
// the counter and a grant always has its receipt on disk.
func (fl *flow) consumeAndRun(in approval.ConsumeInput, exec *fakeExecutor) (approval.ConsumeResult, error) {
	fl.t.Helper()
	return fl.consume(in, func(res approval.ConsumeResult) {
		if res.Granted {
			exec.granted()
		}
	})
}

// runCancel is the Run service's cancel path (T1.04) at the shape the approval
// package requires: the transition and InvalidatePendingForRunTx in one
// transaction, so the Run can never be cancelling while a request against it is
// still pending. The queued-Run cancel path (a Run with no attempt) may carry
// no attempt id; a running or waiting_approval Run always names one.
func (fl *flow) runCancel(reason string, at time.Time) ([]approval.Approval, error) {
	fl.t.Helper()
	var invalidated []approval.Approval
	err := fl.f.store.WithTx(fl.f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		current, err := runstore.GetRun(ctx, tx, fxRun)
		if err != nil {
			return err
		}
		var attempt *string
		if current.Status == run.RunStatusWaitingApproval || current.Status == run.RunStatusRunning {
			attempt = ptrString(fxAttempt)
		}
		if _, err := runstore.TransitionRunTx(ctx, tx, runstore.TransitionInput{
			RunID:            fxRun,
			ExpectedRevision: current.Revision,
			ExpectedStatus:   current.Status,
			Trigger:          run.CommandTrigger(run.CommandRunCancel),
			At:               at,
			Actor:            run.Actor{Type: run.ActorTypeUser, ID: "u-cancel"},
			AttemptID:        attempt,
			Details:          map[string]any{"reason": reason},
			Destinations:     []string{"audit:export"},
		}); err != nil {
			return err
		}
		invalidated, err = approval.InvalidatePendingForRunTx(ctx, tx, fxRun, reason, at)
		return err
	})
	return invalidated, err
}

// userDecision is the human answer the tests repeat.
func userDecision(a approval.Approval, approved bool, at time.Time) approval.DecisionInput {
	return approval.DecisionInput{
		ApprovalID:       a.ID,
		ExpectedRevision: a.Revision,
		Approved:         approved,
		DecidedBy:        "u-1",
		Actor:            run.Actor{Type: run.ActorTypeUser, ID: "u-1"},
		Now:              at,
	}
}

// backendActor is the actor a refusal is delivered to the backend as.
var backendActor = run.Actor{Type: run.ActorTypeSystem, ID: "backend"}

// eventIndex is the position of the first event of a type, or -1.
func eventIndex(events []string, want string) int {
	for i, e := range events {
		if e == want {
			return i
		}
	}
	return -1
}

// assertRefusalWroteNothing is the shared "失败后不得有工具副作用" check for a
// refused consumption: no receipt, no new approval row and no new event.
func assertRefusalWroteNothing(t *testing.T, fl *flow, eventsBefore, approvalsBefore int) {
	t.Helper()
	if got := fl.f.countRows(t, "approval_consumptions"); got != 0 {
		t.Errorf("receipts = %d after a refusal, want 0", got)
	}
	if got := fl.f.countRows(t, "events"); got != eventsBefore {
		t.Errorf("events = %d after a refusal, want %d", got, eventsBefore)
	}
	if got := fl.f.countRows(t, "approvals"); got != approvalsBefore {
		t.Errorf("approvals = %d after a refusal, want %d", got, approvalsBefore)
	}
}

// ---------------------------------------------------------------------------
// TestApprovalFingerprintChanged
// ---------------------------------------------------------------------------

// TestApprovalFingerprintChanged is §27.2.3's "参数、目标内容或策略改变则原决定
// 成为不可消费历史，新建 approval", and the CA-4 chain that releases the Run when
// the stale grant reached the backend.
//
// Part one: a high-risk request is approved, then consumed with a binding that
// changed after the decision — the target content (BaseManifestHash), the
// command arguments, a target path, the agent revision — and every mutation is
// refused as ErrApprovalBindingChanged with the fake executor still at zero and
// nothing written. Part two drives the whole recovery: the refusal is delivered
// to the backend, ResolveDeniedTx returns the Run with approval.denied reason
// "stale"; the old grant can never be spent, not even with its original
// arguments; and the changed call's new request (new tool_call_id, new
// fingerprint) is approved and consumed exactly once.
func TestApprovalFingerprintChanged(t *testing.T) {
	t.Run("changed bindings are refused with zero writes", func(t *testing.T) {
		fl := newFlow(t)
		fl.running()
		in := fl.toolRequest(t, "ap-fp", "tc_fp")
		pending := fl.mustRequestTool(in).Approval
		approved, err := fl.decide(userDecision(pending, true, fl.f.now.Add(10*time.Second)))
		if err != nil {
			t.Fatalf("DecideTx: %v", err)
		}
		exec := &fakeExecutor{}

		cases := []struct {
			name string
			mut  func(*approval.FingerprintInput)
		}{
			{"target content changed", func(fp *approval.FingerprintInput) {
				fp.BaseManifestHash = "sha256:manifest-after-an-edit"
			}},
			{"command arguments changed", func(fp *approval.FingerprintInput) {
				fp.Command = []string{"npm", "test", "--verbose"}
			}},
			{"target path changed", func(fp *approval.FingerprintInput) {
				fp.TargetPaths = []string{"src/b.go"}
			}},
			{"agent revision changed", func(fp *approval.FingerprintInput) {
				fp.AgentRevisionID = "ar-2"
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				fp := in.FingerprintInput
				tc.mut(&fp)
				eventsBefore := fl.f.countRows(t, "events")
				approvalsBefore := fl.f.countRows(t, "approvals")

				res, err := fl.consumeAndRun(fl.consumeInput(t, approved, fp), exec)
				if !errors.Is(err, approval.ErrApprovalBindingChanged) {
					t.Fatalf("error = %v, want ErrApprovalBindingChanged", err)
				}
				if !errors.Is(err, approval.ErrApprovalNoReceipt) {
					t.Errorf("error %v does not wrap ErrApprovalNoReceipt", err)
				}
				if res.Granted {
					t.Error("a refused consumption reported Granted")
				}
				if got := exec.count(); got != 0 {
					t.Errorf("the fake executor ran %d times after a refused consumption, want 0", got)
				}
				assertRefusalWroteNothing(t, fl, eventsBefore, approvalsBefore)
				if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
					t.Errorf("run status = %s after a refused consumption, want waiting_approval", got)
				}
				if status, _ := fl.approvalStatus(approved.ID); status != approval.StatusApproved {
					t.Errorf("approval status = %s, want still approved (history)", status)
				}
			})
		}
		if got := exec.count(); got != 0 {
			t.Errorf("the fake executor ran %d times across all refusals, want 0", got)
		}
		if got := fl.countEvents(fxRun, string(run.EventApprovalApproved)); got != 0 {
			t.Errorf("approval.approved events = %d, want 0", got)
		}
	})

	t.Run("the stale grant is denied, then a new approval executes once", func(t *testing.T) {
		fl := newFlow(t)
		fl.running()
		in := fl.toolRequest(t, "ap-old", "tc_old")
		old := fl.mustRequestTool(in).Approval
		old, err := fl.decide(userDecision(old, true, fl.f.now.Add(10*time.Second)))
		if err != nil {
			t.Fatalf("DecideTx(ap-old): %v", err)
		}
		exec := &fakeExecutor{}

		changed := in.FingerprintInput
		changed.BaseManifestHash = "sha256:manifest-after-an-edit"
		if _, err := fl.consumeAndRun(fl.consumeInput(t, old, changed), exec); !errors.Is(err, approval.ErrApprovalBindingChanged) {
			t.Fatalf("consume with changed target content: error = %v, want ErrApprovalBindingChanged", err)
		}
		if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
			t.Fatalf("run status = %s, want waiting_approval (ap-old still unanswered)", got)
		}
		if got := exec.count(); got != 0 {
			t.Fatalf("the fake executor ran %d times on the stale grant, want 0", got)
		}

		// The refusal reached the backend; only then may the Run move.
		_, moved, err := fl.resolveDenied(old.ID, backendActor, fl.f.now.Add(20*time.Second))
		if err != nil {
			t.Fatalf("ResolveDeniedTx: %v", err)
		}
		if moved.From != run.RunStatusWaitingApproval || moved.To != run.RunStatusRunning {
			t.Fatalf("transition = %s -> %s, want waiting_approval -> running", moved.From, moved.To)
		}
		denied := fl.eventPayloads(fxRun, string(run.EventApprovalDenied))
		if len(denied) != 1 || denied[0]["reason"] != "stale" || denied[0]["approval_id"] != old.ID {
			t.Fatalf("approval.denied payloads = %v, want exactly one with reason stale for %s", denied, old.ID)
		}
		if status, _ := fl.approvalStatus(old.ID); status != approval.StatusApproved {
			t.Fatalf("ap-old status = %s, want approved (the decision stays as history)", status)
		}

		// The old grant can never be spent now, not even with its own binding.
		res, err := fl.consumeAndRun(fl.consumeInput(t, old, in.FingerprintInput), exec)
		if !errors.Is(err, approval.ErrApprovalRunNotConsumable) || !errors.Is(err, approval.ErrApprovalNoReceipt) {
			t.Fatalf("consume the stale grant after the denial: error = %v, want ErrApprovalRunNotConsumable", err)
		}
		if res.Granted {
			t.Fatal("the stale grant reported Granted")
		}
		if got := exec.count(); got != 0 {
			t.Fatalf("the fake executor ran %d times on the stale grant, want 0", got)
		}

		// The changed call asks again: a new tool_call_id and the new fingerprint.
		newIn := fl.toolRequest(t, "ap-new", "tc_new")
		newIn.FingerprintInput.BaseManifestHash = changed.BaseManifestHash
		newer := fl.mustRequestTool(newIn).Approval
		if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
			t.Fatalf("run status after the new request = %s, want waiting_approval", got)
		}
		newer, err = fl.decide(userDecision(newer, true, fl.f.now.Add(30*time.Second)))
		if err != nil {
			t.Fatalf("DecideTx(ap-new): %v", err)
		}
		result, err := fl.consumeAndRun(fl.consumeInput(t, newer, newIn.FingerprintInput), exec)
		if err != nil {
			t.Fatalf("consume the new approval: %v", err)
		}
		if !result.Granted || !result.RunMoved {
			t.Fatalf("consume the new approval = granted %v moved %v, want both true", result.Granted, result.RunMoved)
		}
		if got := fl.status(fxRun); got != run.RunStatusRunning {
			t.Errorf("run status = %s, want running", got)
		}
		if got := exec.count(); got != 1 {
			t.Errorf("the fake executor ran %d times in total, want exactly 1", got)
		}
		if got := fl.f.countRows(t, "approval_consumptions"); got != 1 {
			t.Errorf("receipts = %d, want exactly 1", got)
		}
		if got := fl.countEvents(fxRun, string(run.EventApprovalRequired)); got != 2 {
			t.Errorf("approval.required events = %d, want 2 (ap-old and ap-new)", got)
		}
		if got := fl.countEvents(fxRun, string(run.EventApprovalApproved)); got != 1 {
			t.Errorf("approval.approved events = %d, want 1", got)
		}
	})
}

// ---------------------------------------------------------------------------
// TestApprovalConsumedOnce
// ---------------------------------------------------------------------------

// TestApprovalConsumedOnce is §27.2.5's "同一 tool_call_id 最多一次授权消费" with
// N goroutines, each in its own transaction, racing for one approval: exactly
// one is Granted, exactly one receipt exists, the fake executor runs exactly
// once, and every loser came back as ErrApprovalNoReceipt naming why (it lost
// the receipt race, or the Run already stopped waiting for this approval).
// Afterwards the approval stays unspendable.
//
// Run with -count=10: the interleaving changes between runs, and one run proves
// little.
func TestApprovalConsumedOnce(t *testing.T) {
	fl := newFlow(t)
	fl.running()
	in := fl.toolRequest(t, "ap-once", "tc_once")
	pending := fl.mustRequestTool(in).Approval
	approved, err := fl.decide(userDecision(pending, true, fl.f.now.Add(10*time.Second)))
	if err != nil {
		t.Fatalf("DecideTx: %v", err)
	}
	exec := &fakeExecutor{}
	consumeIn := fl.consumeInput(t, approved, in.FingerprintInput)

	const consumers = 8
	type outcome struct {
		result approval.ConsumeResult
		err    error
	}
	start := make(chan struct{})
	results := make(chan outcome, consumers)
	var wg sync.WaitGroup
	for i := 0; i < consumers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			// Each goroutine uses its own WithTx, as two server requests would.
			res, err := fl.consumeAndRun(consumeIn, exec)
			results <- outcome{result: res, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	granted, alreadyConsumed, runNotWaiting := 0, 0, 0
	for out := range results {
		switch {
		case out.err == nil:
			granted++
			if !out.result.Granted || !out.result.RunMoved {
				t.Errorf("a winning consumer = granted %v moved %v, want both true",
					out.result.Granted, out.result.RunMoved)
			}
		case errors.Is(out.err, approval.ErrApprovalNoReceipt):
			switch {
			case errors.Is(out.err, approval.ErrAlreadyConsumed):
				alreadyConsumed++
			case errors.Is(out.err, approval.ErrApprovalRunNotConsumable):
				runNotWaiting++
			default:
				t.Errorf("loser error = %v, want ErrAlreadyConsumed or ErrApprovalRunNotConsumable", out.err)
			}
			if out.result.Granted {
				t.Error("a losing consumer reported Granted")
			}
		default:
			t.Errorf("consumer error = %v, want a granted call or ErrApprovalNoReceipt", out.err)
		}
	}
	if granted != 1 {
		t.Fatalf("granted consumers = %d, want exactly 1", granted)
	}
	if alreadyConsumed+runNotWaiting != consumers-1 {
		t.Fatalf("losers = %d (%d already consumed, %d run not waiting), want %d",
			alreadyConsumed+runNotWaiting, alreadyConsumed, runNotWaiting, consumers-1)
	}
	if got := exec.count(); got != 1 {
		t.Errorf("the fake executor ran %d times, want exactly 1", got)
	}
	if got := fl.f.countRows(t, "approval_consumptions"); got != 1 {
		t.Errorf("receipts = %d, want exactly 1", got)
	}
	if got := fl.countEvents(fxRun, string(run.EventApprovalApproved)); got != 1 {
		t.Errorf("approval.approved events = %d, want 1", got)
	}
	if got := fl.status(fxRun); got != run.RunStatusRunning {
		t.Errorf("run status = %s, want running (the winner released it)", got)
	}

	// One more attempt after the race is still refused, and writes nothing.
	res, err := fl.consumeAndRun(consumeIn, exec)
	if !errors.Is(err, approval.ErrApprovalNoReceipt) {
		t.Fatalf("post-race consumption error = %v, want ErrApprovalNoReceipt", err)
	}
	if res.Granted {
		t.Error("the post-race consumption reported Granted")
	}
	if got := exec.count(); got != 1 {
		t.Errorf("the fake executor ran %d times after the post-race attempt, want 1", got)
	}
	if got := fl.f.countRows(t, "approval_consumptions"); got != 1 {
		t.Errorf("receipts = %d after the post-race attempt, want 1", got)
	}
	t.Logf("consumers=%d granted=1 already_consumed=%d run_not_waiting=%d",
		consumers, alreadyConsumed, runNotWaiting)

	// The receipt insert is the mutex on its own when nothing about the Run can
	// refuse a loser: a low-risk (auto-approved) grant leaves the Run running and
	// waiting for no approval, so every consumer passes the Run check and the
	// only way to lose is the (approval, tool_call) receipt — the losers must all
	// come back as ErrAlreadyConsumed, and the tool still runs exactly once.
	t.Run("a low-risk grant loses on the receipt alone", func(t *testing.T) {
		fl := newFlow(t)
		fl.running()
		lowIn := fl.lowRiskRequest(t, "ap-low-once", "tc_low_once")
		low := fl.mustRequestTool(lowIn).Approval
		if low.Status != approval.StatusApproved {
			t.Fatalf("low-risk approval status = %s, want approved (auto)", low.Status)
		}
		lowExec := &fakeExecutor{}
		lowIn2 := fl.consumeInput(t, low, lowIn.FingerprintInput)

		granted, alreadyConsumed := 0, 0
		var lowWg sync.WaitGroup
		lowStart := make(chan struct{})
		lowResults := make(chan error, consumers)
		for i := 0; i < consumers; i++ {
			lowWg.Add(1)
			go func() {
				defer lowWg.Done()
				<-lowStart
				_, err := fl.consumeAndRun(lowIn2, lowExec)
				lowResults <- err
			}()
		}
		close(lowStart)
		lowWg.Wait()
		close(lowResults)
		for err := range lowResults {
			switch {
			case err == nil:
				granted++
			case errors.Is(err, approval.ErrApprovalNoReceipt) && errors.Is(err, approval.ErrAlreadyConsumed):
				alreadyConsumed++
			default:
				t.Errorf("low-risk consumer error = %v, want granted or ErrAlreadyConsumed", err)
			}
		}
		if granted != 1 || alreadyConsumed != consumers-1 {
			t.Fatalf("low-risk consumers: granted=%d already_consumed=%d, want 1/%d",
				granted, alreadyConsumed, consumers-1)
		}
		if got := lowExec.count(); got != 1 {
			t.Errorf("the fake executor ran %d times on a low-risk grant, want exactly 1", got)
		}
		if got := fl.status(fxRun); got != run.RunStatusRunning {
			t.Errorf("run status = %s after a low-risk grant, want running (it never waited)", got)
		}
		// A further attempt is still refused: the receipt stands.
		if _, err := fl.consumeAndRun(lowIn2, lowExec); !errors.Is(err, approval.ErrAlreadyConsumed) {
			t.Fatalf("post-race low-risk consumption = %v, want ErrAlreadyConsumed", err)
		}
		if got := lowExec.count(); got != 1 {
			t.Errorf("the fake executor ran %d times after the post-race attempt, want 1", got)
		}
	})
}

// ---------------------------------------------------------------------------
// TestCancelApproveRace
// ---------------------------------------------------------------------------

// TestCancelApproveRace is §27.2.5/§27.2.6: a cancel and an approve/consume race
// for one waiting Run, and exactly one valid execution outcome must exist.
//
// A = "the human approves, the call is consumed, and only a granted receipt
// releases the executor"; B = "the cancel as one transaction: the Run moves to
// cancelling and InvalidatePendingForRunTx runs with it". Rounds run in four
// modes — concurrent, and three forced orders that deterministically cover the
// three legal endings:
//
//   - cancel-won:          the approval is invalidated; A's answer loses with
//     ErrApprovalAlreadyDecided/ErrApprovalConflict, reads the current row and
//     its consumption is refused; executor 0, no receipt, no
//     approval.approved.
//   - approved-unconsumed: the decision landed but the Run was already
//     cancelling; the consumption is refused ErrApprovalRunNotConsumable;
//     executor 0, no receipt, no approval.approved.
//   - executed:            the decision and the consumption both landed before
//     the cancel; executor exactly 1, exactly 1 receipt, and
//     approval.approved precedes run.cancel_requested; the Run ends cancelling.
//
// Every round asserts the state is self-consistent — the cancel always applies
// (final status cancelling), exactly one decision event, executor and receipts
// agree and never exceed one, a refusal never runs the tool — and the per-round
// ending is counted, including the concurrent rounds.
//
// Run with -count=10.
func TestCancelApproveRace(t *testing.T) {
	const rounds = 30
	outcomes := map[string]int{}
	for round := 0; round < rounds; round++ {
		mode := "concurrent"
		switch {
		case round%10 == 0:
			mode = "cancel-first"
		case round%10 == 5:
			mode = "approve-first"
		case round%10 == 7:
			mode = "executed-first"
		}
		ending := cancelApproveRound(t, round, mode)
		outcomes[ending]++
		if mode == "cancel-first" && ending != "cancel-won" {
			t.Fatalf("round %d: forced cancel-first ended as %s, want cancel-won", round, ending)
		}
		if mode == "approve-first" && ending != "approved-unconsumed" {
			t.Fatalf("round %d: forced approve-first ended as %s, want approved-unconsumed", round, ending)
		}
		if mode == "executed-first" && ending != "executed" {
			t.Fatalf("round %d: forced executed-first ended as %s, want executed", round, ending)
		}
	}
	for _, ending := range []string{"cancel-won", "approved-unconsumed", "executed"} {
		if outcomes[ending] == 0 {
			t.Errorf("ending %s never occurred in %d rounds", ending, rounds)
		}
	}
	t.Logf("%d rounds: cancel-won=%d approved-unconsumed=%d executed=%d",
		rounds, outcomes["cancel-won"], outcomes["approved-unconsumed"], outcomes["executed"])
}

// cancelApproveRound runs one round of the race and returns the ending it
// classified into, after asserting the round's invariants.
func cancelApproveRound(t *testing.T, round int, mode string) string {
	t.Helper()
	fl := newFlow(t)
	fl.running()
	in := fl.toolRequest(t, "ap-race", "tc_race")
	pending := fl.mustRequestTool(in).Approval
	exec := &fakeExecutor{}
	consumeIn := fl.consumeInput(t, pending, in.FingerprintInput)
	decideIn := userDecision(pending, true, fl.f.now.Add(10*time.Second))
	cancelAt := fl.f.now.Add(20 * time.Second)

	var (
		decided    approval.Approval
		decideErr  error
		consumed   approval.ConsumeResult
		consumeErr error
		canceled   []approval.Approval
		cancelErr  error
	)
	decide := func() { decided, decideErr = fl.decide(decideIn) }
	consume := func() { consumed, consumeErr = fl.consumeAndRun(consumeIn, exec) }
	decideThenConsume := func() {
		decide()
		// Even a caller that ignored the answer's error cannot get a grant:
		// the consumption is attempted either way.
		consume()
	}
	cancel := func() { canceled, cancelErr = fl.runCancel("run_cancelled", cancelAt) }

	switch mode {
	case "concurrent":
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			decideThenConsume()
		}()
		go func() {
			defer wg.Done()
			<-start
			cancel()
		}()
		close(start)
		wg.Wait()
	case "cancel-first":
		cancel()
		decideThenConsume()
	case "approve-first":
		decide()
		cancel()
		consume()
	case "executed-first":
		decide()
		consume()
		cancel()
	default:
		t.Fatalf("round %d: unknown mode %q", round, mode)
	}
	if cancelErr != nil {
		t.Fatalf("round %d: the cancel failed: %v", round, cancelErr)
	}

	status, _ := fl.approvalStatus(pending.ID)
	execCount := exec.count()
	receipts := fl.f.countRows(t, "approval_consumptions")
	runStatus := fl.status(fxRun)
	events := fl.eventTypes(fxRun)
	requiredEvents := fl.countEvents(fxRun, string(run.EventApprovalRequired))
	decidedEvents := fl.countEvents(fxRun, string(run.EventApprovalDecided))
	approvedEvents := fl.countEvents(fxRun, string(run.EventApprovalApproved))
	cancelEvents := fl.countEvents(fxRun, string(run.EventRunCancelRequested))
	approvedIdx := eventIndex(events, string(run.EventApprovalApproved))
	cancelIdx := eventIndex(events, string(run.EventRunCancelRequested))

	// Invariants shared by every ending.
	if requiredEvents != 1 || cancelEvents != 1 {
		t.Fatalf("round %d: approval.required=%d run.cancel_requested=%d, want 1/1",
			round, requiredEvents, cancelEvents)
	}
	if decidedEvents != 1 {
		t.Fatalf("round %d: approval.decided events = %d, want exactly one decision", round, decidedEvents)
	}
	if runStatus != run.RunStatusCancelling {
		t.Fatalf("round %d: run status = %s, want cancelling (a cancel was issued and must apply)", round, runStatus)
	}
	if execCount != receipts {
		t.Fatalf("round %d: executor ran %d times with %d receipts; an execution without a receipt, or a receipt without an execution, is forbidden",
			round, execCount, receipts)
	}
	if execCount > 1 || receipts > 1 {
		t.Fatalf("round %d: executor=%d receipts=%d, want at most one valid execution", round, execCount, receipts)
	}

	switch {
	case execCount == 1:
		// The approve/consume pair won: the Run was released and then cancelled.
		if status != approval.StatusApproved {
			t.Fatalf("round %d: executed with approval status %s, want approved", round, status)
		}
		if decideErr != nil {
			t.Fatalf("round %d: the decision failed (%v) yet the call executed", round, decideErr)
		}
		if consumeErr != nil || !consumed.Granted || !consumed.RunMoved {
			t.Fatalf("round %d: consume = granted %v moved %v err %v, want granted and the run released",
				round, consumed.Granted, consumed.RunMoved, consumeErr)
		}
		if approvedEvents != 1 || approvedIdx < 0 || cancelIdx < approvedIdx {
			t.Fatalf("round %d: approval.approved=%d at %d before run.cancel_requested at %d; the run must have returned to running before it was cancelled",
				round, approvedEvents, approvedIdx, cancelIdx)
		}
		return "executed"

	case status == approval.StatusInvalidated:
		// The cancel got there first: the human answer must lose and read the
		// current row, never revive the terminal fact.
		if len(canceled) != 1 || canceled[0].ID != pending.ID {
			t.Fatalf("round %d: the cancel invalidated %v, want the pending approval %s", round, canceled, pending.ID)
		}
		if decideErr == nil {
			t.Fatalf("round %d: the decision succeeded on an invalidated approval", round)
		}
		if !errors.Is(decideErr, approval.ErrApprovalAlreadyDecided) && !errors.Is(decideErr, approval.ErrApprovalConflict) {
			t.Fatalf("round %d: the losing decision returned %v, want already-decided or conflict", round, decideErr)
		}
		if decided.Status != approval.StatusInvalidated {
			t.Fatalf("round %d: the losing decision read back %s, want the current row (invalidated)", round, decided.Status)
		}
		if consumeErr == nil || !errors.Is(consumeErr, approval.ErrApprovalNoReceipt) {
			t.Fatalf("round %d: consuming an invalidated approval = %v, want ErrApprovalNoReceipt", round, consumeErr)
		}
		if consumed.Granted {
			t.Fatalf("round %d: a refused consumption reported Granted", round)
		}
		if approvedEvents != 0 || approvedIdx != -1 {
			t.Fatalf("round %d: approval.approved events = %d, want 0", round, approvedEvents)
		}
		return "cancel-won"

	case status == approval.StatusApproved:
		// The decision landed, but the Run was already cancelling when the
		// consumption arrived: the grant is unconsumable history.
		if decideErr != nil {
			t.Fatalf("round %d: the decision failed (%v) yet the approval is approved", round, decideErr)
		}
		if !errors.Is(consumeErr, approval.ErrApprovalRunNotConsumable) || !errors.Is(consumeErr, approval.ErrApprovalNoReceipt) {
			t.Fatalf("round %d: consume after the cancel = %v, want ErrApprovalRunNotConsumable wrapping ErrApprovalNoReceipt", round, consumeErr)
		}
		if consumed.Granted {
			t.Fatalf("round %d: a refused consumption reported Granted", round)
		}
		if approvedEvents != 0 || approvedIdx != -1 {
			t.Fatalf("round %d: approval.approved events = %d, want 0 (the run never resumed)", round, approvedEvents)
		}
		return "approved-unconsumed"
	}
	t.Fatalf("round %d: ending is none of the three legal shapes: approval %s, executor %d, receipts %d",
		round, status, execCount, receipts)
	return ""
}

// ---------------------------------------------------------------------------
// TestApprovalExpiryFakeClock
// ---------------------------------------------------------------------------

// TestApprovalExpiryFakeClock is §15 T2.02's "过期授权拒绝" driven entirely by the
// caller's clock — no test ever waits:
//
//	(a) a decision at or past the deadline expires the row instead of approving;
//	(b) ExpireDueTx then ResolveDeniedTx returns the Run with reason "expired";
//	(c) an approved but unconsumed grant is refused past its deadline, and once
//	    the refusal reached the backend the Run returns with reason "stale" (CA-4);
//	(d) the instant before the deadline still grants, and the deadline instant
//	    itself is already too late.
func TestApprovalExpiryFakeClock(t *testing.T) {
	t.Run("a decision past the deadline expires instead of approving", func(t *testing.T) {
		fl := newFlow(t)
		fl.running()
		in := fl.toolRequest(t, "ap-a", "tc_a")
		in.ExpiresAt = fl.f.now.Add(time.Minute)
		pending := fl.mustRequestTool(in).Approval
		exec := &fakeExecutor{}

		decided, err := fl.decide(userDecision(pending, true, fl.f.now.Add(2*time.Minute)))
		if !errors.Is(err, approval.ErrApprovalExpired) {
			t.Fatalf("error = %v, want ErrApprovalExpired", err)
		}
		if !approval.RefusalWrote(err) {
			t.Fatal("RefusalWrote = false; the expiry was written as the outcome and must be committed")
		}
		fromErr, ok := approval.ExpiredApproval(err)
		if !ok || fromErr.ID != pending.ID || fromErr.Status != approval.StatusExpired {
			t.Fatalf("ExpiredApproval = %+v ok=%v, want the expired row %s", fromErr, ok, pending.ID)
		}
		if decided.Status != approval.StatusExpired || decided.DecidedBy != "system:expiry" {
			t.Fatalf("decided = %s by %q, want expired by system:expiry", decided.Status, decided.DecidedBy)
		}
		if status, _ := fl.approvalStatus(pending.ID); status != approval.StatusExpired {
			t.Fatalf("stored status = %s, want expired (it can never be granted)", status)
		}
		if got := fl.countEvents(fxRun, string(run.EventApprovalApproved)); got != 0 {
			t.Errorf("approval.approved events = %d, want 0", got)
		}
		if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
			t.Errorf("run status = %s, want waiting_approval (the decision alone moves nothing)", got)
		}
		// The expired approval authorizes nothing, even with its exact binding.
		res, err := fl.consumeAndRun(fl.consumeInput(t, decided, in.FingerprintInput), exec)
		if !errors.Is(err, approval.ErrApprovalNotConsumable) || !errors.Is(err, approval.ErrApprovalNoReceipt) {
			t.Fatalf("consuming the expired approval = %v, want ErrApprovalNotConsumable wrapping ErrApprovalNoReceipt", err)
		}
		if res.Granted {
			t.Error("consuming the expired approval reported Granted")
		}
		if got := exec.count(); got != 0 {
			t.Errorf("the fake executor ran %d times, want 0", got)
		}
		if got := fl.f.countRows(t, "approval_consumptions"); got != 0 {
			t.Errorf("receipts = %d, want 0", got)
		}
	})

	t.Run("the sweep expires the request and the backend answers expired", func(t *testing.T) {
		fl := newFlow(t)
		fl.running()
		in := fl.toolRequest(t, "ap-b", "tc_b")
		in.ExpiresAt = fl.f.now.Add(time.Minute)
		pending := fl.mustRequestTool(in).Approval
		exec := &fakeExecutor{}

		expired, err := fl.expire(fl.f.projID, fl.f.now.Add(2*time.Minute))
		if err != nil {
			t.Fatalf("ExpireDueTx: %v", err)
		}
		if len(expired) != 1 || expired[0].ID != pending.ID ||
			expired[0].Status != approval.StatusExpired || expired[0].DecidedBy != "system:expiry" {
			t.Fatalf("expired = %+v, want only %s expired by system:expiry", expired, pending.ID)
		}
		if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
			t.Fatalf("run status after the sweep = %s, want waiting_approval until the refusal is delivered", got)
		}

		_, moved, err := fl.resolveDenied(pending.ID, backendActor, fl.f.now.Add(3*time.Minute))
		if err != nil {
			t.Fatalf("ResolveDeniedTx: %v", err)
		}
		if moved.From != run.RunStatusWaitingApproval || moved.To != run.RunStatusRunning {
			t.Fatalf("transition = %s -> %s, want waiting_approval -> running", moved.From, moved.To)
		}
		if got := fl.status(fxRun); got != run.RunStatusRunning {
			t.Errorf("run status = %s, want running", got)
		}
		denied := fl.eventPayloads(fxRun, string(run.EventApprovalDenied))
		if len(denied) != 1 || denied[0]["reason"] != "expired" || denied[0]["approval_id"] != pending.ID {
			t.Fatalf("approval.denied payloads = %v, want exactly one with reason expired for %s", denied, pending.ID)
		}
		if got := exec.count(); got != 0 {
			t.Errorf("the fake executor ran %d times, want 0", got)
		}
		if got := fl.f.countRows(t, "approval_consumptions"); got != 0 {
			t.Errorf("receipts = %d, want 0", got)
		}
	})

	t.Run("an approved grant past its deadline is refused and answered stale", func(t *testing.T) {
		fl := newFlow(t)
		fl.running()
		in := fl.toolRequest(t, "ap-c", "tc_c")
		in.ExpiresAt = fl.f.now.Add(time.Minute)
		pending := fl.mustRequestTool(in).Approval
		approved, err := fl.decide(userDecision(pending, true, fl.f.now.Add(30*time.Second)))
		if err != nil {
			t.Fatalf("DecideTx: %v", err)
		}
		exec := &fakeExecutor{}

		consumeIn := fl.consumeInput(t, approved, in.FingerprintInput)
		consumeIn.Now = fl.f.now.Add(2 * time.Minute) // past the deadline
		res, err := fl.consumeAndRun(consumeIn, exec)
		if !errors.Is(err, approval.ErrApprovalNotConsumable) || !errors.Is(err, approval.ErrApprovalNoReceipt) {
			t.Fatalf("consuming the expired grant = %v, want ErrApprovalNotConsumable wrapping ErrApprovalNoReceipt", err)
		}
		if res.Granted {
			t.Error("consuming the expired grant reported Granted")
		}
		if got := exec.count(); got != 0 {
			t.Errorf("the fake executor ran %d times, want 0", got)
		}
		if got := fl.f.countRows(t, "approval_consumptions"); got != 0 {
			t.Errorf("receipts = %d, want 0", got)
		}
		if got := fl.status(fxRun); got != run.RunStatusWaitingApproval {
			t.Fatalf("run status = %s, want waiting_approval", got)
		}

		// The refusal is delivered; the grant is approved history the waiting call
		// cannot use, so the Run returns with reason "stale" (CA-4).
		_, moved, err := fl.resolveDenied(approved.ID, backendActor, fl.f.now.Add(3*time.Minute))
		if err != nil {
			t.Fatalf("ResolveDeniedTx: %v", err)
		}
		if moved.From != run.RunStatusWaitingApproval || moved.To != run.RunStatusRunning {
			t.Fatalf("transition = %s -> %s, want waiting_approval -> running", moved.From, moved.To)
		}
		denied := fl.eventPayloads(fxRun, string(run.EventApprovalDenied))
		if len(denied) != 1 || denied[0]["reason"] != "stale" || denied[0]["approval_id"] != approved.ID {
			t.Fatalf("approval.denied payloads = %v, want exactly one with reason stale for %s", denied, approved.ID)
		}
		if status, _ := fl.approvalStatus(approved.ID); status != approval.StatusApproved {
			t.Errorf("approval status = %s, want approved (the decision stays as history)", status)
		}
	})

	t.Run("the instant before the deadline still grants", func(t *testing.T) {
		fl := newFlow(t)
		fl.running()
		in := fl.toolRequest(t, "ap-d", "tc_d")
		in.ExpiresAt = fl.f.now.Add(time.Minute)
		pending := fl.mustRequestTool(in).Approval
		approved, err := fl.decide(userDecision(pending, true, fl.f.now.Add(30*time.Second)))
		if err != nil {
			t.Fatalf("DecideTx: %v", err)
		}
		exec := &fakeExecutor{}

		consumeIn := fl.consumeInput(t, approved, in.FingerprintInput)
		consumeIn.Now = fl.f.now.Add(time.Minute - time.Millisecond)
		res, err := fl.consumeAndRun(consumeIn, exec)
		if err != nil {
			t.Fatalf("consuming one instant before the deadline: %v", err)
		}
		if !res.Granted || !res.RunMoved {
			t.Fatalf("consume = granted %v moved %v, want both true", res.Granted, res.RunMoved)
		}
		if got := fl.status(fxRun); got != run.RunStatusRunning {
			t.Errorf("run status = %s, want running", got)
		}
		if got := exec.count(); got != 1 {
			t.Errorf("the fake executor ran %d times, want 1", got)
		}
		if got := fl.f.countRows(t, "approval_consumptions"); got != 1 {
			t.Errorf("receipts = %d, want 1", got)
		}
	})

	t.Run("the deadline instant itself is already too late", func(t *testing.T) {
		fl := newFlow(t)
		fl.running()
		in := fl.toolRequest(t, "ap-e", "tc_e")
		in.ExpiresAt = fl.f.now.Add(time.Minute)
		pending := fl.mustRequestTool(in).Approval
		approved, err := fl.decide(userDecision(pending, true, fl.f.now.Add(30*time.Second)))
		if err != nil {
			t.Fatalf("DecideTx: %v", err)
		}
		exec := &fakeExecutor{}

		consumeIn := fl.consumeInput(t, approved, in.FingerprintInput)
		consumeIn.Now = fl.f.now.Add(time.Minute) // exactly at expires_at: the deadline is exclusive
		res, err := fl.consumeAndRun(consumeIn, exec)
		if !errors.Is(err, approval.ErrApprovalNotConsumable) || !errors.Is(err, approval.ErrApprovalNoReceipt) {
			t.Fatalf("consuming exactly at the deadline = %v, want ErrApprovalNotConsumable wrapping ErrApprovalNoReceipt", err)
		}
		if res.Granted {
			t.Error("consuming exactly at the deadline reported Granted")
		}
		if got := exec.count(); got != 0 {
			t.Errorf("the fake executor ran %d times, want 0", got)
		}
		if got := fl.f.countRows(t, "approval_consumptions"); got != 0 {
			t.Errorf("receipts = %d, want 0", got)
		}
	})
}
