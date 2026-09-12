package summarize

import (
	"fmt"
	"math"
)

// T13.04.a 冻结的摘要参数契约（§31.3）。
//
// 参数默认值与合法范围：
//
//	参数                    省略默认                 合法范围        显式 0
//	compression_target     0.6                     [0,1]          合法（零压缩目标）
//	compression_ratio      0.6                     [0,1]          合法（零压缩目标）
//	preserve_recent_pct    0.2                     [0,1]          合法（零保留比例）
//	preserve_recent        floor(消息数×0.2)       非负整数        合法（不保留）
//	target_tokens          省略（由比例推导目标）    正整数          非法（必须为正）
//
// 兼容说明：历史上 service 把"显式 0"当作省略并回填默认值；本契约区分二者——
// 省略走默认值，显式 0 按上表语义生效。JSON omitempty 客户端的 0 值字段本就按省略
// 处理，不受影响。service 内不得再把所有 0 改回默认值（接线属 T13.04.b/c）。
const (
	// DefaultCompressionTarget compression_target 省略时的默认值。
	DefaultCompressionTarget = 0.6
	// DefaultCompressionRatio compression_ratio 省略时的默认值。
	DefaultCompressionRatio = 0.6
	// DefaultPreserveRecentPct preserve_recent_pct 省略时的默认值。
	DefaultPreserveRecentPct = 0.2
	// DefaultPreserveRecentFrac preserve_recent 省略时按消息数折算的比例（向下取整）。
	DefaultPreserveRecentFrac = 0.2
	// SummaryMarker 本地 extractive 摘要的标记后缀；输出计量必须包含它。
	SummaryMarker = " [summarized]"
)

// ValidationError 参数校验错误（typed）。HTTP 映射为 400（T13.04.c 接线）。
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid parameter %q: %s", e.Field, e.Message)
}

// BudgetUnsatisfiableError 保留区本身已超过目标预算（typed）。
// HTTP 映射为 422 budget_unsatisfiable（T13.04.c 接线）；不得截坏保留区。
type BudgetUnsatisfiableError struct {
	TargetTokens    int `json:"target_tokens"`
	PreservedTokens int `json:"preserved_tokens"`
}

func (e *BudgetUnsatisfiableError) Error() string {
	return fmt.Sprintf("budget_unsatisfiable: preserved region needs %d tokens, target budget is %d", e.PreservedTokens, e.TargetTokens)
}

// CompressParams 保留字段出现性的文本压缩入参：nil 指针表示字段省略，
// 非 nil 的 0 值表示显式 0（§31.3 要求区分二者）。
type CompressParams struct {
	CompressionRatio  *float64
	PreserveRecentPct *float64
	TargetTokens      *int
}

// ResolvedCompressParams 校验并补齐默认值后的文本压缩参数。
type ResolvedCompressParams struct {
	CompressionRatio  float64
	PreserveRecentPct float64
	// TargetTokens 显式目标预算；HasTargetTokens=false 时无意义（由比例推导）。
	TargetTokens    int
	HasTargetTokens bool
}

// ResolveCompressParams 校验并解析文本压缩参数，非法输入返回 *ValidationError。
func ResolveCompressParams(p CompressParams) (ResolvedCompressParams, error) {
	ratio, err := resolveRatio("compression_ratio", p.CompressionRatio, DefaultCompressionRatio)
	if err != nil {
		return ResolvedCompressParams{}, err
	}
	preservePct, err := resolveRatio("preserve_recent_pct", p.PreserveRecentPct, DefaultPreserveRecentPct)
	if err != nil {
		return ResolvedCompressParams{}, err
	}

	resolved := ResolvedCompressParams{CompressionRatio: ratio, PreserveRecentPct: preservePct}
	if p.TargetTokens != nil {
		if *p.TargetTokens <= 0 {
			return ResolvedCompressParams{}, &ValidationError{Field: "target_tokens", Message: "must be a positive integer when present"}
		}
		resolved.TargetTokens = *p.TargetTokens
		resolved.HasTargetTokens = true
	}
	return resolved, nil
}

// SummarizeParams 保留字段出现性的会话摘要入参。
type SummarizeParams struct {
	// MessageCount 请求中的消息总数（preserve_recent 默认值与上限的依据）。
	MessageCount      int
	CompressionTarget *float64
	PreserveRecent    *int
}

// ResolvedSummarizeParams 校验并补齐默认值后的会话摘要参数。
type ResolvedSummarizeParams struct {
	CompressionTarget float64
	// PreserveRecent 原样保留的最近消息条数；省略时 floor(消息数×0.2)，
	// 大于消息数按保留全部处理。
	PreserveRecent int
}

// ResolveSummarizeParams 校验并解析会话摘要参数，非法输入返回 *ValidationError。
func ResolveSummarizeParams(p SummarizeParams) (ResolvedSummarizeParams, error) {
	target, err := resolveRatio("compression_target", p.CompressionTarget, DefaultCompressionTarget)
	if err != nil {
		return ResolvedSummarizeParams{}, err
	}

	preserveRecent := 0
	if p.PreserveRecent != nil {
		if *p.PreserveRecent < 0 {
			return ResolvedSummarizeParams{}, &ValidationError{Field: "preserve_recent", Message: "must be a non-negative integer"}
		}
		preserveRecent = *p.PreserveRecent
	} else {
		preserveRecent = int(math.Floor(float64(p.MessageCount) * DefaultPreserveRecentFrac))
	}
	if preserveRecent > p.MessageCount {
		preserveRecent = p.MessageCount
	}

	return ResolvedSummarizeParams{CompressionTarget: target, PreserveRecent: preserveRecent}, nil
}

// resolveRatio 校验 [0,1] 比例参数：nil 取默认值，显式 0 合法，NaN/越界返回 typed error。
func resolveRatio(field string, value *float64, fallback float64) (float64, error) {
	if value == nil {
		return fallback, nil
	}
	v := *value
	if math.IsNaN(v) {
		return 0, &ValidationError{Field: field, Message: "must be a number in [0,1], got NaN"}
	}
	if v < 0 || v > 1 {
		return 0, &ValidationError{Field: field, Message: fmt.Sprintf("must be in [0,1], got %v", v)}
	}
	return v, nil
}

// CheckOutputBudget 校验保留区在显式目标预算内可行；保留区本身超预算返回
// *BudgetUnsatisfiableError（调用方不得截坏保留区）。无显式预算时恒可行。
func CheckOutputBudget(targetTokens int, hasTarget bool, preservedTokens int) error {
	if !hasTarget {
		return nil
	}
	if preservedTokens > targetTokens {
		return &BudgetUnsatisfiableError{TargetTokens: targetTokens, PreservedTokens: preservedTokens}
	}
	return nil
}
