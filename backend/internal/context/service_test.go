package context

import (
	stdcontext "context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSQLiteContextServicePresetPersistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "context.db")

	svc, err := NewSQLiteContextService(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteContextService failed: %v", err)
	}
	defer svc.Close()

	req := &PresetCreateRequest{
		Name:        "frontend core",
		Description: "主前端上下文",
		Paths:       []string{"/workspace/apps/desktop", "/workspace/backend"},
		Extensions:  []string{"ts", "go"},
		MaxTokens:   4096,
	}

	created, err := svc.CreatePreset(stdcontext.Background(), req)
	if err != nil {
		t.Fatalf("CreatePreset failed: %v", err)
	}

	req.Paths[0] = "/mutated"
	req.Extensions[0] = "md"
	if created.Paths[0] != "/workspace/apps/desktop" {
		t.Fatalf("CreatePreset should clone paths, got %v", created.Paths)
	}
	if created.Extensions[0] != "ts" {
		t.Fatalf("CreatePreset should clone extensions, got %v", created.Extensions)
	}

	listed, err := svc.ListPresets(stdcontext.Background())
	if err != nil {
		t.Fatalf("ListPresets failed: %v", err)
	}
	if listed.Total != 1 || len(listed.Presets) != 1 {
		t.Fatalf("expected exactly one preset, got total=%d len=%d", listed.Total, len(listed.Presets))
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	svc2, err := NewSQLiteContextService(dbPath)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer svc2.Close()

	reloaded, err := svc2.ListPresets(stdcontext.Background())
	if err != nil {
		t.Fatalf("ListPresets after reopen failed: %v", err)
	}
	if reloaded.Total != 1 || len(reloaded.Presets) != 1 {
		t.Fatalf("expected one persisted preset, got total=%d len=%d", reloaded.Total, len(reloaded.Presets))
	}

	got := reloaded.Presets[0]
	if got.ID != created.ID {
		t.Fatalf("preset id mismatch: got %s want %s", got.ID, created.ID)
	}
	if got.Name != created.Name || got.Description != created.Description || got.MaxTokens != created.MaxTokens {
		t.Fatalf("preset metadata mismatch: got %+v want %+v", got, *created)
	}
	if !reflect.DeepEqual(got.Paths, []string{"/workspace/apps/desktop", "/workspace/backend"}) {
		t.Fatalf("preset paths mismatch: got %v", got.Paths)
	}
	if !reflect.DeepEqual(got.Extensions, []string{"ts", "go"}) {
		t.Fatalf("preset extensions mismatch: got %v", got.Extensions)
	}

	if err := svc2.DeletePreset(stdcontext.Background(), created.ID); err != nil {
		t.Fatalf("DeletePreset failed: %v", err)
	}

	empty, err := svc2.ListPresets(stdcontext.Background())
	if err != nil {
		t.Fatalf("ListPresets after delete failed: %v", err)
	}
	if empty.Total != 0 || len(empty.Presets) != 0 {
		t.Fatalf("expected empty preset list after delete, got total=%d len=%d", empty.Total, len(empty.Presets))
	}
}

func TestSQLiteContextServiceListPresetsStableOrder(t *testing.T) {
	svc, err := NewSQLiteContextService(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteContextService failed: %v", err)
	}
	defer svc.Close()

	_, err = svc.db.Exec(`
		INSERT INTO context_presets (id, name, description, paths_json, extensions_json, max_tokens, created_at, updated_at)
		VALUES
			('a', 'older-a', '', '["/a"]', '[]', 100, 100, 100),
			('b', 'older-b', '', '["/b"]', '["go"]', 100, 100, 100),
			('c', 'newer', '', '["/c"]', '["ts"]', 100, 200, 200)
	`)
	if err != nil {
		t.Fatalf("seed presets failed: %v", err)
	}

	listed, err := svc.ListPresets(stdcontext.Background())
	if err != nil {
		t.Fatalf("ListPresets failed: %v", err)
	}

	gotIDs := []string{listed.Presets[0].ID, listed.Presets[1].ID, listed.Presets[2].ID}
	wantIDs := []string{"c", "b", "a"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("stable order mismatch: got %v want %v", gotIDs, wantIDs)
	}
}

func TestGetContextServiceFallsBackToInMemory(t *testing.T) {
	original := defaultContextService
	defer SetContextService(original)

	SetContextService(nil)
	svc := GetContextService()
	if _, ok := svc.(*InMemoryContextService); !ok {
		t.Fatalf("expected default in-memory context service, got %T", svc)
	}
}
