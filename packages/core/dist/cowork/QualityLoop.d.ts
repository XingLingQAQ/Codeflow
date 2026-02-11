/**
 * QualityLoop - 质量控制循环
 * 实现 Trellis 的 ralph-loop 质量控制循环：Check → Fix → Check
 */
import { EventEmitter } from 'events';
/**
 * 检查类型
 */
export type CheckType = 'lint' | 'type' | 'test' | 'security' | 'performance' | 'style' | 'documentation' | 'custom';
/**
 * 检查严重程度
 */
export type CheckSeverity = 'error' | 'warning' | 'info';
/**
 * 检查问题
 */
export interface CheckIssue {
    id: string;
    type: CheckType;
    severity: CheckSeverity;
    message: string;
    file?: string;
    line?: number;
    column?: number;
    rule?: string;
    suggestion?: string;
    autoFixable: boolean;
}
/**
 * 检查结果
 */
export interface CheckResult {
    passed: boolean;
    issues: CheckIssue[];
    summary: {
        errors: number;
        warnings: number;
        infos: number;
        autoFixable: number;
    };
    duration: number;
    checkType: CheckType;
}
/**
 * 修复结果
 */
export interface FixResult {
    success: boolean;
    fixedIssues: string[];
    failedIssues: string[];
    filesModified: string[];
    duration: number;
}
/**
 * 迭代结果
 */
export interface IterationResult {
    iteration: number;
    checkResult: CheckResult;
    fixResult?: FixResult;
    status: 'passed' | 'fixed' | 'failed' | 'manual_required';
}
/**
 * 循环结果
 */
export interface LoopResult {
    passed: boolean;
    iterations: IterationResult[];
    totalDuration: number;
    finalIssues: CheckIssue[];
    requiresManualIntervention: boolean;
    summary: string;
}
/**
 * Check Agent 回调
 */
export type CheckAgentCallback = (files: string[], checkTypes: CheckType[]) => Promise<CheckResult>;
/**
 * Fix Agent 回调
 */
export type FixAgentCallback = (issues: CheckIssue[]) => Promise<FixResult>;
/**
 * Quality Loop 配置
 */
export interface QualityLoopConfig {
    maxIterations: number;
    checkTypes: CheckType[];
    autoFixEnabled: boolean;
    stopOnError: boolean;
    checkModelId?: string;
    fixModelId?: string;
    timeout: number;
}
/**
 * CheckAgent - 检查 Agent
 */
export declare class CheckAgent extends EventEmitter {
    private callback?;
    private modelId?;
    constructor(callback?: CheckAgentCallback, modelId?: string);
    /**
     * 执行检查
     */
    check(files: string[], checkTypes: CheckType[]): Promise<CheckResult>;
    private generateStaticCheck;
    /**
     * 获取模型 ID
     */
    getModelId(): string | undefined;
}
/**
 * FixAgent - 修复 Agent
 */
export declare class FixAgent extends EventEmitter {
    private callback?;
    private modelId?;
    constructor(callback?: FixAgentCallback, modelId?: string);
    /**
     * 执行修复
     */
    fix(issues: CheckIssue[]): Promise<FixResult>;
    private generateStaticFix;
    /**
     * 获取模型 ID
     */
    getModelId(): string | undefined;
}
/**
 * IterationManager - 迭代管理器
 */
export declare class IterationManager extends EventEmitter {
    private iterations;
    private maxIterations;
    constructor(maxIterations?: number);
    /**
     * 记录迭代
     */
    recordIteration(result: IterationResult): void;
    /**
     * 是否可以继续迭代
     */
    canContinue(): boolean;
    /**
     * 获取当前迭代次数
     */
    getCurrentIteration(): number;
    /**
     * 获取所有迭代
     */
    getIterations(): IterationResult[];
    /**
     * 获取最后一次迭代
     */
    getLastIteration(): IterationResult | undefined;
    /**
     * 重置
     */
    reset(): void;
    /**
     * 更新最大迭代次数
     */
    setMaxIterations(max: number): void;
}
/**
 * QualityLoop - 质量控制循环
 */
export declare class QualityLoop extends EventEmitter {
    private config;
    private checkAgent;
    private fixAgent;
    private iterationManager;
    private running;
    constructor(config?: Partial<QualityLoopConfig>, callbacks?: {
        check?: CheckAgentCallback;
        fix?: FixAgentCallback;
    });
    /**
     * 执行质量循环
     */
    run(files: string[]): Promise<LoopResult>;
    /**
     * 构建循环结果
     */
    private buildLoopResult;
    /**
     * 停止循环
     */
    stop(): void;
    /**
     * 是否正在运行
     */
    isRunning(): boolean;
    /**
     * 获取当前迭代次数
     */
    getCurrentIteration(): number;
    /**
     * 获取迭代历史
     */
    getIterationHistory(): IterationResult[];
    /**
     * 更新配置
     */
    updateConfig(config: Partial<QualityLoopConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): QualityLoopConfig;
    /**
     * 获取 Check Agent
     */
    getCheckAgent(): CheckAgent;
    /**
     * 获取 Fix Agent
     */
    getFixAgent(): FixAgent;
}
//# sourceMappingURL=QualityLoop.d.ts.map