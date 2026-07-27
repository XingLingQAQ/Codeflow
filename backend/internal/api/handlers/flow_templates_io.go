// Package handlers - Floweng template JSON import/export/delete (experimental).
package handlers

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/floweng"
)

// ImportFlowTemplate handles POST /api/v1/flows/templates/import
func ImportFlowTemplate(c *gin.Context) {
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		respondError(c, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	if len(data) == 0 {
		respondError(c, http.StatusBadRequest, "empty request body")
		return
	}
	id, err := floweng.ImportTemplateJSON(data)
	if err != nil {
		if strings.Contains(err.Error(), "built-in") || strings.Contains(err.Error(), "override") {
			respondError(c, http.StatusConflict, err.Error())
			return
		}
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondCreated(c, gin.H{"id": string(id)})
}

// ExportFlowTemplate handles GET /api/v1/flows/templates/:tid/export
func ExportFlowTemplate(c *gin.Context) {
	tid := floweng.TemplateID(c.Param("tid"))
	data, err := floweng.ExportTemplateJSON(tid)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondInternalError(c, "export template", err)
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

// DeleteFlowTemplate handles DELETE /api/v1/flows/templates/:tid
func DeleteFlowTemplate(c *gin.Context) {
	tid := floweng.TemplateID(c.Param("tid"))
	if err := floweng.UnregisterTemplate(tid); err != nil {
		if strings.Contains(err.Error(), "built-in") {
			respondError(c, http.StatusConflict, err.Error())
			return
		}
		if strings.Contains(err.Error(), "not found") {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	respondOK(c, gin.H{"deleted": true, "id": string(tid)})
}
