/**
 * CLI Adapter 接口类型定义
 */

import {
  Message,
  AIResponse,
  StreamChunk,
  RequestPayload,
  DecisionSkeleton,
  cloneMessage,
  getMessageText,
  serializeMessageContent,
  deserializeMessageContent,
  isToolTurnMessage,
} from '../hooks/types.js';

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

export function cloneMessages(messages: Message[]): Message[] {
  return messages.map((message) => cloneMessage(message));
}

export function messageToText(message: Message): string {
  return getMessageText(message.content);
}

export function messagesToPrompt(messages: Message[], separator = '\n\n'): string {
  return messages.map((message) => messageToText(message)).join(separator);
}

export function serializeMessage<T extends Message>(message: T): T & { content: string } {
  return {
    ...cloneMessage(message),
    content: serializeMessageContent(message.content),
  } as T & { content: string };
}

export function deserializeMessage<T extends Message>(message: T): T {
  return {
    ...cloneMessage(message),
    content:
      typeof message.content === 'string'
        ? deserializeMessageContent(message.content)
        : message.content,
  } as T;
}

export function toHookPayload(context: AdapterPayloadContext): RequestPayload {
  return {
    messages: cloneMessages(context.messages),
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
    messages: cloneMessages(payload.messages ?? context.messages),
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
      systemMessages.push(cloneMessage(message));
      continue;
    }
    dialogueMessages.push(cloneMessage(message));
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
  return [...systemMessages, ...dialogueMessages.slice(0, cutoff)].map((message) => cloneMessage(message));
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
    return [...systemMessages, ...dialogueMessages].map((message) => cloneMessage(message));
  }

  const estimateTokens =
    options.estimateTokens ??
    ((history: Message[]) => history.reduce((sum, message) => sum + Math.ceil(messageToText(message).length / 4), 0));
  const keepRatio = options.keepRatio ?? 0.2;
  const minimumRecentMessages = options.minimumRecentMessages ?? 2;
  const keepCount = Math.min(
    dialogueMessages.length,
    Math.max(minimumRecentMessages, Math.ceil(dialogueMessages.length * keepRatio))
  );
  const recentMessages = dialogueMessages.slice(-keepCount);
  const olderMessages = dialogueMessages.slice(0, -keepCount);

  if (olderMessages.length === 0) {
    return [...systemMessages, ...recentMessages].map((message) => cloneMessage(message));
  }

  const skeletonSource = [...systemMessages, ...olderMessages];
  const skeleton = await options.buildSkeleton(skeletonSource, estimateTokens(skeletonSource));
  const relations = skeleton.relations.map((relation) => `${relation.from} ${relation.type} ${relation.to}`).join(', ');
  const summaryMessage: Message = {
    role: 'system',
    content: `[Compressed Context]\nEntities: ${skeleton.entities.join(', ')}\nDecisions: ${skeleton.decisions.join('; ')}\nRelations: ${relations}`,
    timestamp: options.summaryTimestamp ?? Date.now(),
  };

  const preservedSystemMessages = systemMessages.filter(
    (message) => !messageToText(message).startsWith('[Compressed Context]')
  );
  return [...preservedSystemMessages, summaryMessage, ...recentMessages].map((message) => cloneMessage(message));
}

export function hasToolTurns(messages: Message[]): boolean {
  return messages.some((message) => isToolTurnMessage(message));
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
