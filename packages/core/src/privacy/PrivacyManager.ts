/**
 * 隐私管理器实现
 * AES-CBC 加密 + 隐私感知 RAG
 */

import * as crypto from 'crypto';
import {
  IPrivacyManager,
  EncryptionConfig,
  EncryptedData,
  DecryptedData,
  KeyInfo,
  PrivacyPolicy,
  PrivacyAwareDocument,
  EncryptedSearchRequest,
  EncryptedSearchResult,
  KeyRotationEvent,
  EncryptionMetrics,
  DEFAULT_ENCRYPTION_CONFIG,
  DEFAULT_PRIVACY_POLICY,
  PRIVACY_LEVEL_PRIORITY,
  PrivacyLevel,
} from './types.js';

export class PrivacyManager implements IPrivacyManager {
  private config: EncryptionConfig;
  private masterPassword: string;
  private activeKey: KeyInfo | null = null;
  private keys: Map<string, Buffer> = new Map();
  private documents: Map<string, PrivacyAwareDocument> = new Map();
  private metrics: EncryptionMetrics = {
    encryptionTimeMs: 0,
    decryptionTimeMs: 0,
    throughputBytesPerSec: 0,
    operationCount: 0,
  };

  constructor(masterPassword: string, config?: Partial<EncryptionConfig>) {
    this.masterPassword = masterPassword;
    this.config = { ...DEFAULT_ENCRYPTION_CONFIG, ...config };
  }

  async encrypt(plaintext: string, policy?: PrivacyPolicy): Promise<EncryptedData> {
    const startTime = Date.now();
    const effectivePolicy = policy || DEFAULT_PRIVACY_POLICY;

    // 生成随机 salt 和 IV
    const salt = crypto.randomBytes(this.config.saltLength || 16);
    const iv = crypto.randomBytes(this.config.ivLength || 16);

    // 派生密钥
    const key = await this.deriveKey(this.masterPassword, salt);

    // AES-CBC 加密
    const cipher = crypto.createCipheriv(
      this.config.algorithm,
      key,
      iv
    );

    let ciphertext = cipher.update(plaintext, 'utf8', 'base64');
    ciphertext += cipher.final('base64');

    // 更新性能指标
    const elapsed = Date.now() - startTime;
    this.updateMetrics(elapsed, plaintext.length, 'encrypt');

    return {
      ciphertext,
      iv: iv.toString('base64'),
      salt: salt.toString('base64'),
      algorithm: this.config.algorithm,
    };
  }

  async decrypt(encrypted: EncryptedData): Promise<DecryptedData> {
    const startTime = Date.now();

    try {
      const salt = Buffer.from(encrypted.salt, 'base64');
      const iv = Buffer.from(encrypted.iv, 'base64');

      // 派生密钥
      const key = await this.deriveKey(this.masterPassword, salt);

      // AES-CBC 解密
      const decipher = crypto.createDecipheriv(
        encrypted.algorithm,
        key,
        iv
      );

      let plaintext = decipher.update(encrypted.ciphertext, 'base64', 'utf8');
      plaintext += decipher.final('utf8');

      // 更新性能指标
      const elapsed = Date.now() - startTime;
      this.updateMetrics(elapsed, plaintext.length, 'decrypt');

      return {
        plaintext,
        verified: true,
      };
    } catch (error) {
      return {
        plaintext: '',
        verified: false,
      };
    }
  }

  async generateKey(): Promise<KeyInfo> {
    const keyId = `key_${Date.now()}_${crypto.randomBytes(4).toString('hex')}`;
    const keyBuffer = crypto.randomBytes(32);

    this.keys.set(keyId, keyBuffer);

    const keyInfo: KeyInfo = {
      id: keyId,
      algorithm: this.config.algorithm,
      createdAt: Date.now(),
    };

    this.activeKey = keyInfo;
    return keyInfo;
  }

  async rotateKey(): Promise<KeyRotationEvent> {
    const oldKeyId = this.activeKey?.id || 'none';
    const newKey = await this.generateKey();

    const event: KeyRotationEvent = {
      oldKeyId,
      newKeyId: newKey.id,
      rotatedAt: Date.now(),
      documentsReEncrypted: 0,
      status: 'in_progress',
    };

    // 重新加密所有文档
    for (const [docId, doc] of this.documents) {
      if (doc.encryptedContent) {
        const decrypted = await this.decrypt(doc.encryptedContent);
        if (decrypted.verified) {
          doc.encryptedContent = await this.encrypt(decrypted.plaintext, doc.policy);
          event.documentsReEncrypted++;
        }
      }
    }

    event.status = 'completed';
    return event;
  }

  getActiveKey(): KeyInfo | null {
    return this.activeKey;
  }

  async encryptDocument(doc: PrivacyAwareDocument): Promise<PrivacyAwareDocument> {
    if (doc.policy.encryptAtRest && doc.content) {
      doc.encryptedContent = await this.encrypt(doc.content, doc.policy);
      // 清除明文内容
      doc.content = '';
    }

    this.documents.set(doc.id, doc);
    return doc;
  }

  async decryptDocument(doc: PrivacyAwareDocument): Promise<PrivacyAwareDocument> {
    if (doc.encryptedContent) {
      const decrypted = await this.decrypt(doc.encryptedContent);
      if (decrypted.verified) {
        doc.content = decrypted.plaintext;
      }
    }
    return doc;
  }

  async search(request: EncryptedSearchRequest): Promise<EncryptedSearchResult[]> {
    const results: EncryptedSearchResult[] = [];
    const allowedLevels = request.privacyLevels || ['public', 'internal'];

    for (const [docId, doc] of this.documents) {
      // 检查隐私级别
      const levelAllowed = allowedLevels.includes(doc.privacyLevel);

      if (!levelAllowed) {
        results.push({
          documentId: docId,
          score: 0,
          privacyLevel: doc.privacyLevel,
          accessGranted: false,
        });
        continue;
      }

      // 解密并搜索
      let content = doc.content;
      if (doc.encryptedContent && !content) {
        const decrypted = await this.decrypt(doc.encryptedContent);
        if (decrypted.verified) {
          content = decrypted.plaintext;
        }
      }

      // 简单的关键词匹配评分
      const score = this.calculateRelevanceScore(request.query, content);

      if (score > 0) {
        results.push({
          documentId: docId,
          score,
          decryptedContent: content,
          privacyLevel: doc.privacyLevel,
          accessGranted: true,
        });
      }
    }

    // 按分数排序
    results.sort((a, b) => b.score - a.score);

    // 限制结果数量
    if (request.limit) {
      return results.slice(0, request.limit);
    }

    return results;
  }

  getMetrics(): EncryptionMetrics {
    return { ...this.metrics };
  }

  resetMetrics(): void {
    this.metrics = {
      encryptionTimeMs: 0,
      decryptionTimeMs: 0,
      throughputBytesPerSec: 0,
      operationCount: 0,
    };
  }

  // ==================== Private Methods ====================

  private async deriveKey(password: string, salt: Buffer): Promise<Buffer> {
    const keyLength = this.config.algorithm === 'aes-256-cbc' ? 32 : 16;

    if (this.config.keyDerivation === 'scrypt') {
      return new Promise((resolve, reject) => {
        crypto.scrypt(password, salt, keyLength, (err, derivedKey) => {
          if (err) reject(err);
          else resolve(derivedKey);
        });
      });
    }

    // PBKDF2 (default)
    return new Promise((resolve, reject) => {
      crypto.pbkdf2(
        password,
        salt,
        this.config.iterations || 100000,
        keyLength,
        'sha256',
        (err, derivedKey) => {
          if (err) reject(err);
          else resolve(derivedKey);
        }
      );
    });
  }

  private updateMetrics(elapsedMs: number, dataSize: number, operation: 'encrypt' | 'decrypt'): void {
    this.metrics.operationCount++;

    if (operation === 'encrypt') {
      this.metrics.encryptionTimeMs += elapsedMs;
    } else {
      this.metrics.decryptionTimeMs += elapsedMs;
    }

    if (elapsedMs > 0) {
      const bytesPerSec = (dataSize / elapsedMs) * 1000;
      // 移动平均
      this.metrics.throughputBytesPerSec =
        (this.metrics.throughputBytesPerSec * (this.metrics.operationCount - 1) + bytesPerSec) /
        this.metrics.operationCount;
    }
  }

  private calculateRelevanceScore(query: string, content: string): number {
    if (!content) return 0;

    const queryTerms = query.toLowerCase().split(/\s+/);
    const contentLower = content.toLowerCase();

    let matches = 0;
    for (const term of queryTerms) {
      if (contentLower.includes(term)) {
        matches++;
      }
    }

    return matches / queryTerms.length;
  }
}

/**
 * 隐私感知向量存储
 * 支持加密向量的存储和检索
 */
export class PrivacyAwareVectorStore {
  private privacyManager: PrivacyManager;
  private vectors: Map<string, { vector: number[]; encrypted: EncryptedData }> = new Map();

  constructor(privacyManager: PrivacyManager) {
    this.privacyManager = privacyManager;
  }

  async storeVector(id: string, vector: number[], privacyLevel: PrivacyLevel): Promise<void> {
    const vectorStr = JSON.stringify(vector);
    const encrypted = await this.privacyManager.encrypt(vectorStr, {
      level: privacyLevel,
      encryptAtRest: true,
      encryptInTransit: true,
      allowedRoles: ['admin'],
    });

    this.vectors.set(id, { vector, encrypted });
  }

  async retrieveVector(id: string): Promise<number[] | null> {
    const entry = this.vectors.get(id);
    if (!entry) return null;

    // 如果有明文向量，直接返回
    if (entry.vector) return entry.vector;

    // 否则解密
    const decrypted = await this.privacyManager.decrypt(entry.encrypted);
    if (decrypted.verified) {
      return JSON.parse(decrypted.plaintext);
    }

    return null;
  }

  async searchSimilar(queryVector: number[], topK: number = 10): Promise<Array<{ id: string; score: number }>> {
    const results: Array<{ id: string; score: number }> = [];

    for (const [id, entry] of this.vectors) {
      const score = this.cosineSimilarity(queryVector, entry.vector);
      results.push({ id, score });
    }

    results.sort((a, b) => b.score - a.score);
    return results.slice(0, topK);
  }

  private cosineSimilarity(a: number[], b: number[]): number {
    if (a.length !== b.length) return 0;

    let dotProduct = 0;
    let normA = 0;
    let normB = 0;

    for (let i = 0; i < a.length; i++) {
      dotProduct += a[i] * b[i];
      normA += a[i] * a[i];
      normB += b[i] * b[i];
    }

    const denominator = Math.sqrt(normA) * Math.sqrt(normB);
    return denominator === 0 ? 0 : dotProduct / denominator;
  }
}
