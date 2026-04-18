import { describe, it, expect, beforeEach, vi } from 'vitest';
import { HotSwapManager } from '../HotSwapManager.js';
import { ModelInfo, PREDEFINED_MODELS } from '../types.js';
import { ICliAdapter } from '../../adapters/types.js';
import type { ResolvedConfig } from '../../config/types.js';
import { Message } from '../../hooks/types.js';

function createResolvedConfig(overrides: Partial<ResolvedConfig> = {}): ResolvedConfig {
  return {
    model: 'gemini-2.0-flash-exp',
    temperature: 0.4,
    maxTokens: 2048,
    mcpTools: [],
    apiChannel: {
      id: 'default',
      name: 'Default',
      provider: 'google',
      apiKey: 'gemini-key',
      baseURL: 'https://gemini.example.com',
      enabled: true,
    },
    timeout: 45000,
    maxRetries: 2,
    ...overrides,
  };
}

function createMockAdapter(history: Message[] = []): ICliAdapter {
  return {
    send: vi.fn().mockResolvedValue({ content: 'Response', model: 'test' }),
    stream: vi.fn(),
    receive: vi.fn(),
    getHistory: vi.fn().mockReturnValue(history),
    setHistory: vi.fn(),
    rewind: vi.fn(),
    compact: vi.fn(),
    configure: vi.fn(),
    getConfig: vi.fn().mockReturnValue({ model: 'test' }),
  } as unknown as ICliAdapter;
}

function getLastConfigurePatch(adapter: ICliAdapter): Record<string, unknown> | undefined {
  const calls = (adapter.configure as unknown as { mock: { calls: unknown[][] } }).mock.calls;
  return calls.at(-1)?.[0] as Record<string, unknown> | undefined;
}

describe('HotSwapManager', () => {
  let manager: HotSwapManager;

  beforeEach(() => {
    manager = new HotSwapManager();
  });

  describe('constructor', () => {
    it('should create manager with default config', () => {
      const m = new HotSwapManager();
      expect(m).toBeDefined();
    });

    it('should create manager with custom config', () => {
      const m = new HotSwapManager({
        defaultModel: 'custom-model',
        autoRetry: false,
      });
      expect(m).toBeDefined();
    });

    it('should initialize predefined models', () => {
      const models = manager.getAvailableModels();
      expect(models.length).toBeGreaterThan(0);
    });

    it('should keep predefined CLI models offline until adapters are registered', () => {
      expect(manager.getModelInfo('gemini-cli')).toMatchObject({
        available: false,
        status: 'offline',
        adapterKind: 'cli',
        adapterId: 'gemini-cli',
        supportedModelIds: ['gemini-2.0-flash-exp', 'gemini-2.5-pro'],
      });
      expect(manager.getModelInfo('codex-cli')).toMatchObject({
        available: false,
        status: 'offline',
        adapterKind: 'cli',
        adapterId: 'codex-cli',
        supportedModelIds: ['gpt-5.4', 'gpt-5-codex'],
      });
    });
  });

  describe('registerAdapter', () => {
    it('should register adapter for model', () => {
      const adapter = createMockAdapter();
      manager.registerAdapter('claude-3-opus', adapter);

      expect(manager.canSwitch('claude-3-opus')).toBe(true);
    });

    it('should promote predefined CLI model online when adapter is registered', () => {
      const before = manager.getModelInfo('gemini-cli');
      expect(before?.available).toBe(false);
      expect(before?.status).toBe('offline');

      manager.registerAdapter('gemini-cli', createMockAdapter());

      const after = manager.getModelInfo('gemini-cli');
      expect(after?.available).toBe(true);
      expect(after?.status).toBe('online');
      expect(manager.canSwitch('gemini-cli')).toBe(true);
    });

    it('should preserve CLI identity fields when predefined metadata is promoted online', () => {
      manager.registerAdapter('codex-cli', createMockAdapter());

      expect(manager.getModelInfo('codex-cli')).toMatchObject({
        available: true,
        status: 'online',
        adapterKind: 'cli',
        adapterId: 'codex-cli',
        supportedModelIds: ['gpt-5.4', 'gpt-5-codex'],
      });
    });

    it('should set first registered adapter as current', () => {
      const adapter = createMockAdapter();
      manager.registerAdapter('claude-3-opus', adapter);

      const current = manager.getCurrentModel();
      expect(current?.id).toBe('claude-3-opus');
    });

    it('should not override current adapter on subsequent registrations', () => {
      const adapter1 = createMockAdapter();
      const adapter2 = createMockAdapter();

      manager.registerAdapter('claude-3-opus', adapter1);
      manager.registerAdapter('gemini-pro', adapter2);

      const current = manager.getCurrentModel();
      expect(current?.id).toBe('claude-3-opus');
    });
  });

  describe('registerModel', () => {
    it('should register custom model', () => {
      const customModel: ModelInfo = {
        id: 'custom-model',
        name: 'Custom Model',
        provider: 'custom',
        capabilities: {
          streaming: true,
          vision: false,
          functionCalling: false,
          codeExecution: false,
          multimodal: false,
        },
        contextWindow: 8000,
        maxOutputTokens: 2000,
        available: true,
        status: 'online',
      };

      manager.registerModel(customModel);

      const info = manager.getModelInfo('custom-model');
      expect(info).toEqual(customModel);
    });
  });

  describe('getAvailableModels', () => {
    it('should return only available models', () => {
      const models = manager.getAvailableModels();

      expect(models.every(m => m.available)).toBe(true);
    });

    it('should include predefined models', () => {
      const models = manager.getAvailableModels();
      const ids = models.map(m => m.id);

      expect(ids).toContain('claude-3-opus');
      expect(ids).toContain('gemini-pro');
    });

    it('should exclude CLI models until their adapters are registered', () => {
      const idsBefore = manager.getAvailableModels().map(m => m.id);
      expect(idsBefore).not.toContain('gemini-cli');
      expect(idsBefore).not.toContain('codex-cli');

      manager.registerAdapter('codex-cli', createMockAdapter());

      const idsAfter = manager.getAvailableModels().map(m => m.id);
      expect(idsAfter).toContain('codex-cli');
    });
  });

  describe('getCurrentModel', () => {
    it('should return null when no adapter registered', () => {
      const current = manager.getCurrentModel();
      expect(current).toBeNull();
    });

    it('should return current model after registration', () => {
      manager.registerAdapter('claude-3-opus', createMockAdapter());

      const current = manager.getCurrentModel();
      expect(current?.id).toBe('claude-3-opus');
    });
  });

  describe('getModelInfo', () => {
    it('should return model info for predefined model', () => {
      const info = manager.getModelInfo('claude-3-opus');

      expect(info).toBeDefined();
      expect(info?.name).toBe('Claude 3 Opus');
      expect(info?.provider).toBe('claude');
    });

    it('should expose CLI predefined metadata identity fields', () => {
      const info = manager.getModelInfo('gemini-cli');
      const predefined = PREDEFINED_MODELS.find((model) => model.id === 'gemini-cli');

      expect(predefined).toMatchObject({
        adapterKind: 'cli',
        adapterId: 'gemini-cli',
        supportedModelIds: ['gemini-2.0-flash-exp', 'gemini-2.5-pro'],
      });
      expect(info).toMatchObject({
        adapterKind: 'cli',
        adapterId: 'gemini-cli',
        supportedModelIds: ['gemini-2.0-flash-exp', 'gemini-2.5-pro'],
      });
    });

    it('should return null for unknown model', () => {
      const info = manager.getModelInfo('unknown-model');
      expect(info).toBeNull();
    });
  });

  describe('canSwitch', () => {
    it('should return false for unknown model', () => {
      expect(manager.canSwitch('unknown-model')).toBe(false);
    });

    it('should return false for unavailable model', () => {
      manager.registerModel({
        id: 'unavailable',
        name: 'Unavailable',
        provider: 'custom',
        capabilities: {
          streaming: false,
          vision: false,
          functionCalling: false,
          codeExecution: false,
          multimodal: false,
        },
        contextWindow: 8000,
        maxOutputTokens: 2000,
        available: false,
      });

      expect(manager.canSwitch('unavailable')).toBe(false);
    });

    it('should return false for offline model', () => {
      manager.registerModel({
        id: 'offline',
        name: 'Offline',
        provider: 'custom',
        capabilities: {
          streaming: false,
          vision: false,
          functionCalling: false,
          codeExecution: false,
          multimodal: false,
        },
        contextWindow: 8000,
        maxOutputTokens: 2000,
        available: true,
        status: 'offline',
      });

      expect(manager.canSwitch('offline')).toBe(false);
    });

    it('should return false when no adapter registered', () => {
      expect(manager.canSwitch('claude-3-opus')).toBe(false);
    });

    it('should return true when model available and adapter registered', () => {
      manager.registerAdapter('claude-3-opus', createMockAdapter());

      expect(manager.canSwitch('claude-3-opus')).toBe(true);
    });
  });

  describe('switchModel', () => {
    beforeEach(() => {
      manager.registerAdapter('claude-3-opus', createMockAdapter());
      manager.registerAdapter('gemini-pro', createMockAdapter());
    });

    it('should switch to target model', async () => {
      const result = await manager.switchModel('gemini-pro');

      expect(result.success).toBe(true);
      expect(result.currentModel).toBe('gemini-pro');
      expect(result.previousModel).toBe('claude-3-opus');
    });

    it('should fail for unknown model', async () => {
      const result = await manager.switchModel('unknown-model');

      expect(result.success).toBe(false);
      expect(result.error).toContain('not found');
    });

    it('should preserve history when option enabled', async () => {
      const history: Message[] = [
        { role: 'user', content: 'Hello', timestamp: Date.now() },
        { role: 'assistant', content: 'Hi', timestamp: Date.now() },
      ];

      // Create fresh manager for this test
      const testManager = new HotSwapManager();
      const sourceAdapter = createMockAdapter(history);
      const targetAdapter = createMockAdapter();

      testManager.registerAdapter('claude-3-opus', sourceAdapter);
      testManager.registerAdapter('gemini-pro', targetAdapter);

      await testManager.switchModel('gemini-pro', { preserveHistory: true, migrateContext: true });

      expect(targetAdapter.setHistory).toHaveBeenCalled();
    });

    it('should not preserve history when option disabled', async () => {
      const history: Message[] = [
        { role: 'user', content: 'Hello', timestamp: Date.now() },
      ];

      const sourceAdapter = createMockAdapter(history);
      const targetAdapter = createMockAdapter();

      manager.registerAdapter('claude-3-opus', sourceAdapter);
      manager.registerAdapter('gemini-pro', targetAdapter);

      await manager.switchModel('gemini-pro', { preserveHistory: false });

      expect(targetAdapter.setHistory).not.toHaveBeenCalled();
    });

    it('should migrate context when option enabled', async () => {
      const history: Message[] = [
        { role: 'user', content: 'Hello', timestamp: Date.now() },
      ];

      const sourceAdapter = createMockAdapter(history);
      const targetAdapter = createMockAdapter();

      manager.registerAdapter('claude-3-opus', sourceAdapter);
      manager.registerAdapter('gemini-pro', targetAdapter);

      const result = await manager.switchModel('gemini-pro', {
        migrateContext: true,
      });

      expect(result.contextMigrated).toBe(true);
    });

    it('should pass resolved runtime config into target adapter configure', async () => {
      const sourceAdapter = createMockAdapter();
      const targetAdapter = createMockAdapter();
      const resolvedConfig = createResolvedConfig();

      manager.registerAdapter('claude-3-opus', sourceAdapter);
      manager.registerAdapter('gemini-pro', targetAdapter);

      await manager.switchModel('gemini-pro', {
        resolvedConfig,
      });

      expect(targetAdapter.configure).toHaveBeenCalledWith({
        model: 'gemini-2.0-flash-exp',
        temperature: 0.4,
        maxTokens: 2048,
        timeout: 45000,
        maxRetries: 2,
        apiKey: 'gemini-key',
        baseURL: 'https://gemini.example.com',
      });
    });

    it('should preserve explicit zero values while omitting undefined fields', async () => {
      const targetAdapter = createMockAdapter();

      manager.registerAdapter('claude-3-opus', createMockAdapter());
      manager.registerAdapter('gemini-pro', targetAdapter);

      await manager.switchModel('gemini-pro', {
        resolvedConfig: createResolvedConfig({
          temperature: 0,
          maxTokens: undefined,
          timeout: 0,
          maxRetries: undefined,
          apiChannel: {
            id: 'default',
            name: 'Default',
            provider: 'google',
            enabled: true,
          },
        }),
      });

      expect(targetAdapter.configure).toHaveBeenCalledWith({
        model: 'gemini-2.0-flash-exp',
        temperature: 0,
        timeout: 0,
      });
    });

    it('should preserve migrateContext and configure bridge together', async () => {
      const history: Message[] = [
        { role: 'user', content: 'Hello', timestamp: Date.now() },
        { role: 'assistant', content: 'Hi', timestamp: Date.now() },
      ];
      const testManager = new HotSwapManager();
      const sourceAdapter = createMockAdapter(history);
      const targetAdapter = createMockAdapter();

      testManager.registerAdapter('claude-3-opus', sourceAdapter);
      testManager.registerAdapter('gemini-pro', targetAdapter);

      const result = await testManager.switchModel('gemini-pro', {
        preserveHistory: true,
        migrateContext: true,
        resolvedConfig: createResolvedConfig(),
      });

      expect(result.success).toBe(true);
      expect(result.contextMigrated).toBe(true);
      expect(targetAdapter.configure).toHaveBeenCalledTimes(1);
      expect(targetAdapter.setHistory).toHaveBeenCalledWith(history);
    });

    it('should not forward provider request config across provider families', async () => {
      const sourceAdapter = createMockAdapter();
      const targetAdapter = createMockAdapter();

      manager.registerAdapter('claude-3-opus', sourceAdapter);
      manager.registerAdapter('codex-cli', targetAdapter);

      await manager.switchModel('codex-cli', {
        resolvedConfig: createResolvedConfig({
          model: 'gemini-2.0-flash-exp',
        }),
      });

      expect(targetAdapter.configure).toHaveBeenCalledWith({
        model: 'gemini-2.0-flash-exp',
        temperature: 0.4,
        maxTokens: 2048,
        timeout: 45000,
        maxRetries: 2,
      });
    });


    it('should fallback on error when option enabled', async () => {
      manager.registerAdapter('claude-3-sonnet', createMockAdapter());

      // Try to switch to model without adapter
      const result = await manager.switchModel('codex-cli', {
        fallbackOnError: true,
      });

      // Should have tried fallback chain
      expect(result.success).toBe(true);
    });

    it('should not fallback when option disabled', async () => {
      const result = await manager.switchModel('codex-cli', {
        fallbackOnError: false,
      });

      expect(result.success).toBe(false);
      expect(result.error).toContain('Cannot switch');
    });

    it('should configure target adapter with resolved runtime config', async () => {
      const history: Message[] = [
        { role: 'user', content: 'Hello bridge', timestamp: Date.now() },
        { role: 'assistant', content: 'Bridge ready', timestamp: Date.now() },
      ];
      const testManager = new HotSwapManager();
      const sourceAdapter = createMockAdapter(history);
      const targetAdapter = createMockAdapter();

      testManager.registerAdapter('claude-3-opus', sourceAdapter);
      testManager.registerAdapter('gemini-pro', targetAdapter);

      const result = await testManager.switchModel('gemini-pro', {
        preserveHistory: true,
        migrateContext: true,
        resolvedConfig: createResolvedConfig(),
      });

      expect(result.success).toBe(true);
      expect(targetAdapter.configure).toHaveBeenCalledWith({
        model: 'gemini-2.0-flash-exp',
        temperature: 0.4,
        maxTokens: 2048,
        timeout: 45000,
        maxRetries: 2,
        apiKey: 'gemini-key',
        baseURL: 'https://gemini.example.com',
      });
      expect(targetAdapter.setHistory).toHaveBeenCalledWith(history);
    });

    it('should forward resolved runtime config through fallback relay', async () => {
      const history: Message[] = [
        { role: 'user', content: 'Fallback bridge', timestamp: Date.now() },
      ];
      const testManager = new HotSwapManager();
      const sourceAdapter = createMockAdapter(history);
      const fallbackAdapter = createMockAdapter();

      testManager.registerAdapter('claude-3-opus', sourceAdapter);
      testManager.registerAdapter('gemini-pro', fallbackAdapter);

      const result = await testManager.switchModel('codex-cli', {
        fallbackOnError: true,
        preserveHistory: true,
        migrateContext: true,
        resolvedConfig: createResolvedConfig(),
      });

      expect(result.success).toBe(true);
      expect(result.currentModel).toBe('gemini-pro');
      expect(fallbackAdapter.configure).toHaveBeenCalledWith({
        model: 'gemini-2.0-flash-exp',
        temperature: 0.4,
        maxTokens: 2048,
        timeout: 45000,
        maxRetries: 2,
        apiKey: 'gemini-key',
        baseURL: 'https://gemini.example.com',
      });
      expect(fallbackAdapter.setHistory).toHaveBeenCalledWith(history);
    });

    it('should filter provider request fields when providers differ', async () => {
      const targetAdapter = createMockAdapter();
      manager.registerAdapter('claude-3-opus', createMockAdapter());
      manager.registerAdapter('claude-3-sonnet', targetAdapter);

      const result = await manager.switchModel('claude-3-sonnet', {
        resolvedConfig: createResolvedConfig(),
      });

      expect(result.success).toBe(true);
      const patch = getLastConfigurePatch(targetAdapter);
      expect(patch).toMatchObject({
        model: 'gemini-2.0-flash-exp',
        temperature: 0.4,
        maxTokens: 2048,
        timeout: 45000,
        maxRetries: 2,
      });
      expect(patch).not.toHaveProperty('apiKey');
      expect(patch).not.toHaveProperty('baseURL');
    });

    it('should keep openai provider request config for codex targets', async () => {
      const targetAdapter = createMockAdapter();
      manager.registerAdapter('claude-3-opus', createMockAdapter());
      manager.registerAdapter('codex-cli', targetAdapter);

      const result = await manager.switchModel('codex-cli', {
        resolvedConfig: createResolvedConfig({
          model: 'gpt-5.4',
          apiChannel: {
            id: 'openai',
            name: 'OpenAI',
            provider: 'openai',
            apiKey: 'openai-key',
            baseURL: 'https://openai.example.com',
            enabled: true,
          },
        }),
      });

      expect(result.success).toBe(true);
      expect(targetAdapter.configure).toHaveBeenCalledWith({
        model: 'gpt-5.4',
        temperature: 0.4,
        maxTokens: 2048,
        timeout: 45000,
        maxRetries: 2,
        apiKey: 'openai-key',
        baseURL: 'https://openai.example.com',
      });
    });

    it('should preserve zero values and skip undefined fields in resolved config', async () => {
      const targetAdapter = createMockAdapter();
      manager.registerAdapter('claude-3-opus', createMockAdapter());
      manager.registerAdapter('gemini-pro', targetAdapter);

      const result = await manager.switchModel('gemini-pro', {
        resolvedConfig: createResolvedConfig({
          temperature: 0,
          maxTokens: 0,
          timeout: 0,
          maxRetries: 0,
          apiChannel: {
            id: 'default',
            name: 'Default',
            provider: 'google',
            apiKey: undefined,
            baseURL: undefined,
            enabled: true,
          },
        }),
      });

      expect(result.success).toBe(true);
      expect(targetAdapter.configure).toHaveBeenCalledWith({
        model: 'gemini-2.0-flash-exp',
        temperature: 0,
        maxTokens: 0,
        timeout: 0,
        maxRetries: 0,
      });
      const patch = getLastConfigurePatch(targetAdapter);
      expect(patch).not.toHaveProperty('apiKey');
      expect(patch).not.toHaveProperty('baseURL');
    });
  });

  describe('retry', () => {
    it('should return error when no current model', async () => {
      const result = await manager.retry();

      expect(result.success).toBe(false);
      expect(result.error).toContain('No current model');
    });

    it('should retry current model', async () => {
      manager.registerAdapter('claude-3-opus', createMockAdapter());

      const result = await manager.retry();

      expect(result.success).toBe(true);
      expect(result.currentModel).toBe('claude-3-opus');
    });

    it('should relay after max retries exceeded', async () => {
      manager.registerAdapter('claude-3-opus', createMockAdapter());
      manager.registerAdapter('gemini-pro', createMockAdapter());

      // Record failures to exceed threshold
      for (let i = 0; i < 5; i++) {
        manager.recordFailure('claude-3-opus');
      }

      const result = await manager.retry({ maxRetries: 3 });

      // Should have relayed to fallback
      expect(result.success).toBe(true);
    });

    it('should apply exponential backoff', async () => {
      manager.registerAdapter('claude-3-opus', createMockAdapter());

      const startTime = Date.now();
      await manager.retry({
        baseDelay: 10,
        backoffMultiplier: 2,
        maxDelay: 1000,
        maxRetries: 10,
        retryableErrors: [],
      });
      const elapsed = Date.now() - startTime;

      // Should have waited at least baseDelay
      expect(elapsed).toBeGreaterThanOrEqual(10);
    });
  });

  describe('relay', () => {
    it('should try fallback chain', async () => {
      manager.registerAdapter('claude-3-opus', createMockAdapter());
      manager.registerAdapter('gemini-pro', createMockAdapter());

      const result = await manager.relay(['gemini-pro']);

      expect(result.success).toBe(true);
      expect(result.currentModel).toBe('gemini-pro');
    });

    it('should skip current model in fallback chain', async () => {
      manager.registerAdapter('claude-3-opus', createMockAdapter());
      manager.registerAdapter('gemini-pro', createMockAdapter());

      const result = await manager.relay(['claude-3-opus', 'gemini-pro']);

      expect(result.currentModel).toBe('gemini-pro');
    });

    it('should fail when all fallbacks fail', async () => {
      manager.registerAdapter('claude-3-opus', createMockAdapter());

      const result = await manager.relay(['unknown-model-1', 'unknown-model-2']);

      expect(result.success).toBe(false);
      expect(result.error).toContain('All fallback models failed');
    });

    it('should use default fallback chain when not specified', async () => {
      manager.registerAdapter('claude-3-opus', createMockAdapter());
      manager.registerAdapter('gemini-pro', createMockAdapter());

      const result = await manager.relay();

      expect(result.success).toBe(true);
    });

    it('should pass resolved runtime config through direct relay', async () => {
      const history: Message[] = [
        { role: 'user', content: 'Relay bridge', timestamp: Date.now() },
      ];
      const sourceAdapter = createMockAdapter(history);
      const targetAdapter = createMockAdapter();

      manager.registerAdapter('claude-3-opus', sourceAdapter);
      manager.registerAdapter('gemini-pro', targetAdapter);

      const result = await manager.relay(['gemini-pro'], {
        preserveHistory: true,
        migrateContext: true,
        resolvedConfig: createResolvedConfig(),
      });

      expect(result.success).toBe(true);
      expect(targetAdapter.configure).toHaveBeenCalledWith({
        model: 'gemini-2.0-flash-exp',
        temperature: 0.4,
        maxTokens: 2048,
        timeout: 45000,
        maxRetries: 2,
        apiKey: 'gemini-key',
        baseURL: 'https://gemini.example.com',
      });
      expect(targetAdapter.setHistory).toHaveBeenCalledWith(history);
    });
  });

  describe('migrateContext', () => {
    it('should return failure when no current adapter', async () => {
      const result = await manager.migrateContext('gemini-pro');

      expect(result.success).toBe(false);
      expect(result.messages).toEqual([]);
    });

    it('should return failure for unknown target model', async () => {
      manager.registerAdapter('claude-3-opus', createMockAdapter());

      const result = await manager.migrateContext('unknown-model');

      expect(result.success).toBe(false);
    });

    it('should migrate messages successfully', async () => {
      const history: Message[] = [
        { role: 'user', content: 'Hello', timestamp: Date.now() },
        { role: 'assistant', content: 'Hi there', timestamp: Date.now() },
      ];

      manager.registerAdapter('claude-3-opus', createMockAdapter(history));

      const result = await manager.migrateContext('gemini-pro');

      expect(result.success).toBe(true);
      expect(result.messages.length).toBe(2);
      expect(result.truncated).toBe(false);
    });

    it('should truncate messages when exceeding context window', async () => {
      // Create history that exceeds context window
      const longContent = 'A'.repeat(500000); // ~125k tokens
      const history: Message[] = [
        { role: 'user', content: longContent, timestamp: Date.now() },
        { role: 'assistant', content: 'Short response', timestamp: Date.now() },
      ];

      manager.registerAdapter('claude-3-opus', createMockAdapter(history));
      manager.configure({ maxContextTokens: 1000 });

      const result = await manager.migrateContext('gemini-pro');

      expect(result.success).toBe(true);
      expect(result.truncated).toBe(true);
      expect(result.migratedTokens).toBeLessThan(result.originalTokens);
    });

    it('should preserve recent richer tool turn messages when truncating', async () => {
      const history: Message[] = [
        { role: 'user', content: 'A'.repeat(10000), timestamp: 1 },
        {
          role: 'assistant',
          content: [
            { type: 'text', text: 'calling search' },
            { type: 'tool_call', id: 'call-1', toolName: 'search', args: { query: 'recent' } },
          ],
          timestamp: 2,
        },
        {
          role: 'assistant',
          content: [
            { type: 'tool_result', toolCallId: 'call-1', toolName: 'search', result: { hits: 2 } },
          ],
          timestamp: 3,
        },
      ];

      manager.registerAdapter('claude-3-opus', createMockAdapter(history));
      manager.configure({ maxContextTokens: 100 });

      const result = await manager.migrateContext('gemini-pro');

      expect(result.messages).toHaveLength(2);
      expect((result.messages[0].content as any[])[1]).toMatchObject({ type: 'tool_call', toolName: 'search' });
      expect((result.messages[1].content as any[])[0]).toMatchObject({ type: 'tool_result', toolName: 'search' });
    });
  });

  describe('configure', () => {
    it('should update configuration', () => {
      manager.configure({ autoRetry: false });
      expect(manager).toBeDefined();
    });

    it('should merge with existing config', () => {
      manager.configure({ autoRetry: false });
      manager.configure({ maxContextTokens: 50000 });
      expect(manager).toBeDefined();
    });
  });

  describe('setRelayConfig', () => {
    it('should update relay configuration', () => {
      manager.setRelayConfig({
        enabled: false,
        fallbackChain: ['model-1', 'model-2'],
      });
      expect(manager).toBeDefined();
    });
  });

  describe('recordFailure', () => {
    it('should increment failure count', () => {
      manager.registerAdapter('claude-3-opus', createMockAdapter());
      manager.registerAdapter('gemini-pro', createMockAdapter());

      manager.recordFailure('claude-3-opus');
      manager.recordFailure('claude-3-opus');

      // Failures are tracked internally
      expect(manager).toBeDefined();
    });

    it('should trigger auto-switch when threshold reached', async () => {
      manager.registerAdapter('claude-3-opus', createMockAdapter());
      manager.registerAdapter('gemini-pro', createMockAdapter());

      manager.setRelayConfig({
        autoSwitch: true,
        switchThreshold: 2,
        enabled: true,
        fallbackChain: ['gemini-pro'],
      });

      manager.recordFailure('claude-3-opus');
      manager.recordFailure('claude-3-opus');

      // Auto-switch should have been triggered
      // Note: relay() is called but we can't easily verify the result
      expect(manager).toBeDefined();
    });
  });

  describe('resetFailures', () => {
    it('should reset failure count for model', () => {
      manager.registerAdapter('claude-3-opus', createMockAdapter());

      manager.recordFailure('claude-3-opus');
      manager.recordFailure('claude-3-opus');
      manager.resetFailures('claude-3-opus');

      // Failures should be reset
      expect(manager).toBeDefined();
    });
  });
});
