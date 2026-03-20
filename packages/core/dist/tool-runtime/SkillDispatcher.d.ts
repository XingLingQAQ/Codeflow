import type { IAuditManager } from '../audit/types.js';
import type { SkillAuthorizer, SkillExecutionRecord, SkillExecutionRequest, SkillExecutionResult, SkillRuntimeFacade, ToolLikeOutput } from './types.js';
import { SkillRegistry } from './SkillRegistry.js';
export interface SkillDispatcherOptions {
    authorizer?: SkillAuthorizer;
    auditManager?: IAuditManager;
    recordLimit?: number;
}
export declare class SkillDispatcher {
    private readonly registry;
    private readonly runtime;
    private readonly options;
    private readonly authorizer;
    private readonly records;
    private readonly recordLimit;
    constructor(registry: SkillRegistry, runtime: SkillRuntimeFacade, options?: SkillDispatcherOptions);
    execute<TOutput = ToolLikeOutput>(request: SkillExecutionRequest): Promise<SkillExecutionResult<TOutput>>;
    getRecords(): SkillExecutionRecord[];
    clearRecords(): void;
    private executeWithTimeout;
    private createRecord;
    private pushRecord;
    private sanitizeOutput;
    private extractSkillToolCallIds;
    private logAudit;
}
//# sourceMappingURL=SkillDispatcher.d.ts.map