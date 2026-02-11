/**
 * Commander Mode 实现
 * 指挥官模式编排 - Main AI 调用 Coder/Sub Agent
 */
import { ICommander, AgentConfig, AgentRole, CallCoderAgentParams, ConsultSubExpertParams, ToolCallResult, ContextGraftConfig, GraftedContext, ToolDefinition, CallTrace, CommanderEvent, CommanderEventHandler } from './types.js';
import { HookManager } from '../hooks/HookManager.js';
export declare class Commander implements ICommander {
    private agents;
    private callStack;
    private currentCallId;
    private eventHandlers;
    private hookManager?;
    private maxNestingDepth;
    constructor(hookManager?: HookManager, maxNestingDepth?: number);
    registerAgent(config: AgentConfig): void;
    getAgent(role: AgentRole): AgentConfig | undefined;
    callCoderAgent(params: CallCoderAgentParams): Promise<ToolCallResult>;
    consultSubExpert(params: ConsultSubExpertParams): Promise<ToolCallResult>;
    graftContext(sourceRole: AgentRole, targetRole: AgentRole, config?: ContextGraftConfig): Promise<GraftedContext>;
    getToolDefinitions(): ToolDefinition[];
    on(event: CommanderEvent, handler: CommanderEventHandler): void;
    off(event: CommanderEvent, handler: CommanderEventHandler): void;
    getCallTrace(): CallTrace[];
    private generateCallId;
    private getCurrentParentId;
    private getCurrentDepth;
    private pushCall;
    private popCall;
    private emit;
    private buildCoderPrompt;
    private buildSubExpertPrompt;
    /**
     * 估算消息的 Token 数量
     * 使用改进的启发式算法
     */
    private estimateMessageTokens;
}
//# sourceMappingURL=Commander.d.ts.map