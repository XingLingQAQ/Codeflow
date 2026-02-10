package shadow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAPIRegistryRegister(t *testing.T) {
	registry := NewAPIRegistry(nil)

	entry := APIRegistryEntry{
		Path:        "/api/v1/users",
		Method:      "GET",
		Description: "List all users",
		Handler:     "UserController.list",
		Tags:        []string{"user", "list"},
	}

	result := registry.Register(entry)
	if result.IsDuplicate {
		t.Fatal("expected no duplicate on first register")
	}
	if registry.GetEntryCount() != 1 {
		t.Fatalf("expected 1 entry, got %d", registry.GetEntryCount())
	}
}

func TestAPIRegistryCheckDuplicateExact(t *testing.T) {
	registry := NewAPIRegistry(nil)

	entry1 := APIRegistryEntry{
		Path:        "/api/v1/users",
		Method:      "GET",
		Description: "List all users",
		Handler:     "UserController.list",
		Tags:        []string{"user", "list"},
	}
	registry.Register(entry1)

	entry2 := APIRegistryEntry{
		Path:        "/api/v1/users",
		Method:      "GET",
		Description: "Get user list",
		Handler:     "UserHandler.getAll",
		Tags:        []string{"user"},
	}

	result := registry.CheckDuplicate(entry2)
	if !result.IsDuplicate {
		t.Fatal("expected duplicate for same path+method")
	}
	if len(result.SimilarEntries) == 0 {
		t.Fatal("expected at least one similar entry")
	}
	if result.SimilarEntries[0].Similarity != 1.0 {
		t.Fatalf("expected similarity 1.0, got %f", result.SimilarEntries[0].Similarity)
	}
}

func TestAPIRegistryCheckDuplicateSemantic(t *testing.T) {
	registry := NewAPIRegistry(&APIRegistryConfig{SimilarityThreshold: 0.4})

	entry1 := APIRegistryEntry{
		Path:        "/api/v1/users",
		Method:      "GET",
		Description: "List all users with pagination",
		Handler:     "UserController.list",
		Tags:        []string{"user", "list", "pagination"},
	}
	registry.Register(entry1)

	entry2 := APIRegistryEntry{
		Path:        "/api/v2/users/list",
		Method:      "GET",
		Description: "Get all users with pagination support",
		Handler:     "UserHandler.listAll",
		Tags:        []string{"user", "list", "pagination"},
	}

	result := registry.CheckDuplicate(entry2)
	if !result.IsDuplicate {
		t.Fatal("expected semantic duplicate")
	}
}

func TestAPIRegistrySearch(t *testing.T) {
	registry := NewAPIRegistry(nil)

	registry.Register(APIRegistryEntry{
		Path:        "/api/v1/users",
		Method:      "GET",
		Description: "List all users",
		Handler:     "UserController.list",
		Tags:        []string{"user", "list"},
	})
	registry.Register(APIRegistryEntry{
		Path:        "/api/v1/orders",
		Method:      "POST",
		Description: "Create a new order",
		Handler:     "OrderController.create",
		Tags:        []string{"order", "create"},
	})
	registry.Register(APIRegistryEntry{
		Path:        "/api/v1/users/:id",
		Method:      "GET",
		Description: "Get user by ID",
		Handler:     "UserController.getById",
		Tags:        []string{"user", "detail"},
	})

	results := registry.Search("user")
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
}

func TestAPIRegistrySearchNoMatch(t *testing.T) {
	registry := NewAPIRegistry(nil)

	registry.Register(APIRegistryEntry{
		Path:        "/api/v1/users",
		Method:      "GET",
		Description: "List all users",
		Handler:     "UserController.list",
		Tags:        []string{"user", "list"},
	})

	results := registry.Search("zzzzzzzzz")
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestAPIRegistrySaveAndLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "api_registry_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	registryPath := filepath.Join(tmpDir, ".codeflow", "registry", "apis.yaml")

	r1 := NewAPIRegistry(&APIRegistryConfig{RegistryPath: registryPath})
	r1.Register(APIRegistryEntry{
		Path:        "/api/v1/users",
		Method:      "GET",
		Description: "List all users",
		Handler:     "UserController.list",
		Tags:        []string{"user", "list"},
	})
	r1.Register(APIRegistryEntry{
		Path:        "/api/v1/orders",
		Method:      "POST",
		Description: "Create order",
		Handler:     "OrderController.create",
		Tags:        []string{"order"},
	})

	if err := r1.SaveToYAML(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	r2 := NewAPIRegistry(&APIRegistryConfig{RegistryPath: registryPath})
	if err := r2.LoadFromYAML(); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if r2.GetEntryCount() != 2 {
		t.Fatalf("expected 2 entries, got %d", r2.GetEntryCount())
	}

	entries := r2.GetEntries()
	if entries[0].Path != "/api/v1/users" {
		t.Fatalf("unexpected first entry path: %s", entries[0].Path)
	}
}

func TestAPIRegistryLoadMissingFile(t *testing.T) {
	r := NewAPIRegistry(&APIRegistryConfig{RegistryPath: "/nonexistent/path/apis.yaml"})
	if err := r.LoadFromYAML(); err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if r.GetEntryCount() != 0 {
		t.Fatalf("expected 0 entries, got %d", r.GetEntryCount())
	}
}
