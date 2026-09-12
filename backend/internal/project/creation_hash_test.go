package project

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/dbx"
	"github.com/codeflow/backend/internal/floweng"
)

func mustHash(t *testing.T, req *ProjectCreateRequest) string {
	t.Helper()
	hash, err := normalizedProjectCreateRequestHash(req)
	if err != nil {
		t.Fatalf("normalizedProjectCreateRequestHash failed: %v", err)
	}
	if len(hash) != 64 || strings.ToLower(hash) != hash {
		t.Fatalf("hash %q is not lowercase sha256 hex", hash)
	}
	if _, err := hex.DecodeString(hash); err != nil {
		t.Fatalf("hash %q is not hex: %v", hash, err)
	}
	return hash
}

func TestNormalizedProjectCreateRequestHashIgnoresCosmeticDifferences(t *testing.T) {
	var metadataA, metadataB map[string]interface{}
	if err := json.Unmarshal([]byte(`{"a":1,"b":{"x":true,"y":[1,2]},"c":"s"}`), &metadataA); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"c":"s","b":{"y":[1,2],"x":true},"a":1}`), &metadataB); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		a    *ProjectCreateRequest
		b    *ProjectCreateRequest
	}{
		{
			name: "surrounding whitespace",
			a:    &ProjectCreateRequest{Title: "  Alpha  ", Description: "desc\n", GitBranch: " main ", WorkspaceRoot: " /tmp/work "},
			b:    &ProjectCreateRequest{Title: "Alpha", Description: "desc", GitBranch: "main", WorkspaceRoot: "/tmp/work"},
		},
		{
			name: "empty status equals default planning",
			a:    &ProjectCreateRequest{Title: "Alpha"},
			b:    &ProjectCreateRequest{Title: "Alpha", Status: StatusPlanning},
		},
		{
			name: "tag order duplicates and blanks",
			a:    &ProjectCreateRequest{Title: "Alpha", Tags: []string{"b", "a", "b", " "}},
			b:    &ProjectCreateRequest{Title: "Alpha", Tags: []string{"a", "b"}},
		},
		{
			name: "metadata key order",
			a:    &ProjectCreateRequest{Title: "Alpha", Metadata: metadataA},
			b:    &ProjectCreateRequest{Title: "Alpha", Metadata: metadataB},
		},
		{
			name: "absent versus empty metadata and tags",
			a:    &ProjectCreateRequest{Title: "Alpha"},
			b:    &ProjectCreateRequest{Title: "Alpha", Tags: []string{}, Metadata: map[string]interface{}{}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hashA := mustHash(t, tc.a)
			hashB := mustHash(t, tc.b)
			if hashA != hashB {
				t.Fatalf("cosmetic difference changed hash: %s != %s", hashA, hashB)
			}
			if again := mustHash(t, tc.a); again != hashA {
				t.Fatalf("hash is not deterministic: %s != %s", hashA, again)
			}
		})
	}
}

func TestNormalizedProjectCreateRequestHashDetectsSemanticDifferences(t *testing.T) {
	base := func() *ProjectCreateRequest {
		return &ProjectCreateRequest{
			Title:         "Alpha",
			Description:   "desc",
			Status:        StatusPlanning,
			Tags:          []string{"a", "b"},
			GitBranch:     "main",
			WorkspaceRoot: "/tmp/work",
			Metadata:      map[string]interface{}{"owner": "qa", "n": float64(1)},
		}
	}
	cases := []struct {
		name   string
		mutate func(*ProjectCreateRequest)
	}{
		{"title", func(r *ProjectCreateRequest) { r.Title = "Beta" }},
		{"title case", func(r *ProjectCreateRequest) { r.Title = "alpha" }},
		{"description", func(r *ProjectCreateRequest) { r.Description = "other" }},
		{"status", func(r *ProjectCreateRequest) { r.Status = StatusActive }},
		{"tag set", func(r *ProjectCreateRequest) { r.Tags = []string{"a", "c"} }},
		{"git branch", func(r *ProjectCreateRequest) { r.GitBranch = "develop" }},
		{"workspace root", func(r *ProjectCreateRequest) { r.WorkspaceRoot = "/tmp/other" }},
		{"metadata value", func(r *ProjectCreateRequest) { r.Metadata = map[string]interface{}{"owner": "ops", "n": float64(1)} }},
		{"metadata key", func(r *ProjectCreateRequest) { r.Metadata = map[string]interface{}{"team": "qa", "n": float64(1)} }},
		{"metadata value type", func(r *ProjectCreateRequest) { r.Metadata = map[string]interface{}{"owner": "qa", "n": "1"} }},
	}
	want := mustHash(t, base())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := base()
			tc.mutate(mutated)
			if got := mustHash(t, mutated); got == want {
				t.Fatalf("semantic difference %q kept hash %s", tc.name, want)
			}
		})
	}
}

func TestNormalizedProjectCreateRequestHashRejectsUnencodableMetadata(t *testing.T) {
	_, err := normalizedProjectCreateRequestHash(&ProjectCreateRequest{
		Title:    "Alpha",
		Metadata: map[string]interface{}{"bad": func() {}},
	})
	if err == nil {
		t.Fatal("expected error for unencodable metadata")
	}
}

func TestCreateOperationPersistsNormalizedRequestHash(t *testing.T) {
	ctx := context.Background()
	resetIdempotentCreationCache()
	t.Cleanup(resetIdempotentCreationCache)
	dbPath := filepath.Join(t.TempDir(), "project.db")
	svc, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteProjectService failed: %v", err)
	}
	t.Cleanup(func() { svc.Close() })
	flows := floweng.NewInMemoryEngine(nil)

	req := &ProjectCreateRequest{
		Title:       " Alpha ",
		Description: "desc",
		Tags:        []string{"b", "a", "b"},
		Metadata:    map[string]interface{}{"owner": "qa"},
	}
	created, err := CreateProjectWithDefaultFlowIdempotent(ctx, svc, flows, req, "key-alpha")
	if err != nil {
		t.Fatal(err)
	}
	wantHash := mustHash(t, req)

	record, err := svc.GetCreateOperation(ctx, "key-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.NormalizedRequestHash != wantHash {
		t.Fatalf("stored hash=%q want %q", record.NormalizedRequestHash, wantHash)
	}
	var storedHash string
	if err := svc.db.QueryRow(`SELECT normalized_request_hash FROM project_operations WHERE idempotency_key = 'key-alpha'`).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash != wantHash {
		t.Fatalf("raw column hash=%q want %q", storedHash, wantHash)
	}

	again, err := CreateProjectWithDefaultFlowIdempotent(ctx, svc, flows, req, "key-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != created.ID || again.Flow.ID != created.Flow.ID {
		t.Fatalf("same key same payload created new resources: %+v vs %+v", again, created)
	}

	// A different payload under the same key is a deterministic typed
	// conflict (T0.11.b), in-process as well as after a reopen; the journaled
	// hash and intent are never rewritten by a rejected replay.
	conflictReq := &ProjectCreateRequest{Title: "Different payload"}
	assertCreateConflict := func(t *testing.T, svc *SQLiteProjectService, err error) {
		t.Helper()
		var conflictErr *ProjectCreateConflictError
		if !errors.As(err, &conflictErr) {
			t.Fatalf("different payload replay error=%v, want *ProjectCreateConflictError", err)
		}
		if !errors.Is(err, ErrProjectCreateConflict) {
			t.Fatalf("error %v does not unwrap to ErrProjectCreateConflict", err)
		}
		if conflictErr.IdempotencyKey != "key-alpha" || conflictErr.ProjectID != created.ID {
			t.Fatalf("conflict identity=%q/%q want key-alpha/%s", conflictErr.IdempotencyKey, conflictErr.ProjectID, created.ID)
		}
		if conflictErr.AcceptedHash != wantHash || conflictErr.RejectedHash != mustHash(t, conflictReq) {
			t.Fatalf("conflict hashes=%q/%q want %q/%q", conflictErr.AcceptedHash, conflictErr.RejectedHash, wantHash, mustHash(t, conflictReq))
		}
		if len(conflictErr.DifferingFields) != 1 || conflictErr.DifferingFields[0] != "title" {
			t.Fatalf("differing fields=%v want [title]", conflictErr.DifferingFields)
		}
		record, err := svc.GetCreateOperation(ctx, "key-alpha")
		if err != nil {
			t.Fatal(err)
		}
		if record.NormalizedRequestHash != wantHash || record.Title != "Alpha" {
			t.Fatalf("conflict rewrote intent: hash=%q title=%q", record.NormalizedRequestHash, record.Title)
		}
	}
	_, err = CreateProjectWithDefaultFlowIdempotent(ctx, svc, flows, conflictReq, "key-alpha")
	assertCreateConflict(t, svc, err)

	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	// After a reopen the in-process cache is bypassed, so the durable journal
	// path runs: the same payload replays the original operation, while a
	// different payload under the same key conflicts without rewriting the
	// stored hash or intent.
	svc2, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer svc2.Close()
	replayed, err := CreateProjectWithDefaultFlowIdempotent(ctx, svc2, flows, req, "key-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != created.ID || replayed.Title != "Alpha" || replayed.Flow.ID != created.Flow.ID {
		t.Fatalf("replay returned %+v, want project %s titled Alpha", replayed.Project, created.ID)
	}
	_, err = CreateProjectWithDefaultFlowIdempotent(ctx, svc2, flows, conflictReq, "key-alpha")
	assertCreateConflict(t, svc2, err)
}

func TestLegacyCreateOperationMigrationMarksUnknownHash(t *testing.T) {
	ctx := context.Background()
	resetIdempotentCreationCache()
	t.Cleanup(resetIdempotentCreationCache)
	dbPath := filepath.Join(t.TempDir(), "project.db")

	// Hand-build a database with the pre-hash project_operations schema.
	raw, err := dbx.Open(dbPath)
	if err != nil {
		t.Fatalf("open raw db failed: %v", err)
	}
	if _, err := raw.Exec(`
		CREATE TABLE project_operations (
			idempotency_key TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			flow_id TEXT,
			session_id TEXT,
			title TEXT NOT NULL DEFAULT '',
			workspace_root TEXT,
			operation TEXT NOT NULL,
			state TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);
		INSERT INTO project_operations (idempotency_key, project_id, flow_id, session_id, title, workspace_root, operation, state, created_at, updated_at)
		VALUES ('legacy-key', 'legacy-project', NULL, 'legacy-session', 'Legacy Title', NULL, 'create_project', 'session_created', 1000, 1000);
	`); err != nil {
		t.Fatalf("seed legacy db failed: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	svc, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	t.Cleanup(func() { svc.Close() })
	flows := floweng.NewInMemoryEngine(nil)

	legacy, err := svc.GetCreateOperation(ctx, "legacy-key")
	if err != nil {
		t.Fatal(err)
	}
	if legacy == nil || legacy.Title != "Legacy Title" || legacy.FlowID != "" || legacy.SessionID != "legacy-session" {
		t.Fatalf("legacy record unreadable after migration: %+v", legacy)
	}
	if legacy.NormalizedRequestHash != "" {
		t.Fatalf("legacy record hash=%q, want empty legacy marker", legacy.NormalizedRequestHash)
	}
	var hashColumn sql.NullString
	if err := svc.db.QueryRow(`SELECT normalized_request_hash FROM project_operations WHERE idempotency_key = 'legacy-key'`).Scan(&hashColumn); err != nil {
		t.Fatal(err)
	}
	if hashColumn.Valid {
		t.Fatalf("legacy hash column backfilled to %q", hashColumn.String)
	}

	// Replaying the legacy record with a different payload must complete the
	// saga from the stored intent without rewriting it or its empty hash.
	replayed, err := CreateProjectWithDefaultFlowIdempotent(ctx, svc, flows, &ProjectCreateRequest{Title: "Replacement Title"}, "legacy-key")
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != "legacy-project" || replayed.Title != "Legacy Title" {
		t.Fatalf("legacy replay returned %+v", replayed.Project)
	}
	legacyAfter, err := svc.GetCreateOperation(ctx, "legacy-key")
	if err != nil {
		t.Fatal(err)
	}
	if legacyAfter.Title != "Legacy Title" || legacyAfter.NormalizedRequestHash != "" || legacyAfter.State != createOperationCompleted || legacyAfter.FlowID == "" {
		t.Fatalf("legacy record after replay: %+v", legacyAfter)
	}
	if err := svc.db.QueryRow(`SELECT normalized_request_hash FROM project_operations WHERE idempotency_key = 'legacy-key'`).Scan(&hashColumn); err != nil {
		t.Fatal(err)
	}
	if hashColumn.Valid {
		t.Fatalf("legacy hash rewritten from new payload to %q", hashColumn.String)
	}

	// New operations under the migrated schema always persist a real hash.
	freshReq := &ProjectCreateRequest{Title: "Fresh", Tags: []string{"x"}}
	if _, err := CreateProjectWithDefaultFlowIdempotent(ctx, svc, flows, freshReq, "fresh-key"); err != nil {
		t.Fatal(err)
	}
	freshHash := mustHash(t, freshReq)
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopening re-runs the additive migration idempotently; both the legacy
	// marker and the fresh hash survive.
	svc2, err := NewSQLiteProjectService(dbPath)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer svc2.Close()
	legacyReopen, err := svc2.GetCreateOperation(ctx, "legacy-key")
	if err != nil {
		t.Fatal(err)
	}
	if legacyReopen == nil || legacyReopen.NormalizedRequestHash != "" || legacyReopen.Title != "Legacy Title" {
		t.Fatalf("legacy record after reopen: %+v", legacyReopen)
	}
	freshReopen, err := svc2.GetCreateOperation(ctx, "fresh-key")
	if err != nil {
		t.Fatal(err)
	}
	if freshReopen == nil || freshReopen.NormalizedRequestHash != freshHash {
		t.Fatalf("fresh record after reopen: %+v want hash %q", freshReopen, freshHash)
	}
}
