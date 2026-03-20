package audit

import "context"

// EnrichActorFromContext merges trace context into the provided actor.
func EnrichActorFromContext(ctx context.Context, actor AuditActor) AuditActor {
	trace := TraceFromContext(ctx)
	if trace != nil {
		if actor.SessionID == "" {
			actor.SessionID = trace.SessionID
		}
		if actor.ID == "" {
			switch {
			case trace.AgentID != "":
				actor.ID = trace.AgentID
				if actor.Type == "" {
					actor.Type = "agent"
				}
			case trace.SessionID != "":
				actor.ID = trace.SessionID
				if actor.Type == "" {
					actor.Type = "user"
				}
			case trace.RequestID != "":
				actor.ID = trace.RequestID
			}
		}
	}

	if actor.ID == "" {
		actor.ID = "anonymous"
	}
	if actor.Type == "" {
		actor.Type = "service"
	}

	return actor
}

// Record writes an audit entry through the global audit service when configured.
func Record(ctx context.Context, entry *AuditLogEntry) (string, error) {
	if entry == nil {
		return "", nil
	}

	svc := GetAuditService()
	if svc == nil {
		return "", nil
	}

	entry.Actor = EnrichActorFromContext(ctx, entry.Actor)
	if err := svc.Log(ctx, entry); err != nil {
		return "", err
	}

	return entry.ID, nil
}
