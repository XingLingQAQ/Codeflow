/**
 * Factory 函数单元测试
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  registerAiderExecutor,
  createOrchestratorWithAider,
  createTask,
  registerClaudeExecutor,
  registerGeminiExecutor,
  registerCodexExecutor,
  createOrchestratorWithAllEditors,
} from '../factory.js';
import { CoworkOrchestrator } from '../CoworkOrchestrator.js';
import { AiderCodeEditor } from '../editors/AiderCodeEditor.js';

describe('Factory Functions', () => {
  let orchestrator: CoworkOrchestrator;

  beforeEach(() => {
    orchestrator = new CoworkOrchestrator();
  });

  afterEach(async () => {
    await orchestrator.cleanup();
  });

  describe('registerAiderExecutor', () => {
    it('should register Aider executor with default config', () => {
      const editor = registerAiderExecutor(orchestrator);

      expect(editor).toBeInstanceOf(AiderCodeEditor);
      expect(orchestrator.getExecutor('aider')).toBeDefined();
    });

    it('should register Aider executor with custom config', () => {
      const editor = registerAiderExecutor(orchestrator, {
        model: 'gpt-4-turbo',
        cwd: '/custom/path',
      });

      expect(editor).toBeInstanceOf(AiderCodeEditor);
    });

    it('should register with correct capabilities', () => {
      registerAiderExecutor(orchestrator);

      const executor = orchestrator.getExecutor('aider');
      expect(executor?.capabilities.supportedTypes).toContain('code-edit');
      expect(executor?.capabilities.features.multiFile).toBe(true);
    });
  });

  describe('createOrchestratorWithAider', () => {
    it('should create orchestrator with Aider pre-registered', () => {
      const { orchestrator: orch, aiderEditor } = createOrchestratorWithAider();

      expect(orch).toBeInstanceOf(CoworkOrchestrator);
      expect(aiderEditor).toBeInstanceOf(AiderCodeEditor);
      expect(orch.getExecutor('aider')).toBeDefined();

      orch.cleanup();
    });

    it('should accept custom Aider config', () => {
      const { orchestrator: orch, aiderEditor } = createOrchestratorWithAider({
        model: 'gpt-4-turbo',
      });

      expect(aiderEditor).toBeInstanceOf(AiderCodeEditor);

      orch.cleanup();
    });
  });

  describe('createTask', () => {
    it('should create task with required fields', () => {
      const task = createTask('task-1', 'aider', ['file.ts'], 'Add logging');

      expect(task.id).toBe('task-1');
      expect(task.executor).toBe('aider');
      expect(task.input.files).toEqual(['file.ts']);
      expect(task.input.instruction).toBe('Add logging');
      expect(task.status).toBe('pending');
      expect(task.type).toBe('code-edit');
      expect(task.createdAt).toBeDefined();
    });

    it('should create task with custom type', () => {
      const task = createTask('task-2', 'aider', ['file.ts'], 'Refactor', {
        type: 'refactor',
      });

      expect(task.type).toBe('refactor');
    });

    it('should create task with context', () => {
      const task = createTask('task-3', 'aider', ['file.ts'], 'Continue', {
        context: 'Previous changes...',
      });

      expect(task.input.context).toBe('Previous changes...');
    });

    it('should create task with config options', () => {
      const task = createTask('task-4', 'aider', ['file.ts'], 'Edit', {
        timeout: 30000,
        priority: 1,
      });

      expect(task.config?.timeout).toBe(30000);
      expect(task.config?.priority).toBe(1);
    });

    it('should create task with multiple files', () => {
      const task = createTask('task-5', 'aider', ['a.ts', 'b.ts', 'c.ts'], 'Refactor all');

      expect(task.input.files).toEqual(['a.ts', 'b.ts', 'c.ts']);
    });

    it('should set createdAt to current timestamp', () => {
      const before = Date.now();
      const task = createTask('task-6', 'aider', ['file.ts'], 'Edit');
      const after = Date.now();

      expect(task.createdAt).toBeGreaterThanOrEqual(before);
      expect(task.createdAt).toBeLessThanOrEqual(after);
    });
  });

  describe('registerClaudeExecutor', () => {
    it('should register Claude executor', () => {
      // Mock adapter
      const mockAdapter = {
        send: vi.fn(),
        stream: vi.fn(),
        getHistory: vi.fn().mockReturnValue([]),
        setHistory: vi.fn(),
        rewind: vi.fn(),
        compact: vi.fn(),
        configure: vi.fn(),
        getConfig: vi.fn(),
      };

      const editor = registerClaudeExecutor(orchestrator, mockAdapter as any);

      expect(editor.name).toBe('claude-editor');
      expect(orchestrator.getExecutor('claude')).toBeDefined();
    });
  });

  describe('registerGeminiExecutor', () => {
    it('should register Gemini executor', () => {
      const mockAdapter = {
        send: vi.fn(),
        receive: vi.fn(),
        getHistory: vi.fn().mockReturnValue([]),
        setHistory: vi.fn(),
        rewind: vi.fn(),
        compact: vi.fn(),
        configure: vi.fn(),
        getConfig: vi.fn(),
      };

      const editor = registerGeminiExecutor(orchestrator, mockAdapter as any);

      expect(editor.name).toBe('gemini-editor');
      expect(orchestrator.getExecutor('gemini')).toBeDefined();
    });
  });

  describe('registerCodexExecutor', () => {
    it('should register Codex executor', () => {
      const mockAdapter = {
        send: vi.fn(),
        receive: vi.fn(),
        getHistory: vi.fn().mockReturnValue([]),
        setHistory: vi.fn(),
        rewind: vi.fn(),
        compact: vi.fn(),
        configure: vi.fn(),
        getConfig: vi.fn(),
      };

      const editor = registerCodexExecutor(orchestrator, mockAdapter as any);

      expect(editor.name).toBe('codex-editor');
      expect(orchestrator.getExecutor('codex')).toBeDefined();
    });
  });

  describe('createOrchestratorWithAllEditors', () => {
    it('should create orchestrator with all editors registered', () => {
      const mockClaudeAdapter = {
        send: vi.fn(),
        stream: vi.fn(),
        getHistory: vi.fn().mockReturnValue([]),
        setHistory: vi.fn(),
        rewind: vi.fn(),
        compact: vi.fn(),
        configure: vi.fn(),
        getConfig: vi.fn(),
      };

      const mockGeminiAdapter = {
        send: vi.fn(),
        receive: vi.fn(),
        getHistory: vi.fn().mockReturnValue([]),
        setHistory: vi.fn(),
        rewind: vi.fn(),
        compact: vi.fn(),
        configure: vi.fn(),
        getConfig: vi.fn(),
      };

      const mockCodexAdapter = {
        send: vi.fn(),
        receive: vi.fn(),
        getHistory: vi.fn().mockReturnValue([]),
        setHistory: vi.fn(),
        rewind: vi.fn(),
        compact: vi.fn(),
        configure: vi.fn(),
        getConfig: vi.fn(),
      };

      const result = createOrchestratorWithAllEditors({
        claudeAdapter: mockClaudeAdapter as any,
        geminiAdapter: mockGeminiAdapter as any,
        codexAdapter: mockCodexAdapter as any,
      });

      expect(result.orchestrator).toBeInstanceOf(CoworkOrchestrator);
      expect(result.orchestrator.getExecutor('aider')).toBeDefined();
      expect(result.orchestrator.getExecutor('claude')).toBeDefined();
      expect(result.orchestrator.getExecutor('gemini')).toBeDefined();
      expect(result.orchestrator.getExecutor('codex')).toBeDefined();

      result.orchestrator.cleanup();
    });
  });
});
