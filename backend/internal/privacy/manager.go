package privacy

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/scrypt"
)

// PrivacyManager 隐私管理器实现
type PrivacyManager struct {
	config         EncryptionConfig
	masterPassword string
	activeKey      *KeyInfo
	keys           map[string][]byte
	documents      map[string]*PrivacyAwareDocument
	metrics        EncryptionMetrics
	mu             sync.RWMutex
}

// NewPrivacyManager 创建隐私管理器
func NewPrivacyManager(masterPassword string, config *EncryptionConfig) *PrivacyManager {
	cfg := DefaultEncryptionConfig
	if config != nil {
		if config.Algorithm != "" {
			cfg.Algorithm = config.Algorithm
		}
		if config.KeyDerivation != "" {
			cfg.KeyDerivation = config.KeyDerivation
		}
		if config.Iterations > 0 {
			cfg.Iterations = config.Iterations
		}
		if config.SaltLength > 0 {
			cfg.SaltLength = config.SaltLength
		}
		if config.IVLength > 0 {
			cfg.IVLength = config.IVLength
		}
	}

	return &PrivacyManager{
		config:         cfg,
		masterPassword: masterPassword,
		keys:           make(map[string][]byte),
		documents:      make(map[string]*PrivacyAwareDocument),
	}
}

// Encrypt 加密数据
func (m *PrivacyManager) Encrypt(_ context.Context, plaintext string, policy *PrivacyPolicy) (*EncryptedData, error) {
	startTime := time.Now()

	// 生成随机 salt 和 IV
	salt := make([]byte, m.config.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	// 派生密钥
	key, err := m.deriveKeyForAlgorithm(m.masterPassword, salt, AES256GCM)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	// AES-GCM authenticated encryption.
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	// Keep the GCM authentication tag separate for wire compatibility.
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	sealed := gcm.Seal(nil, iv, []byte(plaintext), []byte(AES256GCM))
	tagOffset := len(sealed) - gcm.Overhead()
	ciphertext, tag := sealed[:tagOffset], sealed[tagOffset:]

	// 更新性能指标
	elapsed := time.Since(startTime)
	m.updateMetrics(elapsed, len(plaintext), "encrypt")

	return &EncryptedData{
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		IV:         base64.StdEncoding.EncodeToString(iv),
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Algorithm:  AES256GCM,
		Tag:        base64.StdEncoding.EncodeToString(tag),
	}, nil
}

// Decrypt 解密数据
func (m *PrivacyManager) Decrypt(_ context.Context, encrypted *EncryptedData) (*DecryptedData, error) {
	if encrypted == nil {
		return &DecryptedData{Verified: false}, nil
	}
	startTime := time.Now()

	// 解码 salt 和 IV
	salt, err := base64.StdEncoding.DecodeString(encrypted.Salt)
	if err != nil {
		return &DecryptedData{Verified: false}, nil
	}

	iv, err := base64.StdEncoding.DecodeString(encrypted.IV)
	if err != nil {
		return &DecryptedData{Verified: false}, nil
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encrypted.Ciphertext)
	if err != nil {
		return &DecryptedData{Verified: false}, nil
	}

	// 派生密钥
	algorithm := encrypted.Algorithm
	if algorithm == "" {
		algorithm = AES256CBC
	}
	key, err := m.deriveKeyForAlgorithm(m.masterPassword, salt, algorithm)
	if err != nil {
		return &DecryptedData{Verified: false}, nil
	}

	// Dispatch authenticated records and the read-only legacy CBC format.
	block, err := aes.NewCipher(key)
	if err != nil {
		return &DecryptedData{Verified: false}, nil
	}

	var plaintext []byte
	legacy := false
	switch algorithm {
	case AES256GCM:
		if len(iv) != 12 {
			return &DecryptedData{Verified: false}, nil
		}
		tag, decodeErr := base64.StdEncoding.DecodeString(encrypted.Tag)
		if decodeErr != nil {
			return &DecryptedData{Verified: false}, nil
		}
		gcm, gcmErr := cipher.NewGCM(block)
		if gcmErr != nil || len(tag) != gcm.Overhead() {
			return &DecryptedData{Verified: false}, nil
		}
		plaintext, err = gcm.Open(nil, iv, append(ciphertext, tag...), []byte(AES256GCM))
		if err != nil {
			return &DecryptedData{Verified: false}, nil
		}
	case AES256CBC, AES128CBC:
		legacy = true
		if len(iv) != aes.BlockSize || len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
			return &DecryptedData{Verified: false}, nil
		}
		plaintext = make([]byte, len(ciphertext))
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	default:
		return &DecryptedData{Verified: false}, nil
	}

	// Only legacy CBC records require padding removal.
	unpaddedPlain := plaintext
	if legacy {
		unpaddedPlain, err = pkcs7Unpad(plaintext)
		if err != nil {
			return &DecryptedData{Verified: false}, nil
		}
	}

	// 更新性能指标
	elapsed := time.Since(startTime)
	m.updateMetrics(elapsed, len(unpaddedPlain), "decrypt")

	return &DecryptedData{
		Plaintext: string(unpaddedPlain),
		Verified:  true,
		Legacy:    legacy,
	}, nil
}

// GenerateKey 生成密钥
func (m *PrivacyManager) GenerateKey(_ context.Context) (*KeyInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	keyID := fmt.Sprintf("key_%d_%x", time.Now().UnixMilli(), randBytes(4))
	keyBuffer := make([]byte, 32)
	if _, err := rand.Read(keyBuffer); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	m.keys[keyID] = keyBuffer

	keyInfo := &KeyInfo{
		ID:        keyID,
		Algorithm: AES256GCM,
		CreatedAt: time.Now().UnixMilli(),
	}

	m.activeKey = keyInfo
	return keyInfo, nil
}

// RotateKey 轮换密钥
func (m *PrivacyManager) RotateKey(ctx context.Context) (*KeyRotationEvent, error) {
	m.mu.Lock()
	oldKeyID := ""
	if m.activeKey != nil {
		oldKeyID = m.activeKey.ID
	}
	// 复制文档 ID 列表
	docIDs := make([]string, 0, len(m.documents))
	for id := range m.documents {
		docIDs = append(docIDs, id)
	}
	m.mu.Unlock()

	newKey, err := m.GenerateKey(ctx)
	if err != nil {
		return nil, err
	}

	event := &KeyRotationEvent{
		OldKeyID:             oldKeyID,
		NewKeyID:             newKey.ID,
		RotatedAt:            time.Now().UnixMilli(),
		DocumentsReEncrypted: 0,
		Status:               "in_progress",
	}

	// 重新加密所有文档（在锁外进行）
	for _, docID := range docIDs {
		m.mu.RLock()
		doc, exists := m.documents[docID]
		m.mu.RUnlock()

		if !exists || doc.EncryptedContent == nil {
			continue
		}

		decrypted, err := m.Decrypt(ctx, doc.EncryptedContent)
		if err == nil && decrypted.Verified {
			newEncrypted, err := m.Encrypt(ctx, decrypted.Plaintext, &doc.Policy)
			if err == nil {
				m.mu.Lock()
				if d, ok := m.documents[docID]; ok {
					d.EncryptedContent = newEncrypted
				}
				m.mu.Unlock()
				event.DocumentsReEncrypted++
			}
		}
	}

	event.Status = "completed"
	return event, nil
}

// GetActiveKey 获取当前活动密钥
func (m *PrivacyManager) GetActiveKey() *KeyInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeKey
}

// EncryptDocument 加密文档
func (m *PrivacyManager) EncryptDocument(ctx context.Context, doc *PrivacyAwareDocument) (*PrivacyAwareDocument, error) {
	if doc.Policy.EncryptAtRest && doc.Content != "" {
		encrypted, err := m.Encrypt(ctx, doc.Content, &doc.Policy)
		if err != nil {
			return nil, err
		}
		doc.EncryptedContent = encrypted
		doc.Content = "" // 清除明文
	}

	m.mu.Lock()
	m.documents[doc.ID] = doc
	m.mu.Unlock()

	return doc, nil
}

// DecryptDocument 解密文档
func (m *PrivacyManager) DecryptDocument(ctx context.Context, doc *PrivacyAwareDocument) (*PrivacyAwareDocument, error) {
	if doc.EncryptedContent != nil {
		decrypted, err := m.Decrypt(ctx, doc.EncryptedContent)
		if err != nil {
			return nil, err
		}
		if decrypted.Verified {
			doc.Content = decrypted.Plaintext
			if decrypted.Legacy {
				migrated, migrateErr := m.Encrypt(ctx, decrypted.Plaintext, &doc.Policy)
				if migrateErr != nil {
					return nil, fmt.Errorf("migrate legacy encryption: %w", migrateErr)
				}
				doc.EncryptedContent = migrated
				m.mu.Lock()
				if stored, ok := m.documents[doc.ID]; ok {
					stored.EncryptedContent = migrated
				}
				m.mu.Unlock()
			}
		}
	}
	return doc, nil
}

// Search 隐私检索
func (m *PrivacyManager) Search(ctx context.Context, request *EncryptedSearchRequest) ([]EncryptedSearchResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []EncryptedSearchResult
	allowedLevels := request.PrivacyLevels
	if len(allowedLevels) == 0 {
		allowedLevels = []PrivacyLevel{LevelPublic, LevelInternal}
	}

	for docID, doc := range m.documents {
		// 检查隐私级别
		levelAllowed := false
		for _, level := range allowedLevels {
			if doc.PrivacyLevel == level {
				levelAllowed = true
				break
			}
		}

		if !levelAllowed {
			results = append(results, EncryptedSearchResult{
				DocumentID:    docID,
				Score:         0,
				PrivacyLevel:  doc.PrivacyLevel,
				AccessGranted: false,
			})
			continue
		}

		// 解密并搜索
		content := doc.Content
		if doc.EncryptedContent != nil && content == "" {
			decrypted, _ := m.Decrypt(ctx, doc.EncryptedContent)
			if decrypted != nil && decrypted.Verified {
				content = decrypted.Plaintext
			}
		}

		// 简单的关键词匹配评分
		score := calculateRelevanceScore(request.Query, content)

		if score > 0 {
			results = append(results, EncryptedSearchResult{
				DocumentID:       docID,
				Score:            score,
				DecryptedContent: content,
				PrivacyLevel:     doc.PrivacyLevel,
				AccessGranted:    true,
			})
		}
	}

	// 按分数排序
	sortByRelevance(results)

	// 限制结果数量
	if request.Limit > 0 && len(results) > request.Limit {
		results = results[:request.Limit]
	}

	return results, nil
}

// GetMetrics 获取性能指标
func (m *PrivacyManager) GetMetrics() EncryptionMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.metrics
}

// ResetMetrics 重置性能指标
func (m *PrivacyManager) ResetMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics = EncryptionMetrics{}
}

// deriveKey 派生密钥
func (m *PrivacyManager) deriveKey(password string, salt []byte) ([]byte, error) {
	return m.deriveKeyForAlgorithm(password, salt, AES256GCM)
}

func (m *PrivacyManager) deriveKeyForAlgorithm(password string, salt []byte, algorithm EncryptionAlgorithm) ([]byte, error) {
	keyLength := 32 // AES-256
	if algorithm == AES128CBC {
		keyLength = 16
	}

	if m.config.KeyDerivation == SCRYPT {
		// scrypt: N=16384, r=8, p=1
		return scrypt.Key([]byte(password), salt, 16384, 8, 1, keyLength)
	}

	// PBKDF2 (default)
	return pbkdf2.Key([]byte(password), salt, m.config.Iterations, keyLength, sha256.New), nil
}

// updateMetrics 更新性能指标
func (m *PrivacyManager) updateMetrics(elapsed time.Duration, dataSize int, operation string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.metrics.OperationCount++
	elapsedMs := float64(elapsed.Microseconds()) / 1000.0

	if operation == "encrypt" {
		m.metrics.EncryptionTimeMs += elapsedMs
	} else {
		m.metrics.DecryptionTimeMs += elapsedMs
	}

	if elapsedMs > 0 {
		bytesPerSec := float64(dataSize) / (elapsedMs / 1000.0)
		// 移动平均
		m.metrics.ThroughputBytesPerSec =
			(m.metrics.ThroughputBytesPerSec*float64(m.metrics.OperationCount-1) + bytesPerSec) /
				float64(m.metrics.OperationCount)
	}
}

// pkcs7Pad PKCS7 填充
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := make([]byte, padding)
	for i := range padtext {
		padtext[i] = byte(padding)
	}
	return append(data, padtext...)
}

// pkcs7Unpad PKCS7 去填充
func pkcs7Unpad(data []byte) ([]byte, error) {
	length := len(data)
	if length == 0 {
		return nil, errors.New("empty data")
	}

	padding := int(data[length-1])
	if padding > length || padding == 0 {
		return nil, errors.New("invalid padding")
	}

	// 验证填充
	for i := length - padding; i < length; i++ {
		if data[i] != byte(padding) {
			return nil, errors.New("invalid padding")
		}
	}

	return data[:length-padding], nil
}

// randBytes 生成随机字节
func randBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

// calculateRelevanceScore 计算相关性分数
func calculateRelevanceScore(query, content string) float64 {
	if content == "" {
		return 0
	}

	queryTerms := strings.Fields(strings.ToLower(query))
	contentLower := strings.ToLower(content)

	matches := 0
	for _, term := range queryTerms {
		if strings.Contains(contentLower, term) {
			matches++
		}
	}

	if len(queryTerms) == 0 {
		return 0
	}
	return float64(matches) / float64(len(queryTerms))
}

// sortByRelevance 按相关性排序
func sortByRelevance(results []EncryptedSearchResult) {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}
