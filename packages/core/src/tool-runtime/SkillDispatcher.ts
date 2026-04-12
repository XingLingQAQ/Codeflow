import type { IAuditManager } from '../audit/types.js';
import type {
  SkillAuthorizationDecision,
  SkillAuthorizer,
  SkillExecutionContext,
  SkillExecutionRecord,
  SkillExecutionRequest,
  SkillExecutionResult,
  SkillManifest,
  SkillRuntimeFacade,
  ToolExecutionError,
  ToolTraceArtifact,
  ToolLikeOutput,
} from './types.js';
import { SkillRegistry } from './SkillRegistry.js';

const DEFAULT_TIMEOUT_MS = 30_000;
const SKILL_CONTROLS_METADATA_KEY = 'skillControls';

interface SkillRuntimeControls {
  enabled?: boolean;
  allowedSkills?: string[];
}

function normalizeSkillControls(metadata: Record<string, unknown> | undefined): SkillRuntimeControls | undefined {
  const candidate = metadata?.[SKILL_CONTROLS_METADATA_KEY];
  if (!candidate || typeof candidate !== 'object' || Array.isArray(candidate)) {
    return undefined;
  }

  const raw = candidate as Record<string, unknown>;
  const controls: SkillRuntimeControls = {};

  if (typeof raw.enabled === 'boolean') {
    controls.enabled = raw.enabled;
  }

  if (Array.isArray(raw.allowedSkills)) {
    controls.allowedSkills = raw.allowedSkills.filter(
      (skillId): skillId is string => typeof skillId === 'string' && skillId.trim().length > 0,
    );
  }

  return controls.enabled === undefined && controls.allowedSkills === undefined ? undefined : controls;
}

function summarizeValue(value: unknown): string {
  if (value === undefined) return 'undefined';
  if (value === null) return 'null';
  if (typeof value === 'string') {
    return value.length > 160 ? `${value.slice(0, 157)}...` : value;
  }
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value);
  }
  if (Array.isArray(value)) {
    return `array(${value.length})`;
  }
  if (typeof value === 'object') {
    const keys = Object.keys(value as Record<string, unknown>);
    return `object(${keys.slice(0, 6).join(',')}${keys.length > 6 ? ',...' : ''})`;
  }
  return typeof value;
}

function createRecordId(): string {
  return `skill_${Date.now()}_${Math.random().toString(16).slice(2, 10)}`;
}

class AllowAllSkillAuthorizer implements SkillAuthorizer {
  authorize(): SkillAuthorizationDecision {
    return {
      allowed: true,
      approvalState: 'not_required',
    };
  }
}

class MetadataSkillAuthorizer implements SkillAuthorizer {
  constructor(private readonly fallback: SkillAuthorizer = new AllowAllSkillAuthorizer()) {}

  async authorize(
    skill: SkillManifest,
    request: SkillExecutionRequest,
  ): Promise<SkillAuthorizationDecision> {
    const controls = normalizeSkillControls(request.context.metadata);
    if (controls?.enabled === false) {
      return {
        allowed: false,
        reason: 'Skill execution disabled by runtime controls',
        approvalState: 'rejected',
      };
    }

    if (controls?.allowedSkills && controls.allowedSkills.length > 0) {
      const allowed = new Set(controls.allowedSkills);
      if (!allowed.has(skill.skillId)) {
        return {
          allowed: false,
          reason: `Skill execution denied by runtime controls: ${skill.skillId}`,
          approvalState: 'rejected',
        };
      }
    }

    return await this.fallback.authorize(skill, request);
  }
}

export interface SkillDispatcherOptions {
  authorizer?: SkillAuthorizer;
  auditManager?: IAuditManager;
  recordLimit?: number;
}

export class SkillDispatcher {
  private readonly authorizer: SkillAuthorizer;
  private readonly records: SkillExecutionRecord[] = [];
  private readonly recordLimit: number;

  constructor(
    private readonly registry: SkillRegistry,
    private readonly runtime: SkillRuntimeFacade,
    private readonly options: SkillDispatcherOptions = {}
  ) {
    this.authorizer = new MetadataSkillAuthorizer(options.authorizer);
    this.recordLimit = options.recordLimit ?? 200;
  }

  async execute<TOutput = ToolLikeOutput>(
    request: SkillExecutionRequest
  ): Promise<SkillExecutionResult<TOutput>> {
    const startedAt = Date.now();
    const skill = this.registry.resolve(request.skillId, request.version);
    if (!skill) {
      return {
        ok: false,
        error: {
          code: 'skill_not_found',
          message: `Skill not found: ${request.skillId}`,
        },
        record: this.createRecord(
          {
            skillId: request.skillId,
            version: request.version ?? 'missing',
            description: 'Missing skill',
            tags: [],
            riskLevel: 'medium',
            source: 'internal',
            entryPoints: [request.context.entryPoint],
            inputSchema: { type: 'object', properties: {}, required: [] },
          },
          request,
          startedAt,
          {
            status: 'failed',
            approvalState: 'rejected',
            lifecycle: ['registered'],
            error: 'Skill not found',
          }
        ),
      };
    }

    const manifest = skill.manifest;
    const authorization = await this.authorizer.authorize(manifest, request);
    if (!authorization.allowed) {
      const deniedRecord = this.createRecord(manifest, request, startedAt, {
        status: 'failed',
        approvalState: authorization.approvalState ?? 'rejected',
        lifecycle: ['registered', 'authorized'],
        error: authorization.reason ?? 'Skill execution denied',
      });
      this.pushRecord(deniedRecord);
      await this.logAudit(manifest, deniedRecord);
      return {
        ok: false,
        error: {
          code: 'skill_denied',
          message: authorization.reason ?? 'Skill execution denied',
        },
        record: deniedRecord,
      };
    }

    const lifecycle: SkillExecutionRecord['lifecycle'] = ['registered', 'authorized', 'loaded'];
    const traceCountBefore = this.runtime.getToolTraceCount();
    const context: SkillExecutionContext = {
      ...request.context,
      runtime: this.runtime,
      triggerReason: request.triggerReason,
      approvalState: authorization.approvalState ?? 'not_required',
    };

    try {
      const output = await this.executeWithTimeout(
        manifest,
        () => skill.handler.execute(request.input, context)
      );
      lifecycle.push('executed', 'recorded');
      const record = this.createRecord(manifest, request, startedAt, {
        status: 'success',
        approvalState: authorization.approvalState ?? 'not_required',
        lifecycle,
        output,
        traceCountBefore,
      });
      this.pushRecord(record);
      await this.logAudit(manifest, record);
      return {
        ok: true,
        output: output as TOutput,
        record,
      };
    } catch (error) {
      lifecycle.push('executed', 'recorded');
      const failure = error instanceof Error ? error.message : String(error);
      const record = this.createRecord(manifest, request, startedAt, {
        status: 'failed',
        approvalState: authorization.approvalState ?? 'not_required',
        lifecycle,
        error: failure,
        traceCountBefore,
      });
      this.pushRecord(record);
      await this.logAudit(manifest, record);
      return {
        ok: false,
        error: {
          code: 'skill_execution_failed',
          message: failure,
          details: error,
        } satisfies ToolExecutionError,
        record,
      };
    }
  }

  getRecords(): SkillExecutionRecord[] {
    return [...this.records];
  }

  clearRecords(): void {
    this.records.length = 0;
  }

  private async executeWithTimeout<TOutput>(
    manifest: SkillManifest,
    fn: () => Promise<TOutput> | TOutput
  ): Promise<TOutput> {
    const timeoutMs = manifest.defaultTimeoutMs ?? DEFAULT_TIMEOUT_MS;
    return await Promise.race([
      Promise.resolve(fn()),
      new Promise<TOutput>((_, reject) => {
        setTimeout(() => reject(new Error(`Skill timeout after ${timeoutMs}ms`)), timeoutMs);
      }),
    ]);
  }

  private createRecord(
    manifest: SkillManifest,
    request: SkillExecutionRequest,
    startedAt: number,
    options: {
      status: 'success' | 'failed';
      approvalState: SkillExecutionRecord['approvalState'];
      lifecycle: SkillExecutionRecord['lifecycle'];
      output?: unknown;
      error?: string;
      traceCountBefore?: number;
    }
  ): SkillExecutionRecord {
    const completedAt = Date.now();
    const traces = this.runtime.getToolTraces();
    const relatedTraces = traces.slice(options.traceCountBefore ?? 0).filter(
      (trace) =>
        (request.context.sessionId ? trace.sessionId === request.context.sessionId : true) &&
        (request.context.taskId ? trace.taskId === request.context.taskId : true) &&
        (request.context.agentId ? trace.agentId === request.context.agentId : true)
    );
    const skillToolCallIds = this.extractSkillToolCallIds(options.output);
    const toolIds = manifest.toolIds && manifest.toolIds.length > 0
      ? Array.from(new Set([...manifest.toolIds, ...relatedTraces.map((trace) => trace.toolId)]))
      : Array.from(new Set(relatedTraces.map((trace) => trace.toolId)));
    const toolCallIds = Array.from(
      new Set([...relatedTraces.map((trace) => trace.toolCallId), ...skillToolCallIds])
    );
    const artifacts: ToolTraceArtifact[] = relatedTraces.flatMap((trace) => trace.artifacts ?? []);

    return {
      recordId: createRecordId(),
      skillId: manifest.skillId,
      version: manifest.version,
      source: manifest.source,
      entryPoint: request.context.entryPoint,
      startedAt,
      completedAt,
      duration: completedAt - startedAt,
      status: options.status,
      lifecycle: options.lifecycle,
      approvalState: options.approvalState,
      agentId: request.context.agentId,
      taskId: request.context.taskId,
      sessionId: request.context.sessionId,
      triggerReason: request.triggerReason,
      inputSummary: summarizeValue(request.input),
      outputSummary: options.output === undefined ? undefined : summarizeValue(this.sanitizeOutput(options.output)),
      error: options.error,
      toolIds,
      toolCallIds,
      artifacts,
    };
  }


  private pushRecord(record: SkillExecutionRecord): void {
    this.records.push(record);
    if (this.records.length > this.recordLimit) {
      this.records.splice(0, this.records.length - this.recordLimit);
    }
  }

  private sanitizeOutput(output: unknown): unknown {
    if (!output || typeof output !== 'object' || Array.isArray(output)) {
      return output;
    }
    const cloned = { ...(output as Record<string, unknown>) };
    delete cloned.__skillToolCallId;
    return cloned;
  }

  private extractSkillToolCallIds(output: unknown): string[] {
    if (!output || typeof output !== 'object' || Array.isArray(output)) {
      return [];
    }
    const toolCallId = (output as Record<string, unknown>).__skillToolCallId;
    return typeof toolCallId === 'string' && toolCallId.length > 0 ? [toolCallId] : [];
  }


  private async logAudit(manifest: SkillManifest, record: SkillExecutionRecord): Promise<void> {
    if (!this.options.auditManager) {
      return;
    }

    await this.options.auditManager.log({
      eventType: record.status === 'success' ? 'access' : 'error',
      severity: record.status === 'success' ? 'info' : 'warning',
      actor: {
        id: record.agentId ?? 'skill-dispatcher',
        type: 'agent',
        name: record.agentId ?? 'skill-dispatcher',
      },
      resource: {
        type: 'skill',
        id: `${record.skillId}@${record.version}`,
        name: manifest.description,
        path: manifest.manifestPath,
      },
      action: 'execute',
      outcome: record.status === 'success' ? 'success' : 'failure',
      details: {
        recordId: record.recordId,
        lifecycle: record.lifecycle,
        approvalState: record.approvalState,
        triggerReason: record.triggerReason,
        inputSummary: record.inputSummary,
        outputSummary: record.outputSummary,
        toolIds: record.toolIds,
        toolCallIds: record.toolCallIds,
        duration: record.duration,
        error: record.error,
      },
    });
  }
}
