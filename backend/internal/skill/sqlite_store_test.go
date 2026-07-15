package skill

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteRegistryPersistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "skills.db")
	reg, err := NewSQLiteRegistry(dbPath)
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}

	// builtins seeded on empty db
	list, err := reg.List(context.Background())
	if err != nil || len(list) < 2 {
		t.Fatalf("builtins list=%d err=%v", len(list), err)
	}

	created, err := reg.Create(context.Background(), &CreateRequest{
		Name: "Persist Me", Body: "body", Triggers: []string{"persist"},
	})
	if err != nil {
		t.Fatal(err)
	}

	reg2, err := NewSQLiteRegistry(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reg2.Get(context.Background(), created.ID)
	if err != nil || got.Name != "Persist Me" {
		t.Fatalf("reload got=%+v err=%v", got, err)
	}
	// builtins still present, not re-duplicated
	list2, _ := reg2.List(context.Background())
	if len(list2) != len(list)+1 {
		// first open had builtins N; after create N+1; second open should be N+1 not 2N+1
		t.Fatalf("list size=%d want %d (no builtin reseed)", len(list2), len(list)+1)
	}
}
