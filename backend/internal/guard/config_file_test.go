package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFileAndApply(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "guard.yaml")
	yaml := `
max_file_bytes: 100
denied_path_globs:
  - "secrets/**"
rules:
  stacked_naming:
    severity: warn
  duplicate_symbol:
    severity: off
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxFileBytes != 100 {
		t.Fatalf("max=%d", cfg.MaxFileBytes)
	}
	if cfg.Rules[RuleStackedNaming].Severity != SeverityWarn {
		t.Fatalf("stacked sev=%s", cfg.Rules[RuleStackedNaming].Severity)
	}
	if cfg.Rules[RuleDuplicateSymbol].Severity != SeverityOff {
		t.Fatalf("dup sev=%s", cfg.Rules[RuleDuplicateSymbol].Severity)
	}

	e := NewEngine(nil, nil)
	if err := e.TryLoadConfigFile(path); err != nil {
		t.Fatal(err)
	}
	got := e.Config()
	if got.MaxFileBytes != 100 {
		t.Fatalf("applied max=%d", got.MaxFileBytes)
	}
}

func TestTryLoadMissingOK(t *testing.T) {
	e := NewEngine(nil, nil)
	if err := e.TryLoadConfigFile(filepath.Join(t.TempDir(), "nope.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestIndexTreeFindsDuplicate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package p\n\nfunc Shared() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package p\n\nfunc Shared() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, nil)
	n, err := e.IndexTree(nil, root)
	if err != nil || n < 2 {
		t.Fatalf("indexed=%d err=%v", n, err)
	}
	// writing c.go with Shared should block
	err = e.BeforeWrite(nil, filepath.Join(root, "c.go"), []byte("package p\n\nfunc Shared() {}\n"))
	if err == nil {
		t.Fatal("expected duplicate after index tree")
	}
}

func TestIndexTreeRemovesDeletedPaths(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.go")
	b := filepath.Join(root, "b.go")
	codeKeep := "package p\n\nfunc Keep() {}\n"
	codeGone := "package p\n\nfunc Gone() {}\n"
	if err := os.WriteFile(a, []byte(codeKeep), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(codeGone), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, nil)
	if _, err := e.IndexTree(nil, root); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	if _, err := e.IndexTree(nil, root); err != nil {
		t.Fatal(err)
	}
	if err := e.BeforeWrite(nil, filepath.Join(root, "c.go"), []byte(codeGone)); err != nil {
		t.Fatalf("ghost symbol after re-index: %v", err)
	}
}
