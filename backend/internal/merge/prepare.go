package merge

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/codeflow/backend/internal/runworkspace"
)

// PrepareInput is the triple a candidate is computed from.
type PrepareInput struct {
	// Base is the frozen baseline the run started from.
	Base *runworkspace.Manifest
	// Target is the user's directory as it is right now, captured again
	// immediately before publishing. It is what decides whether a change can
	// still be applied or whether a third party has moved the path.
	Target *runworkspace.Manifest
	// Result is the capture of the run's working copy after the run finished.
	Result *runworkspace.Manifest
	// ResultRoot is the working copy directory Result describes. New content
	// is read from here and only from here.
	ResultRoot string
	// Blobs stores the new content of every create and modify. The hash it
	// returns is the hash the candidate, the diff, and the backup all use.
	Blobs BlobWriter
}

// Prepare compares base, current target, and run result per path and returns
// the candidate change set, or an error when the inputs cannot be compared at
// all.
//
// The rules, per path:
//
//   - result equals base: the run did not touch the path. Nothing is
//     published, so the user's dirty edits and any edit made after the run
//     started both survive.
//   - result differs from base and target still equals base: an operation.
//   - result differs from base and target already equals result: the change is
//     already in place and only recorded in AlreadyApplied.
//   - result differs from base and target is a third state: a
//     target_changed conflict. The target is never overwritten with the run's
//     content on the assumption that it is "fresher": the frozen base is the
//     only thing the run's change was computed against.
//
// A path whose current state the capture refused to describe is a conflict,
// never a silent skip: a new secret or oversize file the run created is
// result_excluded, and a path the target capture excluded while the run
// changed it is target_excluded. A path that both base and result excluded is
// left alone.
//
// Prepare writes nothing: not the target root, not the working copy.
func Prepare(ctx context.Context, in PrepareInput) (*Candidate, error) {
	if err := validateInput(in); err != nil {
		return nil, err
	}

	b := &builder{
		ctx:        ctx,
		base:       newView(in.Base),
		result:     newView(in.Result),
		target:     newView(in.Target),
		resultRoot: filepath.Clean(in.ResultRoot),
		blobs:      in.Blobs,
		operations: []Operation{},
		applied:    []string{},
		conflicts:  []Conflict{},
	}

	// The candidate covers the union of all three manifests, exclusions
	// included: an excluded path has no entry to compare, so its exclusion is
	// the only evidence that the run put something there.
	for _, path := range unionPaths(in.Base, in.Result, in.Target) {
		if isInternalPath(path) {
			continue
		}
		if err := b.classify(path); err != nil {
			return nil, err
		}
	}

	pairRenames(b.operations)

	c := &Candidate{
		FormatVersion:      FormatVersion,
		Root:               in.Target.Root,
		BaseManifestHash:   in.Base.Hash,
		TargetManifestHash: in.Target.Hash,
		ResultManifestHash: in.Result.Hash,
		Operations:         b.operations,
		AlreadyApplied:     b.applied,
		Conflicts:          b.conflicts,
	}
	c.Hash = candidateHash(c)
	return c, nil
}

// builder accumulates one path decision at a time. Every slice it produces is
// appended in ascending path order, and nothing it produces depends on map
// iteration order.
type builder struct {
	ctx        context.Context
	base       *manifestView
	result     *manifestView
	target     *manifestView
	resultRoot string
	blobs      BlobWriter

	operations []Operation
	applied    []string
	conflicts  []Conflict
}

// manifestView indexes one manifest by path.
type manifestView struct {
	byPath   map[string]runworkspace.Entry
	excluded map[string]string
}

func newView(m *runworkspace.Manifest) *manifestView {
	v := &manifestView{
		byPath:   make(map[string]runworkspace.Entry, len(m.Entries)),
		excluded: exclusionIndex(m),
	}
	for _, e := range m.Entries {
		v.byPath[e.Path] = e
	}
	return v
}

// entry returns the entry for path, or nil when the manifest has none. A
// deleted entry is returned as-is: callers distinguish "the manifest says the
// path is gone" from "the manifest never mentioned the path" with entryExists.
func (v *manifestView) entry(path string) *runworkspace.Entry {
	if e, ok := v.byPath[path]; ok {
		copy := e
		return &copy
	}
	return nil
}

func exclusionIndex(m *runworkspace.Manifest) map[string]string {
	out := make(map[string]string, len(m.Excluded))
	for _, x := range m.Excluded {
		if _, seen := out[x.Path]; !seen {
			out[x.Path] = x.Reason
		}
	}
	return out
}

// unionPaths returns every path any of the three manifests mentions, entries
// and exclusions alike, in ascending order.
func unionPaths(ms ...*runworkspace.Manifest) []string {
	seen := map[string]bool{}
	for _, m := range ms {
		for _, e := range m.Entries {
			seen[e.Path] = true
		}
		for _, x := range m.Excluded {
			seen[x.Path] = true
		}
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// pathState is everything the three captures say about one path. It is a
// plain value so the decision it feeds can be tested without a file system.
type pathState struct {
	base   *runworkspace.Entry
	result *runworkspace.Entry
	target *runworkspace.Entry
	// resultExcluded and targetExcluded are the capture reasons, empty when the
	// manifest has an entry for the path.
	resultExcluded string
	targetExcluded string
	// resultExclusionIsNew is true when the result excluded a path the base did
	// not exclude, which is how "the run created something the capture will not
	// describe" is told apart from "a path that was already excluded".
	resultExclusionIsNew bool
}

// pathDecision is the outcome for one path: at most one of an operation kind,
// an already-applied mark, and a conflict.
type pathDecision struct {
	kind           string
	alreadyApplied bool
	conflict       string
}

// decide is the three-way comparison for one path. It is pure: it reads no
// clock, no file system, and no map, so the same three states always produce
// the same decision, on any platform.
//
// The order of the checks matters and is part of the rule:
//
//  1. A path the result capture newly excluded is a conflict before anything
//     else: the run produced something the manifest cannot describe (a secret,
//     an oversize file, a link out of the tree), so no content hash exists to
//     publish under, whether or not the target also looks odd.
//  2. A path the run did not change is left out of the change set. This is
//     checked before the target is examined, so a third state at such a path is
//     not a conflict — the candidate does not mention the path at all.
//  3. A changed path whose current target state the capture refuses to describe
//     is target_excluded.
//  4. Otherwise the target is compared with base and result: equal to base
//     means plan the change, equal to result means it is already applied, and
//     anything else is target_changed.
func decide(s pathState) pathDecision {
	if s.resultExclusionIsNew {
		return pathDecision{conflict: excludeReason(ReasonResultExcluded, s.resultExcluded)}
	}
	if sameState(s.base, s.result) {
		return pathDecision{}
	}
	if s.targetExcluded != "" {
		return pathDecision{conflict: excludeReason(ReasonTargetExcluded, s.targetExcluded)}
	}
	switch {
	case sameState(s.target, s.base):
		baseExists, resultExists := entryExists(s.base), entryExists(s.result)
		switch {
		case !baseExists:
			return pathDecision{kind: KindCreate}
		case !resultExists:
			return pathDecision{kind: KindDelete}
		default:
			return pathDecision{kind: KindModify}
		}
	case sameState(s.target, s.result):
		return pathDecision{alreadyApplied: true}
	default:
		return pathDecision{conflict: ReasonTargetChanged}
	}
}

// isInternalPath reports whether path is CodeFlow's own bookkeeping rather than
// project content. Only the root-level ownership marker qualifies:
// runworkspace.Materialize writes it at the root of the working copy, and it
// must never be published into the user's tree or reported as a change the run
// made. A file with the same name deeper in the tree is ordinary project
// content and is compared like any other path. The name comes from
// runworkspace.OwnerMarkerName(), so it stays defined in one place.
func isInternalPath(path string) bool {
	return path == runworkspace.OwnerMarkerName()
}

// classify decides one path and records the outcome.
func (b *builder) classify(path string) error {
	state := pathState{
		base:           b.base.entry(path),
		result:         b.result.entry(path),
		target:         b.target.entry(path),
		resultExcluded: b.result.excluded[path],
		targetExcluded: b.target.excluded[path],
	}
	if state.resultExcluded != "" {
		// An exclusion the base already carried is not the run's doing: a
		// secret that was a secret before the run stays private and silent. An
		// exclusion the base did not have is a path the run created in a shape
		// the capture will not describe.
		_, alreadyExcluded := b.base.excluded[path]
		state.resultExclusionIsNew = !alreadyExcluded
	}

	switch d := decide(state); {
	case d.conflict != "":
		b.conflict(path, d.conflict, state.base, state.result, state.target)
	case d.alreadyApplied:
		b.applied = append(b.applied, path)
	case d.kind != "":
		return b.plan(path, d.kind, state.base, state.result)
	}
	return nil
}

// plan turns a base-to-result change into an operation, storing the new
// content first so a failure leaves no half-described operation behind.
func (b *builder) plan(path, kind string, base, result *runworkspace.Entry) error {
	op := Operation{Path: path, Kind: kind}
	if kind != KindDelete {
		binary, err := b.storeNewContent(path, *result)
		if err != nil {
			return err
		}
		op.NewType = result.Type
		op.NewHash = result.ContentHash
		op.NewMode = result.Mode
		op.Size = result.Size
		op.Binary = binary
	}
	if kind != KindCreate {
		op.OldType = base.Type
		op.OldHash = base.ContentHash
		op.OldMode = base.Mode
	}

	b.operations = append(b.operations, op)
	return nil
}

// conflict records a path the candidate cannot publish.
func (b *builder) conflict(path, reason string, base, result, target *runworkspace.Entry) {
	b.conflicts = append(b.conflicts, Conflict{
		Path:   path,
		Reason: reason,
		Base:   summarize(base),
		Result: summarize(result),
		Target: summarize(target),
	})
}

// storeNewContent reads the new content of a create or modify from the
// working copy and stores it as a blob. The bytes are never taken from the
// target root, and a link is never followed: a file is read through a regular
// file handle opened after an Lstat, and a symlink contributes its link target
// string.
//
// The blob store answers with the hash of what it stored, and that hash is
// required to equal the hash the result manifest recorded. A mismatch means
// the working copy changed after the result was captured, and no candidate is
// produced from a manifest that no longer describes the working copy.
func (b *builder) storeNewContent(path string, e runworkspace.Entry) (bool, error) {
	local, err := b.localPath(path)
	if err != nil {
		return false, err
	}
	switch e.Type {
	case runworkspace.TypeFile:
		return b.storeFile(path, local, e)
	case runworkspace.TypeSymlink:
		return false, b.storeSymlink(path, local, e)
	default:
		return false, fmt.Errorf("%w: %s has type %q", ErrResultChangedAfterCapture, path, e.Type)
	}
}

// storeFile streams a regular file into the blob store and reports whether its
// first binarySniffBytes bytes contain a NUL byte.
func (b *builder) storeFile(path, local string, e runworkspace.Entry) (bool, error) {
	info, err := os.Lstat(local)
	if err != nil {
		return false, fmt.Errorf("%w: lstat %s: %v", ErrResultChangedAfterCapture, path, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%w: %s is %s on disk, the manifest says %s",
			ErrResultChangedAfterCapture, path, modeWord(info.Mode()), e.Type)
	}
	if info.Size() != e.Size {
		return false, fmt.Errorf("%w: %s is %d bytes on disk, the manifest says %d",
			ErrResultChangedAfterCapture, path, info.Size(), e.Size)
	}

	f, err := os.Open(local)
	if err != nil {
		return false, fmt.Errorf("%w: open %s: %v", ErrResultChangedAfterCapture, path, err)
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, binarySniffBytes)
	head, err := br.Peek(binarySniffBytes)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("merge: read %s: %w", path, err)
	}
	binary := bytes.IndexByte(head, 0) >= 0

	// Exactly the recorded number of bytes goes to the store: the manifest
	// bounds the content the run produced, so a file that grew between the
	// capture and now cannot be stored under the captured hash.
	hash, size, err := b.blobs.Put(b.ctx, io.LimitReader(br, e.Size))
	if err != nil {
		return false, fmt.Errorf("merge: store %s: %w", path, err)
	}
	if hash != e.ContentHash || size != e.Size {
		return false, fmt.Errorf("%w: %s (manifest %s/%d bytes, stored %s/%d bytes)",
			ErrResultChangedAfterCapture, path, e.ContentHash, e.Size, hash, size)
	}
	if _, err := br.ReadByte(); err == nil {
		return false, fmt.Errorf("%w: %s grew past the manifest size of %d bytes",
			ErrResultChangedAfterCapture, path, e.Size)
	} else if !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("merge: read %s: %w", path, err)
	}
	return binary, nil
}

// storeSymlink stores a link target string as the content of a blob, so a
// publisher and a backup can recreate the link from the blob store alone. The
// hash of that string is exactly the manifest content hash of a symlink entry.
func (b *builder) storeSymlink(path, local string, e runworkspace.Entry) error {
	target, err := os.Readlink(local)
	if err != nil {
		return fmt.Errorf("%w: readlink %s: %v", ErrResultChangedAfterCapture, path, err)
	}
	hash, size, err := b.blobs.Put(b.ctx, strings.NewReader(target))
	if err != nil {
		return fmt.Errorf("merge: store %s: %w", path, err)
	}
	// The link target string is the content: a link whose target changed has
	// different content, and the manifest's Size is that string's length.
	if hash != e.ContentHash || size != int64(len(target)) {
		return fmt.Errorf("%w: %s (manifest %s/%d bytes, stored %s/%d bytes)",
			ErrResultChangedAfterCapture, path, e.ContentHash, e.Size, hash, size)
	}
	return nil
}

// localPath resolves a manifest path inside the result root, refusing anything
// that would leave it. A manifest is data: it may have been edited or replayed
// since it was captured, so its paths are not joined to a root unchecked.
func (b *builder) localPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%w: empty path", ErrUnsafeManifestPath)
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("%w: %q", ErrUnsafeManifestPath, path)
		}
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) || filepath.IsAbs(filepath.FromSlash(path)) {
		return "", fmt.Errorf("%w: %q", ErrUnsafeManifestPath, path)
	}
	local := filepath.Join(b.resultRoot, filepath.FromSlash(path))
	rel, err := filepath.Rel(b.resultRoot, local)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q escapes %s", ErrUnsafeManifestPath, path, b.resultRoot)
	}
	return local, nil
}

// pairRenames marks one delete and one create as a rename when the create's
// new content is the delete's old content. Both operations are kept: the
// publisher still has to remove the old path and add the new one, but a reader
// can present the pair as a move, and the backup can keep one copy of the
// content under one hash.
//
// Pairing is deterministic: candidates are grouped by content, the groups are
// visited in sorted key order, and within a group the paths are already sorted
// by path (operations are appended in ascending path order). The nth create of
// a group pairs with the nth delete of that group; leftovers stay unpaired.
func pairRenames(ops []Operation) {
	type contentKey struct {
		typ  string
		hash string
	}
	creates := map[contentKey][]int{}
	deletes := map[contentKey][]int{}
	for i, op := range ops {
		switch op.Kind {
		case KindCreate:
			key := contentKey{op.NewType, op.NewHash}
			creates[key] = append(creates[key], i)
		case KindDelete:
			key := contentKey{op.OldType, op.OldHash}
			deletes[key] = append(deletes[key], i)
		}
	}

	keys := make([]contentKey, 0, len(creates)+len(deletes))
	for key := range creates {
		keys = append(keys, key)
	}
	for key := range deletes {
		if _, both := creates[key]; !both {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].typ != keys[j].typ {
			return keys[i].typ < keys[j].typ
		}
		return keys[i].hash < keys[j].hash
	})

	for _, key := range keys {
		cs, ds := creates[key], deletes[key]
		if len(cs) > len(ds) {
			cs = cs[:len(ds)]
		}
		for i := range cs {
			// The slice indices are ascending, and operations were appended in
			// ascending path order, so both halves of a pair are in path order.
			ops[cs[i]].RenamedFrom = ops[ds[i]].Path
			ops[ds[i]].RenamedTo = ops[cs[i]].Path
		}
	}
}

// summarize describes one side of a conflict. A nil entry and an entry that
// holds no content both carry Exists false, but the deleted type is kept so a
// reader can see that the manifest recorded a removal rather than never
// mentioning the path.
func summarize(e *runworkspace.Entry) *EntrySummary {
	if e == nil {
		return nil
	}
	return &EntrySummary{
		Exists:      entryExists(e),
		Type:        e.Type,
		ContentHash: e.ContentHash,
		Mode:        e.Mode,
		GitStatus:   e.GitStatus,
	}
}

// excludeReason appends the capture's exclusion reason to a conflict reason.
func excludeReason(reason, detail string) string {
	if detail == "" {
		return reason
	}
	return reason + ":" + detail
}

// modeWord names a file mode for an error message without leaking a symbolic
// mode string that differs between platforms.
func modeWord(mode os.FileMode) string {
	switch {
	case mode.IsRegular():
		return "a regular file"
	case mode&os.ModeSymlink != 0:
		return "a symlink"
	case mode&os.ModeDir != 0:
		return "a directory"
	default:
		return "not a regular file"
	}
}

// validateInput checks the triples before a single path is compared. A
// manifest that does not describe itself, or base and target that do not
// describe the same tree, cannot produce a trustworthy candidate.
func validateInput(in PrepareInput) error {
	if in.Base == nil {
		return errors.New("merge: base manifest is required")
	}
	if in.Target == nil {
		return errors.New("merge: target manifest is required")
	}
	if in.Result == nil {
		return errors.New("merge: result manifest is required")
	}
	if in.Blobs == nil {
		return errors.New("merge: blob writer is required")
	}
	if strings.TrimSpace(in.ResultRoot) == "" {
		return fmt.Errorf("%w: result root is required", ErrRootMismatch)
	}
	if err := verifyManifest("base", in.Base); err != nil {
		return err
	}
	if err := verifyManifest("target", in.Target); err != nil {
		return err
	}
	if err := verifyManifest("result", in.Result); err != nil {
		return err
	}
	if in.Base.Mode != in.Target.Mode {
		return fmt.Errorf("%w: base was captured in %q mode, target in %q", ErrModeMismatch, in.Base.Mode, in.Target.Mode)
	}
	if !sameRoot(in.Base.Root, in.Target.Root) {
		return fmt.Errorf("%w: base root %s, target root %s", ErrRootMismatch, in.Base.Root, in.Target.Root)
	}
	if !sameRoot(in.ResultRoot, in.Result.Root) {
		return fmt.Errorf("%w: ResultRoot %s, result manifest root %s", ErrRootMismatch, in.ResultRoot, in.Result.Root)
	}
	return nil
}

// verifyManifest re-checks one manifest and reports which kind of damage was
// found, so a caller can distinguish a stale format from a tampered hash.
func verifyManifest(name string, m *runworkspace.Manifest) error {
	if m.FormatVersion != runworkspace.FormatVersion {
		return fmt.Errorf("%w: %s manifest is format %d, want %d",
			ErrManifestVersion, name, m.FormatVersion, runworkspace.FormatVersion)
	}
	err := m.Verify()
	switch {
	case err == nil:
		return nil
	case errors.Is(err, runworkspace.ErrManifestHashMismatch):
		return fmt.Errorf("%w: %s manifest: %v", ErrManifestHashMismatch, name, err)
	case errors.Is(err, runworkspace.ErrManifestOrder):
		return fmt.Errorf("%w: %s manifest: %v", ErrManifestOrder, name, err)
	default:
		return fmt.Errorf("merge: %s manifest: %w", name, err)
	}
}

// sameRoot reports whether two manifest roots name the same directory.
func sameRoot(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	cleanA, cleanB := filepath.Clean(a), filepath.Clean(b)
	if cleanA == cleanB {
		return true
	}
	if runtime.GOOS == "windows" && strings.EqualFold(cleanA, cleanB) {
		return true
	}
	// The same directory can be spelled through a link, so when both roots
	// exist and both resolve, compare what they resolve to.
	resolvedA, errA := filepath.EvalSymlinks(cleanA)
	resolvedB, errB := filepath.EvalSymlinks(cleanB)
	if errA == nil && errB == nil && resolvedA == resolvedB {
		return true
	}
	return false
}
