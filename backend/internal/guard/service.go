package guard

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const defaultMaxFileBytes = 2 * 1024 * 1024 // 2 MiB

// stackedNaming matches common "stacked rewrite" filenames: foo_v2, foo_new, utils2, xxx_copy.
var stackedNaming = regexp.MustCompile(`(?i)(?:_v\d+|_new|_copy|_bak|2)\.[^./\\]+$|^(?:utils|helpers|lib)2\.|[\\/](?:utils|helpers|lib)2\.`)

// Engine is the default guard implementation.
type Engine struct {
	mu         sync.RWMutex
	cfg        Config
	auditor    Auditor
	symbols    *SymbolIndex
	exemptions map[string]Exemption
	// exStore optionally persists exemptions across process restarts.
	exStore *sqliteExemptionStore
}

// NewEngine creates a guard engine with defaults merged over cfg.
// A SymbolIndex is always attached for duplicate-symbol detection.
func NewEngine(cfg *Config, auditor Auditor) *Engine {
	e := &Engine{
		auditor: auditor,
		cfg:     defaultConfig(),
		symbols: NewSymbolIndex(),
	}
	if cfg != nil {
		e.cfg = mergeConfig(e.cfg, *cfg)
	}
	return e
}

// SymbolIndex returns the engine's symbol index (for seeding from disk).
func (e *Engine) SymbolIndex() *SymbolIndex {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.symbols
}

func defaultConfig() Config {
	return Config{
		Rules: map[RuleID]RuleConfig{
			RuleStackedNaming:   {Severity: SeverityError},
			RuleDeniedPath:      {Severity: SeverityError},
			RuleMaxFileBytes:    {Severity: SeverityError},
			RuleEmptyPath:       {Severity: SeverityError},
			RuleBinaryExecWrite: {Severity: SeverityWarn},
				RuleDuplicateSymbol: {Severity: SeverityError},
		},
		DeniedPathGlobs: []string{
			".env",
			".env.*",
			"**/*.pem",
			"**/*.key",
			"**/id_rsa",
			"**/id_rsa.pub",
		},
		MaxFileBytes: defaultMaxFileBytes,
	}
}

func mergeConfig(base, over Config) Config {
	out := base
	if over.MaxFileBytes > 0 {
		out.MaxFileBytes = over.MaxFileBytes
	}
	if len(over.DeniedPathGlobs) > 0 {
		out.DeniedPathGlobs = append([]string(nil), over.DeniedPathGlobs...)
	}
	if out.Rules == nil {
		out.Rules = map[RuleID]RuleConfig{}
	}
	for k, v := range over.Rules {
		out.Rules[k] = v
	}
	return out
}

// Config returns a shallow copy of the active config.
func (e *Engine) Config() Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return mergeConfig(Config{}, e.cfg)
}

// SetAuditor sets the optional audit sink.
func (e *Engine) SetAuditor(a Auditor) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.auditor = a
}

// BeforeWrite implements workspace.WriteGuard.
func (e *Engine) BeforeWrite(ctx context.Context, absPath string, content []byte) error {
	dec := e.Evaluate(ctx, absPath, content)
	e.mu.RLock()
	auditor := e.auditor
	symbols := e.symbols
	e.mu.RUnlock()
	if auditor != nil {
		_ = auditor.RecordGuardDecision(ctx, absPath, dec)
	}
	if !dec.Allowed {
		for _, v := range dec.Violations {
			if v.Severity == SeverityError {
				return fmt.Errorf("%s: %s", v.Rule, v.Message)
			}
		}
		return fmt.Errorf("write denied by guard")
	}
	// Commit symbols only when write is allowed (index tracks accepted tree).
	if symbols != nil {
		symbols.Commit(ctx, absPath, content)
	}
	return nil
}

// Evaluate runs all enabled rules.
func (e *Engine) Evaluate(ctx context.Context, absPath string, content []byte) Decision {
	e.mu.RLock()
	cfg := e.cfg
	e.mu.RUnlock()

	now := time.Now().UTC()
	violations := make([]Violation, 0)

	base := filepath.Base(absPath)
	if severity(cfg, RuleEmptyPath) != SeverityOff && !e.isExempt(absPath, RuleEmptyPath) {
		if strings.TrimSpace(absPath) == "" || base == "." || base == string(filepath.Separator) {
			violations = append(violations, Violation{
				Rule: RuleEmptyPath, Severity: severity(cfg, RuleEmptyPath),
				Message: "empty or invalid path", Path: absPath, At: now,
			})
		}
	}

	if sev := severity(cfg, RuleStackedNaming); sev != SeverityOff && !e.isExempt(absPath, RuleStackedNaming) {
		if stackedNaming.MatchString(absPath) || stackedNaming.MatchString(base) {
			violations = append(violations, Violation{
				Rule: RuleStackedNaming, Severity: sev,
				Message: "stacked rewrite naming detected (e.g. _v2/_new/utils2); modify original or refactor explicitly",
				Path:    absPath, At: now,
			})
		}
	}

	if sev := severity(cfg, RuleDeniedPath); sev != SeverityOff && !e.isExempt(absPath, RuleDeniedPath) {
		rel := filepath.ToSlash(base)
		full := filepath.ToSlash(absPath)
		for _, g := range cfg.DeniedPathGlobs {
			if matchDenied(g, rel) || matchDenied(g, full) {
				violations = append(violations, Violation{
					Rule: RuleDeniedPath, Severity: sev,
					Message: fmt.Sprintf("path matches denied glob %q", g),
					Path:    absPath, At: now,
				})
				break
			}
		}
	}

	if sev := severity(cfg, RuleMaxFileBytes); sev != SeverityOff && !e.isExempt(absPath, RuleMaxFileBytes) {
		max := cfg.MaxFileBytes
		if max <= 0 {
			max = defaultMaxFileBytes
		}
		if len(content) > max {
			violations = append(violations, Violation{
				Rule: RuleMaxFileBytes, Severity: sev,
				Message: fmt.Sprintf("content size %d exceeds max_file_bytes %d", len(content), max),
				Path:    absPath, At: now,
			})
		}
	}

	if sev := severity(cfg, RuleBinaryExecWrite); sev != SeverityOff && !e.isExempt(absPath, RuleBinaryExecWrite) {
		ext := strings.ToLower(filepath.Ext(absPath))
		switch ext {
		case ".exe", ".dll", ".so", ".dylib", ".bat", ".cmd", ".ps1":
			violations = append(violations, Violation{
				Rule: RuleBinaryExecWrite, Severity: sev,
				Message: fmt.Sprintf("writing executable-like extension %s", ext),
				Path:    absPath, At: now,
			})
		}
	}

	if sev := severity(cfg, RuleDuplicateSymbol); sev != SeverityOff && !e.isExempt(absPath, RuleDuplicateSymbol) {
		e.mu.RLock()
		symbols := e.symbols
		e.mu.RUnlock()
		if symbols != nil {
			for _, loc := range symbols.CheckDuplicates(ctx, absPath, content) {
				violations = append(violations, Violation{
					Rule:     RuleDuplicateSymbol,
					Severity: sev,
					Message:  formatConflict(loc.Name, loc.Kind, loc),
					Path:     absPath,
					At:       now,
				})
			}
		}
	}

	allowed := true
	for _, v := range violations {
		if v.Severity == SeverityError {
			allowed = false
			break
		}
	}
	return Decision{Allowed: allowed, Violations: violations}
}

func severity(cfg Config, id RuleID) Severity {
	if cfg.Rules != nil {
		if r, ok := cfg.Rules[id]; ok && r.Severity != "" {
			return r.Severity
		}
	}
	return SeverityError
}

// matchDenied supports simple patterns: exact, prefix*, *.suffix, and **/name.
func matchDenied(glob, path string) bool {
	glob = strings.TrimSpace(glob)
	path = strings.TrimSpace(path)
	if glob == "" {
		return false
	}
	// **/foo or **/*.ext
	if strings.HasPrefix(glob, "**/") {
		rest := glob[3:]
		if strings.HasPrefix(rest, "*.") {
			return strings.HasSuffix(strings.ToLower(path), strings.ToLower(rest[1:]))
		}
		return strings.HasSuffix(path, "/"+rest) || path == rest || strings.HasSuffix(path, rest)
	}
	if strings.HasPrefix(glob, "*.") {
		return strings.HasSuffix(strings.ToLower(path), strings.ToLower(glob[1:]))
	}
	if strings.HasSuffix(glob, ".*") {
		prefix := glob[:len(glob)-2]
		base := path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			base = path[i+1:]
		}
		return strings.HasPrefix(base, prefix+".")
	}
	// exact base or full
	if path == glob {
		return true
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:] == glob
	}
	return false
}

// --- global ---

var (
	defaultGuard Service
	guardMu      sync.RWMutex
)

// GetService returns the process-wide guard.
func GetService() Service {
	guardMu.RLock()
	s := defaultGuard
	guardMu.RUnlock()
	if s != nil {
		return s
	}
	guardMu.Lock()
	defer guardMu.Unlock()
	if defaultGuard == nil {
		defaultGuard = NewEngine(nil, nil)
	}
	return defaultGuard
}

// SetService sets the process-wide guard.
func SetService(s Service) {
	guardMu.Lock()
	defer guardMu.Unlock()
	defaultGuard = s
}
