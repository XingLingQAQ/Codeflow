package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
)

// DefaultMaxBlobBytes is the per-blob limit Put enforces when no option says
// otherwise: 64 MiB.
//
// The number is deliberately not runworkspace.DefaultMaxFileBytes (5 MiB): a
// blob is a stored artifact — an edited file, a merge backup, a diff — and the
// 5 MiB bound there exists to keep a *baseline traversal* cheap. 64 MiB is
// still a bound, because an unbounded Put would let one caller fill the disk
// and because a bound is what makes "the stream is not the content" a decision
// this store can state.
const DefaultMaxBlobBytes int64 = 64 << 20

// BlobStoreOptions tunes a BlobStore. The zero value uses the defaults.
type BlobStoreOptions struct {
	// MaxBlobBytes caps one blob's size; 0 or negative means
	// DefaultMaxBlobBytes.
	MaxBlobBytes int64
}

func (o BlobStoreOptions) maxBlobBytes() int64 {
	if o.MaxBlobBytes <= 0 {
		return DefaultMaxBlobBytes
	}
	return o.MaxBlobBytes
}

// Sentinel errors of the blob store.
var (
	// ErrInvalidHash means a string is not a content hash this store can
	// address: missing HashPrefix, wrong length, or non-lowercase hex.
	ErrInvalidHash = errors.New("artifact: invalid content hash")

	// ErrBlobNotFound means no blob is stored under the requested hash.
	ErrBlobNotFound = errors.New("artifact: blob not found")

	// ErrBlobCorrupt means a file exists under the hash but its bytes do not
	// hash to it. It is never swallowed: a reader that treated a corrupt blob
	// as a hit would hand out wrong content under a name that promises the
	// right one.
	ErrBlobCorrupt = errors.New("artifact: blob content does not match its hash")

	// ErrBlobTooLarge means the stream exceeded the store's per-blob limit.
	// Nothing is published: the partial temp file is removed.
	ErrBlobTooLarge = errors.New("artifact: blob exceeds the size limit")

	// ErrBlobStoreClosed means the store was used after Close.
	ErrBlobStoreClosed = errors.New("artifact: blob store is closed")
)

// BlobStore is a content-addressed file store under one root directory.
//
// Layout:
//
//	<root>/sha256/<first 2 hex>/<remaining 62 hex>   the blobs, immutable once published
//	<root>/tmp/                                      write staging, never visible
//
// The shard directory keeps a single directory from holding every blob; the
// two-character prefix is the classic content-store split and matches the
// "sha256:" tree that a future `fsck`-style sweep (T12.02) will walk.
//
// A blob is immutable by construction, not by policy: its name is the hash of
// its bytes, so rewriting it would only ever make it unreachable. Publishing is
// atomic from a reader's point of view (see Put), and a file under tmp/ is
// never a blob: Has, Open and Stat do not look there, so a crash mid-write
// leaves nothing a reader could mistake for content.
//
// BlobStore is safe for concurrent use. Its only mutable state is the closed
// flag, which is atomic so Close may race with Put, Open, Stat and Has.
type BlobStore struct {
	root         string
	blobsRoot    string
	tmpRoot      string
	maxBlobBytes int64
	closed       atomic.Bool
}

// OpenBlobStore opens (or creates) a blob store rooted at root.
//
// root must be an absolute path: a relative root would resolve against the
// process working directory, so the same store would address different files
// before and after a chdir, and two runs would silently share or miss blobs.
// The directory itself is created if missing, as are root/sha256 and root/tmp.
func OpenBlobStore(root string, opts BlobStoreOptions) (*BlobStore, error) {
	if root == "" {
		return nil, fmt.Errorf("artifact: blob root is required")
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("artifact: blob root must be an absolute path, got %q", root)
	}
	abs := filepath.Clean(root)

	s := &BlobStore{
		root:         abs,
		blobsRoot:    filepath.Join(abs, "sha256"),
		tmpRoot:      filepath.Join(abs, "tmp"),
		maxBlobBytes: opts.maxBlobBytes(),
	}
	for _, dir := range []string{s.blobsRoot, s.tmpRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("artifact: create %s: %w", dir, err)
		}
	}
	return s, nil
}

// Close marks the store closed. It has no file handle to release — every Put
// and Open opens and closes its own file — so Close exists to make "used after
// shutdown" a loud error rather than a write into a directory the caller has
// already deleted. It is idempotent.
func (s *BlobStore) Close() error {
	if s == nil {
		return nil
	}
	s.closed.Store(true)
	return nil
}

// Root returns the absolute root directory, so a caller can log or move the
// whole store without guessing the layout.
func (s *BlobStore) Root() string { return s.root }

// Put streams r into the store and returns the content hash and byte count.
//
// The stream is written to <root>/tmp and only then published, so a reader can
// never observe a half-written blob: the file appears under sha256/ with its
// final name and final content, or it does not appear at all. A failure
// anywhere (a read error, the size limit, a canceled context) removes the temp
// file. The temp file is also never conflated with a blob: a later Put of the
// same content is unaffected by a leftover from a crash, because nothing reads
// tmp/.
//
// If the target already exists — because the same content was stored before, or
// because another writer won the race — its bytes are hashed first. Matching
// content means success (this is what makes Put idempotent); different content
// means the stored file is corrupt and Put returns ErrBlobCorrupt rather than
// overwriting it. Overwriting would destroy the only evidence that something
// rewrote a file whose name guarantees its content, and it would hide the
// corruption from every later reader.
//
// Put does not fsync the directory (Windows has no portable directory sync, and
// the guarantee this store offers is ordering between the temp write and the
// publish, not crash-durability of the directory entry). The file itself is
// synced before it is published, so a blob that exists is a blob whose bytes
// reached the filesystem.
func (s *BlobStore) Put(ctx context.Context, r io.Reader) (hash string, size int64, err error) {
	if s == nil {
		return "", 0, fmt.Errorf("artifact: nil BlobStore")
	}
	if s.closed.Load() {
		return "", 0, ErrBlobStoreClosed
	}
	if r == nil {
		return "", 0, fmt.Errorf("artifact: Put needs a reader")
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}

	tmp, err := os.CreateTemp(s.tmpRoot, "put-*.tmp")
	if err != nil {
		return "", 0, fmt.Errorf("artifact: create temp blob: %w", err)
	}
	tmpPath := tmp.Name()
	// From here on every failure path removes the temp file. The deferred
	// removal uses the file name, not the handle, so it works even after tmp is
	// closed below.
	published := false
	defer func() {
		if !published {
			_ = os.Remove(tmpPath)
		}
	}()

	sum := sha256.New()
	// Copy at most max+1 bytes: one byte past the limit is enough to prove the
	// stream is too large without buffering an unbounded one. The reader is
	// wrapped so a canceled context stops the copy mid-stream instead of after
	// the whole blob was written.
	limited := io.LimitReader(&contextReader{ctx: ctx, r: r}, s.maxBlobBytes+1)
	n, copyErr := io.Copy(io.MultiWriter(tmp, sum), limited)
	if copyErr != nil {
		_ = tmp.Close()
		return "", 0, fmt.Errorf("artifact: write temp blob: %w", copyErr)
	}
	if n > s.maxBlobBytes {
		_ = tmp.Close()
		return "", 0, fmt.Errorf("%w: more than %d bytes", ErrBlobTooLarge, s.maxBlobBytes)
	}
	// Sync before publish: a blob whose name is its content must not be able to
	// appear (or be renamed over a crash) with unwritten bytes behind it.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", 0, fmt.Errorf("artifact: sync temp blob: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", 0, fmt.Errorf("artifact: close temp blob: %w", err)
	}

	hash = HashPrefix + hex.EncodeToString(sum.Sum(nil))
	if err := s.publish(tmpPath, hash); err != nil {
		return "", 0, err
	}
	published = true
	// The publish either linked the temp file into place (both names now exist)
	// or renamed it (the temp name is gone). Removing it unconditionally is what
	// keeps tmp/ empty on the success path; the ENOENT from the rename case is
	// not an error, the goal is already met.
	_ = os.Remove(tmpPath)
	return hash, n, nil
}

// publish moves the finished temp file to its content address.
//
// It uses a hard link (create-if-absent) rather than a plain rename on purpose:
// POSIX rename silently replaces the destination, so a plain rename could
// overwrite a corrupt blob with good content and leave no trace that the file
// had ever been wrong. link(2) fails with EEXIST instead, which is exactly the
// "someone else got there first" outcome this code has to distinguish, and it
// is supported on the Windows filesystems (NTFS) the backend runs on.
//
// Filesystems without hard links fall back to rename after an existence check.
// The check-then-rename window there is not atomic, but the only writer that
// can appear in it writes the same bytes (the name is the content hash), except
// for external corruption, which is not something this store can lock out. The
// caller removes the temp file afterwards in either case.
func (s *BlobStore) publish(tmpPath, hash string) error {
	shard, name := hashDirName(hash)
	dir := filepath.Join(s.blobsRoot, shard)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("artifact: create shard %s: %w", shard, err)
	}
	final := filepath.Join(dir, name)

	switch err := os.Link(tmpPath, final); {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrExist):
		return s.reconcileExisting(final, hash)
	default:
		if _, statErr := os.Lstat(final); statErr == nil {
			return s.reconcileExisting(final, hash)
		}
		if renameErr := os.Rename(tmpPath, final); renameErr != nil {
			if errors.Is(renameErr, fs.ErrExist) {
				return s.reconcileExisting(final, hash)
			}
			return fmt.Errorf("artifact: publish blob %s: %w", hash, renameErr)
		}
		return nil
	}
}

// reconcileExisting decides what "the target is already there" means: identical
// content is success (Put is idempotent), anything else is ErrBlobCorrupt. The
// stored file is never modified — see publish.
func (s *BlobStore) reconcileExisting(path, hash string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("artifact: open existing blob %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("artifact: read existing blob %s: %w", path, err)
	}
	if got := HashPrefix + hex.EncodeToString(h.Sum(nil)); got != hash {
		return fmt.Errorf("%w: %s holds %s, wanted %s", ErrBlobCorrupt, path, got, hash)
	}
	return nil
}

// Open returns a reader over the blob stored under hash.
//
// The reader verifies the content as it goes: reading to EOF compares what was
// read against the hash in the name and returns ErrBlobCorrupt if they differ.
// A partial read that never reaches EOF is not verified, because the bytes read
// so far cannot prove anything about the whole file — callers that need an
// early answer use Stat and the size recorded with the version.
//
// Closing the reader does not verify; only EOF does.
func (s *BlobStore) Open(hash string) (io.ReadCloser, error) {
	if s == nil {
		return nil, fmt.Errorf("artifact: nil BlobStore")
	}
	if s.closed.Load() {
		return nil, ErrBlobStoreClosed
	}
	path, err := s.blobPath(hash)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrBlobNotFound, hash)
		}
		return nil, fmt.Errorf("artifact: open blob %s: %w", hash, err)
	}
	return &verifyingReader{f: f, sum: sha256.New(), want: hash}, nil
}

// Stat returns the size of the stored blob in bytes.
//
// It reports existence and size, not integrity: a caller that needs to know the
// bytes really hash to the name reads them through Open. Stat is what
// CreateVersionTx uses to refuse a version pointing at a blob that is not
// there, and what a restore point compares before it copies a blob back out.
func (s *BlobStore) Stat(hash string) (int64, error) {
	if s == nil {
		return 0, fmt.Errorf("artifact: nil BlobStore")
	}
	if s.closed.Load() {
		return 0, ErrBlobStoreClosed
	}
	path, err := s.blobPath(hash)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, fmt.Errorf("%w: %s", ErrBlobNotFound, hash)
		}
		return 0, fmt.Errorf("artifact: stat blob %s: %w", hash, err)
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("%w: %s is not a regular file", ErrBlobCorrupt, hash)
	}
	return info.Size(), nil
}

// Has reports whether a blob is stored under hash. A missing blob is (false,
// nil); a hash that cannot be addressed at all is an error, because "no" and
// "that question is malformed" are different answers.
func (s *BlobStore) Has(hash string) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("artifact: nil BlobStore")
	}
	if s.closed.Load() {
		return false, ErrBlobStoreClosed
	}
	path, err := s.blobPath(hash)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("artifact: stat blob %s: %w", hash, err)
	}
	return info.Mode().IsRegular(), nil
}

// blobPath maps a hash to its file, rejecting anything that is not a valid
// hash. Validation is a precondition rather than a repair so that a malformed
// hash can never be turned into a path (an unvalidated hash is a path-traversal
// hole: "sha256:../../x" would address a file outside the store).
func (s *BlobStore) blobPath(hash string) (string, error) {
	if !ValidHash(hash) {
		return "", fmt.Errorf("%w: %q", ErrInvalidHash, hash)
	}
	shard, name := hashDirName(hash)
	return filepath.Join(s.blobsRoot, shard, name), nil
}

// verifyingReader hashes what it serves and compares at EOF.
type verifyingReader struct {
	f    *os.File
	sum  hash.Hash
	want string
	done bool
	err  error
}

func (r *verifyingReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	n, err := r.f.Read(p)
	if n > 0 {
		r.sum.Write(p[:n])
	}
	// io.Reader allows n > 0 together with io.EOF; the hash is complete either
	// way, so verification happens on the first EOF and rewrites the error if
	// the bytes and the name disagree.
	if err == io.EOF && !r.done {
		r.done = true
		if got := HashPrefix + hex.EncodeToString(r.sum.Sum(nil)); got != r.want {
			r.err = fmt.Errorf("%w: stored file holds %s, wanted %s", ErrBlobCorrupt, got, r.want)
			return n, r.err
		}
	}
	return n, err
}

func (r *verifyingReader) Close() error { return r.f.Close() }

// contextReader turns a canceled context into a read error, so a Put that is
// being cancelled stops mid-stream instead of after the whole body was read.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
