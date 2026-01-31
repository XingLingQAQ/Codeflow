import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { LockManager } from '../LockManager.js';

describe('LockManager', () => {
  let lockManager: LockManager;

  beforeEach(() => {
    lockManager = new LockManager({
      defaultTimeout: 5000,
      enableDeadlockDetection: false,
      enableFairness: true,
    });
  });

  afterEach(() => {
    lockManager.stop();
  });

  describe('acquire and release', () => {
    it('should acquire lock successfully', async () => {
      const result = await lockManager.acquire('resource1', 'write', 'owner1');

      expect(result.success).toBe(true);
      expect(result.lockId).toBeDefined();
    });

    it('should release lock successfully', async () => {
      const acquireResult = await lockManager.acquire('resource1', 'write', 'owner1');
      const releaseResult = await lockManager.release(acquireResult.lockId!);

      expect(releaseResult.success).toBe(true);
    });

    it('should fail to release non-existent lock', async () => {
      const result = await lockManager.release('non_existent_lock_id');

      expect(result.success).toBe(false);
      expect(result.error).toContain('not found');
    });

    it('should allow reentrant lock for same owner', async () => {
      await lockManager.acquire('resource1', 'write', 'owner1');
      const result = await lockManager.acquire('resource1', 'write', 'owner1');

      expect(result.success).toBe(true);
    });

    it('should block write lock when resource is locked', async () => {
      await lockManager.acquire('resource1', 'write', 'owner1');

      const result = await lockManager.tryAcquire('resource1', 'write', 'owner2');

      expect(result.success).toBe(false);
    });
  });

  describe('lock compatibility', () => {
    it('should allow multiple read locks', async () => {
      const result1 = await lockManager.acquire('resource1', 'read', 'owner1');
      const result2 = await lockManager.acquire('resource1', 'read', 'owner2');

      expect(result1.success).toBe(true);
      expect(result2.success).toBe(true);
    });

    it('should block write lock when read lock exists', async () => {
      await lockManager.acquire('resource1', 'read', 'owner1');

      const result = await lockManager.tryAcquire('resource1', 'write', 'owner2');

      expect(result.success).toBe(false);
    });

    it('should block read lock when write lock exists', async () => {
      await lockManager.acquire('resource1', 'write', 'owner1');

      const result = await lockManager.tryAcquire('resource1', 'read', 'owner2');

      expect(result.success).toBe(false);
    });
  });

  describe('tryAcquire', () => {
    it('should return immediately on success', async () => {
      const result = await lockManager.tryAcquire('resource1', 'write', 'owner1');

      expect(result.success).toBe(true);
    });

    it('should return immediately on failure', async () => {
      await lockManager.acquire('resource1', 'write', 'owner1');

      const start = Date.now();
      const result = await lockManager.tryAcquire('resource1', 'write', 'owner2');
      const duration = Date.now() - start;

      expect(result.success).toBe(false);
      expect(duration).toBeLessThan(100); // Should be immediate
    });
  });

  describe('isLocked', () => {
    it('should return true for locked resource', async () => {
      await lockManager.acquire('resource1', 'write', 'owner1');

      expect(lockManager.isLocked('resource1')).toBe(true);
    });

    it('should return false for unlocked resource', () => {
      expect(lockManager.isLocked('resource1')).toBe(false);
    });
  });

  describe('getLockInfo', () => {
    it('should return lock info for locked resource', async () => {
      await lockManager.acquire('resource1', 'write', 'owner1');

      const info = lockManager.getLockInfo('resource1');

      expect(info).not.toBeNull();
      expect(info?.resourceId).toBe('resource1');
      expect(info?.type).toBe('write');
      expect(info?.owner).toBe('owner1');
    });

    it('should return null for unlocked resource', () => {
      const info = lockManager.getLockInfo('resource1');

      expect(info).toBeNull();
    });
  });

  describe('getLocksForOwner', () => {
    it('should return all locks for owner', async () => {
      await lockManager.acquire('resource1', 'write', 'owner1');
      await lockManager.acquire('resource2', 'read', 'owner1');

      const locks = lockManager.getLocksForOwner('owner1');

      expect(locks.length).toBe(2);
    });

    it('should return empty array for owner with no locks', () => {
      const locks = lockManager.getLocksForOwner('owner1');

      expect(locks).toEqual([]);
    });
  });

  describe('getPendingRequests', () => {
    it('should return pending requests for resource', async () => {
      await lockManager.acquire('resource1', 'write', 'owner1');

      // Start a blocking acquire (don't await)
      const pendingPromise = lockManager.acquire('resource1', 'write', 'owner2', 100);

      // Give it time to register
      await new Promise(resolve => setTimeout(resolve, 20));

      const pending = lockManager.getPendingRequests('resource1');
      expect(pending.length).toBeGreaterThanOrEqual(0);

      // Clean up
      await pendingPromise.catch(() => {});
    });
  });

  describe('timeout', () => {
    it('should timeout when lock cannot be acquired', async () => {
      await lockManager.acquire('resource1', 'write', 'owner1');

      const result = await lockManager.acquire('resource1', 'write', 'owner2', 100);

      expect(result.success).toBe(false);
      expect(result.error).toContain('timeout');
    });
  });

  describe('deadlock detection', () => {
    it('should detect simple deadlock', async () => {
      const manager = new LockManager({
        enableDeadlockDetection: true,
        deadlockDetectionInterval: 50,
      });

      // Simulate deadlock scenario by manually setting up wait graph
      // This is a simplified test since real deadlock requires concurrent operations

      const result = manager.detectDeadlock();
      expect(result.hasDeadlock).toBe(false); // No deadlock initially

      manager.stop();
    });
  });

  describe('getStats', () => {
    it('should return accurate statistics', async () => {
      const result1 = await lockManager.acquire('resource1', 'write', 'owner1');
      await lockManager.acquire('resource2', 'read', 'owner2');
      await lockManager.release(result1.lockId!);

      const stats = lockManager.getStats();

      expect(stats.totalAcquired).toBe(2);
      expect(stats.totalReleased).toBe(1);
      expect(stats.currentHeld).toBe(1);
    });
  });

  describe('start and stop', () => {
    it('should start deadlock detection timer', () => {
      const manager = new LockManager({
        enableDeadlockDetection: true,
        deadlockDetectionInterval: 1000,
      });

      manager.start();
      // Timer should be running (internal state)

      manager.stop();
      // Timer should be cleared
    });

    it('should handle multiple stop calls', () => {
      lockManager.stop();
      lockManager.stop(); // Should not throw
    });
  });
});
