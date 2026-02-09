package shadow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShadowScaffoldInitialize(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "shadow_scaffold_test")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	gitignorePath := filepath.Join(tmpDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("node_modules\n.codeflow\n"), 0o644); err != nil {
		t.Fatalf("write initial .gitignore failed: %v", err)
	}

	scaffold := NewShadowScaffold()
	if err := scaffold.Initialize(tmpDir); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	shadowRoot := filepath.Join(tmpDir, ".codeflow")
	mustDirExists(t, filepath.Join(shadowRoot, "domain"))
	mustDirExists(t, filepath.Join(shadowRoot, "governance"))
	mustDirExists(t, filepath.Join(shadowRoot, "registry"))

	configPath := filepath.Join(shadowRoot, "config.json")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.json failed: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(configBytes, &cfg); err != nil {
		t.Fatalf("unmarshal config failed: %v", err)
	}
	if cfg.Version != "1.0.0" {
		t.Fatalf("unexpected version: %s", cfg.Version)
	}
	if !cfg.AutoSync {
		t.Fatalf("expected autoSync=true")
	}
	if !cfg.IntentProjection.Enabled {
		t.Fatalf("expected intentProjection.enabled=true")
	}
	if len(cfg.IntentProjection.Languages) != 4 {
		t.Fatalf("unexpected intentProjection.languages: %+v", cfg.IntentProjection.Languages)
	}

	gitignoreBytes, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore failed: %v", err)
	}
	gitignore := string(gitignoreBytes)
	if strings.Contains(gitignore, "\n.codeflow\n") || strings.HasPrefix(gitignore, ".codeflow\n") {
		t.Fatalf("expected .codeflow ignore rules removed, got: %q", gitignore)
	}
	if !strings.Contains(gitignore, "# CodeFlow shadow system (tracked)") {
		t.Fatalf("expected tracked marker in .gitignore")
	}
}

func TestShadowScaffoldInitializeIdempotent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "shadow_scaffold_idempotent")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	scaffold := NewShadowScaffold()
	if err := scaffold.Initialize(tmpDir); err != nil {
		t.Fatalf("first initialize failed: %v", err)
	}
	if err := scaffold.Initialize(tmpDir); err != nil {
		t.Fatalf("second initialize failed: %v", err)
	}

	gitignorePath := filepath.Join(tmpDir, ".gitignore")
	gitignoreBytes, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("read .gitignore failed: %v", err)
	}
	gitignore := string(gitignoreBytes)
	if strings.Count(gitignore, "# CodeFlow shadow system (tracked)") != 1 {
		t.Fatalf("expected marker exactly once, got: %q", gitignore)
	}
}

func mustDirExists(t *testing.T, p string) {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat %s failed: %v", p, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %s to be dir", p)
	}
}
