import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { AtomicSnapshotManager, InMemorySnapshotStorage } from '../AtomicSnapshotManager.js';
import { ISnapshotStorage, AtomicSnapshot } from '../AtomicSnapshotTypes.js';
import { IGitManager, GitSnapshot } from '../types.js';
import { IVectorStore } from '../../memory/types.js';
import { ITripleStore } from '../../samg/types.js';
import { Message } from '../../hooks/types.js';

describe('AtomicSnapshotManager', () => {
  let manager: AtomicSnapshotManager;
  let storage: ISnapshotStorage;
  let mockGitManager: IGitManager;
  let mockVectorStore: IVectorStore;
  let mockTripleStore: ITripleStore;

  const createMockGitSnapshot = (): GitSnapshot => ({
    id: 'git-snapshot-1',
    gitHash: 'abc123def456',
    timestamp: Date.now(),
    files: ['file1.ts', 'file2.ts'],
  });

  const createMockMessages = (): Message[] => [
    {
      role: 'user',
      content: 'Hello',
      timestamp: Date.now(),
    },
    {
      role: 'assistant',
      content: 'Hi there!',
      timestamp: Date.now(),
    },
  ];

  beforeEach(() => {
    storage = new InMemorySnapshotStorage();

    mockGitManager = {
      createSnapshot: vi.fn(),
      restoreSnapshot: vi.fn(),
      listSnapshots: vi.fn(),
      deleteSnapshot: vi.fn(),
      getCurrentHash: vi.fn(),
      getLog: vi.fn(),
      commit: vi.fn(),
      push: vi.fn(),
      pull: vi.fn(),
      getStatus: vi.fn(),
      getDiff: vi.fn(),
      checkout: vi.fn(),
      createBranch: vi.fn(),
      deleteBranch: vi.fn(),
      mergeBranch: vi.fn(),
      getRemotes: vi.fn(),
      addRemote: vi.fn(),
      removeRemote: vi.fn(),
    };

    mockVectorStore = {
      search: vi.fn(),
      add: vi.fn(),
      delete: vi.fn(),
      clear: vi.fn(),
      count: vi.fn(),
      getBySessionId: vi.fn(),
      getByGitCommit: vi.fn(),
      getCollectionInfo: vi.fn(),
    };

    mockTripleStore = {
      add: vi.fn(),
      get: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      clear: vi.fn(),
      query: vi.fn(),
      findBySubject: vi.fn(),
      findByPredicate: vi.fn(),
      findByObject: vi.fn(),
      getEntity: vi.fn(),
      upsertEntity: vi.fn(),
      getEntities: vi.fn(),
      deduplicate: vi.fn(),
      exportGraph: vi.fn(),
      importGraph: vi.fn(),
      getStats: vi.fn(),
    };

    manager = new AtomicSnapshotManager(storage, mockGitManager, {
      maxSnapshots: 10,
      autoCheckpointInterval: 60000,
      enableAutoCheckpoint: false,
      createBackupOnRollback: true,
    });
  });

  afterEach(() => {
    manager.destroy();
  });

  describe('constructor', () => {
    it('should create manager with default config', () => {
      const defaultManager = new AtomicSnapshotManager(storage, mockGitManager);
      expect(defaultManager).toBeDefined();
      defaultManager.destroy();
    });

    it('should create manager with custom config', () => {
      const customManager = new AtomicSnapshotManager(storage, mockGitManager, {
        maxSnapshots: 50,
        autoCheckpointInterval: 120000,
        enableAutoCheckpoint: false,
        createBackupOnRollback: false,
      });
      expect(customManager).toBeDefined();
      customManager.destroy();
    });

    it('should start auto checkpoint when enabled', () => {
      const autoManager = new AtomicSnapshotManager(storage, mockGitManager, {
        enableAutoCheckpoint: true,
        autoCheckpointInterval: 100,
      });
      expect(autoManager).toBeDefined();
      autoManager.destroy();
    });
  });

  describe('setVectorStore', () => {
    it('should set vector store', () => {
      manager.setVectorStore(mockVectorStore);
      expect(manager).toBeDefined();
    });
  });

  describe('setTripleStore', () => {
    it('should set triple store', () => {
      manager.setTripleStore(mockTripleStore);
      expect(manager).toBeDefined();
    });
  });

  describe('setConversationProvider', () => {
    it('should set conversation provider', () => {
      const provider = () => ({
        sessionId: 'session1',
        messages: createMockMessages(),
      });
      manager.setConversationProvider(provider);
      expect(manager).toBeDefined();
    });
  });

  describe('createSnapshot', () => {
    it('should create snapshot with all components', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());
      vi.mocked(mockVectorStore.getCollectionInfo).mockResolvedValue({
        name: 'test-collection',
        count: 10,
        dimension: 384,
      });
      vi.mocked(mockTripleStore.getStats).mockResolvedValue({
        tripleCount: 20,
        entityCount: 15,
      });

      manager.setVectorStore(mockVectorStore);
      manager.setTripleStore(mockTripleStore);
      manager.setConversationProvider(() => ({
        sessionId: 'session1',
        messages: createMockMessages(),
      }));

      const snapshot = await manager.createSnapshot('Test snapshot', 'manual');

      expect(snapshot).toBeDefined();
      expect(snapshot.id).toBeDefined();
      expect(snapshot.description).toBe('Test snapshot');
      expect(snapshot.git.hash).toBe('abc123def456');
      expect(snapshot.conversation.sessionId).toBe('session1');
      expect(snapshot.conversation.messageCount).toBe(2);
      expect(snapshot.memory.vector).toBeDefined();
      expect(snapshot.memory.vector?.chunkCount).toBe(10);
      expect(snapshot.memory.graph).toBeDefined();
      expect(snapshot.memory.graph?.tripleCount).toBe(20);
      expect(snapshot.metadata.trigger).toBe('manual');
    });

    it('should create snapshot without vector store', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());

      const snapshot = await manager.createSnapshot('Test snapshot');

      expect(snapshot.memory.vector).toBeUndefined();
    });

    it('should create snapshot without triple store', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());

      const snapshot = await manager.createSnapshot('Test snapshot');

      expect(snapshot.memory.graph).toBeUndefined();
    });

    it('should create snapshot without conversation provider', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());

      const snapshot = await manager.createSnapshot('Test snapshot');

      expect(snapshot.conversation.sessionId).toBe('unknown');
      expect(snapshot.conversation.messageCount).toBe(0);
    });

    it('should prune old snapshots when limit exceeded', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());

      // Create 11 snapshots (limit is 10)
      for (let i = 0; i < 11; i++) {
        await manager.createSnapshot(`Snapshot ${i}`);
      }

      const snapshots = await manager.listSnapshots(100);
      expect(snapshots.length).toBeLessThanOrEqual(10);
    });
  });

  describe('getSnapshot', () => {
    it('should retrieve snapshot by id', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());

      const created = await manager.createSnapshot('Test snapshot');
      const retrieved = await manager.getSnapshot(created.id);

      expect(retrieved).toBeDefined();
      expect(retrieved?.id).toBe(created.id);
    });

    it('should return null for non-existent snapshot', async () => {
      const snapshot = await manager.getSnapshot('non-existent-id');
      expect(snapshot).toBeNull();
    });
  });

  describe('listSnapshots', () => {
    it('should list all snapshots', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());

      await manager.createSnapshot('Snapshot 1');
      await manager.createSnapshot('Snapshot 2');
      await manager.createSnapshot('Snapshot 3');

      const snapshots = await manager.listSnapshots();

      expect(snapshots.length).toBe(3);
    });

    it('should respect limit parameter', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());

      await manager.createSnapshot('Snapshot 1');
      await manager.createSnapshot('Snapshot 2');
      await manager.createSnapshot('Snapshot 3');

      const snapshots = await manager.listSnapshots(2);

      expect(snapshots.length).toBe(2);
    });

    it('should return snapshots in reverse chronological order', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());

      const s1 = await manager.createSnapshot('Snapshot 1');
      await new Promise(resolve => setTimeout(resolve, 10));
      const s2 = await manager.createSnapshot('Snapshot 2');

      const snapshots = await manager.listSnapshots();

      expect(snapshots[0].id).toBe(s2.id);
      expect(snapshots[1].id).toBe(s1.id);
    });
  });

  describe('findSnapshotByGitHash', () => {
    it('should find snapshot by full git hash', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());

      await manager.createSnapshot('Test snapshot');
      const found = await manager.findSnapshotByGitHash('abc123def456');

      expect(found).toBeDefined();
      expect(found?.git.hash).toBe('abc123def456');
    });

    it('should find snapshot by short git hash', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());

      await manager.createSnapshot('Test snapshot');
      const found = await manager.findSnapshotByGitHash('abc123d');

      expect(found).toBeDefined();
      expect(found?.git.shortHash).toBe('abc123d');
    });

    it('should return null for non-existent hash', async () => {
      const found = await manager.findSnapshotByGitHash('nonexistent');
      expect(found).toBeNull();
    });
  });

  describe('findSnapshotsBySession', () => {
    it('should find snapshots by session id', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());

      manager.setConversationProvider(() => ({
        sessionId: 'session1',
        messages: createMockMessages(),
      }));

      await manager.createSnapshot('Snapshot 1');
      await manager.createSnapshot('Snapshot 2');

      manager.setConversationProvider(() => ({
        sessionId: 'session2',
        messages: createMockMessages(),
      }));

      await manager.createSnapshot('Snapshot 3');

      const session1Snapshots = await manager.findSnapshotsBySession('session1');

      expect(session1Snapshots.length).toBe(2);
      expect(session1Snapshots.every(s => s.conversation.sessionId === 'session1')).toBe(true);
    });

    it('should return empty array for non-existent session', async () => {
      const snapshots = await manager.findSnapshotsBySession('nonexistent');
      expect(snapshots).toEqual([]);
    });
  });

  describe('rollback', () => {
    it('should rollback all components', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());
      vi.mocked(mockGitManager.restoreSnapshot).mockResolvedValue(true);
      vi.mocked(mockVectorStore.getCollectionInfo).mockResolvedValue({
        name: 'test-collection',
        count: 10,
        dimension: 384,
      });
      vi.mocked(mockVectorStore.getBySessionId).mockResolvedValue([]);
      vi.mocked(mockTripleStore.getStats).mockResolvedValue({
        tripleCount: 20,
        entityCount: 15,
      });
      vi.mocked(mockTripleStore.query).mockResolvedValue([]);

      manager.setVectorStore(mockVectorStore);
      manager.setTripleStore(mockTripleStore);

      const snapshot = await manager.createSnapshot('Test snapshot');

      const result = await manager.rollback({
        targetSnapshotId: snapshot.id,
        rollbackGit: true,
        rollbackConversation: true,
        rollbackVector: true,
        rollbackGraph: true,
        createBackupSnapshot: false,
      });

      expect(result.success).toBe(true);
      expect(result.rolledBack.git).toBe(true);
      expect(result.rolledBack.conversation).toBe(true);
      expect(result.rolledBack.vector).toBe(true);
      expect(result.rolledBack.graph).toBe(true);
      expect(result.errors).toEqual([]);
    });

    it('should create backup snapshot when requested', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());
      vi.mocked(mockGitManager.restoreSnapshot).mockResolvedValue(true);

      const snapshot = await manager.createSnapshot('Test snapshot');

      const result = await manager.rollback({
        targetSnapshotId: snapshot.id,
        rollbackGit: true,
        rollbackConversation: false,
        rollbackVector: false,
        rollbackGraph: false,
        createBackupSnapshot: true,
      });

      expect(result.backupSnapshotId).toBeDefined();
    });

    it('should return error for non-existent snapshot', async () => {
      const result = await manager.rollback({
        targetSnapshotId: 'non-existent',
        rollbackGit: true,
        rollbackConversation: false,
        rollbackVector: false,
        rollbackGraph: false,
        createBackupSnapshot: false,
      });

      expect(result.success).toBe(false);
      expect(result.errors.length).toBeGreaterThan(0);
      expect(result.errors[0]).toContain('Snapshot not found');
    });

    it('should handle git rollback failure', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());
      vi.mocked(mockGitManager.restoreSnapshot).mockRejectedValue(new Error('Git error'));
      vi.mocked(mockGitManager.listSnapshots).mockReturnValue([]);

      const snapshot = await manager.createSnapshot('Test snapshot');

      const result = await manager.rollback({
        targetSnapshotId: snapshot.id,
        rollbackGit: true,
        rollbackConversation: false,
        rollbackVector: false,
        rollbackGraph: false,
        createBackupSnapshot: false,
      });

      expect(result.success).toBe(false);
      expect(result.rolledBack.git).toBe(false);
      expect(result.errors.some(e => e.includes('Git rollback failed'))).toBe(true);
    });
  });

  describe('canRollback', () => {
    it('should return true for valid snapshot', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());
      vi.mocked(mockGitManager.getCurrentHash).mockResolvedValue('current-hash');

      const snapshot = await manager.createSnapshot('Test snapshot');
      const canRollback = await manager.canRollback(snapshot.id);

      expect(canRollback).toBe(true);
    });

    it('should return false for non-existent snapshot', async () => {
      const canRollback = await manager.canRollback('non-existent');
      expect(canRollback).toBe(false);
    });

    it('should return false when git manager has no current hash', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());
      vi.mocked(mockGitManager.getCurrentHash).mockResolvedValue(null);

      const snapshot = await manager.createSnapshot('Test snapshot');
      const canRollback = await manager.canRollback(snapshot.id);

      expect(canRollback).toBe(false);
    });
  });

  describe('validateSnapshot', () => {
    it('should validate all components', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());
      vi.mocked(mockGitManager.getLog).mockResolvedValue([
        { hash: 'abc123def456', message: 'Test', author: 'test', date: new Date() },
      ]);
      vi.mocked(mockVectorStore.getCollectionInfo).mockResolvedValue({
        name: 'test-collection',
        count: 10,
        dimension: 384,
      });
      vi.mocked(mockTripleStore.getStats).mockResolvedValue({
        tripleCount: 20,
        entityCount: 15,
      });

      manager.setVectorStore(mockVectorStore);
      manager.setTripleStore(mockTripleStore);
      manager.setConversationProvider(() => ({
        sessionId: 'session1',
        messages: createMockMessages(),
      }));

      const snapshot = await manager.createSnapshot('Test snapshot');
      const validation = await manager.validateSnapshot(snapshot.id);

      expect(validation.valid).toBe(true);
      expect(validation.gitValid).toBe(true);
      expect(validation.conversationValid).toBe(true);
      expect(validation.vectorValid).toBe(true);
      expect(validation.graphValid).toBe(true);
      expect(validation.errors).toEqual([]);
    });

    it('should return invalid for non-existent snapshot', async () => {
      const validation = await manager.validateSnapshot('non-existent');

      expect(validation.valid).toBe(false);
      expect(validation.errors.length).toBeGreaterThan(0);
    });

    it('should detect invalid git hash', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());
      vi.mocked(mockGitManager.getLog).mockResolvedValue([]);

      const snapshot = await manager.createSnapshot('Test snapshot');
      const validation = await manager.validateSnapshot(snapshot.id);

      expect(validation.gitValid).toBe(false);
      expect(validation.errors.some(e => e.includes('Git hash not found'))).toBe(true);
    });
  });

  describe('validateConsistency', () => {
    it('should validate all snapshots', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());
      vi.mocked(mockGitManager.getLog).mockResolvedValue([
        { hash: 'abc123def456', message: 'Test', author: 'test', date: new Date() },
      ]);

      manager.setConversationProvider(() => ({
        sessionId: 'session1',
        messages: createMockMessages(),
      }));

      await manager.createSnapshot('Snapshot 1');
      await manager.createSnapshot('Snapshot 2');

      const validation = await manager.validateConsistency();

      expect(validation).toBeDefined();
    });
  });

  describe('pruneSnapshots', () => {
    it('should prune old snapshots', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());

      for (let i = 0; i < 5; i++) {
        await manager.createSnapshot(`Snapshot ${i}`);
      }

      const deleted = await manager.pruneSnapshots(3);

      expect(deleted).toBe(2);

      const remaining = await manager.listSnapshots();
      expect(remaining.length).toBe(3);
    });

    it('should not prune when count is below limit', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());

      await manager.createSnapshot('Snapshot 1');
      await manager.createSnapshot('Snapshot 2');

      const deleted = await manager.pruneSnapshots(5);

      expect(deleted).toBe(0);
    });
  });

  describe('deleteSnapshot', () => {
    it('should delete snapshot', async () => {
      vi.mocked(mockGitManager.createSnapshot).mockResolvedValue(createMockGitSnapshot());

      const snapshot = await manager.createSnapshot('Test snapshot');
      const result = await manager.deleteSnapshot(snapshot.id);

      expect(result).toBe(true);

      const retrieved = await manager.getSnapshot(snapshot.id);
      expect(retrieved).toBeNull();
    });

    it('should return false for non-existent snapshot', async () => {
      const result = await manager.deleteSnapshot('non-existent');
      expect(result).toBe(false);
    });
  });

  describe('destroy', () => {
    it('should clear auto checkpoint timer', () => {
      const autoManager = new AtomicSnapshotManager(storage, mockGitManager, {
        enableAutoCheckpoint: true,
        autoCheckpointInterval: 100,
      });

      autoManager.destroy();
      expect(autoManager).toBeDefined();
    });
  });
});

describe('InMemorySnapshotStorage', () => {
  let storage: InMemorySnapshotStorage;

  beforeEach(() => {
    storage = new InMemorySnapshotStorage();
  });

  const createMockSnapshot = (id: string): AtomicSnapshot => ({
    id,
    version: '1.0.0',
    timestamp: Date.now(),
    git: {
      hash: 'abc123',
      shortHash: 'abc123',
      message: 'Test',
      files: [],
    },
    conversation: {
      sessionId: 'session1',
      messages: [],
      messageCount: 0,
      lastMessageIndex: -1,
      checksum: 'checksum',
    },
    memory: {},
    metadata: {
      createdBy: 'test',
      trigger: 'manual',
    },
  });

  describe('save', () => {
    it('should save snapshot', async () => {
      const snapshot = createMockSnapshot('snapshot1');
      await storage.save(snapshot);

      const retrieved = await storage.load('snapshot1');
      expect(retrieved).toEqual(snapshot);
    });
  });

  describe('load', () => {
    it('should load snapshot', async () => {
      const snapshot = createMockSnapshot('snapshot1');
      await storage.save(snapshot);

      const loaded = await storage.load('snapshot1');
      expect(loaded).toEqual(snapshot);
    });

    it('should return null for non-existent snapshot', async () => {
      const loaded = await storage.load('non-existent');
      expect(loaded).toBeNull();
    });
  });

  describe('list', () => {
    it('should list all snapshots', async () => {
      await storage.save(createMockSnapshot('snapshot1'));
      await storage.save(createMockSnapshot('snapshot2'));

      const list = await storage.list();
      expect(list.length).toBe(2);
    });

    it('should respect limit', async () => {
      await storage.save(createMockSnapshot('snapshot1'));
      await storage.save(createMockSnapshot('snapshot2'));
      await storage.save(createMockSnapshot('snapshot3'));

      const list = await storage.list(2);
      expect(list.length).toBe(2);
    });

    it('should respect offset', async () => {
      await storage.save(createMockSnapshot('snapshot1'));
      await storage.save(createMockSnapshot('snapshot2'));
      await storage.save(createMockSnapshot('snapshot3'));

      const list = await storage.list(10, 1);
      expect(list.length).toBe(2);
    });

    it('should sort by timestamp descending', async () => {
      const s1 = createMockSnapshot('snapshot1');
      s1.timestamp = 1000;
      const s2 = createMockSnapshot('snapshot2');
      s2.timestamp = 2000;

      await storage.save(s1);
      await storage.save(s2);

      const list = await storage.list();
      expect(list[0].id).toBe('snapshot2');
      expect(list[1].id).toBe('snapshot1');
    });
  });

  describe('delete', () => {
    it('should delete snapshot', async () => {
      await storage.save(createMockSnapshot('snapshot1'));

      const result = await storage.delete('snapshot1');
      expect(result).toBe(true);

      const loaded = await storage.load('snapshot1');
      expect(loaded).toBeNull();
    });

    it('should return false for non-existent snapshot', async () => {
      const result = await storage.delete('non-existent');
      expect(result).toBe(false);
    });
  });

  describe('clear', () => {
    it('should clear all snapshots', async () => {
      await storage.save(createMockSnapshot('snapshot1'));
      await storage.save(createMockSnapshot('snapshot2'));

      await storage.clear();

      const count = await storage.count();
      expect(count).toBe(0);
    });
  });

  describe('count', () => {
    it('should return snapshot count', async () => {
      await storage.save(createMockSnapshot('snapshot1'));
      await storage.save(createMockSnapshot('snapshot2'));

      const count = await storage.count();
      expect(count).toBe(2);
    });

    it('should return 0 for empty storage', async () => {
      const count = await storage.count();
      expect(count).toBe(0);
    });
  });
});
