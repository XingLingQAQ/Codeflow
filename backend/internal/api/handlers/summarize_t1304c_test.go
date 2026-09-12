// Package handlers - T13.04.c 摘要 HTTP 分层错误映射与 §28 点名测试（§31.2 E-06/E-07）。
// 全部经 gin 注册的真实 handler（SummarizeConversation/CompressContext）走 HTTP 断言，
// 而非只调用 service helper；默认模式 local_extractive，不发起任何模型调用。
package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/codeflow/backend/internal/summarize"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptrInt(v int) *int { return &v }

// t1304cRouter 注册与生产一致的摘要 handler 入口。
func t1304cRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/summarize/conversation", SummarizeConversation)
	router.POST("/api/v1/summarize/context", CompressContext)
	return router
}

func t1304cPost(t *testing.T, router *gin.Engine, path string, payload interface{}) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req, err := http.NewRequest("POST", path, bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func t1304cMessages(n int) []summarize.Message {
	messages := make([]summarize.Message, 0, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages = append(messages, summarize.Message{
			Role:      role,
			Content:   fmt.Sprintf("We need to implement and fix module number %d with proper handling.", i),
			Timestamp: time.Now(),
		})
	}
	return messages
}

// TestSummaryPreserveRecentChangesOutput §28 点名（E-06）：经 HTTP handler，
// 改 preserve_recent 有可验证输出变化——最近 N 条原样进入 preserved_messages，
// summary_text 只总结可压缩区。
func TestSummaryPreserveRecentChangesOutput(t *testing.T) {
	summarize.SetSummarizer(summarize.NewSummarizerService())
	router := t1304cRouter()
	messages := t1304cMessages(6)

	// 省略默认 floor(6×0.2)=1。
	wDef := t1304cPost(t, router, "/api/v1/summarize/conversation", summarize.SummarizeRequest{Messages: messages})
	require.Equal(t, http.StatusOK, wDef.Code)
	var def summarize.ConversationSummary
	require.NoError(t, json.Unmarshal(wDef.Body.Bytes(), &def))
	require.Len(t, def.PreservedMessages, 1)
	assert.Equal(t, messages[5].Content, def.PreservedMessages[0].Content)

	// 显式 preserve_recent=3。
	wKeep3 := t1304cPost(t, router, "/api/v1/summarize/conversation", summarize.SummarizeRequest{Messages: messages, PreserveRecent: ptrInt(3)})
	require.Equal(t, http.StatusOK, wKeep3.Code)
	var keep3 summarize.ConversationSummary
	require.NoError(t, json.Unmarshal(wKeep3.Body.Bytes(), &keep3))
	require.Len(t, keep3.PreservedMessages, 3)
	for i := 0; i < 3; i++ {
		assert.Equal(t, messages[3+i].Content, keep3.PreservedMessages[i].Content)
	}

	// 保留条数变化有可验证输出差异（可压缩区不同 -> 摘要文本不同）。
	assert.NotEqual(t, def.SummaryText, keep3.SummaryText)

	// 显式 0 = 不保留（区别于省略默认）。
	wNone := t1304cPost(t, router, "/api/v1/summarize/conversation", summarize.SummarizeRequest{Messages: messages, PreserveRecent: ptrInt(0)})
	require.Equal(t, http.StatusOK, wNone.Code)
	var none summarize.ConversationSummary
	require.NoError(t, json.Unmarshal(wNone.Body.Bytes(), &none))
	assert.Empty(t, none.PreservedMessages)

	// 默认 local_extractive 模式与 estimated 计量质量在响应中明示。
	assert.Equal(t, summarize.ModeLocalExtractive, def.Mode)
	assert.Equal(t, summarize.TokenCountQualityEstimated, def.TokenCountQuality)
}

// TestSummaryTargetTokensIncludesAllOutput §28 点名（E-06）：经 HTTP handler，
// 显式 target_tokens 约束完整输出（摘要正文+SummaryMarker+保留区），
// 实测 compressed_tokens 不超预算且与返回内容逐项对账。
func TestSummaryTargetTokensIncludesAllOutput(t *testing.T) {
	svc := summarize.NewSummarizerService()
	summarize.SetSummarizer(svc)
	router := t1304cRouter()
	context := strings.Repeat("word ", 400) // 2000 字节

	w := t1304cPost(t, router, "/api/v1/summarize/context", summarize.CompressRequest{
		Context:           context,
		TargetTokens:      ptrInt(120),
		CompressionRatio:  f64ptr(0.6),
		PreserveRecentPct: f64ptr(0.2),
	})
	require.Equal(t, http.StatusOK, w.Code)
	var result summarize.ContextCompression
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	// 实测统计覆盖全部返回内容（Summary 含标记，RecentContext 为保留区）。
	assert.Equal(t, svc.CalculateTokens(result.Summary+result.RecentContext), result.CompressedTokens)
	assert.LessOrEqual(t, result.CompressedTokens, 120, "target_tokens must bound the full output")
	assert.True(t, strings.HasSuffix(result.Summary, summarize.SummaryMarker))
	assert.NotEmpty(t, result.RecentContext)

	// 更大预算允许更长摘要：预算变化可观察。
	wRoomy := t1304cPost(t, router, "/api/v1/summarize/context", summarize.CompressRequest{
		Context:           context,
		TargetTokens:      ptrInt(220),
		CompressionRatio:  f64ptr(0.6),
		PreserveRecentPct: f64ptr(0.2),
	})
	require.Equal(t, http.StatusOK, wRoomy.Code)
	var roomy summarize.ContextCompression
	require.NoError(t, json.Unmarshal(wRoomy.Body.Bytes(), &roomy))
	assert.GreaterOrEqual(t, len(roomy.Summary), len(result.Summary))
	assert.LessOrEqual(t, roomy.CompressedTokens, 220)
}

// TestSummaryInvalidRatioReturns400 §28 点名（E-07）：经 HTTP handler，
// 越界比例/非法预算返回 400，envelope 含参数名（field）与原因（reason）。
func TestSummaryInvalidRatioReturns400(t *testing.T) {
	summarize.SetSummarizer(summarize.NewSummarizerService())
	router := t1304cRouter()
	messages := t1304cMessages(4)

	cases := []struct {
		name    string
		path    string
		payload interface{}
		field   string
	}{
		{"target above one", "/api/v1/summarize/conversation", summarize.SummarizeRequest{Messages: messages, CompressionTarget: f64ptr(1.2)}, "compression_target"},
		{"target negative", "/api/v1/summarize/conversation", summarize.SummarizeRequest{Messages: messages, CompressionTarget: f64ptr(-0.1)}, "compression_target"},
		{"preserve negative", "/api/v1/summarize/conversation", summarize.SummarizeRequest{Messages: messages, PreserveRecent: ptrInt(-1)}, "preserve_recent"},
		{"ratio above one", "/api/v1/summarize/context", summarize.CompressRequest{Context: "text", CompressionRatio: f64ptr(1.5)}, "compression_ratio"},
		{"ratio negative", "/api/v1/summarize/context", summarize.CompressRequest{Context: "text", CompressionRatio: f64ptr(-0.1)}, "compression_ratio"},
		// E-07 触发一：preserve_recent_pct=2 曾产生负下标 panic，现在必须是 400。
		{"preserve pct two", "/api/v1/summarize/context", summarize.CompressRequest{Context: "text", PreserveRecentPct: f64ptr(2)}, "preserve_recent_pct"},
		{"target zero", "/api/v1/summarize/context", summarize.CompressRequest{Context: "text", TargetTokens: ptrInt(0)}, "target_tokens"},
		{"target negative", "/api/v1/summarize/context", summarize.CompressRequest{Context: "text", TargetTokens: ptrInt(-5)}, "target_tokens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := t1304cPost(t, router, tc.path, tc.payload)
			require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())

			var resp map[string]interface{}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, tc.field, resp["field"], "envelope must carry the offending parameter name")
			assert.NotEmpty(t, resp["reason"], "envelope must carry the reason")
			assert.NotEmpty(t, resp["error"])
		})
	}
}

// TestSummaryChineseRemainsValidUTF8 §28 点名（E-07）：经 HTTP handler，
// 中文/多字节文本全链路合法 UTF-8，保留区为原文尾部原样子串，
// 响应体不含 U+FFFD 替换字符（不靠 JSON 替换非法字符冒充成功）。
func TestSummaryChineseRemainsValidUTF8(t *testing.T) {
	summarize.SetSummarizer(summarize.NewSummarizerService())
	router := t1304cRouter()

	t.Run("conversation", func(t *testing.T) {
		messages := []summarize.Message{
			{Role: "user", Content: "我们决定实现用户认证模块，包含令牌刷新逻辑。", Timestamp: time.Now()},
			{Role: "assistant", Content: "已实现认证模块，并修复了刷新令牌的缺陷。", Timestamp: time.Now()},
			{Role: "user", Content: "还需要增加限流与审计日志。", Timestamp: time.Now()},
			{Role: "assistant", Content: "已增加限流，审计日志接入现有管道。", Timestamp: time.Now()},
		}
		w := t1304cPost(t, router, "/api/v1/summarize/conversation", summarize.SummarizeRequest{Messages: messages, PreserveRecent: ptrInt(2)})
		require.Equal(t, http.StatusOK, w.Code)

		raw := w.Body.Bytes()
		assert.True(t, utf8.Valid(raw), "raw response body must be valid UTF-8")
		assert.NotContains(t, string(raw), "\uFFFD", "no replacement char may appear in the response")

		var resp summarize.ConversationSummary
		require.NoError(t, json.Unmarshal(raw, &resp))
		assert.True(t, utf8.ValidString(resp.SummaryText))
		require.Len(t, resp.PreservedMessages, 2)
		assert.Equal(t, messages[2].Content, resp.PreservedMessages[0].Content)
		assert.Equal(t, messages[3].Content, resp.PreservedMessages[1].Content)
	})

	t.Run("context", func(t *testing.T) {
		context := strings.Repeat("你好世界，我们做出了关键决定。", 20)
		w := t1304cPost(t, router, "/api/v1/summarize/context", summarize.CompressRequest{
			Context:           context,
			CompressionRatio:  f64ptr(0.5),
			PreserveRecentPct: f64ptr(0.3),
		})
		require.Equal(t, http.StatusOK, w.Code)

		raw := w.Body.Bytes()
		assert.True(t, utf8.Valid(raw), "raw response body must be valid UTF-8")
		assert.NotContains(t, string(raw), "\uFFFD", "no replacement char may appear in the response")

		var resp summarize.ContextCompression
		require.NoError(t, json.Unmarshal(raw, &resp))
		assert.True(t, utf8.ValidString(resp.Summary))
		assert.True(t, utf8.ValidString(resp.RecentContext))
		assert.True(t, strings.HasSuffix(context, resp.RecentContext), "recent context must be a verbatim suffix")
		assert.NotEmpty(t, resp.RecentContext)
	})
}

// TestSummaryBudgetUnsatisfiableReturns422 E-06 负向：保留区+SummaryMarker（最小合法表示）
// 本身超过显式 target_tokens 时经 HTTP handler 返回 422 budget_unsatisfiable，
// envelope 含预算与保留区 token 数，不截坏保留区。
func TestSummaryBudgetUnsatisfiableReturns422(t *testing.T) {
	summarize.SetSummarizer(summarize.NewSummarizerService())
	router := t1304cRouter()
	context := strings.Repeat("word ", 400) // 保留区 400 字节，远超预算 5

	w := t1304cPost(t, router, "/api/v1/summarize/context", summarize.CompressRequest{
		Context:           context,
		TargetTokens:      ptrInt(5),
		PreserveRecentPct: f64ptr(0.2),
	})
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, "body: %s", w.Body.String())

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "budget_unsatisfiable", resp["code"])
	assert.Equal(t, float64(5), resp["target_tokens"])
	require.IsType(t, float64(0), resp["preserved_tokens"])
	assert.Greater(t, resp["preserved_tokens"].(float64), float64(5))
	assert.NotEmpty(t, resp["error"])
}

// t1304cStubSummarizer 按注入错误回复的 ISummarizer stub，验证分层映射本身。
type t1304cStubSummarizer struct {
	err error
}

func (s *t1304cStubSummarizer) SummarizeConversation(*summarize.SummarizeRequest) (*summarize.ConversationSummary, error) {
	return nil, s.err
}
func (s *t1304cStubSummarizer) CompressContext(*summarize.CompressRequest) (*summarize.ContextCompression, error) {
	return nil, s.err
}
func (s *t1304cStubSummarizer) ExtractSkeleton([]summarize.Message) (*summarize.DecisionSkeleton, error) {
	return nil, s.err
}
func (s *t1304cStubSummarizer) CalculateTokens(string) int { return 0 }

// TestSummaryErrorMappingLayered E-06/E-07 对账：同一 handler 按 typed error 分层——
// *ValidationError（含被 %w 包装）→400，*BudgetUnsatisfiableError→422，其它执行故障→500。
func TestSummaryErrorMappingLayered(t *testing.T) {
	router := t1304cRouter()
	payload := summarize.SummarizeRequest{Messages: t1304cMessages(2)}

	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"validation", &summarize.ValidationError{Field: "compression_target", Message: "must be in [0,1]"}, http.StatusBadRequest},
		{"wrapped validation", fmt.Errorf("resolve params: %w", &summarize.ValidationError{Field: "preserve_recent", Message: "must be a non-negative integer"}), http.StatusBadRequest},
		{"budget unsatisfiable", &summarize.BudgetUnsatisfiableError{TargetTokens: 5, PreservedTokens: 100}, http.StatusUnprocessableEntity},
		{"execution failure", errors.New("engine blew up"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summarize.SetSummarizer(&t1304cStubSummarizer{err: tc.err})
			t.Cleanup(func() { summarize.SetSummarizer(summarize.NewSummarizerService()) })

			w := t1304cPost(t, router, "/api/v1/summarize/conversation", payload)
			assert.Equal(t, tc.wantStatus, w.Code, "body: %s", w.Body.String())
			assert.NotEmpty(t, w.Body.String())
		})
	}
}
