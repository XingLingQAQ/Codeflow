// T0.06.c: journey fixture isolation proof plus the journey registry
// required by plan §15 T0.06 and §28 T0.06.c (plan version v3.0-dispatch-4).
//
// This file delivers two things:
//
//  1. TestJourney_FixtureIsolation proves that the copies handed out by
//     copyJourneyFixture are independent working trees: mutating copy A
//     (modify + delete + add) is detected by A's own manifest verification
//     while copy B of the same fixture stays byte-identical and keeps
//     passing verification; the shared source tree under testdata/journeys
//     is not polluted (a third copy taken afterwards still verifies); and
//     copies of different fixtures do not affect each other.
//
//  2. The journey registry below fixes fixture names and test IDs for the
//     §4/§15 user journeys. Pending journeys exist here only as explicit
//     t.Skipf skeletons carrying the blocking reason and the owning task
//     card. No silently passing placeholder tests are allowed (§15 T0.06:
//     "先写 pending 测试表，不伪造通过"; §28 T0.06.c: "暂未实现用例不伪造空成功测试").
//
// Fixture registry (§21.2; hashes live in testdata/journeys/manifest.json):
//
//	fixture                status                owning step / consumer
//	project-go-small       implemented (T0.06.a)  isolated by TestJourney_FixtureIsolation
//	project-node-small     implemented (T0.06.a)  isolated by TestJourney_FixtureIsolation
//	project-no-git         implemented (T0.06.a)  isolated by TestJourney_FixtureIsolation
//	project-junction       pending                 T1.10 (root identity, junction escape)
//	run-tool-sequence      pending                 T1.06.b (fake backend scripted observations)
//	run-output-flood       pending                 T1.08 (process supervisor backpressure)
//	run-crash-restart      pending                 T1.08/T1.09 (kill server per run state, recovery)
//	run-secret-canary      pending                 T2.04 (secret redaction canary scans)
//	run-merge-conflict     pending                 T1.09/T2.03 (three-way merge conflict)
//	memory-low-confidence  pending                 T6.01/T6.02/T6.04 (low-confidence memory)
//
// Journey registry (§15 T0.06 journey list plus the §4 milestone paths):
//
//	journey test ID                    status                  blocked on / consumes
//	TestJourney_FixtureIsolation       implemented (this file) T0.06.c
//	TestJourney_FirstRunOnboarding     pending skeleton         T0.07.c + T0.12.c + T2.06.c
//	TestJourney_CreateProject          pending skeleton         T1.04.c; consumes project-go-small/project-node-small/project-no-git
//	TestJourney_CreateTask             pending skeleton         T3.03.c
//	TestJourney_CreateRun              pending skeleton         T1.04.c + T1.06.b; consumes project-go-small
//	TestJourney_ApprovalConsumeOnce    pending skeleton         T2.02 + T3.04; consumes run-tool-sequence
//	TestJourney_FakeRunToMerge         pending skeleton         T1.14.c (test ID named there); consumes project-* + run-merge-conflict
//	TestJourney_ServerRestartRecovery  pending skeleton         T1.04.c + T1.08.c + T1.09.c; consumes run-crash-restart
//	TestJourney_StreamingReconnect     pending skeleton         T1.12.c
//	TestJourney_MemoryLowConfidence    pending skeleton         T6.01/T6.02/T6.04; consumes memory-low-confidence
//	TestJourney_BookmarkBranch         pending skeleton         T7.01/T7.02
//	TestJourney_DebateGate             pending skeleton         T8.03
package api

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snapshotRegularFiles walks root and returns every regular file's content
// keyed by slash-separated relative path, so a test can prove a fixture copy
// stayed byte-identical while another copy was mutated.
func snapshotRegularFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := make(map[string][]byte)
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		require.True(t, d.Type().IsRegular(), "fixture copy must hold regular files only: %s", path)
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		out[filepath.ToSlash(rel)] = data
		return nil
	}))
	return out
}

// assertTreeUnchanged fails when current does not hold exactly the files and
// bytes recorded in before, reporting per file.
func assertTreeUnchanged(t *testing.T, label string, before, current map[string][]byte) {
	t.Helper()
	keys := make([]string, 0, len(before))
	for name := range before {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	require.Lenf(t, current, len(before), "%s file set changed", label)
	for _, name := range keys {
		got, ok := current[name]
		require.Truef(t, ok, "%s lost file %s", label, name)
		assert.Equalf(t, before[name], got, "%s file %s changed while another copy was mutated", label, name)
	}
}

func TestJourney_FixtureIsolation(t *testing.T) {
	m := loadJourneyManifest(t)

	t.Run("same_fixture_two_copies_are_independent", func(t *testing.T) {
		fx, err := m.fixtureByName("project-no-git")
		require.NoError(t, err)

		dirA := copyJourneyFixture(t, "project-no-git")
		dirB := copyJourneyFixture(t, "project-no-git")
		require.NotEqual(t, dirA, dirB, "each copy must live in its own temp dir")

		beforeB := snapshotRegularFiles(t, dirB)

		// Tamper mode 1: modify a file in copy A. A's own verification must
		// fail and name the file — negative evidence the tamper is real and
		// detected.
		helloA := filepath.Join(dirA, "src", "hello.txt")
		original, err := os.ReadFile(helloA)
		require.NoError(t, err)
		require.NotEmpty(t, original)
		mutated := append([]byte(nil), original...)
		mutated[0] ^= 0xFF
		require.NoError(t, os.WriteFile(helloA, mutated, 0o644))
		err = verifyJourneyFiles(dirA, fx.Files)
		require.Error(t, err, "tampered copy A must fail manifest verification")
		assert.Contains(t, err.Error(), "src/hello.txt")

		// Tamper mode 2: delete a file from copy A.
		require.NoError(t, os.Remove(filepath.Join(dirA, "docs", "notes.md")))
		err = verifyJourneyFiles(dirA, fx.Files)
		require.Error(t, err, "copy A with a deleted file must fail verification")
		assert.Contains(t, err.Error(), "docs/notes.md")

		// Tamper mode 3: add a file the manifest does not list.
		require.NoError(t, os.WriteFile(filepath.Join(dirA, "stray.txt"), []byte("stray"), 0o644))
		err = verifyNoExtraFiles(dirA, fx.Files)
		require.Error(t, err, "copy A with an extra file must fail verification")
		assert.Contains(t, err.Error(), "stray.txt")

		// Positive evidence in the other direction: copy B of the same
		// fixture is untouched — manifest verification still passes and
		// every file is byte-identical to the snapshot taken before A was
		// mutated.
		require.NoError(t, verifyJourneyFiles(dirB, fx.Files))
		require.NoError(t, verifyNoExtraFiles(dirB, fx.Files))
		assertTreeUnchanged(t, "copy B", beforeB, snapshotRegularFiles(t, dirB))

		// The mutation really happened in A only: A now disagrees with the
		// original bytes while B still holds them, and B still has the file
		// A deleted and lacks the file A added.
		helloB, err := os.ReadFile(filepath.Join(dirB, "src", "hello.txt"))
		require.NoError(t, err)
		assert.Equal(t, original, helloB, "copy B must keep the pristine content")
		assert.NotEqual(t, mutated, helloB, "copy B must not observe copy A's edit")
		_, statErr := os.Stat(filepath.Join(dirB, "docs", "notes.md"))
		assert.NoError(t, statErr, "copy B must still hold the file deleted from copy A")
		_, statErr = os.Stat(filepath.Join(dirB, "stray.txt"))
		assert.True(t, os.IsNotExist(statErr), "copy B must not gain the file added to copy A")
	})

	t.Run("source_tree_is_not_polluted", func(t *testing.T) {
		fx, err := m.fixtureByName("project-no-git")
		require.NoError(t, err)

		dirA := copyJourneyFixture(t, "project-no-git")
		helloA := filepath.Join(dirA, "src", "hello.txt")
		mutated, err := os.ReadFile(helloA)
		require.NoError(t, err)
		require.NotEmpty(t, mutated)
		mutated[0] ^= 0xFF
		require.NoError(t, os.WriteFile(helloA, mutated, 0o644))
		require.NoError(t, os.Remove(filepath.Join(dirA, "docs", "notes.md")))
		require.Error(t, verifyJourneyFiles(dirA, fx.Files), "copy A tamper must be real before checking the source tree")

		// The shared source tree under testdata/journeys stays byte-identical
		// to the manifest after copy A was mutated.
		src := filepath.Join(journeysTestdataDir, filepath.FromSlash(fx.Path))
		require.NoError(t, verifyJourneyFiles(src, fx.Files), "source fixture must stay byte-identical after copy A was mutated")
		require.NoError(t, verifyNoExtraFiles(src, fx.Files), "mutations in copy A must not leak into the source tree")

		// A third copy taken after the tampering still passes manifest
		// verification and matches the pristine source content.
		dirC := copyJourneyFixture(t, "project-no-git")
		require.NoError(t, verifyJourneyFiles(dirC, fx.Files))
		helloC, err := os.ReadFile(filepath.Join(dirC, "src", "hello.txt"))
		require.NoError(t, err)
		helloSrc, err := os.ReadFile(filepath.Join(src, "src", "hello.txt"))
		require.NoError(t, err)
		assert.Equal(t, helloSrc, helloC, "fresh copy must match the pristine source")
		assert.NotEqual(t, mutated, helloC, "fresh copy must not inherit copy A's edit")
	})

	t.Run("different_fixtures_are_independent", func(t *testing.T) {
		fxGo, err := m.fixtureByName("project-go-small")
		require.NoError(t, err)
		fxNode, err := m.fixtureByName("project-node-small")
		require.NoError(t, err)

		// The two fixtures are genuinely different trees: the one path they
		// share (README.md) carries different manifest hashes.
		readmeHash := func(fx journeyManifestFixture) string {
			for _, f := range fx.Files {
				if f.Path == "README.md" {
					return f.SHA256
				}
			}
			return ""
		}
		require.NotEmpty(t, readmeHash(fxGo))
		require.NotEmpty(t, readmeHash(fxNode))
		require.NotEqual(t, readmeHash(fxGo), readmeHash(fxNode), "fixtures must be distinct trees, not aliases")

		dirGo := copyJourneyFixture(t, "project-go-small")
		dirNode := copyJourneyFixture(t, "project-node-small")
		nodeBefore := snapshotRegularFiles(t, dirNode)

		// Damage the Go copy: rewrite a source file, delete go.mod, add a
		// marker file. Its own verification must fail...
		require.NoError(t, os.WriteFile(filepath.Join(dirGo, "mathx", "mathx.go"), []byte("package mathx // tampered\n"), 0o644))
		require.NoError(t, os.Remove(filepath.Join(dirGo, "go.mod")))
		require.NoError(t, os.WriteFile(filepath.Join(dirGo, "marker.tmp"), []byte("x"), 0o644))
		err = verifyJourneyFiles(dirGo, fxGo.Files)
		require.Error(t, err, "damaged Go copy must fail its manifest verification")
		assert.Contains(t, err.Error(), "go.mod")
		require.Error(t, verifyNoExtraFiles(dirGo, fxGo.Files), "marker file in the Go copy must be detected")

		// ...while the Node copy still passes its own manifest verification
		// and is byte-identical to its pre-tamper snapshot.
		require.NoError(t, verifyJourneyFiles(dirNode, fxNode.Files))
		require.NoError(t, verifyNoExtraFiles(dirNode, fxNode.Files))
		assertTreeUnchanged(t, "node copy", nodeBefore, snapshotRegularFiles(t, dirNode))
	})
}

// ---------------------------------------------------------------------------
// Pending journey registrations (§15 T0.06 pending test table).
//
// Each skeleton below is a registration, not an implementation: the body is
// a single t.Skipf carrying the blocking reason and the owning task card,
// with no assertions, so the journey shows up as an explicit SKIP instead of
// a fake pass. Replace the skeleton with the real journey when the listed
// cards land; do not add a second test with the same name.
// ---------------------------------------------------------------------------

// TestJourney_FirstRunOnboarding covers §4 journey 1 (首启向导: master key,
// directory binding, backend detection, one model channel).
func TestJourney_FirstRunOnboarding(t *testing.T) {
	t.Skipf("pending: first-run journey needs the sidecar handshake (T0.07.c), readiness/capability gating (T0.12.c) and vault first-boot (T2.06.c); registered by T0.06.c per plan §15 T0.06")
}

// TestJourney_CreateProject covers creating a project through the real API
// against a manifest-verified fixture workspace (project-go-small /
// project-node-small / project-no-git).
func TestJourney_CreateProject(t *testing.T) {
	t.Skipf("pending: project creation journey is re-verified end to end once the Run API wiring lands (T1.04.c); T0.11.c covered only the service-level idempotency; consumes project-go-small/project-node-small/project-no-git; registered by T0.06.c per plan §15 T0.06")
}

// TestJourney_CreateTask covers creating a Task inside a project flow.
func TestJourney_CreateTask(t *testing.T) {
	t.Skipf("pending: task creation journey needs the task/queue domain (T3.03.c, J check); registered by T0.06.c per plan §15 T0.06")
}

// TestJourney_CreateRun covers dispatching a Run against the fake execution
// backend on a fixture workspace.
func TestJourney_CreateRun(t *testing.T) {
	t.Skipf("pending: run creation journey needs the runs handler + worker (T1.04.c) and the scripted fake backend (T1.06.b); consumes project-go-small; registered by T0.06.c per plan §15 T0.06")
}

// TestJourney_ApprovalConsumeOnce covers §27.2: one approval authorizes
// exactly one tool call consumption.
func TestJourney_ApprovalConsumeOnce(t *testing.T) {
	t.Skipf("pending: approval consume-once journey needs the approval domain (T2.02) and its API/guard wiring (T3.04); consumes run-tool-sequence; registered by T0.06.c per plan §15 T0.06")
}

// TestJourney_FakeRunToMerge covers §4 journey 2: create project -> run ->
// review events -> approve -> merge -> audit. The test ID is named by
// T1.14.c; keep this exact name when implementing.
func TestJourney_FakeRunToMerge(t *testing.T) {
	t.Skipf("pending: full create-project -> run -> merge journey is owned by T1.14.c (J check) on top of T1.04.c/T1.09/T2.03; consumes project-* and run-merge-conflict; registered by T0.06.c per plan §15 T0.06")
}

// TestJourney_ServerRestartRecovery covers killing the server in running /
// waiting_approval / cancelling states and reconciling afterwards.
func TestJourney_ServerRestartRecovery(t *testing.T) {
	t.Skipf("pending: restart recovery journey needs worker restart verification (T1.04.c), process supervision (T1.08.c) and merge recovery (T1.09.c); consumes run-crash-restart; registered by T0.06.c per plan §15 T0.06")
}

// TestJourney_StreamingReconnect covers §4 断网: WS disconnect/reconnect with
// cursor resume and no event gap.
func TestJourney_StreamingReconnect(t *testing.T) {
	t.Skipf("pending: streaming reconnect journey needs ordered replay and slow-client handling (T1.12.c); registered by T0.06.c per plan §15 T0.06")
}

// TestJourney_MemoryLowConfidence covers §4 journey 6: low-confidence memory
// candidates, user rejection and later retrieval.
func TestJourney_MemoryLowConfidence(t *testing.T) {
	t.Skipf("pending: memory journey needs the memory/context domain (T6.01/T6.02/T6.04); consumes memory-low-confidence; registered by T0.06.c per plan §15 T0.06")
}

// TestJourney_BookmarkBranch covers §4 journey 7: open a new path from a
// stage bookmark and compare side by side.
func TestJourney_BookmarkBranch(t *testing.T) {
	t.Skipf("pending: bookmark journey needs the bookmark domain (T7.01) and its run/merge wiring (T7.02, J check); registered by T0.06.c per plan §15 T0.06")
}

// TestJourney_DebateGate covers §4 journey 8: review gate failure ->
// three-way debate -> conclusion artifact.
func TestJourney_DebateGate(t *testing.T) {
	t.Skipf("pending: debate journey needs the orchestrator/debate domain (T8.03, J check); registered by T0.06.c per plan §15 T0.06")
}
