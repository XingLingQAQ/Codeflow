package handlers

import (
	"net/http"
	"testing"

	"github.com/codeflow/backend/internal/approval"
)

// The T2.02.c named tests: §28 T2.02.c "API `approved:false` 必须落 rejected；
// 失败后不得有工具副作用", driven through the real HTTP route, the real
// runstore and the real approval service.
//
// The single property this file exists for: `false` is an answer. A request
// that says `"approved": false` must decide the approval as rejected — the same
// 200 path as `true`, one approval.decided event, the row terminal. A request
// that does not say anything about `approved` at all — absent, null, a string,
// or a differently-cased key — is not a denial: it is refused with 422 and
// writes nothing, claims no idempotency key and emits no event.

// TestApprovalFalseIsDecision is the named test: `approved:false` is a decision
// that lands as rejected, never "not filled in".
func TestApprovalFalseIsDecision(t *testing.T) {
	f := newApprovalDecideFixture(t)

	body := f.decideBody(false, func(m map[string]any) { m["reason"] = "the command touches the release branch" })
	response := f.decide(f.approvalID, body, decideClientKey, nil)
	if response.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", response.Status, response.Body)
	}
	decoded := decodeAPIResponse(t, response)
	requireDecidedData(t, decoded, f.approvalID, "rejected", 2, decideClientKey)

	// The row is rejected, by this principal, and the decision is what the
	// response said it was.
	row := f.storedApproval(f.approvalID)
	if row.Status != approval.StatusRejected {
		t.Fatalf("stored status = %s, want rejected", row.Status)
	}
	if row.DecidedBy != principalUnderTest {
		t.Errorf("stored decided_by = %s, want %s", row.DecidedBy, principalUnderTest)
	}

	// Exactly one approval.decided event, and its payload says rejected and
	// carries the comment the decider gave.
	if got := f.countApprovalEvents(f.approvalID); got != 1 {
		t.Fatalf("approval.decided events = %d, want 1", got)
	}
	payload := f.approvalEventPayload(f.approvalID)
	if payload["status"] != "rejected" {
		t.Errorf("event payload.status = %v, want rejected", payload["status"])
	}
	if payload["comment"] != "the command touches the release branch" {
		t.Errorf("event payload.comment = %v, want the request's reason", payload["comment"])
	}
	if payload["reason"] != "rejected" {
		t.Errorf("event payload.reason = %v, want rejected (the answer, not the note)", payload["reason"])
	}

	// The Run did not move: a denial is delivered to the backend by another
	// card (§27.2 item 3), not by the decision endpoint.
	runRow := f.runStatus(decideTestRun)
	if runRow != "waiting_approval" {
		t.Errorf("run status = %s, want waiting_approval (a decision never moves the Run)", runRow)
	}
}

// TestApprovalFalseWithoutReasonIsStillADecision pins the failure direction of
// the same rule: `false` with no note is still a rejection, not an incomplete
// request.
func TestApprovalFalseWithoutReasonIsStillADecision(t *testing.T) {
	f := newApprovalDecideFixture(t)

	response := f.decide(f.approvalID, f.decideBody(false, nil), decideClientKey, nil)
	if response.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", response.Status, response.Body)
	}
	if row := f.storedApproval(f.approvalID); row.Status != approval.StatusRejected {
		t.Fatalf("stored status = %s, want rejected", row.Status)
	}
	payload := f.approvalEventPayload(f.approvalID)
	if _, present := payload["comment"]; present {
		t.Errorf("event payload has a comment nobody gave: %v", payload["comment"])
	}
}

// TestApprovalMissingFalseIsNotApproved covers the other direction: a request
// that never states an answer must not become a denial (or an approval). The
// three spellings of "no answer" are each 422 approved_required, and the
// approval is untouched: still pending, no event, no command record, no key
// claimed.
func TestApprovalMissingFalseIsNotApproved(t *testing.T) {
	cases := []struct {
		name       string
		wantReason string
		body       func(f *approvalDecideFixture) string
	}{
		{
			name: "absent", wantReason: reasonApprovedRequired,
			body: func(f *approvalDecideFixture) string {
				return f.decideBody(false, func(m map[string]any) { delete(m, "approved") })
			},
		},
		{
			// Null is "the client stated no answer", the same condition as an
			// absent field, so it carries the same reason.
			name: "null", wantReason: reasonApprovedRequired,
			body: func(f *approvalDecideFixture) string {
				return f.decideBody(false, func(m map[string]any) { m["approved"] = nil })
			},
		},
		{
			// A string is a malformed boolean field rather than an omitted
			// one, so it is invalid_body. Either way it is not a denial.
			name: "string false", wantReason: reasonInvalidBody,
			body: func(f *approvalDecideFixture) string {
				return f.decideBody(false, func(m map[string]any) { m["approved"] = "false" })
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newApprovalDecideFixture(t)
			before := f.commandRecordCount()

			response := f.decide(f.approvalID, tc.body(f), decideClientKey, nil)
			if response.Status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body %s)", response.Status, response.Body)
			}
			requireErrorReason(t, response.Body, string(CodeInvalidRequest), tc.wantReason)

			if row := f.storedApproval(f.approvalID); row.Status != approval.StatusPending {
				t.Errorf("stored status = %s, want pending (nothing written)", row.Status)
			}
			if got := f.countApprovalEvents(f.approvalID); got != 0 {
				t.Errorf("approval.decided events = %d, want 0", got)
			}
			if got := f.commandRecordCount(); got != before {
				t.Errorf("command records = %d, want %d (a 422 before RunCommand claims no key)", got, before)
			}
		})
	}
}

// TestApprovalCaseVariantKeyIsNotTheApprovedField pins the strict key match:
// Go's encoding/json would accept "Approved" as the field `approved`, but a
// client that spells the key differently is not the client this contract was
// written for — the value is the one thing that must never be guessed at, so
// the key is refused.
func TestApprovalCaseVariantKeyIsNotTheApprovedField(t *testing.T) {
	f := newApprovalDecideFixture(t)
	before := f.commandRecordCount()

	body := f.decideBody(false, func(m map[string]any) {
		delete(m, "approved")
		m["Approved"] = false
	})
	response := f.decide(f.approvalID, body, decideClientKey, nil)
	if response.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body %s)", response.Status, response.Body)
	}
	requireErrorReason(t, response.Body, string(CodeInvalidRequest), reasonInvalidBody)

	if row := f.storedApproval(f.approvalID); row.Status != approval.StatusPending {
		t.Errorf("stored status = %s, want pending (nothing written)", row.Status)
	}
	if got := f.countApprovalEvents(f.approvalID); got != 0 {
		t.Errorf("approval.decided events = %d, want 0", got)
	}
	if got := f.commandRecordCount(); got != before {
		t.Errorf("command records = %d, want %d", got, before)
	}
}

// TestApprovalDuplicateAndUnknownKeysAre422 covers the rest of the strict
// walk: a duplicate key and an unknown key are both refused, and neither
// claims a key or writes anything.
func TestApprovalDuplicateAndUnknownKeysAre422(t *testing.T) {
	cases := []struct {
		name string
		raw  func(f *approvalDecideFixture) string
	}{
		{
			name: "duplicate approved",
			raw: func(f *approvalDecideFixture) string {
				return `{"approved":false,"approved":true,"fingerprint":"` + f.fingerprint + `","expected_revision":1}`
			},
		},
		{
			name: "unknown key",
			raw: func(f *approvalDecideFixture) string {
				return `{"approved":false,"fingerprint":"` + f.fingerprint + `","expected_revision":1,"note":"hi"}`
			},
		},
		{
			name: "trailing content",
			raw: func(f *approvalDecideFixture) string {
				return `{"approved":false,"fingerprint":"` + f.fingerprint + `","expected_revision":1}}`
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newApprovalDecideFixture(t)
			before := f.commandRecordCount()

			response := f.decide(f.approvalID, tc.raw(f), decideClientKey, nil)
			if response.Status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body %s)", response.Status, response.Body)
			}
			requireErrorReason(t, response.Body, string(CodeInvalidRequest), reasonInvalidBody)
			if row := f.storedApproval(f.approvalID); row.Status != approval.StatusPending {
				t.Errorf("stored status = %s, want pending", row.Status)
			}
			if got := f.commandRecordCount(); got != before {
				t.Errorf("command records = %d, want %d", got, before)
			}
		})
	}
}

// TestApprovalOverlongReasonIs422 pins the comment bound before RunCommand: a
// note past MaxDecisionCommentRunes is refused without claiming a key.
func TestApprovalOverlongReasonIs422(t *testing.T) {
	f := newApprovalDecideFixture(t)
	before := f.commandRecordCount()

	long := make([]rune, approval.MaxDecisionCommentRunes+1)
	for i := range long {
		long[i] = '好'
	}
	response := f.decide(f.approvalID, f.decideBody(false, func(m map[string]any) { m["reason"] = string(long) }), decideClientKey, nil)
	if response.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body %s)", response.Status, response.Body)
	}
	requireErrorReason(t, response.Body, string(CodeInvalidRequest), reasonCommentTooLong)
	if row := f.storedApproval(f.approvalID); row.Status != approval.StatusPending {
		t.Errorf("stored status = %s, want pending", row.Status)
	}
	if got := f.commandRecordCount(); got != before {
		t.Errorf("command records = %d, want %d", got, before)
	}
}
