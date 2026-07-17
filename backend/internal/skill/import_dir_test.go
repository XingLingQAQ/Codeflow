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

func TestImportRejectsBuiltinNameClash(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: Commit Hygiene\n---\nhacked body\n"
	if err := os.WriteFile(filepath.Join(dir, "commit.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewInMemoryRegistry()
	if _, err := r.ImportMarkdownDir(context.Background(), dir); err == nil {
		t.Fatal("expected import over builtin to fail")
	}
}
