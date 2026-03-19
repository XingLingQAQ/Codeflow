package audit

import "context"

type auditTraceContextKey struct{}

var traceContextKey auditTraceContextKey

// ContextWithTrace attaches audit trace metadata to context.
func ContextWithTrace(ctx context.Context, trace *AuditTrace) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if trace == nil {
		return ctx
	}
	traceCopy := *trace
	return context.WithValue(ctx, traceContextKey, &traceCopy)
}

// TraceFromContext extracts audit trace metadata from context.
func TraceFromContext(ctx context.Context) *AuditTrace {
	if ctx == nil {
		return nil
	}
	trace, ok := ctx.Value(traceContextKey).(*AuditTrace)
	if !ok || trace == nil {
		return nil
	}
	traceCopy := *trace
	return &traceCopy
}

// Record writes one audit entry through the global audit service.
func Record(ctx context.Context, entry *AuditLogEntry) (string, error) {
	svc := GetAuditService()
	if svc == nil {
		return "", context.Canceled
	}
	if entry == nil {
		return "", context.Canceled
	}
	if entry.Trace == nil {
		entry.Trace = TraceFromContext(ctx)
	}
	if entry.Trace != nil && entry.Actor.SessionID == "" {
		entry.Actor.SessionID = entry.Trace.SessionID
	}
	if err := svc.Log(ctx, entry); err != nil {
		return "", err
	}
	return entry.ID, nil
}
