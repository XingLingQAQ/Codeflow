//go:build linux

package process

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// linuxStartTokenPrefix 让 StartToken 自带平台标签（见 Identity.StartToken）。
const linuxStartTokenPrefix = "linux:"

// procStatStarttimeIndex 是 /proc/<pid>/stat 里 starttime（第 22 字段）在以 comm
// 结尾的右括号之后那段字段里的下标：右括号之后第 1 个字段是 state（第 3 字段），
// 所以第 N 字段的下标是 N-3。
const procStatStarttimeIndex = 22 - 3

func platformSupported() bool { return true }

// bootID 缓存 /proc/sys/kernel/random/boot_id。starttime 是“自开机起的时钟滴答
// 数”，单靠它跨重启无法比较（重启后计数归零，可能出现恰好相同的值），必须与
// boot_id 组合才构成全局唯一的启动标记。boot_id 在一次开机内不变，因此只读一次。
var bootID = sync.OnceValues(readBootID)

func readBootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("process: read boot_id: %w", err)
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return "", errors.New("process: boot_id is empty")
	}
	return id, nil
}

// platformCapture 读取 PID 的启动标记：boot_id + starttime。
func platformCapture(pid int) (string, error) {
	boot, err := bootID()
	if err != nil {
		return "", err
	}
	_, starttime, err := readProcStat(pid)
	if err != nil {
		return "", err
	}
	return linuxStartTokenPrefix + boot + ":" + starttime, nil
}

// platformVerify 按“先判存活、再比启动标记”的顺序判定：僵尸（state=Z）与死进程
// （state=X）都算 exited，即使 starttime 仍与记录一致。
func platformVerify(id Identity) (VerifyResult, error) {
	boot, err := bootID()
	if err != nil {
		return VerifyUnknown, err
	}
	state, starttime, err := readProcStat(id.PID)
	if errors.Is(err, ErrProcessNotFound) {
		return VerifyExited, nil
	}
	if err != nil {
		return VerifyUnknown, err
	}
	if state == 'Z' || state == 'X' || state == 'x' {
		return VerifyExited, nil
	}
	if linuxStartTokenPrefix+boot+":"+starttime != id.StartToken {
		return VerifyReused, nil
	}
	return VerifySame, nil
}

// readProcStat 返回 pid 的 (state, starttime)，只读 /proc，不发送任何信号。
func readProcStat(pid int) (state byte, starttime string, err error) {
	path := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, "", wrapReadProcStatError(path, err)
	}
	state, starttime, err = parseProcStat(data)
	if err == nil {
		return state, starttime, nil
	}
	// 进程恰好在两次读之间退出时 /proc/<pid>/stat 可能被截断成短内容；重读一次
	// 再判定，避免把“正在退出”误判成“格式损坏”。
	if data2, rerr := os.ReadFile(path); rerr == nil {
		if state, starttime, perr := parseProcStat(data2); perr == nil {
			return state, starttime, nil
		}
	} else {
		return 0, "", wrapReadProcStatError(path, rerr)
	}
	return 0, "", err
}

func wrapReadProcStatError(path string, err error) error {
	if os.IsNotExist(err) {
		return fmt.Errorf("process: read %s: %w", path, ErrProcessNotFound)
	}
	return fmt.Errorf("process: read %s: %w", path, err)
}

// parseProcStat 从 /proc/<pid>/stat 的内容里取 state 与 starttime。
//
// comm（第 2 字段）是进程名，可能包含空格甚至括号（例如 “(a b) (c)”），所以必须
// 从最后一个 ')' 之后切分，不能按空格整体切分——否则字段下标会全部错位。
func parseProcStat(data []byte) (state byte, starttime string, err error) {
	idx := bytes.LastIndexByte(data, ')')
	if idx < 0 || idx+1 >= len(data) {
		return 0, "", errors.New("process: malformed /proc stat (no comm terminator)")
	}
	fields := strings.Fields(string(data[idx+1:]))
	if len(fields) <= procStatStarttimeIndex {
		return 0, "", fmt.Errorf("process: malformed /proc stat (%d fields after comm)", len(fields))
	}
	if len(fields[0]) != 1 {
		return 0, "", fmt.Errorf("process: malformed /proc stat state %q", fields[0])
	}
	return fields[0][0], fields[procStatStarttimeIndex], nil
}
