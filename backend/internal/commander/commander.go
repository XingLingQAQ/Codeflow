package commander

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/adapters"
	"github.com/codeflow/backend/internal/config"
)

// Commander 指挥官模式实现
type Commander struct {
	agents          map[AgentRole]*AgentConfig
	callStack       []*CallTrace
	currentCallID   int
	eventHandlers   map[CommanderEvent][]EventHandler
	maxNestingDepth int
	mu              sync.RWMutex
}

// NewCommander 创建指挥官
func NewCommander(maxNestingDepth int) *Commander {
	if maxNestingDepth <= 0 {
		maxNestingDepth = 5
	}
	return &Commander{
		agents:          make(map[AgentRole]*AgentConfig),
		callStack:       make([]*CallTrace, 0),
		eventHandlers:   make(map[CommanderEvent][]EventHandler),
		maxNestingDepth: maxNestingDepth,
	}
}

// RegisterAgent 注册代理
func (c *Commander) RegisterAgent(config AgentConfig) {
	c.mu.Lock()
	c.agents[config.Role] = &config
	c.mu.Unlock()

	c.emit(EventAgentRegistered, map[string]interface{}{
		"role": config.Role,
	})
}

// GetAgent 获取代理
func (c *Commander) GetAgent(role AgentRole) *AgentConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.agents[role]
}

// CallCoderAgent 调用Coder代理
func (c *Commander) CallCoderAgent(params CallCoderAgentParams) (*ToolCallResult, error) {
	startTime := time.Now()
	callID := c.generateCallID()

	trace := &CallTrace{
		ID:        callID,
		ParentID:  c.getCurrentParentID(),
		AgentRole: RoleCoder,
		ToolName:  "call_coder_agent",
		Params: map[string]interface{}{
			"task":        params.Task,
			"context":     params.Context,
			"files":       params.Files,
			"language":    params.Language,
			"constraints": params.Constraints,
		},
		StartTime: startTime.UnixMilli(),
		Children:  make([]*CallTrace, 0),
	}

	c.pushCall(trace)
	c.emit(EventToolCallStart, map[string]interface{}{"trace": trace})

	defer func() {
		trace.EndTime = time.Now().UnixMilli()
		c.emit(EventToolCallEnd, map[string]interface{}{"trace": trace})
		c.popCall()
	}()

	// 检查嵌套深度
	if c.getCurrentDepth() > c.maxNestingDepth {
		result := &ToolCallResult{
			Success:   false,
			Output:    "",
			AgentRole: RoleCoder,
			Duration:  time.Since(startTime).Milliseconds(),
			Error:     fmt.Sprintf("max nesting depth (%d) exceeded", c.maxNestingDepth),
		}
		trace.Result = result
		return result, nil
	}

	coderAgent := c.GetAgent(RoleCoder)
	if coderAgent == nil {
		result := &ToolCallResult{
			Success:   false,
			Output:    "",
			AgentRole: RoleCoder,
			Duration:  time.Since(startTime).Milliseconds(),
			Error:     "coder agent not registered",
		}
		trace.Result = result
		return result, nil
	}

	// 构建prompt
	prompt := c.buildCoderPrompt(params)

	// 嫁接上下文
	var graftedCtx *GraftedContext
	if params.Context != "" {
		mainAgent := c.GetAgent(RoleMain)
		if mainAgent != nil {
			var err error
			graftedCtx, err = c.GraftContext(RoleMain, RoleCoder, &ContextGraftConfig{
				InheritMessages:     true,
				InheritSystemPrompt: true,
				MaxContextTokens:    4000,
			})
			if err == nil && coderAgent.Adapter != nil {
				coderAgent.Adapter.SetHistory(graftedCtx.Messages)
			}
		}
	}

	// 调用Coder代理
	if coderAgent.Adapter == nil {
		result := &ToolCallResult{
			Success:   false,
			Output:    "",
			AgentRole: RoleCoder,
			Duration:  time.Since(startTime).Milliseconds(),
			Error:     "coder agent adapter not configured",
		}
		trace.Result = result
		return result, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sendOptions := buildAgentSendOptions(coderAgent, graftedCtx)
	response, err := coderAgent.Adapter.Send(ctx, prompt, sendOptions)
	if err != nil {
		result := &ToolCallResult{
			Success:   false,
			Output:    "",
			AgentRole: RoleCoder,
			Duration:  time.Since(startTime).Milliseconds(),
			Error:     err.Error(),
		}
		trace.Result = result
		return result, nil
	}

	result := &ToolCallResult{
		Success:   true,
		Output:    response.Content,
		AgentRole: RoleCoder,
		Duration:  time.Since(startTime).Milliseconds(),
	}
	// Usage总是存在的（值类型）
	result.TokenUsage = &TokenUsage{
		PromptTokens:     response.Usage.PromptTokens,
		CompletionTokens: response.Usage.CompletionTokens,
		TotalTokens:      response.Usage.TotalTokens,
	}

	trace.Result = result
	return result, nil
}

// ConsultSubExpert 咨询子专家
func (c *Commander) ConsultSubExpert(params ConsultSubExpertParams) (*ToolCallResult, error) {
	startTime := time.Now()
	callID := c.generateCallID()

	trace := &CallTrace{
		ID:        callID,
		ParentID:  c.getCurrentParentID(),
		AgentRole: RoleSubExpert,
		ToolName:  "consult_sub_expert",
		Params: map[string]interface{}{
			"domain":   params.Domain,
			"question": params.Question,
			"context":  params.Context,
			"depth":    params.Depth,
		},
		StartTime: startTime.UnixMilli(),
		Children:  make([]*CallTrace, 0),
	}

	c.pushCall(trace)
	c.emit(EventToolCallStart, map[string]interface{}{"trace": trace})

	defer func() {
		trace.EndTime = time.Now().UnixMilli()
		c.emit(EventToolCallEnd, map[string]interface{}{"trace": trace})
		c.popCall()
	}()

	// 检查嵌套深度
	maxDepth := params.Depth
	if maxDepth <= 0 {
		maxDepth = c.maxNestingDepth
	}
	if c.getCurrentDepth() > maxDepth {
		result := &ToolCallResult{
			Success:   false,
			Output:    "",
			AgentRole: RoleSubExpert,
			Duration:  time.Since(startTime).Milliseconds(),
			Error:     fmt.Sprintf("max nesting depth (%d) exceeded", maxDepth),
		}
		trace.Result = result
		return result, nil
	}

	subExpert := c.GetAgent(RoleSubExpert)
	if subExpert == nil {
		result := &ToolCallResult{
			Success:   false,
			Output:    "",
			AgentRole: RoleSubExpert,
			Duration:  time.Since(startTime).Milliseconds(),
			Error:     "sub expert agent not registered",
		}
		trace.Result = result
		return result, nil
	}

	// 构建prompt
	prompt := c.buildSubExpertPrompt(params)

	// 嫁接上下文
	var graftedCtx *GraftedContext
	if params.Context != "" {
		mainAgent := c.GetAgent(RoleMain)
		if mainAgent != nil {
			var err error
			graftedCtx, err = c.GraftContext(RoleMain, RoleSubExpert, &ContextGraftConfig{
				InheritMessages:     true,
				InheritSystemPrompt: true,
				MaxContextTokens:    2000,
			})
			if err == nil && subExpert.Adapter != nil {
				subExpert.Adapter.SetHistory(graftedCtx.Messages)
			}
		}
	}

	// 调用子专家
	if subExpert.Adapter == nil {
		result := &ToolCallResult{
			Success:   false,
			Output:    "",
			AgentRole: RoleSubExpert,
			Duration:  time.Since(startTime).Milliseconds(),
			Error:     "sub expert adapter not configured",
		}
		trace.Result = result
		return result, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sendOptions := buildAgentSendOptions(subExpert, graftedCtx)
	response, err := subExpert.Adapter.Send(ctx, prompt, sendOptions)
	if err != nil {
		result := &ToolCallResult{
			Success:   false,
			Output:    "",
			AgentRole: RoleSubExpert,
			Duration:  time.Since(startTime).Milliseconds(),
			Error:     err.Error(),
		}
		trace.Result = result
		return result, nil
	}

	result := &ToolCallResult{
		Success:   true,
		Output:    response.Content,
		AgentRole: RoleSubExpert,
		Duration:  time.Since(startTime).Milliseconds(),
	}
	// Usage总是存在的（值类型）
	result.TokenUsage = &TokenUsage{
		PromptTokens:     response.Usage.PromptTokens,
		CompletionTokens: response.Usage.CompletionTokens,
		TotalTokens:      response.Usage.TotalTokens,
	}

	trace.Result = result
	return result, nil
}

func BuildAgentConfigFromResolved(role AgentRole, resolved *config.ResolvedConfig, adapter adapters.ICliAdapter) (*AgentConfig, error) {
	if resolved == nil {
		return nil, fmt.Errorf("resolved config is nil")
	}
	if adapter == nil {
		return nil, fmt.Errorf("adapter is nil")
	}

	agent := &AgentConfig{
		Role:         role,
		Adapter:      adapter,
		SystemPrompt: strings.TrimSpace(resolved.SystemPrompt),
		Model:        strings.TrimSpace(resolved.Model),
		MaxDepth:     0,
		Timeout:      resolved.Timeout,
	}
	temperature := resolved.Temperature
	agent.Temperature = &temperature
	if resolved.MaxTokens != nil {
		agent.MaxTokens = *resolved.MaxTokens
	}
	if trimmed := strings.TrimSpace(resolved.AnswerStyle); trimmed != "" {
		agent.AnswerStyle = trimmed
	}
	if len(resolved.Capabilities) > 0 {
		agent.Capabilities = append([]string(nil), resolved.Capabilities...)
	}
	if controls := buildResolvedRequestControls(resolved); controls != nil {
		agent.Controls = controls
	}
	return agent, nil
}

func BuildAgentFromResolved(role AgentRole, resolved *config.ResolvedConfig) (*AgentConfig, error) {
	if resolved == nil {
		return nil, fmt.Errorf("resolved config is nil")
	}
	if resolved.APIChannel == nil {
		return nil, fmt.Errorf("resolved config api channel is nil")
	}

	provider, err := resolved.APIChannel.AdapterProvider()
	if err != nil {
		return nil, err
	}

	adapterConfig := &adapters.AdapterConfig{
		APIKey:           resolved.APIChannel.APIKey,
		BaseURL:          resolved.APIChannel.BaseURL,
		Model:            strings.TrimSpace(resolved.Model),
		Temperature:      resolved.Temperature,
		MaxRetries:       resolved.MaxRetries,
		ForceMaxRetries:  true,
		ForceTemperature: true,
	}
	if resolved.MaxTokens != nil {
		adapterConfig.MaxTokens = *resolved.MaxTokens
		adapterConfig.ForceMaxTokens = true
	}
	if resolved.Timeout > 0 {
		adapterConfig.Timeout = time.Duration(resolved.Timeout) * time.Millisecond
		adapterConfig.ForceTimeout = true
	}

	adapter, err := adapters.NewAdapter(provider, adapterConfig)
	if err != nil {
		return nil, err
	}
	return BuildAgentConfigFromResolved(role, resolved, adapter)
}

func RoleFromConfigRole(role config.RoleType) (AgentRole, error) {
	switch role {
	case config.RoleMain:
		return RoleMain, nil
	case config.RoleCoder:
		return RoleCoder, nil
	case config.RoleSub:
		return RoleSubExpert, nil
	default:
		return "", fmt.Errorf("unsupported config role %q", role)
	}
}

func buildResolvedRequestControls(resolved *config.ResolvedConfig) *adapters.RequestControls {
	if resolved == nil {
		return nil
	}
	controls := &adapters.RequestControls{}
	if len(resolved.AllowedSkills) > 0 {
		controls.AllowedSkills = append([]string(nil), resolved.AllowedSkills...)
	}
	if len(resolved.AllowedHooks) > 0 {
		controls.AllowedHooks = append([]string(nil), resolved.AllowedHooks...)
	}
	if len(controls.AllowedSkills) == 0 && len(controls.AllowedHooks) == 0 {
		return nil
	}
	return controls
}

func buildAgentSendOptions(agent *AgentConfig, graftedCtx *GraftedContext) *adapters.SendOptions {
	if agent == nil {
		return nil
	}

	systemPrompt := agent.SystemPrompt
	if graftedCtx != nil && graftedCtx.SystemPrompt != "" {
		systemPrompt = graftedCtx.SystemPrompt
	}

	options := &adapters.SendOptions{
		System:      systemPrompt,
		Semantics:   buildAgentRequestSemantics(agent),
		Model:       agent.Model,
		Temperature: cloneFloat64(agent.Temperature),
		MaxTokens:   agent.MaxTokens,
	}

	if options.System == "" && options.Semantics == nil && options.Model == "" && options.Temperature == nil && options.MaxTokens == 0 {
		return nil
	}

	return options
}

func buildAgentRequestSemantics(agent *AgentConfig) *adapters.RequestSemantics {
	if agent == nil {
		return nil
	}

	semantics := &adapters.RequestSemantics{
		AnswerStyle: strings.TrimSpace(agent.AnswerStyle),
		Controls:    adapters.CloneRequestControls(agent.Controls),
	}
	if len(agent.Capabilities) > 0 {
		semantics.Capabilities = append([]string(nil), agent.Capabilities...)
	}

	if semantics.AnswerStyle == "" && len(semantics.Capabilities) == 0 && semantics.Controls == nil {
		return nil
	}

	return semantics
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// GraftContext 嫁接上下文
func (c *Commander) GraftContext(sourceRole, targetRole AgentRole, config *ContextGraftConfig) (*GraftedContext, error) {
	sourceAgent := c.GetAgent(sourceRole)
	if sourceAgent == nil {
		return nil, fmt.Errorf("source agent '%s' not registered", sourceRole)
	}

	targetAgent := c.GetAgent(targetRole)
	if targetAgent == nil {
		return nil, fmt.Errorf("target agent '%s' not registered", targetRole)
	}

	var messages []adapters.Message
	if sourceAgent.Adapter != nil {
		messages = sourceAgent.Adapter.GetHistory()
	}

	if config == nil {
		config = &ContextGraftConfig{InheritMessages: true}
	}

	if config.InheritMessages {
		// 过滤角色
		if len(config.FilterRoles) > 0 {
			filtered := make([]adapters.Message, 0)
			roleSet := make(map[string]bool)
			for _, r := range config.FilterRoles {
				roleSet[r] = true
			}
			for _, m := range messages {
				if roleSet[string(m.Role)] {
					filtered = append(filtered, m)
				}
			}
			messages = filtered
		}

		// 限制token数量（简化：1 token ≈ 4字符）
		if config.MaxContextTokens > 0 {
			maxChars := config.MaxContextTokens * 4
			totalChars := 0
			filtered := make([]adapters.Message, 0)

			// 从最新消息开始保留
			for i := len(messages) - 1; i >= 0; i-- {
				msgChars := len(messages[i].Content)
				if totalChars+msgChars <= maxChars {
					filtered = append([]adapters.Message{messages[i]}, filtered...)
					totalChars += msgChars
				} else {
					break
				}
			}
			messages = filtered
		}
	} else {
		messages = nil
	}

	var systemPrompt string
	if config.InheritSystemPrompt {
		systemPrompt = sourceAgent.SystemPrompt
	}

	totalChars := 0
	for _, m := range messages {
		totalChars += len(m.Content)
	}

	graftedContext := &GraftedContext{
		Messages:     messages,
		SystemPrompt: systemPrompt,
		Metadata: GraftedContextMetadata{
			SourceAgent: sourceRole,
			GraftedAt:   time.Now().UnixMilli(),
			TokenCount:  (totalChars + 3) / 4, // 向上取整
		},
	}

	c.emit(EventContextGrafted, map[string]interface{}{
		"source_role": sourceRole,
		"target_role": targetRole,
		"context":     graftedContext,
	})

	return graftedContext, nil
}

// GetToolDefinitions 获取工具定义
func (c *Commander) GetToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "call_coder_agent",
			Description: "Delegate a coding task to the Coder Agent. Use this for code generation, refactoring, debugging, or any programming-related tasks.",
			Parameters: ToolDefinitionParams{
				Type: "object",
				Properties: map[string]ToolPropertySchema{
					"task": {
						Type:        "string",
						Description: "The coding task to perform",
					},
					"context": {
						Type:        "string",
						Description: "Additional context or requirements for the task",
					},
					"files": {
						Type:        "array",
						Description: "List of file paths relevant to the task",
						Items:       &ToolPropertyItem{Type: "string"},
					},
					"language": {
						Type:        "string",
						Description: "Programming language for the task",
					},
					"constraints": {
						Type:        "array",
						Description: "Constraints or requirements to follow",
						Items:       &ToolPropertyItem{Type: "string"},
					},
				},
				Required: []string{"task"},
			},
		},
		{
			Name:        "consult_sub_expert",
			Description: "Consult a domain-specific sub-expert for specialized knowledge. Use this for questions requiring deep expertise in a specific area.",
			Parameters: ToolDefinitionParams{
				Type: "object",
				Properties: map[string]ToolPropertySchema{
					"domain": {
						Type:        "string",
						Description: "The domain of expertise (e.g., \"security\", \"performance\", \"architecture\")",
					},
					"question": {
						Type:        "string",
						Description: "The question to ask the sub-expert",
					},
					"context": {
						Type:        "string",
						Description: "Additional context for the question",
					},
					"depth": {
						Type:        "number",
						Description: "Maximum nesting depth for recursive consultations",
					},
				},
				Required: []string{"domain", "question"},
			},
		},
	}
}

// GetCallTrace 获取调用追踪
func (c *Commander) GetCallTrace() []*CallTrace {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*CallTrace, len(c.callStack))
	copy(result, c.callStack)
	return result
}

// On 注册事件处理器
func (c *Commander) On(event CommanderEvent, handler EventHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.eventHandlers[event] = append(c.eventHandlers[event], handler)
}

// Off 移除事件处理器
func (c *Commander) Off(event CommanderEvent, handler EventHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()

	handlers := c.eventHandlers[event]
	for i, h := range handlers {
		// 比较函数指针（简化实现）
		if fmt.Sprintf("%p", h) == fmt.Sprintf("%p", handler) {
			c.eventHandlers[event] = append(handlers[:i], handlers[i+1:]...)
			break
		}
	}
}

// ==================== 私有方法 ====================

func (c *Commander) generateCallID() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentCallID++
	return fmt.Sprintf("call_%d_%d", c.currentCallID, time.Now().UnixMilli())
}

func (c *Commander) getCurrentParentID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.callStack) == 0 {
		return ""
	}
	return c.callStack[len(c.callStack)-1].ID
}

func (c *Commander) getCurrentDepth() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.callStack)
}

func (c *Commander) pushCall(trace *CallTrace) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.callStack) > 0 {
		parent := c.callStack[len(c.callStack)-1]
		parent.Children = append(parent.Children, trace)
		c.emitUnsafe(EventNestedCallStart, map[string]interface{}{
			"trace":  trace,
			"parent": parent,
		})
	}
	c.callStack = append(c.callStack, trace)
}

func (c *Commander) popCall() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.callStack) > 0 {
		trace := c.callStack[len(c.callStack)-1]
		c.callStack = c.callStack[:len(c.callStack)-1]
		if len(c.callStack) > 0 {
			c.emitUnsafe(EventNestedCallEnd, map[string]interface{}{
				"trace": trace,
			})
		}
	}
}

func (c *Commander) emit(event CommanderEvent, data interface{}) {
	c.mu.RLock()
	handlers := c.eventHandlers[event]
	c.mu.RUnlock()

	for _, handler := range handlers {
		func() {
			defer func() {
				recover() // 忽略事件处理器错误
			}()
			handler(data)
		}()
	}
}

func (c *Commander) emitUnsafe(event CommanderEvent, data interface{}) {
	handlers := c.eventHandlers[event]
	for _, handler := range handlers {
		func() {
			defer func() {
				recover()
			}()
			handler(data)
		}()
	}
}

func (c *Commander) buildCoderPrompt(params CallCoderAgentParams) string {
	var sb strings.Builder
	sb.WriteString("## Coding Task\n\n")
	sb.WriteString(params.Task)

	if params.Context != "" {
		sb.WriteString("\n\n## Context\n\n")
		sb.WriteString(params.Context)
	}

	if len(params.Files) > 0 {
		sb.WriteString("\n\n## Relevant Files\n\n")
		for _, f := range params.Files {
			sb.WriteString("- ")
			sb.WriteString(f)
			sb.WriteString("\n")
		}
	}

	if params.Language != "" {
		sb.WriteString("\n\n## Language\n\n")
		sb.WriteString(params.Language)
	}

	if len(params.Constraints) > 0 {
		sb.WriteString("\n\n## Constraints\n\n")
		for _, c := range params.Constraints {
			sb.WriteString("- ")
			sb.WriteString(c)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func (c *Commander) buildSubExpertPrompt(params ConsultSubExpertParams) string {
	var sb strings.Builder
	sb.WriteString("## Domain: ")
	sb.WriteString(params.Domain)
	sb.WriteString("\n\n## Question\n\n")
	sb.WriteString(params.Question)

	if params.Context != "" {
		sb.WriteString("\n\n## Context\n\n")
		sb.WriteString(params.Context)
	}

	return sb.String()
}
