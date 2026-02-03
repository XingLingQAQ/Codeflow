import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  DispatchAgent,
  TaskClassifier,
  ModelSelector,
  AgentSelector,
  SpecSelector,
  ClassificationResult,
  RoutingDecision,
} from '../DispatchAgent.js';

describe('TaskClassifier', () => {
  let classifier: TaskClassifier;

  beforeEach(() => {
    classifier = new TaskClassifier();
  });

  describe('classify', () => {
    it('should classify frontend tasks', () => {
      const result = classifier.classify('Create a React component for user profile');

      expect(result.taskType).toBe('frontend');
      expect(result.keywords).toContain('react');
      expect(result.keywords).toContain('component');
    });

    it('should classify backend tasks', () => {
      const result = classifier.classify('Implement a REST API endpoint for user authentication');

      expect(result.taskType).toBe('api');
      expect(result.keywords).toContain('api');
      expect(result.keywords).toContain('endpoint');
    });

    it('should classify testing tasks', () => {
      const result = classifier.classify('Write unit tests for the login service');

      expect(result.taskType).toBe('testing');
      expect(result.keywords).toContain('test');
      expect(result.keywords).toContain('unit');
    });

    it('should classify bugfix tasks', () => {
      const result = classifier.classify('Fix the bug where login fails with special characters');

      expect(result.taskType).toBe('bugfix');
      expect(result.keywords).toContain('bug');
      expect(result.keywords).toContain('fix');
    });

    it('should classify documentation tasks', () => {
      const result = classifier.classify('Update the README with installation instructions');

      expect(result.taskType).toBe('documentation');
      expect(result.keywords).toContain('readme');
    });

    it('should return unknown for unclassifiable input', () => {
      const result = classifier.classify('Hello world');

      expect(result.taskType).toBe('unknown');
      expect(result.confidence).toBe(0);
    });

    it('should emit classification:complete event', () => {
      const listener = vi.fn();
      classifier.on('classification:complete', listener);

      classifier.classify('Create a React component');

      expect(listener).toHaveBeenCalled();
    });

    it('should assess complexity as simple', () => {
      const result = classifier.classify('Quick fix for typo');

      expect(result.complexity).toBe('simple');
    });

    it('should assess complexity as complex', () => {
      const result = classifier.classify('Implement a comprehensive architecture for the entire system with multiple microservices');

      expect(result.complexity).toBe('complex');
    });

    it('should estimate tokens', () => {
      const input = 'Create a React component';
      const result = classifier.classify(input);

      expect(result.estimatedTokens).toBeGreaterThan(0);
      expect(result.estimatedTokens).toBe(Math.ceil(input.length / 4));
    });
  });
});

describe('ModelSelector', () => {
  let selector: ModelSelector;

  beforeEach(() => {
    selector = new ModelSelector();
  });

  describe('select', () => {
    it('should select low tier model for simple tasks', () => {
      const classification: ClassificationResult = {
        taskType: 'frontend',
        confidence: 0.8,
        keywords: ['react'],
        complexity: 'simple',
        estimatedTokens: 100,
      };

      const result = selector.select(classification);

      expect(result.tier).toBe('low');
      expect(result.modelId).toBeDefined();
    });

    it('should select high tier model for complex tasks', () => {
      const classification: ClassificationResult = {
        taskType: 'refactoring',
        confidence: 0.9,
        keywords: ['refactor'],
        complexity: 'complex',
        estimatedTokens: 500,
      };

      const result = selector.select(classification);

      expect(result.tier).toBe('high');
    });

    it('should select medium tier for research tasks', () => {
      const classification: ClassificationResult = {
        taskType: 'research',
        confidence: 0.7,
        keywords: ['research'],
        complexity: 'moderate',
        estimatedTokens: 300,
      };

      const result = selector.select(classification);

      expect(result.tier).toBe('medium');
    });

    it('should emit model:selected event', () => {
      const listener = vi.fn();
      selector.on('model:selected', listener);

      selector.select({
        taskType: 'frontend',
        confidence: 0.8,
        keywords: [],
        complexity: 'simple',
        estimatedTokens: 100,
      });

      expect(listener).toHaveBeenCalled();
    });

    it('should include reason in result', () => {
      const result = selector.select({
        taskType: 'frontend',
        confidence: 0.8,
        keywords: [],
        complexity: 'simple',
        estimatedTokens: 100,
      });

      expect(result.reason).toContain('Task type: frontend');
      expect(result.reason).toContain('Complexity: simple');
    });

    it('should include fallback model', () => {
      const result = selector.select({
        taskType: 'frontend',
        confidence: 0.8,
        keywords: [],
        complexity: 'simple',
        estimatedTokens: 100,
      });

      expect(result.fallback).toBeDefined();
    });
  });
});

describe('AgentSelector', () => {
  let selector: AgentSelector;

  beforeEach(() => {
    selector = new AgentSelector();
  });

  describe('select', () => {
    it('should select coder agent for frontend tasks', () => {
      const result = selector.select({
        taskType: 'frontend',
        confidence: 0.8,
        keywords: [],
        complexity: 'moderate',
        estimatedTokens: 100,
      });

      expect(result.agentType).toBe('coder');
    });

    it('should select tester agent for testing tasks', () => {
      const result = selector.select({
        taskType: 'testing',
        confidence: 0.8,
        keywords: [],
        complexity: 'moderate',
        estimatedTokens: 100,
      });

      expect(result.agentType).toBe('tester');
    });

    it('should select researcher agent for research tasks', () => {
      const result = selector.select({
        taskType: 'research',
        confidence: 0.8,
        keywords: [],
        complexity: 'moderate',
        estimatedTokens: 100,
      });

      expect(result.agentType).toBe('researcher');
    });

    it('should select main agent for unknown tasks', () => {
      const result = selector.select({
        taskType: 'unknown',
        confidence: 0,
        keywords: [],
        complexity: 'moderate',
        estimatedTokens: 100,
      });

      expect(result.agentType).toBe('main');
    });

    it('should emit agent:selected event', () => {
      const listener = vi.fn();
      selector.on('agent:selected', listener);

      selector.select({
        taskType: 'frontend',
        confidence: 0.8,
        keywords: [],
        complexity: 'moderate',
        estimatedTokens: 100,
      });

      expect(listener).toHaveBeenCalled();
    });

    it('should include capabilities', () => {
      const result = selector.select({
        taskType: 'frontend',
        confidence: 0.8,
        keywords: [],
        complexity: 'moderate',
        estimatedTokens: 100,
      });

      expect(result.capabilities).toContain('code-generation');
    });
  });
});

describe('SpecSelector', () => {
  let selector: SpecSelector;

  beforeEach(() => {
    selector = new SpecSelector();
  });

  describe('select', () => {
    it('should select frontend specs for frontend tasks', () => {
      const result = selector.select({
        taskType: 'frontend',
        confidence: 0.8,
        keywords: [],
        complexity: 'moderate',
        estimatedTokens: 100,
      });

      expect(result.domains).toContain('frontend');
      expect(result.domains).toContain('common');
    });

    it('should select backend specs for backend tasks', () => {
      const result = selector.select({
        taskType: 'backend',
        confidence: 0.8,
        keywords: [],
        complexity: 'moderate',
        estimatedTokens: 100,
      });

      expect(result.domains).toContain('backend');
    });

    it('should select both frontend and backend for fullstack', () => {
      const result = selector.select({
        taskType: 'fullstack',
        confidence: 0.8,
        keywords: [],
        complexity: 'moderate',
        estimatedTokens: 100,
      });

      expect(result.domains).toContain('frontend');
      expect(result.domains).toContain('backend');
    });

    it('should emit spec:selected event', () => {
      const listener = vi.fn();
      selector.on('spec:selected', listener);

      selector.select({
        taskType: 'frontend',
        confidence: 0.8,
        keywords: [],
        complexity: 'moderate',
        estimatedTokens: 100,
      });

      expect(listener).toHaveBeenCalled();
    });

    it('should set high priority for complex tasks', () => {
      const result = selector.select({
        taskType: 'frontend',
        confidence: 0.8,
        keywords: [],
        complexity: 'complex',
        estimatedTokens: 100,
      });

      expect(result.priority).toBe('high');
    });

    it('should set low priority for simple tasks', () => {
      const result = selector.select({
        taskType: 'frontend',
        confidence: 0.8,
        keywords: [],
        complexity: 'simple',
        estimatedTokens: 100,
      });

      expect(result.priority).toBe('low');
    });

    it('should estimate tokens', () => {
      const result = selector.select({
        taskType: 'frontend',
        confidence: 0.8,
        keywords: [],
        complexity: 'moderate',
        estimatedTokens: 100,
      });

      expect(result.estimatedTokens).toBeGreaterThan(0);
    });
  });
});

describe('DispatchAgent', () => {
  let agent: DispatchAgent;

  beforeEach(() => {
    agent = new DispatchAgent();
  });

  describe('route', () => {
    it('should route a request', async () => {
      const decision = await agent.route('Create a React component for user profile');

      expect(decision.id).toBeDefined();
      expect(decision.classification).toBeDefined();
      expect(decision.agent).toBeDefined();
      expect(decision.model).toBeDefined();
      expect(decision.specs).toBeDefined();
      expect(decision.latency).toBeDefined();
    });

    it('should emit route:start event', async () => {
      const listener = vi.fn();
      agent.on('route:start', listener);

      await agent.route('Create a React component');

      expect(listener).toHaveBeenCalled();
    });

    it('should emit route:complete event', async () => {
      const listener = vi.fn();
      agent.on('route:complete', listener);

      await agent.route('Create a React component');

      expect(listener).toHaveBeenCalled();
    });

    it('should use cache for repeated requests', async () => {
      const input = 'Create a React component';

      const decision1 = await agent.route(input);
      const decision2 = await agent.route(input);

      expect(decision1.id).toBe(decision2.id);
    });

    it('should emit route:cached for cached requests', async () => {
      const listener = vi.fn();
      agent.on('route:cached', listener);

      const input = 'Create a React component';
      await agent.route(input);
      await agent.route(input);

      expect(listener).toHaveBeenCalled();
    });

    it('should record decision history', async () => {
      await agent.route('Create a React component');
      await agent.route('Fix a bug in login');

      const history = agent.getDecisionHistory();
      expect(history.length).toBe(2);
    });

    it('should emit route:slow for slow routing', async () => {
      // Use a very small maxLatency that will always be exceeded
      const slowAgent = new DispatchAgent({ maxLatency: -1 });
      const listener = vi.fn();
      slowAgent.on('route:slow', listener);

      await slowAgent.route('Create a React component');

      expect(listener).toHaveBeenCalled();
    });
  });

  describe('clearCache', () => {
    it('should clear cache', async () => {
      const input = 'Create a React component';
      await agent.route(input);

      agent.clearCache();

      const listener = vi.fn();
      agent.on('route:cached', listener);
      await agent.route(input);

      expect(listener).not.toHaveBeenCalled();
    });

    it('should emit cache:cleared event', () => {
      const listener = vi.fn();
      agent.on('cache:cleared', listener);

      agent.clearCache();

      expect(listener).toHaveBeenCalled();
    });
  });

  describe('getStatistics', () => {
    it('should return statistics', async () => {
      await agent.route('Create a React component');
      await agent.route('Fix a bug');

      const stats = agent.getStatistics();

      expect(stats.totalDecisions).toBe(2);
      expect(stats.averageLatency).toBeGreaterThanOrEqual(0);
      expect(stats.byTaskType).toBeDefined();
      expect(stats.byAgent).toBeDefined();
    });

    it('should return empty statistics when no decisions', () => {
      const stats = agent.getStatistics();

      expect(stats.totalDecisions).toBe(0);
      expect(stats.averageLatency).toBe(0);
    });
  });

  describe('configuration', () => {
    it('should update config', () => {
      agent.updateConfig({ maxLatency: 2000 });
      const config = agent.getConfig();

      expect(config.maxLatency).toBe(2000);
    });

    it('should emit config:updated event', () => {
      const listener = vi.fn();
      agent.on('config:updated', listener);

      agent.updateConfig({ maxLatency: 2000 });

      expect(listener).toHaveBeenCalled();
    });

    it('should get config', () => {
      const config = agent.getConfig();

      expect(config.defaultModel).toBeDefined();
      expect(config.maxLatency).toBeDefined();
      expect(config.enableCaching).toBeDefined();
    });
  });

  describe('reset', () => {
    it('should reset agent', async () => {
      await agent.route('Create a React component');

      agent.reset();

      expect(agent.getDecisionHistory()).toHaveLength(0);
    });

    it('should emit dispatch:reset event', () => {
      const listener = vi.fn();
      agent.on('dispatch:reset', listener);

      agent.reset();

      expect(listener).toHaveBeenCalled();
    });
  });

  describe('disable caching', () => {
    it('should not cache when disabled', async () => {
      const noCacheAgent = new DispatchAgent({ enableCaching: false });
      const listener = vi.fn();
      noCacheAgent.on('route:cached', listener);

      const input = 'Create a React component';
      await noCacheAgent.route(input);
      await noCacheAgent.route(input);

      expect(listener).not.toHaveBeenCalled();
    });
  });
});
