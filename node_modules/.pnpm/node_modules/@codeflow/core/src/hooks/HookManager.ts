import { EventEmitter } from 'events';
import {
  IHookManager,
  RequestPayload,
  AIResponse,
  StreamChunk,
  Context,
  DecisionSkeleton,
  Message,
  ExecResult,
  SnapshotID,
  MemoryMatch,
  HookEvent,
  HookHandler,
  TaskExecutionContext,
  TaskExecutionResult,
  TaskFailureContext,
} from './types.js';

/**
 * Hook Bus 事件系统实现
 * 基于 EventEmitter 的事件订阅/发布机制
 */
export class HookManager extends EventEmitter implements IHookManager {
  private handlers: Map<HookEvent, Set<HookHandler>> = new Map();

  constructor() {
    super();
    this.initializeHandlers();
  }

  private initializeHandlers(): void {
    // 初始化所有 Hook 事件的处理器集合
    Object.values(HookEvent).forEach((event) => {
      this.handlers.set(event, new Set());
    });
  }

  /**
   * 注册 Hook 处理器
   */
  public register<T, R>(event: HookEvent, handler: HookHandler<T, R>): void {
    const handlers = this.handlers.get(event);
    if (handlers) {
      handlers.add(handler as HookHandler);
    }
  }

  /**
   * 注销 Hook 处理器
   */
  public unregister<T, R>(event: HookEvent, handler: HookHandler<T, R>): void {
    const handlers = this.handlers.get(event);
    if (handlers) {
      handlers.delete(handler as HookHandler);
    }
  }

  /**
   * 执行 Hook 处理器链
   */
  private async executeHandlers<T, R>(
    event: HookEvent,
    data: T,
    reducer?: (acc: R, result: R) => R
  ): Promise<R | undefined> {
    const handlers = this.handlers.get(event);
    if (!handlers || handlers.size === 0) {
      return undefined;
    }

    let result: R | undefined;
    for (const handler of handlers) {
      const handlerResult = await handler(data);
      if (reducer && handlerResult !== undefined) {
        result = reducer(result as R, handlerResult as R);
      } else if (handlerResult !== undefined) {
        result = handlerResult as R;
      }
    }

    // 触发 EventEmitter 事件
    this.emit(event, data, result);

    return result;
  }

  /**
   * 生命周期 Hook: 发送前拦截
   */
  async hook_before_send(payload: RequestPayload): Promise<RequestPayload> {
    const result = await this.executeHandlers<RequestPayload, RequestPayload>(
      HookEvent.BEFORE_SEND,
      payload,
      (_, current) => current // 使用最后一个处理器的结果
    );
    return result || payload;
  }

  /**
   * 生命周期 Hook: 响应后处理
   */
  async hook_post_response(response: AIResponse): Promise<void> {
    await this.executeHandlers<AIResponse, void>(HookEvent.POST_RESPONSE, response);
  }

  /**
   * 生命周期 Hook: 流式输出处理
   */
  hook_on_stream(chunk: StreamChunk): void {
    const handlers = this.handlers.get(HookEvent.ON_STREAM);
    if (handlers) {
      handlers.forEach((handler) => handler(chunk));
    }
    this.emit(HookEvent.ON_STREAM, chunk);
  }

  /**
   * 上下文治理 Hook: 压缩前导图生成
   */
  async hook_before_compress(context: Context): Promise<DecisionSkeleton> {
    const result = await this.executeHandlers<Context, DecisionSkeleton>(
      HookEvent.BEFORE_COMPRESS,
      context
    );
    return (
      result || {
        entities: [],
        decisions: [],
        relations: [],
      }
    );
  }

  /**
   * 上下文治理 Hook: 消息完成处理
   */
  async hook_on_message_complete(message: Message): Promise<void> {
    await this.executeHandlers<Message, void>(HookEvent.MESSAGE_COMPLETE, message);
  }

  /**
   * 状态管理 Hook: 执行后快照生成
   */
  async hook_after_exec(result: ExecResult): Promise<SnapshotID> {
    const snapshotId = await this.executeHandlers<ExecResult, SnapshotID>(
      HookEvent.AFTER_EXEC,
      result
    );
    return snapshotId || `snapshot_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`;
  }

  /**
   * 状态管理 Hook: 状态恢复
   */
  async hook_restore_state(snapshotId: SnapshotID): Promise<void> {
    await this.executeHandlers<SnapshotID, void>(HookEvent.RESTORE_STATE, snapshotId);
  }

  /**
   * 记忆检索 Hook: 用户输入提交时触发
   */
  async hook_on_user_input_submitted(input: string): Promise<MemoryMatch[]> {
    const results = await this.executeHandlers<string, MemoryMatch[]>(
      HookEvent.USER_INPUT_SUBMITTED,
      input,
      (acc, current) => [...(acc || []), ...current] // 合并所有处理器的结果
    );
    return results || [];
  }

  /**
   * 任务级 Hook: 任务执行前（加载意图文档、相关记忆）
   */
  async hook_before_task_execute(context: TaskExecutionContext): Promise<void> {
    await this.executeHandlers<TaskExecutionContext, void>(
      HookEvent.BEFORE_TASK_EXECUTE,
      context
    );
  }

  /**
   * 任务级 Hook: 任务执行后（存储原子记忆）
   */
  async hook_after_task_execute(result: TaskExecutionResult): Promise<void> {
    await this.executeHandlers<TaskExecutionResult, void>(
      HookEvent.AFTER_TASK_EXECUTE,
      result
    );
  }

  /**
   * 任务级 Hook: 任务失败时（检索历史修复方案）
   */
  async hook_on_task_failure(context: TaskFailureContext): Promise<void> {
    await this.executeHandlers<TaskFailureContext, void>(
      HookEvent.ON_TASK_FAILURE,
      context
    );
  }

  /**
   * 任务级 Hook: 任务完成后（更新用户画像、同步意图文档）
   */
  async hook_on_task_complete(result: TaskExecutionResult): Promise<void> {
    await this.executeHandlers<TaskExecutionResult, void>(
      HookEvent.ON_TASK_COMPLETE,
      result
    );
  }

  /**
   * 清理所有处理器
   */
  public clear(): void {
    this.handlers.forEach((handlers) => handlers.clear());
    this.removeAllListeners();
  }
}
