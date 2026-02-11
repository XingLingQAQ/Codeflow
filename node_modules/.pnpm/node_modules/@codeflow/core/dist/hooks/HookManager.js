import { EventEmitter } from 'events';
import { HookEvent, } from './types.js';
/**
 * Hook Bus 事件系统实现
 * 基于 EventEmitter 的事件订阅/发布机制
 */
export class HookManager extends EventEmitter {
    constructor() {
        super();
        this.handlers = new Map();
        this.initializeHandlers();
    }
    initializeHandlers() {
        // 初始化所有 Hook 事件的处理器集合
        Object.values(HookEvent).forEach((event) => {
            this.handlers.set(event, new Set());
        });
    }
    /**
     * 注册 Hook 处理器
     */
    register(event, handler) {
        const handlers = this.handlers.get(event);
        if (handlers) {
            handlers.add(handler);
        }
    }
    /**
     * 注销 Hook 处理器
     */
    unregister(event, handler) {
        const handlers = this.handlers.get(event);
        if (handlers) {
            handlers.delete(handler);
        }
    }
    /**
     * 执行 Hook 处理器链
     */
    async executeHandlers(event, data, reducer) {
        const handlers = this.handlers.get(event);
        if (!handlers || handlers.size === 0) {
            return undefined;
        }
        let result;
        for (const handler of handlers) {
            const handlerResult = await handler(data);
            if (reducer && handlerResult !== undefined) {
                result = reducer(result, handlerResult);
            }
            else if (handlerResult !== undefined) {
                result = handlerResult;
            }
        }
        // 触发 EventEmitter 事件
        this.emit(event, data, result);
        return result;
    }
    /**
     * 生命周期 Hook: 发送前拦截
     */
    async hook_before_send(payload) {
        const result = await this.executeHandlers(HookEvent.BEFORE_SEND, payload, (_, current) => current // 使用最后一个处理器的结果
        );
        return result || payload;
    }
    /**
     * 生命周期 Hook: 响应后处理
     */
    async hook_post_response(response) {
        await this.executeHandlers(HookEvent.POST_RESPONSE, response);
    }
    /**
     * 生命周期 Hook: 流式输出处理
     */
    hook_on_stream(chunk) {
        const handlers = this.handlers.get(HookEvent.ON_STREAM);
        if (handlers) {
            handlers.forEach((handler) => handler(chunk));
        }
        this.emit(HookEvent.ON_STREAM, chunk);
    }
    /**
     * 上下文治理 Hook: 压缩前导图生成
     */
    async hook_before_compress(context) {
        const result = await this.executeHandlers(HookEvent.BEFORE_COMPRESS, context);
        return (result || {
            entities: [],
            decisions: [],
            relations: [],
        });
    }
    /**
     * 上下文治理 Hook: 消息完成处理
     */
    async hook_on_message_complete(message) {
        await this.executeHandlers(HookEvent.MESSAGE_COMPLETE, message);
    }
    /**
     * 状态管理 Hook: 执行后快照生成
     */
    async hook_after_exec(result) {
        const snapshotId = await this.executeHandlers(HookEvent.AFTER_EXEC, result);
        return snapshotId || `snapshot_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`;
    }
    /**
     * 状态管理 Hook: 状态恢复
     */
    async hook_restore_state(snapshotId) {
        await this.executeHandlers(HookEvent.RESTORE_STATE, snapshotId);
    }
    /**
     * 记忆检索 Hook: 用户输入提交时触发
     */
    async hook_on_user_input_submitted(input) {
        const results = await this.executeHandlers(HookEvent.USER_INPUT_SUBMITTED, input, (acc, current) => [...(acc || []), ...current] // 合并所有处理器的结果
        );
        return results || [];
    }
    /**
     * 任务级 Hook: 任务执行前（加载意图文档、相关记忆）
     */
    async hook_before_task_execute(context) {
        await this.executeHandlers(HookEvent.BEFORE_TASK_EXECUTE, context);
    }
    /**
     * 任务级 Hook: 任务执行后（存储原子记忆）
     */
    async hook_after_task_execute(result) {
        await this.executeHandlers(HookEvent.AFTER_TASK_EXECUTE, result);
    }
    /**
     * 任务级 Hook: 任务失败时（检索历史修复方案）
     */
    async hook_on_task_failure(context) {
        await this.executeHandlers(HookEvent.ON_TASK_FAILURE, context);
    }
    /**
     * 任务级 Hook: 任务完成后（更新用户画像、同步意图文档）
     */
    async hook_on_task_complete(result) {
        await this.executeHandlers(HookEvent.ON_TASK_COMPLETE, result);
    }
    /**
     * 清理所有处理器
     */
    clear() {
        this.handlers.forEach((handlers) => handlers.clear());
        this.removeAllListeners();
    }
}
//# sourceMappingURL=HookManager.js.map