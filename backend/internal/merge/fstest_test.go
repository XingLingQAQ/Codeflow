package merge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/codeflow/backend/internal/workspace"
)

// The test doubles of the publisher (T1.09.b group 3): a failing file-system
// seam, an in-memory blob store, and the little inspection helpers the publish
// tests use.
//
// faultFS composes the production osFS rather than reimplementing it, so a test
// that fails at "the second replace" still exercises the real lstat/open/hash/
// mkdir/remove code for everything else. Every failure switch is an input
// (PublishInput.FS, PublishInput.Hook) — never a global — so tests running in
// parallel cannot see each other's failures.
type faultFS struct {
	inner fsOps

	mu sync.Mutex
	// replaceCalls counts calls to replaceFile; failReplace fails the Nth one.
	// The counter is per *call*, not per path, so a test says "fail the Nth
	// content step".
	replaceCalls int
	failReplace  map[int]error
	// removeCalls counts calls to removeFile (deletes and the removes a staging
	// failure path performs); failRemove fails the Nth one.
	removeCalls int
	failRemove  map[int]error
	// removeDirCalls counts calls to removeDir; failRemoveDir fails the Nth.
	removeDirCalls int
	failRemoveDir  map[int]error
	// identityCalls counts calls to identity; identityOverride replaces the real
	// answer from identityOverrideFrom on (0 = from the first call), and
	// failIdentity makes the Nth capture fail.
	identityCalls        int
	identityOverride     *workspace.RootIdentity
	identityOverrideFrom int
	failIdentity         map[int]error
}

func newFaultFS() *faultFS {
	return &faultFS{
		inner:         osFS{},
		failReplace:   map[int]error{},
		failRemove:    map[int]error{},
		failRemoveDir: map[int]error{},
		failIdentity:  map[int]error{},
	}
}

var _ fsOps = (*faultFS)(nil)

func (f *faultFS) identity(path string) (workspace.RootIdentity, error) {
	f.mu.Lock()
	f.identityCalls++
	call := f.identityCalls
	var override *workspace.RootIdentity
	if f.identityOverride != nil && call >= f.identityOverrideFrom {
		override = f.identityOverride
	}
	failErr := f.failIdentity[call]
	f.mu.Unlock()
	if failErr != nil {
		return workspace.RootIdentity{}, failErr
	}
	if override != nil {
		return *override, nil
	}
	return f.inner.identity(path)
}

func (f *faultFS) lstat(path string) (fsEntry, error) { return f.inner.lstat(path) }

func (f *faultFS) hashFile(path string) (string, int64, error) { return f.inner.hashFile(path) }

func (f *faultFS) open(path string) (io.ReadCloser, error) { return f.inner.open(path) }

func (f *faultFS) mkdir(path string, perm os.FileMode) error { return f.inner.mkdir(path, perm) }

func (f *faultFS) removeFile(path string) error {
	f.mu.Lock()
	f.removeCalls++
	call := f.removeCalls
	failErr := f.failRemove[call]
	f.mu.Unlock()
	if failErr != nil {
		return failErr
	}
	return f.inner.removeFile(path)
}

func (f *faultFS) removeDir(path string) error {
	f.mu.Lock()
	f.removeDirCalls++
	call := f.removeDirCalls
	failErr := f.failRemoveDir[call]
	f.mu.Unlock()
	if failErr != nil {
		return failErr
	}
	return f.inner.removeDir(path)
}

func (f *faultFS) readDir(path string) ([]string, error) { return f.inner.readDir(path) }

func (f *faultFS) createTemp(dir, pattern string) (tempFile, error) {
	return f.inner.createTemp(dir, pattern)
}

func (f *faultFS) replaceFile(tmpPath, targetPath string, perm os.FileMode) error {
	f.mu.Lock()
	f.replaceCalls++
	call := f.replaceCalls
	failErr := f.failReplace[call]
	f.mu.Unlock()
	if failErr != nil {
		return failErr
	}
	return f.inner.replaceFile(tmpPath, targetPath, perm)
}

// --- inspection helpers used by assertions ---

func (f *faultFS) replaces() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.replaceCalls
}

func (f *faultFS) removes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.removeCalls
}

// --- blobs ---

// countingBlobs is recordingBlobs plus failure injection and an Open that
// serves what was Put, so the publisher can read its own backups and new
// content back. recordingBlobs alone only implements Put (it was written for
// Prepare), which is why this exists rather than reusing it.
type countingBlobs struct {
	mu       sync.Mutex
	contents map[string]string
	puts     int
	opens    int
	// failPutAt makes the Nth Put fail.
	failPutAt int
	// failOpenAt makes the Nth Open fail.
	failOpenAt int
}

func newCountingBlobs() *countingBlobs {
	return &countingBlobs{contents: map[string]string{}}
}

var _ BlobStore = (*countingBlobs)(nil)

func (b *countingBlobs) Put(ctx context.Context, r io.Reader) (string, int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.puts++
	if b.failPutAt > 0 && b.puts == b.failPutAt {
		return "", 0, fmt.Errorf("blob store refuses the %d-th object", b.puts)
	}
	sum := sha256.Sum256(data)
	hash := "sha256:" + hex.EncodeToString(sum[:])
	b.contents[hash] = string(data)
	return hash, int64(len(data)), nil
}

func (b *countingBlobs) Open(hash string) (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.opens++
	if b.failOpenAt > 0 && b.opens == b.failOpenAt {
		return nil, fmt.Errorf("blob store refuses the %d-th open", b.opens)
	}
	content, ok := b.contents[hash]
	if !ok {
		return nil, fmt.Errorf("blob %s is not in the store", hash)
	}
	return io.NopCloser(bytes.NewReader([]byte(content))), nil
}

func (b *countingBlobs) has(hash string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.contents[hash]
	return ok
}

func (b *countingBlobs) putCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.puts
}

// --- hooks: the scripted failure points ---

// failAtContentStep fails the content step of a specific (seq, stage) pair. The
// stages are Stage* in fs.go; StageBeforeReplace sits inside contentStep, while
// StageBeforeMark is checked after the disk write and before the journal mark,
// so the two together pin "before the write" and "after the write".
func failAtContentStep(seq int64, stage string) Hook {
	return func(s Step) error {
		if s.Seq == seq && s.Stage == stage {
			return fmt.Errorf("scripted failure at seq %d stage %s", seq, stage)
		}
		return nil
	}
}

// failOnce returns an error the first time it is called and nil afterwards, for
// hooks that should fail exactly one step.
func failOnce(msg string) func(Step) error {
	var failed atomic.Bool
	return func(step Step) error {
		if failed.CompareAndSwap(false, true) {
			return fmt.Errorf("%s at %s (seq %d, path %s)", msg, step.Stage, step.Seq, step.Path)
		}
		return nil
	}
}

// --- tiny helpers ---

// targetFile is a tiny helper for "what does the user's directory hold now".
func targetFile(root, rel string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", false
	}
	return string(data), true
}
