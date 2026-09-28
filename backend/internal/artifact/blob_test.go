package artifact

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newStore opens a store under t.TempDir() with the default options.
func newStore(t *testing.T) (*BlobStore, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "blobs")
	store, err := OpenBlobStore(root, BlobStoreOptions{})
	if err != nil {
		t.Fatalf("OpenBlobStore(%s): %v", root, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, root
}

// tmpEntries lists whatever is left in the staging directory. A successful Put
// must leave it empty: tmp/ is never a blob, so anything there is garbage.
func tmpEntries(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "tmp"))
	if err != nil {
		t.Fatalf("read tmp dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestOpenBlobStoreCreatesLayout(t *testing.T) {
	store, root := newStore(t)
	if store.Root() != filepath.Clean(root) {
		t.Errorf("Root() = %q, want %q", store.Root(), root)
	}
	for _, dir := range []string{"sha256", "tmp"} {
		info, err := os.Stat(filepath.Join(root, dir))
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
	}
}

// TestOpenBlobStoreRequiresAbsoluteRoot pins the root contract: a relative root
// would address different directories before and after a chdir.
func TestOpenBlobStoreRequiresAbsoluteRoot(t *testing.T) {
	if _, err := OpenBlobStore("relative/blobs", BlobStoreOptions{}); err == nil {
		t.Error("OpenBlobStore(relative) = nil error, want a refusal")
	}
	if _, err := OpenBlobStore("", BlobStoreOptions{}); err == nil {
		t.Error("OpenBlobStore(\"\") = nil error, want a refusal")
	}
}

// TestPutRoundTrip is the basic contract: the returned hash addresses the
// bytes, the size is the byte count, and the file lands in the sharded layout
// with the staging directory left empty.
func TestPutRoundTrip(t *testing.T) {
	store, root := newStore(t)
	content := []byte("hello blob\n")

	hash, size, err := store.Put(context.Background(), bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if hash != HashBytes(content) {
		t.Errorf("Put hash = %s, want %s", hash, HashBytes(content))
	}
	if size != int64(len(content)) {
		t.Errorf("Put size = %d, want %d", size, len(content))
	}

	stored, err := os.ReadFile(filepath.Join(root, "sha256", hash[7:9], hash[9:]))
	if err != nil {
		t.Fatalf("read stored blob: %v", err)
	}
	if !bytes.Equal(stored, content) {
		t.Errorf("stored bytes = %q, want %q", stored, content)
	}
	if left := tmpEntries(t, root); len(left) != 0 {
		t.Errorf("tmp dir left behind %v, want empty", left)
	}

	got, err := store.Stat(hash)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got != int64(len(content)) {
		t.Errorf("Stat = %d, want %d", got, len(content))
	}

	has, err := store.Has(hash)
	if err != nil || !has {
		t.Errorf("Has = %v, %v; want true, nil", has, err)
	}

	rc, err := store.Open(hash)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()
	read, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if !bytes.Equal(read, content) {
		t.Errorf("Open content = %q, want %q", read, content)
	}
}

// TestPutIsContentAddressedAndIdempotent: the same bytes always produce the
// same address, a second Put of them succeeds without disturbing the file, and
// different bytes get a different address.
func TestPutIsContentAddressedAndIdempotent(t *testing.T) {
	store, root := newStore(t)
	content := []byte("same bytes\n")

	first, firstSize, err := store.Put(context.Background(), bytes.NewReader(content))
	if err != nil {
		t.Fatalf("first Put: %v", err)
	}
	second, secondSize, err := store.Put(context.Background(), bytes.NewReader(content))
	if err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if first != second || firstSize != secondSize {
		t.Errorf("second Put = (%s, %d), want (%s, %d)", second, secondSize, first, firstSize)
	}
	if left := tmpEntries(t, root); len(left) != 0 {
		t.Errorf("tmp dir left behind %v, want empty", left)
	}

	other, _, err := store.Put(context.Background(), bytes.NewReader([]byte("other bytes\n")))
	if err != nil {
		t.Fatalf("Put other: %v", err)
	}
	if other == first {
		t.Error("different content produced the same hash")
	}
}

// TestPutEmptyBlob covers the zero-byte edge: it is legal, it has the well-known
// empty digest, and Open can read it back.
func TestPutEmptyBlob(t *testing.T) {
	store, _ := newStore(t)
	hash, size, err := store.Put(context.Background(), bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("Put(empty): %v", err)
	}
	if hash != HashBytes(nil) || size != 0 {
		t.Errorf("Put(empty) = (%s, %d), want (%s, 0)", hash, size, HashBytes(nil))
	}
	rc, err := store.Open(hash)
	if err != nil {
		t.Fatalf("Open(empty): %v", err)
	}
	defer rc.Close()
	if data, err := io.ReadAll(rc); err != nil || len(data) != 0 {
		t.Errorf("read empty blob = %q, %v; want empty, nil", data, err)
	}
}

// TestPutRejectsOversizeAndLeavesNothing: the limit is enforced while the
// stream is read (one byte past the bound proves it), and a rejected Put
// neither publishes a blob nor leaves a temp file.
func TestPutRejectsOversizeAndLeavesNothing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "blobs")
	store, err := OpenBlobStore(root, BlobStoreOptions{MaxBlobBytes: 8})
	if err != nil {
		t.Fatalf("OpenBlobStore: %v", err)
	}
	defer store.Close()

	_, _, err = store.Put(context.Background(), bytes.NewReader([]byte("123456789")))
	if !errors.Is(err, ErrBlobTooLarge) {
		t.Fatalf("Put(9 bytes, max 8) = %v, want ErrBlobTooLarge", err)
	}
	if left := tmpEntries(t, root); len(left) != 0 {
		t.Errorf("rejected Put left %v in tmp, want empty", left)
	}
	has, err := store.Has(HashBytes([]byte("123456789")))
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if has {
		t.Error("an oversize blob was published")
	}

	// Exactly at the limit is allowed: the bound is MaxBlobBytes, not
	// MaxBlobBytes-1.
	hash, size, err := store.Put(context.Background(), bytes.NewReader([]byte("12345678")))
	if err != nil {
		t.Fatalf("Put(exactly 8 bytes): %v", err)
	}
	if size != 8 || hash != HashBytes([]byte("12345678")) {
		t.Errorf("Put(8 bytes) = (%s, %d), want the 8-byte hash", hash, size)
	}
}

// TestPutTempFileIsNeverVisible covers the crash semantics: a leftover temp
// file from an interrupted write is not a blob (there is no hash that addresses
// it, and Has/Open/Stat only look under sha256/), and it does not disturb a
// later Put of the same content.
func TestPutTempFileIsNeverVisible(t *testing.T) {
	store, root := newStore(t)
	content := []byte("complete content\n")

	// Simulate a process that died mid-Put: a partial file under tmp/ whose
	// name is not a hash.
	leaked := filepath.Join(root, "tmp", "put-crashed.tmp")
	if err := os.WriteFile(leaked, []byte("partial"), 0o644); err != nil {
		t.Fatalf("seed leaked temp file: %v", err)
	}

	hash, size, err := store.Put(context.Background(), bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put with a leaked temp file: %v", err)
	}
	if hash != HashBytes(content) || size != int64(len(content)) {
		t.Errorf("Put = (%s, %d), want the content hash", hash, size)
	}
	if _, err := os.Stat(leaked); err != nil {
		t.Errorf("the leaked temp file was disturbed: %v", err)
	}

	// The partial bytes were never reachable: the hash of "partial" is not a
	// blob, even though those bytes exist somewhere under the root.
	has, err := store.Has(HashBytes([]byte("partial")))
	if err != nil {
		t.Fatalf("Has(partial hash): %v", err)
	}
	if has {
		t.Error("bytes that only exist under tmp/ are addressable as a blob")
	}
}

// TestPutDetectsCorruptExistingBlob: if the file under an address no longer
// hashes to it, Put refuses (rather than overwriting the evidence) and Open
// reports the corruption at EOF.
func TestPutDetectsCorruptExistingBlob(t *testing.T) {
	store, root := newStore(t)
	content := []byte("original content\n")
	hash, _, err := store.Put(context.Background(), bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	path := filepath.Join(root, "sha256", hash[7:9], hash[9:])
	if err := os.WriteFile(path, []byte("tampered content\n"), 0o644); err != nil {
		t.Fatalf("tamper blob: %v", err)
	}

	if _, _, err := store.Put(context.Background(), bytes.NewReader(content)); !errors.Is(err, ErrBlobCorrupt) {
		t.Errorf("Put over a corrupt blob = %v, want ErrBlobCorrupt", err)
	}
	// The tampered bytes must still be there: overwriting them would hide the
	// corruption from every later reader and from an operator.
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tampered blob: %v", err)
	}
	if string(stored) != "tampered content\n" {
		t.Errorf("stored bytes = %q, want the tampered content left in place", stored)
	}

	rc, err := store.Open(hash)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()
	_, err = io.ReadAll(rc)
	if !errors.Is(err, ErrBlobCorrupt) {
		t.Errorf("reading a corrupt blob = %v, want ErrBlobCorrupt", err)
	}
}

// TestOpenPartialReadIsNotVerified pins what Open does and does not promise: a
// reader that stops before EOF has no verdict (the bytes read cannot prove
// anything about the whole file); the size recorded with a version is what a
// caller uses for a cheap early check.
func TestOpenPartialReadIsNotVerified(t *testing.T) {
	store, root := newStore(t)
	content := []byte("0123456789abcdef\n")
	hash, _, err := store.Put(context.Background(), bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	path := filepath.Join(root, "sha256", hash[7:9], hash[9:])
	if err := os.WriteFile(path, []byte("XXXXXXXXXXXXXXXX\n"), 0o644); err != nil {
		t.Fatalf("tamper blob: %v", err)
	}

	rc, err := store.Open(hash)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()
	buf := make([]byte, 4)
	if _, err := io.ReadFull(rc, buf); err != nil {
		t.Fatalf("partial read: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestBlobLookups covers the lookup contract for valid, absent, malformed and
// non-regular addresses.
func TestBlobLookups(t *testing.T) {
	store, root := newStore(t)
	content := []byte("lookup me\n")
	hash, size, err := store.Put(context.Background(), bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	missing := HashBytes([]byte("never stored"))
	if _, err := store.Stat(missing); !errors.Is(err, ErrBlobNotFound) {
		t.Errorf("Stat(missing) = %v, want ErrBlobNotFound", err)
	}
	if _, err := store.Open(missing); !errors.Is(err, ErrBlobNotFound) {
		t.Errorf("Open(missing) = %v, want ErrBlobNotFound", err)
	}
	has, err := store.Has(missing)
	if err != nil || has {
		t.Errorf("Has(missing) = %v, %v; want false, nil", has, err)
	}

	for _, bad := range []string{"", "sha256:", "sha256:xyz", strings.ToUpper(hash), hash[7:]} {
		if _, err := store.Stat(bad); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("Stat(%q) = %v, want ErrInvalidHash", bad, err)
		}
		if _, err := store.Open(bad); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("Open(%q) = %v, want ErrInvalidHash", bad, err)
		}
		if _, err := store.Has(bad); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("Has(%q) = %v, want ErrInvalidHash", bad, err)
		}
	}

	// A directory sitting where a blob should be is not a blob: serving it
	// would hand a reader a stream of directory entries under a name that
	// promises content.
	dirHash := HashBytes([]byte("this one is a directory"))
	dshard, dname := hashDirName(dirHash)
	if err := os.MkdirAll(filepath.Join(root, "sha256", dshard, dname), 0o755); err != nil {
		t.Fatalf("mkdir where a blob would be: %v", err)
	}
	if _, err := store.Stat(dirHash); err == nil {
		t.Error("Stat(directory) = nil error, want a refusal")
	}
	if has, err := store.Has(dirHash); err != nil || has {
		t.Errorf("Has(directory) = %v, %v; want false, nil", has, err)
	}
	if size != int64(len(content)) {
		t.Errorf("Stat = %d, want %d", size, len(content))
	}
}

// TestPutContextCancelled: a canceled context stops the write and leaves no
// published blob and no temp file.
func TestPutContextCancelled(t *testing.T) {
	store, root := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := store.Put(ctx, bytes.NewReader([]byte("never written")))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Put(canceled) = %v, want context.Canceled", err)
	}
	if left := tmpEntries(t, root); len(left) != 0 {
		t.Errorf("canceled Put left %v in tmp, want empty", left)
	}
}

// TestPutAfterClose: a closed store refuses instead of writing into a directory
// the caller may already have removed.
func TestPutAfterClose(t *testing.T) {
	root := filepath.Join(t.TempDir(), "blobs")
	store, err := OpenBlobStore(root, BlobStoreOptions{})
	if err != nil {
		t.Fatalf("OpenBlobStore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, _, err := store.Put(context.Background(), bytes.NewReader([]byte("x"))); !errors.Is(err, ErrBlobStoreClosed) {
		t.Errorf("Put after Close = %v, want ErrBlobStoreClosed", err)
	}
	if _, err := store.Open(HashBytes([]byte("x"))); !errors.Is(err, ErrBlobStoreClosed) {
		t.Errorf("Open after Close = %v, want ErrBlobStoreClosed", err)
	}
	if err := store.Close(); err != nil {
		t.Errorf("second Close = %v, want nil (idempotent)", err)
	}
}

// TestPutConcurrentSameContent is the §T1.09.b concurrency case: 20 writers
// pushing identical bytes at once must all succeed with the same hash and size,
// leave exactly one file, and leave no temp file behind.
func TestPutConcurrentSameContent(t *testing.T) {
	store, root := newStore(t)
	content := []byte(strings.Repeat("concurrent content\n", 64))
	wantHash := HashBytes(content)
	wantSize := int64(len(content))

	const writers = 20
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		hashes  []string
		sizes   []int64
		failed  []error
		started = make(chan struct{})
	)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-started
			hash, size, err := store.Put(context.Background(), bytes.NewReader(content))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, err)
				return
			}
			hashes = append(hashes, hash)
			sizes = append(sizes, size)
		}()
	}
	close(started)
	wg.Wait()

	if len(failed) != 0 {
		t.Fatalf("%d of %d concurrent Puts failed, first: %v", len(failed), writers, failed[0])
	}
	for i, hash := range hashes {
		if hash != wantHash {
			t.Fatalf("writer %d returned hash %s, want %s", i, hash, wantHash)
		}
	}
	for i, size := range sizes {
		if size != wantSize {
			t.Fatalf("writer %d returned size %d, want %d", i, size, wantSize)
		}
	}

	// Exactly one file under sha256/, and nothing in tmp/.
	var files int
	err := filepath.WalkDir(filepath.Join(root, "sha256"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk blob tree: %v", err)
	}
	if files != 1 {
		t.Errorf("blob tree holds %d files, want exactly 1", files)
	}
	if left := tmpEntries(t, root); len(left) != 0 {
		t.Errorf("concurrent Puts left %v in tmp, want empty", left)
	}

	rc, err := store.Open(wantHash)
	if err != nil {
		t.Fatalf("Open after concurrency: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read after concurrency: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Error("the stored blob does not hold the written content")
	}
}
