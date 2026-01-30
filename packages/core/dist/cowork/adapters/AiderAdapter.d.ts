/**
 * Aider CLI 适配器
 * 封装 Aider CLI 调用，提供统一的 ICLIAdapter 接口
 */
import { ICLIAdapter, CLIResult, CLICapabilities, ExecuteOptions, DiffHunk } from '../types.js';
/**
 * Aider 配置
 */
export interface AiderConfig {
    aiderPath?: string;
    model?: string;
    editFormat?: 'diff' | 'whole' | 'diff-fenced';
    autoConfirm?: boolean;
    stream?: boolean;
    cwd?: string;
    env?: Record<string, string>;
}
/**
 * Aider 适配器
 */
export declare class AiderAdapter implements ICLIAdapter {
    readonly name = "aider";
    readonly version = "0.1.0";
    private config;
    private currentProcess;
    constructor(config?: AiderConfig);
    /**
     * 执行 Aider 命令
     */
    execute(command: string, options?: ExecuteOptions): Promise<CLIResult>;
    /**
     * 流式执行 Aider 命令
     */
    stream(command: string, onChunk: (data: string) => void, options?: ExecuteOptions): Promise<void>;
    /**
     * 中断当前执行
     */
    interrupt(): Promise<void>;
    /**
     * 健康检查
     */
    healthCheck(): Promise<boolean>;
    /**
     * 获取能力描述
     */
    getCapabilities(): CLICapabilities;
    /**
     * 解析 Aider 输出的 diff
     */
    parseDiff(output: string): ParsedDiff[];
    /**
     * 解析单个 diff
     */
    private parseSingleDiff;
    /**
     * 构建命令行参数
     */
    private buildArgs;
    /**
     * 配置更新
     */
    configure(config: Partial<AiderConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): AiderConfig;
}
/**
 * 解析后的 Diff（Aider 特有格式）
 */
export interface ParsedDiff {
    file: string;
    hunks: DiffHunk[];
    additions: number;
    deletions: number;
}
//# sourceMappingURL=AiderAdapter.d.ts.map