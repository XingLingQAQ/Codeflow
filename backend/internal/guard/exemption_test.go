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
}
