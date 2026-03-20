// Package context - Context service for file tree, AST parsing, and token calculation
package context

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// FileNode 文件树节点
type FileNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	Type     string      `json:"type"` // file, directory
	Size     int64       `json:"size,omitempty"`
	ModTime  int64       `json:"mod_time,omitempty"`
	Children []*FileNode `json:"children,omitempty"`
	Language string      `json:"language,omitempty"`
}

// FileTreeRequest 文件树请求
type FileTreeRequest struct {
	RootPath      string   `json:"root_path" binding:"required"`
	MaxDepth      int      `json:"max_depth"`
	IncludeHidden bool     `json:"include_hidden"`
	Extensions    []string `json:"extensions,omitempty"`
}

// FileTreeResponse 文件树响应
type FileTreeResponse struct {
	Root       *FileNode `json:"root"`
	TotalFiles int       `json:"total_files"`
	TotalDirs  int       `json:"total_dirs"`
	TotalSize  int64     `json:"total_size"`
}

// ASTParseRequest AST解析请求
type ASTParseRequest struct {
	FilePath string `json:"file_path"`
	Code     string `json:"code"`
	Language string `json:"language"`
}

// ASTParseResponse AST解析响应
type ASTParseResponse struct {
	Language   string       `json:"language"`
	FilePath   string       `json:"file_path,omitempty"`
	Symbols    []SymbolInfo `json:"symbols"`
	Imports    []ImportInfo `json:"imports"`
	Exports    []ExportInfo `json:"exports"`
	TokenCount int          `json:"token_count"`
	LineCount  int          `json:"line_count"`
	ParseTime  int64        `json:"parse_time_ms"`
}

// SymbolInfo 符号信息
type SymbolInfo struct {
	Name       string       `json:"name"`
	Kind       string       `json:"kind"` // function, class, interface, variable, constant, method, property, type, enum
	StartLine  int          `json:"start_line"`
	EndLine    int          `json:"end_line"`
	Signature  string       `json:"signature,omitempty"`
	DocComment string       `json:"doc_comment,omitempty"`
	Modifiers  []string     `json:"modifiers,omitempty"`
	Children   []SymbolInfo `json:"children,omitempty"`
}

// ImportInfo 导入信息
type ImportInfo struct {
	Source     string   `json:"source"`
	Specifiers []string `json:"specifiers,omitempty"`
	IsDefault  bool     `json:"is_default"`
	Line       int      `json:"line"`
}

// ExportInfo 导出信息
type ExportInfo struct {
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
	Line      int    `json:"line"`
}

// TokenCalculateRequest Token计算请求
type TokenCalculateRequest struct {
	Content string `json:"content" binding:"required"`
	Model   string `json:"model"`
}

// TokenCalculateResponse Token计算响应
type TokenCalculateResponse struct {
	TokenCount int    `json:"token_count"`
	CharCount  int    `json:"char_count"`
	WordCount  int    `json:"word_count"`
	LineCount  int    `json:"line_count"`
	Model      string `json:"model"`
}

// ContextPreset 上下文预设
type ContextPreset struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Paths       []string `json:"paths"`
	Extensions  []string `json:"extensions,omitempty"`
	MaxTokens   int      `json:"max_tokens"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

// PresetCreateRequest 预设创建请求
type PresetCreateRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description,omitempty"`
	Paths       []string `json:"paths" binding:"required"`
	Extensions  []string `json:"extensions,omitempty"`
	MaxTokens   int      `json:"max_tokens"`
}

// PresetListResponse 预设列表响应
type PresetListResponse struct {
	Presets []ContextPreset `json:"presets"`
	Total   int             `json:"total"`
}

// IContextService 上下文服务接口
type IContextService interface {
	GetFileTree(ctx context.Context, req *FileTreeRequest) (*FileTreeResponse, error)
	ParseAST(ctx context.Context, req *ASTParseRequest) (*ASTParseResponse, error)
	CalculateTokens(ctx context.Context, req *TokenCalculateRequest) (*TokenCalculateResponse, error)
	ListPresets(ctx context.Context) (*PresetListResponse, error)
	CreatePreset(ctx context.Context, req *PresetCreateRequest) (*ContextPreset, error)
	DeletePreset(ctx context.Context, id string) error
}

// InMemoryContextService 内存实现的上下文服务
type InMemoryContextService struct {
	mu      sync.RWMutex
	presets map[string]*ContextPreset
}

// NewInMemoryContextService 创建内存上下文服务
func NewInMemoryContextService() *InMemoryContextService {
	return &InMemoryContextService{
		presets: make(map[string]*ContextPreset),
	}
}

// SQLiteContextService SQLite 持久化实现，仅将 preset CRUD 落盘。
type SQLiteContextService struct {
	*InMemoryContextService
	db   *sql.DB
	dbMu sync.RWMutex
}

const createContextPresetTableSQL = `
CREATE TABLE IF NOT EXISTS context_presets (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT,
	paths_json TEXT NOT NULL,
	extensions_json TEXT NOT NULL DEFAULT '[]',
	max_tokens INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_context_presets_created_at
ON context_presets(created_at DESC, id DESC);
`

// NewSQLiteContextService 创建 SQLite 持久化上下文服务。
func NewSQLiteContextService(dbPath string) (*SQLiteContextService, error) {
	connStr, err := buildContextSQLiteConnString(dbPath)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	svc := &SQLiteContextService{
		InMemoryContextService: NewInMemoryContextService(),
		db:                     db,
	}

	if err := svc.initialize(); err != nil {
		db.Close()
		return nil, err
	}

	return svc, nil
}

func buildContextSQLiteConnString(dbPath string) (string, error) {
	if dbPath == "" || dbPath == ":memory:" {
		return "file::memory:?cache=shared&_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", nil
	}

	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create context db dir: %w", err)
		}
	}

	return dbPath + "?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", nil
}

func (s *SQLiteContextService) initialize() error {
	if _, err := s.db.Exec(createContextPresetTableSQL); err != nil {
		return fmt.Errorf("create context preset tables: %w", err)
	}
	return nil
}

// Close 关闭数据库连接。
func (s *SQLiteContextService) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// ListPresets 列出持久化预设。
func (s *SQLiteContextService) ListPresets(ctx context.Context) (*PresetListResponse, error) {
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, paths_json, extensions_json, max_tokens, created_at, updated_at
		FROM context_presets
		ORDER BY created_at DESC, updated_at DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list presets: %w", err)
	}
	defer rows.Close()

	presets := make([]ContextPreset, 0)
	for rows.Next() {
		preset, err := scanContextPreset(rows)
		if err != nil {
			return nil, err
		}
		presets = append(presets, *preset)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate presets: %w", err)
	}

	return &PresetListResponse{Presets: presets, Total: len(presets)}, nil
}

// CreatePreset 创建持久化预设。
func (s *SQLiteContextService) CreatePreset(ctx context.Context, req *PresetCreateRequest) (*ContextPreset, error) {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()

	now := time.Now().Unix()
	preset := &ContextPreset{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		Paths:       cloneStringSlice(req.Paths),
		Extensions:  cloneStringSlice(req.Extensions),
		MaxTokens:   req.MaxTokens,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	pathsJSON, err := marshalStringSlice(preset.Paths)
	if err != nil {
		return nil, fmt.Errorf("marshal preset paths: %w", err)
	}
	extensionsJSON, err := marshalStringSlice(preset.Extensions)
	if err != nil {
		return nil, fmt.Errorf("marshal preset extensions: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO context_presets (id, name, description, paths_json, extensions_json, max_tokens, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, preset.ID, preset.Name, preset.Description, pathsJSON, extensionsJSON, preset.MaxTokens, preset.CreatedAt, preset.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert preset: %w", err)
	}

	return preset, nil
}

// DeletePreset 删除持久化预设。
func (s *SQLiteContextService) DeletePreset(ctx context.Context, id string) error {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()

	if _, err := s.db.ExecContext(ctx, `DELETE FROM context_presets WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete preset: %w", err)
	}
	return nil
}

func scanContextPreset(scanner interface{ Scan(dest ...any) error }) (*ContextPreset, error) {
	var preset ContextPreset
	var description sql.NullString
	var pathsJSON string
	var extensionsJSON string

	if err := scanner.Scan(
		&preset.ID,
		&preset.Name,
		&description,
		&pathsJSON,
		&extensionsJSON,
		&preset.MaxTokens,
		&preset.CreatedAt,
		&preset.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan preset: %w", err)
	}

	preset.Description = description.String

	paths, err := unmarshalStringSlice(pathsJSON)
	if err != nil {
		return nil, fmt.Errorf("decode preset paths: %w", err)
	}
	extensions, err := unmarshalStringSlice(extensionsJSON)
	if err != nil {
		return nil, fmt.Errorf("decode preset extensions: %w", err)
	}
	preset.Paths = paths
	preset.Extensions = extensions

	return &preset, nil
}

func marshalStringSlice(values []string) (string, error) {
	if len(values) == 0 {
		return "[]", nil
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalStringSlice(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	return values, nil
}

func cloneStringSlice(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

// GetFileTree 获取文件树
func (s *InMemoryContextService) GetFileTree(ctx context.Context, req *FileTreeRequest) (*FileTreeResponse, error) {
	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 10
	}

	rootPath := req.RootPath
	info, err := os.Stat(rootPath)
	if err != nil {
		return nil, err
	}

	var totalFiles, totalDirs int
	var totalSize int64

	root := s.buildFileNode(rootPath, info, 0, maxDepth, req.IncludeHidden, req.Extensions, &totalFiles, &totalDirs, &totalSize)

	return &FileTreeResponse{
		Root:       root,
		TotalFiles: totalFiles,
		TotalDirs:  totalDirs,
		TotalSize:  totalSize,
	}, nil
}

// buildFileNode 递归构建文件节点
func (s *InMemoryContextService) buildFileNode(path string, info os.FileInfo, depth, maxDepth int, includeHidden bool, extensions []string, totalFiles, totalDirs *int, totalSize *int64) *FileNode {
	name := info.Name()

	// 跳过隐藏文件
	if !includeHidden && strings.HasPrefix(name, ".") {
		return nil
	}

	node := &FileNode{
		Name:    name,
		Path:    path,
		ModTime: info.ModTime().Unix(),
	}

	if info.IsDir() {
		node.Type = "directory"
		*totalDirs++

		if depth < maxDepth {
			entries, err := os.ReadDir(path)
			if err == nil {
				for _, entry := range entries {
					childInfo, err := entry.Info()
					if err != nil {
						continue
					}
					childPath := filepath.Join(path, entry.Name())
					childNode := s.buildFileNode(childPath, childInfo, depth+1, maxDepth, includeHidden, extensions, totalFiles, totalDirs, totalSize)
					if childNode != nil {
						node.Children = append(node.Children, childNode)
					}
				}
				// 排序：目录在前，文件在后，同类型按名称排序
				sort.Slice(node.Children, func(i, j int) bool {
					if node.Children[i].Type != node.Children[j].Type {
						return node.Children[i].Type == "directory"
					}
					return node.Children[i].Name < node.Children[j].Name
				})
			}
		}
	} else {
		node.Type = "file"
		node.Size = info.Size()
		*totalFiles++
		*totalSize += info.Size()

		// 检查扩展名过滤
		ext := strings.ToLower(filepath.Ext(name))
		if len(extensions) > 0 {
			found := false
			for _, e := range extensions {
				if ext == strings.ToLower(e) || ext == "."+strings.ToLower(e) {
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		}

		node.Language = s.detectLanguage(ext)
	}

	return node
}

// detectLanguage 检测语言
func (s *InMemoryContextService) detectLanguage(ext string) string {
	langMap := map[string]string{
		".ts":    "typescript",
		".tsx":   "typescript",
		".js":    "javascript",
		".jsx":   "javascript",
		".go":    "go",
		".py":    "python",
		".java":  "java",
		".rs":    "rust",
		".cpp":   "cpp",
		".cc":    "cpp",
		".c":     "c",
		".h":     "c",
		".rb":    "ruby",
		".php":   "php",
		".cs":    "csharp",
		".swift": "swift",
		".kt":    "kotlin",
		".scala": "scala",
		".md":    "markdown",
		".json":  "json",
		".yaml":  "yaml",
		".yml":   "yaml",
		".xml":   "xml",
		".html":  "html",
		".css":   "css",
		".scss":  "scss",
		".sql":   "sql",
		".sh":    "shell",
		".bash":  "shell",
	}
	if lang, ok := langMap[ext]; ok {
		return lang
	}
	return "unknown"
}

// ParseAST 解析AST
func (s *InMemoryContextService) ParseAST(ctx context.Context, req *ASTParseRequest) (*ASTParseResponse, error) {
	start := time.Now()

	code := req.Code
	if code == "" && req.FilePath != "" {
		data, err := os.ReadFile(req.FilePath)
		if err != nil {
			return nil, err
		}
		code = string(data)
	}

	language := req.Language
	if language == "" && req.FilePath != "" {
		ext := strings.ToLower(filepath.Ext(req.FilePath))
		language = s.detectLanguage(ext)
	}

	// 简化的AST解析（基于正则匹配）
	symbols := s.extractSymbols(code, language)
	imports := s.extractImports(code, language)
	exports := s.extractExports(code, language)

	lines := strings.Split(code, "\n")
	tokenCount := s.countTokens(code)

	return &ASTParseResponse{
		Language:   language,
		FilePath:   req.FilePath,
		Symbols:    symbols,
		Imports:    imports,
		Exports:    exports,
		TokenCount: tokenCount,
		LineCount:  len(lines),
		ParseTime:  time.Since(start).Milliseconds(),
	}, nil
}

// extractSymbols 提取符号（简化实现）
func (s *InMemoryContextService) extractSymbols(code, language string) []SymbolInfo {
	var symbols []SymbolInfo
	lines := strings.Split(code, "\n")

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		switch language {
		case "go":
			if strings.HasPrefix(trimmed, "func ") {
				name := s.extractFuncName(trimmed, "go")
				if name != "" {
					symbols = append(symbols, SymbolInfo{
						Name:      name,
						Kind:      "function",
						StartLine: lineNum,
						EndLine:   lineNum,
						Signature: trimmed,
					})
				}
			} else if strings.HasPrefix(trimmed, "type ") && strings.Contains(trimmed, "struct") {
				name := s.extractTypeName(trimmed)
				if name != "" {
					symbols = append(symbols, SymbolInfo{
						Name:      name,
						Kind:      "class",
						StartLine: lineNum,
						EndLine:   lineNum,
					})
				}
			} else if strings.HasPrefix(trimmed, "type ") && strings.Contains(trimmed, "interface") {
				name := s.extractTypeName(trimmed)
				if name != "" {
					symbols = append(symbols, SymbolInfo{
						Name:      name,
						Kind:      "interface",
						StartLine: lineNum,
						EndLine:   lineNum,
					})
				}
			}
		case "typescript", "javascript":
			if strings.HasPrefix(trimmed, "function ") || strings.Contains(trimmed, "=> {") {
				name := s.extractFuncName(trimmed, language)
				if name != "" {
					symbols = append(symbols, SymbolInfo{
						Name:      name,
						Kind:      "function",
						StartLine: lineNum,
						EndLine:   lineNum,
						Signature: trimmed,
					})
				}
			} else if strings.HasPrefix(trimmed, "class ") {
				name := s.extractClassName(trimmed)
				if name != "" {
					symbols = append(symbols, SymbolInfo{
						Name:      name,
						Kind:      "class",
						StartLine: lineNum,
						EndLine:   lineNum,
					})
				}
			} else if strings.HasPrefix(trimmed, "interface ") {
				name := s.extractInterfaceName(trimmed)
				if name != "" {
					symbols = append(symbols, SymbolInfo{
						Name:      name,
						Kind:      "interface",
						StartLine: lineNum,
						EndLine:   lineNum,
					})
				}
			}
		case "python":
			if strings.HasPrefix(trimmed, "def ") {
				name := s.extractFuncName(trimmed, "python")
				if name != "" {
					symbols = append(symbols, SymbolInfo{
						Name:      name,
						Kind:      "function",
						StartLine: lineNum,
						EndLine:   lineNum,
						Signature: trimmed,
					})
				}
			} else if strings.HasPrefix(trimmed, "class ") {
				name := s.extractClassName(trimmed)
				if name != "" {
					symbols = append(symbols, SymbolInfo{
						Name:      name,
						Kind:      "class",
						StartLine: lineNum,
						EndLine:   lineNum,
					})
				}
			}
		}
	}

	return symbols
}

// extractFuncName 提取函数名
func (s *InMemoryContextService) extractFuncName(line, language string) string {
	switch language {
	case "go":
		// func Name(...) or func (r *Receiver) Name(...)
		line = strings.TrimPrefix(line, "func ")
		if strings.HasPrefix(line, "(") {
			// 方法
			idx := strings.Index(line, ")")
			if idx > 0 {
				line = strings.TrimSpace(line[idx+1:])
			}
		}
		idx := strings.Index(line, "(")
		if idx > 0 {
			return strings.TrimSpace(line[:idx])
		}
	case "typescript", "javascript":
		line = strings.TrimPrefix(line, "function ")
		line = strings.TrimPrefix(line, "async ")
		idx := strings.Index(line, "(")
		if idx > 0 {
			return strings.TrimSpace(line[:idx])
		}
		// Arrow function: const name = (...) =>
		if strings.Contains(line, "=") && strings.Contains(line, "=>") {
			parts := strings.Split(line, "=")
			if len(parts) > 0 {
				name := strings.TrimSpace(parts[0])
				name = strings.TrimPrefix(name, "const ")
				name = strings.TrimPrefix(name, "let ")
				name = strings.TrimPrefix(name, "var ")
				name = strings.TrimPrefix(name, "export ")
				return strings.TrimSpace(name)
			}
		}
	case "python":
		line = strings.TrimPrefix(line, "def ")
		line = strings.TrimPrefix(line, "async ")
		idx := strings.Index(line, "(")
		if idx > 0 {
			return strings.TrimSpace(line[:idx])
		}
	}
	return ""
}

// extractTypeName 提取类型名
func (s *InMemoryContextService) extractTypeName(line string) string {
	line = strings.TrimPrefix(line, "type ")
	parts := strings.Fields(line)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// extractClassName 提取类名
func (s *InMemoryContextService) extractClassName(line string) string {
	line = strings.TrimPrefix(line, "class ")
	line = strings.TrimPrefix(line, "export ")
	idx := strings.IndexAny(line, " {(:")
	if idx > 0 {
		return strings.TrimSpace(line[:idx])
	}
	return strings.TrimSpace(line)
}

// extractInterfaceName 提取接口名
func (s *InMemoryContextService) extractInterfaceName(line string) string {
	line = strings.TrimPrefix(line, "interface ")
	line = strings.TrimPrefix(line, "export ")
	idx := strings.IndexAny(line, " {<")
	if idx > 0 {
		return strings.TrimSpace(line[:idx])
	}
	return strings.TrimSpace(line)
}

// extractImports 提取导入
func (s *InMemoryContextService) extractImports(code, language string) []ImportInfo {
	var imports []ImportInfo
	lines := strings.Split(code, "\n")

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		switch language {
		case "go":
			if strings.HasPrefix(trimmed, "import ") {
				// 单行 import "xxx"
				if strings.Contains(trimmed, "\"") {
					start := strings.Index(trimmed, "\"")
					end := strings.LastIndex(trimmed, "\"")
					if start < end {
						imports = append(imports, ImportInfo{
							Source: trimmed[start+1 : end],
							Line:   lineNum,
						})
					}
				}
			}
		case "typescript", "javascript":
			if strings.HasPrefix(trimmed, "import ") {
				// import xxx from 'yyy'
				if strings.Contains(trimmed, "from") {
					parts := strings.Split(trimmed, "from")
					if len(parts) == 2 {
						source := strings.Trim(strings.TrimSpace(parts[1]), "'\";")
						specPart := strings.TrimPrefix(strings.TrimSpace(parts[0]), "import ")
						specPart = strings.Trim(specPart, "{} ")
						var specs []string
						if specPart != "" && specPart != "*" {
							for _, sp := range strings.Split(specPart, ",") {
								sp = strings.TrimSpace(sp)
								if sp != "" {
									specs = append(specs, sp)
								}
							}
						}
						imports = append(imports, ImportInfo{
							Source:     source,
							Specifiers: specs,
							IsDefault:  !strings.Contains(parts[0], "{"),
							Line:       lineNum,
						})
					}
				}
			}
		case "python":
			if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "from ") {
				if strings.HasPrefix(trimmed, "from ") {
					// from xxx import yyy
					parts := strings.Split(trimmed, " import ")
					if len(parts) == 2 {
						source := strings.TrimPrefix(strings.TrimSpace(parts[0]), "from ")
						var specs []string
						for _, sp := range strings.Split(parts[1], ",") {
							sp = strings.TrimSpace(sp)
							if sp != "" {
								specs = append(specs, sp)
							}
						}
						imports = append(imports, ImportInfo{
							Source:     source,
							Specifiers: specs,
							Line:       lineNum,
						})
					}
				} else {
					// import xxx
					source := strings.TrimPrefix(trimmed, "import ")
					imports = append(imports, ImportInfo{
						Source: strings.TrimSpace(source),
						Line:   lineNum,
					})
				}
			}
		}
	}

	return imports
}

// extractExports 提取导出
func (s *InMemoryContextService) extractExports(code, language string) []ExportInfo {
	var exports []ExportInfo
	lines := strings.Split(code, "\n")

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		switch language {
		case "typescript", "javascript":
			if strings.HasPrefix(trimmed, "export ") {
				isDefault := strings.Contains(trimmed, "export default")
				name := ""
				if isDefault {
					rest := strings.TrimPrefix(trimmed, "export default ")
					idx := strings.IndexAny(rest, " ({;")
					if idx > 0 {
						name = rest[:idx]
					} else {
						name = rest
					}
				} else {
					rest := strings.TrimPrefix(trimmed, "export ")
					rest = strings.TrimPrefix(rest, "const ")
					rest = strings.TrimPrefix(rest, "let ")
					rest = strings.TrimPrefix(rest, "function ")
					rest = strings.TrimPrefix(rest, "class ")
					rest = strings.TrimPrefix(rest, "interface ")
					rest = strings.TrimPrefix(rest, "type ")
					idx := strings.IndexAny(rest, " =({<:")
					if idx > 0 {
						name = rest[:idx]
					} else {
						name = rest
					}
				}
				if name != "" && name != "{" {
					exports = append(exports, ExportInfo{
						Name:      strings.TrimSpace(name),
						IsDefault: isDefault,
						Line:      lineNum,
					})
				}
			}
		}
	}

	return exports
}

// CalculateTokens 计算Token数量
func (s *InMemoryContextService) CalculateTokens(ctx context.Context, req *TokenCalculateRequest) (*TokenCalculateResponse, error) {
	content := req.Content
	tokenCount := s.countTokens(content)
	charCount := utf8.RuneCountInString(content)
	wordCount := len(strings.Fields(content))
	lineCount := len(strings.Split(content, "\n"))

	model := req.Model
	if model == "" {
		model = "cl100k_base" // GPT-4/Claude default
	}

	return &TokenCalculateResponse{
		TokenCount: tokenCount,
		CharCount:  charCount,
		WordCount:  wordCount,
		LineCount:  lineCount,
		Model:      model,
	}, nil
}

// countTokens 简化的Token计数（基于字符和词的估算）
func (s *InMemoryContextService) countTokens(content string) int {
	// 简化估算：约4个字符 = 1个token（英文）
	// 中文约1.5个字符 = 1个token
	charCount := utf8.RuneCountInString(content)
	wordCount := len(strings.Fields(content))

	// 混合估算
	estimate := (charCount / 4) + (wordCount / 2)
	if estimate < wordCount {
		estimate = wordCount
	}

	return estimate
}

// ListPresets 列出预设
func (s *InMemoryContextService) ListPresets(ctx context.Context) (*PresetListResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	presets := make([]ContextPreset, 0, len(s.presets))
	for _, p := range s.presets {
		preset := *p
		preset.Paths = cloneStringSlice(p.Paths)
		preset.Extensions = cloneStringSlice(p.Extensions)
		presets = append(presets, preset)
	}

	sort.Slice(presets, func(i, j int) bool {
		if presets[i].CreatedAt != presets[j].CreatedAt {
			return presets[i].CreatedAt > presets[j].CreatedAt
		}
		if presets[i].UpdatedAt != presets[j].UpdatedAt {
			return presets[i].UpdatedAt > presets[j].UpdatedAt
		}
		return presets[i].ID > presets[j].ID
	})

	return &PresetListResponse{
		Presets: presets,
		Total:   len(presets),
	}, nil
}

// CreatePreset 创建预设
func (s *InMemoryContextService) CreatePreset(ctx context.Context, req *PresetCreateRequest) (*ContextPreset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	preset := &ContextPreset{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		Paths:       cloneStringSlice(req.Paths),
		Extensions:  cloneStringSlice(req.Extensions),
		MaxTokens:   req.MaxTokens,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.presets[preset.ID] = preset
	return preset, nil
}

// DeletePreset 删除预设
func (s *InMemoryContextService) DeletePreset(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.presets, id)
	return nil
}

// 全局服务实例
var defaultContextService IContextService

// GetContextService 获取上下文服务实例
func GetContextService() IContextService {
	if defaultContextService == nil {
		defaultContextService = NewInMemoryContextService()
	}
	return defaultContextService
}

// HasContextService reports whether the global context service has been configured.
func HasContextService() bool {
	return defaultContextService != nil
}

// SetContextService 设置上下文服务实例
func SetContextService(svc IContextService) {
	defaultContextService = svc
}
