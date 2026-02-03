import { describe, it, expect, beforeEach, vi } from 'vitest';
import { ParallelCommands } from '../ParallelCommands.js';

describe('ParallelCommands', () => {
  let commands: ParallelCommands;

  beforeEach(() => {
    commands = new ParallelCommands();
  });

  describe('command registration', () => {
    it('should register all parallel commands', () => {
      const allCommands = commands.getAllCommands();

      expect(allCommands.length).toBe(5);
      expect(allCommands.map(c => c.name)).toContain('parallel-start');
      expect(allCommands.map(c => c.name)).toContain('parallel-status');
      expect(allCommands.map(c => c.name)).toContain('parallel-compare');
      expect(allCommands.map(c => c.name)).toContain('parallel-select');
      expect(allCommands.map(c => c.name)).toContain('parallel-merge');
    });

    it('should get specific command', () => {
      const command = commands.getCommand('parallel-start');

      expect(command).toBeDefined();
      expect(command?.name).toBe('parallel-start');
      expect(command?.parameters.length).toBeGreaterThan(0);
    });
  });

  describe('parallel-start', () => {
    it('should start a parallel task', async () => {
      const result = await commands.execute('parallel-start', { task: 'Test task' });

      expect(result.success).toBe(true);
      expect(result.command).toBe('parallel-start');
      expect(result.output).toContain('Parallel Task Started');
      expect(result.data).toBeDefined();
    });

    it('should emit task:started event', async () => {
      const listener = vi.fn();
      commands.on('task:started', listener);

      await commands.execute('parallel-start', { task: 'Test task' });

      expect(listener).toHaveBeenCalled();
    });

    it('should fail without task', async () => {
      const result = await commands.execute('parallel-start', {});

      expect(result.success).toBe(false);
      expect(result.error).toContain('Missing required parameter: task');
    });

    it('should accept workers parameter', async () => {
      const result = await commands.execute('parallel-start', {
        task: 'Test task',
        workers: 5,
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Workers: 5');
    });

    it('should accept models parameter', async () => {
      const result = await commands.execute('parallel-start', {
        task: 'Test task',
        models: ['claude-3-opus', 'gemini-pro'],
      });

      expect(result.success).toBe(true);
      const data = result.data as { workers: Array<{ model: string }> };
      expect(data.workers.some(w => w.model === 'claude-3-opus')).toBe(true);
    });

    it('should create workers with worktrees', async () => {
      const result = await commands.execute('parallel-start', { task: 'Test task' });

      const data = result.data as { workers: Array<{ worktree: string; branch: string }> };
      expect(data.workers[0].worktree).toContain('.codeflow/worktrees');
      expect(data.workers[0].branch).toContain('parallel/');
    });

    it('should store task in active tasks', async () => {
      await commands.execute('parallel-start', { task: 'Test task' });

      const tasks = commands.getActiveTasks();
      expect(tasks.length).toBe(1);
    });
  });

  describe('parallel-status', () => {
    it('should show no tasks when empty', async () => {
      const result = await commands.execute('parallel-status', {});

      expect(result.success).toBe(true);
      expect(result.output).toContain('No active parallel tasks');
    });

    it('should show all tasks', async () => {
      await commands.execute('parallel-start', { task: 'Task 1' });
      await commands.execute('parallel-start', { task: 'Task 2' });

      const result = await commands.execute('parallel-status', {});

      expect(result.success).toBe(true);
      expect(result.output).toContain('Active Parallel Tasks');
    });

    it('should show specific task status', async () => {
      const startResult = await commands.execute('parallel-start', { task: 'Test task' });
      const taskId = (startResult.data as { taskId: string }).taskId;

      const result = await commands.execute('parallel-status', { taskId });

      expect(result.success).toBe(true);
      expect(result.output).toContain(taskId);
      expect(result.output).toContain('Task Status');
    });

    it('should fail for unknown task', async () => {
      const result = await commands.execute('parallel-status', { taskId: 'unknown_task' });

      expect(result.success).toBe(false);
      expect(result.error).toContain('Task not found');
    });

    it('should support verbose mode', async () => {
      const startResult = await commands.execute('parallel-start', { task: 'Test task' });
      const taskId = (startResult.data as { taskId: string }).taskId;

      const result = await commands.execute('parallel-status', { taskId, verbose: true });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Verbose');
      expect(result.output).toContain('Worktrees');
    });
  });

  describe('parallel-compare', () => {
    let taskId: string;

    beforeEach(async () => {
      const startResult = await commands.execute('parallel-start', { task: 'Test task' });
      taskId = (startResult.data as { taskId: string }).taskId;
    });

    it('should compare solutions', async () => {
      const result = await commands.execute('parallel-compare', { taskId });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Solution Comparison');
    });

    it('should fail for unknown task', async () => {
      const result = await commands.execute('parallel-compare', { taskId: 'unknown_task' });

      expect(result.success).toBe(false);
      expect(result.error).toContain('Task not found');
    });

    it('should support custom metrics', async () => {
      const result = await commands.execute('parallel-compare', {
        taskId,
        metrics: ['quality', 'security'],
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('quality');
      expect(result.output).toContain('security');
    });

    it('should support json format', async () => {
      const result = await commands.execute('parallel-compare', {
        taskId,
        format: 'json',
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('"id"');
    });

    it('should support markdown format', async () => {
      const result = await commands.execute('parallel-compare', {
        taskId,
        format: 'markdown',
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('|');
    });

    it('should support table format', async () => {
      const result = await commands.execute('parallel-compare', {
        taskId,
        format: 'table',
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Solution');
      expect(result.output).toContain('Model');
    });
  });

  describe('parallel-select', () => {
    let taskId: string;

    beforeEach(async () => {
      const startResult = await commands.execute('parallel-start', { task: 'Test task' });
      taskId = (startResult.data as { taskId: string }).taskId;
    });

    it('should select a solution', async () => {
      const result = await commands.execute('parallel-select', {
        taskId,
        solutionId: 'solution_1',
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Solution Selected');
    });

    it('should emit solution:selected event', async () => {
      const listener = vi.fn();
      commands.on('solution:selected', listener);

      await commands.execute('parallel-select', {
        taskId,
        solutionId: 'solution_1',
      });

      expect(listener).toHaveBeenCalled();
    });

    it('should fail for unknown task', async () => {
      const result = await commands.execute('parallel-select', {
        taskId: 'unknown_task',
        solutionId: 'solution_1',
      });

      expect(result.success).toBe(false);
      expect(result.error).toContain('Task not found');
    });

    it('should accept reason parameter', async () => {
      const result = await commands.execute('parallel-select', {
        taskId,
        solutionId: 'solution_1',
        reason: 'Best performance',
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Best performance');
    });
  });

  describe('parallel-merge', () => {
    let taskId: string;

    beforeEach(async () => {
      const startResult = await commands.execute('parallel-start', { task: 'Test task' });
      taskId = (startResult.data as { taskId: string }).taskId;
    });

    it('should merge a solution', async () => {
      const result = await commands.execute('parallel-merge', {
        taskId,
        solutionId: 'solution_1',
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Merge Complete');
    });

    it('should emit solution:merged event', async () => {
      const listener = vi.fn();
      commands.on('solution:merged', listener);

      await commands.execute('parallel-merge', {
        taskId,
        solutionId: 'solution_1',
      });

      expect(listener).toHaveBeenCalled();
    });

    it('should fail for unknown task', async () => {
      const result = await commands.execute('parallel-merge', {
        taskId: 'unknown_task',
        solutionId: 'solution_1',
      });

      expect(result.success).toBe(false);
      expect(result.error).toContain('Task not found');
    });

    it('should support strategy parameter', async () => {
      const result = await commands.execute('parallel-merge', {
        taskId,
        solutionId: 'solution_1',
        strategy: 'rebase',
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('rebase');
    });

    it('should create backup by default', async () => {
      const result = await commands.execute('parallel-merge', {
        taskId,
        solutionId: 'solution_1',
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Backup');
    });

    it('should skip backup when disabled', async () => {
      const result = await commands.execute('parallel-merge', {
        taskId,
        solutionId: 'solution_1',
        backup: false,
      });

      expect(result.success).toBe(true);
      const data = result.data as { backupBranch?: string };
      expect(data.backupBranch).toBeUndefined();
    });

    it('should mark task as completed', async () => {
      await commands.execute('parallel-merge', {
        taskId,
        solutionId: 'solution_1',
      });

      const task = commands.getTask(taskId);
      expect(task?.status).toBe('completed');
    });
  });

  describe('getHelp', () => {
    it('should return general help', () => {
      const help = commands.getHelp();

      expect(help).toContain('Parallel Mode Commands');
      expect(help).toContain('parallel-start');
      expect(help).toContain('parallel-status');
    });

    it('should return specific command help', () => {
      const help = commands.getHelp('parallel-start');

      expect(help).toContain('parallel-start');
      expect(help).toContain('Usage:');
      expect(help).toContain('Parameters:');
      expect(help).toContain('Examples:');
    });

    it('should return error for unknown command', () => {
      const help = commands.getHelp('unknown-command');

      expect(help).toContain('Unknown command');
    });
  });

  describe('parseArgs', () => {
    it('should parse positional arguments', () => {
      const params = commands.parseArgs('"Test task" solution_1');

      expect(params['_positional1']).toBe('Test task');
      expect(params['_positional2']).toBe('solution_1');
    });

    it('should parse flag parameters', () => {
      const params = commands.parseArgs('--verbose');

      expect(params['verbose']).toBe(true);
    });

    it('should parse value parameters', () => {
      const params = commands.parseArgs('--strategy rebase');

      expect(params['strategy']).toBe('rebase');
    });

    it('should parse number parameters', () => {
      const params = commands.parseArgs('--workers 5');

      expect(params['workers']).toBe(5);
    });

    it('should parse array parameters', () => {
      const params = commands.parseArgs('--models "claude-3-opus,gemini-pro"');

      expect(params['models']).toEqual(['claude-3-opus', 'gemini-pro']);
    });
  });

  describe('events', () => {
    it('should emit command:start event', async () => {
      const listener = vi.fn();
      commands.on('command:start', listener);

      await commands.execute('parallel-start', { task: 'Test' });

      expect(listener).toHaveBeenCalledWith(
        expect.objectContaining({
          command: 'parallel-start',
        })
      );
    });

    it('should emit command:end event', async () => {
      const listener = vi.fn();
      commands.on('command:end', listener);

      await commands.execute('parallel-start', { task: 'Test' });

      expect(listener).toHaveBeenCalledWith(
        expect.objectContaining({
          command: 'parallel-start',
        })
      );
    });

    it('should include duration in result', async () => {
      const result = await commands.execute('parallel-start', { task: 'Test' });

      expect(result.duration).toBeGreaterThanOrEqual(0);
    });
  });

  describe('unknown command', () => {
    it('should return error for unknown command', async () => {
      const result = await commands.execute('unknown-command', {});

      expect(result.success).toBe(false);
      expect(result.error).toContain('Unknown command');
    });
  });

  describe('task management', () => {
    it('should get active tasks', async () => {
      await commands.execute('parallel-start', { task: 'Task 1' });
      await commands.execute('parallel-start', { task: 'Task 2' });

      const tasks = commands.getActiveTasks();

      expect(tasks.length).toBe(2);
    });

    it('should get specific task', async () => {
      const startResult = await commands.execute('parallel-start', { task: 'Test task' });
      const taskId = (startResult.data as { taskId: string }).taskId;

      const task = commands.getTask(taskId);

      expect(task).toBeDefined();
      expect(task?.task).toBe('Test task');
    });

    it('should return undefined for unknown task', () => {
      const task = commands.getTask('unknown_task');

      expect(task).toBeUndefined();
    });
  });
});
