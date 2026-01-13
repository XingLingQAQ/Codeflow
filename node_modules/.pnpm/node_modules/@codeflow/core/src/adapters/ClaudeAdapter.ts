import Anthropic from '@anthropic-ai/sdk';
import { ICliAdapter, AdapterConfig, SendOptions, APIError, TimeoutError } from './types.js';
import { Message, AIResponse, StreamChunk } from '../hooks/types.js';
import { HookManager } from '../hooks/HookManager.js';

/**
 * Claude API Adapter 实现
 * 封装 Anthropic SDK，提供统一的 ICliAdapter 接口
 */
export class ClaudeAdapter implements ICliAdapter {
  private client: Anthropic;
  private config: AdapterConfig;
  private history: Message[] = [];
  private hookManager?: HookManager;
  private currentStream?: AsyncGenerator<StreamChunk>;

  constructor(config: AdapterConfig, hookManager?: HookManager) {
    this.config = {
      maxRetries: 3,
      retryDelay: 1000,
      timeout: 60000,
      temperature: 1.0,
      maxTokens: 4096,
      ...config,
    };

    this.client = new Anthropic({
      apiKey: this.config.apiKey,
      baseURL: this.config.baseURL,
      maxRetries: this.config.maxRetries,
      timeout: this.config.timeout,
    });

    this.hookManager = hookManager;
  }

  /**
   * 发送消息并获取响应
   */
  async send(prompt: string, options?: SendOptions): Promise<AIResponse> {
    // 添加用户消息到历史
    const userMessage: Message = {
      role: 'user',
      content: prompt,
      timestamp: Date.now(),
    };
    this.history.push(userMessage);

    // 构建请求 payload
    let payload = {
      messages: this.history.map((msg) => ({
        role: msg.role,
        content: msg.content,
      })),
      model: options?.model || this.config.model,
      temperature: options?.temperature ?? this.config.temperature,
      max_tokens: options?.maxTokens || this.config.maxTokens,
    };

    // 触发 hook_before_send
    if (this.hookManager) {
      const processedPayload = await this.hookManager.hook_before_send({
        messages: this.history,
        model: payload.model,
        temperature: payload.temperature,
        maxTokens: payload.max_tokens,
      });

      payload = {
        messages: processedPayload.messages.map((msg) => ({
          role: msg.role,
          content: msg.content,
        })),
        model: processedPayload.model || payload.model,
        temperature: processedPayload.temperature ?? payload.temperature,
        max_tokens: processedPayload.maxTokens || payload.max_tokens,
      };
    }

    try {
      // 发送请求
      const response = await this.executeWithRetry(async () => {
        if (options?.stream) {
          throw new Error('Use receive() for streaming responses');
        }

        return await this.client.messages.create({
          messages: payload.messages as Anthropic.MessageParam[],
          model: payload.model,
          temperature: payload.temperature,
          max_tokens: payload.max_tokens!,
          stream: false,
        });
      }, options?.timeout);

      // 构建响应
      const aiResponse: AIResponse = {
        content: response.content[0].type === 'text' ? response.content[0].text : '',
        model: response.model,
        usage: {
          promptTokens: response.usage.input_tokens,
          completionTokens: response.usage.output_tokens,
          totalTokens: response.usage.input_tokens + response.usage.output_tokens,
        },
        finishReason: response.stop_reason || undefined,
      };

      // 添加助手消息到历史
      const assistantMessage: Message = {
        role: 'assistant',
        content: aiResponse.content,
        timestamp: Date.now(),
      };
      this.history.push(assistantMessage);

      // 触发 hook_post_response
      if (this.hookManager) {
        await this.hookManager.hook_post_response(aiResponse);
      }

      return aiResponse;
    } catch (error) {
      this.handleError(error);
      throw error; // TypeScript 需要这行
    }
  }

  /**
   * 接收流式响应
   */
  async *receive(): AsyncGenerator<StreamChunk> {
    if (!this.currentStream) {
      throw new Error('No active stream. Call send() with stream: true first');
    }

    yield* this.currentStream;
  }

  /**
   * 开始流式请求
   */
  async *stream(prompt: string, options?: SendOptions): AsyncGenerator<StreamChunk> {
    // 添加用户消息到历史
    const userMessage: Message = {
      role: 'user',
      content: prompt,
      timestamp: Date.now(),
    };
    this.history.push(userMessage);

    // 构建请求 payload
    let payload = {
      messages: this.history.map((msg) => ({
        role: msg.role,
        content: msg.content,
      })),
      model: options?.model || this.config.model,
      temperature: options?.temperature ?? this.config.temperature,
      max_tokens: options?.maxTokens || this.config.maxTokens,
    };

    // 触发 hook_before_send
    if (this.hookManager) {
      const processedPayload = await this.hookManager.hook_before_send({
        messages: this.history,
        model: payload.model,
        temperature: payload.temperature,
        maxTokens: payload.max_tokens,
      });

      payload = {
        messages: processedPayload.messages.map((msg) => ({
          role: msg.role,
          content: msg.content,
        })),
        model: processedPayload.model || payload.model,
        temperature: processedPayload.temperature ?? payload.temperature,
        max_tokens: processedPayload.maxTokens || payload.max_tokens,
      };
    }

    try {
      const stream = await this.client.messages.create({
        messages: payload.messages as Anthropic.MessageParam[],
        model: payload.model,
        temperature: payload.temperature,
        max_tokens: payload.max_tokens!,
        stream: true,
      });

      let fullContent = '';
      let index = 0;

      for await (const event of stream) {
        if (event.type === 'content_block_delta' && event.delta.type === 'text_delta') {
          const chunk: StreamChunk = {
            delta: event.delta.text,
            index: index++,
            done: false,
          };

          fullContent += event.delta.text;

          // 触发 hook_on_stream
          if (this.hookManager) {
            this.hookManager.hook_on_stream(chunk);
          }

          yield chunk;
        }

        if (event.type === 'message_stop') {
          const finalChunk: StreamChunk = {
            delta: '',
            index: index,
            done: true,
          };

          // 触发 hook_on_stream
          if (this.hookManager) {
            this.hookManager.hook_on_stream(finalChunk);
          }

          yield finalChunk;
        }
      }

      // 添加助手消息到历史
      const assistantMessage: Message = {
        role: 'assistant',
        content: fullContent,
        timestamp: Date.now(),
      };
      this.history.push(assistantMessage);

      // 触发 hook_post_response
      if (this.hookManager) {
        await this.hookManager.hook_post_response({
          content: fullContent,
          model: payload.model,
        });
      }
    } catch (error) {
      this.handleError(error);
      throw error;
    }
  }

  /**
   * 获取对话历史
   */
  getHistory(): Message[] {
    return [...this.history];
  }

  /**
   * 设置对话历史
   */
  setHistory(messages: Message[]): void {
    this.history = [...messages];
  }

  /**
   * 回退指定步数
   */
  async rewind(steps: number): Promise<void> {
    if (steps <= 0) {
      throw new Error('Steps must be positive');
    }

    const messagesToRemove = steps * 2; // 每轮对话包含 user + assistant
    if (messagesToRemove > this.history.length) {
      throw new Error(
        `Cannot rewind ${steps} steps, only ${Math.floor(this.history.length / 2)} rounds available`
      );
    }

    this.history = this.history.slice(0, -messagesToRemove);
  }

  /**
   * 压缩对话历史
   */
  async compact(): Promise<void> {
    if (this.history.length === 0) {
      return;
    }

    // 触发 hook_before_compress
    if (this.hookManager) {
      const skeleton = await this.hookManager.hook_before_compress({
        messages: this.history,
        tokenCount: this.estimateTokens(this.history),
      });

      // 保留最近 20% 的对话 + 决策骨架
      const keepCount = Math.ceil(this.history.length * 0.2);
      const recentMessages = this.history.slice(-keepCount);

      // 构建压缩后的历史
      const summaryMessage: Message = {
        role: 'system',
        content: `[Compressed Context]\nEntities: ${skeleton.entities.join(', ')}\nDecisions: ${skeleton.decisions.join('; ')}\nRelations: ${skeleton.relations.map((r) => `${r.from} ${r.type} ${r.to}`).join(', ')}`,
        timestamp: Date.now(),
      };

      this.history = [summaryMessage, ...recentMessages];
    }
  }

  /**
   * 配置 Adapter
   */
  configure(config: Partial<AdapterConfig>): void {
    this.config = { ...this.config, ...config };

    // 重新创建 client
    this.client = new Anthropic({
      apiKey: this.config.apiKey,
      baseURL: this.config.baseURL,
      maxRetries: this.config.maxRetries,
      timeout: this.config.timeout,
    });
  }

  /**
   * 获取配置
   */
  getConfig(): AdapterConfig {
    return { ...this.config };
  }

  /**
   * 执行带重试的请求
   */
  private async executeWithRetry<T>(fn: () => Promise<T>, timeout?: number): Promise<T> {
    const maxRetries = this.config.maxRetries || 3;
    const retryDelay = this.config.retryDelay || 1000;
    const requestTimeout = timeout || this.config.timeout || 60000;

    for (let attempt = 0; attempt <= maxRetries; attempt++) {
      try {
        // 添加超时控制
        const result = await Promise.race([
          fn(),
          new Promise<never>((_, reject) =>
            setTimeout(() => reject(new TimeoutError()), requestTimeout)
          ),
        ]);

        return result;
      } catch (error) {
        const isLastAttempt = attempt === maxRetries;

        // 超时错误不重试
        if (error instanceof TimeoutError) {
          throw error;
        }

        // 判断是否可重试
        const apiError = this.parseError(error);
        if (!apiError.retryable || isLastAttempt) {
          throw apiError;
        }

        // 等待后重试
        await new Promise((resolve) => setTimeout(resolve, retryDelay * (attempt + 1)));
      }
    }

    throw new Error('Unexpected retry loop exit');
  }

  /**
   * 解析错误
   */
  private parseError(error: unknown): APIError {
    if (error instanceof APIError) {
      return error;
    }

    if (error instanceof Error) {
      // Anthropic SDK 错误
      if ('status' in error) {
        const status = (error as { status: number }).status;
        const code = 'code' in error ? (error as { code: string }).code : undefined;
        const retryable = status === 429 || status === 500 || status === 503;

        return new APIError(error.message, status, code, retryable);
      }

      return new APIError(error.message);
    }

    return new APIError('Unknown error');
  }

  /**
   * 处理错误
   */
  private handleError(error: unknown): never {
    if (error instanceof TimeoutError) {
      throw error;
    }

    const apiError = this.parseError(error);
    throw apiError;
  }

  /**
   * 估算 Token 数量（简单实现）
   */
  private estimateTokens(messages: Message[]): number {
    return messages.reduce((sum, msg) => sum + Math.ceil(msg.content.length / 4), 0);
  }
}
