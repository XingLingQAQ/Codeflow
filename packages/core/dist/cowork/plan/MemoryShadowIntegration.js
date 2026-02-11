/**
 * MemoryShadowIntegration - Plan 模式与记忆/影子系统集成
 *
 * 将记忆系统和影子系统集成到 Plan 模式的任务执行生命周期中：
 * - hook_before_task_execute: 加载相关意图文档
 * - hook_after_task_execute: 存储原子记忆
 * - hook_on_task_failure: 检索历史修复方案
 * - hook_on_task_complete: 更新用户画像 + 同步意图文档
 */
import * as fs from 'fs';
import * as path from 'path';
import { HookEvent, } from '../../hooks/types.js';
const DEFAULT_CONFIG = {
    shadowRoot: '.codeflow',
    projectRoot: '.',
    maxRelevantMemories: 5,
    maxFailureMemories: 3,
    autoSyncIntentDocs: true,
};
export class MemoryShadowIntegration {
    constructor(hookManager, memoryService, config = {}, batchProjector) {
        this.hookManager = hookManager;
        this.memoryService = memoryService;
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.batchProjector = batchProjector;
    }
    /**
     * 注册所有 task-level hooks
     */
    register() {
        this.hookManager.register(HookEvent.BEFORE_TASK_EXECUTE, this.onBeforeTaskExecute.bind(this));
        this.hookManager.register(HookEvent.AFTER_TASK_EXECUTE, this.onAfterTaskExecute.bind(this));
        this.hookManager.register(HookEvent.ON_TASK_FAILURE, this.onTaskFailure.bind(this));
        this.hookManager.register(HookEvent.ON_TASK_COMPLETE, this.onTaskComplete.bind(this));
    }
    /**
     * 注销所有 task-level hooks
     */
    unregister() {
        this.hookManager.unregister(HookEvent.BEFORE_TASK_EXECUTE, this.onBeforeTaskExecute.bind(this));
        this.hookManager.unregister(HookEvent.AFTER_TASK_EXECUTE, this.onAfterTaskExecute.bind(this));
        this.hookManager.unregister(HookEvent.ON_TASK_FAILURE, this.onTaskFailure.bind(this));
        this.hookManager.unregister(HookEvent.ON_TASK_COMPLETE, this.onTaskComplete.bind(this));
    }
    /**
     * hook_before_task_execute: 任务执行前加载相关意图文档和记忆
     *
     * 1) 从 .codeflow/domain/ 加载与任务相关文件的意图文档
     * 2) 检索与任务描述语义相关的原子记忆
     * 3) 将加载的上下文注入到任务 metadata 中
     */
    async onBeforeTaskExecute(context) {
        try {
            const intentDocs = await this.loadRelevantIntentDocs(context);
            const relevantMemories = await this.searchRelevantMemories(context);
            if (!context.metadata) {
                context.metadata = {};
            }
            if (intentDocs.length > 0) {
                context.metadata._intentDocs = intentDocs;
            }
            if (relevantMemories.length > 0) {
                context.metadata._relevantMemories = relevantMemories.map((m) => ({
                    content: m.content,
                    tags: m.tags,
                    importance: m.importance,
                }));
            }
        }
        catch {
            // 加载失败不阻塞任务执行
        }
    }
    /**
     * hook_after_task_execute: 任务执行后存储原子记忆
     *
     * 将任务执行结果作为原子记忆存储，便于后续检索
     */
    async onAfterTaskExecute(result) {
        try {
            const content = this.buildTaskMemoryContent(result);
            const tags = this.buildTaskMemoryTags(result);
            await this.memoryService.add({
                id: `task_${result.taskId}_${Date.now()}`,
                timestamp: Math.floor(Date.now() / 1000),
                content,
                tags,
                sessionId: result.sessionId,
                source: 'system',
                importance: result.status === 'completed' ? 0.7 : 0.5,
            });
        }
        catch {
            // 存储失败不阻塞后续流程
        }
    }
    /**
     * hook_on_task_failure: 任务失败时检索历史修复方案
     *
     * 1) 基于错误信息检索历史记忆中的类似失败和修复方案
     * 2) 将检索结果注入到失败上下文的 metadata 中
     */
    async onTaskFailure(context) {
        try {
            // 存储失败记忆
            await this.memoryService.add({
                id: `failure_${context.taskId}_${Date.now()}`,
                timestamp: Math.floor(Date.now() / 1000),
                content: `任务失败: ${context.title} - ${context.error}${context.phase ? ` (阶段: ${context.phase})` : ''}`,
                tags: ['task_failure', context.planId, ...(context.phase ? [context.phase] : [])],
                sessionId: context.sessionId,
                source: 'system',
                importance: 0.8,
            });
            // 检索历史修复方案
            const historicalFixes = await this.memoryService.search(`修复 ${context.error} ${context.title}`, {
                tags: ['task_failure'],
                limit: this.config.maxFailureMemories,
            });
            if (!context.metadata) {
                context.metadata = {};
            }
            if (historicalFixes.length > 0) {
                context.metadata._historicalFixes = historicalFixes.map((m) => ({
                    content: m.content,
                    tags: m.tags,
                    timestamp: m.timestamp,
                }));
            }
        }
        catch {
            // 检索失败不阻塞错误处理流程
        }
    }
    /**
     * hook_on_task_complete: 任务完成后更新画像 + 同步意图文档
     *
     * 1) 存储任务完成记忆（含修改文件列表）
     * 2) 如果配置了 autoSyncIntentDocs，对修改的文件重新投影意图文档
     */
    async onTaskComplete(result) {
        try {
            // 存储完成记忆
            const completionContent = [
                `任务完成: ${result.title}`,
                result.filesModified?.length
                    ? `修改文件: ${result.filesModified.join(', ')}`
                    : '',
                result.output ? `输出: ${result.output.slice(0, 200)}` : '',
            ]
                .filter(Boolean)
                .join('; ');
            await this.memoryService.add({
                id: `complete_${result.taskId}_${Date.now()}`,
                timestamp: Math.floor(Date.now() / 1000),
                content: completionContent,
                tags: ['task_complete', result.planId, ...(result.filesModified?.slice(0, 3) || [])],
                sessionId: result.sessionId,
                source: 'system',
                importance: 0.6,
            });
            // 同步意图文档
            if (this.config.autoSyncIntentDocs && this.batchProjector && result.filesModified?.length) {
                await this.syncIntentDocs(result.filesModified);
            }
        }
        catch {
            // 完成后处理失败不阻塞
        }
    }
    /**
     * 加载与任务相关文件的意图文档
     */
    async loadRelevantIntentDocs(context) {
        const docs = [];
        const files = context.files || [];
        for (const file of files) {
            const intentPath = this.resolveIntentDocPath(file);
            try {
                const content = await fs.promises.readFile(intentPath, 'utf-8');
                if (content.trim()) {
                    docs.push(content.trim());
                }
            }
            catch {
                // 意图文档不存在则跳过
            }
        }
        return docs;
    }
    /**
     * 检索与任务描述语义相关的原子记忆
     */
    async searchRelevantMemories(context) {
        const query = `${context.title} ${context.description}`.trim();
        if (!query)
            return [];
        return this.memoryService.search(query, {
            limit: this.config.maxRelevantMemories,
            sessionId: context.sessionId,
        });
    }
    /**
     * 构建任务执行记忆内容
     */
    buildTaskMemoryContent(result) {
        const parts = [
            `任务${result.status === 'completed' ? '完成' : '执行'}: ${result.title}`,
        ];
        if (result.filesModified?.length) {
            parts.push(`修改文件: ${result.filesModified.join(', ')}`);
        }
        if (result.durationMs) {
            parts.push(`耗时: ${Math.round(result.durationMs / 1000)}s`);
        }
        if (result.error) {
            parts.push(`错误: ${result.error}`);
        }
        return parts.join('; ');
    }
    /**
     * 构建任务记忆标签
     */
    buildTaskMemoryTags(result) {
        const tags = ['task_execution', result.planId];
        if (result.status === 'completed') {
            tags.push('task_complete');
        }
        else {
            tags.push('task_failed');
        }
        return tags;
    }
    /**
     * 将源文件路径映射到意图文档路径
     */
    resolveIntentDocPath(sourceFile) {
        const projectRoot = path.resolve(this.config.projectRoot);
        const relativePath = path.relative(projectRoot, path.resolve(sourceFile));
        const ext = path.extname(relativePath);
        const withoutExt = relativePath.slice(0, relativePath.length - ext.length);
        return path.join(projectRoot, this.config.shadowRoot, 'domain', `${withoutExt}.intent.md`);
    }
    /**
     * 对修改的文件重新投影意图文档
     */
    async syncIntentDocs(filesModified) {
        if (!this.batchProjector)
            return;
        const projectRoot = path.resolve(this.config.projectRoot);
        for (const file of filesModified) {
            const resolvedFile = path.resolve(file);
            const ext = path.extname(resolvedFile).toLowerCase();
            if (!['.ts', '.js', '.go', '.py'].includes(ext)) {
                continue;
            }
            try {
                await this.batchProjector.projectFile(resolvedFile, projectRoot);
            }
            catch {
                // 单文件投影失败不中断批量同步
            }
        }
    }
}
//# sourceMappingURL=MemoryShadowIntegration.js.map