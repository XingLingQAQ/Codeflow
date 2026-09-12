package adapters

import (
	"bufio"
	"encoding/json"
	"io"
	"strconv"
	"strings"
)

// 本文件是 T13.04.a 抽取的 provider SSE 流 parser（纯函数，无 goroutine/通道/历史写入）。
// 解析方式沿用现行实现的行级 `data: ` 提取与 JSON 解码（当前 parser），
// 不自造完整 SSE 协议（不实现 event:/id:/retry:/多行 data 等完整规范）。
// 终结帧的发送、assistant 历史追加与取消（TerminalCancelled）映射属于发送侧，
// 在 T13.04.b 接线；本文件只产出帧序列与终结信号。

// streamFrameSink 在内容帧解析出来的瞬间接收它。返回 false 表示消费者已
// 取消，parser 必须立即停止扫描。nil sink 表示只累积不投递（纯解析，
// fixture 测试用）。
type streamFrameSink func(StreamChunk) bool

// StreamParseResult 汇总一条 provider SSE 流的 parser 观测结果。
type StreamParseResult struct {
	// Chunks 按到达顺序解析出的内容帧（Done=false，仅 Delta/Index）。
	Chunks []StreamChunk
	// emit 非 nil 时逐帧实时投递：内容帧一解析出来就交给消费者，而不是
	// 等整条流读完再一次性投递。Chunks 仍然累积，终结阶段要用它拼装
	// assistant 历史。
	emit streamFrameSink
	// sinkCancelled 记录 sink 是否报告过取消（消费者已走）。
	sinkCancelled bool
	// FinishReason provider 上报的结束原因原值（stop/end_turn/STOP/...），未上报为空串。
	FinishReason string
	// Usage provider 上报的流式用量，未上报为 nil。
	Usage *Usage
	// ProviderErr provider 流内错误事件（协议级 error 帧），无则 nil。
	ProviderErr *StreamError
	// ScanErr 传输/扫描层失败：异常 EOF、单帧超过 scanner 上限等，无则 nil。
	ScanErr error
	// MalformedFrames 无法解析的 data 帧数（关键帧损坏信号）。
	MalformedFrames int
	// Completed 是否观测到协议完成标记（[DONE] / message_stop / finishReason）。
	Completed bool
}

// appendChunk 记录一个内容帧，并在配置了 sink 时立即投递它（流式）。
// 返回 false 表示消费者已取消，调用方必须停止扫描。
func (r *StreamParseResult) appendChunk(delta string) bool {
	chunk := StreamChunk{Delta: delta, Index: len(r.Chunks)}
	r.Chunks = append(r.Chunks, chunk)
	if r.emit == nil {
		return true
	}
	if !r.emit(chunk) {
		r.sinkCancelled = true
		return false
	}
	return true
}

// TerminalStatus 把 parser 观测结果映射为冻结的终结契约状态（§31.3）。
// 只有协议级可信完成才是 TerminalCompleted；provider 错误、扫描失败、
// 帧损坏、无完成标记的 EOF 一律 TerminalError。取消不由 parser 判定，
// 发送侧依据 ctx.Err() 映射 TerminalCancelled。
func (r *StreamParseResult) TerminalStatus() TerminalStatus {
	if r.ProviderErr != nil || r.ScanErr != nil || r.MalformedFrames > 0 {
		return TerminalError
	}
	if !r.Completed {
		return TerminalError
	}
	return TerminalCompleted
}

// TerminalError 返回与 TerminalStatus 对应的结构化错误；可信完成时返回 nil。
func (r *StreamParseResult) TerminalError() *StreamError {
	if r.ProviderErr != nil {
		return r.ProviderErr
	}
	if r.ScanErr != nil {
		return &StreamError{Code: "stream_scan_failed", Message: r.ScanErr.Error(), Retryable: true}
	}
	if r.MalformedFrames > 0 {
		return &StreamError{Code: "stream_frame_malformed", Message: "stream contained unparseable data frames", Retryable: true}
	}
	if !r.Completed {
		return &StreamError{Code: "stream_unexpected_eof", Message: "stream ended before a completion marker", Retryable: true}
	}
	return nil
}

// scanStreamLines 逐行扫描 SSE 流，对每行调用 handle（返回 true 停止扫描）。
// 保持现行实现的 bufio.Scanner 默认单帧上限（64KB）；扫描失败记入 ScanErr，
// 不再被静默吞掉（E-04）。
func scanStreamLines(r io.Reader, result *StreamParseResult, handle func(line string) (stop bool)) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if handle(scanner.Text()) {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		result.ScanErr = err
	}
}

// streamErrorRetryable 按 provider 错误码/类型估计瞬时可重试性。
func streamErrorRetryable(code string) bool {
	switch strings.ToLower(code) {
	case "server_error", "rate_limit", "rate_limit_exceeded", "overloaded", "overloaded_error",
		"unavailable", "resource_exhausted", "deadline_exceeded", "internal":
		return true
	default:
		return false
	}
}

// parseOpenAIStreamBody 纯解析入口：只累积不投递（fixture 测试用）。
func parseOpenAIStreamBody(r io.Reader) *StreamParseResult {
	return parseOpenAIStreamBodyInto(r, nil)
}

// parseOpenAIStreamBodyInto 解析 OpenAI Chat Completions SSE 流。
// 完成标记为 `data: [DONE]`；另捕获 choices.finish_reason、usage 帧与 error 帧。
// sink 非 nil 时每个内容帧一解析出来就投递（流式），sink 报告取消即停止扫描。
func parseOpenAIStreamBodyInto(r io.Reader, sink streamFrameSink) *StreamParseResult {
	result := &StreamParseResult{emit: sink}
	scanStreamLines(r, result, func(line string) bool {
		if !strings.HasPrefix(line, "data: ") {
			return false
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if data == "[DONE]" {
			result.Completed = true
			return true
		}

		var event struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			result.MalformedFrames++
			return false
		}

		if event.Error != nil {
			code := firstNonEmpty(event.Error.Code, event.Error.Type)
			result.ProviderErr = &StreamError{Code: code, Message: event.Error.Message, Retryable: streamErrorRetryable(code)}
			return true
		}
		if event.Usage != nil {
			result.Usage = &Usage{
				PromptTokens:     event.Usage.PromptTokens,
				CompletionTokens: event.Usage.CompletionTokens,
				TotalTokens:      event.Usage.TotalTokens,
			}
		}
		if len(event.Choices) == 0 {
			return false
		}
		choice := event.Choices[0]
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			result.FinishReason = *choice.FinishReason
		}
		delta := choice.Delta.Content
		if delta == "" {
			return false
		}
		return !result.appendChunk(delta)
	})
	return result
}

// parseClaudeStreamBody 纯解析入口：只累积不投递（fixture 测试用）。
func parseClaudeStreamBody(r io.Reader) *StreamParseResult {
	return parseClaudeStreamBodyInto(r, nil)
}

// parseClaudeStreamBodyInto 解析 Claude Messages SSE 流。
// 完成标记为 message_stop（兼容现行实现也接受的 [DONE]）；
// finish_reason 取自 message_delta.delta.stop_reason 原值；
// usage 取自 message_start（input_tokens）与 message_delta（output_tokens）。
// sink 非 nil 时每个内容帧一解析出来就投递（流式），sink 报告取消即停止扫描。
func parseClaudeStreamBodyInto(r io.Reader, sink streamFrameSink) *StreamParseResult {
	result := &StreamParseResult{emit: sink}
	var inputTokens, outputTokens int
	sawUsage := false

	scanStreamLines(r, result, func(line string) bool {
		if !strings.HasPrefix(line, "data: ") {
			return false
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			result.Completed = true
			return true
		}

		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Type       string `json:"type"`
				Text       string `json:"text"`
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Message *struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage *struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
			Error *struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			result.MalformedFrames++
			return false
		}

		switch event.Type {
		case "error":
			if event.Error != nil {
				result.ProviderErr = &StreamError{Code: event.Error.Type, Message: event.Error.Message, Retryable: streamErrorRetryable(event.Error.Type)}
			} else {
				result.ProviderErr = &StreamError{Code: "provider_error", Message: "provider reported an unknown stream error"}
			}
			return true
		case "message_start":
			if event.Message != nil {
				inputTokens = event.Message.Usage.InputTokens
				outputTokens = event.Message.Usage.OutputTokens
				sawUsage = true
			}
		case "content_block_delta":
			if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				if !result.appendChunk(event.Delta.Text) {
					return true
				}
			}
		case "message_delta":
			if event.Delta.StopReason != "" {
				result.FinishReason = event.Delta.StopReason
			}
			if event.Usage != nil {
				outputTokens = event.Usage.OutputTokens
				sawUsage = true
			}
		case "message_stop":
			result.Completed = true
			return true
		}
		return false
	})

	if sawUsage {
		result.Usage = &Usage{PromptTokens: inputTokens, CompletionTokens: outputTokens, TotalTokens: inputTokens + outputTokens}
	}
	return result
}

// parseGeminiStreamBody 解析 Gemini streamGenerateContent SSE 流。
// Gemini 无 [DONE] 标记（现行实现忽略之）；候选帧携带非空 finishReason 即协议完成信号，
// finish_reason 保留 provider 原值（STOP/MAX_TOKENS/SAFETY/...，不统一改写）。
func parseGeminiStreamBody(r io.Reader) *StreamParseResult {
	return parseGeminiStreamBodyInto(r, nil)
}

// parseGeminiStreamBodyInto 同 parseGeminiStreamBody；sink 非 nil 时每个内容帧
// 一解析出来就投递（流式），sink 报告取消即停止扫描。
func parseGeminiStreamBodyInto(r io.Reader, sink streamFrameSink) *StreamParseResult {
	result := &StreamParseResult{emit: sink}
	scanStreamLines(r, result, func(line string) bool {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			return false
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if data == "" || data == "[DONE]" {
			return false
		}

		var event struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
			} `json:"candidates"`
			UsageMetadata *struct {
				PromptTokenCount     int `json:"promptTokenCount"`
				CandidatesTokenCount int `json:"candidatesTokenCount"`
				TotalTokenCount      int `json:"totalTokenCount"`
			} `json:"usageMetadata"`
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			result.MalformedFrames++
			return false
		}

		if event.Error != nil {
			code := firstNonEmpty(event.Error.Status, strconv.Itoa(event.Error.Code))
			result.ProviderErr = &StreamError{Code: code, Message: event.Error.Message, Retryable: streamErrorRetryable(code)}
			return true
		}
		if event.UsageMetadata != nil {
			result.Usage = &Usage{
				PromptTokens:     event.UsageMetadata.PromptTokenCount,
				CompletionTokens: event.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      event.UsageMetadata.TotalTokenCount,
			}
		}
		if len(event.Candidates) == 0 {
			return false
		}
		candidate := event.Candidates[0]
		if candidate.FinishReason != "" {
			result.FinishReason = candidate.FinishReason
			result.Completed = true
		}
		var delta strings.Builder
		for _, part := range candidate.Content.Parts {
			delta.WriteString(part.Text)
		}
		if delta.Len() == 0 {
			return false
		}
		return !result.appendChunk(delta.String())
	})
	return result
}
