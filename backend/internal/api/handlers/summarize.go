// Package handlers - Summarize API handlers
package handlers

import (
	"errors"
	"net/http"

	"github.com/codeflow/backend/internal/summarize"
	"github.com/gin-gonic/gin"
)

// respondSummarizeError 把 summarize service 的 typed error 分层映射到 HTTP
// （§28 T13.04.c、§31.2 E-06/E-07）：参数校验错误 400（envelope 含参数名与原因），
// 保留区超目标预算 422 budget_unsatisfiable（envelope 含预算与保留区 token 数，
// 不截坏保留区），其余执行故障 5xx。envelope 沿用本组 handler 现有 {"error": ...} 形状。
func respondSummarizeError(c *gin.Context, err error) {
	var validationErr *summarize.ValidationError
	if errors.As(err, &validationErr) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  validationErr.Error(),
			"field":  validationErr.Field,
			"reason": validationErr.Message,
		})
		return
	}
	var budgetErr *summarize.BudgetUnsatisfiableError
	if errors.As(err, &budgetErr) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":            budgetErr.Error(),
			"code":             "budget_unsatisfiable",
			"target_tokens":    budgetErr.TargetTokens,
			"preserved_tokens": budgetErr.PreservedTokens,
		})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

// SummarizeConversation summarizes a conversation.
// POST /api/v1/summarize/conversation
func SummarizeConversation(c *gin.Context) {
	var req summarize.SummarizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := summarize.GetSummarizer()
	summary, err := svc.SummarizeConversation(&req)
	if err != nil {
		respondSummarizeError(c, err)
		return
	}

	c.JSON(http.StatusOK, summary)
}

// CompressContext compresses context using 80/20 strategy.
// POST /api/v1/summarize/context
func CompressContext(c *gin.Context) {
	var req summarize.CompressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := summarize.GetSummarizer()
	result, err := svc.CompressContext(&req)
	if err != nil {
		respondSummarizeError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetDecisionSkeleton extracts decision skeleton from conversation.
// POST /api/v1/summarize/skeleton
func GetDecisionSkeleton(c *gin.Context) {
	var req struct {
		Messages []summarize.Message `json:"messages" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	svc := summarize.GetSummarizer()
	skeleton, err := svc.ExtractSkeleton(req.Messages)
	if err != nil {
		respondSummarizeError(c, err)
		return
	}

	c.JSON(http.StatusOK, skeleton)
}
