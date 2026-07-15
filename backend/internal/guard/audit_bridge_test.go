package guard

import (
	"context"
	"testing"

	"github.com/codeflow/backend/internal/audit"
)

type memAudit struct {
	entries []*audit.AuditLogEntry
}

func (m *memAudit) Log(ctx context.Context, entry *audit.AuditLogEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func TestAuditBridgeRecordsDenial(t *testing.T) {
	log := &memAudit{}
	bridge := NewAuditBridge(log)
	e := NewEngine(nil, bridge)
	err := e.BeforeWrite(context.Background(), "proj/helpers2.ts", []byte("x"))
	if err == nil {
		t.Fatal("expected block")
	}
	if len(log.entries) != 1 {
		t.Fatalf("entries=%d", len(log.entries))
	}
	if log.entries[0].Outcome != audit.OutcomeFailure {
		t.Fatalf("outcome=%s", log.entries[0].Outcome)
	}
	if log.entries[0].Action != "guard.before_write" {
		t.Fatalf("action=%s", log.entries[0].Action)
	}
}
