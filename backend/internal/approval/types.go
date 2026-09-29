package approval

import (
	"strings"
	"time"
)

// The three closed vocabularies an approval row is built from. Each has a
// Valid method and a canonical-order slice, and the slices are the single
// source the Valid methods consult, so a value added to one cannot be missing
// from the other.

// SubjectType is what an approval authorizes.
type SubjectType string

const (
	// SubjectTool is one tool call inside one attempt.
	SubjectTool SubjectType = "tool"
	// SubjectGate is a stage gate. A gate has no Run (§19.1), so a gate
	// approval may carry none.
	SubjectGate SubjectType = "gate"
	// SubjectMerge is a merge, including a manual one.
	SubjectMerge SubjectType = "merge"
)

// SubjectTypes lists every valid SubjectType in canonical order.
var SubjectTypes = []SubjectType{SubjectTool, SubjectGate, SubjectMerge}

// Valid reports whether s is one of SubjectTypes.
func (s SubjectType) Valid() bool {
	for _, v := range SubjectTypes {
		if s == v {
			return true
		}
	}
	return false
}

// Risk is the risk classification a decision is weighed against (plan §28
// T2.02.b decides by it; T3.04.b's standing grants are limited by it).
type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

// Risks lists every valid Risk in canonical order.
var Risks = []Risk{RiskLow, RiskMedium, RiskHigh}

// Valid reports whether r is one of Risks.
func (r Risk) Valid() bool {
	for _, v := range Risks {
		if r == v {
			return true
		}
	}
	return false
}

// Status is the approval's lifecycle state.
//
// The transitions are T2.02.b's business (and its CAS), but the shape is fixed
// here because the schema's CHECKs and triggers are written against it: a row
// starts pending, a decision makes it approved or rejected, a deadline makes it
// expired, and a run that ended (or a superseded subject) makes it invalidated.
// Every decided state is terminal — §19.1 "已批准历史不改", §27.2.5 "输方读取
// 当前状态，不反向复活终态" — which is what trg_approvals_terminal_is_final
// enforces in SQL.
type Status string

const (
	StatusPending     Status = "pending"
	StatusApproved    Status = "approved"
	StatusRejected    Status = "rejected"
	StatusExpired     Status = "expired"
	StatusInvalidated Status = "invalidated"
)

// Statuses lists every valid Status in canonical order: the request, the two
// decisions, then the two ways an unanswered request stops mattering.
var Statuses = []Status{StatusPending, StatusApproved, StatusRejected, StatusExpired, StatusInvalidated}

// Valid reports whether s is one of Statuses.
func (s Status) Valid() bool {
	for _, v := range Statuses {
		if s == v {
			return true
		}
	}
	return false
}

// IsTerminal reports whether s is decided (anything but pending). A terminal
// approval is never edited back (§19.1), and only an approved tool approval can
// be consumed.
func (s Status) IsTerminal() bool { return s != "" && s != StatusPending }

// Approval is one stored approval row, field for field with migration 009.
//
// Time fields cross the storage boundary once: the columns are Unix
// milliseconds (UTC), the fields are time.Time in UTC.
type Approval struct {
	ID        string
	ProjectID string
	// RunID and AttemptID are empty when the approval has no Run. A tool
	// approval always has both (schema CHECK and store rule); a stage gate or a
	// manual merge may have neither.
	RunID     string
	AttemptID string
	// SubjectType and SubjectID name what is authorized. SubjectID is opaque
	// here (a tool_call_id, a stage id, a merge candidate id).
	SubjectType SubjectType
	SubjectID   string
	Risk        Risk
	// ScopeJSON is the canonical encoding of the requested scope. The store
	// normalizes it before it is written (see CanonicalScopeJSON), so two
	// spellings of one scope cannot become two rows.
	ScopeJSON string
	// Fingerprint is "sha256:" + 64 lowercase hex over FingerprintInputJSON.
	// Only Fingerprint (fingerprint.go) produces an acceptable value, and only
	// CreateApprovalTx writes it.
	Fingerprint string
	// FingerprintInputJSON is the exact canonical JSON that was hashed. It is
	// stored so T2.02.b can re-hash the input the decision was made against
	// instead of trusting a recomputation from a live request.
	FingerprintInputJSON string
	Status               Status
	PolicyVersion        string
	RequestedAt          time.Time
	ExpiresAt            time.Time
	// DecidedBy and DecidedAt are set exactly when Status is not pending.
	DecidedBy string
	DecidedAt time.Time
	// Revision is the decision CAS token. A new approval is 1.
	Revision int64
}

// Consumption receipt errors and the receipt type live in store.go next to the
// function that writes them; the record itself is deliberately only an
// existence fact, so there is no stored struct beyond what
// RecordConsumptionTx returns.
//
// A Consumption is what the store hands back for a receipt it just wrote: the
// identity of the one authorization that was spent.
type Consumption struct {
	ApprovalID string
	ToolCallID string
	AttemptID  string
	ConsumedAt time.Time
}

// IsZero reports whether a Consumption names no receipt at all. It exists so a
// caller that treats "no receipt" and "the zero value" as the same thing says so
// explicitly rather than comparing columns by hand.
func (c Consumption) IsZero() bool {
	return c.ApprovalID == "" && c.ToolCallID == "" && c.AttemptID == "" && c.ConsumedAt.IsZero()
}

// trimSpace is the shared normalizer for the opaque string fields: an id with
// surrounding whitespace is the same id, and " tc_1" and "tc_1" must not become
// two identities for one thing.
func trimSpace(s string) string { return strings.TrimSpace(s) }
