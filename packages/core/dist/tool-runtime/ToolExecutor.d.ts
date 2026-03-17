import type { ToolExecutionRequest, ToolExecutionResult, ToolExecutorOptions, ToolLikeOutput, ToolCallTrace } from './types.js';
import { ToolRegistry } from './ToolRegistry.js';
export declare class ToolExecutor {
    private readonly registry;
    private readonly options;
    private readonly traces;
    private readonly traceLimit;
    constructor(registry: ToolRegistry, options?: ToolExecutorOptions);
    execute<TOutput = ToolLikeOutput>(request: ToolExecutionRequest): Promise<ToolExecutionResult<TOutput>>;
    getTraces(): ToolCallTrace[];
    clearTraces(): void;
    private createTrace;
    private pushTrace;
    private logAudit;
}
//# sourceMappingURL=ToolExecutor.d.ts.map