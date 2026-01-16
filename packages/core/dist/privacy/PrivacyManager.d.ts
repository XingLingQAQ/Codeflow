/**
 * 隐私管理器实现
 * AES-CBC 加密 + 隐私感知 RAG
 */
import { IPrivacyManager, EncryptionConfig, EncryptedData, DecryptedData, KeyInfo, PrivacyPolicy, PrivacyAwareDocument, EncryptedSearchRequest, EncryptedSearchResult, KeyRotationEvent, EncryptionMetrics, PrivacyLevel } from './types.js';
export declare class PrivacyManager implements IPrivacyManager {
    private config;
    private masterPassword;
    private activeKey;
    private keys;
    private documents;
    private metrics;
    constructor(masterPassword: string, config?: Partial<EncryptionConfig>);
    encrypt(plaintext: string, policy?: PrivacyPolicy): Promise<EncryptedData>;
    decrypt(encrypted: EncryptedData): Promise<DecryptedData>;
    generateKey(): Promise<KeyInfo>;
    rotateKey(): Promise<KeyRotationEvent>;
    getActiveKey(): KeyInfo | null;
    encryptDocument(doc: PrivacyAwareDocument): Promise<PrivacyAwareDocument>;
    decryptDocument(doc: PrivacyAwareDocument): Promise<PrivacyAwareDocument>;
    search(request: EncryptedSearchRequest): Promise<EncryptedSearchResult[]>;
    getMetrics(): EncryptionMetrics;
    resetMetrics(): void;
    private deriveKey;
    private updateMetrics;
    private calculateRelevanceScore;
}
/**
 * 隐私感知向量存储
 * 支持加密向量的存储和检索
 */
export declare class PrivacyAwareVectorStore {
    private privacyManager;
    private vectors;
    constructor(privacyManager: PrivacyManager);
    storeVector(id: string, vector: number[], privacyLevel: PrivacyLevel): Promise<void>;
    retrieveVector(id: string): Promise<number[] | null>;
    searchSimilar(queryVector: number[], topK?: number): Promise<Array<{
        id: string;
        score: number;
    }>>;
    private cosineSimilarity;
}
//# sourceMappingURL=PrivacyManager.d.ts.map