package adapters

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// GeminiAdapter 通过 Gemini REST generateContent 接口发送请求。
type GeminiAdapter struct {
	*BaseAdapter
}

// NewGeminiAdapter 创建 Gemini 适配器。
func NewGeminiAdapter(config *AdapterConfig) *GeminiAdapter {
	if config != nil && config.BaseURL == "" {
		config.BaseURL = "https://generativelanguage.googleapis.com"
	}
	return &GeminiAdapter{BaseAdapter: NewBaseAdapter(config)}
}

type geminiRequest struct {
	Contents          []geminiContent        `json:"contents"`
	SystemInstruction *geminiContent         `json:"systemInstruction,omitempty"`
	GenerationConfig  geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content      geminiContent `json:"content"`
		FinishReason string        `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// Send 发送消息。
func (a *GeminiAdapter) Send(ctx context.Context, prompt string, options *SendOptions) (*AIResponse, error) {
	userMsg := Message{Role: RoleUser, Content: prompt, Blocks: []ContentBlock{{Type: "text", Text: prompt}}, Timestamp: time.Now()}
	a.AddMessage(userMsg)

	controls := resolveSendControls(a.config, options)
	contextPayload := AdapterPayloadContext{Messages: a.GetHistory(), Model: controls.Model, Temperature: controls.Temperature, MaxTokens: controls.MaxTokens}
	processed, err := applyBeforeSendHooks(ctx, controls.SemanticsControl(), contextPayload)
	if err != nil {
		return nil, err
	}

	response, err := a.generateContent(ctx, processed, composeSystemInstruction(controls.System, controls.Semantics))
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
func (a *GeminiAdapter) Stream(ctx context.Context, prompt string, options *SendOptions) (<-chan StreamChunk, error) {
	userMsg := Message{Role: RoleUser, Content: prompt, Blocks: []ContentBlock{{Type: "text", Text: prompt}}, Timestamp: time.Now()}
	a.AddMessage(userMsg)

	controls := resolveSendControls(a.config, options)
	contextPayload := AdapterPayloadContext{Messages: a.GetHistory(), Model: controls.Model, Temperature: controls.Temperature, MaxTokens: controls.MaxTokens}
	processed, err := applyBeforeSendHooks(ctx, controls.SemanticsControl(), contextPayload)
	if err != nil {
		return nil, err
	}

	reqBody := buildGeminiRequest(processed, composeSystemInstruction(controls.System, controls.Semantics))
	resp, err := a.DoRequest(ctx, "POST", a.geminiEndpoint(processed.Model, "streamGenerateContent"), reqBody, nil)
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
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if data == "" || data == "[DONE]" {
				continue
			}

			var event geminiResponse
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			delta := extractGeminiText(event)
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
func (a *GeminiAdapter) Compact(ctx context.Context) error {
	history := a.GetHistory()
	if len(history) <= 4 {
		return nil
	}
	a.SetHistory(history[len(history)-4:])
	return nil
}

func (a *GeminiAdapter) generateContent(ctx context.Context, payload AdapterPayloadContext, system string) (*ToolTurnResponse, error) {
	reqBody := buildGeminiRequest(payload, system)
	resp, err := a.DoRequest(ctx, "POST", a.geminiEndpoint(payload.Model, "generateContent"), reqBody, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var geminiResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	content := extractGeminiText(geminiResp)
	assistantMsg := Message{Role: RoleAssistant, Content: content, Blocks: []ContentBlock{{Type: "text", Text: content}}, Timestamp: time.Now()}
	finishReason := ""
	if len(geminiResp.Candidates) > 0 {
		finishReason = geminiResp.Candidates[0].FinishReason
	}
	return &ToolTurnResponse{
		Message: assistantMsg,
		Content: content,
		Blocks:  cloneBlocks(assistantMsg.Blocks),
		Model:   payload.Model,
		Usage: Usage{
			PromptTokens:     geminiResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      geminiResp.UsageMetadata.TotalTokenCount,
		},
		FinishReason: finishReason,
	}, nil
}

func (a *GeminiAdapter) geminiEndpoint(model, action string) string {
	base := strings.TrimRight(a.config.BaseURL, "/")
	model = strings.TrimPrefix(model, "models/")
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:%s", base, url.PathEscape(model), action)
	if a.config.APIKey == "" {
		return endpoint
	}
	return endpoint + "?key=" + url.QueryEscape(a.config.APIKey)
}

func buildGeminiRequest(payload AdapterPayloadContext, system string) geminiRequest {
	request := geminiRequest{
		Contents: buildGeminiContents(payload.Messages),
		GenerationConfig: geminiGenerationConfig{
			Temperature:     payload.Temperature,
			MaxOutputTokens: payload.MaxTokens,
		},
	}
	if strings.TrimSpace(system) != "" {
		request.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: system}}}
	}
	return request
}

func buildGeminiContents(messages []Message) []geminiContent {
	contents := make([]geminiContent, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == RoleSystem {
			continue
		}
		role := "user"
		if msg.Role == RoleAssistant {
			role = "model"
		}
		contents = append(contents, geminiContent{Role: role, Parts: []geminiPart{{Text: GetMessageText(msg)}}})
	}
	return contents
}

func extractGeminiText(response geminiResponse) string {
	if len(response.Candidates) == 0 {
		return ""
	}
	parts := response.Candidates[0].Content.Parts
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part.Text) != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "")
}
