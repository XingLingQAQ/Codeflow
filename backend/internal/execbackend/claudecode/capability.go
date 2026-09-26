package claudecode

import (
	"fmt"
	"strings"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
)

// BackendName 是本后端的稳定标识。
//
// 必须与 backend/internal/bootstrap/readiness.go 的 execBackendNames 里的
// "claude_code" 一致，否则 /ready 的 exec_backend:claude_code 探针与 Run 创建判据
// 会指向不同名字。（execbackend.Backend 的接口注释里那个示例名 "claude-code"
// 只是举例，不是本后端标识；本包不改 execbackend 公共类型，故以本常量为准。）
const BackendName = "claude_code"

// MinSupportedVersion 是本包证明能力所需的最低 CLI 版本。
//
// 依据：--permission-prompts（choices 含 none）需要 v2.1.259+
// （https://code.claude.com/docs/en/headless.md
// #turn-off-permission-prompts-in-unattended-runs），而它是 fail-closed 闸门的
// 必要组成；--restricted 需要 v2.1.248+，早于 2.1.259，故取较高者。
const MinSupportedVersion = "2.1.259"

// minSupportedVersion 是 MinSupportedVersion 的解析形式。
var minSupportedVersion = NewVersion(2, 1, 259)

// SourceDescription 写入 CapabilityReport.Source，说明证据来自哪里。
const SourceDescription = "local `claude --version` and `claude --help` output on this machine, " +
	"cross-checked against the Claude Code docs snapshot 2026-09-26 (https://code.claude.com/docs/en/)"

// docsSnapshot 是文档快照日期，写入每条能力证据。
const docsSnapshot = "2026-09-26"

// 问题码（§27.7 能力探测）。
//
// Detail 只写事实与出处：不得包含环境变量值、凭据或子进程 stderr 正文。
const (
	// ProblemCLINotFound 找不到 claude 可执行文件（未配置且 PATH 上没有，或配置的路径不是常规文件）。
	ProblemCLINotFound = "cli_not_found"
	// ProblemCLIPathNotAbsolute 配置的可执行文件路径不是绝对路径。
	ProblemCLIPathNotAbsolute = "cli_path_not_absolute"
	// ProblemProbeFailed 探测命令非零退出或无法启动（Detail 写退出码）。
	ProblemProbeFailed = "probe_failed"
	// ProblemProbeTimeout 探测命令超过单次超时。
	ProblemProbeTimeout = "probe_timeout"
	// ProblemProbeOutputTooLarge 探测命令 stdout 超过上限。
	ProblemProbeOutputTooLarge = "probe_output_too_large"
	// ProblemVersionUnparsable `--version` 输出不是 `X.Y.Z (Claude Code)`。
	ProblemVersionUnparsable = "version_unparsable"
	// ProblemVersionUnsupported 版本低于 MinSupportedVersion。
	ProblemVersionUnsupported = "version_unsupported"
	// ProblemHelpUnparsable `--help` 输出里没有可用的 Options 段。
	ProblemHelpUnparsable = "help_unparsable"
	// ProblemRequiredFlagMissing 推导某能力所需的 flag 不在 help 里（Detail 写缺的 flag）。
	ProblemRequiredFlagMissing = "required_flag_missing"
	// ProblemRequiredChoiceMissing 推导某能力所需的取值不在 flag 的 choices 里。
	ProblemRequiredChoiceMissing = "required_choice_missing"
)

// Problem 是一条能力探测问题：Code 取上面的问题码，Detail 是脱敏说明。
type Problem struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// String 便于日志与测试输出。
func (p Problem) String() string { return p.Code + ": " + p.Detail }

// Facts 是一次只读探测得到的事实，是能力推导的全部输入。
//
// VersionErr/HelpErr 保留解析失败的原因（不改变推导结果：解析失败时对应事实不可用，
// 相关能力一律 false）。零值 Facts 表示"什么都没探测到"，所有能力为 false。
type Facts struct {
	// ExecutablePath 被探测可执行文件的绝对路径；空表示尚未定位到。
	ExecutablePath string
	// ProbedAt 探测时间，由注入时钟给出。
	ProbedAt time.Time
	// Version 解析出的版本；零值表示未知。
	Version Version
	// VersionErr `--version` 的解析错误；非空表示输出不可解析。
	VersionErr error
	// Flags 解析出的 help 事实；零值表示没有可用事实。
	Flags HelpFlags
	// HelpErr `--help` 的解析错误；非空表示输出不可解析。
	HelpErr error
}

// capabilityRule 描述一个能力为真所需的只读事实。
//
// 任一事实缺失 → 该能力为 false 并产出对应 Problem；事实齐全但调用方的 implemented
// 集合不含它 → 仍为 false，但不产出 Problem（那是适配器自己的实现范围，不是 CLI 缺陷）。
type capabilityRule struct {
	capability execbackend.Capability
	// flags 必须存在于 help 的 flag（长名；短别名也能命中）。
	flags []string
	// choices 这些 flag 必须声明的取值。
	choices map[string][]string
	// evidence 事实齐全时写入报告的证据串（含出处与快照日期）。
	evidence string
	// alwaysFalse 非空表示该能力本步永远为 false，这里写理由。
	alwaysFalse string
}

// capabilityRules 返回推导规则，顺序固定为：先六项可从 help 事实证明的能力，再五项本步
// 永远为 false 的能力（后者在报告里不写证据、不写问题，只是不出现）。
// 这个顺序同时决定 DeriveCapabilities 的 problems 顺序，测试依赖它保持稳定。
//
// 名单与文档出处：help 事实来自本机 `claude --help`（2.1.283）与
// docs snapshot 2026-09-26；工具白名单来自 docs/tools-reference.md。
func capabilityRules() []capabilityRule {
	streamJSONFlags := []string{"--output-format", "--verbose"}
	return []capabilityRule{
		{
			capability: execbackend.CapabilityNonInteractive,
			flags:      []string{"--print"},
			evidence: "`claude --help` Options lists `-p, --print` (non-interactive mode also skips " +
				"the workspace trust dialog); https://code.claude.com/docs/en/cli-reference.md#cli-flags " +
				"(snapshot " + docsSnapshot + ")",
		},
		{
			capability: execbackend.CapabilityJSONStream,
			flags:      streamJSONFlags,
			choices:    map[string][]string{"--output-format": {"stream-json"}},
			evidence: "`claude --help` Options: `--output-format` choices include stream-json and " +
				"`--verbose` exists (stream-json output requires --verbose); " +
				"https://code.claude.com/docs/en/headless.md#stream-responses (snapshot " + docsSnapshot + ")",
		},
		{
			capability: execbackend.CapabilityApprovalHook,
			flags:      []string{"--settings", "--permission-mode", "--permission-prompts", "--restricted"},
			choices: map[string][]string{
				"--permission-mode":    {"dontAsk"},
				"--permission-prompts": {"none"},
			},
			evidence: "`claude --help` Options: `--settings`, `--permission-mode` choices include " +
				"dontAsk, `--permission-prompts` choices include none, `--restricted`; PreToolUse hooks are " +
				"non-blocking on timeout/start failure/exit 1, so dontAsk+none is the gate; " +
				"https://code.claude.com/docs/en/permission-modes.md and " +
				"https://code.claude.com/docs/en/hooks.md#other-exit-codes (snapshot " + docsSnapshot + ")",
		},
		{
			capability: execbackend.CapabilityUsageTokens,
			flags:      streamJSONFlags,
			choices:    map[string][]string{"--output-format": {"stream-json"}},
			evidence: "stream-json result message carries per-step `usage` and cumulative usage " +
				"(SDKResultMessage); https://code.claude.com/docs/en/agent-sdk/typescript.md#sdkresultmessage " +
				"and https://code.claude.com/docs/en/agent-sdk/cost-tracking.md#understand-token-usage " +
				"(snapshot " + docsSnapshot + ")",
		},
		{
			capability: execbackend.CapabilityUsageCost,
			flags:      streamJSONFlags,
			choices:    map[string][]string{"--output-format": {"stream-json"}},
			evidence: "stream-json result message carries `total_cost_usd`, a client-side estimate " +
				"(quality=estimated, not billing data); " +
				"https://code.claude.com/docs/en/agent-sdk/cost-tracking.md (snapshot " + docsSnapshot + ")",
		},
		{
			capability: execbackend.CapabilityMCP,
			flags:      []string{"--mcp-config", "--strict-mcp-config"},
			evidence: "`claude --help` Options lists `--mcp-config` and `--strict-mcp-config` " +
				"(S1 ships no MCP server: --strict-mcp-config with no --mcp-config keeps repository " +
				".mcp.json out); https://code.claude.com/docs/en/cli-reference.md#cli-flags " +
				"(snapshot " + docsSnapshot + ")",
		},
		{
			capability: execbackend.CapabilityCancelGraceful,
			alwaysFalse: "缺席：取消语义（先 grace 后 force）需要真实进程证据（T1.13.c 的 smoke），" +
				"本步只做只读探测，不启动进程，因此没有可证明的事实",
		},
		{
			capability: execbackend.CapabilityResumeCheckpoint,
			alwaysFalse: "缺席：`--resume`/`--session-id` 只是会话续接，不等于已验证 checkpoint 恢复" +
				"（§21.1 paused → running 需要 checkpoint 有界重放证据，归 T1.13.b/c）",
		},
		{
			capability: execbackend.CapabilityInject,
			alwaysFalse: "缺席：S1 用文本 stdin 单次写入，不开流式输入" +
				"（--input-format stream-json 属 SDK 入口，本包混用禁令），运行中注入无从证明",
		},
		{
			capability: execbackend.CapabilityPTY,
			alwaysFalse: "缺席：本后端用管道（pipes）而不是 PTY，没有 PTY 证据，" +
				"接口注释也说明 PTY 是可选能力且默认走 pipes",
		},
		{
			capability: execbackend.CapabilitySandbox,
			alwaysFalse: "缺席：`--restricted` 只限制工具与设置来源，不等于平台沙箱隔离；" +
				"§27.5.7 要求证明不了隔离就不算，真实沙箱探针归 T2.04",
		},
	}
}

// DeriveCapabilities 把只读探测事实推导成能力报告与问题列表（§27.7）。
//
// 某能力为真当且仅当三个条件同时成立：
//
//  1. 版本可解析且 ≥ MinSupportedVersion；
//  2. capabilityRules 中该能力要求的 help 事实（flag 存在、choices 含指定取值）全部成立；
//  3. implemented 集合包含该能力（本步不定义适配器的实现集合，由 T1.13.b 传入）。
//
// 版本或 help 不可解析时提前返回：报告 Source 仍会写入（说明证据本应来自本机探测），
// 但 Evidence 为空，因此 CapabilityReport.Has 对任何能力都返回 false。
//
// 问题顺序固定：先版本问题，再 help 问题，最后按能力规则顺序逐个列出缺的 flag 与 choice。
// 产物里的 Backend/ExecutablePath/ProbedAt 直接取 Facts，调用方可原样上报。
func DeriveCapabilities(f Facts, implemented []execbackend.Capability) (execbackend.CapabilityReport, []Problem) {
	report := execbackend.CapabilityReport{
		Backend:        BackendName,
		ExecutablePath: f.ExecutablePath,
		Version:        f.Version.String(),
		Source:         SourceDescription,
		ProbedAt:       f.ProbedAt,
		Evidence:       map[execbackend.Capability]string{},
	}

	if f.VersionErr != nil || !f.Version.Valid() {
		detail := "`claude --version` output is not `X.Y.Z (Claude Code)`"
		if f.VersionErr != nil {
			detail = sanitizeParseError(f.VersionErr)
		}
		return report, []Problem{{Code: ProblemVersionUnparsable, Detail: detail}}
	}
	if !f.Version.AtLeast(minSupportedVersion) {
		return report, []Problem{{
			Code: ProblemVersionUnsupported,
			Detail: fmt.Sprintf("claude version %s is below the minimum supported version %s",
				f.Version.String(), MinSupportedVersion),
		}}
	}
	if f.HelpErr != nil || len(f.Flags.Order) == 0 {
		detail := "`claude --help` output has no usable `Options:` section"
		if f.HelpErr != nil {
			detail = sanitizeParseError(f.HelpErr)
		}
		return report, []Problem{{Code: ProblemHelpUnparsable, Detail: detail}}
	}

	var problems []Problem
	for _, rule := range capabilityRules() {
		if rule.alwaysFalse != "" {
			continue
		}
		missingFlags, missingChoices := missingFacts(rule, f.Flags)
		for _, flag := range missingFlags {
			problems = append(problems, Problem{
				Code: ProblemRequiredFlagMissing,
				Detail: fmt.Sprintf("%s: %s is missing from `claude --help` (v%s)",
					rule.capability, flag, f.Version.String()),
			})
		}
		for _, mc := range missingChoices {
			problems = append(problems, Problem{
				Code: ProblemRequiredChoiceMissing,
				Detail: fmt.Sprintf("%s: %s choices do not include %q (v%s)",
					rule.capability, mc.flag, mc.choice, f.Version.String()),
			})
		}
		if len(missingFlags) > 0 || len(missingChoices) > 0 {
			continue
		}
		if !hasCapability(implemented, rule.capability) {
			continue
		}
		report.Evidence[rule.capability] = rule.evidence
	}
	return report, problems
}

// missingChoice 记录一个缺失的 choice 及其所属 flag。
type missingChoice struct {
	flag   string
	choice string
}

// missingFacts 返回规则里缺失的 flag 与 choice（顺序稳定：先 flags 后 choices，按声明顺序）。
func missingFacts(rule capabilityRule, flags HelpFlags) ([]string, []missingChoice) {
	var missingFlags []string
	for _, name := range rule.flags {
		if !flags.Has(name) {
			missingFlags = append(missingFlags, name)
		}
	}
	var missingChoices []missingChoice
	for _, name := range rule.flags {
		for _, choice := range rule.choices[name] {
			if !flags.HasChoice(name, choice) {
				missingChoices = append(missingChoices, missingChoice{flag: name, choice: choice})
			}
		}
	}
	return missingFlags, missingChoices
}

// hasCapability 报告 implemented 是否包含 c（重复与乱序不影响结果）。
func hasCapability(implemented []execbackend.Capability, c execbackend.Capability) bool {
	for _, item := range implemented {
		if item == c {
			return true
		}
	}
	return false
}

// sanitizeParseError 把解析错误压成单行脱敏文本，用作 Problem.Detail。
func sanitizeParseError(err error) string {
	if err == nil {
		return ""
	}
	return quoteSnippet(strings.ReplaceAll(err.Error(), "\n", " "))
}
