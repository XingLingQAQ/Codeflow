// Package process 定义外部进程的所有权、身份、取消、等待与输出模型，并提供不依赖
// 业务包的“进程身份”原语（§15 T1.08、§27.5 第 6 条）。
//
// 本包只依赖标准库、golang.org/x/sys 与 policy（策略闸控）。T1.08.b 已实现
// Supervisor/Handle/Spool：Start 的闸控顺序、Windows Job Object 与 Unix 进程组绑定、
// 软/强取消与升级、管道持续排空到有界 spool、退出终态与 marker（见 start.go、
// spool.go、tree_*.go）。实时按行/按帧输出流与 spool 脱敏属于后续步骤，不在此列。
//
// 硬性约定：
//   - Start 必须先过策略闸控（OperationProcessStart），闸控失败不得创建进程。
//   - 进程树必须绑定到 OS 原语（Windows Job Object / Unix 进程组），不能只 kill
//     直接子进程——否则脱离的子孙会变成孤儿。
//   - PID 不能单独作为身份：必须同时匹配启动时间标记（Identity.StartToken）与
//     owner（Ownership），判定顺序是 Identity.OwnedBy + Verify。
//   - 输出由 supervisor 自己持续排空到有界 spool；UI 慢不得停止读管道，否则子
//     进程写满管道缓冲区后死锁。
//   - Cancel 幂等；soft 超过 GracePeriod 必须自动升级为 force。
//   - Wait 可重复调用，并返回同一结果。
//   - owner 未确认（OwnedBy 为假或 Verify 不是 same）时不得 kill。
package process

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// 零值语义：Spec 里的 GracePeriod / MaxSpoolBytes 为 0 表示“采用默认值”，
// 负值一律拒绝。
const (
	// DefaultGracePeriod 是软终止到强制终止之间的默认宽限期（§27.6：grace cancel 5s）。
	DefaultGracePeriod = 5 * time.Second

	// DefaultMaxSpoolBytes 是输出 spool 的默认字节上限（8 MiB）。
	DefaultMaxSpoolBytes int64 = 8 << 20
)

// Ownership 标识一次执行的归属：Run、Attempt 与持有该进程的后端实例。
//
// 三项都必填：PID 只说明“哪个进程”，说明不了“谁的进程”。后端重启、多个实例共用
// 一台机器、或 lease 过期后换了 worker 时，只有 owner instance 能证明这个 PID 仍
// 归本次执行所有（§27.5 第 6 条）。
type Ownership struct {
	RunID         string `json:"run_id"`
	AttemptID     string `json:"attempt_id"`
	OwnerInstance string `json:"owner_instance"`
}

// Validate 报告 Ownership 是否足以建立进程归属。空串或纯空白都算缺失。
func (o Ownership) Validate() error {
	switch {
	case strings.TrimSpace(o.RunID) == "":
		return errors.New("process: ownership run id is empty")
	case strings.TrimSpace(o.AttemptID) == "":
		return errors.New("process: ownership attempt id is empty")
	case strings.TrimSpace(o.OwnerInstance) == "":
		return errors.New("process: ownership owner instance is empty")
	}
	return nil
}

// Identity 是一个进程在操作系统层面的身份快照。
//
// StartToken 是平台相关的不透明启动时间标记（见 CaptureIdentity），落库到
// attempts.process_start_id。同一个 PID 在进程退出后被系统复用给新进程时，
// StartToken 必然不同——这是“防误杀”的唯一依据。
type Identity struct {
	PID        int       `json:"pid"`
	StartToken string    `json:"start_token"`
	Owner      Ownership `json:"owner"`
	CapturedAt time.Time `json:"captured_at"`
}

// Validate 报告 Identity 是否可用来做身份比对。CapturedAt 不参与校验：历史记录
// 可能没有精确时间戳，但 PID/标记/owner 缺一不可。
func (id Identity) Validate() error {
	if id.PID <= 0 {
		return fmt.Errorf("process: identity pid %d is not positive", id.PID)
	}
	if strings.TrimSpace(id.StartToken) == "" {
		return errors.New("process: identity start token is empty")
	}
	return id.Owner.Validate()
}

// OwnedBy 报告该身份是否声称由 owner 持有。这是纯比较，不触碰操作系统；调用方必须
// 先 OwnedBy 再 Verify，两者都通过才允许把 PID 当作“自己的进程”处理（kill、写状态、
// 判失败），否则只能记为 lost 并放弃（§27.5 第 6 条）。
func (id Identity) OwnedBy(owner Ownership) bool { return id.Owner == owner }

// Spec 描述一次受监督的进程启动请求。
type Spec struct {
	// Path 是可执行文件的绝对路径。不经过 shell 解释——命令注入在这里被从结构上
	// 排除，参数一律通过 Args 直接传递。
	Path string
	// Args 是传给可执行文件的参数（不含 argv[0]）。
	Args []string
	// Dir 是子进程的工作目录，必须是已清理的绝对路径。越界检查属于上层
	// （workspace/policy）；supervisor 只保证不做相对路径的意外解析。
	Dir string
	// Env 是显式白名单。nil 表示空环境，不继承父进程——继承会把后端进程的凭据、
	// 代理和 PATH 泄漏给子进程。需要什么就显式列出（Windows 上启动进程通常还需要
	// SystemRoot）。
	Env []string
	// Owner 是执行归属，必填。
	Owner Ownership
	// GracePeriod 是 soft 取消后等待进程自行退出的时长；0 表示 DefaultGracePeriod。
	// 超时后 supervisor 必须升级为 force。
	GracePeriod time.Duration
	// MaxSpoolBytes 是输出 spool 保留的字节上限；0 表示 DefaultMaxSpoolBytes。
	// 超出部分被丢弃但计入 Exit.SpoolTruncatedBytes，不能静默截断。
	MaxSpoolBytes int64
	// MarkerPath 是可选的进程身份落盘位置（绝对路径）。非空时 supervisor 在启动后
	// 原子写入 identity/owner/path/参数个数/started_at，退出时原子更新
	// reason/code/forced/finished_at——后端崩溃后，marker 是“这个 PID 属于谁”的
	// 现场凭证。绝不写入 Env 的值或参数内容（可能含凭据）。
	MarkerPath string
}

// Validate 校验 Spec 的可用性。它不做文件系统访问（路径存在性、是否可执行、是否
// 越界属于 Start 的职责）：这里只拒绝那些“无论环境如何都必然错误”的输入。
func (s Spec) Validate() error {
	if err := validateAbsCleanPath("path", s.Path); err != nil {
		return err
	}
	for i, arg := range s.Args {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("process: spec arg %d contains NUL", i)
		}
	}
	if err := validateAbsCleanPath("dir", s.Dir); err != nil {
		return err
	}
	if err := validateEnv(s.Env); err != nil {
		return err
	}
	if err := s.Owner.Validate(); err != nil {
		return err
	}
	if s.GracePeriod < 0 {
		return fmt.Errorf("process: spec grace period %s is negative", s.GracePeriod)
	}
	if s.MaxSpoolBytes < 0 {
		return fmt.Errorf("process: spec max spool bytes %d is negative", s.MaxSpoolBytes)
	}
	if s.MarkerPath != "" {
		if err := validateAbsCleanPath("marker path", s.MarkerPath); err != nil {
			return err
		}
	}
	return nil
}

// validateAbsCleanPath 拒绝空、含 NUL、非绝对或未清理的路径。“未清理”是硬要求：
// `a/../b` 这类路径在闸控时校验的位置和在系统调用时解析的位置可能不是同一个，
// 必须让调用方交出唯一形式。
func validateAbsCleanPath(field, path string) error {
	switch {
	case strings.TrimSpace(path) == "":
		return fmt.Errorf("process: spec %s is empty", field)
	case strings.ContainsRune(path, 0):
		return fmt.Errorf("process: spec %s contains NUL", field)
	case !filepath.IsAbs(path):
		return fmt.Errorf("process: spec %s %q is not absolute", field, path)
	}
	if cleaned := filepath.Clean(path); cleaned != path {
		return fmt.Errorf("process: spec %s %q is not cleaned (want %q)", field, path, cleaned)
	}
	return nil
}

// validateEnv 要求每一项都是 KEY=VALUE、键非空、无重复。Windows 上环境变量名不
// 区分大小写，因此重复判定在 Windows 上忽略大小写。
func validateEnv(env []string) error {
	seen := make(map[string]string, len(env))
	for i, kv := range env {
		if strings.ContainsRune(kv, 0) {
			return fmt.Errorf("process: spec env %d contains NUL", i)
		}
		key, _, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			return fmt.Errorf("process: spec env %d is not KEY=VALUE: %q", i, kv)
		}
		norm := normalizeEnvKey(key)
		if prev, dup := seen[norm]; dup {
			return fmt.Errorf("process: spec env %d repeats key %q (already %q)", i, key, prev)
		}
		seen[norm] = kv
	}
	return nil
}

// normalizeEnvKey 让 Windows 上仅大小写不同的同名变量被判为重复。
func normalizeEnvKey(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}

// CancelMode 是取消的强度。
type CancelMode string

const (
	// CancelSoft 请求优雅终止（Unix SIGTERM / Windows CTRL_BREAK 等软信号）。
	CancelSoft CancelMode = "soft"
	// CancelForce 直接强制终止整棵进程树（Unix SIGKILL / Windows Job 终止）。
	CancelForce CancelMode = "force"
)

// Valid 报告 m 是否是可识别的取消模式。
func (m CancelMode) Valid() bool { return m == CancelSoft || m == CancelForce }

// ExitReason 是一次执行的终态原因。
type ExitReason string

const (
	// ExitExited：进程自行退出（含非零退出码），Code 有值。
	ExitExited ExitReason = "exited"
	// ExitCancelled：soft 取消后进程退出，Code 有值。
	ExitCancelled ExitReason = "cancelled"
	// ExitKilled：force 取消（或 soft 超时升级）后进程被强制终止。
	ExitKilled ExitReason = "killed"
	// ExitTimeout：超过上层给的期限被终止。
	ExitTimeout ExitReason = "timeout"
	// ExitLost：无法确认进程终态（例如后端重启后身份校验失败）。绝不能把这种
	// 情况当成“已退出”来释放资源。
	ExitLost ExitReason = "lost"
)

// Valid 报告 r 是否是可识别的退出原因。
func (r ExitReason) Valid() bool {
	switch r {
	case ExitExited, ExitCancelled, ExitKilled, ExitTimeout, ExitLost:
		return true
	}
	return false
}

// Exit 是一次进程执行的终态。
type Exit struct {
	// Code 是退出码；被强制终止或身份丢失时为 nil（未知，不要当成 0）。
	Code *int `json:"code,omitempty"`
	// Reason 说明为什么结束。
	Reason ExitReason `json:"reason"`
	// SpoolTruncatedBytes 是因超过 MaxSpoolBytes 而被丢弃的输出字节数，
	// 口径是 stdout 与 stderr 两路截断之和（两路各自独立计上限）。
	SpoolTruncatedBytes int64 `json:"spool_truncated_bytes"`
	// Forced 报告这次结束是否用到了强制终止：CancelForce 直接强杀，或 soft 超过
	// GracePeriod 后的升级。Reason 仍是 cancelled——调用方靠 Forced 区分“优雅退出”
	// 与“被强杀”，两者对上层（是否要清理工作区、是否要重试）含义不同。
	Forced bool `json:"forced"`
}

// Supervisor 创建并监督外部进程。
type Supervisor interface {
	// Start 在通过策略闸控（policy.OperationProcessStart）后创建进程，把它绑到
	// OS 进程树原语上（Windows Job Object / Unix 进程组），并返回句柄。Start
	// 返回后输出必须已经在被持续排空：慢消费者只能看到截断，不能反压子进程。
	//
	// ctx 只影响 Start 自身（例如启动前的取消）；进程生命周期由 Handle 控制。
	Start(ctx context.Context, spec Spec) (Handle, error)
}

// Handle 是一个已启动进程的句柄。
type Handle interface {
	// Identity 返回启动时捕获的进程身份（PID+启动时间标记+owner）。只有它落库后，
	// 进程才可能在崩溃恢复时被安全地重新认领或清理。
	Identity() Identity
	// Output 返回 stdout 的有界输出 spool 的只读视图。
	Output() Spool
	// Stderr 返回 stderr 的有界输出 spool 的只读视图。两路分开保留、不混流：
	// 诊断信息与程序输出混在一起会让上游无法区分，也会让脱敏与展示失去依据。
	Stderr() Spool
	// Cancel 请求终止整棵进程树，幂等。mode=CancelSoft 先发软终止，超过
	// spec.GracePeriod 仍未退出则自动升级为强制的 CancelForce；mode=CancelForce
	// 立即强制终止。执行归属未确认（Identity.OwnedBy/Verify 不通过）时不得 kill，
	// 必须返回错误并把状态记为 lost。
	Cancel(ctx context.Context, mode CancelMode) error
	// Wait 等待进程结束并返回终态。可重复调用：第一次之后每次都返回同一结果。
	// ctx 到期只结束本次等待，不代表进程已结束（此时必须返回错误，不能伪造
	// exit code 或把进程当成已退出）。
	Wait(ctx context.Context) (Exit, error)
}

// Spool 是进程输出的有界缓冲。
type Spool interface {
	// Snapshot 返回当前已保留的输出，以及因上限被丢弃的字节数。
	// truncatedBytes 非 0 表示输出不完整，调用方必须把它显式呈现给用户，
	// 不能假装拿到的是全部输出。
	Snapshot() (data []byte, truncatedBytes int64)
}
