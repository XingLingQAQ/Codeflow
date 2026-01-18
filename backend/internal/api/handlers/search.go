// Package handlers - Search API handlers
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/search"
)

// VectorSearch handles POST /api/v1/search/vector
func VectorSearch(c *gin.Context) {
	var req search.VectorSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := search.GetSearchService()
	result, err := svc.VectorSearch(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Vector search failed: "+err.Error())
		return
	}

	respondOK(c, result)
}

// FulltextSearch handles POST /api/v1/search/fulltext
func FulltextSearch(c *gin.Context) {
	var req search.FulltextSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := search.GetSearchService()
	result, err := svc.FulltextSearch(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Fulltext search failed: "+err.Error())
		return
	}

	respondOK(c, result)
}

// GraphSearch handles POST /api/v1/search/graph
func GraphSearch(c *gin.Context) {
	var req search.GraphSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := search.GetSearchService()
	result, err := svc.GraphSearch(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Graph search failed: "+err.Error())
		return
	}

	respondOK(c, result)
}

// HybridSearch handles POST /api/v1/search/hybrid
func HybridSearch(c *gin.Context) {
	var req search.HybridSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	svc := search.GetSearchService()
	result, err := svc.HybridSearch(c.Request.Context(), &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Hybrid search failed: "+err.Error())
		return
	}

	respondOK(c, result)
}
