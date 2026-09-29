package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

// This file is the canonical form the whole approval domain hangs from.
//
// An approval authorizes one concrete action. "Concrete" has to be a hash over
// everything that, if it changed, would mean the human approved something else:
// the project, the subject, the agent revision, the tool parameters, the
// command, the working directory, the target paths, the base manifest and the
// policy version in force (§27.2.3, §15 T2.02, §28 T2.02.a). The hash is
// computed here, from the approval's own input, and never accepted from a
// caller.

// FingerprintPrefix is the algorithm tag on every fingerprint this package
// writes: the same "sha256:" + 64 lowercase hex form artifact content hashes and
// execbackend.ToolRequestFingerprint use.
const FingerprintPrefix = "sha256:"

// FingerprintVersion is the first field of the hashed document. It is stored
// *inside* the hash rather than beside it, so a future change to the field set
// produces a different fingerprint for the same action instead of silently
// reusing the old one — an approval decided under version 1 can never be
// re-read as if it had been hashed with a later layout.
const FingerprintVersion = 1

// TargetPaths' schemes. A path is either relative to the run's working
// directory (the worktree the agent edits) or an artifact reference, which is
// not relative to anything. Explicit schemes stop `docs/a.md` and the artifact
// named `docs/a.md` from hashing identically.
const (
	PathSchemeWorktree = "worktree"
	PathSchemeArtifact = "artifact"
	// PathSchemeArtifactPrefix is the spelling an artifact reference uses in
	// FingerprintInput.TargetPaths.
	PathSchemeArtifactPrefix = PathSchemeArtifact + ":"
	// PathSchemeWorktreePrefix is the explicit spelling of a worktree path, the
	// form CanonicalTargetPath returns and accepts back unchanged.
	PathSchemeWorktreePrefix = PathSchemeWorktree + ":"
)

// FingerprintInput is everything an approval binds, beyond its own identity and
// timestamps. Mutable facts about the approval itself — its id, when it was
// requested, when it expires, who decided it — are deliberately absent: they
// describe the row, not the action, and two approvals for the same action must
// hash identically.
type FingerprintInput struct {
	// ProjectID is the project the action happens in. Required for every
	// subject type: an authorization that does not say where it applies is not
	// an authorization.
	ProjectID string
	// SubjectType and SubjectID are the subject. Required for every type.
	SubjectType SubjectType
	SubjectID   string
	// AgentRevisionID is the agent revision that will act. Required for tool
	// subjects: an approval is for one agent revision's call, not for "the
	// agent" in general.
	AgentRevisionID string
	// ArgumentsHash is execbackend.ToolRequestFingerprint(tool, arguments) —
	// the canonical hash of the tool request. Required for tool subjects, and
	// validated to be a "sha256:" + 64 lowercase hex hash so a caller cannot
	// bind a placeholder.
	ArgumentsHash string
	// Command is the argv the approval covers, in order: argv order is
	// meaningful, so it is preserved exactly and never sorted. An approval to
	// run `["ls", "-la"]` is not an approval to run `["ls", "-al"]`.
	Command []string
	// Cwd is the working directory the command runs in. Empty means "the run's
	// default working directory".
	Cwd string
	// TargetPaths are the files the action may touch. They are canonicalized
	// (see CanonicalTargetPath) and sorted, so the same set of paths in a
	// different order hashes identically — order is not meaningful for a set.
	TargetPaths []string
	// BaseManifestHash is the captured baseline the action was approved against
	// (§27.2.3 "参数、目标内容或策略改变则原决定成为不可消费历史"). Required
	// for tool and merge subjects.
	BaseManifestHash string
	// PolicyVersion is the policy version in force when the approval was
	// requested. Required for every subject type: an approval is only
	// meaningful relative to the rules that were evaluated.
	PolicyVersion string
}

// fingerprintDocument is the exact structure the fingerprint hashes, and the
// field order is the canonical order. It is a struct, not a concatenation of
// strings: JSON gives every value an unambiguous boundary, so no value can
// imitate a separator or another field's slot. `["ls -la"]` and `["ls","-la"]`
// are different documents; `{"command":["a,b"]}` cannot masquerade as
// `{"command":["a","b"]}`.
type fingerprintDocument struct {
	Version          int      `json:"fingerprint_version"`
	ProjectID        string   `json:"project_id"`
	SubjectType      string   `json:"subject_type"`
	SubjectID        string   `json:"subject_id"`
	AgentRevisionID  string   `json:"agent_revision_id"`
	ArgumentsHash    string   `json:"arguments_hash"`
	Command          []string `json:"command"`
	Cwd              string   `json:"cwd"`
	TargetPaths      []string `json:"target_paths"`
	BaseManifestHash string   `json:"base_manifest_hash"`
	PolicyVersion    string   `json:"policy_version"`
}

// Fingerprint returns the canonical fingerprint of in: FingerprintPrefix
// followed by the hex sha256 of the document described above.
//
// It rejects an input that would produce an approval nobody can consume
// unambiguously — a missing project, subject or policy version; a tool input
// without an agent revision, an arguments hash or a base manifest; an arguments
// hash that is not a hash; a target path that is absolute, escapes upwards,
// repeats or is empty. Every rejection is ErrInvalidApproval and names the
// field, so a caller learns which input it forgot rather than that "the hash
// failed".
func Fingerprint(in FingerprintInput) (string, error) {
	_, fingerprint, err := canonicalFingerprint(in)
	return fingerprint, err
}

// CanonicalFingerprintJSON returns the exact JSON document Fingerprint hashed.
// CreateApprovalTx stores it beside the fingerprint, which is what lets T2.02.b
// re-hash the input a decision was made against instead of re-deriving one from
// a live request.
func CanonicalFingerprintJSON(in FingerprintInput) (string, error) {
	canonical, _, err := canonicalFingerprint(in)
	return canonical, err
}

// canonicalFingerprint validates in, normalizes it and returns both the
// canonical document and its fingerprint. The two are produced together
// because the hash is only ever the hash *of* that document; returning either
// alone would invite a caller to pair a hash with a document that does not
// produce it.
func canonicalFingerprint(in FingerprintInput) (string, string, error) {
	projectID := trimSpace(in.ProjectID)
	if projectID == "" {
		return "", "", fmt.Errorf("%w: FingerprintInput.ProjectID is required", ErrInvalidApproval)
	}
	if !in.SubjectType.Valid() {
		if trimSpace(string(in.SubjectType)) == "" {
			return "", "", fmt.Errorf("%w: FingerprintInput.SubjectType is required", ErrInvalidApproval)
		}
		return "", "", fmt.Errorf("%w: FingerprintInput.SubjectType %q is not valid",
			ErrInvalidApproval, in.SubjectType)
	}
	subjectID := trimSpace(in.SubjectID)
	if subjectID == "" {
		return "", "", fmt.Errorf("%w: FingerprintInput.SubjectID is required", ErrInvalidApproval)
	}
	policyVersion := trimSpace(in.PolicyVersion)
	if policyVersion == "" {
		return "", "", fmt.Errorf("%w: FingerprintInput.PolicyVersion is required", ErrInvalidApproval)
	}

	agentRevisionID := trimSpace(in.AgentRevisionID)
	argumentsHash := trimSpace(in.ArgumentsHash)
	baseManifestHash := trimSpace(in.BaseManifestHash)
	switch in.SubjectType {
	case SubjectTool:
		if agentRevisionID == "" {
			return "", "", fmt.Errorf("%w: FingerprintInput.AgentRevisionID is required for a tool subject",
				ErrInvalidApproval)
		}
		if argumentsHash == "" {
			return "", "", fmt.Errorf("%w: FingerprintInput.ArgumentsHash is required for a tool subject",
				ErrInvalidApproval)
		}
		if !ValidHash(argumentsHash) {
			return "", "", fmt.Errorf("%w: FingerprintInput.ArgumentsHash %q is not %s<64 lowercase hex>",
				ErrInvalidApproval, in.ArgumentsHash, FingerprintPrefix)
		}
		if baseManifestHash == "" {
			return "", "", fmt.Errorf("%w: FingerprintInput.BaseManifestHash is required for a tool subject",
				ErrInvalidApproval)
		}
	case SubjectMerge:
		if baseManifestHash == "" {
			return "", "", fmt.Errorf("%w: FingerprintInput.BaseManifestHash is required for a merge subject",
				ErrInvalidApproval)
		}
	}

	targetPaths, err := canonicalTargetPaths(in.TargetPaths)
	if err != nil {
		return "", "", err
	}
	cwd, err := canonicalCwd(in.Cwd)
	if err != nil {
		return "", "", err
	}

	doc := fingerprintDocument{
		Version:     FingerprintVersion,
		ProjectID:   projectID,
		SubjectType: string(in.SubjectType),
		SubjectID:   subjectID,
		// Trimmed but otherwise exact: a command's arguments and their order
		// are the action, so nothing here is sorted, joined or re-quoted.
		AgentRevisionID:  agentRevisionID,
		ArgumentsHash:    argumentsHash,
		Command:          canonicalCommand(in.Command),
		Cwd:              cwd,
		TargetPaths:      targetPaths,
		BaseManifestHash: baseManifestHash,
		PolicyVersion:    policyVersion,
	}

	canonical, err := json.Marshal(doc)
	if err != nil {
		// Unreachable: every field is a string, an int or a []string.
		return "", "", fmt.Errorf("%w: FingerprintInput cannot be encoded: %v", ErrInvalidApproval, err)
	}
	sum := sha256.Sum256(canonical)
	return string(canonical), FingerprintPrefix + hex.EncodeToString(sum[:]), nil
}

// canonicalCommand copies the argv. A nil command and an empty one mean the
// same thing ("no command"), and both must encode as [] rather than one of them
// becoming null: two spellings of one action would otherwise hash differently.
// The elements themselves are kept byte for byte.
func canonicalCommand(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// ValidHash reports whether hash is exactly FingerprintPrefix followed by 64
// lowercase hex characters.
//
// It is a precondition, not a repair: a hash that does not match is rejected
// rather than normalized, because trimming, upper-casing or re-prefixing would
// mean this package accepted an identity no other component recognizes.
func ValidHash(hash string) bool {
	rest, ok := strings.CutPrefix(hash, FingerprintPrefix)
	if !ok || len(rest) != 64 {
		return false
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// CanonicalTargetPath normalizes one target path, or reports why it cannot be
// used. It is exported because a caller assembling TargetPaths from user input
// should be able to reject a bad path before it hands the whole input over.
//
// Normalization: the path is trimmed, backslashes become forward slashes (a
// Windows caller and a Unix caller must hash the same file the same way), a
// leading "./" and duplicate slashes are resolved, and an optional scheme is
// validated. The result is one of:
//
//	worktree:<relative path>   the default; a bare path gets this scheme
//	artifact:<artifact id>     an artifact reference, not relative to a cwd
//
// The result is a fixed point: CanonicalTargetPath(CanonicalTargetPath(p)) is
// CanonicalTargetPath(p), so an input read back from a stored fingerprint
// document hashes the same again. "worktree:src/a.go" and "src/a.go" are one
// target. The body after "worktree:" is a plain relative path — no second scheme
// is read from it — so "worktree:artifact:x" is the file ./artifact:x.
//
// Rejected: an empty path, a NUL byte, an absolute path (including a Windows
// drive letter or a UNC prefix) in the worktree scheme, any ".." segment, a
// path that normalizes to nothing, a scheme written without a body, and an
// unknown scheme — a leading letter, then letters, digits, '+', '-' or '.', then
// ':' ("https:", "notes:"). A file whose name reads like a scheme is written
// "./notes:2024.md" or "worktree:notes:2024.md"; refusing the bare form keeps a
// reference in some other scheme from being mistaken for a file.
func CanonicalTargetPath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", fmt.Errorf("%w: a target path is empty", ErrInvalidApproval)
	}
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("%w: target path %q contains a NUL byte", ErrInvalidApproval, raw)
	}
	p = strings.ReplaceAll(p, `\`, "/")

	if rest, ok := strings.CutPrefix(p, PathSchemeArtifactPrefix); ok {
		id := strings.TrimSpace(rest)
		if id == "" {
			return "", fmt.Errorf("%w: target path %q names the artifact scheme with no artifact",
				ErrInvalidApproval, raw)
		}
		if strings.ContainsRune(id, 0) {
			return "", fmt.Errorf("%w: target path %q contains a NUL byte", ErrInvalidApproval, raw)
		}
		return PathSchemeArtifactPrefix + id, nil
	}

	explicit := false
	if rest, ok := strings.CutPrefix(p, PathSchemeWorktreePrefix); ok {
		p = strings.TrimSpace(rest)
		if p == "" {
			return "", fmt.Errorf("%w: target path %q names the worktree scheme with no path",
				ErrInvalidApproval, raw)
		}
		explicit = true
	}

	if isAbsoluteSlashPath(p) {
		return "", fmt.Errorf("%w: target path %q is absolute; target paths are relative to the run's "+
			"working directory (use %q for an artifact)", ErrInvalidApproval, raw, PathSchemeArtifactPrefix)
	}
	if scheme, ok := leadingScheme(p); ok && !explicit {
		return "", fmt.Errorf("%w: target path %q has the unknown scheme %q (known: %q, %q; write %q for a "+
			"file of that name)", ErrInvalidApproval, raw, scheme, PathSchemeWorktreePrefix,
			PathSchemeArtifactPrefix, "./"+p)
	}
	// ".." is checked on the raw segments, before Clean: path.Clean would
	// resolve `a/../b` to `b` and hide the escape this check exists to stop.
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." {
			return "", fmt.Errorf("%w: target path %q contains a \"..\" segment", ErrInvalidApproval, raw)
		}
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == "" || cleaned == "/" {
		return "", fmt.Errorf("%w: target path %q normalizes to nothing", ErrInvalidApproval, raw)
	}
	if isAbsoluteSlashPath(cleaned) {
		return "", fmt.Errorf("%w: target path %q is absolute", ErrInvalidApproval, raw)
	}
	return PathSchemeWorktreePrefix + cleaned, nil
}

// leadingScheme reports whether p starts with a URI-style scheme — a letter,
// then letters, digits, '+', '-' or '.', then ':' — and returns it. A single
// letter (a Windows drive) never reaches this check: isAbsoluteSlashPath
// refuses it first.
func leadingScheme(p string) (string, bool) {
	end := strings.IndexByte(p, ':')
	if end <= 0 {
		return "", false
	}
	for i := 0; i < end; i++ {
		c := p[i]
		letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if i == 0 && !letter {
			return "", false
		}
		if !letter && (c < '0' || c > '9') && c != '+' && c != '-' && c != '.' {
			return "", false
		}
	}
	return p[:end], true
}

// isAbsoluteSlashPath reports whether an already slash-normalized path is
// absolute: a leading separator, a Windows drive letter, or a UNC prefix.
func isAbsoluteSlashPath(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	if len(p) >= 2 && p[1] == ':' {
		c := p[0]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}

// canonicalTargetPaths normalizes a set of target paths: each is normalized,
// duplicates are refused rather than collapsed (a caller that listed one path
// twice is describing an action the approval cannot represent honestly) and the
// result is sorted, because a set has no order.
func canonicalTargetPaths(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for i, raw := range in {
		p, err := CanonicalTargetPath(raw)
		if err != nil {
			return nil, fmt.Errorf("FingerprintInput.TargetPaths[%d]: %w", i, err)
		}
		if _, duplicate := seen[p]; duplicate {
			return nil, fmt.Errorf("%w: FingerprintInput.TargetPaths[%d] repeats %q",
				ErrInvalidApproval, i, p)
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// canonicalCwd normalizes the working directory. It does not require a relative
// path: a working directory is legitimately absolute (a workspace root), and it
// is compared, not joined. What it does guarantee is that two spellings of one
// directory hash the same — `.` or an empty string mean "the default working
// directory", and backslashes are normalized as in CanonicalTargetPath.
func canonicalCwd(raw string) (string, error) {
	cwd := strings.TrimSpace(raw)
	if cwd == "" {
		return "", nil
	}
	if strings.ContainsRune(cwd, 0) {
		return "", fmt.Errorf("%w: FingerprintInput.Cwd contains a NUL byte", ErrInvalidApproval)
	}
	cwd = strings.ReplaceAll(cwd, `\`, "/")
	cwd = path.Clean(cwd)
	if cwd == "." || cwd == "/" {
		return "", nil
	}
	return cwd, nil
}
