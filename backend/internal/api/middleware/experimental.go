// Package middleware provides HTTP middleware for the API server.
package middleware

import "github.com/gin-gonic/gin"

// Experimental marks API routes as experimental/not-fully-implemented.
// Adds X-Feature-Status header and sets a context flag for handlers.
func Experimental(feature string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Feature-Status", "experimental")
		c.Header("X-Feature-Warning", feature+" functionality is experimental and may return placeholder data")
		c.Set("experimental_feature", feature)
		c.Next()
	}
}

// IsExperimental returns true if the current request is under an experimental route.
func IsExperimental(c *gin.Context) bool {
	_, exists := c.Get("experimental_feature")
	return exists
}
