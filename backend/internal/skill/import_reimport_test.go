package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestImportReimportUpdatesPreservesIDAndEnabled documents ImportMarkdownDir
// re-import semantics: importing the same name again updates the existing
// non-builtin skill in place (ID preserved), replaces body/triggers, and leaves
// the enabled flag untouched (a disabled skill stays disabled).
func TestImportReimportUpdatesPreservesIDAndEnabled(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "reimport.md")

	first := "---\nname: ReimportMe\nversion: 1.0.0\ntriggers: [alpha]\n---\nfirst body\n"
	if err := os.WriteFile(mdPath, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewInMemoryRegistry()
	if n, err := reg.ImportMarkdownDir(ctx, dir); err != nil || n != 1 {
		t.Fatalf("first import n=%d err=%v", n, err)
	}
	created := reg.findByName("ReimportMe")
	if created == nil {
		t.Fatal("skill not created on first import")
	}
	id1 := created.ID

	// Disable it so we can observe whether re-import resets enabled state.
	off := false
	if _, err := reg.Update(ctx, id1, &UpdateRequest{Enabled: &off}); err != nil {
		t.Fatal(err)
	}

	// Re-import with a changed body and triggers under the same name.
	second := "---\nname: ReimportMe\nversion: 2.0.0\ntriggers: [beta]\n---\nsecond body\n"
	if err := os.WriteFile(mdPath, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, err := reg.ImportMarkdownDir(ctx, dir); err != nil || n != 1 {
		t.Fatalf("re-import n=%d err=%v", n, err)
	}

	got := reg.findByName("ReimportMe")
	if got == nil {
		t.Fatal("skill missing after re-import")
	}
	// Updated in place: same ID (no new record minted).
	if got.ID != id1 {
		t.Fatalf("re-import should update in place, id changed %s -> %s", id1, got.ID)
	}
	if strings.TrimSpace(got.Body) != "second body" {
		t.Fatalf("body not replaced on re-import: %q", got.Body)
	}
	if !containsStr(got.Triggers, "beta") || containsStr(got.Triggers, "alpha") {
		t.Fatalf("triggers not replaced on re-import: %v", got.Triggers)
	}
	// Enabled state preserved (Update via import does not touch Enabled).
	if got.Enabled {
		t.Fatal("re-import must preserve disabled state")
	}
	// Exactly one skill by that name — no duplicate.
	all, err := reg.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, s := range all {
		if s.Name == "ReimportMe" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected single ReimportMe skill after re-import, got %d", count)
	}
}
