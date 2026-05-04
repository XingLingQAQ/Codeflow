package adapters

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// OpenAIAdapter 兼容 OpenAI Chat Completions API，也作为 Codex provider 的默认实现。
type OpenAIAdapter struct {
	*BaseAdapter
}

// CodexAdapter 复用 OpenAI-compatible Chat Completions 协议。
type CodexAdapter struct {
	*OpenAIAdapter
}

// NewOpenAIAdapter 创建 OpenAI 适配器。
func NewOpenAIAdapter(config *AdapterConfig) *OpenAIAdapter {
	if config != nil && config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com"
	}
	return &OpenAIAdapter{BaseAdapter: NewBaseAdapter(config)}
}

// NewCodexAdapter 创建 Codex 适配器。
func NewCodexAdapter(config *AdapterConfig) *CodexAdapter {
	if config != nil && config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com"
	}
	return &CodexAdapter{OpenAIAdapter: NewOpenAIAdapter(config)}
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Send 发送消息。
func (a *OpenAIAdapter) Send(ctx context.Context, prompt string, options *SendOptions) (*AIResponse, error) {
	userMsg := Message{Role: RoleUser, Content: prompt, Blocks: []ContentBlock{{Type: "text", Text: prompt}}, Timestamp: time.Now()}
	a.AddMessage(userMsg)

	controls := resolveSendControls(a.config, options)
	contextPayload := AdapterPayloadContext{
		Messages:    a.GetHistory(),
		Model:       controls.Model,
		Temperature: controls.Temperature,
		MaxTokens:   controls.MaxTokens,
	}
	processed, err := applyBeforeSendHooks(ctx, controls.SemanticsControl(), contextPayload)
	if err != nil {
		return nil, err
	}

	response, err := a.sendChatCompletion(ctx, processed, composeSystemInstruction(controls.System, controls.Semantics), false)
	if err != nil {
		return nil, err
	}
	a.AddMessage(response.Message)
	aiResponse := &AIResponse{Content: response.Content, Blocks: cloneBlocks(response.Blocks), Model: response.Model, Usage: response.Usage, FinishReason: response.FinishReason}
	if err := notifyAdapterPostResponse(ctx, controls.SemanticsControl(), aiResponse); err != nil {
		return nil, err
	}
	return aiResponse, nil
}

// Stream 流式发送。
func (a *OpenAIAdapter) Stream(ctx context.Context, prompt string, options *SendOptions) (<-chan StreamChunk, error) {
	userMsg := Message{Role: RoleUser, Content: prompt, Blocks: []ContentBlock{{Type: "text", Text: prompt}}, Timestamp: time.Now()}
	a.AddMessage(userMsg)

	controls := resolveSendControls(a.config, options)
	contextPayload := AdapterPayloadContext{Messages: a.GetHistory(), Model: controls.Model, Temperature: controls.Temperature, MaxTokens: controls.MaxTokens}
	processed, err := applyBeforeSendHooks(ctx, controls.SemanticsControl(), contextPayload)
	if err != nil {
		return nil, err
	}

	reqBody := openAIRequest{
		Model:       processed.Model,
		Messages:    buildOpenAIMessages(processed.Messages, composeSystemInstruction(controls.System, controls.Semantics)),
		Temperature: processed.Temperature,
		MaxTokens:   processed.MaxTokens,
		Stream:      true,
	}

	resp, err := a.DoRequest(ctx, "POST", strings.TrimRight(a.config.BaseURL, "/")+"/v1/chat/completions", reqBody, map[string]string{"Authorization": "Bearer " + a.config.APIKey})
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk, 100)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		var fullContent strings.Builder
		index := 0
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if data == "[DONE]" {
				break
			}

			var event struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil || len(event.Choices) == 0 {
				continue
			}
			delta := event.Choices[0].Delta.Content
			if delta == "" {
				continue
			}
			fullContent.WriteString(delta)
			chunk := StreamChunk{Delta: delta, Index: index, Done: false}
			notifyAdapterStreamChunk(ctx, controls.SemanticsControl(), chunk)
			ch <- chunk
			index++
		}

		content := fullContent.String()
		assistantMsg := Message{Role: RoleAssistant, Content: content, Blocks: []ContentBlock{{Type: "text", Text: content}}, Timestamp: time.Now()}
		a.AddMessage(assistantMsg)
		finalChunk := StreamChunk{Delta: "", Index: index, Done: true}
		notifyAdapterStreamChunk(ctx, controls.SemanticsControl(), finalChunk)
		ch <- finalChunk
		_ = notifyAdapterPostResponse(ctx, controls.SemanticsControl(), &AIResponse{Content: content, Blocks: cloneBlocks(assistantMsg.Blocks), Model: processed.Model, FinishReason: "stop"})
	}()

	return ch, nil
}

// Compact 压缩历史。
func (a *OpenAIAdapter) Compact(ctx context.Context) error {
	history := a.GetHistory()
	if len(history) <= 4 {
		return nil
	}
	a.SetHistory(history[len(history)-4:])
	return nil
}

func (a *OpenAIAdapter) sendChatCompletion(ctx context.Context, payload AdapterPayloadContext, system string, stream bool) (*ToolTurnResponse, error) {
	reqBody := openAIRequest{Model: payload.Model, Messages: buildOpenAIMessages(payload.Messages, system), Temperature: payload.Temperature, MaxTokens: payload.MaxTokens, Stream: stream}
	resp, err := a.DoRequest(ctx, "POST", strings.TrimRight(a.config.BaseURL, "/")+"/v1/chat/completions", reqBody, map[string]string{"Authorization": "Bearer " + a.config.APIKey})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var openAIResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(openAIResp.Choices) == 0 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai response has no choices: %s", string(body))
	}

	content := openAIResp.Choices[0].Message.Content
	model := firstNonEmpty(openAIResp.Model, payload.Model)
	assistantMsg := Message{Role: RoleAssistant, Content: content, Blocks: []ContentBlock{{Type: "text", Text: content}}, Timestamp: time.Now()}
	return &ToolTurnResponse{
		Message:      assistantMsg,
		Content:      content,
		Blocks:       cloneBlocks(assistantMsg.Blocks),
		Model:        model,
		Usage:        Usage{PromptTokens: openAIResp.Usage.PromptTokens, CompletionTokens: openAIResp.Usage.CompletionTokens, TotalTokens: openAIResp.Usage.TotalTokens},
		FinishReason: openAIResp.Choices[0].FinishReason,
	}, nil
}

func buildOpenAIMessages(messages []Message, system string) []openAIMessage {
	out := make([]openAIMessage, 0, len(messages)+1)
	if strings.TrimSpace(system) != "" {
		out = append(out, openAIMessage{Role: string(RoleSystem), Content: system})
	}
	for _, msg := range messages {
		role := string(msg.Role)
		if msg.Role != RoleSystem && msg.Role != RoleUser && msg.Role != RoleAssistant {
			role = string(RoleUser)
		}
		out = append(out, openAIMessage{Role: role, Content: GetMessageText(msg)})
	}
	return out
}

// Send 发送 Codex 消息。
func (a *CodexAdapter) Send(ctx context.Context, prompt string, options *SendOptions) (*AIResponse, error) {
	return a.OpenAIAdapter.Send(ctx, prompt, options)
}

// Stream 流式发送 Codex 消息。
func (a *CodexAdapter) Stream(ctx context.Context, prompt string, options *SendOptions) (<-chan StreamChunk, error) {
	return a.OpenAIAdapter.Stream(ctx, prompt, options)
}

// Compact 压缩 Codex 历史。
func (a *CodexAdapter) Compact(ctx context.Context) error {
	return a.OpenAIAdapter.Compact(ctx)
}

func (c requestControls) SemanticsControl() *RequestControls {
	if c.Semantics == nil {
		return nil
	}
	return c.Semantics.Controls
}
