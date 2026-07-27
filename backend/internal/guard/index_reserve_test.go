package guard

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestIndexTreeReserveWriteDuplicateThenRemove verifies the IndexTree +
// CheckAndCommit interplay: after IndexTree seeds symbols from disk, a
// ReserveWrite introducing a duplicate of an indexed symbol is denied; once the
// seeding file is removed and the tree re-indexed, the same write is allowed.
func TestIndexTreeReserveWriteDuplicateThenRemove(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	a := filepath.Join(root, "a.go")
	code := []byte("package p\n\nfunc Shared() {}\n")
	if err := os.WriteFile(a, code, 0o644); err != nil {
		t.Fatal(err)
	}

	e := NewEngine(nil, nil)
	n, err := e.IndexTree(ctx, root)
	if err != nil || n < 1 {
		t.Fatalf("index tree: n=%d err=%v", n, err)
	}

	// ReserveWrite of a second file with the same top-level symbol is denied.
	b := filepath.Join(root, "b.go")
	if err := e.ReserveWrite(ctx, b, code); err == nil {
		t.Fatal("expected duplicate symbol denied via ReserveWrite after IndexTree")
	}
	// A rejected ReserveWrite must not have committed b into the index.
	for _, p := range e.SymbolIndex().Paths() {
		if filepath.Clean(p) == filepath.Clean(b) {
			t.Fatal("rejected ReserveWrite must not index b.go")
		}
	}

	// Remove the seeding file and re-index; the ghost symbol should be dropped.
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if _, err := e.IndexTree(ctx, root); err != nil {
		t.Fatal(err)
	}
	if err := e.ReserveWrite(ctx, b, code); err != nil {
		t.Fatalf("write should be allowed after seeding file removed: %v", err)
	}
}
