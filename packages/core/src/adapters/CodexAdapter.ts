/**
 * Codex Adapter 实现
 * 基于 OpenAI API（兼容 Codex 端点）
 */

import OpenAI from 'openai';
import {
  ICliAdapter,
  AdapterConfig,
  SendOptions,
  APIError,
  TimeoutError,
  AdapterPayloadContext,
  toHookPayload,
  applyHookPayload,
} from './types.js';
import { Message, AIResponse, StreamChunk } from '../hooks/types.js';
import { HookManager } from '../hooks/HookManager.js';

export class CodexAdapter implements ICliAdapter {
  private client: OpenAI;
  private config: AdapterConfig;
  private history: Message[] = [];
  private hookManager?: HookManager;
  private currentStream?: AsyncGenerator<StreamChunk>;

  constructor(config: AdapterConfig, hookManager?: HookManager) {
    this.config = {
      temperature: 0.7,
      maxTokens: 4096,
      timeout: 60000,
      maxRetries: 3,
      retryDelay: 1000,
      ...config,
      model: config.model || 'gpt-4',
    };

    if (!this.config.apiKey) {
      throw new Error('OpenAI API key is required');
    }

    this.client = new OpenAI({
      apiKey: this.config.apiKey,
      baseURL: this.config.baseURL,
      timeout: this.config.timeout,
      maxRetries: this.config.maxRetries,
    });

    this.hookManager = hookManager;
  }

  setHookManager(hookManager?: HookManager): void {
    this.hookManager = hookManager;
  }

  getHookManager(): HookManager | undefined {
    return this.hookManager;
  }

  private buildPayloadContext(options?: SendOptions): AdapterPayloadContext {
    return {
      messages: [...this.history],
      model: options?.model || this.config.model,
      temperature: options?.temperature ?? this.config.temperature,
      maxTokens: options?.maxTokens || this.config.maxTokens,
    };
  }

  private async applyBeforeSendHooks(context: AdapterPayloadContext): Promise<AdapterPayloadContext> {
    if (!this.hookManager) {
      return context;
    }

    const processedPayload = await this.hookManager.hook_before_send(toHookPayload(context));
    return applyHookPayload(context, processedPayload);
  }

  async send(prompt: string, options?: SendOptions): Promise<AIResponse> {
    if (options?.stream) {
      throw new Error('Use stream() for streaming responses');
    }

    const userMessage: Message = {
      role: 'user',
      content: prompt,
      timestamp: Date.now(),
    };

    this.history.push(userMessage);

    const payload = await this.applyBeforeSendHooks(this.buildPayloadContext(options));
    const messages = payload.messages.map((msg) => ({
      role: msg.role,
      content: msg.content,
    }));

    try {
      const completion = await this.client.chat.completions.create({
        model: payload.model,
        messages: messages as OpenAI.ChatCompletionMessageParam[],
        temperature: payload.temperature,
        max_tokens: payload.maxTokens,
      });

      const response: AIResponse = {
        content: completion.choices[0]?.message?.content || '',
        model: completion.model,
        usage: {
          promptTokens: completion.usage?.prompt_tokens || 0,
          completionTokens: completion.usage?.completion_tokens || 0,
          totalTokens: completion.usage?.total_tokens || 0,
        },
        finishReason: completion.choices[0]?.finish_reason || 'stop',
      };

      const assistantMessage: Message = {
        role: 'assistant',
        content: response.content,
        timestamp: Date.now(),
      };
      this.history.push(assistantMessage);

      if (this.hookManager) {
        await this.hookManager.hook_post_response(response);
      }

      return response;
    } catch (error) {
      throw this.wrapError(error);
    }
  }

  async *stream(prompt: string, options?: SendOptions): AsyncGenerator<StreamChunk> {
    const userMessage: Message = {
      role: 'user',
      content: prompt,
      timestamp: Date.now(),
    };

    this.history.push(userMessage);

    const payload = await this.applyBeforeSendHooks(this.buildPayloadContext(options));
    const messages = payload.messages.map((msg) => ({
      role: msg.role,
      content: msg.content,
    }));

    const streamGenerator = this.createStreamGenerator({
      messages,
      model: payload.model,
      temperature: payload.temperature,
      maxTokens: payload.maxTokens,
    });
    this.currentStream = streamGenerator;

    try {
      yield* streamGenerator;
    } finally {
      this.currentStream = undefined;
    }
  }

  async *receive(): AsyncGenerator<StreamChunk> {
    if (!this.currentStream) {
      throw new Error('No active stream');
    }

    try {
      yield* this.currentStream;
    } finally {
      this.currentStream = undefined;
    }
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
    if (this.history.length > 10) {
      this.history = this.history.slice(-10);
    }
  }

  configure(config: Partial<AdapterConfig>): void {
    this.config = { ...this.config, ...config };

    if (config.apiKey || config.baseURL) {
      this.client = new OpenAI({
        apiKey: this.config.apiKey,
        baseURL: this.config.baseURL,
        timeout: this.config.timeout,
        maxRetries: this.config.maxRetries,
      });
    }
  }

  getConfig(): AdapterConfig {
    return { ...this.config };
  }

  private async *createStreamGenerator(payload: {
    messages: Array<{ role: string; content: string }>;
    model: string;
    temperature?: number;
    maxTokens?: number;
  }): AsyncGenerator<StreamChunk> {
    try {
      const stream = await this.client.chat.completions.create({
        model: payload.model,
        messages: payload.messages as OpenAI.ChatCompletionMessageParam[],
        temperature: payload.temperature,
        max_tokens: payload.maxTokens,
        stream: true,
      });

      let fullContent = '';
      let index = 0;

      for await (const chunk of stream) {
        const delta = chunk.choices[0]?.delta?.content || '';
        if (!delta) {
          continue;
        }

        fullContent += delta;

        const streamChunk: StreamChunk = {
          delta,
          index: index++,
          done: false,
        };

        if (this.hookManager) {
          this.hookManager.hook_on_stream(streamChunk);
        }

        yield streamChunk;
      }

      const finalChunk: StreamChunk = {
        delta: '',
        index,
        done: true,
      };

      if (this.hookManager) {
        this.hookManager.hook_on_stream(finalChunk);
      }

      yield finalChunk;

      const assistantMessage: Message = {
        role: 'assistant',
        content: fullContent,
        timestamp: Date.now(),
      };
      this.history.push(assistantMessage);

      const response: AIResponse = {
        content: fullContent,
        model: payload.model,
        usage: {
          promptTokens: 0,
          completionTokens: 0,
          totalTokens: 0,
        },
        finishReason: 'stop',
      };

      if (this.hookManager) {
        await this.hookManager.hook_post_response(response);
      }
    } catch (error) {
      throw this.wrapError(error);
    }
  }

  private wrapError(error: unknown): Error {
    if (error instanceof TimeoutError || error instanceof APIError) {
      return error;
    }

    const err = error as Error & { status?: number; statusCode?: number; code?: string };
    const message = err.message || 'Unknown error';
    const statusCode = err.status || err.statusCode;
    const retryable = statusCode === 429 || statusCode === 503 || message.includes('timeout');

    return new APIError(message, statusCode, err.code, retryable);
  }
}
