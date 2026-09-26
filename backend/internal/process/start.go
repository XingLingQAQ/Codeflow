package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/policy"
)

// 哨兵错误。调用方用 errors.Is 判定，不依赖文案。
var (
	// ErrMissingOwnerInstance：NewSupervisor 没有给出本实例标识，Start 一律拒绝。
	ErrMissingOwnerInstance = errors.New("process: supervisor requires an owner instance")
	// ErrForeignOwnerInstance：Spec 的归属属于另一个实例。本实例没有资格启动它，
	// 更没有资格在之后杀掉它的进程（§27.5 第 6 条）。
	ErrForeignOwnerInstance = errors.New("process: spec owner instance does not match this supervisor")
	// ErrIdentityUnconfirmed：杀之前无法确认进程身份（Verify 既不是 same 也不是
	// exited）。此时绝不发信号，状态记为 lost。
	ErrIdentityUnconfirmed = errors.New("process: process identity is not confirmed; refusing to signal")
	// ErrInvalidCancelMode：CancelMode 不是 soft/force。
	ErrInvalidCancelMode = errors.New("process: invalid cancel mode")
	// ErrSoftTerminationFailed：软终止没能投递（例如 Windows 上调用方没有控制台，
	// 无法投递 CTRL_BREAK）。这不是“取消失败”：升级为 force 的计时照常进行。
	ErrSoftTerminationFailed = errors.New("process: soft termination could not be delivered; escalation to force is still scheduled")
)

// drainTimeout 是 run() 结束进程树后等待排空 goroutine 收尾的上限。进程树已经被
// 终止，写端必然关闭，正常情况下是毫秒级；设上限只为防止平台异常时 Wait 永远不返回。
const drainTimeout = 5 * time.Second

// treeBinding 是“一棵进程树绑在 OS 原语上”的平台抽象：
// Windows 是 Job Object（KILL_ON_JOB_CLOSE），Linux 是进程组。
//
// 三个方法都必须是幂等且并发安全的：Cancel 可能被多个调用方同时触发，宽限期升级
// goroutine 也会在 run() 释放绑定之后才醒来。绑定释放之后所有方法都变成无害的空操作。
type treeBinding interface {
	// softTerminate 请求优雅终止整棵树。投递失败返回错误（上层如实记录并继续升级计时）。
	softTerminate() error
	// forceTerminate 强制终止整棵树。
	forceTerminate() error
	// release 释放平台资源，幂等。返回后残留后代必须已经被终止（不留孤儿）。
	release()
}

// SupervisorOptions 是 NewSupervisor 的配置。
type SupervisorOptions struct {
	// OwnerInstance 是本后端实例的标识（§27.5 第 6 条）。Start 只接受
	// Owner.OwnerInstance 与它完全一致的 Spec：不一致说明这条执行请求属于另一个
	// 实例（lease 过期后换了 worker、多实例共用一台机器），本实例既不该启动它，
	// 也不该在之后按 PID 去杀它。
	OwnerInstance string
	// SpoolFilter 是写入 spool 之前的过滤钩子（nil = 原样写入），典型用途是日志
	// 脱敏。它只作用于 spool（日志）内容，不作用于 Subscribe 投递的行——适配器
	// 需要原始协议帧。真正的流式密钥脱敏器归 T2.04，这里只提供接口与接线。
	// 契约与 panic 语义见 SpoolFilter 的文档。
	SpoolFilter SpoolFilter
}

// NewSupervisor 创建受监督进程的启动器。OwnerInstance 为空时 Start 一律拒绝
// （返回 ErrMissingOwnerInstance）——没有实例标识就无法证明进程归属。
func NewSupervisor(opts SupervisorOptions) Supervisor {
	return &supervisor{
		ownerInstance: strings.TrimSpace(opts.OwnerInstance),
		spoolFilter:   opts.SpoolFilter,
	}
}

type supervisor struct {
	ownerInstance string
	spoolFilter   SpoolFilter
}

// Start 的顺序是固定的：Spec 校验 → 实例归属校验 → 策略闸控 → 平台启动 →
// 捕获身份 → 写 marker → 起排空与等待 goroutine。
//
// 前两步在策略判定之前：外来/非法请求连策略评估都不该进入（也就不会有审计噪声），
// 更不该有机会创建进程。闸控被拒时一个进程都不会被创建。
func (s *supervisor) Start(ctx context.Context, spec Spec) (Handle, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if s.ownerInstance == "" {
		return nil, ErrMissingOwnerInstance
	}
	if spec.Owner.OwnerInstance != s.ownerInstance {
		return nil, fmt.Errorf("%w: spec owner instance %q, supervisor instance %q",
			ErrForeignOwnerInstance, spec.Owner.OwnerInstance, s.ownerInstance)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	decision := policy.EnforceBoundary(ctx, policy.Request{
		Operation: policy.OperationProcessStart,
		// Resource 只用于评估：process_start 的 Resource 在 policy.normalizeRequest
		// 里一律归一为 "process"，命令行（可能含凭据）不会落审计。
		Resource:  spec.Path,
		RunID:     spec.Owner.RunID,
		AttemptID: spec.Owner.AttemptID,
	})
	if !decision.Allowed {
		return nil, policy.DenialError(decision)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return startSupervised(spec, s.spoolFilter)
}

// startSupervised 完成闸控放行之后的全部步骤。任何一步失败都必须结束已经启动的
// 进程并返回错误，不留孤儿。
func startSupervised(spec Spec, filter SpoolFilter) (*procHandle, error) {
	h := &procHandle{
		spec:       spec,
		grace:      spec.GracePeriod,
		stdoutDone: make(chan struct{}),
		stderrDone: make(chan struct{}),
		waitDone:   make(chan struct{}),
	}
	if h.grace == 0 {
		h.grace = DefaultGracePeriod
	}
	maxSpool := spec.MaxSpoolBytes
	if maxSpool == 0 {
		maxSpool = DefaultMaxSpoolBytes
	}
	h.stdout = newRingSpool(maxSpool)
	h.stderr = newRingSpool(maxSpool)
	h.stdoutStream = newStreamWriter(StreamStdout, h.stdout, filter)
	h.stderrStream = newStreamWriter(StreamStderr, h.stderr, filter)

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("process: open %s: %w", os.DevNull, err)
	}
	defer devNull.Close() //nolint:errcheck // 只关父进程的副本，子进程持有自己的句柄

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("process: create stdout pipe: %w", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdoutR.Close() //nolint:errcheck
		stdoutW.Close() //nolint:errcheck
		return nil, fmt.Errorf("process: create stderr pipe: %w", err)
	}

	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdin = devNull
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	h.cmd = cmd

	binding, startErr := startBoundProcess(cmd)
	// 父进程立刻放掉写端：EOF 只在子进程树的所有写端副本都关闭后出现，我们自己的
	// 副本留着会让排空 goroutine 永远等不到 EOF（从而 Wait 永远不返回）。
	stdoutW.Close() //nolint:errcheck
	stderrW.Close() //nolint:errcheck
	if startErr != nil {
		stdoutR.Close() //nolint:errcheck
		stderrR.Close() //nolint:errcheck
		return nil, startErr
	}
	h.binding = binding
	h.pid = cmd.Process.Pid

	// 排空必须早于一切后续步骤：子进程可能立刻写满管道缓冲区，没人读就会死锁。
	go h.drain(stdoutR, h.stdoutStream, h.stdoutDone)
	go h.drain(stderrR, h.stderrStream, h.stderrDone)

	id, err := CaptureIdentity(h.pid, spec.Owner)
	if err != nil {
		err = fmt.Errorf("process: capture identity of pid %d: %w", h.pid, err)
		h.abortStart()
		return nil, err
	}
	h.id = id
	// startedAt 在写 marker 之前定下，退出时原样保留：marker 的 started_at 与
	// identity.CapturedAt 语义不同（前者是“这次执行的开始时刻”，后者是“身份读取时刻”），
	// 而 id.CapturedAt 在进程秒退的情况下也会被正确填上。
	startedAt := id.CapturedAt
	if spec.MarkerPath != "" {
		if err := writeMarkerStart(spec, id, startedAt); err != nil {
			h.abortStart()
			return nil, err
		}
	}
	h.startedAt = startedAt
	go h.run()
	return h, nil
}

// abortStart 结束已经启动的进程并等排空收尾：Start 失败不允许留下活着的进程。
func (h *procHandle) abortStart() {
	_ = h.binding.forceTerminate()
	_, _ = h.cmd.Process.Wait()
	h.binding.release()
	h.awaitDrains(drainTimeout)
}

// procHandle 是 Supervisor 的默认 Handle 实现。
type procHandle struct {
	spec  Spec
	grace time.Duration

	cmd       *exec.Cmd
	binding   treeBinding
	pid       int
	id        Identity
	startedAt time.Time

	stdout, stderr             *ringSpool
	stdoutStream, stderrStream *streamWriter
	stdoutDone, stderrDone     chan struct{}

	waitDone chan struct{}
	escalate sync.Once

	mu       sync.Mutex
	exit     Exit
	finished bool
	// cancelMode 是本方请求过的取消强度（"" 表示没请求过），forced 表示实际用过强制
	// 终止（含 soft 超时升级）。softErr/forceErr 如实记录平台错误，供回执与诊断。
	cancelMode CancelMode
	softAt     time.Time
	softErr    error
	forceErr   error
	forced     bool
	lost       bool
}

func (h *procHandle) Identity() Identity { return h.id }

func (h *procHandle) Output() Spool { return h.stdout }

func (h *procHandle) Stderr() Spool { return h.stderr }

// Subscribe 为一路输出（stdout 或 stderr，不混流）注册实时按行订阅。
//
// 保证与边界（详见 stream.go）：
//   - 排空永不因订阅者而阻塞：订阅 channel 满即判定该订阅溢出并立即终止
//     （channel 关闭、Err() 为 ErrSubscriberOverflow、DroppedSeq 给出丢失行序号），
//     其他订阅者、spool 与子进程都不受影响。
//   - 第一个订阅者不会丢行：Start 返回后进程可能已经写了若干行、甚至已经退出，这些
//     行还没被任何订阅者认领，会原样交给第一个订阅者（有界预算 1 MiB / 1024 行），
//     之后 channel 按流状态关闭。因此“Start 后立刻 Subscribe”不会错过进程最早的输出，
//     与进程退出得多快无关。未认领行超出过预算时，第一个订阅者直接以
//     ErrSubscriberOverflow 结束（DroppedSeq 为第一条丢失的行）。
//   - 之后的订阅者只收订阅之后的新行；流结束后再订阅：channel 立即处于关闭状态、
//     Err() 为 nil，不回放历史——需要历史请用 Output()/Stderr() 的 spool 快照。
//   - 适配器（T1.13）的责任：收到 ErrSubscriberOverflow 说明协议帧可能已经丢失，
//     必须终止执行并失败（调用 Cancel(CancelForce)），不能继续解析残缺的流。
func (h *procHandle) Subscribe(stream Stream, opts SubscribeOptions) (*Subscription, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	var w *streamWriter
	switch stream {
	case StreamStdout:
		w = h.stdoutStream
	case StreamStderr:
		w = h.stderrStream
	default:
		return nil, fmt.Errorf("%w: %q", ErrInvalidStream, string(stream))
	}
	if w == nil {
		// 只可能出现在包内合成的句柄上（生产路径的 Start 一定装配了流）。
		return nil, fmt.Errorf("%w: %q has no stream writer on this handle", ErrInvalidStream, string(stream))
	}
	return w.subscribe(opts.withDefaults()), nil
}

// drain 持续读一路输出直到 EOF，按行投递给订阅者并写入有界 spool。它绝不阻塞子进程：
// 读多快就收多快，收不下的部分由 spool 丢弃并计数、订阅者溢出即被终止。
func (h *procHandle) drain(r *os.File, w *streamWriter, done chan struct{}) {
	defer close(done)
	defer r.Close() //nolint:errcheck // 读端由排空 goroutine 独占
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			w.feed(buf[:n])
		}
		if err != nil {
			break
		}
	}
	// EOF：投递最后一段不完整的行，然后结束本流（关闭订阅 channel）。
	w.finishPending()
	w.finalize()
}

// awaitDrains 等两路排空收尾，最多等 timeout（进程树已终止，正常是毫秒级）。
func (h *procHandle) awaitDrains(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for _, done := range []chan struct{}{h.stdoutDone, h.stderrDone} {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		select {
		case <-done:
		case <-time.After(remaining):
			return
		}
	}
}

// run 等待进程结束、结束残留后代、收尾输出，然后发布终态。只跑一次。
func (h *procHandle) run() {
	_ = h.cmd.Wait() // ExitError 只说明退出码非 0，终态由 ProcessState 给出
	var code *int
	if st := h.cmd.ProcessState; st != nil {
		if c := st.ExitCode(); c >= 0 {
			code = &c
		}
	}
	// 主进程结束即释放树绑定：Windows 关 Job 句柄、Unix 杀进程组，残留后代随之被
	// 终止（Run 结束不留孤儿），它们的写端关闭后排空才能看到 EOF。
	h.binding.release()
	h.awaitDrains(drainTimeout)
	h.finish(code)
}

// finish 发布终态并原子更新 marker。只调用一次。
func (h *procHandle) finish(code *int) {
	// 兜底结束两路流：正常路径上排空 goroutine 已经在 EOF 处 finalize 过（幂等），
	// 这里只覆盖“排空等待超时”的病态情况——否则订阅者会永远等不到 channel 关闭。
	if h.stdoutStream != nil {
		h.stdoutStream.finalize()
	}
	if h.stderrStream != nil {
		h.stderrStream.finalize()
	}
	h.mu.Lock()
	truncated := h.stdout.truncatedBytes() + h.stderr.truncatedBytes()
	h.finished = true
	var exit Exit
	switch {
	case h.lost:
		// 身份无法确认：绝不能当成“已退出”来释放资源，也不编造退出码。
		exit = Exit{Reason: ExitLost, SpoolTruncatedBytes: truncated}
	case h.cancelMode != "":
		exit = Exit{Reason: ExitCancelled, Forced: h.forced, SpoolTruncatedBytes: truncated}
		if !h.forced {
			exit.Code = code
		}
	default:
		exit = Exit{Reason: ExitExited, Code: code, SpoolTruncatedBytes: truncated}
	}
	h.exit = exit
	if h.spec.MarkerPath != "" {
		// 尽力而为：进程已经结束，marker 更新失败不影响终态判定（Identity/Wait 仍
		// 是权威来源），但绝不能因为写 marker 而卡住 Wait。
		_ = writeMarkerFinish(h.spec, h.id, h.startedAt, exit)
	}
	h.mu.Unlock()
	close(h.waitDone)
}

func (h *procHandle) Wait(ctx context.Context) (Exit, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-h.waitDone:
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.exit, nil
	case <-ctx.Done():
		// ctx 到期只结束本次等待，不改动也不伪造终态。
		return Exit{}, ctx.Err()
	}
}

// Cancel 请求终止整棵进程树，幂等。
//
// ctx 不参与取消决策：取消是安全关键路径，调用方的 ctx 已经结束不是放弃终止进程的
// 理由（那会留下孤儿）。参数保留是为了与 Handle 接口一致，也便于将来把取消请求
// 关联到调用链上。
//
// 顺序：已结束 → 返回 nil；已在下强制终止 → 返回 nil；Verify 身份 → 只有 same 才
// 发信号；soft 成功投递后启动宽限期计时，超时自动升级 force。
func (h *procHandle) Cancel(ctx context.Context, mode CancelMode) error {
	if !mode.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidCancelMode, string(mode))
	}
	h.mu.Lock()
	finished, alreadyForcing := h.finished, h.cancelMode == CancelForce
	h.mu.Unlock()
	if finished || alreadyForcing {
		return nil
	}

	// §27.5 第 6 条：杀之前先确认身份。reused/unknown 一律不发信号，记 lost。
	res, err := Verify(h.id)
	if err != nil {
		h.markLost()
		return fmt.Errorf("%w: pid %d: %w", ErrIdentityUnconfirmed, h.id.PID, err)
	}
	switch res {
	case VerifyExited:
		// 进程已经自己结束了：不 kill，也不记 lost——run() 会给出真实终态。
		return nil
	case VerifySame:
	default:
		h.markLost()
		return fmt.Errorf("%w: pid %d verify=%s", ErrIdentityUnconfirmed, h.id.PID, res)
	}

	var softErr, forceErr error
	h.mu.Lock()
	if h.finished {
		h.mu.Unlock()
		return nil
	}
	switch mode {
	case CancelSoft:
		if h.cancelMode == "" {
			h.cancelMode = CancelSoft
			h.softAt = time.Now()
			h.mu.Unlock()
			softErr = h.binding.softTerminate()
			h.mu.Lock()
			h.softErr = softErr
		}
	case CancelForce:
		h.cancelMode = CancelForce
		h.forced = true
		h.mu.Unlock()
		forceErr = h.binding.forceTerminate()
		h.mu.Lock()
		h.forceErr = forceErr
	}
	grace := h.grace
	h.mu.Unlock()

	if mode == CancelSoft {
		h.scheduleEscalation(grace)
	}
	if forceErr != nil {
		return fmt.Errorf("process: force terminate pid %d: %w", h.pid, forceErr)
	}
	if softErr != nil {
		// 如实上报投递失败；升级为 force 的计时照常进行（已 scheduleEscalation）。
		return fmt.Errorf("%w: %w", ErrSoftTerminationFailed, softErr)
	}
	return nil
}

// scheduleEscalation 在宽限期后把 soft 升级为 force。只调度一次；进程先退出就作罢。
// 计时不受调用方 ctx 影响：软终止已经发出，收尾必须由 supervisor 自己负责。
func (h *procHandle) scheduleEscalation(grace time.Duration) {
	h.escalate.Do(func() {
		go func() {
			timer := time.NewTimer(grace)
			defer timer.Stop()
			select {
			case <-h.waitDone:
				return
			case <-timer.C:
			}
			h.mu.Lock()
			if h.finished {
				h.mu.Unlock()
				return
			}
			h.cancelMode = CancelForce
			h.forced = true
			h.mu.Unlock()
			if err := h.binding.forceTerminate(); err != nil {
				h.mu.Lock()
				h.forceErr = err
				h.mu.Unlock()
			}
		}()
	})
}

// markLost 记录“身份无法确认”。run() 会把终态判成 lost 而不是 exited。
func (h *procHandle) markLost() {
	h.mu.Lock()
	h.lost = true
	h.mu.Unlock()
}

// spoolFilterPanics 返回过滤器 panic 被恢复的次数（每次都以占位说明代替原始单元写入 spool）。
func (h *procHandle) spoolFilterPanics() int64 {
	var n int64
	for _, w := range []*streamWriter{h.stdoutStream, h.stderrStream} {
		if w != nil {
			n += w.filterPanics.Load()
		}
	}
	return n
}

// unclaimedDropped 返回两路因超出“未认领行”预算而丢弃的行数之和。非 0 说明有订阅者
// 在进程已经写出大量输出之后才订阅（诊断用；正常路径应尽早 Subscribe）。
func (h *procHandle) unclaimedDropped() int64 {
	var n int64
	for _, w := range []*streamWriter{h.stdoutStream, h.stderrStream} {
		if w != nil {
			n += w.unclaimedDropped()
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Marker：进程身份的落盘凭证
// ---------------------------------------------------------------------------

// markerRecord 是 marker 文件的内容。
//
// 硬性约定：绝不写入环境变量的值，也绝不写入参数内容——两者都可能携带凭据
// （§15）。参数只记个数；Env 一个字段都不出现在这里。
type markerRecord struct {
	Identity   Identity   `json:"identity"`
	Owner      Ownership  `json:"owner"`
	Path       string     `json:"path"`
	ArgsCount  int        `json:"args_count"`
	StartedAt  time.Time  `json:"started_at"`
	Reason     ExitReason `json:"reason,omitempty"`
	Code       *int       `json:"code,omitempty"`
	Forced     bool       `json:"forced"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// writeMarkerStart 在启动后写第一版 marker：identity + owner + path + 参数个数 +
// started_at。startedAt 由 Start 统一给出，退出时原样保留（不重算）。
func writeMarkerStart(spec Spec, id Identity, startedAt time.Time) error {
	return writeMarkerAtomic(spec.MarkerPath, markerRecord{
		Identity:  id,
		Owner:     spec.Owner,
		Path:      spec.Path,
		ArgsCount: len(spec.Args),
		StartedAt: startedAt,
	})
}

// writeMarkerFinish 原子更新 marker 的终态字段，保留启动时写下的身份信息。
func writeMarkerFinish(spec Spec, id Identity, startedAt time.Time, exit Exit) error {
	finished := time.Now().UTC()
	return writeMarkerAtomic(spec.MarkerPath, markerRecord{
		Identity:   id,
		Owner:      spec.Owner,
		Path:       spec.Path,
		ArgsCount:  len(spec.Args),
		StartedAt:  startedAt,
		Reason:     exit.Reason,
		Code:       exit.Code,
		Forced:     exit.Forced,
		FinishedAt: &finished,
	})
}

// writeMarkerAtomic 先写同目录临时文件再 rename：读者要么看到上一版完整内容，要么
// 看到这一版，不会看到写了一半的 JSON。
func writeMarkerAtomic(path string, rec markerRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("process: marshal marker: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".codeflow-marker-*")
	if err != nil {
		return fmt.Errorf("process: create marker temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()        //nolint:errcheck
		os.Remove(tmpName) //nolint:errcheck
		return fmt.Errorf("process: write marker %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName) //nolint:errcheck
		return fmt.Errorf("process: close marker %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName) //nolint:errcheck
		return fmt.Errorf("process: rename marker into %s: %w", path, err)
	}
	return nil
}
