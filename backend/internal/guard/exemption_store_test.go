package guard

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestExemptionStorePersistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "guard_exemptions.db")
	e1 := NewEngine(nil, nil)
	if err := e1.OpenExemptionStore(dbPath); err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	defer func() { _ = e1.CloseExemptionStore() }()

	path := filepath.Join("proj", "utils2.go")
	if err := e1.BeforeWrite(context.Background(), path, []byte("package p")); err == nil {
		t.Fatal("expected block without exemption")
	}
	e1.GrantExemption(Exemption{
		Path:      path,
		Rules:     []RuleID{RuleStackedNaming},
		Reason:    "approved rename",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err := e1.BeforeWrite(context.Background(), path, []byte("package p")); err != nil {
		t.Fatalf("expected exempt allow on first engine: %v", err)
	}
	_ = e1.CloseExemptionStore()

	e2 := NewEngine(nil, nil)
	if err := e2.OpenExemptionStore(dbPath); err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer func() { _ = e2.CloseExemptionStore() }()
	if err := e2.BeforeWrite(context.Background(), path, []byte("package p")); err != nil {
		t.Fatalf("expected durable exemption after reload: %v", err)
	}
}

func TestExemptionStorePrunesExpired(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "guard_exemptions_exp.db")
	e1 := NewEngine(nil, nil)
	if err := e1.OpenExemptionStore(dbPath); err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	path := filepath.Join("proj", "helpers2.go")
	e1.GrantExemption(Exemption{
		Path:      path,
		Rules:     []RuleID{RuleStackedNaming},
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	})
	_ = e1.CloseExemptionStore()

	e2 := NewEngine(nil, nil)
	if err := e2.OpenExemptionStore(dbPath); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = e2.CloseExemptionStore() }()
	if err := e2.BeforeWrite(context.Background(), path, []byte("package p")); err == nil {
		t.Fatal("expired exemption should not reload as active")
	}
}
