package ast

import (
	"context"
)

// SupportedLanguage 支持的语言类型
type SupportedLanguage string

const (
	LangTypeScript SupportedLanguage = "typescript"
	LangJavaScript SupportedLanguage = "javascript"
	LangGo         SupportedLanguage = "go"
	LangPython     SupportedLanguage = "python"
	LangJava       SupportedLanguage = "java"
	LangRust       SupportedLanguage = "rust"
	LangCpp        SupportedLanguage = "cpp"
	LangC          SupportedLanguage = "c"
	LangUnknown    SupportedLanguage = "unknown"
)

// SymbolKind 符号类型
type SymbolKind string

const (
	SymbolFunction  SymbolKind = "function"
	SymbolClass     SymbolKind = "class"
	SymbolInterface SymbolKind = "interface"
	SymbolVariable  SymbolKind = "variable"
	SymbolConstant  SymbolKind = "constant"
	SymbolMethod    SymbolKind = "method"
	SymbolProperty  SymbolKind = "property"
	SymbolType      SymbolKind = "type"
	SymbolEnum      SymbolKind = "enum"
	SymbolModule    SymbolKind = "module"
	SymbolImport    SymbolKind = "import"
	SymbolExport    SymbolKind = "export"
)

// Position 位置信息
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
	Offset int `json:"offset"`
}

// Range 范围信息
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// ASTNode AST节点
type ASTNode struct {
	Type     string     `json:"type"`
	Name     string     `json:"name,omitempty"`
	Range    Range      `json:"range"`
	Children []*ASTNode `json:"children,omitempty"`
	Parent   *ASTNode   `json:"-"`
	Raw      string     `json:"raw,omitempty"`
}

// SymbolInfo 符号信息
type SymbolInfo struct {
	Name       string            `json:"name"`
	Kind       SymbolKind        `json:"kind"`
	Range      Range             `json:"range"`
	Signature  string            `json:"signature,omitempty"`
	DocComment string            `json:"doc_comment,omitempty"`
	Modifiers  []string          `json:"modifiers,omitempty"`
	Children   []SymbolInfo      `json:"children,omitempty"`
	References []Range           `json:"references,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// ParseResult 解析结果
type ParseResult struct {
	Language   SupportedLanguage `json:"language"`
	FilePath   string            `json:"file_path"`
	Root       *ASTNode          `json:"root,omitempty"`
	Symbols    []SymbolInfo      `json:"symbols"`
	Imports    []ImportInfo      `json:"imports"`
	Exports    []ExportInfo      `json:"exports"`
	TokenCount int               `json:"token_count"`
	LineCount  int               `json:"line_count"`
	Errors     []ParseError      `json:"errors,omitempty"`
}

// ImportInfo 导入信息
type ImportInfo struct {
	Source     string   `json:"source"`
	Specifiers []string `json:"specifiers,omitempty"`
	IsDefault  bool     `json:"is_default"`
	Range      Range    `json:"range"`
}

// ExportInfo 导出信息
type ExportInfo struct {
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
	Range     Range  `json:"range"`
}

// ParseError 解析错误
type ParseError struct {
	Message string `json:"message"`
	Range   Range  `json:"range"`
}

// ContextWindow 上下文窗口
type ContextWindow struct {
	FilePath   string       `json:"file_path"`
	StartLine  int          `json:"start_line"`
	EndLine    int          `json:"end_line"`
	Content    string       `json:"content"`
	Symbols    []SymbolInfo `json:"symbols"`
	TokenCount int          `json:"token_count"`
}

// IASTParser AST解析器接口
type IASTParser interface {
	// Parse 解析代码
	Parse(ctx context.Context, code string, language SupportedLanguage) (*ParseResult, error)

	// ParseFile 解析文件
	ParseFile(ctx context.Context, filePath string) (*ParseResult, error)

	// ExtractSymbols 提取符号
	ExtractSymbols(ctx context.Context, code string, language SupportedLanguage) ([]SymbolInfo, error)

	// CountTokens 统计Token数量
	CountTokens(code string) int

	// GetSupportedLanguages 获取支持的语言列表
	GetSupportedLanguages() []SupportedLanguage
}

// ParserConfig 解析器配置
type ParserConfig struct {
	MaxFileSize      int64    `json:"max_file_size"`
	IncludeComments  bool     `json:"include_comments"`
	ExtractDocString bool     `json:"extract_doc_string"`
	LanguageHints    []string `json:"language_hints,omitempty"`
}

// DefaultParserConfig 默认配置
var DefaultParserConfig = ParserConfig{
	MaxFileSize:      10 * 1024 * 1024, // 10MB
	IncludeComments:  true,
	ExtractDocString: true,
}

// LanguageExtensions 语言扩展名映射
var LanguageExtensions = map[string]SupportedLanguage{
	".ts":   LangTypeScript,
	".tsx":  LangTypeScript,
	".js":   LangJavaScript,
	".jsx":  LangJavaScript,
	".go":   LangGo,
	".py":   LangPython,
	".java": LangJava,
	".rs":   LangRust,
	".cpp":  LangCpp,
	".cc":   LangCpp,
	".c":    LangC,
	".h":    LangC,
}
