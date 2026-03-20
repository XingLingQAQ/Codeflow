package audit

import "context"

type traceContextKey struct{}

var auditTraceContextKey traceContextKey

// ContextWithTrace stores audit trace information in context.
func ContextWithTrace(ctx context.Context, trace *AuditTrace) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if trace == nil {
		return ctx
	}
	copied := *trace
	return context.WithValue(ctx, auditTraceContextKey, &copied)
}

// TraceFromContext retrieves audit trace information from context.
func TraceFromContext(ctx context.Context) *AuditTrace {
	if ctx == nil {
		return nil
	}
	trace, ok := ctx.Value(auditTraceContextKey).(*AuditTrace)
	if !ok || trace == nil {
		return nil
	}
	copied := *trace
	return &copied
}
