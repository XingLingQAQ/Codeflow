package merge

import (
	"runtime"
	"testing"

	"github.com/codeflow/backend/internal/runworkspace"
)

// The decision table is tested through decide, which takes plain values: it
// needs no file system, so every case — symlinks and binary content included —
// runs on every platform, Windows included.

const (
	hashA = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	hashB = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	hashC = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	link1 = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
	link2 = "sha256:5555555555555555555555555555555555555555555555555555555555555555"
)

func fileEntry(hash string) *runworkspace.Entry {
	return &runworkspace.Entry{Path: "p", Type: runworkspace.TypeFile, Mode: 0o644, Size: 3, ContentHash: hash}
}

func linkEntry(hash string) *runworkspace.Entry {
	return &runworkspace.Entry{Path: "p", Type: runworkspace.TypeSymlink, Mode: 0o777, Size: 5, ContentHash: hash}
}

func deletedEntry() *runworkspace.Entry {
	return &runworkspace.Entry{Path: "p", Type: runworkspace.TypeDeleted}
}

func TestDecide(t *testing.T) {
	cases := []struct {
		name  string
		state pathState
		want  pathDecision
	}{
		{
			name:  "nothing anywhere",
			state: pathState{},
			want:  pathDecision{},
		},
		{
			name:  "run creates a file the target does not have",
			state: pathState{result: fileEntry(hashA)},
			want:  pathDecision{kind: KindCreate},
		},
		{
			name:  "run modifies a file the target still has at base content",
			state: pathState{base: fileEntry(hashA), result: fileEntry(hashB), target: fileEntry(hashA)},
			want:  pathDecision{kind: KindModify},
		},
		{
			name:  "run deletes a file the target still has",
			state: pathState{base: fileEntry(hashA), target: fileEntry(hashA)},
			want:  pathDecision{kind: KindDelete},
		},
		{
			name:  "run recreates a path the base recorded as deleted",
			state: pathState{base: deletedEntry(), result: fileEntry(hashA)},
			want:  pathDecision{kind: KindCreate},
		},
		{
			name:  "run deletes a path that base and target record as deleted",
			state: pathState{base: fileEntry(hashA), result: deletedEntry(), target: deletedEntry()},
			want:  pathDecision{alreadyApplied: true},
		},
		{
			name:  "run untouched, user edited the same path: the user edit survives",
			state: pathState{base: fileEntry(hashA), result: fileEntry(hashA), target: fileEntry(hashC)},
			want:  pathDecision{},
		},
		{
			name:  "run untouched, user deleted the path: the deletion survives",
			state: pathState{base: fileEntry(hashA), result: fileEntry(hashA)},
			want:  pathDecision{},
		},
		{
			name:  "run untouched, user created a different file there",
			state: pathState{target: fileEntry(hashC)},
			want:  pathDecision{},
		},
		{
			name:  "target already holds the run result",
			state: pathState{base: fileEntry(hashA), result: fileEntry(hashB), target: fileEntry(hashB)},
			want:  pathDecision{alreadyApplied: true},
		},
		{
			name:  "target holds a third state",
			state: pathState{base: fileEntry(hashA), result: fileEntry(hashB), target: fileEntry(hashC)},
			want:  pathDecision{conflict: ReasonTargetChanged},
		},
		{
			name:  "target holds a third state because the user deleted the file the run changed",
			state: pathState{base: fileEntry(hashA), result: fileEntry(hashB)},
			want:  pathDecision{conflict: ReasonTargetChanged},
		},
		{
			name:  "user created different content where the run created a file",
			state: pathState{result: fileEntry(hashB), target: fileEntry(hashC)},
			want:  pathDecision{conflict: ReasonTargetChanged},
		},
		{
			name:  "run created a secret: the result capture cannot describe it",
			state: pathState{resultExcluded: runworkspace.ReasonSecret, resultExclusionIsNew: true},
			want:  pathDecision{conflict: ReasonResultExcluded + ":" + runworkspace.ReasonSecret},
		},
		{
			name: "run changed a path into an oversize file",
			state: pathState{
				base: fileEntry(hashA), resultExcluded: runworkspace.ReasonOversize, resultExclusionIsNew: true,
			},
			want: pathDecision{conflict: ReasonResultExcluded + ":" + runworkspace.ReasonOversize},
		},
		{
			// Base and result both exclude the path, so the exclusion is not
			// new: the run did not introduce it and it is left alone.
			name: "run changed a path that base already excluded too: no conflict, no operation",
			state: pathState{
				resultExcluded: runworkspace.ReasonSecret,
			},
			want: pathDecision{},
		},
		{
			name: "run changed a path the target capture excludes",
			state: pathState{
				base: fileEntry(hashA), result: fileEntry(hashB), targetExcluded: runworkspace.ReasonOversize,
			},
			want: pathDecision{conflict: ReasonTargetExcluded + ":" + runworkspace.ReasonOversize},
		},
		{
			name: "result exclusion outranks a target problem",
			state: pathState{
				base: fileEntry(hashA), target: fileEntry(hashA),
				resultExcluded: runworkspace.ReasonSecret, resultExclusionIsNew: true,
			},
			want: pathDecision{conflict: ReasonResultExcluded + ":" + runworkspace.ReasonSecret},
		},
		{
			name:  "symlink retargeted by the run",
			state: pathState{base: linkEntry(link1), result: linkEntry(link2), target: linkEntry(link1)},
			want:  pathDecision{kind: KindModify},
		},
		{
			name:  "file replaced by a symlink",
			state: pathState{base: fileEntry(hashA), result: linkEntry(link2), target: fileEntry(hashA)},
			want:  pathDecision{kind: KindModify},
		},
		{
			name:  "symlink replaced by a file",
			state: pathState{base: linkEntry(link1), result: fileEntry(hashB), target: linkEntry(link1)},
			want:  pathDecision{kind: KindModify},
		},
		{
			name:  "same hash but a different type is still a change",
			state: pathState{base: fileEntry(hashB), result: linkEntry(hashB), target: fileEntry(hashB)},
			want:  pathDecision{kind: KindModify},
		},
		{
			name:  "run created a link where the target has nothing",
			state: pathState{result: linkEntry(link1)},
			want:  pathDecision{kind: KindCreate},
		},
		{
			name:  "run's new link collides with the user's own link",
			state: pathState{result: linkEntry(link1), target: linkEntry(link2)},
			want:  pathDecision{conflict: ReasonTargetChanged},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decide(tc.state)
			if got != tc.want {
				t.Fatalf("decide() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSameStateTreatsDeletedAndAbsentAlike(t *testing.T) {
	cases := []struct {
		name string
		a, b *runworkspace.Entry
		want bool
	}{
		{"both absent", nil, nil, true},
		{"deleted entry and absent", deletedEntry(), nil, true},
		{"absent and deleted entry", nil, deletedEntry(), true},
		{"both deleted", deletedEntry(), deletedEntry(), true},
		{"deleted entry and file", deletedEntry(), fileEntry(hashA), false},
		{"same content", fileEntry(hashA), fileEntry(hashA), true},
		{"different content", fileEntry(hashA), fileEntry(hashB), false},
		{"same hash, different type", fileEntry(hashA), linkEntry(hashA), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameState(tc.a, tc.b); got != tc.want {
				t.Fatalf("sameState = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSameContentModeRule pins the platform rule for permission bits: they are
// part of the content identity only where the platform reports real bits. On
// Windows Go reports 0666 for every file, so treating a mode difference as a
// change would make every path look modified.
func TestSameContentModeRule(t *testing.T) {
	readable := &runworkspace.Entry{Path: "p", Type: runworkspace.TypeFile, Mode: 0o644, ContentHash: hashA}
	executable := &runworkspace.Entry{Path: "p", Type: runworkspace.TypeFile, Mode: 0o755, ContentHash: hashA}

	got := sameContent(readable, executable)
	if runtime.GOOS == "windows" {
		if !got {
			t.Fatalf("on windows the permission bits carry no information, sameContent = false")
		}
		return
	}
	if got {
		t.Fatalf("on %s a mode change from 0644 to 0755 is a real change, sameContent = true", runtime.GOOS)
	}
}
