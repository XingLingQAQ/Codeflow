package skill

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

// Two updates archive two prior versions, listed newest-first.
func TestVersionHistoryListsNewestFirst(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()
	sk, err := r.Create(ctx, &CreateRequest{Name: "Hist", Body: "v0", Version: "0.1.0", Triggers: []string{"h"}})
	if err != nil {
		t.Fatal(err)
	}
	b1, b2 := "v1", "v2"
	if _, err := r.Update(ctx, sk.ID, &UpdateRequest{Body: &b1}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Update(ctx, sk.ID, &UpdateRequest{Body: &b2}); err != nil {
		t.Fatal(err)
	}

	vers, err := r.ListVersions(ctx, sk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(vers) != 2 {
		t.Fatalf("expected 2 archived versions, got %d", len(vers))
	}
	// The update to v2 archived the prior v1; the update to v1 archived the original v0.
	if vers[0].Skill.Body != "v1" {
		t.Fatalf("newest archived body=%q want v1", vers[0].Skill.Body)
	}
	if vers[1].Skill.Body != "v0" {
		t.Fatalf("oldest archived body=%q want v0", vers[1].Skill.Body)
	}
	if vers[0].RowID <= vers[1].RowID {
		t.Fatalf("expected newest row id first: %d then %d", vers[0].RowID, vers[1].RowID)
	}
}

// Rollback restores the archived body and archives the pre-rollback state.
func TestVersionHistoryRollbackRestoresAndArchives(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()
	sk, err := r.Create(ctx, &CreateRequest{Name: "Roll", Body: "original", Version: "1.0.0", Triggers: []string{"r"}})
	if err != nil {
		t.Fatal(err)
	}
	changed := "changed"
	if _, err := r.Update(ctx, sk.ID, &UpdateRequest{Body: &changed}); err != nil {
		t.Fatal(err)
	}

	vers, err := r.ListVersions(ctx, sk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(vers) != 1 || vers[0].Skill.Body != "original" {
		t.Fatalf("expected archived original, got %+v", vers)
	}
	rowID := vers[0].RowID

	restored, err := r.RollbackVersion(ctx, sk.ID, rowID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Body != "original" {
		t.Fatalf("rollback body=%q want original", restored.Body)
	}
	cur, err := r.Get(ctx, sk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Body != "original" {
		t.Fatalf("current body after rollback=%q want original", cur.Body)
	}
	// The pre-rollback state ("changed") is now the newest archived version.
	vers2, err := r.ListVersions(ctx, sk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(vers2) != 2 {
		t.Fatalf("expected 2 versions after rollback, got %d", len(vers2))
	}
	if vers2[0].Skill.Body != "changed" {
		t.Fatalf("newest archived (pre-rollback) body=%q want changed", vers2[0].Skill.Body)
	}
}

// History is capped: 25 updates keep only the newest 20 snapshots.
func TestVersionHistoryCapEviction(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()
	sk, err := r.Create(ctx, &CreateRequest{Name: "Cap", Body: "b0", Version: "1", Triggers: []string{"c"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 25; i++ {
		nb := fmt.Sprintf("b%d", i)
		if _, err := r.Update(ctx, sk.ID, &UpdateRequest{Body: &nb}); err != nil {
			t.Fatal(err)
		}
	}
	vers, err := r.ListVersions(ctx, sk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(vers) != maxSkillVersions {
		t.Fatalf("expected %d versions retained, got %d", maxSkillVersions, len(vers))
	}
	// 25 updates archive prior bodies b0..b24; the cap retains the newest 20 (b5..b24).
	if vers[0].Skill.Body != "b24" {
		t.Fatalf("newest retained body=%q want b24", vers[0].Skill.Body)
	}
	if vers[len(vers)-1].Skill.Body != "b5" {
		t.Fatalf("oldest retained body=%q want b5", vers[len(vers)-1].Skill.Body)
	}
	for _, v := range vers {
		if v.Skill.Body == "b0" || v.Skill.Body == "b4" {
			t.Fatalf("evicted version resurfaced: %q", v.Skill.Body)
		}
	}
}

// Builtins cannot be rolled back, and unknown row ids are rejected.
func TestVersionHistoryRollbackRejections(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()
	if _, err := r.RollbackVersion(ctx, "builtin-commit-hygiene", 1); err == nil {
		t.Fatal("expected rollback of builtin to be rejected")
	}
	sk, err := r.Create(ctx, &CreateRequest{Name: "Unknown", Body: "b", Triggers: []string{"u"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.RollbackVersion(ctx, sk.ID, 99999); err == nil {
		t.Fatal("expected unknown version row id to be rejected")
	}
}

// Version history persists across a SQLite close/reopen, and rollback works
// against a reloaded row id.
func TestVersionHistoryPersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "skills_versions.db")

	r, err := NewSQLiteRegistry(dbPath)
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	sk, err := r.Create(ctx, &CreateRequest{Name: "Durable", Body: "d0", Version: "1", Triggers: []string{"d"}})
	if err != nil {
		_ = r.Close()
		t.Fatal(err)
	}
	b1, b2 := "d1", "d2"
	if _, err := r.Update(ctx, sk.ID, &UpdateRequest{Body: &b1}); err != nil {
		_ = r.Close()
		t.Fatal(err)
	}
	if _, err := r.Update(ctx, sk.ID, &UpdateRequest{Body: &b2}); err != nil {
		_ = r.Close()
		t.Fatal(err)
	}
	before, err := r.ListVersions(ctx, sk.ID)
	if err != nil {
		_ = r.Close()
		t.Fatal(err)
	}
	if len(before) != 2 {
		_ = r.Close()
		t.Fatalf("want 2 versions before reopen, got %d", len(before))
	}
	oldestRowID := before[1].RowID // d0 snapshot
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	r2, err := NewSQLiteRegistry(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()

	after, err := r2.ListVersions(ctx, sk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 {
		t.Fatalf("versions not persisted across reopen: got %d", len(after))
	}
	if after[0].Skill.Body != "d1" || after[1].Skill.Body != "d0" {
		t.Fatalf("reloaded versions wrong: %q, %q", after[0].Skill.Body, after[1].Skill.Body)
	}
	restored, err := r2.RollbackVersion(ctx, sk.ID, oldestRowID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Body != "d0" {
		t.Fatalf("rollback after reopen body=%q want d0", restored.Body)
	}
}
