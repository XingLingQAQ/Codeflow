// Package config - PAPI (Prompt AI-Profile Interface) system
package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// PAPIVariable PAPI变量定义
type PAPIVariable struct {
	Name        string   `json:"name"`        // 变量名（如 BACKEND_EXPERT）
	Model       string   `json:"model"`       // 目标模型
	Temperature float64  `json:"temperature"` // 温度
	APIChannel  string   `json:"api_channel"` // API通道
	MCPTools    []string `json:"mcp_tools"`   // MCP工具列表
	Prompt      string   `json:"prompt"`      // System Prompt
	Category    []string `json:"category"`    // 适用任务类别（如 backend, frontend, debug）
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
//
// This is the bare in-memory primitive: it stores a deep copy of the caller's
// variable verbatim (categories are NOT normalized or conflict-checked here;
// every comparison site applies NormalizeCategory instead). The durable write
// path is SQLiteConfigService.DefinePAPIVariable, which validates a candidate
// snapshot through BuildCandidateMapping before publishing.
func (p *PAPIManager) DefineVariable(variable *PAPIVariable) error {
	if variable == nil {
		return fmt.Errorf("variable cannot be nil")
	}
	if variable.Name == "" {
		return fmt.Errorf("variable name cannot be empty")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.mapping.Variables[variable.Name] = clonePAPIVariable(variable)
	return nil
}

// GetVariable 获取PAPI变量
func (p *PAPIManager) GetVariable(name string) (*PAPIVariable, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if variable, ok := p.mapping.Variables[name]; ok {
		return clonePAPIVariable(variable), nil
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
		variables = append(variables, clonePAPIVariable(v))
	}
	return variables
}

// ResolveByCategory 根据任务类别解析PAPI变量
//
// The query and every variable's categories go through the single
// normalization rule (NormalizeCategory, E-10). Resolution is deterministic:
// variable names are scanned in sorted order, and a category claimed by more
// than one variable yields a *CategoryConflictError instead of whichever
// candidate Go map iteration happened to visit first.
func (p *PAPIManager) ResolveByCategory(category string) (*PAPIVariable, error) {
	normalized := NormalizeCategory([]string{category})
	if len(normalized) == 0 {
		return nil, fmt.Errorf("category cannot be empty")
	}
	target := normalized[0]

	p.mu.RLock()
	defer p.mu.RUnlock()

	names := make([]string, 0, len(p.mapping.Variables))
	for name := range p.mapping.Variables {
		names = append(names, name)
	}
	sort.Strings(names)
	claimants := make([]string, 0, 2)
	for _, name := range names {
		variable := p.mapping.Variables[name]
		if variable == nil {
			continue
		}
		for _, cat := range NormalizeCategory(variable.Category) {
			if cat == target {
				claimants = append(claimants, name)
				break
			}
		}
	}
	switch len(claimants) {
	case 0:
		return nil, fmt.Errorf("no variable found for category: %s", category)
	case 1:
		return clonePAPIVariable(p.mapping.Variables[claimants[0]]), nil
	default:
		return nil, &CategoryConflictError{Category: target, Variables: claimants}
	}
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
//
// Like DefineVariable this is the bare in-memory primitive; the durable path
// is SQLiteConfigService.HotSwapPAPI.
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
	replacement := clonePAPIVariable(newVariable)
	replacement.Name = varName
	p.mapping.Variables[varName] = replacement

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
//
// Category claims are compared after NormalizeCategory, the same rule used by
// create/update validation and ResolveByCategory (E-10): a "backend" vs
// "Backend" clash is reported here exactly as it is rejected on write and
// refused on resolve. The output is deterministic (categories and claimants
// sorted), which also makes it suitable as the load-time diagnostics record.
func (p *PAPIManager) DetectConflicts() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	claims := make(map[string][]string)
	names := make([]string, 0, len(p.mapping.Variables))
	for name := range p.mapping.Variables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		variable := p.mapping.Variables[name]
		if variable == nil {
			continue
		}
		for _, cat := range NormalizeCategory(variable.Category) {
			claims[cat] = append(claims[cat], name)
		}
	}

	categories := make([]string, 0, len(claims))
	for cat := range claims {
		categories = append(categories, cat)
	}
	sort.Strings(categories)
	var conflicts []string
	for _, cat := range categories {
		holders := claims[cat]
		if len(holders) > 1 {
			conflicts = append(conflicts, (&CategoryConflictError{Category: cat, Variables: holders}).Error())
		}
	}
	return conflicts
}

// GetMapping 获取完整的PAPI映射
func (p *PAPIManager) GetMapping() *PAPIMapping {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return clonePAPIMapping(&p.mapping)
}

// LoadMapping 加载PAPI映射
//
// The load path applies the same normalization rule as every other
// comparison site: each variable's categories are replaced by
// NormalizeCategory output as the mapping becomes live. Loading never rejects
// and never drops user data — a persisted category conflict stays visible in
// the live mapping, resolvable for non-conflicted categories, and is reported
// by DetectConflicts (E-10). The input is deep-copied, so later caller
// mutation cannot leak into the live snapshot.
func (p *PAPIManager) LoadMapping(mapping *PAPIMapping) error {
	if mapping == nil {
		return fmt.Errorf("mapping cannot be nil")
	}

	clone := clonePAPIMapping(mapping)
	for _, variable := range clone.Variables {
		if variable == nil {
			continue
		}
		variable.Category = NormalizeCategory(variable.Category)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.mapping = *clone
	if p.mapping.Variables == nil {
		p.mapping.Variables = make(map[string]*PAPIVariable)
	}

	return nil
}

// publishMapping replaces the live mapping with a deep copy of an already
// validated candidate produced by BuildCandidateMapping. It performs no
// validation itself: callers must publish only after the candidate's durable
// write has committed, so the visible snapshot never runs ahead of persisted
// state (E-09).
func (p *PAPIManager) publishMapping(candidate *PAPIMapping) {
	clone := clonePAPIMapping(candidate)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mapping = *clone
	if p.mapping.Variables == nil {
		p.mapping.Variables = make(map[string]*PAPIVariable)
	}
}

// clonePAPIVariable deep-copies a variable including its slices, so values
// handed out by getters can never mutate the manager's live mapping (E-09).
func clonePAPIVariable(v *PAPIVariable) *PAPIVariable {
	if v == nil {
		return nil
	}
	cp := *v
	cp.MCPTools = append([]string(nil), v.MCPTools...)
	cp.Category = append([]string(nil), v.Category...)
	return &cp
}

func clonePAPIMapping(m *PAPIMapping) *PAPIMapping {
	out := &PAPIMapping{Variables: make(map[string]*PAPIVariable, len(m.Variables))}
	for name, v := range m.Variables {
		out.Variables[name] = clonePAPIVariable(v)
	}
	return out
}

// NormalizeCategory trims, lowercases, drops empty labels, and dedups a
// category list, preserving first-occurrence order. The result is always
// non-nil, so "normalized to empty" stays distinguishable from "unset". This
// is the single PAPI category-comparison rule (E-10): conflict detection,
// create/update validation, and resolution must all agree on it.
func NormalizeCategory(categories []string) []string {
	out := make([]string, 0, len(categories))
	seen := make(map[string]struct{}, len(categories))
	for _, cat := range categories {
		normalized := strings.ToLower(strings.TrimSpace(cat))
		if normalized == "" {
			continue
		}
		if _, dup := seen[normalized]; dup {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

// CategoryConflictError reports a normalized PAPI category claimed by more
// than one variable. It is the typed signal for create/update validation
// (mapped to 409 at the API layer) and for resolve diagnostics (E-10).
type CategoryConflictError struct {
	Category  string
	Variables []string
}

func (e *CategoryConflictError) Error() string {
	return fmt.Sprintf("papi category %q has multiple variables: %v", e.Category, e.Variables)
}

// BuildCategoryIndex maps each normalized category to the single variable
// that claims it. Variable names are processed in sorted order so both the
// index and any conflict are deterministic regardless of Go map iteration
// order. A category claimed by two different variables yields a
// *CategoryConflictError naming all claimants in sorted order; duplicate
// categories within one variable never self-conflict because each variable's
// list is normalized first.
func BuildCategoryIndex(variables map[string]*PAPIVariable) (map[string]string, error) {
	names := make([]string, 0, len(variables))
	for name := range variables {
		names = append(names, name)
	}
	sort.Strings(names)
	claims := make(map[string][]string)
	for _, name := range names {
		variable := variables[name]
		if variable == nil {
			continue
		}
		for _, cat := range NormalizeCategory(variable.Category) {
			claims[cat] = append(claims[cat], name)
		}
	}
	cats := make([]string, 0, len(claims))
	for cat := range claims {
		cats = append(cats, cat)
	}
	sort.Strings(cats)
	index := make(map[string]string, len(claims))
	for _, cat := range cats {
		holders := claims[cat]
		if len(holders) > 1 {
			return nil, &CategoryConflictError{Category: cat, Variables: holders}
		}
		index[cat] = holders[0]
	}
	return index, nil
}

// BuildCandidateMapping deep-copies the live mapping, applies mutate to the
// copy, normalizes every variable's categories, and validates category
// uniqueness. The live mapping is never modified and nothing is published:
// persisting and then publishing a validated candidate is the caller's
// separate step (wired with the mutation lock and SQL transaction in
// T13.03.b). A category claimed by two variables yields a
// *CategoryConflictError; a non-nil mutate error aborts the candidate
// unchanged.
func (p *PAPIManager) BuildCandidateMapping(mutate func(*PAPIMapping) error) (*PAPIMapping, error) {
	p.mu.RLock()
	candidate := clonePAPIMapping(&p.mapping)
	p.mu.RUnlock()
	if mutate != nil {
		if err := mutate(candidate); err != nil {
			return nil, err
		}
	}
	for _, variable := range candidate.Variables {
		if variable == nil {
			continue
		}
		variable.Category = NormalizeCategory(variable.Category)
	}
	if _, err := BuildCategoryIndex(candidate.Variables); err != nil {
		return nil, err
	}
	return candidate, nil
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
