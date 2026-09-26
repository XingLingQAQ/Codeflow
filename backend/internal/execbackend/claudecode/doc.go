// Package claudecode 实现 Claude Code 执行后端的只读能力探测与启动参数层
// （计划 §28 T1.13.a、§27.7；设计 codeflow-3.0-redesign.md §4.2）。
//
// 本包只做四件事，全部可离线测试：
//
//   - cli_facts.go：解析本机 `claude --version` / `claude --help` 的输出；
//   - capability.go：把解析出的事实推导成 execbackend.CapabilityReport（缺证据即 false）；
//   - launch.go：构造并校验非交互启动参数（argv）；
//   - probe.go：定位可执行文件、执行 --version/--help（只读）、净化子进程环境。
//
// 适配器本体（进程接线、stream-json 帧解析、钩子配置生成、审批 ID 映射）属于
// T1.13.b，不在本包。本包不依赖 execbackend 之外的任何 codeflow 包。
//
// # 入口裁定：CLI 非交互，不用 Agent SDK
//
// 只走 CLI 非交互入口 `claude -p`，不使用 Agent SDK。理由：后端是 Go；SDK 是
// TypeScript/Python 库，需要额外运行时与依赖；SDK 与 CLI 之间的宿主控制协议
// （control_request / can_use_tool）属于 SDK 内部实现，Go 侧无法诚实复用。
//
// 不混用两套启动参数：本包不实现任何 SDK 控制消息，argv 里永不出现
// --permission-prompt-tool、--permission-prompts host、--input-format、
// --replay-user-messages（见 launch.go 的禁用参数表）。
//
// # 策略闸门必须失败即拒绝（fail-closed）
//
// 下面三个参数是强制组合，任何选项都关不掉：
//
//   - --permission-mode dontAsk：需要询问的调用一律自动拒绝，只放行无需审批的只读
//     动作、permissions.allow 规则、PreToolUse 钩子明确批准的调用
//     （https://code.claude.com/docs/en/permission-modes.md
//     #allow-only-pre-approved-tools-with-dontask-mode）。
//   - --permission-prompts none：没有宿主应答，任何会弹询问的都拒绝
//     （https://code.claude.com/docs/en/headless.md
//     #turn-off-permission-prompts-in-unattended-runs，需 v2.1.259+）。
//   - --restricted：只加载 managed settings 与 --settings，仓库里不可信的
//     .claude/settings.json 不生效；文件工具限制在工作目录；拒绝 bypassPermissions
//     （https://code.claude.com/docs/en/cli-reference.md#cli-flags，需 v2.1.248+）。
//
// 为什么必须靠 dontAsk 兜底而不能只靠钩子：官方文档明确 PreToolUse 钩子超时不拦截
// （调用继续走正常权限流程，"don't count on a stalled hook to act as a gate"），
// 钩子起不来（路径错/127）、exit 1 无有效 JSON、JSON 不合 schema 同样都是非阻断错误，
// 动作继续（https://code.claude.com/docs/en/hooks.md#other-exit-codes 与
// #timeouts）；而 `-p` 模式下校验失败的 settings 文件被静默忽略
// （cli-reference.md 的 -p 说明）。只有 dontAsk + permission-prompts none 兜底，
// 才能把"钩子失效"变成"拒绝"而不是"放行"。
//
// # argv 规则
//
//   - 不允许任何位置参数：提示词只能由 T1.13.b 写入 stdin（纯文本，写完关闭）。
//     理由：不进进程列表、避开 Windows 32K 命令行上限；`--tools <tools...>` 与
//     `--add-dir <directories...>` 是变长参数，会吞掉后面的位置参数。
//   - 没有 ExtraArgs 之类的逃生口；参数由 LaunchSpec 逐个字段决定，Validate 拒绝
//     任何不合法取值。
//
// # 能力推导
//
// 某能力为真当且仅当：版本可解析且 ≥ MinSupportedVersion、capability.go 表中要求的
// help 事实全部存在、调用方传入的 implemented 集合包含它。其余能力（cancel_graceful、
// resume_checkpoint、inject、pty、sandbox）本步永远为 false，理由见 capability.go。
//
// 探测只执行 `<exe> --version` 与 `<exe> --help`：不发模型请求、不自动登录/安装、
// 不回显凭据；子进程环境走白名单（probe.go 的 ProbeEnv）。
package claudecode
