/**
 * SpecInjector - 规范自动注入系统
 * 实现规范自动注入机制，根据任务类型选择并注入相关规范
 */
import { EventEmitter } from 'events';
import { SpecLibrary, SpecDocument, SpecDomain, SpecPriority } from './SpecLibrary.js';
/**
 * 任务类型
 */
export type TaskType = 'frontend' | 'backend' | 'fullstack' | 'api' | 'database' | 'testing' | 'documentation' | 'refactoring' | 'bugfix' | 'feature' | 'unknown';
/**
 * 上下文分析结果
 */
export interface ContextAnalysis {
    taskType: TaskType;
    confidence: number;
    keywords: string[];
    suggestedDomains: SpecDomain[];
    suggestedTags: string[];
}
/**
 * 注入结果
 */
export interface InjectionResult {
    specs: SpecDocument[];
    totalTokens: number;
    truncated: boolean;
    analysis: ContextAnalysis;
}
/**
 * 注入钩子
 */
export interface InjectionHook {
    id: string;
    name: string;
    trigger: 'startup' | 'task' | 'file' | 'manual';
    filter?: (spec: SpecDocument) => boolean;
    priority: number;
    enabled: boolean;
}
/**
 * 注入器配置
 */
export interface SpecInjectorConfig {
    maxTokens: number;
    priorityOrder: SpecPriority[];
    defaultDomains: SpecDomain[];
    enableHooks: boolean;
    traceInjections: boolean;
}
/**
 * ContextAnalyzer - 上下文分析器
 */
export declare class ContextAnalyzer extends EventEmitter {
    /**
     * 分析任务上下文
     */
    analyze(input: string): ContextAnalysis;
    /**
     * 从文件路径分析
     */
    analyzeFromPath(filePath: string): ContextAnalysis;
    /**
     * 提取标签
     */
    private extractTags;
    /**
     * 从路径提取标签
     */
    private extractTagsFromPath;
}
/**
 * RelevantSpecSelector - 相关规范选择器
 */
export declare class RelevantSpecSelector extends EventEmitter {
    private library;
    private config;
    constructor(library: SpecLibrary, config?: Partial<SpecInjectorConfig>);
    /**
     * 选择相关规范
     */
    select(analysis: ContextAnalysis): SpecDocument[];
    /**
     * 按优先级排序
     */
    private sortByPriority;
    /**
     * 应用 Token 限制
     */
    applyTokenLimit(specs: SpecDocument[]): {
        specs: SpecDocument[];
        truncated: boolean;
        totalTokens: number;
    };
    /**
     * 估算 Token 数量
     */
    private estimateTokens;
}
/**
 * SpecInjector - 规范注入器
 */
export declare class SpecInjector extends EventEmitter {
    private library;
    private analyzer;
    private selector;
    private config;
    private hooks;
    private injectionHistory;
    constructor(library: SpecLibrary, config?: Partial<SpecInjectorConfig>);
    /**
     * 注入规范
     */
    inject(input: string): Promise<InjectionResult>;
    /**
     * 从文件路径注入
     */
    injectFromPath(filePath: string): Promise<InjectionResult>;
    /**
     * 手动注入指定规范
     */
    injectManual(specIds: string[]): Promise<InjectionResult>;
    /**
     * 注册钩子
     */
    registerHook(hook: InjectionHook): void;
    /**
     * 移除钩子
     */
    unregisterHook(hookId: string): boolean;
    /**
     * 获取所有钩子
     */
    getHooks(): InjectionHook[];
    /**
     * 启用/禁用钩子
     */
    setHookEnabled(hookId: string, enabled: boolean): boolean;
    /**
     * 应用钩子过滤
     */
    private applyHooks;
    /**
     * 格式化注入内容
     */
    formatInjection(result: InjectionResult): string;
    /**
     * 获取注入历史
     */
    getInjectionHistory(): InjectionResult[];
    /**
     * 清除注入历史
     */
    clearHistory(): void;
    /**
     * 更新配置
     */
    updateConfig(config: Partial<SpecInjectorConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): SpecInjectorConfig;
}
//# sourceMappingURL=SpecInjector.d.ts.map