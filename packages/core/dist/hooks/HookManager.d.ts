import { EventEmitter } from 'events';
import { IHookManager, RequestPayload, AIResponse, StreamChunk, Context, DecisionSkeleton, Message, ExecResult, SnapshotID, MemoryMatch, HookEvent, HookHandler, TaskExecutionContext, TaskExecutionResult, TaskFailureContext } from './types.js';
/**
 * Hook Bus 事件系统实现
 * 基于 EventEmitter 的事件订阅/发布机制
 */
export declare class HookManager extends EventEmitter implements IHookManager {
    private handlers;
    constructor();
    private initializeHandlers;
    /**
     * 注册 Hook 处理器
     */
    register<T, R>(event: HookEvent, handler: HookHandler<T, R>): void;
    /**
     * 注销 Hook 处理器
     */
    unregister<T, R>(event: HookEvent, handler: HookHandler<T, R>): void;
    /**
     * 执行 Hook 处理器链
     */
    private executeHandlers;
    /**
     * 生命周期 Hook: 发送前拦截
     */
    hook_before_send(payload: RequestPayload): Promise<RequestPayload>;
    /**
     * 生命周期 Hook: 响应后处理
     */
    hook_post_response(response: AIResponse): Promise<void>;
    /**
     * 生命周期 Hook: 流式输出处理
     */
    hook_on_stream(chunk: StreamChunk): void;
    /**
     * 上下文治理 Hook: 压缩前导图生成
     */
    hook_before_compress(context: Context): Promise<DecisionSkeleton>;
    /**
     * 上下文治理 Hook: 消息完成处理
     */
    hook_on_message_complete(message: Message): Promise<void>;
    /**
     * 状态管理 Hook: 执行后快照生成
     */
    hook_after_exec(result: ExecResult): Promise<SnapshotID>;
    /**
     * 状态管理 Hook: 状态恢复
     */
    hook_restore_state(snapshotId: SnapshotID): Promise<void>;
    /**
     * 记忆检索 Hook: 用户输入提交时触发
     */
    hook_on_user_input_submitted(input: string): Promise<MemoryMatch[]>;
    /**
     * 任务级 Hook: 任务执行前（加载意图文档、相关记忆）
     */
    hook_before_task_execute(context: TaskExecutionContext): Promise<void>;
    /**
     * 任务级 Hook: 任务执行后（存储原子记忆）
     */
    hook_after_task_execute(result: TaskExecutionResult): Promise<void>;
    /**
     * 任务级 Hook: 任务失败时（检索历史修复方案）
     */
    hook_on_task_failure(context: TaskFailureContext): Promise<void>;
    /**
     * 任务级 Hook: 任务完成后（更新用户画像、同步意图文档）
     */
    hook_on_task_complete(result: TaskExecutionResult): Promise<void>;
    /**
     * 清理所有处理器
     */
    clear(): void;
}
//# sourceMappingURL=HookManager.d.ts.map