import { describe, it, expect, beforeEach, vi } from 'vitest';
import { TransactionManager } from '../TransactionManager.js';
import { ILockManager, LockType, LockResult, RESOURCE_TYPES } from '../LockTypes.js';

describe('TransactionManager', () => {
  let manager: TransactionManager;
  let mockLockManager: ILockManager;

  beforeEach(() => {
    mockLockManager = {
      acquire: vi.fn(),
      release: vi.fn(),
      isLocked: vi.fn(),
      getLockInfo: vi.fn(),
      getActiveLocks: vi.fn(),
      releaseAll: vi.fn(),
    };

    manager = new TransactionManager(mockLockManager, {
      transactionTimeout: 5000,
      autoRollbackOnError: true,
      lockTimeout: 3000,
    });
  });

  describe('constructor', () => {
    it('should create manager with default config', () => {
      const defaultManager = new TransactionManager(mockLockManager);
      expect(defaultManager).toBeDefined();
    });

    it('should create manager with custom config', () => {
      const customManager = new TransactionManager(mockLockManager, {
        transactionTimeout: 10000,
        autoRollbackOnError: false,
        lockTimeout: 5000,
      });
      expect(customManager).toBeDefined();
    });
  });

  describe('begin', () => {
    it('should create new transaction', async () => {
      const txId = await manager.begin('owner1');

      expect(txId).toBeDefined();
      expect(typeof txId).toBe('string');

      const tx = manager.getTransaction(txId);
      expect(tx).toBeDefined();
      expect(tx?.state).toBe('active');
      expect(tx?.locks).toEqual([]);
      expect(tx?.operations).toEqual([]);
    });

    it('should prevent duplicate active transactions for same owner', async () => {
      await manager.begin('owner1');

      await expect(manager.begin('owner1')).rejects.toThrow(
        'already has an active transaction'
      );
    });

    it('should allow new transaction after previous one completes', async () => {
      const txId1 = await manager.begin('owner1');
      vi.mocked(mockLockManager.acquire).mockResolvedValue({
        success: true,
        lockId: 'lock1',
      });
      await manager.commit(txId1);

      const txId2 = await manager.begin('owner1');
      expect(txId2).toBeDefined();
      expect(txId2).not.toBe(txId1);
    });
  });

  describe('commit', () => {
    it('should commit active transaction', async () => {
      const txId = await manager.begin('owner1');
      vi.mocked(mockLockManager.acquire).mockResolvedValue({
        success: true,
        lockId: 'lock1',
      });
      vi.mocked(mockLockManager.release).mockResolvedValue(true);

      await manager.acquireLock(txId, 'resource1', 'exclusive');
      const result = await manager.commit(txId);

      expect(result).toBe(true);
      const tx = manager.getTransaction(txId);
      expect(tx?.state).toBe('committed');
    });

    it('should throw error for non-existent transaction', async () => {
      await expect(manager.commit('invalid-tx-id')).rejects.toThrow(
        'Transaction not found'
      );
    });

    it('should throw error for non-active transaction', async () => {
      const txId = await manager.begin('owner1');
      vi.mocked(mockLockManager.release).mockResolvedValue(true);
      await manager.commit(txId);

      await expect(manager.commit(txId)).rejects.toThrow('is not active');
    });

    it('should release locks on commit', async () => {
      const txId = await manager.begin('owner1');
      vi.mocked(mockLockManager.acquire).mockResolvedValue({
        success: true,
        lockId: 'lock1',
      });
      vi.mocked(mockLockManager.release).mockResolvedValue(true);

      await manager.acquireLock(txId, 'resource1', 'exclusive');
      await manager.commit(txId);

      expect(mockLockManager.release).toHaveBeenCalledWith('lock1');
    });

    it('should rollback on commit failure when autoRollbackOnError is true', async () => {
      const txId = await manager.begin('owner1');
      manager.recordOperation(txId, {
        type: 'vector',
        action: 'add',
        resourceId: 'resource1',
        timestamp: Date.now(),
      });

      // Mock validation failure
      vi.spyOn(manager as any, 'validateOperation').mockReturnValue(false);
      vi.mocked(mockLockManager.release).mockResolvedValue(true);

      await expect(manager.commit(txId)).rejects.toThrow('Invalid operation');

      const tx = manager.getTransaction(txId);
      expect(tx?.state).toBe('rolledback');
    });
  });

  describe('rollback', () => {
    it('should rollback active transaction', async () => {
      const txId = await manager.begin('owner1');
      manager.recordOperation(txId, {
        type: 'vector',
        action: 'add',
        resourceId: 'resource1',
        timestamp: Date.now(),
      });
      vi.mocked(mockLockManager.release).mockResolvedValue(true);

      const result = await manager.rollback(txId);

      expect(result).toBe(true);
      const tx = manager.getTransaction(txId);
      expect(tx?.state).toBe('rolledback');
    });

    it('should throw error for non-existent transaction', async () => {
      await expect(manager.rollback('invalid-tx-id')).rejects.toThrow(
        'Transaction not found'
      );
    });

    it('should prevent rollback during active Git operation', async () => {
      const txId = await manager.begin('owner1');
      manager.recordOperation(txId, {
        type: 'git',
        action: 'begin',
        resourceId: RESOURCE_TYPES.GIT,
        timestamp: Date.now(),
      });

      await expect(manager.rollback(txId)).rejects.toThrow(
        'Cannot rollback: Git operation in progress'
      );
    });

    it('should release locks on rollback', async () => {
      const txId = await manager.begin('owner1');
      vi.mocked(mockLockManager.acquire).mockResolvedValue({
        success: true,
        lockId: 'lock1',
      });
      vi.mocked(mockLockManager.release).mockResolvedValue(true);

      await manager.acquireLock(txId, 'resource1', 'exclusive');
      await manager.rollback(txId);

      expect(mockLockManager.release).toHaveBeenCalledWith('lock1');
    });
  });

  describe('acquireLock', () => {
    it('should acquire lock for active transaction', async () => {
      const txId = await manager.begin('owner1');
      vi.mocked(mockLockManager.acquire).mockResolvedValue({
        success: true,
        lockId: 'lock1',
      });

      const result = await manager.acquireLock(txId, 'resource1', 'exclusive');

      expect(result).toBe(true);
      expect(mockLockManager.acquire).toHaveBeenCalledWith(
        'resource1',
        'exclusive',
        txId,
        3000
      );

      const tx = manager.getTransaction(txId);
      expect(tx?.locks).toContain('lock1');
    });

    it('should throw error for non-existent transaction', async () => {
      await expect(
        manager.acquireLock('invalid-tx-id', 'resource1', 'exclusive')
      ).rejects.toThrow('Transaction not found');
    });

    it('should throw error for non-active transaction', async () => {
      const txId = await manager.begin('owner1');
      vi.mocked(mockLockManager.release).mockResolvedValue(true);
      await manager.commit(txId);

      await expect(
        manager.acquireLock(txId, 'resource1', 'exclusive')
      ).rejects.toThrow('is not active');
    });

    it('should return false when lock acquisition fails', async () => {
      const txId = await manager.begin('owner1');
      vi.mocked(mockLockManager.acquire).mockResolvedValue({
        success: false,
      });

      const result = await manager.acquireLock(txId, 'resource1', 'exclusive');

      expect(result).toBe(false);
    });
  });

  describe('recordOperation', () => {
    it('should record operation for active transaction', () => {
      const txId = manager.begin('owner1');

      txId.then(id => {
        manager.recordOperation(id, {
          type: 'vector',
          action: 'add',
          resourceId: 'resource1',
          timestamp: Date.now(),
        });

        const tx = manager.getTransaction(id);
        expect(tx?.operations.length).toBe(1);
        expect(tx?.operations[0].type).toBe('vector');
      });
    });

    it('should throw error for non-existent transaction', () => {
      expect(() =>
        manager.recordOperation('invalid-tx-id', {
          type: 'vector',
          action: 'add',
          resourceId: 'resource1',
          timestamp: Date.now(),
        })
      ).toThrow('Transaction not found');
    });

    it('should throw error for non-active transaction', async () => {
      const txId = await manager.begin('owner1');
      vi.mocked(mockLockManager.release).mockResolvedValue(true);
      await manager.commit(txId);

      expect(() =>
        manager.recordOperation(txId, {
          type: 'vector',
          action: 'add',
          resourceId: 'resource1',
          timestamp: Date.now(),
        })
      ).toThrow('is not active');
    });
  });

  describe('getTransaction', () => {
    it('should return transaction info', async () => {
      const txId = await manager.begin('owner1');
      const tx = manager.getTransaction(txId);

      expect(tx).toBeDefined();
      expect(tx?.id).toBe(txId);
      expect(tx?.state).toBe('active');
    });

    it('should return null for non-existent transaction', () => {
      const tx = manager.getTransaction('invalid-tx-id');
      expect(tx).toBeNull();
    });
  });

  describe('getActiveTransactions', () => {
    it('should return all active transactions', async () => {
      const txId1 = await manager.begin('owner1');
      const txId2 = await manager.begin('owner2');

      const active = manager.getActiveTransactions();

      expect(active.length).toBe(2);
      expect(active.map(t => t.id)).toContain(txId1);
      expect(active.map(t => t.id)).toContain(txId2);
    });

    it('should not include committed transactions', async () => {
      const txId1 = await manager.begin('owner1');
      const txId2 = await manager.begin('owner2');

      vi.mocked(mockLockManager.release).mockResolvedValue(true);
      await manager.commit(txId1);

      const active = manager.getActiveTransactions();

      expect(active.length).toBe(1);
      expect(active[0].id).toBe(txId2);
    });
  });

  describe('canRollback', () => {
    it('should return true when no active Git operations', async () => {
      const txId = await manager.begin('owner1');
      manager.recordOperation(txId, {
        type: 'vector',
        action: 'add',
        resourceId: 'resource1',
        timestamp: Date.now(),
      });

      expect(manager.canRollback(txId)).toBe(true);
    });

    it('should return false when Git operation is active', async () => {
      const txId = await manager.begin('owner1');
      manager.recordOperation(txId, {
        type: 'git',
        action: 'begin',
        resourceId: RESOURCE_TYPES.GIT,
        timestamp: Date.now(),
      });

      expect(manager.canRollback(txId)).toBe(false);
    });

    it('should return false for non-existent transaction', () => {
      expect(manager.canRollback('invalid-tx-id')).toBe(false);
    });
  });

  describe('beginGitTransaction', () => {
    it('should acquire exclusive Git lock', async () => {
      const txId = await manager.begin('owner1');
      vi.mocked(mockLockManager.acquire).mockResolvedValue({
        success: true,
        lockId: 'git-lock',
      });

      const result = await manager.beginGitTransaction(txId);

      expect(result).toBe(true);
      expect(mockLockManager.acquire).toHaveBeenCalledWith(
        RESOURCE_TYPES.GIT,
        'exclusive',
        txId,
        3000
      );

      const tx = manager.getTransaction(txId);
      expect(tx?.operations.some(op => op.type === 'git' && op.action === 'begin')).toBe(true);
    });

    it('should return false when lock acquisition fails', async () => {
      const txId = await manager.begin('owner1');
      vi.mocked(mockLockManager.acquire).mockResolvedValue({
        success: false,
      });

      const result = await manager.beginGitTransaction(txId);

      expect(result).toBe(false);
    });
  });

  describe('endGitTransaction', () => {
    it('should record successful Git commit', async () => {
      const txId = await manager.begin('owner1');
      await manager.endGitTransaction(txId, true);

      const tx = manager.getTransaction(txId);
      const gitOp = tx?.operations.find(op => op.type === 'git' && op.action === 'commit');

      expect(gitOp).toBeDefined();
      expect(gitOp?.data?.success).toBe(true);
    });

    it('should record failed Git rollback', async () => {
      const txId = await manager.begin('owner1');
      await manager.endGitTransaction(txId, false);

      const tx = manager.getTransaction(txId);
      const gitOp = tx?.operations.find(op => op.type === 'git' && op.action === 'rollback');

      expect(gitOp).toBeDefined();
      expect(gitOp?.data?.success).toBe(false);
    });
  });

  describe('transaction timeout', () => {
    it('should handle transaction timeout', async () => {
      const shortTimeoutManager = new TransactionManager(mockLockManager, {
        transactionTimeout: 100,
        autoRollbackOnError: true,
        lockTimeout: 3000,
      });

      const txId = await shortTimeoutManager.begin('owner1');
      vi.mocked(mockLockManager.release).mockResolvedValue(true);

      // Wait for timeout and rollback to complete
      await new Promise(resolve => setTimeout(resolve, 200));

      const tx = shortTimeoutManager.getTransaction(txId);
      // After timeout, transaction is first set to 'failed', then rollback() changes it to 'rolledback'
      expect(tx?.state).toBe('rolledback');
    });
  });
});
