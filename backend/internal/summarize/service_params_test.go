package summarize

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件是 T13.04.b 的摘要参数兑现测试矩阵（§28 T13.04.b、§31.2 E-06/E-07）。
// 全部本地纯内存调用，无模型依赖；默认模式为 local_extractive，token 为估算值。

func sampleConversation(n int) []Message {
	messages := make([]Message, 0, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages = append(messages, Message{
			Role:      role,
			Content:   fmt.Sprintf("We need to implement and fix module number %d with proper handling.", i),
			Timestamp: time.Now(),
		})
	}
	return messages
}

// TestSummaryPreserveRecentChangesOutput E-06：preserve_recent 对消息生效——
// 最近 N 条原样保留进 preserved_messages，summary_text 只总结可压缩区，二者随参数变化。
func TestSummaryPreserveRecentChangesOutput(t *testing.T) {
	svc := NewSummarizerService()
	messages := sampleConversation(6)

	def, err := svc.SummarizeConversation(&SummarizeRequest{Messages: messages})
	require.NoError(t, err)
	// 省略默认 floor(6×0.2)=1：最近 1 条原样保留。
	require.Len(t, def.PreservedMessages, 1)
	assert.Equal(t, messages[5].Content, def.PreservedMessages[0].Content)

	keep3, err := svc.SummarizeConversation(&SummarizeRequest{Messages: messages, PreserveRecent: i32(3)})
	require.NoError(t, err)
	require.Len(t, keep3.PreservedMessages, 3)
	for i := 0; i < 3; i++ {
		assert.Equal(t, messages[3+i].Content, keep3.PreservedMessages[i].Content)
	}

	// 可压缩区不同 -> 摘要文本不同；保留条数变化有可验证输出差异。
	assert.NotEqual(t, def.SummaryText, keep3.SummaryText)

	// 显式 0 = 不保留（区别于省略默认）。
	none, err := svc.SummarizeConversation(&SummarizeRequest{Messages: messages, PreserveRecent: i32(0)})
	require.NoError(t, err)
	assert.Empty(t, none.PreservedMessages)

	// 大于消息数按保留全部处理：可压缩区为空，不产生摘要正文。
	all, err := svc.SummarizeConversation(&SummarizeRequest{Messages: messages, PreserveRecent: i32(100)})
	require.NoError(t, err)
	require.Len(t, all.PreservedMessages, 6)
	assert.Equal(t, "", all.SummaryText)
}

// TestSummaryCompressionTargetChangesOutput E-06：compression_target 驱动摘要收录预算。
func TestSummaryCompressionTargetChangesOutput(t *testing.T) {
	svc := NewSummarizerService()
	messages := sampleConversation(10)

	full, err := svc.SummarizeConversation(&SummarizeRequest{Messages: messages, CompressionTarget: f64(0)})
	require.NoError(t, err)
	tight, err := svc.SummarizeConversation(&SummarizeRequest{Messages: messages, CompressionTarget: f64(0.9)})
	require.NoError(t, err)

	// 显式 0 = 零压缩目标：收录全部关键句；target=0.9 预算极小：明显更短。
	assert.Greater(t, len(full.SummaryText), len(tight.SummaryText))
	// CompressionRatio 是实测值，不回填请求目标。
	assert.Equal(t, MeasuredCompressionRatio(full.OriginalTokens, full.CompressedTokens), full.CompressionRatio)
	assert.NotEqual(t, 0.9, tight.CompressionRatio)
}

// TestSummaryMeasuredStatsCoverAllOutput E-06：compressed_tokens 覆盖完整返回语义内容
// （摘要正文+SummaryMarker+保留区），与返回字段逐项对账。
func TestSummaryMeasuredStatsCoverAllOutput(t *testing.T) {
	svc := NewSummarizerService()
	messages := sampleConversation(6)

	summary, err := svc.SummarizeConversation(&SummarizeRequest{Messages: messages, PreserveRecent: i32(2)})
	require.NoError(t, err)

	// SummaryText = 摘要正文 + 标记；完整返回 = SummaryText + 保留消息正文。
	expected := svc.CalculateTokens(summary.SummaryText + joinMessageContents(summary.PreservedMessages))
	assert.Equal(t, expected, summary.CompressedTokens)
	assert.Equal(t, svc.countMessageContents(messages), summary.OriginalTokens)
	assert.Equal(t, MeasuredCompressionRatio(summary.OriginalTokens, summary.CompressedTokens), summary.CompressionRatio)
	assert.Equal(t, ModeLocalExtractive, summary.Mode)
	assert.Equal(t, TokenCountQualityEstimated, summary.TokenCountQuality)
	assert.True(t, strings.HasSuffix(summary.SummaryText, SummaryMarker))
}

// TestSummaryInvalidParamsTypedError E-07：非法参数返回 typed *ValidationError（400 映射归 c 步）。
func TestSummaryInvalidParamsTypedError(t *testing.T) {
	svc := NewSummarizerService()
	messages := sampleConversation(4)

	cases := []struct {
		name  string
		req   *SummarizeRequest
		field string
	}{
		{"target above one", &SummarizeRequest{Messages: messages, CompressionTarget: f64(1.2)}, "compression_target"},
		{"target negative", &SummarizeRequest{Messages: messages, CompressionTarget: f64(-0.1)}, "compression_target"},
		{"target NaN", &SummarizeRequest{Messages: messages, CompressionTarget: f64(math.NaN())}, "compression_target"},
		{"preserve negative", &SummarizeRequest{Messages: messages, PreserveRecent: i32(-1)}, "preserve_recent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.SummarizeConversation(tc.req)
			require.Error(t, err)
			var verr *ValidationError
			require.True(t, errors.As(err, &verr), "expected *ValidationError, got %T", err)
			assert.Equal(t, tc.field, verr.Field)
		})
	}
}

// TestSummaryChineseRemainsValidUTF8 E-07：中文消息在保留/摘要/计量全链路合法。
func TestSummaryChineseRemainsValidUTF8(t *testing.T) {
	svc := NewSummarizerService()
	messages := []Message{
		{Role: "user", Content: "我们决定实现用户认证模块，包含令牌刷新逻辑。", Timestamp: time.Now()},
		{Role: "assistant", Content: "已实现认证模块，并修复了刷新令牌的缺陷。", Timestamp: time.Now()},
		{Role: "user", Content: "还需要增加限流与审计日志。", Timestamp: time.Now()},
		{Role: "assistant", Content: "已增加限流，审计日志接入现有管道。", Timestamp: time.Now()},
	}

	summary, err := svc.SummarizeConversation(&SummarizeRequest{Messages: messages, PreserveRecent: i32(2)})
	require.NoError(t, err)
	assert.True(t, utf8.ValidString(summary.SummaryText), "summary must be valid UTF-8")
	require.Len(t, summary.PreservedMessages, 2)
	assert.Equal(t, messages[2].Content, summary.PreservedMessages[0].Content)
	assert.Equal(t, messages[3].Content, summary.PreservedMessages[1].Content)
}

// TestCompressTargetTokensIncludesAllOutput E-06：显式 target_tokens 约束完整输出
// （摘要正文+SummaryMarker+保留区），实测 compressed_tokens 不超预算且真实计量。
func TestCompressTargetTokensIncludesAllOutput(t *testing.T) {
	svc := NewSummarizerService()
	context := strings.Repeat("word ", 400) // 2000 字节

	result, err := svc.CompressContext(&CompressRequest{
		Context:           context,
		TargetTokens:      i32(120),
		CompressionRatio:  f64(0.6),
		PreserveRecentPct: f64(0.2),
	})
	require.NoError(t, err)

	// 实测统计与返回内容逐项对账：Summary 含标记，RecentContext 为保留区。
	assert.Equal(t, svc.CalculateTokens(result.Summary+result.RecentContext), result.CompressedTokens)
	assert.LessOrEqual(t, result.CompressedTokens, 120, "target_tokens must bound the full output")
	assert.Equal(t, svc.CalculateTokens(context), result.OriginalTokens)
	assert.Equal(t, MeasuredCompressionRatio(result.OriginalTokens, result.CompressedTokens), result.CompressionRatio)
	assert.True(t, strings.HasSuffix(result.Summary, SummaryMarker))
	assert.Equal(t, ModeLocalExtractive, result.Mode)
	assert.Equal(t, TokenCountQualityEstimated, result.TokenCountQuality)

	// 目标预算可观察：更大的预算允许更长的摘要。
	roomy, err := svc.CompressContext(&CompressRequest{
		Context:           context,
		TargetTokens:      i32(220),
		CompressionRatio:  f64(0.6),
		PreserveRecentPct: f64(0.2),
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(roomy.Summary), len(result.Summary))
	assert.LessOrEqual(t, roomy.CompressedTokens, 220)
}

// TestCompressBudgetUnsatisfiable E-06：保留区+标记（最小合法表示）超预算时明确拒绝，
// 返回 typed *BudgetUnsatisfiableError，不截坏保留区（422 映射归 c 步）。
func TestCompressBudgetUnsatisfiable(t *testing.T) {
	svc := NewSummarizerService()
	context := strings.Repeat("word ", 400) // 保留区 400 字节 ≈ 100 token，远超预算 5

	_, err := svc.CompressContext(&CompressRequest{
		Context:           context,
		TargetTokens:      i32(5),
		PreserveRecentPct: f64(0.2),
	})
	require.Error(t, err)
	var berr *BudgetUnsatisfiableError
	require.True(t, errors.As(err, &berr), "expected *BudgetUnsatisfiableError, got %T", err)
	assert.Equal(t, 5, berr.TargetTokens)
	assert.Greater(t, berr.PreservedTokens, 5)
}

// TestCompressInvalidParamsTypedError E-07：比例越界/NaN/非法预算返回 typed *ValidationError，
// 不再 panic 或由 HTTP recovery 兜底成 500。
func TestCompressInvalidParamsTypedError(t *testing.T) {
	svc := NewSummarizerService()

	cases := []struct {
		name  string
		req   *CompressRequest
		field string
	}{
		{"ratio above one", &CompressRequest{Context: "text", CompressionRatio: f64(1.5)}, "compression_ratio"},
		{"ratio negative", &CompressRequest{Context: "text", CompressionRatio: f64(-0.1)}, "compression_ratio"},
		{"ratio NaN", &CompressRequest{Context: "text", CompressionRatio: f64(math.NaN())}, "compression_ratio"},
		// E-07 触发一：preserve_recent_pct=2 曾产生负下标 panic
		{"preserve pct two", &CompressRequest{Context: "text", PreserveRecentPct: f64(2)}, "preserve_recent_pct"},
		{"preserve pct negative", &CompressRequest{Context: "text", PreserveRecentPct: f64(-0.5)}, "preserve_recent_pct"},
		{"target zero", &CompressRequest{Context: "text", TargetTokens: i32(0)}, "target_tokens"},
		{"target negative", &CompressRequest{Context: "text", TargetTokens: i32(-5)}, "target_tokens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CompressContext(tc.req)
			require.Error(t, err)
			var verr *ValidationError
			require.True(t, errors.As(err, &verr), "expected *ValidationError, got %T", err)
			assert.Equal(t, tc.field, verr.Field)
		})
	}
}

// TestCompressChineseRemainsValidUTF8 E-07 触发二：中文上下文按合法字符边界切分，
// 保留区是原文尾部原样子串，摘要与保留区均为合法 UTF-8（不经 JSON 替换字符冒充成功）。
func TestCompressChineseRemainsValidUTF8(t *testing.T) {
	svc := NewSummarizerService()
	context := strings.Repeat("你好世界，我们做出了关键决定。", 20)

	result, err := svc.CompressContext(&CompressRequest{
		Context:           context,
		CompressionRatio:  f64(0.5),
		PreserveRecentPct: f64(0.3),
	})
	require.NoError(t, err)
	assert.True(t, utf8.ValidString(result.Summary), "summary must be valid UTF-8")
	assert.True(t, utf8.ValidString(result.RecentContext), "recent context must be valid UTF-8")
	assert.True(t, strings.HasSuffix(context, result.RecentContext), "recent context must be a verbatim suffix")
	assert.NotEmpty(t, result.RecentContext)
}

// TestCompressPreservePctHonored E-06：preserve_recent_pct 对文本生效。
func TestCompressPreservePctHonored(t *testing.T) {
	svc := NewSummarizerService()
	context := strings.Repeat("abcdefghij", 100) // 1000 字节，无句点

	// 显式 0 = 零保留比例：整个上下文进入可压缩区，保留区为空。
	zero, err := svc.CompressContext(&CompressRequest{Context: context, PreserveRecentPct: f64(0)})
	require.NoError(t, err)
	assert.Equal(t, "", zero.RecentContext)

	half, err := svc.CompressContext(&CompressRequest{Context: context, PreserveRecentPct: f64(0.5)})
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(context, half.RecentContext))
	assert.InDelta(t, 500, len(half.RecentContext), 1, "preserve pct applies to the text tail")

	// 显式 ratio=0 = 零压缩目标：可压缩区原样进入摘要。
	full, err := svc.CompressContext(&CompressRequest{Context: context, CompressionRatio: f64(0), PreserveRecentPct: f64(0.2)})
	require.NoError(t, err)
	compressible, recent := SplitByPreservePct(context, 0.2)
	assert.Equal(t, recent, full.RecentContext)
	assert.Equal(t, compressible+SummaryMarker, full.Summary)
}
