package ast

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// SimpleASTParser 简单AST解析器（基于正则表达式）
type SimpleASTParser struct {
	config   ParserConfig
	patterns map[SupportedLanguage]*languagePatterns
	mu       sync.RWMutex
}

// languagePatterns 语言特定的正则模式
type languagePatterns struct {
	Function  *regexp.Regexp
	Class     *regexp.Regexp
	Interface *regexp.Regexp
	Import    *regexp.Regexp
	Export    *regexp.Regexp
	Variable  *regexp.Regexp
	Constant  *regexp.Regexp
	Method    *regexp.Regexp
	Comment   *regexp.Regexp
	DocString *regexp.Regexp
}

// NewSimpleASTParser 创建简单AST解析器
func NewSimpleASTParser(config *ParserConfig) *SimpleASTParser {
	cfg := DefaultParserConfig
	if config != nil {
		cfg = *config
	}

	parser := &SimpleASTParser{
		config:   cfg,
		patterns: make(map[SupportedLanguage]*languagePatterns),
	}

	parser.initPatterns()
	return parser
}

// initPatterns 初始化语言模式
func (p *SimpleASTParser) initPatterns() {
	// TypeScript/JavaScript
	tsPatterns := &languagePatterns{
		Function:  regexp.MustCompile(`(?m)^[\t ]*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*\([^)]*\)`),
		Class:     regexp.MustCompile(`(?m)^[\t ]*(?:export\s+)?(?:abstract\s+)?class\s+(\w+)(?:\s+extends\s+\w+)?(?:\s+implements\s+[\w,\s]+)?`),
		Interface: regexp.MustCompile(`(?m)^[\t ]*(?:export\s+)?interface\s+(\w+)(?:\s+extends\s+[\w,\s]+)?`),
		Import:    regexp.MustCompile(`(?m)^[\t ]*import\s+(?:{[^}]+}|[\w*]+|\*\s+as\s+\w+)\s+from\s+['"]([^'"]+)['"]`),
		Export:    regexp.MustCompile(`(?m)^[\t ]*export\s+(?:default\s+)?(?:class|function|const|let|var|interface|type|enum)\s+(\w+)`),
		Variable:  regexp.MustCompile(`(?m)^[\t ]*(?:export\s+)?(?:const|let|var)\s+(\w+)\s*[=:]`),
		Constant:  regexp.MustCompile(`(?m)^[\t ]*(?:export\s+)?const\s+([A-Z][A-Z0-9_]*)\s*=`),
		Method:    regexp.MustCompile(`(?m)^[\t ]*(?:public|private|protected)?\s*(?:static\s+)?(?:async\s+)?(\w+)\s*\([^)]*\)\s*[:{]`),
		Comment:   regexp.MustCompile(`(?m)//.*$|/\*[\s\S]*?\*/`),
		DocString: regexp.MustCompile(`(?m)/\*\*[\s\S]*?\*/`),
	}
	p.patterns[LangTypeScript] = tsPatterns
	p.patterns[LangJavaScript] = tsPatterns

	// Go
	p.patterns[LangGo] = &languagePatterns{
		Function:  regexp.MustCompile(`(?m)^func\s+(?:\([^)]+\)\s+)?(\w+)\s*\([^)]*\)`),
		Class:     regexp.MustCompile(`(?m)^type\s+(\w+)\s+struct\s*{`),
		Interface: regexp.MustCompile(`(?m)^type\s+(\w+)\s+interface\s*{`),
		Import:    regexp.MustCompile(`(?m)^\s*(?:import\s+)?"([^"]+)"`),
		Export:    nil, // Go uses capitalization
		Variable:  regexp.MustCompile(`(?m)^(?:var|:=)\s+(\w+)\s*[=:]`),
		Constant:  regexp.MustCompile(`(?m)^const\s+(\w+)\s*=`),
		Method:    regexp.MustCompile(`(?m)^func\s+\((\w+)\s+\*?\w+\)\s+(\w+)\s*\(`),
		Comment:   regexp.MustCompile(`(?m)//.*$|/\*[\s\S]*?\*/`),
		DocString: regexp.MustCompile(`(?m)^//\s*\w+.*$`),
	}

	// Python
	p.patterns[LangPython] = &languagePatterns{
		Function:  regexp.MustCompile(`(?m)^[\t ]*(?:async\s+)?def\s+(\w+)\s*\(`),
		Class:     regexp.MustCompile(`(?m)^[\t ]*class\s+(\w+)(?:\([^)]*\))?:`),
		Interface: nil, // Python uses ABC
		Import:    regexp.MustCompile(`(?m)^[\t ]*(?:from\s+([\w.]+)\s+)?import\s+(.+)`),
		Export:    nil,
		Variable:  regexp.MustCompile(`(?m)^[\t ]*(\w+)\s*=`),
		Constant:  regexp.MustCompile(`(?m)^[\t ]*([A-Z][A-Z0-9_]*)\s*=`),
		Method:    regexp.MustCompile(`(?m)^[\t ]+def\s+(\w+)\s*\(self`),
		Comment:   regexp.MustCompile(`(?m)#.*$`),
		DocString: regexp.MustCompile(`(?m)"""[\s\S]*?"""|'''[\s\S]*?'''`),
	}

	// Java
	p.patterns[LangJava] = &languagePatterns{
		Function:  regexp.MustCompile(`(?m)^[\t ]*(?:public|private|protected)?\s*(?:static\s+)?[\w<>[\],\s]+\s+(\w+)\s*\([^)]*\)\s*(?:throws\s+[\w,\s]+)?\s*{`),
		Class:     regexp.MustCompile(`(?m)^[\t ]*(?:public\s+)?(?:abstract\s+)?(?:final\s+)?class\s+(\w+)`),
		Interface: regexp.MustCompile(`(?m)^[\t ]*(?:public\s+)?interface\s+(\w+)`),
		Import:    regexp.MustCompile(`(?m)^[\t ]*import\s+(?:static\s+)?([\w.]+);`),
		Export:    nil,
		Variable:  regexp.MustCompile(`(?m)^[\t ]*(?:public|private|protected)?\s*(?:static\s+)?[\w<>[\],\s]+\s+(\w+)\s*[=;]`),
		Constant:  regexp.MustCompile(`(?m)^[\t ]*(?:public|private)?\s*static\s+final\s+[\w<>[\],\s]+\s+([A-Z][A-Z0-9_]*)\s*=`),
		Method:    regexp.MustCompile(`(?m)^[\t ]*(?:public|private|protected)?\s*(?:static\s+)?[\w<>[\],\s]+\s+(\w+)\s*\([^)]*\)`),
		Comment:   regexp.MustCompile(`(?m)//.*$|/\*[\s\S]*?\*/`),
		DocString: regexp.MustCompile(`(?m)/\*\*[\s\S]*?\*/`),
	}
}

// Parse 解析代码
func (p *SimpleASTParser) Parse(ctx context.Context, code string, language SupportedLanguage) (*ParseResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	result := &ParseResult{
		Language:   language,
		Symbols:    make([]SymbolInfo, 0),
		Imports:    make([]ImportInfo, 0),
		Exports:    make([]ExportInfo, 0),
		TokenCount: p.CountTokens(code),
		LineCount:  strings.Count(code, "\n") + 1,
	}

	patterns := p.getPatterns(language)
	if patterns == nil {
		return result, nil
	}

	// 提取符号
	result.Symbols = p.extractSymbolsWithPatterns(code, patterns)

	// 提取导入
	result.Imports = p.extractImports(code, patterns, language)

	// 提取导出
	result.Exports = p.extractExports(code, patterns)

	return result, nil
}

// ParseFile 解析文件
func (p *SimpleASTParser) ParseFile(ctx context.Context, filePath string) (*ParseResult, error) {
	// 检测语言
	language := p.detectLanguage(filePath)

	// 读取文件
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// 检查文件大小
	if int64(len(content)) > p.config.MaxFileSize {
		return &ParseResult{
			Language: language,
			FilePath: filePath,
			Errors: []ParseError{{
				Message: "file too large",
			}},
		}, nil
	}

	result, err := p.Parse(ctx, string(content), language)
	if err != nil {
		return nil, err
	}
	result.FilePath = filePath

	return result, nil
}

// ExtractSymbols 提取符号
func (p *SimpleASTParser) ExtractSymbols(ctx context.Context, code string, language SupportedLanguage) ([]SymbolInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	patterns := p.getPatterns(language)
	if patterns == nil {
		return []SymbolInfo{}, nil
	}

	return p.extractSymbolsWithPatterns(code, patterns), nil
}

// CountTokens 统计Token数量（简单估算）
func (p *SimpleASTParser) CountTokens(code string) int {
	// 简单估算：按空白和标点分割
	// 更精确的实现应使用 tiktoken 或类似库
	tokens := 0
	inWord := false

	for _, r := range code {
		isWordChar := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_'

		if isWordChar {
			if !inWord {
				tokens++
				inWord = true
			}
		} else {
			inWord = false
			// 标点符号也算token
			if !isWhitespace(r) {
				tokens++
			}
		}
	}

	return tokens
}

// GetSupportedLanguages 获取支持的语言列表
func (p *SimpleASTParser) GetSupportedLanguages() []SupportedLanguage {
	return []SupportedLanguage{
		LangTypeScript,
		LangJavaScript,
		LangGo,
		LangPython,
		LangJava,
	}
}

// detectLanguage 检测语言
func (p *SimpleASTParser) detectLanguage(filePath string) SupportedLanguage {
	ext := strings.ToLower(filepath.Ext(filePath))
	if lang, ok := LanguageExtensions[ext]; ok {
		return lang
	}
	return LangUnknown
}

// getPatterns 获取语言模式
func (p *SimpleASTParser) getPatterns(language SupportedLanguage) *languagePatterns {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.patterns[language]
}

// extractSymbolsWithPatterns 使用模式提取符号
func (p *SimpleASTParser) extractSymbolsWithPatterns(code string, patterns *languagePatterns) []SymbolInfo {
	symbols := make([]SymbolInfo, 0)
	lines := strings.Split(code, "\n")

	// 提取函数
	if patterns.Function != nil {
		matches := patterns.Function.FindAllStringSubmatchIndex(code, -1)
		for _, match := range matches {
			if len(match) >= 4 {
				name := code[match[2]:match[3]]
				line := p.getLineNumber(code, match[0])
				symbols = append(symbols, SymbolInfo{
					Name: name,
					Kind: SymbolFunction,
					Range: Range{
						Start: Position{Line: line, Column: 0},
						End:   Position{Line: line, Column: len(lines[line-1])},
					},
					Signature: strings.TrimSpace(code[match[0]:match[1]]),
				})
			}
		}
	}

	// 提取类
	if patterns.Class != nil {
		matches := patterns.Class.FindAllStringSubmatchIndex(code, -1)
		for _, match := range matches {
			if len(match) >= 4 {
				name := code[match[2]:match[3]]
				line := p.getLineNumber(code, match[0])
				symbols = append(symbols, SymbolInfo{
					Name: name,
					Kind: SymbolClass,
					Range: Range{
						Start: Position{Line: line, Column: 0},
						End:   Position{Line: line, Column: len(lines[line-1])},
					},
				})
			}
		}
	}

	// 提取接口
	if patterns.Interface != nil {
		matches := patterns.Interface.FindAllStringSubmatchIndex(code, -1)
		for _, match := range matches {
			if len(match) >= 4 {
				name := code[match[2]:match[3]]
				line := p.getLineNumber(code, match[0])
				symbols = append(symbols, SymbolInfo{
					Name: name,
					Kind: SymbolInterface,
					Range: Range{
						Start: Position{Line: line, Column: 0},
						End:   Position{Line: line, Column: len(lines[line-1])},
					},
				})
			}
		}
	}

	// 提取常量
	if patterns.Constant != nil {
		matches := patterns.Constant.FindAllStringSubmatchIndex(code, -1)
		for _, match := range matches {
			if len(match) >= 4 {
				name := code[match[2]:match[3]]
				line := p.getLineNumber(code, match[0])
				symbols = append(symbols, SymbolInfo{
					Name: name,
					Kind: SymbolConstant,
					Range: Range{
						Start: Position{Line: line, Column: 0},
						End:   Position{Line: line, Column: len(lines[line-1])},
					},
				})
			}
		}
	}

	return symbols
}

// extractImports 提取导入
func (p *SimpleASTParser) extractImports(code string, patterns *languagePatterns, language SupportedLanguage) []ImportInfo {
	imports := make([]ImportInfo, 0)

	if patterns.Import == nil {
		return imports
	}

	matches := patterns.Import.FindAllStringSubmatchIndex(code, -1)
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		// 找到第一个有效的捕获组（某些语言可能有多个组）
		var source string
		for i := 2; i < len(match)-1; i += 2 {
			if match[i] >= 0 && match[i+1] >= 0 {
				source = code[match[i]:match[i+1]]
				break
			}
		}
		if source == "" {
			continue
		}
		line := p.getLineNumber(code, match[0])
		imports = append(imports, ImportInfo{
			Source: source,
			Range: Range{
				Start: Position{Line: line, Column: 0},
				End:   Position{Line: line, Column: 0},
			},
		})
	}

	return imports
}

// extractExports 提取导出
func (p *SimpleASTParser) extractExports(code string, patterns *languagePatterns) []ExportInfo {
	exports := make([]ExportInfo, 0)

	if patterns.Export == nil {
		return exports
	}

	matches := patterns.Export.FindAllStringSubmatchIndex(code, -1)
	for _, match := range matches {
		if len(match) >= 4 {
			name := code[match[2]:match[3]]
			line := p.getLineNumber(code, match[0])
			isDefault := strings.Contains(code[match[0]:match[1]], "default")
			exports = append(exports, ExportInfo{
				Name:      name,
				IsDefault: isDefault,
				Range: Range{
					Start: Position{Line: line, Column: 0},
					End:   Position{Line: line, Column: 0},
				},
			})
		}
	}

	return exports
}

// getLineNumber 获取行号
func (p *SimpleASTParser) getLineNumber(code string, offset int) int {
	return strings.Count(code[:offset], "\n") + 1
}

// ContextBuilder 上下文构建器实现
type ContextBuilder struct {
	parser     IASTParser
	windowSize int
}

// NewContextBuilder 创建上下文构建器
func NewContextBuilder(parser IASTParser, windowSize int) *ContextBuilder {
	if windowSize <= 0 {
		windowSize = 50 // 默认50行
	}
	return &ContextBuilder{
		parser:     parser,
		windowSize: windowSize,
	}
}

// BuildContext 构建上下文窗口
func (b *ContextBuilder) BuildContext(ctx context.Context, filePath string, targetLine int, windowSize int) (*ContextWindow, error) {
	if windowSize <= 0 {
		windowSize = b.windowSize
	}

	// 读取文件
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// 读取所有行
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	totalLines := len(lines)
	if totalLines == 0 {
		return &ContextWindow{FilePath: filePath}, nil
	}

	// 计算窗口范围
	startLine := targetLine - windowSize/2
	if startLine < 1 {
		startLine = 1
	}
	endLine := startLine + windowSize - 1
	if endLine > totalLines {
		endLine = totalLines
		startLine = endLine - windowSize + 1
		if startLine < 1 {
			startLine = 1
		}
	}

	// 提取内容
	var contentLines []string
	for i := startLine - 1; i < endLine && i < totalLines; i++ {
		contentLines = append(contentLines, lines[i])
	}
	content := strings.Join(contentLines, "\n")

	// 解析符号
	language := b.parser.(*SimpleASTParser).detectLanguage(filePath)
	symbols, _ := b.parser.ExtractSymbols(ctx, content, language)

	return &ContextWindow{
		FilePath:   filePath,
		StartLine:  startLine,
		EndLine:    endLine,
		Content:    content,
		Symbols:    symbols,
		TokenCount: b.parser.CountTokens(content),
	}, nil
}

// BuildSymbolContext 基于符号构建上下文
func (b *ContextBuilder) BuildSymbolContext(ctx context.Context, filePath string, symbolName string) (*ContextWindow, error) {
	// 解析文件
	result, err := b.parser.ParseFile(ctx, filePath)
	if err != nil {
		return nil, err
	}

	// 查找符号
	var targetSymbol *SymbolInfo
	for i := range result.Symbols {
		if result.Symbols[i].Name == symbolName {
			targetSymbol = &result.Symbols[i]
			break
		}
	}

	if targetSymbol == nil {
		// 符号未找到，返回文件开头
		return b.BuildContext(ctx, filePath, 1, b.windowSize)
	}

	// 以符号为中心构建上下文
	return b.BuildContext(ctx, filePath, targetSymbol.Range.Start.Line, b.windowSize)
}

// TruncateToTokenLimit 截断到Token限制
func (b *ContextBuilder) TruncateToTokenLimit(content string, maxTokens int) string {
	if maxTokens <= 0 {
		return content
	}

	currentTokens := b.parser.CountTokens(content)
	if currentTokens <= maxTokens {
		return content
	}

	// 按行截断
	lines := strings.Split(content, "\n")
	var result []string
	tokens := 0

	for _, line := range lines {
		lineTokens := b.parser.CountTokens(line)
		if tokens+lineTokens > maxTokens {
			break
		}
		result = append(result, line)
		tokens += lineTokens
	}

	return strings.Join(result, "\n")
}

// isWhitespace 判断是否为空白字符
func isWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// EstimateTokensGPT 估算GPT风格的token数量
func EstimateTokensGPT(text string) int {
	// GPT模型大约每4个字符一个token（英文）
	// 中文大约每个字符1-2个token
	charCount := utf8.RuneCountInString(text)
	return (charCount + 3) / 4
}
