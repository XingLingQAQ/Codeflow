import { describe, it, expect, beforeEach, vi } from 'vitest';
import { Commander } from '../Commander.js';
import { AgentConfig, AgentRole, CommanderEvent, CallCoderAgentParams, ConsultSubExpertParams } from '../types.js';
import { ICliAdapter } from '../../adapters/types.js';
import { HookManager } from '../../hooks/HookManager.js';

interface MockAdapterOptions {
  hookAware?: boolean;
  history?: any[];
}

// Mock adapter factory
function createMockAdapter(options: MockAdapterOptions = {}): ICliAdapter {
  const adapter = {
    send: vi.fn().mockResolvedValue({
      content: 'Mock response',
      model: 'test-model',
      usage: { promptTokens: 10, completionTokens: 5, totalTokens: 15 },
      finishReason: 'stop',
    }),
    stream: vi.fn(),
    receive: vi.fn(),
    getHistory: vi.fn().mockReturnValue(options.history ?? []),
    setHistory: vi.fn(),
    rewind: vi.fn(),
    compact: vi.fn(),
    configure: vi.fn(),
    getConfig: vi.fn().mockReturnValue({ model: 'test-model' }),
  } as Record<string, unknown>;

  if (options.hookAware) {
    adapter.setHookManager = vi.fn();
  }

  return adapter as unknown as ICliAdapter;
}

describe('Commander', () => {
  let commander: Commander;
  let mockHookManager: HookManager;
  let mockMainAdapter: ICliAdapter;
  let mockCoderAdapter: ICliAdapter;
  let mockSubExpertAdapter: ICliAdapter;

  beforeEach(() => {
    mockHookManager = {} as HookManager;
    mockMainAdapter = createMockAdapter();
    mockCoderAdapter = createMockAdapter();
    mockSubExpertAdapter = createMockAdapter();

    commander = new Commander(mockHookManager, 5);
  });

  describe('constructor', () => {
    it('should create commander with default max nesting depth', () => {
      const defaultCommander = new Commander();
      expect(defaultCommander).toBeDefined();
    });

    it('should create commander with custom max nesting depth', () => {
      const customCommander = new Commander(undefined, 10);
      expect(customCommander).toBeDefined();
    });

    it('should create commander with hook manager', () => {
      const commanderWithHooks = new Commander(mockHookManager);
      expect(commanderWithHooks).toBeDefined();
    });
  });

  describe('registerAgent', () => {
    it('should register main agent', () => {
      const config: AgentConfig = {
        role: 'main',
        adapter: mockMainAdapter,
        systemPrompt: 'You are the main AI',
      };

      commander.registerAgent(config);

      const agent = commander.getAgent('main');
      expect(agent).toBeDefined();
      expect(agent?.role).toBe('main');
      expect(agent?.systemPrompt).toBe('You are the main AI');
    });

    it('should inject hook manager into hook-aware adapters', () => {
      const hookAwareAdapter = createMockAdapter({ hookAware: true }) as ICliAdapter & {
        setHookManager: ReturnType<typeof vi.fn>;
      };

      commander.registerAgent({
        role: 'main',
        adapter: hookAwareAdapter,
      });

      expect(hookAwareAdapter.setHookManager).toHaveBeenCalledWith(mockHookManager);
    });

    it('should register coder agent', () => {
      const config: AgentConfig = {
        role: 'coder',
        adapter: mockCoderAdapter,
      };

      commander.registerAgent(config);

      const agent = commander.getAgent('coder');
      expect(agent).toBeDefined();
      expect(agent?.role).toBe('coder');
    });

    it('should register sub_expert agent', () => {
      const config: AgentConfig = {
        role: 'sub_expert',
        adapter: mockSubExpertAdapter,
      };

      commander.registerAgent(config);

      const agent = commander.getAgent('sub_expert');
      expect(agent).toBeDefined();
      expect(agent?.role).toBe('sub_expert');
    });

    it('should emit AGENT_REGISTERED event', () => {
      const handler = vi.fn();
      commander.on(CommanderEvent.AGENT_REGISTERED, handler);

      commander.registerAgent({
        role: 'coder',
        adapter: mockCoderAdapter,
      });

      expect(handler).toHaveBeenCalledWith({ role: 'coder' });
    });

    it('should overwrite existing agent with same role', () => {
      const adapter1 = createMockAdapter();
      const adapter2 = createMockAdapter();

      commander.registerAgent({ role: 'coder', adapter: adapter1 });
      commander.registerAgent({ role: 'coder', adapter: adapter2 });

      const agent = commander.getAgent('coder');
      expect(agent?.adapter).toBe(adapter2);
    });
  });

  describe('getAgent', () => {
    it('should return undefined for unregistered agent', () => {
      const agent = commander.getAgent('coder');
      expect(agent).toBeUndefined();
    });

    it('should return registered agent', () => {
      commander.registerAgent({
        role: 'main',
        adapter: mockMainAdapter,
      });

      const agent = commander.getAgent('main');
      expect(agent).toBeDefined();
    });
  });

  describe('callCoderAgent', () => {
    beforeEach(() => {
      commander.registerAgent({ role: 'main', adapter: mockMainAdapter });
      commander.registerAgent({ role: 'coder', adapter: mockCoderAdapter });
    });

    it('should call coder agent with task', async () => {
      const params: CallCoderAgentParams = {
        task: 'Write a function to add two numbers',
      };

      const result = await commander.callCoderAgent(params);

      expect(result.success).toBe(true);
      expect(result.output).toBe('Mock response');
      expect(result.agentRole).toBe('coder');
      expect(result.tokenUsage).toBeDefined();
      expect(result.duration).toBeGreaterThanOrEqual(0);
    });

    it('should call coder agent with full params', async () => {
      const params: CallCoderAgentParams = {
        task: 'Refactor the code',
        context: 'This is a legacy codebase',
        files: ['src/index.ts', 'src/utils.ts'],
        language: 'TypeScript',
        constraints: ['Keep backward compatibility', 'Add tests'],
      };

      const result = await commander.callCoderAgent(params);

      expect(result.success).toBe(true);
      expect(mockCoderAdapter.send).toHaveBeenCalled();

      const sentPrompt = (mockCoderAdapter.send as any).mock.calls[0][0];
      expect(sentPrompt).toContain('Refactor the code');
      expect(sentPrompt).toContain('legacy codebase');
      expect(sentPrompt).toContain('src/index.ts');
      expect(sentPrompt).toContain('TypeScript');
      expect(sentPrompt).toContain('backward compatibility');
    });

    it('should fail when coder agent not registered', async () => {
      const newCommander = new Commander();
      newCommander.registerAgent({ role: 'main', adapter: mockMainAdapter });

      const result = await newCommander.callCoderAgent({ task: 'Test' });

      expect(result.success).toBe(false);
      expect(result.error).toBe('Coder agent not registered');
    });

    it('should emit TOOL_CALL_START and TOOL_CALL_END events', async () => {
      const startHandler = vi.fn();
      const endHandler = vi.fn();

      commander.on(CommanderEvent.TOOL_CALL_START, startHandler);
      commander.on(CommanderEvent.TOOL_CALL_END, endHandler);

      await commander.callCoderAgent({ task: 'Test' });

      expect(startHandler).toHaveBeenCalled();
      expect(endHandler).toHaveBeenCalled();
    });

    it('should graft context when context param provided', async () => {
      const mainHistory = [
        { role: 'user' as const, content: 'Hello', timestamp: Date.now() },
        { role: 'assistant' as const, content: 'Hi', timestamp: Date.now() },
      ];
      (mockMainAdapter.getHistory as any).mockReturnValue(mainHistory);

      await commander.callCoderAgent({
        task: 'Test',
        context: 'Some context',
      });

      expect(mockCoderAdapter.setHistory).toHaveBeenCalled();
    });

    it('should handle adapter errors gracefully', async () => {
      (mockCoderAdapter.send as any).mockRejectedValue(new Error('API Error'));

      const result = await commander.callCoderAgent({ task: 'Test' });

      expect(result.success).toBe(false);
      expect(result.error).toBe('API Error');
    });

    it('should track call in call trace', async () => {
      await commander.callCoderAgent({ task: 'Test' });

      // Call trace is cleared after completion, but events were emitted
      const trace = commander.getCallTrace();
      expect(trace).toEqual([]);
    });
  });

  describe('consultSubExpert', () => {
    beforeEach(() => {
      commander.registerAgent({ role: 'main', adapter: mockMainAdapter });
      commander.registerAgent({ role: 'sub_expert', adapter: mockSubExpertAdapter });
    });

    it('should consult sub expert with domain and question', async () => {
      const params: ConsultSubExpertParams = {
        domain: 'security',
        question: 'How to prevent SQL injection?',
      };

      const result = await commander.consultSubExpert(params);

      expect(result.success).toBe(true);
      expect(result.output).toBe('Mock response');
      expect(result.agentRole).toBe('sub_expert');
    });

    it('should consult sub expert with full params', async () => {
      const params: ConsultSubExpertParams = {
        domain: 'performance',
        question: 'How to optimize database queries?',
        context: 'We have a PostgreSQL database',
        depth: 3,
      };

      const result = await commander.consultSubExpert(params);

      expect(result.success).toBe(true);

      const sentPrompt = (mockSubExpertAdapter.send as any).mock.calls[0][0];
      expect(sentPrompt).toContain('performance');
      expect(sentPrompt).toContain('optimize database queries');
      expect(sentPrompt).toContain('PostgreSQL');
    });

    it('should fail when sub expert not registered', async () => {
      const newCommander = new Commander();
      newCommander.registerAgent({ role: 'main', adapter: mockMainAdapter });

      const result = await newCommander.consultSubExpert({
        domain: 'security',
        question: 'Test',
      });

      expect(result.success).toBe(false);
      expect(result.error).toBe('Sub expert agent not registered');
    });

    it('should emit events during consultation', async () => {
      const startHandler = vi.fn();
      const endHandler = vi.fn();

      commander.on(CommanderEvent.TOOL_CALL_START, startHandler);
      commander.on(CommanderEvent.TOOL_CALL_END, endHandler);

      await commander.consultSubExpert({
        domain: 'security',
        question: 'Test',
      });

      expect(startHandler).toHaveBeenCalled();
      expect(endHandler).toHaveBeenCalled();
    });

    it('should respect custom depth limit', async () => {
      // Create a commander with max depth 1
      const shallowCommander = new Commander(undefined, 1);
      shallowCommander.registerAgent({ role: 'main', adapter: mockMainAdapter });
      shallowCommander.registerAgent({ role: 'sub_expert', adapter: mockSubExpertAdapter });

      // First call should succeed
      const result1 = await shallowCommander.consultSubExpert({
        domain: 'test',
        question: 'Test',
      });
      expect(result1.success).toBe(true);
    });

    it('should handle adapter errors gracefully', async () => {
      (mockSubExpertAdapter.send as any).mockRejectedValue(new Error('Network Error'));

      const result = await commander.consultSubExpert({
        domain: 'security',
        question: 'Test',
      });

      expect(result.success).toBe(false);
      expect(result.error).toBe('Network Error');
    });
  });

  describe('graftContext', () => {
    beforeEach(() => {
      commander.registerAgent({ role: 'main', adapter: mockMainAdapter, systemPrompt: 'Main system prompt' });
      commander.registerAgent({ role: 'coder', adapter: mockCoderAdapter });
    });

    it('should graft context from source to target', async () => {
      const mainHistory = [
        { role: 'user' as const, content: 'Hello', timestamp: Date.now() },
        { role: 'assistant' as const, content: 'Hi there', timestamp: Date.now() },
      ];
      (mockMainAdapter.getHistory as any).mockReturnValue(mainHistory);

      const grafted = await commander.graftContext('main', 'coder');

      expect(grafted.messages).toHaveLength(2);
      expect(grafted.metadata.sourceAgent).toBe('main');
      expect(grafted.metadata.graftedAt).toBeGreaterThan(0);
    });

    it('should throw error for unregistered source agent', async () => {
      await expect(commander.graftContext('sub_expert', 'coder')).rejects.toThrow(
        "Source agent 'sub_expert' not registered"
      );
    });

    it('should throw error for unregistered target agent', async () => {
      await expect(commander.graftContext('main', 'sub_expert')).rejects.toThrow(
        "Target agent 'sub_expert' not registered"
      );
    });

    it('should inherit system prompt when configured', async () => {
      (mockMainAdapter.getHistory as any).mockReturnValue([]);

      const grafted = await commander.graftContext('main', 'coder', {
        inheritSystemPrompt: true,
      });

      expect(grafted.systemPrompt).toBe('Main system prompt');
    });

    it('should not inherit system prompt by default', async () => {
      (mockMainAdapter.getHistory as any).mockReturnValue([]);

      const grafted = await commander.graftContext('main', 'coder');

      expect(grafted.systemPrompt).toBeUndefined();
    });

    it('should filter messages by role', async () => {
      const mainHistory = [
        { role: 'user' as const, content: 'User message', timestamp: Date.now() },
        { role: 'assistant' as const, content: 'Assistant message', timestamp: Date.now() },
        { role: 'system' as const, content: 'System message', timestamp: Date.now() },
      ];
      (mockMainAdapter.getHistory as any).mockReturnValue(mainHistory);

      const grafted = await commander.graftContext('main', 'coder', {
        filterRoles: ['user', 'assistant'],
      });

      expect(grafted.messages).toHaveLength(2);
      expect(grafted.messages.every(m => m.role !== 'system')).toBe(true);
    });

    it('should limit context by max tokens', async () => {
      const mainHistory = [
        { role: 'user' as const, content: 'A'.repeat(1000), timestamp: Date.now() },
        { role: 'assistant' as const, content: 'B'.repeat(1000), timestamp: Date.now() },
        { role: 'user' as const, content: 'C'.repeat(100), timestamp: Date.now() },
      ];
      (mockMainAdapter.getHistory as any).mockReturnValue(mainHistory);

      const grafted = await commander.graftContext('main', 'coder', {
        maxContextTokens: 200,
      });

      // Should keep most recent messages within token limit
      expect(grafted.messages.length).toBeLessThan(3);
    });

    it('should not inherit messages when inheritMessages is false', async () => {
      const mainHistory = [
        { role: 'user' as const, content: 'Hello', timestamp: Date.now() },
        { role: 'assistant' as const, content: 'Hi there', timestamp: Date.now() },
      ];
      (mockMainAdapter.getHistory as any).mockReturnValue(mainHistory);

      const grafted = await commander.graftContext('main', 'coder', {
        inheritMessages: false,
      });

      expect(grafted.messages).toEqual([]);
      expect(grafted.metadata.tokenCount).toBe(0);
    });

    it('should keep richer tool turn messages and count projected tokens', async () => {
      const mainHistory = [
        {
          role: 'assistant' as const,
          content: [
            { type: 'text' as const, text: 'calling search' },
            { type: 'tool_call' as const, id: 'call-1', toolName: 'search', args: { query: 'schema' } },
          ],
          timestamp: 1,
        },
        {
          role: 'assistant' as const,
          content: [
            { type: 'tool_result' as const, toolCallId: 'call-1', toolName: 'search', result: { hits: 3 } },
          ],
          timestamp: 2,
        },
      ];
      (mockMainAdapter.getHistory as any).mockReturnValue(mainHistory);

      const grafted = await commander.graftContext('main', 'coder');

      expect(grafted.messages).toEqual(mainHistory);
      expect(grafted.metadata.tokenCount).toBeGreaterThan(0);
      expect((grafted.messages[0].content as any[])[1]).toMatchObject({
        type: 'tool_call',
        toolName: 'search',
      });
      expect((grafted.messages[1].content as any[])[0]).toMatchObject({
        type: 'tool_result',
        toolName: 'search',
      });
    });

    it('should keep most recent richer message when max tokens truncates context', async () => {
      const mainHistory = [
        { role: 'user' as const, content: 'A'.repeat(2000), timestamp: 1 },
        {
          role: 'assistant' as const,
          content: [
            { type: 'tool_result' as const, toolCallId: 'call-1', toolName: 'search', result: { hits: 3 } },
          ],
          timestamp: 2,
        },
      ];
      (mockMainAdapter.getHistory as any).mockReturnValue(mainHistory);

      const grafted = await commander.graftContext('main', 'coder', {
        maxContextTokens: 50,
      });

      expect(grafted.messages).toHaveLength(1);
      expect((grafted.messages[0].content as any[])[0]).toMatchObject({
        type: 'tool_result',
        toolName: 'search',
      });
    });

    it('should emit CONTEXT_GRAFTED event', async () => {
      const handler = vi.fn();
      commander.on(CommanderEvent.CONTEXT_GRAFTED, handler);

      (mockMainAdapter.getHistory as any).mockReturnValue([]);

      await commander.graftContext('main', 'coder');

      expect(handler).toHaveBeenCalledWith(
        expect.objectContaining({
          sourceRole: 'main',
          targetRole: 'coder',
        })
      );
    });
  });

  describe('getToolDefinitions', () => {
    it('should return tool definitions', () => {
      const definitions = commander.getToolDefinitions();

      expect(definitions).toHaveLength(2);
    });

    it('should include call_coder_agent definition', () => {
      const definitions = commander.getToolDefinitions();
      const coderTool = definitions.find(d => d.name === 'call_coder_agent');

      expect(coderTool).toBeDefined();
      expect(coderTool?.description).toContain('Coder Agent');
      expect(coderTool?.parameters.required).toContain('task');
    });

    it('should include consult_sub_expert definition', () => {
      const definitions = commander.getToolDefinitions();
      const expertTool = definitions.find(d => d.name === 'consult_sub_expert');

      expect(expertTool).toBeDefined();
      expect(expertTool?.description).toContain('sub-expert');
      expect(expertTool?.parameters.required).toContain('domain');
      expect(expertTool?.parameters.required).toContain('question');
    });

    it('should have proper parameter types', () => {
      const definitions = commander.getToolDefinitions();
      const coderTool = definitions.find(d => d.name === 'call_coder_agent');

      expect(coderTool?.parameters.type).toBe('object');
      expect(coderTool?.parameters.properties.task.type).toBe('string');
      expect(coderTool?.parameters.properties.files.type).toBe('array');
    });
  });

  describe('event system', () => {
    it('should register event handler with on()', () => {
      const handler = vi.fn();
      commander.on(CommanderEvent.AGENT_REGISTERED, handler);

      commander.registerAgent({ role: 'coder', adapter: mockCoderAdapter });

      expect(handler).toHaveBeenCalled();
    });

    it('should unregister event handler with off()', () => {
      const handler = vi.fn();
      commander.on(CommanderEvent.AGENT_REGISTERED, handler);
      commander.off(CommanderEvent.AGENT_REGISTERED, handler);

      commander.registerAgent({ role: 'coder', adapter: mockCoderAdapter });

      expect(handler).not.toHaveBeenCalled();
    });

    it('should support multiple handlers for same event', () => {
      const handler1 = vi.fn();
      const handler2 = vi.fn();

      commander.on(CommanderEvent.AGENT_REGISTERED, handler1);
      commander.on(CommanderEvent.AGENT_REGISTERED, handler2);

      commander.registerAgent({ role: 'coder', adapter: mockCoderAdapter });

      expect(handler1).toHaveBeenCalled();
      expect(handler2).toHaveBeenCalled();
    });

    it('should handle errors in event handlers gracefully', () => {
      const errorHandler = vi.fn().mockImplementation(() => {
        throw new Error('Handler error');
      });
      const normalHandler = vi.fn();

      commander.on(CommanderEvent.AGENT_REGISTERED, errorHandler);
      commander.on(CommanderEvent.AGENT_REGISTERED, normalHandler);

      // Should not throw
      expect(() => {
        commander.registerAgent({ role: 'coder', adapter: mockCoderAdapter });
      }).not.toThrow();

      // Normal handler should still be called
      expect(normalHandler).toHaveBeenCalled();
    });

    it('should emit NESTED_CALL_START for nested calls', async () => {
      const handler = vi.fn();
      commander.on(CommanderEvent.NESTED_CALL_START, handler);

      commander.registerAgent({ role: 'main', adapter: mockMainAdapter });
      commander.registerAgent({ role: 'coder', adapter: mockCoderAdapter });
      commander.registerAgent({ role: 'sub_expert', adapter: mockSubExpertAdapter });

      // Simulate nested call by making coder call sub_expert
      // This is a simplified test - in real scenario, coder would trigger sub_expert
      await commander.callCoderAgent({ task: 'Test' });

      // First call doesn't have a parent, so NESTED_CALL_START won't be emitted
      // This tests the event system is set up correctly
      expect(handler).not.toHaveBeenCalled();
    });
  });

  describe('getCallTrace', () => {
    it('should return empty array when no calls in progress', () => {
      const trace = commander.getCallTrace();
      expect(trace).toEqual([]);
    });

    it('should return copy of call stack', () => {
      const trace1 = commander.getCallTrace();
      const trace2 = commander.getCallTrace();

      expect(trace1).not.toBe(trace2);
      expect(trace1).toEqual(trace2);
    });
  });

  describe('max nesting depth', () => {
    it('should enforce max nesting depth for callCoderAgent', async () => {
      // Create commander with max depth 0 (no calls allowed)
      const restrictedCommander = new Commander(undefined, 0);
      restrictedCommander.registerAgent({ role: 'main', adapter: mockMainAdapter });
      restrictedCommander.registerAgent({ role: 'coder', adapter: mockCoderAdapter });

      const result = await restrictedCommander.callCoderAgent({ task: 'Test' });

      expect(result.success).toBe(false);
      expect(result.error).toContain('Max nesting depth');
    });

    it('should enforce max nesting depth for consultSubExpert', async () => {
      const restrictedCommander = new Commander(undefined, 0);
      restrictedCommander.registerAgent({ role: 'main', adapter: mockMainAdapter });
      restrictedCommander.registerAgent({ role: 'sub_expert', adapter: mockSubExpertAdapter });

      const result = await restrictedCommander.consultSubExpert({
        domain: 'test',
        question: 'Test',
      });

      expect(result.success).toBe(false);
      expect(result.error).toContain('Max nesting depth');
    });

    it('should use custom depth from params for consultSubExpert', async () => {
      const restrictedCommander = new Commander(undefined, 10);
      restrictedCommander.registerAgent({ role: 'main', adapter: mockMainAdapter });
      restrictedCommander.registerAgent({ role: 'sub_expert', adapter: mockSubExpertAdapter });

      // Use depth: 0 in params to restrict
      const result = await restrictedCommander.consultSubExpert({
        domain: 'test',
        question: 'Test',
        depth: 0,
      });

      expect(result.success).toBe(false);
      expect(result.error).toContain('Max nesting depth (0) exceeded');
    });
  });

  describe('prompt building', () => {
    beforeEach(() => {
      commander.registerAgent({ role: 'main', adapter: mockMainAdapter });
      commander.registerAgent({ role: 'coder', adapter: mockCoderAdapter });
      commander.registerAgent({ role: 'sub_expert', adapter: mockSubExpertAdapter });
    });

    it('should build coder prompt with all sections', async () => {
      await commander.callCoderAgent({
        task: 'Implement feature',
        context: 'Background info',
        files: ['file1.ts', 'file2.ts'],
        language: 'TypeScript',
        constraints: ['Constraint 1', 'Constraint 2'],
      });

      const sentPrompt = (mockCoderAdapter.send as any).mock.calls[0][0];

      expect(sentPrompt).toContain('## Coding Task');
      expect(sentPrompt).toContain('Implement feature');
      expect(sentPrompt).toContain('## Context');
      expect(sentPrompt).toContain('Background info');
      expect(sentPrompt).toContain('## Relevant Files');
      expect(sentPrompt).toContain('- file1.ts');
      expect(sentPrompt).toContain('## Language');
      expect(sentPrompt).toContain('TypeScript');
      expect(sentPrompt).toContain('## Constraints');
      expect(sentPrompt).toContain('- Constraint 1');
    });

    it('should build sub expert prompt with all sections', async () => {
      await commander.consultSubExpert({
        domain: 'security',
        question: 'How to secure API?',
        context: 'REST API context',
      });

      const sentPrompt = (mockSubExpertAdapter.send as any).mock.calls[0][0];

      expect(sentPrompt).toContain('## Domain: security');
      expect(sentPrompt).toContain('## Question');
      expect(sentPrompt).toContain('How to secure API?');
      expect(sentPrompt).toContain('## Context');
      expect(sentPrompt).toContain('REST API context');
    });
  });

  describe('token estimation', () => {
    beforeEach(() => {
      commander.registerAgent({ role: 'main', adapter: mockMainAdapter });
      commander.registerAgent({ role: 'coder', adapter: mockCoderAdapter });
    });

    it('should estimate tokens for English text', async () => {
      const mainHistory = [
        { role: 'user' as const, content: 'Hello world', timestamp: Date.now() },
      ];
      (mockMainAdapter.getHistory as any).mockReturnValue(mainHistory);

      const grafted = await commander.graftContext('main', 'coder', {
        maxContextTokens: 1000,
      });

      expect(grafted.metadata.tokenCount).toBeGreaterThan(0);
    });

    it('should estimate tokens for CJK text', async () => {
      const mainHistory = [
        { role: 'user' as const, content: '你好世界', timestamp: Date.now() },
      ];
      (mockMainAdapter.getHistory as any).mockReturnValue(mainHistory);

      const grafted = await commander.graftContext('main', 'coder', {
        maxContextTokens: 1000,
      });

      expect(grafted.metadata.tokenCount).toBeGreaterThan(0);
    });

    it('should estimate tokens for code blocks', async () => {
      const mainHistory = [
        {
          role: 'user' as const,
          content: '```typescript\nfunction hello() { return "world"; }\n```',
          timestamp: Date.now(),
        },
      ];
      (mockMainAdapter.getHistory as any).mockReturnValue(mainHistory);

      const grafted = await commander.graftContext('main', 'coder', {
        maxContextTokens: 1000,
      });

      expect(grafted.metadata.tokenCount).toBeGreaterThan(0);
    });
  });
});
