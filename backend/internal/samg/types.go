package samg

import (
	"context"
)

// ExtractionMethod 提取方法
type ExtractionMethod string

const (
	ExtractionLLM      ExtractionMethod = "llm"
	ExtractionRule     ExtractionMethod = "rule"
	ExtractionUser     ExtractionMethod = "user"
	ExtractionInferred ExtractionMethod = "inferred"
)

// TripleNode 三元组节点（实体引用）
type TripleNode struct {
	ID    string   `json:"@id"`
	Type  []string `json:"@type,omitempty"`
	Label string   `json:"label,omitempty"`
}

// LiteralValue 字面量值
type LiteralValue struct {
	Value    interface{} `json:"@value"`
	Type     string      `json:"@type,omitempty"`
	Language string      `json:"@language,omitempty"`
}

// TripleSource 三元组来源
type TripleSource struct {
	SessionID        string           `json:"session_id"`
	MessageIndex     int              `json:"message_index,omitempty"`
	AgentRole        string           `json:"agent_role,omitempty"`
	GitCommitHash    string           `json:"git_commit_hash,omitempty"`
	ExtractionMethod ExtractionMethod `json:"extraction_method"`
}

// TripleObject 三元组对象（可以是节点或字面量）
type TripleObject struct {
	Node    *TripleNode   `json:"node,omitempty"`
	Literal *LiteralValue `json:"literal,omitempty"`
}

// IsLiteral 判断是否为字面量
func (o *TripleObject) IsLiteral() bool {
	return o.Literal != nil
}

// GetID 获取对象ID
func (o *TripleObject) GetID() string {
	if o.Node != nil {
		return o.Node.ID
	}
	return ""
}

// Triple S-P-O 三元组
type Triple struct {
	ID         string                 `json:"@id"`
	Subject    TripleNode             `json:"subject"`
	Predicate  string                 `json:"predicate"`
	Object     TripleObject           `json:"object"`
	Confidence float64                `json:"confidence"`
	Timestamp  int64                  `json:"timestamp"`
	Source     TripleSource           `json:"source"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// Entity 实体定义
type Entity struct {
	ID          string                 `json:"@id"`
	Type        []string               `json:"@type"`
	Label       string                 `json:"label"`
	Description string                 `json:"description,omitempty"`
	Properties  map[string]interface{} `json:"properties,omitempty"`
	Aliases     []string               `json:"aliases,omitempty"`
	CreatedAt   int64                  `json:"created_at"`
	UpdatedAt   int64                  `json:"updated_at"`
}

// JsonLdContext JSON-LD上下文
type JsonLdContext struct {
	Vocab string            `json:"@vocab,omitempty"`
	Base  string            `json:"@base,omitempty"`
	Extra map[string]string `json:"-"`
}

// GraphMetadata 图谱元数据
type GraphMetadata struct {
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
	TripleCount    int    `json:"triple_count"`
	EntityCount    int    `json:"entity_count"`
	PredicateCount int    `json:"predicate_count"`
	Version        string `json:"version"`
}

// JsonLdGraph JSON-LD图谱
type JsonLdGraph struct {
	Context  JsonLdContext `json:"@context"`
	ID       string        `json:"@id"`
	Type     string        `json:"@type"`
	Graph    []Triple      `json:"@graph"`
	Metadata GraphMetadata `json:"metadata"`
}

// TripleQuery 三元组查询条件
type TripleQuery struct {
	Subject       string        `json:"subject,omitempty"`
	Predicate     string        `json:"predicate,omitempty"`
	Object        string        `json:"object,omitempty"`
	MinConfidence float64       `json:"min_confidence,omitempty"`
	Source        *TripleSource `json:"source,omitempty"`
	Limit         int           `json:"limit,omitempty"`
	Offset        int           `json:"offset,omitempty"`
}

// TripleStoreConfig 三元组存储配置
type TripleStoreConfig struct {
	GraphID              string `json:"graph_id"`
	BaseURI              string `json:"base_uri"`
	VocabURI             string `json:"vocab_uri"`
	EnableDeduplication  bool   `json:"enable_deduplication"`
	EnableInference      bool   `json:"enable_inference"`
	MaxTriples           int    `json:"max_triples"`
}

// ITripleStore 三元组存储接口
type ITripleStore interface {
	// 基础CRUD
	Add(ctx context.Context, triples []Triple) error
	Get(ctx context.Context, id string) (*Triple, error)
	Update(ctx context.Context, id string, updates map[string]interface{}) error
	Delete(ctx context.Context, ids []string) error
	Clear(ctx context.Context) error

	// 查询
	Query(ctx context.Context, query TripleQuery) ([]Triple, error)
	FindBySubject(ctx context.Context, subjectID string) ([]Triple, error)
	FindByPredicate(ctx context.Context, predicate string) ([]Triple, error)
	FindByObject(ctx context.Context, objectID string) ([]Triple, error)

	// 实体操作
	GetEntity(ctx context.Context, id string) (*Entity, error)
	GetEntities(ctx context.Context) ([]Entity, error)
	UpsertEntity(ctx context.Context, entity Entity) error

	// 图谱操作
	ExportGraph(ctx context.Context) (*JsonLdGraph, error)
	ImportGraph(ctx context.Context, graph *JsonLdGraph) error
	GetStats(ctx context.Context) (*GraphMetadata, error)

	// 去重
	Deduplicate(ctx context.Context) (int, error)
}

// ITripleExtractor 三元组提取器接口
type ITripleExtractor interface {
	Extract(ctx context.Context, text string, source TripleSource) ([]Triple, error)
}

// DefaultTripleStoreConfig 默认配置
var DefaultTripleStoreConfig = TripleStoreConfig{
	GraphID:              "codeflow:samg",
	BaseURI:              "https://codeflow.ai/graph/",
	VocabURI:             "https://codeflow.ai/vocab/",
	EnableDeduplication:  true,
	EnableInference:      false,
	MaxTriples:           1000000,
}

// Predicates 预定义谓词
var Predicates = struct {
	// 代码关系
	Calls      string
	Imports    string
	Extends    string
	Implements string
	Defines    string
	Uses       string
	DependsOn  string
	// 对话关系
	Mentions   string
	References string
	Decides    string
	Creates    string
	Modifies   string
	Deletes    string
	// 知识关系
	IsA        string
	SubclassOf string
	RelatedTo  string
	SameAs     string
	DerivedFrom string
}{
	Calls:       "codeflow:calls",
	Imports:     "codeflow:imports",
	Extends:     "codeflow:extends",
	Implements:  "codeflow:implements",
	Defines:     "codeflow:defines",
	Uses:        "codeflow:uses",
	DependsOn:   "codeflow:dependsOn",
	Mentions:    "codeflow:mentions",
	References:  "codeflow:references",
	Decides:     "codeflow:decides",
	Creates:     "codeflow:creates",
	Modifies:    "codeflow:modifies",
	Deletes:     "codeflow:deletes",
	IsA:         "rdf:type",
	SubclassOf:  "rdfs:subClassOf",
	RelatedTo:   "codeflow:relatedTo",
	SameAs:      "owl:sameAs",
	DerivedFrom: "codeflow:derivedFrom",
}

// EntityTypes 预定义实体类型
var EntityTypes = struct {
	// 代码实体
	File     string
	Class    string
	Function string
	Variable string
	Module   string
	Package  string
	// 对话实体
	Decision    string
	Requirement string
	Issue       string
	Feature     string
	Bug         string
	// 概念实体
	Concept    string
	Technology string
	Pattern    string
}{
	File:        "codeflow:File",
	Class:       "codeflow:Class",
	Function:    "codeflow:Function",
	Variable:    "codeflow:Variable",
	Module:      "codeflow:Module",
	Package:     "codeflow:Package",
	Decision:    "codeflow:Decision",
	Requirement: "codeflow:Requirement",
	Issue:       "codeflow:Issue",
	Feature:     "codeflow:Feature",
	Bug:         "codeflow:Bug",
	Concept:     "codeflow:Concept",
	Technology:  "codeflow:Technology",
	Pattern:     "codeflow:Pattern",
}

// GenerateTripleID 生成三元组ID
func GenerateTripleID(subject, predicate, object string) string {
	hash := simpleHash(subject + "|" + predicate + "|" + object)
	return "triple:" + hash
}

// GenerateEntityID 生成实体ID
func GenerateEntityID(entityType, label string) string {
	hash := simpleHash(entityType + "|" + label)
	return "entity:" + hash
}

// simpleHash 简单哈希函数
func simpleHash(str string) string {
	var hash int64
	for i := 0; i < len(str); i++ {
		char := int64(str[i])
		hash = ((hash << 5) - hash) + char
		hash = hash & 0xFFFFFFFF
	}
	if hash < 0 {
		hash = -hash
	}
	return int64ToBase36(hash)
}

// int64ToBase36 转换为base36
func int64ToBase36(n int64) string {
	const chars = "0123456789abcdefghijklmnopqrstuvwxyz"
	if n == 0 {
		return "0"
	}
	result := ""
	for n > 0 {
		result = string(chars[n%36]) + result
		n /= 36
	}
	return result
}

// CreateNode 创建三元组节点
func CreateNode(id string, nodeType string, label string) TripleNode {
	node := TripleNode{ID: id, Label: label}
	if nodeType != "" {
		node.Type = []string{nodeType}
	}
	return node
}

// CreateLiteral 创建字面量值
func CreateLiteral(value interface{}, valueType string, language string) LiteralValue {
	return LiteralValue{
		Value:    value,
		Type:     valueType,
		Language: language,
	}
}

// CreateNodeObject 从节点创建对象
func CreateNodeObject(node TripleNode) TripleObject {
	return TripleObject{Node: &node}
}

// CreateLiteralObject 从字面量创建对象
func CreateLiteralObject(literal LiteralValue) TripleObject {
	return TripleObject{Literal: &literal}
}
