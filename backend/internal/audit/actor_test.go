package audit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestActorValidateAcceptsFourKinds(t *testing.T) {
	actors := []Actor{
		{Type: ActorTypeUser, ID: "user-1", Source: "http-session"},
		{Type: ActorTypeAgent, ID: "agent-1", Source: "runner"},
		{Type: ActorTypeSystem, ID: "recovery-1", Source: "recovery"},
		{Type: ActorTypeIntegration, ID: "github", Source: "webhook:github"},
	}
	for _, actor := range actors {
		if err := actor.Validate(); err != nil {
			t.Errorf("actor %+v should be valid: %v", actor, err)
		}
	}

	// source 可省略
	if err := (Actor{Type: ActorTypeSystem, ID: "sys"}).Validate(); err != nil {
		t.Errorf("actor without source should be valid: %v", err)
	}
}

func TestActorValidateRejectsInvalidType(t *testing.T) {
	for _, typ := range []ActorType{"", "service", "superuser", "AGENT", "User", "anonymous"} {
		err := (Actor{Type: typ, ID: "x"}).Validate()
		if !errors.Is(err, ErrInvalidActorType) {
			t.Errorf("type %q: expected ErrInvalidActorType, got %v", string(typ), err)
		}
	}
}

func TestActorValidateRejectsBadIDAndSource(t *testing.T) {
	// 空 id / 纯空白 id（冒充形状）
	for _, id := range []string{"", "   ", "\t\n"} {
		err := (Actor{Type: ActorTypeAgent, ID: id}).Validate()
		if !errors.Is(err, ErrEmptyActorID) {
			t.Errorf("id %q: expected ErrEmptyActorID, got %v", id, err)
		}
	}

	// id 超长
	err := (Actor{Type: ActorTypeUser, ID: strings.Repeat("a", MaxActorIDLength+1)}).Validate()
	if !errors.Is(err, ErrActorIDTooLong) {
		t.Errorf("expected ErrActorIDTooLong, got %v", err)
	}
	// 边界：恰好等于上限应通过
	if err := (Actor{Type: ActorTypeUser, ID: strings.Repeat("a", MaxActorIDLength)}).Validate(); err != nil {
		t.Errorf("id at max length should be valid: %v", err)
	}

	// source 超长
	err = (Actor{Type: ActorTypeUser, ID: "u", Source: strings.Repeat("s", MaxActorSourceLength+1)}).Validate()
	if !errors.Is(err, ErrActorSourceTooLong) {
		t.Errorf("expected ErrActorSourceTooLong, got %v", err)
	}
}

func TestActorContextRoundTrip(t *testing.T) {
	want := Actor{Type: ActorTypeUser, ID: "user-42", Source: "http-session"}

	ctx := WithActor(context.Background(), want)
	got, ok := ActorFromContext(ctx)
	if !ok {
		t.Fatal("expected actor present in context")
	}
	if got != want {
		t.Errorf("round trip mismatch: want %+v, got %+v", want, got)
	}

	// 未注入 / nil 上下文
	if _, ok := ActorFromContext(context.Background()); ok {
		t.Error("expected ok=false for context without actor")
	}
	if _, ok := ActorFromContext(nil); ok {
		t.Error("expected ok=false for nil context")
	}

	// WithActor 对 nil 上下文安全
	if _, ok := ActorFromContext(WithActor(nil, want)); !ok {
		t.Error("WithActor(nil, ...) should still store the actor")
	}
}

func TestResolveActorMissingIdentityPolicies(t *testing.T) {
	ctx := context.Background()

	// 分支一：显式降级为 system，绝不冒充 agent
	actor, err := ResolveActor(ctx, MissingActorDefaultSystem)
	if err != nil {
		t.Fatalf("default_system branch: %v", err)
	}
	if actor.Type != ActorTypeSystem || actor.ID == "" {
		t.Errorf("expected system actor with non-empty id, got %+v", actor)
	}
	if actor.Type == ActorTypeAgent {
		t.Error("missing identity must never default to agent")
	}
	if err := actor.Validate(); err != nil {
		t.Errorf("default system actor should itself validate: %v", err)
	}

	// 分支二：拒绝
	if _, err := ResolveActor(ctx, MissingActorReject); !errors.Is(err, ErrMissingActor) {
		t.Errorf("reject branch: expected ErrMissingActor, got %v", err)
	}

	// 未知策略不得静默放行
	if _, err := ResolveActor(ctx, MissingActorPolicy("bogus")); err == nil {
		t.Error("unknown policy must error, not silently allow")
	}
}

func TestResolveActorRejectsSpoofedActor(t *testing.T) {
	// 空 id 声称 agent 的冒充输入：两种缺身份策略下都必须拒绝，不得降级
	spoofed := WithActor(context.Background(), Actor{Type: ActorTypeAgent, ID: ""})
	for _, policy := range []MissingActorPolicy{MissingActorDefaultSystem, MissingActorReject} {
		if _, err := ResolveActor(spoofed, policy); !errors.Is(err, ErrEmptyActorID) {
			t.Errorf("policy %q: expected ErrEmptyActorID for spoofed actor, got %v", string(policy), err)
		}
	}

	// 非法 type 同样拒绝
	badType := WithActor(context.Background(), Actor{Type: "service", ID: "x"})
	if _, err := ResolveActor(badType, MissingActorDefaultSystem); !errors.Is(err, ErrInvalidActorType) {
		t.Errorf("expected ErrInvalidActorType, got %v", err)
	}

	// 合法身份正常解析
	valid := WithActor(context.Background(), Actor{Type: ActorTypeIntegration, ID: "github", Source: "webhook:github"})
	actor, err := ResolveActor(valid, MissingActorReject)
	if err != nil {
		t.Fatalf("valid actor should resolve: %v", err)
	}
	if actor.Type != ActorTypeIntegration || actor.ID != "github" {
		t.Errorf("unexpected resolved actor %+v", actor)
	}
}

func TestActorAuditActorCompatibility(t *testing.T) {
	actor := Actor{Type: ActorTypeUser, ID: "user-1", Source: "approval"}
	legacy := actor.AuditActor()
	if legacy.ID != actor.ID || legacy.Type != string(actor.Type) || legacy.Source != actor.Source {
		t.Errorf("AuditActor mapping mismatch: %+v", legacy)
	}

	// 兼容挂载：Source 为空时序列化不含 source 键，旧条目形状与哈希不受影响
	raw, err := json.Marshal(AuditActor{ID: "user-1", Type: "user"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "source") {
		t.Errorf("empty source must be omitted for backward compatibility: %s", raw)
	}

	// 旧格式（无 source 字段）可正常反序列化
	var decoded AuditActor
	if err := json.Unmarshal([]byte(`{"id":"user-1","type":"user"}`), &decoded); err != nil {
		t.Fatalf("unmarshal legacy shape: %v", err)
	}
	if decoded.ID != "user-1" || decoded.Source != "" {
		t.Errorf("legacy decode mismatch: %+v", decoded)
	}

	// 带 Source 的条目仍被 CalculateEntryHash 覆盖（字段参与序列化即可）
	entry := &AuditLogEntry{ID: "e1", Timestamp: 1, EventType: EventAccess, Severity: SeverityInfo,
		Actor: AuditActor{ID: "user-1", Type: "user", Source: "approval"}, Action: "approve", Outcome: OutcomeSuccess}
	entryNoSource := *entry
	entryNoSource.Actor.Source = ""
	if CalculateEntryHash(entry) == CalculateEntryHash(&entryNoSource) {
		t.Error("actor source should be covered by entry hash")
	}
}
