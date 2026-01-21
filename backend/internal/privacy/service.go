// Package privacy - Privacy Service implementation
package privacy

import (
	"context"
	"sync"
)

// PrivacyService implements IPrivacyService interface
type PrivacyService struct {
	manager   *PrivacyManager
	chainKey  *ChainKeyDerivation
	redactor  *PIIRedactor
	mu        sync.RWMutex
}

var (
	globalPrivacyService *PrivacyService
	globalServiceMu      sync.RWMutex
)

// NewPrivacyService creates a new privacy service
func NewPrivacyService(masterPassword string, config *EncryptionConfig) (*PrivacyService, error) {
	manager := NewPrivacyManager(masterPassword, config)

	chainKey, err := NewChainKeyDerivation(masterPassword)
	if err != nil {
		return nil, err
	}

	redactor := NewPIIRedactor(nil)

	return &PrivacyService{
		manager:  manager,
		chainKey: chainKey,
		redactor: redactor,
	}, nil
}

// SetPrivacyService sets the global privacy service instance
func SetPrivacyService(svc *PrivacyService) {
	globalServiceMu.Lock()
	defer globalServiceMu.Unlock()
	globalPrivacyService = svc
}

// GetPrivacyService returns the global privacy service instance
func GetPrivacyService() *PrivacyService {
	globalServiceMu.RLock()
	defer globalServiceMu.RUnlock()
	return globalPrivacyService
}

// === Method A: AES-CBC Encryption ===

// Encrypt encrypts data using AES-CBC (Method A)
func (s *PrivacyService) Encrypt(ctx context.Context, plaintext string, policy *PrivacyPolicy) (*EncryptedData, error) {
	return s.manager.Encrypt(ctx, plaintext, policy)
}

// Decrypt decrypts data using AES-CBC (Method A)
func (s *PrivacyService) Decrypt(ctx context.Context, encrypted *EncryptedData) (*DecryptedData, error) {
	return s.manager.Decrypt(ctx, encrypted)
}

// === Method B: Chain Key Derivation ===

// ChainEncrypt encrypts data using chain key derivation (Method B)
func (s *PrivacyService) ChainEncrypt(ctx context.Context, plaintext string) (*ChainEncryptedData, error) {
	return s.chainKey.Encrypt(ctx, plaintext)
}

// ChainDecrypt decrypts data using chain key derivation (Method B)
func (s *PrivacyService) ChainDecrypt(ctx context.Context, encrypted *ChainEncryptedData) (string, error) {
	return s.chainKey.Decrypt(ctx, encrypted)
}

// RotateChainKey rotates the chain key
func (s *PrivacyService) RotateChainKey() (*ChainNode, error) {
	return s.chainKey.RotateKey()
}

// GetChainInfo returns chain information
func (s *PrivacyService) GetChainInfo() *ChainInfo {
	return s.chainKey.GetChainInfo()
}

// VerifyChain verifies the chain integrity
func (s *PrivacyService) VerifyChain() (*ChainVerificationResult, error) {
	return s.chainKey.VerifyChain()
}

// === PII Redaction ===

// DetectPII detects PII in text
func (s *PrivacyService) DetectPII(ctx context.Context, text string) []PIIMatch {
	return s.redactor.Detect(ctx, text)
}

// RedactPII redacts PII from text
func (s *PrivacyService) RedactPII(ctx context.Context, text string) *RedactionResult {
	return s.redactor.Redact(ctx, text)
}

// === Key Management ===

// GenerateKey generates a new encryption key
func (s *PrivacyService) GenerateKey(ctx context.Context) (*KeyInfo, error) {
	return s.manager.GenerateKey(ctx)
}

// RotateKey rotates the encryption key
func (s *PrivacyService) RotateKey(ctx context.Context) (*KeyRotationEvent, error) {
	return s.manager.RotateKey(ctx)
}

// GetActiveKey returns the active encryption key
func (s *PrivacyService) GetActiveKey() *KeyInfo {
	return s.manager.GetActiveKey()
}

// GetKeys returns all encryption keys
func (s *PrivacyService) GetKeys() []KeyInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var keys []KeyInfo
	activeKey := s.manager.GetActiveKey()
	if activeKey != nil {
		keys = append(keys, *activeKey)
	}
	return keys
}

// === Document Operations ===

// EncryptDocument encrypts a document
func (s *PrivacyService) EncryptDocument(ctx context.Context, doc *PrivacyAwareDocument) (*PrivacyAwareDocument, error) {
	return s.manager.EncryptDocument(ctx, doc)
}

// DecryptDocument decrypts a document
func (s *PrivacyService) DecryptDocument(ctx context.Context, doc *PrivacyAwareDocument) (*PrivacyAwareDocument, error) {
	return s.manager.DecryptDocument(ctx, doc)
}

// === Privacy Search ===

// Search performs privacy-aware search
func (s *PrivacyService) Search(ctx context.Context, request *EncryptedSearchRequest) ([]EncryptedSearchResult, error) {
	return s.manager.Search(ctx, request)
}

// === Metrics ===

// GetMetrics returns encryption metrics
func (s *PrivacyService) GetMetrics() EncryptionMetrics {
	return s.manager.GetMetrics()
}

// ResetMetrics resets encryption metrics
func (s *PrivacyService) ResetMetrics() {
	s.manager.ResetMetrics()
}

// === Redactor Configuration ===

// SetRedactionConfig sets the redaction configuration
func (s *PrivacyService) SetRedactionConfig(config *RedactionConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.redactor = NewPIIRedactor(config)
}

// GetRedactor returns the PII redactor
func (s *PrivacyService) GetRedactor() *PIIRedactor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.redactor
}
