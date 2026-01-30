/**
 * CLI 进程管理器
 * 管理 CLI 进程的生命周期：启动、停止、重启、健康检查
 */
import { EventEmitter } from 'events';
import { Readable } from 'stream';
/**
 * 进程状态
 */
export type ProcessStatus = 'starting' | 'running' | 'stopping' | 'stopped' | 'crashed';
/**
 * 进程信息
 */
export interface ProcessInfo {
    id: string;
    cli: string;
    args: string[];
    status: ProcessStatus;
    pid?: number;
    startedAt?: number;
    stoppedAt?: number;
    exitCode?: number;
    restartCount: number;
}
/**
 * 进程配置
 */
export interface ProcessConfig {
    cwd?: string;
    env?: Record<string, string>;
    timeout?: number;
    autoRestart?: boolean;
    maxRestarts?: number;
    restartDelay?: number;
}
/**
 * 进程事件
 */
export type ProcessEvent = {
    type: 'spawn';
    processId: string;
    pid: number;
} | {
    type: 'stdout';
    processId: string;
    data: string;
} | {
    type: 'stderr';
    processId: string;
    data: string;
} | {
    type: 'exit';
    processId: string;
    code: number | null;
    signal: string | null;
} | {
    type: 'error';
    processId: string;
    error: Error;
} | {
    type: 'restart';
    processId: string;
    attempt: number;
};
/**
 * CLI 进程管理器
 */
export declare class CLIProcessManager extends EventEmitter {
    private processes;
    private idCounter;
    constructor();
    /**
     * 启动新进程
     */
    spawn(cli: string, args: string[], config?: ProcessConfig): Promise<string>;
    /**
     * 内部启动进程
     */
    private startProcess;
    /**
     * 终止进程
     */
    kill(processId: string, signal?: NodeJS.Signals): Promise<void>;
    /**
     * 重启进程
     */
    restart(processId: string): Promise<void>;
    /**
     * 健康检查
     */
    healthCheck(processId: string): Promise<boolean>;
    /**
     * 获取进程信息
     */
    getInfo(processId: string): ProcessInfo | undefined;
    /**
     * 获取所有进程信息
     */
    getAllProcesses(): ProcessInfo[];
    /**
     * 获取进程输出
     */
    getOutput(processId: string): string[];
    /**
     * 获取进程错误输出
     */
    getErrors(processId: string): string[];
    /**
     * 创建输出流
     */
    createOutputStream(processId: string): Readable;
    /**
     * 向进程发送输入
     */
    sendInput(processId: string, input: string): Promise<void>;
    /**
     * 移除进程记录
     */
    remove(processId: string): boolean;
    /**
     * 清理所有进程
     */
    cleanup(): Promise<void>;
}
//# sourceMappingURL=CLIProcessManager.d.ts.map