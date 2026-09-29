package approval_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/approval"
	"github.com/codeflow/backend/internal/execbackend"
)

// sha256Hex is the hex digest of s, computed here rather than by the package,
// so the assertion that the fingerprint is the hash of the canonical document
// is independent of the code that produces it.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// fingerprinted is the input every fingerprint case starts from: a complete,
// valid tool approval.
func fingerprinted(t *testing.T) approval.FingerprintInput {
	t.Helper()
	hash, err := execbackend.ToolRequestFingerprint("run_shell", json.RawMessage(`{"command":"ls -la"}`))
	if err != nil {
		t.Fatalf("ToolRequestFingerprint: %v", err)
	}
	return approval.FingerprintInput{
		ProjectID:        "p-1",
		SubjectType:      approval.SubjectTool,
		SubjectID:        "tc_1",
		AgentRevisionID:  "ar-1",
		ArgumentsHash:    hash,
		Command:          []string{"ls", "-la"},
		Cwd:              "/workspace/p-1",
		TargetPaths:      []string{"src/a.go"},
		BaseManifestHash: "sha256:manifest",
		PolicyVersion:    "b4-2026-08-19",
	}
}

func mustFingerprint(t *testing.T, in approval.FingerprintInput) string {
	t.Helper()
	got, err := approval.Fingerprint(in)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if !approval.ValidHash(got) {
		t.Fatalf("Fingerprint = %q, want sha256:<64 lowercase hex>", got)
	}
	return got
}

// TestFingerprintIsStable: the same input hashes the same, however many times
// it is computed. A fingerprint that drifted would make every stored approval
// unconsumable.
func TestFingerprintIsStable(t *testing.T) {
	in := fingerprinted(t)
	first := mustFingerprint(t, in)
	for i := 0; i < 5; i++ {
		if again := mustFingerprint(t, in); again != first {
			t.Fatalf("Fingerprint run %d = %s, want %s", i, again, first)
		}
	}
	// A copy of the slices, not an alias, must hash identically: the canonical
	// form is of the values, not of the caller's memory.
	other := in
	other.Command = append([]string{}, in.Command...)
	other.TargetPaths = append([]string{}, in.TargetPaths...)
	if got := mustFingerprint(t, other); got != first {
		t.Errorf("a copy of the input hashed %s, want %s", got, first)
	}
}

// TestFingerprintChangesWithEveryField is the coverage claim of §27.2.3: every
// field the fingerprint binds must matter. Each case changes exactly one field
// and requires a different hash.
func TestFingerprintChangesWithEveryField(t *testing.T) {
	base := mustFingerprint(t, fingerprinted(t))

	// The substitution hashes are real ToolRequestFingerprint outputs, so the
	// "arguments changed" case is the same comparison production makes.
	otherArguments, err := execbackend.ToolRequestFingerprint("run_shell", json.RawMessage(`{"command":"rm -rf /"}`))
	if err != nil {
		t.Fatalf("ToolRequestFingerprint: %v", err)
	}

	cases := []struct {
		name string
		mut  func(*approval.FingerprintInput)
	}{
		{"project", func(in *approval.FingerprintInput) { in.ProjectID = "p-2" }},
		{"subject type", func(in *approval.FingerprintInput) {
			in.SubjectType = approval.SubjectMerge
			in.AgentRevisionID = ""
			in.ArgumentsHash = ""
		}},
		{"subject id", func(in *approval.FingerprintInput) { in.SubjectID = "tc_2" }},
		{"agent revision", func(in *approval.FingerprintInput) { in.AgentRevisionID = "ar-2" }},
		{"arguments hash", func(in *approval.FingerprintInput) { in.ArgumentsHash = otherArguments }},
		{"command", func(in *approval.FingerprintInput) { in.Command = []string{"ls", "-al"} }},
		{"command argument count", func(in *approval.FingerprintInput) { in.Command = []string{"ls -la"} }},
		{"cwd", func(in *approval.FingerprintInput) { in.Cwd = "/workspace/p-2" }},
		{"target paths", func(in *approval.FingerprintInput) { in.TargetPaths = []string{"src/b.go"} }},
		{"base manifest", func(in *approval.FingerprintInput) { in.BaseManifestHash = "sha256:other" }},
		{"policy version", func(in *approval.FingerprintInput) { in.PolicyVersion = "b5-2026-09-01" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := fingerprinted(t)
			tc.mut(&in)
			if got := mustFingerprint(t, in); got == base {
				t.Errorf("changing %s did not change the fingerprint (%s)", tc.name, got)
			}
		})
	}
}

// TestFingerprintTargetPathOrderAndForm: the target-path set is unordered but
// its contents are not, and the two spellings of one path are one path.
func TestFingerprintTargetPathOrderAndForm(t *testing.T) {
	base := fingerprinted(t)
	base.TargetPaths = []string{"src/a.go", "docs/readme.md"}
	want := mustFingerprint(t, base)

	t.Run("order does not matter", func(t *testing.T) {
		in := fingerprinted(t)
		in.TargetPaths = []string{"docs/readme.md", "src/a.go"}
		if got := mustFingerprint(t, in); got != want {
			t.Errorf("reordered target paths hashed %s, want %s", got, want)
		}
	})

	t.Run("spelling does not matter", func(t *testing.T) {
		in := fingerprinted(t)
		in.TargetPaths = []string{"./src/a.go", `docs\readme.md`}
		if got := mustFingerprint(t, in); got != want {
			t.Errorf("an equivalent spelling hashed %s, want %s", got, want)
		}
	})

	t.Run("contents do", func(t *testing.T) {
		in := fingerprinted(t)
		in.TargetPaths = []string{"src/a.go", "docs/other.md"}
		if got := mustFingerprint(t, in); got == want {
			t.Errorf("a different path set hashed %s, the same as the original", got)
		}
	})

	t.Run("an empty set is not a different set", func(t *testing.T) {
		left := fingerprinted(t)
		left.TargetPaths = nil
		right := fingerprinted(t)
		right.TargetPaths = []string{}
		if a, b := mustFingerprint(t, left), mustFingerprint(t, right); a != b {
			t.Errorf("nil and empty target paths hashed %s and %s", a, b)
		}
	})
}

// TestFingerprintRejectsBadTargetPaths: a path that cannot be compared
// unambiguously is refused rather than normalized into something else.
func TestFingerprintRejectsBadTargetPaths(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
		field string
	}{
		{"empty", []string{""}, "TargetPaths[0]"},
		{"blank", []string{"   "}, "TargetPaths[0]"},
		{"duplicate", []string{"src/a.go", "./src/a.go"}, "TargetPaths[1]"},
		{"absolute", []string{"/etc/passwd"}, "TargetPaths[0]"},
		{"windows absolute", []string{`C:\Windows\system32`}, "TargetPaths[0]"},
		{"escapes upward", []string{"../../etc/passwd"}, "TargetPaths[0]"},
		{"escapes upward inside", []string{"src/../../etc/passwd"}, "TargetPaths[0]"},
		{"normalizes away", []string{"./."}, "TargetPaths[0]"},
		{"nul byte", []string{"src/a\x00.go"}, "TargetPaths[0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := fingerprinted(t)
			in.TargetPaths = tc.paths
			_, err := approval.Fingerprint(in)
			if !errors.Is(err, approval.ErrInvalidApproval) {
				t.Fatalf("error = %v, want ErrInvalidApproval", err)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("error %q does not name %s", err, tc.field)
			}
		})
	}

	t.Run("artifact paths are named, not relative", func(t *testing.T) {
		in := fingerprinted(t)
		in.TargetPaths = []string{"artifact:art-1"}
		if _, err := approval.Fingerprint(in); err != nil {
			t.Fatalf("an artifact target path must be accepted: %v", err)
		}
		in.TargetPaths = []string{"artifact:"}
		if _, err := approval.Fingerprint(in); !errors.Is(err, approval.ErrInvalidApproval) {
			t.Fatalf("an empty artifact reference = %v, want ErrInvalidApproval", err)
		}
		// An artifact reference and a worktree file of the same name are
		// different targets.
		left := fingerprinted(t)
		left.TargetPaths = []string{"artifact:docs/a.md"}
		right := fingerprinted(t)
		right.TargetPaths = []string{"docs/a.md"}
		if a, b := mustFingerprint(t, left), mustFingerprint(t, right); a == b {
			t.Error("an artifact reference and a worktree path of the same name hashed identically")
		}
	})

	t.Run("CanonicalTargetPath is usable on its own", func(t *testing.T) {
		got, err := approval.CanonicalTargetPath(`.\src\a.go`)
		if err != nil {
			t.Fatalf("CanonicalTargetPath: %v", err)
		}
		if got != "worktree:src/a.go" {
			t.Errorf("CanonicalTargetPath = %q, want worktree:src/a.go", got)
		}
	})
}

// TestCanonicalTargetPathSchemes pins the scheme rules: the canonical form is a
// fixed point (a stored path fed back in is unchanged, so re-deriving a
// fingerprint from a stored input reproduces it), the explicit worktree scheme
// means the same file as the bare path, and a scheme this package does not know
// is refused instead of being read as a file name.
func TestCanonicalTargetPathSchemes(t *testing.T) {
	for _, raw := range []string{
		"src/a.go", `.\src\a.go`, "a//b/./c", "artifact:art-1", "artifact: art-1 ",
		"worktree:src/a.go", "./notes:2024.md", "worktree:notes:2024.md", "src/a:b.go",
		"./artifact:art-1", // a worktree file named artifact:art-1, not the artifact
	} {
		once, err := approval.CanonicalTargetPath(raw)
		if err != nil {
			t.Fatalf("CanonicalTargetPath(%q): %v", raw, err)
		}
		twice, err := approval.CanonicalTargetPath(once)
		if err != nil {
			t.Fatalf("CanonicalTargetPath(%q) (the canonical form of %q): %v", once, raw, err)
		}
		if twice != once {
			t.Errorf("CanonicalTargetPath is not a fixed point: %q -> %q -> %q", raw, once, twice)
		}
	}

	for raw, want := range map[string]string{
		"worktree:src/a.go":      "worktree:src/a.go",
		"./notes:2024.md":        "worktree:notes:2024.md",
		"worktree:notes:2024.md": "worktree:notes:2024.md",
		"src/a:b.go":             "worktree:src/a:b.go",
		// After the worktree scheme the body is a plain relative path, so this
		// is the file ./artifact:art-1 and not the artifact art-1.
		"./artifact:art-1":        "worktree:artifact:art-1",
		"worktree:artifact:art-1": "worktree:artifact:art-1",
	} {
		if got, err := approval.CanonicalTargetPath(raw); err != nil || got != want {
			t.Errorf("CanonicalTargetPath(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}

	for _, raw := range []string{
		"foo:bar",           // unknown scheme
		"notes:2024.md",     // reads as a scheme; the file is ./notes:2024.md
		"Worktree:src/a.go", // schemes are exact
		"https://x.test/a",  // not a target path at all
		"worktree:",         // scheme without a body
		"worktree:../x",     // the body obeys the relative-path rules
		"worktree:/etc/hosts",
		"worktree:C:/x",
	} {
		if got, err := approval.CanonicalTargetPath(raw); !errors.Is(err, approval.ErrInvalidApproval) {
			t.Errorf("CanonicalTargetPath(%q) = %q, %v; want ErrInvalidApproval", raw, got, err)
		}
	}

	// In a fingerprint, the explicit and the bare spelling are one target: the
	// same fingerprint, and listing both is a repeat.
	bare, explicit := fingerprinted(t), fingerprinted(t)
	bare.TargetPaths = []string{"src/a.go"}
	explicit.TargetPaths = []string{"worktree:src/a.go"}
	if a, b := mustFingerprint(t, bare), mustFingerprint(t, explicit); a != b {
		t.Errorf("src/a.go and worktree:src/a.go hashed differently: %s vs %s", a, b)
	}
	both := fingerprinted(t)
	both.TargetPaths = []string{"src/a.go", "worktree:src/a.go"}
	if _, err := approval.Fingerprint(both); !errors.Is(err, approval.ErrInvalidApproval) {
		t.Errorf("listing src/a.go and worktree:src/a.go = %v, want a repeat refused", err)
	}
}

// TestFingerprintHasNoConcatenationCollisions is the reason the document is
// structured JSON rather than joined strings: values that would run together in
// a concatenation must not collide.
func TestFingerprintHasNoConcatenationCollisions(t *testing.T) {
	cases := []struct {
		name  string
		left  func(*approval.FingerprintInput)
		right func(*approval.FingerprintInput)
	}{
		{
			// "a,b" as one path and "a" + "b" as two: a delimiter-joined form
			// cannot tell them apart.
			name:  "a comma inside a path",
			left:  func(in *approval.FingerprintInput) { in.TargetPaths = []string{"a,b"} },
			right: func(in *approval.FingerprintInput) { in.TargetPaths = []string{"a", "b"} },
		},
		{
			// One argv element with a space against two elements: a
			// space-joined form cannot tell them apart.
			name:  "a space inside a command argument",
			left:  func(in *approval.FingerprintInput) { in.Command = []string{"ls -la"} },
			right: func(in *approval.FingerprintInput) { in.Command = []string{"ls", "-la"} },
		},
		{
			name:  "a colon inside a value",
			left:  func(in *approval.FingerprintInput) { in.SubjectID = "tc:1" },
			right: func(in *approval.FingerprintInput) { in.SubjectID = "tc" },
		},
		{
			name:  "a quote inside a value",
			left:  func(in *approval.FingerprintInput) { in.Cwd = `/workspace/"p-1"` },
			right: func(in *approval.FingerprintInput) { in.Cwd = "/workspace/p-1" },
		},
		{
			name:  "an empty command element",
			left:  func(in *approval.FingerprintInput) { in.Command = []string{"ls", "", "-la"} },
			right: func(in *approval.FingerprintInput) { in.Command = []string{"ls", "-la"} },
		},
		{
			// The classic boundary shift: without framing, the end of one value
			// and the start of the next read as the same text. Here the project
			// and the subject trade one character and every other field is
			// identical, so only a structured document can tell the two apart.
			name: "a value shifting across a field boundary",
			left: func(in *approval.FingerprintInput) {
				in.ProjectID = "p-1"
				in.SubjectID = "tool-tc"
			},
			right: func(in *approval.FingerprintInput) {
				in.ProjectID = "p-1tool"
				in.SubjectID = "-tc"
			},
		},
		{
			// The same shift between a field and the command: the command's
			// first element continues the agent revision.
			name: "a command continuing the agent revision",
			left: func(in *approval.FingerprintInput) {
				in.AgentRevisionID = "ar-1"
				in.Command = []string{"ls"}
			},
			right: func(in *approval.FingerprintInput) {
				in.AgentRevisionID = "ar-1ls"
				in.Command = nil
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			left, right := fingerprinted(t), fingerprinted(t)
			tc.left(&left)
			tc.right(&right)
			a, b := mustFingerprint(t, left), mustFingerprint(t, right)
			if a == b {
				t.Errorf("two different inputs both hashed %s", a)
			}
		})
	}

	t.Run("the document is JSON, not a concatenation", func(t *testing.T) {
		canonical, err := approval.CanonicalFingerprintJSON(fingerprinted(t))
		if err != nil {
			t.Fatalf("CanonicalFingerprintJSON: %v", err)
		}
		if !json.Valid([]byte(canonical)) {
			t.Fatalf("canonical document is not JSON: %s", canonical)
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(canonical), &doc); err != nil {
			t.Fatalf("canonical document does not decode: %v", err)
		}
		// The fixed field set and the version tag: the version is inside the
		// hash, so a future field-set change cannot reuse a decision made under
		// this one.
		for _, field := range []string{
			"fingerprint_version", "project_id", "subject_type", "subject_id",
			"agent_revision_id", "arguments_hash", "command", "cwd", "target_paths",
			"base_manifest_hash", "policy_version",
		} {
			if _, ok := doc[field]; !ok {
				t.Errorf("canonical document has no %q field", field)
			}
		}
		if len(doc) != 11 {
			t.Errorf("canonical document has %d fields, want 11: %v", len(doc), doc)
		}
		if version := doc["fingerprint_version"]; version != float64(approval.FingerprintVersion) {
			t.Errorf("fingerprint_version = %v, want %d", version, approval.FingerprintVersion)
		}
	})

	t.Run("CanonicalFingerprintJSON hashes to Fingerprint", func(t *testing.T) {
		in := fingerprinted(t)
		canonical, err := approval.CanonicalFingerprintJSON(in)
		if err != nil {
			t.Fatalf("CanonicalFingerprintJSON: %v", err)
		}
		hash, err := approval.Fingerprint(in)
		if err != nil {
			t.Fatalf("Fingerprint: %v", err)
		}
		sum := sha256Hex(canonical)
		if hash != "sha256:"+sum {
			t.Errorf("Fingerprint = %s, but sha256 of the canonical document = %s", hash, "sha256:"+sum)
		}
	})
}

// TestFingerprintEmptyAndMissingCommand: an absent command and an empty one
// describe the same action, and both are legal for a gate.
func TestFingerprintEmptyAndMissingCommand(t *testing.T) {
	left := fingerprinted(t)
	left.Command = nil
	right := fingerprinted(t)
	right.Command = []string{}
	if a, b := mustFingerprint(t, left), mustFingerprint(t, right); a != b {
		t.Errorf("nil and empty command hashed %s and %s", a, b)
	}

	gate := fingerprinted(t)
	gate.SubjectType = approval.SubjectGate
	gate.AgentRevisionID = ""
	gate.ArgumentsHash = ""
	gate.BaseManifestHash = ""
	gate.Command = nil
	mustFingerprint(t, gate)
}

// TestValidHash pins the hash grammar: the tag, the length and the case.
func TestValidHash(t *testing.T) {
	good := "sha256:" + strings.Repeat("a1b2c3d4", 8)
	if !approval.ValidHash(good) {
		t.Errorf("ValidHash(%q) = false, want true", good)
	}
	for _, bad := range []string{
		"", "sha256:", "sha256:" + strings.Repeat("a", 63), "sha256:" + strings.Repeat("a", 65),
		"sha1:" + strings.Repeat("a", 64), "sha256:" + strings.Repeat("A", 64),
		"sha256:" + strings.Repeat("g", 64), strings.Repeat("a", 64),
		" sha256:" + strings.Repeat("a", 64),
	} {
		if approval.ValidHash(bad) {
			t.Errorf("ValidHash(%q) = true, want false", bad)
		}
	}
}

// TestVocabulariesAreClosed: each enum's Valid method and its canonical slice
// must agree, and a value outside the set must be refused by the store.
func TestVocabulariesAreClosed(t *testing.T) {
	for _, s := range approval.SubjectTypes {
		if !s.Valid() {
			t.Errorf("SubjectType %q is listed but not valid", s)
		}
	}
	if approval.SubjectType("sandbox").Valid() {
		t.Error("SubjectType(sandbox) reported valid")
	}
	for _, r := range approval.Risks {
		if !r.Valid() {
			t.Errorf("Risk %q is listed but not valid", r)
		}
	}
	if approval.Risk("extreme").Valid() {
		t.Error("Risk(extreme) reported valid")
	}
	for _, s := range approval.Statuses {
		if !s.Valid() {
			t.Errorf("Status %q is listed but not valid", s)
		}
		if s == approval.StatusPending {
			if s.IsTerminal() {
				t.Error("pending reported terminal")
			}
			continue
		}
		if !s.IsTerminal() {
			t.Errorf("Status %q reported non-terminal", s)
		}
	}

	f := newFixture(t)
	in := f.newApproval(t)
	in.Risk = approval.Risk("extreme")
	if _, err := f.create(t, in); !errors.Is(err, approval.ErrInvalidApproval) ||
		!strings.Contains(err.Error(), "Risk") {
		t.Errorf("an invalid risk = %v, want ErrInvalidApproval naming Risk", err)
	}
	in = f.newApproval(t)
	in.SubjectType = approval.SubjectType("sandbox")
	in.FingerprintInput.SubjectType = approval.SubjectType("sandbox")
	if _, err := f.create(t, in); !errors.Is(err, approval.ErrInvalidApproval) ||
		!strings.Contains(err.Error(), "SubjectType") {
		t.Errorf("an invalid subject type = %v, want ErrInvalidApproval naming SubjectType", err)
	}
}
