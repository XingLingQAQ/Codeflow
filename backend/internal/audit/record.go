package audit

import (
	"context"
	"errors"
	"fmt"
)

type actorContextKey struct{}

var auditActorContextKey actorContextKey

// MissingActorPolicy 决定 ResolveActor 在上下文缺少操作者身份时的处理，
// 由调用入口显式选择；绝不存在"默认冒充 agent"的分支。
type MissingActorPolicy string

const (
	// MissingActorDefaultSystem 缺身份时降级为 DefaultSystemActor（Type=system），
	// 适用于系统恢复、后台任务等本就没有用户身份的入口。
	MissingActorDefaultSystem MissingActorPolicy = "default_system"
	// MissingActorReject 缺身份时返回 ErrMissingActor 拒绝记录，
	// 适用于用户操作、审批、webhook 等必须能归因到真实操作者的入口。
	MissingActorReject MissingActorPolicy = "reject"
)

const (
	// DefaultSystemActorID 缺身份降级时使用的 system 操作者 ID
	DefaultSystemActorID = "system"
	// ActorSourceAutoDefault 标识该身份是入口显式选择降级的结果，而非真实认证身份
	ActorSourceAutoDefault = "auto-default"
)

// ErrMissingActor 在 MissingActorReject 策略下上下文无操作者身份时返回。
var ErrMissingActor = errors.New("audit: actor missing from context")

// DefaultSystemActor 返回缺身份降级用的 system 操作者（合法、可校验、可审计地区分于真实用户/Agent）。
func DefaultSystemActor() Actor {
	return Actor{Type: ActorTypeSystem, ID: DefaultSystemActorID, Source: ActorSourceAutoDefault}
}

// WithActor 将操作者身份注入上下文，供下游审计记录提取。
// 注入方（认证中间件、审批流、恢复器、webhook 验签入口）对身份可信性负责；
// 本函数不校验，校验发生在提取侧 ResolveActor。
func WithActor(ctx context.Context, actor Actor) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, auditActorContextKey, actor)
}

// ActorFromContext 提取上下文中的操作者身份；ok=false 表示未注入。
// 注意：ok=true 仅表示存在，不代表已通过 Validate；需校验请用 ResolveActor。
func ActorFromContext(ctx context.Context) (Actor, bool) {
	if ctx == nil {
		return Actor{}, false
	}
	actor, ok := ctx.Value(auditActorContextKey).(Actor)
	return actor, ok
}

// ResolveActor 提取并校验上下文中的操作者身份：
//   - 已注入且合法：返回该身份；
//   - 已注入但非法（含空 id 声称 agent 等冒充形状）：无论策略如何都拒绝，绝不降级放行；
//   - 未注入：按 policy 二选一——MissingActorDefaultSystem 返回 DefaultSystemActor()，
//     MissingActorReject 返回 ErrMissingActor。
func ResolveActor(ctx context.Context, policy MissingActorPolicy) (Actor, error) {
	actor, ok := ActorFromContext(ctx)
	if !ok {
		switch policy {
		case MissingActorDefaultSystem:
			return DefaultSystemActor(), nil
		case MissingActorReject:
			return Actor{}, ErrMissingActor
		default:
			return Actor{}, fmt.Errorf("audit: unknown missing-actor policy %q", string(policy))
		}
	}
	if err := actor.Validate(); err != nil {
		return Actor{}, err
	}
	return actor, nil
}

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
