import { describe, it, expect, beforeEach, vi } from 'vitest';
import { ClaudeAdapter } from '../ClaudeAdapter.js';
import { AdapterConfig, APIError, TimeoutError } from '../types.js';
import { HookManager } from '../../hooks/HookManager.js';

// Mock Anthropic SDK
vi.mock('@anthropic-ai/sdk', () => {
  return {
    default: vi.fn().mockImplementation(() => ({
      messages: {
        create: vi.fn(),
      },
    })),
  };
});

describe('ClaudeAdapter', () => {
  let adapter: ClaudeAdapter;
  let mockConfig: AdapterConfig;
  let mockHookManager: HookManager;

  beforeEach(() => {
    mockConfig = {
      apiKey: 'test-api-key',
      model: 'claude-3-5-sonnet-20241022',
      temperature: 1.0,
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
        entities: [],
        decisions: [],
        relations: [],
      }),
    } as unknown as HookManager;

    adapter = new ClaudeAdapter(mockConfig, mockHookManager);
  });

  describe('constructor', () => {
    it('should create adapter with config', () => {
      expect(adapter).toBeDefined();
      expect(adapter.getConfig().model).toBe('claude-3-5-sonnet-20241022');
    });

    it('should use default values for optional config', () => {
      const minimalConfig: AdapterConfig = {
        apiKey: 'test-key',
        model: 'claude-3-5-sonnet-20241022',
      };
      const minimalAdapter = new ClaudeAdapter(minimalConfig);

      const config = minimalAdapter.getConfig();
      expect(config.maxRetries).toBe(3);
      expect(config.retryDelay).toBe(1000);
      expect(config.timeout).toBe(60000);
      expect(config.temperature).toBe(1.0);
      expect(config.maxTokens).toBe(4096);
    });
  });

  describe('send', () => {
    it('should send message and return response', async () => {
      const mockResponse = {
        content: [{ type: 'text', text: 'Hello, world!' }],
        model: 'claude-3-5-sonnet-20241022',
        usage: {
          input_tokens: 10,
          output_tokens: 5,
        },
        stop_reason: 'end_turn',
      };

      const mockClient = (adapter as any).client;
      mockClient.messages.create.mockResolvedValue(mockResponse);

      const response = await adapter.send('Hello');

      expect(response.content).toBe('Hello, world!');
      expect(response.model).toBe('claude-3-5-sonnet-20241022');
      expect(response.usage.promptTokens).toBe(10);
      expect(response.usage.completionTokens).toBe(5);
      expect(response.usage.totalTokens).toBe(15);
      expect(response.finishReason).toBe('end_turn');
    });

    it('should add messages to history', async () => {
      const mockResponse = {
        content: [{ type: 'text', text: 'Response' }],
        model: 'claude-3-5-sonnet-20241022',
        usage: { input_tokens: 10, output_tokens: 5 },
        stop_reason: 'end_turn',
      };

      const mockClient = (adapter as any).client;
      mockClient.messages.create.mockResolvedValue(mockResponse);

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
        content: [{ type: 'text', text: 'Response' }],
        model: 'claude-3-5-sonnet-20241022',
        usage: { input_tokens: 10, output_tokens: 5 },
        stop_reason: 'end_turn',
      };

      const mockClient = (adapter as any).client;
      mockClient.messages.create.mockResolvedValue(mockResponse);

      await adapter.send('Hello');

      expect(mockHookManager.hook_before_send).toHaveBeenCalled();
      expect(mockHookManager.hook_post_response).toHaveBeenCalled();
    });

    it('should handle API errors', async () => {
      const mockClient = (adapter as any).client;
      mockClient.messages.create.mockRejectedValue(
        Object.assign(new Error('API Error'), { status: 500 })
      );

      await expect(adapter.send('Hello')).rejects.toThrow(APIError);
    });

    it('should retry on retryable errors', async () => {
      const mockClient = (adapter as any).client;
      mockClient.messages.create
        .mockRejectedValueOnce(Object.assign(new Error('Rate limit'), { status: 429 }))
        .mockResolvedValueOnce({
          content: [{ type: 'text', text: 'Success' }],
          model: 'claude-3-5-sonnet-20241022',
          usage: { input_tokens: 10, output_tokens: 5 },
          stop_reason: 'end_turn',
        });

      const response = await adapter.send('Hello');
      expect(response.content).toBe('Success');
      expect(mockClient.messages.create).toHaveBeenCalledTimes(2);
    });

    it('should timeout after specified duration', async () => {
      const mockClient = (adapter as any).client;
      mockClient.messages.create.mockImplementation(
        () => new Promise((resolve) => setTimeout(resolve, 100000))
      );

      await expect(adapter.send('Hello', { timeout: 100 })).rejects.toThrow(TimeoutError);
    });

    it('should throw error for stream option', async () => {
      await expect(adapter.send('Hello', { stream: true })).rejects.toThrow(
        'Use receive() for streaming responses'
      );
    });
  });

  describe('stream', () => {
    it('should stream response chunks', async () => {
      const mockStream = [
        { type: 'content_block_delta', delta: { type: 'text_delta', text: 'Hello' } },
        { type: 'content_block_delta', delta: { type: 'text_delta', text: ' world' } },
        { type: 'message_stop' },
      ];

      const mockClient = (adapter as any).client;
      mockClient.messages.create.mockResolvedValue({
        [Symbol.asyncIterator]: async function* () {
          for (const event of mockStream) {
            yield event;
          }
        },
      });

      const chunks: string[] = [];
      for await (const chunk of adapter.stream('Hello')) {
        if (chunk.delta) {
          chunks.push(chunk.delta);
        }
      }

      expect(chunks).toEqual(['Hello', ' world']);
    });

    it('should add streamed message to history', async () => {
      const mockStream = [
        { type: 'content_block_delta', delta: { type: 'text_delta', text: 'Hello' } },
        { type: 'message_stop' },
      ];

      const mockClient = (adapter as any).client;
      mockClient.messages.create.mockResolvedValue({
        [Symbol.asyncIterator]: async function* () {
          for (const event of mockStream) {
            yield event;
          }
        },
      });

      for await (const _ of adapter.stream('Test')) {
        // Consume stream
      }

      const history = adapter.getHistory();
      expect(history.length).toBe(2);
      expect(history[1].content).toBe('Hello');
    });

    it('should call stream hooks', async () => {
      const mockStream = [
        { type: 'content_block_delta', delta: { type: 'text_delta', text: 'Test' } },
        { type: 'message_stop' },
      ];

      const mockClient = (adapter as any).client;
      mockClient.messages.create.mockResolvedValue({
        [Symbol.asyncIterator]: async function* () {
          for (const event of mockStream) {
            yield event;
          }
        },
      });

      for await (const _ of adapter.stream('Hello')) {
        // Consume stream
      }

      expect(mockHookManager.hook_on_stream).toHaveBeenCalled();
      expect(mockHookManager.hook_post_response).toHaveBeenCalled();
    });
  });

  describe('receive', () => {
    it('should throw error when no active stream', async () => {
      const generator = adapter.receive();
      await expect(generator.next()).rejects.toThrow('No active stream');
    });
  });

  describe('getHistory', () => {
    it('should return copy of history', async () => {
      const mockResponse = {
        content: [{ type: 'text', text: 'Response' }],
        model: 'claude-3-5-sonnet-20241022',
        usage: { input_tokens: 10, output_tokens: 5 },
        stop_reason: 'end_turn',
      };

      const mockClient = (adapter as any).client;
      mockClient.messages.create.mockResolvedValue(mockResponse);

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
    it('should rewind specified steps', async () => {
      const mockResponse = {
        content: [{ type: 'text', text: 'Response' }],
        model: 'claude-3-5-sonnet-20241022',
        usage: { input_tokens: 10, output_tokens: 5 },
        stop_reason: 'end_turn',
      };

      const mockClient = (adapter as any).client;
      mockClient.messages.create.mockResolvedValue(mockResponse);

      await adapter.send('Message 1');
      await adapter.send('Message 2');

      await adapter.rewind(1);

      const history = adapter.getHistory();
      expect(history.length).toBe(2);
    });

    it('should throw error for invalid steps', async () => {
      await expect(adapter.rewind(0)).rejects.toThrow('Steps must be positive');
    });

    it('should throw error when rewinding too many steps', async () => {
      await expect(adapter.rewind(10)).rejects.toThrow('Cannot rewind');
    });
  });

  describe('compact', () => {
    it('should compact history when empty', async () => {
      await adapter.compact();
      expect(adapter.getHistory().length).toBe(0);
    });

    it('should compact history with messages', async () => {
      const mockResponse = {
        content: [{ type: 'text', text: 'Response' }],
        model: 'claude-3-5-sonnet-20241022',
        usage: { input_tokens: 10, output_tokens: 5 },
        stop_reason: 'end_turn',
      };

      const mockClient = (adapter as any).client;
      mockClient.messages.create.mockResolvedValue(mockResponse);

      // Add many messages
      for (let i = 0; i < 20; i++) {
        await adapter.send(`Message ${i}`);
      }

      await adapter.compact();

      const history = adapter.getHistory();
      // Should have summary + recent 20% messages
      expect(history.length).toBeGreaterThan(0);
      expect(history[0].role).toBe('system');
      expect(history[0].content).toContain('[Compressed Context]');
    });

    it('should call compress hook', async () => {
      const mockResponse = {
        content: [{ type: 'text', text: 'Response' }],
        model: 'claude-3-5-sonnet-20241022',
        usage: { input_tokens: 10, output_tokens: 5 },
        stop_reason: 'end_turn',
      };

      const mockClient = (adapter as any).client;
      mockClient.messages.create.mockResolvedValue(mockResponse);

      await adapter.send('Message');
      await adapter.compact();

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
  });

  describe('getConfig', () => {
    it('should return copy of config', () => {
      const config1 = adapter.getConfig();
      const config2 = adapter.getConfig();

      expect(config1).not.toBe(config2);
      expect(config1).toEqual(config2);
    });
  });

  describe('estimateTokens', () => {
    it('should estimate tokens for English text', () => {
      const messages = [
        { role: 'user' as const, content: 'Hello world', timestamp: Date.now() },
      ];

      const tokens = (adapter as any).estimateTokens(messages);
      expect(tokens).toBeGreaterThan(0);
    });

    it('should estimate tokens for CJK text', () => {
      const messages = [
        { role: 'user' as const, content: '你好世界', timestamp: Date.now() },
      ];

      const tokens = (adapter as any).estimateTokens(messages);
      expect(tokens).toBeGreaterThan(0);
    });

    it('should estimate tokens for code blocks', () => {
      const messages = [
        {
          role: 'user' as const,
          content: '```typescript\nfunction hello() { return "world"; }\n```',
          timestamp: Date.now(),
        },
      ];

      const tokens = (adapter as any).estimateTokens(messages);
      expect(tokens).toBeGreaterThan(0);
    });

    it('should estimate tokens for mixed content', () => {
      const messages = [
        {
          role: 'user' as const,
          content: 'Hello 你好 ```code``` 123',
          timestamp: Date.now(),
        },
      ];

      const tokens = (adapter as any).estimateTokens(messages);
      expect(tokens).toBeGreaterThan(0);
    });
  });
});
