package guard

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// With no deprecated_path_globs configured (the default) the rule never fires.
func TestDeprecatedPathDefaultOff(t *testing.T) {
	e := NewEngine(nil, nil)
	target := filepath.Join("legacy", "mod.go")
	dec := e.Evaluate(context.Background(), target, []byte("package p\n"))
	for _, v := range dec.Violations {
		if v.Rule == RuleDeprecatedPath {
			t.Fatalf("no deprecated_path violation expected with empty globs: %+v", dec.Violations)
		}
	}
	if err := e.BeforeWrite(context.Background(), target, []byte("package p\n")); err != nil {
		t.Fatalf("write should be allowed by default: %v", err)
	}
}

// At the default warn severity a matching write is allowed but the violation is surfaced.
func TestDeprecatedPathWarnSurfaced(t *testing.T) {
	e := NewEngine(nil, nil)
	path := writeGuardConfig(t, `
deprecated_path_globs:
  - "deprecated/**"
`)
	if err := e.TryLoadConfigFile(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join("deprecated", "mod.go")
	if err := e.BeforeWrite(context.Background(), target, []byte("package p\n")); err != nil {
		t.Fatalf("deprecated_path at warn should allow: %v", err)
	}
	dec := e.Evaluate(context.Background(), target, []byte("package p\n"))
	if !dec.Allowed {
		t.Fatalf("warn should keep the decision allowed: %+v", dec)
	}
	found := false
	for _, v := range dec.Violations {
		if v.Rule == RuleDeprecatedPath {
			if v.Severity != SeverityWarn {
				t.Fatalf("expected warn severity, got %s", v.Severity)
			}
			if !strings.Contains(v.Message, "deprecated/**") {
				t.Fatalf("message should name the matched glob: %q", v.Message)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("expected deprecated_path warn violation: %+v", dec.Violations)
	}
}

// Promoting deprecated_path to error denies a matching write.
func TestDeprecatedPathPromotedToError(t *testing.T) {
	e := NewEngine(nil, nil)
	path := writeGuardConfig(t, `
deprecated_path_globs:
  - "deprecated/**"
rules:
  deprecated_path:
    severity: "error"
`)
	if err := e.TryLoadConfigFile(path); err != nil {
		t.Fatal(err)
	}
	err := e.BeforeWrite(context.Background(), filepath.Join("deprecated", "mod.go"), []byte("package p\n"))
	if err == nil {
		t.Fatal("expected deprecated_path at error to deny the write")
	}
	if !strings.Contains(err.Error(), string(RuleDeprecatedPath)) {
		t.Fatalf("err should name deprecated_path: %v", err)
	}
}

// An exemption bypasses deprecated_path just like the other rules.
func TestDeprecatedPathExemptionBypass(t *testing.T) {
	e := NewEngine(nil, nil)
	path := writeGuardConfig(t, `
deprecated_path_globs:
  - "deprecated/**"
rules:
  deprecated_path:
    severity: "error"
`)
	if err := e.TryLoadConfigFile(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join("deprecated", "mod.go")
	if err := e.BeforeWrite(context.Background(), target, []byte("package p\n")); err == nil {
		t.Fatal("expected block before exemption")
	}
	e.GrantExemption(Exemption{
		Path:      target,
		Rules:     []RuleID{RuleDeprecatedPath},
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err := e.BeforeWrite(context.Background(), target, []byte("package p\n")); err != nil {
		t.Fatalf("exemption should bypass deprecated_path: %v", err)
	}
}
