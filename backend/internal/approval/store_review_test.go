package approval_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/approval"
	"github.com/codeflow/backend/internal/runstore"
)

// These tests pin the rules added when T2.02.a was accepted: a tool approval
// authorizes exactly the call it was requested for, only until its deadline,
// against the run's own agent revision, and an approval row is history that is
// never deleted. Each one failed against the delivered store before the fix.

// TestConsumeOnlyTheApprovedCall: a tool approval is requested for one tool
// call — its subject — and only that call can spend it. Without this rule one
// approved call would authorize every other call of the attempt, each under a
// receipt of its own (the receipt key is (approval_id, tool_call_id)).
func TestConsumeOnlyTheApprovedCall(t *testing.T) {
	f := newFixture(t)
	stored := f.mustCreate(t, f.newApproval(t)) // subject tc_1
	f.approve(t, stored.ID, "user-1", f.now.Add(time.Minute))

	_, err := f.consume(t, approval.ConsumptionInput{
		ApprovalID: stored.ID, ToolCallID: "tc_2", AttemptID: f.attempt, ConsumedAt: f.now.Add(2 * time.Minute),
	})
	if !errors.Is(err, approval.ErrApprovalNotConsumable) {
		t.Fatalf("consuming the approval of tc_1 for tc_2 = %v, want ErrApprovalNotConsumable", err)
	}
	// The store's own check names the call the approval is for; the schema
	// trigger behind it would refuse too, but without saying why.
	if !strings.Contains(err.Error(), "authorizes tool call tc_1") {
		t.Errorf("error %q does not name the approval's own call", err)
	}
	if n := f.countRows(t, "approval_consumptions"); n != 0 {
		t.Fatalf("receipts = %d, want 0", n)
	}

	if _, err := f.consume(t, approval.ConsumptionInput{
		ApprovalID: stored.ID, ToolCallID: "tc_1", AttemptID: f.attempt, ConsumedAt: f.now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("consuming the approval for its own call: %v", err)
	}
	// Spent on its own call, it still authorizes no other.
	_, err = f.consume(t, approval.ConsumptionInput{
		ApprovalID: stored.ID, ToolCallID: "tc_3", AttemptID: f.attempt, ConsumedAt: f.now.Add(3 * time.Minute),
	})
	if !errors.Is(err, approval.ErrApprovalNotConsumable) {
		t.Fatalf("a spent approval consumed for another call = %v, want ErrApprovalNotConsumable", err)
	}
	if n := f.countRows(t, "approval_consumptions"); n != 1 {
		t.Errorf("receipts = %d, want exactly 1", n)
	}
}

// TestConsumeRefusesAnExpiredGrant is T2.02's "过期授权拒绝". An approved row
// is terminal and can never become expired (trg_approvals_terminal_is_final),
// so the deadline has to be checked when the grant is spent.
func TestConsumeRefusesAnExpiredGrant(t *testing.T) {
	f := newFixture(t)
	stored := f.mustCreate(t, f.newApproval(t)) // expires at now+1h
	f.approve(t, stored.ID, "user-1", f.now.Add(time.Minute))

	for _, at := range []time.Time{stored.ExpiresAt, stored.ExpiresAt.Add(time.Hour)} {
		_, err := f.consume(t, approval.ConsumptionInput{
			ApprovalID: stored.ID, ToolCallID: "tc_1", AttemptID: f.attempt, ConsumedAt: at,
		})
		if !errors.Is(err, approval.ErrApprovalNotConsumable) {
			t.Fatalf("consuming at %s (expires %s) = %v, want ErrApprovalNotConsumable",
				at.Format(time.RFC3339Nano), stored.ExpiresAt.Format(time.RFC3339Nano), err)
		}
		if !strings.Contains(err.Error(), "expired") {
			t.Errorf("error %q does not say the grant expired", err)
		}
	}
	if n := f.countRows(t, "approval_consumptions"); n != 0 {
		t.Fatalf("receipts = %d, want 0", n)
	}

	// The last millisecond before the deadline is still inside it.
	if _, err := f.consume(t, approval.ConsumptionInput{
		ApprovalID: stored.ID, ToolCallID: "tc_1", AttemptID: f.attempt,
		ConsumedAt: stored.ExpiresAt.Add(-time.Millisecond),
	}); err != nil {
		t.Fatalf("consuming just before the deadline: %v", err)
	}
}

// TestApprovalRowsAreHistory: an approval is never deleted, pending or decided.
// Retries keep the old run's approvals (§27.2.1), a decision is history
// (§19.1), and runstore refuses DELETE on every other history table.
func TestApprovalRowsAreHistory(t *testing.T) {
	f := newFixture(t)
	pending := f.mustCreate(t, f.newApproval(t))
	decided := f.mustCreate(t, func() approval.NewApproval {
		in := f.newApproval(t)
		in.ID = "ap-2"
		in.SubjectID = "tc_2"
		in.FingerprintInput.SubjectID = "tc_2"
		return in
	}())
	f.approve(t, decided.ID, "user-1", f.now.Add(time.Minute))

	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"a pending approval", `DELETE FROM approvals WHERE id='` + pending.ID + `'`},
		{"a decided approval", `DELETE FROM approvals WHERE id='` + decided.ID + `'`},
		{"every approval", `DELETE FROM approvals`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
				_, execErr := tx.ExecContext(ctx, tc.sql)
				return execErr
			})
			if err == nil {
				t.Fatalf("deleting %s was accepted", tc.name)
			}
			if !errors.Is(approval.MapRefusal(err), approval.ErrApprovalIsHistory) {
				t.Errorf("MapRefusal(%v) is not ErrApprovalIsHistory", err)
			}
		})
	}
	if n := f.countRows(t, "approvals"); n != 2 {
		t.Errorf("approvals rows = %d, want the original 2", n)
	}
}

// TestCreateRejectsAnotherAgentRevision: the fingerprint names the agent
// revision that will act, and the run records the one that does. An approval
// whose hash describes another revision than its run authorizes nothing that
// can ever happen, so it is refused rather than stored.
func TestCreateRejectsAnotherAgentRevision(t *testing.T) {
	t.Run("tool", func(t *testing.T) {
		f := newFixture(t)
		in := f.newApproval(t)
		in.FingerprintInput.AgentRevisionID = "ar-9" // the run executes ar-1
		_, err := f.create(t, in)
		if !errors.Is(err, approval.ErrAgentRevisionMismatch) {
			t.Fatalf("error = %v, want ErrAgentRevisionMismatch", err)
		}
		if n := f.countRows(t, "approvals"); n != 0 {
			t.Errorf("approvals rows = %d, want 0", n)
		}
	})

	gate := func(t *testing.T, f *fixture, agentRevision string) approval.NewApproval {
		in := f.newApproval(t)
		in.SubjectType = approval.SubjectGate
		in.SubjectID = "stage-1"
		in.AttemptID = ""
		in.FingerprintInput.SubjectType = approval.SubjectGate
		in.FingerprintInput.SubjectID = "stage-1"
		in.FingerprintInput.ArgumentsHash = ""
		in.FingerprintInput.AgentRevisionID = agentRevision
		return in
	}
	t.Run("gate naming a run and another revision", func(t *testing.T) {
		f := newFixture(t)
		if _, err := f.create(t, gate(t, f, "ar-9")); !errors.Is(err, approval.ErrAgentRevisionMismatch) {
			t.Fatalf("error = %v, want ErrAgentRevisionMismatch", err)
		}
	})
	t.Run("gate naming a run and no revision", func(t *testing.T) {
		f := newFixture(t)
		f.mustCreate(t, gate(t, f, ""))
	})
	t.Run("gate naming a run and its revision", func(t *testing.T) {
		f := newFixture(t)
		f.mustCreate(t, gate(t, f, "ar-1"))
	})
}

// TestCreateBoundsAToolSubjectLikeACallID: a tool approval's subject is the
// call id a consumption must present, so it obeys the same bound; a subject no
// receipt could ever name is refused when the approval is created, not
// discovered when it cannot be spent.
func TestCreateBoundsAToolSubjectLikeACallID(t *testing.T) {
	f := newFixture(t)
	in := f.newApproval(t)
	in.SubjectID = strings.Repeat("t", 257)
	in.FingerprintInput.SubjectID = in.SubjectID
	_, err := f.create(t, in)
	if !errors.Is(err, approval.ErrInvalidApproval) {
		t.Fatalf("error = %v, want ErrInvalidApproval", err)
	}
	if !strings.Contains(err.Error(), "SubjectID") {
		t.Errorf("error %q does not name SubjectID", err)
	}
	if n := f.countRows(t, "approvals"); n != 0 {
		t.Errorf("approvals rows = %d, want 0", n)
	}
}

// TestSchemaRefusesReceiptsWithoutAGrant: the receipt table itself refuses a
// receipt the store would have refused, so a hand-written INSERT cannot spend an
// approval that is not approved, belongs to another call or attempt, or has
// expired. The store's own checks give the precise error; this is the backstop.
func TestSchemaRefusesReceiptsWithoutAGrant(t *testing.T) {
	const insert = `INSERT INTO approval_consumptions (approval_id, tool_call_id, attempt_id, consumed_at)
	                VALUES (?, ?, ?, ?)`
	now := time.UnixMilli(1700000000000).UTC() // newFixture's clock
	inside := now.Add(2 * time.Minute).UnixMilli()
	deadline := now.Add(time.Hour).UnixMilli() // newApproval expires at now+1h

	for _, tc := range []struct {
		name    string
		approve bool
		args    []any
	}{
		{"pending approval", false, []any{"ap-1", "tc_1", fxAttempt, inside}},
		{"another call", true, []any{"ap-1", "tc_2", fxAttempt, inside}},
		{"another attempt", true, []any{"ap-1", "tc_1", fxOtherAtte, inside}},
		{"at the deadline", true, []any{"ap-1", "tc_1", fxAttempt, deadline}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			stored := f.mustCreate(t, f.newApproval(t))
			if tc.approve {
				f.approve(t, stored.ID, "user-1", f.now.Add(time.Minute))
			}
			err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
				_, execErr := tx.ExecContext(ctx, insert, tc.args...)
				return execErr
			})
			if err == nil {
				t.Fatalf("a hand-written receipt for %s was accepted", tc.name)
			}
			if !errors.Is(approval.MapRefusal(err), approval.ErrApprovalNotConsumable) {
				t.Errorf("MapRefusal(%v) is not ErrApprovalNotConsumable", err)
			}
			if n := f.countRows(t, "approval_consumptions"); n != 0 {
				t.Errorf("receipts = %d, want 0", n)
			}
		})
	}

	t.Run("a legal hand-written receipt is accepted", func(t *testing.T) {
		// The control: the same statement with nothing wrong must succeed, so
		// the cases above fail for their reason and not because the statement
		// or the fixture is broken.
		f := newFixture(t)
		stored := f.mustCreate(t, f.newApproval(t))
		f.approve(t, stored.ID, "user-1", f.now.Add(time.Minute))
		err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
			_, execErr := tx.ExecContext(ctx, insert, "ap-1", "tc_1", fxAttempt, inside)
			return execErr
		})
		if err != nil {
			t.Fatalf("the control receipt was refused: %v", err)
		}
	})
}
