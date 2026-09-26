package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 本文件的测试全部经 Supervisor 启动 helper（测试二进制自我重入），结束时都确认没有
// 残留进程：每个 Start 都登记了强制清理（startHandle/startHandleWith）。
//
// 确定性做法：需要“订阅一定先于输出”的场景用 lines helper 的 RELEASE 闸门（订阅完再
// 放行），需要“输出一定先于订阅”的场景用 HOLD 闸门（进程写完行后保持存活）。

// linesSpec 构造一个 lines helper 的合法 Spec。
func linesSpec(t *testing.T, extra ...string) Spec {
	t.Helper()
	return supervisorSpec(t, helperModeLines, extra...)
}

// startHandleWith 用给定 supervisor（可带 SpoolFilter）启动进程并登记清理。
func startHandleWith(t *testing.T, sup Supervisor, spec Spec) Handle {
	t.Helper()
	allowProcessStart(t)
	h, err := sup.Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { reapHandle(t, h) })
	return h
}

// subscribeOrFail 订阅一路输出。
func subscribeOrFail(t *testing.T, h Handle, stream Stream, opts SubscribeOptions) *Subscription {
	t.Helper()
	sub, err := h.Subscribe(stream, opts)
	if err != nil {
		t.Fatalf("Subscribe(%s, %+v): %v", stream, opts, err)
	}
	t.Cleanup(sub.Close)
	return sub
}

// collectLines 读空订阅 channel，返回收到的行与终止原因。
func collectLines(t *testing.T, sub *Subscription, timeout time.Duration) ([]Line, error) {
	t.Helper()
	deadline := time.After(timeout)
	var out []Line
	for {
		select {
		case l, ok := <-sub.C():
			if !ok {
				return out, sub.Err()
			}
			out = append(out, l)
		case <-deadline:
			t.Fatalf("subscription %s did not close within %s after %d line(s)", sub.stream, timeout, len(out))
			return out, nil
		}
	}
}

// readLines 读满 n 行（不等 channel 关闭）。
func readLines(t *testing.T, sub *Subscription, n int, timeout time.Duration) []Line {
	t.Helper()
	deadline := time.After(timeout)
	out := make([]Line, 0, n)
	for len(out) < n {
		select {
		case l, ok := <-sub.C():
			if !ok {
				t.Fatalf("subscription closed after %d line(s), want %d (Err = %v)", len(out), n, sub.Err())
			}
			out = append(out, l)
		case <-deadline:
			t.Fatalf("only %d of %d line(s) arrived within %s", len(out), n, timeout)
		}
	}
	return out
}

// expectLine 校验一行：序号、内容、是否截断。
func expectLine(t *testing.T, l Line, wantSeq int64, wantData string, wantTruncated bool) {
	t.Helper()
	if l.Seq != wantSeq {
		t.Fatalf("line Seq = %d, want %d (data %q)", l.Seq, wantSeq, l.Data)
	}
	if string(l.Data) != wantData {
		t.Fatalf("line %d data = %q, want %q", l.Seq, l.Data, wantData)
	}
	if l.Truncated != wantTruncated {
		t.Fatalf("line %d Truncated = %v, want %v", l.Seq, l.Truncated, wantTruncated)
	}
}

// TestSubscribeReceivesAllLinesInOrder：10 万行、订阅者持续读 → Seq 连续 1..N、
// 内容逐行一致；stdout 与 stderr 各自独立、不混流。
func TestSubscribeReceivesAllLinesInOrder(t *testing.T) {
	const (
		stdoutLines = 100000
		stderrLines = 500
	)
	report, release := helperDir(t)
	spec := linesSpec(t,
		helperLinesEnv+"="+fmt.Sprint(stdoutLines),
		helperStderrEnv+"="+fmt.Sprint(stderrLines),
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
	)
	h := startHandleWith(t, testSupervisor(), spec)
	// 缓冲给得比默认值宽：10 万行是一次真正的输出洪峰，订阅者与排空在同一个测试进程
	// 里抢处理器（本轮还有并行的其他测试），默认 1024 行会让读取方被误判为溢出。
	outSub := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{BufferLines: 16384})
	errSub := subscribeOrFail(t, h, StreamStderr, SubscribeOptions{BufferLines: 1024})
	releaseHelper(t, release)

	started := time.Now()
	stdout, outErr := collectLines(t, outSub, testWaitLong)
	stderr, errErr := collectLines(t, errSub, testWaitLong)
	t.Logf("read %d stdout + %d stderr line(s) in %s", len(stdout), len(stderr), time.Since(started))
	if outErr != nil {
		t.Fatalf("stdout subscription ended with %v, want nil", outErr)
	}
	if errErr != nil {
		t.Fatalf("stderr subscription ended with %v, want nil", errErr)
	}
	if len(stdout) != stdoutLines {
		t.Fatalf("stdout line count = %d, want %d", len(stdout), stdoutLines)
	}
	if len(stderr) != stderrLines {
		t.Fatalf("stderr line count = %d, want %d", len(stderr), stderrLines)
	}
	for i, l := range stdout {
		want := helperLineContent("line", i+1, 0)
		if l.Seq != int64(i+1) {
			t.Fatalf("stdout line #%d Seq = %d, want %d (seq must be contiguous from 1)", i, l.Seq, i+1)
		}
		if string(l.Data) != want {
			t.Fatalf("stdout line %d = %q, want %q", l.Seq, l.Data, want)
		}
		if strings.Contains(string(l.Data), "err-") {
			t.Fatalf("stdout line %d = %q: streams are mixed", l.Seq, l.Data)
		}
	}
	for i, l := range stderr {
		want := helperLineContent("err", i+1, 0)
		if l.Seq != int64(i+1) {
			t.Fatalf("stderr line #%d Seq = %d, want %d (per-stream sequence)", i, l.Seq, i+1)
		}
		if string(l.Data) != want {
			t.Fatalf("stderr line %d = %q, want %q", l.Seq, l.Data, want)
		}
	}

	exit := waitExit(t, h, testWaitLong)
	if exit.Reason != ExitExited || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("Exit = %+v, want exited with code 0", exit)
	}
	if exit.SpoolTruncatedBytes != 0 {
		t.Errorf("Exit.SpoolTruncatedBytes = %d, want 0 (output fits the spool)", exit.SpoolTruncatedBytes)
	}
}

// TestSlowSubscriberOverflowDoesNotStallProcess：BufferLines=8 且完全不读的订阅者以
// ErrSubscriberOverflow 终止；另一个正常读取的订阅者收到全部行；子进程照常退出；
// spool 截断计数与第 1 组口径一致（精确字节、每路独立计上限）。
func TestSlowSubscriberOverflowDoesNotStallProcess(t *testing.T) {
	const (
		lineCount = 200
		maxSpool  = 512
	)
	report, release := helperDir(t)
	spec := linesSpec(t,
		helperLinesEnv+"="+fmt.Sprint(lineCount),
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
	)
	spec.MaxSpoolBytes = maxSpool
	h := startHandleWith(t, testSupervisor(), spec)
	slow := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{BufferLines: 8})
	good := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{})
	releaseHelper(t, release)

	started := time.Now()
	lines, err := collectLines(t, good, testWaitLong)
	if err != nil {
		t.Fatalf("healthy subscription ended with %v, want nil", err)
	}
	if len(lines) != lineCount {
		t.Fatalf("healthy subscriber got %d line(s), want %d", len(lines), lineCount)
	}
	for i, l := range lines {
		if want := helperLineContent("line", i+1, 0); string(l.Data) != want || l.Seq != int64(i+1) {
			t.Fatalf("healthy subscriber line #%d = (%d, %q), want (%d, %q)", i, l.Seq, l.Data, i+1, want)
		}
	}

	// 慢订阅者：缓冲区恰好存下 8 行，第 9 行投递失败即被终止（channel 关闭）。
	buffered, slowErr := collectLines(t, slow, testWaitShort)
	if !errors.Is(slowErr, ErrSubscriberOverflow) {
		t.Fatalf("slow subscriber Err = %v, want ErrSubscriberOverflow", slowErr)
	}
	if len(buffered) != 8 {
		t.Errorf("slow subscriber received %d buffered line(s), want exactly 8 (BufferLines)", len(buffered))
	}
	if want := int64(9); slow.DroppedSeq() != want {
		t.Errorf("slow subscriber DroppedSeq = %d, want %d (first dropped line)", slow.DroppedSeq(), want)
	}

	exit := waitExit(t, h, testWaitShort)
	elapsed := time.Since(started)
	t.Logf("process exited %s after release with %+v (slow subscriber overflowed, healthy one read %d lines)",
		elapsed, exit, len(lines))
	if elapsed >= testWaitShort {
		t.Errorf("process took %s to exit, want < %s: a full subscriber buffer must not stall the process", elapsed, testWaitShort)
	}
	if exit.Reason != ExitExited || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("Exit = %+v, want exited with code 0", exit)
	}

	// spool 截断口径与第 1 组一致：保留最近 maxSpool 字节，其余精确计入截断。
	total := int64(lineCount) * int64(len(helperLineContent("line", 1, 0))+1)
	wantTruncated := total - maxSpool
	if exit.SpoolTruncatedBytes != wantTruncated {
		t.Errorf("Exit.SpoolTruncatedBytes = %d, want %d (total %d - limit %d)",
			exit.SpoolTruncatedBytes, wantTruncated, total, maxSpool)
	}
	data, truncated := h.Output().Snapshot()
	if int64(len(data)) != maxSpool || truncated != wantTruncated {
		t.Errorf("stdout spool = (%d bytes, %d truncated), want (%d, %d)", len(data), truncated, maxSpool, wantTruncated)
	}
	if want := helperLineContent("line", lineCount, 0); !strings.HasSuffix(strings.TrimRight(string(data), "\n"), want) {
		t.Errorf("stdout spool does not end with the last line %q: %q", want, data)
	}
	if stderrData, stderrTrunc := h.Stderr().Snapshot(); len(stderrData) != 0 || stderrTrunc != 0 {
		t.Errorf("stderr spool = (%d bytes, %d truncated), want (0, 0)", len(stderrData), stderrTrunc)
	}
}

// TestPartialLastLineDeliveredAtExit：没有结尾换行的最后一段也要投递。
func TestPartialLastLineDeliveredAtExit(t *testing.T) {
	spec := linesSpec(t, helperLinesEnv+"=3", helperLineNoEOLEnv+"=1")
	h := startHandleWith(t, testSupervisor(), spec)
	sub := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{})
	lines, err := collectLines(t, sub, testWaitLong)
	if err != nil {
		t.Fatalf("subscription ended with %v, want nil", err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d line(s), want 3 (the partial last line must be delivered): %+v", len(lines), lines)
	}
	for i, l := range lines {
		expectLine(t, l, int64(i+1), helperLineContent("line", i+1, 0), false)
	}
	if exit := waitExit(t, h, testWaitShort); exit.Reason != ExitExited || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("Exit = %+v, want exited with code 0", exit)
	}
	// spool 收到的是原始字节（最后一行没有换行符）。
	data, _ := h.Output().Snapshot()
	if want := strings.Join([]string{helperLineContent("line", 1, 0), helperLineContent("line", 2, 0), helperLineContent("line", 3, 0)}, "\n"); string(data) != want {
		t.Errorf("stdout spool = %q, want %q", data, want)
	}
}

// TestLongLineTruncated：超过 MaxLineBytes 的行按订阅上限截断（Data 长度恰为上限、
// Truncated=true），且截断是“按订阅”的：上限更大的订阅者拿到完整行。
func TestLongLineTruncated(t *testing.T) {
	const (
		lineBytes = 5000
		smallCap  = 64
	)
	report, release := helperDir(t)
	spec := linesSpec(t,
		helperLinesEnv+"=2",
		helperLineBytesEnv+"="+fmt.Sprint(lineBytes),
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
	)
	h := startHandleWith(t, testSupervisor(), spec)
	small := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{MaxLineBytes: smallCap})
	large := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{MaxLineBytes: 4 * lineBytes})
	releaseHelper(t, release)

	smallLines, err := collectLines(t, small, testWaitLong)
	if err != nil {
		t.Fatalf("small subscription ended with %v, want nil", err)
	}
	if len(smallLines) != 2 {
		t.Fatalf("small subscription got %d line(s), want 2", len(smallLines))
	}
	for i, l := range smallLines {
		if len(l.Data) != smallCap {
			t.Fatalf("line %d Data length = %d, want exactly MaxLineBytes %d", l.Seq, len(l.Data), smallCap)
		}
		if !l.Truncated {
			t.Fatalf("line %d Truncated = false, want true (data was cut at %d bytes)", l.Seq, smallCap)
		}
		if want := helperLineContent("line", i+1, lineBytes)[:smallCap]; string(l.Data) != want {
			t.Fatalf("line %d data = %q, want prefix %q", l.Seq, l.Data, want)
		}
	}

	largeLines, err := collectLines(t, large, testWaitLong)
	if err != nil {
		t.Fatalf("large subscription ended with %v, want nil", err)
	}
	if len(largeLines) != 2 {
		t.Fatalf("large subscription got %d line(s), want 2", len(largeLines))
	}
	for i, l := range largeLines {
		expectLine(t, l, int64(i+1), helperLineContent("line", i+1, lineBytes), false)
	}

	if exit := waitExit(t, h, testWaitShort); exit.Reason != ExitExited || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("Exit = %+v, want exited with code 0", exit)
	}
	// 订阅侧的截断不影响 spool：spool 保留完整行（截断只发生在投递路径）。
	data, _ := h.Output().Snapshot()
	if !bytes.Contains(data, []byte(helperLineContent("line", 1, lineBytes))) {
		t.Errorf("stdout spool lost the untruncated long line (len %d)", len(data))
	}
}

// TestCRLFLines：\r\n 行尾被识别为同一行，投递内容不含 \r 也不含 \n；spool 保留原始
// 字节（含 \r\n）。
func TestCRLFLines(t *testing.T) {
	report, release := helperDir(t)
	spec := linesSpec(t,
		helperLinesEnv+"=3",
		helperLineCRLFEnv+"=1",
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
	)
	h := startHandleWith(t, testSupervisor(), spec)
	sub := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{})
	releaseHelper(t, release)
	lines, err := collectLines(t, sub, testWaitLong)
	if err != nil {
		t.Fatalf("subscription ended with %v, want nil", err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d line(s), want 3", len(lines))
	}
	for i, l := range lines {
		if bytes.ContainsAny(l.Data, "\r\n") {
			t.Fatalf("line %d = %q still contains a line ending", l.Seq, l.Data)
		}
		expectLine(t, l, int64(i+1), helperLineContent("line", i+1, 0), false)
	}
	if exit := waitExit(t, h, testWaitShort); exit.Reason != ExitExited {
		t.Fatalf("Exit = %+v, want exited", exit)
	}
	data, _ := h.Output().Snapshot()
	if got, want := strings.Count(string(data), "\r\n"), 3; got != want {
		t.Errorf("stdout spool contains %d CRLF(s), want %d (spool keeps raw bytes): %q", got, want, data)
	}
}

// TestSubscribeBeforeFirstOutput：Start 返回后立刻订阅不得错过进程最早的输出。
//
// 两个子用例都确定性成立：
//   - immediate：helper 写完 8 行后用 HOLD 闸门保持存活，订阅紧跟 Start（此时进程
//     可能已经写了若干行，它们还没被任何订阅者认领）。
//   - after-output-visible：先等到 spool 里已经出现全部 8 行，再订阅——这是“输出
//     早于订阅”的极端情形，仍未认领的行必须原样交给第一个订阅者。
func TestSubscribeBeforeFirstOutput(t *testing.T) {
	const lineCount = 8

	run := func(t *testing.T, waitForOutputFirst bool) {
		report, _ := helperDir(t)
		hold := filepath.Join(filepath.Dir(report), "hold")
		// 不设 RELEASE：helper 立刻写行（这正是“输出可能早于订阅”的场景），写完用
		// HOLD 闸门保持存活，直到测试断言完再放行。
		spec := linesSpec(t,
			helperLinesEnv+"="+fmt.Sprint(lineCount),
			helperHoldEnv+"="+hold,
		)
		h := startHandleWith(t, testSupervisor(), spec)
		if waitForOutputFirst {
			waitForSpoolNewlines(t, h, lineCount, testWaitLong)
		}
		sub := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{})
		// 进程还被 HOLD 闸门挡着，读满 8 行即可证明没有丢首行；随后放行、等流收尾。
		lines := readLines(t, sub, lineCount, testWaitShort)
		for i, l := range lines {
			expectLine(t, l, int64(i+1), helperLineContent("line", i+1, 0), false)
		}
		if dropped := h.(*procHandle).unclaimedDropped(); dropped != 0 {
			t.Errorf("unclaimedDropped = %d, want 0 (output fits the unclaimed budget)", dropped)
		}
		writeFile(t, hold)
		rest, err := collectLines(t, sub, testWaitShort)
		if err != nil {
			t.Fatalf("subscription ended with %v, want nil", err)
		}
		if len(rest) != 0 {
			t.Fatalf("got %d extra line(s) after the %d expected ones", len(rest), lineCount)
		}
		if exit := waitExit(t, h, testWaitShort); exit.Reason != ExitExited || exit.Code == nil || *exit.Code != 0 {
			t.Fatalf("Exit = %+v, want exited with code 0", exit)
		}
	}

	t.Run("immediate", func(t *testing.T) { run(t, false) })
	t.Run("after-output-visible", func(t *testing.T) { run(t, true) })
}

// TestSubscribeAfterExit：进程在任何人订阅之前就写完输出并退出（快速失败的 CLI 正是
// 如此）——第一个订阅者仍拿到这些从未投递过的行，然后 channel 以 Err() 为 nil 关闭；
// 之后的订阅者拿到已关闭、零行的订阅，不回放历史（需要历史用 spool 快照）。
func TestSubscribeAfterExit(t *testing.T) {
	const lineCount = 4
	spec := linesSpec(t, helperLinesEnv+"="+fmt.Sprint(lineCount))
	h := startHandleWith(t, testSupervisor(), spec)
	if exit := waitExit(t, h, testWaitShort); exit.Reason != ExitExited || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("Exit = %+v, want exited with code 0", exit)
	}
	if data, _ := h.Output().Snapshot(); strings.Count(string(data), "\n") != lineCount {
		t.Fatalf("stdout spool = %q, want %d lines (the process did produce output)", data, lineCount)
	}

	first := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{})
	lines, err := collectLines(t, first, testWaitShort)
	if err != nil {
		t.Fatalf("first post-exit subscription Err = %v, want nil", err)
	}
	if len(lines) != lineCount {
		t.Fatalf("first post-exit subscription got %d line(s), want the %d never-delivered ones", len(lines), lineCount)
	}
	for i, l := range lines {
		expectLine(t, l, int64(i+1), helperLineContent("line", i+1, 0), false)
	}

	second := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{})
	again, err := collectLines(t, second, testWaitShort)
	if err != nil {
		t.Fatalf("second post-exit subscription Err = %v, want nil", err)
	}
	if len(again) != 0 {
		t.Fatalf("second post-exit subscription replayed %d line(s), want 0 (use the spool snapshot for history)", len(again))
	}
	// Close 幂等。
	second.Close()
	second.Close()
}

// TestUnclaimedOverflowFailsFirstSubscriber：订阅之前的输出超出未认领预算，最旧的行已
// 丢失——第一个订阅者必须以 ErrSubscriberOverflow 结束（DroppedSeq=1），而不是收到一段
// 缺了开头、看似完整的流。进程仍在运行（live）与已经退出（after-exit）两种时机结果相同。
func TestUnclaimedOverflowFailsFirstSubscriber(t *testing.T) {
	const lineCount = maxUnclaimedLines + 76

	run := func(t *testing.T, waitForExit bool) {
		report, _ := helperDir(t)
		hold := filepath.Join(filepath.Dir(report), "hold")
		env := []string{helperLinesEnv + "=" + fmt.Sprint(lineCount)}
		if !waitForExit {
			env = append(env, helperHoldEnv+"="+hold)
		}
		h := startHandleWith(t, testSupervisor(), linesSpec(t, env...))
		if waitForExit {
			if exit := waitExit(t, h, testWaitLong); exit.Reason != ExitExited || exit.Code == nil || *exit.Code != 0 {
				t.Fatalf("Exit = %+v, want exited with code 0", exit)
			}
		} else {
			waitForSpoolNewlines(t, h, lineCount, testWaitLong)
		}

		first := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{})
		lines, err := collectLines(t, first, testWaitShort)
		if !errors.Is(err, ErrSubscriberOverflow) {
			t.Fatalf("first subscription Err = %v, want ErrSubscriberOverflow (unclaimed lines were lost)", err)
		}
		if len(lines) != 0 {
			t.Fatalf("first subscription delivered %d line(s) of a stream missing its head", len(lines))
		}
		if got := first.DroppedSeq(); got != 1 {
			t.Fatalf("DroppedSeq = %d, want 1 (the oldest unclaimed line went first)", got)
		}
		if dropped := h.(*procHandle).unclaimedDropped(); dropped != lineCount-maxUnclaimedLines {
			t.Fatalf("unclaimedDropped = %d, want %d", dropped, lineCount-maxUnclaimedLines)
		}
		if !waitForExit {
			writeFile(t, hold)
			if exit := waitExit(t, h, testWaitShort); exit.Reason != ExitExited || exit.Code == nil || *exit.Code != 0 {
				t.Fatalf("Exit = %+v, want exited with code 0", exit)
			}
		}
	}

	t.Run("live", func(t *testing.T) { run(t, false) })
	t.Run("after-exit", func(t *testing.T) { run(t, true) })
}

// TestSpoolFilterAppliesOnlyToSpool：过滤器只改写入 spool 的字节，订阅者始终拿到原始行；
// 过滤器 panic 被恢复并计数（spool 只留占位说明、不留原始字节），排空与子进程都不受影响。
func TestSpoolFilterAppliesOnlyToSpool(t *testing.T) {
	const canary = "canary-spool-secret-4b71"
	linePrefix := canary + " line"

	t.Run("redaction", func(t *testing.T) {
		report, release := helperDir(t)
		spec := linesSpec(t,
			helperLinesEnv+"=3",
			helperLinePrefixEnv+"="+canary+" ",
			helperReportEnv+"="+report,
			helperReleaseEnv+"="+release,
		)
		sup := NewSupervisor(SupervisorOptions{
			OwnerInstance: testOwnership().OwnerInstance,
			SpoolFilter: func(stream Stream, line []byte) []byte {
				if bytes.Contains(line, []byte(canary)) {
					return []byte("[redacted by filter]\n")
				}
				return line
			},
		})
		h := startHandleWith(t, sup, spec)
		sub := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{})
		releaseHelper(t, release)
		lines, err := collectLines(t, sub, testWaitLong)
		if err != nil {
			t.Fatalf("subscription ended with %v, want nil", err)
		}
		if len(lines) != 3 {
			t.Fatalf("got %d line(s), want 3", len(lines))
		}
		for i, l := range lines {
			// 订阅者拿到的是原始协议字节：过滤只面向日志。
			expectLine(t, l, int64(i+1), helperLineContent(linePrefix, i+1, 0), false)
			if !strings.Contains(string(l.Data), canary) {
				t.Fatalf("line %d = %q, want the raw canary line (the filter must not touch subscribers)", l.Seq, l.Data)
			}
		}
		if exit := waitExit(t, h, testWaitShort); exit.Reason != ExitExited {
			t.Fatalf("Exit = %+v, want exited", exit)
		}
		data, _ := h.Output().Snapshot()
		if bytes.Contains(data, []byte(canary)) {
			t.Fatalf("stdout spool still contains the canary: %q", data)
		}
		if got, want := strings.Count(string(data), "[redacted by filter]\n"), 3; got != want {
			t.Fatalf("stdout spool has %d redacted line(s), want %d: %q", got, want, data)
		}
		if stderrData, _ := h.Stderr().Snapshot(); bytes.Contains(stderrData, []byte(canary)) {
			t.Fatalf("stderr spool contains the canary: %q", stderrData)
		}
	})

	t.Run("panic", func(t *testing.T) {
		report, release := helperDir(t)
		spec := linesSpec(t,
			helperLinesEnv+"=3",
			helperLinePrefixEnv+"="+canary+" ",
			helperReportEnv+"="+report,
			helperReleaseEnv+"="+release,
		)
		sup := NewSupervisor(SupervisorOptions{
			OwnerInstance: testOwnership().OwnerInstance,
			SpoolFilter: func(stream Stream, line []byte) []byte {
				panic("filter is broken")
			},
		})
		h := startHandleWith(t, sup, spec)
		sub := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{})
		releaseHelper(t, release)
		lines, err := collectLines(t, sub, testWaitLong)
		if err != nil {
			t.Fatalf("subscription ended with %v, want nil (a panicking filter must not break the drain)", err)
		}
		if len(lines) != 3 {
			t.Fatalf("got %d line(s), want 3", len(lines))
		}
		exit := waitExit(t, h, testWaitShort)
		if exit.Reason != ExitExited || exit.Code == nil || *exit.Code != 0 {
			t.Fatalf("Exit = %+v, want exited with code 0 (a panicking filter must not stall the child)", exit)
		}
		// 每个完整行是一个写入单元：3 行 → 恰好 3 次 panic 被恢复。
		if panics := h.(*procHandle).spoolFilterPanics(); panics != 3 {
			t.Errorf("spoolFilterPanics = %d, want 3 (every panic must be recovered and counted)", panics)
		}
		// 失败即关闭：坏掉的（脱敏）过滤器不能让原始字节落进 spool，只留占位说明。
		data, _ := h.Output().Snapshot()
		if bytes.Contains(data, []byte(canary)) {
			t.Fatalf("stdout spool = %q: a panicking filter leaked the raw canary line", data)
		}
		for i := 1; i <= 3; i++ {
			unit := helperLineContent(linePrefix, i, 0) + "\n"
			if want := string(filterFailedPlaceholder(len(unit))); !strings.Contains(string(data), want) {
				t.Fatalf("stdout spool = %q, want the placeholder %q for line %d", data, want, i)
			}
		}
		if got := strings.Count(string(data), "[spool filter failed: "); got != 3 {
			t.Fatalf("stdout spool has %d placeholder(s), want 3: %q", got, data)
		}
	})
}

// TestSubscribeRejectsInvalidInput：非法流与非法选项在订阅前被拒绝，且不创建订阅。
func TestSubscribeRejectsInvalidInput(t *testing.T) {
	spec := linesSpec(t, helperLinesEnv+"=1")
	h := startHandleWith(t, testSupervisor(), spec)
	if _, err := h.Subscribe(Stream("pty"), SubscribeOptions{}); !errors.Is(err, ErrInvalidStream) {
		t.Errorf("Subscribe(pty) error = %v, want ErrInvalidStream", err)
	}
	if _, err := h.Subscribe(StreamStdout, SubscribeOptions{BufferLines: -1}); !errors.Is(err, ErrInvalidSubscribeOptions) {
		t.Errorf("Subscribe(BufferLines=-1) error = %v, want ErrInvalidSubscribeOptions", err)
	}
	if _, err := h.Subscribe(StreamStderr, SubscribeOptions{MaxLineBytes: -1}); !errors.Is(err, ErrInvalidSubscribeOptions) {
		t.Errorf("Subscribe(MaxLineBytes=-1) error = %v, want ErrInvalidSubscribeOptions", err)
	}
	if exit := waitExit(t, h, testWaitShort); exit.Reason != ExitExited {
		t.Fatalf("Exit = %+v, want exited", exit)
	}
}

// TestSubscriptionCloseStopsDelivery：Close 关闭 channel、Err() 为 ErrSubscriptionClosed、
// 幂等；进程与 spool 不受影响。
func TestSubscriptionCloseStopsDelivery(t *testing.T) {
	report, release := helperDir(t)
	spec := linesSpec(t,
		helperLinesEnv+"=5",
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
	)
	h := startHandleWith(t, testSupervisor(), spec)
	sub := subscribeOrFail(t, h, StreamStdout, SubscribeOptions{})
	releaseHelper(t, release)

	// 先读一行，确认订阅是活的。
	select {
	case l, ok := <-sub.C():
		if !ok {
			t.Fatalf("subscription closed before delivering any line (Err = %v)", sub.Err())
		}
		expectLine(t, l, 1, helperLineContent("line", 1, 0), false)
	case <-time.After(testWaitLong):
		t.Fatalf("no line arrived within %s", testWaitLong)
	}
	sub.Close()
	sub.Close() // 幂等
	if err := sub.Err(); !errors.Is(err, ErrSubscriptionClosed) {
		t.Fatalf("Err after Close = %v, want ErrSubscriptionClosed", err)
	}
	// channel 已关闭：读空即结束（缓冲区里可能还有 Close 之前投递的行）。
	deadline := time.After(testWaitShort)
	for {
		select {
		case _, ok := <-sub.C():
			if !ok {
				goto closed
			}
		case <-deadline:
			t.Fatalf("subscription channel was not closed by Close()")
		}
	}
closed:
	if exit := waitExit(t, h, testWaitShort); exit.Reason != ExitExited || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("Exit = %+v, want exited with code 0", exit)
	}
	data, _ := h.Output().Snapshot()
	if got := strings.Count(string(data), "\n"); got != 5 {
		t.Errorf("stdout spool has %d line(s), want 5 (closing a subscription must not affect the spool)", got)
	}
}

// TestConcurrentSubscribeAndCloseDuringOutput：订阅、关闭与排空投递并发进行时不死锁、
// 不向已关闭 channel 发送（-race 下也是干净的）、被关掉的订阅不影响其余订阅者与
// 已结束订阅的 Err 语义。
func TestConcurrentSubscribeAndCloseDuringOutput(t *testing.T) {
	const stdoutLines = 2000
	report, release := helperDir(t)
	hold := filepath.Join(filepath.Dir(report), "hold")
	spec := linesSpec(t,
		helperLinesEnv+"="+fmt.Sprint(stdoutLines),
		helperStderrEnv+"=500",
		helperReportEnv+"="+report,
		helperReleaseEnv+"="+release,
		helperHoldEnv+"="+hold,
	)
	h := startHandleWith(t, testSupervisor(), spec)

	// 4 个订阅并发注册（2 个 stdout、2 个 stderr），全部在放行之前完成。
	var wg sync.WaitGroup
	subs := make([]*Subscription, 4)
	for i := range subs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			stream := StreamStdout
			if i%2 == 1 {
				stream = StreamStderr
			}
			sub, err := h.Subscribe(stream, SubscribeOptions{BufferLines: 2048})
			if err != nil {
				t.Errorf("Subscribe #%d: %v", i, err)
				return
			}
			subs[i] = sub
		}(i)
	}
	wg.Wait()
	t.Cleanup(func() {
		for _, sub := range subs {
			if sub != nil {
				sub.Close()
			}
		}
	})

	releaseHelper(t, release)
	// 让输出先跑起来，再并发关闭两个订阅：关闭必须与投递互斥（不能 panic）。
	waitForSpoolNewlines(t, h, 32, testWaitLong)
	var closer sync.WaitGroup
	for _, i := range []int{1, 3} {
		closer.Add(1)
		go func(i int) {
			defer closer.Done()
			subs[i].Close()
		}(i)
	}
	// 同时有两个 goroutine 在读还没关的订阅（读与关闭并发）。此时进程还被 HOLD
	// 挡着：流不会结束，所以“读满 N 行”是确定的判据，不需要等 channel 关闭。
	for _, i := range []int{0, 2} {
		closer.Add(1)
		go func(i int) {
			defer closer.Done()
			lines := readLines(t, subs[i], stdoutLines, testWaitLong)
			for n, l := range lines {
				if l.Seq != int64(n+1) {
					t.Errorf("subscription #%d line #%d Seq = %d, want %d", i, n, l.Seq, n+1)
					return
				}
			}
			if err := subs[i].Err(); err != nil {
				t.Errorf("subscription #%d Err = %v, want nil (it is still running)", i, err)
			}
		}(i)
	}
	closer.Wait()

	// 被关闭的订阅：以调用方关闭结束（ErrSubscriptionClosed），不能是 nil 或被
	// 后来的“流结束”改成别的值。
	for _, i := range []int{1, 3} {
		if err := subs[i].Err(); !errors.Is(err, ErrSubscriptionClosed) {
			t.Errorf("closed subscription #%d Err = %v, want ErrSubscriptionClosed", i, err)
		}
	}
	// 放行 helper：两路流收尾时不会再去碰已关闭的订阅。
	writeFile(t, hold)
	if exit := waitExit(t, h, testWaitShort); exit.Reason != ExitExited || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("Exit = %+v, want exited with code 0", exit)
	}
	for _, i := range []int{1, 3} {
		if err := subs[i].Err(); !errors.Is(err, ErrSubscriptionClosed) {
			t.Errorf("after stream end, closed subscription #%d Err = %v, want ErrSubscriptionClosed (terminal cause is sticky)", i, err)
		}
	}
	data, _ := h.Output().Snapshot()
	if got, want := strings.Count(string(data), "\n"), stdoutLines; got != want {
		t.Errorf("stdout spool has %d line(s), want %d (closing subscriptions must not affect the spool)", got, want)
	}
}

// waitForSpoolNewlines 轮询 stdout spool 直到出现 want 个换行符（证明进程已经写了输出）。
func waitForSpoolNewlines(t *testing.T, h Handle, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		data, _ := h.Output().Snapshot()
		if strings.Count(string(data), "\n") >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("stdout spool has %d newline(s) after %s, want %d: %q",
				strings.Count(string(data), "\n"), timeout, want, data)
		}
		time.Sleep(helperReleasePoll)
	}
}

// writeFile 创建（触碰）一个闸门文件。
func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("go\n"), 0o644); err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
}
