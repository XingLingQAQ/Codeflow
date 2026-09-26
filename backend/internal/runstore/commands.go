// Command records: the idempotency ledger of every write the API accepts
// (T1.11.a).
//
// §27.3, §20.4 and §19.2's idempotency_records row describe one mechanism: a
// client generates an Idempotency-Key *before* it sends a request, and the
// server records "this (principal, project, operation, key) has been used for
// exactly this request" so that a retry after a lost response replays the first
// answer instead of executing a second time. This file is the storage half of
// that mechanism; the HTTP helper and the handler are T1.11.b, and the named
// end-to-end tests are T1.11.c.
//
// The rules this file exists to make true:
//
//   - The client owns the key, and generates it *before* the request is sent
//     (client key 先于请求生成, §20.4). The server never generates a command_id:
//     a key the server invented is useless to a client that lost the response,
//     because the client's own key is the only identifier it still holds. Every
//     function here is called with a key that already exists on the client.
//   - The key is claimed in the caller's transaction, before any side effect,
//     and the claim and the side effect commit or roll back together. A caller
//     that rolls back releases the key for free, which is correct: its side
//     effect never happened either, so a later retry with the same key must be
//     allowed to run. TestCommandClaimRollbackReleasesKey proves it.
//   - Exactly one caller owns a claim, so exactly one caller produces the side
//     effect. Everyone else gets the recorded command back and reads its state:
//     still in_flight → the API answers 202 accepted (§28 T1.11.b), terminal →
//     the API replays the stored status and response.
//   - A reused key with a different request hash is refused
//     (ErrCommandKeyReused → HTTP 409 idempotency_key_reused). It is never
//     answered with the first command's result: that would tell the client its
//     new request had been applied.
//   - An unknown external side effect is recorded, not guessed: in_flight →
//     reconciling with a reason, and the key stays taken, so nothing can start
//     the operation a second time (§20.4 "禁止启动第二次操作").
//   - A terminal command is retained for at least 30 days and then becomes a
//     tombstone that keeps the key and the hash. The key is therefore never
//     released by time: an old key cannot accidentally execute again.
//
// Concurrency: every state change is a single UPDATE ... WHERE the current state
// and the request hash, i.e. a compare-and-swap, and a CAS that matches no row
// re-reads to say which of "no such command", "different request" or "already
// completed" happened. Correctness does not depend on OpenStore's BEGIN
// IMMEDIATE: the primary key is the arbiter of a claim, and the CAS condition is
// the arbiter of every transition. Under the immediate mode the losers of a
// claim race simply wait for the winner's commit and then read its row.

package runstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Command record limits, matching the CHECK constraints of command_records
// (migration 006). They are enforced in Go as well so an unusable key is refused
// before any SQL runs, which is what keeps a rejected call from leaving a
// partial write — or a claimed key — behind.
const (
	// MaxCommandPrincipalLength is the byte limit of principal_id.
	MaxCommandPrincipalLength = 128
	// MaxCommandProjectLength is the byte limit of project_id.
	MaxCommandProjectLength = 128
	// MaxCommandOperationLength is the byte limit of operation.
	MaxCommandOperationLength = 64
	// MaxCommandIDLength is the byte limit of command_id.
	MaxCommandIDLength = 128
	// MaxCommandResponseBytes is the byte limit of a stored response body
	// (§27.3's command ledger keeps an answer, not a document).
	MaxCommandResponseBytes = 64 * 1024
	// MaxCommandLastErrorBytes is the byte limit of a reconciling reason. It is
	// truncated on a rune boundary, never mid-character, so the stored text is
	// always valid UTF-8.
	MaxCommandLastErrorBytes = 512
)

// DefaultCommandRetention is how long a completed command's recorded response is
// kept before it may be replaced by a tombstone. §27.3 requires at least 30 days
// for terminal commands; a caller may ask for longer, never for less: a shorter
// window would let a client that is still retrying an old command find its
// result gone.
const DefaultCommandRetention = 30 * 24 * time.Hour

// CommandState is the lifecycle state of one command record. The set is closed
// and mirrored by migration 006's CHECK.
type CommandState string

const (
	// CommandStateInFlight means the key is claimed and the owner is still
	// working. A duplicate request for the same key is answered "accepted"
	// (§20.4/§28 T1.11.b) — the caller must not execute it again.
	CommandStateInFlight CommandState = "in_flight"
	// CommandStateReconciling means the owner produced an external side effect
	// whose outcome is unknown (a timeout, a crashed peer). The key stays
	// claimed: starting the operation again is exactly what §20.4 forbids.
	CommandStateReconciling CommandState = "reconciling"
	// CommandStateSucceeded means the command completed and its response is
	// recorded.
	CommandStateSucceeded CommandState = "succeeded"
	// CommandStateFailed means the command was refused or failed, and the
	// refusal is recorded. It is terminal: retrying it with the same key
	// replays the recorded failure rather than executing again.
	CommandStateFailed CommandState = "failed"
	// CommandStateExpired is the tombstone a terminal record becomes after its
	// retention window. The key, the request hash and the outcome are kept; the
	// response body is not. The key is never released by expiry.
	CommandStateExpired CommandState = "expired"
)

// Valid reports whether s is one of the five states. It is the Go-side copy of
// migration 006's state CHECK.
func (s CommandState) Valid() bool {
	switch s {
	case CommandStateInFlight, CommandStateReconciling,
		CommandStateSucceeded, CommandStateFailed, CommandStateExpired:
		return true
	default:
		return false
	}
}

// Active reports whether the command is still unfinished, i.e. whether its owner
// may still produce a side effect and the API must answer "accepted".
func (s CommandState) Active() bool {
	return s == CommandStateInFlight || s == CommandStateReconciling
}

// Terminal reports whether the command reached a final outcome. An expired
// tombstone is terminal too: it has an outcome, it just no longer has a body.
func (s CommandState) Terminal() bool {
	switch s {
	case CommandStateSucceeded, CommandStateFailed, CommandStateExpired:
		return true
	default:
		return false
	}
}

// CommandKey is the idempotency scope of one command (§27.3):
// (principal_id, project_id, operation, command_id). Every field is required and
// is stored exactly as given — surrounding whitespace is refused rather than
// trimmed, because " rq_1" and "rq_1" are different byte strings and accepting
// both as one key would let one client retry claim two scopes (the same rule
// LegacySource applies to its identity).
type CommandKey struct {
	// PrincipalID is the authenticated principal the command is executed for.
	// It is part of the scope because two principals may choose the same
	// command id.
	PrincipalID string
	// ProjectID is the project the command acts on. It is not verified against
	// project_refs: a command may create the first runtime resource of a project
	// (migration 006's header explains why there is no foreign key).
	ProjectID string
	// Operation names what the command does, in lower case: "runs.create",
	// "runs.cancel", "approvals.decide". The vocabulary belongs to the API layer;
	// this package only requires it to be usable as a key.
	Operation string
	// CommandID is the client's own Idempotency-Key, generated before the
	// request was sent (§20.4). The server never generates it.
	CommandID string
}

// CommandRecord is one stored command: the scope it was claimed under, the
// request it was claimed for, where it got to, and the answer it produced.
type CommandRecord struct {
	Key CommandKey
	// RequestHash is the hash the key was claimed with. A later call with the
	// same key must present the same hash.
	RequestHash string
	State       CommandState
	// StatusCode and Response are the recorded answer. Both are nil while the
	// command is active and non-nil once it is terminal (except on a tombstone,
	// where only StatusCode survives).
	StatusCode *int
	Response   json.RawMessage
	// ResourceType/ResourceID name the resource the command created, when it
	// created one. Both are set or both are nil.
	ResourceType *string
	ResourceID   *string
	// LastError explains why an unfinished command is reconciling. It never
	// carries a credential: the caller supplies it and this package truncates it.
	LastError *string
	// CreatedAt/UpdatedAt are the row's own timestamps (server clock).
	CreatedAt time.Time
	UpdatedAt time.Time
	// CompletedAt is set exactly when the command became terminal.
	CompletedAt *time.Time
	// ExpiresAt is when the recorded response may be replaced by a tombstone.
	ExpiresAt *time.Time
}

// CommandOutcome is the recorded result of a completed command. It is only ever
// built by the owner of the claim, inside the same transaction as the side
// effect, so the recorded answer and the side effect cannot disagree.
type CommandOutcome struct {
	// State is the terminal state: succeeded or failed. The active states and
	// expired are not outcomes, and are refused before any SQL runs.
	State CommandState
	// StatusCode is the HTTP status the client was (or would have been) given.
	// It must be a real HTTP status (100..599).
	StatusCode int
	// Response is the saved response body: one JSON object, canonicalised and
	// stored as given up to MaxCommandResponseBytes. It must be the body the
	// caller is willing to replay to the same client later, so it must not
	// contain a credential — the same deny-list check a snapshot body goes
	// through (§27.1) is applied here, before any SQL runs.
	Response json.RawMessage
	// ResourceType/ResourceID name the resource the command created, when it
	// created one (T1.04's Run). Both empty means "no resource".
	ResourceType string
	ResourceID   string
}

// CommandRequestHash is the request identity of a command: the value that decides
// "the client re-sent the same request" from "the client reused the key for a
// different one".
//
// The hash is sha256 over method + "\n" + canonicalPath + "\n" + canonical JSON
// of body, returned as "sha256:" + 64 lowercase hex characters. The inputs are:
//
//   - method, upper-cased, so "post" and "POST" are one request;
//   - canonicalPath, the path the operation is addressed by, exactly as given —
//     this package cannot know an application's canonical form (which query
//     parameters are significant, whether a trailing slash matters), so the
//     caller must pass the canonical path and §27.3 requires it to be part of
//     the hash;
//   - body, which must already be de-identified by the caller: §27.3 says the
//     hash covers "去敏后的稳定请求结构及必要的秘密引用版本，不含 access
//     token". A credential must be replaced by a reference (a vault id, a secret
//     version) *before* this call, because this package cannot tell a bearer
//     token from any other string, and hashing a secret would still put it in
//     the caller's memory as the key material of a stored row. A body that names
//     a secret field and holds a non-empty string is refused outright
//     (ErrInvalidCommand), exactly as a snapshot body is.
//
// Canonicalisation is the same one snapshots and task inputs use, so two callers
// that serialise one object differently — different key order, different
// whitespace — get the same hash, while an array keeps its order (order is
// meaningful there). An empty body means "no body" and hashes as {}.
//
// A body that is not one JSON document (malformed, or several values) is
// ErrInvalidCommand. It is deliberately not hashed leniently: a body the server
// cannot canonicalise is a body whose "same request" test would be a guess.
func CommandRequestHash(method, canonicalPath string, body []byte) (string, error) {
	if strings.TrimSpace(method) == "" {
		return "", fmt.Errorf("%w: method is blank", ErrInvalidCommand)
	}
	if strings.TrimSpace(canonicalPath) == "" {
		return "", fmt.Errorf("%w: canonical path is blank", ErrInvalidCommand)
	}

	payload := body
	if len(strings.TrimSpace(string(payload))) == 0 {
		// An empty (or whitespace-only) body is the empty request, and the empty
		// request is the empty object: a client that sends no body and a client
		// that sends {} must hash identically, or one of them would be told its
		// retry reused the key for a different request.
		payload = []byte("{}")
	}
	canonical, _, err := canonicalJSON(payload)
	if err != nil {
		return "", fmt.Errorf("%w: request body: %v", ErrInvalidCommand, err)
	}
	if err := checkNoSecrets([]byte(canonical)); err != nil {
		return "", err
	}

	sum := sha256.Sum256([]byte(strings.ToUpper(method) + "\n" + canonicalPath + "\n" + canonical))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// ClaimCommandTx claims the idempotency key of one command inside the caller's
// transaction and reports whether this caller owns the claim.
//
// It never opens a transaction of its own. That is the point of the signature:
// the claim and the side effect it guards must commit or roll back together
// (§27.3 "事务先占 key，只有拥有者产生副作用"). A caller whose transaction rolls
// back leaves no claim, so the next attempt with the same key becomes the owner
// — correct, because the first attempt's side effect was rolled back too.
//
// The three outcomes:
//
//   - owner = true: the key was free, an in_flight record was inserted, and the
//     caller is the one caller that may produce the side effect. The returned
//     record is the row just written.
//   - owner = false, err = nil: the key is already claimed for the same request
//     hash. The returned record is the existing one, and its State tells the
//     caller what to answer — in_flight/reconciling → 202 accepted, succeeded/
//     failed → replay StatusCode/Response, expired → the tombstone's outcome
//     with no body (§28 T1.11.b). Nothing was written.
//   - err = ErrCommandKeyReused: the key is claimed for a *different* request
//     hash. The caller must answer 409 idempotency_key_reused and must not
//     execute anything.
//
// An expired tombstone behaves like any other existing record: same hash returns
// it with owner = false (the command is never executed again), different hash is
// ErrCommandKeyReused. Expiry is not a release.
//
// The one race this leaves is a conflicting row invisible to this transaction
// (another writer's uncommitted insert, which BEGIN IMMEDIATE makes unreachable
// in practice). It is reported as ErrCommandClaimRaced: roll back and retry, and
// the retry reads the committed row. It is a separate sentinel for the same
// reason ErrLegacyProjectionRaced is — the caller's action is the opposite of
// the one for ErrCommandKeyReused.
//
// Validation happens before any SQL runs: the four key fields must be non-blank,
// within their limits, and unchanged by trimming; operation must be lower-case
// [a-z0-9._-]; command_id must be printable ASCII with no whitespace; the
// request hash must be non-blank. A violation wraps ErrInvalidCommand and leaves
// the caller's transaction untouched.
func ClaimCommandTx(ctx context.Context, tx Tx, key CommandKey, requestHash string, now time.Time) (CommandRecord, bool, error) {
	if tx == nil {
		return CommandRecord{}, false, fmt.Errorf("%w: nil Tx", ErrInvalidCommand)
	}
	if err := validateCommandKey(key); err != nil {
		return CommandRecord{}, false, err
	}
	if strings.TrimSpace(requestHash) == "" {
		return CommandRecord{}, false, fmt.Errorf("%w: request hash is blank", ErrInvalidCommand)
	}

	row := tx.QueryRowContext(ctx, `
		INSERT INTO command_records (principal_id, project_id, operation, command_id,
		                             request_hash, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING `+commandColumns,
		key.PrincipalID, key.ProjectID, key.Operation, key.CommandID,
		requestHash, string(CommandStateInFlight), unixMilli(now), unixMilli(now),
	)
	record, err := scanCommand(row)
	if err == nil {
		return record, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		// A refusal here is not a lost race: the insert either matched the
		// primary key (handled below) or violated a constraint the caller's key
		// should have prevented (a CHECK), which mapConstraintError reports as a
		// database-level refusal rather than a typed command error.
		if !isCommandKeyConflict(err) {
			return CommandRecord{}, false, mapConstraintError("claim command "+key.CommandID, err)
		}
	}

	// The key is taken. Read the winner's row — inside this transaction, so it
	// is the same snapshot the insert saw — and let the request hash decide
	// whether this is a retry of the same command or a reused key.
	existing, err := GetCommand(ctx, tx, key)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// The conflicting row exists but is not visible to this transaction
			// (another writer's uncommitted insert, which the immediate lock
			// mode makes unreachable in practice). It is the same class of race
			// as ErrLegacyProjectionRaced: roll back and retry, and the retry
			// reads the committed row.
			return CommandRecord{}, false, fmt.Errorf("%w: %s", ErrCommandClaimRaced, key.CommandID)
		}
		return CommandRecord{}, false, fmt.Errorf("claim command %s: %w", key.CommandID, err)
	}
	if existing.RequestHash != requestHash {
		return CommandRecord{}, false, fmt.Errorf("%w: %s/%s/%s/%s was claimed with a different request",
			ErrCommandKeyReused, key.PrincipalID, key.ProjectID, key.Operation, key.CommandID)
	}
	return existing, false, nil
}

// CompleteCommandTx records the outcome of a command the caller owns, inside the
// caller's transaction, and stamps the retention deadline.
//
// Only an active command can be completed: the CAS requires the stored state to
// be in_flight or reconciling *and* the stored request hash to equal requestHash.
// A terminal record (succeeded, failed or an expired tombstone) is refused with
// ErrCommandAlreadyCompleted — the first outcome is the recorded one, and
// rewriting it would let a replay answer differently from the first response. A
// hash mismatch is ErrCommandKeyReused, the same condition ClaimCommandTx
// reports, because from the ledger's point of view it is one condition: this key
// belongs to a different request.
//
// retention is how long the response is kept before it may become a tombstone;
// retention <= 0 means DefaultCommandRetention (30 days). The deadline is
// computed from now, so a caller that completes a command late still keeps the
// full window.
//
// Validation, all before any SQL runs:
//
//   - outcome.State must be succeeded or failed;
//   - outcome.StatusCode must be a real HTTP status (100..599);
//   - outcome.Response must be a JSON object, at most MaxCommandResponseBytes
//     after canonicalisation, and must not name a secret field holding a
//     non-empty string;
//   - ResourceType and ResourceID must be given together or not at all.
//
// A violation wraps ErrInvalidCommand and leaves the record exactly as it was —
// in particular, a rejected outcome does not move the command out of in_flight,
// so the owner can fix its answer and complete again.
func CompleteCommandTx(ctx context.Context, tx Tx, key CommandKey, requestHash string, outcome CommandOutcome, retention time.Duration, now time.Time) (CommandRecord, error) {
	if tx == nil {
		return CommandRecord{}, fmt.Errorf("%w: nil Tx", ErrInvalidCommand)
	}
	if err := validateCommandKey(key); err != nil {
		return CommandRecord{}, err
	}
	if strings.TrimSpace(requestHash) == "" {
		return CommandRecord{}, fmt.Errorf("%w: request hash is blank", ErrInvalidCommand)
	}
	canonical, err := validateCommandOutcome(outcome)
	if err != nil {
		return CommandRecord{}, err
	}

	expiresAt := now.Add(commandRetention(retention))
	row := tx.QueryRowContext(ctx, `
		UPDATE command_records
		SET state         = ?,
		    status_code   = ?,
		    response_json = ?,
		    resource_type = ?,
		    resource_id   = ?,
		    completed_at  = ?,
		    expires_at    = ?,
		    updated_at    = ?
		WHERE principal_id = ? AND project_id = ? AND operation = ? AND command_id = ?
		  AND request_hash = ?
		  AND state IN ('in_flight', 'reconciling')
		RETURNING `+commandColumns,
		string(outcome.State), outcome.StatusCode, canonical,
		nullableString(optionalString(outcome.ResourceType)), nullableString(optionalString(outcome.ResourceID)),
		unixMilli(now), unixMilli(expiresAt), unixMilli(now),
		key.PrincipalID, key.ProjectID, key.Operation, key.CommandID, requestHash,
	)
	record, err := scanCommand(row)
	if err == nil {
		return record, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CommandRecord{}, mapConstraintError("complete command "+key.CommandID, err)
	}

	// No row matched. Re-read inside the same transaction to say which of the
	// three conditions it was, because the caller's next action differs: give
	// up (not found), stop reusing the key (different request), or accept that
	// the command is already finished.
	current, getErr := GetCommand(ctx, tx, key)
	if getErr != nil {
		if errors.Is(getErr, ErrNotFound) {
			return CommandRecord{}, fmt.Errorf("complete command %s: %w", key.CommandID, ErrNotFound)
		}
		return CommandRecord{}, fmt.Errorf("complete command %s: %w", key.CommandID, getErr)
	}
	if current.RequestHash != requestHash {
		return CommandRecord{}, fmt.Errorf("%w: %s/%s/%s/%s was claimed with a different request",
			ErrCommandKeyReused, key.PrincipalID, key.ProjectID, key.Operation, key.CommandID)
	}
	return CommandRecord{}, fmt.Errorf("%w: command %s is %s", ErrCommandAlreadyCompleted, key.CommandID, current.State)
}

// MarkCommandReconcilingTx records that the owner of a command produced an
// external side effect whose outcome is unknown, and moves the command from
// in_flight to reconciling.
//
// This is the one transition §20.4 requires by name: "有外部副作用而状态不明时
// 进入 reconciling，禁止启动第二次操作". The key therefore stays claimed. A
// duplicate request for the same key still finds an active record and is
// answered "accepted" (T1.11.b), so the operation is never started twice.
//
// reason explains the unknown state to whoever reconciles it. It is truncated to
// MaxCommandLastErrorBytes on a rune boundary, so the stored text is always valid
// UTF-8; the caller is responsible for keeping credentials out of it, and the
// truncation is not a redaction.
//
// Only an in_flight command can be marked reconciling (CAS). A record that is
// already reconciling is not an error: it is returned as it is, which makes the
// call idempotent for a caller that retries its own bookkeeping. A terminal
// record is ErrCommandAlreadyCompleted, and a hash mismatch ErrCommandKeyReused,
// exactly as in CompleteCommandTx.
func MarkCommandReconcilingTx(ctx context.Context, tx Tx, key CommandKey, requestHash, reason string, now time.Time) (CommandRecord, error) {
	if tx == nil {
		return CommandRecord{}, fmt.Errorf("%w: nil Tx", ErrInvalidCommand)
	}
	if err := validateCommandKey(key); err != nil {
		return CommandRecord{}, err
	}
	if strings.TrimSpace(requestHash) == "" {
		return CommandRecord{}, fmt.Errorf("%w: request hash is blank", ErrInvalidCommand)
	}
	if strings.TrimSpace(reason) == "" {
		return CommandRecord{}, fmt.Errorf("%w: reconciling reason is blank", ErrInvalidCommand)
	}

	row := tx.QueryRowContext(ctx, `
		UPDATE command_records
		SET state      = ?,
		    last_error = ?,
		    updated_at = ?
		WHERE principal_id = ? AND project_id = ? AND operation = ? AND command_id = ?
		  AND request_hash = ?
		  AND state = 'in_flight'
		RETURNING `+commandColumns,
		string(CommandStateReconciling), truncateRunes(reason, MaxCommandLastErrorBytes), unixMilli(now),
		key.PrincipalID, key.ProjectID, key.Operation, key.CommandID, requestHash,
	)
	record, err := scanCommand(row)
	if err == nil {
		return record, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CommandRecord{}, mapConstraintError("mark command reconciling "+key.CommandID, err)
	}

	current, getErr := GetCommand(ctx, tx, key)
	if getErr != nil {
		if errors.Is(getErr, ErrNotFound) {
			return CommandRecord{}, fmt.Errorf("mark command %s reconciling: %w", key.CommandID, ErrNotFound)
		}
		return CommandRecord{}, fmt.Errorf("mark command %s reconciling: %w", key.CommandID, getErr)
	}
	if current.RequestHash != requestHash {
		return CommandRecord{}, fmt.Errorf("%w: %s/%s/%s/%s was claimed with a different request",
			ErrCommandKeyReused, key.PrincipalID, key.ProjectID, key.Operation, key.CommandID)
	}
	if current.State == CommandStateReconciling {
		// Already where the caller wanted it. Returning the record instead of an
		// error keeps a retried bookkeeping call harmless.
		return current, nil
	}
	return CommandRecord{}, fmt.Errorf("%w: command %s is %s", ErrCommandAlreadyCompleted, key.CommandID, current.State)
}

// GetCommand reads one command record by its key. It is the read side of §20.4's
// reconciliation: a client whose response was lost calls
// GET /commands/:command_id and the handler answers from this record.
//
// It takes a Querier, so a caller can read through the pool (the HTTP path) or
// inside its own transaction (ClaimCommandTx's conflict check, which must see
// the caller's own uncommitted claim). A key with no record is ErrNotFound,
// which the API layer answers as 404 command_not_found (§20.4).
func GetCommand(ctx context.Context, q Querier, key CommandKey) (CommandRecord, error) {
	if q == nil {
		return CommandRecord{}, fmt.Errorf("%w: nil Querier", ErrInvalidCommand)
	}
	if err := validateCommandKey(key); err != nil {
		return CommandRecord{}, err
	}

	row := q.QueryRowContext(ctx, `
		SELECT `+commandColumns+`
		FROM command_records
		WHERE principal_id = ? AND project_id = ? AND operation = ? AND command_id = ?`,
		key.PrincipalID, key.ProjectID, key.Operation, key.CommandID,
	)
	record, err := scanCommand(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CommandRecord{}, fmt.Errorf("get command %s: %w", key.CommandID, ErrNotFound)
	}
	if err != nil {
		return CommandRecord{}, fmt.Errorf("get command %s: %w", key.CommandID, err)
	}
	return record, nil
}

// ExpireCommandsTx replaces the recorded response of up to limit due commands
// with a tombstone: state becomes expired, response_json is cleared, and the
// key, the request hash, the status code, the resource and the timestamps stay.
//
// §27.3: "活动命令不按 TTL 删除，终态默认保留至少 30 天；过期保留 tombstone，
// 使旧 key 不意外再执行". Only succeeded/failed rows whose expires_at has passed
// are touched, so an in_flight or reconciling command is never expired however
// long it has been running, and the tombstone keeps the key claimed forever: a
// client that re-sends the old key gets owner = false and an expired record, not
// a second execution.
//
// limit bounds one sweep (limit <= 0 means "no bound"), so a retention job
// (T12.02) can work in batches without holding the write lock for a long time.
// The count of rows actually tombstoned is returned.
func ExpireCommandsTx(ctx context.Context, tx Tx, now time.Time, limit int) (int, error) {
	if tx == nil {
		return 0, fmt.Errorf("%w: nil Tx", ErrInvalidCommand)
	}

	// The LIMIT is applied to the CAS's own WHERE clause rather than to a
	// separate SELECT: one statement means there is no window between choosing
	// the rows and tombstoning them, and the CAS condition is repeated so a row
	// completed concurrently (under a caller that is not using the immediate
	// lock mode) is not expired by mistake.
	query := `
		UPDATE command_records
		SET state         = 'expired',
		    response_json = NULL,
		    updated_at    = ?
		WHERE state IN ('succeeded', 'failed')
		  AND expires_at IS NOT NULL
		  AND expires_at <= ?`
	args := []any{unixMilli(now), unixMilli(now)}
	if limit > 0 {
		// The bound is applied through the full key tuple, not through
		// command_id alone: one client id may legitimately exist in several
		// scopes at once, and selecting by command_id would tombstone rows the
		// caller did not choose.
		query += ` AND (principal_id, project_id, operation, command_id) IN (
			SELECT principal_id, project_id, operation, command_id FROM command_records
			WHERE state IN ('succeeded', 'failed') AND expires_at IS NOT NULL AND expires_at <= ?
			ORDER BY expires_at
			LIMIT ?)`
		args = append(args, unixMilli(now), limit)
	}

	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, mapConstraintError("expire commands", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("expire commands: rows affected: %w", err)
	}
	return int(n), nil
}

// ---------------------------------------------------------------------------
// Internals
// ---------------------------------------------------------------------------

// commandColumns is the column list every command read uses, in the order
// scanCommand expects. Naming the columns rather than SELECT * keeps a future
// migration from silently reordering a scan.
const commandColumns = `principal_id, project_id, operation, command_id, request_hash, state,
	status_code, response_json, resource_type, resource_id, last_error,
	created_at, updated_at, completed_at, expires_at`

// scanCommand reads one command_records row.
func scanCommand(row scanner) (CommandRecord, error) {
	var (
		record                                            CommandRecord
		principalID, projectID, operation, commandID      string
		requestHash, state                                string
		statusCode                                        sql.NullInt64
		responseJSON, resourceType, resourceID, lastError sql.NullString
		createdAt, updatedAt                              int64
		completedAt, expiresAt                            sql.NullInt64
	)
	if err := row.Scan(
		&principalID, &projectID, &operation, &commandID, &requestHash, &state,
		&statusCode, &responseJSON, &resourceType, &resourceID, &lastError,
		&createdAt, &updatedAt, &completedAt, &expiresAt,
	); err != nil {
		return CommandRecord{}, err
	}

	record.Key = CommandKey{
		PrincipalID: principalID,
		ProjectID:   projectID,
		Operation:   operation,
		CommandID:   commandID,
	}
	record.RequestHash = requestHash
	record.State = CommandState(state)
	record.StatusCode = fromNullInt(statusCode)
	if responseJSON.Valid {
		record.Response = json.RawMessage(responseJSON.String)
	}
	record.ResourceType = fromNullString(resourceType)
	record.ResourceID = fromNullString(resourceID)
	record.LastError = fromNullString(lastError)
	record.CreatedAt = fromUnixMilli(createdAt)
	record.UpdatedAt = fromUnixMilli(updatedAt)
	record.CompletedAt = fromNullTime(completedAt)
	record.ExpiresAt = fromNullTime(expiresAt)
	return record, nil
}

// validateCommandKey enforces the key contract before any SQL runs.
//
// The rules, and why they are stricter than the schema's length CHECKs:
//
//   - every field non-blank and within its byte limit;
//   - no surrounding whitespace, because the key is stored verbatim and
//     " rq_1" is a different row from "rq_1" — accepting both while intending
//     one client retry would let a retry claim a second scope;
//   - operation is lower-case [a-z0-9._-]: it is a route name the API layer
//     maps, not free text, and a caller that spells it two ways would create two
//     idempotency scopes for one operation;
//   - command_id is printable ASCII with no whitespace: it travels in a URL path
//     (GET /commands/:command_id, §20.4) and in an HTTP header, so a control
//     character or a space would make it unaddressable or ambiguous.
func validateCommandKey(key CommandKey) error {
	if err := checkCommandField("principal_id", key.PrincipalID, MaxCommandPrincipalLength); err != nil {
		return err
	}
	if err := checkCommandField("project_id", key.ProjectID, MaxCommandProjectLength); err != nil {
		return err
	}
	if err := checkCommandField("operation", key.Operation, MaxCommandOperationLength); err != nil {
		return err
	}
	if err := checkCommandField("command_id", key.CommandID, MaxCommandIDLength); err != nil {
		return err
	}
	for i := 0; i < len(key.Operation); i++ {
		c := key.Operation[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '.', c == '_', c == '-':
		default:
			return fmt.Errorf("%w: operation %q contains %q; want lower-case [a-z0-9._-]",
				ErrInvalidCommand, key.Operation, string(c))
		}
	}
	for _, r := range key.CommandID {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) || unicode.IsSpace(r) {
			return fmt.Errorf("%w: command_id %q contains a non-printable or non-ASCII character",
				ErrInvalidCommand, key.CommandID)
		}
	}
	return nil
}

// checkCommandField applies the one rule every key field shares: non-blank, at
// most max bytes, and unchanged by trimming.
func checkCommandField(field, value string, max int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s is blank", ErrInvalidCommand, field)
	}
	if len(value) > max {
		return fmt.Errorf("%w: %s is %d bytes, limit is %d", ErrInvalidCommand, field, len(value), max)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%w: %s %q has surrounding whitespace; the key is stored verbatim",
			ErrInvalidCommand, field, value)
	}
	return nil
}

// validateCommandOutcome canonicalises the response body and returns it, or
// refuses the outcome before any SQL runs.
//
// The body must be one JSON object. An array or a scalar is refused because the
// recorded response is the envelope a handler replays (§27.3's error envelope is
// an object); canonicalisation makes "the same response" byte-identical however
// the handler serialised it; the size bound keeps one command from filling the
// ledger; and the secret scan is the same deny-list a snapshot body goes
// through, so a handler that accidentally echoed a credential is refused loudly
// rather than storing it for 30 days.
func validateCommandOutcome(outcome CommandOutcome) (string, error) {
	switch outcome.State {
	case CommandStateSucceeded, CommandStateFailed:
	default:
		return "", fmt.Errorf("%w: outcome state %q must be %s or %s",
			ErrInvalidCommand, outcome.State, CommandStateSucceeded, CommandStateFailed)
	}
	if outcome.StatusCode < 100 || outcome.StatusCode > 599 {
		return "", fmt.Errorf("%w: status code %d is not an HTTP status (100..599)",
			ErrInvalidCommand, outcome.StatusCode)
	}
	if (outcome.ResourceType == "") != (outcome.ResourceID == "") {
		return "", fmt.Errorf("%w: resource type %q and id %q must be given together",
			ErrInvalidCommand, outcome.ResourceType, outcome.ResourceID)
	}

	canonical, _, err := canonicalJSON(outcome.Response)
	if err != nil {
		return "", fmt.Errorf("%w: response: %v", ErrInvalidCommand, err)
	}
	if len(canonical) > MaxCommandResponseBytes {
		return "", fmt.Errorf("%w: response is %d bytes, limit is %d",
			ErrInvalidCommand, len(canonical), MaxCommandResponseBytes)
	}
	trimmed := strings.TrimSpace(canonical)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return "", fmt.Errorf("%w: response must be a JSON object", ErrInvalidCommand)
	}
	if err := checkNoSecrets([]byte(canonical)); err != nil {
		return "", err
	}
	return canonical, nil
}

// commandRetention is the effective retention window: the caller's, or the
// 30-day default when the caller asked for nothing. A caller can extend the
// window, never shorten it below the contract (§27.3 "终态默认保留至少 30 天").
func commandRetention(retention time.Duration) time.Duration {
	if retention <= 0 {
		return DefaultCommandRetention
	}
	return retention
}

// truncateRunes cuts s to at most max bytes without splitting a UTF-8 rune, so
// the stored text is always valid UTF-8. It never returns an invalid prefix: a
// multi-byte rune that would straddle the limit is dropped whole.
func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// optionalString turns "" into nil and anything else into a pointer, so an
// absent resource is stored as NULL rather than as an empty string (the CHECK
// in migration 006 keeps the pair consistent).
func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// fromNullInt is fromNullInt64 for a plain int, used by status_code.
func fromNullInt(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

// isCommandKeyConflict reports whether err is command_records' primary key
// refusal, i.e. the key is already claimed. It matches on the SQLite message
// rather than an extended result code so this package keeps its single-driver
// dependency (dbx) and does not import the driver, exactly as
// isLegacySourceConflict does; the message of a PK refusal is stable and names
// the table, which is also what a test can assert.
func isCommandKeyConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "command_records") &&
		(strings.Contains(msg, "UNIQUE constraint failed") ||
			strings.Contains(msg, "PRIMARY KEY must be unique"))
}
