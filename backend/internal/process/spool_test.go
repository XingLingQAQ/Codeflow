package process

import (
	"bytes"
	"sync"
	"sync/atomic"
	"testing"
)

// TestRingSpoolKeepsTailAndCountsTruncation：超过上限时保留最近字节，截断计数精确。
func TestRingSpoolKeepsTailAndCountsTruncation(t *testing.T) {
	cases := []struct {
		name      string
		limit     int64
		writes    [][]byte
		wantTail  []byte
		wantTrunc int64
	}{
		{"under limit", 8, [][]byte{[]byte("abc")}, []byte("abc"), 0},
		{"exactly at limit", 3, [][]byte{[]byte("abc")}, []byte("abc"), 0},
		{"one over limit", 3, [][]byte{[]byte("abcd")}, []byte("bcd"), 1},
		{"many small writes over limit", 4, [][]byte{[]byte("ab"), []byte("cd"), []byte("ef")}, []byte("cdef"), 2},
		{"single write larger than limit", 2, [][]byte{[]byte("wxyz")}, []byte("yz"), 2},
		{"zero limit drops everything", 0, [][]byte{[]byte("abc")}, []byte{}, 3},
		{"empty write is a no-op", 4, [][]byte{{}, []byte("ab")}, []byte("ab"), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sp := newRingSpool(tc.limit)
			for _, p := range tc.writes {
				sp.write(p)
			}
			got, truncated := sp.Snapshot()
			if !bytes.Equal(got, tc.wantTail) {
				t.Fatalf("Snapshot data = %q, want %q", got, tc.wantTail)
			}
			if truncated != tc.wantTrunc {
				t.Fatalf("Snapshot truncated = %d, want %d", truncated, tc.wantTrunc)
			}
		})
	}
}

// TestRingSpoolSnapshotIsCopy：快照是拷贝，调用方改动它不能影响 spool。
func TestRingSpoolSnapshotIsCopy(t *testing.T) {
	sp := newRingSpool(4)
	sp.write([]byte("abcd"))
	got, _ := sp.Snapshot()
	got[0] = 'z'
	again, _ := sp.Snapshot()
	if !bytes.Equal(again, []byte("abcd")) {
		t.Fatalf("Snapshot after mutating a previous copy = %q, want %q", again, "abcd")
	}
}

// TestRingSpoolNeverExceedsLimit：任意写入序列下保留量都不超过上限。
func TestRingSpoolNeverExceedsLimit(t *testing.T) {
	const limit = 64
	sp := newRingSpool(limit)
	var total int64
	for i := 0; i < 200; i++ {
		chunk := bytes.Repeat([]byte{byte(i)}, i%37)
		sp.write(chunk)
		total += int64(len(chunk))
		data, truncated := sp.Snapshot()
		if int64(len(data)) > limit {
			t.Fatalf("write #%d: retained %d bytes, limit %d", i, len(data), limit)
		}
		if truncated+int64(len(data)) != total {
			t.Fatalf("write #%d: retained %d + truncated %d != total %d", i, len(data), truncated, total)
		}
	}
}

// TestRingSpoolConcurrentWriteAndSnapshot：排空 goroutine 写、UI 侧读快照并发时不能
// 出现数据竞争（-race 下可验证）；保留量仍然有界。
func TestRingSpoolConcurrentWriteAndSnapshot(t *testing.T) {
	sp := newRingSpool(256)
	const writers, iterations = 4, 500
	var wg sync.WaitGroup
	wg.Add(writers)
	for w := 0; w < writers; w++ {
		go func(w int) {
			defer wg.Done()
			payload := bytes.Repeat([]byte{byte('a' + w)}, 33)
			for i := 0; i < iterations; i++ {
				sp.write(payload)
			}
		}(w)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()
	var snapshots int64
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-done:
				return
			default:
			}
			data, truncated := sp.Snapshot()
			if int64(len(data)) > 256 || truncated < 0 {
				t.Errorf("snapshot = (%d bytes, %d truncated)", len(data), truncated)
				return
			}
			atomic.AddInt64(&snapshots, 1)
		}
	}()
	wg.Wait()
	<-readerDone
	if atomic.LoadInt64(&snapshots) == 0 {
		t.Fatalf("no snapshot was taken while writers were running")
	}
}

// TestRingSpoolLimitAboveInitialAllocation：上限超过预分配阈值时（>1 MiB）按需增长，
// 仍然精确保留最近字节。
func TestRingSpoolLimitAboveInitialAllocation(t *testing.T) {
	const limit = 2 << 20
	sp := newRingSpool(limit)
	if cap(sp.buf) > 1<<20 {
		t.Fatalf("initial capacity %d, want <= 1 MiB (lazy growth)", cap(sp.buf))
	}
	total := int64(3 << 20)
	chunk := bytes.Repeat([]byte("0123456789abcdef"), 4096) // 64 KiB
	var written int64
	for written < total {
		n := int64(len(chunk))
		if remaining := total - written; remaining < n {
			n = remaining
		}
		sp.write(chunk[:n])
		written += n
	}
	data, truncated := sp.Snapshot()
	if int64(len(data)) != limit {
		t.Fatalf("retained %d bytes, want %d", len(data), limit)
	}
	if want := total - limit; truncated != want {
		t.Fatalf("truncated = %d, want %d", truncated, want)
	}
	if !bytes.Equal(data, chunk[len(chunk)-int(limit)%len(chunk):]) {
		// 只做长度与尾部一致性检查：尾部内容必须等于按同周期写入的期望值。
		for i := 0; i < len(data); i++ {
			want := chunk[(int(total)-len(data)+i)%len(chunk)]
			if data[i] != want {
				t.Fatalf("data[%d] = %q, want %q", i, data[i], want)
			}
		}
	}
}
