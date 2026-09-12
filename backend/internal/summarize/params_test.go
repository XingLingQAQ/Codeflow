package summarize

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T13.04.a：§31.3 参数契约的验证纯函数测试矩阵。

func f64(v float64) *float64 { return &v }
func i32(v int) *int         { return &v }

func TestResolveCompressParamsDefaults(t *testing.T) {
	r, err := ResolveCompressParams(CompressParams{})
	require.NoError(t, err)
	assert.Equal(t, DefaultCompressionRatio, r.CompressionRatio)
	assert.Equal(t, DefaultPreserveRecentPct, r.PreserveRecentPct)
	assert.False(t, r.HasTargetTokens)
}

func TestResolveCompressParamsExplicitZeroIsLegal(t *testing.T) {
	// §31.3：显式 0 合法，分别表示零压缩目标与零保留比例；不得再被改回默认值。
	r, err := ResolveCompressParams(CompressParams{
		CompressionRatio:  f64(0),
		PreserveRecentPct: f64(0),
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, r.CompressionRatio)
	assert.Equal(t, 0.0, r.PreserveRecentPct)
}

func TestResolveCompressParamsBoundaries(t *testing.T) {
	r, err := ResolveCompressParams(CompressParams{
		CompressionRatio:  f64(1),
		PreserveRecentPct: f64(1),
		TargetTokens:      i32(1),
	})
	require.NoError(t, err)
	assert.Equal(t, 1.0, r.CompressionRatio)
	assert.Equal(t, 1.0, r.PreserveRecentPct)
	assert.True(t, r.HasTargetTokens)
	assert.Equal(t, 1, r.TargetTokens)
}

func TestResolveCompressParamsInvalidRatios(t *testing.T) {
	cases := []struct {
		name   string
		params CompressParams
		field  string
	}{
		{"ratio negative", CompressParams{CompressionRatio: f64(-0.1)}, "compression_ratio"},
		{"ratio above one", CompressParams{CompressionRatio: f64(1.5)}, "compression_ratio"},
		{"ratio NaN", CompressParams{CompressionRatio: f64(math.NaN())}, "compression_ratio"},
		{"preserve pct negative", CompressParams{PreserveRecentPct: f64(-0.5)}, "preserve_recent_pct"},
		// E-07 触发一：preserve_recent_pct=2 曾产生负下标 panic
		{"preserve pct two", CompressParams{PreserveRecentPct: f64(2)}, "preserve_recent_pct"},
		{"preserve pct NaN", CompressParams{PreserveRecentPct: f64(math.NaN())}, "preserve_recent_pct"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveCompressParams(tc.params)
			require.Error(t, err)
			var verr *ValidationError
			require.True(t, errors.As(err, &verr), "expected typed *ValidationError, got %T", err)
			assert.Equal(t, tc.field, verr.Field)
		})
	}
}

func TestResolveCompressParamsInvalidTargetTokens(t *testing.T) {
	for _, v := range []int{0, -5} {
		_, err := ResolveCompressParams(CompressParams{TargetTokens: i32(v)})
		require.Error(t, err)
		var verr *ValidationError
		require.True(t, errors.As(err, &verr), "expected typed *ValidationError, got %T", err)
		assert.Equal(t, "target_tokens", verr.Field)
	}
}

func TestResolveSummarizeParamsDefaults(t *testing.T) {
	r, err := ResolveSummarizeParams(SummarizeParams{MessageCount: 10})
	require.NoError(t, err)
	assert.Equal(t, DefaultCompressionTarget, r.CompressionTarget)
	assert.Equal(t, 2, r.PreserveRecent) // floor(10×0.2)

	r, err = ResolveSummarizeParams(SummarizeParams{MessageCount: 3})
	require.NoError(t, err)
	assert.Equal(t, 0, r.PreserveRecent) // floor(3×0.2)=0

	r, err = ResolveSummarizeParams(SummarizeParams{MessageCount: 0})
	require.NoError(t, err)
	assert.Equal(t, 0, r.PreserveRecent)
}

func TestResolveSummarizeParamsPreserveRecent(t *testing.T) {
	// 显式 0 合法（不保留）
	r, err := ResolveSummarizeParams(SummarizeParams{MessageCount: 10, PreserveRecent: i32(0)})
	require.NoError(t, err)
	assert.Equal(t, 0, r.PreserveRecent)

	// 大于消息数按保留全部处理
	r, err = ResolveSummarizeParams(SummarizeParams{MessageCount: 4, PreserveRecent: i32(100)})
	require.NoError(t, err)
	assert.Equal(t, 4, r.PreserveRecent)

	// 负数非法
	_, err = ResolveSummarizeParams(SummarizeParams{MessageCount: 4, PreserveRecent: i32(-1)})
	require.Error(t, err)
	var verr *ValidationError
	require.True(t, errors.As(err, &verr))
	assert.Equal(t, "preserve_recent", verr.Field)
}

func TestResolveSummarizeParamsCompressionTarget(t *testing.T) {
	r, err := ResolveSummarizeParams(SummarizeParams{MessageCount: 5, CompressionTarget: f64(0)})
	require.NoError(t, err)
	assert.Equal(t, 0.0, r.CompressionTarget)

	_, err = ResolveSummarizeParams(SummarizeParams{MessageCount: 5, CompressionTarget: f64(1.2)})
	require.Error(t, err)
	var verr *ValidationError
	require.True(t, errors.As(err, &verr))
	assert.Equal(t, "compression_target", verr.Field)
}

func TestCheckOutputBudget(t *testing.T) {
	assert.NoError(t, CheckOutputBudget(0, false, 10)) // 无显式预算恒可行
	assert.NoError(t, CheckOutputBudget(100, true, 60))
	assert.NoError(t, CheckOutputBudget(100, true, 100))

	err := CheckOutputBudget(50, true, 60)
	require.Error(t, err)
	var berr *BudgetUnsatisfiableError
	require.True(t, errors.As(err, &berr), "expected typed *BudgetUnsatisfiableError, got %T", err)
	assert.Equal(t, 50, berr.TargetTokens)
	assert.Equal(t, 60, berr.PreservedTokens)
}
