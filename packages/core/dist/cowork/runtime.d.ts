/**
 * Cowork Runtime
 * 收敛执行器注册、上下文装配、策略校验与沙箱准备
 */
import type { IAuditManager } from '../audit/types.js';
import { HeadlessToolRuntime } from '../tool-runtime/HeadlessToolRuntime.js';
import type { FileOperationService } from '../tool-runtime/FileOperationService.js';
import type { ToolExecutor } from '../tool-runtime/ToolExecutor.js';
import type { ToolRegistry } from '../tool-runtime/ToolRegistry.js';
import type { ToolCallTrace } from '../tool-runtime/types.js';
import { AgentRuntimeLike, ContextAssembler, CoworkTask, ExecutionResult, ExecutorCapabilities, ExecutorRegistration, ICodeEditor, ModelPool, PolicyDecision, PolicyGuard, RuntimeExecutionOptions, SandboxedTask, ExecutionSandbox } from './types.js';
declare class InMemoryModelPool implements ModelPool {
    private models;
    registerExecutor(executor: ExecutorRegistration): void;
    getModelId(executorName: string): string | undefined;
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
}
export declare class AgentRuntime implements AgentRuntimeLike {
    private executors;
    private modelPool;
    private contextAssembler;
    private policyGuard;
    private sandbox;
    private headlessToolRuntime;
    private toolRegistry;
    private toolExecutor;
    private fileOperationService;
    constructor(deps?: AgentRuntimeDeps);
    registerExecutor(name: string, editor: ICodeEditor, capabilities: ExecutorCapabilities, modelId?: string): void;
    getExecutor(name: string): ExecutorRegistration | undefined;
    getAllExecutors(): ExecutorRegistration[];
    buildContextFromResult(result: ExecutionResult): string;
    attachPreviousResult(task: CoworkTask, result: ExecutionResult): CoworkTask;
    getToolRegistry(): ToolRegistry;
    getToolExecutor(): ToolExecutor;
    getToolTraces(): ToolCallTrace[];
    executeTask(task: CoworkTask, options?: RuntimeExecutionOptions): Promise<ExecutionResult>;
    private executeTool;
    private createToolContext;
}
export { InMemoryModelPool, DefaultContextAssembler, AllowAllPolicyGuard, PassthroughSandbox, };
//# sourceMappingURL=runtime.d.ts.map