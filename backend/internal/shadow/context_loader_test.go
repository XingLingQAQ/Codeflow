package shadow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContextLoaderLoadContext(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "context_loader_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	createTestProjectStructure(t, tmpDir)

	loader := NewContextLoader(&ContextLoaderConfig{
		ProjectRoot:    tmpDir,
		ShadowRoot:     ".codeflow",
		MaxTokenBudget: 8000,
		CacheEnabled:   true,
	})

	result, err := loader.LoadContext("user authentication login")
	if err != nil {
		t.Fatalf("load context failed: %v", err)
	}

	if len(result.Contexts) == 0 {
		t.Fatal("expected at least 1 context")
	}
	if result.TotalTokens <= 0 {
		t.Fatal("expected positive total tokens")
	}
	if result.TotalTokens > 8000 {
		t.Fatalf("expected total tokens <= 8000, got %d", result.TotalTokens)
	}
	if result.BudgetRemaining < 0 {
		t.Fatal("expected non-negative budget remaining")
	}
}

func TestContextLoaderLoadContextBudget(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "context_loader_budget_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	createTestProjectStructure(t, tmpDir)

	loader := NewContextLoader(&ContextLoaderConfig{
		ProjectRoot:    tmpDir,
		ShadowRoot:     ".codeflow",
		MaxTokenBudget: 10,
		CacheEnabled:   false,
	})

	result, err := loader.LoadContext("user auth login")
	if err != nil {
		t.Fatalf("load context failed: %v", err)
	}

	if result.TotalTokens > 10 {
		t.Fatalf("expected total tokens <= 10, got %d", result.TotalTokens)
	}
}

func TestContextLoaderLoadContextNoMatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "context_loader_nomatch_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	createTestProjectStructure(t, tmpDir)

	loader := NewContextLoader(&ContextLoaderConfig{
		ProjectRoot:    tmpDir,
		ShadowRoot:     ".codeflow",
		MaxTokenBudget: 8000,
	})

	result, err := loader.LoadContext("zzzzzzzzz")
	if err != nil {
		t.Fatalf("load context failed: %v", err)
	}

	if len(result.Contexts) != 0 {
		t.Fatalf("expected 0 contexts, got %d", len(result.Contexts))
	}
	if result.TotalTokens != 0 {
		t.Fatalf("expected 0 total tokens, got %d", result.TotalTokens)
	}
}

func TestContextLoaderLoadWithDependencies(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "context_loader_deps_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// 创建源文件
	srcDir := filepath.Join(tmpDir, "src", "auth")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("create src dir: %v", err)
	}

	sourceFile := filepath.Join(srcDir, "login.ts")
	if err := os.WriteFile(sourceFile, []byte("import { validate } from './validator.js';\n\nexport function login() {}\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	// 创建对应的意图文档
	shadowDomain := filepath.Join(tmpDir, ".codeflow", "domain", "src", "auth")
	if err := os.MkdirAll(shadowDomain, 0o755); err != nil {
		t.Fatalf("create shadow domain dir: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(shadowDomain, "login.intent.md"),
		[]byte("# Login\n\nAuthentication login handler."),
		0o644,
	); err != nil {
		t.Fatalf("write intent doc: %v", err)
	}

	loader := NewContextLoader(&ContextLoaderConfig{
		ProjectRoot:    tmpDir,
		ShadowRoot:     ".codeflow",
		MaxTokenBudget: 8000,
	})

	result, err := loader.LoadWithDependencies(sourceFile)
	if err != nil {
		t.Fatalf("load with dependencies failed: %v", err)
	}

	if len(result.Contexts) == 0 {
		t.Fatal("expected at least 1 context")
	}
}

func TestContextLoaderLoadWithDependenciesMissing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "context_loader_missing_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	loader := NewContextLoader(&ContextLoaderConfig{
		ProjectRoot:    tmpDir,
		ShadowRoot:     ".codeflow",
		MaxTokenBudget: 8000,
	})

	result, err := loader.LoadWithDependencies(filepath.Join(tmpDir, "src", "nonexistent.ts"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(result.Contexts) != 0 {
		t.Fatalf("expected 0 contexts, got %d", len(result.Contexts))
	}
}

func TestContextLoaderClearCache(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "context_loader_cache_test")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	createTestProjectStructure(t, tmpDir)

	loader := NewContextLoader(&ContextLoaderConfig{
		ProjectRoot:    tmpDir,
		ShadowRoot:     ".codeflow",
		MaxTokenBudget: 8000,
		CacheEnabled:   true,
	})

	// Load to populate cache
	loader.LoadContext("user auth")

	// Clear cache
	loader.ClearCache()

	// Load again should still work
	result, err := loader.LoadContext("user auth")
	if err != nil {
		t.Fatalf("load after cache clear failed: %v", err)
	}
	if len(result.Contexts) == 0 {
		t.Fatal("expected at least 1 context after cache clear")
	}
}

func createTestProjectStructure(t *testing.T, projectRoot string) {
	t.Helper()

	domainDir := filepath.Join(projectRoot, ".codeflow", "domain")
	authDir := filepath.Join(domainDir, "auth")
	orderDir := filepath.Join(domainDir, "order")

	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("create auth dir: %v", err)
	}
	if err := os.MkdirAll(orderDir, 0o755); err != nil {
		t.Fatalf("create order dir: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(authDir, "login.intent.md"),
		[]byte("# Login Intent\n\nHandles user authentication login flow.\nValidates credentials and returns JWT token.\nTags: user, auth, login, jwt"),
		0o644,
	); err != nil {
		t.Fatalf("write login intent: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(authDir, "register.intent.md"),
		[]byte("# Register Intent\n\nHandles user registration.\nCreates new user account with email validation.\nTags: user, auth, register"),
		0o644,
	); err != nil {
		t.Fatalf("write register intent: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(orderDir, "create.intent.md"),
		[]byte("# Create Order Intent\n\nHandles order creation.\nValidates cart items and processes payment.\nTags: order, create, payment"),
		0o644,
	); err != nil {
		t.Fatalf("write order intent: %v", err)
	}
}
