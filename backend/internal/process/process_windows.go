//go:build windows

package process

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// windowsStillActive 是 GetExitCodeProcess 对“进程仍在运行”返回的哨兵值
// （STILL_ACTIVE）。x/sys 没有导出该常量，因此在此显式定义。
const windowsStillActive = 259

// windowsStartTokenPrefix 让 StartToken 自带平台标签：跨平台的数据（数据库、
// 事件流）里一眼能看出这个标记是哪个平台写的。
const windowsStartTokenPrefix = "windows:"

func platformSupported() bool { return true }

// platformCapture 读取 PID 的进程创建时间（100ns FILETIME）。
//
// 创建时间在进程存活期间不变，PID 被复用后必然不同，因此“PID+创建时间”足以把
// “同一个进程”与“复用同一 PID 的新进程”区分开（§27.5 第 6 条）。
// 对已经退出但进程对象仍被句柄引用的进程，GetProcessTimes 依然返回其创建时间；
// 这种“身份可读但已退出”的情况由 platformVerify 的退出码检查判为 exited。
func platformCapture(pid int) (string, error) {
	h, err := openProcessForQuery(pid)
	if err != nil {
		return "", classifyOpenProcessError(pid, err)
	}
	defer windows.CloseHandle(h) //nolint:errcheck // 只读句柄，关闭失败不影响判定
	return captureTokenFromHandle(h)
}

// captureTokenFromHandle 从句柄读取创建时间并格式化成启动标记。
func captureTokenFromHandle(h windows.Handle) (string, error) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return "", fmt.Errorf("process: GetProcessTimes: %w", err)
	}
	filetime := uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)
	return fmt.Sprintf("%s%d", windowsStartTokenPrefix, filetime), nil
}

// openProcessForQuery 以最小权限打开进程：PROCESS_QUERY_LIMITED_INFORMATION 足以
// 读取创建时间与退出码，又不像 PROCESS_QUERY_INFORMATION 那样在受保护进程上被拒。
func openProcessForQuery(pid int) (windows.Handle, error) {
	return windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
}

// classifyOpenProcessError 把 OpenProcess 的失败映射成稳定的哨兵错误：
// ERROR_INVALID_PARAMETER(87) 表示该 PID 不存在（进程对象已回收），
// ERROR_ACCESS_DENIED(5) 表示进程存在但不允许查询——后者不能当成不存在。
func classifyOpenProcessError(pid int, err error) error {
	switch {
	case errors.Is(err, windows.ERROR_INVALID_PARAMETER):
		return fmt.Errorf("process: pid %d: %w", pid, ErrProcessNotFound)
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return fmt.Errorf("process: pid %d: access denied: %w", pid, err)
	default:
		return fmt.Errorf("process: OpenProcess pid %d: %w", pid, err)
	}
}

// platformVerify 按“先判存活、再比创建时间”的顺序判定。
//
// 顺序不能反：PID 被复用时 GetExitCodeProcess 对新进程返回 STILL_ACTIVE，只有走到
// 创建时间比较才能发现“不是原来那个进程”。反过来，若先比创建时间，一个已退出且
// 未被复用的 PID 会因为拿不到句柄而被判 unknown，丢失“已退出”这个更强的结论。
func platformVerify(id Identity) (VerifyResult, error) {
	h, err := openProcessForQuery(id.PID)
	if err != nil {
		switch {
		case errors.Is(err, windows.ERROR_INVALID_PARAMETER):
			return VerifyExited, nil
		case errors.Is(err, windows.ERROR_ACCESS_DENIED):
			return VerifyUnknown, nil
		default:
			return VerifyUnknown, fmt.Errorf("process: OpenProcess pid %d: %w", id.PID, err)
		}
	}
	defer windows.CloseHandle(h) //nolint:errcheck // 只读句柄，关闭失败不影响判定

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return VerifyUnknown, fmt.Errorf("process: GetExitCodeProcess pid %d: %w", id.PID, err)
	}
	if code != windowsStillActive {
		return VerifyExited, nil
	}
	token, err := captureTokenFromHandle(h)
	if err != nil {
		return VerifyUnknown, err
	}
	if token != id.StartToken {
		return VerifyReused, nil
	}
	return VerifySame, nil
}
