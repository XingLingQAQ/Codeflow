package artifact

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/runstore"
)

// Version errors.
var (
	// ErrInvalidVersion means the caller handed CreateVersionTx a version the
	// contract rejects: a blank id, artifact id, project or actor; a negative
	// size; a content hash that is not a content hash. Like runstore's
	// ErrInvalidRecord these are caught before any SQL runs, so a rejected call
	// leaves the caller's transaction exactly as it was.
	ErrInvalidVersion = errors.New("artifact: invalid version")

	// ErrBlobMissing means the version's content hash has no blob on disk (or
	// the blob's size does not match the version's). Nothing is written: this
	// is the check that keeps a visible version from pointing at content that
	// does not exist (§27.8). Write the blob first (BlobStore.Put), then the
	// row.
	ErrBlobMissing = errors.New("artifact: blob is missing")

	// ErrArtifactProjectMismatch means an artifact_id that already has versions
	// in one project was reused in another. An artifact belongs to exactly one
	// project, and letting the same id appear under two would make every
	// version listing and later retention decision ambiguous about which
	// project owns the content.
	ErrArtifactProjectMismatch = errors.New("artifact: artifact belongs to another project")

	// ErrRunProjectMismatch means a version named a run (or attempt) that
	// belongs to another project, or an attempt that belongs to another run.
	// The database cannot express this: runs has no UNIQUE (id, project_id) for
	// a composite foreign key (see migration 008), so the store checks it here.
	ErrRunProjectMismatch = errors.New("artifact: run belongs to another project")

	// ErrParentVersionMismatch means parent_version_id names a version of a
	// different artifact. A version's parent is its predecessor in that
	// artifact's own history, not another artifact's version.
	ErrParentVersionMismatch = errors.New("artifact: parent version belongs to another artifact")

	// ErrVersionConflict means the version number was taken by a concurrent
	// writer between this transaction reading max(version_no) and inserting.
	// Roll the transaction back and retry; the retry reads the new maximum.
	// Under runstore's BEGIN IMMEDIATE transactions it is not expected (the
	// write lock serializes version allocation), but correctness must not
	// depend on the lock mode.
	ErrVersionConflict = errors.New("artifact: version number allocated concurrently")
)

// VersionInput is what a caller must supply to create a version. The id is the
// caller's — like every other id in the runtime library, it is an opaque string
// owned by the caller, so a retry can reuse the same id and the uniqueness of
// (artifact_id, version_no) is the database's job rather than a generated
// identity's.
type VersionInput struct {
	// ID is the row identity.
	ID string
	// ArtifactID is the artifact this version belongs to. Version numbers are
	// per artifact.
	ArtifactID string
	// ProjectID is the owning project. It must agree with every other version of
	// the same artifact and with RunID when one is given.
	ProjectID string
	// ContentHash is the blob address, "sha256:<64 lowercase hex>". The blob
	// must already exist in the BlobStore handed to CreateVersionTx.
	ContentHash string
	// Size is the blob size in bytes. It must equal what BlobStore.Stat returns.
	Size int64
	// CreatedBy is the actor identity (user, agent revision, system).
	CreatedBy string
	// CreatedAt is when the version was created. Zero means "now" is not
	// substituted: the caller decides, so a replay produces the same row.
	CreatedAt time.Time
	// StageID, RunID and AttemptID are optional context. RunID and AttemptID
	// are a pair with a rule: an attempt is always under a run, so AttemptID
	// without RunID is refused, and both must belong to ProjectID.
	StageID   string
	RunID     string
	AttemptID string
	// ParentVersionID optionally names the version this one supersedes; it must
	// belong to the same artifact.
	ParentVersionID string
}

// Version is one stored artifact version.
type Version struct {
	ID              string
	ArtifactID      string
	ProjectID       string
	VersionNo       int64
	ContentHash     string
	Size            int64
	ContentRef      string
	CreatedBy       string
	CreatedAt       time.Time
	StageID         string
	RunID           string
	AttemptID       string
	ParentVersionID string
}

// ContentRefBlobPrefix is the scheme content_ref uses. The ref is stored rather
// than re-derived on read so a future store (T12.01's migrated domains) can
// address content differently without a reader guessing.
const ContentRefBlobPrefix = "blob:"

// CreateVersionTx writes one new version of an artifact and returns it with the
// version number the store allocated.
//
// The order this function exists to enforce (§27.8, and the package comment):
// the blob must already be persisted — created by BlobStore.Put before the
// caller opened this transaction — and this call only makes it *visible* under
// an artifact version. A crash between the two steps leaves an unreferenced
// blob, which is harmless and collectable; the reverse (a committed row whose
// blob is missing) is refused here: ErrBlobMissing is returned and no row is
// written.
//
// Allocation: version_no is max(version_no)+1 for this artifact_id, read and
// inserted in the caller's transaction. Under runstore's BEGIN IMMEDIATE that
// is serialized against other writers; the UNIQUE (artifact_id, version_no) in
// migration 008 is the backstop, and a losing writer gets ErrVersionConflict
// and should roll back and retry. Serializing here rather than "retry until it
// works" inside the function is deliberate: the caller owns the transaction, so
// it is also the only layer that can restart the surrounding work (a merge
// journal entry, a publish) that would otherwise be half-applied.
func CreateVersionTx(ctx context.Context, tx runstore.Tx, blobs *BlobStore, in VersionInput) (Version, error) {
	if tx == nil {
		return Version{}, fmt.Errorf("%w: nil transaction", ErrInvalidVersion)
	}
	if blobs == nil {
		// Without a blob store there is no way to prove the content exists, and
		// writing the row anyway is exactly the "visible version pointing at a
		// missing blob" the contract forbids.
		return Version{}, fmt.Errorf("%w: nil BlobStore", ErrInvalidVersion)
	}
	if strings.TrimSpace(in.ID) == "" {
		return Version{}, fmt.Errorf("%w: ID is required", ErrInvalidVersion)
	}
	if strings.TrimSpace(in.ArtifactID) == "" {
		return Version{}, fmt.Errorf("%w: ArtifactID is required", ErrInvalidVersion)
	}
	if strings.TrimSpace(in.ProjectID) == "" {
		return Version{}, fmt.Errorf("%w: ProjectID is required", ErrInvalidVersion)
	}
	if strings.TrimSpace(in.CreatedBy) == "" {
		return Version{}, fmt.Errorf("%w: CreatedBy is required", ErrInvalidVersion)
	}
	if !ValidHash(in.ContentHash) {
		return Version{}, fmt.Errorf("%w: content hash %q is not %s<64 lowercase hex>",
			ErrInvalidVersion, in.ContentHash, HashPrefix)
	}
	if in.Size < 0 {
		return Version{}, fmt.Errorf("%w: size %d is negative", ErrInvalidVersion, in.Size)
	}
	if in.AttemptID != "" && in.RunID == "" {
		return Version{}, fmt.Errorf("%w: AttemptID needs RunID", ErrInvalidVersion)
	}
	if in.CreatedAt.IsZero() {
		return Version{}, fmt.Errorf("%w: CreatedAt is required", ErrInvalidVersion)
	}

	// The blob must exist and be the size the caller claims. Checking both
	// catches the two ways a version can point at wrong content: nothing on
	// disk, or a blob that was replaced by a different one (its name would
	// change, but a size mismatch is the cheap signal for a truncated file).
	size, err := blobs.Stat(in.ContentHash)
	if err != nil {
		if errors.Is(err, ErrBlobNotFound) {
			return Version{}, fmt.Errorf("%w: %s", ErrBlobMissing, in.ContentHash)
		}
		return Version{}, err
	}
	if size != in.Size {
		return Version{}, fmt.Errorf("%w: %s is %d bytes, version says %d",
			ErrBlobMissing, in.ContentHash, size, in.Size)
	}

	// The artifact's project is fixed by its first version. A reused artifact_id
	// under another project would make ownership ambiguous everywhere the
	// artifact is read.
	var otherProject string
	err = tx.QueryRowContext(ctx,
		`SELECT project_id FROM artifact_versions WHERE artifact_id = ? LIMIT 1`,
		in.ArtifactID).Scan(&otherProject)
	switch {
	case err == nil && otherProject != in.ProjectID:
		return Version{}, fmt.Errorf("%w: artifact %s already has versions in project %s",
			ErrArtifactProjectMismatch, in.ArtifactID, otherProject)
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return Version{}, fmt.Errorf("artifact: read existing versions of %s: %w", in.ArtifactID, err)
	}

	if in.ParentVersionID != "" {
		var parentArtifact string
		err := tx.QueryRowContext(ctx,
			`SELECT artifact_id FROM artifact_versions WHERE id = ?`, in.ParentVersionID).Scan(&parentArtifact)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return Version{}, fmt.Errorf("%w: no version %s", ErrParentVersionMismatch, in.ParentVersionID)
		case err != nil:
			return Version{}, fmt.Errorf("artifact: read parent version %s: %w", in.ParentVersionID, err)
		case parentArtifact != in.ArtifactID:
			return Version{}, fmt.Errorf("%w: version %s belongs to artifact %s",
				ErrParentVersionMismatch, in.ParentVersionID, parentArtifact)
		}
	}

	if in.RunID != "" {
		if err := checkRunAndAttempt(ctx, tx, in.ProjectID, in.RunID, in.AttemptID); err != nil {
			return Version{}, err
		}
	}

	versionNo, err := nextVersionNoTx(ctx, tx, in.ArtifactID)
	if err != nil {
		return Version{}, err
	}

	contentRef := ContentRefBlobPrefix + in.ContentHash
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifact_versions (
			id, artifact_id, project_id, version_no, content_hash, size, content_ref,
			created_by, created_at, stage_id, run_id, attempt_id, parent_version_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.ID, in.ArtifactID, in.ProjectID, versionNo, in.ContentHash, in.Size, contentRef,
		in.CreatedBy, in.CreatedAt.UTC().UnixMilli(),
		nullableString(in.StageID), nullableString(in.RunID), nullableString(in.AttemptID),
		nullableString(in.ParentVersionID),
	); err != nil {
		if isUniqueViolation(err) {
			// (artifact_id, version_no) was taken between the max() read and
			// the insert. The caller must roll back and retry; see
			// ErrVersionConflict.
			return Version{}, fmt.Errorf("%w: artifact %s version %d", ErrVersionConflict, in.ArtifactID, versionNo)
		}
		return Version{}, fmt.Errorf("artifact: insert version %s: %w", in.ID, err)
	}

	return Version{
		ID:              in.ID,
		ArtifactID:      in.ArtifactID,
		ProjectID:       in.ProjectID,
		VersionNo:       versionNo,
		ContentHash:     in.ContentHash,
		Size:            in.Size,
		ContentRef:      contentRef,
		CreatedBy:       in.CreatedBy,
		CreatedAt:       in.CreatedAt.UTC(),
		StageID:         in.StageID,
		RunID:           in.RunID,
		AttemptID:       in.AttemptID,
		ParentVersionID: in.ParentVersionID,
	}, nil
}

// nextVersionNoTx allocates the next version number of one artifact inside the
// caller's transaction.
func nextVersionNoTx(ctx context.Context, tx runstore.Tx, artifactID string) (int64, error) {
	var next int64
	err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version_no), 0) + 1 FROM artifact_versions WHERE artifact_id = ?`,
		artifactID).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("artifact: read max version of %s: %w", artifactID, err)
	}
	if next < 1 {
		// Unreachable with the schema's CHECK and the COALESCE above, but a
		// version number of 0 would silently break the ">= 1" contract.
		return 0, fmt.Errorf("%w: artifact %s allocated version %d", ErrInvalidVersion, artifactID, next)
	}
	return next, nil
}

// checkRunAndAttempt verifies that a named run belongs to projectID and that a
// named attempt belongs to that run. Migration 008 explains why the database
// cannot: runs has no UNIQUE (id, project_id) for a composite foreign key.
func checkRunAndAttempt(ctx context.Context, tx runstore.Tx, projectID, runID, attemptID string) error {
	var runProject string
	err := tx.QueryRowContext(ctx, `SELECT project_id FROM runs WHERE id = ?`, runID).Scan(&runProject)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: no run %s", ErrRunProjectMismatch, runID)
	case err != nil:
		return fmt.Errorf("artifact: read run %s: %w", runID, err)
	case runProject != projectID:
		return fmt.Errorf("%w: run %s is in project %s", ErrRunProjectMismatch, runID, runProject)
	}
	if attemptID == "" {
		return nil
	}
	var attemptRun string
	err = tx.QueryRowContext(ctx, `SELECT run_id FROM attempts WHERE id = ?`, attemptID).Scan(&attemptRun)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: no attempt %s", ErrRunProjectMismatch, attemptID)
	case err != nil:
		return fmt.Errorf("artifact: read attempt %s: %w", attemptID, err)
	case attemptRun != runID:
		return fmt.Errorf("%w: attempt %s belongs to run %s", ErrRunProjectMismatch, attemptID, attemptRun)
	}
	return nil
}

// GetVersion reads one version by id.
//
// It returns runstore.ErrNotFound when there is no such row, so callers branch
// on one sentinel for "absent" across the runtime library.
func GetVersion(ctx context.Context, q runstore.Querier, id string) (Version, error) {
	if strings.TrimSpace(id) == "" {
		return Version{}, fmt.Errorf("%w: id is required", ErrInvalidVersion)
	}
	row := q.QueryRowContext(ctx, selectVersionColumns+` WHERE id = ?`, id)
	v, err := scanVersion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, fmt.Errorf("%w: artifact version %s", runstore.ErrNotFound, id)
	}
	if err != nil {
		return Version{}, fmt.Errorf("artifact: get version %s: %w", id, err)
	}
	return v, nil
}

// ListVersions returns every version of one artifact, oldest first.
//
// The order is by version_no, not by created_at: the number is the artifact's
// own history and two versions created in the same millisecond must still come
// back in the order they were allocated. A version whose artifact does not
// exist is an empty list, not an error — "this artifact has no versions yet" is
// a legitimate state for a caller that is about to create the first one.
func ListVersions(ctx context.Context, q runstore.Querier, artifactID string) ([]Version, error) {
	if strings.TrimSpace(artifactID) == "" {
		return nil, fmt.Errorf("%w: artifact id is required", ErrInvalidVersion)
	}
	rows, err := q.QueryContext(ctx, selectVersionColumns+
		` WHERE artifact_id = ? ORDER BY version_no ASC`, artifactID)
	if err != nil {
		return nil, fmt.Errorf("artifact: list versions of %s: %w", artifactID, err)
	}
	defer rows.Close()

	var out []Version
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, fmt.Errorf("artifact: scan version of %s: %w", artifactID, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("artifact: iterate versions of %s: %w", artifactID, err)
	}
	return out, nil
}

// LatestVersion returns the highest-numbered version of one artifact. It
// returns runstore.ErrNotFound when the artifact has no versions.
func LatestVersion(ctx context.Context, q runstore.Querier, artifactID string) (Version, error) {
	if strings.TrimSpace(artifactID) == "" {
		return Version{}, fmt.Errorf("%w: artifact id is required", ErrInvalidVersion)
	}
	row := q.QueryRowContext(ctx, selectVersionColumns+
		` WHERE artifact_id = ? ORDER BY version_no DESC LIMIT 1`, artifactID)
	v, err := scanVersion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, fmt.Errorf("%w: no version of artifact %s", runstore.ErrNotFound, artifactID)
	}
	if err != nil {
		return Version{}, fmt.Errorf("artifact: latest version of %s: %w", artifactID, err)
	}
	return v, nil
}

// selectVersionColumns is the projection every read shares, so a column added
// by a later migration has one place to be added and cannot drift between the
// three readers.
const selectVersionColumns = `
	SELECT id, artifact_id, project_id, version_no, content_hash, size, content_ref,
	       created_by, created_at, stage_id, run_id, attempt_id, parent_version_id
	FROM artifact_versions`

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanVersion reads one projected row. Timestamps cross this boundary exactly
// once: the column is Unix milliseconds, the struct field is time in UTC.
func scanVersion(s scanner) (Version, error) {
	var (
		v         Version
		createdAt int64
		stageID   sql.NullString
		runID     sql.NullString
		attemptID sql.NullString
		parentID  sql.NullString
	)
	if err := s.Scan(
		&v.ID, &v.ArtifactID, &v.ProjectID, &v.VersionNo, &v.ContentHash, &v.Size, &v.ContentRef,
		&v.CreatedBy, &createdAt, &stageID, &runID, &attemptID, &parentID,
	); err != nil {
		return Version{}, err
	}
	v.CreatedAt = time.UnixMilli(createdAt).UTC()
	v.StageID = stageID.String
	v.RunID = runID.String
	v.AttemptID = attemptID.String
	v.ParentVersionID = parentID.String
	return v, nil
}

// nullableString maps "" to SQL NULL, as runstore does for its own optional
// columns.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// isUniqueViolation reports whether err is a duplicate (artifact_id,
// version_no).
//
// It matches the composite column list, not "UNIQUE constraint failed" in
// general: the same class of error is also raised by the primary key, and
// reporting a duplicate id as a version race would tell the caller to retry
// something that can never succeed. The driver (modernc.org/sqlite) spells the
// columns out in the message as
// "UNIQUE constraint failed: artifact_versions.artifact_id, artifact_versions.version_no"
// (verified against the pinned engine, see runstore/schema_test.go's version
// assertion); matching that text keeps this package free of a driver import,
// which is the same reason runstore classifies its refusals by message.
func isUniqueViolation(err error) bool {
	const columns = "UNIQUE constraint failed: artifact_versions.artifact_id, artifact_versions.version_no"
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), strings.ToUpper(columns))
}
