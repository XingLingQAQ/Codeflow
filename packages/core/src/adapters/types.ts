/**
 * CLI Adapter 接口类型定义
 */

import { Message, AIResponse, StreamChunk } from '../hooks/types.js';

/**
 * 发送选项
 */
export interface SendOptions {
  model?: string;
  temperature?: number;
  maxTokens?: number;
  stream?: boolean;
  timeout?: number;
  [key: string]: unknown;
}

/**
 * Adapter 配置
 */
export interface AdapterConfig {
  apiKey?: string;
  baseURL?: string;
  model: string;
  temperature?: number;
  maxTokens?: number;
  timeout?: number;
  maxRetries?: number;
  retryDelay?: number;
}

/**
 * CLI Adapter 核心接口
 */
export interface ICliAdapter {
  // 基础通信
  send(prompt: string, options?: SendOptions): Promise<AIResponse>;
  receive(): AsyncGenerator<StreamChunk>;

  // 上下文管理
  getHistory(): Message[];
  setHistory(messages: Message[]): void;

  // 状态控制
  rewind(steps: number): Promise<void>;
  compact(): Promise<void>;

  // 配置
  configure(config: Partial<AdapterConfig>): void;
  getConfig(): AdapterConfig;
}

/**
 * API 错误类型
 */
export class APIError extends Error {
  constructor(
    message: string,
    public statusCode?: number,
    public code?: string,
    public retryable: boolean = false
  ) {
    super(message);
    this.name = 'APIError';
  }
}

/**
 * 超时错误
 */
export class TimeoutError extends Error {
  constructor(message: string = 'Request timeout') {
    super(message);
    this.name = 'TimeoutError';
  }
}
