/**
 * MemoryShadowIntegration - Plan 模式与记忆/影子系统集成
 *
 * 将记忆系统和影子系统集成到 Plan 模式的任务执行生命周期中：
 * - hook_before_task_execute: 加载相关意图文档
 * - hook_after_task_execute: 存储原子记忆
 * - hook_on_task_failure: 检索历史修复方案
 * - hook_on_task_complete: 更新用户画像 + 同步意图文档
 */
import { HookManager } from '../../hooks/HookManager.js';
import { AtomicMemoryService } from '../../memory/AtomicMemoryService.js';
import { BatchProjector } from '../../shadow/BatchProjector.js';
export interface MemoryShadowIntegrationConfig {
    /** .codeflow 影子目录根路径 */
    shadowRoot: string;
    /** 项目根路径 */
    projectRoot: string;
    /** 检索相关记忆的最大条数 */
    maxRelevantMemories: number;
    /** 检索历史修复方案的最大条数 */
    maxFailureMemories: number;
    /** 是否在任务完成后自动同步意图文档 */
    autoSyncIntentDocs: boolean;
}
export declare class MemoryShadowIntegration {
    private readonly hookManager;
    private readonly memoryService;
    private readonly batchProjector?;
    private readonly config;
    constructor(hookManager: HookManager, memoryService: AtomicMemoryService, config?: Partial<MemoryShadowIntegrationConfig>, batchProjector?: BatchProjector);
    /**
     * 注册所有 task-level hooks
     */
    register(): void;
    /**
     * 注销所有 task-level hooks
     */
    unregister(): void;
    /**
     * hook_before_task_execute: 任务执行前加载相关意图文档和记忆
     *
     * 1) 从 .codeflow/domain/ 加载与任务相关文件的意图文档
     * 2) 检索与任务描述语义相关的原子记忆
     * 3) 将加载的上下文注入到任务 metadata 中
     */
    private onBeforeTaskExecute;
    /**
     * hook_after_task_execute: 任务执行后存储原子记忆
     *
     * 将任务执行结果作为原子记忆存储，便于后续检索
     */
    private onAfterTaskExecute;
    /**
     * hook_on_task_failure: 任务失败时检索历史修复方案
     *
     * 1) 基于错误信息检索历史记忆中的类似失败和修复方案
     * 2) 将检索结果注入到失败上下文的 metadata 中
     */
    private onTaskFailure;
    /**
     * hook_on_task_complete: 任务完成后更新画像 + 同步意图文档
     *
     * 1) 存储任务完成记忆（含修改文件列表）
     * 2) 如果配置了 autoSyncIntentDocs，对修改的文件重新投影意图文档
     */
    private onTaskComplete;
    /**
     * 加载与任务相关文件的意图文档
     */
    private loadRelevantIntentDocs;
    /**
     * 检索与任务描述语义相关的原子记忆
     */
    private searchRelevantMemories;
    /**
     * 构建任务执行记忆内容
     */
    private buildTaskMemoryContent;
    /**
     * 构建任务记忆标签
     */
    private buildTaskMemoryTags;
    /**
     * 将源文件路径映射到意图文档路径
     */
    private resolveIntentDocPath;
    /**
     * 对修改的文件重新投影意图文档
     */
    private syncIntentDocs;
}
//# sourceMappingURL=MemoryShadowIntegration.d.ts.map