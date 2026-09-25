//go:build windows

package process

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsTree 把一棵进程树绑到 Job Object 上。
//
// 为什么必须是 Job 而不是“杀直接子进程”：Windows 不会因为父进程退出而结束子进程，
// 只杀直接子进程会把整棵子树变成孤儿。Job Object 的成员资格会被后代继承，因此
// 从 Job 创建那一刻起，这棵树就只有一个终止入口。
type windowsTree struct {
	mu       sync.Mutex
	job      windows.Handle
	pid      int
	released bool
}

// startBoundProcess 以 CREATE_SUSPENDED | CREATE_NEW_PROCESS_GROUP 启动进程，先把它
// 放进 Job Object 再恢复主线程。
//
// 为什么要挂起：os/exec 不暴露“创建后入 Job 前”的钩子，若先把进程跑起来再
// AssignProcessToJobObject，它在这段窗口里派生的后代已经逃出 Job 了（逃逸窗口）。
// 挂起状态下的进程一个指令都没执行，不可能派生任何东西。
//
// 契约：返回 error 时没有任何进程残留。
func startBoundProcess(cmd *exec.Cmd) (treeBinding, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// CREATE_NEW_PROCESS_GROUP 是 Windows 上定向投递 CTRL_BREAK 的前提：
		// GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, pid) 只对进程组 id = pid 的组生效，
		// 没有它就只能向整个控制台广播（会误伤调用方自己）。
		CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("process: start %s: %w", cmd.Path, err)
	}
	t := &windowsTree{pid: cmd.Process.Pid}
	if err := t.bind(); err != nil {
		// 进程仍在挂起：没有任何后代，直接结束它即可，不存在孤儿。
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, err
	}
	return t, nil
}

// bind 创建 Job、设置 KILL_ON_JOB_CLOSE、把挂起的进程放进去，最后恢复主线程。
func (t *windowsTree) bind() error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("process: CreateJobObject: %w", err)
	}
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	// KILL_ON_JOB_CLOSE：句柄一关，Job 里所有进程（含后代）被终止——这是“Run 结束
	// 不留孤儿”的兜底。不设置 BREAKAWAY_OK / SILENT_BREAKAWAY_OK：不允许脱离。
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job) //nolint:errcheck
		return fmt.Errorf("process: SetInformationJobObject: %w", err)
	}
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(t.pid))
	if err != nil {
		windows.CloseHandle(job) //nolint:errcheck
		return fmt.Errorf("process: OpenProcess pid %d: %w", t.pid, err)
	}
	err = windows.AssignProcessToJobObject(job, ph)
	windows.CloseHandle(ph) //nolint:errcheck
	if err != nil {
		windows.CloseHandle(job) //nolint:errcheck
		return fmt.Errorf("process: AssignProcessToJobObject pid %d: %w", t.pid, err)
	}
	if err := resumeProcessThread(t.pid); err != nil {
		// 进程还挂着：直接终止整个 Job，随后关句柄。
		_ = windows.TerminateJobObject(job, 1)
		windows.CloseHandle(job) //nolint:errcheck
		return fmt.Errorf("process: resume pid %d: %w", t.pid, err)
	}
	t.mu.Lock()
	t.job = job
	t.mu.Unlock()
	return nil
}

// resumeProcessThread 恢复 pid 的主线程。
//
// os/exec 不暴露 CreateProcess 返回的线程句柄（syscall.StartProcess 只回传进程句柄），
// 因此用线程快照按 OwnerProcessID 找它：刚创建且已挂起的进程只有主线程一个，而它未
// 恢复执行，不可能再创建线程，所以这里找到的唯一线程就是主线程。
func resumeProcessThread(pid int) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("process: CreateToolhelp32Snapshot(TH32CS_SNAPTHREAD): %w", err)
	}
	defer windows.CloseHandle(snap) //nolint:errcheck // 快照句柄，关闭失败不影响后续
	var entry windows.ThreadEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	err = windows.Thread32First(snap, &entry)
	for err == nil {
		if int(entry.OwnerProcessID) == pid {
			th, oerr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if oerr != nil {
				return fmt.Errorf("process: OpenThread %d of pid %d: %w", entry.ThreadID, pid, oerr)
			}
			defer windows.CloseHandle(th) //nolint:errcheck // 恢复后不再需要线程句柄
			if _, rerr := windows.ResumeThread(th); rerr != nil {
				return fmt.Errorf("process: ResumeThread %d of pid %d: %w", entry.ThreadID, pid, rerr)
			}
			return nil
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		err = windows.Thread32Next(snap, &entry)
	}
	return fmt.Errorf("process: no thread found for pid %d in thread snapshot: %w", pid, err)
}

// softTerminate 向该进程组投递 CTRL_BREAK_EVENT。
//
// 需要与目标进程共享同一个控制台；调用方没有控制台时（例如以服务方式运行）
// GenerateConsoleCtrlEvent 会失败，此时返回错误，由上层如实记录并直接进入升级计时。
func (t *windowsTree) softTerminate() error {
	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(t.pid)); err != nil {
		return fmt.Errorf("process: GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, %d): %w", t.pid, err)
	}
	return nil
}

// forceTerminate 终止 Job 里的整棵树。Job 只含本次启动的进程树，因此这里不需要
// 逐个 PID 定向（也就不会有 PID 复用的风险）；即便如此，Cancel 仍然先 Verify 身份，
// 因为“这个 Job 是不是本次启动的那个”也要靠身份确认来兜底。
func (t *windowsTree) forceTerminate() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.released || t.job == 0 {
		return nil
	}
	if err := windows.TerminateJobObject(t.job, 1); err != nil {
		return fmt.Errorf("process: TerminateJobObject pid %d: %w", t.pid, err)
	}
	return nil
}

// release 关闭 Job 句柄，幂等。KILL_ON_JOB_CLOSE 会让残留后代随之被终止——这是
// “Run 结束不留孤儿”的最后一道保证，也保证排空 goroutine 能看到 EOF。
func (t *windowsTree) release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.released {
		return
	}
	t.released = true
	if t.job != 0 {
		windows.CloseHandle(t.job) //nolint:errcheck // 关闭即可，失败也没有补救手段
		t.job = 0
	}
}
