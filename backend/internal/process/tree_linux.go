//go:build linux

package process

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
)

// unixTree 把一棵进程树绑到进程组上：Setpgid 让子进程成为新进程组的组长，后代默认
// 继承该组，于是 kill(-pgid, sig) 一次覆盖整棵树。
//
// 与 Windows 的 Job Object 相比，进程组不是强制容器：后代可以调用 setsid() 主动
// 脱离。这是 Linux 侧的已知边界（内核没有等价于 Job 的“成员资格继承”原语），
// §15 也只要求进程组级别的绑定。
type unixTree struct {
	mu       sync.Mutex
	pgid     int
	released bool
}

// startBoundProcess 以独立进程组启动进程。返回 error 时没有进程残留。
func startBoundProcess(cmd *exec.Cmd) (treeBinding, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("process: start %s: %w", cmd.Path, err)
	}
	// 子进程自己的 pid 就是新进程组的 pgid（Setpgid 让它在 exec 前成为组长）。
	return &unixTree{pgid: cmd.Process.Pid}, nil
}

// softTerminate 向整个进程组发 SIGTERM。
func (t *unixTree) softTerminate() error { return t.signalGroup(syscall.SIGTERM, "SIGTERM") }

// forceTerminate 向整个进程组发 SIGKILL。
func (t *unixTree) forceTerminate() error { return t.signalGroup(syscall.SIGKILL, "SIGKILL") }

func (t *unixTree) signalGroup(sig syscall.Signal, name string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.released {
		return nil
	}
	// 负数 pid 表示“发给整个进程组”。ESRCH 说明组里已经没有进程了，不是错误。
	if err := syscall.Kill(-t.pgid, sig); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("process: kill(-%d, %s): %w", t.pgid, name, err)
	}
	return nil
}

// release 在主进程退出后清理整个进程组，幂等：SIGKILL 掉残余后代，不留孤儿。
//
// 这里不再 Verify：主进程已经退出，它的身份不可能再确认。残留的隐患是 pgid 恰好被
// 系统复用给新的进程组——Linux 上 pgid 就是 pid，需要该 pid 被回收，概率极低；
// runc/containerd 等实现同样接受这一边界。
func (t *unixTree) release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.released {
		return
	}
	t.released = true
	_ = syscall.Kill(-t.pgid, syscall.SIGKILL)
}
