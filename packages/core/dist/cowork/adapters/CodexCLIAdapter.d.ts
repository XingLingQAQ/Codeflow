/**
 * Codex CLI 适配器
 * 基于 CLIProcessManager 封装 Codex CLI 的非交互调用
 */
import { HookManager } from '../../hooks/HookManager.js';
import { CLIProcessManager } from '../process/CLIProcessManager.js';
import { ICLIAdapter, CLIResult, CLICapabilities, ExecuteOptions } from '../types.js';
/**
 * Codex CLI 配置
 */
export interface CodexCLIAdapterConfig {
    codexPath?: string;
    model?: string;
    cwd?: string;
    env?: Record<string, string>;
    timeout?: number;
    extraArgs?: string[];
    sandbox?: 'read-only' | 'workspace-write' | 'danger-full-access' | string;
    skipGitRepoCheck?: boolean;
    ephemeral?: boolean;
    outputLastMessage?: boolean;
    outputDirectory?: string;
}
/**
 * Codex CLI 适配器
 */
export declare class CodexCLIAdapter implements ICLIAdapter {
    readonly name = "codex-cli";
    readonly version = "0.1.0";
    private config;
    private processManager;
    private currentProcessId;
    private hookManager?;
    constructor(config?: CodexCLIAdapterConfig, processManager?: CLIProcessManager);
    setHookManager(hookManager?: HookManager): void;
    getHookManager(): HookManager | undefined;
    configure(config: Partial<CodexCLIAdapterConfig>): void;
    getConfig(): CodexCLIAdapterConfig;
    /**
     * 非流式执行 Codex CLI
     */
    execute(command: string, options?: ExecuteOptions): Promise<CLIResult>;
    /**
     * 流式执行 Codex CLI
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
    private applyBeforeSendHooks;
    private toPrompt;
    private createOutputCapture;
    private resolveOutput;
    private cleanupOutputCapture;
    private buildVersionArgs;
    private buildExecArgs;
    private runInvocation;
    private registerRuntimeGuards;
    private waitForExit;
    private disposeProcess;
}
//# sourceMappingURL=CodexCLIAdapter.d.ts.map