/**
 * CLI Adapter 接口类型定义
 */

import { Message, AIResponse, StreamChunk, RequestPayload, DecisionSkeleton } from '../hooks/types.js';

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

export interface HistoryGovernanceParts {
  systemMessages: Message[];
  dialogueMessages: Message[];
}

export function splitHistoryForGovernance(messages: Message[]): HistoryGovernanceParts {
  const systemMessages: Message[] = [];
  const dialogueMessages: Message[] = [];

  for (const message of messages) {
    if (message.role === 'system') {
      systemMessages.push({ ...message });
      continue;
    }
    dialogueMessages.push({ ...message });
  }

  return { systemMessages, dialogueMessages };
}

export function countCompletedTurns(messages: Message[]): number {
  return messages.filter((message) => message.role === 'assistant').length;
}

export function rewindHistoryByTurns(messages: Message[], steps: number): Message[] {
  if (steps <= 0) {
    throw new Error('Steps must be positive');
  }

  const { systemMessages, dialogueMessages } = splitHistoryForGovernance(messages);
  const assistantBoundaries = dialogueMessages.flatMap((message, index) =>
    message.role === 'assistant' ? [index + 1] : []
  );
  const availableTurns = assistantBoundaries.length;
  if (steps > availableTurns) {
    throw new Error(`Cannot rewind ${steps} steps, only ${availableTurns} rounds available`);
  }

  const turnsToKeep = availableTurns - steps;
  const cutoff = turnsToKeep === 0 ? 0 : assistantBoundaries[turnsToKeep - 1];
  return [...systemMessages, ...dialogueMessages.slice(0, cutoff)];
}

export async function compactHistoryWithSummary(
  messages: Message[],
  options: {
    buildSkeleton?: (messages: Message[], tokenCount: number) => Promise<DecisionSkeleton>;
    estimateTokens?: (messages: Message[]) => number;
    keepRatio?: number;
    minimumRecentMessages?: number;
    summaryTimestamp?: number;
  } = {}
): Promise<Message[]> {
  if (messages.length === 0) {
    return [];
  }

  const { systemMessages, dialogueMessages } = splitHistoryForGovernance(messages);
  if (dialogueMessages.length === 0 || !options.buildSkeleton) {
    return [...systemMessages, ...dialogueMessages];
  }

  const estimateTokens = options.estimateTokens ?? ((history: Message[]) => history.reduce((sum, message) => sum + Math.ceil((message.content?.length ?? 0) / 4), 0));
  const keepRatio = options.keepRatio ?? 0.2;
  const minimumRecentMessages = options.minimumRecentMessages ?? 2;
  const keepCount = Math.min(
    dialogueMessages.length,
    Math.max(minimumRecentMessages, Math.ceil(dialogueMessages.length * keepRatio))
  );
  const recentMessages = dialogueMessages.slice(-keepCount);
  const olderMessages = dialogueMessages.slice(0, -keepCount);

  if (olderMessages.length === 0) {
    return [...systemMessages, ...recentMessages];
  }

  const skeletonSource = [...systemMessages, ...olderMessages];
  const skeleton = await options.buildSkeleton(skeletonSource, estimateTokens(skeletonSource));
  const relations = skeleton.relations.map((relation) => `${relation.from} ${relation.type} ${relation.to}`).join(', ');
  const summaryMessage: Message = {
    role: 'system',
    content: `[Compressed Context]\nEntities: ${skeleton.entities.join(', ')}\nDecisions: ${skeleton.decisions.join('; ')}\nRelations: ${relations}`,
    timestamp: options.summaryTimestamp ?? Date.now(),
  };

  const preservedSystemMessages = systemMessages.filter((message) => !message.content.startsWith('[Compressed Context]'));
  return [...preservedSystemMessages, summaryMessage, ...recentMessages];
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
