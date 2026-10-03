// The file-system seam of the merge publisher (T1.09.b group 3).
//
// Publish is the one operation in CodeFlow that writes a user's own directory,
// and the ordering of those writes is the thing its tests must pin. The seam in
// this file exists so they can: the publisher never calls os directly, it calls
// an fsOps, and a test hands in an implementation that fails at a chosen step
// of a chosen file. Nothing here is a global — the seam is an input
// (PublishInput.FS, PublishInput.Hook), so two tests running in parallel cannot
// see each other's failures.
//
// The interface is deliberately the *primitives* the publish algorithm needs,
// not a file-system abstraction: there is no "write file", because writing a
// file correctly (temp in the same directory, fsync, replace, mode) is the
// algorithm this card is about and it must be visible in publish.go. What is
// abstracted is only what touches the machine.
package merge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/codeflow/backend/internal/workspace"
)

// fsEntry is what the publisher knows about one path before it writes it.
//
// It is a value rather than an os.FileInfo because every field here is one the
// publish rules test by name, and because a test implementation must be able to
// produce one without an actual file.
type fsEntry struct {
	// Exists is false when the path is not present at all.
	Exists bool
	// IsDir, IsSymlink and IsRegular describe the entry when it exists. They
	// are independent: a symlink to a directory is IsSymlink and not IsDir,
	// because the publisher never follows a link.
	IsDir     bool
	IsSymlink bool
	IsRegular bool
	Size      int64
	// Mode is the permission bits (os.FileMode.Perm()).
	Mode os.FileMode
}

// tempFile is a staging file in the target's own directory. It is an interface
// only so a test can count and fail Sync/Write; production uses *os.File.
type tempFile interface {
	Name() string
	Write(p []byte) (int, error)
	Sync() error
	Close() error
}

// fsOps is every file-system operation the publisher performs.
//
// Paths are absolute: the publisher resolves them from TargetRoot once, and the
// seam is not the place where path safety is enforced (safeRelativePath and the
// join in publish.go are). A path in this interface is always inside the target
// root.
type fsOps interface {
	// identity returns the OS identity of an existing directory. It follows
	// links, exactly like workspace.CaptureRootIdentity.
	identity(path string) (workspace.RootIdentity, error)
	// lstat describes path without following a final link.
	lstat(path string) (fsEntry, error)
	// hashFile streams path and returns its content hash ("sha256:" + 64 hex)
	// and size. The caller is expected to have checked with lstat that the path
	// is a regular file.
	hashFile(path string) (string, int64, error)
	// open returns the content of path for reading (a regular file).
	open(path string) (io.ReadCloser, error)
	// mkdir creates one directory. It fails when the directory already exists,
	// so "create it" and "it is already there" never look the same.
	mkdir(path string, perm os.FileMode) error
	// removeFile removes a regular file or a link.
	removeFile(path string) error
	// removeDir removes one empty directory.
	removeDir(path string) error
	// readDir lists the names in a directory.
	readDir(path string) ([]string, error)
	// createTemp creates an empty staging file in dir.
	createTemp(dir, pattern string) (tempFile, error)
	// replaceFile makes targetPath hold the content of tmpPath: it sets the
	// mode and then replaces the target with the staged file in one step (see
	// the platform note in the method).
	replaceFile(tmpPath, targetPath string, perm os.FileMode) error
}

// osFS is the production implementation: the standard library, nothing else.
type osFS struct{}

var _ fsOps = osFS{}

func (osFS) identity(path string) (workspace.RootIdentity, error) {
	return workspace.CaptureRootIdentity(path)
}

func (osFS) lstat(path string) (fsEntry, error) {
	info, err := os.Lstat(path)
	switch {
	case os.IsNotExist(err):
		return fsEntry{}, nil
	case err != nil:
		return fsEntry{}, fmt.Errorf("merge: lstat %s: %w", path, err)
	}
	mode := info.Mode()
	return fsEntry{
		Exists:    true,
		IsDir:     mode.IsDir(),
		IsSymlink: mode&os.ModeSymlink != 0,
		IsRegular: mode.IsRegular(),
		Size:      info.Size(),
		Mode:      mode.Perm(),
	}, nil
}

func (osFS) hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("merge: open %s: %w", path, err)
	}
	defer f.Close()
	sum := sha256.New()
	n, err := io.Copy(sum, f)
	if err != nil {
		return "", 0, fmt.Errorf("merge: read %s: %w", path, err)
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil)), n, nil
}

func (osFS) open(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("merge: open %s: %w", path, err)
	}
	return f, nil
}

func (osFS) mkdir(path string, perm os.FileMode) error {
	if err := os.Mkdir(path, perm); err != nil {
		return fmt.Errorf("merge: create directory %s: %w", path, err)
	}
	return nil
}

func (osFS) removeFile(path string) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("merge: remove %s: %w", path, err)
	}
	return nil
}

func (osFS) removeDir(path string) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("merge: remove directory %s: %w", path, err)
	}
	return nil
}

func (osFS) readDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("merge: read directory %s: %w", path, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

func (osFS) createTemp(dir, pattern string) (tempFile, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, fmt.Errorf("merge: create staging file in %s: %w", dir, err)
	}
	return f, nil
}

// replaceFile is the single-file atomic replacement of §27.5 item 4, and the
// platform difference is real enough that it is spelled out here rather than in
// a comment elsewhere:
//
//   - POSIX: os.Rename is rename(2). It replaces the destination atomically —
//     a concurrent reader sees either the old file or the new one, never a
//     truncated mix — and the mode was set on the staging file before the
//     rename, so the file appears with its final mode.
//   - Windows: os.Rename is MoveFileExW with MOVEFILE_REPLACE_EXISTING (see
//     Go's internal/syscall/windows.Rename). That is the single-file
//     replace-or-move primitive of Win32 and it is what the platform offers;
//     the documented limitation is that it is not transactional against a
//     second writer of the same path and can fail with a sharing violation
//     while another process holds the file open. A failure is reported, never
//     retried silently: the caller rolls back.
//   - Windows permissions: NTFS has no POSIX permission bits. os.Chmod there
//     can only toggle the read-only attribute, so the mode is applied on a
//     best-effort basis and a failure to apply it does not fail the publish
//     (the content is what the candidate promised; the mode is metadata the
//     platform does not really hold). On POSIX the chmod is part of the
//     operation: a file that appears with the wrong mode is a wrong result.
func (osFS) replaceFile(tmpPath, targetPath string, perm os.FileMode) error {
	if err := os.Chmod(tmpPath, perm); err != nil {
		if runtime.GOOS != "windows" {
			return fmt.Errorf("merge: set mode %v on %s: %w", perm, tmpPath, err)
		}
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("merge: replace %s: %w", targetPath, err)
	}
	return nil
}

// syncDir asks the file system to persist a directory entry (the rename above).
//
// It is best-effort by design and its failure is not returned: Windows has no
// portable directory fsync (os.Open on a directory and Fsync fail or are
// meaningless there), and the guarantee this publisher offers is not crash
// durability of the directory — that is the blob store's job for content and
// the journal's job for the plan. What the caller gets from the rename is
// atomic *visibility*, which does not depend on this call.
func syncDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	f, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = f.Sync()
	_ = f.Close()
}

// --- hooks: the scripted failure points ---

// The moments at which a test may fail the publish. They are named after the
// step they sit next to:
//
//	StageBeforeReplace  the content step (replace or remove) has not happened;
//	StageAfterReplace   the content step happened, the journal does not know;
//	StageBeforeMark     as above, and the next statement is the journal mark;
//	StageBeforeEvent    inside transaction 3, after the operation row was moved
//	                    to applied and before merge.completed is appended. A
//	                    failure here aborts the whole transaction (the operation
//	                    stays applying and the caller rolls back), which is how
//	                    a test pins "applied and the event are one transaction".
const (
	StageBeforeReplace = "before_replace"
	StageAfterReplace  = "after_replace"
	StageBeforeMark    = "before_mark"
	StageBeforeEvent   = "before_event"
)

// Step describes where the publisher is when a Hook is called.
type Step struct {
	OperationID string
	// Seq is the 1-based position of the file in the publish order.
	Seq  int64
	Path string
	Kind string
	// Stage is one of the Stage* constants.
	Stage string
}

// Hook is an optional callback the publisher calls at each Stage. A non-nil
// error stops the publish exactly like a file-system error would: the caller
// gets it wrapped in the failure path (rollback included) it interrupted.
//
// Production passes nil. It is an input field rather than a package variable on
// purpose: a global "fail the next replace" would be visible to every other
// test running in the same process.
type Hook func(Step) error

func (h Hook) call(step Step) error {
	if h == nil {
		return nil
	}
	return h(step)
}

// --- path helpers ---

// targetPath joins a journal path to the target root. The path was validated
// with safeRelativePath before it was stored; this re-checks, because the
// journal is data and a recovery replays it without the candidate.
func targetPath(root, rel string) (string, error) {
	if !safeRelativePath(rel) {
		return "", fmt.Errorf("%w: %q", ErrUnsafeManifestPath, rel)
	}
	local := filepath.Join(root, filepath.FromSlash(rel))
	back, err := filepath.Rel(root, local)
	if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q escapes %s", ErrUnsafeManifestPath, rel, root)
	}
	return local, nil
}

// ancestorDirs returns the directories between the root and path, parents
// first, as paths relative to the root with forward slashes. The path itself is
// never included.
func ancestorDirs(rel string) []string {
	segments := strings.Split(rel, "/")
	if len(segments) <= 1 {
		return nil
	}
	out := make([]string, 0, len(segments)-1)
	for i := 1; i < len(segments); i++ {
		out = append(out, strings.Join(segments[:i], "/"))
	}
	return out
}
