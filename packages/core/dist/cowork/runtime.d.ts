import type { HookManager } from '../hooks/HookManager.js';
import type { HookRuntimeControls } from '../hooks/types.js';
import type { IAuditManager } from '../audit/types.js';
import { HeadlessToolRuntime } from '../tool-runtime/HeadlessToolRuntime.js';
import type { FileOperationService } from '../tool-runtime/FileOperationService.js';
import type { ToolExecutor } from '../tool-runtime/ToolExecutor.js';
import type { ToolRegistry } from '../tool-runtime/ToolRegistry.js';
import type { SkillExecutionRecord, SkillRegistration, ToolCallTrace } from '../tool-runtime/types.js';
import type { AgentRuntimeLike, ContextAssembler, CoworkTask, ExecutionResult, ExecutorCapabilities, ExecutorRegistration, ExecutionSandbox, ICodeEditor, ModelPool, PolicyDecision, PolicyGuard, RuntimeExecutionOptions, SandboxedTask } from './types.js';
declare class InMemoryModelPool implements ModelPool {
    private readonly models;
    private readonly executors;
    private readonly health;
    registerExecutor(executor: ExecutorRegistration): void;
    getModelId(executorName: string): string | undefined;
    markExecutorHealthy(executorName: string): void;
    markExecutorUnhealthy(executorName: string, reason?: string): void;
    getFallbackExecutor(task: CoworkTask, currentExecutor: string): ExecutorRegistration | undefined;
}
declare class DefaultContextAssembler implements ContextAssembler {
    buildContextFromResult(result: ExecutionResult): string;
    attachPreviousResult(task: CoworkTask, result: ExecutionResult): CoworkTask;
}
declare class AllowAllPolicyGuard implements PolicyGuard {
    canExecute(): PolicyDecision;
}
declare class PassthroughSandbox implements ExecutionSandbox {
    prepare(task: CoworkTask): Promise<SandboxedTask>;
}
export interface AgentRuntimeDeps {
    modelPool?: ModelPool;
    contextAssembler?: ContextAssembler;
    policyGuard?: PolicyGuard;
    sandbox?: ExecutionSandbox;
    auditManager?: IAuditManager;
    toolRegistry?: ToolRegistry;
    toolExecutor?: ToolExecutor;
    fileOperationService?: FileOperationService;
    headlessToolRuntime?: HeadlessToolRuntime;
    hookManager?: HookManager;
    hookControls?: HookRuntimeControls;
}
export declare class AgentRuntime implements AgentRuntimeLike {
    private readonly executors;
    private readonly modelPool;
    private readonly contextAssembler;
    private readonly policyGuard;
    private readonly sandbox;
    private readonly auditManager;
    private readonly policyEngine;
    private readonly auditTrail;
    private readonly headlessToolRuntime;
    private readonly toolRegistry;
    private readonly toolExecutor;
    private readonly fileOperationService;
    constructor(deps?: AgentRuntimeDeps);
    registerExecutor(name: string, editor: ICodeEditor, capabilities: ExecutorCapabilities, modelId?: string): void;
    registerSkill(skill: SkillRegistration, replace?: boolean): void;
    getExecutor(name: string): ExecutorRegistration | undefined;
    getAllExecutors(): ExecutorRegistration[];
    buildContextFromResult(result: ExecutionResult): string;
    attachPreviousResult(task: CoworkTask, result: ExecutionResult): CoworkTask;
    getToolRegistry(): ToolRegistry;
    getToolExecutor(): ToolExecutor;
    getHookManager(): HookManager;
    getToolTraces(): ToolCallTrace[];
    getSkillExecutionRecords(): SkillExecutionRecord[];
    executeSkill<TOutput = unknown>(skillId: string, input: unknown, context?: {
        taskId?: string;
        sessionId?: string;
        agentId?: string;
        triggerReason?: string;
        metadata?: Record<string, unknown>;
    }): Promise<TOutput>;
    executeTask(task: CoworkTask, options?: RuntimeExecutionOptions): Promise<ExecutionResult>;
    private runTaskAttempt;
    private executeTool;
    private createToolContext;
    private evaluateRuntimePolicy;
    private sanitizeMetadataPreview;
    private sanitizeMetadataValue;
    private createRuntimeSnapshot;
    private canProceed;
    private getRuntimeErrorMessage;
    private toRuntimeOutcome;
}
export { InMemoryModelPool, DefaultContextAssembler, AllowAllPolicyGuard, PassthroughSandbox };
//# sourceMappingURL=runtime.d.ts.map