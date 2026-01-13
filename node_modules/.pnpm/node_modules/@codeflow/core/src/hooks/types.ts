/**
 * Hook Bus 事件系统类型定义
 */

export interface RequestPayload {
  messages: Message[];
  model?: string;
  temperature?: number;
  maxTokens?: number;
  [key: string]: unknown;
}

export interface Message {
  role: 'user' | 'assistant' | 'system';
  content: string;
  timestamp?: number;
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
}

export type SnapshotID = string;

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
}

/**
 * Hook 处理函数类型
 */
export type HookHandler<T = unknown, R = void> = (data: T) => Promise<R> | R;

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
}
