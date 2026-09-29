// Package approval stores the approval facts §19.1 defines and the one-shot
// consumption receipts §27.2.5 requires (T2.02.a).
//
// An approval records what a human or a policy was asked to authorize — which
// subject, against which canonical fingerprint, under which policy version,
// until when — and a consumption receipt records the one time that
// authorization was spent on one tool call. The package is deliberately only
// the *facts*:
//
//   - CreateApprovalTx computes the fingerprint itself. A caller cannot supply
//     one, because an approval whose hash does not match what it authorizes is
//     exactly the bypass this package exists to prevent. The canonical input
//     that was hashed is stored beside the hash, so a later re-check (T2.02.b,
//     at consumption time) compares against a string that cannot have drifted
//     rather than re-deriving one from a live request.
//   - CreateApprovalTx never writes a decision. A new approval is always
//     pending with revision 1 and no decider; the decision path — with its
//     event/outbox write and its expected-revision CAS — is T2.02.b, and the
//     schema's triggers bound what that path may do: a decided row is final,
//     and a pending row's binding is frozen (parameters change means a new
//     approval, §27.2.3).
//   - RecordConsumptionTx is the mutex. It writes one immutable receipt row and
//     leans on (approval_id, tool_call_id) and (attempt_id, tool_call_id) being
//     unique, so two racing consumers cannot both believe they were granted. A
//     tool approval is spent only on its own subject call, only by its own
//     attempt and only before its deadline; the same conditions are a schema
//     trigger, so a hand-written receipt cannot spend a grant either.
//   - Nothing is deleted. Approval rows and receipts are history; a pending
//     approval that stops mattering becomes expired or invalidated.
//
// Dependency direction: this package imports runstore (as artifact does), never
// the other way round. It does not import execbackend; the tool-parameter hash
// it binds is computed by the caller with execbackend.ToolRequestFingerprint and
// handed over as ArgumentsHash.
package approval
