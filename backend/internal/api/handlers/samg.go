// Package handlers - SAMG API handlers
package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/codeflow/backend/internal/samg"
	"github.com/gin-gonic/gin"
)

// AddTriplesRequest 添加三元组请求
type AddTriplesRequest struct {
	Triples []samg.Triple `json:"triples" binding:"required"`
}

// ExtractRequest 提取请求
type ExtractRequest struct {
	Content      string `json:"content" binding:"required"`
	SessionID    string `json:"session_id"`
	MessageIndex int    `json:"message_index"`
	AgentRole    string `json:"agent_role"`
}

// ActivateRequest 激活请求
type ActivateRequest struct {
	SourceIDs []string `json:"source_ids" binding:"required"`
}

// FindPathsRequest 路径查找请求
type FindPathsRequest struct {
	SourceID string `json:"source_id" binding:"required"`
	TargetID string `json:"target_id" binding:"required"`
	MaxHops  int    `json:"max_hops"`
}

// DecayConfigRequest 衰减配置请求
type DecayConfigRequest struct {
	DecayRate       float64 `json:"decay_rate"`
	BaseActivation  float64 `json:"base_activation"`
	MinActivation   float64 `json:"min_activation"`
	HideThreshold   float64 `json:"hide_threshold"`
	BoostOnAccess   float64 `json:"boost_on_access"`
	TimeUnit        int64   `json:"time_unit"`
	EnableAutoDecay bool    `json:"enable_auto_decay"`
}

// ActivationConfigRequest 激活配置请求
type ActivationConfigRequest struct {
	InitialActivation float64 `json:"initial_activation"`
	DecayFactor       float64 `json:"decay_factor"`
	FiringThreshold   float64 `json:"firing_threshold"`
	MaxHops           int     `json:"max_hops"`
	MaxActivatedNodes int     `json:"max_activated_nodes"`
	SpreadingFactor   float64 `json:"spreading_factor"`
}

// GetTriples 获取三元组列表
// GET /api/v1/samg/triples
func GetTriples(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	ctx := c.Request.Context()
	query := samg.TripleQuery{}

	if subject := c.Query("subject"); subject != "" {
		query.Subject = subject
	}
	if predicate := c.Query("predicate"); predicate != "" {
		query.Predicate = predicate
	}
	if object := c.Query("object"); object != "" {
		query.Object = object
	}
	if minConf := c.Query("min_confidence"); minConf != "" {
		if conf, err := strconv.ParseFloat(minConf, 64); err == nil {
			query.MinConfidence = conf
		}
	}
	if limit := c.Query("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil {
			query.Limit = l
		}
	}
	if offset := c.Query("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil {
			query.Offset = o
		}
	}

	triples, err := svc.QueryTriples(ctx, query)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(c, gin.H{"triples": triples, "count": len(triples)})
}

// PLACEHOLDER_SAMG_REST

// AddTriples 添加三元组
// POST /api/v1/samg/triples
func AddTriples(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	var req AddTriplesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()
	err := svc.AddTriples(ctx, req.Triples)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	respondCreated(c, gin.H{"message": "triples added", "count": len(req.Triples)})
}

// GetTriple 获取单个三元组
// GET /api/v1/samg/triples/:id
func GetTriple(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	tripleID := c.Param("id")
	ctx := c.Request.Context()

	triple, err := svc.GetTriple(ctx, tripleID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if triple == nil {
		respondError(c, http.StatusNotFound, "triple not found")
		return
	}

	respondOK(c, triple)
}

// PLACEHOLDER_SAMG_REST2

// DeleteTriples 删除三元组
// DELETE /api/v1/samg/triples
func DeleteTriples(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	var req struct {
		IDs []string `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()
	err := svc.DeleteTriples(ctx, req.IDs)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(c, gin.H{"message": "triples deleted", "count": len(req.IDs)})
}

// GetRelations 获取节点关联关系
// GET /api/v1/samg/triples/:id/relations
func GetRelations(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	nodeID := c.Param("id")
	ctx := c.Request.Context()

	relations, err := svc.GetRelations(ctx, nodeID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(c, gin.H{"node_id": nodeID, "relations": relations, "count": len(relations)})
}

// PLACEHOLDER_SAMG_REST3

// ExtractTriples 从文本提取三元组
// POST /api/v1/samg/extract
func ExtractTriples(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	var req ExtractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()
	source := samg.TripleSource{
		SessionID:        req.SessionID,
		MessageIndex:     req.MessageIndex,
		AgentRole:        req.AgentRole,
		ExtractionMethod: samg.ExtractionRule,
	}

	triples, err := svc.ExtractFromMessage(ctx, req.Content, source)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(c, gin.H{"triples": triples, "count": len(triples)})
}

// Activate 扩展激活
// POST /api/v1/samg/activate
func Activate(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	var req ActivateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()
	result, err := svc.Activate(ctx, req.SourceIDs)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(c, result)
}

// PLACEHOLDER_SAMG_REST4

// FindPaths 查找路径
// POST /api/v1/samg/paths
func FindPaths(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	var req FindPathsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()
	paths, err := svc.FindPaths(ctx, req.SourceID, req.TargetID, req.MaxHops)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(c, gin.H{"source_id": req.SourceID, "target_id": req.TargetID, "paths": paths, "count": len(paths)})
}

// GetDecayConfig 获取衰减配置
// GET /api/v1/samg/decay
func GetDecayConfig(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	respondOK(c, svc.GetDecayConfig())
}

// UpdateDecayConfig 更新衰减配置
// PUT /api/v1/samg/decay
func UpdateDecayConfig(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	var req DecayConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	config := samg.DecayConfig{
		DecayRate:       req.DecayRate,
		BaseActivation:  req.BaseActivation,
		MinActivation:   req.MinActivation,
		HideThreshold:   req.HideThreshold,
		BoostOnAccess:   req.BoostOnAccess,
		TimeUnit:        req.TimeUnit,
		EnableAutoDecay: req.EnableAutoDecay,
	}

	svc.UpdateDecayConfig(config)
	respondOK(c, gin.H{"message": "decay config updated", "config": config})
}

// PLACEHOLDER_SAMG_REST5

// ApplyDecay 应用衰减
// POST /api/v1/samg/decay/apply
func ApplyDecay(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	ctx := c.Request.Context()
	decayed, hidden, err := svc.ApplyDecay(ctx)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(c, gin.H{"decayed_nodes": decayed, "hidden_nodes": hidden})
}

// enrichNodeActivations maps NodeActivation list to SAMGEntity-compatible format
// by looking up the full Entity from the store for each activation.
func enrichNodeActivations(svc *samg.SAMGService, ctx context.Context, activations []samg.NodeActivation) []gin.H {
	store := svc.GetStore()
	result := make([]gin.H, 0, len(activations))
	for _, na := range activations {
		node := gin.H{
			"@id":          na.NodeID,
			"@type":        []string{na.Type},
			"label":        na.Label,
			"activation":   na.Activation,
			"access_count": na.AccessCount,
			"hidden":       na.Hidden,
			"created_at":   na.CreatedTime,
			"updated_at":   na.LastAccessTime,
		}
		// Try to enrich with full entity data (pointers, description, aliases)
		if entity, err := store.GetEntity(ctx, na.NodeID); err == nil && entity != nil {
			node["description"] = entity.Description
			node["properties"] = entity.Properties
			node["aliases"] = entity.Aliases
			node["pointers"] = entity.Pointers
		}
		result = append(result, node)
	}
	return result
}

// GetVisibleNodes 获取可见节点
// GET /api/v1/samg/nodes/visible
func GetVisibleNodes(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	ctx := c.Request.Context()
	activations := svc.GetVisibleNodes(ctx)
	nodes := enrichNodeActivations(svc, ctx, activations)
	respondOK(c, gin.H{"nodes": nodes, "count": len(nodes)})
}

// GetHiddenNodes 获取隐藏节点
// GET /api/v1/samg/nodes/hidden
func GetHiddenNodes(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	ctx := c.Request.Context()
	activations := svc.GetHiddenNodes(ctx)
	nodes := enrichNodeActivations(svc, ctx, activations)
	respondOK(c, gin.H{"nodes": nodes, "count": len(nodes)})
}

// PLACEHOLDER_SAMG_REST6

// GetTopNodes 获取Top N节点
// GET /api/v1/samg/nodes/top
func GetTopNodes(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	n := 10
	if nStr := c.Query("n"); nStr != "" {
		if parsed, err := strconv.Atoi(nStr); err == nil && parsed > 0 {
			n = parsed
		}
	}

	ctx := c.Request.Context()
	activations := svc.GetTopNodes(ctx, n)
	nodes := enrichNodeActivations(svc, ctx, activations)
	respondOK(c, gin.H{"nodes": nodes, "count": len(nodes)})
}

// RecordAccess 记录节点访问
// POST /api/v1/samg/nodes/:id/access
func RecordAccess(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	nodeID := c.Param("id")
	ctx := c.Request.Context()

	err := svc.RecordAccess(ctx, nodeID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(c, gin.H{"message": "access recorded", "node_id": nodeID})
}

// GetActivationConfig 获取激活配置
// GET /api/v1/samg/activation
func GetActivationConfig(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	respondOK(c, svc.GetActivationConfig())
}

// PLACEHOLDER_SAMG_REST7

// UpdateActivationConfig 更新激活配置
// PUT /api/v1/samg/activation
func UpdateActivationConfig(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	var req ActivationConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	config := samg.ActivationConfig{
		InitialActivation: req.InitialActivation,
		DecayFactor:       req.DecayFactor,
		FiringThreshold:   req.FiringThreshold,
		MaxHops:           req.MaxHops,
		MaxActivatedNodes: req.MaxActivatedNodes,
		SpreadingFactor:   req.SpreadingFactor,
	}

	svc.UpdateActivationConfig(config)
	respondOK(c, gin.H{"message": "activation config updated", "config": config})
}

// ExportGraph 导出图谱
// GET /api/v1/samg/graph/export
func ExportGraph(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	ctx := c.Request.Context()
	graph, err := svc.ExportGraph(ctx)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(c, graph)
}

// ImportGraph 导入图谱
// POST /api/v1/samg/graph/import
func ImportGraph(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	var graph samg.JsonLdGraph
	if err := c.ShouldBindJSON(&graph); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()
	err := svc.ImportGraph(ctx, &graph)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(c, gin.H{"message": "graph imported", "triple_count": len(graph.Graph)})
}

// GetSAMGStats 获取SAMG统计信息
// GET /api/v1/samg/stats
func GetSAMGStats(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	ctx := c.Request.Context()
	stats, err := svc.GetStats(ctx)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	respondOK(c, stats)
}

// QueryMemory 通过 SAMG 神经索引查询相关记忆
// POST /api/v1/samg/query-memory
func QueryMemory(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	var req samg.QueryMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	ctx := c.Request.Context()
	result, err := svc.QueryMemory(ctx, req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Query memory failed: "+err.Error())
		return
	}

	respondOK(c, result)
}

// GetNodePointers 获取节点的所有指针
// GET /api/v1/samg/nodes/:id/pointers
func GetNodePointers(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	nodeID := c.Param("id")
	if nodeID == "" {
		respondError(c, http.StatusBadRequest, "Missing node ID")
		return
	}

	ctx := c.Request.Context()
	pointers, err := svc.GetNodePointers(ctx, nodeID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Get pointers failed: "+err.Error())
		return
	}

	respondOK(c, gin.H{"node_id": nodeID, "pointers": pointers, "count": len(pointers)})
}

// AddNodePointerRequest 添加指针请求
type AddNodePointerRequest struct {
	SourceID   string  `json:"source_id" binding:"required"`
	SourceType string  `json:"source_type" binding:"required"`
	Summary    string  `json:"summary"`
	LineRange  string  `json:"line_range,omitempty"`
	FilePath   string  `json:"file_path,omitempty"`
	Relevance  float64 `json:"relevance"`
}

// AddNodePointer 手动添加指针到节点
// POST /api/v1/samg/nodes/:id/pointers
func AddNodePointer(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	nodeID := c.Param("id")
	if nodeID == "" {
		respondError(c, http.StatusBadRequest, "Missing node ID")
		return
	}

	var req AddNodePointerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	ptr := samg.Pointer{
		SourceID:   req.SourceID,
		SourceType: req.SourceType,
		Summary:    req.Summary,
		LineRange:  req.LineRange,
		FilePath:   req.FilePath,
		Relevance:  req.Relevance,
	}

	ctx := c.Request.Context()
	if err := svc.AddNodePointer(ctx, nodeID, ptr); err != nil {
		respondError(c, http.StatusInternalServerError, "Add pointer failed: "+err.Error())
		return
	}

	respondCreated(c, gin.H{"message": "Pointer added"})
}

// ExtractWithPointers 提取三元组并绑定 Raw Archive 指针
// POST /api/v1/samg/extract-with-pointers
func ExtractWithPointers(c *gin.Context) {
	svc := samg.GetSAMGService()
	if svc == nil {
		respondError(c, http.StatusServiceUnavailable, "SAMG service not available")
		return
	}

	var req struct {
		Content       string `json:"content" binding:"required"`
		SessionID     string `json:"session_id"`
		MessageIndex  int    `json:"message_index"`
		AgentRole     string `json:"agent_role"`
		RawArchiveID  string `json:"raw_archive_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	source := samg.TripleSource{
		SessionID:        req.SessionID,
		MessageIndex:     req.MessageIndex,
		AgentRole:        req.AgentRole,
		ExtractionMethod: samg.ExtractionRule,
	}

	ctx := c.Request.Context()
	triples, err := svc.ExtractWithPointers(ctx, req.Content, source, req.RawArchiveID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Extract failed: "+err.Error())
		return
	}

	respondOK(c, gin.H{"triples": triples, "count": len(triples)})
}
