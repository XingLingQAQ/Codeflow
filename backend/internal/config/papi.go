// Package config - PAPI (Prompt AI-Profile Interface) system
package config

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// PAPIVariable PAPI变量定义
type PAPIVariable struct {
	Name        string   `json:"name"`         // 变量名（如 BACKEND_EXPERT）
	Model       string   `json:"model"`        // 目标模型
	Temperature float64  `json:"temperature"`  // 温度
	APIChannel  string   `json:"api_channel"`  // API通道
	MCPTools    []string `json:"mcp_tools"`    // MCP工具列表
	Prompt      string   `json:"prompt"`       // System Prompt
	Category    []string `json:"category"`     // 适用任务类别（如 backend, frontend, debug）
}

// PAPIMapping PAPI变量映射
type PAPIMapping struct {
	Variables map[string]*PAPIVariable `json:"variables"` // 变量名 -> 变量定义
}

// PAPIManager PAPI管理器
type PAPIManager struct {
	mapping   PAPIMapping
	mu        sync.RWMutex
	varRegexp *regexp.Regexp
}

// NewPAPIManager 创建PAPI管理器
func NewPAPIManager() *PAPIManager {
	return &PAPIManager{
		mapping: PAPIMapping{
			Variables: make(map[string]*PAPIVariable),
		},
		varRegexp: regexp.MustCompile(`\$\{([A-Z_]+)\}`),
	}
}

// DefineVariable 定义PAPI变量
func (p *PAPIManager) DefineVariable(variable *PAPIVariable) error {
	if variable == nil {
		return fmt.Errorf("variable cannot be nil")
	}
	if variable.Name == "" {
		return fmt.Errorf("variable name cannot be empty")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.mapping.Variables[variable.Name] = variable
	return nil
}

// GetVariable 获取PAPI变量
func (p *PAPIManager) GetVariable(name string) (*PAPIVariable, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if variable, ok := p.mapping.Variables[name]; ok {
		copy := *variable
		return &copy, nil
	}

	return nil, fmt.Errorf("variable %s not found", name)
}

// DeleteVariable 删除PAPI变量
func (p *PAPIManager) DeleteVariable(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.mapping.Variables[name]; ok {
		delete(p.mapping.Variables, name)
		return true
	}
	return false
}

// ListVariables 列出所有PAPI变量
func (p *PAPIManager) ListVariables() []*PAPIVariable {
	p.mu.RLock()
	defer p.mu.RUnlock()

	variables := make([]*PAPIVariable, 0, len(p.mapping.Variables))
	for _, v := range p.mapping.Variables {
		copy := *v
		variables = append(variables, &copy)
	}
	return variables
}

// ResolveByCategory 根据任务类别解析PAPI变量
func (p *PAPIManager) ResolveByCategory(category string) (*PAPIVariable, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, variable := range p.mapping.Variables {
		for _, cat := range variable.Category {
			if strings.EqualFold(cat, category) {
				copy := *variable
				return &copy, nil
			}
		}
	}

	return nil, fmt.Errorf("no variable found for category: %s", category)
}

// ParseVariables 解析文本中的PAPI变量
func (p *PAPIManager) ParseVariables(text string) []string {
	matches := p.varRegexp.FindAllStringSubmatch(text, -1)
	variables := make([]string, 0, len(matches))
	seen := make(map[string]bool)

	for _, match := range matches {
		if len(match) > 1 {
			varName := match[1]
			if !seen[varName] {
				seen[varName] = true
				variables = append(variables, varName)
			}
		}
	}

	return variables
}

// ExpandVariables 展开文本中的PAPI变量
func (p *PAPIManager) ExpandVariables(text string) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := text
	matches := p.varRegexp.FindAllStringSubmatch(text, -1)

	for _, match := range matches {
		if len(match) > 1 {
			varName := match[1]
			if variable, ok := p.mapping.Variables[varName]; ok {
				// 替换为变量的模型名称
				result = strings.ReplaceAll(result, match[0], variable.Model)
			} else {
				return "", fmt.Errorf("undefined variable: ${%s}", varName)
			}
		}
	}

	return result, nil
}

// HotSwap 热切换：将变量映射到新的配置
func (p *PAPIManager) HotSwap(varName string, newVariable *PAPIVariable) error {
	if newVariable == nil {
		return fmt.Errorf("new variable cannot be nil")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// 检查变量是否存在
	if _, ok := p.mapping.Variables[varName]; !ok {
		return fmt.Errorf("variable %s not found", varName)
	}

	// 保留原变量名
	newVariable.Name = varName
	p.mapping.Variables[varName] = newVariable

	return nil
}

// ApplyToRoleConfig 将PAPI变量应用到角色配置
func (p *PAPIManager) ApplyToRoleConfig(varName string, roleConfig *RoleConfig) error {
	variable, err := p.GetVariable(varName)
	if err != nil {
		return err
	}

	roleConfig.Model = variable.Model
	roleConfig.Temperature = variable.Temperature
	roleConfig.APIChannel = variable.APIChannel
	roleConfig.MCPTools = append(roleConfig.MCPTools, variable.MCPTools...)
	if variable.Prompt != "" {
		roleConfig.SystemPrompt = variable.Prompt
	}

	return nil
}

// DetectConflicts 检测PAPI变量冲突
func (p *PAPIManager) DetectConflicts() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var conflicts []string

	// 检查类别冲突：同一类别不应有多个变量
	categoryMap := make(map[string][]string)
	for varName, variable := range p.mapping.Variables {
		for _, cat := range variable.Category {
			categoryMap[cat] = append(categoryMap[cat], varName)
		}
	}

	for cat, vars := range categoryMap {
		if len(vars) > 1 {
			conflicts = append(conflicts, fmt.Sprintf(
				"Category '%s' has multiple variables: %v",
				cat, vars))
		}
	}

	return conflicts
}

// GetMapping 获取完整的PAPI映射
func (p *PAPIManager) GetMapping() *PAPIMapping {
	p.mu.RLock()
	defer p.mu.RUnlock()

	mapping := PAPIMapping{
		Variables: make(map[string]*PAPIVariable),
	}

	for name, variable := range p.mapping.Variables {
		copy := *variable
		mapping.Variables[name] = &copy
	}

	return &mapping
}

// LoadMapping 加载PAPI映射
func (p *PAPIManager) LoadMapping(mapping *PAPIMapping) error {
	if mapping == nil {
		return fmt.Errorf("mapping cannot be nil")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.mapping = *mapping
	if p.mapping.Variables == nil {
		p.mapping.Variables = make(map[string]*PAPIVariable)
	}

	return nil
}

// DefaultPAPIVariables 默认PAPI变量
var DefaultPAPIVariables = []*PAPIVariable{
	{
		Name:        "BACKEND_EXPERT",
		Model:       "claude-3-5-sonnet-20241022",
		Temperature: 0.7,
		APIChannel:  "default",
		MCPTools:    []string{"filesystem", "linter", "debugger"},
		Prompt:      "You are a backend development expert specializing in Go, Python, and system architecture.",
		Category:    []string{"backend", "api", "database"},
	},
	{
		Name:        "FRONTEND_EXPERT",
		Model:       "gemini-2.0-flash",
		Temperature: 0.8,
		APIChannel:  "default",
		MCPTools:    []string{"filesystem", "browser"},
		Prompt:      "You are a frontend development expert specializing in React, Vue, and modern CSS.",
		Category:    []string{"frontend", "ui", "css"},
	},
	{
		Name:        "DEBUGGER",
		Model:       "o3-mini",
		Temperature: 0.5,
		APIChannel:  "default",
		MCPTools:    []string{"debugger", "profiler"},
		Prompt:      "You are a debugging expert with deep logical reasoning capabilities.",
		Category:    []string{"debug", "troubleshoot", "performance"},
	},
	{
		Name:        "DOC_WRITER",
		Model:       "claude-3-5-haiku-20241022",
		Temperature: 0.9,
		APIChannel:  "default",
		MCPTools:    []string{"filesystem"},
		Prompt:      "You are a technical documentation expert.",
		Category:    []string{"documentation", "readme", "comments"},
	},
}
