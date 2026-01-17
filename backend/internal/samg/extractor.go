package samg

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// TripleExtractorConfig 提取器配置
type TripleExtractorConfig struct {
	MinConfidence            float64 `json:"min_confidence"`
	MaxTriplesPerExtraction  int     `json:"max_triples_per_extraction"`
	EnableRuleBasedExtraction bool    `json:"enable_rule_based_extraction"`
}

// DefaultExtractorConfig 默认配置
var DefaultExtractorConfig = TripleExtractorConfig{
	MinConfidence:            0.5,
	MaxTriplesPerExtraction:  50,
	EnableRuleBasedExtraction: true,
}

// TripleExtractor 三元组提取器
type TripleExtractor struct {
	config TripleExtractorConfig
}

// NewTripleExtractor 创建提取器
func NewTripleExtractor(config *TripleExtractorConfig) *TripleExtractor {
	cfg := DefaultExtractorConfig
	if config != nil {
		cfg = *config
	}
	return &TripleExtractor{config: cfg}
}

// Extract 从文本提取三元组
func (e *TripleExtractor) Extract(ctx context.Context, text string, source TripleSource) ([]Triple, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var triples []Triple

	if e.config.EnableRuleBasedExtraction {
		ruleSource := source
		ruleSource.ExtractionMethod = ExtractionRule
		triples = append(triples, e.extractByRules(text, ruleSource)...)
	}

	// 过滤低置信度
	filtered := make([]Triple, 0)
	for _, t := range triples {
		if t.Confidence >= e.config.MinConfidence {
			filtered = append(filtered, t)
		}
	}

	// 去重
	filtered = e.deduplicateTriples(filtered)

	// 限制数量
	if len(filtered) > e.config.MaxTriplesPerExtraction {
		filtered = filtered[:e.config.MaxTriplesPerExtraction]
	}

	return filtered, nil
}

// extractByRules 基于规则提取
func (e *TripleExtractor) extractByRules(text string, source TripleSource) []Triple {
	var triples []Triple

	triples = append(triples, e.extractCodeRelations(text, source)...)
	triples = append(triples, e.extractDecisionRelations(text, source)...)
	triples = append(triples, e.extractFileRelations(text, source)...)

	return triples
}

// extractCodeRelations 提取代码关系
func (e *TripleExtractor) extractCodeRelations(text string, source TripleSource) []Triple {
	var triples []Triple

	// Pattern: "X calls Y" / "X 调用 Y"
	callPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(\w+)\s+(?:calls?|invokes?|调用)\s+(\w+)`),
	}

	for _, pattern := range callPatterns {
		matches := pattern.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) >= 3 {
				caller, callee := match[1], match[2]
				triples = append(triples, e.createTriple(
					caller, EntityTypes.Function,
					Predicates.Calls,
					callee, EntityTypes.Function,
					0.7, source,
				))
			}
		}
	}

	// Pattern: "import ... from '...'"
	importPattern := regexp.MustCompile(`(?i)import\s+(?:\{[^}]+\}|[\w*]+)\s+from\s+['"]([^'"]+)['"]`)
	importMatches := importPattern.FindAllStringSubmatch(text, -1)
	for _, match := range importMatches {
		if len(match) >= 2 {
			moduleName := match[1]
			triples = append(triples, e.createTriple(
				"currentModule", EntityTypes.Module,
				Predicates.Imports,
				moduleName, EntityTypes.Module,
				0.9, source,
			))
		}
	}

	// Pattern: "class X extends Y" or "X class extends Y"
	extendsPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)class\s+(\w+)\s+extends\s+(\w+)`),
		regexp.MustCompile(`(?i)(\w+)\s+class\s+extends\s+(\w+)`),
		regexp.MustCompile(`(?i)(\w+)\s+extends\s+(\w+)`),
	}
	for _, pattern := range extendsPatterns {
		matches := pattern.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) >= 3 {
				child, parent := match[1], match[2]
				// 过滤掉非类名的匹配
				if len(child) > 1 && len(parent) > 1 {
					triples = append(triples, e.createTriple(
						child, EntityTypes.Class,
						Predicates.Extends,
						parent, EntityTypes.Class,
						0.95, source,
					))
				}
			}
		}
	}

	// Pattern: "class X implements Y" or "X implements Y"
	implementsPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)class\s+(\w+)(?:\s+extends\s+\w+)?\s+implements\s+([\w,\s]+)`),
		regexp.MustCompile(`(?i)(\w+)\s+implements\s+(\w+)`),
	}
	for _, implementsPattern := range implementsPatterns {
		implementsMatches := implementsPattern.FindAllStringSubmatch(text, -1)
		for _, match := range implementsMatches {
			if len(match) >= 3 {
				className := match[1]
				interfaces := strings.Split(match[2], ",")
				for _, iface := range interfaces {
					iface = strings.TrimSpace(iface)
					if iface != "" {
						triples = append(triples, e.createTriple(
							className, EntityTypes.Class,
							Predicates.Implements,
							iface, EntityTypes.Class,
							0.95, source,
						))
					}
				}
			}
		}
	}

	return triples
}

// extractDecisionRelations 提取决策关系
func (e *TripleExtractor) extractDecisionRelations(text string, source TripleSource) []Triple {
	var triples []Triple

	// Pattern: "decided to X" / "决定 X"
	decisionPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:we|I|team)?\s*(?:decided?|chose?|选择|决定)\s+(?:to\s+)?(.+?)(?:\.|$)`),
		regexp.MustCompile(`(?i)(?:decision|决定|选择)[:：]\s*(.+?)(?:\.|$)`),
	}

	for _, pattern := range decisionPatterns {
		matches := pattern.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) >= 2 {
				decision := strings.TrimSpace(match[1])
				if len(decision) > 5 && len(decision) < 200 {
					decisionID := GenerateEntityID(EntityTypes.Decision, decision[:min(50, len(decision))])
					triples = append(triples, Triple{
						ID:      GenerateTripleID("session", Predicates.Decides, decisionID),
						Subject: CreateNode("session:current", "codeflow:Session", "Current Session"),
						Predicate: Predicates.Decides,
						Object:  CreateNodeObject(CreateNode(decisionID, EntityTypes.Decision, decision[:min(100, len(decision))])),
						Confidence: 0.6,
						Timestamp:  time.Now().UnixMilli(),
						Source:    source,
					})
				}
			}
		}
	}

	// Pattern: "created X" / "创建了 X"
	createPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:created?|added?|implemented?|创建|添加|实现)\s+(?:a\s+)?(?:new\s+)?(\w+(?:\s+\w+)?)`),
	}

	for _, pattern := range createPatterns {
		matches := pattern.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) >= 2 {
				entity := strings.TrimSpace(match[1])
				if len(entity) > 2 {
					triples = append(triples, e.createTriple(
						"session:current", "codeflow:Session",
						Predicates.Creates,
						entity, EntityTypes.Concept,
						0.5, source,
					))
				}
			}
		}
	}

	return triples
}

// extractFileRelations 提取文件关系
func (e *TripleExtractor) extractFileRelations(text string, source TripleSource) []Triple {
	var triples []Triple

	// Pattern: file paths (more flexible matching)
	filePatterns := []*regexp.Regexp{
		regexp.MustCompile(`([\w./\\-]+\.(?:ts|js|tsx|jsx|py|java|go|rs|cpp|c|h|css|scss|html|json|yaml|yml|md))\b`),
	}

	var files []string
	seen := make(map[string]bool)
	for _, pattern := range filePatterns {
		matches := pattern.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) >= 2 {
				filePath := match[1]
				if !seen[filePath] && len(filePath) > 3 {
					seen[filePath] = true
					files = append(files, filePath)
				}
			}
		}
	}

	// 创建文件之间的关联
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			triples = append(triples, e.createTriple(
				files[i], EntityTypes.File,
				Predicates.RelatedTo,
				files[j], EntityTypes.File,
				0.4, source,
			))
		}
	}

	// Pattern: "X modifies/changes Y"
	modifyPattern := regexp.MustCompile(`(?i)(?:modif(?:y|ied)|chang(?:e|ed)|updat(?:e|ed)|修改|更新)\s+(?:the\s+)?(?:file\s+)?['"]?([^'"]+\.(?:ts|js|tsx|jsx|py))['"]?`)
	modifyMatches := modifyPattern.FindAllStringSubmatch(text, -1)
	for _, match := range modifyMatches {
		if len(match) >= 2 {
			filePath := match[1]
			triples = append(triples, e.createTriple(
				"session:current", "codeflow:Session",
				Predicates.Modifies,
				filePath, EntityTypes.File,
				0.7, source,
			))
		}
	}

	return triples
}

// createTriple 创建三元组
func (e *TripleExtractor) createTriple(
	subjectLabel string, subjectType string,
	predicate string,
	objectLabel string, objectType string,
	confidence float64, source TripleSource,
) Triple {
	subjectID := GenerateEntityID(subjectType, subjectLabel)
	objectID := GenerateEntityID(objectType, objectLabel)

	return Triple{
		ID:         GenerateTripleID(subjectID, predicate, objectID),
		Subject:    CreateNode(subjectID, subjectType, subjectLabel),
		Predicate:  predicate,
		Object:     CreateNodeObject(CreateNode(objectID, objectType, objectLabel)),
		Confidence: confidence,
		Timestamp:  time.Now().UnixMilli(),
		Source:     source,
	}
}

// deduplicateTriples 去重
func (e *TripleExtractor) deduplicateTriples(triples []Triple) []Triple {
	seen := make(map[string]Triple)

	for _, triple := range triples {
		var objectID string
		if triple.Object.IsLiteral() {
			objectID = toString(triple.Object.Literal.Value)
		} else if triple.Object.Node != nil {
			objectID = triple.Object.Node.ID
		}
		key := triple.Subject.ID + "|" + triple.Predicate + "|" + objectID

		existing, exists := seen[key]
		if !exists || triple.Confidence > existing.Confidence {
			seen[key] = triple
		}
	}

	result := make([]Triple, 0, len(seen))
	for _, triple := range seen {
		result = append(result, triple)
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
