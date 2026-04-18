/**
 * CLI Adapter 接口类型定义
 */

import { Message, AIResponse, StreamChunk, RequestPayload } from '../hooks/types.js';

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

export interface ProviderRequestConfig {
  apiKey?: string;
  baseURL?: string;
  timeout?: number;
  maxRetries?: number;
}

export interface AdapterRuntimeConfig extends ProviderRequestConfig {
  model: string;
  temperature?: number;
  maxTokens?: number;
}

/**
 * 运行时 resolved config 的最小只读视图。
 * provider 认证/重试属于 provider request config；
 * runtime metadata 继续由 ConfigManager/runtime 真相源持有，不下沉到 adapter config。
 */
export interface ResolvedAdapterConfig extends AdapterRuntimeConfig {
  systemPrompt?: string;
  answerStyle?: string;
  capabilities?: string[];
  allowedSkills?: string[];
  allowedHooks?: string[];
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

export interface AdapterPayloadContext {
  messages: Message[];
  model: string;
  temperature?: number;
  maxTokens?: number;
}

export function toHookPayload(context: AdapterPayloadContext): RequestPayload {
  return {
    messages: context.messages.map((message) => ({ ...message })),
    model: context.model,
    temperature: context.temperature,
    maxTokens: context.maxTokens,
  };
}

export function applyHookPayload(
  context: AdapterPayloadContext,
  payload: RequestPayload
): AdapterPayloadContext {
  return {
    messages: (payload.messages ?? context.messages).map((message) => ({ ...message })),
    model: typeof payload.model === 'string' && payload.model.length > 0 ? payload.model : context.model,
    temperature: payload.temperature ?? context.temperature,
    maxTokens: payload.maxTokens ?? context.maxTokens,
  };
}

/**
 * CLI Adapter 核心接口
 */
export interface ICliAdapter {
  // 基础通信
  send(prompt: string, options?: SendOptions): Promise<AIResponse>;
  stream(prompt: string, options?: SendOptions): AsyncGenerator<StreamChunk>;
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
