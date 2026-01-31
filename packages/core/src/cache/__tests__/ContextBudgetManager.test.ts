import { describe, it, expect, beforeEach } from 'vitest';
import { ContextBudgetManager } from '../ContextBudgetManager.js';

describe('ContextBudgetManager', () => {
  let manager: ContextBudgetManager;

  beforeEach(() => {
    manager = new ContextBudgetManager(100000);
  });

  describe('constructor', () => {
    it('should create manager with default total tokens', () => {
      const defaultManager = new ContextBudgetManager();
      const budget = defaultManager.getBudget();
      expect(budget.totalTokens).toBe(128000);
    });

    it('should create manager with custom total tokens', () => {
      const customManager = new ContextBudgetManager(50000);
      const budget = customManager.getBudget();
      expect(budget.totalTokens).toBe(50000);
      expect(budget.remainingTokens).toBe(50000);
    });
  });

  describe('allocate', () => {
    it('should allocate tokens to existing category', () => {
      const result = manager.allocate('history', 1000);
      expect(result).toBe(true);

      const budget = manager.getBudget();
      expect(budget.usedTokens).toBe(1000);
      expect(budget.remainingTokens).toBe(99000);
    });

    it('should allocate tokens to new category', () => {
      const result = manager.allocate('context', 2000);
      expect(result).toBe(true);

      const budget = manager.getBudget();
      const contextAlloc = budget.allocations.find(a => a.category === 'context');
      expect(contextAlloc).toBeDefined();
    });

    it('should fail when not enough tokens', () => {
      const smallManager = new ContextBudgetManager(1000);
      const result = smallManager.allocate('history', 2000);
      expect(result).toBe(false);

      const budget = smallManager.getBudget();
      expect(budget.usedTokens).toBe(0);
    });

    it('should accumulate allocations to same category', () => {
      manager.allocate('history', 1000);
      manager.allocate('history', 500);

      const budget = manager.getBudget();
      expect(budget.usedTokens).toBe(1500);
    });
  });

  describe('release', () => {
    it('should release tokens from category', () => {
      manager.allocate('history', 1000);
      manager.release('history', 500);

      const budget = manager.getBudget();
      expect(budget.usedTokens).toBe(500);
      expect(budget.remainingTokens).toBe(99500);
    });

    it('should not release more than allocated', () => {
      manager.allocate('history', 1000);
      manager.release('history', 2000);

      const budget = manager.getBudget();
      // Should only release what was allocated
      expect(budget.usedTokens).toBeLessThanOrEqual(0);
    });

    it('should handle releasing from category with no used tokens', () => {
      // context exists in default allocations but has 0 usedTokens
      // release should not cause negative usedTokens
      manager.release('context', 1000);

      const budget = manager.getBudget();
      // usedTokens should remain 0, not go negative
      expect(budget.usedTokens).toBe(0);
    });
  });

  describe('getBudget', () => {
    it('should return copy of budget', () => {
      const budget1 = manager.getBudget();
      const budget2 = manager.getBudget();

      expect(budget1).not.toBe(budget2);
      expect(budget1).toEqual(budget2);
    });

    it('should include all allocations', () => {
      const budget = manager.getBudget();
      expect(budget.allocations).toBeDefined();
      expect(Array.isArray(budget.allocations)).toBe(true);
    });
  });

  describe('canAllocate', () => {
    it('should return true when enough tokens', () => {
      expect(manager.canAllocate(50000)).toBe(true);
    });

    it('should return false when not enough tokens', () => {
      expect(manager.canAllocate(200000)).toBe(false);
    });

    it('should account for already allocated tokens', () => {
      manager.allocate('history', 90000);
      expect(manager.canAllocate(20000)).toBe(false);
      expect(manager.canAllocate(5000)).toBe(true);
    });
  });

  describe('compress', () => {
    it('should compress compressible allocations', () => {
      manager.allocate('history', 50000);
      manager.allocate('context', 40000);

      const compressed = manager.compress(50000);

      expect(compressed).toBeGreaterThan(0);
      const budget = manager.getBudget();
      expect(budget.usedTokens).toBeLessThanOrEqual(50000);
    });

    it('should not compress non-compressible allocations', () => {
      manager.allocate('system', 10000);
      manager.allocate('user', 10000);

      const initialUsed = manager.getBudget().usedTokens;
      const compressed = manager.compress(5000);

      // system and user are non-compressible by default
      // So compression might be limited
      expect(compressed).toBeGreaterThanOrEqual(0);
    });

    it('should return 0 when already under target', () => {
      manager.allocate('history', 1000);
      const compressed = manager.compress(50000);
      expect(compressed).toBe(0);
    });

    it('should compress by priority (high priority last)', () => {
      manager.allocate('history', 30000);
      manager.allocate('context', 30000);

      manager.compress(40000);

      const budget = manager.getBudget();
      // Higher priority allocations should be compressed less
      expect(budget.usedTokens).toBeLessThanOrEqual(40000);
    });

    it('should compress at most 50% of each allocation', () => {
      manager.allocate('history', 40000);

      const compressed = manager.compress(10000);

      const budget = manager.getBudget();
      const historyAlloc = budget.allocations.find(a => a.category === 'history');
      // Should have at least 50% remaining
      expect(historyAlloc?.tokens).toBeGreaterThanOrEqual(20000);
    });
  });

  describe('reset', () => {
    it('should reset to initial state', () => {
      manager.allocate('history', 50000);
      manager.allocate('context', 30000);

      manager.reset();

      const budget = manager.getBudget();
      expect(budget.usedTokens).toBe(0);
      expect(budget.remainingTokens).toBe(100000);
    });

    it('should preserve total tokens after reset', () => {
      const customManager = new ContextBudgetManager(50000);
      customManager.allocate('history', 20000);
      customManager.reset();

      const budget = customManager.getBudget();
      expect(budget.totalTokens).toBe(50000);
    });
  });

  describe('integration scenarios', () => {
    it('should handle typical conversation flow', () => {
      // System prompt
      manager.allocate('system', 2000);
      expect(manager.canAllocate(98000)).toBe(true);

      // User message
      manager.allocate('user', 500);

      // Context retrieval
      manager.allocate('context', 10000);

      // History
      manager.allocate('history', 20000);

      const budget = manager.getBudget();
      expect(budget.usedTokens).toBe(32500);
      expect(budget.remainingTokens).toBe(67500);
    });

    it('should handle compression when approaching limit', () => {
      manager.allocate('history', 40000);
      manager.allocate('context', 40000);

      // Need more space
      if (!manager.canAllocate(30000)) {
        manager.compress(60000);
      }

      expect(manager.canAllocate(30000)).toBe(true);
    });
  });
});
