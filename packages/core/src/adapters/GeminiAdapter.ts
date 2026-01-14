/**
 * Gemini Adapter 实现
 * 支持多模态输入（文本、图片）
 */

import { GoogleGenerativeAI, GenerativeModel, Content, Part } from '@google/generative-ai';
import { ICliAdapter, AdapterConfig, SendOptions, APIError, TimeoutError } from './types.js';
import { Message, AIResponse, StreamChunk } from '../hooks/types.js';
import { HookManager } from '../hooks/HookManager.js';

export interface MultimodalInput {
  text?: string;
  images?: Array<{ data: string; mimeType: string }>;
}

export class GeminiAdapter implements ICliAdapter {
  private client: GoogleGenerativeAI;
  private model: GenerativeModel;
  private config: AdapterConfig;
  private history: Message[] = [];
  private hookManager?: HookManager;
  private currentStream?: AsyncGenerator<StreamChunk>;

  constructor(config: AdapterConfig, hookManager?: HookManager) {
    this.config = {
      temperature: 0.7,
      maxTokens: 8192,
      timeout: 60000,
      maxRetries: 3,
      retryDelay: 1000,
      ...config,
      model: config.model || 'gemini-2.0-flash-exp',
    };

    if (!this.config.apiKey) {
      throw new Error('Gemini API key is required');
    }

    this.client = new GoogleGenerativeAI(this.config.apiKey);
    this.model = this.client.getGenerativeModel({ model: this.config.model });
    this.hookManager = hookManager;
  }

  async send(prompt: string | MultimodalInput, options?: SendOptions): Promise<AIResponse> {
    const mergedOptions = { ...this.config, ...options };

    // 构建消息
    const userMessage: Message = {
      role: 'user',
      content: typeof prompt === 'string' ? prompt : prompt.text || '',
      timestamp: Date.now(),
    };

    this.history.push(userMessage);

    // Hook: before_send
    const payload = {
      messages: [...this.history],
      model: mergedOptions.model!,
      temperature: mergedOptions.temperature,
      maxTokens: mergedOptions.maxTokens,
    };

    if (this.hookManager) {
      await this.hookManager.hook_before_send(payload);
    }

    // 转换为 Gemini 格式
    const contents = this.convertToGeminiFormat(prompt);

    // 发送请求（带重试）
    let lastError: Error | null = null;
    for (let attempt = 0; attempt < (mergedOptions.maxRetries || 3); attempt++) {
      try {
        const result = await this.sendWithTimeout(contents, mergedOptions);

        const response: AIResponse = {
          content: result.response.text(),
          model: mergedOptions.model!,
          usage: {
            promptTokens: result.response.usageMetadata?.promptTokenCount || 0,
            completionTokens: result.response.usageMetadata?.candidatesTokenCount || 0,
            totalTokens: result.response.usageMetadata?.totalTokenCount || 0,
          },
          finishReason: result.response.candidates?.[0]?.finishReason || 'stop',
        };

        // 保存助手消息
        const assistantMessage: Message = {
          role: 'assistant',
          content: response.content,
          timestamp: Date.now(),
        };
        this.history.push(assistantMessage);

        // Hook: post_response
        if (this.hookManager) {
          await this.hookManager.hook_post_response(response);
        }

        return response;
      } catch (error) {
        lastError = error as Error;

        if (error instanceof TimeoutError || this.isRetryableError(error)) {
          if (attempt < (mergedOptions.maxRetries || 3) - 1) {
            await this.delay(mergedOptions.retryDelay || 1000);
            continue;
          }
        }

        throw this.wrapError(error);
      }
    }

    throw lastError || new APIError('Request failed after retries');
  }

  async *receive(): AsyncGenerator<StreamChunk> {
    if (!this.currentStream) {
      throw new Error('No active stream');
    }

    yield* this.currentStream;
  }

  getHistory(): Message[] {
    return [...this.history];
  }

  setHistory(messages: Message[]): void {
    this.history = [...messages];
  }

  async rewind(steps: number): Promise<void> {
    if (steps <= 0 || steps > this.history.length) {
      throw new Error('Invalid rewind steps');
    }
    this.history = this.history.slice(0, -steps);
  }

  async compact(): Promise<void> {
    // 保留最近 10 条消息
    if (this.history.length > 10) {
      this.history = this.history.slice(-10);
    }
  }

  configure(config: Partial<AdapterConfig>): void {
    this.config = { ...this.config, ...config };

    if (config.model) {
      this.model = this.client.getGenerativeModel({ model: config.model });
    }
  }

  getConfig(): AdapterConfig {
    return { ...this.config };
  }

  // ==================== 私有方法 ====================

  private convertToGeminiFormat(prompt: string | MultimodalInput): Content[] {
    if (typeof prompt === 'string') {
      return [{ role: 'user', parts: [{ text: prompt }] }];
    }

    const parts: Part[] = [];

    if (prompt.text) {
      parts.push({ text: prompt.text });
    }

    if (prompt.images) {
      for (const image of prompt.images) {
        parts.push({
          inlineData: {
            data: image.data,
            mimeType: image.mimeType,
          },
        });
      }
    }

    return [{ role: 'user', parts }];
  }

  private async sendWithTimeout(
    contents: Content[],
    options: SendOptions & AdapterConfig
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ): Promise<{ response: { text: () => string; usageMetadata?: any; candidates?: any[] } }> {
    const timeout = options.timeout || 60000;

    return Promise.race([
      this.model.generateContent({
        contents,
        generationConfig: {
          temperature: options.temperature,
          maxOutputTokens: options.maxTokens,
        },
      }),
      new Promise<never>((_, reject) => setTimeout(() => reject(new TimeoutError()), timeout)),
    ]);
  }

  private isRetryableError(error: unknown): boolean {
    if (error instanceof APIError) {
      return error.retryable;
    }

    const message = (error as Error).message?.toLowerCase() || '';
    return (
      message.includes('rate limit') ||
      message.includes('timeout') ||
      message.includes('503') ||
      message.includes('429')
    );
  }

  private wrapError(error: unknown): Error {
    if (error instanceof TimeoutError || error instanceof APIError) {
      return error;
    }

    const err = error as Error & { status?: number; statusCode?: number; code?: string };
    const message = err.message || 'Unknown error';
    const statusCode = err.status || err.statusCode;

    return new APIError(message, statusCode, err.code, this.isRetryableError(error));
  }

  private delay(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }
}
