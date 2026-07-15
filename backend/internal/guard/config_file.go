package guard

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// fileConfig is the on-disk shape for .codeflow/guard.yaml.
type fileConfig struct {
	Rules map[string]struct {
		Severity string                 `yaml:"severity"`
		Params   map[string]interface{} `yaml:"params"`
	} `yaml:"rules"`
	DeniedPathGlobs []string `yaml:"denied_path_globs"`
	MaxFileBytes    int      `yaml:"max_file_bytes"`
}

// LoadConfigFile reads a guard YAML config. Missing file returns default config + nil error? No — returns error for missing.
func LoadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fc fileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parse guard yaml: %w", err)
	}
	cfg := defaultConfig()
	if fc.MaxFileBytes > 0 {
		cfg.MaxFileBytes = fc.MaxFileBytes
	}
	if len(fc.DeniedPathGlobs) > 0 {
		cfg.DeniedPathGlobs = append([]string(nil), fc.DeniedPathGlobs...)
	}
	if cfg.Rules == nil {
		cfg.Rules = map[RuleID]RuleConfig{}
	}
	for id, r := range fc.Rules {
		sev := Severity(r.Severity)
		if sev == "" {
			sev = SeverityError
		}
		cfg.Rules[RuleID(id)] = RuleConfig{Severity: sev, Params: r.Params}
	}
	return &cfg, nil
}

// ApplyConfig replaces engine config (keeps symbol index + auditor).
func (e *Engine) ApplyConfig(cfg Config) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = mergeConfig(defaultConfig(), cfg)
}

// TryLoadConfigFile loads YAML if present; ignores missing file.
func (e *Engine) TryLoadConfigFile(path string) error {
	cfg, err := LoadConfigFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	e.ApplyConfig(*cfg)
	return nil
}
