/**
 * PlanCommands - Plan 模式 CLI 命令
 * 实现 /codeflow:plan-* 系列命令
 */
import { EventEmitter } from 'events';
/**
 * 命令参数基础接口
 */
export interface CommandParams {
    [key: string]: unknown;
}
/**
 * Plan 命令参数
 */
export interface PlanNewParams extends CommandParams {
    name: string;
    description?: string;
    template?: string;
}
export interface PlanVisionParams extends CommandParams {
    planId: string;
    interactive?: boolean;
    questions?: string[];
}
export interface PlanConstraintsParams extends CommandParams {
    planId: string;
    source?: 'vision' | 'requirements' | 'both';
    format?: 'json' | 'markdown';
}
export interface PlanFastForwardParams extends CommandParams {
    planId: string;
    skipPhases?: string[];
    dryRun?: boolean;
}
export interface PlanExecuteParams extends CommandParams {
    planId: string;
    phase?: string;
    autoApprove?: boolean;
    maxIterations?: number;
}
/**
 * 命令执行结果
 */
export interface CommandResult {
    success: boolean;
    command: string;
    output: string;
    data?: unknown;
    error?: string;
    duration: number;
}
/**
 * 命令定义
 */
export interface CommandDefinition {
    name: string;
    description: string;
    usage: string;
    examples: string[];
    parameters: ParameterDefinition[];
}
/**
 * 参数定义
 */
export interface ParameterDefinition {
    name: string;
    type: 'string' | 'boolean' | 'number' | 'array';
    description: string;
    required: boolean;
    default?: unknown;
    choices?: string[];
}
/**
 * Plan 命令处理器
 */
export declare class PlanCommands extends EventEmitter {
    private commands;
    constructor();
    /**
     * 注册所有 Plan 命令
     */
    private registerCommands;
    /**
     * 执行命令
     */
    execute(commandName: string, params: CommandParams): Promise<CommandResult>;
    /**
     * 验证参数
     */
    private validateParams;
    /**
     * 执行 plan-new 命令
     */
    private executePlanNew;
    /**
     * 执行 plan-vision 命令
     */
    private executePlanVision;
    /**
     * 执行 plan-constraints 命令
     */
    private executePlanConstraints;
    /**
     * 执行 plan-ff 命令
     */
    private executePlanFastForward;
    /**
     * 执行 plan-execute 命令
     */
    private executePlanExecute;
    /**
     * 获取命令定义
     */
    getCommand(name: string): CommandDefinition | undefined;
    /**
     * 获取所有命令
     */
    getAllCommands(): CommandDefinition[];
    /**
     * 获取帮助信息
     */
    getHelp(commandName?: string): string;
    /**
     * 解析命令行参数
     */
    parseArgs(args: string): CommandParams;
}
//# sourceMappingURL=PlanCommands.d.ts.map