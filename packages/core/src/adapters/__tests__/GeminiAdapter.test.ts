import { describe, it, expect, beforeEach, vi } from 'vitest';
import { GeminiAdapter, MultimodalInput } from '../GeminiAdapter.js';
import { AdapterConfig, APIError, TimeoutError } from '../types.js';
import { HookManager } from '../../hooks/HookManager.js';
import { HookEvent } from '../../hooks/types.js';

// Mock Google Generative AI SDK
vi.mock('@google/generative-ai', () => {
  return {
    GoogleGenerativeAI: vi.fn().mockImplementation(() => ({
      getGenerativeModel: vi.fn().mockReturnValue({
        generateContent: vi.fn(),
      }),
    })),
  };
});

describe('GeminiAdapter', () => {
  let adapter: GeminiAdapter;
  let mockConfig: AdapterConfig;
  let mockHookManager: HookManager;

  beforeEach(() => {
    mockConfig = {
      apiKey: 'test-api-key',
      model: 'gemini-2.0-flash-exp',
      temperature: 0.7,
      maxTokens: 8192,
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
    } as unknown as HookManager;

    adapter = new GeminiAdapter(mockConfig, mockHookManager);
  });

  describe('constructor', () => {
    it('should create adapter with config', () => {
      expect(adapter).toBeDefined();
      expect(adapter.getConfig().model).toBe('gemini-2.0-flash-exp');
    });

    it('should throw error without API key', () => {
      expect(() => new GeminiAdapter({ model: 'gemini-2.0-flash-exp' })).toThrow(
        'Gemini API key is required'
      );
    });

    it('should use default values for optional config', () => {
      const minimalConfig: AdapterConfig = {
        apiKey: 'test-key',
        model: 'gemini-2.0-flash-exp',
      };
      const minimalAdapter = new GeminiAdapter(minimalConfig);

      const config = minimalAdapter.getConfig();
      expect(config.temperature).toBe(0.7);
      expect(config.maxTokens).toBe(8192);
      expect(config.timeout).toBe(60000);
      expect(config.maxRetries).toBe(3);
      expect(config.retryDelay).toBe(1000);
    });
  });

  describe('send', () => {
    it('should send text message and return response', async () => {
      const mockResponse = {
        response: {
          text: () => 'Hello, world!',
          usageMetadata: {
            promptTokenCount: 10,
            candidatesTokenCount: 5,
            totalTokenCount: 15,
          },
          candidates: [{ finishReason: 'STOP' }],
        },
      };

      const mockModel = (adapter as any).model;
      mockModel.generateContent.mockResolvedValue(mockResponse);

      const response = await adapter.send('Hello');

      expect(response.content).toBe('Hello, world!');
      expect(response.model).toBe('gemini-2.0-flash-exp');
      expect(response.usage?.promptTokens).toBe(10);
      expect(response.usage?.completionTokens).toBe(5);
      expect(response.usage?.totalTokens).toBe(15);
      expect(response.finishReason).toBe('STOP');
    });

    it('applies before_send payload mutations to prompt and model selection', async () => {
      const manager = new HookManager();
      manager.register(HookEvent.BEFORE_SEND, async (payload) => ({
        ...payload,
        model: 'gemini-hook-model',
        temperature: 0.1,
        maxTokens: 33,
        messages: [
          { role: 'system', content: 'hooked system', timestamp: Date.now() },
          { role: 'user', content: 'rewritten prompt', timestamp: Date.now() },
        ],
      }));
      adapter = new GeminiAdapter(mockConfig, manager);

      const mockClient = (adapter as any).client;
      const switchedModel = { generateContent: vi.fn().mockResolvedValue({
        response: {
          text: () => 'hooked response',
          usageMetadata: {
            promptTokenCount: 10,
            candidatesTokenCount: 5,
            totalTokenCount: 15,
          },
          candidates: [{ finishReason: 'STOP' }],
        },
      }) };
      mockClient.getGenerativeModel.mockReturnValue(switchedModel);

      const response = await adapter.send('Hello');

      expect(mockClient.getGenerativeModel).toHaveBeenCalledWith({ model: 'gemini-hook-model' });
      expect(switchedModel.generateContent).toHaveBeenCalledWith(
        expect.objectContaining({
          contents: [{ role: 'user', parts: [{ text: 'rewritten prompt' }] }],
          generationConfig: expect.objectContaining({
            temperature: 0.1,
            maxOutputTokens: 33,
          }),
        })
      );
      expect(response.model).toBe('gemini-hook-model');
    });

    it('should send multimodal message', async () => {
      const mockResponse = {
        response: {
          text: () => 'Image description',
          usageMetadata: {
            promptTokenCount: 20,
            candidatesTokenCount: 10,
            totalTokenCount: 30,
          },
          candidates: [{ finishReason: 'STOP' }],
        },
      };

      const mockModel = (adapter as any).model;
      mockModel.generateContent.mockResolvedValue(mockResponse);

      const multimodalInput: MultimodalInput = {
        text: 'Describe this image',
        images: [{ data: 'base64data', mimeType: 'image/png' }],
      };

      const response = await adapter.send(multimodalInput);

      expect(response.content).toBe('Image description');
    });

    it('should add messages to history', async () => {
      const mockResponse = {
        response: {
          text: () => 'Response',
          usageMetadata: {
            promptTokenCount: 10,
            candidatesTokenCount: 5,
            totalTokenCount: 15,
          },
          candidates: [{ finishReason: 'STOP' }],
        },
      };

      const mockModel = (adapter as any).model;
      mockModel.generateContent.mockResolvedValue(mockResponse);

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
        response: {
          text: () => 'Response',
          usageMetadata: {
            promptTokenCount: 10,
            candidatesTokenCount: 5,
            totalTokenCount: 15,
          },
          candidates: [{ finishReason: 'STOP' }],
        },
      };

      const mockModel = (adapter as any).model;
      mockModel.generateContent.mockResolvedValue(mockResponse);

      await adapter.send('Hello');

      expect(mockHookManager.hook_before_send).toHaveBeenCalled();
      expect(mockHookManager.hook_post_response).toHaveBeenCalled();
    });

    it('should retry on retryable errors', async () => {
      const mockModel = (adapter as any).model;
      mockModel.generateContent
        .mockRejectedValueOnce(new Error('rate limit'))
        .mockResolvedValueOnce({
          response: {
            text: () => 'Success',
            usageMetadata: {
              promptTokenCount: 10,
              candidatesTokenCount: 5,
              totalTokenCount: 15,
            },
            candidates: [{ finishReason: 'STOP' }],
          },
        });

      const response = await adapter.send('Hello');
      expect(response.content).toBe('Success');
      expect(mockModel.generateContent).toHaveBeenCalledTimes(2);
    });

    it('should timeout after specified duration', async () => {
      const mockModel = (adapter as any).model;
      mockModel.generateContent.mockImplementation(
        () => new Promise((resolve) => setTimeout(resolve, 100000))
      );

      await expect(adapter.send('Hello', { timeout: 100 })).rejects.toThrow(TimeoutError);
    });

    it('should handle API errors', async () => {
      const mockModel = (adapter as any).model;
      mockModel.generateContent.mockRejectedValue(new Error('API Error'));

      await expect(adapter.send('Hello')).rejects.toThrow(APIError);
    });

    it('should not retry non-retryable errors', async () => {
      const mockModel = (adapter as any).model;
      mockModel.generateContent.mockRejectedValue(new Error('Invalid request'));

      await expect(adapter.send('Hello')).rejects.toThrow();
      expect(mockModel.generateContent).toHaveBeenCalledTimes(1);
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
        response: {
          text: () => 'Response',
          usageMetadata: {
            promptTokenCount: 10,
            candidatesTokenCount: 5,
            totalTokenCount: 15,
          },
          candidates: [{ finishReason: 'STOP' }],
        },
      };

      const mockModel = (adapter as any).model;
      mockModel.generateContent.mockResolvedValue(mockResponse);

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
        response: {
          text: () => 'Response',
          usageMetadata: {
            promptTokenCount: 10,
            candidatesTokenCount: 5,
            totalTokenCount: 15,
          },
          candidates: [{ finishReason: 'STOP' }],
        },
      };

      const mockModel = (adapter as any).model;
      mockModel.generateContent.mockResolvedValue(mockResponse);

      await adapter.send('Message 1');
      await adapter.send('Message 2');

      await adapter.rewind(1);

      const history = adapter.getHistory();
      expect(history.length).toBe(3);
    });

    it('should throw error for invalid steps', async () => {
      await expect(adapter.rewind(0)).rejects.toThrow('Invalid rewind steps');
    });

    it('should throw error when rewinding too many steps', async () => {
      await expect(adapter.rewind(10)).rejects.toThrow('Invalid rewind steps');
    });
  });

  describe('compact', () => {
    it('should compact history', async () => {
      const mockResponse = {
        response: {
          text: () => 'Response',
          usageMetadata: {
            promptTokenCount: 10,
            candidatesTokenCount: 5,
            totalTokenCount: 15,
          },
          candidates: [{ finishReason: 'STOP' }],
        },
      };

      const mockModel = (adapter as any).model;
      mockModel.generateContent.mockResolvedValue(mockResponse);

      for (let i = 0; i < 15; i++) {
        await adapter.send(`Message ${i}`);
      }

      await adapter.compact();

      const history = adapter.getHistory();
      expect(history.length).toBeLessThanOrEqual(10);
    });
  });

  describe('configure', () => {
    it('should update configuration', () => {
      adapter.configure({ temperature: 0.5, maxTokens: 4096 });

      const config = adapter.getConfig();
      expect(config.temperature).toBe(0.5);
      expect(config.maxTokens).toBe(4096);
    });

    it('should call getGenerativeModel when model name changes', () => {
      const mockClient = (adapter as any).client;
      const spy = vi.spyOn(mockClient, 'getGenerativeModel');

      adapter.configure({ model: 'gemini-pro' });

      expect(spy).toHaveBeenCalledWith({ model: 'gemini-pro' });
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

  describe('convertToGeminiFormat', () => {
    it('should convert string prompt', () => {
      const result = (adapter as any).convertToGeminiFormat('Hello');

      expect(result).toEqual([{ role: 'user', parts: [{ text: 'Hello' }] }]);
    });

    it('should convert multimodal prompt with text only', () => {
      const input: MultimodalInput = { text: 'Hello' };
      const result = (adapter as any).convertToGeminiFormat(input);

      expect(result).toEqual([{ role: 'user', parts: [{ text: 'Hello' }] }]);
    });

    it('should convert multimodal prompt with images', () => {
      const input: MultimodalInput = {
        text: 'Describe',
        images: [{ data: 'base64', mimeType: 'image/png' }],
      };
      const result = (adapter as any).convertToGeminiFormat(input);

      expect(result[0].parts.length).toBe(2);
      expect(result[0].parts[0]).toEqual({ text: 'Describe' });
      expect(result[0].parts[1]).toEqual({
        inlineData: { data: 'base64', mimeType: 'image/png' },
      });
    });

    it('should convert multimodal prompt with multiple images', () => {
      const input: MultimodalInput = {
        text: 'Compare',
        images: [
          { data: 'base64_1', mimeType: 'image/png' },
          { data: 'base64_2', mimeType: 'image/jpeg' },
        ],
      };
      const result = (adapter as any).convertToGeminiFormat(input);

      expect(result[0].parts.length).toBe(3);
    });
  });

  describe('error handling', () => {
    it('should identify retryable errors', () => {
      const freshAdapter = new GeminiAdapter(mockConfig);

      expect((freshAdapter as any).isRetryableError(new Error('rate limit'))).toBe(true);
      expect((freshAdapter as any).isRetryableError(new Error('timeout'))).toBe(true);
      expect((freshAdapter as any).isRetryableError(new Error('503'))).toBe(true);
      expect((freshAdapter as any).isRetryableError(new Error('429'))).toBe(true);
      expect((freshAdapter as any).isRetryableError(new Error('invalid request'))).toBe(false);
    });

    it('should wrap errors as APIError', () => {
      const freshAdapter = new GeminiAdapter(mockConfig);
      const error = new Error('Test error');

      const wrapped = (freshAdapter as any).wrapError(error);

      expect(wrapped).toBeInstanceOf(APIError);
      expect(wrapped.message).toBe('Test error');
    });

    it('should preserve TimeoutError', () => {
      const freshAdapter = new GeminiAdapter(mockConfig);
      const error = new TimeoutError();

      const wrapped = (freshAdapter as any).wrapError(error);

      expect(wrapped).toBeInstanceOf(TimeoutError);
    });

    it('should preserve APIError', () => {
      const freshAdapter = new GeminiAdapter(mockConfig);
      const error = new APIError('API Error', 500);

      const wrapped = (freshAdapter as any).wrapError(error);

      expect(wrapped).toBeInstanceOf(APIError);
      expect(wrapped.statusCode).toBe(500);
    });
  });
});
