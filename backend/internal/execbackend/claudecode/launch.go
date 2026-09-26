package claudecode

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/codeflow/backend/internal/execbackend"
)

// ---------------------------------------------------------------------------
// 工具白名单
// ---------------------------------------------------------------------------

// allowedTools 是 T1.13 允许通过 `--tools` 开放的内置工具全集（精确名字）。
//
// 只保留"在受控工作目录内读写代码"需要的工具，逐类排除并在下面写明理由：
//
//   - 联网：WebFetch / WebSearch —— 执行期不得访问外部网络（§27.5 隔离与隐私边界）。
//   - 子 Agent：Agent —— 会绕过父 Run 的预算、事件与审批 ID 映射。
//   - 对外发布：Artifact / SendUserFile / ShareOnboardingGuide / PushNotification /
//     RemoteTrigger / SendFeedback —— 对外产生副作用，不能由后台 Run 触发。
//   - 后台/定时：CronCreate / CronDelete / CronList / ScheduleWakeup / Monitor / Workflow
//     —— 在 Run 生命周期之外继续执行，无法归属到一次 Run。
//   - 切换目录：EnterWorktree / ExitWorktree —— 会把文件工具带出受控工作目录。
//   - 需要人应答：AskUserQuestion / EnterPlanMode / ExitPlanMode —— dontAsk 下必然被拒，
//     放进来只会浪费一次被拒的工具调用。
//
// 名单来自 docs/tools-reference.md（快照 2026-09-26）。
var allowedTools = []string{
	"Read",
	"Edit",
	"Write",
	"Glob",
	"Grep",
	"Bash",
	"PowerShell",
	"NotebookEdit",
}

// AllowedTools 返回工具白名单的副本，供 T1.13.b 生成 settings 与做 UI 展示。
//
// 返回副本是刻意的：调用方改自己的切片不会污染本包的白名单判据。
func AllowedTools() []string { return append([]string(nil), allowedTools...) }

// looksLikePermissionRule 识别 `Bash(git *)`、`Read(~/secrets/**)` 这类权限规则写法。
//
// `--tools` 只接受精确的内置工具名；把权限规则写进工具名既不会生效，还会掩盖"白名单
// 被当成放行规则"的错误，因此在 Validate 里显式拒绝。
var looksLikePermissionRule = regexp.MustCompile(`[()*?\[\]|]`)

// ---------------------------------------------------------------------------
// 禁用参数表
// ---------------------------------------------------------------------------

// BannedArg 是一条禁用参数及其理由；Args 永不输出这些参数。
type BannedArg struct {
	// Name 参数名（带前缀，短别名单独列一条）。
	Name string
	// Reason 为什么禁用；说明它绕过了哪一层闸门。
	Reason string
}

// bannedArgs 是禁用参数表（§28 T1.13.a 裁定 4）。
//
// 别名分开列：表里每一项都要能被测试逐个断言"不出现在任意合法 spec 的 Args 里"。
// 放行/拒绝规则不走 argv（由 T1.13.b 生成的 settings 负责），所以 --allowedTools /
// --disallowedTools 也在禁用之列；--permission-mode 只允许 dontAsk，见 PermissionModes。
var bannedArgs = []BannedArg{
	{Name: "--dangerously-skip-permissions", Reason: "绕过全部权限检查，直接废掉 fail-closed 闸门"},
	{Name: "--allow-dangerously-skip-permissions", Reason: "让 bypassPermissions 成为可用选项，绕过闸门的入口"},
	{Name: "--bare", Reason: "最小模式跳过 settings/插件定义的钩子，PreToolUse 守卫会整体失效"},
	{Name: "--safe-mode", Reason: "同上：禁用钩子与自定义配置，守卫不再运行"},
	{Name: "--permission-prompt-tool", Reason: "SDK 宿主回问通道（MCP 工具应答询问）；本包只走 CLI 入口，不混用两套启动参数"},
	{Name: "--input-format", Reason: "stream-json 流式输入属于 SDK 入口；S1 只写一次纯文本 stdin"},
	{Name: "--replay-user-messages", Reason: "SDK 流式输入的应答语义；同上"},
	{Name: "-c", Reason: "续接最近会话：Run 必须用固定 --session-id，不能复用人类交互会话"},
	{Name: "--continue", Reason: "同上"},
	{Name: "-r", Reason: "恢复会话：会话续接不等于已验证 checkpoint 恢复，且会带进旧 transcript"},
	{Name: "--resume", Reason: "同上"},
	{Name: "--fork-session", Reason: "配合 --resume/--continue，属人类交互续接路径"},
	{Name: "--from-pr", Reason: "从 PR 恢复会话；外部状态进入执行上下文"},
	{Name: "--teleport", Reason: "恢复 teleport 会话；同上"},
	{Name: "--ide", Reason: "连接 IDE：引入本机交互环境，不属于受控 Run"},
	{Name: "--chrome", Reason: "浏览器集成：额外外部通道"},
	{Name: "--tmux", Reason: "tmux 会话：让进程脱离 supervisor 的直接管辖"},
	{Name: "-w", Reason: "git worktree：工作目录由 CodeFlow 决定，不走 CLI 自建"},
	{Name: "--worktree", Reason: "同上"},
	{Name: "--bg", Reason: "后台会话：Run 必须由 process supervisor 拥有"},
	{Name: "--background", Reason: "同上"},
	{Name: "--remote-control", Reason: "远程控制通道：执行期不得接受外部驱动"},
	{Name: "--cloud", Reason: "云会话：沙箱与执行位置逃出本机受控边界"},
	{Name: "--environment", Reason: "自托管云环境：同上"},
	{Name: "--agents", Reason: "内联自定义 Agent：第二条模型调用路径，绕过预算与审批映射"},
	{Name: "--agent", Reason: "同上"},
	{Name: "--plugin-dir", Reason: "加载插件：插件可带自己的钩子与命令，绕过 settings 冻结"},
	{Name: "--plugin-url", Reason: "远程插件：还会产生联网下载"},
	{Name: "--file", Reason: "启动时下载文件资源：联网副作用，不经工具审批"},
	{Name: "--client-data-url", Reason: "远程配置文档：退出条件受外部内容控制"},
	{Name: "--betas", Reason: "临时 beta 头：让协议行为随 flag 漂移，能力证据失效"},
	{Name: "--mcp-config", Reason: "S1 不接 MCP；接入要走独立的能力探测与审批映射"},
	{Name: "--allowedTools", Reason: "放行规则由 T1.13.b 生成的 settings 负责，不走 argv"},
	{Name: "--allowed-tools", Reason: "同上"},
	{Name: "--disallowedTools", Reason: "拒绝规则同上"},
	{Name: "--disallowed-tools", Reason: "同上"},
	{Name: "--system-prompt", Reason: "覆盖系统提示词：冻结规则的注入方式归 T1.13.b 裁定"},
	{Name: "--append-system-prompt", Reason: "同上"},
}

// BannedArgs 返回禁用参数表（副本），供测试与文档化使用。
func BannedArgs() []BannedArg { return append([]BannedArg(nil), bannedArgs...) }

// AllowedPermissionMode 是唯一允许的 `--permission-mode` 取值。
//
// 其他取值一律不出现在 argv：acceptEdits/auto/manual/plan 都会在需要判定时等待询问，
// bypassPermissions 直接绕过全部检查。dontAsk 让"需要询问"变成"自动拒绝"
// （https://code.claude.com/docs/en/permission-modes.md
// #allow-only-pre-approved-tools-with-dontask-mode）。
const AllowedPermissionMode = "dontAsk"

// PermissionPromptsTarget 是唯一允许的 `--permission-prompts` 取值："host" 表示等宿主
// 应答（本后端没有宿主应答通道，会挂住），"none" 才是不等任何人、无法解决的一律拒绝。
const PermissionPromptsTarget = "none"

// ---------------------------------------------------------------------------
// 启动参数
// ---------------------------------------------------------------------------

// uuidPattern 是标准 UUID 形状：8-4-4-4-12 十六进制（不限制版本位）。
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// modelPattern 限制 --model 取值字符集：首字符必须是字母或数字，其余为字母数字与 `._:-[]`。
//
// 反例（会被拒绝）：含空白、`;`、`$()`、反引号、`&`、`|`、`>`、引号的字符串；以及以 `-`
// 开头的取值——否则 `--model --dangerously-skip-permissions` 会把一个禁用参数原样送进
// argv，是否被当成模型名全看 CLI 解析器的实现，不能拿来当闸门。
// 允许 `[1m]` 这类窗口标注，因此不禁止方括号。
var modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:\[\]-]*$`)

// LaunchSpec 是一次非交互启动所需的全部输入。
//
// 字段与 argv 一一对应：没有 ExtraArgs 之类的逃生口，任何新参数都必须先在这里有字段、
// 有 Validate 规则、有测试。
//
// 校验顺序：先 Validate 再 Args。Args 假设刚刚 Validate 通过，不做任何校验。
type LaunchSpec struct {
	// Executable claude 可执行文件绝对路径（由 ResolveExecutable 定位）。
	Executable string
	// SettingsPath T1.13.b 生成的 settings JSON 绝对路径（--settings）。
	// 只加载 managed settings 与它：--restricted 让仓库里的 .claude/settings.json 不生效。
	SettingsPath string
	// Tools `--tools` 的工具名列表：必须非空、属于 AllowedTools、不重复、精确名字。
	// 逗号连接后作为单个参数传出。
	Tools []string
	// SessionID 本次 Run 的会话 ID，标准 UUID；`--session-id` 固定下来，
	// 便于事件与恢复路径按 ID 对账。
	SessionID string
	// Model 可选模型名；空表示用 CLI 默认。
	Model string
	// AddDirs 额外开放给文件工具的绝对目录，每个目录一对 `--add-dir <dir>`。
	AddDirs []string
	// IncludeHookEvents 输出钩子生命周期事件（需要 --output-format stream-json）。
	IncludeHookEvents bool
	// IncludePartialMessages 输出流式增量消息（同上）。
	IncludePartialMessages bool
	// Persist 是否保留 CLI 自己的会话副本。
	//
	// 零值 false 表示不保留，即默认带上 `--no-session-persistence`：Run 的会话状态由
	// CodeFlow 自己记录，不在 ~/.claude 留副本。置 true 才去掉该参数。
	Persist bool
}

// Validate 校验 spec 是否可用于启动，无副作用。
//
// 失败返回 Code=execbackend.CodeInvalidRequest 的 *execbackend.Error，Field 指出字段：
// executable、settings_path、tools、session_id、model、add_dirs。
// 消息只回显调用方自己传入的值，不涉及环境变量或凭据。
func (s LaunchSpec) Validate() error {
	if !isAbsolutePath(s.Executable) {
		return execbackend.NewInvalidRequest("executable",
			"executable must be an absolute path to the claude CLI")
	}
	if !isAbsolutePath(s.SettingsPath) {
		return execbackend.NewInvalidRequest("settings_path",
			"settings_path must be an absolute path to the generated settings JSON")
	}
	if err := validateTools(s.Tools); err != nil {
		return err
	}
	if !uuidPattern.MatchString(s.SessionID) {
		return execbackend.NewInvalidRequest("session_id",
			"session_id must be a canonical UUID (8-4-4-4-12 hex digits)")
	}
	if s.Model != "" && !modelPattern.MatchString(s.Model) {
		return execbackend.NewInvalidRequest("model",
			"model must start with a letter or digit and may only contain letters, digits and . _ : - [ ]")
	}
	for _, dir := range s.AddDirs {
		if !isAbsolutePath(dir) {
			return execbackend.NewInvalidRequest("add_dirs",
				"every --add-dir entry must be an absolute path: "+dir)
		}
	}
	return nil
}

// Args 返回启动 argv（固定顺序，逐元素可断言）。
//
// 顺序（方括号表示可选，省略时整对参数消失）：
//
//	-p
//	--output-format stream-json
//	--verbose
//	--restricted
//	--permission-mode dontAsk
//	--permission-prompts none
//	--settings <SettingsPath>
//	--strict-mcp-config
//	--tools <逗号连接的工具名>
//	--session-id <SessionID>
//	[--model <Model>]
//	[--add-dir <dir>]...
//	[--include-hook-events]
//	[--include-partial-messages]
//	[--no-session-persistence]
//
// 三个硬闸门（--restricted、--permission-mode dontAsk、--permission-prompts none）在
// 任何 spec 下都出现，没有开关能关掉，见包注释。
//
// 前置条件：spec 必须已通过 Validate；本方法不做校验，也不接受额外参数（没有逃生口）。
// argv 里没有任何位置参数：提示词由 T1.13.b 写 stdin。
func (s LaunchSpec) Args() []string {
	args := make([]string, 0, 20+2*len(s.AddDirs))
	args = append(args,
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--restricted",
		"--permission-mode", AllowedPermissionMode,
		"--permission-prompts", PermissionPromptsTarget,
		"--settings", s.SettingsPath,
		"--strict-mcp-config",
		"--tools", strings.Join(s.Tools, ","),
		"--session-id", s.SessionID,
	)
	if s.Model != "" {
		args = append(args, "--model", s.Model)
	}
	for _, dir := range s.AddDirs {
		args = append(args, "--add-dir", dir)
	}
	if s.IncludeHookEvents {
		args = append(args, "--include-hook-events")
	}
	if s.IncludePartialMessages {
		args = append(args, "--include-partial-messages")
	}
	if !s.Persist {
		args = append(args, "--no-session-persistence")
	}
	return args
}

// validateTools 校验工具白名单。
func validateTools(tools []string) error {
	if len(tools) == 0 {
		return execbackend.NewInvalidRequest("tools", "tools must not be empty")
	}
	seen := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		if tool == "" {
			return execbackend.NewInvalidRequest("tools", "tool name must not be empty")
		}
		if looksLikePermissionRule.MatchString(tool) {
			return execbackend.NewInvalidRequest("tools",
				"tools must be exact built-in tool names, not permission rules: "+tool)
		}
		if _, ok := seen[tool]; ok {
			return execbackend.NewInvalidRequest("tools", "duplicate tool name: "+tool)
		}
		seen[tool] = struct{}{}
		if !isAllowedTool(tool) {
			return execbackend.NewInvalidRequest("tools", "unknown tool name: "+tool)
		}
	}
	return nil
}

// isAllowedTool 报告 name 是否在白名单里（精确匹配，大小写敏感）。
func isAllowedTool(name string) bool {
	for _, allowed := range allowedTools {
		if allowed == name {
			return true
		}
	}
	return false
}

// isAbsolutePath 报告 p 是否是非空绝对路径。
func isAbsolutePath(p string) bool {
	return p != "" && filepath.IsAbs(p)
}
