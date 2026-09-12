package adapters

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/hooks"
)

// 本文件是 T13.04.b 的发送侧接线测试（§28 T13.04.b、§31.2 E-04/E-05）。
// 全部使用本地 httptest fake transport，不发起真实模型调用。
// 帧序列与终结状态由 a 步 parser 驱动：内容帧一解析出来就投递（逐帧流式），
// 解析结束后才产出终结帧。取消语义覆盖两个阶段——body 读取期（ctx 取消使读取
// 失败）与投递期（send select ctx.Done，sink 报告取消后 parser 立即停止扫描）。

type streamProviderCase struct {
	name          string
	newAdapter    func(baseURL string) ICliAdapter
	successBody   string
	wantDeltas    []string
	wantContent   string
	wantFinish    string
	wantUsage     *Usage
	providerErr   string
	wantErrCode   string
	eofBody       string
	malformedBody string
	oversizedBody string
	frame         func(text string) string
	// terminator 该 provider 的完成标记帧，拼在 frame() 之后即构成一条
	// 可信完成的流（逐帧投递测试用）。
	terminator string
}

func streamProviderCases() []streamProviderCase {
	newAdapter := func(base string, newFn func(*AdapterConfig) ICliAdapter) ICliAdapter {
		return newFn(&AdapterConfig{APIKey: "test-key", BaseURL: base, Model: "test-model"})
	}
	return []streamProviderCase{
		{
			name: "openai",
			newAdapter: func(base string) ICliAdapter {
				return newAdapter(base, func(c *AdapterConfig) ICliAdapter { return NewOpenAIAdapter(c) })
			},
			successBody: "data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n" +
				"data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n" +
				"data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n" +
				"data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
				"data: {\"id\":\"chatcmpl-1\",\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":2,\"total_tokens\":11}}\n\n" +
				"data: [DONE]\n\n",
			wantDeltas:  []string{"Hello", " world"},
			wantContent: "Hello world",
			wantFinish:  "stop",
			wantUsage:   &Usage{PromptTokens: 9, CompletionTokens: 2, TotalTokens: 11},
			providerErr: "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n" +
				"data: {\"error\":{\"message\":\"The server had an error\",\"type\":\"server_error\",\"code\":null}}\n\n",
			wantErrCode: "server_error",
			eofBody:     "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"cut off\"},\"finish_reason\":null}]}\n\n",
			malformedBody: "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n" +
				"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"half",
			oversizedBody: "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"before\"},\"finish_reason\":null}]}\n\n" +
				"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"" + strings.Repeat("x", 128*1024) + "\"},\"finish_reason\":null}]}\n\n" +
				"data: [DONE]\n\n",
			frame: func(text string) string {
				return fmt.Sprintf("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", text)
			},
			terminator: "data: [DONE]\n\n",
		},
		{
			name: "claude",
			newAdapter: func(base string) ICliAdapter {
				return newAdapter(base, func(c *AdapterConfig) ICliAdapter { return NewClaudeAdapter(c) })
			},
			successBody: "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":25,\"output_tokens\":1}}}\n\n" +
				"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
				"data: {\"type\":\"ping\"}\n\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" Claude\"}}\n\n" +
				"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
				"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":12}}\n\n" +
				"data: {\"type\":\"message_stop\"}\n\n",
			wantDeltas:  []string{"Hello", " Claude"},
			wantContent: "Hello Claude",
			wantFinish:  "end_turn",
			wantUsage:   &Usage{PromptTokens: 25, CompletionTokens: 12, TotalTokens: 37},
			providerErr: "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n" +
				"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n" +
				"data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n",
			wantErrCode: "overloaded_error",
			eofBody:     "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"cut\"}}\n\n",
			malformedBody: "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n" +
				"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_",
			oversizedBody: "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"before\"}}\n\n" +
				"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"" + strings.Repeat("好", 40*1024) + "\"}}\n\n" +
				"data: {\"type\":\"message_stop\"}\n\n",
			frame: func(text string) string {
				return fmt.Sprintf("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", text)
			},
			terminator: "data: {\"type\":\"message_stop\"}\n\n",
		},
		{
			name: "gemini",
			newAdapter: func(base string) ICliAdapter {
				return newAdapter(base, func(c *AdapterConfig) ICliAdapter { return NewGeminiAdapter(c) })
			},
			successBody: "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Hello\"}]}}]}\n\n" +
				"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\" Gemini\"}]}}]}\n\n" +
				"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"!\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":3,\"totalTokenCount\":10}}\n\n",
			wantDeltas:  []string{"Hello", " Gemini", "!"},
			wantContent: "Hello Gemini!",
			wantFinish:  "STOP",
			wantUsage:   &Usage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10},
			providerErr: "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"partial\"}]}}]}\n\n" +
				"data: {\"error\":{\"code\":429,\"message\":\"Quota exceeded\",\"status\":\"RESOURCE_EXHAUSTED\"}}\n\n",
			wantErrCode: "RESOURCE_EXHAUSTED",
			eofBody:     "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"cut\"}]}}]}\n\n",
			malformedBody: "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"ok\"}]}}]}\n\n" +
				"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"par",
			oversizedBody: "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"before\"}]}}]}\n\n" +
				"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"" + strings.Repeat("y", 128*1024) + "\"}]}}]}\n\n" +
				"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[]},\"finishReason\":\"STOP\"}]}\n\n",
			frame: func(text string) string {
				return fmt.Sprintf("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":%q}]}}]}\n\n", text)
			},
			terminator: "data: {\"candidates\":[{\"content\":{\"parts\":[]},\"finishReason\":\"STOP\"}]}\n\n",
		},
	}
}

// serveSSE 起本地 fake provider，响应体为固定 SSE 内容。
func serveSSE(t *testing.T, body string, requests *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests != nil {
			requests.Add(1)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// collectStreamChunks 在 deadline 内排空 channel；channel 未关闭（goroutine 泄漏）即失败。
func collectStreamChunks(t *testing.T, ch <-chan StreamChunk) []StreamChunk {
	t.Helper()
	deadline := time.After(5 * time.Second)
	var chunks []StreamChunk
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return chunks
			}
			chunks = append(chunks, chunk)
		case <-deadline:
			t.Fatal("timed out waiting for stream channel to close (goroutine leak)")
			return nil
		}
	}
}

// splitTerminal 断言恰好一个终结帧，返回内容帧与终结帧。
func splitTerminal(t *testing.T, chunks []StreamChunk) (contents []StreamChunk, terminal StreamChunk) {
	t.Helper()
	terminalCount := 0
	for _, chunk := range chunks {
		if chunk.Done {
			terminalCount++
			terminal = chunk
			continue
		}
		if chunk.TerminalStatus != "" || chunk.Error != nil || chunk.Usage != nil {
			t.Fatalf("content frame must not carry terminal fields: %+v", chunk)
		}
		contents = append(contents, chunk)
	}
	if terminalCount != 1 {
		t.Fatalf("expected exactly one terminal frame, got %d (chunks=%v)", terminalCount, chunks)
	}
	return contents, terminal
}

func assertAssistantHistory(t *testing.T, adapter ICliAdapter, wantContent string) {
	t.Helper()
	history := adapter.GetHistory()
	if len(history) != 2 {
		t.Fatalf("expected user+assistant history, got %d messages", len(history))
	}
	if history[0].Role != RoleUser || history[1].Role != RoleAssistant {
		t.Fatalf("unexpected history roles: %v, %v", history[0].Role, history[1].Role)
	}
	if history[1].Content != wantContent {
		t.Fatalf("assistant history = %q, want %q", history[1].Content, wantContent)
	}
}

func assertNoAssistantHistory(t *testing.T, adapter ICliAdapter) {
	t.Helper()
	history := adapter.GetHistory()
	for _, msg := range history {
		if msg.Role == RoleAssistant {
			t.Fatalf("failed stream must not append successful assistant history, got %+v", msg)
		}
	}
}

// TestAdapterStreamCompletedTerminal E-04 正向：协议合法完成才有唯一 completed 终结帧，
// finish_reason/usage 为 provider 原值，assistant 内容追加为成功历史。
func TestAdapterStreamCompletedTerminal(t *testing.T) {
	for _, tc := range streamProviderCases() {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			srv := serveSSE(t, tc.successBody, &requests)
			adapter := tc.newAdapter(srv.URL)

			ch, err := adapter.Stream(context.Background(), "hello", nil)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			contents, terminal := splitTerminal(t, collectStreamChunks(t, ch))

			if len(contents) != len(tc.wantDeltas) {
				t.Fatalf("expected %d content frames, got %d", len(tc.wantDeltas), len(contents))
			}
			for i, want := range tc.wantDeltas {
				if contents[i].Delta != want || contents[i].Index != i {
					t.Fatalf("content frame %d = %+v, want delta %q index %d", i, contents[i], want, i)
				}
			}
			if terminal.TerminalStatus != TerminalCompleted {
				t.Fatalf("terminal status = %q, want completed", terminal.TerminalStatus)
			}
			if terminal.Error != nil {
				t.Fatalf("completed terminal must not carry error, got %+v", terminal.Error)
			}
			if terminal.FinishReason != tc.wantFinish {
				t.Fatalf("finish_reason = %q, want provider-reported %q (no fake stop)", terminal.FinishReason, tc.wantFinish)
			}
			if tc.wantUsage != nil {
				if terminal.Usage == nil || *terminal.Usage != *tc.wantUsage {
					t.Fatalf("terminal usage = %+v, want %+v", terminal.Usage, tc.wantUsage)
				}
			}
			assertAssistantHistory(t, adapter, tc.wantContent)
			if got := requests.Load(); got != 1 {
				t.Fatalf("expected exactly one request (no re-send of consumed request), got %d", got)
			}
		})
	}
}

// TestAdapterStreamProviderErrorNotSuccessfulHistory E-04 负向：流内 provider 错误
// 产出 error 终结（含结构化 code），不产生成功终结与成功 assistant 历史；已投递的
// 部分 delta 保留供错误展示，但不冒充完整回答。
func TestAdapterStreamProviderErrorNotSuccessfulHistory(t *testing.T) {
	for _, tc := range streamProviderCases() {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			srv := serveSSE(t, tc.providerErr, &requests)
			adapter := tc.newAdapter(srv.URL)

			ch, err := adapter.Stream(context.Background(), "hello", nil)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			contents, terminal := splitTerminal(t, collectStreamChunks(t, ch))

			if len(contents) != 1 || contents[0].Delta != "partial" {
				t.Fatalf("expected partial delta before error, got %v", contents)
			}
			if terminal.TerminalStatus != TerminalError {
				t.Fatalf("terminal status = %q, want error", terminal.TerminalStatus)
			}
			if terminal.Error == nil || terminal.Error.Code != tc.wantErrCode {
				t.Fatalf("terminal error = %+v, want code %q", terminal.Error, tc.wantErrCode)
			}
			assertNoAssistantHistory(t, adapter)
			if got := requests.Load(); got != 1 {
				t.Fatalf("failed stream must not re-send a consumed request, got %d requests", got)
			}
		})
	}
}

// TestAdapterProviderErrorNotSuccessfulHistory E-04 负向（§28 T13.04.c 点名）：
// 流内 provider 错误产出唯一 error 终结帧（结构化 code 为 provider 原值），
// 不追加成功 assistant 历史；已投递的部分 delta 仅供错误展示，不冒充完整回答，
// 已消费的失败请求不重发。语义与 TestAdapterStreamProviderErrorNotSuccessfulHistory 一致，
// 本测试钉住 §28 点名入口。
func TestAdapterProviderErrorNotSuccessfulHistory(t *testing.T) {
	for _, tc := range streamProviderCases() {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			srv := serveSSE(t, tc.providerErr, &requests)
			adapter := tc.newAdapter(srv.URL)

			ch, err := adapter.Stream(context.Background(), "hello", nil)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			contents, terminal := splitTerminal(t, collectStreamChunks(t, ch))

			if len(contents) != 1 || contents[0].Delta != "partial" {
				t.Fatalf("expected only the partial delta before the error, got %v", contents)
			}
			if terminal.TerminalStatus != TerminalError {
				t.Fatalf("terminal status = %q, want error", terminal.TerminalStatus)
			}
			if terminal.Error == nil || terminal.Error.Code != tc.wantErrCode {
				t.Fatalf("terminal error = %+v, want provider code %q", terminal.Error, tc.wantErrCode)
			}
			assertNoAssistantHistory(t, adapter)
			if got := requests.Load(); got != 1 {
				t.Fatalf("failed stream must not re-send a consumed request, got %d requests", got)
			}
		})
	}
}

// TestAdapterUnexpectedEOFTerminatesWithError E-04 负向：无完成标记的 EOF 不是成功。
func TestAdapterUnexpectedEOFTerminatesWithError(t *testing.T) {
	for _, tc := range streamProviderCases() {
		t.Run(tc.name, func(t *testing.T) {
			srv := serveSSE(t, tc.eofBody, nil)
			adapter := tc.newAdapter(srv.URL)

			ch, err := adapter.Stream(context.Background(), "hello", nil)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			_, terminal := splitTerminal(t, collectStreamChunks(t, ch))

			if terminal.TerminalStatus != TerminalError {
				t.Fatalf("terminal status = %q, want error", terminal.TerminalStatus)
			}
			if terminal.Error == nil || terminal.Error.Code != "stream_unexpected_eof" {
				t.Fatalf("terminal error = %+v, want stream_unexpected_eof", terminal.Error)
			}
			assertNoAssistantHistory(t, adapter)
		})
	}
}

// TestAdapterStreamOversizedFrameTerminatesWithError E-04 负向：超过 scanner 上限的单帧
// 记为扫描失败，不再被静默吞掉后伪报完成。
func TestAdapterStreamOversizedFrameTerminatesWithError(t *testing.T) {
	for _, tc := range streamProviderCases() {
		t.Run(tc.name, func(t *testing.T) {
			srv := serveSSE(t, tc.oversizedBody, nil)
			adapter := tc.newAdapter(srv.URL)

			ch, err := adapter.Stream(context.Background(), "hello", nil)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			_, terminal := splitTerminal(t, collectStreamChunks(t, ch))

			if terminal.TerminalStatus != TerminalError || terminal.Error == nil || terminal.Error.Code != "stream_scan_failed" {
				t.Fatalf("terminal = %+v, want error/stream_scan_failed", terminal)
			}
			assertNoAssistantHistory(t, adapter)
		})
	}
}

// TestAdapterStreamMalformedFrameTerminatesWithError E-04 负向：关键帧损坏记为 error 终结。
func TestAdapterStreamMalformedFrameTerminatesWithError(t *testing.T) {
	for _, tc := range streamProviderCases() {
		t.Run(tc.name, func(t *testing.T) {
			srv := serveSSE(t, tc.malformedBody, nil)
			adapter := tc.newAdapter(srv.URL)

			ch, err := adapter.Stream(context.Background(), "hello", nil)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			_, terminal := splitTerminal(t, collectStreamChunks(t, ch))

			if terminal.TerminalStatus != TerminalError || terminal.Error == nil || terminal.Error.Code != "stream_frame_malformed" {
				t.Fatalf("terminal = %+v, want error/stream_frame_malformed", terminal)
			}
			assertNoAssistantHistory(t, adapter)
		})
	}
}

// TestAdapterCancelledFullChannelClosesBody E-05：provider 输出超过 channel 缓冲（100），
// 消费者读几帧后取消并停止读取；投递期阻塞的 send 必须经 select ctx.Done 解除，
// channel 在 deadline 内关闭（close 晚于 resp.Body.Close 的 defer 序，故关闭即 body 已关闭），
// 不为发送取消终结帧等待不存在的消费者，也不产生成功历史。
func TestAdapterCancelledFullChannelClosesBody(t *testing.T) {
	for _, tc := range streamProviderCases() {
		t.Run(tc.name, func(t *testing.T) {
			var body strings.Builder
			for i := 0; i < 150; i++ {
				body.WriteString(tc.frame(fmt.Sprintf("chunk-%d", i)))
			}
			srv := serveSSE(t, body.String(), nil) // 无完成标记，body 结束即 EOF
			adapter := tc.newAdapter(srv.URL)

			ctx, cancel := context.WithCancel(context.Background())
			ch, err := adapter.Stream(ctx, "hello", nil)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}

			for i := 0; i < 3; i++ {
				select {
				case <-ch:
				case <-time.After(2 * time.Second):
					t.Fatalf("timed out waiting for content frame %d", i)
				}
			}
			cancel()

			// 停止读取后 channel 仍须在 deadline 内关闭。
			rest := collectStreamChunks(t, ch)
			for _, chunk := range rest {
				if chunk.Done && chunk.TerminalStatus == TerminalCompleted {
					t.Fatalf("cancelled stream must not report completed: %+v", chunk)
				}
				if chunk.Done && chunk.TerminalStatus != TerminalCancelled {
					t.Fatalf("terminal after cancel must be cancelled, got %+v", chunk)
				}
			}
			assertNoAssistantHistory(t, adapter)
		})
	}
}

// TestAdapterCancelledDuringBodyReadClosesChannel E-05：body 读取期（流未结束、parser
// 阻塞在读）取消，ctx 使传输层读取失败，channel/body 在 deadline 内关闭。
// 服务端经 request context 观测到客户端断开，证明响应体确已关闭。
func TestAdapterCancelledDuringBodyReadClosesChannel(t *testing.T) {
	for _, tc := range streamProviderCases() {
		t.Run(tc.name, func(t *testing.T) {
			disconnected := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, tc.frame("first"))
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				select {
				case <-r.Context().Done():
					close(disconnected)
				case <-time.After(5 * time.Second):
				}
			}))
			t.Cleanup(srv.Close)
			adapter := tc.newAdapter(srv.URL)

			ctx, cancel := context.WithCancel(context.Background())
			ch, err := adapter.Stream(ctx, "hello", nil)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}

			// 流仍在传输（body 未结束）：provider 已 flush 的内容帧必须已经
			// 投递到消费者手里——Stream 是逐帧投递，不是读完整条流再一次性
			// 交付。这里不能断言"此时没有任何帧"，那会把缓冲到底的行为钉死。
			select {
			case chunk := <-ch:
				if chunk.Done || chunk.Delta != "first" {
					t.Fatalf("expected the already-flushed content frame, got %+v", chunk)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("content frame flushed by the provider was not delivered before the stream ended")
			}

			cancel()

			// 允许一帧 best-effort 的 cancelled 终结帧；不允许任何内容帧或其它终结状态。
			for _, chunk := range collectStreamChunks(t, ch) {
				if !chunk.Done {
					t.Fatalf("cancelled-before-completion stream must not emit content frames, got %+v", chunk)
				}
				if chunk.TerminalStatus != TerminalCancelled {
					t.Fatalf("terminal after cancel must be cancelled, got %+v", chunk)
				}
			}
			select {
			case <-disconnected:
			case <-time.After(2 * time.Second):
				t.Fatal("server did not observe client disconnect (response body not closed)")
			}
			assertNoAssistantHistory(t, adapter)
		})
	}
}

// TestAdapterStreamPostResponseUsesProviderFinishReason E-04：成功流的 PostResponse hook
// 收到 provider 上报的 finish_reason 与 usage 原值（不伪报 stop）；失败流不触发 PostResponse。
func TestAdapterStreamPostResponseUsesProviderFinishReason(t *testing.T) {
	previous := hooks.GetHookManager()
	hooks.SetHookManager(nil)
	t.Cleanup(func() { hooks.SetHookManager(previous) })

	mgr := hooks.NewHookManager()
	hooks.SetHookManager(mgr)

	type postResponseSeen struct {
		finishReason string
		usage        Usage
	}
	var postCalls []postResponseSeen
	if err := mgr.Register(hooks.HookConfig{Name: "post", Type: hooks.HookPostResponse, Enabled: true}, func(ctx context.Context, payload interface{}) (interface{}, error) {
		if resp, ok := payload.(*AIResponse); ok {
			postCalls = append(postCalls, postResponseSeen{finishReason: resp.FinishReason, usage: resp.Usage})
		}
		return payload, nil
	}); err != nil {
		t.Fatalf("register post hook: %v", err)
	}

	for _, tc := range streamProviderCases() {
		t.Run(tc.name+"_success", func(t *testing.T) {
			postCalls = nil
			srv := serveSSE(t, tc.successBody, nil)
			adapter := tc.newAdapter(srv.URL)
			ch, err := adapter.Stream(context.Background(), "hello", nil)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			collectStreamChunks(t, ch)
			if len(postCalls) != 1 {
				t.Fatalf("expected one PostResponse hook call, got %d", len(postCalls))
			}
			if postCalls[0].finishReason != tc.wantFinish {
				t.Fatalf("PostResponse finish reason = %q, want %q (not a fake stop)", postCalls[0].finishReason, tc.wantFinish)
			}
			if tc.wantUsage != nil && postCalls[0].usage != *tc.wantUsage {
				t.Fatalf("PostResponse usage = %+v, want %+v", postCalls[0].usage, *tc.wantUsage)
			}
		})
		t.Run(tc.name+"_error", func(t *testing.T) {
			postCalls = nil
			srv := serveSSE(t, tc.providerErr, nil)
			adapter := tc.newAdapter(srv.URL)
			ch, err := adapter.Stream(context.Background(), "hello", nil)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			collectStreamChunks(t, ch)
			if len(postCalls) != 0 {
				t.Fatalf("failed stream must not trigger PostResponse, got %d calls", len(postCalls))
			}
		})
	}
}

// TestAdapterStreamDeliversFramesIncrementally 钉住 Stream 的核心语义：内容帧
// 在 provider 产出的当时就投递给消费者，而不是等整条响应体读完再一次性交付。
//
// 复审 P1 修复的回归测试。修复前三个 provider 都是
// `emitStreamResult(..., parseXStreamBody(resp.Body), ch)`——Go 先求值参数，
// 于是整条 SSE 被读完解析完才投出第一帧，消费者在模型生成结束前什么都收不到，
// Stream 退化成一次性返回。本测试让 provider 在发完第一帧后阻塞，只有逐帧
// 投递才能在阻塞期间收到它。
func TestAdapterStreamDeliversFramesIncrementally(t *testing.T) {
	for _, tc := range streamProviderCases() {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				flusher, _ := w.(http.Flusher)
				_, _ = io.WriteString(w, tc.frame("early"))
				if flusher != nil {
					flusher.Flush()
				}
				// 第二帧与完成标记要等消费者确认收到第一帧后才发出。
				select {
				case <-release:
				case <-time.After(5 * time.Second):
				}
				_, _ = io.WriteString(w, tc.frame("late")+tc.terminator)
				if flusher != nil {
					flusher.Flush()
				}
			}))
			t.Cleanup(srv.Close)

			adapter := tc.newAdapter(srv.URL)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			ch, err := adapter.Stream(ctx, "hello", nil)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}

			// 响应体还没写完（服务端阻塞中），第一帧就必须已经到达。
			select {
			case chunk, ok := <-ch:
				if !ok {
					t.Fatal("stream closed before delivering the first frame")
				}
				if chunk.Done {
					t.Fatalf("expected a content frame before the stream ended, got terminal %+v", chunk)
				}
				if chunk.Delta != "early" {
					t.Fatalf("expected the first flushed delta %q, got %+v", "early", chunk)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("first content frame was not delivered while the response body was still open: Stream is buffering instead of streaming")
			}

			close(release)

			// 放行后流正常完成：第二帧 + completed 终结帧，历史正常落地。
			rest := collectStreamChunks(t, ch)
			var deltas []string
			var terminal *StreamChunk
			for i := range rest {
				if rest[i].Done {
					terminal = &rest[i]
					continue
				}
				deltas = append(deltas, rest[i].Delta)
			}
			if len(deltas) != 1 || deltas[0] != "late" {
				t.Fatalf("expected the remaining content frame %q, got %v", "late", deltas)
			}
			if terminal == nil {
				t.Fatal("stream ended without a terminal frame")
			}
			if terminal.TerminalStatus != TerminalCompleted {
				t.Fatalf("expected completed terminal, got %+v", terminal)
			}
		})
	}
}
