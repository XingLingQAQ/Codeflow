package guard

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestExemptionBypassesStackedNaming(t *testing.T) {
	e := NewEngine(nil, nil)
	path := filepath.Join("proj", "utils2.go")
	if err := e.BeforeWrite(context.Background(), path, []byte("package p")); err == nil {
		t.Fatal("expected block without exemption")
	}
	e.GrantExemption(Exemption{
		Path:      path,
		Rules:     []RuleID{RuleStackedNaming},
		Reason:    "approved rename",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err := e.BeforeWrite(context.Background(), path, []byte("package p")); err != nil {
		t.Fatalf("expected exempt allow: %v", err)
	}

	listed := e.ListExemptions()
	if len(listed) != 1 || listed[0].Path != filepath.Clean(path) {
		t.Fatalf("list exemptions: %+v", listed)
	}
	e.ClearExemption(path)
	if len(e.ListExemptions()) != 0 {
		t.Fatal("expected empty after clear")
	}
	if err := e.BeforeWrite(context.Background(), path, []byte("package p")); err == nil {
		t.Fatal("expected block after clear")
	}
}

func TestExemptionRelativePathMatchesAbsolute(t *testing.T) {
	e := NewEngine(nil, nil)
	// Grant with project-relative path; evaluate with absolute-looking join.
	rel := filepath.Join("proj", "helpers2.go")
	abs := filepath.Join(t.TempDir(), rel)
	e.GrantExemption(Exemption{
		Path:      rel,
		Rules:     []RuleID{RuleStackedNaming},
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err := e.BeforeWrite(context.Background(), abs, []byte("package p")); err != nil {
		t.Fatalf("relative exemption should match absolute write path: %v", err)
	}
}

func TestExemptionBareSuffixDoesNotMatch(t *testing.T) {
	// Exempt key "s2.go" must not bare-suffix match "utils2.go".
	e := NewEngine(nil, nil)
	e.GrantExemption(Exemption{
		Path:      "s2.go",
		Rules:     []RuleID{RuleStackedNaming},
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	blocked := filepath.Join(t.TempDir(), "utils2.go")
	if err := e.BeforeWrite(context.Background(), blocked, []byte("package p")); err == nil {
		t.Fatal("bare-suffix exemption s2.go must not exempt utils2.go")
	}
}
