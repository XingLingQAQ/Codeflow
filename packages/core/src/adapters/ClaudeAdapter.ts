import Anthropic from '@anthropic-ai/sdk';
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

  setHookManager(hookManager?: HookManager): void {
    this.hookManager = hookManager;
  }

  getHookManager(): HookManager | undefined {
    return this.hookManager;
  }

  /**
   * 发送消息并获取响应
   */
  async send(prompt: string, options?: SendOptions): Promise<AIResponse> {
    if (options?.stream) {
      throw new Error('Use stream() for streaming responses');
    }

    // 添加用户消息到历史
    const userMessage: Message = {
      role: 'user',
      content: prompt,
      timestamp: Date.now(),
    };
    this.history.push(userMessage);

    const payload = await this.applyBeforeSendHooks(this.buildPayloadContext(options));
    const system = this.extractSystemPrompt(payload.messages);
    const messages = this.mapMessagesToProviderPayload(payload.messages);

    try {
      // 发送请求
      const response = await this.executeWithRetry(async () => {
        return await this.client.messages.create({
          messages,
          system,
          model: payload.model,
          temperature: payload.temperature,
          max_tokens: payload.maxTokens!,
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
      throw new Error('No active stream');
    }

    try {
      yield* this.currentStream;
    } finally {
      this.currentStream = undefined;
    }
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

    const payload = await this.applyBeforeSendHooks(this.buildPayloadContext(options));
    const system = this.extractSystemPrompt(payload.messages);
    const messages = this.mapMessagesToProviderPayload(payload.messages);

    const streamGenerator = this.createStreamGenerator({
      messages,
      system,
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

  private buildPayloadContext(options?: SendOptions): AdapterPayloadContext {
    return {
      messages: [...this.history],
      model: options?.model || this.config.model,
      temperature: options?.temperature ?? this.config.temperature,
      maxTokens: options?.maxTokens || this.config.maxTokens,
    };
  }

  private mapMessagesToProviderPayload(messages: Message[]): Anthropic.MessageParam[] {
    return messages
      .filter((message): message is Message & { role: 'user' | 'assistant' } =>
        message.role === 'user' || message.role === 'assistant'
      )
      .map((message) => ({
        role: message.role,
        content: message.content,
      })) as Anthropic.MessageParam[];
  }

  private extractSystemPrompt(messages: Message[]): string | undefined {
    const systemMessages = messages.filter((message) => message.role === 'system');
    if (systemMessages.length === 0) {
      return undefined;
    }

    return systemMessages.map((message) => message.content).join('\n\n');
  }

  private async applyBeforeSendHooks(context: AdapterPayloadContext): Promise<AdapterPayloadContext> {
    if (!this.hookManager) {
      return context;
    }

    const processedPayload = await this.hookManager.hook_before_send(toHookPayload(context));
    return applyHookPayload(context, processedPayload);
  }

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

  private async *createStreamGenerator(payload: {
    messages: Anthropic.MessageParam[];
    system?: string;
    model: string;
    temperature?: number;
    maxTokens?: number;
  }): AsyncGenerator<StreamChunk> {
    try {
      const stream = await this.client.messages.create({
        messages: payload.messages,
        system: payload.system,
        model: payload.model,
        temperature: payload.temperature,
        max_tokens: payload.maxTokens!,
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

          if (this.hookManager) {
            this.hookManager.hook_on_stream(chunk);
          }

          yield chunk;
        }

        if (event.type === 'message_stop') {
          const finalChunk: StreamChunk = {
            delta: '',
            index,
            done: true,
          };

          if (this.hookManager) {
            this.hookManager.hook_on_stream(finalChunk);
          }

          yield finalChunk;
        }
      }

      const assistantMessage: Message = {
        role: 'assistant',
        content: fullContent,
        timestamp: Date.now(),
      };
      this.history.push(assistantMessage);

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
   * 估算 Token 数量
   * 使用改进的启发式算法，考虑不同语言和内容类型
   */
  private estimateTokens(messages: Message[]): number {
    return messages.reduce((sum, msg) => sum + this.estimateContentTokens(msg.content), 0);
  }

  /**
   * 估算单个内容的 Token 数量
   * 基于 Claude tokenizer 的特性进行估算
   */
  private estimateContentTokens(content: string): number {
    if (!content) return 0;

    let tokens = 0;

    // 1. 检测代码块并单独计算
    const codeBlockRegex = /```[\s\S]*?```/g;
    const codeBlocks = content.match(codeBlockRegex) || [];
    let nonCodeContent = content;

    for (const block of codeBlocks) {
      // 代码通常每 3-4 字符一个 token（因为有很多符号和短标识符）
      tokens += Math.ceil(block.length / 3.5);
      nonCodeContent = nonCodeContent.replace(block, '');
    }

    // 2. 检测中文/日文/韩文字符（CJK）
    const cjkRegex = /[\u4e00-\u9fff\u3040-\u309f\u30a0-\u30ff\uac00-\ud7af]/g;
    const cjkChars = nonCodeContent.match(cjkRegex) || [];
    // CJK 字符通常每个字符 1-2 个 token
    tokens += cjkChars.length * 1.5;

    // 移除 CJK 字符后计算剩余内容
    const nonCjkContent = nonCodeContent.replace(cjkRegex, '');

    // 3. 计算英文和其他内容
    // 英文通常每 4 字符一个 token，但需要考虑：
    // - 空格和标点符号
    // - 常见单词可能被合并
    // - 数字和特殊字符

    // 按空格分词
    const words = nonCjkContent.split(/\s+/).filter(w => w.length > 0);

    for (const word of words) {
      if (/^\d+$/.test(word)) {
        // 纯数字：每 3-4 位一个 token
        tokens += Math.ceil(word.length / 3);
      } else if (/^[a-zA-Z]+$/.test(word)) {
        // 纯英文单词
        if (word.length <= 4) {
          tokens += 1; // 短单词通常是一个 token
        } else if (word.length <= 8) {
          tokens += 1.5; // 中等长度单词
        } else {
          tokens += Math.ceil(word.length / 4); // 长单词
        }
      } else {
        // 混合内容（包含符号等）
        tokens += Math.ceil(word.length / 3);
      }
    }

    // 4. 添加标点符号和空格的 token（通常被合并到相邻 token）
    const punctuation = nonCjkContent.match(/[.,!?;:'"()\[\]{}]/g) || [];
    tokens += punctuation.length * 0.3; // 标点通常被合并

    // 5. 添加换行符的 token
    const newlines = content.match(/\n/g) || [];
    tokens += newlines.length * 0.5;

    return Math.ceil(tokens);
  }
}
