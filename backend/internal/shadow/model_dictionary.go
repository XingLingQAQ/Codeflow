package shadow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// ModelField 模型字段。
type ModelField struct {
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type" json:"type"`
	Required bool   `yaml:"required" json:"required"`
}

// ModelRelationship 模型关系。
type ModelRelationship struct {
	Entity string `yaml:"entity" json:"entity"`
	DTO    string `yaml:"dto" json:"dto"`
	Type   string `yaml:"type" json:"type"` // map, extend, subset, transform
}

// ModelEntry 模型字典条目。
type ModelEntry struct {
	Name          string              `yaml:"name" json:"name"`
	Fields        []ModelField        `yaml:"fields" json:"fields"`
	Relationships []ModelRelationship `yaml:"relationships,omitempty" json:"relationships,omitempty"`
	Source        string              `yaml:"source" json:"source"`
	Tags          []string            `yaml:"tags" json:"tags"`
	Similarity    float64             `yaml:"similarity,omitempty" json:"similarity,omitempty"`
}

// ModelDictionaryConfig 模型字典配置。
type ModelDictionaryConfig struct {
	DictionaryPath      string
	SimilarityThreshold float64
}

// ModelDuplicateCheckResult 模型重复检查结果。
type ModelDuplicateCheckResult struct {
	IsDuplicate   bool
	SimilarModels []ModelEntry
}

// ModelDictionary 模型字典，防止重复数据结构。
type ModelDictionary struct {
	mu            sync.RWMutex
	models        []ModelEntry
	relationships []ModelRelationship
	config        ModelDictionaryConfig
}

// NewModelDictionary 创建模型字典实例。
func NewModelDictionary(config *ModelDictionaryConfig) *ModelDictionary {
	cfg := ModelDictionaryConfig{
		DictionaryPath:      ".codeflow/registry/models.yaml",
		SimilarityThreshold: 0.6,
	}
	if config != nil {
		if config.DictionaryPath != "" {
			cfg.DictionaryPath = config.DictionaryPath
		}
		if config.SimilarityThreshold > 0 {
			cfg.SimilarityThreshold = config.SimilarityThreshold
		}
	}
	return &ModelDictionary{
		models:        make([]ModelEntry, 0),
		relationships: make([]ModelRelationship, 0),
		config:        cfg,
	}
}

// Register 注册模型条目，检查重复后添加。
func (d *ModelDictionary) Register(entry ModelEntry) ModelDuplicateCheckResult {
	d.mu.Lock()
	defer d.mu.Unlock()

	result := d.checkDuplicateLocked(entry)
	if result.IsDuplicate {
		return result
	}

	entry.Similarity = 0
	d.models = append(d.models, entry)
	return ModelDuplicateCheckResult{IsDuplicate: false, SimilarModels: nil}
}

// Search 搜索匹配的模型。
func (d *ModelDictionary) Search(query string) []ModelEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()

	queryKw := extractKeywords(query)
	if len(queryKw) == 0 {
		return nil
	}

	type scored struct {
		model ModelEntry
		score float64
	}

	var results []scored
	for _, model := range d.models {
		modelKw := modelKeywords(model)
		score := computeKeywordSimilarity(queryKw, modelKw)
		if score > 0 {
			m := model
			m.Similarity = score
			results = append(results, scored{model: m, score: score})
		}
	}

	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	out := make([]ModelEntry, len(results))
	for i, r := range results {
		out[i] = r.model
	}
	return out
}

// CheckDuplicate 检查是否存在重复模型。
func (d *ModelDictionary) CheckDuplicate(entry ModelEntry) ModelDuplicateCheckResult {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.checkDuplicateLocked(entry)
}

func (d *ModelDictionary) checkDuplicateLocked(entry ModelEntry) ModelDuplicateCheckResult {
	var similar []ModelEntry

	for _, existing := range d.models {
		if entry.Name == existing.Name {
			e := existing
			e.Similarity = 1.0
			similar = append(similar, e)
			continue
		}

		structuralScore := computeStructuralSimilarity(entry, existing)
		keywordScore := computeKeywordSimilarity(modelKeywords(entry), modelKeywords(existing))
		combinedScore := structuralScore*0.6 + keywordScore*0.4

		if combinedScore >= d.config.SimilarityThreshold {
			e := existing
			e.Similarity = combinedScore
			similar = append(similar, e)
		}
	}

	for i := 0; i < len(similar); i++ {
		for j := i + 1; j < len(similar); j++ {
			if similar[j].Similarity > similar[i].Similarity {
				similar[i], similar[j] = similar[j], similar[i]
			}
		}
	}

	return ModelDuplicateCheckResult{
		IsDuplicate:   len(similar) > 0,
		SimilarModels: similar,
	}
}

// RecordRelationship 记录 Entity-DTO 关系。
func (d *ModelDictionary) RecordRelationship(entity, dto, relType string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, r := range d.relationships {
		if r.Entity == entity && r.DTO == dto && r.Type == relType {
			return
		}
	}
	d.relationships = append(d.relationships, ModelRelationship{
		Entity: entity,
		DTO:    dto,
		Type:   relType,
	})
}

// GetRelationships 获取所有关系。
func (d *ModelDictionary) GetRelationships() []ModelRelationship {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]ModelRelationship, len(d.relationships))
	copy(out, d.relationships)
	return out
}

// LoadFromYAML 从 YAML 文件加载模型字典。
func (d *ModelDictionary) LoadFromYAML() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := os.ReadFile(d.config.DictionaryPath)
	if err != nil {
		if os.IsNotExist(err) {
			d.models = make([]ModelEntry, 0)
			d.relationships = make([]ModelRelationship, 0)
			return nil
		}
		return fmt.Errorf("read model dictionary: %w", err)
	}

	var payload struct {
		Models        []ModelEntry        `yaml:"models"`
		Relationships []ModelRelationship `yaml:"relationships"`
	}
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("unmarshal model dictionary: %w", err)
	}

	d.models = payload.Models
	if d.models == nil {
		d.models = make([]ModelEntry, 0)
	}
	d.relationships = payload.Relationships
	if d.relationships == nil {
		d.relationships = make([]ModelRelationship, 0)
	}
	return nil
}

// SaveToYAML 保存模型字典到 YAML 文件。
func (d *ModelDictionary) SaveToYAML() error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	dir := filepath.Dir(d.config.DictionaryPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dictionary dir: %w", err)
	}

	payload := struct {
		Models        []ModelEntry        `yaml:"models"`
		Relationships []ModelRelationship `yaml:"relationships,omitempty"`
	}{
		Models:        d.models,
		Relationships: d.relationships,
	}

	data, err := yaml.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal model dictionary: %w", err)
	}

	header := []byte("# CodeFlow Model Dictionary\n# Auto-generated - do not edit manually\n\n")
	content := append(header, data...)

	if err := os.WriteFile(d.config.DictionaryPath, content, 0o644); err != nil {
		return fmt.Errorf("write model dictionary: %w", err)
	}
	return nil
}

// GetModels 获取所有模型。
func (d *ModelDictionary) GetModels() []ModelEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]ModelEntry, len(d.models))
	copy(out, d.models)
	return out
}

// GetModelCount 获取模型数量。
func (d *ModelDictionary) GetModelCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.models)
}

func modelKeywords(model ModelEntry) []string {
	parts := []string{model.Name, model.Source}
	parts = append(parts, model.Tags...)
	for _, f := range model.Fields {
		parts = append(parts, f.Name)
	}
	return extractKeywords(strings.Join(parts, " "))
}

func computeStructuralSimilarity(a, b ModelEntry) float64 {
	fieldsA := make(map[string]bool)
	for _, f := range a.Fields {
		fieldsA[f.Name+":"+f.Type] = true
	}

	fieldsB := make(map[string]bool)
	for _, f := range b.Fields {
		fieldsB[f.Name+":"+f.Type] = true
	}

	if len(fieldsA) == 0 && len(fieldsB) == 0 {
		return 0
	}

	intersection := 0
	for k := range fieldsA {
		if fieldsB[k] {
			intersection++
		}
	}

	union := make(map[string]bool)
	for k := range fieldsA {
		union[k] = true
	}
	for k := range fieldsB {
		union[k] = true
	}

	if len(union) == 0 {
		return 0
	}

	jaccardFields := float64(intersection) / float64(len(union))

	// 字段名重叠
	namesA := make(map[string]bool)
	for _, f := range a.Fields {
		namesA[strings.ToLower(f.Name)] = true
	}
	namesB := make(map[string]bool)
	for _, f := range b.Fields {
		namesB[strings.ToLower(f.Name)] = true
	}

	nameIntersection := 0
	for n := range namesA {
		if namesB[n] {
			nameIntersection++
		}
	}

	nameUnion := make(map[string]bool)
	for n := range namesA {
		nameUnion[n] = true
	}
	for n := range namesB {
		nameUnion[n] = true
	}

	nameOverlap := 0.0
	if len(nameUnion) > 0 {
		nameOverlap = float64(nameIntersection) / float64(len(nameUnion))
	}

	return jaccardFields*0.7 + nameOverlap*0.3
}
