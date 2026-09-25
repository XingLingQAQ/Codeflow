package audit

import (
	"context"
	"testing"
)

// setupActorKindsFileService 安装真实文件审计存储（t.TempDir）为全局服务，
// 测试结束关闭存储并恢复全局状态。
func setupActorKindsFileService(t *testing.T) (*AuditService, *FileAuditStorage, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &FileStorageConfig{
		LogDir:          dir,
		FilePrefix:      "actor-kinds",
		MaxFileSize:     1 << 20,
		MaxFiles:        2,
		VerifyOnStartup: true,
		FlushInterval:   60000,
	}
	store, err := CreateFileAuditStorage(cfg)
	if err != nil {
		t.Fatalf("CreateFileAuditStorage: %v", err)
	}
	svc := NewAuditService(store)
	SetAuditService(svc)
	t.Cleanup(func() {
		_ = svc.Close()
		SetAuditService(nil)
	})
	return svc, store, dir
}

// TestAuditActorKinds 验证四种操作者枚举（user/agent/system/integration）经
// 入口注入（WithActor）后各自产生可区分的审计条目，且真实文件存储上的
// 哈希链可以复核（含重启后重开存储的启动校验）。
func TestAuditActorKinds(t *testing.T) {
	svc, _, dir := setupActorKindsFileService(t)
	ctx := context.Background()

	kinds := []Actor{
		{Type: ActorTypeUser, ID: "user-1", Source: "sidecar-token"},
		{Type: ActorTypeAgent, ID: "agent-1", Source: "runner"},
		{Type: ActorTypeSystem, ID: "recovery-1", Source: "recovery"},
		{Type: ActorTypeIntegration, ID: "github", Source: "webhook:github"},
	}

	entryIDs := make([]string, 0, len(kinds))
	for _, actor := range kinds {
		actor := actor
		t.Run(string(actor.Type), func(t *testing.T) {
			// 调用方留空 Actor：身份必须来自入口注入（EnrichActorFromContext 采信
			// 可信注入身份），与 middleware/policy 的消费路径同型。
			entryCtx := WithActor(ctx, actor)
			id, err := Record(entryCtx, &AuditLogEntry{
				EventType: EventSecurity,
				Severity:  SeverityInfo,
				Resource:  AuditResource{Type: "boundary", ID: "res-" + string(actor.Type)},
				Action:    "actor.kinds",
				Outcome:   OutcomeSuccess,
			})
			if err != nil {
				t.Fatalf("Record: %v", err)
			}
			entryIDs = append(entryIDs, id)

			// 存储级证据：按 ID 从服务重新取出，逐字段比对注入身份。
			stored, err := svc.GetByID(ctx, id)
			if err != nil {
				t.Fatalf("GetByID(%s): %v", id, err)
			}
			if stored.Actor.Type != string(actor.Type) || stored.Actor.ID != actor.ID || stored.Actor.Source != actor.Source {
				t.Fatalf("stored actor mismatch: want %s/%s/%s, got %+v",
					actor.Type, actor.ID, actor.Source, stored.Actor)
			}
		})
	}

	// 四枚举两两可区分：落盘的 type 集合恰好是四个不同枚举值。
	seen := make(map[string]string, len(kinds))
	for i, id := range entryIDs {
		stored, err := svc.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("GetByID(%s): %v", id, err)
		}
		if prevID, dup := seen[stored.Actor.Type]; dup {
			t.Fatalf("actor type %q not distinguishable (entries %s and %s)", stored.Actor.Type, prevID, id)
		}
		seen[stored.Actor.Type] = id
		if stored.Actor.Type != string(kinds[i].Type) {
			t.Fatalf("entry %s type: want %q, got %q", id, kinds[i].Type, stored.Actor.Type)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("expected 4 distinct actor types, got %d: %v", len(seen), seen)
	}

	// 含全部四类条目的链在真实文件存储上复核有效。
	result, err := svc.VerifyChain(ctx)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !result.Valid || result.CheckedEntries != len(kinds) {
		t.Fatalf("hash chain over four actor kinds must verify: %+v", result)
	}

	// 重启证据：关闭后重开同一目录，启动校验必须再次通过（链在磁盘上可独立复核）。
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := CreateFileAuditStorage(&FileStorageConfig{
		LogDir: dir, FilePrefix: "actor-kinds", MaxFileSize: 1 << 20, MaxFiles: 2,
		VerifyOnStartup: true, FlushInterval: 60000,
	})
	if err != nil {
		t.Fatalf("reopen with startup verification must succeed: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	for i, id := range entryIDs {
		stored, err := reopened.Get(ctx, id)
		if err != nil || stored == nil {
			t.Fatalf("reopened Get(%s): entry=%v err=%v", id, stored, err)
		}
		if stored.Actor.Type != string(kinds[i].Type) {
			t.Fatalf("reopened entry %s type: want %q, got %q", id, kinds[i].Type, stored.Actor.Type)
		}
	}
}

// TestEnrichActorNeverAdoptsTraceIdentity 钉住 T0.10.c 关闭的冒充通道：
// 客户端可控的 trace 头（X-Agent-ID 等）不再成为 actor 身份来源；身份缺口只由
// 可信注入身份填补，形状非法的注入身份同样不采信。
func TestEnrichActorNeverAdoptsTraceIdentity(t *testing.T) {
	ctx := ContextWithTrace(context.Background(), &AuditTrace{
		AgentID: "spoofed-agent", SessionID: "sess-1", RequestID: "req-1",
	})

	// 无注入：自报的 agent 头不得升级为身份，落 anonymous/service 兜底；
	// SessionID 仅作关联保留（非身份字段）。
	actor := EnrichActorFromContext(ctx, AuditActor{})
	if actor.Type == "agent" || actor.ID == "spoofed-agent" || actor.ID == "req-1" {
		t.Fatalf("trace header claim must not become actor identity: %+v", actor)
	}
	if actor.ID != "anonymous" || actor.Type != "service" {
		t.Fatalf("expected anonymous/service fallback, got %+v", actor)
	}
	if actor.SessionID != "sess-1" {
		t.Fatalf("session correlation lost: %+v", actor)
	}

	// 合法注入：优先于一切兜底被采用。
	trusted := WithActor(ctx, Actor{Type: ActorTypeUser, ID: "sidecar-user", Source: "sidecar-token"})
	actor = EnrichActorFromContext(trusted, AuditActor{})
	if actor.ID != "sidecar-user" || actor.Type != "user" || actor.Source != "sidecar-token" {
		t.Fatalf("trusted injected actor not adopted: %+v", actor)
	}

	// 形状非法的注入（空 id 声称 agent）：不采信，落兜底，绝不保留 agent type。
	invalid := WithActor(ctx, Actor{Type: ActorTypeAgent, ID: ""})
	actor = EnrichActorFromContext(invalid, AuditActor{})
	if actor.Type == "agent" || actor.ID != "anonymous" || actor.Type != "service" {
		t.Fatalf("invalid injected actor must not be adopted: %+v", actor)
	}

	// 调用方显式身份（如审批人、容器）不被注入覆盖。
	actor = EnrichActorFromContext(trusted, AuditActor{ID: "user-9", Type: "user"})
	if actor.ID != "user-9" || actor.Source != "" {
		t.Fatalf("explicit actor must not be overwritten: %+v", actor)
	}
}
