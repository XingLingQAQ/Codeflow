import { describe, expect, it } from 'vitest';
import { AuditManager, InMemoryAuditStorage } from '../../audit/index.js';
import { AgentRuntime } from '../../cowork/runtime.js';
import type { Diff, EditResult, ICodeEditor } from '../../cowork/types.js';
import { HeadlessToolRuntime } from '../HeadlessToolRuntime.js';
import { MCPGateway } from '../MCPGateway.js';
import {
  SearchGateway,
  SearchProviderRegistry,
  SearchRouter,
  StaticSearchProvider,
} from '../SearchProvider.js';
import { ToolExecutor } from '../ToolExecutor.js';
import { ToolRegistry } from '../ToolRegistry.js';

describe('headless tool runtime', () => {
  const createEditor = (): ICodeEditor => ({
    name: 'mock-editor',
    edit: async (file: string, instruction: string): Promise<EditResult> => ({
      success: true,
      file,
      diff: {
        file,
        hunks: [
          {
            oldStart: 1,
            oldLines: 0,
            newStart: 1,
            newLines: 1,
            content: `+ ${instruction}`,
          },
        ],
        additions: 1,
        deletions: 0,
      },
    }),
    editMultiple: async (files: string[], instruction: string): Promise<EditResult[]> =>
      Promise.all(files.map((file) => createEditor().edit(file, instruction))),
    preview: async (file: string, instruction: string): Promise<Diff> => ({
      file,
      hunks: [
        {
          oldStart: 1,
          oldLines: 0,
          newStart: 1,
          newLines: 1,
          content: `+ ${instruction}`,
        },
      ],
      additions: 1,
      deletions: 0,
    }),
    applyDiff: async () => undefined,
    undo: async () => undefined,
  });

  it('routes AgentRuntime file edits through shared ToolRegistry and ToolExecutor', async () => {
    const auditManager = new AuditManager(new InMemoryAuditStorage());
    const runtime = new AgentRuntime({ auditManager });
    const editor = createEditor();

    runtime.registerExecutor(
      'claude',
      editor,
      {
        name: 'claude',
        supportedTypes: ['code-edit'],
        maxConcurrency: 1,
        estimatedSpeed: 'fast',
        features: {
          streaming: false,
          multiFile: true,
          contextAware: true,
          codeReview: false,
        },
      },
      'claude-opus-4.6'
    );

    const result = await runtime.executeTask({
      id: 'task-1',
      type: 'code-edit',
      executor: 'claude',
      input: {
        files: ['demo.ts'],
        instruction: 'add runtime entry',
      },
      status: 'pending',
      createdAt: Date.now(),
    });

    expect(result.status).toBe('completed');
    expect(runtime.getToolRegistry().has('file.edit')).toBe(true);
    expect(runtime.getToolTraces()).toHaveLength(1);
    expect(runtime.getToolTraces()[0].toolId).toBe('file.edit');

    const auditEntries = await auditManager.getLatestEntries(10);
    expect(auditEntries[0]?.resource.id).toBe('file.edit');
  });

  it('routes search providers through shared registry', async () => {
    const registry = new SearchProviderRegistry();
    registry.register(
      new StaticSearchProvider('memory-default', 'memory', async (request) => ({
        provider: 'memory-default',
        total: 1,
        items: [
          {
            id: 'mem-1',
            title: request.query,
            snippet: 'memory result',
            source: 'memory',
          },
        ],
      }))
    );

    const router = new SearchRouter(registry);
    const result = await router.search(
      'memory',
      { query: 'runtime' },
      { entryPoint: 'agent' }
    );

    expect(result.provider).toBe('memory-default');
    expect(result.items[0]?.title).toBe('runtime');
  });

  it('executes shared search tools through ToolExecutor traces', async () => {
    const auditManager = new AuditManager(new InMemoryAuditStorage());
    const toolRegistry = new ToolRegistry();
    const toolExecutor = new ToolExecutor(toolRegistry, { auditManager });
    const providerRegistry = new SearchProviderRegistry();
    const gateway = new SearchGateway(providerRegistry, toolRegistry, toolExecutor);

    gateway.register(
      new StaticSearchProvider('memory-default', 'memory', async (request) => ({
        provider: 'memory-default',
        total: 1,
        items: [
          {
            id: 'mem-1',
            title: request.query,
            snippet: 'memory result',
            source: 'memory',
          },
        ],
      }))
    );

    const result = await gateway.execute(
      'memory',
      { query: 'runtime', limit: 5 },
      { entryPoint: 'frontend', sessionId: 'sess-1' }
    );

    expect(result.ok).toBe(true);
    expect(result.output?.provider).toBe('memory-default');
    expect(result.trace.toolId).toBe('search.memory');
    expect(toolRegistry.has('search.memory')).toBe(true);

    const auditEntries = await auditManager.getLatestEntries(10);
    expect(auditEntries[0]?.resource.id).toBe('search.memory');
  });

  it('dispatches skills through shared runtime with execution records', async () => {
    const auditManager = new AuditManager(new InMemoryAuditStorage());
    const runtime = new HeadlessToolRuntime({ auditManager });

    runtime.registerSkill({
      manifest: {
        skillId: 'skill.summarize',
        version: '1.0.0',
        description: 'summarize content',
        tags: ['skill', 'summary'],
        riskLevel: 'low',
        source: 'internal',
        entryPoints: ['agent', 'api'],
        inputSchema: {
          type: 'object',
          properties: {
            text: { type: 'string' },
          },
          required: ['text'],
        },
        toolIds: ['search.code'],
      },
      handler: {
        execute: async (input, context) => {
          await context.runtime.executeSearch('code', { query: String((input as { text: string }).text) }, {
            entryPoint: 'agent',
            sessionId: context.sessionId,
            taskId: context.taskId,
            agentId: context.agentId,
          });
          return {
            summary: String((input as { text: string }).text).slice(0, 8),
          };
        },
      },
    });

    runtime.registerSearchProvider(
      new StaticSearchProvider('code-default', 'code', async (request) => ({
        provider: 'code-default',
        total: 1,
        items: [
          {
            id: 'code-1',
            title: request.query,
            snippet: 'code result',
            source: 'code',
          },
        ],
      }))
    );

    const result = await runtime.executeSkill<{ summary: string }>({
      skillId: 'skill.summarize',
      input: { text: 'runtime summary text' },
      triggerReason: 'agent requested summary',
      context: {
        entryPoint: 'agent',
        sessionId: 'sess-1',
        taskId: 'task-1',
        agentId: 'tester',
      },
    });

    expect(result.ok).toBe(true);
    expect(result.output?.summary).toBe('runtime ');
    expect(result.record.skillId).toBe('skill.summarize');
    expect(result.record.lifecycle).toEqual(['registered', 'authorized', 'loaded', 'executed', 'recorded']);
    expect(result.record.toolIds).toContain('search.code');
    expect(runtime.getSkillExecutionRecords()).toHaveLength(1);

    const auditEntries = await auditManager.getLatestEntries(10);
    expect(auditEntries.some((entry) => entry.resource.type === 'skill')).toBe(true);
  });

  it('lets AgentRuntime execute registered skills through headless runtime', async () => {
    const runtime = new AgentRuntime();
    const editor = createEditor();

    runtime.registerExecutor(
      'claude',
      editor,
      {
        name: 'claude',
        supportedTypes: ['code-edit'],
        maxConcurrency: 1,
        estimatedSpeed: 'fast',
        features: {
          streaming: false,
          multiFile: true,
          contextAware: true,
          codeReview: false,
        },
      },
      'claude-opus-4.6'
    );

    runtime.registerSkill({
      manifest: {
        skillId: 'skill.inspect',
        version: '1.0.0',
        description: 'inspect text',
        tags: ['skill', 'inspect'],
        riskLevel: 'low',
        source: 'internal',
        entryPoints: ['agent'],
        inputSchema: {
          type: 'object',
          properties: {
            text: { type: 'string' },
          },
          required: ['text'],
        },
      },
      handler: {
        execute: async (input) => ({
          echoed: (input as { text: string }).text,
        }),
      },
    });

    const output = await runtime.executeSkill<{ echoed: string }>('skill.inspect', { text: 'runtime' }, {
      taskId: 'task-skill',
      sessionId: 'sess-skill',
      agentId: 'claude',
      triggerReason: 'follow-up task',
    });

    expect(output.echoed).toBe('runtime');
    expect(runtime.getSkillExecutionRecords()).toHaveLength(1);
  });

  it('shares registry and traces across file search skill and mcp gateways', async () => {
    const runtime = new HeadlessToolRuntime();
    const editor = createEditor();

    runtime.registerSearchProvider(
      new StaticSearchProvider('code-default', 'code', async (request) => ({
        provider: 'code-default',
        total: 1,
        items: [
          {
            id: 'code-1',
            title: request.query,
            snippet: 'code result',
            source: 'code',
          },
        ],
      }))
    );

    runtime.registerSkillTool({
      id: 'skill.summarize',
      version: '1.0.0',
      description: 'summarize content',
      tags: ['skill', 'summary'],
      riskLevel: 'low',
      executionModes: ['sync'],
      entryPoints: ['agent', 'api'],
      inputSchema: {
        type: 'object',
        properties: {
          text: { type: 'string' },
        },
        required: ['text'],
      },
      handler: {
        execute: async (input) => ({
          summary: String((input as { text: string }).text).slice(0, 8),
        }),
      },
    });

    runtime.registerMCPServer({
      serverId: 'mock-search',
      tools: [
        {
          id: 'query',
          description: 'query external knowledge',
          inputSchema: {
            type: 'object',
            properties: {
              query: { type: 'string' },
            },
            required: ['query'],
          },
          handler: {
            execute: async (input) => ({ echoed: input }),
          },
        },
      ],
    });

    const fileResult = await runtime.execute('file.edit', { file: 'demo.ts', instruction: 'sync runtime' }, {
      entryPoint: 'agent',
      agentId: 'tester',
      resources: { editor },
    });
    const searchResult = await runtime.executeSearch('code', { query: 'runtime' }, { entryPoint: 'api' });
    const skillResult = await runtime.executeSkill<{ summary: string }>({
      skillId: 'skill.summarize',
      input: { text: 'runtime summary text' },
      context: { entryPoint: 'agent', agentId: 'tester', sessionId: 'sess-runtime' },
    });
    const mcpResult = await runtime.getMCPGateway().execute(
      'mock-search',
      'query',
      { query: 'docs' },
      { entryPoint: 'frontend' }
    );

    expect(fileResult.ok).toBe(true);
    expect(searchResult.ok).toBe(true);
    expect(skillResult.ok).toBe(true);
    expect(mcpResult.ok).toBe(true);
    expect(runtime.getToolRegistry().has('file.edit')).toBe(true);
    expect(runtime.getToolRegistry().has('search.code')).toBe(true);
    expect(runtime.listSkills().map((skill) => skill.skillId)).toContain('skill.summarize');
    expect(runtime.getToolRegistry().has('mock-search.query')).toBe(true);
    expect(runtime.getToolTraces().map((trace) => trace.toolId)).toEqual([
      'file.edit',
      'search.code',
      'skill.summarize',
      'mock-search.query',
    ]);
  });

  it('registers MCP tools inside gateway and executes through ToolExecutor traces', async () => {
    const registry = new ToolRegistry();
    const executor = new ToolExecutor(registry);
    const gateway = new MCPGateway(registry, executor);

    gateway.registerServer({
      serverId: 'mock-search',
      description: 'mock mcp server',
      tools: [
        {
          id: 'query',
          description: 'query external knowledge',
          inputSchema: {
            type: 'object',
            properties: {
              query: { type: 'string' },
            },
            required: ['query'],
          },
          handler: {
            execute: async (input) => ({
              ok: true,
              echoed: input,
            }),
          },
        },
      ],
    });

    const result = await gateway.execute(
      'mock-search',
      'query',
      { query: 'docs' },
      { entryPoint: 'agent', agentId: 'tester' }
    );

    expect(result.ok).toBe(true);
    expect(result.trace.toolId).toBe('mock-search.query');
    expect(registry.has('mock-search.query')).toBe(true);
  });
});
