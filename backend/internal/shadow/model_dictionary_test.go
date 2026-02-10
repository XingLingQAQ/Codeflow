package shadow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModelDictionaryRegister(t *testing.T) {
	dict := NewModelDictionary(nil)

	entry := ModelEntry{
		Name: "User",
		Fields: []ModelField{
			{Name: "id", Type: "string", Required: true},
			{Name: "email", Type: "string", Required: true},
			{Name: "name", Type: "string", Required: false},
		},
		Source: "models/User.ts",
		Tags:   []string{"user", "auth"},
	}

	result := dict.Register(entry)
	if result.IsDuplicate {
		t.Fatal("expected no duplicate on first register")
	}
	if dict.GetModelCount() != 1 {
		t.Fatalf("expected 1 model, got %d", dict.GetModelCount())
	}
}

func TestModelDictionaryCheckDuplicateExactName(t *testing.T) {
	dict := NewModelDictionary(nil)

	entry1 := ModelEntry{
		Name: "User",
		Fields: []ModelField{
			{Name: "id", Type: "string", Required: true},
			{Name: "email", Type: "string", Required: true},
		},
		Source: "models/User.ts",
		Tags:   []string{"user"},
	}
	dict.Register(entry1)

	entry2 := ModelEntry{
		Name: "User",
		Fields: []ModelField{
			{Name: "id", Type: "number", Required: true},
			{Name: "username", Type: "string", Required: true},
		},
		Source: "dto/UserDTO.ts",
		Tags:   []string{"user", "dto"},
	}

	result := dict.CheckDuplicate(entry2)
	if !result.IsDuplicate {
		t.Fatal("expected duplicate for same name")
	}
	if len(result.SimilarModels) == 0 {
		t.Fatal("expected at least one similar model")
	}
	if result.SimilarModels[0].Similarity != 1.0 {
		t.Fatalf("expected similarity 1.0, got %f", result.SimilarModels[0].Similarity)
	}
}

func TestModelDictionaryCheckDuplicateStructural(t *testing.T) {
	dict := NewModelDictionary(&ModelDictionaryConfig{SimilarityThreshold: 0.3})

	entry1 := ModelEntry{
		Name: "UserEntity",
		Fields: []ModelField{
			{Name: "id", Type: "string", Required: true},
			{Name: "email", Type: "string", Required: true},
			{Name: "name", Type: "string", Required: false},
			{Name: "createdAt", Type: "number", Required: true},
		},
		Source: "entities/User.ts",
		Tags:   []string{"user", "entity"},
	}
	dict.Register(entry1)

	entry2 := ModelEntry{
		Name: "UserDTO",
		Fields: []ModelField{
			{Name: "id", Type: "string", Required: true},
			{Name: "email", Type: "string", Required: true},
			{Name: "name", Type: "string", Required: false},
		},
		Source: "dto/UserDTO.ts",
		Tags:   []string{"user", "dto"},
	}

	result := dict.CheckDuplicate(entry2)
	if !result.IsDuplicate {
		t.Fatal("expected structural duplicate")
	}
}

func TestModelDictionarySearch(t *testing.T) {
	dict := NewModelDictionary(nil)

	dict.Register(ModelEntry{
		Name:   "User",
		Fields: []ModelField{{Name: "id", Type: "string", Required: true}},
		Source:  "models/User.ts",
		Tags:   []string{"user", "auth"},
	})
	dict.Register(ModelEntry{
		Name:   "Order",
		Fields: []ModelField{{Name: "id", Type: "string", Required: true}},
		Source:  "models/Order.ts",
		Tags:   []string{"order", "commerce"},
	})

	results := dict.Search("user")
	if len(results) < 1 {
		t.Fatalf("expected at least 1 result, got %d", len(results))
	}
	if results[0].Name != "User" {
		t.Fatalf("expected first result to be User, got %s", results[0].Name)
	}
}

func TestModelDictionaryRecordRelationship(t *testing.T) {
	dict := NewModelDictionary(nil)

	dict.RecordRelationship("UserEntity", "UserDTO", "map")
	dict.RecordRelationship("OrderEntity", "OrderResponse", "subset")
	dict.RecordRelationship("UserEntity", "UserDTO", "map") // duplicate

	rels := dict.GetRelationships()
	if len(rels) != 2 {
		t.Fatalf("expected 2 relationships, got %d", len(rels))
	}
	if rels[0].Entity != "UserEntity" || rels[0].DTO != "UserDTO" || rels[0].Type != "map" {
		t.Fatalf("unexpected first relationship: %+v", rels[0])
	}
}

func TestModelDictionarySaveAndLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "model_dict_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dictPath := filepath.Join(tmpDir, ".codeflow", "registry", "models.yaml")

	d1 := NewModelDictionary(&ModelDictionaryConfig{DictionaryPath: dictPath})
	d1.Register(ModelEntry{
		Name: "User",
		Fields: []ModelField{
			{Name: "id", Type: "string", Required: true},
			{Name: "email", Type: "string", Required: true},
		},
		Source: "models/User.ts",
		Tags:   []string{"user"},
	})
	d1.RecordRelationship("User", "UserDTO", "map")

	if err := d1.SaveToYAML(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	d2 := NewModelDictionary(&ModelDictionaryConfig{DictionaryPath: dictPath})
	if err := d2.LoadFromYAML(); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if d2.GetModelCount() != 1 {
		t.Fatalf("expected 1 model, got %d", d2.GetModelCount())
	}
	models := d2.GetModels()
	if models[0].Name != "User" {
		t.Fatalf("unexpected model name: %s", models[0].Name)
	}
}

func TestModelDictionaryLoadMissingFile(t *testing.T) {
	d := NewModelDictionary(&ModelDictionaryConfig{DictionaryPath: "/nonexistent/path/models.yaml"})
	if err := d.LoadFromYAML(); err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if d.GetModelCount() != 0 {
		t.Fatalf("expected 0 models, got %d", d.GetModelCount())
	}
}
