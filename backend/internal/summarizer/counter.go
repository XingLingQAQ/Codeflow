package summarizer

import (
	"unicode"

	"github.com/codeflow/backend/internal/adapters"
)

// TokenCounter Token计数器实现
type TokenCounter struct {
	charsPerTokenEN    float64
	charsPerTokenZH    float64
	overheadPerMessage int
}

// NewTokenCounter 创建Token计数器
func NewTokenCounter(config *struct {
	CharsPerTokenEN    float64
	CharsPerTokenZH    float64
	OverheadPerMessage int
}) *TokenCounter {
	tc := &TokenCounter{
		charsPerTokenEN:    TokenEstimation.CharsPerTokenEN,
		charsPerTokenZH:    TokenEstimation.CharsPerTokenZH,
		overheadPerMessage: TokenEstimation.OverheadPerMessage,
	}

	if config != nil {
		if config.CharsPerTokenEN > 0 {
			tc.charsPerTokenEN = config.CharsPerTokenEN
		}
		if config.CharsPerTokenZH > 0 {
			tc.charsPerTokenZH = config.CharsPerTokenZH
		}
		if config.OverheadPerMessage > 0 {
			tc.overheadPerMessage = config.OverheadPerMessage
		}
	}

	return tc
}

// Count 计算文本的Token数
func (tc *TokenCounter) Count(text string) int {
	return tc.EstimateTokens(text)
}

// CountMessages 计算消息列表的Token数
func (tc *TokenCounter) CountMessages(messages []adapters.Message) TokenCount {
	byMessage := make([]int, 0, len(messages))
	byRole := map[string]int{
		"user":      0,
		"assistant": 0,
		"system":    0,
	}
	total := 0

	for _, msg := range messages {
		tokens := tc.Count(msg.Content) + tc.overheadPerMessage
		byMessage = append(byMessage, tokens)
		byRole[string(msg.Role)] += tokens
		total += tokens
	}

	return TokenCount{
		Total:     total,
		ByRole:    byRole,
		ByMessage: byMessage,
	}
}

// EstimateTokens 估算文本的Token数
func (tc *TokenCounter) EstimateTokens(text string) int {
	if text == "" {
		return 0
	}

	var enChars, zhChars int

	for _, r := range text {
		if isChinese(r) {
			zhChars++
		} else {
			enChars++
		}
	}

	enTokens := int(float64(enChars)/tc.charsPerTokenEN + 0.5)
	zhTokens := int(float64(zhChars)/tc.charsPerTokenZH + 0.5)

	return enTokens + zhTokens
}

// isChinese 判断是否为中文字符
func isChinese(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		(r >= 0x3400 && r <= 0x4DBF) ||   // CJK Extension A
		(r >= 0x20000 && r <= 0x2A6DF) || // CJK Extension B
		(r >= 0x2A700 && r <= 0x2B73F) || // CJK Extension C
		(r >= 0x2B740 && r <= 0x2B81F) || // CJK Extension D
		(r >= 0x2B820 && r <= 0x2CEAF) || // CJK Extension E
		(r >= 0xF900 && r <= 0xFAFF) ||   // CJK Compatibility Ideographs
		(r >= 0x2F800 && r <= 0x2FA1F)    // CJK Compatibility Ideographs Supplement
}
