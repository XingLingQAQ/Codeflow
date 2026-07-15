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
