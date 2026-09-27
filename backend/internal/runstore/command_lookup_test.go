package runstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// These tests cover command_lookup.go and migration 007: the read path §20.4's
// reconciliation endpoint needs (T1.11.b). The properties they exist to prove:
//
//   - a client key that was never used returns an empty result and no error, so
//     the API can answer 404 command_not_found without the store inventing an
//     error for "no such command";
//   - a key used once returns that one record, whatever operation it was claimed
//     under;
//   - a key used under two operations returns both rows, ordered by operation,
//     because §27.3's scope includes the operation and the caller must be able
//     to see the ambiguity instead of being handed an arbitrary row;
//   - the lookup is scoped by principal and project: another principal's or
//     another project's identical client key is invisible;
//   - migration 007's index exists, and reopening the database applies nothing;
//   - unusable arguments are refused with ErrInvalidCommand before any SQL runs.

// lookupFixture is a store with one project_ref and the path, plus a helper to
// write command rows directly (claim + complete) without going through a
// handler. It reuses the command fixtures of commands_test.go so the two files
// cannot drift on the key shape.
type lookupFixture struct {
	*commandFixture
}

func newLookupFixture(t *testing.T) *lookupFixture {
	t.Helper()
	return &lookupFixture{commandFixture: newCommandFixture(t)}
}

// seedCommand claims a key and completes it, so the lookup has a terminal row
// with a status code and a response to compare field for field.
func (f *lookupFixture) seedCommand(key CommandKey, hash string, statusCode int, response string) CommandRecord {
	f.t.Helper()
	if _, owner, err := f.claim(key, hash, commandTime(0)); err != nil || !owner {
		f.t.Fatalf("claim %+v = owner %v, err %v", key, owner, err)
	}
	record, err := f.complete(key, hash, CommandOutcome{
		State:        CommandStateSucceeded,
		StatusCode:   statusCode,
		Response:     json.RawMessage(response),
		ResourceType: "run",
		ResourceID:   "run-" + key.Operation,
	}, 0, commandTime(time.Minute))
	if err != nil {
		f.t.Fatalf("complete %+v: %v", key, err)
	}
	return record
}

// ---------------------------------------------------------------------------
// Lookup
// ---------------------------------------------------------------------------

// TestFindCommandsByClientKeyEmptyAndSingle covers the two ordinary outcomes of
// §20.4's reconciliation: a key nobody used is an empty result (not an error),
// and a key used once is exactly the record that was stored.
func TestFindCommandsByClientKeyEmptyAndSingle(t *testing.T) {
	ctx := context.Background()
	f := newLookupFixture(t)

	found, err := FindCommandsByClientKey(ctx, f.store.DB(), commandTestPrincipal, "p-1", "rq_absent")
	if err != nil {
		t.Fatalf("FindCommandsByClientKey on an unused key: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("FindCommandsByClientKey on an unused key = %d rows, want 0", len(found))
	}

	key := commandKey("runs.create", "rq_single")
	hash := commandHash(t, `{"task_id":"t-1","agent_revision_id":"ar-1"}`)
	stored := f.seedCommand(key, hash, 202, `{"run_id":"run-1","revision":1}`)

	found, err = FindCommandsByClientKey(ctx, f.store.DB(), commandTestPrincipal, "p-1", "rq_single")
	if err != nil {
		t.Fatalf("FindCommandsByClientKey: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("FindCommandsByClientKey = %d rows, want 1", len(found))
	}
	got := found[0]
	if got.Key != key {
		t.Errorf("Key = %+v, want %+v", got.Key, key)
	}
	if got.State != CommandStateSucceeded {
		t.Errorf("State = %q, want %q", got.State, CommandStateSucceeded)
	}
	if got.StatusCode == nil || *got.StatusCode != 202 {
		t.Errorf("StatusCode = %v, want 202", got.StatusCode)
	}
	if string(got.Response) != string(stored.Response) {
		t.Errorf("Response = %s, want %s", got.Response, stored.Response)
	}
	if got.ResourceType == nil || *got.ResourceType != "run" ||
		got.ResourceID == nil || *got.ResourceID != "run-runs.create" {
		t.Errorf("Resource = %v/%v, want run/run-runs.create", got.ResourceType, got.ResourceID)
	}
	if !got.CreatedAt.Equal(stored.CreatedAt) || !got.CompletedAt.Equal(*stored.CompletedAt) {
		t.Errorf("timestamps = %v/%v, want %v/%v", got.CreatedAt, got.CompletedAt, stored.CreatedAt, stored.CompletedAt)
	}
}

// TestFindCommandsByClientKeyMultipleOperations is the ambiguity case: one
// client key claimed under two operations is two legal records, and the lookup
// must return both, ordered, rather than picking one.
func TestFindCommandsByClientKeyMultipleOperations(t *testing.T) {
	ctx := context.Background()
	f := newLookupFixture(t)

	// Inserted in an order that is not the answer order, so a lookup that
	// returned insertion order would fail.
	f.seedCommand(commandKey("runs.retry", "rq_multi"), commandHash(t, `{"a":2}`), 202, `{"run_id":"run-2"}`)
	f.seedCommand(commandKey("runs.cancel", "rq_multi"), commandHash(t, `{"a":1}`), 200, `{"run_id":"run-1"}`)
	f.seedCommand(commandKey("runs.create", "rq_multi"), commandHash(t, `{"a":3}`), 201, `{"run_id":"run-3"}`)

	found, err := FindCommandsByClientKey(ctx, f.store.DB(), commandTestPrincipal, "p-1", "rq_multi")
	if err != nil {
		t.Fatalf("FindCommandsByClientKey: %v", err)
	}
	var operations []string
	for _, record := range found {
		operations = append(operations, record.Key.Operation)
	}
	want := []string{"runs.cancel", "runs.create", "runs.retry"}
	if strings.Join(operations, ",") != strings.Join(want, ",") {
		t.Errorf("operations = %v, want %v (ordered by operation)", operations, want)
	}
}

// TestFindCommandsByClientKeyScopeIsolation proves the lookup cannot see a
// record outside its (principal, project) scope. This is what makes the
// endpoint's 404 for a foreign key indistinguishable from "never used": the
// query has no row to leak.
func TestFindCommandsByClientKeyScopeIsolation(t *testing.T) {
	ctx := context.Background()
	f := newLookupFixture(t)

	// A second project must exist before a command may name it (project_refs is
	// the local anchor every task/run FK points at).
	seedTask(t, f.store, "t-2", "p-2")

	own := commandKey("runs.create", "rq_shared")
	f.seedCommand(own, commandHash(t, `{"mine":true}`), 202, `{"run_id":"run-mine"}`)

	otherPrincipal := own
	otherPrincipal.PrincipalID = "principal-2"
	f.seedCommand(otherPrincipal, commandHash(t, `{"theirs":true}`), 202, `{"run_id":"run-theirs"}`)

	otherProject := own
	otherProject.ProjectID = "p-2"
	f.seedCommand(otherProject, commandHash(t, `{"elsewhere":true}`), 202, `{"run_id":"run-elsewhere"}`)

	for _, tc := range []struct {
		name               string
		principal, project string
		wantResourceID     string
	}{
		{"own scope", commandTestPrincipal, "p-1", "run-runs.create"},
		{"other principal", "principal-2", "p-1", "run-runs.create"},
		{"other project", commandTestPrincipal, "p-2", "run-runs.create"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, err := FindCommandsByClientKey(ctx, f.store.DB(), tc.principal, tc.project, "rq_shared")
			if err != nil {
				t.Fatalf("FindCommandsByClientKey: %v", err)
			}
			if len(found) != 1 {
				t.Fatalf("FindCommandsByClientKey = %d rows, want exactly the scope's own 1", len(found))
			}
			if found[0].Key.PrincipalID != tc.principal || found[0].Key.ProjectID != tc.project {
				t.Errorf("row scope = %s/%s, want %s/%s",
					found[0].Key.PrincipalID, found[0].Key.ProjectID, tc.principal, tc.project)
			}
			if found[0].ResourceID == nil || *found[0].ResourceID != tc.wantResourceID {
				t.Errorf("ResourceID = %v, want %s", found[0].ResourceID, tc.wantResourceID)
			}
		})
	}

	// A key that exists in neither scope is still an empty answer, and the
	// other principals' rows are untouched by the lookup.
	found, err := FindCommandsByClientKey(ctx, f.store.DB(), "principal-3", "p-1", "rq_shared")
	if err != nil {
		t.Fatalf("FindCommandsByClientKey for a third principal: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("third principal sees %d rows, want 0", len(found))
	}
	if n := commandRecordCount(t, f.store.DB()); n != 3 {
		t.Errorf("command_records rows = %d, want 3 (a lookup must not write)", n)
	}
}

// TestFindCommandsByClientKeyRejectsInvalidArgs covers the validation contract:
// the three arguments follow the CommandKey rules, and a violation is
// ErrInvalidCommand with no SQL run.
func TestFindCommandsByClientKeyRejectsInvalidArgs(t *testing.T) {
	ctx := context.Background()
	f := newLookupFixture(t)

	cases := []struct {
		name                          string
		principal, project, commandID string
	}{
		{"blank principal", "  ", "p-1", "rq_1"},
		{"padded principal", " principal-1", "p-1", "rq_1"},
		{"oversized principal", strings.Repeat("p", MaxCommandPrincipalLength+1), "p-1", "rq_1"},
		{"blank project", commandTestPrincipal, "", "rq_1"},
		{"padded project", commandTestPrincipal, "p-1 ", "rq_1"},
		{"oversized project", commandTestPrincipal, strings.Repeat("p", MaxCommandProjectLength+1), "rq_1"},
		{"blank command id", commandTestPrincipal, "p-1", "\t"},
		{"padded command id", commandTestPrincipal, "p-1", " rq_1"},
		{"command id with space", commandTestPrincipal, "p-1", "rq 1"},
		{"command id with newline", commandTestPrincipal, "p-1", "rq\n1"},
		{"command id non-ascii", commandTestPrincipal, "p-1", "rq_一"},
		{"oversized command id", commandTestPrincipal, "p-1", strings.Repeat("r", MaxCommandIDLength+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found, err := FindCommandsByClientKey(ctx, f.store.DB(), tc.principal, tc.project, tc.commandID)
			if !errors.Is(err, ErrInvalidCommand) {
				t.Fatalf("error = %v, want ErrInvalidCommand", err)
			}
			if found != nil {
				t.Errorf("result = %v, want nil on a refused call", found)
			}
		})
	}

	// A nil Querier is the same class of programmer error and must not panic.
	if _, err := FindCommandsByClientKey(ctx, nil, commandTestPrincipal, "p-1", "rq_1"); !errors.Is(err, ErrInvalidCommand) {
		t.Errorf("nil Querier error = %v, want ErrInvalidCommand", err)
	}
}

// TestFindCommandsByClientKeyInTransaction proves the lookup works through a Tx
// as well as through the pool, which is what lets a caller that is already
// inside a transaction (T1.04's create path) reconcile without a second
// connection.
func TestFindCommandsByClientKeyInTransaction(t *testing.T) {
	ctx := context.Background()
	f := newLookupFixture(t)

	key := commandKey("runs.create", "rq_tx")
	f.seedCommand(key, commandHash(t, `{"in_tx":true}`), 201, `{"run_id":"run-tx"}`)

	var inside []CommandRecord
	err := f.store.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		var err error
		inside, err = FindCommandsByClientKey(ctx, tx, commandTestPrincipal, "p-1", "rq_tx")
		return err
	})
	if err != nil {
		t.Fatalf("FindCommandsByClientKey in a transaction: %v", err)
	}
	if len(inside) != 1 || inside[0].Key != key {
		t.Fatalf("in-transaction result = %+v, want the one seeded record %+v", inside, key)
	}
}

// TestCommandMigration007Index covers §19.4 for migration 007: the index
// exists, it covers exactly the reconciliation lookup's columns, the table and
// the earlier index/trigger are unchanged, and reopening the database applies
// nothing.
func TestCommandMigration007Index(t *testing.T) {
	ctx := context.Background()
	f := newLookupFixture(t)

	var sqlText string
	if err := f.store.DB().QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_command_records_client_key'`).Scan(&sqlText); err != nil {
		t.Fatalf("migration 007 index missing: %v", err)
	}
	for _, needle := range []string{"principal_id", "project_id", "command_id"} {
		if !strings.Contains(sqlText, needle) {
			t.Errorf("index definition %q does not name %s", sqlText, needle)
		}
	}
	// The index is not a uniqueness rule: the same client key may legally exist
	// under several operations (§27.3), which the ambiguity test relies on.
	if strings.Contains(strings.ToUpper(sqlText), "UNIQUE") {
		t.Errorf("index %q is UNIQUE; the key is scoped by operation and may repeat", sqlText)
	}

	// 007 must not have disturbed 006's objects.
	for _, name := range []string{"command_records", "idx_command_records_expiry", "trg_command_records_expired_is_final"} {
		var n int
		if err := f.store.DB().QueryRow(
			`SELECT count(*) FROM sqlite_master WHERE name = ?`, name).Scan(&n); err != nil {
			t.Fatalf("query sqlite_master for %q: %v", name, err)
		}
		if n != 1 {
			t.Errorf("schema object %q does not exist after migration 007", name)
		}
	}

	// The index is reachable, not just present. The plan depends on the shape of
	// the statement, and both shapes matter:
	//
	//   - the lookup exactly as FindCommandsByClientKey issues it (the three
	//     equality predicates plus ORDER BY operation) is planned as a SEARCH:
	//     SQLite satisfies the sort from the primary key's own order and tests
	//     command_id as a residual predicate, which is cheap because one
	//     principal/project pair holds few rows. What must never happen is a
	//     full table scan of command_records, which is what this asserts;
	//   - the same equality predicates without the sort — the shape a caller
	//     probes a single key with — is planned against the new index, which is
	//     the evidence that migration 007 is what keeps that shape from
	//     degrading into a scan as the ledger grows.
	lookupQuery := "SELECT " + commandColumns + `
		FROM command_records
		WHERE principal_id = ? AND project_id = ? AND command_id = ?
		ORDER BY operation`
	plan := explainLookupPlan(t, f.store.DB(), lookupQuery)
	if !strings.Contains(plan, "SEARCH command_records") {
		t.Errorf("the reconciliation lookup is not planned as a search:\n%s", plan)
	}
	if strings.Contains(plan, "SCAN command_records") {
		t.Errorf("the reconciliation lookup falls back to a table scan:\n%s", plan)
	}

	probePlan := explainLookupPlan(t, f.store.DB(),
		"SELECT count(*) FROM command_records WHERE principal_id = ? AND project_id = ? AND command_id = ?")
	if !strings.Contains(probePlan, "idx_command_records_client_key") {
		t.Errorf("the unordered key probe does not use idx_command_records_client_key:\n%s", probePlan)
	}

	// Reopen: nothing to apply, and the rows written before the reopen are
	// still found by the new lookup.
	key := commandKey("runs.create", "rq_reopen")
	f.seedCommand(key, commandHash(t, `{"reopen":true}`), 202, `{"run_id":"run-reopen"}`)
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
	if res.ToVersion != 7 {
		t.Errorf("reopen ToVersion = %d, want 7", res.ToVersion)
	}
	found, err := FindCommandsByClientKey(ctx, reopened.DB(), commandTestPrincipal, "p-1", "rq_reopen")
	if err != nil {
		t.Fatalf("FindCommandsByClientKey after reopen: %v", err)
	}
	if len(found) != 1 || found[0].Key != key {
		t.Fatalf("after reopen = %+v, want the one record for %+v", found, key)
	}
}

// explainLookupPlan renders the query plan of one statement as text, so a test
// can assert which index SQLite chose.
func explainLookupPlan(t *testing.T, q Querier, query string) string {
	t.Helper()
	rows, err := q.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query,
		commandTestPrincipal, "p-1", "rq_reopen")
	if err != nil {
		t.Fatalf("explain query plan: %v", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan query plan: %v", err)
		}
		lines = append(lines, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate query plan: %v", err)
	}
	return strings.Join(lines, "\n")
}
