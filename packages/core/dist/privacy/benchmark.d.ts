/**
 * 加密性能基准测试套件
 * 测量 AES-256-CBC 加密/解密吞吐量、密钥派生延迟
 */
/**
 * 单次基准测试结果
 */
export interface BenchmarkResult {
    dataSize: number;
    dataSizeLabel: string;
    encryptOpsPerSec: number;
    decryptOpsPerSec: number;
    encryptThroughputMBps: number;
    decryptThroughputMBps: number;
    avgEncryptTimeMs: number;
    avgDecryptTimeMs: number;
}
/**
 * 密钥派生基准测试结果
 */
export interface KeyDerivationResult {
    method: 'pbkdf2' | 'scrypt';
    iterations: number;
    avgTimeMs: number;
    opsPerSec: number;
}
/**
 * 完整基准测试报告
 */
export interface BenchmarkReport {
    timestamp: number;
    algorithm: string;
    testSizes: BenchmarkResult[];
    keyDerivation: KeyDerivationResult[];
    summary: {
        avgEncryptThroughputMBps: number;
        avgDecryptThroughputMBps: number;
        recommendedKeyDerivation: 'pbkdf2' | 'scrypt';
    };
}
/**
 * 基准测试配置
 */
export interface BenchmarkConfig {
    iterations: number;
    warmupIterations: number;
    testSizes: number[];
    pbkdf2Iterations: number;
}
/**
 * 加密性能基准测试类
 */
export declare class EncryptionBenchmark {
    private config;
    constructor(config?: Partial<BenchmarkConfig>);
    /**
     * 运行完整基准测试
     */
    run(): Promise<BenchmarkReport>;
    /**
     * 测试指定数据量的加密/解密性能
     */
    benchmarkEncryption(dataSize: number): Promise<BenchmarkResult>;
    /**
     * 测试密钥派生性能
     */
    benchmarkKeyDerivation(method: 'pbkdf2' | 'scrypt'): Promise<KeyDerivationResult>;
    /**
     * 生成测试数据
     */
    private generateTestData;
    /**
     * 派生密钥
     */
    private deriveKey;
    /**
     * 格式化数据大小
     */
    private formatSize;
    /**
     * 输出报告到控制台
     */
    static printReport(report: BenchmarkReport): void;
}
/**
 * 快速运行基准测试
 */
export declare function runBenchmark(config?: Partial<BenchmarkConfig>): Promise<BenchmarkReport>;
//# sourceMappingURL=benchmark.d.ts.map