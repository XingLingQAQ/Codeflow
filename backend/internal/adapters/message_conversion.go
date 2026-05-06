package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	backendhooks "github.com/codeflow/backend/internal/hooks"
)

// AdapterPayloadContext 是 adapter 调用 provider 前的可变请求上下文。
type AdapterPayloadContext struct {
	Messages    []Message `json:"messages,omitempty"`
	Model       string    `json:"model,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// AdapterHookPayload 是 hook_before_send 使用的请求 payload。
type AdapterHookPayload struct {
	Messages    []Message `json:"messages,omitempty"`
	Model       string    `json:"model,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// CloneMessage 深拷贝消息，避免结构化块在调用方之间共享。
func CloneMessage(message Message) Message {
	clone := message
	clone.Blocks = cloneBlocks(message.Blocks)
	return clone
}

// CloneMessages 深拷贝消息列表。
func CloneMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, CloneMessage(message))
	}
	return out
}

// GetMessageText 将结构化消息投影为 provider 安全的纯文本。
func GetMessageText(message Message) string {
	text := strings.TrimSpace(blocksToText(message.Blocks))
	if text != "" {
		return text
	}
	return message.Content
}

// ToHookPayload 将 adapter 请求上下文转换为 hook payload。
func ToHookPayload(context AdapterPayloadContext) AdapterHookPayload {
	return AdapterHookPayload{
		Messages:    CloneMessages(context.Messages),
		Model:       context.Model,
		Temperature: cloneFloat64Ptr(context.Temperature),
		MaxTokens:   context.MaxTokens,
	}
}

// ApplyHookPayload 将 hook 返回值合并回 adapter 请求上下文。
func ApplyHookPayload(context AdapterPayloadContext, payload AdapterHookPayload) AdapterPayloadContext {
	hasMessageOverride := len(payload.Messages) > 0
	shouldApplyScalarOverrides := hasMessageOverride || payload.Messages == nil

	out := AdapterPayloadContext{
		Messages:    CloneMessages(context.Messages),
		Model:       context.Model,
		Temperature: cloneFloat64Ptr(context.Temperature),
		MaxTokens:   context.MaxTokens,
	}

	if hasMessageOverride {
		out.Messages = CloneMessages(payload.Messages)
	}
	if shouldApplyScalarOverrides {
		if strings.TrimSpace(payload.Model) != "" {
			out.Model = payload.Model
		}
		if payload.Temperature != nil {
			out.Temperature = cloneFloat64Ptr(payload.Temperature)
		}
		if payload.MaxTokens > 0 {
			out.MaxTokens = payload.MaxTokens
		}
	}
	return out
}

func applyBeforeSendHooks(ctx context.Context, controls *RequestControls, context AdapterPayloadContext) (AdapterPayloadContext, error) {
	payload := ToHookPayload(context)
	result, err := triggerAdapterHook(ctx, controls, backendhooks.HookBeforeSend, payload)
	if err != nil {
		return context, err
	}
	converted, ok := adapterHookPayloadFromAny(result)
	if !ok {
		return context, nil
	}
	return ApplyHookPayload(context, converted), nil
}

func notifyAdapterPostResponse(ctx context.Context, controls *RequestControls, response any) error {
	_, err := triggerAdapterHook(ctx, controls, backendhooks.HookPostResponse, response)
	return err
}

func notifyAdapterStreamChunk(ctx context.Context, controls *RequestControls, chunk StreamChunk) {
	_, _ = triggerAdapterHook(ctx, controls, backendhooks.HookOnStream, chunk)
}

func triggerAdapterHook(ctx context.Context, controls *RequestControls, hookType backendhooks.HookType, payload any) (any, error) {
	if !shouldRunAdapterHook(controls, hookType) {
		return payload, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return backendhooks.GetHookManager().Trigger(ctx, hookType, payload)
}

func shouldRunAdapterHook(controls *RequestControls, hookType backendhooks.HookType) bool {
	if !backendhooks.HasHookManager() {
		return false
	}
	if controls != nil && controls.EnableHooks != nil && !*controls.EnableHooks {
		return false
	}
	if controls == nil || len(controls.AllowedHooks) == 0 {
		return true
	}

	want := string(hookType)
	wantWithoutPrefix := strings.TrimPrefix(want, "hook_")
	for _, allowed := range controls.AllowedHooks {
		candidate := strings.TrimSpace(allowed)
		if candidate == want || candidate == wantWithoutPrefix {
			return true
		}
	}
	return false
}

func adapterHookPayloadFromAny(value any) (AdapterHookPayload, bool) {
	switch payload := value.(type) {
	case AdapterHookPayload:
		return payload, true
	case *AdapterHookPayload:
		if payload == nil {
			return AdapterHookPayload{}, false
		}
		return *payload, true
	case AdapterPayloadContext:
		return AdapterHookPayload{
			Messages:    CloneMessages(payload.Messages),
			Model:       payload.Model,
			Temperature: cloneFloat64Ptr(payload.Temperature),
			MaxTokens:   payload.MaxTokens,
		}, true
	case *AdapterPayloadContext:
		if payload == nil {
			return AdapterHookPayload{}, false
		}
		return AdapterHookPayload{
			Messages:    CloneMessages(payload.Messages),
			Model:       payload.Model,
			Temperature: cloneFloat64Ptr(payload.Temperature),
			MaxTokens:   payload.MaxTokens,
		}, true
	default:
		return AdapterHookPayload{}, false
	}
}

func blocksToText(blocks []ContentBlock) string {
	if len(blocks) == 0 {
		return ""
	}

	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		segment := contentBlockToText(block)
		if strings.TrimSpace(segment) != "" {
			parts = append(parts, segment)
		}
	}
	return strings.Join(parts, "\n")
}

func contentBlockToText(block ContentBlock) string {
	switch block.Type {
	case "text":
		return block.Text
	case "tool_use", "tool_call":
		return formatToolUseBlock(block)
	case "tool_result":
		return formatToolResultBlock(block)
	case "json":
		if len(block.Input) > 0 {
			return safeJSONString(block.Input)
		}
		if block.Result != "" {
			return block.Result
		}
		return block.Text
	default:
		if block.Text != "" {
			return block.Text
		}
		if block.Result != "" {
			return block.Result
		}
		return safeJSONString(block)
	}
}

func formatToolUseBlock(block ContentBlock) string {
	name := strings.TrimSpace(block.Name)
	if name == "" {
		name = strings.TrimSpace(block.ID)
	}
	payload := ""
	if len(block.Input) > 0 {
		payload = safeJSONString(block.Input)
	}
	return strings.TrimSpace(fmt.Sprintf("[tool_call:%s] %s", name, payload))
}

func formatToolResultBlock(block ContentBlock) string {
	name := strings.TrimSpace(block.Name)
	if name == "" {
		name = strings.TrimSpace(block.ToolUseID)
	}
	return strings.TrimSpace(fmt.Sprintf("[tool_result:%s] %s", name, block.Result))
}

func safeJSONString(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(payload)
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
