// Package hooks - Trigger-point table and failure policy (T1.07.a).
//
// 本文件冻结 3.0 的“钩子触发点表”（计划 §15 T1.07、§28 T1.07.a、问题 I-27）：
// 每个 hook 在哪里触发、失败怎么处理、观察的是哪一件事，全部写在这里，并由
// trigger_points_test.go 的 AST 扫描把它和真实代码绑死——新增一个 Trigger 调用、
// 或删掉一个调用点，测试立刻失败。
//
// # 失败策略语义（冻结）
//
//   - reject：hook 报错或拒绝即阻止操作（失败即关闭）。用于 HookBeforeWrite、
//     HookBeforeSend、HookPreToolUse、HookRunStart。hook 的“放行”永远不能推翻
//     Guard/policy 的拒绝：policy.EnforceBoundary 在每次 hook 执行前运行
//     （T0.09.c，manager.go Trigger/TriggerHook），本表不改变这个顺序。
//   - warn：操作照常进行，错误必须被记录（今天是日志；T1.07.b 起写 outbox
//     warning）。绝不重做操作本身。
//   - retry：只重跑 hook handler（HookConfig.RetryCount / executeWithRetry），
//     用尽后降级为 warn；绝不重做底层操作。只用于 handler 可安全重复的点
//     （after 类：操作已经发生，重跑观察者不会重放它）。
//
// tool_pre 在工具请求进入 Guard 之前触发，hook 放行后 Guard 仍然执行；tool_post
// 在结果净化（脱敏）之后触发（计划 T1.07 卡片原文）。
//
// # 不重复计数规则
//
// 服务端自己的 FSService 写入（UI 与旧的进程内 agent）只触发 HookBeforeWrite，
// 观察 workspace_service_write；外部 CLI 后端在它自己的 Run 工作副本里的写入由
// 后端工具调用观察，只触发 HookPreToolUse，观察 cli_tool_call。CLI 的写入从不
// 经过 FSService.Write：runworkspace 物化工作副本用自己的一段复制代码
// （runworkspace/copy.go 的 copyFile 直接用 os.OpenFile 写目标文件），所以两个触发点
// 观察的是两件不同的事，Observation 不同、Source 也不同。把工作副本合入发布树是
// 另一个操作（merge），不是同一次工具调用，也不由这两个点计数。
//
// # 表的不变量（trigger_points_test.go 逐条断言）
//
//   - 表里每个 Owner="existing" 的点在代码里恰好出现一次（Site+Hook 唯一）；
//   - 代码里每个触发调用都在表里（未登记调用 = 测试失败）；
//   - 保留点（Owner="T1.07.b"）今天没有调用点，且它的 Site 文件尚不存在；
//   - 每个 Observation 全局唯一，不跨 Source 共享；
//   - Policy 与代码现状不一致的点必须出现在 BehaviorGaps() 里并写明收口步骤，
//     不能静默接受。
//
// 本步只冻结契约，不改变任何触发行为：接线（allowlist、execbackend 端口、
// outbox warning）归 T1.07.b。
package hooks

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// TriggerCategory 是触发点类别的封闭枚举：一个触发点属于哪一段生命周期。
type TriggerCategory string

const (
	// TriggerCategoryInput：用户/会话输入进入系统。
	TriggerCategoryInput TriggerCategory = "input"
	// TriggerCategoryBeforeWrite：一次文件写入落盘之前。
	TriggerCategoryBeforeWrite TriggerCategory = "before_write"
	// TriggerCategoryAfterExec：一次命令/工具执行已经发生之后。
	TriggerCategoryAfterExec TriggerCategory = "after_exec"
	// TriggerCategoryContext：上下文压缩与消息完成等上下文管理。
	TriggerCategoryContext TriggerCategory = "context"
	// TriggerCategoryModelIO：模型请求/响应/流的边界。
	TriggerCategoryModelIO TriggerCategory = "model_io"
	// TriggerCategoryTaskLifecycle：任务生命周期。
	TriggerCategoryTaskLifecycle TriggerCategory = "task_lifecycle"
	// TriggerCategoryStateRestore：快照/状态恢复之后。
	TriggerCategoryStateRestore TriggerCategory = "state_restore"
	// TriggerCategoryManual：按名字手动触发（没有固定的 hook 类型）。
	TriggerCategoryManual TriggerCategory = "manual"
	// TriggerCategoryToolPre：外部后端的工具请求，进入 Guard 之前。
	TriggerCategoryToolPre TriggerCategory = "tool_pre"
	// TriggerCategoryToolPost：外部后端的工具结果，脱敏之后。
	TriggerCategoryToolPost TriggerCategory = "tool_post"
	// TriggerCategoryRunStart：Run 被认领、进程启动之前。
	TriggerCategoryRunStart TriggerCategory = "run_start"
	// TriggerCategoryRunFinish：Run 到达终态之后。
	TriggerCategoryRunFinish TriggerCategory = "run_finish"
)

// TriggerCategories 按声明顺序列出全部类别，供覆盖率断言使用。
var TriggerCategories = []TriggerCategory{
	TriggerCategoryInput,
	TriggerCategoryBeforeWrite,
	TriggerCategoryAfterExec,
	TriggerCategoryContext,
	TriggerCategoryModelIO,
	TriggerCategoryTaskLifecycle,
	TriggerCategoryStateRestore,
	TriggerCategoryManual,
	TriggerCategoryToolPre,
	TriggerCategoryToolPost,
	TriggerCategoryRunStart,
	TriggerCategoryRunFinish,
}

// Valid reports whether c is one of the closed category values.
func (c TriggerCategory) Valid() bool {
	for _, known := range TriggerCategories {
		if c == known {
			return true
		}
	}
	return false
}

// ObservationSource 是“这个触发点从哪条链路观察”的封闭枚举。它与
// TriggerCategory 正交：同一个类别可以在旧会话链路和外部 CLI 链路上各有一个
// 触发点，但两者观察的现实事实必须不同（不重复计数规则）。
type ObservationSource string

const (
	// ObservationSourceLegacyWorkspace：服务端自己的 workspace.FSService 写入。
	ObservationSourceLegacyWorkspace ObservationSource = "legacy_workspace"
	// ObservationSourceLegacySession：旧会话链路（websocket / adapters /
	// summarize / snapshot / commander 等）。
	ObservationSourceLegacySession ObservationSource = "legacy_session"
	// ObservationSourceLegacyPlanner：旧 planner 的任务生命周期。
	ObservationSourceLegacyPlanner ObservationSource = "legacy_planner"
	// ObservationSourceCLIBackend：外部 CLI 执行后端（execbackend 端口之后）。
	ObservationSourceCLIBackend ObservationSource = "cli_backend"
	// ObservationSourceHTTPManual：HTTP 手动按名触发。
	ObservationSourceHTTPManual ObservationSource = "http_manual"
	// ObservationSourceIntegration：外部集成按 manifest 触发。
	ObservationSourceIntegration ObservationSource = "integration"
)

// ObservationSources 按声明顺序列出全部来源。
var ObservationSources = []ObservationSource{
	ObservationSourceLegacyWorkspace,
	ObservationSourceLegacySession,
	ObservationSourceLegacyPlanner,
	ObservationSourceCLIBackend,
	ObservationSourceHTTPManual,
	ObservationSourceIntegration,
}

// Valid reports whether s is one of the closed source values.
func (s ObservationSource) Valid() bool {
	for _, known := range ObservationSources {
		if s == known {
			return true
		}
	}
	return false
}

// FailurePolicy 是 3.0 对“hook 失败时怎么办”的封闭枚举。
type FailurePolicy string

const (
	// FailurePolicyReject：失败即关闭，操作被拒。
	FailurePolicyReject FailurePolicy = "reject"
	// FailurePolicyWarn：操作照常，错误必须被记录。
	FailurePolicyWarn FailurePolicy = "warn"
	// FailurePolicyRetry：只重跑 hook handler，用尽后降级为 warn。
	FailurePolicyRetry FailurePolicy = "retry"
)

// FailurePolicies 按声明顺序列出全部策略。
var FailurePolicies = []FailurePolicy{FailurePolicyReject, FailurePolicyWarn, FailurePolicyRetry}

// Valid reports whether p is one of the closed policy values.
func (p FailurePolicy) Valid() bool {
	for _, known := range FailurePolicies {
		if p == known {
			return true
		}
	}
	return false
}

// FailureConsequence 是失败对“这个触发点守护的操作”的后果。
type FailureConsequence string

const (
	// ConsequenceBlock：操作被拒（不发生）。
	ConsequenceBlock FailureConsequence = "block"
	// ConsequenceContinue：操作照常进行。
	ConsequenceContinue FailureConsequence = "continue"
)

// Consequence reports what a failure policy does to the guarded operation.
func (p FailurePolicy) Consequence() FailureConsequence {
	if p == FailurePolicyReject {
		return ConsequenceBlock
	}
	return ConsequenceContinue
}

// CurrentBehavior 是“今天这段代码出错时实际做什么”的封闭枚举，用于把声明策略和
// 现状对照（见 BehaviorGaps）。
type CurrentBehavior string

const (
	// BehaviorRejected：操作被拒（错误被转成拒绝）。
	BehaviorRejected CurrentBehavior = "rejected"
	// BehaviorLoggedWarn：操作照常，错误被记录（至少一条日志）。
	BehaviorLoggedWarn CurrentBehavior = "logged-warn"
	// BehaviorReturnedToCaller：触发点只把错误交给调用方，自己不拒绝也不记录；
	// 操作是否继续由调用方决定，而调用方之间可能并不一致。
	BehaviorReturnedToCaller CurrentBehavior = "returned-to-caller"
	// BehaviorDiscarded：错误被完全丢弃，操作照常且没有任何记录。
	BehaviorDiscarded CurrentBehavior = "discarded"
	// BehaviorNotWired：今天没有调用点（保留点，接线归 T1.07.b），所以没有
	// 现状可对照。只有 Owner="T1.07.b" 的行可以用它。
	BehaviorNotWired CurrentBehavior = "not-wired"
)

// CurrentBehaviors 按声明顺序列出全部现状取值。
var CurrentBehaviors = []CurrentBehavior{
	BehaviorRejected,
	BehaviorLoggedWarn,
	BehaviorReturnedToCaller,
	BehaviorDiscarded,
	BehaviorNotWired,
}

// Valid reports whether b is one of the closed behaviour values.
func (b CurrentBehavior) Valid() bool {
	for _, known := range CurrentBehaviors {
		if b == known {
			return true
		}
	}
	return false
}

// Consequence reports what today's behaviour does to the guarded operation.
func (b CurrentBehavior) Consequence() FailureConsequence {
	if b == BehaviorRejected {
		return ConsequenceBlock
	}
	return ConsequenceContinue
}

// CompatibleWith reports whether a point whose code behaves as c today already
// satisfies policy p. 不一致的点就是 BehaviorGaps() 里的差距，必须由 Owner 收口。
//
// reject 只有“操作被拒”能满足；warn 与 retry 只有“操作继续且错误被记录”能满足
// （retry 用尽重试后降级为 warn，所以它同样要求记录）。于是：
//
//   - discarded 永远不兼容：错误消失了，没有任何记录；
//   - returned-to-caller 永远不兼容：触发点自己既不拒绝也不记录，结果由调用方
//     决定，而调用方可能互相矛盾（HookPostResponse 正是这样：非流式路径让 Send
//     失败，流式路径把错误丢掉）。
func (p FailurePolicy) CompatibleWith(c CurrentBehavior) bool {
	switch p {
	case FailurePolicyReject:
		return c == BehaviorRejected
	case FailurePolicyWarn, FailurePolicyRetry:
		return c == BehaviorLoggedWarn
	default:
		return false
	}
}

// TriggerPoint 是触发点表的一行：一个 hook 在哪里触发、观察什么、失败怎么办。
type TriggerPoint struct {
	// Hook 是被触发的 hook 类型；只有“按名手动触发”（TriggerHook）可以为空，
	// 因为名字由请求给出。
	Hook HookType
	// Category 是这段生命周期。
	Category TriggerCategory
	// Source 是观察来源链路。
	Source ObservationSource
	// Observation 是这个触发点观察的唯一现实事实。表里每个 Observation 全局
	// 唯一（不重复计数）：一件事只对应一个触发点。
	Observation string
	// Site 是代码位置：internal/<包路径>/<文件>.go:<函数名> 或
	// ...:(*Type).Method，不带行号。未接线的保留点用 <tbd> 占位。
	Site string
	// Policy 是 3.0 声明的失败策略。
	Policy FailurePolicy
	// CurrentBehavior 是今天出错时代码实际做什么。
	CurrentBehavior CurrentBehavior
	// Retained 表示这个触发点保留在 3.0 的触发集合里。false 表示建议退役
	// （代码不删，理由写在 Note）。
	Retained bool
	// Owner 是 "existing"（既有调用点）或 "T1.07.b"（保留点，尚未接线）。
	Owner string
	// Note 记录判定依据，以及容易被误解的地方。
	Note string
	// GapClosure 非空当且仅当 Policy 与 CurrentBehavior 不一致；它写清哪一步、
	// 做什么收口（见 BehaviorGaps）。
	GapClosure string
}

// 触发点的 Owner 取值。
const (
	// TriggerPointOwnerExisting：调用点已经存在。
	TriggerPointOwnerExisting = "existing"
	// TriggerPointOwnerT107B：保留点，接线归 T1.07.b。
	TriggerPointOwnerT107B = "T1.07.b"
)

// ErrInvalidTriggerPoint 是触发点表校验失败的哨兵，供 errors.Is 使用。
var ErrInvalidTriggerPoint = errors.New("hooks: invalid trigger point")

// TriggerPointError 指明表里哪一行、哪个字段不合契约。
type TriggerPointError struct {
	Index  int
	Field  string
	Reason string
}

// Error implements error.
func (e *TriggerPointError) Error() string {
	return fmt.Sprintf("hooks: trigger point #%d: field %q: %s", e.Index, e.Field, e.Reason)
}

// Is lets errors.Is report ErrInvalidTriggerPoint.
func (e *TriggerPointError) Is(target error) bool { return target == ErrInvalidTriggerPoint }

// sitePattern 是已接线触发点的 Site 形状：internal/<pkg...>/<file>.go:<符号>，
// 符号可以是函数名或 (*Type).Method，绝不带行号（行号会随重构漂移）。
var sitePattern = regexp.MustCompile(`^internal/([a-z0-9_]+/)*[a-z0-9_]+\.go:(\(\*?[A-Za-z0-9_]+\)\.)?[A-Za-z0-9_]+$`)

// pendingSitePattern 是未接线保留点的 Site 形状：文件路径 + <tbd> 占位。
var pendingSitePattern = regexp.MustCompile(`^internal/([a-z0-9_]+/)*[a-z0-9_]+\.go:<tbd>$`)

// SiteFile returns the file path part of a Site, and whether the Site has the
// shape this package expects (a <tbd> placeholder is not a file).
func SiteFile(site string) (string, bool) {
	if pendingSitePattern.MatchString(site) {
		return "", false
	}
	if !sitePattern.MatchString(site) {
		return "", false
	}
	return site[:strings.Index(site, ":")], true
}

// Validate checks one row against the frozen contract: closed enums, non-empty
// observation, a well-formed Site and a closed Owner. It does not check the
// cross-row invariants (uniqueness, table-vs-code binding) — those belong to
// trigger_points_test.go.
func (t TriggerPoint) Validate() error { return t.validate(0) }

func (t TriggerPoint) validate(index int) error {
	fail := func(field, reason string) error {
		return &TriggerPointError{Index: index, Field: field, Reason: reason}
	}
	if t.Hook != "" && !t.Hook.Valid() {
		return fail("Hook", fmt.Sprintf("%q is not a declared HookType", string(t.Hook)))
	}
	if t.Hook == "" && t.Category != TriggerCategoryManual {
		return fail("Hook", "only a manual by-name trigger point may leave the hook empty")
	}
	if !t.Category.Valid() {
		return fail("Category", fmt.Sprintf("%q is not a declared TriggerCategory", string(t.Category)))
	}
	if !t.Source.Valid() {
		return fail("Source", fmt.Sprintf("%q is not a declared ObservationSource", string(t.Source)))
	}
	if strings.TrimSpace(t.Observation) == "" {
		return fail("Observation", "empty: a trigger point must name the one fact it observes")
	}
	if t.Owner != TriggerPointOwnerExisting && t.Owner != TriggerPointOwnerT107B {
		return fail("Owner", fmt.Sprintf("%q is neither %q nor %q", t.Owner, TriggerPointOwnerExisting, TriggerPointOwnerT107B))
	}
	if t.Owner == TriggerPointOwnerT107B {
		if !pendingSitePattern.MatchString(t.Site) {
			return fail("Site", fmt.Sprintf("%q is not a pending site (internal/<pkg>/<file>.go:<tbd>)", t.Site))
		}
	} else if !sitePattern.MatchString(t.Site) {
		return fail("Site", fmt.Sprintf("%q is not internal/<pkg>/<file>.go:<symbol>", t.Site))
	}
	if !t.Policy.Valid() {
		return fail("Policy", fmt.Sprintf("%q is not a declared FailurePolicy", string(t.Policy)))
	}
	if !t.CurrentBehavior.Valid() {
		return fail("CurrentBehavior", fmt.Sprintf("%q is not a declared CurrentBehavior", string(t.CurrentBehavior)))
	}
	if t.Owner == TriggerPointOwnerT107B {
		// A pending point has no call site, so there is no current behaviour to
		// compare its policy against: it is not-wired by definition, and a
		// not-wired point can never be a gap.
		if t.CurrentBehavior != BehaviorNotWired {
			return fail("CurrentBehavior", fmt.Sprintf("%q on a pending point: a point with no call site is %s", string(t.CurrentBehavior), BehaviorNotWired))
		}
		if t.GapClosure != "" {
			return fail("GapClosure", "set on a pending point: there is no behaviour to differ from")
		}
		return nil
	}
	if t.CurrentBehavior == BehaviorNotWired {
		return fail("CurrentBehavior", fmt.Sprintf("%q on an existing point: it has a call site, so it has a behaviour", string(BehaviorNotWired)))
	}
	if !t.Policy.CompatibleWith(t.CurrentBehavior) && strings.TrimSpace(t.GapClosure) == "" {
		return fail("GapClosure", fmt.Sprintf("policy %s is not satisfied by behaviour %s; the gap must name its closure step", t.Policy, t.CurrentBehavior))
	}
	if t.Policy.CompatibleWith(t.CurrentBehavior) && t.GapClosure != "" {
		return fail("GapClosure", "set on a point whose policy already matches its behaviour")
	}
	return nil
}

// triggerPoints 是冻结的触发点表。改动它必须同时改代码（或保留点的接线），
// 否则 trigger_points_test.go 会失败。
var triggerPoints = []TriggerPoint{
	{
		Hook:            HookBeforeWrite,
		Category:        TriggerCategoryBeforeWrite,
		Source:          ObservationSourceLegacyWorkspace,
		Observation:     "workspace_service_write",
		Site:            "internal/workspace/service.go:(*FSService).Write",
		Policy:          FailurePolicyReject,
		CurrentBehavior: BehaviorRejected,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note: "FSService.Write 在 guard.BeforeWrite 之前触发，hook 报错即 " +
			`"write blocked by before-write hook" 拒写；hook 返回的 content 可以改写待写内容。` +
			"这是服务端自己写入的唯一触发点，外部 CLI 工作副本的写入不经此处（见包注释的不重复计数规则）。",
	},
	{
		Hook:            HookBeforeSend,
		Category:        TriggerCategoryModelIO,
		Source:          ObservationSourceLegacySession,
		Observation:     "adapter_before_send",
		Site:            "internal/adapters/message_conversion.go:applyBeforeSendHooks",
		Policy:          FailurePolicyReject,
		CurrentBehavior: BehaviorRejected,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note: "applyBeforeSendHooks 把错误原样返回，六个调用点（claude.go 两处、gemini.go 两处、" +
			"openai.go 两处）都是 return nil, err：模型请求不会发出，所以现状就是 reject。" +
			"触发点经 triggerAdapterHook 转发（hook 类型是参数），扫描器按调用方的常量实参定 Site。",
	},
	{
		Hook:            HookPostResponse,
		Category:        TriggerCategoryModelIO,
		Source:          ObservationSourceLegacySession,
		Observation:     "adapter_post_response",
		Site:            "internal/adapters/message_conversion.go:notifyAdapterPostResponse",
		Policy:          FailurePolicyWarn,
		CurrentBehavior: BehaviorReturnedToCaller,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note: "响应已经拿到，hook 只是通知。非流式路径（claude.go/gemini.go/openai.go）把错误 " +
			"return 给调用方、让 Send 失败；流式路径（adapters/types.go 的 " +
			`"_ = notifyAdapterPostResponse(...)"）把错误丢掉。两条路径不一致，所以记 returned-to-caller。`,
		GapClosure: "T1.07.b：after 类点不得让已经完成的模型调用失败——两条路径统一为 warn（记录错误、" +
			"继续返回响应），并写 outbox warning。",
	},
	{
		Hook:            HookOnStream,
		Category:        TriggerCategoryModelIO,
		Source:          ObservationSourceLegacySession,
		Observation:     "adapter_stream_chunk",
		Site:            "internal/adapters/message_conversion.go:notifyAdapterStreamChunk",
		Policy:          FailurePolicyWarn,
		CurrentBehavior: BehaviorDiscarded,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note: `notifyAdapterStreamChunk 用 "_, _ =" 丢弃错误（hook 失败时该帧照常发出）。` +
			"manager.Trigger 仍会在中心记 HookEvent 与审计，但触发点自己没有留下任何 warning。",
		GapClosure: "T1.07.b：丢弃改成记录（outbox warning），操作照常——绝不因为 hook 失败丢帧或重发帧。",
	},
	{
		Hook:            HookBeforeCompress,
		Category:        TriggerCategoryContext,
		Source:          ObservationSourceLegacySession,
		Observation:     "summarize_before_compress",
		Site:            "internal/summarize/compressor.go:notifyBeforeCompressHook",
		Policy:          FailurePolicyWarn,
		CurrentBehavior: BehaviorLoggedWarn,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note: `出错打 "[WARN] summarize before-compress hook failed" 并返回原 payload 继续压缩：` +
			"现状与 warn 一致。",
	},
	{
		Hook:            HookOnUserInputSubmitted,
		Category:        TriggerCategoryInput,
		Source:          ObservationSourceLegacySession,
		Observation:     "ws_user_text_input",
		Site:            "internal/websocket/hub.go:(*Client).handleMessage",
		Policy:          FailurePolicyWarn,
		CurrentBehavior: BehaviorLoggedWarn,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note:            "只在 MsgTypeText 分支触发（心跳/订阅不触发），失败打 WARN，消息照常处理。",
	},
	{
		Hook:            HookOnMessageComplete,
		Category:        TriggerCategoryContext,
		Source:          ObservationSourceLegacySession,
		Observation:     "ws_message_complete",
		Site:            "internal/websocket/hub.go:notifyMessageCompleteHook",
		Policy:          FailurePolicyWarn,
		CurrentBehavior: BehaviorLoggedWarn,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note: "handleMessage 的每条消息处理完都触发一次（包括 pong/订阅）。" +
			"注意 internal/samg/service.go 有同名方法 (*SAMGService).HookOnMessageComplete，" +
			"它不是 hooks 管理器的调用、也不被本表计数（扫描器只认管理器接收者）。",
	},
	{
		Hook:            HookAfterExec,
		Category:        TriggerCategoryAfterExec,
		Source:          ObservationSourceLegacySession,
		Observation:     "commander_after_exec",
		Site:            "internal/commander/commander.go:notifyAfterExecHook",
		Policy:          FailurePolicyRetry,
		CurrentBehavior: BehaviorLoggedWarn,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note: "命令已经执行完，payload 是 ExecResult（含 exit code）。retry 只重跑 hook handler " +
			"（HookConfig.RetryCount，今天默认 0 即只跑一次），失败打 WARN 后照常返回——" +
			"绝不会重跑命令本身。",
	},
	{
		Hook:            HookRestoreState,
		Category:        TriggerCategoryStateRestore,
		Source:          ObservationSourceLegacySession,
		Observation:     "snapshot_restore_state",
		Site:            "internal/snapshot/service.go:(*InMemorySnapshotService).Restore",
		Policy:          FailurePolicyWarn,
		CurrentBehavior: BehaviorLoggedWarn,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note:            "恢复动作已经做完（payload 是 snapshot id），hook 失败打 WARN，Restore 照常返回结果。",
	},
	{
		Hook:            HookBeforeTaskExecute,
		Category:        TriggerCategoryTaskLifecycle,
		Source:          ObservationSourceLegacyPlanner,
		Observation:     "planner_task_before_execute",
		Site:            "internal/planner/memory_integration.go:emitGlobalTaskHook",
		Policy:          FailurePolicyWarn,
		CurrentBehavior: BehaviorReturnedToCaller,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note: "emitGlobalTaskHook 用 switch 分派四个 task hook，四条记录同 Site 不同 Hook。" +
			"触发点把错误返回给调用方；planner/service.go 的 emitTaskLifecycleHooks 打 WARN 后继续，" +
			"但触发点自己既不拒绝也不记录。",
		GapClosure: "T1.07.b：任务 hook 的失败在触发点记录（outbox warning）并继续；" +
			"不阻塞任务执行。调用方（planner）改成不依赖返回值。",
	},
	{
		Hook:            HookAfterTaskExecute,
		Category:        TriggerCategoryTaskLifecycle,
		Source:          ObservationSourceLegacyPlanner,
		Observation:     "planner_task_after_execute",
		Site:            "internal/planner/memory_integration.go:emitGlobalTaskHook",
		Policy:          FailurePolicyRetry,
		CurrentBehavior: BehaviorReturnedToCaller,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note:            "任务状态已经写进 plan，after 类点可以安全重跑 handler（retry），绝不重跑任务。",
		GapClosure:      "T1.07.b：同 HookBeforeTaskExecute——记录 warning 并继续，不把错误当任务失败。",
	},
	{
		Hook:            HookOnTaskFailure,
		Category:        TriggerCategoryTaskLifecycle,
		Source:          ObservationSourceLegacyPlanner,
		Observation:     "planner_task_failure",
		Site:            "internal/planner/memory_integration.go:emitGlobalTaskHook",
		Policy:          FailurePolicyRetry,
		CurrentBehavior: BehaviorReturnedToCaller,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note: "任务已经失败（payload 是 TaskFailureContext），观察者重跑是安全的；" +
			"绝不能因为 hook 失败改变任务的失败结论。",
		GapClosure: "T1.07.b：记录 warning 并继续；任务失败结论不受 hook 影响。",
	},
	{
		Hook:            HookOnTaskComplete,
		Category:        TriggerCategoryTaskLifecycle,
		Source:          ObservationSourceLegacyPlanner,
		Observation:     "planner_task_complete",
		Site:            "internal/planner/memory_integration.go:emitGlobalTaskHook",
		Policy:          FailurePolicyRetry,
		CurrentBehavior: BehaviorReturnedToCaller,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note:            "任务已经完成，观察者重跑安全；hook 失败不能把已完成的任务改回去。",
		GapClosure:      "T1.07.b：记录 warning 并继续。",
	},
	{
		Hook:            "",
		Category:        TriggerCategoryManual,
		Source:          ObservationSourceHTTPManual,
		Observation:     "http_manual_hook_trigger",
		Site:            "internal/api/handlers/hooks.go:TriggerHook",
		Policy:          FailurePolicyReject,
		CurrentBehavior: BehaviorRejected,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note: "HTTP 按名手动触发（TriggerHook）：hook 不存在 404，执行失败 500。" +
			"这里的“操作”就是触发本身，所以失败即 reject；Hook 为空是因为名字来自请求。",
	},
	{
		Hook:            "",
		Category:        TriggerCategoryManual,
		Source:          ObservationSourceIntegration,
		Observation:     "integration_invoke_hook",
		Site:            "internal/integration/service.go:(*InMemoryIntegrationService).Invoke",
		Policy:          FailurePolicyReject,
		CurrentBehavior: BehaviorRejected,
		Retained:        true,
		Owner:           TriggerPointOwnerExisting,
		Note: "集成按 manifest 的 HookName 触发：失败时审计记 OutcomeFailure 并 return nil, err，" +
			"调用被拒。Hook 为空（名字来自 manifest）。",
	},
	{
		Hook:            HookPreToolUse,
		Category:        TriggerCategoryToolPre,
		Source:          ObservationSourceCLIBackend,
		Observation:     "cli_tool_call",
		Site:            "internal/execbackend/hooks.go:<tbd>",
		Policy:          FailurePolicyReject,
		CurrentBehavior: BehaviorNotWired,
		Retained:        true,
		Owner:           TriggerPointOwnerT107B,
		Note: "保留点，今天没有调用点（本步不接线）。在工具请求进入 Guard 之前触发；hook 放行之后 " +
			"Guard 仍然执行，hook 的放行永远不能推翻 policy 的拒绝。拒绝即工具不执行。",
	},
	{
		Hook:            HookPostToolUse,
		Category:        TriggerCategoryToolPost,
		Source:          ObservationSourceCLIBackend,
		Observation:     "cli_tool_result",
		Site:            "internal/execbackend/hooks.go:<tbd>",
		Policy:          FailurePolicyWarn,
		CurrentBehavior: BehaviorNotWired,
		Retained:        true,
		Owner:           TriggerPointOwnerT107B,
		Note: "保留点，今天没有调用点。在结果净化（脱敏）之后触发，payload 带状态/退出码/脱敏输出引用，" +
			"绝不带原文；失败只记 warning，绝不重跑已经执行的工具。",
	},
	{
		Hook:            HookRunStart,
		Category:        TriggerCategoryRunStart,
		Source:          ObservationSourceCLIBackend,
		Observation:     "cli_run_start",
		Site:            "internal/execbackend/hooks.go:<tbd>",
		Policy:          FailurePolicyReject,
		CurrentBehavior: BehaviorNotWired,
		Retained:        true,
		Owner:           TriggerPointOwnerT107B,
		Note: "保留点，今天没有调用点。claim 之后（Run 处于 starting、attempt 已创建）、进程启动之前触发；" +
			"reject 即不启动进程（T1.07.c 的 TestBeforeHookDenyPreventsProcess）。" +
			"EventID 是 scheduler.claimed 事件 ID，身份需要 run+attempt+agent_revision。",
	},
	{
		Hook:            HookRunFinish,
		Category:        TriggerCategoryRunFinish,
		Source:          ObservationSourceCLIBackend,
		Observation:     "cli_run_finish",
		Site:            "internal/execbackend/hooks.go:<tbd>",
		Policy:          FailurePolicyWarn,
		CurrentBehavior: BehaviorNotWired,
		Retained:        true,
		Owner:           TriggerPointOwnerT107B,
		Note: "保留点，今天没有调用点。终态迁移之后触发（Status 必须是四个终态之一），" +
			"失败只记 warning；Run 的终态不因 hook 改变。",
	},
}

// TriggerPoints returns a copy of the frozen trigger-point table.
func TriggerPoints() []TriggerPoint {
	out := make([]TriggerPoint, len(triggerPoints))
	copy(out, triggerPoints)
	return out
}

// RetainedTriggerPoints returns the points kept in the 3.0 trigger set.
func RetainedTriggerPoints() []TriggerPoint {
	out := make([]TriggerPoint, 0, len(triggerPoints))
	for _, point := range triggerPoints {
		if point.Retained {
			out = append(out, point)
		}
	}
	return out
}

// PendingTriggerPoints returns the retained points that have no call site yet
// (Owner "T1.07.b"): they are the work the wiring step must pick up.
func PendingTriggerPoints() []TriggerPoint {
	out := make([]TriggerPoint, 0, len(triggerPoints))
	for _, point := range triggerPoints {
		if point.Owner == TriggerPointOwnerT107B {
			out = append(out, point)
		}
	}
	return out
}

// TriggerPointFor returns the row for one code site and hook type. The pair is
// the row's identity: a hook may legitimately have several rows (the four task
// hooks share one Site, the two manual triggers share the empty hook).
func TriggerPointFor(site string, hook HookType) (TriggerPoint, bool) {
	for _, point := range triggerPoints {
		if point.Site == site && point.Hook == hook {
			return point, true
		}
	}
	return TriggerPoint{}, false
}

// TriggerPointsForHook returns every row that triggers one hook type.
func TriggerPointsForHook(hook HookType) []TriggerPoint {
	out := make([]TriggerPoint, 0, 1)
	for _, point := range triggerPoints {
		if point.Hook == hook {
			out = append(out, point)
		}
	}
	return out
}

// BehaviorGap is one row where the declared failure policy and today's code
// disagree. A gap is not silently accepted: it names the step that closes it.
type BehaviorGap struct {
	Hook            HookType
	Site            string
	Policy          FailurePolicy
	CurrentBehavior CurrentBehavior
	Owner           string
	Closure         string
}

// BehaviorGaps returns the declared-policy/current-behaviour gaps, derived from
// the table (never hand-written), so a new or edited row cannot hide one.
func BehaviorGaps() []BehaviorGap {
	out := make([]BehaviorGap, 0, len(triggerPoints))
	for _, point := range triggerPoints {
		if point.GapClosure == "" {
			continue
		}
		out = append(out, BehaviorGap{
			Hook:            point.Hook,
			Site:            point.Site,
			Policy:          point.Policy,
			CurrentBehavior: point.CurrentBehavior,
			Owner:           TriggerPointOwnerT107B,
			Closure:         point.GapClosure,
		})
	}
	return out
}

// AllHookTypes returns every declared HookType in the declaration order of
// types.go (the 13 pre-3.0 types, then the four reserved ones). The equality
// with types.go is proved by TestAllHookTypesMatchTypesGo, which parses the
// constants out of that file.
func AllHookTypes() []HookType {
	out := make([]HookType, len(allHookTypes))
	copy(out, allHookTypes)
	return out
}

var allHookTypes = []HookType{
	HookBeforeSend,
	HookPostResponse,
	HookOnStream,
	HookBeforeCompress,
	HookOnMessageComplete,
	HookBeforeWrite,
	HookAfterExec,
	HookRestoreState,
	HookOnUserInputSubmitted,
	HookBeforeTaskExecute,
	HookAfterTaskExecute,
	HookOnTaskFailure,
	HookOnTaskComplete,
	HookPreToolUse,
	HookPostToolUse,
	HookRunStart,
	HookRunFinish,
}

// Valid reports whether h is one of the declared HookType constants. It is the
// closed-enum check the payload contracts use; a hook type that is not declared
// can never be triggered.
func (h HookType) Valid() bool {
	for _, known := range allHookTypes {
		if h == known {
			return true
		}
	}
	return false
}

// HookTypesWithTriggerPoint returns the hook types that appear in at least one
// trigger point, in AllHookTypes order.
func HookTypesWithTriggerPoint() []HookType {
	out := make([]HookType, 0, len(allHookTypes))
	for _, hook := range allHookTypes {
		if len(TriggerPointsForHook(hook)) > 0 {
			out = append(out, hook)
		}
	}
	return out
}
