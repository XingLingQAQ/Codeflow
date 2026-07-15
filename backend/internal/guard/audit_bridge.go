package guard

import (
	"context"
	"path/filepath"

	"github.com/codeflow/backend/internal/audit"
)

// AuditLogger is the minimal audit surface used by guard (avoids hard dependency on concrete service in tests).
type AuditLogger interface {
	Log(ctx context.Context, entry *audit.AuditLogEntry) error
}

// AuditBridge records guard decisions into the audit log chain.
type AuditBridge struct {
	log AuditLogger
}

// NewAuditBridge wraps an audit logger. log may be nil (no-op).
func NewAuditBridge(log AuditLogger) *AuditBridge {
	return &AuditBridge{log: log}
}

// RecordGuardDecision implements Auditor.
func (b *AuditBridge) RecordGuardDecision(ctx context.Context, absPath string, decision Decision) error {
	if b == nil || b.log == nil {
		return nil
	}
	outcome := audit.OutcomeSuccess
	sev := audit.SeverityInfo
	if !decision.Allowed {
		outcome = audit.OutcomeFailure
		sev = audit.SeverityWarning
	}
	details := map[string]interface{}{
		"allowed": decision.Allowed,
		"path":    absPath,
	}
	if len(decision.Violations) > 0 {
		msgs := make([]string, 0, len(decision.Violations))
		for _, v := range decision.Violations {
			msgs = append(msgs, string(v.Rule)+": "+v.Message)
		}
		details["violations"] = msgs
	}
	return b.log.Log(ctx, &audit.AuditLogEntry{
		EventType: audit.EventSecurity,
		Severity:  sev,
		Actor:     audit.AuditActor{Type: "system", ID: "guard"},
		Resource: audit.AuditResource{
			Type: "file",
			ID:   filepath.Base(absPath),
			Name: absPath,
		},
		Action:  "guard.before_write",
		Outcome: outcome,
		Details: details,
	})
}
