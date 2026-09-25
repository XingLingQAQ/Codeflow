package process

import "sync"

// ringSpool 是进程输出的有界缓冲：只保留最近 limit 字节，被丢弃的字节数被精确
// 累计（§15 T1.08：输出持续排空到有界 spool）。
//
// 写路径绝不阻塞、绝不返回错误：排空 goroutine 必须永远能立刻收下子进程写出的
// 数据，否则子进程写满管道缓冲区后会死锁——UI 慢不能反压子进程。内存上限是
// limit（加上 append 增长带来的少量余量），与子进程总共写了多少无关。
type ringSpool struct {
	mu        sync.Mutex
	limit     int64
	buf       []byte // 最旧字节在 buf[0]，最新在末尾；len(buf) <= limit
	truncated int64
}

// newRingSpool 创建上限为 limit 字节的 spool。limit <= 0 表示一个字节都不保留，
// 全部计入截断（调用方在此之前已把 0 换成默认值）。
func newRingSpool(limit int64) *ringSpool {
	if limit < 0 {
		limit = 0
	}
	sp := &ringSpool{limit: limit}
	// 上限不超过 1 MiB 时一次性分配：缓冲满之后每写一次都会先丢弃再追加，
	// 长度永远不会超过 cap，因此不会触发 append 的翻倍增长。上限更大时按需
	// 增长（不会超过 limit），避免为一条输出线预留 GB 级内存。
	if limit > 0 && limit <= 1<<20 {
		sp.buf = make([]byte, 0, limit)
	}
	return sp
}

// write 追加一段输出。超过上限的部分从最旧一侧丢弃并计入截断字节数。
func (sp *ringSpool) write(p []byte) {
	if len(p) == 0 {
		return
	}
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.limit <= 0 {
		sp.truncated += int64(len(p))
		return
	}
	if int64(len(p)) >= sp.limit {
		// 单次写入就超过上限：只留最后 limit 字节。
		keep := p[int64(len(p))-sp.limit:]
		sp.buf = append(sp.buf[:0], keep...)
		sp.truncated += int64(len(p)) - sp.limit
		return
	}
	if overflow := int64(len(sp.buf)) + int64(len(p)) - sp.limit; overflow > 0 {
		copy(sp.buf, sp.buf[overflow:])
		sp.buf = sp.buf[:int64(len(sp.buf))-overflow]
		sp.truncated += overflow
	}
	sp.buf = append(sp.buf, p...)
}

// truncatedBytes 返回累计被丢弃的字节数。
func (sp *ringSpool) truncatedBytes() int64 {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.truncated
}

// Snapshot 返回当前保留输出的副本与累计被丢弃的字节数。返回的是拷贝：调用方
// 改动它不会影响后续快照，也不会与排空 goroutine 争用同一块内存。
func (sp *ringSpool) Snapshot() ([]byte, int64) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	out := make([]byte, len(sp.buf))
	copy(out, sp.buf)
	return out, sp.truncated
}
