package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// 索引 worker 终态 last_error 的原因前缀（确定性终态，不再重试）。
const (
	atomicIndexFailStaleSuperseded  = "stale_superseded"
	atomicIndexFailMemoryRowMissing = "memory_row_missing"
	atomicIndexFailAttemptsExceeded = "attempts_exceeded"
	atomicIndexFailUnknownOperation = "unknown_operation"
)

// AtomicIndexWorkerConfig 配置原子记忆索引 worker 的消费参数。
// 零值字段回落到默认值，测试可只覆盖关心的字段。
type AtomicIndexWorkerConfig struct {
	// PollInterval 运行期扫描 atomic_index_jobs 的间隔。
	PollInterval time.Duration
	// MaxAttempts 单条 job 的最大失败尝试次数；达到上限标 failed 终态。
	MaxAttempts int
	// Concurrency 同时执行的 job 上限（按不同 memory_id 并发）。
	Concurrency int
	// ScanLimit 单次扫描发现的 pending job 上限（透传 ListPendingJobs）。
	ScanLimit int
	// StopTimeout Close 时等待在途 job 退出的有界超时。
	StopTimeout time.Duration
	// PruneInterval done 行保留策略的执行周期（T13.02.c）；构造时也会先
	// 执行一次。<=0 回退默认 1h。
	PruneInterval time.Duration
	// PrunePolicy done 行保留策略（零值回退默认：7 天窗口、每 memory 16 条）。
	PrunePolicy AtomicIndexJobPrunePolicy
}

// defaultAtomicIndexWorkerConfig 生产默认参数：2s 轮询、最多 8 次尝试、
// 4 路并发、每轮最多发现 100 条、关闭等待 5s、done 行每小时按保留策略清理。
func defaultAtomicIndexWorkerConfig() AtomicIndexWorkerConfig {
	return AtomicIndexWorkerConfig{
		PollInterval: 2 * time.Second,
		MaxAttempts:  8,
		Concurrency:  4,
		ScanLimit:    100,
		StopTimeout:  5 * time.Second,
		PruneInterval: time.Hour,
		PrunePolicy:  defaultAtomicIndexJobPrunePolicy(),
	}
}

func (c AtomicIndexWorkerConfig) normalized() AtomicIndexWorkerConfig {
	defaults := defaultAtomicIndexWorkerConfig()
	if c.PollInterval <= 0 {
		c.PollInterval = defaults.PollInterval
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = defaults.MaxAttempts
	}
	if c.Concurrency <= 0 {
		c.Concurrency = defaults.Concurrency
	}
	if c.ScanLimit <= 0 {
		c.ScanLimit = defaults.ScanLimit
	}
	if c.StopTimeout <= 0 {
		c.StopTimeout = defaults.StopTimeout
	}
	if c.PruneInterval <= 0 {
		c.PruneInterval = defaults.PruneInterval
	}
	c.PrunePolicy = c.PrunePolicy.normalized()
	return c
}

// atomicMemoryServiceOptions 是 NewAtomicMemoryService 的可选配置。
type atomicMemoryServiceOptions struct {
	workerCfg      AtomicIndexWorkerConfig
	workerDisabled bool
}

// AtomicMemoryServiceOption 配置 AtomicMemoryService 的可选行为。
type AtomicMemoryServiceOption func(*atomicMemoryServiceOptions)

// WithAtomicIndexWorkerConfig 覆盖索引 worker 参数（测试注入短轮询间隔
// 与低重试上限）；未设置的字段回落默认值。
func WithAtomicIndexWorkerConfig(cfg AtomicIndexWorkerConfig) AtomicMemoryServiceOption {
	return func(o *atomicMemoryServiceOptions) {
		o.workerCfg = cfg
	}
}

// WithAtomicIndexWorkerDisabled 关闭索引 worker 与启动恢复扫描（仅测试
// 用于确定性断言手工发布路径；生产构造链不得使用）。
func WithAtomicIndexWorkerDisabled() AtomicMemoryServiceOption {
	return func(o *atomicMemoryServiceOptions) {
		o.workerDisabled = true
	}
}

// atomicIndexWorker 消费 atomic_index_jobs 持久化同步意图（§28 T13.02.b
// part 2）。队列即 atomic_index_jobs 表本身，不引入外部依赖：
//   - 同一 memory_id 任何时刻最多一个 job 在执行（串行）；不同 ID 以
//     Concurrency 有界并发；
//   - 执行路径复用服务的 publishUpsert/publishDelete，向量 I/O 不在任何
//     数据库事务内；
//   - 失败经 MarkRetry 记 attempts+1 保持 pending 供下轮重试；达到
//     MaxAttempts 或被 CanApplyUpsert 门禁拦截的 stale job 标 failed 终态
//     （last_error 记录原因），不再重试；
//   - stop 停收新 job、取消在途 job 的 ctx，并有界等待其退出。
type atomicIndexWorker struct {
	svc *AtomicMemoryService
	cfg AtomicIndexWorkerConfig

	ctx    context.Context
	cancel context.CancelFunc

	stopCh   chan struct{}
	stopOnce sync.Once
	loopWG   sync.WaitGroup // dispatcher goroutine
	jobsWG   sync.WaitGroup // 在途 job goroutine

	mu       sync.Mutex
	inFlight map[string]struct{} // 正在执行的 memory_id（串行与并发上限）

	// lastPruneAt 上次 done 行清理时间（store 时钟口径）。只在构造（start
	// 之前）与 loop goroutine 内读写，无需加锁。
	lastPruneAt time.Time
}

func newAtomicIndexWorker(svc *AtomicMemoryService, cfg AtomicIndexWorkerConfig) *atomicIndexWorker {
	ctx, cancel := context.WithCancel(context.Background())
	return &atomicIndexWorker{
		svc:      svc,
		cfg:      cfg.normalized(),
		ctx:      ctx,
		cancel:   cancel,
		stopCh:   make(chan struct{}),
		inFlight: make(map[string]struct{}),
	}
}

// recoverPending 启动恢复扫描：服务构造时同步执行，按 updated_at 升序
// 补做遗留 pending job（与运行期 worker 同一 applyJob 执行路径），返回
// 处理的 job 数。超过 ScanLimit 的积压由运行期轮询继续消化。
func (w *atomicIndexWorker) recoverPending(ctx context.Context) int {
	jobs, err := w.svc.syncStore.ListPendingJobs(ctx, w.cfg.ScanLimit)
	if err != nil {
		return 0
	}
	for _, job := range jobs {
		w.applyJob(ctx, job)
	}
	return len(jobs)
}

// start 启动运行期 dispatcher goroutine（构造即调用，生产接线非仅测试）。
func (w *atomicIndexWorker) start() {
	w.loopWG.Add(1)
	go w.loop()
}

// pruneDoneJobs 按保留策略清理 done 行（T13.02.c）：构造时先执行一次，
// 运行期每 PruneInterval 执行一次。清理失败只在下轮重试，不阻断索引消费；
// 只删 done 行，pending/failed 不受影响。
func (w *atomicIndexWorker) pruneDoneJobs(ctx context.Context) {
	_, _ = w.svc.syncStore.PruneDoneJobs(ctx, w.cfg.PrunePolicy)
	w.lastPruneAt = w.svc.syncStore.now()
}

// stop 关闭责任：停收新 job（dispatcher 退出）、取消在途 job 的 ctx，
// 并有界等待在途 job 退出。幂等，可重复调用。
func (w *atomicIndexWorker) stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
		w.loopWG.Wait()
		w.cancel()
		done := make(chan struct{})
		go func() {
			w.jobsWG.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(w.cfg.StopTimeout):
		}
	})
}

func (w *atomicIndexWorker) loop() {
	defer w.loopWG.Done()
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.dispatchPending()
			if w.svc.syncStore.now().Sub(w.lastPruneAt) >= w.cfg.PruneInterval {
				w.pruneDoneJobs(w.ctx)
			}
		}
	}
}

// dispatchPending 发现 pending job 并派发执行：已在执行的 memory_id 跳过
// （串行保证），并发数达上限的其余 ID 留待下轮。扫描/派发错误只在下轮
// 重试，不 panic、不阻塞 dispatcher。
func (w *atomicIndexWorker) dispatchPending() {
	jobs, err := w.svc.syncStore.ListPendingJobs(w.ctx, w.cfg.ScanLimit)
	if err != nil {
		return
	}
	for _, job := range jobs {
		select {
		case <-w.stopCh:
			return
		default:
		}
		if !w.acquire(job.MemoryID) {
			continue
		}
		w.jobsWG.Add(1)
		go func(job AtomicIndexJob) {
			defer w.jobsWG.Done()
			defer w.release(job.MemoryID)
			w.applyJob(w.ctx, job)
		}(job)
	}
}

func (w *atomicIndexWorker) acquire(memoryID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.inFlight) >= w.cfg.Concurrency {
		return false
	}
	if _, ok := w.inFlight[memoryID]; ok {
		return false
	}
	w.inFlight[memoryID] = struct{}{}
	return true
}

func (w *atomicIndexWorker) release(memoryID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.inFlight, memoryID)
}

// applyJob 执行一条同步意图。upsert 先过 tombstone/旧版本门禁（被拦截的
// stale job 标 failed 确定性终态，不复活正文、不反复重试），再复用 b1 的
// 发布路径；delete 先过 CanApplyDelete 门禁（T13.02.c：复活/重复删除已推进
// revision 的旧删除意图标 failed 终态），再复用发布路径（删除幂等）。发布
// 失败记 retry，attempts 达上限标 failed 终态。
func (w *atomicIndexWorker) applyJob(ctx context.Context, job AtomicIndexJob) {
	if job.Attempts >= w.cfg.MaxAttempts {
		w.failJob(ctx, job, fmt.Sprintf("%s(%d): %s", atomicIndexFailAttemptsExceeded, job.Attempts, job.LastError))
		return
	}
	switch job.Operation {
	case AtomicIndexOpUpsert:
		allowed, err := w.svc.syncStore.CanApplyUpsert(ctx, job.MemoryID, job.Revision)
		if err != nil {
			w.retryAndCap(ctx, job, err)
			return
		}
		if !allowed {
			w.failJob(ctx, job, fmt.Sprintf("%s: upsert blocked by newer revision or tombstone", atomicIndexFailStaleSuperseded))
			return
		}
		mem, err := w.svc.GetByID(ctx, job.MemoryID)
		if err != nil {
			if errors.Is(err, ErrAtomicMemoryNotFound) {
				// 区分物理缺失与"门禁与读之间被并发删除/推进"（T13.02.c 复核
				// 发现的竞态）：行仍存在（tombstone 或更高 revision）说明本任务
				// 已被取代，与门禁同口径标 stale 终态；仅物理缺失才报
				// memory_row_missing。
				state, serr := w.svc.syncStore.RevisionState(ctx, job.MemoryID)
				if serr == nil && state.Exists {
					w.failJob(ctx, job, fmt.Sprintf("%s: memory row deleted or superseded before publish", atomicIndexFailStaleSuperseded))
					return
				}
				w.failJob(ctx, job, fmt.Sprintf("%s: upsert job has no live memory row", atomicIndexFailMemoryRowMissing))
				return
			}
			w.retryAndCap(ctx, job, err)
			return
		}
		w.svc.publishUpsert(ctx, mem, job.Revision)
		w.capAttempts(ctx, job)
	case AtomicIndexOpDelete:
		// 删除侧 stale 门禁（T13.02.c）：复活/重复删除已推进 revision 的旧
		// 删除意图标 failed 确定性终态，不清理新版本内容的索引、不反复重试。
		allowed, err := w.svc.syncStore.CanApplyDelete(ctx, job.MemoryID, job.Revision)
		if err != nil {
			w.retryAndCap(ctx, job, err)
			return
		}
		if !allowed {
			w.failJob(ctx, job, fmt.Sprintf("%s: delete blocked by newer revision or live row", atomicIndexFailStaleSuperseded))
			return
		}
		w.svc.publishDelete(ctx, job.MemoryID, job.Revision)
		w.capAttempts(ctx, job)
	default:
		w.failJob(ctx, job, fmt.Sprintf("%s: %s", atomicIndexFailUnknownOperation, job.Operation))
	}
}

// retryAndCap 记录一次失败尝试（attempts+1 保持 pending），随后复查是否
// 已达上限，达上限标 failed 终态。
func (w *atomicIndexWorker) retryAndCap(ctx context.Context, job AtomicIndexJob, cause error) {
	_ = w.svc.syncStore.MarkRetry(ctx, job.MemoryID, job.Revision, cause)
	w.capAttempts(ctx, job)
}

// capAttempts 发布尝试后复查：job 仍 pending 且 attempts 达上限时标
// failed 终态（保留最后一次错误作为原因），保证不无限重试。
func (w *atomicIndexWorker) capAttempts(ctx context.Context, job AtomicIndexJob) {
	cur, err := w.svc.syncStore.GetJob(ctx, job.MemoryID, job.Revision)
	if err != nil || cur == nil || cur.State != AtomicIndexJobPending {
		return
	}
	if cur.Attempts >= w.cfg.MaxAttempts {
		w.failJob(ctx, *cur, fmt.Sprintf("%s(%d): %s", atomicIndexFailAttemptsExceeded, cur.Attempts, cur.LastError))
	}
}

func (w *atomicIndexWorker) failJob(ctx context.Context, job AtomicIndexJob, reason string) {
	_ = w.svc.syncStore.MarkFailed(ctx, job.MemoryID, job.Revision, reason)
}
