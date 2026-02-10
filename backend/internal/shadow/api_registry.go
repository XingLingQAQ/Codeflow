package shadow

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// APIRegistryEntry API 注册表条目。
type APIRegistryEntry struct {
	Path        string   `yaml:"path" json:"path"`
	Method      string   `yaml:"method" json:"method"`
	Description string   `yaml:"description" json:"description"`
	Handler     string   `yaml:"handler" json:"handler"`
	Tags        []string `yaml:"tags" json:"tags"`
	Similarity  float64  `yaml:"similarity,omitempty" json:"similarity,omitempty"`
}

// APIRegistryConfig API 注册表配置。
type APIRegistryConfig struct {
	RegistryPath        string
	SimilarityThreshold float64
}

// DuplicateCheckResult 重复检查结果。
type DuplicateCheckResult struct {
	IsDuplicate    bool
	SimilarEntries []APIRegistryEntry
}

// APIRegistry API 注册表，防止重复接口。
type APIRegistry struct {
	mu      sync.RWMutex
	entries []APIRegistryEntry
	config  APIRegistryConfig
}

// NewAPIRegistry 创建 API 注册表实例。
func NewAPIRegistry(config *APIRegistryConfig) *APIRegistry {
	cfg := APIRegistryConfig{
		RegistryPath:        ".codeflow/registry/apis.yaml",
		SimilarityThreshold: 0.7,
	}
	if config != nil {
		if config.RegistryPath != "" {
			cfg.RegistryPath = config.RegistryPath
		}
		if config.SimilarityThreshold > 0 {
			cfg.SimilarityThreshold = config.SimilarityThreshold
		}
	}
	return &APIRegistry{
		entries: make([]APIRegistryEntry, 0),
		config:  cfg,
	}
}

// Register 注册 API 条目，检查重复后添加。
func (r *APIRegistry) Register(entry APIRegistryEntry) DuplicateCheckResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := r.checkDuplicateLocked(entry)
	if result.IsDuplicate {
		return result
	}

	entry.Similarity = 0
	r.entries = append(r.entries, entry)
	return DuplicateCheckResult{IsDuplicate: false, SimilarEntries: nil}
}

// Search 搜索匹配的 API。
func (r *APIRegistry) Search(query string) []APIRegistryEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	queryKw := extractKeywords(query)
	if len(queryKw) == 0 {
		return nil
	}

	type scored struct {
		entry APIRegistryEntry
		score float64
	}

	var results []scored
	for _, entry := range r.entries {
		entryKw := entryKeywords(entry)
		score := computeKeywordSimilarity(queryKw, entryKw)
		if score > 0 {
			e := entry
			e.Similarity = score
			results = append(results, scored{entry: e, score: score})
		}
	}

	// 按分数降序排序
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	out := make([]APIRegistryEntry, len(results))
	for i, r := range results {
		out[i] = r.entry
	}
	return out
}

// CheckDuplicate 检查是否存在重复 API。
func (r *APIRegistry) CheckDuplicate(entry APIRegistryEntry) DuplicateCheckResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.checkDuplicateLocked(entry)
}

func (r *APIRegistry) checkDuplicateLocked(entry APIRegistryEntry) DuplicateCheckResult {
	entryKw := entryKeywords(entry)
	var similar []APIRegistryEntry

	for _, existing := range r.entries {
		if entry.Path == existing.Path && entry.Method == existing.Method {
			e := existing
			e.Similarity = 1.0
			similar = append(similar, e)
			continue
		}

		existingKw := entryKeywords(existing)
		score := computeKeywordSimilarity(entryKw, existingKw)
		if score >= r.config.SimilarityThreshold {
			e := existing
			e.Similarity = score
			similar = append(similar, e)
		}
	}

	// 按相似度降序排序
	for i := 0; i < len(similar); i++ {
		for j := i + 1; j < len(similar); j++ {
			if similar[j].Similarity > similar[i].Similarity {
				similar[i], similar[j] = similar[j], similar[i]
			}
		}
	}

	return DuplicateCheckResult{
		IsDuplicate:    len(similar) > 0,
		SimilarEntries: similar,
	}
}

// LoadFromYAML 从 YAML 文件加载注册表。
func (r *APIRegistry) LoadFromYAML() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(r.config.RegistryPath)
	if err != nil {
		if os.IsNotExist(err) {
			r.entries = make([]APIRegistryEntry, 0)
			return nil
		}
		return fmt.Errorf("read api registry: %w", err)
	}

	var entries []APIRegistryEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("unmarshal api registry: %w", err)
	}

	r.entries = entries
	return nil
}

// SaveToYAML 保存注册表到 YAML 文件。
func (r *APIRegistry) SaveToYAML() error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	dir := filepath.Dir(r.config.RegistryPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create registry dir: %w", err)
	}

	data, err := yaml.Marshal(r.entries)
	if err != nil {
		return fmt.Errorf("marshal api registry: %w", err)
	}

	header := []byte("# CodeFlow API Registry\n# Auto-generated - do not edit manually\n\n")
	content := append(header, data...)

	if err := os.WriteFile(r.config.RegistryPath, content, 0o644); err != nil {
		return fmt.Errorf("write api registry: %w", err)
	}
	return nil
}

// GetEntries 获取所有条目。
func (r *APIRegistry) GetEntries() []APIRegistryEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]APIRegistryEntry, len(r.entries))
	copy(out, r.entries)
	return out
}

// GetEntryCount 获取条目数量。
func (r *APIRegistry) GetEntryCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

func entryKeywords(entry APIRegistryEntry) []string {
	parts := []string{entry.Path, entry.Method, entry.Description, entry.Handler}
	parts = append(parts, entry.Tags...)
	return extractKeywords(strings.Join(parts, " "))
}

func extractKeywords(text string) []string {
	text = strings.ToLower(text)
	var sb strings.Builder
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' || r == '_' || (r >= 0x4e00 && r <= 0x9fff) {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(' ')
		}
	}

	fields := strings.Fields(sb.String())
	keywords := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) > 1 {
			keywords = append(keywords, f)
		}
	}
	return keywords
}

func computeKeywordSimilarity(kwA, kwB []string) float64 {
	if len(kwA) == 0 || len(kwB) == 0 {
		return 0
	}

	setA := make(map[string]bool, len(kwA))
	for _, kw := range kwA {
		setA[kw] = true
	}

	setB := make(map[string]bool, len(kwB))
	for _, kw := range kwB {
		setB[kw] = true
	}

	intersection := 0
	for kw := range setA {
		if setB[kw] {
			intersection++
		}
	}

	union := make(map[string]bool)
	for kw := range setA {
		union[kw] = true
	}
	for kw := range setB {
		union[kw] = true
	}

	if len(union) == 0 {
		return 0
	}

	return float64(intersection) / float64(len(union))
}

// round 四舍五入到指定小数位。
func round(val float64, precision int) float64 {
	p := math.Pow(10, float64(precision))
	return math.Round(val*p) / p
}
