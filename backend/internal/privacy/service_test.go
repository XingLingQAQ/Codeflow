// Package privacy - Privacy Service tests
package privacy

import (
	"context"
	"testing"
)

func TestPrivacyService_Create(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	if svc == nil {
		t.Error("expected non-nil service")
	}
}

func TestPrivacyService_MethodA_EncryptDecrypt(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	ctx := context.Background()
	plaintext := "Secret message for Method A"

	// Encrypt
	encrypted, err := svc.Encrypt(ctx, plaintext, nil)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	if encrypted.Ciphertext == "" {
		t.Error("expected non-empty ciphertext")
	}

	// Decrypt
	decrypted, err := svc.Decrypt(ctx, encrypted)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if !decrypted.Verified {
		t.Error("expected verification to pass")
	}

	if decrypted.Plaintext != plaintext {
		t.Errorf("expected %q, got %q", plaintext, decrypted.Plaintext)
	}
}

func TestPrivacyService_MethodB_ChainEncryptDecrypt(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	ctx := context.Background()
	plaintext := "Secret message for Method B (Chain)"

	// Chain Encrypt
	encrypted, err := svc.ChainEncrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("chain encryption failed: %v", err)
	}

	if encrypted.NodeID == "" {
		t.Error("expected non-empty node ID")
	}

	// Chain Decrypt
	decrypted, err := svc.ChainDecrypt(ctx, encrypted)
	if err != nil {
		t.Fatalf("chain decryption failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestPrivacyService_ChainKeyRotation(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	// Get initial chain info
	info1 := svc.GetChainInfo()
	initialNodes := info1.TotalNodes

	// Rotate chain key
	newNode, err := svc.RotateChainKey()
	if err != nil {
		t.Fatalf("chain key rotation failed: %v", err)
	}

	if newNode == nil {
		t.Error("expected non-nil new node")
	}

	// Verify chain has more nodes
	info2 := svc.GetChainInfo()
	if info2.TotalNodes != initialNodes+1 {
		t.Errorf("expected %d nodes, got %d", initialNodes+1, info2.TotalNodes)
	}
}

func TestPrivacyService_VerifyChain(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	// Rotate a few times
	for i := 0; i < 3; i++ {
		svc.RotateChainKey()
	}

	// Verify chain
	result, err := svc.VerifyChain()
	if err != nil {
		t.Fatalf("chain verification failed: %v", err)
	}

	if !result.Valid {
		t.Error("expected chain to be valid")
	}
}

func TestPrivacyService_PIIDetection(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	ctx := context.Background()
	text := "Contact: user@example.com, IP: 192.168.1.1"

	matches := svc.DetectPII(ctx, text)

	if len(matches) == 0 {
		t.Error("expected to detect PII")
	}

	foundEmail := false
	foundIP := false
	for _, m := range matches {
		if m.Type == PIITypeEmail {
			foundEmail = true
		}
		if m.Type == PIITypeIPAddress {
			foundIP = true
		}
	}

	if !foundEmail {
		t.Error("expected to detect email")
	}

	if !foundIP {
		t.Error("expected to detect IP address")
	}
}

func TestPrivacyService_PIIRedaction(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	ctx := context.Background()
	text := "Email: secret@company.com, Token: ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

	result := svc.RedactPII(ctx, text)

	if result.RedactedCount == 0 {
		t.Error("expected some redactions")
	}

	if result.RedactedText == text {
		t.Error("expected redacted text to be different")
	}
}

func TestPrivacyService_KeyManagement(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	ctx := context.Background()

	// Initially no active key
	activeKey := svc.GetActiveKey()
	if activeKey != nil {
		t.Error("expected no active key initially")
	}

	// Generate key
	keyInfo, err := svc.GenerateKey(ctx)
	if err != nil {
		t.Fatalf("key generation failed: %v", err)
	}

	if keyInfo.ID == "" {
		t.Error("expected non-empty key ID")
	}

	// Now should have active key
	activeKey = svc.GetActiveKey()
	if activeKey == nil {
		t.Error("expected active key after generation")
	}

	if activeKey.ID != keyInfo.ID {
		t.Errorf("expected active key ID %s, got %s", keyInfo.ID, activeKey.ID)
	}
}

func TestPrivacyService_KeyRotation(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	ctx := context.Background()

	// Generate initial key
	oldKey, _ := svc.GenerateKey(ctx)

	// Rotate key
	event, err := svc.RotateKey(ctx)
	if err != nil {
		t.Fatalf("key rotation failed: %v", err)
	}

	if event.OldKeyID != oldKey.ID {
		t.Errorf("expected old key ID %s, got %s", oldKey.ID, event.OldKeyID)
	}

	if event.NewKeyID == "" || event.NewKeyID == oldKey.ID {
		t.Error("expected new key ID to be different")
	}

	if event.Status != "completed" {
		t.Errorf("expected status 'completed', got %s", event.Status)
	}
}

func TestPrivacyService_DocumentOperations(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	ctx := context.Background()

	doc := &PrivacyAwareDocument{
		ID:           "doc-001",
		Content:      "Confidential document content",
		PrivacyLevel: LevelConfidential,
		Policy: PrivacyPolicy{
			Level:         LevelConfidential,
			EncryptAtRest: true,
		},
	}

	// Encrypt document
	encDoc, err := svc.EncryptDocument(ctx, doc)
	if err != nil {
		t.Fatalf("document encryption failed: %v", err)
	}

	if encDoc.Content != "" {
		t.Error("expected content to be cleared after encryption")
	}

	if encDoc.EncryptedContent == nil {
		t.Error("expected encrypted content")
	}

	// Decrypt document
	decDoc, err := svc.DecryptDocument(ctx, encDoc)
	if err != nil {
		t.Fatalf("document decryption failed: %v", err)
	}

	if decDoc.Content != "Confidential document content" {
		t.Errorf("unexpected decrypted content: %s", decDoc.Content)
	}
}

func TestPrivacyService_Metrics(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	ctx := context.Background()

	// Initial metrics should be zero
	metrics := svc.GetMetrics()
	if metrics.OperationCount != 0 {
		t.Error("expected zero operation count initially")
	}

	// Perform some operations
	for i := 0; i < 5; i++ {
		encrypted, _ := svc.Encrypt(ctx, "test data", nil)
		svc.Decrypt(ctx, encrypted)
	}

	// Check metrics
	metrics = svc.GetMetrics()
	if metrics.OperationCount != 10 {
		t.Errorf("expected 10 operations, got %d", metrics.OperationCount)
	}

	// Reset metrics
	svc.ResetMetrics()
	metrics = svc.GetMetrics()
	if metrics.OperationCount != 0 {
		t.Error("expected zero operation count after reset")
	}
}

func TestPrivacyService_GlobalInstance(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	SetPrivacyService(svc)

	retrieved := GetPrivacyService()
	if retrieved != svc {
		t.Error("expected same service instance")
	}
}

func TestPrivacyService_SetRedactionConfig(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	ctx := context.Background()

	// Set custom redaction config
	config := &RedactionConfig{
		EnabledTypes:  []PIIType{PIITypeEmail},
		RedactionMask: "[HIDDEN]",
	}
	svc.SetRedactionConfig(config)

	// Test redaction with new config
	text := "Email: test@example.com, IP: 192.168.1.1"
	result := svc.RedactPII(ctx, text)

	// Should only redact email, not IP
	foundEmail := false
	foundIP := false
	for _, m := range result.Matches {
		if m.Type == PIITypeEmail {
			foundEmail = true
		}
		if m.Type == PIITypeIPAddress {
			foundIP = true
		}
	}

	if !foundEmail {
		t.Error("expected to detect email")
	}

	if foundIP {
		t.Error("IP should not be detected with limited config")
	}
}

func TestPrivacyService_GetRedactor(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	redactor := svc.GetRedactor()
	if redactor == nil {
		t.Error("expected non-nil redactor")
	}

	// Verify redactor is functional
	ctx := context.Background()
	matches := redactor.Detect(ctx, "test@example.com")
	if len(matches) == 0 {
		t.Error("expected redactor to detect email")
	}
}

func TestPrivacyService_GetKeys(t *testing.T) {
	svc, err := NewPrivacyService("test-password", nil)
	if err != nil {
		t.Fatalf("failed to create privacy service: %v", err)
	}

	ctx := context.Background()

	// Initially no keys
	keys := svc.GetKeys()
	if len(keys) != 0 {
		t.Errorf("expected 0 keys initially, got %d", len(keys))
	}

	// Generate a key
	svc.GenerateKey(ctx)

	// Should have one key
	keys = svc.GetKeys()
	if len(keys) != 1 {
		t.Errorf("expected 1 key, got %d", len(keys))
	}
}
