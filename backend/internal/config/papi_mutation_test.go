// Package config - PAPI mutation pipeline tests (T13.03.b): service-level
// mutation lock, candidate snapshot, single transaction, publish-after-commit.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openPAPITestService(t *testing.T) (*SQLiteConfigService, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "papi_mutation.db")
	svc, err := NewSQLiteConfigService(dbPath)
	require.NoError(t, err)
	return svc, dbPath
}

func reopenPAPITestService(t *testing.T, dbPath string) *SQLiteConfigService {
	t.Helper()
	svc, err := NewSQLiteConfigService(dbPath)
	require.NoError(t, err)
	return svc
}

func countPAPIRows(t *testing.T, svc *SQLiteConfigService) int {
	t.Helper()
	var n int
	require.NoError(t, svc.db.QueryRow("SELECT COUNT(*) FROM papi_variables").Scan(&n))
	return n
}

// A persistence failure inside the mutation pipeline publishes nothing: the
// visible snapshot and the stored rows both stay exactly as they were (E-09).
func TestPAPIMutationPersistFailureKeepsVisibleSnapshot(t *testing.T) {
	svc, dbPath := openPAPITestService(t)
	defer svc.Close()

	require.NoError(t, svc.DefinePAPIVariable(&PAPIVariable{
		Name: "BASE_VAR", Model: "m1", Category: []string{"backend"},
	}))
	// A second variable keeps every later mutation non-empty, so the injected
	// INSERT failure below fires even for a delete (an empty candidate would
	// insert nothing and never touch the trigger).
	require.NoError(t, svc.DefinePAPIVariable(&PAPIVariable{
		Name: "KEEP_VAR", Model: "k1", Category: []string{"docs"},
	}))

	// Mid-transaction fault: the full-snapshot replace has already executed
	// its DELETE when the first INSERT aborts, so the transaction must roll
	// back for the stored rows to survive.
	_, err := svc.db.Exec(`
CREATE TRIGGER fail_papi_insert
BEFORE INSERT ON papi_variables
BEGIN SELECT RAISE(ABORT, 'injected papi write failure'); END;`)
	require.NoError(t, err)

	// Create fails and stays invisible.
	err = svc.DefinePAPIVariable(&PAPIVariable{Name: "NEW_VAR", Model: "m9", Category: []string{"other"}})
	require.Error(t, err)
	_, getErr := svc.GetPAPIManager().GetVariable("NEW_VAR")
	assert.Error(t, getErr, "failed create must not be visible")

	// Update (hot swap) fails and the old value stays visible.
	swapErr := svc.HotSwapPAPI("BASE_VAR", &PAPIVariable{Model: "m2", Category: []string{"backend"}})
	require.Error(t, swapErr)
	cur, err := svc.GetPAPIManager().GetVariable("BASE_VAR")
	require.NoError(t, err)
	assert.Equal(t, "m1", cur.Model, "failed hot swap must keep the old visible value")

	// Delete fails and the variable stays visible.
	delErr := svc.DeletePAPIVariable("BASE_VAR")
	require.Error(t, delErr)
	_, getErr = svc.GetPAPIManager().GetVariable("BASE_VAR")
	assert.NoError(t, getErr, "failed delete must not remove the visible variable")

	// The stored rows are untouched by every failed mutation above.
	assert.Equal(t, 2, countPAPIRows(t, svc))
	assert.Len(t, svc.GetPAPIManager().ListVariables(), 2)

	// Reopen against the same file: only the original variables load.
	reopened := reopenPAPITestService(t, dbPath)
	loaded, err := reopened.GetPAPIManager().GetVariable("BASE_VAR")
	require.NoError(t, err)
	assert.Equal(t, "m1", loaded.Model)
	assert.Len(t, reopened.GetPAPIManager().ListVariables(), 2)
	require.NoError(t, reopened.Close())

	// The service recovers once the fault is cleared.
	_, err = svc.db.Exec(`DROP TRIGGER fail_papi_insert`)
	require.NoError(t, err)
	require.NoError(t, svc.HotSwapPAPI("BASE_VAR", &PAPIVariable{Model: "m2", Category: []string{"backend"}}))
	cur, err = svc.GetPAPIManager().GetVariable("BASE_VAR")
	require.NoError(t, err)
	assert.Equal(t, "m2", cur.Model)

	reopened = reopenPAPITestService(t, dbPath)
	loaded, err = reopened.GetPAPIManager().GetVariable("BASE_VAR")
	require.NoError(t, err)
	assert.Equal(t, "m2", loaded.Model)
	require.NoError(t, reopened.Close())
}

// A new write that would claim an already-taken (normalized) category is
// rejected with a typed *CategoryConflictError, deterministically, and writes
// nothing (E-10). The HTTP 409 mapping is wired in T13.03.c; here the typed
// error itself must be discernible.
func TestPAPINewCategoryConflictIsTypedDeterministicAndNotWritten(t *testing.T) {
	svc, dbPath := openPAPITestService(t)
	defer svc.Close()

	require.NoError(t, svc.DefinePAPIVariable(&PAPIVariable{
		Name: "AAA_FRONT", Model: "m1", Category: []string{"Backend"},
	}))

	var first string
	for i := 0; i < 50; i++ {
		err := svc.DefinePAPIVariable(&PAPIVariable{
			Name: "ZZZ_BACK", Model: "m2", Category: []string{" backend "},
		})
		require.Error(t, err)
		var conflict *CategoryConflictError
		require.True(t, errors.As(err, &conflict), "iteration %d: expected *CategoryConflictError, got %T (%v)", i, err, err)
		assert.Equal(t, "backend", conflict.Category)
		assert.Equal(t, []string{"AAA_FRONT", "ZZZ_BACK"}, conflict.Variables)
		if first == "" {
			first = err.Error()
		} else {
			assert.Equal(t, first, err.Error(), "iteration %d: conflict error must be deterministic", i)
		}
		_, getErr := svc.GetPAPIManager().GetVariable("ZZZ_BACK")
		assert.Error(t, getErr, "iteration %d: rejected write must stay invisible", i)
	}
	assert.Len(t, svc.GetPAPIManager().ListVariables(), 1)
	assert.Equal(t, 1, countPAPIRows(t, svc))

	// A hot swap that would claim another variable's category is the same
	// typed rejection and changes nothing.
	require.NoError(t, svc.DefinePAPIVariable(&PAPIVariable{
		Name: "C_VAR", Model: "m3", Category: []string{"docs"},
	}))
	swapErr := svc.HotSwapPAPI("C_VAR", &PAPIVariable{Model: "m4", Category: []string{"BACKEND"}})
	var conflict *CategoryConflictError
	require.True(t, errors.As(swapErr, &conflict), "expected *CategoryConflictError, got %T (%v)", swapErr, swapErr)
	assert.Equal(t, "backend", conflict.Category)
	assert.Equal(t, []string{"AAA_FRONT", "C_VAR"}, conflict.Variables)
	cur, err := svc.GetPAPIManager().GetVariable("C_VAR")
	require.NoError(t, err)
	assert.Equal(t, "m3", cur.Model)
	assert.Equal(t, []string{"docs"}, cur.Category)

	// The write path normalized categories before persisting them.
	reopened := reopenPAPITestService(t, dbPath)
	defer reopened.Close()
	loaded, err := reopened.GetPAPIManager().GetVariable("AAA_FRONT")
	require.NoError(t, err)
	assert.Equal(t, []string{"backend"}, loaded.Category)
	assert.Equal(t, 2, countPAPIRows(t, reopened))
}

// Concurrent mutations serialize through the pipeline: after both writers
// finish, the in-memory snapshot equals the snapshot a fresh service loads
// from the same database file (E-09).
func TestConcurrentPAPIMutationsMatchReopenedState(t *testing.T) {
	svc, dbPath := openPAPITestService(t)

	require.NoError(t, svc.DefinePAPIVariable(&PAPIVariable{
		Name: "BASE", Model: "m0", Category: []string{"base"},
	}))

	const iterations = 20
	var wg sync.WaitGroup
	errs := make([]error, 2*iterations)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			errs[i] = svc.DefinePAPIVariable(&PAPIVariable{
				Name:     fmt.Sprintf("G1_VAR_%02d", i),
				Model:    fmt.Sprintf("g1-model-%d", i),
				Category: []string{fmt.Sprintf("g1cat-%d", i)},
			})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			errs[iterations+i] = svc.HotSwapPAPI("BASE", &PAPIVariable{
				Model:    fmt.Sprintf("base-model-%d", i),
				Category: []string{"base"},
			})
		}
	}()
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, "mutation %d", i)
	}

	// Snapshot the visible state, then compare it against a fresh service
	// reading the same file.
	beforeClose := svc.GetPAPIManager().GetMapping()
	require.NoError(t, svc.Close())

	reopened := reopenPAPITestService(t, dbPath)
	defer reopened.Close()
	afterReopen := reopened.GetPAPIManager().GetMapping()

	require.Len(t, afterReopen.Variables, iterations+1)
	names := make([]string, 0, len(beforeClose.Variables))
	for name := range beforeClose.Variables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		before := beforeClose.Variables[name]
		after := afterReopen.Variables[name]
		require.NotNil(t, after, "variable %s missing after reopen", name)
		assert.Equal(t, before.Model, after.Model, "variable %s model", name)
		assert.Equal(t, before.Category, after.Category, "variable %s category", name)
		assert.Equal(t, before.MCPTools, after.MCPTools, "variable %s tools", name)
	}
	// Whichever hot swap won the serialization is the one that persisted.
	assert.Contains(t, beforeClose.Variables["BASE"].Model, "base-model-")
}

// Persisted data from before the category rule existed still loads: the
// conflict is recorded in diagnostics, nothing is deleted, resolution of a
// conflicted category is a typed deterministic error instead of a random
// pick, and non-conflicted categories keep resolving (E-10).
func TestPAPILoadKeepsLegacyConflictsWithDiagnosticsAndTypedResolve(t *testing.T) {
	svc, dbPath := openPAPITestService(t)

	seed := func(v PAPIVariable) {
		payload, err := json.Marshal(&v)
		require.NoError(t, err)
		_, err = svc.db.Exec("INSERT INTO papi_variables (name, variable_json) VALUES (?, ?)", v.Name, string(payload))
		require.NoError(t, err)
	}
	seed(PAPIVariable{Name: "LEGACY_ONE", Model: "m1", Category: []string{"Backend"}})
	seed(PAPIVariable{Name: "LEGACY_TWO", Model: "m2", Category: []string{" backend "}})
	seed(PAPIVariable{Name: "CLEAN_VAR", Model: "m3", Category: []string{"docs"}})
	require.NoError(t, svc.Close())

	reopened := reopenPAPITestService(t, dbPath)
	defer reopened.Close()

	// Diagnostics name the conflict; no variable was dropped on load.
	diagnostics := reopened.PAPIDiagnostics()
	require.NotEmpty(t, diagnostics)
	joined := fmt.Sprint(diagnostics)
	assert.Contains(t, joined, "backend")
	assert.Contains(t, joined, "LEGACY_ONE")
	assert.Contains(t, joined, "LEGACY_TWO")
	assert.Len(t, reopened.GetPAPIManager().ListVariables(), 3)
	assert.Equal(t, 3, countPAPIRows(t, reopened))
	assert.NotEmpty(t, reopened.GetPAPIManager().DetectConflicts())

	// The load path applied the normalization rule to stored categories.
	one, err := reopened.GetPAPIManager().GetVariable("LEGACY_ONE")
	require.NoError(t, err)
	assert.Equal(t, []string{"backend"}, one.Category)

	// Resolving the conflicted category is a typed, deterministic conflict —
	// never a random choice between the claimants.
	var first string
	for i := 0; i < 50; i++ {
		_, err := reopened.GetPAPIManager().ResolveByCategory("Backend")
		require.Error(t, err)
		var conflict *CategoryConflictError
		require.True(t, errors.As(err, &conflict), "iteration %d: expected *CategoryConflictError, got %T (%v)", i, err, err)
		assert.Equal(t, "backend", conflict.Category)
		assert.Equal(t, []string{"LEGACY_ONE", "LEGACY_TWO"}, conflict.Variables)
		if first == "" {
			first = err.Error()
		} else {
			assert.Equal(t, first, err.Error(), "iteration %d: resolve conflict must be deterministic", i)
		}
	}

	// Non-conflicted categories resolve normally alongside the conflict.
	resolved, err := reopened.GetPAPIManager().ResolveByCategory("docs")
	require.NoError(t, err)
	assert.Equal(t, "CLEAN_VAR", resolved.Name)
}

// CRUD writes persist the normalized snapshot: messy category input is stored
// in canonical form, and an upsert that moves a category moves its resolution.
func TestPAPIMutationPersistsNormalizedCategories(t *testing.T) {
	svc, dbPath := openPAPITestService(t)
	defer svc.Close()

	require.NoError(t, svc.DefinePAPIVariable(&PAPIVariable{
		Name: "NORM_VAR", Model: "m1", Category: []string{" Backend ", "BACKEND", ""},
	}))
	cur, err := svc.GetPAPIManager().GetVariable("NORM_VAR")
	require.NoError(t, err)
	assert.Equal(t, []string{"backend"}, cur.Category)

	// Upsert moves the variable to a different category.
	require.NoError(t, svc.DefinePAPIVariable(&PAPIVariable{
		Name: "NORM_VAR", Model: "m2", Category: []string{"Frontend"},
	}))
	_, err = svc.GetPAPIManager().ResolveByCategory("backend")
	assert.Error(t, err, "old category must stop resolving after the upsert")
	resolved, err := svc.GetPAPIManager().ResolveByCategory("FRONTEND")
	require.NoError(t, err)
	assert.Equal(t, "NORM_VAR", resolved.Name)
	assert.Equal(t, "m2", resolved.Model)

	reopened := reopenPAPITestService(t, dbPath)
	defer reopened.Close()
	loaded, err := reopened.GetPAPIManager().GetVariable("NORM_VAR")
	require.NoError(t, err)
	assert.Equal(t, []string{"frontend"}, loaded.Category)
	assert.Equal(t, "m2", loaded.Model)
	assert.Equal(t, 1, countPAPIRows(t, reopened))
}
