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
export type MessagePart = MessageTextPart | MessageToolCallPart | MessageToolResultPart | MessageJsonPart;
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
export declare function cloneMessagePart(part: MessagePart): MessagePart;
export declare function normalizeMessageContent(content: MessageContent): MessagePart[];
export declare function getMessageText(content: MessageContent): string;
export declare function hasMessagePartType(content: MessageContent, type: MessagePart['type']): boolean;
export declare function isToolTurnMessage(message: Message): boolean;
export declare function cloneMessage(message: Message): Message;
export declare function serializeMessageContent(content: MessageContent): string;
export declare function deserializeMessageContent(content: string): MessageContent;
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
    relations: Array<{
        from: string;
        to: string;
        type: string;
    }>;
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
export type CodeChangeEventType = 'file_edit' | 'batch_edit' | 'command_mutation' | 'formatting' | 'restore' | 'checkpoint_create';
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
    hook_before_send(payload: RequestPayload): Promise<RequestPayload>;
    hook_post_response(response: AIResponse): Promise<void>;
    hook_on_stream(chunk: StreamChunk): void;
    hook_before_compress(context: Context): Promise<DecisionSkeleton>;
    hook_on_message_complete(message: Message): Promise<void>;
    hook_after_exec(result: ExecResult): Promise<SnapshotID>;
    hook_restore_state(snapshotId: SnapshotID): Promise<void>;
    hook_on_user_input_submitted(input: string): Promise<MemoryMatch[]>;
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
export declare enum HookEvent {
    BEFORE_SEND = "before_send",
    POST_RESPONSE = "post_response",
    ON_STREAM = "on_stream",
    BEFORE_COMPRESS = "before_compress",
    MESSAGE_COMPLETE = "message_complete",
    AFTER_EXEC = "after_exec",
    RESTORE_STATE = "restore_state",
    USER_INPUT_SUBMITTED = "user_input_submitted",
    BEFORE_TASK_EXECUTE = "before_task_execute",
    AFTER_TASK_EXECUTE = "after_task_execute",
    ON_TASK_FAILURE = "on_task_failure",
    ON_TASK_COMPLETE = "on_task_complete"
}
//# sourceMappingURL=types.d.ts.map