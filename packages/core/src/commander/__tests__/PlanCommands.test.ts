import { describe, it, expect, beforeEach, vi } from 'vitest';
import { PlanCommands } from '../PlanCommands.js';

describe('PlanCommands', () => {
  let commands: PlanCommands;

  beforeEach(() => {
    commands = new PlanCommands();
  });

  describe('command registration', () => {
    it('should register all plan commands', () => {
      const allCommands = commands.getAllCommands();

      expect(allCommands.length).toBe(5);
      expect(allCommands.map(c => c.name)).toContain('plan-new');
      expect(allCommands.map(c => c.name)).toContain('plan-vision');
      expect(allCommands.map(c => c.name)).toContain('plan-constraints');
      expect(allCommands.map(c => c.name)).toContain('plan-ff');
      expect(allCommands.map(c => c.name)).toContain('plan-execute');
    });

    it('should get specific command', () => {
      const command = commands.getCommand('plan-new');

      expect(command).toBeDefined();
      expect(command?.name).toBe('plan-new');
      expect(command?.parameters.length).toBeGreaterThan(0);
    });
  });

  describe('plan-new', () => {
    it('should create a new plan', async () => {
      const result = await commands.execute('plan-new', { name: 'Test Plan' });

      expect(result.success).toBe(true);
      expect(result.command).toBe('plan-new');
      expect(result.output).toContain('Plan created successfully');
      expect(result.data).toBeDefined();
      expect((result.data as { name: string }).name).toBe('Test Plan');
    });

    it('should emit plan:created event', async () => {
      const listener = vi.fn();
      commands.on('plan:created', listener);

      await commands.execute('plan-new', { name: 'Test Plan' });

      expect(listener).toHaveBeenCalled();
    });

    it('should fail without name', async () => {
      const result = await commands.execute('plan-new', {});

      expect(result.success).toBe(false);
      expect(result.error).toContain('Missing required parameter: name');
    });

    it('should accept template parameter', async () => {
      const result = await commands.execute('plan-new', {
        name: 'Test Plan',
        template: 'api-refactor',
      });

      expect(result.success).toBe(true);
      expect((result.data as { template: string }).template).toBe('api-refactor');
    });

    it('should validate template choices', async () => {
      const result = await commands.execute('plan-new', {
        name: 'Test Plan',
        template: 'invalid-template',
      });

      expect(result.success).toBe(false);
      expect(result.error).toContain('Invalid value for template');
    });
  });

  describe('plan-vision', () => {
    it('should start vision builder', async () => {
      const result = await commands.execute('plan-vision', { planId: 'plan_123' });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Vision Builder Started');
      expect(result.output).toContain('plan_123');
    });

    it('should emit vision:started event', async () => {
      const listener = vi.fn();
      commands.on('vision:started', listener);

      await commands.execute('plan-vision', { planId: 'plan_123' });

      expect(listener).toHaveBeenCalled();
    });

    it('should support interactive mode', async () => {
      const result = await commands.execute('plan-vision', {
        planId: 'plan_123',
        interactive: true,
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Interactive Mode');
    });

    it('should support custom questions', async () => {
      const result = await commands.execute('plan-vision', {
        planId: 'plan_123',
        questions: ['Q1', 'Q2'],
      });

      expect(result.success).toBe(true);
      expect((result.data as { questions: string[] }).questions).toContain('Q1');
    });
  });

  describe('plan-constraints', () => {
    it('should generate constraints', async () => {
      const result = await commands.execute('plan-constraints', { planId: 'plan_123' });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Constraints Generated');
    });

    it('should emit constraints:generated event', async () => {
      const listener = vi.fn();
      commands.on('constraints:generated', listener);

      await commands.execute('plan-constraints', { planId: 'plan_123' });

      expect(listener).toHaveBeenCalled();
    });

    it('should support json format', async () => {
      const result = await commands.execute('plan-constraints', {
        planId: 'plan_123',
        format: 'json',
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('"id"');
    });

    it('should support source parameter', async () => {
      const result = await commands.execute('plan-constraints', {
        planId: 'plan_123',
        source: 'vision',
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Source: vision');
    });
  });

  describe('plan-ff', () => {
    it('should generate all artifacts', async () => {
      const result = await commands.execute('plan-ff', { planId: 'plan_123' });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Fast-Forward Complete');
      expect(result.output).toContain('proposal.md');
      expect(result.output).toContain('design.md');
    });

    it('should emit artifacts:generated event', async () => {
      const listener = vi.fn();
      commands.on('artifacts:generated', listener);

      await commands.execute('plan-ff', { planId: 'plan_123' });

      expect(listener).toHaveBeenCalled();
    });

    it('should support dry run', async () => {
      const result = await commands.execute('plan-ff', {
        planId: 'plan_123',
        dryRun: true,
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Dry Run');
      expect(result.output).toContain('Preview');
    });

    it('should not emit event on dry run', async () => {
      const listener = vi.fn();
      commands.on('artifacts:generated', listener);

      await commands.execute('plan-ff', { planId: 'plan_123', dryRun: true });

      expect(listener).not.toHaveBeenCalled();
    });
  });

  describe('plan-execute', () => {
    it('should start execution', async () => {
      const result = await commands.execute('plan-execute', { planId: 'plan_123' });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Plan Execution Started');
    });

    it('should emit execution:started event', async () => {
      const listener = vi.fn();
      commands.on('execution:started', listener);

      await commands.execute('plan-execute', { planId: 'plan_123' });

      expect(listener).toHaveBeenCalled();
    });

    it('should support phase parameter', async () => {
      const result = await commands.execute('plan-execute', {
        planId: 'plan_123',
        phase: 'implement',
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('implement');
    });

    it('should support auto-approve', async () => {
      const result = await commands.execute('plan-execute', {
        planId: 'plan_123',
        autoApprove: true,
      });

      expect(result.success).toBe(true);
      expect(result.output).toContain('Auto-Approve: true');
    });

    it('should validate phase choices', async () => {
      const result = await commands.execute('plan-execute', {
        planId: 'plan_123',
        phase: 'invalid-phase',
      });

      expect(result.success).toBe(false);
      expect(result.error).toContain('Invalid value for phase');
    });
  });

  describe('getHelp', () => {
    it('should return general help', () => {
      const help = commands.getHelp();

      expect(help).toContain('Plan Mode Commands');
      expect(help).toContain('plan-new');
      expect(help).toContain('plan-vision');
    });

    it('should return specific command help', () => {
      const help = commands.getHelp('plan-new');

      expect(help).toContain('plan-new');
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
    it('should parse positional argument', () => {
      const params = commands.parseArgs('"Test Plan"');

      expect(params['_positional']).toBe('Test Plan');
    });

    it('should parse flag parameters', () => {
      const params = commands.parseArgs('--interactive');

      expect(params['interactive']).toBe(true);
    });

    it('should parse value parameters', () => {
      const params = commands.parseArgs('--template api-refactor');

      expect(params['template']).toBe('api-refactor');
    });

    it('should parse number parameters', () => {
      const params = commands.parseArgs('--maxIterations 5');

      expect(params['maxIterations']).toBe(5);
    });

    it('should parse array parameters', () => {
      const params = commands.parseArgs('--questions "Q1,Q2,Q3"');

      expect(params['questions']).toEqual(['Q1', 'Q2', 'Q3']);
    });

    it('should parse mixed parameters', () => {
      const params = commands.parseArgs('"Test Plan" --template default --interactive');

      expect(params['_positional']).toBe('Test Plan');
      expect(params['template']).toBe('default');
      expect(params['interactive']).toBe(true);
    });
  });

  describe('events', () => {
    it('should emit command:start event', async () => {
      const listener = vi.fn();
      commands.on('command:start', listener);

      await commands.execute('plan-new', { name: 'Test' });

      expect(listener).toHaveBeenCalledWith(
        expect.objectContaining({
          command: 'plan-new',
          params: { name: 'Test' },
        })
      );
    });

    it('should emit command:end event', async () => {
      const listener = vi.fn();
      commands.on('command:end', listener);

      await commands.execute('plan-new', { name: 'Test' });

      expect(listener).toHaveBeenCalledWith(
        expect.objectContaining({
          command: 'plan-new',
        })
      );
    });

    it('should include duration in result', async () => {
      const result = await commands.execute('plan-new', { name: 'Test' });

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
});
