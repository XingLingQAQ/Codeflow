package guard

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/codeflow/backend/internal/workspace"
)

func TestDuplicateSymbolAcrossFiles(t *testing.T) {
	e := NewEngine(nil, nil)
	ctx := context.Background()
	a := filepath.Join("proj", "a.go")
	b := filepath.Join("proj", "b.go")

	codeA := "package proj\n\nfunc ParseConfig() {}\n"
	if err := e.BeforeWrite(ctx, a, []byte(codeA)); err != nil {
		t.Fatalf("first write should pass: %v", err)
	}
	e.AfterWrite(ctx, a, []byte(codeA))

	codeB := "package proj\n\nfunc ParseConfig() {}\n"
	err := e.BeforeWrite(ctx, b, []byte(codeB))
	if err == nil {
		t.Fatal("expected duplicate symbol block")
	}
	if !strings.Contains(err.Error(), string(RuleDuplicateSymbol)) {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "ParseConfig") {
		t.Fatalf("err should name symbol: %v", err)
	}
}

func TestSameFileRewriteAllowed(t *testing.T) {
	e := NewEngine(nil, nil)
	ctx := context.Background()
	path := filepath.Join("proj", "svc.go")
	code := "package proj\n\nfunc Helper() {}\n"
	if err := e.BeforeWrite(ctx, path, []byte(code)); err != nil {
		t.Fatal(err)
	}
	e.AfterWrite(ctx, path, []byte(code))
	// rewrite same path with same symbol ok
	if err := e.BeforeWrite(ctx, path, []byte(code+"\n// touch\n")); err != nil {
		t.Fatalf("rewrite same file: %v", err)
	}
	e.AfterWrite(ctx, path, []byte(code+"\n// touch\n"))
}

func TestDuplicateBlocksWorkspaceWrite(t *testing.T) {
	root := t.TempDir()
	g := NewEngine(nil, nil)
	svc := workspace.NewFSService(g)

	if _, err := svc.Write(context.Background(), &workspace.WriteRequest{
		Root: root, Path: "one.ts",
		Content: []byte("export function loadUser() { return 1 }\n"),
	}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Write(context.Background(), &workspace.WriteRequest{
		Root: root, Path: "two.ts",
		Content: []byte("export function loadUser() { return 2 }\n"),
	})
	if err == nil {
		t.Fatal("expected duplicate block via workspace")
	}
}

func TestConcurrentReserveWrite(t *testing.T) {
	e := NewEngine(nil, nil)
	ctx := context.Background()
	code := []byte("package p\n\nfunc Shared() {}\n")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, p := range []string{filepath.Join("proj", "a.go"), filepath.Join("proj", "b.go")} {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			errs <- e.ReserveWrite(ctx, path, code)
		}(p)
	}
	wg.Wait()
	close(errs)
	ok, fail := 0, 0
	for err := range errs {
		if err == nil {
			ok++
		} else {
			fail++
		}
	}
	if ok != 1 || fail != 1 {
		t.Fatalf("expected exactly one success and one failure, ok=%d fail=%d", ok, fail)
	}
}
