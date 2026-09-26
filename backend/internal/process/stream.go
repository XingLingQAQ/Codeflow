package process

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

// 本文件实现“实时按行订阅”与 spool 写入侧过滤钩子（§15 T1.08 第 2 组）。
//
// 设计要点：
//   - 排空 goroutine 是唯一的产出方：它按 \n 切行（兼容 \r\n），把完整行非阻塞地
//     投递给每个订阅者。订阅者缓冲区满只会终止该订阅，绝不反压子进程（UI/适配器慢
//     不能停止读管道）。
//   - stdout 与 stderr 各自一条流、各自一个序号序列，不混流。
//   - SpoolFilter 只作用于写入 spool 的字节（日志用途），订阅者收到的行始终是原始
//     字节：适配器要解析协议帧，不能被展示层的脱敏改写。真正的流式密钥脱敏器归
//     T2.04，本组只提供接口与接线。
var (
	// ErrInvalidStream：Subscribe 收到的不是 stdout/stderr。
	ErrInvalidStream = errors.New("process: unknown output stream")
	// ErrInvalidSubscribeOptions：BufferLines/MaxLineBytes 为负。
	ErrInvalidSubscribeOptions = errors.New("process: invalid subscribe options")
	// ErrSubscriberOverflow：订阅者缓冲区已满，该订阅被立即终止（channel 关闭）。
	// 丢失的第一行序号见 Subscription.DroppedSeq。这只影响该订阅：其他订阅者与
	// spool 不受影响，子进程继续运行。
	//
	// 责任边界：计划要求“关键协议帧无法持久化则终止执行并失败”。本包不做这个决定，
	// 由适配器（T1.13）在 Err() 返回本错误后调用 Handle.Cancel(CancelForce) 实现。
	ErrSubscriberOverflow = errors.New("process: subscriber buffer overflowed; subscription terminated")
	// ErrSubscriptionClosed：调用方自己调用了 Close，订阅按调用方意愿结束。
	ErrSubscriptionClosed = errors.New("process: subscription closed by the caller")
)

const (
	// DefaultBufferLines 是 SubscribeOptions.BufferLines 为 0 时的订阅缓冲行数。
	DefaultBufferLines = 1024
	// DefaultMaxLineBytes 是 SubscribeOptions.MaxLineBytes 为 0 时的单行字节上限。
	DefaultMaxLineBytes = 1 << 20

	// maxUnclaimedLines / maxUnclaimedBytes 是“还没有任何订阅者认领的行”的预算。
	// 它存在的唯一理由是消除竞态：Start 返回后进程可能已经写了若干行（甚至已经退出），
	// 此时立刻 Subscribe 不得丢行（这些行还没被任何人认领，不是“回放历史”）。超出预算
	// 丢最旧的行并计数（见 streamWriter.droppedUncl）；丢过行则第一个订阅者以
	// ErrSubscriberOverflow 结束，不把缺了开头的流当作完整的流交出去。
	maxUnclaimedLines = 1024
	maxUnclaimedBytes = 1 << 20

	// spoolFlushBytes 是 spool 写入单元的上限：正常行以 \n 收尾，超长行或完全不含
	// 换行的流（例如二进制洪峰）按此大小分片写入，保证 spool 内存有界且字节不丢。
	// 过滤器看到的“行”在这种流上是分片，这是流式脱敏的已知边界（真正的行级脱敏
	// 归 T2.04）。
	spoolFlushBytes = 64 << 10

	// lineYieldEvery 是排空侧每投递多少行让出一次处理器（见 streamWriter.deliver）。
	// 输出洪峰下订阅者与排空在同一个后端进程里抢处理器，长时间不让出会让订阅者
	// 来不及取走缓冲、被误判为溢出。
	lineYieldEvery = 256
)

// Stream 标识受监督进程的一路输出。stdout 与 stderr 分开订阅、分开成行，不混流。
type Stream string

const (
	// StreamStdout 是标准输出流。
	StreamStdout Stream = "stdout"
	// StreamStderr 是标准错误流。
	StreamStderr Stream = "stderr"
)

// Valid 报告 s 是否是可订阅的输出流。
func (s Stream) Valid() bool { return s == StreamStdout || s == StreamStderr }

// SpoolFilter 在字节写入 spool 之前改写它们（典型用途是脱敏）。
//
// 契约：
//   - 只作用于 spool（日志）内容；订阅者收到的行不受影响。
//   - line 是即将写入 spool 的原始字节：完整行含行尾换行符（\n 或 \r\n）；超长行
//     与不含换行的流按 spoolFlushBytes 分片（见该常量的注释）。返回值被原样写入
//     spool（本包不增删任何字节）；返回 nil 或空切片表示该单元不写入 spool。
//   - line 的内存归本包所有，过滤器不得保留切片或在其返回后继续使用。
//   - stdout/stderr 两路会并发调用过滤器，实现必须并发安全。
//   - panic 被本包恢复并计数（见 procHandle.spoolFilterPanics），排空不会因此中断，
//     子进程不会被拖死。该单元的原始字节不写入 spool，改写一行占位说明（见
//     filterFailedPlaceholder）：过滤器的典型用途是脱敏，坏掉的脱敏器不能让密钥
//     原样落进日志（失败即关闭）。
type SpoolFilter func(stream Stream, line []byte) []byte

// filterFailedPlaceholder 是过滤器 panic 时代替原始单元写入 spool 的内容：只说明
// 扣下了多少字节，不含原始内容。
func filterFailedPlaceholder(n int) []byte {
	return []byte(fmt.Sprintf("[spool filter failed: %d byte(s) withheld]\n", n))
}

// SubscribeOptions 是一次订阅的配置。零值表示采用默认值；负值被拒绝。
type SubscribeOptions struct {
	// BufferLines 是订阅 channel 的缓冲行数（0 表示 DefaultBufferLines）。
	// 它是“订阅者允许落后多少行”的承诺：满了即判定溢出并终止该订阅。
	BufferLines int
	// MaxLineBytes 是单行投递的字节上限（0 表示 DefaultMaxLineBytes）。超长行被
	// 截断投递（Line.Truncated=true，Data 长度恰为上限），超出上限的字节被丢弃。
	// 它同时是该订阅在流侧的单行积累上限：本包不会为一行保留超过此值的字节。
	MaxLineBytes int
}

// Validate 拒绝负数配置。
func (o SubscribeOptions) Validate() error {
	if o.BufferLines < 0 {
		return fmt.Errorf("%w: buffer lines %d is negative", ErrInvalidSubscribeOptions, o.BufferLines)
	}
	if o.MaxLineBytes < 0 {
		return fmt.Errorf("%w: max line bytes %d is negative", ErrInvalidSubscribeOptions, o.MaxLineBytes)
	}
	return nil
}

// withDefaults 把 0 换成默认值。
func (o SubscribeOptions) withDefaults() SubscribeOptions {
	if o.BufferLines == 0 {
		o.BufferLines = DefaultBufferLines
	}
	if o.MaxLineBytes == 0 {
		o.MaxLineBytes = DefaultMaxLineBytes
	}
	return o
}

// Line 是投递给订阅者的一行输出。
type Line struct {
	// Seq 是该行在本流内的序号，从 1 递增（stdout 与 stderr 各自独立计数）。
	Seq int64
	// Data 是行内容，不含行尾换行符（\r\n 的 \r 也被去掉）。多个订阅者共享同一份
	// 底层数组，调用方只读，不得修改。
	Data []byte
	// Truncated 表示这一行超过了该订阅的 MaxLineBytes（或被流侧积累上限截断），
	// Data 只是前缀，内容不完整。
	Truncated bool
}

// Subscription 是一个订阅句柄。
//
// 生命周期：C() 上的 channel 由监督器在“进程结束且所有行投递完毕”后关闭（Err() 为
// nil），或因订阅者缓冲区溢出被提前关闭（Err() 为 ErrSubscriberOverflow；第一个订阅
// 者认领的未认领行已经超出过预算时也是这个结果）。调用方也可以主动 Close()（Err() 为
// ErrSubscriptionClosed）。
type Subscription struct {
	stream       Stream
	maxLineBytes int
	ch           chan Line
	w            *streamWriter // 状态锁的持有者；订阅创建后一直有效

	// 以下字段由 w.mu 保护。
	err        error
	droppedSeq int64
	closed     bool
}

// C 返回只读的行 channel。
func (s *Subscription) C() <-chan Line { return s.ch }

// Err 返回订阅的终止原因：nil 表示流正常结束（所有行已投递），
// ErrSubscriberOverflow 表示该订阅因缓冲区满被终止（丢行序号见 DroppedSeq），
// ErrSubscriptionClosed 表示调用方自己关闭了订阅。终止原因一经确定不再改变。
func (s *Subscription) Err() error {
	s.w.mu.Lock()
	defer s.w.mu.Unlock()
	return s.err
}

// DroppedSeq 返回因溢出而丢失的第一行序号（0 表示没有丢过行）。
func (s *Subscription) DroppedSeq() int64 {
	s.w.mu.Lock()
	defer s.w.mu.Unlock()
	return s.droppedSeq
}

// Close 结束订阅：channel 被关闭，之后不再收到任何行。幂等。
func (s *Subscription) Close() {
	s.w.mu.Lock()
	defer s.w.mu.Unlock()
	if s.closed {
		return
	}
	s.finishLocked(ErrSubscriptionClosed)
	s.w.dead++
}

// finishLocked 关闭 channel 并记下终止原因（第一次生效）。调用方必须持有 w.mu。
func (s *Subscription) finishLocked(err error) {
	if s.closed {
		return
	}
	s.closed = true
	if err != nil {
		s.err = err
	}
	close(s.ch)
}

// deliverLocked 非阻塞投递一行；缓冲区满即判定溢出并终止该订阅。
// 调用方必须持有 w.mu（保证不与 Close/结束时的 close 并发，避免向已关闭 channel 发送）。
func (s *Subscription) deliverLocked(l Line) {
	if s.closed {
		return
	}
	if max := s.maxLineBytes; len(l.Data) > max {
		l.Data = l.Data[:max]
		l.Truncated = true
	}
	select {
	case s.ch <- l:
	default:
		s.droppedSeq = l.Seq
		s.finishLocked(fmt.Errorf("%w: %s subscription dropped line %d (buffer full)",
			ErrSubscriberOverflow, s.stream, l.Seq))
	}
}

// streamWriter 是一路输出的行解析、spool 写入与订阅分发器。stdout/stderr 各一个。
//
// 只有该路的排空 goroutine 触碰 unit/lineBuf/lineTruncated 与 spool；订阅者相关状态
// 由 mu 保护（Subscribe/Close/finalize 可能来自其他 goroutine）。
type streamWriter struct {
	stream Stream
	spool  *ringSpool
	filter SpoolFilter

	// unit 是当前 spool 写入单元（整行或分片），lineBuf 是当前行已积累的字节。
	unit          []byte
	lineBuf       []byte
	lineTruncated bool

	// lineCap 是当前的单行积累上限（订阅者 MaxLineBytes 的最大值，无订阅时为默认值）。
	lineCap atomic.Int64
	// filterPanics 是过滤器 panic 的次数（恢复后写占位说明，不写原始字节）。
	filterPanics atomic.Int64

	mu             sync.Mutex
	subs           []*Subscription
	dead           int
	unclaimed      []Line
	unclaimedBytes int
	everSubscribed bool
	nextSeq        int64
	finished       bool
	droppedUncl    int64
	// firstDroppedUncl 是第一条因超出未认领预算而丢弃的行序号（0 表示没丢过）。
	firstDroppedUncl int64
}

func newStreamWriter(stream Stream, spool *ringSpool, filter SpoolFilter) *streamWriter {
	w := &streamWriter{stream: stream, spool: spool, filter: filter}
	w.lineCap.Store(DefaultMaxLineBytes)
	return w
}

// subscribe 注册一个新的订阅者。
//
// 第一个订阅者无论何时到来都先接管“还没被任何人认领的行”——包括进程在它订阅之前
// 就已经退出的情形：快速失败的进程可能在 Start 返回前就写完输出并结束，这些行从未
// 投递给任何人，丢掉它们会让适配器把“有输出”误判成“无输出”。未认领行曾超出预算时，
// 第一个订阅者以 ErrSubscriberOverflow 结束（DroppedSeq 为第一条丢失的行），不交出
// 缺了开头的流。之后的订阅者只收新行；流已结束时返回已关闭、Err() 为 nil 的订阅
// （不回放历史：需要历史用 Spool 快照）。
func (w *streamWriter) subscribe(opts SubscribeOptions) *Subscription {
	sub := &Subscription{
		stream:       w.stream,
		maxLineBytes: opts.MaxLineBytes,
		ch:           make(chan Line, opts.BufferLines),
		w:            w,
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.everSubscribed {
		w.everSubscribed = true
		pending := w.unclaimed
		w.unclaimed, w.unclaimedBytes = nil, 0
		if w.firstDroppedUncl > 0 {
			sub.droppedSeq = w.firstDroppedUncl
			sub.finishLocked(fmt.Errorf("%w: %s lost %d unclaimed line(s) from line %d before the first subscription",
				ErrSubscriberOverflow, w.stream, w.droppedUncl, w.firstDroppedUncl))
			return sub
		}
		for _, l := range pending {
			sub.deliverLocked(l)
			if sub.closed {
				// 自己的缓冲区装不下未认领的行：已按溢出终止，不再登记。
				return sub
			}
		}
	}
	if w.finished {
		sub.finishLocked(nil)
		return sub
	}
	if w.dead > 0 {
		w.compactLocked()
	}
	w.subs = append(w.subs, sub)
	if int64(opts.MaxLineBytes) > w.lineCap.Load() {
		w.lineCap.Store(int64(opts.MaxLineBytes))
	}
	return sub
}

// compactLocked 清理已结束的订阅（调用方必须持有 mu，且不得在遍历 subs 时调用）。
func (w *streamWriter) compactLocked() {
	live := w.subs[:0]
	for _, s := range w.subs {
		if !s.closed {
			live = append(live, s)
		}
	}
	w.subs, w.dead = live, 0
}

// feed 消费一段排空读到的字节：切行、写 spool、投递订阅者。绝不阻塞。
func (w *streamWriter) feed(chunk []byte) {
	for len(chunk) > 0 {
		i := bytes.IndexByte(chunk, '\n')
		if i < 0 {
			w.appendUnit(chunk)
			w.appendLine(chunk)
			if len(w.unit) >= spoolFlushBytes {
				w.flushUnit()
			}
			return
		}
		w.appendUnit(chunk[:i+1])
		w.appendLine(chunk[:i])
		w.flushUnit()
		w.completeLine()
		chunk = chunk[i+1:]
	}
}

// finishPending 冲刷流结束时残留的最后一个 spool 分片与最后一段不完整的行
// （进程结束时最后一段不完整的行也要投递）。
func (w *streamWriter) finishPending() {
	w.flushUnit()
	if len(w.lineBuf) > 0 || w.lineTruncated {
		w.completeLine()
	}
}

// appendUnit 把字节追加到 spool 写入单元（无换行流按 spoolFlushBytes 分片）。
func (w *streamWriter) appendUnit(p []byte) { w.unit = append(w.unit, p...) }

// appendLine 把字节追加到当前行，超过 lineCap 的部分丢弃并标记截断。
func (w *streamWriter) appendLine(p []byte) {
	limit := int(w.lineCap.Load())
	if len(w.lineBuf) >= limit {
		w.lineTruncated = true
		return
	}
	if room := limit - len(w.lineBuf); len(p) > room {
		w.lineBuf = append(w.lineBuf, p[:room]...)
		w.lineTruncated = true
		return
	}
	w.lineBuf = append(w.lineBuf, p...)
}

// flushUnit 把一个写入单元过滤后写进 spool。过滤器 panic 被恢复：写占位说明并计数。
func (w *streamWriter) flushUnit() {
	if len(w.unit) == 0 {
		return
	}
	unit := w.unit
	if w.filter != nil {
		unit = w.applyFilter(unit)
	}
	if len(unit) > 0 {
		w.spool.write(unit)
	}
	w.unit = w.unit[:0]
}

func (w *streamWriter) applyFilter(unit []byte) (out []byte) {
	defer func() {
		if r := recover(); r != nil {
			// 过滤器坏了不能拖垮排空（§15：不能因过滤失败停止读管道），也不能让未脱敏的
			// 原始字节落进日志：写占位说明，计数留证。
			w.filterPanics.Add(1)
			out = filterFailedPlaceholder(len(unit))
		}
	}()
	return w.filter(w.stream, unit)
}

// completeLine 结束当前行并投递：去掉行尾换行（含 \r\n 的 \r），拷贝出稳定的字节切片。
func (w *streamWriter) completeLine() {
	data := append([]byte(nil), w.lineBuf...)
	truncated := w.lineTruncated
	w.lineBuf, w.lineTruncated = w.lineBuf[:0], false
	if n := len(data); n > 0 && data[n-1] == '\r' {
		data = data[:n-1]
	}
	w.deliver(Line{Data: data, Truncated: truncated})
}

// deliver 把一行投递给所有订阅者（非阻塞），或放进“未认领”缓冲等第一个订阅者。
func (w *streamWriter) deliver(l Line) {
	w.mu.Lock()
	if w.finished {
		w.mu.Unlock()
		return
	}
	w.nextSeq++
	l.Seq = w.nextSeq
	if !w.everSubscribed {
		w.bufferUnclaimedLocked(l)
		w.mu.Unlock()
		return
	}
	if w.dead > 0 {
		w.compactLocked()
	}
	for _, sub := range w.subs {
		sub.deliverLocked(l)
		if sub.closed {
			w.dead++
		}
	}
	seq := w.nextSeq
	hasSubs := len(w.subs) > 0
	w.mu.Unlock()

	// 每投递若干行让出一次处理器：排空必须一直读管道（否则子进程会写满管道死锁），
	// 但也不能把同进程的订阅者（UI/适配器就在同一个后端进程里）饿到缓冲区溢出。
	// Gosched 不阻塞、不改变投递语义，只是让运行队列里的其他 goroutine 有机会运行。
	if hasSubs && seq%lineYieldEvery == 0 {
		runtime.Gosched()
	}
}

// bufferUnclaimedLocked 保留一行供第一个订阅者认领，超出预算丢最旧的并计数。
func (w *streamWriter) bufferUnclaimedLocked(l Line) {
	for (len(w.unclaimed) >= maxUnclaimedLines || w.unclaimedBytes+len(l.Data) > maxUnclaimedBytes) && len(w.unclaimed) > 0 {
		old := w.unclaimed[0]
		w.unclaimed = w.unclaimed[1:]
		w.unclaimedBytes -= len(old.Data)
		w.noteUnclaimedDropLocked(old.Seq)
	}
	if len(w.unclaimed) == 0 && len(l.Data) > maxUnclaimedBytes {
		w.noteUnclaimedDropLocked(l.Seq)
		return
	}
	w.unclaimed = append(w.unclaimed, l)
	w.unclaimedBytes += len(l.Data)
}

// noteUnclaimedDropLocked 记一条未认领行的丢弃（调用方必须持有 mu）。
func (w *streamWriter) noteUnclaimedDropLocked(seq int64) {
	w.droppedUncl++
	if w.firstDroppedUncl == 0 {
		w.firstDroppedUncl = seq
	}
}

// finalize 结束本流：关闭所有订阅 channel（Err() 为 nil）。还没有订阅者时保留未认领
// 的行留给第一个订阅者（见 subscribe），内存仍受未认领预算约束。
// 幂等；由排空 goroutine 在 EOF 后调用，finish() 也会兜底调用一次（防止排空超时）。
func (w *streamWriter) finalize() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished {
		return
	}
	w.finished = true
	for _, sub := range w.subs {
		sub.finishLocked(nil)
	}
	w.subs, w.dead = nil, 0
}

// unclaimedDropped 返回因超出“未认领”预算而丢弃的行数（诊断用）。
func (w *streamWriter) unclaimedDropped() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.droppedUncl
}
