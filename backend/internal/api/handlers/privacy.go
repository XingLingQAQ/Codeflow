// Package handlers - Privacy API handlers
package handlers

import (
	"context"
	"net/http"

	"github.com/codeflow/backend/internal/privacy"
	"github.com/gin-gonic/gin"
)

// EncryptRequest represents a request to encrypt data
type EncryptRequest struct {
	Plaintext string                 `json:"plaintext" binding:"required"`
	Method    string                 `json:"method"` // "standard" or "chain"
	Policy    *privacy.PrivacyPolicy `json:"policy,omitempty"`
}

// EncryptResponse represents the encryption response
type EncryptResponse struct {
	Method          string                     `json:"method"`
	EncryptedData   *privacy.EncryptedData     `json:"encrypted_data,omitempty"`
	ChainEncrypted  *privacy.ChainEncryptedData `json:"chain_encrypted,omitempty"`
}

// DecryptRequest represents a request to decrypt data
type DecryptRequest struct {
	Method         string                      `json:"method" binding:"required"` // "standard" or "chain"
	EncryptedData  *privacy.EncryptedData      `json:"encrypted_data,omitempty"`
	ChainEncrypted *privacy.ChainEncryptedData `json:"chain_encrypted,omitempty"`
}

// DecryptResponse represents the decryption response
type DecryptResponse struct {
	Plaintext string `json:"plaintext"`
	Verified  bool   `json:"verified"`
}

// RedactRequest represents a request to redact PII
type RedactRequest struct {
	Text           string             `json:"text" binding:"required"`
	EnabledTypes   []privacy.PIIType  `json:"enabled_types,omitempty"`
	RedactionMask  string             `json:"redaction_mask,omitempty"`
	PreserveLength bool               `json:"preserve_length,omitempty"`
	IncludeOriginal bool              `json:"include_original,omitempty"`
}

// KeyRequest represents a request to generate or rotate keys
type KeyRequest struct {
	Action string `json:"action" binding:"required"` // "generate", "rotate", "rotate_chain"
}

// Encrypt encrypts data using the privacy service.
// POST /api/v1/privacy/encrypt
func Encrypt(c *gin.Context) {
	svc := privacy.GetPrivacyService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "privacy service not available"})
		return
	}

	var req EncryptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	response := &EncryptResponse{Method: req.Method}

	if req.Method == "chain" {
		// Use chain key derivation (Method B)
		encrypted, err := svc.ChainEncrypt(ctx, req.Plaintext)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		response.ChainEncrypted = encrypted
	} else {
		// Use standard AES-CBC (Method A) - default
		response.Method = "standard"
		encrypted, err := svc.Encrypt(ctx, req.Plaintext, req.Policy)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		response.EncryptedData = encrypted
	}

	c.JSON(http.StatusOK, response)
}

// Decrypt decrypts data using the privacy service.
// POST /api/v1/privacy/decrypt
func Decrypt(c *gin.Context) {
	svc := privacy.GetPrivacyService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "privacy service not available"})
		return
	}

	var req DecryptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	response := &DecryptResponse{}

	if req.Method == "chain" {
		if req.ChainEncrypted == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "chain_encrypted is required for chain method"})
			return
		}
		plaintext, err := svc.ChainDecrypt(ctx, req.ChainEncrypted)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		response.Plaintext = plaintext
		response.Verified = true
	} else {
		if req.EncryptedData == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "encrypted_data is required for standard method"})
			return
		}
		decrypted, err := svc.Decrypt(ctx, req.EncryptedData)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		response.Plaintext = decrypted.Plaintext
		response.Verified = decrypted.Verified
	}

	c.JSON(http.StatusOK, response)
}

// Redact redacts PII from text.
// POST /api/v1/privacy/redact
func Redact(c *gin.Context) {
	svc := privacy.GetPrivacyService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "privacy service not available"})
		return
	}

	var req RedactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	// Configure redactor if custom settings provided
	if len(req.EnabledTypes) > 0 || req.RedactionMask != "" {
		config := &privacy.RedactionConfig{
			EnabledTypes:    req.EnabledTypes,
			RedactionMask:   req.RedactionMask,
			PreserveLength:  req.PreserveLength,
			IncludeOriginal: req.IncludeOriginal,
		}
		svc.SetRedactionConfig(config)
	}

	result := svc.RedactPII(ctx, req.Text)
	c.JSON(http.StatusOK, result)
}

// DetectPII detects PII in text without redacting.
// POST /api/v1/privacy/detect
func DetectPII(c *gin.Context) {
	svc := privacy.GetPrivacyService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "privacy service not available"})
		return
	}

	var req struct {
		Text string `json:"text" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	matches := svc.DetectPII(ctx, req.Text)

	c.JSON(http.StatusOK, gin.H{
		"matches": matches,
		"count":   len(matches),
	})
}

// GetKeys returns information about encryption keys.
// GET /api/v1/privacy/keys
func GetKeys(c *gin.Context) {
	svc := privacy.GetPrivacyService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "privacy service not available"})
		return
	}

	keys := svc.GetKeys()
	activeKey := svc.GetActiveKey()
	chainInfo := svc.GetChainInfo()

	c.JSON(http.StatusOK, gin.H{
		"keys":       keys,
		"active_key": activeKey,
		"chain_info": chainInfo,
	})
}

// ManageKeys generates or rotates encryption keys.
// POST /api/v1/privacy/keys
func ManageKeys(c *gin.Context) {
	svc := privacy.GetPrivacyService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "privacy service not available"})
		return
	}

	var req KeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	switch req.Action {
	case "generate":
		keyInfo, err := svc.GenerateKey(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"action":   "generate",
			"key_info": keyInfo,
		})

	case "rotate":
		event, err := svc.RotateKey(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"action": "rotate",
			"event":  event,
		})

	case "rotate_chain":
		node, err := svc.RotateChainKey()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"action": "rotate_chain",
			"node": gin.H{
				"id":             node.ID,
				"index":          node.Index,
				"key_hash":       node.KeyHash,
				"integrity_hash": node.IntegrityHash,
				"created_at":     node.CreatedAt,
			},
		})

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid action, must be 'generate', 'rotate', or 'rotate_chain'"})
	}
}

// VerifyChain verifies the integrity of the chain key derivation.
// POST /api/v1/privacy/verify-chain
func VerifyChain(c *gin.Context) {
	svc := privacy.GetPrivacyService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "privacy service not available"})
		return
	}

	result, err := svc.VerifyChain()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetMetrics returns encryption performance metrics.
// GET /api/v1/privacy/metrics
func GetPrivacyMetrics(c *gin.Context) {
	svc := privacy.GetPrivacyService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "privacy service not available"})
		return
	}

	metrics := svc.GetMetrics()
	c.JSON(http.StatusOK, metrics)
}

// ResetMetrics resets encryption performance metrics.
// POST /api/v1/privacy/metrics/reset
func ResetPrivacyMetrics(c *gin.Context) {
	svc := privacy.GetPrivacyService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "privacy service not available"})
		return
	}

	svc.ResetMetrics()
	c.JSON(http.StatusOK, gin.H{"message": "metrics reset successfully"})
}
