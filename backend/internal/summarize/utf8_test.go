package summarize

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T13.04.a：UTF-8 切分与输出计量纯函数测试矩阵（E-07）。
// 覆盖空输入 / 边界 / 超长 / 中文混合 / 多字节符号。

func TestSplitByPreservePctEmpty(t *testing.T) {
	head, tail := SplitByPreservePct("", 0.2)
	assert.Equal(t, "", head)
	assert.Equal(t, "", tail)
}

func TestSplitByPreservePctChinese(t *testing.T) {
	// E-07 触发二："你好" 默认比例曾把切分点落到 UTF-8 字符内部。
	head, tail := SplitByPreservePct("你好", DefaultPreserveRecentPct)
	assert.Equal(t, "你", head) // int(6×0.8)=4 → 回退到字节 3
	assert.Equal(t, "好", tail)
	assert.True(t, utf8.ValidString(head))
	assert.True(t, utf8.ValidString(tail))
	assert.Equal(t, "你好", head+tail)
}

func TestSplitByPreservePctRatioExtremes(t *testing.T) {
	head, tail := SplitByPreservePct("hello 你好", 0)
	assert.Equal(t, "hello 你好", head)
	assert.Equal(t, "", tail)

	head, tail = SplitByPreservePct("hello 你好", 1)
	assert.Equal(t, "", head)
	assert.Equal(t, "hello 你好", tail)
}

func TestSplitByPreservePctLongMixed(t *testing.T) {
	text := strings.Repeat("ab你", 5000) + strings.Repeat("🙂cd", 1000) // 超长中英文+emoji 混合
	head, tail := SplitByPreservePct(text, 0.2)
	require.True(t, utf8.ValidString(head), "compressible part must stay valid UTF-8")
	require.True(t, utf8.ValidString(tail), "preserved part must stay valid UTF-8")
	assert.Equal(t, text, head+tail, "split must not lose or gain bytes")
	// 保留比例允许因 rune 回退略小，但保留区不得为空
	assert.NotEmpty(t, tail)
}

func TestSplitAtRuneBoundary(t *testing.T) {
	s := "a你b" // 字节布局: a(0) 你(1,2,3) b(4)
	cases := []struct {
		name     string
		index    int
		wantHead string
		wantTail string
	}{
		{"negative clamps to start", -3, "", "a你b"},
		{"zero", 0, "", "a你b"},
		{"inside rune backs off", 2, "a", "你b"},
		{"inside rune last byte", 3, "a", "你b"},
		{"exact rune boundary", 4, "a你", "b"},
		{"at end", 5, "a你b", ""},
		{"beyond end clamps", 100, "a你b", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			head, tail := SplitAtRuneBoundary(s, tc.index)
			assert.Equal(t, tc.wantHead, head)
			assert.Equal(t, tc.wantTail, tail)
		})
	}
}

func TestTruncateUTF8(t *testing.T) {
	assert.Equal(t, "", TruncateUTF8("你好", 0))
	assert.Equal(t, "", TruncateUTF8("你好", -1))
	assert.Equal(t, "你好", TruncateUTF8("你好", 100))
	assert.Equal(t, "你", TruncateUTF8("你好", 4)) // 4 落在"好"内部 → 回退
	assert.Equal(t, "你", TruncateUTF8("你好", 3)) // 恰好边界
	assert.Equal(t, "", TruncateUTF8("你好", 2))  // 落在"你"内部 → 回退到空
	out := TruncateUTF8(strings.Repeat("你", 100), 7)
	assert.Equal(t, "你你", out)
	assert.True(t, utf8.ValidString(out))
}

func TestMeasureCompressedOutputIncludesMarkerAndRecent(t *testing.T) {
	counter := NewTokenCounter(nil)
	summary, recent := "关键决策记录", "最近原文保留"
	total := MeasureCompressedOutput(counter, summary, SummaryMarker, recent)

	assert.Equal(t, counter.Count(summary+SummaryMarker+recent), total)
	assert.Greater(t, total, counter.Count(summary), "marker 与保留区必须计入")
	assert.Greater(t, total, counter.Count(summary+SummaryMarker), "保留区必须计入")
}

func TestMeasureCompressedOutputNilCounterAndEmpty(t *testing.T) {
	assert.Equal(t, 0, MeasureCompressedOutput(nil, "", "", ""))
	assert.Greater(t, MeasureCompressedOutput(nil, "a", SummaryMarker, "b"), 0)
}

func TestMeasuredCompressionRatio(t *testing.T) {
	assert.InDelta(t, 0.6, MeasuredCompressionRatio(100, 40), 1e-9)
	assert.Equal(t, 0.0, MeasuredCompressionRatio(0, 0))  // original=0 定义为 0
	assert.Equal(t, 0.0, MeasuredCompressionRatio(0, 10)) // 不回填请求目标
	assert.Equal(t, 1.0, MeasuredCompressionRatio(100, 0))
	assert.Negative(t, MeasuredCompressionRatio(100, 150)) // 实测膨胀如实为负
}
