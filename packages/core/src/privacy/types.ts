/**
 * 隐私感知 RAG 类型定义
 * AES-CBC 加密 + 隐私保护检索
 */

/**
 * 加密算法类型
 */
export type EncryptionAlgorithm = 'aes-256-cbc' | 'aes-128-cbc';

/**
 * 加密配置
 */
export interface EncryptionConfig {
  algorithm: EncryptionAlgorithm;
  keyDerivation: 'pbkdf2' | 'scrypt';
  iterations?: number;
  saltLength?: number;
  ivLength?: number;
}

/**
 * 加密结果
 */
export interface EncryptedData {
  ciphertext: string;
  iv: string;
  salt: string;
  algorithm: EncryptionAlgorithm;
  tag?: string;
}

/**
 * 解密结果
 */
export interface DecryptedData {
  plaintext: string;
  verified: boolean;
}

/**
 * 密钥信息
 */
export interface KeyInfo {
  id: string;
  algorithm: EncryptionAlgorithm;
  createdAt: number;
  expiresAt?: number;
  rotatedFrom?: string;
}

/**
 * 隐私级别
 */
export type PrivacyLevel = 'public' | 'internal' | 'confidential' | 'secret';

/**
 * 隐私策略
 */
export interface PrivacyPolicy {
  level: PrivacyLevel;
  encryptAtRest: boolean;
  encryptInTransit: boolean;
  allowedRoles: string[];
  retentionDays?: number;
  autoRedact?: boolean;
}

/**
 * 隐私感知文档
 */
export interface PrivacyAwareDocument {
  id: string;
  content: string;
  encryptedContent?: EncryptedData;
  privacyLevel: PrivacyLevel;
  policy: PrivacyPolicy;
  metadata: DocumentMetadata;
}

/**
 * 文档元数据
 */
export interface DocumentMetadata {
  createdAt: number;
  updatedAt: number;
  createdBy: string;
  classification?: string;
  tags?: string[];
}

/**
 * 加密检索请求
 */
export interface EncryptedSearchRequest {
  query: string;
  encryptQuery?: boolean;
  privacyLevels?: PrivacyLevel[];
  limit?: number;
}

/**
 * 加密检索结果
 */
export interface EncryptedSearchResult {
  documentId: string;
  score: number;
  decryptedContent?: string;
  privacyLevel: PrivacyLevel;
  accessGranted: boolean;
}

/**
 * 密钥轮换事件
 */
export interface KeyRotationEvent {
  oldKeyId: string;
  newKeyId: string;
  rotatedAt: number;
  documentsReEncrypted: number;
  status: 'pending' | 'in_progress' | 'completed' | 'failed';
}

/**
 * 加密性能指标
 */
export interface EncryptionMetrics {
  encryptionTimeMs: number;
  decryptionTimeMs: number;
  throughputBytesPerSec: number;
  operationCount: number;
}

/**
 * 隐私管理器接口
 */
export interface IPrivacyManager {
  // 加密操作
  encrypt(plaintext: string, policy?: PrivacyPolicy): Promise<EncryptedData>;
  decrypt(encrypted: EncryptedData): Promise<DecryptedData>;

  // 密钥管理
  generateKey(): Promise<KeyInfo>;
  rotateKey(): Promise<KeyRotationEvent>;
  getActiveKey(): KeyInfo | null;

  // 文档操作
  encryptDocument(doc: PrivacyAwareDocument): Promise<PrivacyAwareDocument>;
  decryptDocument(doc: PrivacyAwareDocument): Promise<PrivacyAwareDocument>;

  // 隐私检索
  search(request: EncryptedSearchRequest): Promise<EncryptedSearchResult[]>;

  // 性能监控
  getMetrics(): EncryptionMetrics;
  resetMetrics(): void;
}

/**
 * 默认加密配置
 */
export const DEFAULT_ENCRYPTION_CONFIG: EncryptionConfig = {
  algorithm: 'aes-256-cbc',
  keyDerivation: 'pbkdf2',
  iterations: 100000,
  saltLength: 16,
  ivLength: 16,
};

/**
 * 默认隐私策略
 */
export const DEFAULT_PRIVACY_POLICY: PrivacyPolicy = {
  level: 'internal',
  encryptAtRest: true,
  encryptInTransit: true,
  allowedRoles: ['admin', 'user'],
  retentionDays: 365,
  autoRedact: false,
};

/**
 * 隐私级别优先级
 */
export const PRIVACY_LEVEL_PRIORITY: Record<PrivacyLevel, number> = {
  public: 0,
  internal: 1,
  confidential: 2,
  secret: 3,
};
