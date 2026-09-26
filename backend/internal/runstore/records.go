// Typed operations over the run domain (T1.01.b).
//
// Every write here takes a Tx and is a package-level function, so a caller can
// compose several of them into one atomic unit (T1.05's events/outbox and
// T1.11's command record join the same transaction the same way). Reads take a
// Querier so the identical call works inside a transaction and on the pool.
//
// Two invariants are enforced in Go before any SQL runs, because the database
// cannot express them usefully:
//
//   - InsertTask/InsertRun force revision = 1. The schema CHECK allows any
//     revision >= 1, but a freshly created row that starts at revision 2 would
//     silently break every expected_revision CAS that follows.
//   - InsertTask accepts only the two initial statuses (ready, queued). A task
//     inserted straight into running would bypass the claim protocol.
//
// Everything else (frozen input, terminal irreversibility, one active run per
// task, the snapshot identity check) is enforced by the schema and mapped to a
// typed error by mapConstraintError.

package runstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/run"
)

// ---------------------------------------------------------------------------
// Legacy reference snapshots
// ---------------------------------------------------------------------------

// UpsertProjectRef records the read-only snapshot of a legacy project.
//
// captured_at is written only on first insert: it records when the snapshot was
// taken, and a re-verification must not move it. On conflict the mutable
// columns are refreshed — source_revision, snapshot_hash, state and verified_at
// — because §27.1 requires that a project archived or rebound after capture is
// detected: the state column is the only thing that lets a later dispatch
// refuse to start a run for an archived project.
func UpsertProjectRef(ctx context.Context, tx Tx, ref run.ProjectRef) error {
	if ref.ProjectID == "" {
		return fmt.Errorf("%w: ProjectRef.ProjectID is empty", ErrInvalidRecord)
	}
	if !ref.State.Valid() {
		return fmt.Errorf("%w: ProjectRef.State %q is not valid", ErrInvalidRecord, ref.State)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO project_refs (project_id, source_revision, snapshot_hash, state, captured_at, verified_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET
			source_revision = excluded.source_revision,
			snapshot_hash   = excluded.snapshot_hash,
			state           = excluded.state,
			verified_at     = excluded.verified_at`,
		ref.ProjectID, nullableInt64(ref.SourceRevision), ref.SnapshotHash, string(ref.State),
		unixMilli(ref.CapturedAt), unixMilli(ref.VerifiedAt),
	)
	return mapConstraintError("upsert project ref "+ref.ProjectID, err)
}

// PutLegacyRef records the read-only snapshot of one legacy resource.
//
// Idempotency rule: re-putting the same (project, kind, resource) with the same
// snapshot_hash is a no-op, so a caller that re-verifies a parent on every
// dispatch does not churn the row. A different hash means the legacy resource
// changed after capture, and the snapshot is updated to the newly verified
// revision — the plan requires the runtime library to see the current legacy
// revision, and refusing the write would leave the store permanently stale.
// Callers that need "the parent moved" to be an error must compare the hash
// themselves before calling; this function only guarantees the row is never
// left holding a hash that was not really observed.
func PutLegacyRef(ctx context.Context, tx Tx, ref run.LegacyResourceRef) error {
	if ref.ProjectID == "" || ref.ResourceID == "" {
		return fmt.Errorf("%w: LegacyResourceRef needs ProjectID and ResourceID", ErrInvalidRecord)
	}
	if !ref.Kind.Valid() {
		return fmt.Errorf("%w: LegacyResourceRef.Kind %q is not valid", ErrInvalidRecord, ref.Kind)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO legacy_resource_refs (project_id, kind, resource_id, source_revision, snapshot_hash, captured_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id, kind, resource_id) DO UPDATE SET
			snapshot_hash   = excluded.snapshot_hash,
			source_revision = excluded.source_revision
		WHERE legacy_resource_refs.snapshot_hash IS NOT excluded.snapshot_hash`,
		ref.ProjectID, string(ref.Kind), ref.ResourceID,
		nullableInt64(ref.SourceRevision), ref.SnapshotHash, unixMilli(ref.CapturedAt),
	)
	return mapConstraintError("put legacy ref "+ref.ResourceID, err)
}

// ---------------------------------------------------------------------------
// Tasks
// ---------------------------------------------------------------------------

// InsertTask writes a new task and fills in the fields the store owns.
//
// The store computes InputHash from InputJSON rather than trusting the caller,
// so the hash a CAS later compares is always the hash of the stored bytes.
// InputJSON is canonicalised the same way a snapshot body is, which makes
// "the same input" hash identically however the caller serialised it.
//
// Revision is forced to 1 and Status must be one of the two initial states:
// ready (not yet offered to a worker) or queued (offered). Anything else is a
// caller bug, reported as ErrInvalidRecord before any SQL runs.
func InsertTask(ctx context.Context, tx Tx, task *run.Task) error {
	if task == nil {
		return fmt.Errorf("%w: nil Task", ErrInvalidRecord)
	}
	if task.ID == "" || task.ProjectID == "" {
		return fmt.Errorf("%w: Task needs ID and ProjectID", ErrInvalidRecord)
	}
	if !task.Kind.Valid() {
		return fmt.Errorf("%w: Task.Kind %q is not valid", ErrInvalidRecord, task.Kind)
	}
	switch task.Status {
	case run.TaskStatusReady, run.TaskStatusQueued:
	default:
		return fmt.Errorf("%w: a new task must start ready or queued, got %q", ErrInvalidRecord, task.Status)
	}

	canonical, hash, err := canonicalJSON([]byte(task.InputJSON))
	if err != nil {
		return fmt.Errorf("%w: Task.InputJSON: %v", ErrInvalidRecord, err)
	}

	task.InputJSON = canonical
	task.InputHash = hash
	task.Revision = 1
	// A new task is unclaimed; the lease columns are written as given so a
	// caller that deliberately seeds a claim (a test, or a migration replay)
	// still gets exactly what it asked for.

	_, err = tx.ExecContext(ctx, `
		INSERT INTO tasks (id, project_id, flow_id, stage_id, title, kind, status, priority,
		                   input_json, input_hash, lease_owner, lease_until, lease_epoch,
		                   revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		task.ID, task.ProjectID, nullableString(task.FlowID), nullableString(task.StageID),
		task.Title, string(task.Kind), string(task.Status), task.Priority,
		task.InputJSON, task.InputHash,
		nullableString(task.LeaseOwner), nullableTime(task.LeaseUntil), task.LeaseEpoch,
		unixMilli(task.CreatedAt), unixMilli(task.UpdatedAt),
	)
	return mapConstraintError("insert task "+task.ID, err)
}

// ---------------------------------------------------------------------------
// Input snapshots
// ---------------------------------------------------------------------------

// secretKeyNames are the object keys whose non-empty string value makes a
// snapshot body a secret-bearing payload. Matching is case-insensitive and
// ignores '_' and '-' so that apiKey, API_KEY and api-key are all caught.
//
// The list is deliberately a deny-list of the obvious credential names rather
// than a heuristic: the contract (plan §27.1) is that a secret value must never
// enter this table, and a false positive is a loud failure the caller can fix
// by storing a reference instead.
var secretKeyNames = []string{
	"password", "passwd", "secret", "token",
	"apikey", "privatekey", "authorization", "cookie",
}

// referenceKeySuffixes are the suffixes that mark a value as a reference rather
// than a secret: secret_ref, token_name, api_key_id and friends name where the
// secret lives, they do not carry it. They are checked first, so
// "secret_ref": "vault://..." is accepted while "secret": "..." is not.
var referenceKeySuffixes = []string{"_ref", "_refs", "_id", "_name"}

// InsertInputSnapshot canonicalises content, verifies it carries no secret
// value, stores it and returns the content hash the caller must pin on the run.
//
// Canonicalisation: the body is decoded with json.Decoder.UseNumber and
// re-encoded with sorted keys and no insignificant whitespace, so two callers
// that serialise the same object differently get the same hash — and a large
// integer keeps its exact digits instead of being rounded through a float64.
//
// The returned hash is "sha256:" + 64 lowercase hex characters. It is returned
// rather than computed by the caller so the stored body and the pinned hash can
// never disagree; InsertRun verifies them together.
func InsertInputSnapshot(ctx context.Context, tx Tx, projectID, id string, content json.RawMessage, now time.Time) (string, error) {
	if projectID == "" || id == "" {
		return "", fmt.Errorf("%w: InsertInputSnapshot needs projectID and id", ErrInvalidRecord)
	}
	canonical, hash, err := canonicalJSON(content)
	if err != nil {
		return "", fmt.Errorf("%w: input snapshot %s: %v", ErrInvalidRecord, id, err)
	}
	if err := checkNoSecrets([]byte(canonical)); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO input_snapshots (id, project_id, content_json, content_hash, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		id, projectID, canonical, hash, unixMilli(now),
	); err != nil {
		return "", mapConstraintError("insert input snapshot "+id, err)
	}
	return hash, nil
}

// canonicalJSON returns the canonical encoding of raw and its content hash.
//
// "Canonical" means: object keys sorted by byte order, no whitespace between
// tokens, numbers preserved exactly as written. Arrays keep their order (order
// is meaningful in an array); objects are order-insensitive, which is the whole
// point of sorting.
func canonicalJSON(raw []byte) (string, string, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()

	var value any
	if err := dec.Decode(&value); err != nil {
		return "", "", fmt.Errorf("decode: %w", err)
	}
	// A second value (or trailing garbage) means the caller handed us something
	// that is not one JSON document; hashing a prefix of it would be a silent
	// data loss.
	if err := requireJSONEnd(dec); err != nil {
		return "", "", err
	}

	var sb strings.Builder
	if err := writeCanonical(&sb, value); err != nil {
		return "", "", err
	}
	out := sb.String()
	sum := sha256.Sum256([]byte(out))
	return out, "sha256:" + hex.EncodeToString(sum[:]), nil
}

// requireJSONEnd reports an error unless dec has consumed its whole input apart
// from whitespace. dec.More() is not enough: it returns false when the next byte
// is a closing '}' or ']', so `{"a":1}}` would pass and the stray delimiter
// would be dropped without a trace.
func requireJSONEnd(dec *json.Decoder) error {
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing content after the JSON value")
	}
	return nil
}

// writeCanonical writes value in canonical form. It handles exactly the JSON
// value space (nil, bool, json.Number, string, []any, map[string]any), which is
// what json.Decoder produces with UseNumber.
func writeCanonical(sb *strings.Builder, value any) error {
	switch v := value.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		if v {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case json.Number:
		// Written verbatim: no float64 round-trip, so a 20-digit id keeps every
		// digit and 1.0 does not become 1.
		sb.WriteString(v.String())
	case string:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("encode string: %w", err)
		}
		sb.Write(encoded)
	case []any:
		sb.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				sb.WriteByte(',')
			}
			if err := writeCanonical(sb, item); err != nil {
				return err
			}
		}
		sb.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			encoded, err := json.Marshal(k)
			if err != nil {
				return fmt.Errorf("encode key: %w", err)
			}
			sb.Write(encoded)
			sb.WriteByte(':')
			if err := writeCanonical(sb, v[k]); err != nil {
				return err
			}
		}
		sb.WriteByte('}')
	default:
		return fmt.Errorf("unsupported JSON value type %T", value)
	}
	return nil
}

// checkNoSecrets walks the decoded body and fails on the first object key that
// names a credential and holds a non-empty string. The error carries the key
// path only — never the value — so a rejected snapshot cannot leak the secret
// into a log or an HTTP response.
func checkNoSecrets(raw []byte) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()

	var value any
	if err := dec.Decode(&value); err != nil {
		return fmt.Errorf("%w: decode for secret scan: %v", ErrInvalidRecord, err)
	}
	return scanForSecrets(value, "")
}

func scanForSecrets(value any, path string) error {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys) // deterministic: the first reported path is stable
		for _, k := range keys {
			child := k
			if path != "" {
				child = path + "." + k
			}
			if isSecretKey(k) {
				// Only a non-empty string counts: "password": "" carries no
				// value, and "password": null or a nested object is not a
				// literal credential.
				if s, ok := v[k].(string); ok && s != "" {
					return fmt.Errorf("%w: key %q", ErrSecretInSnapshot, child)
				}
			}
			if err := scanForSecrets(v[k], child); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range v {
			if err := scanForSecrets(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// isSecretKey reports whether key names a credential rather than a reference to
// one. A key ending in _ref/_refs/_id/_name is a reference and is allowed even
// when it also contains a secret word (secret_ref, token_name, api_key_id).
func isSecretKey(key string) bool {
	normalized := strings.ToLower(key)
	for _, suffix := range referenceKeySuffixes {
		if strings.HasSuffix(normalized, suffix) {
			return false
		}
	}
	compact := strings.NewReplacer("_", "", "-", "", " ", "").Replace(normalized)
	for _, name := range secretKeyNames {
		if compact == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Runs
// ---------------------------------------------------------------------------

// InsertRun writes a new run.
//
// Status must be queued and Revision is forced to 1: a run is created queued and
// the state machine (T1.02) owns every transition after that. The store does
// not validate whether a transition is legal — that is T1.02's table — but it
// does refuse to create a row that is already past the start.
//
// The input snapshot identity is not verified here; migration 002's
// trg_runs_input_snapshot_must_match does it inside the same statement, so
// there is no window in which a run could point at a snapshot that does not
// exist, belongs to another project, or has a different hash. That refusal is
// returned as ErrInputSnapshotMismatch.
func InsertRun(ctx context.Context, tx Tx, r *run.Run) error {
	if r == nil {
		return fmt.Errorf("%w: nil Run", ErrInvalidRecord)
	}
	if r.ID == "" || r.TaskID == "" || r.ProjectID == "" {
		return fmt.Errorf("%w: Run needs ID, TaskID and ProjectID", ErrInvalidRecord)
	}
	if r.Status != run.RunStatusQueued {
		return fmt.Errorf("%w: a new run must be queued, got %q", ErrInvalidRecord, r.Status)
	}
	if r.BindingRevision < 1 {
		return fmt.Errorf("%w: Run.BindingRevision %d must be >= 1", ErrInvalidRecord, r.BindingRevision)
	}

	budgetJSON, err := encodeBudget(r.Budget)
	if err != nil {
		return fmt.Errorf("%w: Run.Budget: %v", ErrInvalidRecord, err)
	}

	r.Revision = 1
	_, err = tx.ExecContext(ctx, `
		INSERT INTO runs (id, task_id, project_id, command_id, binding_id, binding_revision,
		                  base_manifest_hash, base_commit, agent_revision_id, input_snapshot_id,
		                  input_snapshot_hash, budget_json, status, revision, retry_of_run_id,
		                  created_at, updated_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)`,
		r.ID, r.TaskID, r.ProjectID, nullableString(r.CommandID),
		r.BindingID, r.BindingRevision, r.BaseManifestHash, nullableString(r.BaseCommit),
		r.AgentRevisionID, r.InputSnapshotID, r.InputSnapshotHash, budgetJSON,
		string(r.Status), nullableString(r.RetryOfRunID),
		unixMilli(r.CreatedAt), unixMilli(r.UpdatedAt), nullableTime(r.FinishedAt),
	)
	return mapConstraintError("insert run "+r.ID, err)
}

// InsertAttempt writes a new backend-process attempt for a run.
//
// The store does not police the attempt's status: "one active attempt per run"
// is a partial unique index in the schema, and which statuses are active is
// run.AttemptStatus.IsActive's business, not a second list kept here.
func InsertAttempt(ctx context.Context, tx Tx, a *run.Attempt) error {
	if a == nil {
		return fmt.Errorf("%w: nil Attempt", ErrInvalidRecord)
	}
	if a.ID == "" || a.RunID == "" {
		return fmt.Errorf("%w: Attempt needs ID and RunID", ErrInvalidRecord)
	}
	if a.AttemptNo < 1 {
		return fmt.Errorf("%w: Attempt.AttemptNo %d must be >= 1", ErrInvalidRecord, a.AttemptNo)
	}
	if !a.Status.Valid() {
		return fmt.Errorf("%w: Attempt.Status %q is not valid", ErrInvalidRecord, a.Status)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO attempts (id, run_id, attempt_no, backend, backend_version, status,
		                      owner_instance, pid, process_start_id, started_at, finished_at,
		                      exit_code, exit_reason, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.RunID, a.AttemptNo, a.Backend, nullableString(a.BackendVersion),
		string(a.Status), nullableString(a.OwnerInstance), nullableInt64(a.PID),
		nullableString(a.ProcessStartID), nullableTime(a.StartedAt), nullableTime(a.FinishedAt),
		nullableInt64(a.ExitCode), nullableString(a.ExitReason), unixMilli(a.CreatedAt),
	)
	return mapConstraintError("insert attempt "+a.ID, err)
}

// encodeBudget renders the budget as the JSON the schema stores. An empty
// budget encodes as '{}', matching the column default, so a run with no limits
// and a run whose limits were all omitted are the same row.
func encodeBudget(b run.Budget) (string, error) {
	encoded, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// decodeBudget reverses encodeBudget. The pointers stay nil when a limit was
// not set: nil means "unknown", which must not be read as zero.
func decodeBudget(raw string) (run.Budget, error) {
	var b run.Budget
	if raw == "" {
		return b, nil
	}
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		return run.Budget{}, fmt.Errorf("decode budget %q: %w", raw, err)
	}
	return b, nil
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// taskColumns is the column list shared by every task read, in the order
// scanTask expects. Naming it once means a read cannot drift from its scanner.
const taskColumns = `id, project_id, flow_id, stage_id, title, kind, status, priority,
	       input_json, input_hash, lease_owner, lease_until, lease_epoch,
	       revision, created_at, updated_at`

// GetTask reads one task by id. A missing row is ErrNotFound.
func GetTask(ctx context.Context, q Querier, id string) (run.Task, error) {
	row := q.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = ?`, id)
	task, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return run.Task{}, fmt.Errorf("get task %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return run.Task{}, fmt.Errorf("get task %s: %w", id, err)
	}
	return task, nil
}

// scanner is the subset of *sql.Row and *sql.Rows that the scan helpers need,
// so one helper serves both a single-row read and a list read.
type scanner interface {
	Scan(dest ...any) error
}

func scanTask(row scanner) (run.Task, error) {
	var (
		task       run.Task
		flowID     sql.NullString
		stageID    sql.NullString
		kind       string
		status     string
		leaseOwner sql.NullString
		leaseUntil sql.NullInt64
		createdAt  int64
		updatedAt  int64
	)
	if err := row.Scan(
		&task.ID, &task.ProjectID, &flowID, &stageID, &task.Title, &kind, &status, &task.Priority,
		&task.InputJSON, &task.InputHash, &leaseOwner, &leaseUntil, &task.LeaseEpoch,
		&task.Revision, &createdAt, &updatedAt,
	); err != nil {
		return run.Task{}, err
	}
	task.FlowID = fromNullString(flowID)
	task.StageID = fromNullString(stageID)
	task.Kind = run.TaskKind(kind)
	task.Status = run.TaskStatus(status)
	task.LeaseOwner = fromNullString(leaseOwner)
	task.LeaseUntil = fromNullTime(leaseUntil)
	task.CreatedAt = fromUnixMilli(createdAt)
	task.UpdatedAt = fromUnixMilli(updatedAt)
	return task, nil
}

// runColumns is the column list shared by every run read.
const runColumns = `id, task_id, project_id, command_id, binding_id, binding_revision,
	       base_manifest_hash, base_commit, agent_revision_id, input_snapshot_id,
	       input_snapshot_hash, budget_json, status, revision, retry_of_run_id,
	       created_at, updated_at, finished_at`

// GetRun reads one run by id. A missing row is ErrNotFound.
func GetRun(ctx context.Context, q Querier, id string) (run.Run, error) {
	row := q.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id = ?`, id)
	r, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return run.Run{}, fmt.Errorf("get run %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return run.Run{}, fmt.Errorf("get run %s: %w", id, err)
	}
	return r, nil
}

func scanRun(row scanner) (run.Run, error) {
	var (
		r               run.Run
		commandID       sql.NullString
		baseCommit      sql.NullString
		budgetJSON      string
		status          string
		retryOfRunID    sql.NullString
		createdAt       int64
		updatedAt       int64
		finishedAt      sql.NullInt64
		bindingRevision int64
	)
	if err := row.Scan(
		&r.ID, &r.TaskID, &r.ProjectID, &commandID, &r.BindingID, &bindingRevision,
		&r.BaseManifestHash, &baseCommit, &r.AgentRevisionID, &r.InputSnapshotID,
		&r.InputSnapshotHash, &budgetJSON, &status, &r.Revision, &retryOfRunID,
		&createdAt, &updatedAt, &finishedAt,
	); err != nil {
		return run.Run{}, err
	}
	r.CommandID = fromNullString(commandID)
	r.BindingRevision = bindingRevision
	r.BaseCommit = fromNullString(baseCommit)
	budget, err := decodeBudget(budgetJSON)
	if err != nil {
		return run.Run{}, err
	}
	r.Budget = budget
	r.Status = run.RunStatus(status)
	r.RetryOfRunID = fromNullString(retryOfRunID)
	r.CreatedAt = fromUnixMilli(createdAt)
	r.UpdatedAt = fromUnixMilli(updatedAt)
	r.FinishedAt = fromNullTime(finishedAt)
	return r, nil
}

// ListRunsByTask returns every run of a task, oldest first. The order is
// created_at then id so that a caller paginating over the result sees a stable
// sequence even when two runs share a timestamp.
func ListRunsByTask(ctx context.Context, q Querier, taskID string) ([]run.Run, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+runColumns+` FROM runs WHERE task_id = ? ORDER BY created_at, id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list runs for task %s: %w", taskID, err)
	}
	defer rows.Close()

	var out []run.Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("list runs for task %s: %w", taskID, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list runs for task %s: %w", taskID, err)
	}
	return out, nil
}

// attemptColumns is the column list shared by every attempt read.
const attemptColumns = `id, run_id, attempt_no, backend, backend_version, status,
	       owner_instance, pid, process_start_id, started_at, finished_at,
	       exit_code, exit_reason, created_at`

// GetAttempt reads one attempt by id. A missing row is ErrNotFound.
func GetAttempt(ctx context.Context, q Querier, id string) (run.Attempt, error) {
	row := q.QueryRowContext(ctx, `SELECT `+attemptColumns+` FROM attempts WHERE id = ?`, id)
	a, err := scanAttempt(row)
	if errors.Is(err, sql.ErrNoRows) {
		return run.Attempt{}, fmt.Errorf("get attempt %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return run.Attempt{}, fmt.Errorf("get attempt %s: %w", id, err)
	}
	return a, nil
}

func scanAttempt(row scanner) (run.Attempt, error) {
	var (
		a              run.Attempt
		backendVersion sql.NullString
		status         string
		ownerInstance  sql.NullString
		pid            sql.NullInt64
		processStartID sql.NullString
		startedAt      sql.NullInt64
		finishedAt     sql.NullInt64
		exitCode       sql.NullInt64
		exitReason     sql.NullString
		createdAt      int64
	)
	if err := row.Scan(
		&a.ID, &a.RunID, &a.AttemptNo, &a.Backend, &backendVersion, &status,
		&ownerInstance, &pid, &processStartID, &startedAt, &finishedAt,
		&exitCode, &exitReason, &createdAt,
	); err != nil {
		return run.Attempt{}, err
	}
	a.BackendVersion = fromNullString(backendVersion)
	a.Status = run.AttemptStatus(status)
	a.OwnerInstance = fromNullString(ownerInstance)
	a.PID = fromNullInt64(pid)
	a.ProcessStartID = fromNullString(processStartID)
	a.StartedAt = fromNullTime(startedAt)
	a.FinishedAt = fromNullTime(finishedAt)
	a.ExitCode = fromNullInt64(exitCode)
	a.ExitReason = fromNullString(exitReason)
	a.CreatedAt = fromUnixMilli(createdAt)
	return a, nil
}

// ListAttemptsByRun returns the attempts of a run in attempt_no order, which is
// the order they were started and the order an operator reads them in.
func ListAttemptsByRun(ctx context.Context, q Querier, runID string) ([]run.Attempt, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+attemptColumns+` FROM attempts WHERE run_id = ? ORDER BY attempt_no`, runID)
	if err != nil {
		return nil, fmt.Errorf("list attempts for run %s: %w", runID, err)
	}
	defer rows.Close()

	var out []run.Attempt
	for rows.Next() {
		a, err := scanAttempt(rows)
		if err != nil {
			return nil, fmt.Errorf("list attempts for run %s: %w", runID, err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list attempts for run %s: %w", runID, err)
	}
	return out, nil
}

// InputSnapshot is one stored frozen input body.
type InputSnapshot struct {
	ID          string
	ProjectID   string
	ContentJSON string
	ContentHash string
	CreatedAt   time.Time
}

// GetInputSnapshot reads one snapshot by id. A missing row is ErrNotFound.
//
// The content hash is returned alongside the body so a caller can re-verify
// that what it is about to hand to a backend is still what the run pinned,
// without re-canonicalising it.
func GetInputSnapshot(ctx context.Context, q Querier, id string) (InputSnapshot, error) {
	var (
		snap      InputSnapshot
		createdAt int64
	)
	err := q.QueryRowContext(ctx,
		`SELECT id, project_id, content_json, content_hash, created_at FROM input_snapshots WHERE id = ?`, id,
	).Scan(&snap.ID, &snap.ProjectID, &snap.ContentJSON, &snap.ContentHash, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return InputSnapshot{}, fmt.Errorf("get input snapshot %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return InputSnapshot{}, fmt.Errorf("get input snapshot %s: %w", id, err)
	}
	snap.CreatedAt = fromUnixMilli(createdAt)
	return snap, nil
}

// ---------------------------------------------------------------------------
// Compare-and-swap
// ---------------------------------------------------------------------------

// UpdateRunStatusCAS moves a run to next only if it is still at
// expectedRevision.
//
// It is one statement with RETURNING, not a read followed by a write. That is
// deliberate and is the fix §26.17 recorded: a read-then-write pair needs the
// write lock held across both steps, and doing it in one statement removes the
// window entirely. Combined with OpenStore's BEGIN IMMEDIATE, a concurrent CAS
// either loses on the revision predicate or waits for the lock — it never fails
// with SQLITE_BUSY_SNAPSHOT.
//
// The statement sets revision = revision + 1 (the schema trigger refuses any
// other change to revision) and stamps finished_at only when next is terminal,
// leaving an already-set finished_at alone otherwise.
//
// This function does not check whether the transition is legal: T1.02 owns the
// transition table. It does guarantee that a terminal run cannot leave its
// terminal state, because the schema trigger refuses that and the refusal is
// mapped to ErrTerminalImmutable.
//
// When no row matches, the store re-reads to distinguish "no such run"
// (ErrNotFound) from "someone moved it first" (*RevisionConflictError). That
// re-read happens after the failed UPDATE inside the same transaction, so it
// observes the same snapshot the UPDATE did.
func UpdateRunStatusCAS(ctx context.Context, tx Tx, runID string, expectedRevision int64, next run.RunStatus, now time.Time) (run.Run, error) {
	if !next.Valid() {
		return run.Run{}, fmt.Errorf("%w: RunStatus %q is not valid", ErrInvalidRecord, next)
	}
	if expectedRevision < 1 {
		return run.Run{}, fmt.Errorf("%w: expected revision %d must be >= 1", ErrInvalidRecord, expectedRevision)
	}

	// finished_at: set to now on the transition into a terminal state, kept
	// otherwise. CASE on the new status rather than on the old one, so the
	// stamp lands in the same statement that makes the run terminal.
	row := tx.QueryRowContext(ctx, `
		UPDATE runs
		SET status      = ?,
		    revision    = revision + 1,
		    updated_at  = ?,
		    finished_at = CASE WHEN ? THEN COALESCE(finished_at, ?) ELSE finished_at END
		WHERE id = ? AND revision = ?
		RETURNING `+runColumns,
		string(next), unixMilli(now), next.IsTerminal(), unixMilli(now), runID, expectedRevision,
	)
	updated, err := scanRun(row)
	if err == nil {
		return updated, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return run.Run{}, mapConstraintError("cas run "+runID, err)
	}

	// No row matched: the id does not exist, or the revision moved on.
	current, getErr := GetRun(ctx, tx, runID)
	if errors.Is(getErr, ErrNotFound) {
		return run.Run{}, fmt.Errorf("cas run %s: %w", runID, ErrNotFound)
	}
	if getErr != nil {
		return run.Run{}, fmt.Errorf("cas run %s: %w", runID, getErr)
	}
	return run.Run{}, &RevisionConflictError{
		RunID:         runID,
		Expected:      expectedRevision,
		Current:       current.Revision,
		CurrentStatus: current.Status,
	}
}

// UpdateTaskStatusCAS is UpdateRunStatusCAS for tasks. It carries the same
// guarantees: one statement, revision + 1, terminal states frozen by the
// schema. Task status has no finished_at column, so only updated_at moves.
//
// failed is not terminal for a task (an explicit retry CASes it back to
// queued), which is the schema's rule and not restated here.
func UpdateTaskStatusCAS(ctx context.Context, tx Tx, taskID string, expectedRevision int64, next run.TaskStatus, now time.Time) (run.Task, error) {
	if !next.Valid() {
		return run.Task{}, fmt.Errorf("%w: TaskStatus %q is not valid", ErrInvalidRecord, next)
	}
	if expectedRevision < 1 {
		return run.Task{}, fmt.Errorf("%w: expected revision %d must be >= 1", ErrInvalidRecord, expectedRevision)
	}

	row := tx.QueryRowContext(ctx, `
		UPDATE tasks
		SET status     = ?,
		    revision   = revision + 1,
		    updated_at = ?
		WHERE id = ? AND revision = ?
		RETURNING `+taskColumns,
		string(next), unixMilli(now), taskID, expectedRevision,
	)
	updated, err := scanTask(row)
	if err == nil {
		return updated, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return run.Task{}, mapConstraintError("cas task "+taskID, err)
	}

	current, getErr := GetTask(ctx, tx, taskID)
	if errors.Is(getErr, ErrNotFound) {
		return run.Task{}, fmt.Errorf("cas task %s: %w", taskID, ErrNotFound)
	}
	if getErr != nil {
		return run.Task{}, fmt.Errorf("cas task %s: %w", taskID, getErr)
	}
	return run.Task{}, &TaskRevisionConflictError{
		TaskID:        taskID,
		Expected:      expectedRevision,
		Current:       current.Revision,
		CurrentStatus: current.Status,
	}
}

// ---------------------------------------------------------------------------
// Time and NULL conversion
// ---------------------------------------------------------------------------

// unixMilli is the only way a time.Time reaches the database. Every *_at column
// is Unix milliseconds UTC, so a caller's location cannot change what is
// stored.
func unixMilli(t time.Time) int64 { return t.UTC().UnixMilli() }

// fromUnixMilli is the only way a stored instant becomes a time.Time. The
// result is UTC; a caller that needs a local rendering converts at the edge.
func fromUnixMilli(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func nullableString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return unixMilli(*t)
}

func fromNullString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func fromNullInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func fromNullTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromUnixMilli(v.Int64)
	return &t
}
