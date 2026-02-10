package shadow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const charsPerToken = 4

// LoadedContext 已加载的上下文。
type LoadedContext struct {
	FilePath   string `json:"file_path"`
	Content    string `json:"content"`
	TokenCount int    `json:"token_count"`
}

// ContextLoadResult 上下文加载结果。
type ContextLoadResult struct {
	Contexts        []LoadedContext `json:"contexts"`
	TotalTokens     int            `json:"total_tokens"`
	BudgetRemaining int            `json:"budget_remaining"`
}

// ContextLoaderConfig 上下文加载器配置。
type ContextLoaderConfig struct {
	MaxTokenBudget int
	CacheEnabled   bool
	ShadowRoot     string
	ProjectRoot    string
}

type cacheEntry struct {
	content    string
	tokenCount int
	accessedAt int64
}

// ContextLoader 上下文聚焦加载器。
type ContextLoader struct {
	mu           sync.RWMutex
	config       ContextLoaderConfig
	cache        map[string]*cacheEntry
	maxCacheSize int
}

// NewContextLoader 创建上下文加载器实例。
func NewContextLoader(config *ContextLoaderConfig) *ContextLoader {
	cfg := ContextLoaderConfig{
		MaxTokenBudget: 8000,
		CacheEnabled:   true,
		ShadowRoot:     ".codeflow",
		ProjectRoot:    ".",
	}
	if config != nil {
		if config.MaxTokenBudget > 0 {
			cfg.MaxTokenBudget = config.MaxTokenBudget
		}
		if config.ShadowRoot != "" {
			cfg.ShadowRoot = config.ShadowRoot
		}
		if config.ProjectRoot != "" {
			cfg.ProjectRoot = config.ProjectRoot
		}
		cfg.CacheEnabled = config.CacheEnabled
	}
	return &ContextLoader{
		config:       cfg,
		cache:        make(map[string]*cacheEntry),
		maxCacheSize: 100,
	}
}

// LoadContext 根据意图语义搜索 .codeflow 文档，返回 token 预算内的相关文档。
func (l *ContextLoader) LoadContext(intent string) (*ContextLoadResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	domainDir := filepath.Join(l.config.ProjectRoot, l.config.ShadowRoot, "domain")
	intentFiles, err := l.scanIntentFiles(domainDir)
	if err != nil {
		return &ContextLoadResult{
			BudgetRemaining: l.config.MaxTokenBudget,
		}, nil
	}

	keywords := extractKeywords(intent)
	if len(keywords) == 0 {
		return &ContextLoadResult{
			BudgetRemaining: l.config.MaxTokenBudget,
		}, nil
	}

	type scoredFile struct {
		filePath string
		content  string
		score    float64
	}

	var scored []scoredFile
	for _, fp := range intentFiles {
		content, err := l.readWithCache(fp)
		if err != nil || content == "" {
			continue
		}
		fileKw := extractKeywords(content)
		score := l.computeRelevance(keywords, fileKw)
		if score > 0 {
			scored = append(scored, scoredFile{filePath: fp, content: content, score: score})
		}
	}

	// 按分数降序排序
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	var contexts []LoadedContext
	totalTokens := 0

	for _, item := range scored {
		tokenCount := estimateTokens(item.content)
		if totalTokens+tokenCount > l.config.MaxTokenBudget {
			remaining := l.config.MaxTokenBudget - totalTokens
			if remaining > 100 {
				truncLen := remaining * charsPerToken
				if truncLen > len(item.content) {
					truncLen = len(item.content)
				}
				truncated := item.content[:truncLen]
				truncTokens := estimateTokens(truncated)
				contexts = append(contexts, LoadedContext{
					FilePath:   item.filePath,
					Content:    truncated,
					TokenCount: truncTokens,
				})
				totalTokens += truncTokens
			}
			break
		}

		contexts = append(contexts, LoadedContext{
			FilePath:   item.filePath,
			Content:    item.content,
			TokenCount: tokenCount,
		})
		totalTokens += tokenCount
	}

	return &ContextLoadResult{
		Contexts:        contexts,
		TotalTokens:     totalTokens,
		BudgetRemaining: l.config.MaxTokenBudget - totalTokens,
	}, nil
}

// LoadWithDependencies 加载源文件的意图文档及其依赖的意图文档。
func (l *ContextLoader) LoadWithDependencies(filePath string) (*ContextLoadResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	var contexts []LoadedContext
	totalTokens := 0

	intentDocPath := l.resolveIntentDocPath(absPath)
	mainContent, _ := l.readFileOrEmpty(intentDocPath)

	if mainContent != "" {
		tokenCount := estimateTokens(mainContent)
		if totalTokens+tokenCount <= l.config.MaxTokenBudget {
			contexts = append(contexts, LoadedContext{
				FilePath:   intentDocPath,
				Content:    mainContent,
				TokenCount: tokenCount,
			})
			totalTokens += tokenCount
		}
	}

	sourceContent, _ := l.readFileOrEmpty(absPath)
	if sourceContent != "" {
		imports := l.extractImportPaths(sourceContent)
		for _, imp := range imports {
			if totalTokens >= l.config.MaxTokenBudget {
				break
			}
			depIntentPath := l.resolveImportIntentPath(imp, absPath)
			depContent, _ := l.readFileOrEmpty(depIntentPath)
			if depContent != "" {
				tokenCount := estimateTokens(depContent)
				if totalTokens+tokenCount <= l.config.MaxTokenBudget {
					contexts = append(contexts, LoadedContext{
						FilePath:   depIntentPath,
						Content:    depContent,
						TokenCount: tokenCount,
					})
					totalTokens += tokenCount
				}
			}
		}
	}

	return &ContextLoadResult{
		Contexts:        contexts,
		TotalTokens:     totalTokens,
		BudgetRemaining: l.config.MaxTokenBudget - totalTokens,
	}, nil
}

// ClearCache 清空缓存。
func (l *ContextLoader) ClearCache() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache = make(map[string]*cacheEntry)
}

func estimateTokens(text string) int {
	n := len(text) / charsPerToken
	if len(text)%charsPerToken != 0 {
		n++
	}
	return n
}

func (l *ContextLoader) readWithCache(filePath string) (string, error) {
	if l.config.CacheEnabled {
		if entry, ok := l.cache[filePath]; ok {
			return entry.content, nil
		}
	}

	content, err := l.readFileOrEmpty(filePath)
	if err != nil {
		return "", err
	}

	if l.config.CacheEnabled && content != "" {
		l.evictIfNeeded()
		l.cache[filePath] = &cacheEntry{
			content:    content,
			tokenCount: estimateTokens(content),
		}
	}

	return content, nil
}

func (l *ContextLoader) evictIfNeeded() {
	if len(l.cache) < l.maxCacheSize {
		return
	}
	// 删除第一个找到的条目（简单 LRU 近似）
	for k := range l.cache {
		delete(l.cache, k)
		break
	}
}

func (l *ContextLoader) readFileOrEmpty(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", nil
	}
	return string(data), nil
}

func (l *ContextLoader) scanIntentFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 忽略错误继续扫描
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".md") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func (l *ContextLoader) computeRelevance(queryKw, docKw []string) float64 {
	if len(queryKw) == 0 || len(docKw) == 0 {
		return 0
	}

	docSet := make(map[string]bool, len(docKw))
	for _, kw := range docKw {
		docSet[kw] = true
	}

	matches := 0
	for _, kw := range queryKw {
		if docSet[kw] {
			matches++
		}
	}

	return float64(matches) / float64(len(queryKw))
}

func (l *ContextLoader) resolveIntentDocPath(sourceFilePath string) string {
	projectRoot, _ := filepath.Abs(l.config.ProjectRoot)
	relPath, err := filepath.Rel(projectRoot, sourceFilePath)
	if err != nil {
		relPath = sourceFilePath
	}

	ext := filepath.Ext(relPath)
	intentRel := strings.TrimSuffix(relPath, ext) + ".intent.md"
	return filepath.Join(projectRoot, l.config.ShadowRoot, "domain", intentRel)
}

func (l *ContextLoader) resolveImportIntentPath(importPath, fromFile string) string {
	projectRoot, _ := filepath.Abs(l.config.ProjectRoot)
	var resolvedImport string

	if strings.HasPrefix(importPath, ".") {
		resolvedImport = filepath.Join(filepath.Dir(fromFile), importPath)
	} else {
		resolvedImport = filepath.Join(projectRoot, "src", importPath)
	}

	ext := filepath.Ext(resolvedImport)
	knownExts := map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".go": true, ".py": true}
	if !knownExts[ext] {
		resolvedImport = resolvedImport + ".ts"
	}

	relPath, err := filepath.Rel(projectRoot, resolvedImport)
	if err != nil {
		relPath = resolvedImport
	}

	ext = filepath.Ext(relPath)
	intentRel := strings.TrimSuffix(relPath, ext) + ".intent.md"
	return filepath.Join(projectRoot, l.config.ShadowRoot, "domain", intentRel)
}

func (l *ContextLoader) extractImportPaths(sourceCode string) []string {
	var imports []string
	lines := strings.Split(sourceCode, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// TypeScript/JavaScript: import ... from '...'
		if strings.Contains(trimmed, "from") && (strings.Contains(trimmed, "'") || strings.Contains(trimmed, "\"")) {
			parts := strings.Split(trimmed, "from")
			if len(parts) >= 2 {
				importPart := strings.TrimSpace(parts[len(parts)-1])
				importPart = strings.Trim(importPart, "'\";")
				if importPart != "" {
					imports = append(imports, importPart)
				}
			}
		}

		// Go: import "..."
		if strings.HasPrefix(trimmed, "import") && strings.Contains(trimmed, "\"") {
			start := strings.Index(trimmed, "\"")
			end := strings.LastIndex(trimmed, "\"")
			if start >= 0 && end > start {
				imp := trimmed[start+1 : end]
				if imp != "" {
					imports = append(imports, imp)
				}
			}
		}
	}

	return imports
}

// String 格式化输出。
func (r *ContextLoadResult) String() string {
	return fmt.Sprintf("ContextLoadResult{contexts=%d, totalTokens=%d, budgetRemaining=%d}",
		len(r.Contexts), r.TotalTokens, r.BudgetRemaining)
}
