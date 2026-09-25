package fake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/execbackend"
)

// DefaultObservationBuffer 是 Observations channel 的默认缓冲深度。
//
// 有缓冲才能保证 Backend.Start 返回前能把 process_started 交给调用方；缓冲填满后
// 脚本会阻塞在投递上（背压），直到调用方读取或被 Cancel/Close/HardDeadline 打断。
const DefaultObservationBuffer = 64

// 能力证据常量：fake 的能力来自脚本实现本身，不是外部探测。
const (
	fakeEvidence = "fake:scripted"
	fakeSource   = "fake backend"
)

// Options 配置一个 fake Backend。
type Options struct {
	// Name 后端标识；空则用 "fake"。
	Name string
	// Clock 时间来源；nil 则用 RealClock。测试建议传 NewManualClock。
	Clock Clock
	// Capabilities 本次后端声明（已证明）的能力集合。
	//
	// nil 表示使用 DefaultCapabilities()；显式传空切片（[]execbackend.Capability{}）
	// 表示"什么都不声明"，用于验证缺能力路径。
	Capabilities []execbackend.Capability
	// Script 这是每次 Start 使用的固定脚本；nil/空脚本等价于"立即 completed/exit 0"。
	Script []Step
	// ScriptFor 按请求生成脚本；非 nil 时优先于 Script（便于按 Required/工作目录分叉）。
	ScriptFor func(execbackend.StartRequest) []Step
	// ObservationBuffer 覆盖 Observations channel 的缓冲深度；<= 0 用默认值。
	ObservationBuffer int
}

// DefaultCapabilities 返回 fake 默认声明（并真的有对应行为实现）的能力：
// non_interactive、json_stream、approval_hook、cancel_graceful、usage_tokens、usage_cost。
//
// 未列出的能力（sandbox、pty、inject、mcp、resume_checkpoint）fake 不声明——缺证据的
// 能力一律为 false（§27.7），脚本里用到的能力必须由 Options.Capabilities 显式声明。
func DefaultCapabilities() []execbackend.Capability {
	return []execbackend.Capability{
		execbackend.CapabilityNonInteractive,
		execbackend.CapabilityJSONStream,
		execbackend.CapabilityApprovalHook,
		execbackend.CapabilityCancelGraceful,
		execbackend.CapabilityUsageTokens,
		execbackend.CapabilityUsageCost,
	}
}

// Backend 是脚本化的 execbackend.Backend 实现：不启动任何进程、不联网、不写磁盘。
//
// 并发安全；Capabilities 与 Prepare 无副作用；Start 返回的 Session 由调用方拥有并
// 必须 Close（见 execbackend.Session 注释的关闭责任）。
type Backend struct {
	name      string
	clock     Clock
	caps      []execbackend.Capability
	script    []Step
	scriptFor func(execbackend.StartRequest) []Step
	buffer    int
}

// New 构造 fake Backend；零值 Options 得到 name="fake"、RealClock、默认能力、空脚本。
func New(opts Options) *Backend {
	b := &Backend{
		name:      strings.TrimSpace(opts.Name),
		clock:     opts.Clock,
		script:    opts.Script,
		scriptFor: opts.ScriptFor,
		buffer:    opts.ObservationBuffer,
	}
	if b.name == "" {
		b.name = "fake"
	}
	if b.clock == nil {
		b.clock = RealClock{}
	}
	if b.buffer <= 0 {
		b.buffer = DefaultObservationBuffer
	}
	if opts.Capabilities == nil {
		b.caps = DefaultCapabilities()
	} else {
		b.caps = append([]execbackend.Capability(nil), opts.Capabilities...)
	}
	return b
}

// Name 实现 execbackend.Backend。
func (b *Backend) Name() string { return b.name }

// Capabilities 实现 execbackend.Backend：只读、不联网，每次返回全新的报告与证据
// （调用方改不了内部状态）。证据固定为 "fake:scripted"，Source 固定为 "fake backend"，
// ExecutablePath 留空（fake 没有可执行文件）。
func (b *Backend) Capabilities(ctx context.Context) (execbackend.CapabilityReport, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return execbackend.CapabilityReport{}, execbackend.NewBackendUnavailable(b.name, err)
		}
	}
	evidence := make(map[execbackend.Capability]string, len(b.caps))
	for _, c := range b.caps {
		if !c.Valid() {
			// 枚举外的能力不进入证据：CapabilityReport.Has 也不认可它们。
			continue
		}
		evidence[c] = fakeEvidence
	}
	report := execbackend.CapabilityReport{
		Backend:  b.name,
		Source:   fakeSource,
		ProbedAt: b.clock.Now(),
		Evidence: evidence,
	}
	return report, nil
}

// Prepare 实现 execbackend.Backend：校验请求 + 校验必需能力，无副作用。
// 返回的 PreparedStart 固化请求副本（StartRequest.Clone），调用方之后修改原请求
// 不影响 Start。
func (b *Backend) Prepare(ctx context.Context, req execbackend.StartRequest) (execbackend.PreparedStart, error) {
	if err := req.Validate(); err != nil {
		return execbackend.PreparedStart{}, err
	}
	report, err := b.Capabilities(ctx)
	if err != nil {
		return execbackend.PreparedStart{}, err
	}
	if err := execbackend.CheckRequired(report, req.Required); err != nil {
		return execbackend.PreparedStart{}, err
	}
	return execbackend.PreparedStart{Request: req.Clone(), Report: report}, nil
}

// Start 实现 execbackend.Backend：投递 process_started，再在独立 goroutine 里运行脚本。
//
// 零值或未经 Prepare 的 PreparedStart 一律拒绝（invalid_request）；请求要求但报告
// 未证明的能力同样拒绝（capability_unavailable）。
func (b *Backend) Start(ctx context.Context, p execbackend.PreparedStart) (execbackend.Session, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, execbackend.NewBackendUnavailable(b.name, err)
		}
	}
	if err := p.Request.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Report.Backend) == "" || strings.TrimSpace(p.Report.Source) == "" {
		return nil, execbackend.NewInvalidRequest("report", "PreparedStart must come from Backend.Prepare")
	}
	if p.Report.Backend != b.name {
		return nil, execbackend.NewInvalidRequest("report.backend",
			fmt.Sprintf("PreparedStart belongs to backend %q, not %q", p.Report.Backend, b.name))
	}
	if err := execbackend.CheckRequired(p.Report, p.Request.Required); err != nil {
		return nil, err
	}

	script := b.script
	if b.scriptFor != nil {
		script = b.scriptFor(p.Request)
	}

	s := &session{
		backend:   b,
		request:   p.Request.Clone(),
		report:    p.Report,
		clock:     b.clock,
		script:    script,
		obs:       make(chan execbackend.Observation, b.buffer),
		stopCh:    make(chan struct{}),
		closeCh:   make(chan struct{}),
		finished:  make(chan struct{}),
		obsClosed: make(chan struct{}),
		approvals: make(map[string]*approvalWaiter),
	}

	started := execbackend.Observation{
		Kind: execbackend.ObservationProcessStarted,
		Payload: mustJSON(map[string]any{
			"backend":  b.name,
			"work_dir": s.request.WorkDir,
		}),
	}
	prepared, err := s.prepare(started)
	if err != nil {
		return nil, execbackend.NewInvalidRequest("payload", err.Error())
	}
	// 缓冲刚建立、stopCh/closeCh 均未关闭，此处的发送不会阻塞。
	if err := s.send(prepared); err != nil {
		return nil, execbackend.NewBackendUnavailable(b.name, err)
	}

	go s.run()

	if hardDeadline := s.request.Process.HardDeadline; !hardDeadline.IsZero() {
		go s.watchHardDeadline(hardDeadline)
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// 会话
// ---------------------------------------------------------------------------

// approvalWaiter 是一次待审批的握手点（每个 approval ProviderRef 一个）。
type approvalWaiter struct {
	ch       chan execbackend.ApprovalDecision
	decided  bool
	decision execbackend.ApprovalDecision
}

// session 实现 execbackend.Session。
//
// goroutine 与关闭责任：
//   - run goroutine（Start 里启动）是 Observations channel 唯一的写入者与关闭者：
//     它跑脚本、投递终结观察、close(obs)、close(obsClosed)。
//   - watchHardDeadline goroutine（仅有 HardDeadline 时启动）在硬截止到期时请求终结，
//     会话一结束（obsClosed）即退出。
//   - 调用方只调用 Session 方法，不直接碰 channel。
//
// 状态机：done=已确定终结结果（Wait 可立即返回）；closed=调用方已 Close（Observe 已关闭）。
type session struct {
	backend *Backend
	request execbackend.StartRequest
	report  execbackend.CapabilityReport
	clock   Clock
	script  []Step
	obs     chan execbackend.Observation

	stopCh    chan struct{} // 由 Cancel/HardDeadline/Close 关闭：请求终结
	stopOnce  sync.Once
	closeCh   chan struct{} // 由 Close 关闭：调用方不再关心观察，放弃投递
	closeOnce sync.Once
	finished  chan struct{} // 终结结果已确定（Wait 可返回）
	obsClosed chan struct{} // obs 已关闭（Close 在此之上等待）

	mu        sync.Mutex
	closed    bool
	done      bool
	result    execbackend.ExitResult
	waitErr   error
	approvals map[string]*approvalWaiter
}

// Observations 实现 execbackend.Session。
func (s *session) Observations() <-chan execbackend.Observation { return s.obs }

// Wait 实现 execbackend.Session：可重复调用，每次返回同一最终结果；未终结时阻塞
// 直到终结或 ctx 取消（ctx 取消只影响本次等待，不改变会话结果）。
//
// 脚本自身出错（编排非法观察等）时返回非 nil error，此时终结结果仍是
// protocol_error，且终结观察已经投递。
func (s *session) Wait(ctx context.Context) (execbackend.ExitResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// 已终结优先返回结果：ctx 已取消不影响"结果已经存在"这一事实。
	select {
	case <-s.finished:
		return s.resultSnapshot()
	default:
	}
	select {
	case <-s.finished:
		return s.resultSnapshot()
	case <-ctx.Done():
		return execbackend.ExitResult{}, ctx.Err()
	}
}

// Approve 实现 execbackend.Session。
//
// 判定顺序（冻结）：会话已 Close 或已终结 → ErrSessionClosed；未声明 approval_hook →
// capability_unavailable；ProviderRef 为空或没有对应待审批 → invalid_request。
// 同一个 ProviderRef 的重复裁决（含与首条相反的裁决）是幂等 no-op：首个裁决生效。
func (s *session) Approve(_ context.Context, d execbackend.ApprovalDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.done {
		return execbackend.ErrSessionClosed
	}
	if !s.report.Has(execbackend.CapabilityApprovalHook) {
		return execbackend.NewCapabilityUnavailable(execbackend.CapabilityApprovalHook)
	}
	if strings.TrimSpace(d.ProviderRef) == "" {
		return execbackend.NewInvalidRequest("provider_ref", "provider_ref is required to match a pending approval")
	}
	waiter, ok := s.approvals[d.ProviderRef]
	if !ok {
		return execbackend.NewInvalidRequest("provider_ref",
			fmt.Sprintf("no pending approval with provider_ref %q", d.ProviderRef))
	}
	if waiter.decided {
		return nil
	}
	waiter.decided = true
	waiter.decision = d
	waiter.ch <- d // 缓冲 1：不会阻塞
	return nil
}

// Cancel 实现 execbackend.Session：幂等。会话已终结（含已 Close）时返回 nil/ErrSessionClosed。
//
// 取舍：fake 没有可软终止的真实进程，graceful 与 force 都立即以 exited{cancelled}
// 终结（仍遵守"终结观察 + Wait 结果一致"的契约）；未声明 cancel_graceful 能力 →
// capability_unavailable（枚举里没有单独的 force 能力，两种模式统一以它为准）。
func (s *session) Cancel(_ context.Context, mode execbackend.CancelMode) error {
	if !mode.Valid() {
		return execbackend.NewInvalidRequest("mode", fmt.Sprintf("unknown cancel mode %q", string(mode)))
	}
	s.mu.Lock()
	closed, done := s.closed, s.done
	s.mu.Unlock()
	switch {
	case closed:
		return execbackend.ErrSessionClosed
	case done:
		// 会话已经终结：调用方想要的结果已达成。
		return nil
	}
	// Only the graceful path is capability-gated; force cancel must always be
	// able to stop a run, so the caller can escalate to it (§27.2 item 6).
	if mode == execbackend.CancelGraceful && !s.report.Has(execbackend.CapabilityCancelGraceful) {
		return execbackend.NewCapabilityUnavailable(execbackend.CapabilityCancelGraceful)
	}
	s.terminate(cancelledResult())
	return nil
}

// Close 实现 execbackend.Session：幂等；释放本会话全部 goroutine。
//
// 返回时保证 Observations 已关闭；不再关心尚未投递的观察（包括终结观察）。会话若
// 尚未终结，其最终结果记为 cancelled。Close 之后 Approve/Cancel 返回 ErrSessionClosed。
func (s *session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		<-s.obsClosed
		return nil
	}
	s.closed = true
	if !s.done {
		closeResult(s, cancelledResult(), nil)
	}
	s.mu.Unlock()

	s.stopOnce.Do(func() { close(s.stopCh) })
	s.closeOnce.Do(func() { close(s.closeCh) })
	<-s.obsClosed
	return nil
}

// ---------------------------------------------------------------------------
// 内部：观察投递
// ---------------------------------------------------------------------------

// prepare 盖章 ObservedAt 并校验观察；非法观察返回 scriptError（不投递任何东西）。
func (s *session) prepare(o execbackend.Observation) (execbackend.Observation, error) {
	o.ObservedAt = s.clock.Now()
	if err := o.Validate(); err != nil {
		return execbackend.Observation{}, scriptErrf("invalid observation kind=%s ref=%q: %v", o.Kind, o.ProviderRef, err)
	}
	return o, nil
}

// send 投递一条普通观察；会话被终结（stopCh）时放弃投递并返回 errTerminated。
//
// 调用方不读时这里会阻塞——这正是背压：脚本停在发送上，但 Cancel/Close/HardDeadline
// 仍能让会话结束。
func (s *session) send(o execbackend.Observation) error {
	select {
	case s.obs <- o:
		return nil
	case <-s.stopCh:
		return errTerminated
	}
}

// sendTerminal 投递终结观察：只有 Close（closeCh）能让它放弃，Cancel/HardDeadline
// 不会丢弃它——只要调用方在读，就一定能读到 exited。
func (s *session) sendTerminal(o execbackend.Observation) bool {
	select {
	case s.obs <- o:
		return true
	case <-s.closeCh:
		return false
	}
}

// runStepsThenFinalize 是 run goroutine 的主体。
func (s *session) run() {
	r := &runner{s: s}
	err := r.runSteps(s.script)

	var scriptErr *scriptError
	switch {
	case errors.As(err, &scriptErr):
		if s.recordTerminal(protocolErrorResult(), scriptErr.err) {
			s.deliverScriptErrorWarning(scriptErr.err)
		}
	case errors.Is(err, errScriptExited), errors.Is(err, errTerminated):
		// 结果已由脚本的 Exit/Crash 或终结请求方（Cancel/HardDeadline/Close）记录。
	case err != nil:
		if s.recordTerminal(protocolErrorResult(), err) {
			s.deliverScriptErrorWarning(err)
		}
	default:
		// 脚本走完没有显式 Exit：自动以 completed / exit 0 结束。
		code := 0
		s.recordTerminal(execbackend.ExitResult{
			ExitCode: &code,
			Reason:   execbackend.ExitReasonCompleted,
			Usage:    UnknownUsage(),
		}, nil)
	}

	// 兜底：任何路径都必须留下一个结果，避免 Wait 永久阻塞。
	s.recordTerminal(cancelledResult(), nil)

	s.finalize(r.delivered)
}

// finalize 投递终结观察（若脚本还没投递过）并关闭 Observations。
// 只由 run goroutine 调用：它是 obs 的唯一关闭者。
func (s *session) finalize(delivered bool) {
	if !delivered {
		s.mu.Lock()
		result := s.result
		s.mu.Unlock()
		if payload, err := json.Marshal(result); err == nil {
			terminal := execbackend.Observation{
				Kind:       execbackend.ObservationExited,
				ObservedAt: s.clock.Now(),
				Payload:    payload,
			}
			if terminal.Validate() == nil {
				s.sendTerminal(terminal)
			}
		}
	}
	close(s.obs)
	close(s.obsClosed)
}

// deliverScriptErrorWarning 在终结观察之前补一条 protocol_warning，说明脚本内部错误
// （尽力而为：调用方不读时会被 Cancel/Close 打断）。
func (s *session) deliverScriptErrorWarning(cause error) {
	terminal, err := s.prepare(execbackend.Observation{
		Kind: execbackend.ObservationProtocolWarning,
		Payload: mustJSON(map[string]any{
			"stage":  "script",
			"reason": cause.Error(),
		}),
	})
	if err != nil {
		return
	}
	_ = s.send(terminal)
}

// recordTerminal 记录终结结果（首个生效）；返回是否由本次调用记录。
func (s *session) recordTerminal(result execbackend.ExitResult, waitErr error) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return false
	}
	closeResult(s, result, waitErr)
	return true
}

// closeResult 在持锁状态下写入终结结果并唤醒 Wait；调用方必须持有 s.mu。
func closeResult(s *session, result execbackend.ExitResult, waitErr error) {
	s.done = true
	s.result = result
	s.waitErr = waitErr
	close(s.finished)
}

// terminate 记录终结结果并请求脚本停止；已经终结时为 no-op。
func (s *session) terminate(result execbackend.ExitResult) {
	if !s.recordTerminal(result, nil) {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// watchHardDeadline 在 Clock 上等硬截止，到期以 timeout 终结会话；会话先结束则退出。
func (s *session) watchHardDeadline(deadline time.Time) {
	d := deadline.Sub(s.clock.Now())
	if d <= 0 {
		s.terminate(timeoutResult())
		return
	}
	select {
	case <-s.clock.After(d):
		s.terminate(timeoutResult())
	case <-s.obsClosed:
	}
}

// registerApproval 登记一个待审批 ProviderRef；重复使用同一 ref 是脚本错误。
func (s *session) registerApproval(ref string) (*approvalWaiter, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, scriptErrf("AwaitApproval: provider_ref is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.approvals[ref]; exists {
		return nil, scriptErrf("AwaitApproval: provider_ref %q is already used", ref)
	}
	waiter := &approvalWaiter{ch: make(chan execbackend.ApprovalDecision, 1)}
	s.approvals[ref] = waiter
	return waiter, nil
}

// resultSnapshot 在锁内读取终结结果。
func (s *session) resultSnapshot() (execbackend.ExitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result, s.waitErr
}

// ---------------------------------------------------------------------------
// 终结结果构造
// ---------------------------------------------------------------------------

func cancelledResult() execbackend.ExitResult {
	return execbackend.ExitResult{
		Reason:    execbackend.ExitReasonCancelled,
		Retryable: false,
		Usage:     UnknownUsage(),
	}
}

func timeoutResult() execbackend.ExitResult {
	return execbackend.ExitResult{
		Reason:    execbackend.ExitReasonTimeout,
		Retryable: false,
		Usage:     UnknownUsage(),
	}
}

func protocolErrorResult() execbackend.ExitResult {
	return execbackend.ExitResult{
		Reason:    execbackend.ExitReasonProtocolError,
		Retryable: false,
		Usage:     UnknownUsage(),
	}
}

// ---------------------------------------------------------------------------
// 测试辅助（非导出）
// ---------------------------------------------------------------------------

// pendingFrames 返回已投递但尚未被读取的观察条数（测试判断背压用）。
func (s *session) pendingFrames() int { return len(s.obs) }

// isDone 返回终结结果是否已确定。
func (s *session) isDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

// isClosed 返回调用方是否已 Close。
func (s *session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// awaitingApproval 返回指定 provider_ref 是否正阻塞在审批上（测试用）。
func (s *session) awaitingApproval(ref string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	waiter, ok := s.approvals[ref]
	return ok && !waiter.decided
}
