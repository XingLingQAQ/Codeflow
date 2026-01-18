// Package handlers provides HTTP request handlers for the API.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response represents a standard API response.
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// HealthCheck handles GET /health
func HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, Response{
		Success: true,
		Data: gin.H{
			"status":  "healthy",
			"version": "0.1.0",
		},
	})
}

// respondOK sends a successful response.
func respondOK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Success: true,
		Data:    data,
	})
}

// respondCreated sends a 201 Created response.
func respondCreated(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, Response{
		Success: true,
		Data:    data,
	})
}

// respondError sends an error response.
func respondError(c *gin.Context, status int, message string) {
	c.JSON(status, Response{
		Success: false,
		Error:   message,
	})
}

// respondNotImplemented sends a 501 Not Implemented response.
func respondNotImplemented(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, Response{
		Success: false,
		Error:   "Not implemented yet",
	})
}
