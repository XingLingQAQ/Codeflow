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
    const mergedOptions = { ...this.config, ...options };

    // 构建消息
    const userMessage: Message = {
      role: 'user',
      content: prompt,
      timestamp: Date.now(),
    };

    this.history.push(userMessage);

    const payload = await this.applyBeforeSendHooks(this.buildPayloadContext(options));

    // 转换为 OpenAI 格式
    const messages = payload.messages.map((msg) => ({
      role: msg.role,
      content: msg.content,
    }));

    try {
      if (mergedOptions.stream) {
        // 流式响应
        return await this.handleStream(messages, {
          ...mergedOptions,
          model: payload.model,
          temperature: payload.temperature,
          maxTokens: payload.maxTokens,
        });
      } else {
        // 非流式响应
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
      }
    } catch (error) {
      throw this.wrapError(error);
    }
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

    // 重新创建客户端（如果 API key 或 baseURL 变更）
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

  // ==================== 私有方法 ====================

  private async handleStream(
    messages: Array<{ role: string; content: string }>,
    options: SendOptions & AdapterConfig
  ): Promise<AIResponse> {
    const stream = await this.client.chat.completions.create({
      model: options.model!,
      messages: messages as OpenAI.ChatCompletionMessageParam[],
      temperature: options.temperature,
      max_tokens: options.maxTokens,
      stream: true,
    });

    let fullContent = '';
    let index = 0;

    this.currentStream = async function* (this: CodexAdapter) {
      for await (const chunk of stream) {
        const delta = chunk.choices[0]?.delta?.content || '';
        if (delta) {
          fullContent += delta;

          const streamChunk: StreamChunk = {
            delta,
            index: index++,
            done: false,
          };

          // Hook: on_stream
          if (this.hookManager) {
            this.hookManager.hook_on_stream(streamChunk);
          }

          yield streamChunk;
        }
      }

      // 最后一个 chunk
      yield {
        delta: '',
        index: index++,
        done: true,
      };
    }.bind(this)();

    // 等待流完成
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    for await (const _ of this.currentStream) {
      // 消费流
    }

    // 保存助手消息
    const assistantMessage: Message = {
      role: 'assistant',
      content: fullContent,
      timestamp: Date.now(),
    };
    this.history.push(assistantMessage);

    const response: AIResponse = {
      content: fullContent,
      model: options.model!,
      usage: {
        promptTokens: 0,
        completionTokens: 0,
        totalTokens: 0,
      },
      finishReason: 'stop',
    };

    // Hook: post_response
    if (this.hookManager) {
      await this.hookManager.hook_post_response(response);
    }

    return response;
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
