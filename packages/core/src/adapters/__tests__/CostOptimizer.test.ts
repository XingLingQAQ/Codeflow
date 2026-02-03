import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  CostOptimizer,
  TaskTypeRouter,
  UsageTracker,
  CostTaskType,
  RoutingRule,
  ModelCostInfo,
} from '../CostOptimizer.js';

describe('TaskTypeRouter', () => {
  let router: TaskTypeRouter;

  beforeEach(() => {
    router = new TaskTypeRouter();
  });

  describe('route', () => {
    it('should route decision tasks to premium models', () => {
      const result = router.route('decision');

      expect(['claude-opus-4', 'claude-3-opus', 'gpt-4o']).toContain(result.modelId);
      expect(result.reason).toContain('decision');
    });

    it('should route coding tasks to low-cost models', () => {
      const result = router.route('coding');

      expect(['claude-3-haiku', 'gemini-flash', 'gpt-4o-mini']).toContain(result.modelId);
    });

    it('should route research tasks to medium-tier models', () => {
      const result = router.route('research');

      expect(['claude-3-5-sonnet', 'gemini-pro']).toContain(result.modelId);
    });

    it('should route simple tasks to lowest-cost models', () => {
      const result = router.route('simple');

      expect(['claude-3-haiku', 'gemini-flash', 'gpt-4o-mini']).toContain(result.modelId);
    });

    it('should use fallback when preferred not available', () => {
      const result = router.route('decision', ['claude-3-5-sonnet']);

      expect(result.modelId).toBe('claude-3-5-sonnet');
      expect(result.reason).toContain('Fallback');
    });

    it('should emit route:complete event', () => {
      const listener = vi.fn();
      router.on('route:complete', listener);

      router.route('coding');

      expect(listener).toHaveBeenCalled();
    });

    it('should include estimated cost', () => {
      const result = router.route('coding');

      expect(result.estimatedCost).toBeGreaterThanOrEqual(0);
    });

    it('should handle unknown task type', () => {
      const result = router.route('unknown');

      expect(result.modelId).toBeDefined();
      expect(result.rule.taskType).toBe('unknown');
    });
  });

  describe('addRule', () => {
    it('should add a custom rule', () => {
      const rule: RoutingRule = {
        id: 'custom_rule',
        taskType: 'coding',
        preferredModels: ['custom-model'],
        fallbackModels: [],
        priority: 1,
        enabled: true,
      };

      router.addRule(rule);
      const rules = router.getRules();

      expect(rules.find(r => r.id === 'custom_rule')).toBeDefined();
    });

    it('should emit rule:added event', () => {
      const listener = vi.fn();
      router.on('rule:added', listener);

      router.addRule({
        id: 'test',
        taskType: 'simple',
        preferredModels: [],
        fallbackModels: [],
        priority: 1,
        enabled: true,
      });

      expect(listener).toHaveBeenCalled();
    });
  });

  describe('removeRule', () => {
    it('should remove a rule', () => {
      const removed = router.removeRule('coding');

      expect(removed).toBe(true);
    });

    it('should return false for non-existent rule', () => {
      const removed = router.removeRule('nonexistent' as CostTaskType);

      expect(removed).toBe(false);
    });
  });

  describe('model costs', () => {
    it('should add model cost', () => {
      const cost: ModelCostInfo = {
        modelId: 'custom-model',
        provider: 'custom',
        inputCostPer1k: 0.001,
        outputCostPer1k: 0.002,
        tier: 'low',
        capabilities: ['coding'],
      };

      router.addModelCost(cost);
      const retrieved = router.getModelCost('custom-model');

      expect(retrieved).toEqual(cost);
    });

    it('should get all model costs', () => {
      const costs = router.getAllModelCosts();

      expect(costs.length).toBeGreaterThan(0);
    });
  });
});

describe('UsageTracker', () => {
  let tracker: UsageTracker;

  beforeEach(() => {
    tracker = new UsageTracker();
  });

  describe('record', () => {
    it('should record usage', () => {
      const record = tracker.record('claude-3-haiku', 'coding', 1000, 500);

      expect(record.id).toBeDefined();
      expect(record.modelId).toBe('claude-3-haiku');
      expect(record.inputTokens).toBe(1000);
      expect(record.outputTokens).toBe(500);
      expect(record.cost).toBeGreaterThan(0);
    });

    it('should emit usage:recorded event', () => {
      const listener = vi.fn();
      tracker.on('usage:recorded', listener);

      tracker.record('claude-3-haiku', 'coding', 1000, 500);

      expect(listener).toHaveBeenCalled();
    });

    it('should include session ID', () => {
      const record = tracker.record('claude-3-haiku', 'coding', 1000, 500, 'session-1');

      expect(record.sessionId).toBe('session-1');
    });
  });

  describe('getStatistics', () => {
    beforeEach(() => {
      tracker.record('claude-3-haiku', 'coding', 1000, 500);
      tracker.record('claude-3-5-sonnet', 'research', 2000, 1000);
      tracker.record('claude-3-opus', 'decision', 500, 200);
    });

    it('should return total cost', () => {
      const stats = tracker.getStatistics();

      expect(stats.totalCost).toBeGreaterThan(0);
    });

    it('should return total tokens', () => {
      const stats = tracker.getStatistics();

      expect(stats.totalInputTokens).toBe(3500);
      expect(stats.totalOutputTokens).toBe(1700);
    });

    it('should group by model', () => {
      const stats = tracker.getStatistics();

      expect(stats.byModel['claude-3-haiku']).toBeDefined();
      expect(stats.byModel['claude-3-5-sonnet']).toBeDefined();
    });

    it('should group by task type', () => {
      const stats = tracker.getStatistics();

      expect(stats.byTaskType['coding']).toBeDefined();
      expect(stats.byTaskType['research']).toBeDefined();
    });

    it('should calculate average cost', () => {
      const stats = tracker.getStatistics();

      expect(stats.averageCostPerRequest).toBeGreaterThan(0);
    });

    it('should estimate savings', () => {
      const stats = tracker.getStatistics();

      expect(stats.estimatedSavings).toBeGreaterThan(0);
    });

    it('should filter by time range', () => {
      const now = Date.now();
      const stats = tracker.getStatistics(now - 1000, now + 1000);

      expect(stats.totalInputTokens).toBe(3500);
    });
  });

  describe('getRecords', () => {
    it('should return records in reverse order', () => {
      tracker.record('claude-3-haiku', 'coding', 1000, 500);
      tracker.record('claude-3-5-sonnet', 'research', 2000, 1000);

      const records = tracker.getRecords();

      expect(records[0].modelId).toBe('claude-3-5-sonnet');
    });

    it('should limit records', () => {
      tracker.record('claude-3-haiku', 'coding', 1000, 500);
      tracker.record('claude-3-5-sonnet', 'research', 2000, 1000);
      tracker.record('claude-3-opus', 'decision', 500, 200);

      const records = tracker.getRecords(2);

      expect(records.length).toBe(2);
    });
  });

  describe('clearRecords', () => {
    it('should clear all records', () => {
      tracker.record('claude-3-haiku', 'coding', 1000, 500);
      tracker.clearRecords();

      expect(tracker.getRecords().length).toBe(0);
    });

    it('should emit records:cleared event', () => {
      const listener = vi.fn();
      tracker.on('records:cleared', listener);

      tracker.clearRecords();

      expect(listener).toHaveBeenCalled();
    });
  });

  describe('getCurrentPeriodCost', () => {
    it('should return daily cost', () => {
      tracker.record('claude-3-haiku', 'coding', 1000, 500);

      const cost = tracker.getCurrentPeriodCost('daily');

      expect(cost).toBeGreaterThan(0);
    });

    it('should return weekly cost', () => {
      tracker.record('claude-3-haiku', 'coding', 1000, 500);

      const cost = tracker.getCurrentPeriodCost('weekly');

      expect(cost).toBeGreaterThan(0);
    });

    it('should return monthly cost', () => {
      tracker.record('claude-3-haiku', 'coding', 1000, 500);

      const cost = tracker.getCurrentPeriodCost('monthly');

      expect(cost).toBeGreaterThan(0);
    });
  });
});

describe('CostOptimizer', () => {
  let optimizer: CostOptimizer;

  beforeEach(() => {
    optimizer = new CostOptimizer();
  });

  describe('selectModel', () => {
    it('should select model based on task type', () => {
      const result = optimizer.selectModel('coding');

      expect(result.modelId).toBeDefined();
      expect(result.rule).toBeDefined();
    });

    it('should emit route:complete event', () => {
      const listener = vi.fn();
      optimizer.on('route:complete', listener);

      optimizer.selectModel('coding');

      expect(listener).toHaveBeenCalled();
    });

    it('should emit budget:warning when near limit', () => {
      const lowBudgetOptimizer = new CostOptimizer({
        budgetLimit: 0.001,
        alertThreshold: 0.5,
      });

      // Record some usage to approach budget
      lowBudgetOptimizer.recordUsage('claude-3-haiku', 'coding', 10000, 5000);

      const listener = vi.fn();
      lowBudgetOptimizer.on('budget:warning', listener);

      lowBudgetOptimizer.selectModel('coding');

      expect(listener).toHaveBeenCalled();
    });

    it('should use default model when budget exceeded', () => {
      const lowBudgetOptimizer = new CostOptimizer({
        budgetLimit: 0.0001,
        defaultModel: 'claude-3-haiku',
      });

      // Record usage to exceed budget
      lowBudgetOptimizer.recordUsage('claude-3-opus', 'decision', 100000, 50000);

      const result = lowBudgetOptimizer.selectModel('decision');

      expect(result.modelId).toBe('claude-3-haiku');
      expect(result.reason).toContain('Budget exceeded');
    });
  });

  describe('recordUsage', () => {
    it('should record usage', () => {
      const record = optimizer.recordUsage('claude-3-haiku', 'coding', 1000, 500);

      expect(record.modelId).toBe('claude-3-haiku');
    });

    it('should emit usage:recorded event', () => {
      const listener = vi.fn();
      optimizer.on('usage:recorded', listener);

      optimizer.recordUsage('claude-3-haiku', 'coding', 1000, 500);

      expect(listener).toHaveBeenCalled();
    });

    it('should not track when disabled', () => {
      const noTrackOptimizer = new CostOptimizer({ enableTracking: false });

      const record = noTrackOptimizer.recordUsage('claude-3-haiku', 'coding', 1000, 500);

      expect(record.id).toBe('tracking_disabled');
    });
  });

  describe('getStatistics', () => {
    it('should return statistics', () => {
      optimizer.recordUsage('claude-3-haiku', 'coding', 1000, 500);

      const stats = optimizer.getStatistics();

      expect(stats.totalCost).toBeGreaterThan(0);
    });
  });

  describe('getSavingsPercentage', () => {
    it('should return savings percentage', () => {
      optimizer.recordUsage('claude-3-haiku', 'coding', 1000, 500);

      const savings = optimizer.getSavingsPercentage();

      expect(savings).toBeGreaterThan(0);
    });

    it('should return 0 when no usage', () => {
      const savings = optimizer.getSavingsPercentage();

      expect(savings).toBe(0);
    });
  });

  describe('getBudgetStatus', () => {
    it('should return budget status', () => {
      optimizer.recordUsage('claude-3-haiku', 'coding', 1000, 500);

      const status = optimizer.getBudgetStatus();

      expect(status.currentCost).toBeGreaterThan(0);
      expect(status.budgetLimit).toBeDefined();
      expect(status.percentUsed).toBeGreaterThan(0);
      expect(status.remaining).toBeDefined();
    });
  });

  describe('routing rules', () => {
    it('should add routing rule', () => {
      optimizer.addRoutingRule({
        id: 'custom',
        taskType: 'simple',
        preferredModels: ['custom-model'],
        fallbackModels: [],
        priority: 1,
        enabled: true,
      });

      const rules = optimizer.getRoutingRules();
      expect(rules.find(r => r.id === 'custom')).toBeDefined();
    });

    it('should get routing rules', () => {
      const rules = optimizer.getRoutingRules();

      expect(rules.length).toBeGreaterThan(0);
    });
  });

  describe('model costs', () => {
    it('should add model cost', () => {
      optimizer.addModelCost({
        modelId: 'custom-model',
        provider: 'custom',
        inputCostPer1k: 0.001,
        outputCostPer1k: 0.002,
        tier: 'low',
        capabilities: ['coding'],
      });

      const cost = optimizer.getModelCost('custom-model');
      expect(cost).toBeDefined();
    });

    it('should get all model costs', () => {
      const costs = optimizer.getAllModelCosts();

      expect(costs.length).toBeGreaterThan(0);
    });
  });

  describe('usage records', () => {
    it('should get usage records', () => {
      optimizer.recordUsage('claude-3-haiku', 'coding', 1000, 500);

      const records = optimizer.getUsageRecords();

      expect(records.length).toBe(1);
    });

    it('should limit usage records', () => {
      optimizer.recordUsage('claude-3-haiku', 'coding', 1000, 500);
      optimizer.recordUsage('claude-3-5-sonnet', 'research', 2000, 1000);

      const records = optimizer.getUsageRecords(1);

      expect(records.length).toBe(1);
    });
  });

  describe('configuration', () => {
    it('should update config', () => {
      optimizer.updateConfig({ budgetLimit: 200 });
      const config = optimizer.getConfig();

      expect(config.budgetLimit).toBe(200);
    });

    it('should emit config:updated event', () => {
      const listener = vi.fn();
      optimizer.on('config:updated', listener);

      optimizer.updateConfig({ budgetLimit: 200 });

      expect(listener).toHaveBeenCalled();
    });

    it('should get config', () => {
      const config = optimizer.getConfig();

      expect(config.budgetLimit).toBeDefined();
      expect(config.budgetPeriod).toBeDefined();
      expect(config.enableTracking).toBeDefined();
    });
  });

  describe('reset', () => {
    it('should reset optimizer', () => {
      optimizer.recordUsage('claude-3-haiku', 'coding', 1000, 500);
      optimizer.reset();

      expect(optimizer.getUsageRecords().length).toBe(0);
    });

    it('should emit optimizer:reset event', () => {
      const listener = vi.fn();
      optimizer.on('optimizer:reset', listener);

      optimizer.reset();

      expect(listener).toHaveBeenCalled();
    });
  });
});
