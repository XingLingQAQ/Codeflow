//go:build !windows && !linux

package process

import (
	"fmt"
	"os/exec"
)

// startBoundProcess 在未实现进程树绑定的平台上拒绝启动。
//
// 能力必须如实上报（§28 T1.08）：不能“先跑起来再说”，那会让上层以为进程树受控，
// 实际上一旦父进程退出，后代就变成无法收拾的孤儿。上层据此把执行能力标为
// capability=false。
func startBoundProcess(cmd *exec.Cmd) (treeBinding, error) {
	return nil, fmt.Errorf("process: cannot bind a process tree on this platform: %w", ErrUnsupportedPlatform)
}
