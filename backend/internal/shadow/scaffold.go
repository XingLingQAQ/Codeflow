package shadow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config 影子系统配置。
type Config struct {
	Version          string `json:"version"`
	ProjectRoot      string `json:"projectRoot"`
	AutoSync         bool   `json:"autoSync"`
	IntentProjection struct {
		Enabled   bool     `json:"enabled"`
		Languages []string `json:"languages"`
	} `json:"intentProjection"`
	Registry struct {
		APIRegistry     bool `json:"apiRegistry"`
		ModelDictionary bool `json:"modelDictionary"`
	} `json:"registry"`
}

// ShadowScaffold 影子目录脚手架。
type ShadowScaffold struct{}

// NewShadowScaffold 创建脚手架实例。
func NewShadowScaffold() *ShadowScaffold {
	return &ShadowScaffold{}
}

// Initialize 初始化 .codeflow 目录结构与配置。
func (s *ShadowScaffold) Initialize(projectRoot string) error {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		return errors.New("project root is required")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve project root: %w", err)
	}

	shadowRoot := filepath.Join(absRoot, ".codeflow")
	dirs := []string{
		filepath.Join(shadowRoot, "domain"),
		filepath.Join(shadowRoot, "governance"),
		filepath.Join(shadowRoot, "registry"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create shadow dir %s: %w", dir, err)
		}
	}

	cfg := defaultConfig(absRoot)
	cfgPath := filepath.Join(shadowRoot, "config.json")
	cfgBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal shadow config: %w", err)
	}
	if err := os.WriteFile(cfgPath, append(cfgBytes, '\n'), 0o644); err != nil {
		return fmt.Errorf("write shadow config: %w", err)
	}

	if err := updateGitignore(absRoot); err != nil {
		return err
	}

	return nil
}

func defaultConfig(projectRoot string) Config {
	cfg := Config{
		Version:     "1.0.0",
		ProjectRoot: projectRoot,
		AutoSync:    true,
	}
	cfg.IntentProjection.Enabled = true
	cfg.IntentProjection.Languages = []string{"typescript", "javascript", "go", "python"}
	cfg.Registry.APIRegistry = true
	cfg.Registry.ModelDictionary = true
	return cfg
}

func updateGitignore(projectRoot string) error {
	gitignorePath := filepath.Join(projectRoot, ".gitignore")
	content, err := os.ReadFile(gitignorePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read .gitignore: %w", err)
	}

	lines := splitLines(string(content))
	filtered := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if trimmed == ".codeflow" || trimmed == ".codeflow/" {
			continue
		}
		filtered = append(filtered, trimmed)
	}

	marker := "# CodeFlow shadow system (tracked)"
	if !containsLine(filtered, marker) {
		filtered = append(filtered, marker)
	}

	next := strings.Join(filtered, "\n") + "\n"
	if err := os.WriteFile(gitignorePath, []byte(next), 0o644); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}
	return nil
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

func containsLine(lines []string, target string) bool {
	for _, line := range lines {
		if line == target {
			return true
		}
	}
	return false
}
