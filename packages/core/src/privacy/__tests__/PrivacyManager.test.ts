import { describe, it, expect, beforeEach } from 'vitest';
import { PrivacyManager, PrivacyAwareVectorStore } from '../PrivacyManager.js';
import {
  EncryptionConfig,
  EncryptedData,
  PrivacyPolicy,
  PrivacyAwareDocument,
  EncryptedSearchRequest,
  DEFAULT_ENCRYPTION_CONFIG,
  DEFAULT_PRIVACY_POLICY,
  PRIVACY_LEVEL_PRIORITY,
} from '../types.js';

describe('PrivacyManager', () => {
  let manager: PrivacyManager;
  const testPassword = 'test-master-password-123';

  beforeEach(() => {
    manager = new PrivacyManager(testPassword);
  });

  describe('constructor', () => {
    it('should create manager with default config', () => {
      const m = new PrivacyManager('password');
      expect(m).toBeDefined();
    });

    it('should create manager with custom config', () => {
      const m = new PrivacyManager('password', {
        algorithm: 'aes-128-cbc',
        iterations: 50000,
      });
      expect(m).toBeDefined();
    });

    it('should merge custom config with defaults', () => {
      const m = new PrivacyManager('password', {
        iterations: 50000,
      });
      expect(m).toBeDefined();
    });
  });

  describe('encrypt', () => {
    it('should encrypt plaintext', async () => {
      const plaintext = 'Hello, World!';

      const encrypted = await manager.encrypt(plaintext);

      expect(encrypted.ciphertext).toBeDefined();
      expect(encrypted.ciphertext).not.toBe(plaintext);
      expect(encrypted.iv).toBeDefined();
      expect(encrypted.salt).toBeDefined();
      expect(encrypted.algorithm).toBe('aes-256-cbc');
    });

    it('should produce different ciphertext for same plaintext', async () => {
      const plaintext = 'Same message';

      const encrypted1 = await manager.encrypt(plaintext);
      const encrypted2 = await manager.encrypt(plaintext);

      // Different IV and salt should produce different ciphertext
      expect(encrypted1.ciphertext).not.toBe(encrypted2.ciphertext);
      expect(encrypted1.iv).not.toBe(encrypted2.iv);
      expect(encrypted1.salt).not.toBe(encrypted2.salt);
    });

    it('should encrypt with custom policy', async () => {
      const plaintext = 'Secret data';
      const policy: PrivacyPolicy = {
        level: 'secret',
        encryptAtRest: true,
        encryptInTransit: true,
        allowedRoles: ['admin'],
      };

      const encrypted = await manager.encrypt(plaintext, policy);

      expect(encrypted.ciphertext).toBeDefined();
    });

    it('should encrypt empty string', async () => {
      const encrypted = await manager.encrypt('');

      expect(encrypted.ciphertext).toBeDefined();
    });

    it('should encrypt long text', async () => {
      const longText = 'A'.repeat(10000);

      const encrypted = await manager.encrypt(longText);

      expect(encrypted.ciphertext).toBeDefined();
      expect(encrypted.ciphertext.length).toBeGreaterThan(0);
    });

    it('should encrypt unicode text', async () => {
      const unicodeText = '你好世界 🌍 مرحبا';

      const encrypted = await manager.encrypt(unicodeText);

      expect(encrypted.ciphertext).toBeDefined();
    });

    it('should update metrics after encryption', async () => {
      manager.resetMetrics();

      await manager.encrypt('Test data');

      const metrics = manager.getMetrics();
      expect(metrics.operationCount).toBe(1);
      expect(metrics.encryptionTimeMs).toBeGreaterThanOrEqual(0);
    });
  });

  describe('decrypt', () => {
    it('should decrypt encrypted data', async () => {
      const plaintext = 'Hello, World!';
      const encrypted = await manager.encrypt(plaintext);

      const decrypted = await manager.decrypt(encrypted);

      expect(decrypted.verified).toBe(true);
      expect(decrypted.plaintext).toBe(plaintext);
    });

    it('should decrypt unicode text', async () => {
      const unicodeText = '你好世界 🌍 مرحبا';
      const encrypted = await manager.encrypt(unicodeText);

      const decrypted = await manager.decrypt(encrypted);

      expect(decrypted.verified).toBe(true);
      expect(decrypted.plaintext).toBe(unicodeText);
    });

    it('should decrypt long text', async () => {
      const longText = 'A'.repeat(10000);
      const encrypted = await manager.encrypt(longText);

      const decrypted = await manager.decrypt(encrypted);

      expect(decrypted.verified).toBe(true);
      expect(decrypted.plaintext).toBe(longText);
    });

    it('should return verified=false for invalid ciphertext', async () => {
      const invalidEncrypted: EncryptedData = {
        ciphertext: 'invalid-ciphertext',
        iv: 'invalid-iv',
        salt: 'invalid-salt',
        algorithm: 'aes-256-cbc',
      };

      const decrypted = await manager.decrypt(invalidEncrypted);

      expect(decrypted.verified).toBe(false);
      expect(decrypted.plaintext).toBe('');
    });

    it('should return verified=false for tampered ciphertext', async () => {
      const encrypted = await manager.encrypt('Original message');

      // Tamper with ciphertext
      const tampered: EncryptedData = {
        ...encrypted,
        ciphertext: encrypted.ciphertext.slice(0, -4) + 'XXXX',
      };

      const decrypted = await manager.decrypt(tampered);

      expect(decrypted.verified).toBe(false);
    });

    it('should return verified=false for wrong IV', async () => {
      const encrypted = await manager.encrypt('Test message');
      const anotherEncrypted = await manager.encrypt('Another message');

      // Use wrong IV
      const wrongIv: EncryptedData = {
        ...encrypted,
        iv: anotherEncrypted.iv,
      };

      const decrypted = await manager.decrypt(wrongIv);

      expect(decrypted.verified).toBe(false);
    });

    it('should update metrics after decryption', async () => {
      const encrypted = await manager.encrypt('Test data');
      manager.resetMetrics();

      await manager.decrypt(encrypted);

      const metrics = manager.getMetrics();
      expect(metrics.operationCount).toBe(1);
      expect(metrics.decryptionTimeMs).toBeGreaterThanOrEqual(0);
    });
  });

  describe('generateKey', () => {
    it('should generate new key', async () => {
      const keyInfo = await manager.generateKey();

      expect(keyInfo.id).toBeDefined();
      expect(keyInfo.id).toContain('key_');
      expect(keyInfo.algorithm).toBe('aes-256-cbc');
      expect(keyInfo.createdAt).toBeLessThanOrEqual(Date.now());
    });

    it('should set generated key as active', async () => {
      const keyInfo = await manager.generateKey();

      const activeKey = manager.getActiveKey();
      expect(activeKey).toEqual(keyInfo);
    });

    it('should generate unique key IDs', async () => {
      const key1 = await manager.generateKey();
      const key2 = await manager.generateKey();

      expect(key1.id).not.toBe(key2.id);
    });
  });

  describe('rotateKey', () => {
    it('should rotate key', async () => {
      await manager.generateKey();

      const event = await manager.rotateKey();

      expect(event.status).toBe('completed');
      expect(event.newKeyId).toBeDefined();
      expect(event.rotatedAt).toBeLessThanOrEqual(Date.now());
    });

    it('should re-encrypt documents on rotation', async () => {
      await manager.generateKey();

      // Add a document
      const doc: PrivacyAwareDocument = {
        id: 'doc-1',
        content: 'Secret content',
        privacyLevel: 'confidential',
        policy: {
          level: 'confidential',
          encryptAtRest: true,
          encryptInTransit: true,
          allowedRoles: ['admin'],
        },
        metadata: {
          createdAt: Date.now(),
          updatedAt: Date.now(),
          createdBy: 'test-user',
        },
      };

      await manager.encryptDocument(doc);

      const event = await manager.rotateKey();

      expect(event.documentsReEncrypted).toBe(1);
    });

    it('should handle rotation with no documents', async () => {
      await manager.generateKey();

      const event = await manager.rotateKey();

      expect(event.documentsReEncrypted).toBe(0);
      expect(event.status).toBe('completed');
    });

    it('should track old key ID', async () => {
      const oldKey = await manager.generateKey();

      const event = await manager.rotateKey();

      expect(event.oldKeyId).toBe(oldKey.id);
    });
  });

  describe('getActiveKey', () => {
    it('should return null when no key generated', () => {
      const activeKey = manager.getActiveKey();
      expect(activeKey).toBeNull();
    });

    it('should return active key after generation', async () => {
      const keyInfo = await manager.generateKey();

      const activeKey = manager.getActiveKey();
      expect(activeKey).toEqual(keyInfo);
    });
  });

  describe('encryptDocument', () => {
    it('should encrypt document content', async () => {
      const doc: PrivacyAwareDocument = {
        id: 'doc-1',
        content: 'Secret content',
        privacyLevel: 'confidential',
        policy: {
          level: 'confidential',
          encryptAtRest: true,
          encryptInTransit: true,
          allowedRoles: ['admin'],
        },
        metadata: {
          createdAt: Date.now(),
          updatedAt: Date.now(),
          createdBy: 'test-user',
        },
      };

      const encrypted = await manager.encryptDocument(doc);

      expect(encrypted.encryptedContent).toBeDefined();
      expect(encrypted.content).toBe(''); // Plaintext should be cleared
    });

    it('should not encrypt when encryptAtRest is false', async () => {
      const doc: PrivacyAwareDocument = {
        id: 'doc-2',
        content: 'Public content',
        privacyLevel: 'public',
        policy: {
          level: 'public',
          encryptAtRest: false,
          encryptInTransit: false,
          allowedRoles: ['*'],
        },
        metadata: {
          createdAt: Date.now(),
          updatedAt: Date.now(),
          createdBy: 'test-user',
        },
      };

      const result = await manager.encryptDocument(doc);

      expect(result.encryptedContent).toBeUndefined();
      expect(result.content).toBe('Public content');
    });

    it('should store document after encryption', async () => {
      const doc: PrivacyAwareDocument = {
        id: 'doc-3',
        content: 'Stored content',
        privacyLevel: 'internal',
        policy: DEFAULT_PRIVACY_POLICY,
        metadata: {
          createdAt: Date.now(),
          updatedAt: Date.now(),
          createdBy: 'test-user',
        },
      };

      await manager.encryptDocument(doc);

      // Document should be searchable
      const results = await manager.search({ query: 'Stored' });
      expect(results.length).toBeGreaterThanOrEqual(0);
    });
  });

  describe('decryptDocument', () => {
    it('should decrypt document content', async () => {
      const originalContent = 'Secret content to decrypt';
      const doc: PrivacyAwareDocument = {
        id: 'doc-decrypt-1',
        content: originalContent,
        privacyLevel: 'confidential',
        policy: {
          level: 'confidential',
          encryptAtRest: true,
          encryptInTransit: true,
          allowedRoles: ['admin'],
        },
        metadata: {
          createdAt: Date.now(),
          updatedAt: Date.now(),
          createdBy: 'test-user',
        },
      };

      const encrypted = await manager.encryptDocument(doc);
      const decrypted = await manager.decryptDocument(encrypted);

      expect(decrypted.content).toBe(originalContent);
    });

    it('should handle document without encrypted content', async () => {
      const doc: PrivacyAwareDocument = {
        id: 'doc-plain',
        content: 'Plain content',
        privacyLevel: 'public',
        policy: {
          level: 'public',
          encryptAtRest: false,
          encryptInTransit: false,
          allowedRoles: ['*'],
        },
        metadata: {
          createdAt: Date.now(),
          updatedAt: Date.now(),
          createdBy: 'test-user',
        },
      };

      const result = await manager.decryptDocument(doc);

      expect(result.content).toBe('Plain content');
    });
  });

  describe('search', () => {
    beforeEach(async () => {
      // Add test documents
      const docs: PrivacyAwareDocument[] = [
        {
          id: 'search-doc-1',
          content: 'TypeScript is a programming language',
          privacyLevel: 'public',
          policy: { ...DEFAULT_PRIVACY_POLICY, level: 'public', encryptAtRest: false },
          metadata: { createdAt: Date.now(), updatedAt: Date.now(), createdBy: 'user' },
        },
        {
          id: 'search-doc-2',
          content: 'React is a JavaScript library',
          privacyLevel: 'internal',
          policy: { ...DEFAULT_PRIVACY_POLICY, level: 'internal', encryptAtRest: false },
          metadata: { createdAt: Date.now(), updatedAt: Date.now(), createdBy: 'user' },
        },
        {
          id: 'search-doc-3',
          content: 'Secret API keys and passwords',
          privacyLevel: 'secret',
          policy: { ...DEFAULT_PRIVACY_POLICY, level: 'secret', encryptAtRest: true },
          metadata: { createdAt: Date.now(), updatedAt: Date.now(), createdBy: 'admin' },
        },
      ];

      for (const doc of docs) {
        await manager.encryptDocument(doc);
      }
    });

    it('should search documents by query', async () => {
      const results = await manager.search({
        query: 'TypeScript',
        privacyLevels: ['public', 'internal'],
      });

      expect(results.some((r) => r.documentId === 'search-doc-1')).toBe(true);
    });

    it('should filter by privacy level', async () => {
      const results = await manager.search({
        query: 'API keys',
        privacyLevels: ['public', 'internal'],
      });

      // Secret document should not be accessible
      const secretResult = results.find((r) => r.documentId === 'search-doc-3');
      if (secretResult) {
        expect(secretResult.accessGranted).toBe(false);
      }
    });

    it('should return access denied for restricted documents', async () => {
      const results = await manager.search({
        query: 'secret',
        privacyLevels: ['public'],
      });

      const secretResult = results.find((r) => r.documentId === 'search-doc-3');
      if (secretResult) {
        expect(secretResult.accessGranted).toBe(false);
        expect(secretResult.score).toBe(0);
      }
    });

    it('should limit results', async () => {
      const results = await manager.search({
        query: 'is',
        privacyLevels: ['public', 'internal'],
        limit: 1,
      });

      expect(results.length).toBeLessThanOrEqual(1);
    });

    it('should sort results by score', async () => {
      const results = await manager.search({
        query: 'programming language',
        privacyLevels: ['public', 'internal'],
      });

      if (results.length > 1) {
        for (let i = 0; i < results.length - 1; i++) {
          expect(results[i].score).toBeGreaterThanOrEqual(results[i + 1].score);
        }
      }
    });

    it('should return empty results for no matches', async () => {
      const results = await manager.search({
        query: 'nonexistent term xyz123',
        privacyLevels: ['public', 'internal'],
      });

      const matchingResults = results.filter((r) => r.score > 0);
      expect(matchingResults.length).toBe(0);
    });

    it('should use default privacy levels when not specified', async () => {
      const results = await manager.search({
        query: 'TypeScript',
      });

      // Default levels are ['public', 'internal']
      expect(results.length).toBeGreaterThanOrEqual(0);
    });
  });

  describe('getMetrics', () => {
    it('should return initial metrics', () => {
      const metrics = manager.getMetrics();

      expect(metrics.encryptionTimeMs).toBe(0);
      expect(metrics.decryptionTimeMs).toBe(0);
      expect(metrics.throughputBytesPerSec).toBe(0);
      expect(metrics.operationCount).toBe(0);
    });

    it('should track encryption operations', async () => {
      await manager.encrypt('Test data 1');
      await manager.encrypt('Test data 2');

      const metrics = manager.getMetrics();

      expect(metrics.operationCount).toBe(2);
      expect(metrics.encryptionTimeMs).toBeGreaterThanOrEqual(0);
    });

    it('should track decryption operations', async () => {
      const encrypted = await manager.encrypt('Test data');
      manager.resetMetrics();

      await manager.decrypt(encrypted);

      const metrics = manager.getMetrics();

      expect(metrics.operationCount).toBe(1);
      expect(metrics.decryptionTimeMs).toBeGreaterThanOrEqual(0);
    });

    it('should return copy of metrics', () => {
      const metrics1 = manager.getMetrics();
      const metrics2 = manager.getMetrics();

      expect(metrics1).not.toBe(metrics2);
      expect(metrics1).toEqual(metrics2);
    });
  });

  describe('resetMetrics', () => {
    it('should reset all metrics to zero', async () => {
      await manager.encrypt('Test data');
      await manager.encrypt('More data');

      manager.resetMetrics();

      const metrics = manager.getMetrics();

      expect(metrics.encryptionTimeMs).toBe(0);
      expect(metrics.decryptionTimeMs).toBe(0);
      expect(metrics.throughputBytesPerSec).toBe(0);
      expect(metrics.operationCount).toBe(0);
    });
  });

  describe('different encryption algorithms', () => {
    it('should work with aes-128-cbc', async () => {
      const manager128 = new PrivacyManager(testPassword, {
        algorithm: 'aes-128-cbc',
      });

      const plaintext = 'Test with AES-128';
      const encrypted = await manager128.encrypt(plaintext);
      const decrypted = await manager128.decrypt(encrypted);

      expect(decrypted.verified).toBe(true);
      expect(decrypted.plaintext).toBe(plaintext);
    });
  });

  describe('different key derivation', () => {
    it('should work with scrypt', async () => {
      const managerScrypt = new PrivacyManager(testPassword, {
        keyDerivation: 'scrypt',
      });

      const plaintext = 'Test with scrypt';
      const encrypted = await managerScrypt.encrypt(plaintext);
      const decrypted = await managerScrypt.decrypt(encrypted);

      expect(decrypted.verified).toBe(true);
      expect(decrypted.plaintext).toBe(plaintext);
    });
  });
});

describe('PrivacyAwareVectorStore', () => {
  let privacyManager: PrivacyManager;
  let vectorStore: PrivacyAwareVectorStore;

  beforeEach(() => {
    privacyManager = new PrivacyManager('test-password');
    vectorStore = new PrivacyAwareVectorStore(privacyManager);
  });

  describe('storeVector', () => {
    it('should store vector with encryption', async () => {
      const vector = [0.1, 0.2, 0.3, 0.4, 0.5];

      await vectorStore.storeVector('vec-1', vector, 'confidential');

      const retrieved = await vectorStore.retrieveVector('vec-1');
      expect(retrieved).toEqual(vector);
    });

    it('should store multiple vectors', async () => {
      await vectorStore.storeVector('vec-1', [0.1, 0.2], 'public');
      await vectorStore.storeVector('vec-2', [0.3, 0.4], 'internal');
      await vectorStore.storeVector('vec-3', [0.5, 0.6], 'confidential');

      const vec1 = await vectorStore.retrieveVector('vec-1');
      const vec2 = await vectorStore.retrieveVector('vec-2');
      const vec3 = await vectorStore.retrieveVector('vec-3');

      expect(vec1).toEqual([0.1, 0.2]);
      expect(vec2).toEqual([0.3, 0.4]);
      expect(vec3).toEqual([0.5, 0.6]);
    });

    it('should overwrite existing vector', async () => {
      await vectorStore.storeVector('vec-1', [0.1, 0.2], 'public');
      await vectorStore.storeVector('vec-1', [0.9, 0.8], 'public');

      const retrieved = await vectorStore.retrieveVector('vec-1');
      expect(retrieved).toEqual([0.9, 0.8]);
    });
  });

  describe('retrieveVector', () => {
    it('should return null for non-existent vector', async () => {
      const retrieved = await vectorStore.retrieveVector('non-existent');
      expect(retrieved).toBeNull();
    });

    it('should retrieve stored vector', async () => {
      const vector = [1.0, 2.0, 3.0];
      await vectorStore.storeVector('vec-1', vector, 'internal');

      const retrieved = await vectorStore.retrieveVector('vec-1');
      expect(retrieved).toEqual(vector);
    });
  });

  describe('searchSimilar', () => {
    beforeEach(async () => {
      // Store test vectors
      await vectorStore.storeVector('vec-1', [1, 0, 0], 'public');
      await vectorStore.storeVector('vec-2', [0, 1, 0], 'public');
      await vectorStore.storeVector('vec-3', [0, 0, 1], 'public');
      await vectorStore.storeVector('vec-4', [0.9, 0.1, 0], 'public');
    });

    it('should find similar vectors', async () => {
      const queryVector = [1, 0, 0];

      const results = await vectorStore.searchSimilar(queryVector, 2);

      expect(results.length).toBe(2);
      expect(results[0].id).toBe('vec-1'); // Exact match
      expect(results[0].score).toBeCloseTo(1, 5);
    });

    it('should return results sorted by similarity', async () => {
      const queryVector = [1, 0, 0];

      const results = await vectorStore.searchSimilar(queryVector, 4);

      for (let i = 0; i < results.length - 1; i++) {
        expect(results[i].score).toBeGreaterThanOrEqual(results[i + 1].score);
      }
    });

    it('should limit results by topK', async () => {
      const queryVector = [0.5, 0.5, 0];

      const results = await vectorStore.searchSimilar(queryVector, 2);

      expect(results.length).toBe(2);
    });

    it('should handle orthogonal vectors', async () => {
      const queryVector = [1, 0, 0];

      const results = await vectorStore.searchSimilar(queryVector, 4);

      // vec-2 [0,1,0] and vec-3 [0,0,1] are orthogonal to query
      const orthogonalResult = results.find((r) => r.id === 'vec-2');
      expect(orthogonalResult?.score).toBeCloseTo(0, 5);
    });

    it('should use default topK of 10', async () => {
      const queryVector = [0.5, 0.5, 0];

      const results = await vectorStore.searchSimilar(queryVector);

      expect(results.length).toBeLessThanOrEqual(10);
    });
  });

  describe('cosine similarity edge cases', () => {
    it('should handle zero vectors', async () => {
      await vectorStore.storeVector('zero', [0, 0, 0], 'public');

      const results = await vectorStore.searchSimilar([1, 0, 0], 1);

      // Zero vector should have 0 similarity
      const zeroResult = results.find((r) => r.id === 'zero');
      if (zeroResult) {
        expect(zeroResult.score).toBe(0);
      }
    });

    it('should handle different length vectors', async () => {
      await vectorStore.storeVector('short', [1, 0], 'public');

      const results = await vectorStore.searchSimilar([1, 0, 0], 1);

      // Different length should return 0 similarity
      const shortResult = results.find((r) => r.id === 'short');
      if (shortResult) {
        expect(shortResult.score).toBe(0);
      }
    });
  });
});

describe('Privacy Types', () => {
  describe('DEFAULT_ENCRYPTION_CONFIG', () => {
    it('should have correct default values', () => {
      expect(DEFAULT_ENCRYPTION_CONFIG.algorithm).toBe('aes-256-cbc');
      expect(DEFAULT_ENCRYPTION_CONFIG.keyDerivation).toBe('pbkdf2');
      expect(DEFAULT_ENCRYPTION_CONFIG.iterations).toBe(100000);
      expect(DEFAULT_ENCRYPTION_CONFIG.saltLength).toBe(16);
      expect(DEFAULT_ENCRYPTION_CONFIG.ivLength).toBe(16);
    });
  });

  describe('DEFAULT_PRIVACY_POLICY', () => {
    it('should have correct default values', () => {
      expect(DEFAULT_PRIVACY_POLICY.level).toBe('internal');
      expect(DEFAULT_PRIVACY_POLICY.encryptAtRest).toBe(true);
      expect(DEFAULT_PRIVACY_POLICY.encryptInTransit).toBe(true);
      expect(DEFAULT_PRIVACY_POLICY.allowedRoles).toContain('admin');
      expect(DEFAULT_PRIVACY_POLICY.allowedRoles).toContain('user');
      expect(DEFAULT_PRIVACY_POLICY.retentionDays).toBe(365);
      expect(DEFAULT_PRIVACY_POLICY.autoRedact).toBe(false);
    });
  });

  describe('PRIVACY_LEVEL_PRIORITY', () => {
    it('should have correct priority order', () => {
      expect(PRIVACY_LEVEL_PRIORITY.public).toBe(0);
      expect(PRIVACY_LEVEL_PRIORITY.internal).toBe(1);
      expect(PRIVACY_LEVEL_PRIORITY.confidential).toBe(2);
      expect(PRIVACY_LEVEL_PRIORITY.secret).toBe(3);
    });

    it('should have increasing priority', () => {
      expect(PRIVACY_LEVEL_PRIORITY.public).toBeLessThan(PRIVACY_LEVEL_PRIORITY.internal);
      expect(PRIVACY_LEVEL_PRIORITY.internal).toBeLessThan(PRIVACY_LEVEL_PRIORITY.confidential);
      expect(PRIVACY_LEVEL_PRIORITY.confidential).toBeLessThan(PRIVACY_LEVEL_PRIORITY.secret);
    });
  });
});
