package fake

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"runtime"
	"strconv"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/codeflow/backend/internal/execbackend"
)

// 本文件覆盖"重复/并发/恢复"断言：重复帧、乱码帧、洪峰不丢帧、背压下可取消。

func TestDuplicateFrameDelivered(t *testing.T) {
	base := runtime.NumGoroutine()
	clock := NewManualClock(testStart)
	original := map[string]any{"stream": "stdout", "text": "one frame"}
	script := []Step{
		Emit(execbackend.ObservationOutput, "msg-dup-1", original),
		Duplicate(),
		Duplicate(),
		Exit(0, execbackend.ExitReasonCompleted, UnknownUsage()),
	}
	be := New(Options{Clock: clock, Script: script})
	sess := startSession(t, be, validRequest(t))
	defer mustClose(t, sess, base)

	all := collect(t, sess, obsTimeout)
	assertAllValid(t, all)
	assertSameSequence(t,
		[]string{"process_started:", "output:msg-dup-1", "output:msg-dup-1", "output:msg-dup-1", "exited:"},
		refsOf(all))

	first := all[1]
	for _, dup := range all[2:4] {
		if dup.ProviderRef != first.ProviderRef {
			t.Fatalf("duplicate provider_ref = %q, want %q", dup.ProviderRef, first.ProviderRef)
		}
		if !bytes.Equal(dup.Payload, first.Payload) {
			t.Fatalf("duplicate payload = %s, want %s", dup.Payload, first.Payload)
		}
	}
}

func TestDuplicateWithoutPreviousIsScriptError(t *testing.T) {
	base := runtime.NumGoroutine()
	be := New(Options{Clock: NewManualClock(testStart), Script: []Step{Duplicate()}})
	sess := startSession(t, be, validRequest(t))
	defer mustClose(t, sess, base)

	all := collect(t, sess, obsTimeout)
	assertSameSequence(t, []string{"process_started:", "protocol_warning:", "exited:"}, refsOf(all))
	assertAllValid(t, all)
	if result := exitResultOf(t, all[len(all)-1]); result.Reason != execbackend.ExitReasonProtocolError {
		t.Fatalf("exit reason = %s, want protocol_error", result.Reason)
	}
	if _, err := sess.Wait(context.Background()); err == nil {
		t.Fatalf("Wait must surface the script error")
	}
}

func TestGarbledFrameIsProtocolWarning(t *testing.T) {
	base := runtime.NumGoroutine()
	be := New(Options{
		Clock: NewManualClock(testStart),
		Script: []Step{
			Emit(execbackend.ObservationOutput, "msg-1", map[string]any{"stream": "stdout", "text": "before"}),
			Garbled(),
			Exit(0, execbackend.ExitReasonCompleted, UnknownUsage()),
		},
	})
	sess := startSession(t, be, validRequest(t))
	defer mustClose(t, sess, base)

	all := collect(t, sess, obsTimeout)
	assertAllValid(t, all)
	assertSameSequence(t,
		[]string{"process_started:", "output:msg-1", "protocol_warning:garbled-1", "exited:"},
		refsOf(all))

	warning := all[2]
	if len(warning.Payload) > execbackend.MaxControlFrameBytes {
		t.Fatalf("garbled payload is %d bytes, over control frame limit", len(warning.Payload))
	}
	var payload struct {
		Stage       string `json:"stage"`
		Reason      string `json:"reason"`
		RawBytes    int    `json:"raw_bytes"`
		RawEncoding string `json:"raw_encoding"`
		Raw         string `json:"raw"`
	}
	if err := json.Unmarshal(warning.Payload, &payload); err != nil {
		t.Fatalf("garbled payload: %v (payload=%s)", err, warning.Payload)
	}
	if payload.Stage == "" || payload.Reason == "" {
		t.Fatalf("garbled payload lacks stage/reason: %s", warning.Payload)
	}
	if payload.RawEncoding != "base64" {
		t.Fatalf("garbled raw_encoding = %q, want base64", payload.RawEncoding)
	}
	raw, err := base64.StdEncoding.DecodeString(payload.Raw)
	if err != nil {
		t.Fatalf("garbled raw is not base64: %v", err)
	}
	if len(raw) != payload.RawBytes {
		t.Fatalf("garbled raw_bytes = %d, decoded %d", payload.RawBytes, len(raw))
	}
	if utf8.Valid(raw) {
		t.Fatalf("garbled raw should not be valid UTF-8: %q", raw)
	}
}

func TestFloodDeliversAllInOrder(t *testing.T) {
	base := runtime.NumGoroutine()
	const (
		count = 5000
		size  = 1024
	)
	be := New(Options{Clock: NewManualClock(testStart), Script: []Step{
		Flood(count, size),
		Exit(0, execbackend.ExitReasonCompleted, UnknownUsage()),
	}})
	sess := startSession(t, be, validRequest(t))
	defer mustClose(t, sess, base)

	// 洪峰必须逐条、按序到达，且 payload 不超过声明的大小与协议上限。
	var frames int
	var lastSeq = -1
	for {
		o, ok := recv(t, sess, obsTimeout)
		if !ok {
			break
		}
		if o.Kind == execbackend.ObservationExited {
			break
		}
		if frames == 0 {
			if o.Kind != execbackend.ObservationProcessStarted {
				t.Fatalf("first observation = %s, want process_started", o.Kind)
			}
			frames++
			continue
		}
		if o.Kind != execbackend.ObservationOutput {
			t.Fatalf("frame %d kind = %s, want output", frames, o.Kind)
		}
		if len(o.Payload) != size {
			t.Fatalf("frame %d payload = %d bytes, want %d", frames, len(o.Payload), size)
		}
		index := frames - 1
		if want := "out-" + padIndex(index); o.ProviderRef != want {
			t.Fatalf("frame %d provider_ref = %q, want %q", frames, o.ProviderRef, want)
		}
		var payload struct {
			Stream string `json:"stream"`
			Seq    int    `json:"seq"`
		}
		if err := json.Unmarshal(o.Payload, &payload); err != nil {
			t.Fatalf("frame %d payload: %v", frames, err)
		}
		if payload.Seq != index || payload.Seq != lastSeq+1 {
			t.Fatalf("frame %d seq = %d, want %d", frames, payload.Seq, index)
		}
		lastSeq = payload.Seq
		frames++
	}
	if frames != count+1 {
		t.Fatalf("delivered %d frames (process_started + outputs), want %d", frames, count+1)
	}
}

// padIndex 复现 fake 的 provider_ref 序号格式（out-%08d）。
func padIndex(index int) string {
	s := strconv.Itoa(index)
	for len(s) < 8 {
		s = "0" + s
	}
	return s
}

func TestBackpressureDoesNotBlockCancel(t *testing.T) {
	base := runtime.NumGoroutine()
	be := New(Options{
		Clock:             NewManualClock(testStart),
		Script:            []Step{Flood(5000, 256)},
		ObservationBuffer: 4,
	})
	sess := startSession(t, be, validRequest(t))
	concrete := asSession(t, sess)
	defer mustClose(t, sess, base)

	// 读掉 process_started，之后不再读：脚本很快会卡在发送上。
	if o, ok := recv(t, sess, obsTimeout); !ok || o.Kind != execbackend.ObservationProcessStarted {
		t.Fatalf("first observation = %s/%v, want process_started", o.Kind, ok)
	}
	deadline := time.Now().Add(2 * time.Second)
	for concrete.pendingFrames() < cap(concrete.obs) {
		if time.Now().After(deadline) {
			t.Fatalf("script never filled the observation buffer (pending=%d, cap=%d)",
				concrete.pendingFrames(), cap(concrete.obs))
		}
		time.Sleep(time.Millisecond)
	}

	start := time.Now()
	if err := sess.Cancel(context.Background(), execbackend.CancelForce); err != nil {
		t.Fatalf("Cancel(force): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := sess.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait after Cancel under backpressure: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("cancel took %s under backpressure, want < 1s", elapsed)
	}
	if result.Reason != execbackend.ExitReasonCancelled {
		t.Fatalf("exit reason = %s, want cancelled", result.Reason)
	}
	if !concrete.isDone() {
		t.Fatalf("session should be done after Cancel")
	}
}

func TestFloodBackpressureResumesWhenRead(t *testing.T) {
	base := runtime.NumGoroutine()
	const count = 500
	be := New(Options{
		Clock:             NewManualClock(testStart),
		Script:            []Step{Flood(count, 64)},
		ObservationBuffer: 2,
	})
	sess := startSession(t, be, validRequest(t))
	concrete := asSession(t, sess)
	defer mustClose(t, sess, base)

	// 先让脚本撞上背压（不读），确认它停在发送上；再开始读，帧必须一条不少。
	deadline := time.Now().Add(2 * time.Second)
	for concrete.pendingFrames() < cap(concrete.obs) {
		if time.Now().After(deadline) {
			t.Fatalf("script never hit backpressure (pending=%d)", concrete.pendingFrames())
		}
		time.Sleep(time.Millisecond)
	}
	if concrete.isDone() {
		t.Fatalf("session finished while the consumer was not reading")
	}

	all := collect(t, sess, obsTimeout)
	if len(all) != count+2 {
		t.Fatalf("delivered %d observations, want %d (process_started + flood + exited)", len(all), count+2)
	}
	assertAllValid(t, all)
	if all[len(all)-1].Kind != execbackend.ObservationExited {
		t.Fatalf("last observation = %s, want exited", all[len(all)-1].Kind)
	}
	if result := exitResultOf(t, all[len(all)-1]); result.Reason != execbackend.ExitReasonCompleted {
		t.Fatalf("exit reason = %s, want completed", result.Reason)
	}
}
