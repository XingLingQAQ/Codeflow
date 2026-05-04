/**
 * Hook Bus 事件系统类型定义
 */

export type MessageRole = 'user' | 'assistant' | 'system';

export interface MessageTextPart {
  type: 'text';
  text: string;
}

export interface MessageToolCallPart {
  type: 'tool_call';
  id: string;
  toolName: string;
  args?: unknown;
}

export interface MessageToolResultPart {
  type: 'tool_result';
  toolCallId?: string;
  toolName: string;
  result?: unknown;
  isError?: boolean;
}

export interface MessageJsonPart {
  type: 'json';
  data: unknown;
}

export type MessagePart =
  | MessageTextPart
  | MessageToolCallPart
  | MessageToolResultPart
  | MessageJsonPart;

export type MessageContent = string | MessagePart[];

export interface MessageMetadata {
  toolCallId?: string;
  toolName?: string;
  provider?: string;
  [key: string]: unknown;
}

export interface Message {
  role: MessageRole;
  content: MessageContent;
  timestamp?: number;
  metadata?: MessageMetadata;
}

export function cloneMessagePart(part: MessagePart): MessagePart {
  if (part.type === 'text') {
    return { ...part };
  }

  if (part.type === 'tool_call') {
    return {
      ...part,
      args: part.args === undefined ? undefined : structuredClone(part.args),
    };
  }

  if (part.type === 'tool_result') {
    return {
      ...part,
      result: part.result === undefined ? undefined : structuredClone(part.result),
    };
  }

  return {
    ...part,
    data: structuredClone(part.data),
  };
}

export function normalizeMessageContent(content: MessageContent): MessagePart[] {
  if (typeof content === 'string') {
    return [{ type: 'text', text: content }];
  }

  return content.map((part) => cloneMessagePart(part));
}

export function getMessageText(content: MessageContent): string {
  if (typeof content === 'string') {
    return content;
  }

  return content
    .map((part) => {
      switch (part.type) {
        case 'text':
          return part.text;
        case 'tool_call':
          return `[tool_call:${part.toolName}] ${safeJsonStringify(part.args)}`.trim();
        case 'tool_result':
          return `[tool_result:${part.toolName}] ${safeJsonStringify(part.result)}`.trim();
        case 'json':
          return safeJsonStringify(part.data);
        default:
          return '';
      }
    })
    .filter((segment) => segment.length > 0)
    .join('\n');
}

export function hasMessagePartType(
  content: MessageContent,
  type: MessagePart['type']
): boolean {
  return normalizeMessageContent(content).some((part) => part.type === type);
}

export function isToolTurnMessage(message: Message): boolean {
  return hasMessagePartType(message.content, 'tool_call') || hasMessagePartType(message.content, 'tool_result');
}

export function cloneMessage(message: Message): Message {
  return {
    ...message,
    content: typeof message.content === 'string'
      ? message.content
      : message.content.map((part) => cloneMessagePart(part)),
    metadata: message.metadata ? structuredClone(message.metadata) : undefined,
  };
}

export function serializeMessageContent(content: MessageContent): string {
  if (typeof content === 'string') {
    return content;
  }

  return JSON.stringify({ __codeflowMessageContent: true, parts: content });
}

export function deserializeMessageContent(content: string): MessageContent {
  if (!content.startsWith('{')) {
    return content;
  }

  try {
    const parsed = JSON.parse(content) as { __codeflowMessageContent?: boolean; parts?: MessagePart[] };
    if (parsed.__codeflowMessageContent && Array.isArray(parsed.parts)) {
      return parsed.parts.map((part) => cloneMessagePart(part));
    }
  } catch {
    return content;
  }

  return content;
}

function safeJsonStringify(value: unknown): string {
  if (value === undefined) {
    return '';
  }

  if (typeof value === 'string') {
    return value;
  }

  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

export interface RequestPayload {
  messages: Message[];
  model?: string;
  temperature?: number;
  maxTokens?: number;
  [key: string]: unknown;
}

export interface AIResponse {
  content: string;
  model: string;
  usage?: {
    promptTokens: number;
    completionTokens: number;
    totalTokens: number;
  };
  finishReason?: string;
}

export interface StreamChunk {
  delta: string;
  index: number;
  done: boolean;
}

export interface Context {
  messages: Message[];
  tokenCount: number;
}

export interface DecisionSkeleton {
  entities: string[];
  decisions: string[];
  relations: Array<{ from: string; to: string; type: string }>;
}

export interface ExecResult {
  command: string;
  exitCode: number;
  stdout: string;
  stderr: string;
  timestamp: number;
  sessionId?: string;
  taskId?: string;
  agentId?: string;
  filesModified?: string[];
  metadata?: Record<string, unknown>;
}

export type SnapshotID = string;

export type CodeChangeEventType =
  | 'file_edit'
  | 'batch_edit'
  | 'command_mutation'
  | 'formatting'
  | 'restore'
  | 'checkpoint_create';

export interface CodeChangeEvent {
  id: string;
  type: CodeChangeEventType;
  timestamp: number;
  summary: string;
  sessionId?: string;
  taskId?: string;
  agentId?: string;
  snapshotId?: string;
  files?: string[];
  metadata?: Record<string, unknown>;
}

export interface CodeChangeEventFilter {
  type?: CodeChangeEventType;
  sessionId?: string;
  taskId?: string;
  agentId?: string;
  snapshotId?: string;
  limit?: number;
}

export interface CodeChangeEventRecorder {
  appendCodeChangeEvent(event: Omit<CodeChangeEvent, 'id' | 'timestamp'>): CodeChangeEvent;
  listCodeChangeEvents(filter?: CodeChangeEventFilter): CodeChangeEvent[];
  clearCodeChangeEvents(): void;
  countCodeChangeEvents(): number;
}

export interface MemoryMatch {
  content: string;
  similarity: number;
  source: 'vector' | 'graph' | 'rules';
  metadata?: Record<string, unknown>;
}

/**
 * Hook Manager 核心接口
 */
export interface IHookManager {
  // 生命周期 Hooks
  hook_before_send(payload: RequestPayload): Promise<RequestPayload>;
  hook_post_response(response: AIResponse): Promise<void>;
  hook_on_stream(chunk: StreamChunk): void;

  // 上下文治理 Hooks
  hook_before_compress(context: Context): Promise<DecisionSkeleton>;
  hook_on_message_complete(message: Message): Promise<void>;

  // 状态管理 Hooks
  hook_after_exec(result: ExecResult): Promise<SnapshotID>;
  hook_restore_state(snapshotId: SnapshotID): Promise<void>;

  // 记忆检索 Hooks
  hook_on_user_input_submitted(input: string): Promise<MemoryMatch[]>;

  // 任务级 Hooks（Plan 模式集成）
  hook_before_task_execute(context: TaskExecutionContext): Promise<void>;
  hook_after_task_execute(result: TaskExecutionResult): Promise<void>;
  hook_on_task_failure(context: TaskFailureContext): Promise<void>;
  hook_on_task_complete(result: TaskExecutionResult): Promise<void>;
}

export interface HookRuntimeControls {
  enabled?: boolean;
  allowedHooks?: string[];
}

/**
 * Hook 处理函数类型
 */
export type HookHandler<T = unknown, R = void> = (data: T) => Promise<R> | R;

/**
 * 任务执行上下文（用于 task-level hooks）
 */
export interface TaskExecutionContext {
  taskId: string;
  planId: string;
  title: string;
  description: string;
  files?: string[];
  sessionId: string;
  metadata?: Record<string, unknown>;
}

/**
 * 任务执行结果（用于 hook_after_task_execute / hook_on_task_complete）
 */
export interface TaskExecutionResult {
  taskId: string;
  planId: string;
  title: string;
  status: 'completed' | 'failed';
  filesModified?: string[];
  output?: string;
  error?: string;
  durationMs?: number;
  sessionId: string;
  metadata?: Record<string, unknown>;
}

/**
 * 任务失败上下文（用于 hook_on_task_failure）
 */
export interface TaskFailureContext {
  taskId: string;
  planId: string;
  title: string;
  error: string;
  phase?: string;
  filesModified?: string[];
  sessionId: string;
  metadata?: Record<string, unknown>;
}

/**
 * Hook 事件类型枚举
 */
export enum HookEvent {
  BEFORE_SEND = 'before_send',
  POST_RESPONSE = 'post_response',
  ON_STREAM = 'on_stream',
  BEFORE_COMPRESS = 'before_compress',
  MESSAGE_COMPLETE = 'message_complete',
  AFTER_EXEC = 'after_exec',
  RESTORE_STATE = 'restore_state',
  USER_INPUT_SUBMITTED = 'user_input_submitted',
  BEFORE_TASK_EXECUTE = 'before_task_execute',
  AFTER_TASK_EXECUTE = 'after_task_execute',
  ON_TASK_FAILURE = 'on_task_failure',
  ON_TASK_COMPLETE = 'on_task_complete',
}
