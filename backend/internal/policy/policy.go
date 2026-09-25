// Package policy provides the shared execution-boundary policy contract.
// It is deliberately independent of adapters, workspace, hooks, and plugins
// so those packages cannot accidentally create a policy bypass through a
// handler-only check.
package policy

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/codeflow/backend/internal/audit"
)

const RuleVersion = "b4-2026-08-19"

const (
	OperationOutboundRequest   = "outbound_request"
	OperationResponseReceive   = "response_receive"
	OperationWorkspaceWrite    = "workspace_write"
	OperationProcessStart      = "process_start"
	OperationHookExecute       = "hook_execute"
	OperationPluginInvoke      = "plugin_invoke"
	OperationPluginRegister    = "plugin_register"
	OperationPluginToggle      = "plugin_toggle"
	OperationIntegrationInvoke = "integration_invoke"
)

// Request describes a security-sensitive operation at its lowest execution
// boundary. Callers should provide the most specific identity available.
type Request struct {
	Operation string
	Resource  string
	ProjectID string
	AgentID   string
	PluginID  string
	ActorID   string
	// 请求/命令关联字段（§15 T0.10）：由调用方在已知时提供，recordDecision
	// 原样落入审计详情与 Decision；未提供则留空（omitempty），绝不伪造。
	RequestID   string
	RunID       string
	AttemptID   string
	Risk        string
	ApprovalID  string
	Fingerprint string
	Context     map[string]interface{}
}

// Decision is returned to callers and is also serialized into the audit log.
type Decision struct {
	Allowed     bool   `json:"allowed"`
	Reason      string `json:"reason"`
	RuleVersion string `json:"rule_version"`
	Operation   string `json:"operation"`
	Resource    string `json:"resource,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
	PluginID    string `json:"plugin_id,omitempty"`
	AuditID     string `json:"audit_id,omitempty"`
	// 关联与归因字段（§15 T0.10，omitempty 兼容新增；空值时序列化形状与旧版一致）。
	RequestID   string `json:"request_id,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	AttemptID   string `json:"attempt_id,omitempty"`
	Risk        string `json:"risk,omitempty"`
	ApprovalID  string `json:"approval_id,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	ActorType   string `json:"actor_type,omitempty"`
}

// Evaluator is the shared policy implementation contract.
type Evaluator interface {
	Evaluate(context.Context, Request) Decision
}

// StaticEvaluator is a compact production/test evaluator. An empty allow-list
// is fail-closed; LocalDevelopment explicitly enables the local desktop mode.
type StaticEvaluator struct {
	RuleVersion       string
	AllowedOperations map[string]bool
	LocalDevelopment  bool
	RequireProjectID  bool
}

func NewFailClosedEvaluator() *StaticEvaluator {
	return &StaticEvaluator{RuleVersion: RuleVersion, AllowedOperations: map[string]bool{}}
}

func NewLocalEvaluator() *StaticEvaluator {
	return &StaticEvaluator{
		RuleVersion:      RuleVersion,
		LocalDevelopment: true,
		AllowedOperations: map[string]bool{
			OperationOutboundRequest:   true,
			OperationResponseReceive:   true,
			OperationWorkspaceWrite:    true,
			OperationProcessStart:      true,
			OperationHookExecute:       true,
			OperationPluginInvoke:      true,
			OperationPluginRegister:    true,
			OperationPluginToggle:      true,
			OperationIntegrationInvoke: true,
		},
	}
}

func (e *StaticEvaluator) Evaluate(ctx context.Context, req Request) Decision {
	req = normalizeRequest(ctx, req)
	if e == nil {
		return recordDecision(ctx, req, Decision{Allowed: false, Reason: "policy evaluator is not configured", RuleVersion: RuleVersion,
			Operation: req.Operation, Resource: req.Resource, ProjectID: req.ProjectID, AgentID: req.AgentID, PluginID: req.PluginID})
	}
	version := strings.TrimSpace(e.RuleVersion)
	if version == "" {
		version = RuleVersion
	}
	d := Decision{Allowed: false, Reason: "operation denied by policy", RuleVersion: version,
		Operation: req.Operation, Resource: req.Resource, ProjectID: req.ProjectID,
		AgentID: req.AgentID, PluginID: req.PluginID}
	if !e.LocalDevelopment && e.RequireProjectID && strings.TrimSpace(req.ProjectID) == "" {
		d.Reason = "project identity is required"
		return recordDecision(ctx, req, d)
	}
	if !e.AllowedOperations[req.Operation] {
		return recordDecision(ctx, req, d)
	}
	d.Allowed = true
	d.Reason = "operation allowed by policy"
	return recordDecision(ctx, req, d)
}

var (
	globalMu            sync.RWMutex
	global              Evaluator
	enforcementRequired bool
)

// SetEvaluator installs the process-wide evaluator. Passing nil removes it.
func SetEvaluator(e Evaluator) {
	globalMu.Lock()
	global = e
	globalMu.Unlock()
}

func GetEvaluator() Evaluator {
	globalMu.RLock()
	e := global
	globalMu.RUnlock()
	return e
}

func HasEvaluator() bool { return GetEvaluator() != nil }

// RequireEnforcement marks the process as a production execution host. Once
// enabled, a missing evaluator is denied instead of using unit-test
// compatibility mode.
func RequireEnforcement(required bool) {
	globalMu.Lock()
	enforcementRequired = required
	globalMu.Unlock()
}

func EnforcementRequired() bool {
	globalMu.RLock()
	required := enforcementRequired
	globalMu.RUnlock()
	return required
}

// Evaluate evaluates against the global policy. A missing evaluator is
// fail-closed for direct callers; execution packages use EvaluateBoundary for
// backwards-compatible in-memory unit construction.
func Evaluate(ctx context.Context, req Request) Decision {
	req = normalizeRequest(ctx, req)
	e := GetEvaluator()
	if e == nil {
		return recordDecision(ctx, req, Decision{Allowed: false, Reason: "policy evaluator is not configured", RuleVersion: RuleVersion,
			Operation: req.Operation, Resource: req.Resource, ProjectID: req.ProjectID, AgentID: req.AgentID, PluginID: req.PluginID})
	}
	return e.Evaluate(ctx, req)
}

func normalizeRequest(ctx context.Context, req Request) Request {
	if trace := audit.TraceFromContext(ctx); trace != nil {
		if req.ProjectID == "" {
			req.ProjectID = trace.ProjectID
		}
		if req.AgentID == "" {
			req.AgentID = trace.AgentID
		}
	}
	// 关联字段只做形状规整（去空白），不生成、不补全：无则留空，不伪造。
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.RunID = strings.TrimSpace(req.RunID)
	req.AttemptID = strings.TrimSpace(req.AttemptID)
	req.Risk = strings.TrimSpace(req.Risk)
	req.ApprovalID = strings.TrimSpace(req.ApprovalID)
	req.Fingerprint = strings.TrimSpace(req.Fingerprint)
	if req.Operation == OperationOutboundRequest || req.Operation == OperationResponseReceive {
		if parsed, err := url.Parse(req.Resource); err == nil && parsed.Scheme != "" {
			parsed.RawQuery = ""
			parsed.Fragment = ""
			// userinfo（https://user:token@host/...）与 query 一样是凭据载体，不落盘。
			parsed.User = nil
			req.Resource = parsed.String()
		}
	}
	if req.Operation == OperationProcessStart {
		// Commands may contain credentials, commit messages, or user data. The
		// policy is operation-based, so never retain the raw command in audit.
		req.Resource = "process"
	}
	return req
}

// EvaluateBoundary lets constructors used by isolated unit tests operate
// without process-wide bootstrap, while still enforcing any installed policy.
func EvaluateBoundary(ctx context.Context, req Request) Decision {
	if !HasEvaluator() {
		if EnforcementRequired() {
			return Evaluate(ctx, req)
		}
		return Decision{Allowed: true, Reason: "policy not installed (in-memory compatibility mode)", RuleVersion: RuleVersion,
			Operation: req.Operation, Resource: req.Resource, ProjectID: req.ProjectID, AgentID: req.AgentID, PluginID: req.PluginID}
	}
	return Evaluate(ctx, req)
}

func recordDecision(ctx context.Context, req Request, d Decision) Decision {
	if ctx == nil {
		ctx = context.Background()
	}
	// 关联字段回填：Decision 携带请求实际提供的关联身份；未提供则留空，不伪造。
	// 回填集中在 recordDecision，三个 fail-closed 构造点（Evaluate/StaticEvaluator/
	// ExecutionPolicy）无需各自复制。
	if d.RequestID == "" {
		d.RequestID = req.RequestID
	}
	if d.RunID == "" {
		d.RunID = req.RunID
	}
	if d.AttemptID == "" {
		d.AttemptID = req.AttemptID
	}
	if d.Risk == "" {
		d.Risk = req.Risk
	}
	if d.ApprovalID == "" {
		d.ApprovalID = req.ApprovalID
	}
	if d.Fingerprint == "" {
		d.Fingerprint = req.Fingerprint
	}

	actor, actorResolution := resolveDecisionActor(ctx)
	d.ActorType = string(actor.Type)

	safeResource := redactString(req.Resource)
	details := sanitizeDetailMap(map[string]interface{}{
		"allowed": d.Allowed, "reason": d.Reason, "rule_version": d.RuleVersion,
		"operation": req.Operation, "resource": safeResource,
		"project_id": req.ProjectID, "agent_id": req.AgentID, "plugin_id": req.PluginID,
	})
	// 以下字段在脱敏后写入：关联值仍过 redactString（请求侧字符串一律先脱敏再入盘），
	// actor_resolution 是本函数自产常量。
	putIfNotEmpty(details, "request_id", req.RequestID)
	putIfNotEmpty(details, "run_id", req.RunID)
	putIfNotEmpty(details, "attempt_id", req.AttemptID)
	putIfNotEmpty(details, "risk", req.Risk)
	putIfNotEmpty(details, "approval_id", req.ApprovalID)
	putIfNotEmpty(details, "fingerprint", req.Fingerprint)
	if actorResolution != "" {
		details["actor_resolution"] = actorResolution
	}
	if len(req.Context) > 0 {
		// 只记录键名（哪些上下文字段存在过）作为证据；Context 的值一律不落盘。
		keys := make([]string, 0, len(req.Context))
		for key := range req.Context {
			keys = append(keys, redactString(key))
		}
		sort.Strings(keys)
		details["context_keys"] = keys
	}
	severity := audit.SeverityInfo
	outcome := audit.OutcomeSuccess
	if !d.Allowed {
		severity = audit.SeverityWarning
		outcome = audit.OutcomeFailure
	}
	entryID, err := audit.Record(ctx, &audit.AuditLogEntry{
		EventType: audit.EventSecurity,
		Severity:  severity,
		Actor:     actor.AuditActor(),
		Resource:  audit.AuditResource{Type: req.Operation, ID: safeResource, Name: safeResource},
		Action:    req.Operation,
		Outcome:   outcome,
		Details:   details,
	})
	if err == nil {
		d.AuditID = entryID
	}
	return d
}

// resolveDecisionActor 解析决策审计的操作者身份（T0.10.b）。policy 域是执行边界
// 的系统域，自身没有用户入口；真实身份由入口（认证中间件、审批流等，注入接线
// 归 T0.10.c）经 audit.WithActor 放入上下文。解析路径：
//  1. 已注入且 Validate 通过：如实采用（注入方对身份可信性负责，见 audit.WithActor）；
//  2. 已注入但形状非法（含空 id 声称 agent 的冒充形状）：不采用该身份，降级为
//     DefaultSystemActor 并返回留痕标记——决策证据不因身份缺陷而丢失；
//  3. 未注入：按 MissingActorDefaultSystem 降级为 system(auto-default)，绝不默认
//     冒充 agent；请求里的 AgentID 仅作为 details 证据保留，不升级为 actor 身份。
func resolveDecisionActor(ctx context.Context) (audit.Actor, string) {
	actor, err := audit.ResolveActor(ctx, audit.MissingActorDefaultSystem)
	if err != nil {
		return audit.DefaultSystemActor(), "invalid_context_actor"
	}
	return actor, ""
}

// redactedPlaceholder 是脱敏后的固定占位值；脱敏不可逆，原值不进入审计序列化
// 与哈希链。
const redactedPlaceholder = "[redacted]"

// 审计落盘的 redaction 规则（§28 T0.10.b；确定性——同输入必同输出）。
//
// 字段名黑名单（作用于递归遍历到的 map 键；recordDecision 顶层 details 键全部是
// 本函数自建的白名单，请求侧数据只能作为值进入，黑名单是防御纵深）：
//
//	K1 键名归一化（去空白、小写）后含有下列片段之一，其值无条件替换为占位值：
//	   token / secret / password / passwd / api_key / apikey / api-key /
//	   authorization。K1 刻意不含裸 "key"，避免误伤 "monkey" 之类词。
//
// 值模式（作用于一切进入 details/资源字段的字符串，命中任一即将整串替换，
// 不做局部遮蔽，避免残留半段凭据）：
//
//	V1 授权头形态：Bearer|Basic 后接凭据串；
//	V2 敏感键值形态：token / api_key / apikey / api-key / key / secret /
//	   password / passwd / authorization / access_token / client_secret
//	   后接 = 或 :（覆盖 URL query 残留、"token: abc" 文本、argv 中的
//	   --token=xxx；process_start 的整段命令在 normalizeRequest 已替换为
//	   "process"，此处为纵深防御）；
//	V3 知名凭据前缀：sk-*(OpenAI)、gh[pousr]_*/github_pat_*(GitHub)、
//	   xox*-*(Slack)、AIza*(Google)、eyJ*.*.*(JWT)。
var sensitiveDetailKeyFragments = []string{
	"token", "secret", "password", "passwd", "api_key", "apikey", "api-key", "authorization",
}

var redactionValuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[A-Za-z0-9._~+/=-]{4,}`),
	regexp.MustCompile(`(?i)\b(?:token|api[_-]?key|key|secret|password|passwd|authorization|access[_-]?token|client[_-]?secret)\s*[=:]\s*[^\s&;"']+`),
	regexp.MustCompile(`(?:sk-[A-Za-z0-9]{8,}|gh[pousr]_[A-Za-z0-9]{8,}|github_pat_[A-Za-z0-9_]{8,}|xox[a-z]-[A-Za-z0-9-]{6,}|AIza[A-Za-z0-9_-]{8,}|eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)`),
}

// redactString 命中任一值模式即整串替换为占位值；空串与未命中原样返回。
func redactString(s string) string {
	if s == "" {
		return s
	}
	for _, re := range redactionValuePatterns {
		if re.MatchString(s) {
			return redactedPlaceholder
		}
	}
	return s
}

func isSensitiveDetailKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, fragment := range sensitiveDetailKeyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

// sanitizeDetailMap 递归过滤 details：敏感键名的值无条件占位，字符串值过值模式。
func sanitizeDetailMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = sanitizeDetailValue(key, value)
	}
	return out
}

func sanitizeDetailValue(key string, value interface{}) interface{} {
	if isSensitiveDetailKey(key) {
		return redactedPlaceholder
	}
	switch v := value.(type) {
	case string:
		return redactString(v)
	case []string:
		out := make([]string, len(v))
		for i, item := range v {
			out[i] = redactString(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = sanitizeDetailValue("", item)
		}
		return out
	case map[string]interface{}:
		return sanitizeDetailMap(v)
	default:
		return value
	}
}

// putIfNotEmpty 仅写入请求实际提供的关联值；空值不留键，审计不伪造关联。
func putIfNotEmpty(details map[string]interface{}, key, value string) {
	if value != "" {
		details[key] = redactString(value)
	}
}

// BootstrapEvaluator returns the production evaluator. Local execution is an
// explicit opt-in for desktop development; server deployments remain closed
// until a policy is installed and operations are allowed.
func BootstrapEvaluator() Evaluator {
	if os.Getenv("CODEFLOW_ALLOW_LOCAL_EXECUTION") == "1" {
		return NewLocalEvaluator()
	}
	e := NewFailClosedEvaluator()
	e.RequireProjectID = true
	return e
}

func DenialError(d Decision) error {
	if d.Allowed {
		return nil
	}
	return &DeniedError{Decision: d}
}

// DeniedError is non-retryable: repeating an operation cannot change policy.
type DeniedError struct {
	Decision Decision
}

func (e *DeniedError) Error() string {
	d := e.Decision
	return fmt.Sprintf("policy denied %s: %s (rule %s, audit %s)", d.Operation, d.Reason, d.RuleVersion, d.AuditID)
}
