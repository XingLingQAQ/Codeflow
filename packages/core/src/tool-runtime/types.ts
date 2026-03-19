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

export interface RegisteredTool<TInput = unknown, TOutput = unknown>
  extends ToolMetadata {
  handler: ToolHandler<TInput, TOutput>;
}

export interface ToolPolicyDecision {
  allowed: boolean;
  reason?: string;
}

export interface ToolPolicyGuard {
  canExecute(
    tool: ToolMetadata,
    request: ToolExecutionRequest
  ): Promise<ToolPolicyDecision> | ToolPolicyDecision;
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

export type ToolRuntimeRiskLevel = ToolRiskLevel;
export type ToolRuntimeDecision = 'allow' | 'allow_with_isolation' | 'require_approval' | 'deny';

export interface ToolRuntimeBoundary {
  type: 'command' | 'path' | 'network' | 'resource';
  value: string;
  risk: ToolRuntimeRiskLevel;
  required?: boolean;
  reason?: string;
  metadata?: Record<string, unknown>;
}

export interface PermissionProfile {
  id: string;
  name: string;
  defaultDecision: ToolRuntimeDecision;
  boundaries: ToolRuntimeBoundary[];
  metadata?: Record<string, unknown>;
}

export interface RuntimeToolExecutionRequest {
  command: string;
  args?: string[];
  cwd?: string;
  env?: Record<string, string>;
  actor: AuditActor;
  boundaries: ToolRuntimeBoundary[];
  metadata?: Record<string, unknown>;
}

export interface PolicyDecisionResult {
  decision: ToolRuntimeDecision;
  risk: ToolRuntimeRiskLevel;
  matchedBoundaries: ToolRuntimeBoundary[];
  missingBoundaries: ToolRuntimeBoundary[];
  notes: string[];
}

export interface ToolRuntimeExecutionSnapshot {
  decision: ToolRuntimeDecision;
  risk: ToolRuntimeRiskLevel;
  isolated: boolean;
  processId?: string;
  command: string;
  args: string[];
  cwd?: string;
  envKeys: string[];
  boundarySummary: {
    matched: number;
    missing: number;
    required: number;
  };
  metadataPreview?: Record<string, unknown>;
  notes: string[];
}

export interface ExecutionSandboxResult {
  processId?: string;
  decision: PolicyDecisionResult;
  isolated: boolean;
  snapshot: ToolRuntimeExecutionSnapshot;
}

export interface ToolExecutorOptions {
  policyGuard?: ToolPolicyGuard;
  auditManager?: {
    log: (
      entry: {
        eventType: 'access' | 'modify' | 'delete' | 'create' | 'login' | 'logout' | 'permission_change' | 'config_change' | 'error' | 'security';
        severity: 'info' | 'warning' | 'error' | 'critical';
        actor: AuditActor;
        resource: { type: string; id: string; name?: string; path?: string };
        action: string;
        outcome: 'success' | 'failure';
        details?: Record<string, unknown>;
      }
    ) => Promise<unknown>;
  };
  traceLimit?: number;
}

export type ToolLikeOutput =
  | SearchResponse
  | Diff
  | EditResult
  | EditResult[]
  | FileUndoResult
  | Record<string, unknown>
  | unknown[]
  | string
  | number
  | boolean
  | null;

export type SkillSource = 'internal' | 'official' | 'imported';
export type SkillApprovalState = 'not_required' | 'approved' | 'pending' | 'rejected';
export type SkillLifecycleState =
  | 'registered'
  | 'authorized'
  | 'loaded'
  | 'executed'
  | 'recorded'
  | 'deprecated';

export interface SkillManifest {
  skillId: string;
  version: string;
  description: string;
  tags: string[];
  aliases?: string[];
  riskLevel: ToolRiskLevel;
  source: SkillSource;
  entryPoints: ToolEntryPoint[];
  inputSchema: ToolSchema;
  outputSchema?: ToolSchema;
  defaultTimeoutMs?: number;
  auditLevel?: 'basic' | 'detailed';
  applicableRoles?: string[];
  manifestPath?: string;
  toolIds?: string[];
  deprecated?: boolean;
  metadata?: Record<string, unknown>;
}

export interface SkillRuntimeFacade {
  execute<TOutput = ToolLikeOutput>(
    toolId: string,
    input: unknown,
    context: ToolContext
  ): Promise<ToolExecutionResult<TOutput>>;
  executeSearch(
    kind: SearchProviderKind,
    request: SearchRequest,
    context: ToolContext
  ): Promise<ToolExecutionResult<SearchResponse>>;
  getToolTraces(): ToolCallTrace[];
  getToolTraceCount(): number;
}

export interface SkillExecutionContext extends ToolContext {
  runtime: SkillRuntimeFacade;
  triggerReason?: string;
  approvalState?: SkillApprovalState;
}

export interface SkillHandler<TInput = unknown, TOutput = unknown> {
  execute(input: TInput, context: SkillExecutionContext): Promise<TOutput> | TOutput;
}

export interface SkillRegistration<TInput = unknown, TOutput = unknown> {
  manifest: SkillManifest;
  handler: SkillHandler<TInput, TOutput>;
}

export interface SkillExecutionRequest<TInput = unknown> {
  skillId: string;
  version?: string;
  input: TInput;
  context: ToolContext;
  triggerReason?: string;
}

export interface SkillAuthorizationDecision {
  allowed: boolean;
  reason?: string;
  approvalState?: SkillApprovalState;
}

export interface SkillAuthorizer {
  authorize(
    skill: SkillManifest,
    request: SkillExecutionRequest
  ): Promise<SkillAuthorizationDecision> | SkillAuthorizationDecision;
}

export interface SkillExecutionRecord {
  recordId: string;
  skillId: string;
  version: string;
  source: SkillSource;
  entryPoint: ToolEntryPoint;
  startedAt: number;
  completedAt: number;
  duration: number;
  status: 'success' | 'failed';
  lifecycle: SkillLifecycleState[];
  approvalState: SkillApprovalState;
  agentId?: string;
  taskId?: string;
  sessionId?: string;
  triggerReason?: string;
  inputSummary: string;
  outputSummary?: string;
  error?: string;
  toolIds: string[];
  toolCallIds: string[];
  artifacts?: ToolTraceArtifact[];
}

export interface SkillExecutionResult<TOutput = unknown> {
  ok: boolean;
  output?: TOutput;
  error?: ToolExecutionError;
  record: SkillExecutionRecord;
}

export interface RegisterSkillOptions {
  replace?: boolean;
}

export interface SkillRegistryFilter {
  entryPoint?: ToolEntryPoint;
  tag?: string;
  riskLevel?: ToolRiskLevel;
  includeDeprecated?: boolean;
  source?: SkillSource;
  role?: string;
}
