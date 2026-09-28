package merge

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/codeflow/backend/internal/artifact"
	"github.com/codeflow/backend/internal/runworkspace"
)

// The blob store of T1.09.b group 1 is what production hands to Prepare. The
// two groups were built in parallel against one agreed signature, so this file
// proves they meet: at compile time, and end to end on real directories.
var _ BlobWriter = (*artifact.BlobStore)(nil)

// TestPrepareStoresNewContentInTheArtifactBlobStore runs Prepare with the real
// content-addressed store. Every create/modify operation's NewHash must name a
// stored blob whose bytes are exactly the working copy's, read back through the
// store's verifying reader; the root ownership marker produces nothing, while a
// file of the same name deeper in the tree is ordinary content and is published.
func TestPrepareStoresNewContentInTheArtifactBlobStore(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "A\n")
	writeTestFile(t, root, "keep.txt", "K\n")

	base := capturePlain(t, root)
	wc := materialize(t, base)

	writeTestFile(t, wc.Path, "a.txt", "A changed by the run\n")
	writeTestFile(t, wc.Path, "sub/"+runworkspace.OwnerMarkerName(), "{\"project\":\"content\"}\n")
	writeTestFile(t, wc.Path, "new.bin", "bin\x00ary\n")

	result := capturePlain(t, wc.Path)
	target := capturePlain(t, root)

	store, err := artifact.OpenBlobStore(filepath.Join(t.TempDir(), "blobs"), artifact.BlobStoreOptions{})
	if err != nil {
		t.Fatalf("open blob store: %v", err)
	}
	c, err := Prepare(context.Background(), PrepareInput{
		Base: base, Target: target, Result: result, ResultRoot: wc.Path, Blobs: store,
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(c.Conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", c.Conflicts)
	}
	if !c.Publishable() {
		t.Fatal("candidate with three changes and no conflicts must be publishable")
	}

	noOperationFor(t, c, runworkspace.OwnerMarkerName())
	noOperationFor(t, c, "keep.txt")
	if op := findOperation(t, c, "a.txt"); op.Kind != KindModify {
		t.Errorf("a.txt kind = %s, want %s", op.Kind, KindModify)
	}
	if op := findOperation(t, c, "sub/"+runworkspace.OwnerMarkerName()); op.Kind != KindCreate {
		t.Errorf("nested marker-named file kind = %s, want %s (only the root marker is internal)", op.Kind, KindCreate)
	}
	if op := findOperation(t, c, "new.bin"); op.Kind != KindCreate || !op.Binary {
		t.Errorf("new.bin = %+v, want a binary create", op)
	}

	for _, op := range c.Operations {
		if op.Kind == KindDelete {
			continue
		}
		size, err := store.Stat(op.NewHash)
		if err != nil {
			t.Fatalf("%s: new content %s is not in the blob store: %v", op.Path, op.NewHash, err)
		}
		if size != op.Size {
			t.Errorf("%s: stored %d bytes, operation says %d", op.Path, size, op.Size)
		}
		reader, err := store.Open(op.NewHash)
		if err != nil {
			t.Fatalf("%s: open blob: %v", op.Path, err)
		}
		stored, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatalf("%s: read blob (verified at EOF): %v", op.Path, err)
		}
		onDisk, err := os.ReadFile(filepath.Join(wc.Path, filepath.FromSlash(op.Path)))
		if err != nil {
			t.Fatalf("%s: read working copy: %v", op.Path, err)
		}
		if !bytes.Equal(stored, onDisk) {
			t.Errorf("%s: stored bytes differ from the working copy", op.Path)
		}
	}
}
