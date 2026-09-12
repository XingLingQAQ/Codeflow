package skill

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// registrySnapshot captures the visible state of one skill for comparison
// across a failed update and a close/reopen cycle.
type registrySnapshot struct {
	body     string
	enabled  bool
	rowIDs   []int64
	verBody  []string
	archived []int64 // ArchivedAt UnixMilli, aligned with rowIDs
}

func captureSkillState(t *testing.T, r *InMemoryRegistry, id string) registrySnapshot {
	t.Helper()
	ctx := context.Background()
	cur, err := r.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	vers, err := r.ListVersions(ctx, id)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	snap := registrySnapshot{body: cur.Body, enabled: cur.Enabled}
	// ListVersions is newest-first; record oldest-first for stable comparison.
	for i := len(vers) - 1; i >= 0; i-- {
		snap.rowIDs = append(snap.rowIDs, vers[i].RowID)
		snap.verBody = append(snap.verBody, vers[i].Skill.Body)
		snap.archived = append(snap.archived, vers[i].ArchivedAt.UnixMilli())
	}
	return snap
}

func assertSkillState(t *testing.T, r *InMemoryRegistry, id string, want registrySnapshot) {
	t.Helper()
	got := captureSkillState(t, r, id)
	if got.body != want.body || got.enabled != want.enabled {
		t.Fatalf("current state = body %q enabled %v, want body %q enabled %v", got.body, got.enabled, want.body, want.enabled)
	}
	if len(got.rowIDs) != len(want.rowIDs) {
		t.Fatalf("history length = %d (%v), want %d (%v)", len(got.rowIDs), got.rowIDs, len(want.rowIDs), want.rowIDs)
	}
	for i := range want.rowIDs {
		if got.rowIDs[i] != want.rowIDs[i] || got.verBody[i] != want.verBody[i] || got.archived[i] != want.archived[i] {
			t.Fatalf("history[%d] = row %d body %q at %d, want row %d body %q at %d",
				i, got.rowIDs[i], got.verBody[i], got.archived[i], want.rowIDs[i], want.verBody[i], want.archived[i])
		}
	}
}

func openSQLiteRegistryForTxTest(t *testing.T) (*InMemoryRegistry, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "skills_registry_tx.db")
	r, err := NewSQLiteRegistry(dbPath)
	if err != nil {
		t.Fatalf("open sqlite registry: %v", err)
	}
	return r, dbPath
}

func installTrigger(t *testing.T, r *InMemoryRegistry, name, ddl string) {
	t.Helper()
	if _, err := r.store.db.Exec(ddl); err != nil {
		t.Fatalf("install trigger %s: %v", name, err)
	}
}

func dropTrigger(t *testing.T, r *InMemoryRegistry, name string) {
	t.Helper()
	if _, err := r.store.db.Exec(`DROP TRIGGER IF EXISTS ` + name); err != nil {
		t.Fatalf("drop trigger %s: %v", name, err)
	}
}

// A failure at any of the three durable steps (current write, archive, prune)
// rolls the whole update back: the visible current skill and the entire
// version history are preserved exactly, in memory and across a reopen (E-03).
func TestRegistryUpdateFailureAtEachStoreStepPreservesState(t *testing.T) {
	ctx := context.Background()

	newSeededRegistry := func(t *testing.T, updates int) (*InMemoryRegistry, string, string) {
		r, dbPath := openSQLiteRegistryForTxTest(t)
		sk, err := r.Create(ctx, &CreateRequest{Name: "Tx", Body: "b0", Version: "1", Triggers: []string{"tx"}})
		if err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= updates; i++ {
			body := fmt.Sprintf("b%d", i)
			if _, err := r.Update(ctx, sk.ID, &UpdateRequest{Body: &body}); err != nil {
				t.Fatalf("seed update %d: %v", i, err)
			}
		}
		return r, dbPath, sk.ID
	}

	t.Run("current write failure", func(t *testing.T) {
		r, dbPath, id := newSeededRegistry(t, 2)
		before := captureSkillState(t, r, id)
		installTrigger(t, r, "fail_skill_current", `
CREATE TRIGGER fail_skill_current
BEFORE INSERT ON skills
BEGIN SELECT RAISE(ABORT, 'injected current write failure'); END;`)

		body := "should-not-persist"
		if _, err := r.Update(ctx, id, &UpdateRequest{Body: &body}); err == nil {
			t.Fatal("expected injected current write failure")
		} else if !strings.Contains(err.Error(), "write current skill") {
			t.Fatalf("expected current-write step error, got %v", err)
		}
		assertSkillState(t, r, id, before)
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := NewSQLiteRegistry(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		assertSkillState(t, reopened, id, before)

		dropTrigger(t, reopened, "fail_skill_current")
		if _, err := reopened.Update(ctx, id, &UpdateRequest{Body: &body}); err != nil {
			t.Fatalf("update after dropping trigger: %v", err)
		}
		after := captureSkillState(t, reopened, id)
		if after.body != body || len(after.rowIDs) != len(before.rowIDs)+1 {
			t.Fatalf("post-recovery state = body %q history %d, want body %q history %d",
				after.body, len(after.rowIDs), body, len(before.rowIDs)+1)
		}
	})

	t.Run("archive failure", func(t *testing.T) {
		r, dbPath, id := newSeededRegistry(t, 2)
		before := captureSkillState(t, r, id)
		installTrigger(t, r, "fail_skill_archive_tx", `
CREATE TRIGGER fail_skill_archive_tx
BEFORE INSERT ON skill_versions
BEGIN SELECT RAISE(ABORT, 'injected archive failure'); END;`)

		body := "should-not-persist"
		if _, err := r.Update(ctx, id, &UpdateRequest{Body: &body}); err == nil {
			t.Fatal("expected injected archive failure")
		} else if !strings.Contains(err.Error(), "archive previous skill") {
			t.Fatalf("expected archive step error, got %v", err)
		}
		assertSkillState(t, r, id, before)
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := NewSQLiteRegistry(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		assertSkillState(t, reopened, id, before)

		dropTrigger(t, reopened, "fail_skill_archive_tx")
		if _, err := reopened.Update(ctx, id, &UpdateRequest{Body: &body}); err != nil {
			t.Fatalf("update after dropping trigger: %v", err)
		}
		after := captureSkillState(t, reopened, id)
		if after.body != body || len(after.rowIDs) != len(before.rowIDs)+1 {
			t.Fatalf("post-recovery state = body %q history %d, want body %q history %d",
				after.body, len(after.rowIDs), body, len(before.rowIDs)+1)
		}
	})

	// The prune step only deletes once the history is at capacity, so the
	// prune fault point is exercised with a full 20-version history: a failed
	// update at capacity must preserve all 20 archived snapshots (E-03).
	t.Run("prune failure at capacity keeps all 20 versions", func(t *testing.T) {
		// 1 create + 21 updates: 21 archives pruned to the newest 20.
		r, dbPath, id := newSeededRegistry(t, 21)
		before := captureSkillState(t, r, id)
		if len(before.rowIDs) != maxSkillVersions {
			t.Fatalf("seed history = %d versions, want %d", len(before.rowIDs), maxSkillVersions)
		}
		installTrigger(t, r, "fail_skill_prune", `
CREATE TRIGGER fail_skill_prune
BEFORE DELETE ON skill_versions
BEGIN SELECT RAISE(ABORT, 'injected prune failure'); END;`)

		body := "should-not-persist"
		if _, err := r.Update(ctx, id, &UpdateRequest{Body: &body}); err == nil {
			t.Fatal("expected injected prune failure")
		} else if !strings.Contains(err.Error(), "prune skill versions") {
			t.Fatalf("expected prune step error, got %v", err)
		}
		// The whole history, not just a prefix, survives: no archive was added
		// and nothing was pruned.
		assertSkillState(t, r, id, before)
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := NewSQLiteRegistry(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		assertSkillState(t, reopened, id, before)

		dropTrigger(t, reopened, "fail_skill_prune")
		if _, err := reopened.Update(ctx, id, &UpdateRequest{Body: &body}); err != nil {
			t.Fatalf("update after dropping trigger: %v", err)
		}
		after := captureSkillState(t, reopened, id)
		if after.body != body || len(after.rowIDs) != maxSkillVersions {
			t.Fatalf("post-recovery state = body %q history %d, want body %q history %d",
				after.body, len(after.rowIDs), body, maxSkillVersions)
		}
		// The successful update evicted exactly the oldest snapshot.
		if after.rowIDs[0] == before.rowIDs[0] {
			t.Fatalf("expected oldest row %d to be evicted after recovery update", before.rowIDs[0])
		}
		for i := 1; i < len(before.rowIDs); i++ {
			if after.rowIDs[i-1] != before.rowIDs[i] {
				t.Fatalf("history shift mismatch at %d: got %d, want %d", i-1, after.rowIDs[i-1], before.rowIDs[i])
			}
		}
	})
}

// Rollback drives the same transactional Update path: an injected mid-update
// failure aborts the rollback with no fact changed, and an unknown target row
// id is rejected before anything is written.
func TestRegistryRollbackSharesTransactionalPath(t *testing.T) {
	ctx := context.Background()
	r, dbPath := openSQLiteRegistryForTxTest(t)
	sk, err := r.Create(ctx, &CreateRequest{Name: "RbTx", Body: "v0", Version: "1", Triggers: []string{"rb"}})
	if err != nil {
		t.Fatal(err)
	}
	body := "v1"
	if _, err := r.Update(ctx, sk.ID, &UpdateRequest{Body: &body}); err != nil {
		t.Fatal(err)
	}
	vers, err := r.ListVersions(ctx, sk.ID)
	if err != nil || len(vers) != 1 {
		t.Fatalf("versions=%+v err=%v", vers, err)
	}
	targetRowID := vers[0].RowID
	before := captureSkillState(t, r, sk.ID)

	// Injected archive failure: the rollback restores nothing and archives
	// nothing; the pre-rollback state is fully preserved.
	installTrigger(t, r, "fail_skill_rollback", `
CREATE TRIGGER fail_skill_rollback
BEFORE INSERT ON skill_versions
BEGIN SELECT RAISE(ABORT, 'injected archive failure'); END;`)
	if _, err := r.RollbackVersion(ctx, sk.ID, targetRowID); err == nil {
		t.Fatal("expected injected failure during rollback")
	} else if !strings.Contains(err.Error(), "archive previous skill") {
		t.Fatalf("expected archive step error from rollback, got %v", err)
	}
	assertSkillState(t, r, sk.ID, before)
	dropTrigger(t, r, "fail_skill_rollback")

	// Unknown row id: rejected before any write, so again nothing changes.
	if _, err := r.RollbackVersion(ctx, sk.ID, 999999); err == nil {
		t.Fatal("expected unknown row id to be rejected")
	} else if !strings.Contains(err.Error(), "skill version not found") {
		t.Fatalf("expected version-not-found error, got %v", err)
	}
	assertSkillState(t, r, sk.ID, before)

	// After the fault is cleared the same rollback succeeds through the
	// identical path, and the pre-rollback state is archived (reversible).
	restored, err := r.RollbackVersion(ctx, sk.ID, targetRowID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Body != "v0" {
		t.Fatalf("rollback body=%q want v0", restored.Body)
	}
	after := captureSkillState(t, r, sk.ID)
	if len(after.rowIDs) != len(before.rowIDs)+1 || after.verBody[len(after.verBody)-1] != "v1" {
		t.Fatalf("history after rollback = %v (bodies %v), want v1 appended", after.rowIDs, after.verBody)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// The rollback result is durable across reopen.
	reopened, err := NewSQLiteRegistry(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertSkillState(t, reopened, sk.ID, after)
}
