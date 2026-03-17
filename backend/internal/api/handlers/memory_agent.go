package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/memory"
)

// MemoryAgentIngest handles POST /api/v1/memory/agent/ingest
func MemoryAgentIngest(c *gin.Context) {
	var req memory.IngestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	agent, err := memory.GetMemoryAgent()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Agent init failed: "+err.Error())
		return
	}

	result, err := agent.Ingest(c.Request.Context(), req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Ingest failed: "+err.Error())
		return
	}

	respondCreated(c, result)
}

// MemoryAgentRetrieve handles POST /api/v1/memory/agent/retrieve
func MemoryAgentRetrieve(c *gin.Context) {
	var req memory.RetrieveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	agent, err := memory.GetMemoryAgent()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Agent init failed: "+err.Error())
		return
	}

	result, err := agent.Retrieve(c.Request.Context(), req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Retrieve failed: "+err.Error())
		return
	}

	respondOK(c, result)
}

// MemoryAgentContext handles POST /api/v1/memory/agent/context
func MemoryAgentContext(c *gin.Context) {
	var req memory.ContextRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	agent, err := memory.GetMemoryAgent()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Agent init failed: "+err.Error())
		return
	}

	result, err := agent.AssembleContext(c.Request.Context(), req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Context assembly failed: "+err.Error())
		return
	}

	respondOK(c, result)
}
