package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestImportMarkdownDir(t *testing.T) {
	dir := t.TempDir()
	md := "---\nname: Imported Skill\ntriggers: [importme]\n---\nBody text here.\n"
	if err := os.WriteFile(filepath.Join(dir, "imported.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewInMemoryRegistry()
	n, err := reg.ImportMarkdownDir(context.Background(), dir)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	matches, err := reg.Match(context.Background(), &MatchRequest{Text: "please importme now"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range matches {
		if m.Skill.Name == "Imported Skill" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected imported skill match: %+v", matches)
	}
}
