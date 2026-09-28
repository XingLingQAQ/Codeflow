package merge

import (
	"testing"
)

// candidateFixture is one fully populated candidate. Every field is set,
// because the hash coverage test below mutates one field at a time and a zero
// value would hide a field that the encoding forgot.
func candidateFixture() *Candidate {
	return &Candidate{
		FormatVersion:      FormatVersion,
		Root:               "C:/work/project",
		BaseManifestHash:   hashA,
		TargetManifestHash: hashB,
		ResultManifestHash: hashC,
		Operations: []Operation{{
			Path:        "a.txt",
			Kind:        KindModify,
			OldType:     "file",
			OldHash:     hashA,
			OldMode:     0o644,
			NewType:     "file",
			NewHash:     hashB,
			NewMode:     0o755,
			Size:        17,
			Binary:      true,
			RenamedFrom: "old/a.txt",
			RenamedTo:   "new/a.txt",
		}},
		AlreadyApplied: []string{"b.txt"},
		Conflicts: []Conflict{{
			Path:   "c.txt",
			Reason: ReasonTargetChanged,
			Base: &EntrySummary{
				Exists: true, Type: "file", ContentHash: hashA, Mode: 0o644, GitStatus: " M",
			},
			Result: &EntrySummary{Exists: false, Type: "deleted"},
			Target: &EntrySummary{Exists: true, Type: "symlink", ContentHash: link1, Mode: 0o777, GitStatus: "??"},
		}},
	}
}

// TestCandidateHashCoversEveryField proves the candidate hash is a function of
// everything the candidate says: a change in any field, including the Old*
// expectations the publisher re-verifies and every conflict field, produces a
// different hash. A hash that missed a field would let an approval bind a
// different change set than the one that gets published.
func TestCandidateHashCoversEveryField(t *testing.T) {
	want := candidateHash(candidateFixture())

	mutations := []struct {
		name   string
		mutate func(c *Candidate)
	}{
		{"format version", func(c *Candidate) { c.FormatVersion = 2 }},
		{"root", func(c *Candidate) { c.Root = "C:/work/other" }},
		{"base manifest hash", func(c *Candidate) { c.BaseManifestHash = hashB }},
		{"target manifest hash", func(c *Candidate) { c.TargetManifestHash = hashC }},
		{"result manifest hash", func(c *Candidate) { c.ResultManifestHash = hashA }},
		{"operations dropped", func(c *Candidate) { c.Operations = nil }},
		{"operation added", func(c *Candidate) {
			c.Operations = append(c.Operations, Operation{Path: "z.txt", Kind: KindCreate})
		}},
		{"operation order", func(c *Candidate) {
			c.Operations = append(c.Operations, Operation{Path: "a.txt", Kind: KindDelete})
		}},
		{"operation path", func(c *Candidate) { c.Operations[0].Path = "a2.txt" }},
		{"operation kind", func(c *Candidate) { c.Operations[0].Kind = KindCreate }},
		{"operation old type", func(c *Candidate) { c.Operations[0].OldType = "symlink" }},
		{"operation old hash", func(c *Candidate) { c.Operations[0].OldHash = hashC }},
		{"operation old mode", func(c *Candidate) { c.Operations[0].OldMode = 0o600 }},
		{"operation new type", func(c *Candidate) { c.Operations[0].NewType = "symlink" }},
		{"operation new hash", func(c *Candidate) { c.Operations[0].NewHash = hashC }},
		{"operation new mode", func(c *Candidate) { c.Operations[0].NewMode = 0o600 }},
		{"operation size", func(c *Candidate) { c.Operations[0].Size = 18 }},
		{"operation binary", func(c *Candidate) { c.Operations[0].Binary = false }},
		{"operation renamed from", func(c *Candidate) { c.Operations[0].RenamedFrom = "other/a.txt" }},
		{"operation renamed to", func(c *Candidate) { c.Operations[0].RenamedTo = "other/a.txt" }},
		{"already applied dropped", func(c *Candidate) { c.AlreadyApplied = nil }},
		{"already applied changed", func(c *Candidate) { c.AlreadyApplied = []string{"b2.txt"} }},
		{"conflicts dropped", func(c *Candidate) { c.Conflicts = nil }},
		{"conflict path", func(c *Candidate) { c.Conflicts[0].Path = "c2.txt" }},
		{"conflict reason", func(c *Candidate) { c.Conflicts[0].Reason = ReasonTargetChanged + ":detail" }},
		{"conflict base dropped", func(c *Candidate) { c.Conflicts[0].Base = nil }},
		{"conflict base exists", func(c *Candidate) { c.Conflicts[0].Base.Exists = false }},
		{"conflict base type", func(c *Candidate) { c.Conflicts[0].Base.Type = "symlink" }},
		{"conflict base content hash", func(c *Candidate) { c.Conflicts[0].Base.ContentHash = hashB }},
		{"conflict base mode", func(c *Candidate) { c.Conflicts[0].Base.Mode = 0o600 }},
		{"conflict base git status", func(c *Candidate) { c.Conflicts[0].Base.GitStatus = "M " }},
		{"conflict result dropped", func(c *Candidate) { c.Conflicts[0].Result = nil }},
		{"conflict result type", func(c *Candidate) { c.Conflicts[0].Result.Type = "file" }},
		{"conflict target dropped", func(c *Candidate) { c.Conflicts[0].Target = nil }},
		{"conflict target hash", func(c *Candidate) { c.Conflicts[0].Target.ContentHash = hashB }},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			c := candidateFixture()
			m.mutate(c)
			if got := candidateHash(c); got == want {
				t.Fatalf("candidate hash did not change when %s changed", m.name)
			}
		})
	}
}

// TestCandidateHashIsStable pins two properties at once: the same candidate
// always hashes the same, and two independently built but equal candidates
// agree. The second is what lets Guard, check, and approval refer to one
// change set by hash.
func TestCandidateHashIsStable(t *testing.T) {
	a, b := candidateFixture(), candidateFixture()
	if candidateHash(a) != candidateHash(b) {
		t.Fatalf("equal candidates hashed differently: %s vs %s", candidateHash(a), candidateHash(b))
	}

	// The encoding carries a domain prefix, so a candidate hash can never be
	// mistaken for a manifest hash of the same content.
	if got := candidateHash(a); got[:7] != "sha256:" || len(got) != 7+64 {
		t.Fatalf("candidate hash is not a sha256 content hash: %q", got)
	}

	// Every operation's Old* side is part of the hash even though a publisher
	// only reads them: a candidate whose expectations differ is a different
	// change set and must not be accepted under the same hash.
	stale := candidateFixture()
	stale.Operations[0].OldHash = hashC
	if candidateHash(stale) == candidateHash(a) {
		t.Fatal("a changed old-content expectation did not change the candidate hash")
	}
}

// TestCandidateHashGolden pins the canonical encoding. A change here is a
// format change: every approval recorded against a candidate hash would stop
// matching, so the value may only move together with FormatVersion.
func TestCandidateHashGolden(t *testing.T) {
	const golden = "sha256:83258fac8d7c12ad99eb4f4c4d39f480cb0b0439226501d5f5979e41df0e7bd4"
	got := candidateHash(candidateFixture())
	if got != golden {
		t.Fatalf("canonical encoding changed:\n got %s\nwant %s", got, golden)
	}
}

func TestCandidatePublishable(t *testing.T) {
	cases := []struct {
		name string
		c    *Candidate
		want bool
	}{
		{"nil", nil, false},
		{"no operations and no conflicts", &Candidate{}, false},
		{"one operation", &Candidate{Operations: []Operation{{Path: "a.txt", Kind: KindCreate}}}, true},
		{
			"conflict blocks even with operations",
			&Candidate{
				Operations: []Operation{{Path: "a.txt", Kind: KindCreate}},
				Conflicts:  []Conflict{{Path: "b.txt", Reason: ReasonTargetChanged}},
			},
			false,
		},
		{"no operations but a conflict", &Candidate{Conflicts: []Conflict{{Path: "b.txt", Reason: ReasonTargetChanged}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.Publishable(); got != tc.want {
				t.Fatalf("Publishable = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestPairRenamesPairsByContent proves the rename pairing is deterministic and
// content-based. Several creates and deletes can share one content hash, and
// pairing must not depend on map iteration order: the nth create in path order
// pairs with the nth delete in path order.
func TestPairRenamesPairsByContent(t *testing.T) {
	for i := 0; i < 100; i++ {
		ops := []Operation{
			{Path: "a-delete", Kind: KindDelete, OldType: "file", OldHash: hashA},
			{Path: "b-create", Kind: KindCreate, NewType: "file", NewHash: hashA},
			{Path: "c-delete", Kind: KindDelete, OldType: "file", OldHash: hashA},
			{Path: "d-create", Kind: KindCreate, NewType: "file", NewHash: hashA},
			{Path: "e-create", Kind: KindCreate, NewType: "file", NewHash: hashB},
		}
		pairRenames(ops)

		if ops[1].RenamedFrom != "a-delete" || ops[0].RenamedTo != "b-create" {
			t.Fatalf("iteration %d: first create/delete pair not matched: %+v", i, ops)
		}
		if ops[3].RenamedFrom != "c-delete" || ops[2].RenamedTo != "d-create" {
			t.Fatalf("iteration %d: second create/delete pair not matched: %+v", i, ops)
		}
		if ops[4].RenamedFrom != "" || ops[4].RenamedTo != "" {
			t.Fatalf("iteration %d: unmatched create was paired: %+v", i, ops[4])
		}
	}
}

func TestPairRenamesLeavesUnrelatedOperationsAlone(t *testing.T) {
	// The modified path's old content is never paired: it is neither a create
	// nor a delete. The deletes and creates that remain have no hash in
	// common, so nothing may be paired.
	ops := []Operation{
		{Path: "a.txt", Kind: KindModify, OldType: "file", OldHash: hashA, NewType: "file", NewHash: hashB},
		{Path: "b.txt", Kind: KindDelete, OldType: "file", OldHash: hashA},
		{Path: "c.txt", Kind: KindCreate, NewType: "file", NewHash: hashC},
		{Path: "d.txt", Kind: KindDelete, OldType: "file", OldHash: link1},
		{Path: "e.txt", Kind: KindCreate, NewType: "file", NewHash: link2},
	}
	pairRenames(ops)
	for _, op := range ops {
		if op.RenamedFrom != "" || op.RenamedTo != "" {
			t.Fatalf("unrelated operation was paired: %+v", op)
		}
	}
}

// TestPairRenamesIsTypeSensitive proves a delete and a create only pair when
// the type matches too. A symlink and a file that happen to hash alike (a link
// whose target string is the file's content) are not a rename of each other.
// The Manifest cannot be built without a real capture, so this is covered with
// constructed operations, which run on every platform.
func TestPairRenamesIsTypeSensitive(t *testing.T) {
	ops := []Operation{
		{Path: "a-link", Kind: KindDelete, OldType: "symlink", OldHash: hashA},
		{Path: "b-file", Kind: KindCreate, NewType: "file", NewHash: hashA},
	}
	pairRenames(ops)
	if ops[0].RenamedTo != "" || ops[1].RenamedFrom != "" {
		t.Fatalf("a symlink delete paired with a file create: %+v", ops)
	}

	matching := []Operation{
		{Path: "a-link", Kind: KindDelete, OldType: "symlink", OldHash: link1},
		{Path: "b-link", Kind: KindCreate, NewType: "symlink", NewHash: link1},
	}
	pairRenames(matching)
	if matching[0].RenamedTo != "b-link" || matching[1].RenamedFrom != "a-link" {
		t.Fatalf("a retargeted symlink move was not paired: %+v", matching)
	}
}
