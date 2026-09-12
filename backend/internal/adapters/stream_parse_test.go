package adapters

import (
	"encoding/json"
	"strings"
	"testing"
)

// 本文件是 T13.04.a 的 parser/terminal 单测（§28 T13.04.a）。
// 边界说明：终结状态映射（TerminalStatus/TerminalError）是冻结契约的纯函数，
// 在此断言；终结帧的实际发送、历史追加与取消接线属 T13.04.b，不在此测试。

func chunkDeltas(chunks []StreamChunk) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, c.Delta)
	}
	return out
}

func assertContentChunks(t *testing.T, chunks []StreamChunk, want []string) {
	t.Helper()
	if len(chunks) != len(want) {
		t.Fatalf("expected %d content chunks, got %d: %v", len(want), len(chunks), chunkDeltas(chunks))
	}
	for i, c := range chunks {
		if c.Delta != want[i] {
			t.Fatalf("chunk %d delta = %q, want %q", i, c.Delta, want[i])
		}
		if c.Index != i {
			t.Fatalf("chunk %d index = %d, want %d", i, c.Index, i)
		}
		if c.Done {
			t.Fatalf("chunk %d must be a content frame (Done=false)", i)
		}
		if c.TerminalStatus != "" || c.Error != nil || c.Usage != nil {
			t.Fatalf("chunk %d must not carry terminal fields", i)
		}
	}
}

func assertTerminal(t *testing.T, r *StreamParseResult, wantStatus TerminalStatus, wantErrCode string) {
	t.Helper()
	if got := r.TerminalStatus(); got != wantStatus {
		t.Fatalf("TerminalStatus = %q, want %q (result: %+v, scanErr=%v)", got, wantStatus, r, r.ScanErr)
	}
	err := r.TerminalError()
	if wantStatus == TerminalCompleted {
		if err != nil {
			t.Fatalf("completed stream must not carry a terminal error, got %+v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("status %q must carry a structured terminal error", wantStatus)
	}
	if wantErrCode != "" && err.Code != wantErrCode {
		t.Fatalf("terminal error code = %q, want %q", err.Code, wantErrCode)
	}
}

// --- OpenAI fixtures ---

const openAINormalStream = "data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n" +
	"data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n" +
	"data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n" +
	"data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
	"data: {\"id\":\"chatcmpl-1\",\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":2,\"total_tokens\":11}}\n\n" +
	"data: [DONE]\n\n"

func TestParseOpenAIStreamNormalCompletion(t *testing.T) {
	r := parseOpenAIStreamBody(strings.NewReader(openAINormalStream))
	assertContentChunks(t, r.Chunks, []string{"Hello", " world"})
	if !r.Completed {
		t.Fatal("expected completion marker from [DONE]")
	}
	if r.FinishReason != "stop" {
		t.Fatalf("FinishReason = %q, want provider-reported %q", r.FinishReason, "stop")
	}
	if r.Usage == nil || r.Usage.PromptTokens != 9 || r.Usage.CompletionTokens != 2 || r.Usage.TotalTokens != 11 {
		t.Fatalf("Usage = %+v, want 9/2/11", r.Usage)
	}
	assertTerminal(t, r, TerminalCompleted, "")
}

func TestParseOpenAIStreamProviderError(t *testing.T) {
	stream := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"error\":{\"message\":\"The server had an error\",\"type\":\"server_error\",\"code\":null}}\n\n"
	r := parseOpenAIStreamBody(strings.NewReader(stream))
	assertContentChunks(t, r.Chunks, []string{"partial"})
	if r.ProviderErr == nil {
		t.Fatal("expected provider error frame to be surfaced")
	}
	if r.ProviderErr.Code != "server_error" || r.ProviderErr.Message != "The server had an error" {
		t.Fatalf("ProviderErr = %+v", r.ProviderErr)
	}
	if !r.ProviderErr.Retryable {
		t.Fatal("server_error must be classified retryable")
	}
	if r.Completed {
		t.Fatal("provider error stream must not be marked completed")
	}
	assertTerminal(t, r, TerminalError, "server_error")
}

func TestParseOpenAIStreamUnexpectedEOF(t *testing.T) {
	stream := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"cut off\"},\"finish_reason\":null}]}\n\n"
	r := parseOpenAIStreamBody(strings.NewReader(stream))
	assertContentChunks(t, r.Chunks, []string{"cut off"})
	if r.Completed {
		t.Fatal("clean EOF without [DONE] must not be marked completed")
	}
	if r.ScanErr != nil {
		t.Fatalf("clean EOF is not a scan error, got %v", r.ScanErr)
	}
	// 负向断言：EOF 不产生"成功终结"判定
	assertTerminal(t, r, TerminalError, "stream_unexpected_eof")
}

func TestParseOpenAIStreamOversizedFrame(t *testing.T) {
	big := strings.Repeat("x", 128*1024)
	stream := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"before\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"" + big + "\"},\"finish_reason\":null}]}\n\n" +
		"data: [DONE]\n\n"
	r := parseOpenAIStreamBody(strings.NewReader(stream))
	if r.ScanErr == nil {
		t.Fatal("expected scanner error for frame beyond the default 64KB token limit")
	}
	assertContentChunks(t, r.Chunks, []string{"before"})
	assertTerminal(t, r, TerminalError, "stream_scan_failed")
}

func TestParseOpenAIStreamTruncatedFinalFrame(t *testing.T) {
	stream := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"half"
	r := parseOpenAIStreamBody(strings.NewReader(stream))
	if r.MalformedFrames != 1 {
		t.Fatalf("MalformedFrames = %d, want 1", r.MalformedFrames)
	}
	assertTerminal(t, r, TerminalError, "stream_frame_malformed")
}

// --- Claude fixtures ---

const claudeNormalStream = "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":25,\"output_tokens\":1}}}\n\n" +
	"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
	"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
	"data: {\"type\":\"ping\"}\n\n" +
	"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" Claude\"}}\n\n" +
	"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
	"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":12}}\n\n" +
	"data: {\"type\":\"message_stop\"}\n\n"

func TestParseClaudeStreamNormalCompletion(t *testing.T) {
	r := parseClaudeStreamBody(strings.NewReader(claudeNormalStream))
	assertContentChunks(t, r.Chunks, []string{"Hello", " Claude"})
	if !r.Completed {
		t.Fatal("expected completion marker from message_stop")
	}
	if r.FinishReason != "end_turn" {
		t.Fatalf("FinishReason = %q, want provider-reported %q (must not be rewritten to stop)", r.FinishReason, "end_turn")
	}
	if r.Usage == nil || r.Usage.PromptTokens != 25 || r.Usage.CompletionTokens != 12 || r.Usage.TotalTokens != 37 {
		t.Fatalf("Usage = %+v, want 25/12/37", r.Usage)
	}
	assertTerminal(t, r, TerminalCompleted, "")
}

func TestParseClaudeStreamDoneMarkerCompletion(t *testing.T) {
	// 现行实现与既有 fixture 接受 [DONE] 作为 Claude 流的完成标记，保持兼容。
	stream := "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n" +
		"data: [DONE]\n\n"
	r := parseClaudeStreamBody(strings.NewReader(stream))
	assertContentChunks(t, r.Chunks, []string{"hello"})
	if !r.Completed {
		t.Fatal("expected completion marker from [DONE]")
	}
	assertTerminal(t, r, TerminalCompleted, "")
}

func TestParseClaudeStreamProviderError(t *testing.T) {
	stream := "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n" +
		"data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"
	r := parseClaudeStreamBody(strings.NewReader(stream))
	assertContentChunks(t, r.Chunks, []string{"partial"})
	if r.ProviderErr == nil {
		t.Fatal("expected provider error event to be surfaced")
	}
	if r.ProviderErr.Code != "overloaded_error" || r.ProviderErr.Message != "Overloaded" {
		t.Fatalf("ProviderErr = %+v", r.ProviderErr)
	}
	if !r.ProviderErr.Retryable {
		t.Fatal("overloaded_error must be classified retryable")
	}
	assertTerminal(t, r, TerminalError, "overloaded_error")
}

func TestParseClaudeStreamUnexpectedEOF(t *testing.T) {
	stream := "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"cut\"}}\n\n"
	r := parseClaudeStreamBody(strings.NewReader(stream))
	assertContentChunks(t, r.Chunks, []string{"cut"})
	if r.Completed {
		t.Fatal("EOF without message_stop must not be marked completed")
	}
	assertTerminal(t, r, TerminalError, "stream_unexpected_eof")
}

func TestParseClaudeStreamOversizedFrame(t *testing.T) {
	big := strings.Repeat("好", 40*1024) // 3 字节/字，超 64KB 单帧上限
	stream := "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"before\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"" + big + "\"}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	r := parseClaudeStreamBody(strings.NewReader(stream))
	if r.ScanErr == nil {
		t.Fatal("expected scanner error for frame beyond the default 64KB token limit")
	}
	assertContentChunks(t, r.Chunks, []string{"before"})
	assertTerminal(t, r, TerminalError, "stream_scan_failed")
}

// --- Gemini fixtures ---

const geminiNormalStream = "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Hello\"}]}}]}\n\n" +
	"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\" Gemini\"}]}}]}\n\n" +
	"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"!\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":3,\"totalTokenCount\":10}}\n\n"

func TestParseGeminiStreamNormalCompletion(t *testing.T) {
	r := parseGeminiStreamBody(strings.NewReader(geminiNormalStream))
	assertContentChunks(t, r.Chunks, []string{"Hello", " Gemini", "!"})
	if !r.Completed {
		t.Fatal("expected completion signal from finishReason")
	}
	if r.FinishReason != "STOP" {
		t.Fatalf("FinishReason = %q, want provider-reported %q", r.FinishReason, "STOP")
	}
	if r.Usage == nil || r.Usage.PromptTokens != 7 || r.Usage.CompletionTokens != 3 || r.Usage.TotalTokens != 10 {
		t.Fatalf("Usage = %+v, want 7/3/10", r.Usage)
	}
	assertTerminal(t, r, TerminalCompleted, "")
}

func TestParseGeminiStreamMaxTokensKeepsProviderReason(t *testing.T) {
	stream := "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"long\"}]},\"finishReason\":\"MAX_TOKENS\"}]}\n\n"
	r := parseGeminiStreamBody(strings.NewReader(stream))
	assertContentChunks(t, r.Chunks, []string{"long"})
	if r.FinishReason != "MAX_TOKENS" {
		t.Fatalf("FinishReason = %q, want unmodified provider value %q", r.FinishReason, "MAX_TOKENS")
	}
	if !r.Completed {
		t.Fatal("finishReason is the Gemini protocol completion signal")
	}
	assertTerminal(t, r, TerminalCompleted, "")
}

func TestParseGeminiStreamProviderError(t *testing.T) {
	stream := "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"partial\"}]}}]}\n\n" +
		"data: {\"error\":{\"code\":429,\"message\":\"Quota exceeded\",\"status\":\"RESOURCE_EXHAUSTED\"}}\n\n"
	r := parseGeminiStreamBody(strings.NewReader(stream))
	assertContentChunks(t, r.Chunks, []string{"partial"})
	if r.ProviderErr == nil {
		t.Fatal("expected provider error frame to be surfaced")
	}
	if r.ProviderErr.Code != "RESOURCE_EXHAUSTED" || r.ProviderErr.Message != "Quota exceeded" {
		t.Fatalf("ProviderErr = %+v", r.ProviderErr)
	}
	if !r.ProviderErr.Retryable {
		t.Fatal("RESOURCE_EXHAUSTED must be classified retryable")
	}
	assertTerminal(t, r, TerminalError, "RESOURCE_EXHAUSTED")
}

func TestParseGeminiStreamUnexpectedEOF(t *testing.T) {
	stream := "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"cut\"}]}}]}\n\n"
	r := parseGeminiStreamBody(strings.NewReader(stream))
	assertContentChunks(t, r.Chunks, []string{"cut"})
	if r.Completed {
		t.Fatal("EOF without finishReason must not be marked completed")
	}
	assertTerminal(t, r, TerminalError, "stream_unexpected_eof")
}

func TestParseGeminiStreamDoneMarkerIgnored(t *testing.T) {
	// Gemini 协议无 [DONE] 标记，现行实现忽略之；[DONE] 不构成完成信号。
	stream := "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hi\"}]}}]}\n\n" +
		"data: [DONE]\n\n"
	r := parseGeminiStreamBody(strings.NewReader(stream))
	assertContentChunks(t, r.Chunks, []string{"hi"})
	if r.Completed {
		t.Fatal("[DONE] must not mark a Gemini stream completed")
	}
	assertTerminal(t, r, TerminalError, "stream_unexpected_eof")
}

func TestParseGeminiStreamOversizedFrame(t *testing.T) {
	big := strings.Repeat("y", 128*1024)
	stream := "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"before\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"" + big + "\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[]},\"finishReason\":\"STOP\"}]}\n\n"
	r := parseGeminiStreamBody(strings.NewReader(stream))
	if r.ScanErr == nil {
		t.Fatal("expected scanner error for frame beyond the default 64KB token limit")
	}
	assertContentChunks(t, r.Chunks, []string{"before"})
	assertTerminal(t, r, TerminalError, "stream_scan_failed")
}

// --- 终结契约 JSON 形状 ---

func TestStreamChunkContentFrameOmitsTerminalFields(t *testing.T) {
	payload, err := json.Marshal(StreamChunk{Delta: "a", Index: 2, Done: false})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"terminal_status", "error", "finish_reason", "usage"} {
		if strings.Contains(string(payload), key) {
			t.Fatalf("content frame must omit %q, got %s", key, payload)
		}
	}
}

func TestStreamChunkTerminalFrameCarriesStatus(t *testing.T) {
	payload, err := json.Marshal(StreamChunk{
		Done:           true,
		TerminalStatus: TerminalCompleted,
		FinishReason:   "stop",
		Usage:          &Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(payload), `"terminal_status":"completed"`) {
		t.Fatalf("terminal frame must carry terminal_status, got %s", payload)
	}

	errPayload, err := json.Marshal(StreamChunk{
		Done:           true,
		TerminalStatus: TerminalError,
		Error:          &StreamError{Code: "stream_unexpected_eof", Message: "cut", Retryable: true},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"terminal_status":"error"`, `"code":"stream_unexpected_eof"`, `"retryable":true`} {
		if !strings.Contains(string(errPayload), want) {
			t.Fatalf("error terminal frame must contain %s, got %s", want, errPayload)
		}
	}
}

func TestStreamChunkLegacyJSONStaysCompatible(t *testing.T) {
	// 旧消费者只认 Delta/Index/Done 的 JSON 必须能零值兼容解析。
	var chunk StreamChunk
	if err := json.Unmarshal([]byte(`{"delta":"x","index":3,"done":true}`), &chunk); err != nil {
		t.Fatalf("unmarshal legacy frame: %v", err)
	}
	if chunk.Delta != "x" || chunk.Index != 3 || !chunk.Done {
		t.Fatalf("legacy fields broken: %+v", chunk)
	}
	if chunk.TerminalStatus != "" || chunk.Error != nil || chunk.FinishReason != "" || chunk.Usage != nil {
		t.Fatalf("new terminal fields must default to zero values: %+v", chunk)
	}
}

func TestTerminalStatusEnumFrozen(t *testing.T) {
	// §31.3：terminal_status 枚举固定 completed/error/cancelled。
	want := map[TerminalStatus]bool{
		TerminalCompleted: true,
		TerminalError:     true,
		TerminalCancelled: true,
	}
	for status := range want {
		switch status {
		case "completed", "error", "cancelled":
		default:
			t.Fatalf("unexpected terminal status value %q", status)
		}
	}
}
