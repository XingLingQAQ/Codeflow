/**
 * Gemini CLI 适配器
 * 基于 CLIProcessManager 封装 Gemini CLI 的非交互调用
 */
import { HookManager } from '../../hooks/HookManager.js';
import { CLIProcessManager } from '../process/CLIProcessManager.js';
import { ICLIAdapter, CLIResult, CLICapabilities, ExecuteOptions } from '../types.js';
/**
 * Gemini CLI 配置
 */
export interface GeminiCLIAdapterConfig {
    geminiPath?: string;
    model?: string;
    cwd?: string;
    env?: Record<string, string>;
    timeout?: number;
    extraArgs?: string[];
    sandbox?: boolean;
    includeDirectories?: string[];
}
/**
 * Gemini CLI 适配器
 *
 * 说明：当前只接线官方文档确认的 headless 文本 prompt 与 stream-json 事件流。
 * 官方文档未给出稳定的 CLI 图片输入参数，因此此适配器明确声明多模态输入暂不支持。
 */
export declare class GeminiCLIAdapter implements ICLIAdapter {
    readonly name = "gemini-cli";
    readonly version = "0.1.0";
    private config;
    private processManager;
    private currentProcessId;
    private hookManager?;
    constructor(config?: GeminiCLIAdapterConfig, processManager?: CLIProcessManager);
    setHookManager(hookManager?: HookManager): void;
    getHookManager(): HookManager | undefined;
    configure(config: Partial<GeminiCLIAdapterConfig>): void;
    getConfig(): GeminiCLIAdapterConfig;
    /**
     * 非流式执行 Gemini CLI
     */
    execute(command: string, options?: ExecuteOptions): Promise<CLIResult>;
    /**
     * 流式执行 Gemini CLI
     *
     * 当前按官方 headless stream-json 输出原样透传事件行，不假设特定 JSON schema。
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
    private buildVersionArgs;
    private buildPromptArgs;
    private runInvocation;
    private registerRuntimeGuards;
    private waitForExit;
    private disposeProcess;
}
//# sourceMappingURL=GeminiCLIAdapter.d.ts.map