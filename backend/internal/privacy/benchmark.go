package privacy

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"time"

	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/scrypt"
)

// BenchmarkResult 单次基准测试结果
type BenchmarkResult struct {
	DataSize               int     `json:"data_size"`
	DataSizeLabel          string  `json:"data_size_label"`
	EncryptOpsPerSec       float64 `json:"encrypt_ops_per_sec"`
	DecryptOpsPerSec       float64 `json:"decrypt_ops_per_sec"`
	EncryptThroughputMBps  float64 `json:"encrypt_throughput_mbps"`
	DecryptThroughputMBps  float64 `json:"decrypt_throughput_mbps"`
	AvgEncryptTimeMs       float64 `json:"avg_encrypt_time_ms"`
	AvgDecryptTimeMs       float64 `json:"avg_decrypt_time_ms"`
}

// KeyDerivationResult 密钥派生基准测试结果
type KeyDerivationResult struct {
	Method     KeyDerivation `json:"method"`
	Iterations int           `json:"iterations"`
	AvgTimeMs  float64       `json:"avg_time_ms"`
	OpsPerSec  float64       `json:"ops_per_sec"`
}

// BenchmarkReport 完整基准测试报告
type BenchmarkReport struct {
	Timestamp     int64                 `json:"timestamp"`
	Algorithm     string                `json:"algorithm"`
	TestSizes     []BenchmarkResult     `json:"test_sizes"`
	KeyDerivation []KeyDerivationResult `json:"key_derivation"`
	Summary       BenchmarkSummary      `json:"summary"`
}

// BenchmarkSummary 基准测试汇总
type BenchmarkSummary struct {
	AvgEncryptThroughputMBps   float64       `json:"avg_encrypt_throughput_mbps"`
	AvgDecryptThroughputMBps   float64       `json:"avg_decrypt_throughput_mbps"`
	RecommendedKeyDerivation   KeyDerivation `json:"recommended_key_derivation"`
}

// BenchmarkConfig 基准测试配置
type BenchmarkConfig struct {
	Iterations       int   `json:"iterations"`
	WarmupIterations int   `json:"warmup_iterations"`
	TestSizes        []int `json:"test_sizes"`
	PBKDF2Iterations int   `json:"pbkdf2_iterations"`
}

// DefaultBenchmarkConfig 默认基准测试配置
var DefaultBenchmarkConfig = BenchmarkConfig{
	Iterations:       100,
	WarmupIterations: 10,
	TestSizes:        []int{1024, 10240, 102400, 1048576}, // 1KB, 10KB, 100KB, 1MB
	PBKDF2Iterations: 100000,
}

// EncryptionBenchmark 加密性能基准测试类
type EncryptionBenchmark struct {
	config BenchmarkConfig
}

// NewEncryptionBenchmark 创建基准测试
func NewEncryptionBenchmark(config *BenchmarkConfig) *EncryptionBenchmark {
	cfg := DefaultBenchmarkConfig
	if config != nil {
		if config.Iterations > 0 {
			cfg.Iterations = config.Iterations
		}
		if config.WarmupIterations > 0 {
			cfg.WarmupIterations = config.WarmupIterations
		}
		if len(config.TestSizes) > 0 {
			cfg.TestSizes = config.TestSizes
		}
		if config.PBKDF2Iterations > 0 {
			cfg.PBKDF2Iterations = config.PBKDF2Iterations
		}
	}
	return &EncryptionBenchmark{config: cfg}
}

// Run 运行完整基准测试
func (b *EncryptionBenchmark) Run() (*BenchmarkReport, error) {
	var testSizes []BenchmarkResult
	var keyDerivation []KeyDerivationResult

	// 测试不同数据量的加密/解密性能
	for _, size := range b.config.TestSizes {
		result, err := b.benchmarkEncryption(size)
		if err != nil {
			return nil, err
		}
		testSizes = append(testSizes, *result)
	}

	// 测试密钥派生性能
	pbkdf2Result, err := b.benchmarkKeyDerivation(PBKDF2)
	if err != nil {
		return nil, err
	}
	keyDerivation = append(keyDerivation, *pbkdf2Result)

	scryptResult, err := b.benchmarkKeyDerivation(SCRYPT)
	if err != nil {
		return nil, err
	}
	keyDerivation = append(keyDerivation, *scryptResult)

	// 计算汇总
	var avgEncrypt, avgDecrypt float64
	for _, r := range testSizes {
		avgEncrypt += r.EncryptThroughputMBps
		avgDecrypt += r.DecryptThroughputMBps
	}
	avgEncrypt /= float64(len(testSizes))
	avgDecrypt /= float64(len(testSizes))

	recommendedKdf := PBKDF2
	if pbkdf2Result.AvgTimeMs > scryptResult.AvgTimeMs {
		recommendedKdf = SCRYPT
	}

	return &BenchmarkReport{
		Timestamp:     time.Now().UnixMilli(),
		Algorithm:     "aes-256-cbc",
		TestSizes:     testSizes,
		KeyDerivation: keyDerivation,
		Summary: BenchmarkSummary{
			AvgEncryptThroughputMBps: avgEncrypt,
			AvgDecryptThroughputMBps: avgDecrypt,
			RecommendedKeyDerivation: recommendedKdf,
		},
	}, nil
}

// benchmarkEncryption 测试指定数据量的加密/解密性能
func (b *EncryptionBenchmark) benchmarkEncryption(dataSize int) (*BenchmarkResult, error) {
	testData := b.generateTestData(dataSize)
	manager := NewPrivacyManager("benchmark-password", nil)

	// 预热
	for i := 0; i < b.config.WarmupIterations; i++ {
		encrypted, _ := manager.Encrypt(nil, testData, nil)
		manager.Decrypt(nil, encrypted)
	}

	// 加密基准测试
	var encryptTimes []float64
	var encryptedResults []*EncryptedData

	for i := 0; i < b.config.Iterations; i++ {
		start := time.Now()
		encrypted, _ := manager.Encrypt(nil, testData, nil)
		encryptTimes = append(encryptTimes, float64(time.Since(start).Microseconds())/1000.0)
		encryptedResults = append(encryptedResults, encrypted)
	}

	// 解密基准测试
	var decryptTimes []float64
	for i := 0; i < b.config.Iterations; i++ {
		start := time.Now()
		manager.Decrypt(nil, encryptedResults[i])
		decryptTimes = append(decryptTimes, float64(time.Since(start).Microseconds())/1000.0)
	}

	avgEncryptTime := average(encryptTimes)
	avgDecryptTime := average(decryptTimes)

	encryptOpsPerSec := 1000.0 / avgEncryptTime
	decryptOpsPerSec := 1000.0 / avgDecryptTime

	dataSizeMB := float64(dataSize) / (1024 * 1024)
	encryptThroughputMBps := dataSizeMB * encryptOpsPerSec
	decryptThroughputMBps := dataSizeMB * decryptOpsPerSec

	return &BenchmarkResult{
		DataSize:              dataSize,
		DataSizeLabel:         formatSize(dataSize),
		EncryptOpsPerSec:      encryptOpsPerSec,
		DecryptOpsPerSec:      decryptOpsPerSec,
		EncryptThroughputMBps: encryptThroughputMBps,
		DecryptThroughputMBps: decryptThroughputMBps,
		AvgEncryptTimeMs:      avgEncryptTime,
		AvgDecryptTimeMs:      avgDecryptTime,
	}, nil
}

// benchmarkKeyDerivation 测试密钥派生性能
func (b *EncryptionBenchmark) benchmarkKeyDerivation(method KeyDerivation) (*KeyDerivationResult, error) {
	password := "benchmark-password"
	salt := make([]byte, 16)
	rand.Read(salt)
	keyLength := 32
	var times []float64

	// 预热
	for i := 0; i < b.config.WarmupIterations; i++ {
		b.deriveKey(method, password, salt, keyLength)
	}

	// 基准测试
	for i := 0; i < b.config.Iterations; i++ {
		start := time.Now()
		b.deriveKey(method, password, salt, keyLength)
		times = append(times, float64(time.Since(start).Microseconds())/1000.0)
	}

	avgTime := average(times)
	iterations := b.config.PBKDF2Iterations
	if method == SCRYPT {
		iterations = 16384
	}

	return &KeyDerivationResult{
		Method:     method,
		Iterations: iterations,
		AvgTimeMs:  avgTime,
		OpsPerSec:  1000.0 / avgTime,
	}, nil
}

// generateTestData 生成测试数据
func (b *EncryptionBenchmark) generateTestData(sizeBytes int) string {
	data := make([]byte, sizeBytes)
	rand.Read(data)
	return string(data)
}

// deriveKey 派生密钥
func (b *EncryptionBenchmark) deriveKey(method KeyDerivation, password string, salt []byte, keyLength int) ([]byte, error) {
	if method == SCRYPT {
		return scrypt.Key([]byte(password), salt, 16384, 8, 1, keyLength)
	}
	return pbkdf2.Key([]byte(password), salt, b.config.PBKDF2Iterations, keyLength, sha256.New), nil
}

// average 计算平均值
func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// formatSize 格式化数据大小
func formatSize(bytes int) string {
	if bytes >= 1048576 {
		return fmt.Sprintf("%dMB", bytes/1048576)
	}
	if bytes >= 1024 {
		return fmt.Sprintf("%dKB", bytes/1024)
	}
	return fmt.Sprintf("%dB", bytes)
}

// PrintReport 输出报告到控制台
func PrintReport(report *BenchmarkReport) {
	fmt.Println("\n========== Encryption Benchmark Report ==========")
	fmt.Printf("Timestamp: %s\n", time.UnixMilli(report.Timestamp).Format(time.RFC3339))
	fmt.Printf("Algorithm: %s\n", report.Algorithm)

	fmt.Println("\n--- Encryption/Decryption Performance ---")
	fmt.Println("Size\t\tEnc ops/s\tDec ops/s\tEnc MB/s\tDec MB/s")
	for _, r := range report.TestSizes {
		fmt.Printf("%s\t\t%.2f\t\t%.2f\t\t%.2f\t\t%.2f\n",
			r.DataSizeLabel, r.EncryptOpsPerSec, r.DecryptOpsPerSec,
			r.EncryptThroughputMBps, r.DecryptThroughputMBps)
	}

	fmt.Println("\n--- Key Derivation Performance ---")
	fmt.Println("Method\t\tIterations\tAvg Time (ms)\tops/s")
	for _, k := range report.KeyDerivation {
		fmt.Printf("%s\t\t%d\t\t%.2f\t\t%.2f\n",
			k.Method, k.Iterations, k.AvgTimeMs, k.OpsPerSec)
	}

	fmt.Println("\n--- Summary ---")
	fmt.Printf("Avg Encrypt Throughput: %.2f MB/s\n", report.Summary.AvgEncryptThroughputMBps)
	fmt.Printf("Avg Decrypt Throughput: %.2f MB/s\n", report.Summary.AvgDecryptThroughputMBps)
	fmt.Printf("Recommended Key Derivation: %s\n", report.Summary.RecommendedKeyDerivation)
	fmt.Println("=================================================")
}

// RunBenchmark 快速运行基准测试
func RunBenchmark(config *BenchmarkConfig) (*BenchmarkReport, error) {
	benchmark := NewEncryptionBenchmark(config)
	return benchmark.Run()
}
