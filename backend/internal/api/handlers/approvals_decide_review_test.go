package handlers

// Main-agent additions from the T2.02.c group 2 acceptance review: two strict
// body branches no delivered test reached. Each mutation survived the delivered
// suite — "a blank fingerprint is let through to RunCommand" (answered 409 and
// the key claimed) and "a non-integer expected_revision is read as revision 1"
// — and each test below fails under it.

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/codeflow/backend/internal/approval"
)

// TestDecideApprovalBlankFingerprintIs422BeforeAnyClaim pins fingerprint_required
// for every shape of "no fingerprint": absent, null, empty and whitespace. All
// are refused before RunCommand, so no key is claimed and nothing is written.
func TestDecideApprovalBlankFingerprintIs422BeforeAnyClaim(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(m map[string]any)
	}{
		{"absent", func(m map[string]any) { delete(m, "fingerprint") }},
		{"null", func(m map[string]any) { m["fingerprint"] = nil }},
		{"empty", func(m map[string]any) { m["fingerprint"] = "" }},
		{"whitespace", func(m map[string]any) { m["fingerprint"] = " \t " }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newApprovalDecideFixture(t)
			before := f.commandRecordCount()

			response := f.decide(f.approvalID, f.decideBody(true, tc.mutate), decideClientKey, nil)
			if response.Status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body %s)", response.Status, response.Body)
			}
			requireErrorReason(t, response.Body, string(CodeInvalidRequest), reasonFingerprintRequired)
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

// TestDecideApprovalNonIntegerExpectedRevisionIs422 pins invalid_expected_revision
// for every body revision that is not a JSON integer literal. None of them may be
// coerced into some revision and run the CAS: the decision would then be taken
// against a revision nobody stated.
func TestDecideApprovalNonIntegerExpectedRevisionIs422(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"string", `"1"`},
		{"fraction", `1.5`},
		{"integral fraction", `1.0`},
		{"exponent", `1e0`},
		{"boolean", `true`},
		{"null", `null`},
		{"object", `{}`},
		{"past int64", `9223372036854775808`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newApprovalDecideFixture(t)
			before := f.commandRecordCount()

			raw := fmt.Sprintf(`{"approved":true,"fingerprint":%q,"expected_revision":%s}`, f.fingerprint, tc.value)
			response := f.decide(f.approvalID, raw, decideClientKey, nil)
			if response.Status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body %s)", response.Status, response.Body)
			}
			requireErrorReason(t, response.Body, string(CodeInvalidRequest), reasonInvalidExpectedRev)
			if row := f.storedApproval(f.approvalID); row.Status != approval.StatusPending {
				t.Errorf("stored status = %s, want pending (nothing written)", row.Status)
			}
			if got := f.commandRecordCount(); got != before {
				t.Errorf("command records = %d, want %d (a 422 before RunCommand claims no key)", got, before)
			}
		})
	}
}
