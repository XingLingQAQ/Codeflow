/**
 * MemoryInjectionHook - MS-220 记忆自动注入
 *
 * 注册到 HookEvent.BEFORE_SEND，在发送请求前自动检索相关记忆
 * 并注入为 system 消息。
 */

import { HookManager } from './HookManager.js';
import { HookEvent, RequestPayload } from './types.js';
import { PassiveRAGService } from '../memory/PassiveRAG.js';

/**
 * MemoryInjectionHook 配置
 */
export interface MemoryInjectionConfig {
  /** 是否启用注入 */
  enabled: boolean;
  /** 默认 sessionId */
  sessionId?: string;
  /** 注入位置 */
  position: 'prepend' | 'append';
  /** 最大注入字符数 */
  maxInjectionLength: number;
}

const DEFAULT_CONFIG: MemoryInjectionConfig = {
  enabled: true,
  position: 'prepend',
  maxInjectionLength: 2000,
};

export class MemoryInjectionHook {
  private readonly hookManager: HookManager;
  private readonly ragService: PassiveRAGService;
  private config: MemoryInjectionConfig;
  private boundHandler: (payload: RequestPayload) => Promise<RequestPayload>;

  constructor(
    hookManager: HookManager,
    ragService: PassiveRAGService,
    config: Partial<MemoryInjectionConfig> = {}
  ) {
    this.hookManager = hookManager;
    this.ragService = ragService;
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.boundHandler = this.onBeforeSend.bind(this);
  }

  /**
   * 注册到 hook_before_send
   */
  register(): void {
    this.hookManager.register(HookEvent.BEFORE_SEND, this.boundHandler);
  }

  /**
   * 注销
   */
  unregister(): void {
    this.hookManager.unregister(HookEvent.BEFORE_SEND, this.boundHandler);
  }

  /**
   * 启用注入
   */
  enable(): void {
    this.config.enabled = true;
  }

  /**
   * 禁用注入
   */
  disable(): void {
    this.config.enabled = false;
  }

  /**
   * 是否启用
   */
  isEnabled(): boolean {
    return this.config.enabled;
  }

  /**
   * hook_before_send 处理器
   */
  private async onBeforeSend(payload: RequestPayload): Promise<RequestPayload> {
    if (!this.config.enabled) {
      return payload;
    }

    try {
      const lastUserMessage = this.extractLastUserMessage(payload);
      if (!lastUserMessage) {
        return payload;
      }

      const memories = await this.ragService.retrieve(
        lastUserMessage,
        this.config.sessionId
      );

      if (memories.length === 0) {
        return payload;
      }

      let contextText = this.ragService.formatForInjection(memories);
      if (contextText.length > this.config.maxInjectionLength) {
        contextText = contextText.slice(0, this.config.maxInjectionLength) + '\n...（已截断）';
      }

      const memoryMessage = {
        role: 'system' as const,
        content: contextText,
        timestamp: Date.now(),
      };

      const messages = [...payload.messages];

      if (this.config.position === 'prepend') {
        messages.unshift(memoryMessage);
      } else {
        const lastSystemIndex = this.findLastSystemIndex(messages);
        messages.splice(lastSystemIndex + 1, 0, memoryMessage);
      }

      return { ...payload, messages };
    } catch {
      return payload;
    }
  }

  /**
   * 提取最后一条用户消息
   */
  private extractLastUserMessage(payload: RequestPayload): string | null {
    for (let i = payload.messages.length - 1; i >= 0; i--) {
      if (payload.messages[i].role === 'user') {
        return payload.messages[i].content;
      }
    }
    return null;
  }

  /**
   * 查找最后一个 system 消息的索引
   */
  private findLastSystemIndex(messages: { role: string }[]): number {
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].role === 'system') {
        return i;
      }
    }
    return -1;
  }
}
