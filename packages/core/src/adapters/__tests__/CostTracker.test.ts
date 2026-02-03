import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import {
  CostTracker,
  TokenUsage,
  CostEntry,
  SessionCostSummary,
  ModelPricing,
} from '../CostTracker.js';

describe('CostTracker', () => {
  let tracker: CostTracker;

  beforeEach(() => {
    tracker = new CostTracker({ enableRealTimeUpdates: false });
  });

  afterEach(() => {
    tracker.reset();
  });

  describe('startSession', () => {
    it('should start a session', () => {
      tracker.startSession('session-1');

      expect(tracker.getSessionEntries()).toHaveLength(0);
    });

    it('should emit session:started event', () => {
      const listener = vi.fn();
      tracker.on('session:started', listener);

      tracker.startSession('session-1');

      expect(listener).toHaveBeenCalledWith(
        expect.objectContaining({
          sessionId: 'session-1',
        })
      );
    });
  });

  describe('endSession', () => {
    beforeEach(() => {
      tracker.startSession('session-1');
    });

    it('should return session summary', () => {
      tracker.recordUsage('claude-3-haiku', { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 });

      const summary = tracker.endSession();

      expect(summary.sessionId).toBe('session-1');
      expect(summary.totalCost).toBeGreaterThan(0);
      expect(summary.requestCount).toBe(1);
    });

    it('should emit session:ended event', () => {
      const listener = vi.fn();
      tracker.on('session:ended', listener);

      tracker.endSession();

      expect(listener).toHaveBeenCalled();
    });

    it('should include duration', () => {
      const summary = tracker.endSession();

      expect(summary.duration).toBeGreaterThanOrEqual(0);
    });
  });

  describe('recordUsage', () => {
    beforeEach(() => {
      tracker.startSession('session-1');
    });

    it('should record usage', () => {
      const usage: TokenUsage = { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 };
      const entry = tracker.recordUsage('claude-3-haiku', usage);

      expect(entry.modelId).toBe('claude-3-haiku');
      expect(entry.inputTokens).toBe(1000);
      expect(entry.outputTokens).toBe(500);
      expect(entry.totalCost).toBeGreaterThan(0);
    });

    it('should emit cost:recorded event', () => {
      const listener = vi.fn();
      tracker.on('cost:recorded', listener);

      tracker.recordUsage('claude-3-haiku', { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 });

      expect(listener).toHaveBeenCalled();
    });

    it('should emit cost:updated event', () => {
      const listener = vi.fn();
      tracker.on('cost:updated', listener);

      tracker.recordUsage('claude-3-haiku', { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 });

      expect(listener).toHaveBeenCalled();
    });

    it('should include request type', () => {
      const entry = tracker.recordUsage(
        'claude-3-haiku',
        { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 },
        'coding'
      );

      expect(entry.requestType).toBe('coding');
    });

    it('should include metadata', () => {
      const entry = tracker.recordUsage(
        'claude-3-haiku',
        { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 },
        'coding',
        { taskId: 'task-1' }
      );

      expect(entry.metadata?.taskId).toBe('task-1');
    });

    it('should calculate cost correctly', () => {
      // claude-3-haiku: input $0.00025/1k, output $0.00125/1k
      const entry = tracker.recordUsage('claude-3-haiku', {
        inputTokens: 1000,
        outputTokens: 1000,
        totalTokens: 2000,
      });

      expect(entry.inputCost).toBeCloseTo(0.00025, 5);
      expect(entry.outputCost).toBeCloseTo(0.00125, 5);
      expect(entry.totalCost).toBeCloseTo(0.0015, 5);
    });
  });

  describe('getRealTimeStatus', () => {
    beforeEach(() => {
      tracker.startSession('session-1');
    });

    it('should return real-time status', () => {
      tracker.recordUsage('claude-3-haiku', { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 });

      const status = tracker.getRealTimeStatus();

      expect(status.currentSessionCost).toBeGreaterThan(0);
      expect(status.currentPeriodCost).toBeGreaterThan(0);
      expect(status.budgetRemaining).toBeDefined();
      expect(status.budgetPercentUsed).toBeDefined();
    });

    it('should indicate over budget', () => {
      const lowBudgetTracker = new CostTracker({
        periodBudget: 0.0001,
        enableRealTimeUpdates: false,
      });
      lowBudgetTracker.startSession('session-1');
      lowBudgetTracker.recordUsage('claude-3-opus', { inputTokens: 10000, outputTokens: 5000, totalTokens: 15000 });

      const status = lowBudgetTracker.getRealTimeStatus();

      expect(status.isOverBudget).toBe(true);
    });
  });

  describe('getCurrentSessionCost', () => {
    beforeEach(() => {
      tracker.startSession('session-1');
    });

    it('should return current session cost', () => {
      tracker.recordUsage('claude-3-haiku', { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 });
      tracker.recordUsage('claude-3-5-sonnet', { inputTokens: 2000, outputTokens: 1000, totalTokens: 3000 });

      const cost = tracker.getCurrentSessionCost();

      expect(cost).toBeGreaterThan(0);
    });

    it('should return 0 for empty session', () => {
      const cost = tracker.getCurrentSessionCost();

      expect(cost).toBe(0);
    });
  });

  describe('getCostByModel', () => {
    beforeEach(() => {
      tracker.startSession('session-1');
      tracker.recordUsage('claude-3-haiku', { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 });
      tracker.recordUsage('claude-3-haiku', { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 });
      tracker.recordUsage('claude-3-5-sonnet', { inputTokens: 2000, outputTokens: 1000, totalTokens: 3000 });
    });

    it('should group costs by model', () => {
      const byModel = tracker.getCostByModel();

      expect(byModel['claude-3-haiku']).toBeDefined();
      expect(byModel['claude-3-5-sonnet']).toBeDefined();
    });

    it('should include request count', () => {
      const byModel = tracker.getCostByModel();

      expect(byModel['claude-3-haiku'].requestCount).toBe(2);
      expect(byModel['claude-3-5-sonnet'].requestCount).toBe(1);
    });

    it('should calculate percentage', () => {
      const byModel = tracker.getCostByModel();
      const totalPercentage = Object.values(byModel).reduce((sum, m) => sum + m.percentage, 0);

      expect(totalPercentage).toBeCloseTo(100, 1);
    });
  });

  describe('getCostByRequestType', () => {
    beforeEach(() => {
      tracker.startSession('session-1');
      tracker.recordUsage('claude-3-haiku', { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 }, 'coding');
      tracker.recordUsage('claude-3-haiku', { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 }, 'coding');
      tracker.recordUsage('claude-3-5-sonnet', { inputTokens: 2000, outputTokens: 1000, totalTokens: 3000 }, 'research');
    });

    it('should group costs by request type', () => {
      const byType = tracker.getCostByRequestType();

      expect(byType['coding']).toBeDefined();
      expect(byType['research']).toBeDefined();
    });

    it('should handle unknown type', () => {
      tracker.recordUsage('claude-3-haiku', { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 });

      const byType = tracker.getCostByRequestType();

      expect(byType['unknown']).toBeDefined();
    });
  });

  describe('alerts', () => {
    it('should emit warning alert', () => {
      // claude-3-haiku: input $0.00025/1k, output $0.00125/1k
      // 1000 input + 500 output = $0.00025 + $0.000625 = $0.000875
      // sessionBudget = 0.002, warningThreshold = 0.5 => warning at $0.001
      // So $0.000875 is ~43.75% which is below 50% warning threshold
      // Need to use more tokens to trigger warning but not exceed budget
      const alertTracker = new CostTracker({
        sessionBudget: 0.0015, // $0.0015 budget
        warningThreshold: 0.5, // warning at 50% = $0.00075
        enableAlerts: true,
        enableRealTimeUpdates: false,
      });

      const listener = vi.fn();
      alertTracker.on('alert', listener);

      alertTracker.startSession('session-1');
      // 1000 input + 500 output = $0.00025 + $0.000625 = $0.000875 (58% of budget, triggers warning)
      alertTracker.recordUsage('claude-3-haiku', { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 });

      expect(listener).toHaveBeenCalled();
      expect(listener.mock.calls[0][0].type).toBe('warning');
    });

    it('should emit critical alert', () => {
      // sessionBudget = 0.001, criticalThreshold = 0.5 => critical at $0.0005
      // 1000 input + 500 output = $0.000875 (87.5% of budget, triggers critical)
      const alertTracker = new CostTracker({
        sessionBudget: 0.001,
        warningThreshold: 0.3, // warning at 30%
        criticalThreshold: 0.5, // critical at 50%
        enableAlerts: true,
        enableRealTimeUpdates: false,
      });

      const listener = vi.fn();
      alertTracker.on('alert', listener);

      alertTracker.startSession('session-1');
      alertTracker.recordUsage('claude-3-haiku', { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 });

      const criticalAlert = listener.mock.calls.find((call: unknown[]) => (call[0] as { type: string }).type === 'critical');
      expect(criticalAlert).toBeDefined();
    });

    it('should emit exceeded alert', () => {
      const alertTracker = new CostTracker({
        sessionBudget: 0.0001,
        enableAlerts: true,
        enableRealTimeUpdates: false,
      });

      const listener = vi.fn();
      alertTracker.on('alert', listener);

      alertTracker.startSession('session-1');
      alertTracker.recordUsage('claude-3-opus', { inputTokens: 10000, outputTokens: 5000, totalTokens: 15000 });

      const exceededAlert = listener.mock.calls.find((call: unknown[]) => (call[0] as { type: string }).type === 'exceeded');
      expect(exceededAlert).toBeDefined();
    });

    it('should get alerts', () => {
      const alertTracker = new CostTracker({
        sessionBudget: 0.0001,
        enableAlerts: true,
        enableRealTimeUpdates: false,
      });

      alertTracker.startSession('session-1');
      alertTracker.recordUsage('claude-3-opus', { inputTokens: 10000, outputTokens: 5000, totalTokens: 15000 });

      const alerts = alertTracker.getAlerts();
      expect(alerts.length).toBeGreaterThan(0);
    });

    it('should clear alerts', () => {
      const alertTracker = new CostTracker({
        sessionBudget: 0.0001,
        enableAlerts: true,
        enableRealTimeUpdates: false,
      });

      alertTracker.startSession('session-1');
      alertTracker.recordUsage('claude-3-opus', { inputTokens: 10000, outputTokens: 5000, totalTokens: 15000 });
      alertTracker.clearAlerts();

      expect(alertTracker.getAlerts()).toHaveLength(0);
    });
  });

  describe('pricing', () => {
    it('should add pricing', () => {
      const pricing: ModelPricing = {
        modelId: 'custom-model',
        provider: 'custom',
        inputPricePer1k: 0.001,
        outputPricePer1k: 0.002,
      };

      tracker.addPricing(pricing);
      const retrieved = tracker.getPricing('custom-model');

      expect(retrieved).toEqual(pricing);
    });

    it('should emit pricing:added event', () => {
      const listener = vi.fn();
      tracker.on('pricing:added', listener);

      tracker.addPricing({
        modelId: 'custom-model',
        provider: 'custom',
        inputPricePer1k: 0.001,
        outputPricePer1k: 0.002,
      });

      expect(listener).toHaveBeenCalled();
    });

    it('should get all pricing', () => {
      const pricing = tracker.getAllPricing();

      expect(pricing.length).toBeGreaterThan(0);
    });
  });

  describe('formatCost', () => {
    it('should format cost with currency', () => {
      const formatted = tracker.formatCost(0.0015);

      expect(formatted).toContain('USD');
      expect(formatted).toContain('0.0015');
    });

    it('should use configured currency', () => {
      const eurTracker = new CostTracker({ currency: 'EUR', enableRealTimeUpdates: false });
      const formatted = eurTracker.formatCost(0.0015);

      expect(formatted).toContain('EUR');
    });
  });

  describe('configuration', () => {
    it('should update config', () => {
      tracker.updateConfig({ sessionBudget: 50 });
      const config = tracker.getConfig();

      expect(config.sessionBudget).toBe(50);
    });

    it('should emit config:updated event', () => {
      const listener = vi.fn();
      tracker.on('config:updated', listener);

      tracker.updateConfig({ sessionBudget: 50 });

      expect(listener).toHaveBeenCalled();
    });

    it('should get config', () => {
      const config = tracker.getConfig();

      expect(config.sessionBudget).toBeDefined();
      expect(config.periodBudget).toBeDefined();
      expect(config.period).toBeDefined();
    });
  });

  describe('reset', () => {
    it('should reset tracker', () => {
      tracker.startSession('session-1');
      tracker.recordUsage('claude-3-haiku', { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 });

      tracker.reset();

      expect(tracker.getSessionEntries()).toHaveLength(0);
      expect(tracker.getCurrentSessionCost()).toBe(0);
    });

    it('should emit tracker:reset event', () => {
      const listener = vi.fn();
      tracker.on('tracker:reset', listener);

      tracker.reset();

      expect(listener).toHaveBeenCalled();
    });
  });

  describe('session summary', () => {
    beforeEach(() => {
      tracker.startSession('session-1');
      tracker.recordUsage('claude-3-haiku', { inputTokens: 1000, outputTokens: 500, totalTokens: 1500 }, 'coding');
      tracker.recordUsage('claude-3-5-sonnet', { inputTokens: 2000, outputTokens: 1000, totalTokens: 3000 }, 'research');
    });

    it('should include total tokens', () => {
      const summary = tracker.endSession();

      expect(summary.totalInputTokens).toBe(3000);
      expect(summary.totalOutputTokens).toBe(1500);
    });

    it('should include average cost per request', () => {
      const summary = tracker.endSession();

      expect(summary.averageCostPerRequest).toBeGreaterThan(0);
    });

    it('should include peak cost request', () => {
      const summary = tracker.endSession();

      expect(summary.peakCostRequest).not.toBeNull();
      expect(summary.peakCostRequest?.modelId).toBe('claude-3-5-sonnet');
    });

    it('should include by model breakdown', () => {
      const summary = tracker.endSession();

      expect(summary.byModel['claude-3-haiku']).toBeDefined();
      expect(summary.byModel['claude-3-5-sonnet']).toBeDefined();
    });

    it('should include by request type breakdown', () => {
      const summary = tracker.endSession();

      expect(summary.byRequestType['coding']).toBeDefined();
      expect(summary.byRequestType['research']).toBeDefined();
    });
  });
});
