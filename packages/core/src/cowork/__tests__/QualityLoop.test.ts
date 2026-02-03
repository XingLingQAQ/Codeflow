import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  QualityLoop,
  CheckAgent,
  FixAgent,
  IterationManager,
  CheckResult,
  FixResult,
  CheckIssue,
  CheckAgentCallback,
  FixAgentCallback,
} from '../QualityLoop.js';

// Mock data
const createMockCheckResult = (passed: boolean, issues: CheckIssue[] = []): CheckResult => ({
  passed,
  issues,
  summary: {
    errors: issues.filter(i => i.severity === 'error').length,
    warnings: issues.filter(i => i.severity === 'warning').length,
    infos: issues.filter(i => i.severity === 'info').length,
    autoFixable: issues.filter(i => i.autoFixable).length,
  },
  duration: 100,
  checkType: 'lint',
});

const createMockIssue = (
  id: string,
  severity: 'error' | 'warning' | 'info' = 'warning',
  autoFixable: boolean = true
): CheckIssue => ({
  id,
  type: 'lint',
  severity,
  message: `Issue ${id}`,
  autoFixable,
});

describe('CheckAgent', () => {
  let agent: CheckAgent;

  beforeEach(() => {
    agent = new CheckAgent();
  });

  describe('check', () => {
    it('should execute check', async () => {
      const result = await agent.check(['src/index.ts'], ['lint']);

      expect(result).toBeDefined();
      expect(result.duration).toBeGreaterThanOrEqual(0);
    });

    it('should emit check events', async () => {
      const startListener = vi.fn();
      const completeListener = vi.fn();
      agent.on('check:start', startListener);
      agent.on('check:complete', completeListener);

      await agent.check(['src/index.ts'], ['lint']);

      expect(startListener).toHaveBeenCalled();
      expect(completeListener).toHaveBeenCalled();
    });

    it('should use custom callback', async () => {
      const mockResult = createMockCheckResult(true);
      const callback: CheckAgentCallback = vi.fn().mockResolvedValue(mockResult);
      const customAgent = new CheckAgent(callback);

      const result = await customAgent.check(['src/index.ts'], ['lint']);

      expect(callback).toHaveBeenCalledWith(['src/index.ts'], ['lint']);
      expect(result.passed).toBe(true);
    });

    it('should handle errors', async () => {
      const callback: CheckAgentCallback = vi.fn().mockRejectedValue(new Error('Check failed'));
      const errorAgent = new CheckAgent(callback);

      const errorListener = vi.fn();
      errorAgent.on('check:error', errorListener);

      const result = await errorAgent.check(['src/index.ts'], ['lint']);

      expect(result.passed).toBe(false);
      expect(result.issues[0].message).toContain('Check failed');
      expect(errorListener).toHaveBeenCalled();
    });
  });

  describe('getModelId', () => {
    it('should return model ID', () => {
      const agentWithModel = new CheckAgent(undefined, 'claude-opus-4');
      expect(agentWithModel.getModelId()).toBe('claude-opus-4');
    });
  });
});

describe('FixAgent', () => {
  let agent: FixAgent;

  beforeEach(() => {
    agent = new FixAgent();
  });

  describe('fix', () => {
    it('should execute fix', async () => {
      const issues = [createMockIssue('issue-1')];
      const result = await agent.fix(issues);

      expect(result).toBeDefined();
      expect(result.success).toBe(true);
    });

    it('should emit fix events', async () => {
      const startListener = vi.fn();
      const completeListener = vi.fn();
      agent.on('fix:start', startListener);
      agent.on('fix:complete', completeListener);

      await agent.fix([createMockIssue('issue-1')]);

      expect(startListener).toHaveBeenCalled();
      expect(completeListener).toHaveBeenCalled();
    });

    it('should only fix auto-fixable issues', async () => {
      const issues = [
        createMockIssue('fixable', 'warning', true),
        createMockIssue('not-fixable', 'error', false),
      ];

      const result = await agent.fix(issues);

      expect(result.fixedIssues).toContain('fixable');
      expect(result.fixedIssues).not.toContain('not-fixable');
    });

    it('should return empty result when no fixable issues', async () => {
      const issues = [createMockIssue('not-fixable', 'error', false)];
      const result = await agent.fix(issues);

      expect(result.success).toBe(true);
      expect(result.fixedIssues.length).toBe(0);
    });

    it('should use custom callback', async () => {
      const mockResult: FixResult = {
        success: true,
        fixedIssues: ['issue-1'],
        failedIssues: [],
        filesModified: ['src/index.ts'],
        duration: 50,
      };
      const callback: FixAgentCallback = vi.fn().mockResolvedValue(mockResult);
      const customAgent = new FixAgent(callback);

      const result = await customAgent.fix([createMockIssue('issue-1')]);

      expect(callback).toHaveBeenCalled();
      expect(result.filesModified).toContain('src/index.ts');
    });
  });
});

describe('IterationManager', () => {
  let manager: IterationManager;

  beforeEach(() => {
    manager = new IterationManager(3);
  });

  describe('recordIteration', () => {
    it('should record iteration', () => {
      manager.recordIteration({
        iteration: 1,
        checkResult: createMockCheckResult(true),
        status: 'passed',
      });

      expect(manager.getIterations().length).toBe(1);
    });

    it('should emit iteration:recorded event', () => {
      const listener = vi.fn();
      manager.on('iteration:recorded', listener);

      manager.recordIteration({
        iteration: 1,
        checkResult: createMockCheckResult(true),
        status: 'passed',
      });

      expect(listener).toHaveBeenCalled();
    });
  });

  describe('canContinue', () => {
    it('should return true when under max iterations', () => {
      expect(manager.canContinue()).toBe(true);
    });

    it('should return false when at max iterations', () => {
      for (let i = 0; i < 3; i++) {
        manager.recordIteration({
          iteration: i + 1,
          checkResult: createMockCheckResult(false),
          status: 'failed',
        });
      }

      expect(manager.canContinue()).toBe(false);
    });
  });

  describe('getCurrentIteration', () => {
    it('should return current iteration count', () => {
      expect(manager.getCurrentIteration()).toBe(0);

      manager.recordIteration({
        iteration: 1,
        checkResult: createMockCheckResult(true),
        status: 'passed',
      });

      expect(manager.getCurrentIteration()).toBe(1);
    });
  });

  describe('getLastIteration', () => {
    it('should return last iteration', () => {
      manager.recordIteration({
        iteration: 1,
        checkResult: createMockCheckResult(false),
        status: 'failed',
      });
      manager.recordIteration({
        iteration: 2,
        checkResult: createMockCheckResult(true),
        status: 'passed',
      });

      const last = manager.getLastIteration();
      expect(last?.iteration).toBe(2);
      expect(last?.status).toBe('passed');
    });

    it('should return undefined when no iterations', () => {
      expect(manager.getLastIteration()).toBeUndefined();
    });
  });

  describe('reset', () => {
    it('should reset iterations', () => {
      manager.recordIteration({
        iteration: 1,
        checkResult: createMockCheckResult(true),
        status: 'passed',
      });

      manager.reset();

      expect(manager.getIterations().length).toBe(0);
    });
  });
});

describe('QualityLoop', () => {
  let loop: QualityLoop;

  beforeEach(() => {
    loop = new QualityLoop();
  });

  describe('run', () => {
    it('should run quality loop', async () => {
      const passCallback: CheckAgentCallback = vi.fn().mockResolvedValue(
        createMockCheckResult(true)
      );
      const passLoop = new QualityLoop({}, { check: passCallback });

      const result = await passLoop.run(['src/index.ts']);

      expect(result.passed).toBe(true);
      expect(result.iterations.length).toBe(1);
    });

    it('should emit loop events', async () => {
      const startListener = vi.fn();
      const completeListener = vi.fn();
      loop.on('loop:start', startListener);
      loop.on('loop:complete', completeListener);

      const passCallback: CheckAgentCallback = vi.fn().mockResolvedValue(
        createMockCheckResult(true)
      );
      const eventLoop = new QualityLoop({}, { check: passCallback });
      eventLoop.on('loop:start', startListener);
      eventLoop.on('loop:complete', completeListener);

      await eventLoop.run(['src/index.ts']);

      expect(startListener).toHaveBeenCalled();
      expect(completeListener).toHaveBeenCalled();
    });

    it('should iterate until passed', async () => {
      let callCount = 0;
      const checkCallback: CheckAgentCallback = vi.fn().mockImplementation(() => {
        callCount++;
        if (callCount >= 2) {
          return Promise.resolve(createMockCheckResult(true));
        }
        return Promise.resolve(createMockCheckResult(false, [
          createMockIssue('issue-1', 'warning', true),
        ]));
      });

      const iterLoop = new QualityLoop({ maxIterations: 3 }, { check: checkCallback });
      const result = await iterLoop.run(['src/index.ts']);

      expect(result.passed).toBe(true);
      expect(result.iterations.length).toBe(2);
    });

    it('should stop at max iterations', async () => {
      const failCallback: CheckAgentCallback = vi.fn().mockResolvedValue(
        createMockCheckResult(false, [createMockIssue('issue-1', 'warning', true)])
      );

      const maxLoop = new QualityLoop({ maxIterations: 2 }, { check: failCallback });
      const result = await maxLoop.run(['src/index.ts']);

      expect(result.passed).toBe(false);
      expect(result.iterations.length).toBe(2);
    });

    it('should require manual intervention when no auto-fixable issues', async () => {
      const manualCallback: CheckAgentCallback = vi.fn().mockResolvedValue(
        createMockCheckResult(false, [createMockIssue('issue-1', 'error', false)])
      );

      const manualLoop = new QualityLoop({}, { check: manualCallback });
      const result = await manualLoop.run(['src/index.ts']);

      expect(result.requiresManualIntervention).toBe(true);
    });

    it('should throw when already running', async () => {
      const slowCallback: CheckAgentCallback = vi.fn().mockImplementation(
        () => new Promise(resolve => setTimeout(() => resolve(createMockCheckResult(true)), 100))
      );

      const slowLoop = new QualityLoop({}, { check: slowCallback });

      // Start first run
      const firstRun = slowLoop.run(['src/index.ts']);

      // Try to start second run
      await expect(slowLoop.run(['src/index.ts'])).rejects.toThrow('already running');

      await firstRun;
    });
  });

  describe('stop', () => {
    it('should stop the loop', async () => {
      const stopListener = vi.fn();
      loop.on('loop:stopped', stopListener);

      loop.stop();

      expect(stopListener).toHaveBeenCalled();
    });
  });

  describe('isRunning', () => {
    it('should return running state', () => {
      expect(loop.isRunning()).toBe(false);
    });
  });

  describe('getCurrentIteration', () => {
    it('should return current iteration', async () => {
      expect(loop.getCurrentIteration()).toBe(0);

      const passCallback: CheckAgentCallback = vi.fn().mockResolvedValue(
        createMockCheckResult(true)
      );
      const iterLoop = new QualityLoop({}, { check: passCallback });
      await iterLoop.run(['src/index.ts']);

      expect(iterLoop.getCurrentIteration()).toBe(1);
    });
  });

  describe('getIterationHistory', () => {
    it('should return iteration history', async () => {
      const passCallback: CheckAgentCallback = vi.fn().mockResolvedValue(
        createMockCheckResult(true)
      );
      const histLoop = new QualityLoop({}, { check: passCallback });
      await histLoop.run(['src/index.ts']);

      const history = histLoop.getIterationHistory();
      expect(history.length).toBe(1);
    });
  });

  describe('configuration', () => {
    it('should update config', () => {
      loop.updateConfig({ maxIterations: 5 });
      const config = loop.getConfig();

      expect(config.maxIterations).toBe(5);
    });

    it('should use custom check types', async () => {
      const checkCallback: CheckAgentCallback = vi.fn().mockResolvedValue(
        createMockCheckResult(true)
      );

      const customLoop = new QualityLoop(
        { checkTypes: ['lint', 'type', 'security'] },
        { check: checkCallback }
      );

      await customLoop.run(['src/index.ts']);

      expect(checkCallback).toHaveBeenCalledWith(
        ['src/index.ts'],
        ['lint', 'type', 'security']
      );
    });

    it('should disable auto-fix', async () => {
      const noFixCallback: CheckAgentCallback = vi.fn().mockResolvedValue(
        createMockCheckResult(false, [createMockIssue('issue-1', 'warning', true)])
      );

      const noFixLoop = new QualityLoop(
        { autoFixEnabled: false },
        { check: noFixCallback }
      );

      const result = await noFixLoop.run(['src/index.ts']);

      expect(result.requiresManualIntervention).toBe(true);
    });
  });

  describe('getCheckAgent', () => {
    it('should return check agent', () => {
      const agent = loop.getCheckAgent();
      expect(agent).toBeInstanceOf(CheckAgent);
    });
  });

  describe('getFixAgent', () => {
    it('should return fix agent', () => {
      const agent = loop.getFixAgent();
      expect(agent).toBeInstanceOf(FixAgent);
    });
  });
});
