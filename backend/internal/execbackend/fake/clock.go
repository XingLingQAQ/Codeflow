// Package fake 是 execbackend 的脚本化测试替身（计划 §15 T1.06.b、§21.2）。
//
// 目标：不安装真实 CLI、不联网也能完整驱动 Run 状态机需要的会话行为——工具序列、
// 审批（批准/拒绝/未知引用/会话结束后投递）、重复帧、乱码帧、输出洪峰、背压、
// 取消、硬截止、崩溃、Close/Wait 幂等。
//
// 设计要点：
//   - 一切等待都走 Clock（ManualClock 让测试不依赖真实 sleep）。
//   - 会话终结结果（Wait 的返回值）与终结观察（Kind=exited）由同一份状态决定，
//     投递顺序由单一 run goroutine 保证；Close 后 Observations 已关闭。
//   - 所有投递的 Observation 都先过 Observation.Validate；ObservedAt 取 Clock.Now()。
//   - 本包只依赖标准库与 execbackend 包（§27.1 依赖约束）。
package fake

import (
	"sync"
	"time"
)

// Clock 抽象 fake 内的全部等待与时间戳来源。
//
// 真实适配器场景用 RealClock；测试用 NewManualClock 手动推进，从而不依赖真实时间
// 流逝（Script 的 Sleep、ProcessControl.HardDeadline 都走本接口）。
type Clock interface {
	// Now 返回当前时间，用于 Observation.ObservedAt 与硬截止计算。
	Now() time.Time
	// After 返回一个在 d 之后触发的 channel；d <= 0 时立即触发。
	After(d time.Duration) <-chan time.Time
}

// RealClock 是 Clock 的真实实现（time.Now / time.After）。
type RealClock struct{}

// Now 实现 Clock。
func (RealClock) Now() time.Time { return time.Now() }

// After 实现 Clock。
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// ManualClock 是手动时钟：只有 Advance 才推进时间并触发到期定时器。
//
// 并发安全。定时器按到期先后触发；触发过的定时器不会被重复触发。
type ManualClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*manualTimer
}

type manualTimer struct {
	deadline time.Time
	ch       chan time.Time
}

// NewManualClock 创建以 start 为当前时间的手动时钟。
func NewManualClock(start time.Time) *ManualClock {
	return &ManualClock{now: start}
}

// Now 实现 Clock。
func (c *ManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After 实现 Clock：d <= 0 时立即返回已触发的 channel，否则登记到由 Advance 触发的
// 定时器列表。
func (c *ManualClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.timers = append(c.timers, &manualTimer{deadline: c.now.Add(d), ch: ch})
	return ch
}

// Advance 把当前时间向前推进 d（d <= 0 只做到期检查，不推进），触发所有到期定时器，
// 返回推进后的当前时间。
//
// 定时器 channel 带缓冲 1，触发不会阻塞 Advance。
func (c *ManualClock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	if d > 0 {
		c.now = c.now.Add(d)
	}
	now := c.now
	var due []*manualTimer
	pending := make([]*manualTimer, 0, len(c.timers))
	for _, timer := range c.timers {
		if timer.deadline.After(now) {
			pending = append(pending, timer)
			continue
		}
		due = append(due, timer)
	}
	c.timers = pending
	c.mu.Unlock()

	for _, timer := range due {
		select {
		case timer.ch <- now:
		default:
		}
	}
	return now
}

// PendingTimers 返回已登记、尚未到期的定时器数量（测试与调试用）。
func (c *ManualClock) PendingTimers() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}
