// Package handlers - Privacy API handlers
package handlers

import (
	"net/http"

	"github.com/codeflow/backend/internal/audit"
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

func recordPrivacyAudit(ctx *gin.Context, action string, outcome audit.AuditOutcome, severity audit.AuditSeverity, details map[string]interface{}) {
	requestCtx := ctx.Request.Context()
	_, _ = audit.Record(requestCtx, &audit.AuditLogEntry{
		EventType: audit.EventPrivacy,
		Severity:  severity,
		Actor:     audit.AuditActor{Type: "user"},
		Resource: audit.AuditResource{
			Type: "privacy",
			ID:   action,
			Name: action,
		},
		Action:  action,
		Outcome: outcome,
		Details: details,
	})
}

func privacyPolicyLevel(policy *privacy.PrivacyPolicy) string {
	if policy == nil {
		return ""
	}
	return string(policy.Level)
}

func piiTypesToStrings(types []privacy.PIIType) []string {
	if len(types) == 0 {
		return nil
	}
	result := make([]string, 0, len(types))
	for _, item := range types {
		result = append(result, string(item))
	}
	return result
}

func piiMatchesToTypes(matches []privacy.PIIMatch) []string {
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[privacy.PIIType]struct{}, len(matches))
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		if _, ok := seen[match.Type]; ok {
			continue
		}
		seen[match.Type] = struct{}{}
		result = append(result, string(match.Type))
	}
	return result
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

	ctx := c.Request.Context()
	response := &EncryptResponse{Method: req.Method}

	if req.Method == "chain" {
		// Use chain key derivation (Method B)
		encrypted, err := svc.ChainEncrypt(ctx, req.Plaintext)
		if err != nil {
			recordPrivacyAudit(c, "chain_encrypt", audit.OutcomeFailure, audit.SeverityError, map[string]interface{}{
				"method": "chain",
				"error":  err.Error(),
			})
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		response.ChainEncrypted = encrypted
		recordPrivacyAudit(c, "chain_encrypt", audit.OutcomeSuccess, audit.SeverityInfo, map[string]interface{}{
			"method":    "chain",
			"node_id":   encrypted.NodeID,
			"algorithm": encrypted.Algorithm,
		})
	} else {
		// Use standard AES-CBC (Method A) - default
		response.Method = "standard"
		encrypted, err := svc.Encrypt(ctx, req.Plaintext, req.Policy)
		if err != nil {
			recordPrivacyAudit(c, "encrypt", audit.OutcomeFailure, audit.SeverityError, map[string]interface{}{
				"method":       "standard",
				"policy_level": privacyPolicyLevel(req.Policy),
				"error":        err.Error(),
			})
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		response.EncryptedData = encrypted
		recordPrivacyAudit(c, "encrypt", audit.OutcomeSuccess, audit.SeverityInfo, map[string]interface{}{
			"method":       "standard",
			"policy_level": privacyPolicyLevel(req.Policy),
			"algorithm":    string(encrypted.Algorithm),
		})
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

	ctx := c.Request.Context()
	response := &DecryptResponse{}

	if req.Method == "chain" {
		if req.ChainEncrypted == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "chain_encrypted is required for chain method"})
			return
		}
		plaintext, err := svc.ChainDecrypt(ctx, req.ChainEncrypted)
		if err != nil {
			recordPrivacyAudit(c, "chain_decrypt", audit.OutcomeFailure, audit.SeverityError, map[string]interface{}{
				"method":  "chain",
				"node_id": req.ChainEncrypted.NodeID,
				"error":   err.Error(),
			})
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		response.Plaintext = plaintext
		response.Verified = true
		recordPrivacyAudit(c, "chain_decrypt", audit.OutcomeSuccess, audit.SeverityInfo, map[string]interface{}{
			"method":   "chain",
			"node_id":  req.ChainEncrypted.NodeID,
			"verified": true,
		})
	} else {
		if req.EncryptedData == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "encrypted_data is required for standard method"})
			return
		}
		decrypted, err := svc.Decrypt(ctx, req.EncryptedData)
		if err != nil {
			recordPrivacyAudit(c, "decrypt", audit.OutcomeFailure, audit.SeverityError, map[string]interface{}{
				"method":    "standard",
				"algorithm": string(req.EncryptedData.Algorithm),
				"error":     err.Error(),
			})
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		response.Plaintext = decrypted.Plaintext
		response.Verified = decrypted.Verified
		recordPrivacyAudit(c, "decrypt", audit.OutcomeSuccess, audit.SeverityInfo, map[string]interface{}{
			"method":    "standard",
			"algorithm": string(req.EncryptedData.Algorithm),
			"verified":  decrypted.Verified,
		})
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

	ctx := c.Request.Context()

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
	recordPrivacyAudit(c, "redact", audit.OutcomeSuccess, audit.SeverityInfo, map[string]interface{}{
		"enabled_types":    piiTypesToStrings(req.EnabledTypes),
		"redaction_mask":   req.RedactionMask,
		"preserve_length":  req.PreserveLength,
		"include_original": req.IncludeOriginal,
		"redacted_count":   result.RedactedCount,
		"match_count":      len(result.Matches),
	})
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

	ctx := c.Request.Context()
	matches := svc.DetectPII(ctx, req.Text)
	recordPrivacyAudit(c, "detect_pii", audit.OutcomeSuccess, audit.SeverityInfo, map[string]interface{}{
		"count":     len(matches),
		"pii_types": piiMatchesToTypes(matches),
	})

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

	ctx := c.Request.Context()

	switch req.Action {
	case "generate":
		keyInfo, err := svc.GenerateKey(ctx)
		if err != nil {
			recordPrivacyAudit(c, "generate_key", audit.OutcomeFailure, audit.SeverityError, map[string]interface{}{
				"action": "generate",
				"error":  err.Error(),
			})
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		recordPrivacyAudit(c, "generate_key", audit.OutcomeSuccess, audit.SeverityInfo, map[string]interface{}{
			"action":    "generate",
			"key_id":    keyInfo.ID,
			"algorithm": string(keyInfo.Algorithm),
		})
		c.JSON(http.StatusOK, gin.H{
			"action":   "generate",
			"key_info": keyInfo,
		})

	case "rotate":
		event, err := svc.RotateKey(ctx)
		if err != nil {
			recordPrivacyAudit(c, "rotate_key", audit.OutcomeFailure, audit.SeverityError, map[string]interface{}{
				"action": "rotate",
				"error":  err.Error(),
			})
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		recordPrivacyAudit(c, "rotate_key", audit.OutcomeSuccess, audit.SeverityInfo, map[string]interface{}{
			"action":                  "rotate",
			"old_key_id":              event.OldKeyID,
			"new_key_id":              event.NewKeyID,
			"documents_re_encrypted":  event.DocumentsReEncrypted,
			"status":                  event.Status,
		})
		c.JSON(http.StatusOK, gin.H{
			"action": "rotate",
			"event":  event,
		})

	case "rotate_chain":
		node, err := svc.RotateChainKey()
		if err != nil {
			recordPrivacyAudit(c, "rotate_chain_key", audit.OutcomeFailure, audit.SeverityError, map[string]interface{}{
				"action": "rotate_chain",
				"error":  err.Error(),
			})
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		recordPrivacyAudit(c, "rotate_chain_key", audit.OutcomeSuccess, audit.SeverityInfo, map[string]interface{}{
			"action":         "rotate_chain",
			"node_id":        node.ID,
			"index":          node.Index,
			"key_hash":       node.KeyHash,
			"integrity_hash": node.IntegrityHash,
		})
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
		recordPrivacyAudit(c, "manage_keys", audit.OutcomeFailure, audit.SeverityWarning, map[string]interface{}{
			"action": req.Action,
			"error":  "invalid action",
		})
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
		recordPrivacyAudit(c, "verify_chain", audit.OutcomeFailure, audit.SeverityError, map[string]interface{}{
			"error": err.Error(),
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	recordPrivacyAudit(c, "verify_chain", audit.OutcomeSuccess, audit.SeverityInfo, map[string]interface{}{
		"valid":         result.Valid,
		"checked_nodes": result.CheckedNodes,
		"invalid_nodes": len(result.InvalidNodes),
	})

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
