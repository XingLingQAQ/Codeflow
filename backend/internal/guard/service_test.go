package guard

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/workspace"
)

func TestStackedNamingBlocked(t *testing.T) {
	e := NewEngine(nil, nil)
	err := e.BeforeWrite(context.Background(), filepath.Join("proj", "utils2.go"), []byte("package x"))
	if err == nil {
		t.Fatal("expected stacked naming block")
	}
	if !strings.Contains(err.Error(), string(RuleStackedNaming)) {
		t.Fatalf("err=%v", err)
	}
}

func TestDeniedEnv(t *testing.T) {
	e := NewEngine(nil, nil)
	err := e.BeforeWrite(context.Background(), filepath.Join("proj", ".env"), []byte("SECRET=1"))
	if err == nil {
		t.Fatal("expected denied path")
	}
}

func TestMaxFileBytes(t *testing.T) {
	e := NewEngine(&Config{MaxFileBytes: 8}, nil)
	err := e.BeforeWrite(context.Background(), filepath.Join("proj", "big.txt"), []byte("0123456789"))
	if err == nil {
		t.Fatal("expected max bytes")
	}
}

func TestWarnDoesNotBlock(t *testing.T) {
	cfg := defaultConfig()
	cfg.Rules[RuleBinaryExecWrite] = RuleConfig{Severity: SeverityWarn}
	e := NewEngine(&cfg, nil)
	// .exe is warn by default — should allow
	if err := e.BeforeWrite(context.Background(), filepath.Join("proj", "tool.exe"), []byte("MZ")); err != nil {
		t.Fatalf("warn should not block: %v", err)
	}
	dec := e.Evaluate(context.Background(), filepath.Join("proj", "tool.exe"), []byte("MZ"))
	if !dec.Allowed || len(dec.Violations) == 0 {
		t.Fatalf("expected allowed with warn violations: %+v", dec)
	}
}

func TestNormalWriteAllowed(t *testing.T) {
	e := NewEngine(nil, nil)
	if err := e.BeforeWrite(context.Background(), filepath.Join("proj", "src", "main.go"), []byte("package main")); err != nil {
		t.Fatal(err)
	}
}

func TestImplementsWriteGuardAndBlocksWorkspace(t *testing.T) {
	var _ workspace.WriteGuard = (*Engine)(nil)
	root := t.TempDir()
	g := NewEngine(nil, nil)
	svc := workspace.NewFSService(g)
	_, err := svc.Write(context.Background(), &workspace.WriteRequest{
		Root:    root,
		Path:    "helpers2.ts",
		Content: []byte("export {}"),
	})
	if err == nil {
		t.Fatal("expected guard to block helpers2.ts")
	}
	// allowed write
	if _, err := svc.Write(context.Background(), &workspace.WriteRequest{
		Root:    root,
		Path:    "helpers.ts",
		Content: []byte("export {}"),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGetSetService(t *testing.T) {
	prev := GetService()
	t.Cleanup(func() { SetService(prev) })
	custom := NewEngine(&Config{MaxFileBytes: 1}, nil)
	SetService(custom)
	if GetService() != custom {
		t.Fatal("expected custom")
	}
}
