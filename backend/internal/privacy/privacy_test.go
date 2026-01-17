package privacy

import (
	"context"
	"testing"
)

func TestPrivacyManagerEncryptDecrypt(t *testing.T) {
	manager := NewPrivacyManager("test-password", nil)
	ctx := context.Background()

	// 测试加密
	plaintext := "Hello, World! This is a secret message."
	encrypted, err := manager.Encrypt(ctx, plaintext, nil)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if encrypted.Ciphertext == "" {
		t.Error("expected non-empty ciphertext")
	}
	if encrypted.IV == "" {
		t.Error("expected non-empty IV")
	}
	if encrypted.Salt == "" {
		t.Error("expected non-empty salt")
	}
	if encrypted.Algorithm != AES256CBC {
		t.Errorf("expected algorithm %s, got %s", AES256CBC, encrypted.Algorithm)
	}

	// 测试解密
	decrypted, err := manager.Decrypt(ctx, encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !decrypted.Verified {
		t.Error("expected verification to pass")
	}
	if decrypted.Plaintext != plaintext {
		t.Errorf("expected plaintext %q, got %q", plaintext, decrypted.Plaintext)
	}
}

func TestPrivacyManagerWrongPassword(t *testing.T) {
	manager1 := NewPrivacyManager("password1", nil)
	manager2 := NewPrivacyManager("password2", nil)
	ctx := context.Background()

	// 使用 manager1 加密
	plaintext := "Secret data"
	encrypted, err := manager1.Encrypt(ctx, plaintext, nil)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// 使用 manager2 解密（错误密码）
	decrypted, err := manager2.Decrypt(ctx, encrypted)
	if err != nil {
		t.Fatalf("Decrypt should not return error, got: %v", err)
	}

	// 验证应该失败或解密出错误的内容
	if decrypted.Verified && decrypted.Plaintext == plaintext {
		t.Error("decryption with wrong password should fail or produce wrong result")
	}
}

func TestPrivacyManagerScrypt(t *testing.T) {
	manager := NewPrivacyManager("test-password", &EncryptionConfig{
		KeyDerivation: SCRYPT,
	})
	ctx := context.Background()

	plaintext := "Test with scrypt key derivation"
	encrypted, err := manager.Encrypt(ctx, plaintext, nil)
	if err != nil {
		t.Fatalf("Encrypt with scrypt failed: %v", err)
	}

	decrypted, err := manager.Decrypt(ctx, encrypted)
	if err != nil {
		t.Fatalf("Decrypt with scrypt failed: %v", err)
	}

	if !decrypted.Verified || decrypted.Plaintext != plaintext {
		t.Error("scrypt encryption/decryption failed")
	}
}

func TestPrivacyManagerKeyGeneration(t *testing.T) {
	manager := NewPrivacyManager("test-password", nil)
	ctx := context.Background()

	// 初始状态没有活动密钥
	if manager.GetActiveKey() != nil {
		t.Error("expected no active key initially")
	}

	// 生成密钥
	keyInfo, err := manager.GenerateKey(ctx)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if keyInfo.ID == "" {
		t.Error("expected non-empty key ID")
	}
	if keyInfo.Algorithm != AES256CBC {
		t.Errorf("expected algorithm %s, got %s", AES256CBC, keyInfo.Algorithm)
	}
	if keyInfo.CreatedAt == 0 {
		t.Error("expected non-zero creation time")
	}

	// 验证活动密钥
	activeKey := manager.GetActiveKey()
	if activeKey == nil || activeKey.ID != keyInfo.ID {
		t.Error("active key mismatch")
	}
}

func TestPrivacyManagerKeyRotation(t *testing.T) {
	manager := NewPrivacyManager("test-password", nil)
	ctx := context.Background()

	// 先生成一个密钥
	oldKey, _ := manager.GenerateKey(ctx)

	// 添加一个文档
	doc := &PrivacyAwareDocument{
		ID:           "doc1",
		Content:      "Test document content",
		PrivacyLevel: LevelConfidential,
		Policy:       DefaultPrivacyPolicy,
	}
	manager.EncryptDocument(ctx, doc)

	// 轮换密钥
	event, err := manager.RotateKey(ctx)
	if err != nil {
		t.Fatalf("RotateKey failed: %v", err)
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

func TestPrivacyManagerDocument(t *testing.T) {
	manager := NewPrivacyManager("test-password", nil)
	ctx := context.Background()

	// 创建文档
	doc := &PrivacyAwareDocument{
		ID:           "doc-001",
		Content:      "This is confidential information",
		PrivacyLevel: LevelConfidential,
		Policy: PrivacyPolicy{
			Level:         LevelConfidential,
			EncryptAtRest: true,
			AllowedRoles:  []string{"admin"},
		},
	}

	// 加密文档
	encDoc, err := manager.EncryptDocument(ctx, doc)
	if err != nil {
		t.Fatalf("EncryptDocument failed: %v", err)
	}

	if encDoc.Content != "" {
		t.Error("plaintext content should be cleared after encryption")
	}
	if encDoc.EncryptedContent == nil {
		t.Error("expected encrypted content")
	}

	// 解密文档
	decDoc, err := manager.DecryptDocument(ctx, encDoc)
	if err != nil {
		t.Fatalf("DecryptDocument failed: %v", err)
	}

	if decDoc.Content != "This is confidential information" {
		t.Errorf("unexpected decrypted content: %s", decDoc.Content)
	}
}

func TestPrivacyManagerSearch(t *testing.T) {
	manager := NewPrivacyManager("test-password", nil)
	ctx := context.Background()

	// 添加测试文档
	docs := []*PrivacyAwareDocument{
		{
			ID:           "doc1",
			Content:      "The quick brown fox jumps over the lazy dog",
			PrivacyLevel: LevelPublic,
			Policy:       PrivacyPolicy{EncryptAtRest: false},
		},
		{
			ID:           "doc2",
			Content:      "Hello world programming example",
			PrivacyLevel: LevelInternal,
			Policy:       PrivacyPolicy{EncryptAtRest: false},
		},
		{
			ID:           "doc3",
			Content:      "Secret confidential data",
			PrivacyLevel: LevelSecret,
			Policy:       PrivacyPolicy{EncryptAtRest: false},
		},
	}

	for _, doc := range docs {
		manager.EncryptDocument(ctx, doc)
	}

	// 搜索 - 只允许 public 和 internal
	results, err := manager.Search(ctx, &EncryptedSearchRequest{
		Query:         "quick fox",
		PrivacyLevels: []PrivacyLevel{LevelPublic, LevelInternal},
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// 应该找到 doc1（包含 quick 和 fox）
	foundDoc1 := false
	for _, r := range results {
		if r.DocumentID == "doc1" && r.AccessGranted && r.Score > 0 {
			foundDoc1 = true
		}
		// doc3 应该被拒绝访问
		if r.DocumentID == "doc3" && r.AccessGranted {
			t.Error("doc3 should not have access granted")
		}
	}

	if !foundDoc1 {
		t.Error("expected to find doc1 in search results")
	}
}

func TestPrivacyManagerMetrics(t *testing.T) {
	manager := NewPrivacyManager("test-password", nil)
	ctx := context.Background()

	// 初始指标应该为零
	metrics := manager.GetMetrics()
	if metrics.OperationCount != 0 {
		t.Error("expected zero operation count initially")
	}

	// 执行一些操作
	for i := 0; i < 5; i++ {
		encrypted, _ := manager.Encrypt(ctx, "test data", nil)
		manager.Decrypt(ctx, encrypted)
	}

	// 检查指标
	metrics = manager.GetMetrics()
	if metrics.OperationCount != 10 { // 5 encrypts + 5 decrypts
		t.Errorf("expected 10 operations, got %d", metrics.OperationCount)
	}
	if metrics.EncryptionTimeMs <= 0 {
		t.Error("expected positive encryption time")
	}
	if metrics.DecryptionTimeMs <= 0 {
		t.Error("expected positive decryption time")
	}

	// 重置指标
	manager.ResetMetrics()
	metrics = manager.GetMetrics()
	if metrics.OperationCount != 0 {
		t.Error("expected zero operation count after reset")
	}
}

func TestPKCS7Padding(t *testing.T) {
	// 测试填充
	data := []byte("hello")
	padded := pkcs7Pad(data, 16)
	if len(padded)%16 != 0 {
		t.Error("padded data should be multiple of block size")
	}

	// 测试去填充
	unpadded, err := pkcs7Unpad(padded)
	if err != nil {
		t.Fatalf("pkcs7Unpad failed: %v", err)
	}
	if string(unpadded) != "hello" {
		t.Errorf("expected 'hello', got %s", string(unpadded))
	}

	// 测试无效填充
	_, err = pkcs7Unpad([]byte{})
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestCalculateRelevanceScore(t *testing.T) {
	// 完全匹配
	score := calculateRelevanceScore("hello world", "hello world program")
	if score != 1.0 {
		t.Errorf("expected score 1.0, got %f", score)
	}

	// 部分匹配
	score = calculateRelevanceScore("hello world", "hello there")
	if score != 0.5 {
		t.Errorf("expected score 0.5, got %f", score)
	}

	// 无匹配
	score = calculateRelevanceScore("foo bar", "hello world")
	if score != 0.0 {
		t.Errorf("expected score 0.0, got %f", score)
	}

	// 空内容
	score = calculateRelevanceScore("hello", "")
	if score != 0.0 {
		t.Errorf("expected score 0.0 for empty content, got %f", score)
	}
}
