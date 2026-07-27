package skill

import (
	"context"
	"path/filepath"
	"testing"
)

// TestSQLiteReloadPreservesDisabledAndUpdate verifies that a durable registry
// persists a disabled flag and a body Update across a close/reopen cycle.
func TestSQLiteReloadPreservesDisabledAndUpdate(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "skills_reload.db")

	reg, err := NewSQLiteRegistry(dbPath)
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	created, err := reg.Create(ctx, &CreateRequest{
		Name: "Reload Me", Body: "v1 body", Triggers: []string{"reload"},
	})
	if err != nil {
		_ = reg.Close()
		t.Fatal(err)
	}

	off := false
	newBody := "v2 body persisted"
	if _, err := reg.Update(ctx, created.ID, &UpdateRequest{Enabled: &off, Body: &newBody}); err != nil {
		_ = reg.Close()
		t.Fatal(err)
	}
	if err := reg.Close(); err != nil {
		t.Fatal(err)
	}

	reg2, err := NewSQLiteRegistry(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reg2.Close()

	got, err := reg2.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatal("reload must preserve enabled=false")
	}
	if got.Body != "v2 body persisted" {
		t.Fatalf("reload must preserve updated body, got %q", got.Body)
	}
}
