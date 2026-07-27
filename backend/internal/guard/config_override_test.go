package guard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGuardConfig writes a guard.yaml into a temp dir and returns its path.
func writeGuardConfig(t *testing.T, yaml string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "guard.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Promoting binary_exec_write (default warn) to error must deny the write.
func TestConfigPromotesBinaryExecToError(t *testing.T) {
	e := NewEngine(nil, nil)
	// Default severity is warn: .exe is allowed before override.
	if err := e.BeforeWrite(context.Background(), filepath.Join("proj", "tool.exe"), []byte("MZ")); err != nil {
		t.Fatalf("default warn should allow .exe: %v", err)
	}

	path := writeGuardConfig(t, `
rules:
  binary_exec_write:
    severity: "error"
`)
	if err := e.TryLoadConfigFile(path); err != nil {
		t.Fatal(err)
	}
	err := e.BeforeWrite(context.Background(), filepath.Join("proj", "tool.exe"), []byte("MZ"))
	if err == nil {
		t.Fatal("expected .exe denied after promoting binary_exec_write to error")
	}
	if !strings.Contains(err.Error(), string(RuleBinaryExecWrite)) {
		t.Fatalf("err should name binary_exec_write: %v", err)
	}
}

// Demoting stacked_naming (default error) to warn must allow the write while
// still surfacing the violation in Evaluate.
func TestConfigDemotesStackedNamingToWarn(t *testing.T) {
	e := NewEngine(nil, nil)
	path := writeGuardConfig(t, `
rules:
  stacked_naming:
    severity: "warn"
`)
	if err := e.TryLoadConfigFile(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join("proj", "utils2.go")
	if err := e.BeforeWrite(context.Background(), target, []byte("package p\n")); err != nil {
		t.Fatalf("stacked_naming demoted to warn should allow: %v", err)
	}
	dec := e.Evaluate(context.Background(), target, []byte("package p\n"))
	if !dec.Allowed {
		t.Fatalf("expected allowed decision: %+v", dec)
	}
	found := false
	for _, v := range dec.Violations {
		if v.Rule == RuleStackedNaming {
			if v.Severity != SeverityWarn {
				t.Fatalf("expected warn severity, got %s", v.Severity)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("expected stacked_naming warn violation: %+v", dec.Violations)
	}
}

// Setting duplicate_symbol to off must make the ReserveWrite path skip the
// cross-file duplicate check (service.go commits the index without conflict).
func TestConfigDuplicateSymbolOffSkipsReserveCheck(t *testing.T) {
	e := NewEngine(nil, nil)
	ctx := context.Background()
	code := []byte("package p\n\nfunc Shared() {}\n")
	a := filepath.Join("proj", "a.go")
	b := filepath.Join("proj", "b.go")

	if err := e.ReserveWrite(ctx, a, code); err != nil {
		t.Fatalf("seed write should pass: %v", err)
	}
	// Under default error severity the duplicate is detected.
	if dec := e.Evaluate(ctx, b, code); dec.Allowed {
		t.Fatalf("expected duplicate detected before override: %+v", dec)
	}

	path := writeGuardConfig(t, `
rules:
  duplicate_symbol:
    severity: "off"
`)
	if err := e.TryLoadConfigFile(path); err != nil {
		t.Fatal(err)
	}
	if err := e.ReserveWrite(ctx, b, code); err != nil {
		t.Fatalf("ReserveWrite should skip duplicate check when off: %v", err)
	}
}

// Custom denied_path_globs from guard.yaml replace defaults and block matches.
func TestConfigCustomDeniedPathGlobs(t *testing.T) {
	e := NewEngine(nil, nil)
	path := writeGuardConfig(t, `
denied_path_globs:
  - "**/*.secret"
  - "credentials.json"
`)
	if err := e.TryLoadConfigFile(path); err != nil {
		t.Fatal(err)
	}
	if got := e.Config().DeniedPathGlobs; len(got) != 2 {
		t.Fatalf("expected custom globs to replace defaults, got %v", got)
	}
	if err := e.BeforeWrite(context.Background(), filepath.Join("proj", "app.secret"), []byte("x")); err == nil {
		t.Fatal("expected *.secret denied by custom glob")
	}
	if err := e.BeforeWrite(context.Background(), filepath.Join("proj", "credentials.json"), []byte("{}")); err == nil {
		t.Fatal("expected credentials.json denied by custom glob")
	}
	// A path not matching any custom glob is allowed.
	if err := e.BeforeWrite(context.Background(), filepath.Join("proj", "readme.txt"), []byte("hi")); err != nil {
		t.Fatalf("non-matching path should be allowed: %v", err)
	}
}

// Unknown rule IDs in guard.yaml are ignored gracefully: load succeeds, the
// unknown rule has no effect, and known rules keep working.
func TestConfigUnknownRuleIDIgnored(t *testing.T) {
	e := NewEngine(nil, nil)
	path := writeGuardConfig(t, `
rules:
  frobnicate_files:
    severity: "error"
  stacked_naming:
    severity: "error"
`)
	if err := e.TryLoadConfigFile(path); err != nil {
		t.Fatalf("unknown rule id should load gracefully: %v", err)
	}
	// Unknown rule does not block an otherwise-clean write.
	if err := e.BeforeWrite(context.Background(), filepath.Join("proj", "main.go"), []byte("package main\n")); err != nil {
		t.Fatalf("unknown rule must not affect clean write: %v", err)
	}
	// Known rule still enforced.
	if err := e.BeforeWrite(context.Background(), filepath.Join("proj", "helpers2.go"), []byte("package p\n")); err == nil {
		t.Fatal("known stacked_naming rule should still block")
	}
}
