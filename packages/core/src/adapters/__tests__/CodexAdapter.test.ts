import { describe, it, expect, beforeEach, vi } from 'vitest';
import { CodexAdapter } from '../CodexAdapter.js';
import { AdapterConfig, APIError } from '../types.js';
import { HookManager } from '../../hooks/HookManager.js';
import { HookEvent } from '../../hooks/types.js';

// Mock OpenAI SDK
vi.mock('openai', () => {
  return {
    default: vi.fn().mockImplementation(() => ({
      chat: {
        completions: {
          create: vi.fn(),
        },
      },
    })),
  };
});

describe('CodexAdapter', () => {
  let adapter: CodexAdapter;
  let mockConfig: AdapterConfig;
  let mockHookManager: HookManager;

  beforeEach(() => {
    mockConfig = {
      apiKey: 'test-api-key',
      model: 'gpt-4',
      temperature: 0.7,
      maxTokens: 4096,
      timeout: 60000,
      maxRetries: 3,
      retryDelay: 1000,
    };

    mockHookManager = {
      hook_before_send: vi.fn().mockResolvedValue({
        messages: [],
        model: mockConfig.model,
        temperature: mockConfig.temperature,
        maxTokens: mockConfig.maxTokens,
      }),
      hook_post_response: vi.fn().mockResolvedValue(undefined),
      hook_on_stream: vi.fn(),
      hook_before_compress: vi.fn().mockResolvedValue({
        entities: ['session'],
        decisions: ['preserve recent turns'],
        relations: [],
      }),
    } as unknown as HookManager;

    adapter = new CodexAdapter(mockConfig, mockHookManager);
  });

  describe('constructor', () => {
    it('should create adapter with config', () => {
      expect(adapter).toBeDefined();
      expect(adapter.getConfig().model).toBe('gpt-4');
    });

    it('should throw error without API key', () => {
      expect(() => new CodexAdapter({ model: 'gpt-4' })).toThrow('OpenAI API key is required');
    });

    it('should use default values for optional config', () => {
      const minimalConfig: AdapterConfig = {
        apiKey: 'test-key',
        model: 'gpt-4',
      };
      const minimalAdapter = new CodexAdapter(minimalConfig);

      const config = minimalAdapter.getConfig();
      expect(config.temperature).toBe(0.7);
      expect(config.maxTokens).toBe(4096);
      expect(config.timeout).toBe(60000);
      expect(config.maxRetries).toBe(3);
      expect(config.retryDelay).toBe(1000);
    });
  });

  describe('send', () => {
    it('should send message and return response', async () => {
      const mockResponse = {
        choices: [
          {
            message: { content: 'Hello, world!' },
            finish_reason: 'stop',
          },
        ],
        model: 'gpt-4',
        usage: {
          prompt_tokens: 10,
          completion_tokens: 5,
          total_tokens: 15,
        },
      };

      const mockClient = (adapter as any).client;
      mockClient.chat.completions.create.mockResolvedValue(mockResponse);

      const response = await adapter.send('Hello');

      expect(response.content).toBe('Hello, world!');
      expect(response.model).toBe('gpt-4');
      expect(response.usage.promptTokens).toBe(10);
      expect(response.usage.completionTokens).toBe(5);
      expect(response.usage.totalTokens).toBe(15);
      expect(response.finishReason).toBe('stop');
    });

    it('applies before_send payload mutations to provider request', async () => {
      const manager = new HookManager();
      manager.register(HookEvent.BEFORE_SEND, async (payload) => ({
        ...payload,
        model: 'hook-model',
        temperature: 0.1,
        maxTokens: 12,
        messages: [
          { role: 'system', content: 'hooked-system', timestamp: Date.now() },
          ...payload.messages,
        ],
      }));
      adapter = new CodexAdapter(mockConfig, manager);

      const mockResponse = {
        choices: [{ message: { content: 'Response' }, finish_reason: 'stop' }],
        model: 'hook-model',
        usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
      };

      const mockClient = (adapter as any).client;
      mockClient.chat.completions.create.mockResolvedValue(mockResponse);

      await adapter.send('Hello');

      expect(mockClient.chat.completions.create).toHaveBeenCalledWith(
        expect.objectContaining({
          model: 'hook-model',
          temperature: 0.1,
          max_tokens: 12,
          messages: expect.arrayContaining([
            expect.objectContaining({ role: 'system', content: 'hooked-system' }),
            expect.objectContaining({ role: 'user', content: 'Hello' }),
          ]),
        })
      );
    });

    it('should add messages to history', async () => {
      const mockResponse = {
        choices: [{ message: { content: 'Response' }, finish_reason: 'stop' }],
        model: 'gpt-4',
        usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
      };

      const mockClient = (adapter as any).client;
      mockClient.chat.completions.create.mockResolvedValue(mockResponse);

      await adapter.send('Hello');

      const history = adapter.getHistory();
      expect(history.length).toBe(2);
      expect(history[0].role).toBe('user');
      expect(history[0].content).toBe('Hello');
      expect(history[1].role).toBe('assistant');
      expect(history[1].content).toBe('Response');
    });

    it('should call hooks', async () => {
      const mockResponse = {
        choices: [{ message: { content: 'Response' }, finish_reason: 'stop' }],
        model: 'gpt-4',
        usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
      };

      const mockClient = (adapter as any).client;
      mockClient.chat.completions.create.mockResolvedValue(mockResponse);

      await adapter.send('Hello');

      expect(mockHookManager.hook_before_send).toHaveBeenCalled();
      expect(mockHookManager.hook_post_response).toHaveBeenCalled();
    });

    it('should handle API errors', async () => {
      const mockClient = (adapter as any).client;
      mockClient.chat.completions.create.mockRejectedValue(
        Object.assign(new Error('API Error'), { status: 500 })
      );

      await expect(adapter.send('Hello')).rejects.toThrow(APIError);
    });

    it('should throw error for stream option', async () => {
      await expect(adapter.send('Hello', { stream: true })).rejects.toThrow(
        'Use stream() for streaming responses'
      );
    });

    it('should handle streaming via stream()', async () => {
      const mockStream = [
        { choices: [{ delta: { content: 'Hello' } }] },
        { choices: [{ delta: { content: ' world' } }] },
        { choices: [{ delta: {} }] },
      ];

      const mockClient = (adapter as any).client;
      mockClient.chat.completions.create.mockResolvedValue({
        [Symbol.asyncIterator]: async function* () {
          for (const chunk of mockStream) {
            yield chunk;
          }
        },
      });

      const deltas: string[] = [];
      let finalDone = false;
      for await (const chunk of adapter.stream('Hello')) {
        if (chunk.delta) {
          deltas.push(chunk.delta);
        }
        if (chunk.done) {
          finalDone = true;
        }
      }

      expect(deltas).toEqual(['Hello', ' world']);
      expect(finalDone).toBe(true);
      expect(mockHookManager.hook_on_stream).toHaveBeenCalled();
      expect(mockHookManager.hook_post_response).toHaveBeenCalled();
      expect(adapter.getHistory().at(-1)?.content).toBe('Hello world');
    });
  });

  describe('receive', () => {
    it('should throw no active stream error', async () => {
      const generator = adapter.receive();
      await expect(generator.next()).rejects.toThrow('No active stream');
    });

    it('should continue active stream and clear state after completion', async () => {
      const mockStream = [
        { choices: [{ delta: { content: 'Hello' } }] },
        { choices: [{ delta: { content: ' world' } }] },
      ];

      const mockClient = (adapter as any).client;
      mockClient.chat.completions.create.mockResolvedValue({
        [Symbol.asyncIterator]: async function* () {
          for (const chunk of mockStream) {
            yield chunk;
          }
        },
      });

      const stream = adapter.stream('Hello');
      const first = await stream.next();
      expect(first.value).toMatchObject({ delta: 'Hello', done: false });

      const received: string[] = [];
      for await (const chunk of adapter.receive()) {
        if (chunk.delta) {
          received.push(chunk.delta);
        }
      }

      expect(received).toEqual([' world']);
      await expect(adapter.receive().next()).rejects.toThrow('No active stream');
    });
  });

  describe('getHistory', () => {
    it('should return copy of history', async () => {
      const mockResponse = {
        choices: [{ message: { content: 'Response' }, finish_reason: 'stop' }],
        model: 'gpt-4',
        usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
      };

      const mockClient = (adapter as any).client;
      mockClient.chat.completions.create.mockResolvedValue(mockResponse);

      await adapter.send('Hello');

      const history1 = adapter.getHistory();
      const history2 = adapter.getHistory();

      expect(history1).not.toBe(history2);
      expect(history1).toEqual(history2);
    });
  });

  describe('setHistory', () => {
    it('should set history', () => {
      const messages = [
        { role: 'user' as const, content: 'Hello', timestamp: Date.now() },
        { role: 'assistant' as const, content: 'Hi', timestamp: Date.now() },
      ];

      adapter.setHistory(messages);

      const history = adapter.getHistory();
      expect(history.length).toBe(2);
      expect(history[0].content).toBe('Hello');
    });
  });

  describe('rewind', () => {
    it('should rewind specified steps by completed turns', async () => {
      const mockResponse = {
        choices: [{ message: { content: 'Response' }, finish_reason: 'stop' }],
        model: 'gpt-4',
        usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
      };

      const mockClient = (adapter as any).client;
      mockClient.chat.completions.create.mockResolvedValue(mockResponse);

      await adapter.send('Message 1');
      await adapter.send('Message 2');

      await adapter.rewind(1);

      const history = adapter.getHistory();
      expect(history).toHaveLength(2);
      expect(history.map((message) => message.content)).toEqual(['Message 1', 'Response']);
    });

    it('should preserve system prompts when rewinding completed turns', async () => {
      adapter.setHistory([
        { role: 'system', content: 'system prompt', timestamp: 1 },
        { role: 'user', content: 'Message 1', timestamp: 2 },
        { role: 'assistant', content: 'Response 1', timestamp: 3 },
        { role: 'user', content: 'Message 2', timestamp: 4 },
        { role: 'assistant', content: 'Response 2', timestamp: 5 },
      ]);

      await adapter.rewind(1);

      expect(adapter.getHistory().map((message) => `${message.role}:${message.content}`)).toEqual([
        'system:system prompt',
        'user:Message 1',
        'assistant:Response 1',
      ]);
    });

    it('should throw error for invalid steps', async () => {
      await expect(adapter.rewind(0)).rejects.toThrow('Steps must be positive');
    });

    it('should throw error when rewinding too many steps', async () => {
      await expect(adapter.rewind(10)).rejects.toThrow('Cannot rewind');
    });
  });

  describe('compact', () => {
    it('should compact history with governance summary and recent turns', async () => {
      const mockResponse = {
        choices: [{ message: { content: 'Response' }, finish_reason: 'stop' }],
        model: 'gpt-4',
        usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
      };

      const mockClient = (adapter as any).client;
      mockClient.chat.completions.create.mockResolvedValue(mockResponse);

      for (let i = 0; i < 15; i++) {
        await adapter.send(`Message ${i}`);
      }

      await adapter.compact();

      const history = adapter.getHistory();
      expect(history[0]).toMatchObject({ role: 'system' });
      expect(history[0]?.content).toContain('[Compressed Context]');
      expect(history.slice(1).length).toBeGreaterThan(0);
      expect(history.at(-1)?.role).toBe('assistant');
      expect(mockHookManager.hook_before_compress).toHaveBeenCalled();
    });
  });

  describe('configure', () => {
    it('should update configuration', () => {
      adapter.configure({ temperature: 0.5, maxTokens: 2048 });

      const config = adapter.getConfig();
      expect(config.temperature).toBe(0.5);
      expect(config.maxTokens).toBe(2048);
    });

    it('should recreate client when apiKey changes', () => {
      const oldClient = (adapter as any).client;
      adapter.configure({ apiKey: 'new-key' });
      const newClient = (adapter as any).client;

      expect(newClient).not.toBe(oldClient);
    });

    it('should recreate client when baseURL changes', () => {
      const oldClient = (adapter as any).client;
      adapter.configure({ baseURL: 'https://new-url.com' });
      const newClient = (adapter as any).client;

      expect(newClient).not.toBe(oldClient);
    });
  });

  describe('getConfig', () => {
    it('should return copy of config', () => {
      const config1 = adapter.getConfig();
      const config2 = adapter.getConfig();

      expect(config1).not.toBe(config2);
      expect(config1).toEqual(config2);
    });
  });

  describe('error handling', () => {
    it('should wrap errors as APIError', async () => {
      const mockClient = (adapter as any).client;
      mockClient.chat.completions.create.mockRejectedValue(new Error('Test error'));

      await expect(adapter.send('Hello')).rejects.toThrow(APIError);
    });

    it('should mark rate limit errors as retryable', async () => {
      const mockClient = (adapter as any).client;
      mockClient.chat.completions.create.mockRejectedValue(
        Object.assign(new Error('Rate limit'), { status: 429 })
      );

      try {
        await adapter.send('Hello');
      } catch (error) {
        expect(error).toBeInstanceOf(APIError);
        expect((error as APIError).retryable).toBe(true);
      }
    });

    it('should mark 503 errors as retryable', async () => {
      const mockClient = (adapter as any).client;
      mockClient.chat.completions.create.mockRejectedValue(
        Object.assign(new Error('Service unavailable'), { status: 503 })
      );

      try {
        await adapter.send('Hello');
      } catch (error) {
        expect(error).toBeInstanceOf(APIError);
        expect((error as APIError).retryable).toBe(true);
      }
    });

    it('should mark timeout errors as retryable', async () => {
      const mockClient = (adapter as any).client;
      mockClient.chat.completions.create.mockRejectedValue(new Error('timeout'));

      try {
        await adapter.send('Hello');
      } catch (error) {
        expect(error).toBeInstanceOf(APIError);
        expect((error as APIError).retryable).toBe(true);
      }
    });
  });
});

