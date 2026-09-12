package summarize

import (
	"unicode/utf8"
)

// 本文件是 T13.04.a 新增的文本切分/计量纯函数（E-07）。
// 所有按字节下标的切分都必须落在合法 UTF-8 字符边界上，不得在字符内部切断。

// floorRuneBoundary 把字节下标回退到不超过它的最近一个 rune 起始位置。
func floorRuneBoundary(s string, byteIndex int) int {
	if byteIndex <= 0 {
		return 0
	}
	if byteIndex >= len(s) {
		return len(s)
	}
	for byteIndex > 0 && !utf8.RuneStart(s[byteIndex]) {
		byteIndex--
	}
	return byteIndex
}

// SplitAtRuneBoundary 在不超字节下标的最近合法 UTF-8 边界处切分 s。
// byteIndex 越界时按端点钳制；返回的两段拼接恒等于 s，且各自都是合法 UTF-8。
func SplitAtRuneBoundary(s string, byteIndex int) (head, tail string) {
	cut := floorRuneBoundary(s, byteIndex)
	return s[:cut], s[cut:]
}

// SplitByPreservePct 按保留比例切分上下文：preservePct 是尾部原样保留的字节比例
// （[0,1]，调用方需先经 ResolveCompressParams 校验）。0 表示全部进入可压缩区，
// 1 表示全部保留。切分点向 rune 边界回退，多字节字符（中文/emoji 等）不会被截坏。
func SplitByPreservePct(s string, preservePct float64) (compressible, recent string) {
	if s == "" {
		return "", ""
	}
	target := int(float64(len(s)) * (1.0 - preservePct))
	return SplitAtRuneBoundary(s, target)
}

// TruncateUTF8 返回 s 的不超过 maxBytes 字节的最长合法 UTF-8 前缀。
// maxBytes <= 0 返回空串；maxBytes >= len(s) 原样返回。
func TruncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if maxBytes >= len(s) {
		return s
	}
	return s[:floorRuneBoundary(s, maxBytes)]
}

// MeasureCompressedOutput 计量完整返回语义内容的 token 数（§31.3 输出计量）：
// 摘要正文、摘要标记与保留区全部计入，按最终返回内容拼接后一次性计量。
// counter 为 nil 时使用默认 TokenCounter（估算口径与预算判定一致）。
func MeasureCompressedOutput(counter *TokenCounter, summary, marker, recent string) int {
	if counter == nil {
		counter = NewTokenCounter(nil)
	}
	return counter.Count(summary + marker + recent)
}

// MeasuredCompressionRatio 实测压缩率：1 - compressed/original（§31.3）。
// original == 0 时定义为 0；不回填请求目标值。
func MeasuredCompressionRatio(originalTokens, compressedTokens int) float64 {
	if originalTokens <= 0 {
		return 0
	}
	return 1.0 - float64(compressedTokens)/float64(originalTokens)
}
