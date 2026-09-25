package process

import (
	"errors"
	"fmt"
	"time"
)

// ErrUnsupportedPlatform 表示当前平台没有实现进程身份原语。按 §28 T1.08.a 的要求，
// 这种平台必须如实上报 capability=false，不得假装成功：能力探测（Supported）、
// CaptureIdentity 与 Verify 都以此错误为准。
var ErrUnsupportedPlatform = errors.New("process: process identity is not supported on this platform")

// ErrProcessNotFound 表示操作系统报告该 PID 不存在（进程已退出且进程对象已被回收，
// 或从未存在过）。它不是“暂时拿不到”，调用方据此判定 exited 而不是 unknown。
var ErrProcessNotFound = errors.New("process: no such process")

// VerifyResult 是 Verify 的判定结果。
//
// 四种结果的含义是刻意区分开的：只有 same 允许继续操作该进程；
// reused 表示 PID 已被复用，绝不能把它当作原进程处理（防误杀的核心）；
// unknown 表示无权限等原因无法判定，也只能当作“不是自己的进程”。
type VerifyResult string

const (
	// VerifySame：同一进程（PID 存活且启动时间标记与记录一致）。
	VerifySame VerifyResult = "same"
	// VerifyExited：进程不存在、已退出或是僵尸。
	VerifyExited VerifyResult = "exited"
	// VerifyReused：PID 存活但启动时间标记不同，说明该 PID 已被系统复用给新进程。
	VerifyReused VerifyResult = "reused"
	// VerifyUnknown：无法判定（无权限、读取失败等）。
	VerifyUnknown VerifyResult = "unknown"
)

// Valid 报告 r 是否是可识别的判定结果。
func (r VerifyResult) Valid() bool {
	switch r {
	case VerifySame, VerifyExited, VerifyReused, VerifyUnknown:
		return true
	}
	return false
}

// String 返回判定结果本身（便于日志与错误信息直接使用）。
func (r VerifyResult) String() string { return string(r) }

// Supported 报告当前平台是否实现进程身份原语。Windows 与 Linux 为 true；
// 其他平台为 false，此时 CaptureIdentity/Verify 返回 ErrUnsupportedPlatform，
// 上层必须把对应的能力标为 capability=false，而不是降级成“PID 相同就当同一个”。
func Supported() bool { return platformSupported() }

// CaptureIdentity 读取 pid 的操作系统启动时间标记，连同 owner 一起构成进程身份。
//
// 标记是平台相关的不透明字符串，调用方只应记录和比较，不要解析：
//   - windows:  "windows:<100ns FILETIME 十进制>"
//   - linux:    "linux:<boot_id>:<starttime 时钟滴答数>"
//
// 进程不存在时返回包装了 ErrProcessNotFound 的错误；平台不支持时返回
// ErrUnsupportedPlatform。owner 缺失即拒绝：没有归属的 PID 不允许被记录下来，
// 否则将来无法判断它是不是“自己的进程”。
func CaptureIdentity(pid int, owner Ownership) (Identity, error) {
	if err := owner.Validate(); err != nil {
		return Identity{}, err
	}
	if pid <= 0 {
		return Identity{}, fmt.Errorf("process: capture identity: pid %d is not positive", pid)
	}
	token, err := platformCapture(pid)
	if err != nil {
		return Identity{}, err
	}
	if token == "" {
		return Identity{}, fmt.Errorf("process: capture identity: platform returned empty start token for pid %d", pid)
	}
	return Identity{
		PID:        pid,
		StartToken: token,
		Owner:      owner,
		CapturedAt: time.Now().UTC(),
	}, nil
}

// Verify 判断 id 描述的那个进程现在是否还是同一个进程。
//
// 调用方必须先校验归属（Identity.OwnedBy）再看这里的返回值：Verify 只回答
// “这个 PID 还是原来那个进程吗”，不回答“它是不是我的”。任何非 same 的结果都不
// 允许对 PID 执行 kill 或据此发布状态（§27.5 第 6 条）。
func Verify(id Identity) (VerifyResult, error) {
	if err := id.Validate(); err != nil {
		return VerifyUnknown, err
	}
	return platformVerify(id)
}
