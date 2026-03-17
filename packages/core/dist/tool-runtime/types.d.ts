/**
 * Headless Tool Runtime 类型定义
 */
import type { AuditActor } from '../audit/types.js';
import type { Diff, EditResult, ICodeEditor } from '../cowork/types.js';
export type ToolEntryPoint = 'frontend' | 'agent' | 'api';
export type ToolExecutionMode = 'sync' | 'stream' | 'background';
export type ToolRiskLevel = 'low' | 'medium' | 'high';
export type ToolSource = 'internal' | 'mcp' | 'skill';
export type SearchProviderKind = 'code' | 'knowledge' | 'memory' | 'graph';
export interface ToolSchema {
    type: 'object';
    properties: Record<string, unknown>;
    required?: string[];
}
export interface ToolMetadata {
    id: string;
    version: string;
    description: string;
    tags: string[];
    riskLevel: ToolRiskLevel;
    executionModes: ToolExecutionMode[];
    entryPoints: ToolEntryPoint[];
    source: ToolSource;
    inputSchema: ToolSchema;
    outputSchema?: ToolSchema;
}
export interface ToolContext {
    entryPoint: ToolEntryPoint;
    sessionId?: string;
    taskId?: string;
    agentId?: string;
    actor?: AuditActor;
    signal?: AbortSignal;
    metadata?: Record<string, unknown>;
    resources?: {
        editor?: ICodeEditor;
        [key: string]: unknown;
    };
}
export interface ToolExecutionRequest<TInput = unknown> {
    toolId: string;
    input: TInput;
    context: ToolContext;
}
export interface ToolTraceArtifact {
    type: string;
    id?: string;
    path?: string;
    summary?: string;
}
export interface ToolCallTrace {
    toolCallId: string;
    toolId: string;
    source: ToolSource;
    entryPoint: ToolEntryPoint;
    sessionId?: string;
    taskId?: string;
    agentId?: string;
    startedAt: number;
    completedAt: number;
    duration: number;
    inputSummary: string;
    outputSummary?: string;
    success: boolean;
    error?: string;
    artifacts?: ToolTraceArtifact[];
}
export interface ToolExecutionError {
    code: string;
    message: string;
    details?: unknown;
}
export interface ToolExecutionResult<TOutput = unknown> {
    ok: boolean;
    output?: TOutput;
    error?: ToolExecutionError;
    trace: ToolCallTrace;
}
export interface ToolHandler<TInput = unknown, TOutput = unknown> {
    execute(input: TInput, context: ToolContext): Promise<TOutput> | TOutput;
}
export interface RegisteredTool<TInput = unknown, TOutput = unknown> extends ToolMetadata {
    handler: ToolHandler<TInput, TOutput>;
}
export interface ToolPolicyDecision {
    allowed: boolean;
    reason?: string;
}
export interface ToolPolicyGuard {
    canExecute(tool: ToolMetadata, request: ToolExecutionRequest): Promise<ToolPolicyDecision> | ToolPolicyDecision;
}
export interface RegisterToolOptions {
    replace?: boolean;
}
export interface ToolRegistryFilter {
    entryPoint?: ToolEntryPoint;
    source?: ToolSource;
    tag?: string;
}
export interface FilePreviewInput {
    file: string;
    instruction: string;
}
export interface FileEditInput {
    file: string;
    instruction: string;
}
export interface FileEditMultipleInput {
    files: string[];
    instruction: string;
}
export interface FileApplyDiffInput {
    file: string;
    diff: Diff;
}
export interface FileUndoResult {
    restored: boolean;
}
export interface SearchRequest {
    query: string;
    limit?: number;
    filters?: Record<string, unknown>;
}
export interface SearchResultItem {
    id: string;
    title: string;
    snippet: string;
    source: string;
    score?: number;
    metadata?: Record<string, unknown>;
}
export interface SearchResponse {
    provider: string;
    items: SearchResultItem[];
    total: number;
}
export interface SearchProvider {
    readonly id: string;
    readonly kind: SearchProviderKind;
    search(request: SearchRequest, context: ToolContext): Promise<SearchResponse>;
}
export interface MCPServerToolRegistration {
    id: string;
    version?: string;
    description: string;
    tags?: string[];
    riskLevel?: ToolRiskLevel;
    executionModes?: ToolExecutionMode[];
    entryPoints?: ToolEntryPoint[];
    inputSchema: ToolSchema;
    outputSchema?: ToolSchema;
    handler: ToolHandler;
}
export interface MCPServerRegistration {
    serverId: string;
    description?: string;
    timeoutMs?: number;
    tools: MCPServerToolRegistration[];
}
export interface MCPServerInfo {
    serverId: string;
    description?: string;
    timeoutMs: number;
    toolIds: string[];
}
export interface ToolExecutorOptions {
    policyGuard?: ToolPolicyGuard;
    auditManager?: {
        log: (entry: {
            eventType: 'access' | 'modify' | 'delete' | 'create' | 'login' | 'logout' | 'permission_change' | 'config_change' | 'error' | 'security';
            severity: 'info' | 'warning' | 'error' | 'critical';
            actor: AuditActor;
            resource: {
                type: string;
                id: string;
                name?: string;
                path?: string;
            };
            action: string;
            outcome: 'success' | 'failure';
            details?: Record<string, unknown>;
        }) => Promise<unknown>;
    };
    traceLimit?: number;
}
export type ToolLikeOutput = SearchResponse | Diff | EditResult | EditResult[] | FileUndoResult | Record<string, unknown> | unknown[] | string | number | boolean | null;
//# sourceMappingURL=types.d.ts.map