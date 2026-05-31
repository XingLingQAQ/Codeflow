package privacy

// EncryptionAlgorithm 加密算法类型
type EncryptionAlgorithm string

const (
	AES256CBC EncryptionAlgorithm = "aes-256-cbc"
	AES128CBC EncryptionAlgorithm = "aes-128-cbc"
)

// KeyDerivation 密钥派生方法
type KeyDerivation string

const (
	PBKDF2 KeyDerivation = "pbkdf2"
	SCRYPT KeyDerivation = "scrypt"
)

// PrivacyLevel 隐私级别
type PrivacyLevel string

const (
	LevelPublic       PrivacyLevel = "public"
	LevelInternal     PrivacyLevel = "internal"
	LevelConfidential PrivacyLevel = "confidential"
	LevelSecret       PrivacyLevel = "secret"
)

// PrivacyLevelPriority 隐私级别优先级
var PrivacyLevelPriority = map[PrivacyLevel]int{
	LevelPublic:       0,
	LevelInternal:     1,
	LevelConfidential: 2,
	LevelSecret:       3,
}

// EncryptionConfig 加密配置
type EncryptionConfig struct {
	Algorithm     EncryptionAlgorithm
	KeyDerivation KeyDerivation
	Iterations    int
	SaltLength    int
	IVLength      int
}

// DefaultEncryptionConfig 默认加密配置
var DefaultEncryptionConfig = EncryptionConfig{
	Algorithm:     AES256CBC,
	KeyDerivation: PBKDF2,
	Iterations:    100000,
	SaltLength:    16,
	IVLength:      16,
}

// EncryptedData 加密结果
type EncryptedData struct {
	Ciphertext string              `json:"ciphertext"`
	IV         string              `json:"iv"`
	Salt       string              `json:"salt"`
	Algorithm  EncryptionAlgorithm `json:"algorithm"`
	Tag        string              `json:"tag,omitempty"`
}

// DecryptedData 解密结果
type DecryptedData struct {
	Plaintext string `json:"plaintext"`
	Verified  bool   `json:"verified"`
}

// KeyInfo 密钥信息
type KeyInfo struct {
	ID          string              `json:"id"`
	Algorithm   EncryptionAlgorithm `json:"algorithm"`
	CreatedAt   int64               `json:"created_at"`
	ExpiresAt   int64               `json:"expires_at,omitempty"`
	RotatedFrom string              `json:"rotated_from,omitempty"`
}

// PrivacyPolicy 隐私策略
type PrivacyPolicy struct {
	Level            PrivacyLevel `json:"level"`
	EncryptAtRest    bool         `json:"encrypt_at_rest"`
	EncryptInTransit bool         `json:"encrypt_in_transit"`
	AllowedRoles     []string     `json:"allowed_roles"`
	RetentionDays    int          `json:"retention_days,omitempty"`
	AutoRedact       bool         `json:"auto_redact"`
}

// DefaultPrivacyPolicy 默认隐私策略
var DefaultPrivacyPolicy = PrivacyPolicy{
	Level:            LevelInternal,
	EncryptAtRest:    true,
	EncryptInTransit: true,
	AllowedRoles:     []string{"admin", "user"},
	RetentionDays:    365,
	AutoRedact:       false,
}

// DocumentMetadata 文档元数据
type DocumentMetadata struct {
	CreatedAt      int64    `json:"created_at"`
	UpdatedAt      int64    `json:"updated_at"`
	CreatedBy      string   `json:"created_by"`
	Classification string   `json:"classification,omitempty"`
	Tags           []string `json:"tags,omitempty"`
}

// PrivacyAwareDocument 隐私感知文档
type PrivacyAwareDocument struct {
	ID               string           `json:"id"`
	Content          string           `json:"content"`
	EncryptedContent *EncryptedData   `json:"encrypted_content,omitempty"`
	PrivacyLevel     PrivacyLevel     `json:"privacy_level"`
	Policy           PrivacyPolicy    `json:"policy"`
	Metadata         DocumentMetadata `json:"metadata"`
}

// EncryptedSearchRequest 加密检索请求
type EncryptedSearchRequest struct {
	Query         string         `json:"query"`
	EncryptQuery  bool           `json:"encrypt_query"`
	PrivacyLevels []PrivacyLevel `json:"privacy_levels"`
	Limit         int            `json:"limit"`
}

// EncryptedSearchResult 加密检索结果
type EncryptedSearchResult struct {
	DocumentID       string       `json:"document_id"`
	Score            float64      `json:"score"`
	DecryptedContent string       `json:"decrypted_content,omitempty"`
	PrivacyLevel     PrivacyLevel `json:"privacy_level"`
	AccessGranted    bool         `json:"access_granted"`
}

// KeyRotationEvent 密钥轮换事件
type KeyRotationEvent struct {
	OldKeyID             string `json:"old_key_id"`
	NewKeyID             string `json:"new_key_id"`
	RotatedAt            int64  `json:"rotated_at"`
	DocumentsReEncrypted int    `json:"documents_re_encrypted"`
	Status               string `json:"status"` // pending, in_progress, completed, failed
}

// EncryptionMetrics 加密性能指标
type EncryptionMetrics struct {
	EncryptionTimeMs      float64 `json:"encryption_time_ms"`
	DecryptionTimeMs      float64 `json:"decryption_time_ms"`
	ThroughputBytesPerSec float64 `json:"throughput_bytes_per_sec"`
	OperationCount        int64   `json:"operation_count"`
}

// ChainEncryptedData 链式加密数据（在chain_key.go中定义，这里声明类型别名）
// 注意：实际类型定义在chain_key.go中

// PIIMatch PII匹配结果（在redaction.go中定义）
// 注意：实际类型定义在redaction.go中

// RedactionResult 脱敏结果（在redaction.go中定义）
// 注意：实际类型定义在redaction.go中

// ChainNode 链节点（在chain_key.go中定义）
// 注意：实际类型定义在chain_key.go中

// ChainInfo 链信息（在chain_key.go中定义）
// 注意：实际类型定义在chain_key.go中

// ChainVerificationResult 链验证结果（在chain_key.go中定义）
// 注意：实际类型定义在chain_key.go中
