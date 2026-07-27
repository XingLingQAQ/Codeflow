// Package guard enforces write-time policy for workspace mutations.
// It implements workspace.WriteGuard so all FSService writes can be forced through here.
package guard

import (
	"context"
	"time"
)

// Severity is the rule outcome level.
type Severity string

const (
	SeverityError Severity = "error"
	SeverityWarn  Severity = "warn"
	SeverityOff   Severity = "off"
)

// RuleID identifies a built-in or config rule.
type RuleID string

const (
	RuleStackedNaming   RuleID = "stacked_naming"
	RuleDeniedPath      RuleID = "denied_path"
	RuleMaxFileBytes    RuleID = "max_file_bytes"
	RuleEmptyPath       RuleID = "empty_path"
	RuleBinaryExecWrite RuleID = "binary_exec_write"
	RuleDuplicateSymbol RuleID = "duplicate_symbol"
	RuleDeprecatedPath  RuleID = "deprecated_path"
)

// Exemption grants a temporary path-level bypass for selected rules.
type Exemption struct {
	Path      string    `json:"path"`
	Rules     []RuleID  `json:"rules,omitempty"` // empty = all rules
	Reason    string    `json:"reason,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

// RuleConfig is per-rule severity and parameters.
type RuleConfig struct {
	Severity Severity               `json:"severity" yaml:"severity"`
	Params   map[string]interface{} `json:"params,omitempty" yaml:"params,omitempty"`
}

// Config is the project/global guard configuration.
type Config struct {
	// Rules maps rule id → config. Missing rules use defaults.
	Rules map[RuleID]RuleConfig `json:"rules" yaml:"rules"`
	// DeniedPathGlobs are slash-style globs relative to project root (e.g. ".env", "**/*.key").
	DeniedPathGlobs []string `json:"denied_path_globs" yaml:"denied_path_globs"`
	// DeprecatedPathGlobs mark modules that should not be appended to; writes to a
	// matching path raise the deprecated_path rule (default warn, empty = disabled).
	DeprecatedPathGlobs []string `json:"deprecated_path_globs" yaml:"deprecated_path_globs"`
	// MaxFileBytes rejects writes larger than this when rule is error (0 = use default).
	MaxFileBytes int `json:"max_file_bytes" yaml:"max_file_bytes"`
}

// Violation is one rule hit.
type Violation struct {
	Rule     RuleID    `json:"rule"`
	Severity Severity  `json:"severity"`
	Message  string    `json:"message"`
	Path     string    `json:"path,omitempty"`
	At       time.Time `json:"at"`
}

// Decision is the aggregate BeforeWrite result.
type Decision struct {
	Allowed    bool        `json:"allowed"`
	Violations []Violation `json:"violations,omitempty"`
}

// Auditor receives denied/warned write attempts (optional; audit package may wrap).
type Auditor interface {
	RecordGuardDecision(ctx context.Context, absPath string, decision Decision) error
	RecordGuardEvent(ctx context.Context, action string, details map[string]interface{}) error
}

// RequestStatus tracks exemption-request lifecycle.
type RequestStatus string

const (
	RequestPending  RequestStatus = "pending"
	RequestApproved RequestStatus = "approved"
	RequestRejected RequestStatus = "rejected"
)

// ExemptionRequest is an agent's explicit ask for a temporary rule bypass.
// It must be approved by a human (or privileged caller) before the exemption
// becomes active.
type ExemptionRequest struct {
	ID           string        `json:"id"`
	Path         string        `json:"path"`
	RuleID       RuleID        `json:"rule_id,omitempty"`
	Reason       string        `json:"reason"`
	Requester    string        `json:"requester"`
	Status       RequestStatus `json:"status"`
	DecidedBy    string        `json:"decided_by,omitempty"`
	DecideReason string        `json:"decide_reason,omitempty"`
	TTL          time.Duration `json:"ttl"`
	CreatedAt    time.Time     `json:"created_at"`
	DecidedAt    time.Time     `json:"decided_at,omitempty"`
}

// Service evaluates write policy.
type Service interface {
	// BeforeWrite implements workspace.WriteGuard.
	BeforeWrite(ctx context.Context, absPath string, content []byte) error
	// Evaluate returns the full decision without short-circuiting on first error.
	Evaluate(ctx context.Context, absPath string, content []byte) Decision
	// Config returns a copy of the active config.
	Config() Config
}

// Note: IndexTree / TryLoadConfigFile remain on *Engine (concrete) for admin APIs.
