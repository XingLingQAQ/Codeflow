package guard

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/audit"
)

// testAuditor captures guard audit events for assertion.
type testAuditor struct {
	decisions []Decision
	events    []*audit.AuditLogEntry
}

func (a *testAuditor) RecordGuardDecision(_ context.Context, _ string, d Decision) error {
	a.decisions = append(a.decisions, d)
	return nil
}

func (a *testAuditor) RecordGuardEvent(_ context.Context, action string, details map[string]interface{}) error {
	a.events = append(a.events, &audit.AuditLogEntry{Action: action, Details: details})
	return nil
}

func TestExemptionRequestApproveActivatesExemption(t *testing.T) {
	aud := &testAuditor{}
	e := NewEngine(nil, aud)
	ctx := context.Background()
	path := filepath.Join("proj", "utils2.go")

	// Without exemption the stacked-naming rule blocks.
	if err := e.BeforeWrite(ctx, path, []byte("package p\n")); err == nil {
		t.Fatal("expected block without exemption")
	}

	req, err := e.RequestExemption(ctx, ExemptionRequest{
		Path:      path,
		RuleID:    RuleStackedNaming,
		Reason:    "approved rename needed",
		Requester: "agent-123",
		TTL:       5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Status != RequestPending {
		t.Fatalf("status=%s want pending", req.Status)
	}

	// Still blocked while pending.
	if err := e.BeforeWrite(ctx, path, []byte("package p\n")); err == nil {
		t.Fatal("expected block while request is pending")
	}

	decided, err := e.DecideExemptionRequest(ctx, req.ID, true, "admin", "looks fine")
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != RequestApproved {
		t.Fatalf("status=%s want approved", decided.Status)
	}

	// Now the write should pass.
	if err := e.BeforeWrite(ctx, path, []byte("package p\n")); err != nil {
		t.Fatalf("expected allow after approval: %v", err)
	}

	// Audit received both events.
	if len(aud.events) < 2 {
		t.Fatalf("expected 2 audit events, got %d", len(aud.events))
	}
	if aud.events[0].Action != "guard.exemption_requested" {
		t.Fatalf("event[0] action=%s", aud.events[0].Action)
	}
	if aud.events[1].Action != "guard.exemption_decided" {
		t.Fatalf("event[1] action=%s", aud.events[1].Action)
	}
}

func TestExemptionRequestRejectNoExemption(t *testing.T) {
	e := NewEngine(nil, nil)
	ctx := context.Background()
	path := filepath.Join("proj", "utils2.go")

	req, err := e.RequestExemption(ctx, ExemptionRequest{
		Path:      path,
		RuleID:    RuleStackedNaming,
		Reason:    "want rename",
		Requester: "agent-456",
	})
	if err != nil {
		t.Fatal(err)
	}

	decided, err := e.DecideExemptionRequest(ctx, req.ID, false, "admin", "denied")
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != RequestRejected {
		t.Fatalf("status=%s want rejected", decided.Status)
	}

	// Write should still be blocked.
	if err := e.BeforeWrite(ctx, path, []byte("package p\n")); err == nil {
		t.Fatal("expected block after rejection")
	}
}

func TestExemptionRequestDoubleDeclideRejected(t *testing.T) {
	e := NewEngine(nil, nil)
	ctx := context.Background()

	req, err := e.RequestExemption(ctx, ExemptionRequest{
		Path: "proj/x2.go", Reason: "r", Requester: "a",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.DecideExemptionRequest(ctx, req.ID, true, "admin", "ok"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DecideExemptionRequest(ctx, req.ID, false, "admin2", "changed mind"); err == nil {
		t.Fatal("expected error on double-decide")
	}
}

func TestExemptionRequestListFiltersAndOrdering(t *testing.T) {
	e := NewEngine(nil, nil)
	ctx := context.Background()

	r1, _ := e.RequestExemption(ctx, ExemptionRequest{Path: "a2.go", Reason: "r1", Requester: "a"})
	time.Sleep(time.Millisecond)
	r2, _ := e.RequestExemption(ctx, ExemptionRequest{Path: "b2.go", Reason: "r2", Requester: "a"})
	time.Sleep(time.Millisecond)
	r3, _ := e.RequestExemption(ctx, ExemptionRequest{Path: "c2.go", Reason: "r3", Requester: "a"})
	e.DecideExemptionRequest(ctx, r2.ID, false, "admin", "nope")

	all := e.ListExemptionRequests("")
	if len(all) != 3 {
		t.Fatalf("all len=%d want 3", len(all))
	}
	// Newest first: r3, r2, r1
	if all[0].ID != r3.ID || all[2].ID != r1.ID {
		t.Fatalf("ordering: got [%s,%s,%s] want [%s,%s,%s]", all[0].ID, all[1].ID, all[2].ID, r3.ID, r2.ID, r1.ID)
	}

	pending := e.ListExemptionRequests(RequestPending)
	if len(pending) != 2 {
		t.Fatalf("pending len=%d want 2", len(pending))
	}
	rejected := e.ListExemptionRequests(RequestRejected)
	if len(rejected) != 1 || rejected[0].ID != r2.ID {
		t.Fatalf("rejected=%+v", rejected)
	}
}

func TestExemptionRequestValidationRejections(t *testing.T) {
	e := NewEngine(nil, nil)
	ctx := context.Background()
	cases := []ExemptionRequest{
		{Path: "", Reason: "r", Requester: "a"},
		{Path: "x", Reason: "", Requester: "a"},
		{Path: "x", Reason: "r", Requester: ""},
	}
	for i, c := range cases {
		if _, err := e.RequestExemption(ctx, c); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestExemptionRequestPersistenceAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "guard_exreq.db")

	e1 := NewEngine(nil, nil)
	if err := e1.OpenExemptionStore(dbPath); err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}

	req, err := e1.RequestExemption(ctx, ExemptionRequest{
		Path: "proj/v2.go", Reason: "need", Requester: "agent-a",
	})
	if err != nil {
		_ = e1.CloseExemptionStore()
		t.Fatal(err)
	}
	dec, err := e1.DecideExemptionRequest(ctx, req.ID, true, "admin", "approved")
	if err != nil {
		_ = e1.CloseExemptionStore()
		t.Fatal(err)
	}

	// Create a second pending request.
	req2, err := e1.RequestExemption(ctx, ExemptionRequest{
		Path: "proj/w2.go", Reason: "another", Requester: "agent-b",
	})
	if err != nil {
		_ = e1.CloseExemptionStore()
		t.Fatal(err)
	}

	_ = e1.CloseExemptionStore()

	// Reopen on a fresh engine.
	e2 := NewEngine(nil, nil)
	if err := e2.OpenExemptionStore(dbPath); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = e2.CloseExemptionStore() }()

	all := e2.ListExemptionRequests("")
	if len(all) != 2 {
		t.Fatalf("expected 2 requests after reopen, got %d", len(all))
	}

	// Approved request preserved with decision fields.
	var found bool
	for _, r := range all {
		if r.ID == dec.ID {
			found = true
			if r.Status != RequestApproved {
				t.Fatalf("approved status lost: %s", r.Status)
			}
			if r.DecidedBy != "admin" {
				t.Fatalf("decided_by lost: %s", r.DecidedBy)
			}
		}
	}
	if !found {
		t.Fatal("approved request not found after reopen")
	}

	// Pending request also survives.
	var foundPending bool
	for _, r := range all {
		if r.ID == req2.ID && r.Status == RequestPending {
			foundPending = true
		}
	}
	if !foundPending {
		t.Fatal("pending request not found after reopen")
	}
}

func TestExemptionRequestAuditBridgeEvents(t *testing.T) {
	log := &memAudit{}
	bridge := NewAuditBridge(log)
	e := NewEngine(nil, bridge)
	ctx := context.Background()

	req, err := e.RequestExemption(ctx, ExemptionRequest{
		Path: "proj/t2.go", Reason: "need", Requester: "agent-x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.DecideExemptionRequest(ctx, req.ID, true, "admin", "ok"); err != nil {
		t.Fatal(err)
	}

	// AuditBridge should have logged the guard.before_write decision (from the
	// approval check above was not a write), plus the two lifecycle events.
	requested, decided := 0, 0
	for _, entry := range log.entries {
		switch entry.Action {
		case "guard.exemption_requested":
			requested++
		case "guard.exemption_decided":
			decided++
		}
	}
	if requested != 1 {
		t.Fatalf("guard.exemption_requested count=%d want 1", requested)
	}
	if decided != 1 {
		t.Fatalf("guard.exemption_decided count=%d want 1", decided)
	}
}
