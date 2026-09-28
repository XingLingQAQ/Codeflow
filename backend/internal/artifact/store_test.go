package artifact

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/run"
	"github.com/codeflow/backend/internal/runstore"
)

// fixture owns the three things a version test needs: a migrated store, a blob
// store, and the project the versions belong to.
type fixture struct {
	store  *runstore.Store
	blobs  *BlobStore
	blobR  string
	ctx    context.Context
	now    time.Time
	projID string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	store, _, err := runstore.OpenStore(ctx, filepath.Join(t.TempDir(), "codeflow.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	blobRoot := filepath.Join(t.TempDir(), "blobs")
	blobs, err := OpenBlobStore(blobRoot, BlobStoreOptions{})
	if err != nil {
		t.Fatalf("OpenBlobStore: %v", err)
	}
	t.Cleanup(func() { _ = blobs.Close() })

	f := &fixture{store: store, blobs: blobs, blobR: blobRoot, ctx: ctx,
		now: time.UnixMilli(1700000000000).UTC(), projID: "p-1"}
	f.seedProject(t, "p-1")
	return f
}

// seedProject writes the project_refs row every artifact version points at.
func (f *fixture) seedProject(t *testing.T, projectID string) {
	t.Helper()
	err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		return runstore.UpsertProjectRef(ctx, tx, run.ProjectRef{
			ProjectID:    projectID,
			SnapshotHash: "sha256:" + projectID,
			State:        run.ProjectRefStateActive,
			CapturedAt:   f.now,
			VerifiedAt:   f.now,
		})
	})
	if err != nil {
		t.Fatalf("seed project %s: %v", projectID, err)
	}
}

// put writes content into the blob store and returns its hash and size.
func (f *fixture) put(t *testing.T, content string) (string, int64) {
	t.Helper()
	hash, size, err := f.blobs.Put(f.ctx, bytes.NewReader([]byte(content)))
	if err != nil {
		t.Fatalf("Put(%q): %v", content, err)
	}
	return hash, size
}

// createVersion runs CreateVersionTx in its own transaction.
func (f *fixture) createVersion(t *testing.T, in VersionInput) Version {
	t.Helper()
	var out Version
	err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		v, err := CreateVersionTx(ctx, tx, f.blobs, in)
		if err != nil {
			return err
		}
		out = v
		return nil
	})
	if err != nil {
		t.Fatalf("create version %s: %v", in.ID, err)
	}
	return out
}

// TestCreateVersionAssignsSequentialNumbers: version_no is per artifact and
// grows by one, while a different artifact starts at 1 again.
func TestCreateVersionAssignsSequentialNumbers(t *testing.T) {
	f := newFixture(t)
	hashA, sizeA := f.put(t, "content A\n")
	hashB, sizeB := f.put(t, "content B\n")

	first := f.createVersion(t, VersionInput{
		ID: "v-1", ArtifactID: "art-1", ProjectID: f.projID,
		ContentHash: hashA, Size: sizeA, CreatedBy: "user-1", CreatedAt: f.now,
	})
	if first.VersionNo != 1 {
		t.Errorf("first VersionNo = %d, want 1", first.VersionNo)
	}
	if first.ContentRef != "blob:"+hashA {
		t.Errorf("ContentRef = %q, want blob:%s", first.ContentRef, hashA)
	}

	second := f.createVersion(t, VersionInput{
		ID: "v-2", ArtifactID: "art-1", ProjectID: f.projID,
		ContentHash: hashB, Size: sizeB, CreatedBy: "user-1", CreatedAt: f.now.Add(time.Second),
		ParentVersionID: first.ID,
	})
	if second.VersionNo != 2 {
		t.Errorf("second VersionNo = %d, want 2", second.VersionNo)
	}
	if second.ParentVersionID != "v-1" {
		t.Errorf("ParentVersionID = %q, want v-1", second.ParentVersionID)
	}

	// A different artifact has its own sequence, not a global one.
	other := f.createVersion(t, VersionInput{
		ID: "v-3", ArtifactID: "art-2", ProjectID: f.projID,
		ContentHash: hashA, Size: sizeA, CreatedBy: "user-1", CreatedAt: f.now,
	})
	if other.VersionNo != 1 {
		t.Errorf("other artifact VersionNo = %d, want 1 (sequences are per artifact)", other.VersionNo)
	}

	// The same content under two artifacts is legal: content_hash is not a
	// global unique key (§19.1).
	if other.ContentHash != first.ContentHash {
		t.Errorf("the two artifacts should share a hash, got %s and %s", other.ContentHash, first.ContentHash)
	}
}

// TestCreateVersionReadsBack covers the three readers and their ordering, plus
// the optional columns round-tripping as empty strings.
func TestCreateVersionReadsBack(t *testing.T) {
	f := newFixture(t)
	hash1, size1 := f.put(t, "one\n")
	hash2, size2 := f.put(t, "two\n")

	first := f.createVersion(t, VersionInput{
		ID: "v-1", ArtifactID: "art-1", ProjectID: f.projID,
		ContentHash: hash1, Size: size1, CreatedBy: "agent:ar-1", CreatedAt: f.now,
	})
	second := f.createVersion(t, VersionInput{
		ID: "v-2", ArtifactID: "art-1", ProjectID: f.projID,
		ContentHash: hash2, Size: size2, CreatedBy: "agent:ar-1", CreatedAt: f.now.Add(time.Second),
		ParentVersionID: first.ID,
	})

	got, err := GetVersion(f.ctx, f.store.DB(), second.ID)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if got.ID != second.ID || got.ArtifactID != "art-1" || got.ProjectID != f.projID ||
		got.VersionNo != 2 || got.ContentHash != hash2 || got.Size != size2 ||
		got.ContentRef != "blob:"+hash2 || got.CreatedBy != "agent:ar-1" ||
		got.ParentVersionID != first.ID {
		t.Errorf("GetVersion = %+v, want the created version", got)
	}
	if !got.CreatedAt.Equal(f.now.Add(time.Second)) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, f.now.Add(time.Second))
	}
	for name, value := range map[string]string{
		"StageID": got.StageID, "RunID": got.RunID,
		"AttemptID": got.AttemptID,
	} {
		if value != "" {
			t.Errorf("%s = %q, want empty (NULL round-trips as the zero value)", name, value)
		}
	}

	list, err := ListVersions(f.ctx, f.store.DB(), "art-1")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(list) != 2 || list[0].VersionNo != 1 || list[1].VersionNo != 2 {
		t.Errorf("ListVersions = %+v, want versions 1 then 2", list)
	}

	latest, err := LatestVersion(f.ctx, f.store.DB(), "art-1")
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}
	if latest.ID != "v-2" {
		t.Errorf("LatestVersion = %s, want v-2", latest.ID)
	}

	// An artifact with no versions is an empty list and ErrNotFound, not a
	// database error: a caller about to create the first version asks this.
	empty, err := ListVersions(f.ctx, f.store.DB(), "art-none")
	if err != nil {
		t.Fatalf("ListVersions(unknown): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("ListVersions(unknown) = %+v, want empty", empty)
	}
	if _, err := LatestVersion(f.ctx, f.store.DB(), "art-none"); !errors.Is(err, runstore.ErrNotFound) {
		t.Errorf("LatestVersion(unknown) = %v, want runstore.ErrNotFound", err)
	}
	if _, err := GetVersion(f.ctx, f.store.DB(), "v-none"); !errors.Is(err, runstore.ErrNotFound) {
		t.Errorf("GetVersion(unknown) = %v, want runstore.ErrNotFound", err)
	}
}

// TestCreateVersionRequiresBlob is the "no visible version without content"
// rule: a version naming a hash that was never Put, or a size that disagrees
// with the stored blob, is refused and writes no row.
func TestCreateVersionRequiresBlob(t *testing.T) {
	f := newFixture(t)
	hash, size := f.put(t, "stored\n")

	cases := []struct {
		name string
		in   VersionInput
	}{
		{"blob never written", VersionInput{
			ID: "v-1", ArtifactID: "art-1", ProjectID: f.projID,
			ContentHash: HashBytes([]byte("never written")), Size: 13,
			CreatedBy: "user-1", CreatedAt: f.now,
		}},
		{"size disagrees with the blob", VersionInput{
			ID: "v-1", ArtifactID: "art-1", ProjectID: f.projID,
			ContentHash: hash, Size: size + 1, CreatedBy: "user-1", CreatedAt: f.now,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
				_, err := CreateVersionTx(ctx, tx, f.blobs, tc.in)
				return err
			})
			if !errors.Is(err, ErrBlobMissing) {
				t.Fatalf("CreateVersionTx = %v, want ErrBlobMissing", err)
			}
			if _, err := GetVersion(f.ctx, f.store.DB(), tc.in.ID); !errors.Is(err, runstore.ErrNotFound) {
				t.Errorf("a refused version left a row behind: %v", err)
			}
		})
	}
}

// TestCreateVersionValidation collects the pre-SQL checks: each must be refused
// before anything is written, and each must leave no row.
func TestCreateVersionValidation(t *testing.T) {
	f := newFixture(t)
	hash, size := f.put(t, "validated\n")
	base := VersionInput{
		ID: "v-1", ArtifactID: "art-1", ProjectID: f.projID,
		ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
	}

	cases := []struct {
		name    string
		mutate  func(*VersionInput)
		wantErr error
	}{
		{"empty id", func(in *VersionInput) { in.ID = "" }, ErrInvalidVersion},
		{"blank id", func(in *VersionInput) { in.ID = "  " }, ErrInvalidVersion},
		{"empty artifact", func(in *VersionInput) { in.ArtifactID = "" }, ErrInvalidVersion},
		{"empty project", func(in *VersionInput) { in.ProjectID = "" }, ErrInvalidVersion},
		{"empty actor", func(in *VersionInput) { in.CreatedBy = "" }, ErrInvalidVersion},
		{"empty hash", func(in *VersionInput) { in.ContentHash = "" }, ErrInvalidVersion},
		{"uppercase hash", func(in *VersionInput) { in.ContentHash = "sha256:" + upperHex(hash) }, ErrInvalidVersion},
		{"no prefix", func(in *VersionInput) { in.ContentHash = hash[len(HashPrefix):] }, ErrInvalidVersion},
		{"negative size", func(in *VersionInput) { in.Size = -1 }, ErrInvalidVersion},
		{"zero time", func(in *VersionInput) { in.CreatedAt = time.Time{} }, ErrInvalidVersion},
		{"attempt without run", func(in *VersionInput) { in.AttemptID = "a-1" }, ErrInvalidVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The artifact is unique per case so "no row was written" can be
			// asked of the table rather than of one id — two of these cases
			// blank the id itself, which no lookup could address.
			in := base
			in.ArtifactID = "art-" + tc.name
			tc.mutate(&in)

			err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
				_, err := CreateVersionTx(ctx, tx, f.blobs, in)
				return err
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("CreateVersionTx = %v, want %v", err, tc.wantErr)
			}
			rows, err := ListVersions(f.ctx, f.store.DB(), "art-"+tc.name)
			if err != nil {
				t.Fatalf("ListVersions: %v", err)
			}
			if len(rows) != 0 {
				t.Errorf("a refused version left a row behind: %+v", rows)
			}
		})
	}
}

func upperHex(hash string) string {
	out := []byte(hash[len(HashPrefix):])
	for i, c := range out {
		if c >= 'a' && c <= 'f' {
			out[i] = c - 'a' + 'A'
		}
	}
	return string(out)
}

// TestCreateVersionProjectIsFixedForArtifact: an artifact belongs to one
// project, so reusing its id under a second project is refused.
func TestCreateVersionProjectIsFixedForArtifact(t *testing.T) {
	f := newFixture(t)
	f.seedProject(t, "p-2")
	hash, size := f.put(t, "content\n")

	f.createVersion(t, VersionInput{
		ID: "v-1", ArtifactID: "art-1", ProjectID: "p-1",
		ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
	})

	err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		_, err := CreateVersionTx(ctx, tx, f.blobs, VersionInput{
			ID: "v-2", ArtifactID: "art-1", ProjectID: "p-2",
			ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
		})
		return err
	})
	if !errors.Is(err, ErrArtifactProjectMismatch) {
		t.Fatalf("cross-project reuse = %v, want ErrArtifactProjectMismatch", err)
	}
	if _, err := ListVersions(f.ctx, f.store.DB(), "art-1"); err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
}

// TestCreateVersionParentMustBeSameArtifact: a parent is a predecessor in the
// same artifact's history.
func TestCreateVersionParentMustBeSameArtifact(t *testing.T) {
	f := newFixture(t)
	hash, size := f.put(t, "content\n")

	f.createVersion(t, VersionInput{
		ID: "v-1", ArtifactID: "art-1", ProjectID: f.projID,
		ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
	})
	f.createVersion(t, VersionInput{
		ID: "v-2", ArtifactID: "art-2", ProjectID: f.projID,
		ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
	})

	for name, parent := range map[string]string{"absent": "v-none", "other artifact": "v-2"} {
		t.Run(name, func(t *testing.T) {
			err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
				_, err := CreateVersionTx(ctx, tx, f.blobs, VersionInput{
					ID: "v-3", ArtifactID: "art-1", ProjectID: f.projID,
					ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
					ParentVersionID: parent,
				})
				return err
			})
			if !errors.Is(err, ErrParentVersionMismatch) {
				t.Fatalf("parent %s = %v, want ErrParentVersionMismatch", parent, err)
			}
		})
	}
}

// TestCreateVersionRunMustMatchProject covers the check the database cannot
// express (migration 008 explains why): a run/attempt named by a version must
// belong to the same project.
func TestCreateVersionRunMustMatchProject(t *testing.T) {
	f := newFixture(t)
	f.seedProject(t, "p-2")
	hash, size := f.put(t, "with a run\n")

	// A task and run in p-1, plus a task/run in p-2. The fixtures go through
	// the store so the schema's own triggers (frozen input, snapshot match) are
	// satisfied exactly as production writes them.
	seedRun := func(t *testing.T, projectID, taskID, runID, snapshotID string, attemptID string) {
		t.Helper()
		err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
			task := run.Task{
				ID: taskID, ProjectID: projectID, Title: "t", Kind: run.TaskKindCode,
				Status: run.TaskStatusReady, InputJSON: `{"p":1}`,
				CreatedAt: f.now, UpdatedAt: f.now,
			}
			if err := runstore.InsertTask(ctx, tx, &task); err != nil {
				return err
			}
			hash, err := runstore.InsertInputSnapshot(ctx, tx, projectID, snapshotID, []byte(`{"p":1}`), f.now)
			if err != nil {
				return err
			}
			r := run.Run{
				ID: runID, TaskID: taskID, ProjectID: projectID,
				BindingID: "b-1", BindingRevision: 1,
				BaseManifestHash: "sha256:manifest", AgentRevisionID: "ar-1",
				InputSnapshotID: snapshotID, InputSnapshotHash: hash,
				Status:    run.RunStatusQueued,
				CreatedAt: f.now, UpdatedAt: f.now,
			}
			if err := runstore.InsertRun(ctx, tx, &r); err != nil {
				return err
			}
			if attemptID == "" {
				return nil
			}
			a := run.Attempt{
				ID: attemptID, RunID: runID, AttemptNo: 1, Backend: "fake",
				Status: run.AttemptStatusRunning, CreatedAt: f.now,
			}
			return runstore.InsertAttempt(ctx, tx, &a)
		})
		if err != nil {
			t.Fatalf("seed run %s: %v", runID, err)
		}
	}
	seedRun(t, "p-1", "t-1", "r-1", "snap-1", "a-1")
	seedRun(t, "p-2", "t-2", "r-2", "snap-2", "a-2")

	// A version in p-1 may name p-1's run and attempt.
	f.createVersion(t, VersionInput{
		ID: "v-ok", ArtifactID: "art-1", ProjectID: "p-1",
		ContentHash: hash, Size: size, CreatedBy: "agent:ar-1", CreatedAt: f.now,
		RunID: "r-1", AttemptID: "a-1",
	})

	refused := []struct {
		name string
		in   VersionInput
	}{
		{"run of another project", VersionInput{
			ID: "v-1", ArtifactID: "art-2", ProjectID: "p-1",
			ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now, RunID: "r-2",
		}},
		{"run that does not exist", VersionInput{
			ID: "v-2", ArtifactID: "art-3", ProjectID: "p-1",
			ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now, RunID: "r-none",
		}},
		{"attempt of another run", VersionInput{
			ID: "v-3", ArtifactID: "art-4", ProjectID: "p-1",
			ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
			RunID: "r-1", AttemptID: "a-2",
		}},
		{"attempt that does not exist", VersionInput{
			ID: "v-4", ArtifactID: "art-5", ProjectID: "p-1",
			ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
			RunID: "r-1", AttemptID: "a-none",
		}},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
				_, err := CreateVersionTx(ctx, tx, f.blobs, tc.in)
				return err
			})
			if !errors.Is(err, ErrRunProjectMismatch) {
				t.Fatalf("CreateVersionTx = %v, want ErrRunProjectMismatch", err)
			}
			if _, err := GetVersion(f.ctx, f.store.DB(), tc.in.ID); !errors.Is(err, runstore.ErrNotFound) {
				t.Errorf("a refused version left a row behind: %v", err)
			}
		})
	}
}

// TestCreateVersionWithoutRunIsAllowed pins §19.1 "人工成果允许无 Run": a manual
// artifact is a version with no run at all, and nothing should force one.
func TestCreateVersionWithoutRunIsAllowed(t *testing.T) {
	f := newFixture(t)
	hash, size := f.put(t, "hand written\n")
	v := f.createVersion(t, VersionInput{
		ID: "v-1", ArtifactID: "art-1", ProjectID: f.projID,
		ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
		StageID: "stage-legacy-1",
	})
	if v.RunID != "" || v.AttemptID != "" {
		t.Errorf("RunID/AttemptID = %q/%q, want empty", v.RunID, v.AttemptID)
	}
	if v.StageID != "stage-legacy-1" {
		t.Errorf("StageID = %q, want stage-legacy-1", v.StageID)
	}
}

// TestRollbackLeavesBlobButNoVersion is the ordering rule from the other side:
// the blob is written before the transaction, so rolling the transaction back
// must lose the version and keep the blob. That asymmetry is the whole design —
// an unreferenced blob is harmless, a version without a blob is not — and this
// test is what stops someone "fixing" it by deleting the blob on rollback.
func TestRollbackLeavesBlobButNoVersion(t *testing.T) {
	f := newFixture(t)
	hash, size := f.put(t, "content that survives the rollback\n")

	sentinel := errors.New("caller aborted after CreateVersionTx")
	err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		if _, err := CreateVersionTx(ctx, tx, f.blobs, VersionInput{
			ID: "v-1", ArtifactID: "art-1", ProjectID: f.projID,
			ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
		}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx = %v, want the sentinel back", err)
	}

	if _, err := GetVersion(f.ctx, f.store.DB(), "v-1"); !errors.Is(err, runstore.ErrNotFound) {
		t.Errorf("the rolled-back version is still visible: %v", err)
	}
	if has, err := f.blobs.Has(hash); err != nil || !has {
		t.Errorf("Has(blob) = %v, %v; the blob must survive the rollback", has, err)
	}
	if _, err := f.blobs.Stat(hash); err != nil {
		t.Errorf("Stat(blob) = %v, want the blob to still be there", err)
	}
}

// TestVersionOrderingIgnoresCreatedAt: version_no is the order, not the
// timestamp. Two versions written in the same millisecond (or with a clock that
// went backwards) must still list in allocation order.
func TestVersionOrderingIgnoresCreatedAt(t *testing.T) {
	f := newFixture(t)
	hash, size := f.put(t, "ordered\n")

	// The second version carries an *earlier* created_at on purpose.
	f.createVersion(t, VersionInput{
		ID: "v-1", ArtifactID: "art-1", ProjectID: f.projID,
		ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now.Add(time.Hour),
	})
	f.createVersion(t, VersionInput{
		ID: "v-2", ArtifactID: "art-1", ProjectID: f.projID,
		ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
	})

	list, err := ListVersions(f.ctx, f.store.DB(), "art-1")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(list) != 2 || list[0].ID != "v-1" || list[1].ID != "v-2" {
		t.Errorf("ListVersions order = %+v, want v-1 then v-2 (by version_no)", list)
	}
}

// TestCreateVersionConcurrentSameArtifact: two transactions racing to create a
// version of one artifact must not both get version number 1. Whichever loses
// either blocks (BEGIN IMMEDIATE serializes them) or fails with
// ErrVersionConflict; both outcomes are acceptable, a duplicate number is not.
func TestCreateVersionConcurrentSameArtifact(t *testing.T) {
	f := newFixture(t)
	hash, size := f.put(t, "contended\n")

	const writers = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		numbers []int64
		raced   int
		other   []error
		started = make(chan struct{})
	)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-started
			var got Version
			err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
				v, err := CreateVersionTx(ctx, tx, f.blobs, VersionInput{
					ID: fmt.Sprintf("v-%d", i), ArtifactID: "art-1", ProjectID: f.projID,
					ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
				})
				got = v
				return err
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				numbers = append(numbers, got.VersionNo)
			case errors.Is(err, ErrVersionConflict):
				raced++
			default:
				other = append(other, err)
			}
		}(i)
	}
	close(started)
	wg.Wait()

	if len(other) != 0 {
		t.Fatalf("unexpected errors: %v", other)
	}
	seen := map[int64]bool{}
	for _, n := range numbers {
		if seen[n] {
			t.Fatalf("version %d was allocated twice (numbers=%v, raced=%d)", n, numbers, raced)
		}
		seen[n] = true
	}
	// Every writer either committed a distinct version or reported a conflict.
	if len(numbers)+raced != writers {
		t.Errorf("committed %d + raced %d != %d writers", len(numbers), raced, writers)
	}

	stored, err := ListVersions(f.ctx, f.store.DB(), "art-1")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if int64(len(stored)) != int64(len(numbers)) {
		t.Errorf("stored %d versions, %d writers reported success", len(stored), len(numbers))
	}
	for i, v := range stored {
		if v.VersionNo != int64(i+1) {
			t.Errorf("stored[%d].VersionNo = %d, want %d (no holes, no duplicates)", i, v.VersionNo, i+1)
		}
	}
}

// TestSchemaRejectsContentHashWithoutPrefix proves the migration's CHECK is
// really in the database and not only in Go: a hand-written insert that skips
// the store must still be refused.
func TestSchemaRejectsContentHashWithoutPrefix(t *testing.T) {
	f := newFixture(t)
	db := f.store.DB()

	// The project_refs row is seeded by the fixture, so only the hash is wrong.
	_, err := db.Exec(`
		INSERT INTO artifact_versions
			(id, artifact_id, project_id, version_no, content_hash, size, content_ref, created_by, created_at)
		VALUES ('v-raw', 'art-1', 'p-1', 1, 'not-a-hash', 1, 'blob:not-a-hash', 'user-1', 1)`)
	if err == nil {
		t.Fatal("the schema accepted a content_hash that is not sha256:<64 hex>")
	}

	// A NULL project must be refused too: every version belongs to a project.
	_, err = db.Exec(`
		INSERT INTO artifact_versions
			(id, artifact_id, project_id, version_no, content_hash, size, content_ref, created_by, created_at)
		VALUES ('v-null', 'art-1', NULL, 1, ?, 1, 'blob:x', 'user-1', 1)`, HashBytes(nil))
	if err == nil {
		t.Fatal("the schema accepted a NULL project_id")
	}
}

// TestVersionRowsAreNotExternallyMutable documents the boundary this card does
// not cross: retention (T12.02) will need to retire rows, so no delete trigger
// is added here, but the store exposes no update path and the table is not
// addressable through any exported function that rewrites a row.
func TestVersionRowsAreNotExternallyMutable(t *testing.T) {
	f := newFixture(t)
	hash, size := f.put(t, "immutable by contract\n")
	v := f.createVersion(t, VersionInput{
		ID: "v-1", ArtifactID: "art-1", ProjectID: f.projID,
		ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
	})

	// The unique constraint is the real guard against duplicate version
	// numbers, whatever a future caller does.
	_, err := f.store.DB().Exec(`
		INSERT INTO artifact_versions
			(id, artifact_id, project_id, version_no, content_hash, size, content_ref, created_by, created_at)
		VALUES ('v-dup', 'art-1', 'p-1', ?, ?, 20, ?, 'user-1', 1)`,
		v.VersionNo, hash, "blob:"+hash)
	if err == nil {
		t.Fatal("(artifact_id, version_no) unique constraint did not fire")
	}

	// And a version cannot name a run the database has never seen.
	_, err = f.store.DB().Exec(`
		INSERT INTO artifact_versions
			(id, artifact_id, project_id, version_no, content_hash, size, content_ref, created_by, created_at, run_id)
		VALUES ('v-run', 'art-2', 'p-1', 1, ?, 20, ?, 'user-1', 1, 'r-none')`,
		hash, "blob:"+hash)
	if err == nil {
		t.Fatal("a version named a run that does not exist")
	}
}

// TestCreateVersionDuplicateIDIsNotARace: a repeated version id must not be
// reported as ErrVersionConflict. The primary key raises the same class of
// SQLite error as the (artifact_id, version_no) constraint, and telling a
// caller to "roll back and retry" a duplicate id would send it into a loop that
// can never succeed — the id is the caller's own and will still exist on the
// retry.
func TestCreateVersionDuplicateIDIsNotARace(t *testing.T) {
	f := newFixture(t)
	hash, size := f.put(t, "duplicate id\n")

	f.createVersion(t, VersionInput{
		ID: "v-1", ArtifactID: "art-1", ProjectID: f.projID,
		ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
	})

	err := f.store.WithTx(f.ctx, func(ctx context.Context, tx runstore.Tx) error {
		_, err := CreateVersionTx(ctx, tx, f.blobs, VersionInput{
			ID: "v-1", ArtifactID: "art-2", ProjectID: f.projID,
			ContentHash: hash, Size: size, CreatedBy: "user-1", CreatedAt: f.now,
		})
		return err
	})
	if err == nil {
		t.Fatal("a duplicate version id was accepted")
	}
	if errors.Is(err, ErrVersionConflict) {
		t.Errorf("duplicate id reported as ErrVersionConflict (%v); it is not retryable", err)
	}
}
