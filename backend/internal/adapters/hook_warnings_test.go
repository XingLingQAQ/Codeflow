package adapters

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	backendhooks "github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/policy/policytesting"
)

// 本文件是 T1.07.b 第 2 组在模型 provider 层的测试（§15 T1.07、§28 T1.07.b）。
// 它用真实的 hooks.NewHookManager()（SetHookManager 注入、Cleanup 恢复）与真实
// 注册的、必然失败的 hook handler 钉住：
//
//   - HookPostResponse 失败不让已经完成的模型调用失败：非流式 Send 与流式
//     Stream 都照常返回响应，且警告恰好一条（两条路径行为一致，正是收口断言）；
//   - HookOnStream 失败不丢帧、不重发帧：每个失败分片照常投递，警告每个分片
//     恰好一条；
//   - 没有 sink 时走默认日志，日志里绝不含响应正文或分片内容；
//   - sink 读写并发安全（20 个 goroutine 同时触发失败 hook，20 条警告一条不多
//     一条不少；本机无 cgo/gcc，-race 不可用，见回执的 not_run 登记）。
//
// HookBeforeSend 的 reject 语义不在本步骤改动，也在这里再断言一次：hook 失败时
// 模型请求不发出（一次 HTTP 都不能有）。

// hookTestEnv 安装一个真实的 hook 管理器（Cleanup 时恢复原管理器）。
func hookTestEnv(t *testing.T) *backendhooks.HookManager {
	t.Helper()
	policytesting.AllowForTest(t, policy.OperationOutboundRequest, policy.OperationResponseReceive, policy.OperationHookExecute)
	previous := backendhooks.GetHookManager()
	mgr := backendhooks.NewHookManager()
	backendhooks.SetHookManager(mgr)
	t.Cleanup(func() { backendhooks.SetHookManager(previous) })
	return mgr
}

// registerFailingHook 注册一个必然失败的 hook handler，并返回它的调用次数。
func registerFailingHook(t *testing.T, mgr *backendhooks.HookManager, hook backendhooks.HookType, err error) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	if regErr := mgr.Register(backendhooks.HookConfig{
		Name:    "failing-" + string(hook),
		Type:    hook,
		Enabled: true,
		Timeout: 5 * time.Second,
	}, func(context.Context, backendhooks.HookPayload) (backendhooks.HookResult, error) {
		calls.Add(1)
		return nil, err
	}); regErr != nil {
		t.Fatalf("register %s hook: %v", hook, regErr)
	}
	return &calls
}

// captureAdapterWarnings 安装一个收集警告的 sink，Cleanup 时恢复 nil（默认日志）。
func captureAdapterWarnings(t *testing.T) (*[]HookWarning, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var warnings []HookWarning
	SetHookWarningSink(func(_ context.Context, w HookWarning) {
		mu.Lock()
		warnings = append(warnings, w)
		mu.Unlock()
	})
	t.Cleanup(func() { SetHookWarningSink(nil) })
	return &warnings, &mu
}

// adapterWarnings 返回已收集的警告快照。
func adapterWarnings(warnings *[]HookWarning, mu *sync.Mutex) []HookWarning {
	mu.Lock()
	defer mu.Unlock()
	return append([]HookWarning(nil), (*warnings)...)
}

// adapterWarningLog 把默认警告日志抓到内存里（缺少 sink 时用）。
func adapterWarningLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var captured bytes.Buffer
	var mu sync.Mutex
	previous := log.Writer()
	log.SetOutput(&lockedWriter{mu: &mu, w: &captured})
	t.Cleanup(func() { log.SetOutput(previous) })
	return &captured
}

// lockedWriter 让 log.SetOutput 的写入与测试读取互斥（日志可能来自流 goroutine）。
type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// nonStreamingProviderCase 描述一个非流式 provider 的 fake 后端与调用方式。
type nonStreamingProviderCase struct {
	name string
	// adapter 已经指向 fake provider（由 nonStreamingProviderCases 起服务）。
	adapter ICliAdapter
	// content 是 fake provider 返回的正文；断言它一定出现在返回的响应里。
	content string
}

// nonStreamingProviderCases 用三个 provider 的真实 Send 路径覆盖「响应已经拿到
// 之后的 hook 失败不能让调用失败」。
func nonStreamingProviderCases(t *testing.T) []nonStreamingProviderCase {
	t.Helper()
	const content = "post-response-payload-must-not-be-logged"

	serve := func(body string) string {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, body)
		}))
		t.Cleanup(server.Close)
		return server.URL
	}

	claudeURL := serve(fmt.Sprintf(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":%q}],"model":"test-model","stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":4}}`, content))
	geminiURL := serve(fmt.Sprintf(`{"candidates":[{"content":{"role":"model","parts":[{"text":%q}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4,"totalTokenCount":7}}`, content))
	openaiURL := serve(fmt.Sprintf(`{"id":"chatcmpl-1","model":"test-model","choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`, content))

	// 三个 provider 的 Send 都调用 notifyAdapterPostResponse（gemini.go:75、
	// claude.go:364、openai.go:90），所以三条路径都必须通过同一组断言。
	return []nonStreamingProviderCase{
		{
			name:    "claude",
			adapter: NewClaudeAdapter(&AdapterConfig{APIKey: "test-key", BaseURL: claudeURL, Model: "test-model", MaxRetries: 0}),
			content: content,
		},
		{
			name:    "gemini",
			adapter: NewGeminiAdapter(&AdapterConfig{APIKey: "test-key", BaseURL: geminiURL, Model: "test-model", MaxRetries: 0}),
			content: content,
		},
		{
			name:    "openai",
			adapter: NewOpenAIAdapter(&AdapterConfig{APIKey: "test-key", BaseURL: openaiURL, Model: "test-model", MaxRetries: 0}),
			content: content,
		},
	}
}

// TestPostResponseHookFailureDoesNotFailNonStreamingSend 收口的第一半：非流式路径
// 的 PostResponse hook 失败时，模型响应照常返回，警告恰好一条。
func TestPostResponseHookFailureDoesNotFailNonStreamingSend(t *testing.T) {
	hookErr := errors.New("post-response observer exploded")
	for _, tc := range nonStreamingProviderCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			mgr := hookTestEnv(t)
			calls := registerFailingHook(t, mgr, backendhooks.HookPostResponse, hookErr)
			warnings, mu := captureAdapterWarnings(t)

			response, err := tc.adapter.Send(context.Background(), "hello", nil)
			if err != nil {
				t.Fatalf("a failing after-hook made Send fail: %v", err)
			}
			if response == nil {
				t.Fatal("Send returned a nil response")
			}
			if !strings.Contains(response.Content, tc.content) {
				t.Fatalf("response content = %q, want it to contain the provider payload", response.Content)
			}

			got := adapterWarnings(warnings, mu)
			if got := calls.Load(); got != 1 {
				t.Fatalf("the handler ran %d times, want exactly 1 (one response, one trigger record)", got)
			}
			if len(got) != 1 {
				t.Fatalf("warnings = %d %+v, want exactly 1 for the failing PostResponse hook", len(got), got)
			}
			if got[0].Hook != backendhooks.HookPostResponse {
				t.Errorf("warning hook = %q, want %q", got[0].Hook, backendhooks.HookPostResponse)
			}
			if !errors.Is(got[0].Err, hookErr) {
				t.Errorf("warning error = %v, want the handler failure", got[0].Err)
			}
		})
	}
}

// TestPostResponseHookFailureDoesNotFailStreamingStream 收口的第二半：流式路径的
// PostResponse hook 失败时，流照常完成、帧照常投递，警告恰好一条。两条路径都成功、
// 都恰好一条警告——一致本身就是这个触发点的收口证据。
func TestPostResponseHookFailureDoesNotFailStreamingStream(t *testing.T) {
	hookErr := errors.New("stream post-response observer exploded")
	for _, tc := range streamProviderCases() {
		t.Run(tc.name, func(t *testing.T) {
			mgr := hookTestEnv(t)
			calls := registerFailingHook(t, mgr, backendhooks.HookPostResponse, hookErr)
			warnings, mu := captureAdapterWarnings(t)

			server := serveSSE(t, tc.successBody, nil)
			ch, err := tc.newAdapter(server.URL).Stream(context.Background(), "hello", nil)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			contents, terminal := splitTerminal(t, collectStreamChunks(t, ch))
			if terminal.TerminalStatus != TerminalCompleted {
				t.Fatalf("terminal status = %s, want completed: a post-response hook failure must not change the stream outcome", terminal.TerminalStatus)
			}
			if joined := joinDeltas(contents); joined != tc.wantContent {
				t.Fatalf("streamed content = %q, want %q", joined, tc.wantContent)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("the handler ran %d times, want exactly 1", got)
			}
			got := adapterWarnings(warnings, mu)
			if len(got) != 1 {
				t.Fatalf("warnings = %d %+v, want exactly 1 for the failing PostResponse hook", len(got), got)
			}
			if got[0].Hook != backendhooks.HookPostResponse || !errors.Is(got[0].Err, hookErr) {
				t.Errorf("warning = %+v, want the PostResponse handler failure", got[0])
			}
		})
	}
}

// TestOnStreamHookFailureKeepsEveryFrame 钉住 HookOnStream 的收口：handler 每个分片
// 都失败，每个分片照常投递（一帧不多、一帧不少、顺序不变），且每个分片恰好一条
// 警告——绝不因为 hook 失败丢帧或重发帧。
func TestOnStreamHookFailureKeepsEveryFrame(t *testing.T) {
	hookErr := errors.New("chunk observer exploded")
	for _, tc := range streamProviderCases() {
		t.Run(tc.name, func(t *testing.T) {
			mgr := hookTestEnv(t)
			calls := registerFailingHook(t, mgr, backendhooks.HookOnStream, hookErr)
			warnings, mu := captureAdapterWarnings(t)

			server := serveSSE(t, tc.successBody, nil)
			ch, err := tc.newAdapter(server.URL).Stream(context.Background(), "hello", nil)
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			contents, terminal := splitTerminal(t, collectStreamChunks(t, ch))
			if terminal.TerminalStatus != TerminalCompleted {
				t.Fatalf("terminal status = %s, want completed", terminal.TerminalStatus)
			}

			gotDeltas := make([]string, 0, len(contents))
			for _, chunk := range contents {
				gotDeltas = append(gotDeltas, chunk.Delta)
			}
			if len(gotDeltas) != len(tc.wantDeltas) {
				t.Fatalf("delivered %d content frames (%v), want exactly %d (%v): a failing hook must not drop or duplicate frames",
					len(gotDeltas), gotDeltas, len(tc.wantDeltas), tc.wantDeltas)
			}
			for i := range gotDeltas {
				if gotDeltas[i] != tc.wantDeltas[i] {
					t.Fatalf("frame %d = %q, want %q (order must not change)", i, gotDeltas[i], tc.wantDeltas[i])
				}
			}

			// 每个投递出去的帧（内容帧 + 终结帧）都恰好走过一次
			// notifyAdapterStreamChunk：触发次数必须等于投递帧数。丢帧会少、重发会多，
			// 所以这个等式连同上面的 delta 逐一比对一起证明「不丢帧、不重发」。
			triggered := int(calls.Load())
			if want := len(contents) + 1; triggered != want {
				t.Fatalf("the handler ran %d times, want exactly %d (one per delivered frame: %d content + 1 terminal)",
					triggered, want, len(contents))
			}
			got := adapterWarnings(warnings, mu)
			if len(got) != triggered {
				t.Fatalf("warnings = %d, want one per triggered frame (%d)", len(got), triggered)
			}
			for _, warning := range got {
				if warning.Hook != backendhooks.HookOnStream || !errors.Is(warning.Err, hookErr) {
					t.Fatalf("warning = %+v, want the OnStream handler failure", warning)
				}
			}
		})
	}
}

// TestHookWarningDefaultLogOmitsResponsePayload nil sink 时警告走一行固定格式日志，
// 日志里绝不能出现响应正文。
func TestHookWarningDefaultLogOmitsResponsePayload(t *testing.T) {
	const payload = "zz-sensitive-model-output-mark-zz"
	mgr := hookTestEnv(t)
	registerFailingHook(t, mgr, backendhooks.HookPostResponse, errors.New("observer exploded"))
	SetHookWarningSink(nil)
	logged := adapterWarningLog(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":%q}],"model":"test-model","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`, payload)
	}))
	t.Cleanup(server.Close)

	adapter := NewClaudeAdapter(&AdapterConfig{APIKey: "test-key", BaseURL: server.URL, Model: "test-model", MaxRetries: 0})
	response, err := adapter.Send(context.Background(), "hello", nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(response.Content, payload) {
		t.Fatalf("response content = %q, want the provider payload", response.Content)
	}

	text := logged.String()
	if !strings.Contains(text, string(backendhooks.HookPostResponse)) {
		t.Fatalf("default log %q does not name the failing hook", text)
	}
	if strings.Contains(text, payload) {
		t.Fatalf("default log contains the model response payload: %q", text)
	}
}

// TestOnStreamHookFailureLogOmitsChunkPayload 同上，针对流分片：默认日志不含分片正文。
func TestOnStreamHookFailureLogOmitsChunkPayload(t *testing.T) {
	const payload = "zz-sensitive-stream-delta-mark-zz"
	mgr := hookTestEnv(t)
	registerFailingHook(t, mgr, backendhooks.HookOnStream, errors.New("chunk observer exploded"))
	SetHookWarningSink(nil)
	logged := adapterWarningLog(t)

	frame := fmt.Sprintf("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", payload)
	server := serveSSE(t, frame+"data: {\"type\":\"message_stop\"}\n\n", nil)
	ch, err := NewClaudeAdapter(&AdapterConfig{APIKey: "test-key", BaseURL: server.URL, Model: "test-model", MaxRetries: 0}).Stream(context.Background(), "hello", nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	delivered := false
	for _, chunk := range collectStreamChunks(t, ch) {
		if strings.Contains(chunk.Delta, payload) {
			delivered = true
		}
	}
	if !delivered {
		t.Fatal("the frame was not delivered at all: the hook failure must not drop it")
	}

	text := logged.String()
	if !strings.Contains(text, string(backendhooks.HookOnStream)) {
		t.Fatalf("default log %q does not name the failing hook", text)
	}
	if strings.Contains(text, payload) {
		t.Fatalf("default log contains the stream delta payload: %q", text)
	}
}

// TestHookWarningSinkNilFallsBackToDefaultLog 直接钉住 sink 的 nil 语义：nil 时走
// 默认日志，而不是把警告丢掉。
func TestHookWarningSinkNilFallsBackToDefaultLog(t *testing.T) {
	logged := adapterWarningLog(t)
	SetHookWarningSink(func(context.Context, HookWarning) {})
	SetHookWarningSink(nil)
	t.Cleanup(func() { SetHookWarningSink(nil) })

	// ctx 为 nil 同样安全：触发点允许 nil controls/ctx（shouldRunAdapterHook 的
	// 既有语义），警告出口也必须允许。
	emitHookWarning(nil, HookWarning{Hook: backendhooks.HookOnStream, Err: errors.New("boom")})

	text := logged.String()
	if !strings.Contains(text, string(backendhooks.HookOnStream)) || !strings.Contains(text, "boom") {
		t.Fatalf("default log = %q, want the hook type and the error", text)
	}
}

// TestHookWarningSinkIsConcurrencySafe 20 个 goroutine 同时触发失败的
// HookPostResponse：sink 恰好收到 20 条警告。生产代码的 sink 读写用 RWMutex 保护，
// 所以这里只做计数断言（本机 -race 不可用，见回执）。
func TestHookWarningSinkIsConcurrencySafe(t *testing.T) {
	mgr := hookTestEnv(t)
	registerFailingHook(t, mgr, backendhooks.HookPostResponse, errors.New("concurrent observer exploded"))

	var mu sync.Mutex
	count := 0
	SetHookWarningSink(func(context.Context, HookWarning) {
		mu.Lock()
		count++
		mu.Unlock()
	})
	t.Cleanup(func() { SetHookWarningSink(nil) })

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			notifyAdapterPostResponse(context.Background(), nil, &AIResponse{Content: "zz-concurrent-content-mark-zz"})
		}()
	}
	wg.Wait()

	mu.Lock()
	got := count
	mu.Unlock()
	if got != goroutines {
		t.Fatalf("sink received %d warnings across %d goroutines, want exactly %d", got, goroutines, goroutines)
	}

	// 卸载 sink 之后再触发一次：回到默认日志，不会 panic，也不会再送给旧 sink。
	SetHookWarningSink(nil)
	notifyAdapterPostResponse(context.Background(), nil, &AIResponse{Content: "zz-after-uninstall-mark-zz"})
	mu.Lock()
	defer mu.Unlock()
	if count != goroutines {
		t.Fatalf("the uninstalled sink received %d warnings in total, want %d", count, goroutines)
	}
}

// TestBeforeSendHookFailureStillRejects 证明第 2 组没有放宽 reject 类触发点：
// HookBeforeSend 失败时模型请求一次都不发出，Send/Stream 都返回错误。
func TestBeforeSendHookFailureStillRejects(t *testing.T) {
	mgr := hookTestEnv(t)
	beforeErr := errors.New("before-send hook says no")
	registerFailingHook(t, mgr, backendhooks.HookBeforeSend, beforeErr)

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"test-model","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	t.Cleanup(server.Close)

	adapter := NewClaudeAdapter(&AdapterConfig{APIKey: "test-key", BaseURL: server.URL, Model: "test-model", MaxRetries: 0})
	if _, err := adapter.Send(context.Background(), "hello", nil); err == nil {
		t.Fatal("Send succeeded although the before-send hook failed: reject must still block the model call")
	} else if !errors.Is(err, beforeErr) {
		t.Fatalf("Send error = %v, want the before-send hook failure", err)
	}
	if _, err := adapter.Stream(context.Background(), "hello", nil); err == nil {
		t.Fatal("Stream succeeded although the before-send hook failed")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("the model request was issued %d times, want 0: a before-send rejection must not reach the provider", got)
	}
}

// joinDeltas 拼接内容帧的 delta。
func joinDeltas(chunks []StreamChunk) string {
	var b strings.Builder
	for _, chunk := range chunks {
		b.WriteString(chunk.Delta)
	}
	return b.String()
}
