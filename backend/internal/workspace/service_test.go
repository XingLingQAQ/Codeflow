package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRejectsEscape(t *testing.T) {
	root := t.TempDir()
	svc := NewFSService(nil)

	if _, err := svc.Resolve(root, "../outside"); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := svc.Resolve(root, "..\\outside"); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := svc.Resolve(root, "/etc/passwd"); err == nil {
		t.Fatal("expected absolute path error")
	}
}

func TestListReadWrite(t *testing.T) {
	root := t.TempDir()
	svc := NewFSService(nil)
	ctx := context.Background()

	// write with parents
	ent, err := svc.Write(ctx, &WriteRequest{
		Root:          root,
		Path:          "src/hello.txt",
		Content:       []byte("hi"),
		CreateParents: true,
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if ent.Path != "src/hello.txt" || ent.Size != 2 {
		t.Fatalf("entry=%+v", ent)
	}

	// list root
	list, err := svc.List(ctx, &ListRequest{Root: root, Path: ""})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range list {
		if e.Name == "src" && e.IsDir {
			found = true
		}
	}
	if !found {
		t.Fatalf("src dir not listed: %+v", list)
	}

	// list src
	list, err = svc.List(ctx, &ListRequest{Root: root, Path: "src"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "hello.txt" {
		t.Fatalf("list src: %+v", list)
	}

	// read
	fc, err := svc.Read(ctx, &ReadRequest{Root: root, Path: "src/hello.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if string(fc.Content) != "hi" {
		t.Fatalf("content=%q", fc.Content)
	}

	// stat
	st, err := svc.Stat(ctx, root, "src/hello.txt")
	if err != nil || st.IsDir || st.Size != 2 {
		t.Fatalf("stat=%+v err=%v", st, err)
	}
}

func TestWriteGuardBlocks(t *testing.T) {
	root := t.TempDir()
	blocked := errors.New("nope")
	svc := NewFSService(guardFunc(func(ctx context.Context, abs string, content []byte) error {
		return blocked
	}))
	_, err := svc.Write(context.Background(), &WriteRequest{
		Root:    root,
		Path:    "x.txt",
		Content: []byte("x"),
	})
	if err == nil || !errors.Is(err, blocked) && !contains(err.Error(), "blocked") {
		t.Fatalf("expected guard block, got %v", err)
	}
	// file must not exist
	if _, err := os.Stat(filepath.Join(root, "x.txt")); !os.IsNotExist(err) {
		t.Fatalf("file should not exist, err=%v", err)
	}
}

func TestWriteGuardAllows(t *testing.T) {
	root := t.TempDir()
	called := false
	svc := NewFSService(guardFunc(func(ctx context.Context, abs string, content []byte) error {
		called = true
		return nil
	}))
	_, err := svc.Write(context.Background(), &WriteRequest{
		Root:    root,
		Path:    "ok.txt",
		Content: []byte("ok"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("guard not called")
	}
}

func TestStageAndPromote(t *testing.T) {
	root := t.TempDir()
	svc := NewFSService(nil)
	ctx := context.Background()
	_, err := svc.Write(ctx, &WriteRequest{
		Root: root, Path: "src/a.txt", Content: []byte("staged"),
		CreateParents: true, Mode: WriteModeStage,
	})
	if err != nil {
		t.Fatal(err)
	}
	// real path must not exist yet
	if _, err := os.Stat(filepath.Join(root, "src", "a.txt")); !os.IsNotExist(err) {
		t.Fatal("real file should not exist before promote")
	}
	// staged exists
	if _, err := os.Stat(filepath.Join(root, ".codeflow", "staging", "src", "a.txt")); err != nil {
		t.Fatalf("staged missing: %v", err)
	}
	staged, err := svc.ListStaged(ctx, root)
	if err != nil || len(staged) != 1 || staged[0].Path != "src/a.txt" {
		t.Fatalf("list staged=%+v err=%v", staged, err)
	}
	if _, err := svc.Promote(ctx, root, "src/a.txt"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "src", "a.txt"))
	if err != nil || string(data) != "staged" {
		t.Fatalf("promoted content=%q err=%v", data, err)
	}
}

func TestGetSetService(t *testing.T) {
	prev := GetService()
	t.Cleanup(func() { SetService(prev) })
	custom := NewFSService(nil)
	SetService(custom)
	if GetService() != custom {
		t.Fatal("expected custom service")
	}
	SetService(nil)
	if GetService() == nil {
		t.Fatal("expected lazy default")
	}
}

type guardFunc func(ctx context.Context, abs string, content []byte) error

func (f guardFunc) BeforeWrite(ctx context.Context, abs string, content []byte) error {
	return f(ctx, abs, content)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestDiscardStaged(t *testing.T) {
	root := t.TempDir()
	svc := NewFSService(nil)
	ctx := context.Background()
	_, err := svc.Write(ctx, &WriteRequest{
		Root: root, Path: "tmp/x.txt", Content: []byte("x"),
		CreateParents: true, Mode: WriteModeStage,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DiscardStaged(ctx, root, "tmp/x.txt"); err != nil {
		t.Fatal(err)
	}
	staged, err := svc.ListStaged(ctx, root)
	if err != nil || len(staged) != 0 {
		t.Fatalf("expected empty after discard: %+v err=%v", staged, err)
	}
}
