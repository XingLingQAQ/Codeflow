/**
 * 加密性能基准测试套件
 * 测量 AES-256-CBC 加密/解密吞吐量、密钥派生延迟
 */

import * as crypto from 'crypto';
import { PrivacyManager } from './PrivacyManager.js';
import { EncryptionConfig } from './types.js';

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

const DEFAULT_BENCHMARK_CONFIG: BenchmarkConfig = {
  iterations: 100,
  warmupIterations: 10,
  testSizes: [1024, 10240, 102400, 1048576], // 1KB, 10KB, 100KB, 1MB
  pbkdf2Iterations: 100000,
};

/**
 * 加密性能基准测试类
 */
export class EncryptionBenchmark {
  private config: BenchmarkConfig;

  constructor(config?: Partial<BenchmarkConfig>) {
    this.config = { ...DEFAULT_BENCHMARK_CONFIG, ...config };
  }

  /**
   * 运行完整基准测试
   */
  async run(): Promise<BenchmarkReport> {
    const testSizes: BenchmarkResult[] = [];
    const keyDerivation: KeyDerivationResult[] = [];

    // 测试不同数据量的加密/解密性能
    for (const size of this.config.testSizes) {
      const result = await this.benchmarkEncryption(size);
      testSizes.push(result);
    }

    // 测试密钥派生性能
    const pbkdf2Result = await this.benchmarkKeyDerivation('pbkdf2');
    const scryptResult = await this.benchmarkKeyDerivation('scrypt');
    keyDerivation.push(pbkdf2Result, scryptResult);

    // 计算汇总
    const avgEncryptThroughput = testSizes.reduce((sum, r) => sum + r.encryptThroughputMBps, 0) / testSizes.length;
    const avgDecryptThroughput = testSizes.reduce((sum, r) => sum + r.decryptThroughputMBps, 0) / testSizes.length;
    const recommendedKdf = pbkdf2Result.avgTimeMs < scryptResult.avgTimeMs ? 'pbkdf2' : 'scrypt';

    return {
      timestamp: Date.now(),
      algorithm: 'aes-256-cbc',
      testSizes,
      keyDerivation,
      summary: {
        avgEncryptThroughputMBps: avgEncryptThroughput,
        avgDecryptThroughputMBps: avgDecryptThroughput,
        recommendedKeyDerivation: recommendedKdf,
      },
    };
  }

  /**
   * 测试指定数据量的加密/解密性能
   */
  async benchmarkEncryption(dataSize: number): Promise<BenchmarkResult> {
    const testData = this.generateTestData(dataSize);
    const manager = new PrivacyManager('benchmark-password');

    // 预热
    for (let i = 0; i < this.config.warmupIterations; i++) {
      const encrypted = await manager.encrypt(testData);
      await manager.decrypt(encrypted);
    }

    // 加密基准测试
    const encryptTimes: number[] = [];
    const encryptedResults: Awaited<ReturnType<typeof manager.encrypt>>[] = [];

    for (let i = 0; i < this.config.iterations; i++) {
      const start = performance.now();
      const encrypted = await manager.encrypt(testData);
      encryptTimes.push(performance.now() - start);
      encryptedResults.push(encrypted);
    }

    // 解密基准测试
    const decryptTimes: number[] = [];
    for (let i = 0; i < this.config.iterations; i++) {
      const start = performance.now();
      await manager.decrypt(encryptedResults[i]);
      decryptTimes.push(performance.now() - start);
    }

    const avgEncryptTime = encryptTimes.reduce((a, b) => a + b, 0) / encryptTimes.length;
    const avgDecryptTime = decryptTimes.reduce((a, b) => a + b, 0) / decryptTimes.length;

    const encryptOpsPerSec = 1000 / avgEncryptTime;
    const decryptOpsPerSec = 1000 / avgDecryptTime;

    const dataSizeMB = dataSize / (1024 * 1024);
    const encryptThroughputMBps = dataSizeMB * encryptOpsPerSec;
    const decryptThroughputMBps = dataSizeMB * decryptOpsPerSec;

    return {
      dataSize,
      dataSizeLabel: this.formatSize(dataSize),
      encryptOpsPerSec,
      decryptOpsPerSec,
      encryptThroughputMBps,
      decryptThroughputMBps,
      avgEncryptTimeMs: avgEncryptTime,
      avgDecryptTimeMs: avgDecryptTime,
    };
  }

  /**
   * 测试密钥派生性能
   */
  async benchmarkKeyDerivation(method: 'pbkdf2' | 'scrypt'): Promise<KeyDerivationResult> {
    const password = 'benchmark-password';
    const salt = crypto.randomBytes(16);
    const keyLength = 32;
    const times: number[] = [];

    // 预热
    for (let i = 0; i < this.config.warmupIterations; i++) {
      await this.deriveKey(method, password, salt, keyLength);
    }

    // 基准测试
    for (let i = 0; i < this.config.iterations; i++) {
      const start = performance.now();
      await this.deriveKey(method, password, salt, keyLength);
      times.push(performance.now() - start);
    }

    const avgTime = times.reduce((a, b) => a + b, 0) / times.length;

    return {
      method,
      iterations: method === 'pbkdf2' ? this.config.pbkdf2Iterations : 16384,
      avgTimeMs: avgTime,
      opsPerSec: 1000 / avgTime,
    };
  }

  /**
   * 生成测试数据
   */
  private generateTestData(sizeBytes: number): string {
    return crypto.randomBytes(sizeBytes).toString('base64');
  }

  /**
   * 派生密钥
   */
  private deriveKey(
    method: 'pbkdf2' | 'scrypt',
    password: string,
    salt: Buffer,
    keyLength: number
  ): Promise<Buffer> {
    return new Promise((resolve, reject) => {
      if (method === 'scrypt') {
        crypto.scrypt(password, salt, keyLength, (err, key) => {
          if (err) reject(err);
          else resolve(key);
        });
      } else {
        crypto.pbkdf2(password, salt, this.config.pbkdf2Iterations, keyLength, 'sha256', (err, key) => {
          if (err) reject(err);
          else resolve(key);
        });
      }
    });
  }

  /**
   * 格式化数据大小
   */
  private formatSize(bytes: number): string {
    if (bytes >= 1048576) return `${(bytes / 1048576).toFixed(0)}MB`;
    if (bytes >= 1024) return `${(bytes / 1024).toFixed(0)}KB`;
    return `${bytes}B`;
  }

  /**
   * 输出报告到控制台
   */
  static printReport(report: BenchmarkReport): void {
    console.log('\n========== Encryption Benchmark Report ==========');
    console.log(`Timestamp: ${new Date(report.timestamp).toISOString()}`);
    console.log(`Algorithm: ${report.algorithm}`);

    console.log('\n--- Encryption/Decryption Performance ---');
    console.log('Size\t\tEnc ops/s\tDec ops/s\tEnc MB/s\tDec MB/s');
    for (const r of report.testSizes) {
      console.log(
        `${r.dataSizeLabel}\t\t${r.encryptOpsPerSec.toFixed(2)}\t\t${r.decryptOpsPerSec.toFixed(2)}\t\t${r.encryptThroughputMBps.toFixed(2)}\t\t${r.decryptThroughputMBps.toFixed(2)}`
      );
    }

    console.log('\n--- Key Derivation Performance ---');
    console.log('Method\t\tIterations\tAvg Time (ms)\tops/s');
    for (const k of report.keyDerivation) {
      console.log(`${k.method}\t\t${k.iterations}\t\t${k.avgTimeMs.toFixed(2)}\t\t${k.opsPerSec.toFixed(2)}`);
    }

    console.log('\n--- Summary ---');
    console.log(`Avg Encrypt Throughput: ${report.summary.avgEncryptThroughputMBps.toFixed(2)} MB/s`);
    console.log(`Avg Decrypt Throughput: ${report.summary.avgDecryptThroughputMBps.toFixed(2)} MB/s`);
    console.log(`Recommended Key Derivation: ${report.summary.recommendedKeyDerivation}`);
    console.log('=================================================\n');
  }
}

/**
 * 快速运行基准测试
 */
export async function runBenchmark(config?: Partial<BenchmarkConfig>): Promise<BenchmarkReport> {
  const benchmark = new EncryptionBenchmark(config);
  return benchmark.run();
}
