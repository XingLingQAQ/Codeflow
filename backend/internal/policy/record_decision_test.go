package policy

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/audit"
)

// setupDecisionAudit 安装内存审计存储与 fail-closed 评估器（全部拒绝，保证
// 每次 Evaluate 都走 recordDecision 落盘），测试结束恢复原状。
func setupDecisionAudit(t *testing.T) (*audit.AuditService, *audit.MemoryStorage) {
	t.Helper()
	storage := audit.NewMemoryStorage()
	svc := audit.NewAuditService(storage)
	audit.SetAuditService(svc)
	SetEvaluator(NewFailClosedEvaluator())
	t.Cleanup(func() {
		SetEvaluator(nil)
		audit.SetAuditService(nil)
	})
	return svc, storage
}

func mustGetEntry(t *testing.T, svc *audit.AuditService, id string) *audit.AuditLogEntry {
	t.Helper()
	entry, err := svc.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetByID(%s): %v", id, err)
	}
	return entry
}

// TestRecordDecisionActorTypeFromContext 验证上下文注入的 actor type 如实落入
// 审计条目与 Decision（I-50：agent/user/integration 可区分）。
func TestRecordDecisionActorTypeFromContext(t *testing.T) {
	svc, _ := setupDecisionAudit(t)

	cases := []struct {
		name       string
		actor      audit.Actor
		wantType   string
		wantID     string
		wantSource string
	}{
		{"agent", audit.Actor{Type: audit.ActorTypeAgent, ID: "agent-7", Source: "runner"}, "agent", "agent-7", "runner"},
		{"user", audit.Actor{Type: audit.ActorTypeUser, ID: "user-3", Source: "http-session"}, "user", "user-3", "http-session"},
		{"integration", audit.Actor{Type: audit.ActorTypeIntegration, ID: "github", Source: "webhook:github"}, "integration", "github", "webhook:github"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := audit.WithActor(context.Background(), tc.actor)
			decision := Evaluate(ctx, Request{Operation: OperationPluginInvoke, Resource: "res-" + tc.name, ProjectID: "p1"})
			if decision.AuditID == "" {
				t.Fatal("expected audit id on denied decision")
			}
			if decision.ActorType != tc.wantType {
				t.Errorf("decision actor_type: want %q, got %q", tc.wantType, decision.ActorType)
			}
			entry := mustGetEntry(t, svc, decision.AuditID)
			if entry.Actor.Type != tc.wantType || entry.Actor.ID != tc.wantID || entry.Actor.Source != tc.wantSource {
				t.Errorf("audit actor: want %s/%s/%s, got %+v", tc.wantType, tc.wantID, tc.wantSource, entry.Actor)
			}
			if _, leaked := entry.Details["actor_resolution"]; leaked {
				t.Errorf("valid context actor must not leave fallback marker: %+v", entry.Details)
			}
		})
	}
}

// TestRecordDecisionMissingActorDefaultsSystem 验证未注入身份时按
// MissingActorDefaultSystem 降级为 system，且与注入 agent 的同请求可区分；
// 请求携带 AgentID 也不得默认冒充 agent（T0.10.a 语义）。
func TestRecordDecisionMissingActorDefaultsSystem(t *testing.T) {
	svc, _ := setupDecisionAudit(t)

	req := Request{Operation: OperationWorkspaceWrite, Resource: "proj/file.txt", ProjectID: "p1", AgentID: "agent-trace"}
	injected := Evaluate(audit.WithActor(context.Background(), audit.Actor{Type: audit.ActorTypeAgent, ID: "agent-9", Source: "runner"}), req)
	missing := Evaluate(context.Background(), req)
	if injected.AuditID == "" || missing.AuditID == "" {
		t.Fatalf("both decisions must be audited: %+v / %+v", injected, missing)
	}

	injectedEntry := mustGetEntry(t, svc, injected.AuditID)
	if injectedEntry.Actor.Type != "agent" || injectedEntry.Actor.ID != "agent-9" {
		t.Fatalf("injected actor not recorded: %+v", injectedEntry.Actor)
	}

	missingEntry := mustGetEntry(t, svc, missing.AuditID)
	if missingEntry.Actor.Type != "system" || missingEntry.Actor.ID != audit.DefaultSystemActorID ||
		missingEntry.Actor.Source != audit.ActorSourceAutoDefault {
		t.Fatalf("missing identity must degrade to system auto-default: %+v", missingEntry.Actor)
	}
	if missing.ActorType != "system" {
		t.Fatalf("decision actor_type: want system, got %q", missing.ActorType)
	}
	if missingEntry.Actor.Type == "agent" {
		t.Fatal("missing identity must never default to agent")
	}
	// AgentID 作为证据保留在 details，但不升级为 actor 身份。
	if missingEntry.Details["agent_id"] != "agent-trace" {
		t.Fatalf("agent_id evidence lost: %+v", missingEntry.Details)
	}
}

// TestRecordDecisionInvalidContextActorFallsBack 验证冒充形状（空 id 声称 agent）
// 不被采用：降级 system 并留痕 actor_resolution，决策证据仍落盘。
func TestRecordDecisionInvalidContextActorFallsBack(t *testing.T) {
	svc, _ := setupDecisionAudit(t)

	ctx := audit.WithActor(context.Background(), audit.Actor{Type: audit.ActorTypeAgent, ID: ""})
	decision := Evaluate(ctx, Request{Operation: OperationPluginInvoke, Resource: "res", ProjectID: "p1"})
	if decision.AuditID == "" {
		t.Fatal("decision evidence must be recorded even when injected identity is invalid")
	}
	if decision.ActorType != "system" {
		t.Fatalf("spoofed actor must not be adopted: actor_type=%q", decision.ActorType)
	}
	entry := mustGetEntry(t, svc, decision.AuditID)
	if entry.Actor.Type != "system" || entry.Actor.ID != audit.DefaultSystemActorID {
		t.Fatalf("expected system fallback actor, got %+v", entry.Actor)
	}
	if entry.Details["actor_resolution"] != "invalid_context_actor" {
		t.Fatalf("fallback must be traceable via actor_resolution: %+v", entry.Details)
	}
}

// TestRecordDecisionDetailsRedactionMatrix 脱敏过滤矩阵：注入含 URL query、
// argv、token/API key 字样的输入，逐字段断言落盘内容零泄漏。
func TestRecordDecisionDetailsRedactionMatrix(t *testing.T) {
	svc, _ := setupDecisionAudit(t)

	cases := []struct {
		name string
		req  Request
		// canaries 是注入的敏感值，落盘条目序列化后一律不得出现
		canaries []string
		// wantDetails 断言脱敏后 details 的精确字段值
		wantDetails map[string]interface{}
		// absentSubstrings 额外不得出现的子串（如 "api_key=" 这类键值残片）
		absentSubstrings []string
	}{
		{
			name: "url query with api_key",
			req:  Request{Operation: OperationOutboundRequest, Resource: "https://api.test/v1/chat?api_key=sk-live123456789&mode=fast"},
			canaries: []string{"sk-live123456789"},
			wantDetails: map[string]interface{}{"resource": "https://api.test/v1/chat"},
			absentSubstrings: []string{"api_key=", "mode=fast"},
		},
		{
			name: "argv with token flag",
			req:  Request{Operation: OperationProcessStart, Resource: `git clone https://repo.test/x --config http.token=ghp_abcdef123456789`},
			canaries: []string{"ghp_abcdef123456789"},
			wantDetails: map[string]interface{}{"resource": "process"},
			absentSubstrings: []string{"--config", "git clone"},
		},
		{
			name: "bearer header value in field",
			req:  Request{Operation: OperationPluginInvoke, Resource: "res-bearer", ProjectID: "proj Bearer abcdef123456"},
			canaries: []string{"abcdef123456"},
			wantDetails: map[string]interface{}{"project_id": redactedPlaceholder},
		},
		{
			name: "token key-value in agent id",
			req:  Request{Operation: OperationPluginInvoke, Resource: "res-kv", AgentID: "agent token:xoxb-1234567890-abcdefghij"},
			canaries: []string{"xoxb-1234567890-abcdefghij"},
			wantDetails: map[string]interface{}{"agent_id": redactedPlaceholder},
		},
		{
			name: "standalone api key prefix",
			req:  Request{Operation: OperationPluginInvoke, Resource: "res-sk", PluginID: "sk-proj123456789"},
			canaries: []string{"sk-proj123456789"},
			wantDetails: map[string]interface{}{"plugin_id": redactedPlaceholder},
		},
		{
			name: "jwt in url path",
			req: Request{Operation: OperationOutboundRequest,
				Resource: "https://api.test/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJVadQssw5c"},
			canaries: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9", "SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJVadQssw5c"},
			wantDetails: map[string]interface{}{"resource": redactedPlaceholder},
		},
		{
			name: "context values never persisted",
			req: Request{Operation: OperationPluginInvoke, Resource: "res-ctx", PluginID: "plugin-v2",
				Context: map[string]interface{}{"token": "nested-secret-value", "note": "ok"}},
			canaries: []string{"nested-secret-value"},
			wantDetails: map[string]interface{}{"plugin_id": "plugin-v2", "context_keys": []string{"note", "token"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := Evaluate(context.Background(), tc.req)
			if decision.AuditID == "" {
				t.Fatal("expected audit id")
			}
			entry := mustGetEntry(t, svc, decision.AuditID)
			raw, err := json.Marshal(entry)
			if err != nil {
				t.Fatalf("marshal entry: %v", err)
			}
			for _, canary := range tc.canaries {
				if strings.Contains(string(raw), canary) {
					t.Errorf("audit entry leaked %q: %s", canary, raw)
				}
			}
			for _, absent := range tc.absentSubstrings {
				if strings.Contains(string(raw), absent) {
					t.Errorf("audit entry retained %q: %s", absent, raw)
				}
			}
			for key, want := range tc.wantDetails {
				if got := entry.Details[key]; !reflect.DeepEqual(got, want) {
					t.Errorf("details[%q]: want %#v, got %#v", key, want, got)
				}
			}
		})
	}
}

// TestSanitizeDetailMapRules 直接验证 redaction 规则表：字段名黑名单（K1）与
// 值模式（V1/V2/V3）各自独立生效，良性内容不受影响。
func TestSanitizeDetailMapRules(t *testing.T) {
	out := sanitizeDetailMap(map[string]interface{}{
		"api_token":   "plain-value-no-pattern",          // K1：键名命中，值无模式也占位
		"note":        "Authorization: Bearer tok123456", // V1
		"cmdline":     "--api_key=AK123456789",           // V2
		"standalone":  "ghp_abcdef123456789",             // V3
		"nested":      map[string]interface{}{"client_secret": "zzz"}, // K1 递归
		"list":        []string{"sk-abcdefgh1234", "ok"},              // V3 元素级
		"benign":      "project alpha release",
		"count":       3,
	})
	want := map[string]interface{}{
		"api_token":  redactedPlaceholder,
		"note":       redactedPlaceholder,
		"cmdline":    redactedPlaceholder,
		"standalone": redactedPlaceholder,
		"nested":     map[string]interface{}{"client_secret": redactedPlaceholder},
		"list":       []string{redactedPlaceholder, "ok"},
		"benign":     "project alpha release",
		"count":      3,
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("sanitizeDetailMap mismatch:\nwant %#v\ngot  %#v", want, out)
	}

	// 确定性：同输入两次过滤结果完全一致
	again := sanitizeDetailMap(map[string]interface{}{
		"api_token": "plain-value-no-pattern", "note": "Authorization: Bearer tok123456",
		"cmdline": "--api_key=AK123456789", "standalone": "ghp_abcdef123456789",
		"nested": map[string]interface{}{"client_secret": "zzz"}, "list": []string{"sk-abcdefgh1234", "ok"},
		"benign": "project alpha release", "count": 3,
	})
	if !reflect.DeepEqual(out, again) {
		t.Fatal("redaction must be deterministic for identical input")
	}
}

// TestRecordDecisionCorrelationFields 验证 request/run/attempt/risk/approval/
// fingerprint 关联字段落入 details 与 Decision；未提供时键不存在、序列化省略，
// 绝不伪造。
func TestRecordDecisionCorrelationFields(t *testing.T) {
	svc, _ := setupDecisionAudit(t)

	decision := Evaluate(context.Background(), Request{
		Operation: OperationPluginInvoke, Resource: "res", ProjectID: "p1",
		RequestID: "req-1", RunID: "run-1", AttemptID: "attempt-1", Risk: "high",
		ApprovalID: "appr-1", Fingerprint: "fp-abc",
	})
	entry := mustGetEntry(t, svc, decision.AuditID)
	wantDetails := map[string]string{
		"request_id": "req-1", "run_id": "run-1", "attempt_id": "attempt-1",
		"risk": "high", "approval_id": "appr-1", "fingerprint": "fp-abc",
	}
	for key, want := range wantDetails {
		if got := entry.Details[key]; got != want {
			t.Errorf("details[%q]: want %q, got %#v", key, want, got)
		}
	}
	if decision.RequestID != "req-1" || decision.RunID != "run-1" || decision.AttemptID != "attempt-1" ||
		decision.Risk != "high" || decision.ApprovalID != "appr-1" || decision.Fingerprint != "fp-abc" {
		t.Errorf("decision correlation fields mismatch: %+v", decision)
	}

	// 未提供关联字段：details 无键、Decision 为空且 JSON 省略（形状与旧版一致）。
	bare := Evaluate(context.Background(), Request{Operation: OperationPluginInvoke, Resource: "res2", ProjectID: "p1"})
	bareEntry := mustGetEntry(t, svc, bare.AuditID)
	for _, key := range []string{"request_id", "run_id", "attempt_id", "risk", "approval_id", "fingerprint"} {
		if _, present := bareEntry.Details[key]; present {
			t.Errorf("details[%q] must be absent when request omits it: %+v", key, bareEntry.Details)
		}
	}
	raw, err := json.Marshal(bare)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	for _, key := range []string{"request_id", "run_id", "attempt_id", "risk", "approval_id", "fingerprint"} {
		if strings.Contains(string(raw), key) {
			t.Errorf("empty %s must be omitted from decision JSON: %s", key, raw)
		}
	}
}

// TestRecordDecisionRedactionDeterministicAndHashVerifiable 验证脱敏在哈希之前
// 发生且可复核：同输入两次落盘的 details 完全一致、固定易变字段后哈希一致；
// 落盘条目哈希与内容吻合；篡改回原始敏感值则哈希改变。
func TestRecordDecisionRedactionDeterministicAndHashVerifiable(t *testing.T) {
	svc, storage := setupDecisionAudit(t)

	req := Request{
		Operation: OperationPluginInvoke, Resource: "res", ProjectID: "proj token=canary-777",
		RequestID: "req-1", RunID: "run-1",
	}
	d1 := Evaluate(context.Background(), req)
	d2 := Evaluate(context.Background(), req)
	e1 := mustGetEntry(t, svc, d1.AuditID)
	e2 := mustGetEntry(t, svc, d2.AuditID)

	// 同输入同脱敏输出
	if !reflect.DeepEqual(e1.Details, e2.Details) {
		t.Fatalf("redaction must be deterministic:\n%#v\n%#v", e1.Details, e2.Details)
	}
	if e1.Details["project_id"] != redactedPlaceholder {
		t.Fatalf("expected redacted project_id, got %#v", e1.Details["project_id"])
	}
	raw, err := json.Marshal(e1)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	if strings.Contains(string(raw), "canary-777") {
		t.Fatalf("canary leaked into persisted entry: %s", raw)
	}

	// 同输入同 hash（哈希输入中的易变字段——id/timestamp/previous_hash 由服务
	// 生成——固定后，两条同输入条目的哈希必须一致）
	c2 := *e2
	c2.ID, c2.Timestamp, c2.PreviousHash = e1.ID, e1.Timestamp, e1.PreviousHash
	if audit.CalculateEntryHash(e1) != audit.CalculateEntryHash(&c2) {
		t.Fatal("identical redacted input must produce identical hash contribution")
	}

	// 落盘条目的哈希与脱敏后内容吻合（脱敏先于哈希）
	if e1.Hash != audit.CalculateEntryHash(e1) || e2.Hash != audit.CalculateEntryHash(e2) {
		t.Fatal("persisted entry hash must verify against redacted content")
	}

	// 哈希覆盖的是脱敏后内容：把原始敏感值塞回 details 会改变哈希
	tampered := *e1
	tampered.Details = map[string]interface{}{"project_id": "proj token=canary-777"}
	if audit.CalculateEntryHash(&tampered) == e1.Hash {
		t.Fatal("hash must change when redacted content is replaced with raw secret")
	}

	// 含脱敏条目的链保持完整
	result, err := storage.VerifyHashChain(context.Background())
	if err != nil {
		t.Fatalf("VerifyHashChain: %v", err)
	}
	if !result.Valid || result.CheckedEntries < 2 {
		t.Fatalf("hash chain must remain valid over redacted entries: %+v", result)
	}
}
