package runstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/codeflow/backend/internal/dbx"
	"github.com/codeflow/backend/internal/run"
)

// These tests cover commands.go and migration 006: the idempotency ledger of
// client-issued commands (T1.11.a). The properties they exist to prove:
//
//   - a client key is claimed exactly once, by exactly one caller, and only the
//     owner produces the side effect; everyone else gets the recorded command
//     back and reads its state (§27.3 "事务先占 key，只有拥有者产生副作用");
//   - the claim and the side effect are one transaction: a rollback releases the
//     key for free, because the side effect was rolled back too;
//   - the same key with a different request hash is refused, never answered with
//     the first command's result;
//   - a completed command cannot be completed twice, and its recorded answer is
//     canonical JSON that never contains a credential;
//   - an unknown external side effect is recorded as reconciling and the key
//     stays claimed, so the operation cannot be started a second time;
//   - an expired record is a tombstone: the key and hash stay, the response is
//     gone, the command is never executed again, and no statement can revive it;
//   - the request hash is order- and whitespace-insensitive on objects, keeps
//     array order, normalizes the method's case, treats an empty body as {}, and
//     refuses a body it cannot canonicalise;
//   - an unusable key is refused before any SQL runs;
//   - migration 006's CHECKs refuse a row whose state and fields disagree, and
//     reopening the database re-applies nothing.

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// commandTestPrincipal is the principal the command fixtures act as. It is a
// constant so a test that accidentally compared two different principals'
// records would fail rather than pass for the wrong reason.
const commandTestPrincipal = "principal-1"

// commandFixture is a store with one project_ref (so the side-effect half of a
// claim can insert a task) plus the path so a test can open a second handle on
// the same file.
type commandFixture struct {
	t     *testing.T
	store *Store
	path  string
}

func newCommandFixture(t *testing.T) *commandFixture {
	t.Helper()
	store, path := newTestStore(t)
	seedTask(t, store, "t-1", "p-1")
	return &commandFixture{t: t, store: store, path: path}
}

// openSecondStore opens another handle on the same file. The concurrency tests
// use it so the race is between two real connections, not between two goroutines
// sharing one pool entry.
func openSecondStore(t *testing.T, path string) *Store {
	t.Helper()
	store, res, err := OpenStore(context.Background(), path)
	if err != nil {
		t.Fatalf("second OpenStore(%s): %v", path, err)
	}
	if len(res.Applied) != 0 {
		t.Fatalf("second OpenStore applied %d migrations, want 0 (the file is already at version %d)",
			len(res.Applied), res.ToVersion)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// commandKey builds the scope of one command with the fixture's principal and
// project, so a test only spells out what it is varying.
func commandKey(operation, commandID string) CommandKey {
	return CommandKey{
		PrincipalID: commandTestPrincipal,
		ProjectID:   "p-1",
		Operation:   operation,
		CommandID:   commandID,
	}
}

// commandHash is the request hash of one body for the fixture's canonical
// operation path.
func commandHash(t *testing.T, body string) string {
	t.Helper()
	hash, err := CommandRequestHash("POST", "/api/v1/projects/p-1/runs", []byte(body))
	if err != nil {
		t.Fatalf("CommandRequestHash(%s): %v", body, err)
	}
	return hash
}

// commandTime is a fixed instant inside the fixture's timeline. Every test uses
// explicit times so a stored deadline is comparable without tolerance.
func commandTime(offset time.Duration) time.Time {
	return time.UnixMilli(1700000000000).Add(offset)
}

// claim runs one ClaimCommandTx in its own transaction and returns what it said.
func (f *commandFixture) claim(key CommandKey, hash string, now time.Time) (CommandRecord, bool, error) {
	f.t.Helper()
	return claimWith(context.Background(), f.store, key, hash, now)
}

// claimWith is claim against an arbitrary handle, so the concurrency tests can
// use their own store without duplicating the transaction boilerplate.
func claimWith(ctx context.Context, store *Store, key CommandKey, hash string, now time.Time) (CommandRecord, bool, error) {
	var (
		record CommandRecord
		owner  bool
	)
	err := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		record, owner, err = ClaimCommandTx(ctx, tx, key, hash, now)
		return err
	})
	return record, owner, err
}

// complete runs one CompleteCommandTx in its own transaction.
func (f *commandFixture) complete(key CommandKey, hash string, outcome CommandOutcome, retention time.Duration, now time.Time) (CommandRecord, error) {
	f.t.Helper()
	var record CommandRecord
	err := f.store.WithTx(context.Background(), func(ctx context.Context, tx Tx) error {
		var err error
		record, err = CompleteCommandTx(ctx, tx, key, hash, outcome, retention, now)
		return err
	})
	return record, err
}

// commandRecordCount counts command_records rows, the evidence that a refused
// call wrote nothing.
func commandRecordCount(t *testing.T, q Querier) int {
	t.Helper()
	return eventRowCount(t, q, "command_records")
}

// commandStored reads the raw row, so a test can assert on the stored NULLs and
// the exact stored text rather than on the decoded record.
func commandStored(t *testing.T, q Querier, key CommandKey) commandRow {
	t.Helper()
	var row commandRow
	err := q.QueryRowContext(context.Background(), `
		SELECT state, status_code, response_json, resource_type, resource_id, last_error,
		       completed_at, expires_at, request_hash
		FROM command_records
		WHERE principal_id = ? AND project_id = ? AND operation = ? AND command_id = ?`,
		key.PrincipalID, key.ProjectID, key.Operation, key.CommandID,
	).Scan(&row.State, &row.StatusCode, &row.Response, &row.ResourceType, &row.ResourceID,
		&row.LastError, &row.CompletedAt, &row.ExpiresAt, &row.RequestHash)
	if err != nil {
		t.Fatalf("read command record %+v: %v", key, err)
	}
	return row
}

// commandRow is the raw shape of a command_records row: NULLs and stored text as
// the database holds them, so a test can assert on the storage rather than on
// what the store decoded.
type commandRow struct {
	State        string
	StatusCode   sql.NullInt64
	Response     sql.NullString
	ResourceType sql.NullString
	ResourceID   sql.NullString
	LastError    sql.NullString
	CompletedAt  sql.NullInt64
	ExpiresAt    sql.NullInt64
	RequestHash  string
}

// ---------------------------------------------------------------------------
// Claiming
// ---------------------------------------------------------------------------

// TestCommandClaimFirstOwnerAndDuplicate is the core of §27.3: the first claim
// of a key owns it, a second claim for the same request returns the same record
// without owning it, and a claim for a different request is refused.
func TestCommandClaimFirstOwnerAndDuplicate(t *testing.T) {
	f := newCommandFixture(t)
	key := commandKey("runs.create", "rq_01JRUN_CREATE_001")
	hash := commandHash(t, `{"task_id":"t-1"}`)
	now := commandTime(0)

	first, owner, err := f.claim(key, hash, now)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !owner {
		t.Fatal("first claim reported owner=false, want the first caller to own the key")
	}
	if first.State != CommandStateInFlight {
		t.Errorf("first claim state = %q, want %q", first.State, CommandStateInFlight)
	}
	if first.RequestHash != hash {
		t.Errorf("first claim request hash = %s, want %s", first.RequestHash, hash)
	}
	if first.Key != key {
		t.Errorf("first claim key = %+v, want %+v", first.Key, key)
	}
	if first.StatusCode != nil || first.Response != nil || first.CompletedAt != nil || first.ExpiresAt != nil {
		t.Errorf("an in_flight record carries an outcome: status=%v response=%s completed=%v expires=%v",
			first.StatusCode, first.Response, first.CompletedAt, first.ExpiresAt)
	}
	if !first.CreatedAt.Equal(now) || !first.UpdatedAt.Equal(now) {
		t.Errorf("created/updated = %v/%v, want %v", first.CreatedAt, first.UpdatedAt, now)
	}
	if n := commandRecordCount(t, f.store.DB()); n != 1 {
		t.Fatalf("command_records rows = %d, want 1", n)
	}

	// Same key, same request: the second caller does not own the key and gets
	// the first caller's record, with nothing written.
	second, owner, err := f.claim(key, hash, now.Add(time.Second))
	if err != nil {
		t.Fatalf("duplicate claim: %v", err)
	}
	if owner {
		t.Error("duplicate claim reported owner=true, want false")
	}
	if second.Key != first.Key || second.State != first.State || second.RequestHash != first.RequestHash {
		t.Errorf("duplicate claim returned %+v, want the first record %+v", second, first)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("duplicate claim created_at = %v, want the first claim's %v", second.CreatedAt, first.CreatedAt)
	}
	if n := commandRecordCount(t, f.store.DB()); n != 1 {
		t.Errorf("command_records rows after duplicate = %d, want 1", n)
	}

	// Same key, different request: refused, and the stored record is untouched.
	other := commandHash(t, `{"task_id":"t-1","budget":{"tokens":9000}}`)
	if _, _, err := f.claim(key, other, now); !errors.Is(err, ErrCommandKeyReused) {
		t.Fatalf("claim with a different hash = %v, want ErrCommandKeyReused", err)
	}
	if stored := commandStored(t, f.store.DB(), key); stored.RequestHash != hash {
		t.Errorf("stored request hash = %s, want the first claim's %s", stored.RequestHash, hash)
	}
	if n := commandRecordCount(t, f.store.DB()); n != 1 {
		t.Errorf("command_records rows after a reused key = %d, want 1", n)
	}
}

// TestCommandClaimScopeIsolation proves the four-part scope: the same client key
// in a different principal, project or operation is a different command, which
// is what makes the key safe for a client to choose freely (§27.3).
func TestCommandClaimScopeIsolation(t *testing.T) {
	f := newCommandFixture(t)
	hash := commandHash(t, `{"task_id":"t-1"}`)
	now := commandTime(0)

	base := commandKey("runs.create", "rq_shared")
	variants := map[string]CommandKey{
		"base":      base,
		"principal": {PrincipalID: "principal-2", ProjectID: "p-1", Operation: "runs.create", CommandID: "rq_shared"},
		"project":   {PrincipalID: commandTestPrincipal, ProjectID: "p-2", Operation: "runs.create", CommandID: "rq_shared"},
		"operation": {PrincipalID: commandTestPrincipal, ProjectID: "p-1", Operation: "runs.cancel", CommandID: "rq_shared"},
	}

	for name, key := range variants {
		if _, owner, err := f.claim(key, hash, now); err != nil || !owner {
			t.Fatalf("%s: claim = owner %v, err %v; want owner true and no error", name, owner, err)
		}
	}
	if n := commandRecordCount(t, f.store.DB()); n != len(variants) {
		t.Fatalf("command_records rows = %d, want %d (one per distinct scope)", n, len(variants))
	}

	// The base key is still owned by its first claim: adding siblings did not
	// release it.
	if _, owner, err := f.claim(base, hash, now); err != nil || owner {
		t.Fatalf("re-claim of the base scope = owner %v, err %v; want owner false and no error", owner, err)
	}
}

// TestCommandClaimConcurrentSameHashSingleOwner is the concurrency assertion of
// §27.3: two handles and sixteen claimers race for one key with one request
// hash, and exactly one becomes the owner. Everyone else must read the winner's
// record rather than be told the key is reused — the request is identical.
//
// It also asserts that no claim fails with ErrCommandClaimRaced, i.e. that under
// the BEGIN IMMEDIATE transactions OpenStore pins, the loser of a claim race
// always sees the committed row.
func TestCommandClaimConcurrentSameHashSingleOwner(t *testing.T) {
	ctx := context.Background()
	f := newCommandFixture(t)
	second := openSecondStore(t, f.path)

	key := commandKey("runs.create", "rq_concurrent_same")
	hash := commandHash(t, `{"task_id":"t-1"}`)
	now := commandTime(0)

	const perHandle = 8
	stores := []*Store{f.store, second}
	type result struct {
		index  int
		record CommandRecord
		owner  bool
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, len(stores)*perHandle)

	var wg sync.WaitGroup
	for h, store := range stores {
		for i := 0; i < perHandle; i++ {
			wg.Add(1)
			go func(h, i int, store *Store) {
				defer wg.Done()
				<-start // release every claimer at once to maximise the race
				record, owner, err := claimWith(ctx, store, key, hash, now)
				results <- result{index: h*perHandle + i, record: record, owner: owner, err: err}
			}(h, i, store)
		}
	}
	close(start)
	wg.Wait()
	close(results)

	var (
		owners   []int
		losers   int
		raced    int
		other    []error
		firstRec CommandRecord
	)
	for res := range results {
		switch {
		case res.err == nil && res.owner:
			owners = append(owners, res.index)
			firstRec = res.record
		case res.err == nil:
			losers++
			if res.record.State != CommandStateInFlight || res.record.RequestHash != hash || res.record.Key != key {
				t.Errorf("claimer %d got %+v, want the winner's in_flight record for %+v", res.index, res.record, key)
			}
		case errors.Is(res.err, ErrCommandClaimRaced):
			raced++
		default:
			other = append(other, fmt.Errorf("claimer %d: %w", res.index, res.err))
		}
	}
	if len(owners) != 1 {
		t.Errorf("owners = %v, want exactly one claimer to own the key", owners)
	}
	if losers != len(stores)*perHandle-1 {
		t.Errorf("losers = %d, want %d (every other claimer reads the record)", losers, len(stores)*perHandle-1)
	}
	if raced != 0 {
		t.Errorf("claim races = %d, want 0 under the immediate transaction mode", raced)
	}
	for _, err := range other {
		t.Errorf("unexpected claim error: %v", err)
	}
	if n := commandRecordCount(t, f.store.DB()); n != 1 {
		t.Errorf("command_records rows = %d, want 1", n)
	}
	if firstRec.State != CommandStateInFlight {
		t.Errorf("winner state = %q, want %q", firstRec.State, CommandStateInFlight)
	}
}

// TestCommandClaimConcurrentDifferentHashReused is the other half: when the
// claimers disagree about the request, the winner owns the key and every loser
// is told the key was reused for a different request. Not one of them may be
// answered with the winner's record, because that record is a different request.
func TestCommandClaimConcurrentDifferentHashReused(t *testing.T) {
	ctx := context.Background()
	f := newCommandFixture(t)
	second := openSecondStore(t, f.path)

	key := commandKey("runs.create", "rq_concurrent_diff")
	now := commandTime(0)

	const perHandle = 8
	stores := []*Store{f.store, second}
	type result struct {
		index int
		owner bool
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, len(stores)*perHandle)

	var wg sync.WaitGroup
	for h, store := range stores {
		for i := 0; i < perHandle; i++ {
			wg.Add(1)
			go func(h, i int, store *Store) {
				defer wg.Done()
				// A distinct, valid request per claimer: different bodies mean
				// different request hashes.
				hash := commandHash(t, fmt.Sprintf(`{"task_id":"t-%d"}`, h*perHandle+i))
				<-start
				_, owner, err := claimWith(ctx, store, key, hash, now)
				results <- result{index: h*perHandle + i, owner: owner, err: err}
			}(h, i, store)
		}
	}
	close(start)
	wg.Wait()
	close(results)

	var (
		owners int
		reused int
		other  []error
	)
	for res := range results {
		switch {
		case res.err == nil && res.owner:
			owners++
		case errors.Is(res.err, ErrCommandKeyReused):
			reused++
		case res.err == nil:
			other = append(other, fmt.Errorf("claimer %d: reported owner=false for a request nobody else made", res.index))
		default:
			other = append(other, fmt.Errorf("claimer %d: %w", res.index, res.err))
		}
	}
	if owners != 1 {
		t.Errorf("owners = %d, want exactly 1", owners)
	}
	if reused != len(stores)*perHandle-1 {
		t.Errorf("reused-key errors = %d, want %d", reused, len(stores)*perHandle-1)
	}
	for _, err := range other {
		t.Errorf("unexpected claim outcome: %v", err)
	}
	if n := commandRecordCount(t, f.store.DB()); n != 1 {
		t.Errorf("command_records rows = %d, want 1", n)
	}
}

// TestCommandClaimOwnerOnlySideEffect is §27.3's "只有拥有者产生副作用": sixteen
// claimers each try to insert a task when they own the key, and exactly one row
// appears. This is the property the whole ledger exists for.
func TestCommandClaimOwnerOnlySideEffect(t *testing.T) {
	ctx := context.Background()
	f := newCommandFixture(t)
	second := openSecondStore(t, f.path)

	key := commandKey("runs.create", "rq_side_effect")
	hash := commandHash(t, `{"task_id":"t-1"}`)
	now := commandTime(0)

	const perHandle = 8
	stores := []*Store{f.store, second}
	start := make(chan struct{})
	errs := make(chan error, len(stores)*perHandle)

	var wg sync.WaitGroup
	for h, store := range stores {
		for i := 0; i < perHandle; i++ {
			wg.Add(1)
			go func(h, i int, store *Store) {
				defer wg.Done()
				<-start
				werr := store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
					_, owner, err := ClaimCommandTx(ctx, tx, key, hash, now)
					if err != nil {
						return err
					}
					if !owner {
						// The side effect is what the owner does; everyone else
						// has nothing to do but report the recorded state.
						return nil
					}
					task := run.Task{
						ID:        fmt.Sprintf("side-%d-%d", h, i),
						ProjectID: "p-1",
						Title:     "created by the owner of the key",
						Kind:      run.TaskKindCode,
						Status:    run.TaskStatusReady,
						InputJSON: `{"prompt":"side effect"}`,
						CreatedAt: now,
						UpdatedAt: now,
					}
					return InsertTask(ctx, tx, &task)
				})
				if werr != nil {
					errs <- fmt.Errorf("claimer %d-%d: %w", h, i, werr)
				}
			}(h, i, store)
		}
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("claimer failed: %v", err)
	}

	var n int
	if err := f.store.DB().QueryRow(`SELECT COUNT(*) FROM tasks WHERE id LIKE 'side-%'`).Scan(&n); err != nil {
		t.Fatalf("count side effects: %v", err)
	}
	if n != 1 {
		t.Errorf("side-effect rows = %d, want exactly 1 (only the key's owner may act)", n)
	}
	if n := commandRecordCount(t, f.store.DB()); n != 1 {
		t.Errorf("command_records rows = %d, want 1", n)
	}
}

// TestCommandClaimRollbackReleasesKey is the atomicity assertion: the claim and
// the side effect are one transaction, so a rollback takes both away and the
// next attempt with the same key becomes the owner. Anything else would either
// lose a command (key held, side effect gone) or execute twice (side effect
// kept, key free).
func TestCommandClaimRollbackReleasesKey(t *testing.T) {
	ctx := context.Background()
	f := newCommandFixture(t)
	key := commandKey("runs.create", "rq_rollback")
	hash := commandHash(t, `{"task_id":"t-1"}`)
	now := commandTime(0)

	sentinel := errors.New("caller aborted after the side effect")
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		_, owner, err := ClaimCommandTx(ctx, tx, key, hash, now)
		if err != nil {
			return err
		}
		if !owner {
			return errors.New("first claim did not own the key")
		}
		task := run.Task{
			ID:        "side-rollback",
			ProjectID: "p-1",
			Title:     "rolled back",
			Kind:      run.TaskKindCode,
			Status:    run.TaskStatusReady,
			InputJSON: `{"prompt":"rolled back"}`,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := InsertTask(ctx, tx, &task); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx error = %v, want the caller's own error", err)
	}

	// Neither half survived.
	if _, err := GetCommand(ctx, f.store.DB(), key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCommand after rollback = %v, want ErrNotFound", err)
	}
	var tasks int
	if err := f.store.DB().QueryRow(`SELECT COUNT(*) FROM tasks WHERE id = 'side-rollback'`).Scan(&tasks); err != nil {
		t.Fatalf("count rolled-back task: %v", err)
	}
	if tasks != 0 {
		t.Errorf("rolled-back side-effect rows = %d, want 0", tasks)
	}

	// The key is free again, and the new owner can act.
	record, owner, err := f.claim(key, hash, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("claim after rollback: %v", err)
	}
	if !owner {
		t.Fatal("claim after rollback reported owner=false, want the key to be free")
	}
	if record.State != CommandStateInFlight {
		t.Errorf("claim after rollback state = %q, want %q", record.State, CommandStateInFlight)
	}
}

// ---------------------------------------------------------------------------
// Completing
// ---------------------------------------------------------------------------

// TestCommandCompleteSucceeded covers the terminal transition: the recorded
// answer is canonical JSON, the status code and resource are stored, and the
// retention deadline is now + the caller's window (the 30-day default when the
// caller asks for nothing).
func TestCommandCompleteSucceeded(t *testing.T) {
	f := newCommandFixture(t)
	key := commandKey("runs.create", "rq_complete_ok")
	hash := commandHash(t, `{"task_id":"t-1"}`)
	claimedAt := commandTime(0)
	if _, owner, err := f.claim(key, hash, claimedAt); err != nil || !owner {
		t.Fatalf("claim = owner %v, err %v", owner, err)
	}

	completedAt := claimedAt.Add(2 * time.Second)
	// Deliberately serialised with different key order and whitespace: the
	// stored body must be the canonical form, not the caller's bytes.
	record, err := f.complete(key, hash, CommandOutcome{
		State:        CommandStateSucceeded,
		StatusCode:   201,
		Response:     json.RawMessage("{\n  \"run_id\" : \"run-1\",\n  \"status\"  :\"queued\"\n}"),
		ResourceType: "run",
		ResourceID:   "run-1",
	}, 0, completedAt)
	if err != nil {
		t.Fatalf("CompleteCommandTx: %v", err)
	}

	if record.State != CommandStateSucceeded {
		t.Errorf("state = %q, want %q", record.State, CommandStateSucceeded)
	}
	if record.StatusCode == nil || *record.StatusCode != 201 {
		t.Errorf("status code = %v, want 201", record.StatusCode)
	}
	if got, want := string(record.Response), `{"run_id":"run-1","status":"queued"}`; got != want {
		t.Errorf("stored response = %s, want the canonical %s", got, want)
	}
	if record.ResourceType == nil || *record.ResourceType != "run" ||
		record.ResourceID == nil || *record.ResourceID != "run-1" {
		t.Errorf("resource = %v/%v, want run/run-1", record.ResourceType, record.ResourceID)
	}
	if record.CompletedAt == nil || !record.CompletedAt.Equal(completedAt) {
		t.Errorf("completed_at = %v, want %v", record.CompletedAt, completedAt)
	}
	wantExpires := fromUnixMilli(unixMilli(completedAt) + DefaultCommandRetention.Milliseconds())
	if record.ExpiresAt == nil || !record.ExpiresAt.Equal(wantExpires) {
		t.Errorf("expires_at = %v, want %v (the 30-day default)", record.ExpiresAt, wantExpires)
	}
	if !record.UpdatedAt.Equal(completedAt) {
		t.Errorf("updated_at = %v, want %v", record.UpdatedAt, completedAt)
	}
	if !record.CreatedAt.Equal(claimedAt) {
		t.Errorf("created_at = %v, want the claim's %v", record.CreatedAt, claimedAt)
	}

	// A caller may extend the retention window; the record says so.
	if _, err := f.complete(key, hash, CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"ok":true}`)}, 0, completedAt); !errors.Is(err, ErrCommandAlreadyCompleted) {
		t.Fatalf("completing a terminal command = %v, want ErrCommandAlreadyCompleted", err)
	}

	// The raw row holds the canonical body and a non-NULL outcome.
	stored := commandStored(t, f.store.DB(), key)
	if stored.State != string(CommandStateSucceeded) || !stored.StatusCode.Valid || stored.StatusCode.Int64 != 201 {
		t.Errorf("stored state/status = %q/%v, want succeeded/201", stored.State, stored.StatusCode)
	}
	if stored.Response.String != `{"run_id":"run-1","status":"queued"}` {
		t.Errorf("stored response_json = %s, want the canonical body", stored.Response.String)
	}
	if stored.ResourceType.String != "run" || stored.ResourceID.String != "run-1" {
		t.Errorf("stored resource = %q/%q, want run/run-1", stored.ResourceType.String, stored.ResourceID.String)
	}
	if stored.LastError.Valid {
		t.Errorf("stored last_error = %q, want NULL for a completed command", stored.LastError.String)
	}
	if !stored.CompletedAt.Valid || !stored.ExpiresAt.Valid {
		t.Errorf("stored completed_at/expires_at = %v/%v, want both set", stored.CompletedAt, stored.ExpiresAt)
	}
	if stored.RequestHash != hash {
		t.Errorf("stored request hash = %s, want %s", stored.RequestHash, hash)
	}

	// A duplicate request reads the recorded answer instead of executing.
	duplicate, owner, err := f.claim(key, hash, completedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("claim after completion: %v", err)
	}
	if owner {
		t.Error("claim after completion reported owner=true, want the recorded command")
	}
	if duplicate.State != CommandStateSucceeded || string(duplicate.Response) != `{"run_id":"run-1","status":"queued"}` {
		t.Errorf("duplicate read %+v, want the recorded success", duplicate)
	}

	// A longer retention window is honoured.
	longKey := commandKey("runs.create", "rq_complete_long")
	if _, owner, err := f.claim(longKey, hash, claimedAt); err != nil || !owner {
		t.Fatalf("claim long-retention command = owner %v, err %v", owner, err)
	}
	long, err := f.complete(longKey, hash, CommandOutcome{
		State: CommandStateSucceeded, StatusCode: 202, Response: json.RawMessage(`{"accepted":true}`),
	}, 90*24*time.Hour, completedAt)
	if err != nil {
		t.Fatalf("complete with a 90-day window: %v", err)
	}
	wantLong := fromUnixMilli(unixMilli(completedAt) + (90 * 24 * time.Hour).Milliseconds())
	if long.ExpiresAt == nil || !long.ExpiresAt.Equal(wantLong) {
		t.Errorf("expires_at = %v, want %v (the caller's 90-day window)", long.ExpiresAt, wantLong)
	}
}

// TestCommandCompleteFailed proves a failure is a recorded outcome too: it is
// terminal, it carries the status code the client was given, and a retry reads
// it back rather than executing again.
func TestCommandCompleteFailed(t *testing.T) {
	f := newCommandFixture(t)
	key := commandKey("runs.create", "rq_complete_failed")
	hash := commandHash(t, `{"task_id":"t-1"}`)
	claimedAt := commandTime(0)
	if _, owner, err := f.claim(key, hash, claimedAt); err != nil || !owner {
		t.Fatalf("claim = owner %v, err %v", owner, err)
	}

	record, err := f.complete(key, hash, CommandOutcome{
		State:      CommandStateFailed,
		StatusCode: 503,
		Response:   json.RawMessage(`{"error":{"code":"backend_unavailable","retryable":true}}`),
	}, 0, claimedAt.Add(time.Second))
	if err != nil {
		t.Fatalf("CompleteCommandTx: %v", err)
	}
	if record.State != CommandStateFailed {
		t.Errorf("state = %q, want %q", record.State, CommandStateFailed)
	}
	if record.ResourceType != nil || record.ResourceID != nil {
		t.Errorf("resource = %v/%v, want nil for a failed command", record.ResourceType, record.ResourceID)
	}

	replay, owner, err := f.claim(key, hash, claimedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("claim after failure: %v", err)
	}
	if owner {
		t.Error("claim after failure reported owner=true, want the recorded failure")
	}
	if replay.State != CommandStateFailed || replay.StatusCode == nil || *replay.StatusCode != 503 {
		t.Errorf("replay = %+v, want the recorded 503 failure", replay)
	}

	// A failure is terminal: the caller cannot replace it with a success.
	if _, err := f.complete(key, hash, CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"ok":true}`)}, 0, claimedAt.Add(2*time.Hour)); !errors.Is(err, ErrCommandAlreadyCompleted) {
		t.Fatalf("completing a failed command = %v, want ErrCommandAlreadyCompleted", err)
	}
}

// TestCommandCompleteFromReconciling proves a reconciled command can still reach
// a terminal state: the owner resolved the unknown outcome and records what it
// found.
func TestCommandCompleteFromReconciling(t *testing.T) {
	ctx := context.Background()
	f := newCommandFixture(t)
	key := commandKey("runs.create", "rq_reconciled")
	hash := commandHash(t, `{"task_id":"t-1"}`)
	now := commandTime(0)
	if _, owner, err := f.claim(key, hash, now); err != nil || !owner {
		t.Fatalf("claim = owner %v, err %v", owner, err)
	}

	var reconciling CommandRecord
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		reconciling, err = MarkCommandReconcilingTx(ctx, tx, key, hash, "peer timed out after the request was sent", now.Add(time.Second))
		return err
	})
	if err != nil {
		t.Fatalf("MarkCommandReconcilingTx: %v", err)
	}
	if reconciling.State != CommandStateReconciling {
		t.Fatalf("state = %q, want %q", reconciling.State, CommandStateReconciling)
	}

	record, err := f.complete(key, hash, CommandOutcome{
		State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"run_id":"run-1"}`),
		ResourceType: "run", ResourceID: "run-1",
	}, 0, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("CompleteCommandTx from reconciling: %v", err)
	}
	if record.State != CommandStateSucceeded || record.StatusCode == nil || *record.StatusCode != 200 {
		t.Errorf("record = %+v, want the reconciled success", record)
	}
	if record.LastError == nil || *record.LastError == "" {
		t.Error("last_error was cleared by completion, want the reconciliation reason kept as evidence")
	}
}

// TestCommandCompleteRejectsInvalidOutcome is the validation half: an outcome
// the contract rejects is refused before any SQL runs, and the record stays
// exactly as it was, so the owner can fix its answer and complete again.
func TestCommandCompleteRejectsInvalidOutcome(t *testing.T) {
	f := newCommandFixture(t)
	key := commandKey("runs.create", "rq_bad_outcome")
	hash := commandHash(t, `{"task_id":"t-1"}`)
	now := commandTime(0)
	if _, owner, err := f.claim(key, hash, now); err != nil || !owner {
		t.Fatalf("claim = owner %v, err %v", owner, err)
	}

	bigBody := json.RawMessage(`{"pad":"` + strings.Repeat("x", MaxCommandResponseBytes) + `"}`)
	cases := []struct {
		name    string
		outcome CommandOutcome
	}{
		{"active state", CommandOutcome{State: CommandStateInFlight, StatusCode: 200, Response: json.RawMessage(`{"ok":true}`)}},
		{"expired state", CommandOutcome{State: CommandStateExpired, StatusCode: 200, Response: json.RawMessage(`{"ok":true}`)}},
		{"unknown state", CommandOutcome{State: "done", StatusCode: 200, Response: json.RawMessage(`{"ok":true}`)}},
		{"status below 100", CommandOutcome{State: CommandStateSucceeded, StatusCode: 99, Response: json.RawMessage(`{"ok":true}`)}},
		{"status above 599", CommandOutcome{State: CommandStateSucceeded, StatusCode: 600, Response: json.RawMessage(`{"ok":true}`)}},
		{"zero status", CommandOutcome{State: CommandStateSucceeded, StatusCode: 0, Response: json.RawMessage(`{"ok":true}`)}},
		{"array response", CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`[1,2,3]`)}},
		{"string response", CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`"ok"`)}},
		{"null response", CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`null`)}},
		{"empty response", CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: nil}},
		{"malformed response", CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"ok":`)}},
		{"trailing content", CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"ok":true}}`)}},
		{"oversized response", CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: bigBody}},
		{"secret in response", CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"token":"cf_live_9f3"}`)}},
		{"secret in a nested response", CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"error":{"secret":"cf_live_9f3"}}`)}},
		{"resource type without id", CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"ok":true}`), ResourceType: "run"}},
		{"resource id without type", CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"ok":true}`), ResourceID: "run-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.complete(key, hash, tc.outcome, 0, now.Add(time.Second))
			if err == nil {
				t.Fatalf("CompleteCommandTx accepted %+v, want a refusal", tc.outcome)
			}
			// A secret-bearing body is reported as the secret sentinel (the
			// same one a snapshot body gets); everything else is
			// ErrInvalidCommand. Both are contract refusals.
			if !errors.Is(err, ErrInvalidCommand) && !errors.Is(err, ErrSecretInSnapshot) {
				t.Fatalf("error = %v, want ErrInvalidCommand or ErrSecretInSnapshot", err)
			}
			if strings.Contains(tc.name, "secret in") && !errors.Is(err, ErrSecretInSnapshot) {
				t.Errorf("error = %v, want ErrSecretInSnapshot for a secret-bearing response", err)
			}
			if strings.Contains(err.Error(), "cf_live_9f3") {
				t.Errorf("error %q leaks the rejected value", err)
			}

			// The record is untouched: still in_flight, no outcome columns.
			stored := commandStored(t, f.store.DB(), key)
			if stored.State != string(CommandStateInFlight) {
				t.Errorf("state after a refused outcome = %q, want %q", stored.State, CommandStateInFlight)
			}
			if stored.StatusCode.Valid || stored.Response.Valid || stored.CompletedAt.Valid || stored.ExpiresAt.Valid {
				t.Errorf("a refused outcome wrote status/response/completed/expires: %v/%v/%v/%v",
					stored.StatusCode, stored.Response, stored.CompletedAt, stored.ExpiresAt)
			}
		})
	}

	// After all of that the owner can still complete correctly, which is the
	// point of refusing before the write.
	if _, err := f.complete(key, hash, CommandOutcome{
		State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"ok":true}`),
	}, 0, now.Add(time.Minute)); err != nil {
		t.Fatalf("CompleteCommandTx after refused outcomes: %v", err)
	}
}

// TestCommandCompleteRejectsWrongStateOrHash covers the CAS's other two
// refusals: a hash that does not match the claim, and a key nobody claimed.
func TestCommandCompleteRejectsWrongStateOrHash(t *testing.T) {
	ctx := context.Background()
	f := newCommandFixture(t)
	key := commandKey("runs.create", "rq_complete_cas")
	hash := commandHash(t, `{"task_id":"t-1"}`)
	other := commandHash(t, `{"task_id":"t-1","budget":{"tokens":9000}}`)
	now := commandTime(0)
	if _, owner, err := f.claim(key, hash, now); err != nil || !owner {
		t.Fatalf("claim = owner %v, err %v", owner, err)
	}

	outcome := CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"ok":true}`)}
	if _, err := f.complete(key, other, outcome, 0, now); !errors.Is(err, ErrCommandKeyReused) {
		t.Fatalf("complete with a different hash = %v, want ErrCommandKeyReused", err)
	}
	if stored := commandStored(t, f.store.DB(), key); stored.State != string(CommandStateInFlight) {
		t.Errorf("state after a hash mismatch = %q, want the record untouched", stored.State)
	}

	if _, err := f.complete(commandKey("runs.create", "rq_never_claimed"), hash, outcome, 0, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("complete of an unknown key = %v, want ErrNotFound", err)
	}

	// The same hash check applies to the reconciling transition.
	var markErr error
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		_, markErr = MarkCommandReconcilingTx(ctx, tx, key, other, "peer timed out", now)
		return markErr
	})
	if !errors.Is(err, ErrCommandKeyReused) {
		t.Fatalf("MarkCommandReconcilingTx with a different hash = %v, want ErrCommandKeyReused", err)
	}
}

// ---------------------------------------------------------------------------
// Reconciling
// ---------------------------------------------------------------------------

// TestCommandReconcileAndGet covers the read side of §20.4 and the reconciling
// transition: a client whose response was lost asks by key, an unknown key is
// ErrNotFound, and a command with an unknown external outcome is recorded as
// reconciling with the key still claimed.
func TestCommandReconcileAndGet(t *testing.T) {
	ctx := context.Background()
	f := newCommandFixture(t)
	key := commandKey("runs.create", "rq_get")
	hash := commandHash(t, `{"task_id":"t-1"}`)
	now := commandTime(0)

	if _, err := GetCommand(ctx, f.store.DB(), key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCommand of an unknown key = %v, want ErrNotFound", err)
	}

	if _, owner, err := f.claim(key, hash, now); err != nil || !owner {
		t.Fatalf("claim = owner %v, err %v", owner, err)
	}

	// The read works through the pool and inside a transaction alike.
	fromPool, err := GetCommand(ctx, f.store.DB(), key)
	if err != nil {
		t.Fatalf("GetCommand: %v", err)
	}
	if fromPool.State != CommandStateInFlight || fromPool.RequestHash != hash || fromPool.Key != key {
		t.Errorf("GetCommand = %+v, want the claimed in_flight record", fromPool)
	}

	var marked CommandRecord
	err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		// A claim of the same key inside another transaction is not needed to
		// see the row; the read itself is what the HTTP path uses.
		if _, err := GetCommand(ctx, tx, key); err != nil {
			return err
		}
		var err error
		marked, err = MarkCommandReconcilingTx(ctx, tx, key, hash, "external side effect unknown", now.Add(time.Second))
		return err
	})
	if err != nil {
		t.Fatalf("MarkCommandReconcilingTx: %v", err)
	}
	if marked.State != CommandStateReconciling {
		t.Errorf("state = %q, want %q", marked.State, CommandStateReconciling)
	}
	if marked.LastError == nil || *marked.LastError != "external side effect unknown" {
		t.Errorf("last_error = %v, want the reason", marked.LastError)
	}
	if marked.CompletedAt != nil || marked.ExpiresAt != nil {
		t.Errorf("a reconciling record has completed_at/expires_at: %v/%v", marked.CompletedAt, marked.ExpiresAt)
	}
	if marked.StatusCode != nil || marked.Response != nil {
		t.Errorf("a reconciling record carries an outcome: %v/%s", marked.StatusCode, marked.Response)
	}

	// A duplicate request still finds an active command: the API answers 202
	// accepted and, crucially, does not start the operation again (§20.4).
	duplicate, owner, err := f.claim(key, hash, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("claim of a reconciling command: %v", err)
	}
	if owner {
		t.Error("claim of a reconciling command reported owner=true, want false")
	}
	if !duplicate.State.Active() {
		t.Errorf("duplicate state = %q, want an active state the API answers 202 for", duplicate.State)
	}

	// Marking an already-reconciling command is a no-op, not an error: a caller
	// retrying its own bookkeeping must not have to care.
	err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		again, err := MarkCommandReconcilingTx(ctx, tx, key, hash, "still unknown", now.Add(3*time.Second))
		if err != nil {
			return err
		}
		if again.State != CommandStateReconciling {
			t.Errorf("state = %q, want %q", again.State, CommandStateReconciling)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("second MarkCommandReconcilingTx: %v", err)
	}

	// A blank reason is a caller bug: refusing it keeps an unexplained
	// reconciling row out of the ledger.
	err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		_, err := MarkCommandReconcilingTx(ctx, tx, key, hash, "   ", now)
		return err
	})
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("blank reason = %v, want ErrInvalidCommand", err)
	}

	// A terminal command cannot be marked reconciling.
	terminalKey := commandKey("runs.create", "rq_get_terminal")
	if _, owner, err := f.claim(terminalKey, hash, now); err != nil || !owner {
		t.Fatalf("claim terminal command = owner %v, err %v", owner, err)
	}
	if _, err := f.complete(terminalKey, hash, CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"ok":true}`)}, 0, now); err != nil {
		t.Fatalf("complete: %v", err)
	}
	err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		_, err := MarkCommandReconcilingTx(ctx, tx, terminalKey, hash, "too late", now)
		return err
	})
	if !errors.Is(err, ErrCommandAlreadyCompleted) {
		t.Fatalf("marking a terminal command reconciling = %v, want ErrCommandAlreadyCompleted", err)
	}
}

// TestCommandReconcileTruncatesReason proves the stored reason is bounded and
// never split mid-character: it must be valid UTF-8 whatever the caller passed.
func TestCommandReconcileTruncatesReason(t *testing.T) {
	ctx := context.Background()
	f := newCommandFixture(t)
	key := commandKey("runs.create", "rq_reason")
	hash := commandHash(t, `{"task_id":"t-1"}`)
	now := commandTime(0)
	if _, owner, err := f.claim(key, hash, now); err != nil || !owner {
		t.Fatalf("claim = owner %v, err %v", owner, err)
	}

	// Three-byte runes: a byte-wise cut would land inside one.
	reason := strings.Repeat("原", MaxCommandLastErrorBytes)
	var marked CommandRecord
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		marked, err = MarkCommandReconcilingTx(ctx, tx, key, hash, reason, now)
		return err
	})
	if err != nil {
		t.Fatalf("MarkCommandReconcilingTx: %v", err)
	}
	if marked.LastError == nil {
		t.Fatal("last_error is nil, want the truncated reason")
	}
	if got := len(*marked.LastError); got > MaxCommandLastErrorBytes {
		t.Errorf("last_error is %d bytes, want at most %d", got, MaxCommandLastErrorBytes)
	}
	if !utf8.ValidString(*marked.LastError) {
		t.Errorf("last_error %q is not valid UTF-8", *marked.LastError)
	}
	if !strings.HasPrefix(reason, *marked.LastError) {
		t.Errorf("last_error %q is not a prefix of the reason", *marked.LastError)
	}
	if want := MaxCommandLastErrorBytes - MaxCommandLastErrorBytes%3; len(*marked.LastError) != want {
		t.Errorf("last_error is %d bytes, want %d (the longest rune-aligned prefix)", len(*marked.LastError), want)
	}
	// The stored row agrees with what the transition returned.
	if stored := commandStored(t, f.store.DB(), key); stored.LastError.String != *marked.LastError {
		t.Errorf("stored last_error = %q, want %q", stored.LastError.String, *marked.LastError)
	}
}

// ---------------------------------------------------------------------------
// Expiry
// ---------------------------------------------------------------------------

// TestCommandExpireTombstone is §27.3's retention rule: a terminal record past
// its deadline becomes a tombstone that keeps the key, the hash and the outcome
// but drops the response, and the key is not released by expiring.
func TestCommandExpireTombstone(t *testing.T) {
	ctx := context.Background()
	f := newCommandFixture(t)
	hash := commandHash(t, `{"task_id":"t-1"}`)
	claimedAt := commandTime(-48 * time.Hour)

	keys := []CommandKey{
		commandKey("runs.create", "rq_expire_success"),
		commandKey("runs.create", "rq_expire_failure"),
	}
	if _, err := f.complete(keys[0], hash, CommandOutcome{
		State: CommandStateSucceeded, StatusCode: 201, Response: json.RawMessage(`{"run_id":"run-1"}`),
		ResourceType: "run", ResourceID: "run-1",
	}, time.Hour, claimedAt); err == nil {
		// complete() claims nothing by itself: claim first.
		t.Fatal("completing an unclaimed key succeeded, want ErrNotFound")
	}
	for i, key := range keys {
		if _, owner, err := f.claim(key, hash, claimedAt); err != nil || !owner {
			t.Fatalf("claim %d = owner %v, err %v", i, owner, err)
		}
	}
	outcomes := []CommandOutcome{
		{State: CommandStateSucceeded, StatusCode: 201, Response: json.RawMessage(`{"run_id":"run-1"}`), ResourceType: "run", ResourceID: "run-1"},
		{State: CommandStateFailed, StatusCode: 503, Response: json.RawMessage(`{"error":{"code":"backend_unavailable"}}`)},
	}
	for i, key := range keys {
		if _, err := f.complete(key, hash, outcomes[i], time.Hour, claimedAt); err != nil {
			t.Fatalf("complete %d: %v", i, err)
		}
	}

	// An active command from the same era must not be touched: it has no
	// deadline at all.
	activeKey := commandKey("runs.create", "rq_expire_active")
	if _, owner, err := f.claim(activeKey, hash, claimedAt); err != nil || !owner {
		t.Fatalf("claim active command = owner %v, err %v", owner, err)
	}
	reconcilingKey := commandKey("runs.create", "rq_expire_reconciling")
	if _, owner, err := f.claim(reconcilingKey, hash, claimedAt); err != nil || !owner {
		t.Fatalf("claim reconciling command = owner %v, err %v", owner, err)
	}
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		_, err := MarkCommandReconcilingTx(ctx, tx, reconcilingKey, hash, "unknown", claimedAt)
		return err
	})
	if err != nil {
		t.Fatalf("MarkCommandReconcilingTx: %v", err)
	}

	now := commandTime(0)
	var expired int
	err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		expired, err = ExpireCommandsTx(ctx, tx, now, 0)
		return err
	})
	if err != nil {
		t.Fatalf("ExpireCommandsTx: %v", err)
	}
	if expired != 2 {
		t.Fatalf("expired %d commands, want 2", expired)
	}

	for i, key := range keys {
		record, err := GetCommand(ctx, f.store.DB(), key)
		if err != nil {
			t.Fatalf("GetCommand %d: %v", i, err)
		}
		if record.State != CommandStateExpired {
			t.Errorf("record %d state = %q, want %q", i, record.State, CommandStateExpired)
		}
		if record.Response != nil {
			t.Errorf("record %d still holds a response: %s", i, record.Response)
		}
		if record.RequestHash != hash {
			t.Errorf("record %d request hash = %s, want %s", i, record.RequestHash, hash)
		}
		if record.StatusCode == nil || *record.StatusCode != outcomes[i].StatusCode {
			t.Errorf("record %d status = %v, want %d", i, record.StatusCode, outcomes[i].StatusCode)
		}
		if record.CompletedAt == nil || record.ExpiresAt == nil {
			t.Errorf("record %d lost its timestamps: completed=%v expires=%v", i, record.CompletedAt, record.ExpiresAt)
		}
		if !record.CreatedAt.Equal(claimedAt) {
			t.Errorf("record %d created_at = %v, want %v", i, record.CreatedAt, claimedAt)
		}
	}
	// The resource reference survives the tombstone: the client can still find
	// what the command created.
	if record, err := GetCommand(ctx, f.store.DB(), keys[0]); err != nil {
		t.Fatalf("GetCommand: %v", err)
	} else if record.ResourceID == nil || *record.ResourceID != "run-1" {
		t.Errorf("resource id = %v, want run-1", record.ResourceID)
	}

	// Active commands are untouched, however old they are.
	for _, key := range []CommandKey{activeKey, reconcilingKey} {
		record, err := GetCommand(ctx, f.store.DB(), key)
		if err != nil {
			t.Fatalf("GetCommand(%s): %v", key.CommandID, err)
		}
		if record.State.Terminal() {
			t.Errorf("command %s became %q, want an active state (an active command is never expired)",
				key.CommandID, record.State)
		}
	}

	// A tombstone keeps the key claimed. Same request → owner=false and the
	// expired record (the command is never executed again); different request →
	// the key is reused, which is a 409.
	duplicate, owner, err := f.claim(keys[0], hash, now)
	if err != nil {
		t.Fatalf("claim of an expired key: %v", err)
	}
	if owner {
		t.Fatal("claim of an expired key reported owner=true, want the tombstone")
	}
	if duplicate.State != CommandStateExpired || duplicate.Response != nil {
		t.Errorf("duplicate = %+v, want the expired tombstone", duplicate)
	}
	if _, _, err := f.claim(keys[0], commandHash(t, `{"task_id":"other"}`), now); !errors.Is(err, ErrCommandKeyReused) {
		t.Fatalf("claim of an expired key with a different request = %v, want ErrCommandKeyReused", err)
	}
	// Completing a tombstone is refused: the first outcome stands.
	if _, err := f.complete(keys[0], hash, CommandOutcome{State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"ok":true}`)}, 0, now); !errors.Is(err, ErrCommandAlreadyCompleted) {
		t.Fatalf("completing an expired command = %v, want ErrCommandAlreadyCompleted", err)
	}
	// A second sweep finds nothing: the tombstone is not a candidate again.
	err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		expired, err = ExpireCommandsTx(ctx, tx, now, 0)
		return err
	})
	if err != nil {
		t.Fatalf("second ExpireCommandsTx: %v", err)
	}
	if expired != 0 {
		t.Errorf("second sweep expired %d commands, want 0", expired)
	}
}

// TestCommandExpireRespectsLimit proves the sweep is bounded and deterministic:
// a batch size of one tombstones the due record with the earliest deadline, and
// repeated sweeps make progress.
func TestCommandExpireRespectsLimit(t *testing.T) {
	ctx := context.Background()
	f := newCommandFixture(t)
	hash := commandHash(t, `{"task_id":"t-1"}`)
	claimedAt := commandTime(-72 * time.Hour)
	now := commandTime(0)

	// Three completed commands with different deadlines: 1h, 2h and 3h after the
	// claim, all in the past.
	keys := []CommandKey{
		commandKey("runs.create", "rq_limit_a"),
		commandKey("runs.create", "rq_limit_b"),
		commandKey("runs.create", "rq_limit_c"),
	}
	retentions := []time.Duration{time.Hour, 2 * time.Hour, 3 * time.Hour}
	for i, key := range keys {
		if _, owner, err := f.claim(key, hash, claimedAt); err != nil || !owner {
			t.Fatalf("claim %d = owner %v, err %v", i, owner, err)
		}
		if _, err := f.complete(key, hash, CommandOutcome{
			State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"ok":true}`),
		}, retentions[i], claimedAt); err != nil {
			t.Fatalf("complete %d: %v", i, err)
		}
	}

	// A deadline in the future is not due: make the last one due later than now.
	futureKey := commandKey("runs.create", "rq_limit_future")
	if _, owner, err := f.claim(futureKey, hash, claimedAt); err != nil || !owner {
		t.Fatalf("claim future = owner %v, err %v", owner, err)
	}
	if _, err := f.complete(futureKey, hash, CommandOutcome{
		State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"ok":true}`),
	}, 100*time.Hour, claimedAt); err != nil {
		t.Fatalf("complete future: %v", err)
	}

	sweep := func(limit int) int {
		t.Helper()
		var n int
		err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
			var err error
			n, err = ExpireCommandsTx(ctx, tx, now, limit)
			return err
		})
		if err != nil {
			t.Fatalf("ExpireCommandsTx(limit=%d): %v", limit, err)
		}
		return n
	}

	// One row at a time, in deadline order.
	if n := sweep(1); n != 1 {
		t.Fatalf("first sweep expired %d, want 1", n)
	}
	first, err := GetCommand(ctx, f.store.DB(), keys[0])
	if err != nil {
		t.Fatalf("GetCommand: %v", err)
	}
	if first.State != CommandStateExpired {
		t.Errorf("the earliest deadline was not expired first: %q", first.State)
	}
	for _, key := range keys[1:] {
		if record, err := GetCommand(ctx, f.store.DB(), key); err != nil {
			t.Fatalf("GetCommand: %v", err)
		} else if record.State != CommandStateSucceeded {
			t.Errorf("command %s = %q, want it left for a later batch", key.CommandID, record.State)
		}
	}
	if n := sweep(1); n != 1 {
		t.Fatalf("second sweep expired %d, want 1", n)
	}
	second, err := GetCommand(ctx, f.store.DB(), keys[1])
	if err != nil {
		t.Fatalf("GetCommand: %v", err)
	}
	if second.State != CommandStateExpired {
		t.Errorf("the second-earliest deadline was not expired next: %q", second.State)
	}
	if n := sweep(2); n != 1 {
		t.Fatalf("third sweep expired %d, want 1 (only one row is still due)", n)
	}
	if n := sweep(2); n != 0 {
		t.Fatalf("fourth sweep expired %d, want 0", n)
	}
	if record, err := GetCommand(ctx, f.store.DB(), futureKey); err != nil {
		t.Fatalf("GetCommand future: %v", err)
	} else if record.State != CommandStateSucceeded {
		t.Errorf("a command whose deadline has not passed = %q, want succeeded", record.State)
	}
}

// TestCommandExpiredTombstoneIsFinal proves the schema's own guard: no statement
// can move an expired record back to an active state, because that would make an
// old key executable a second time. The store has no such path, so the test
// issues the statement by hand — which is exactly the scenario the trigger
// exists for.
func TestCommandExpiredTombstoneIsFinal(t *testing.T) {
	ctx := context.Background()
	f := newCommandFixture(t)
	key := commandKey("runs.create", "rq_tombstone_final")
	hash := commandHash(t, `{"task_id":"t-1"}`)
	claimedAt := commandTime(-48 * time.Hour)
	if _, owner, err := f.claim(key, hash, claimedAt); err != nil || !owner {
		t.Fatalf("claim = owner %v, err %v", owner, err)
	}
	if _, err := f.complete(key, hash, CommandOutcome{
		State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"ok":true}`),
	}, time.Hour, claimedAt); err != nil {
		t.Fatalf("complete: %v", err)
	}
	var expired int
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		expired, err = ExpireCommandsTx(ctx, tx, commandTime(0), 0)
		return err
	})
	if err != nil || expired != 1 {
		t.Fatalf("ExpireCommandsTx = %d, %v; want 1, nil", expired, err)
	}

	for _, stmt := range []string{
		`UPDATE command_records SET state = 'in_flight' WHERE command_id = 'rq_tombstone_final'`,
		`UPDATE command_records SET state = 'succeeded' WHERE command_id = 'rq_tombstone_final'`,
		`UPDATE command_records SET state = 'reconciling', last_error = 'revived' WHERE command_id = 'rq_tombstone_final'`,
	} {
		_, err := f.store.DB().Exec(stmt)
		if err == nil {
			t.Fatalf("%s succeeded, want the trigger to refuse reviving a tombstone", stmt)
		}
		if !strings.Contains(err.Error(), errNameCommandExpiredIsFinal) {
			t.Errorf("%s error = %v, want the RAISE body %q", stmt, err, errNameCommandExpiredIsFinal)
		}
		// The store maps that refusal to the same sentinel a second completion
		// gets, so a caller sees one condition whichever layer caught it.
		if mapped := mapConstraintError("revive command", err); !errors.Is(mapped, ErrCommandAlreadyCompleted) {
			t.Errorf("mapConstraintError(%v) = %v, want ErrCommandAlreadyCompleted", err, mapped)
		}
	}

	// The row is unchanged after every refused statement.
	record, err := GetCommand(ctx, f.store.DB(), key)
	if err != nil {
		t.Fatalf("GetCommand: %v", err)
	}
	if record.State != CommandStateExpired || record.Response != nil || record.StatusCode == nil || *record.StatusCode != 200 {
		t.Errorf("tombstone = %+v, want it exactly as the sweep left it", record)
	}
}

// ---------------------------------------------------------------------------
// Request hash
// ---------------------------------------------------------------------------

// TestCommandRequestHash pins the hash contract of §27.3: method case and object
// key order and whitespace do not change it; path, array order and content do.
func TestCommandRequestHash(t *testing.T) {
	path := "/api/v1/projects/p-1/runs"
	hashOf := func(t *testing.T, method, path, body string) string {
		t.Helper()
		hash, err := CommandRequestHash(method, path, []byte(body))
		if err != nil {
			t.Fatalf("CommandRequestHash(%s, %s, %s): %v", method, path, body, err)
		}
		return hash
	}

	canonical := hashOf(t, "POST", path, `{"task_id":"t-1","budget":{"tokens":50000}}`)

	t.Run("equivalent JSON hashes the same", func(t *testing.T) {
		equivalents := []string{
			`{"budget":{"tokens":50000},"task_id":"t-1"}`,
			"{\n  \"task_id\" : \"t-1\",\n  \"budget\"  : { \"tokens\" : 50000 }\n}\n",
			`{ "budget" : { "tokens" : 50000 } , "task_id" : "t-1" }`,
		}
		for _, body := range equivalents {
			if got := hashOf(t, "POST", path, body); got != canonical {
				t.Errorf("hash of %s = %s, want %s", body, got, canonical)
			}
		}
	})

	t.Run("method case is normalized", func(t *testing.T) {
		if got := hashOf(t, "post", path, `{"task_id":"t-1","budget":{"tokens":50000}}`); got != canonical {
			t.Errorf("lower-case method hash = %s, want %s", got, canonical)
		}
	})

	t.Run("empty body is the empty object", func(t *testing.T) {
		empty := hashOf(t, "POST", path, "")
		if got := hashOf(t, "POST", path, `{}`); got != empty {
			t.Errorf("{} hashes to %s, want the empty body's %s", got, empty)
		}
		if got := hashOf(t, "POST", path, "  \n "); got != empty {
			t.Errorf("whitespace-only body hashes to %s, want the empty body's %s", got, empty)
		}
	})

	t.Run("different inputs hash differently", func(t *testing.T) {
		cases := map[string]string{
			"other path":       hashOf(t, "POST", "/api/v1/projects/p-1/tasks", `{"task_id":"t-1","budget":{"tokens":50000}}`),
			"other value":      hashOf(t, "POST", path, `{"task_id":"t-1","budget":{"tokens":50001}}`),
			"other method":     hashOf(t, "PUT", path, `{"task_id":"t-1","budget":{"tokens":50000}}`),
			"array order":      hashOf(t, "POST", path, `{"tags":["a","b"]}`),
			"array order swap": hashOf(t, "POST", path, `{"tags":["b","a"]}`),
		}
		seen := map[string]string{canonical: "canonical"}
		for name, hash := range cases {
			if hash == canonical {
				t.Errorf("%s hashed to the canonical hash, want a different one", name)
			}
			if prev, dup := seen[hash]; dup {
				t.Errorf("%s and %s hash to the same value %s", name, prev, hash)
			}
			seen[hash] = name
		}
	})

	t.Run("shape of the hash", func(t *testing.T) {
		if !strings.HasPrefix(canonical, "sha256:") || len(canonical) != len("sha256:")+64 {
			t.Errorf("hash = %s, want \"sha256:\" plus 64 hex characters", canonical)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		for _, tc := range []struct {
			name, method, path, body string
		}{
			{"blank method", "", path, `{}`},
			{"blank path", "POST", "  ", `{}`},
			{"malformed body", "POST", path, `{"a":`},
			{"trailing content", "POST", path, `{"a":1}}`},
			{"two documents", "POST", path, `{"a":1} {"b":2}`},
			{"secret in body", "POST", path, `{"password":"hunter2"}`},
		} {
			if _, err := CommandRequestHash(tc.method, tc.path, []byte(tc.body)); err == nil {
				t.Errorf("%s was accepted, want a refusal", tc.name)
			} else if !errors.Is(err, ErrInvalidCommand) && !errors.Is(err, ErrSecretInSnapshot) {
				t.Errorf("%s error = %v, want ErrInvalidCommand or ErrSecretInSnapshot", tc.name, err)
			} else if strings.Contains(err.Error(), "hunter2") {
				t.Errorf("%s error %q leaks the rejected value", tc.name, err)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Key validation
// ---------------------------------------------------------------------------

// TestCommandKeyValidation proves every rule is applied before any SQL runs: an
// unusable key is refused, and the caller's transaction — including any earlier
// work in it — is untouched by the refusal itself.
func TestCommandKeyValidation(t *testing.T) {
	ctx := context.Background()
	f := newCommandFixture(t)
	hash := commandHash(t, `{"task_id":"t-1"}`)
	now := commandTime(0)
	long := strings.Repeat("x", 129)

	cases := []struct {
		name string
		key  CommandKey
	}{
		{"blank principal", CommandKey{PrincipalID: " ", ProjectID: "p-1", Operation: "runs.create", CommandID: "rq-1"}},
		{"blank project", CommandKey{PrincipalID: "principal-1", ProjectID: "", Operation: "runs.create", CommandID: "rq-1"}},
		{"blank operation", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1", Operation: "", CommandID: "rq-1"}},
		{"blank command id", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1", Operation: "runs.create", CommandID: "  "}},
		{"padded principal", CommandKey{PrincipalID: " principal-1", ProjectID: "p-1", Operation: "runs.create", CommandID: "rq-1"}},
		{"padded project", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1 ", Operation: "runs.create", CommandID: "rq-1"}},
		{"padded operation", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1", Operation: " runs.create", CommandID: "rq-1"}},
		{"padded command id", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1", Operation: "runs.create", CommandID: "rq-1 "}},
		{"oversized principal", CommandKey{PrincipalID: long, ProjectID: "p-1", Operation: "runs.create", CommandID: "rq-1"}},
		{"oversized project", CommandKey{PrincipalID: "principal-1", ProjectID: long, Operation: "runs.create", CommandID: "rq-1"}},
		{"oversized operation", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1", Operation: strings.Repeat("a", 65), CommandID: "rq-1"}},
		{"oversized command id", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1", Operation: "runs.create", CommandID: long}},
		{"upper-case operation", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1", Operation: "Runs.Create", CommandID: "rq-1"}},
		{"operation with a slash", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1", Operation: "runs/create", CommandID: "rq-1"}},
		{"operation with a space", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1", Operation: "runs create", CommandID: "rq-1"}},
		{"command id with a space", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1", Operation: "runs.create", CommandID: "rq 1"}},
		{"command id with a newline", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1", Operation: "runs.create", CommandID: "rq\n1"}},
		{"command id with a tab", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1", Operation: "runs.create", CommandID: "rq\t1"}},
		{"non-ASCII command id", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1", Operation: "runs.create", CommandID: "指令-1"}},
		{"command id with a control byte", CommandKey{PrincipalID: "principal-1", ProjectID: "p-1", Operation: "runs.create", CommandID: "rq\x7f1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Claim, read and both transitions must all refuse it, and none of
			// them may write.
			err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				_, _, err := ClaimCommandTx(ctx, tx, tc.key, hash, now)
				return err
			})
			if !errors.Is(err, ErrInvalidCommand) {
				t.Fatalf("ClaimCommandTx = %v, want ErrInvalidCommand", err)
			}
			if _, err := GetCommand(ctx, f.store.DB(), tc.key); !errors.Is(err, ErrInvalidCommand) {
				t.Errorf("GetCommand = %v, want ErrInvalidCommand", err)
			}
			err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				_, err := CompleteCommandTx(ctx, tx, tc.key, hash, CommandOutcome{
					State: CommandStateSucceeded, StatusCode: 200, Response: json.RawMessage(`{"ok":true}`),
				}, 0, now)
				return err
			})
			if !errors.Is(err, ErrInvalidCommand) {
				t.Errorf("CompleteCommandTx = %v, want ErrInvalidCommand", err)
			}
			err = f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
				_, err := MarkCommandReconcilingTx(ctx, tx, tc.key, hash, "unknown", now)
				return err
			})
			if !errors.Is(err, ErrInvalidCommand) {
				t.Errorf("MarkCommandReconcilingTx = %v, want ErrInvalidCommand", err)
			}
			if n := commandRecordCount(t, f.store.DB()); n != 0 {
				t.Errorf("command_records rows = %d, want 0 (nothing may be written for an unusable key)", n)
			}
		})
	}

	// A blank request hash is refused too: it would make every request look like
	// every other one.
	valid := commandKey("runs.create", "rq-valid")
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		_, _, err := ClaimCommandTx(ctx, tx, valid, "  ", now)
		return err
	})
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("claim with a blank hash = %v, want ErrInvalidCommand", err)
	}

	// The accepted spelling of each rule: a dotted, dashed, underscored
	// operation and a printable-ASCII command id.
	if _, owner, err := f.claim(commandKey("runs.create", "rq_01JRUN_CREATE_001.abc-def"), hash, now); err != nil || !owner {
		t.Fatalf("claim with a valid key = owner %v, err %v; want owner true and no error", owner, err)
	}
}

// ---------------------------------------------------------------------------
// Migration 006
// ---------------------------------------------------------------------------

// rawCommandRow is a command_records row written by hand, so a test can build
// the rows the store never would and prove the schema refuses them. It is the
// writer's view of commandRow (the reader's), so the two together cover every
// column the schema has.
type rawCommandRow struct {
	principalID, projectID, operation, commandID, requestHash, state string
	statusCode                                                       any
	responseJSON, resourceType, resourceID, lastError                any
	createdAt, updatedAt                                             int64
	completedAt, expiresAt                                           any
}

// validRawCommandRow is a consistent succeeded row; a test mutates one field to
// make it inconsistent.
func validRawCommandRow() rawCommandRow {
	return rawCommandRow{
		principalID: "principal-1", projectID: "p-1", operation: "runs.create", commandID: "rq-raw",
		requestHash: "sha256:raw", state: "succeeded",
		statusCode: 200, responseJSON: `{"ok":true}`,
		createdAt: 1700000000000, updatedAt: 1700000001000,
		completedAt: 1700000001000, expiresAt: 1700000002000,
	}
}

func (r rawCommandRow) insert(db *sql.DB) error {
	_, err := db.Exec(`
		INSERT INTO command_records (principal_id, project_id, operation, command_id,
		                             request_hash, state, status_code, response_json,
		                             resource_type, resource_id, last_error,
		                             created_at, updated_at, completed_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.principalID, r.projectID, r.operation, r.commandID, r.requestHash, r.state,
		r.statusCode, r.responseJSON, r.resourceType, r.resourceID, r.lastError,
		r.createdAt, r.updatedAt, r.completedAt, r.expiresAt,
	)
	return err
}

// TestCommandMigration006Schema covers §19.4 for migration 006: the table, its
// partial index and its trigger exist; a consistent row is accepted; every
// inconsistent row is refused by the CHECKs; and reopening the database applies
// nothing and keeps the rows.
func TestCommandMigration006Schema(t *testing.T) {
	ctx := context.Background()
	f := newCommandFixture(t)

	for _, name := range []string{"command_records"} {
		if !tableExists(t, f.store.DB(), name) {
			t.Fatalf("table %q does not exist after migration 006", name)
		}
	}
	for _, name := range []string{"idx_command_records_expiry", "trg_command_records_expired_is_final"} {
		var n int
		if err := f.store.DB().QueryRow(
			`SELECT count(*) FROM sqlite_master WHERE name = ?`, name).Scan(&n); err != nil {
			t.Fatalf("query sqlite_master for %q: %v", name, err)
		}
		if n != 1 {
			t.Errorf("schema object %q does not exist", name)
		}
	}

	// The store's own writes are accepted, which proves the CHECKs and the
	// column set agree with the Go side.
	key := commandKey("runs.create", "rq_schema_ok")
	hash := commandHash(t, `{"task_id":"t-1"}`)
	now := commandTime(0)
	if _, owner, err := f.claim(key, hash, now); err != nil || !owner {
		t.Fatalf("claim = owner %v, err %v", owner, err)
	}
	if _, err := f.complete(key, hash, CommandOutcome{
		State: CommandStateSucceeded, StatusCode: 201, Response: json.RawMessage(`{"run_id":"run-1"}`),
		ResourceType: "run", ResourceID: "run-1",
	}, 0, now); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := validRawCommandRow().insert(f.store.DB()); err != nil {
		t.Fatalf("a consistent hand-written row was refused: %v", err)
	}

	// Every row below violates exactly one rule the schema is supposed to hold.
	tooLong := strings.Repeat("x", 129)
	bigResponse := `{"pad":"` + strings.Repeat("x", MaxCommandResponseBytes) + `"}`
	tooLongError := strings.Repeat("e", 513)
	cases := []struct {
		name string
		mut  func(*rawCommandRow)
	}{
		{"in_flight with a status code", func(r *rawCommandRow) {
			r.state, r.statusCode, r.responseJSON, r.completedAt, r.expiresAt = "in_flight", 200, nil, nil, nil
		}},
		{"in_flight with an expiry", func(r *rawCommandRow) { r.state, r.statusCode, r.responseJSON = "in_flight", nil, nil }},
		{"reconciling with a response", func(r *rawCommandRow) {
			r.state, r.statusCode, r.completedAt, r.expiresAt = "reconciling", nil, nil, nil
		}},
		{"succeeded without a status code", func(r *rawCommandRow) { r.statusCode = nil }},
		{"succeeded without a response", func(r *rawCommandRow) { r.responseJSON = nil }},
		{"succeeded without completed_at", func(r *rawCommandRow) { r.completedAt = nil }},
		{"succeeded without expires_at", func(r *rawCommandRow) { r.expiresAt = nil }},
		{"failed without a response", func(r *rawCommandRow) { r.state, r.responseJSON = "failed", nil }},
		{"expired with a response", func(r *rawCommandRow) { r.state = "expired" }},
		{"expired without completed_at", func(r *rawCommandRow) { r.state, r.responseJSON, r.completedAt = "expired", nil, nil }},
		{"unknown state", func(r *rawCommandRow) { r.state = "done" }},
		{"status below 100", func(r *rawCommandRow) { r.statusCode = 99 }},
		{"status above 599", func(r *rawCommandRow) { r.statusCode = 600 }},
		{"blank principal", func(r *rawCommandRow) { r.principalID = "" }},
		{"blank project", func(r *rawCommandRow) { r.projectID = "" }},
		{"blank operation", func(r *rawCommandRow) { r.operation = "" }},
		{"blank command id", func(r *rawCommandRow) { r.commandID = "" }},
		{"blank request hash", func(r *rawCommandRow) { r.requestHash = "" }},
		{"oversized principal", func(r *rawCommandRow) { r.principalID = tooLong }},
		{"oversized project", func(r *rawCommandRow) { r.projectID = tooLong }},
		{"oversized operation", func(r *rawCommandRow) { r.operation = strings.Repeat("a", 65) }},
		{"oversized command id", func(r *rawCommandRow) { r.commandID = tooLong }},
		{"oversized response", func(r *rawCommandRow) { r.responseJSON = bigResponse }},
		{"oversized last_error", func(r *rawCommandRow) { r.lastError = tooLongError }},
		{"resource type without id", func(r *rawCommandRow) { r.resourceType = "run" }},
		{"resource id without type", func(r *rawCommandRow) { r.resourceID = "run-1" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := validRawCommandRow()
			row.commandID = "rq-raw-" + strings.ReplaceAll(tc.name, " ", "_")
			tc.mut(&row)
			err := row.insert(f.store.DB())
			if err == nil {
				t.Fatalf("the schema accepted a %s row: %+v", tc.name, row)
			}
			if !strings.Contains(err.Error(), "constraint") {
				t.Errorf("error = %v, want a constraint refusal", err)
			}
		})
	}

	// Reopen: the recorded version is current, so nothing is applied, and the
	// rows written before the reopen are still there, field for field.
	before, err := GetCommand(ctx, f.store.DB(), key)
	if err != nil {
		t.Fatalf("GetCommand: %v", err)
	}
	if err := f.store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, res, err := OpenStore(ctx, f.path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if len(res.Applied) != 0 {
		t.Errorf("reopen applied %d migrations, want 0", len(res.Applied))
	}
	if res.ToVersion != 6 {
		t.Errorf("reopen ToVersion = %d, want 6", res.ToVersion)
	}
	after, err := GetCommand(ctx, reopened.DB(), key)
	if err != nil {
		t.Fatalf("GetCommand after reopen: %v", err)
	}
	if after.State != before.State || after.RequestHash != before.RequestHash ||
		string(after.Response) != string(before.Response) || *after.StatusCode != *before.StatusCode ||
		!after.ExpiresAt.Equal(*before.ExpiresAt) {
		t.Errorf("record after reopen = %+v, want %+v", after, before)
	}
	if n := commandRecordCount(t, reopened.DB()); n != 2 {
		t.Errorf("command_records rows after reopen = %d, want 2 (one store-written, one raw)", n)
	}
}

// TestCommandStateHelpers pins the small predicates the API layer (T1.11.b)
// branches on, so a change to the state set cannot quietly change what
// "accepted" means.
func TestCommandStateHelpers(t *testing.T) {
	for _, tc := range []struct {
		state              CommandState
		valid, active, end bool
	}{
		{CommandStateInFlight, true, true, false},
		{CommandStateReconciling, true, true, false},
		{CommandStateSucceeded, true, false, true},
		{CommandStateFailed, true, false, true},
		{CommandStateExpired, true, false, true},
		{CommandState("done"), false, false, false},
		{CommandState(""), false, false, false},
	} {
		if got := tc.state.Valid(); got != tc.valid {
			t.Errorf("%q.Valid() = %v, want %v", tc.state, got, tc.valid)
		}
		if got := tc.state.Active(); got != tc.active {
			t.Errorf("%q.Active() = %v, want %v", tc.state, got, tc.active)
		}
		if got := tc.state.Terminal(); got != tc.end {
			t.Errorf("%q.Terminal() = %v, want %v", tc.state, got, tc.end)
		}
	}
}

// TestCommandClaimDeferredTransactionRaceNotAnError drives the claim through a
// plain dbx handle instead of Store.WithTx, so the transaction is BEGIN DEFERRED
// — the one mode OpenStore deliberately does not use (store.go's package comment
// explains why). The store's own handle cannot produce this case, so this is the
// only way to show what a real claim race looks like when the loser reads a stale
// snapshot:
//
//   - both claimers see no row, both try to insert, and SQLite refuses the second
//     one because its snapshot is stale (SQLITE_BUSY, extended code 5, reported
//     as "database is locked" after the busy handler has run out);
//   - the refusal is a BUSY error, not ErrCommandKeyReused and not
//     ErrCommandClaimRaced: the loser has learned nothing about the winner's row,
//     so it must retry, and the retry reads the committed record and does not own
//     the key;
//   - exactly one row exists afterwards.
//
// The point is that correctness never depended on the lock mode: the primary key
// is the arbiter, and the outcome of losing is "retry" — never a second owner and
// never a key that looks reused. That is the same reason the store pins BEGIN
// IMMEDIATE: under immediate the loser blocks at BEGIN, then reads the committed
// row directly and gets owner=false instead of a BUSY failure
// (TestCommandClaimConcurrentSameHashSingleOwner asserts that, with zero BUSY
// errors).
func TestCommandClaimDeferredTransactionRaceNotAnError(t *testing.T) {
	ctx := context.Background()

	// A handle on its own file, opened WITHOUT dbx.WithTxLock: its transactions
	// are deferred, exactly like a handle that ignored the store's locking
	// contract. Its own store handle is closed before the test returns, so the
	// temp directory can be removed.
	path := filepath.Join(t.TempDir(), "deferred.db")
	seed, _, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatalf("seed the deferred database: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close the seeding handle: %v", err)
	}
	raw, err := dbx.Open(path)
	if err != nil {
		t.Fatalf("dbx.Open(%s): %v", path, err)
	}
	defer raw.Close()
	// A short busy timeout: the loser's refusal must be observed as the stale
	// snapshot it is, not as five seconds of waiting followed by the same error.
	if _, err := raw.ExecContext(ctx, `PRAGMA busy_timeout = 200`); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}

	key := commandKey("runs.create", "rq_deferred_race")
	hash := commandHash(t, `{"task_id":"t-1"}`)
	now := commandTime(0)

	// A: claim and pause, holding its snapshot.
	txA, err := raw.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin A: %v", err)
	}
	defer txA.Rollback()
	if _, owner, err := ClaimCommandTx(ctx, txA, key, hash, now); err != nil || !owner {
		t.Fatalf("A claim = owner %v, err %v; want the first claim to own the key", owner, err)
	}

	// B: the same claim, while A is still open. Its snapshot predates A's row,
	// so the insert cannot be applied to it.
	txB, err := raw.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin B: %v", err)
	}
	_, owner, claimErr := ClaimCommandTx(ctx, txB, key, hash, now)
	if claimErr == nil {
		t.Fatalf("B claim succeeded while A held an uncommitted claim (owner=%v), want a refusal", owner)
	}
	if errors.Is(claimErr, ErrCommandKeyReused) || errors.Is(claimErr, ErrCommandClaimRaced) || errors.Is(claimErr, ErrNotFound) {
		t.Errorf("B claim = %v, want a plain BUSY refusal: B has seen nothing of A's row", claimErr)
	}
	if !strings.Contains(claimErr.Error(), "database is locked") {
		t.Errorf("B claim = %v, want the SQLite busy refusal", claimErr)
	}
	t.Logf("EVIDENCE B claim refused with %v (extended code %d)", claimErr, sqliteExtended(claimErr))
	_ = txB.Rollback()

	// A commits: the key is taken, by A, once.
	if err := txA.Commit(); err != nil {
		t.Fatalf("commit A: %v", err)
	}

	// The retry reads A's committed record and does not own the key.
	store := NewStore(raw)
	record, owner, err := claimWith(ctx, store, key, hash, now)
	if err != nil {
		t.Fatalf("claim after the race: %v", err)
	}
	if owner {
		t.Fatal("claim after the race reported owner=true, want the committed record")
	}
	if record.State != CommandStateInFlight || record.RequestHash != hash {
		t.Errorf("record after the race = %+v, want A's in_flight record", record)
	}
	var n int
	if err := raw.QueryRowContext(ctx, `SELECT COUNT(*) FROM command_records WHERE command_id = ?`, key.CommandID).Scan(&n); err != nil {
		t.Fatalf("count command_records: %v", err)
	}
	if n != 1 {
		t.Errorf("command_records rows = %d, want 1 (the primary key is the arbiter)", n)
	}
}
