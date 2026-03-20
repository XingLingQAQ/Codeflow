import { AuditManager } from '../audit/AuditManager.js';
import type { IAuditManager } from '../audit/types.js';
import { HeadlessToolRuntime } from '../tool-runtime/HeadlessToolRuntime.js';
import type { FileOperationService } from '../tool-runtime/FileOperationService.js';
import type { ToolExecutor } from '../tool-runtime/ToolExecutor.js';
import type { ToolRegistry } from '../tool-runtime/ToolRegistry.js';
import type {
  ExecutionSandboxResult,
  PermissionProfile,
  PolicyDecisionResult,
  RuntimeToolExecutionRequest,
  SkillExecutionRecord,
  SkillRegistration,
  ToolCallTrace,
  ToolRuntimeBoundary,
  ToolRuntimeExecutionSnapshot,
} from '../tool-runtime/types.js';
import type {
  AgentRuntimeLike,
  ContextAssembler,
  CoworkTask,
  EditResult,
  ExecutionResult,
  ExecutorCapabilities,
  ExecutorRegistration,
  ExecutionSandbox,
  ICodeEditor,
  ModelPool,
  PolicyDecision,
  PolicyGuard,
  RuntimeExecutionOptions,
  SandboxedTask,
} from './types.js';

class InMemoryModelPool implements ModelPool {
  private readonly models = new Map<string, string | undefined>();
  private readonly executors = new Map<string, ExecutorRegistration>();
  private readonly health = new Map<string, { healthy: boolean; reason?: string }>();

  registerExecutor(executor: ExecutorRegistration): void {
    this.models.set(executor.name, executor.modelId);
    this.executors.set(executor.name, executor);
    this.markExecutorHealthy(executor.name);
  }

  getModelId(executorName: string): string | undefined {
    return this.models.get(executorName);
  }

  markExecutorHealthy(executorName: string): void {
    this.health.set(executorName, { healthy: true });
  }

  markExecutorUnhealthy(executorName: string, reason?: string): void {
    this.health.set(executorName, { healthy: false, reason });
  }

  getFallbackExecutor(task: CoworkTask, currentExecutor: string): ExecutorRegistration | undefined {
    for (const executor of this.executors.values()) {
      if (executor.name === currentExecutor) {
        continue;
      }
      if (!executor.capabilities.supportedTypes.includes(task.type)) {
        continue;
      }
      const status = this.health.get(executor.name);
      if (status?.healthy === false) {
        continue;
      }
      return executor;
    }
    return undefined;
  }
}

class DefaultContextAssembler implements ContextAssembler {
  buildContextFromResult(result: ExecutionResult): string {
    if (!result.output) {
      return '';
    }

    const parts: string[] = [];
    if (result.output.result) {
      parts.push(`Previous output:\n${result.output.result}`);
    }
    if (result.output.diffs && result.output.diffs.length > 0) {
      parts.push(
        `Previous changes:\n${result.output.diffs
          .map((diff) => `- ${diff.file}: +${diff.additions}/-${diff.deletions}`)
          .join('\n')}`,
      );
    }

    return parts.join('\n\n');
  }

  attachPreviousResult(task: CoworkTask, result: ExecutionResult): CoworkTask {
    return {
      ...task,
      input: {
        ...task.input,
        context: this.buildContextFromResult(result),
      },
    };
  }
}

class AllowAllPolicyGuard implements PolicyGuard {
  canExecute(): PolicyDecision {
    return { allowed: true };
  }
}

class PassthroughSandbox implements ExecutionSandbox {
  async prepare(task: CoworkTask): Promise<SandboxedTask> {
    return { task };
  }
}

class RuntimePolicyEngine {
  evaluate(profile: PermissionProfile, request: RuntimeToolExecutionRequest): PolicyDecisionResult {
    const matchedBoundaries: ToolRuntimeBoundary[] = [];
    const missingBoundaries = request.boundaries
      .filter((boundary) => {
        const matched = profile.boundaries.find(
          (candidate) => candidate.type === boundary.type && candidate.value === boundary.value,
        );
        if (matched) {
          matchedBoundaries.push(matched);
          return false;
        }
        return true;
      })
      .map((boundary) => ({
        type: boundary.type,
        value: boundary.value,
        risk: boundary.risk,
        reason: `Boundary ${boundary.type}:${boundary.value} is not permitted by profile ${profile.id}`,
      }));

    const requiredViolations = profile.boundaries
      .filter((boundary) => boundary.required)
      .filter(
        (boundary) =>
          !request.boundaries.some(
            (candidate) => candidate.type === boundary.type && candidate.value === boundary.value,
          ),
      )
      .map((boundary) => ({
        type: boundary.type,
        value: boundary.value,
        risk: boundary.risk,
        reason: `Required boundary ${boundary.type}:${boundary.value} is missing from request`,
      }));

    missingBoundaries.push(...requiredViolations);

    const riskLevels = [
      ...matchedBoundaries.map((boundary) => boundary.risk),
      ...missingBoundaries.map((boundary) => boundary.risk),
    ];
    const risk = this.resolveRisk(riskLevels);
    const violationRisk = this.resolveRisk(missingBoundaries.map((boundary) => boundary.risk));
    const decision = this.resolveDecision(profile.defaultDecision, violationRisk, missingBoundaries.length > 0);

    return {
      decision,
      risk,
      matchedBoundaries,
      missingBoundaries,
      notes: this.buildNotes(decision, risk, missingBoundaries.length),
    };
  }

  private resolveRisk(levels: Array<'low' | 'medium' | 'high'>): 'low' | 'medium' | 'high' {
    if (levels.includes('high')) {
      return 'high';
    }
    if (levels.includes('medium')) {
      return 'medium';
    }
    return 'low';
  }

  private resolveDecision(
    defaultDecision: PermissionProfile['defaultDecision'],
    violationRisk: 'low' | 'medium' | 'high',
    hasViolations: boolean,
  ): PermissionProfile['defaultDecision'] {
    if (!hasViolations) {
      return defaultDecision;
    }
    if (violationRisk === 'high') {
      return 'deny';
    }
    if (violationRisk === 'medium') {
      return 'require_approval';
    }
    return 'allow_with_isolation';
  }

  private buildNotes(
    decision: PermissionProfile['defaultDecision'],
    risk: 'low' | 'medium' | 'high',
    missingCount: number,
  ): string[] {
    const notes = [`decision=${decision}`, `risk=${risk}`];
    if (missingCount > 0) {
      notes.push(`${missingCount} boundary violation(s) detected`);
    }
    if (decision === 'allow_with_isolation') {
      notes.push('execution must run in an isolated environment');
    }
    if (decision === 'require_approval') {
      notes.push('manual approval required before execution');
    }
    if (decision === 'deny') {
      notes.push('execution blocked by policy');
    }
    return notes;
  }
}

function sanitizeRuntimeMetadataValue(value: unknown): unknown {
  if (typeof value === 'string') {
    if (value.length > 120) {
      return `${value.slice(0, 117)}...`;
    }
    if (/token|secret|password/i.test(value)) {
      return '[REDACTED]';
    }
    return value;
  }
  if (typeof value === 'number' || typeof value === 'boolean' || value === null) {
    return value;
  }
  if (Array.isArray(value)) {
    return value.slice(0, 3).map((item) => sanitizeRuntimeMetadataValue(item));
  }
  if (typeof value === 'object') {
    return '[OBJECT]';
  }
  return String(value);
}

function sanitizeRuntimeMetadataPreview(metadata?: Record<string, unknown>): Record<string, unknown> | undefined {
  if (!metadata) {
    return undefined;
  }

  const preview = Object.entries(metadata).reduce<Record<string, unknown>>((acc, [key, value]) => {
    if (value === undefined) {
      return acc;
    }
    const normalizedKey = key.toLowerCase();
    if (normalizedKey.includes('token') || normalizedKey.includes('secret') || normalizedKey.includes('password')) {
      acc[key] = '[REDACTED]';
      return acc;
    }
    if (Array.isArray(value)) {
      acc[key] = value.slice(0, 3).map((item) => sanitizeRuntimeMetadataValue(item));
      return acc;
    }
    acc[key] = sanitizeRuntimeMetadataValue(value);
    return acc;
  }, {});

  return Object.keys(preview).length > 0 ? preview : undefined;
}

class RuntimeAuditTrail {
  constructor(private readonly auditManager: IAuditManager) {}

  async recordDecision(
    request: RuntimeToolExecutionRequest,
    result: PolicyDecisionResult,
    processId?: string,
  ): Promise<void> {
    const denied = result.decision === 'deny' || result.decision === 'require_approval';
    await this.auditManager.log({
      eventType: 'security',
      severity:
        result.decision === 'deny'
          ? 'error'
          : result.decision === 'require_approval'
            ? 'warning'
            : 'info',
      actor: {
        id: request.actor.id,
        type: request.actor.type,
        name: request.actor.name,
        sessionId: request.actor.sessionId,
      },
      resource: {
        type: 'tool-runtime',
        id: processId ?? request.command,
        name: request.command,
        path: request.cwd,
      },
      action: denied ? 'policy_block' : 'policy_execute',
      outcome: denied ? 'failure' : 'success',
      details: {
        command: request.command,
        args: request.args ?? [],
        metadataPreview: sanitizeRuntimeMetadataPreview(request.metadata),
        decision: result.decision,
        risk: result.risk,
        notes: result.notes,
        matchedBoundaries: result.matchedBoundaries,
        missingBoundaries: result.missingBoundaries,
        processId,
      },
    });
  }
}

type TaskDiffs = NonNullable<NonNullable<CoworkTask['output']>['diffs']>;
type RuntimeOutcome = NonNullable<NonNullable<CoworkTask['output']>['runtime']>;
type RuntimeFallback = NonNullable<RuntimeOutcome['fallback']>;

interface ExecutionAttemptResult {
  diffs: TaskDiffs;
  runtimeResult?: ExecutionSandboxResult;
  error?: string;
  retryable: boolean;
}

function normalizeExecutionResult(
  task: CoworkTask,
  executor: ExecutorRegistration,
  startTime: number,
  diffs: NonNullable<CoworkTask['output']>['diffs'],
  error?: string,
  runtime?: CoworkTask['output'] extends infer T
    ? T extends { runtime?: infer R }
      ? R
      : never
    : never,
): ExecutionResult {
  const duration = Date.now() - startTime;
  const success = !error;
  const status = success ? 'completed' : 'failed';
  const resultMessage = success ? 'Task completed successfully' : undefined;

  return {
    taskId: task.id,
    status,
    executor: executor.name,
    duration,
    success,
    diffs,
    error,
    output: {
      result: resultMessage,
      diffs,
      error,
      runtime,
      metrics: {
        duration,
      },
    },
  };
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

export class AgentRuntime implements AgentRuntimeLike {
  private readonly executors = new Map<string, ExecutorRegistration>();
  private readonly modelPool: ModelPool;
  private readonly contextAssembler: ContextAssembler;
  private readonly policyGuard: PolicyGuard;
  private readonly sandbox: ExecutionSandbox;
  private readonly auditManager: IAuditManager;
  private readonly policyEngine = new RuntimePolicyEngine();
  private readonly auditTrail: RuntimeAuditTrail;
  private readonly headlessToolRuntime: HeadlessToolRuntime;
  private readonly toolRegistry: ToolRegistry;
  private readonly toolExecutor: ToolExecutor;
  private readonly fileOperationService: FileOperationService;

  constructor(deps: AgentRuntimeDeps = {}) {
    this.modelPool = deps.modelPool ?? new InMemoryModelPool();
    this.contextAssembler = deps.contextAssembler ?? new DefaultContextAssembler();
    this.policyGuard = deps.policyGuard ?? new AllowAllPolicyGuard();
    this.sandbox = deps.sandbox ?? new PassthroughSandbox();
    this.auditManager = deps.auditManager ?? new AuditManager();
    this.auditTrail = new RuntimeAuditTrail(this.auditManager);
    this.headlessToolRuntime =
      deps.headlessToolRuntime ??
      new HeadlessToolRuntime({
        auditManager: this.auditManager,
        toolRegistry: deps.toolRegistry,
        toolExecutor: deps.toolExecutor,
        fileOperationService: deps.fileOperationService,
      });
    this.toolRegistry = this.headlessToolRuntime.getToolRegistry();
    this.toolExecutor = this.headlessToolRuntime.getToolExecutor();
    this.fileOperationService = this.headlessToolRuntime.getFileOperationService();
  }

  registerExecutor(
    name: string,
    editor: ICodeEditor,
    capabilities: ExecutorCapabilities,
    modelId?: string,
  ): void {
    const registration: ExecutorRegistration = {
      name,
      editor,
      capabilities,
      modelId,
    };
    this.executors.set(name, registration);
    this.modelPool.registerExecutor(registration);
  }

  registerSkill(skill: SkillRegistration, replace = false): void {
    this.headlessToolRuntime.registerSkill(skill, replace);
  }

  getExecutor(name: string): ExecutorRegistration | undefined {
    const executor = this.executors.get(name);
    if (!executor) {
      return undefined;
    }

    const modelId = executor.modelId ?? this.modelPool.getModelId(name);
    return {
      ...executor,
      modelId,
    };
  }

  getAllExecutors(): ExecutorRegistration[] {
    return Array.from(this.executors.keys())
      .map((name) => this.getExecutor(name))
      .filter((executor): executor is ExecutorRegistration => Boolean(executor));
  }

  buildContextFromResult(result: ExecutionResult): string {
    return this.contextAssembler.buildContextFromResult(result);
  }

  attachPreviousResult(task: CoworkTask, result: ExecutionResult): CoworkTask {
    return this.contextAssembler.attachPreviousResult(task, result);
  }

  getToolRegistry(): ToolRegistry {
    return this.toolRegistry;
  }

  getToolExecutor(): ToolExecutor {
    return this.toolExecutor;
  }

  getToolTraces(): ToolCallTrace[] {
    return this.toolExecutor.getTraces();
  }

  getSkillExecutionRecords(): SkillExecutionRecord[] {
    return this.headlessToolRuntime.getSkillExecutionRecords();
  }

  async executeSkill<TOutput = unknown>(
    skillId: string,
    input: unknown,
    context: {
      taskId?: string;
      sessionId?: string;
      agentId?: string;
      triggerReason?: string;
    } = {},
  ): Promise<TOutput> {
    const result = await this.headlessToolRuntime.executeSkill<TOutput>({
      skillId,
      input,
      triggerReason: context.triggerReason,
      context: {
        entryPoint: 'agent',
        taskId: context.taskId,
        sessionId: context.sessionId,
        agentId: context.agentId,
        actor: {
          id: context.agentId ?? 'agent-runtime',
          type: 'agent',
          name: context.agentId ?? 'agent-runtime',
        },
      },
    });

    if (!result.ok) {
      throw new Error(result.error?.message ?? `Skill execution failed: ${skillId}`);
    }

    return result.output as TOutput;
  }

  async executeTask(task: CoworkTask, options: RuntimeExecutionOptions = {}): Promise<ExecutionResult> {
    const startTime = Date.now();
    const executor = this.getExecutor(task.executor);
    if (!executor) {
      return normalizeExecutionResult(
        task,
        {
          name: task.executor,
          editor: {
            name: 'missing-editor',
            edit: async () => {
              throw new Error('missing executor');
            },
            editMultiple: async () => {
              throw new Error('missing executor');
            },
            preview: async () => ({ file: '', hunks: [], additions: 0, deletions: 0 }),
            applyDiff: async () => undefined,
            undo: async () => undefined,
          },
          capabilities: {
            name: task.executor,
            supportedTypes: [],
            maxConcurrency: 0,
            estimatedSpeed: 'slow',
            features: {
              streaming: false,
              multiFile: false,
              contextAware: false,
              codeReview: false,
            },
          },
        },
        startTime,
        [],
        `Executor '${task.executor}' not found`,
      );
    }

    const policy = await this.policyGuard.canExecute(task, executor);
    if (!policy.allowed) {
      return normalizeExecutionResult(task, executor, startTime, [], policy.reason || 'Execution denied');
    }

    const sandboxed = await this.sandbox.prepare(task, executor, options);
    const initialExecutor = options.executorOverride ?? sandboxed.executor ?? executor;

    try {
      const primaryAttempt = await this.runTaskAttempt(sandboxed.task, initialExecutor, options);
      if (!primaryAttempt.error) {
        this.modelPool.markExecutorHealthy(initialExecutor.name);
        return normalizeExecutionResult(
          task,
          initialExecutor,
          startTime,
          primaryAttempt.diffs,
          undefined,
          primaryAttempt.runtimeResult ? this.toRuntimeOutcome(primaryAttempt.runtimeResult) : undefined,
        );
      }

      this.modelPool.markExecutorUnhealthy(initialExecutor.name, primaryAttempt.error);
      const fallback = this.modelPool.getFallbackExecutor(sandboxed.task, initialExecutor.name);
      if (!primaryAttempt.retryable || !fallback) {
        return normalizeExecutionResult(
          task,
          initialExecutor,
          startTime,
          primaryAttempt.diffs,
          primaryAttempt.error,
          primaryAttempt.runtimeResult
            ? this.toRuntimeOutcome(primaryAttempt.runtimeResult, {
                attempted: Boolean(fallback),
                fromExecutor: initialExecutor.name,
                toExecutor: fallback?.name,
                reason: primaryAttempt.error,
                recovered: false,
              })
            : undefined,
        );
      }

      const fallbackAttempt = await this.runTaskAttempt(sandboxed.task, fallback, {
        ...options,
        executorOverride: fallback,
      });
      const fallbackMeta: RuntimeFallback = {
        attempted: true,
        fromExecutor: initialExecutor.name,
        toExecutor: fallback.name,
        reason: primaryAttempt.error,
        recovered: !fallbackAttempt.error,
      };

      if (!fallbackAttempt.error) {
        this.modelPool.markExecutorHealthy(fallback.name);
        return normalizeExecutionResult(
          task,
          fallback,
          startTime,
          fallbackAttempt.diffs,
          undefined,
          fallbackAttempt.runtimeResult
            ? this.toRuntimeOutcome(fallbackAttempt.runtimeResult, fallbackMeta)
            : undefined,
        );
      }

      this.modelPool.markExecutorUnhealthy(fallback.name, fallbackAttempt.error);
      return normalizeExecutionResult(
        task,
        fallback,
        startTime,
        fallbackAttempt.diffs,
        fallbackAttempt.error,
        fallbackAttempt.runtimeResult
          ? this.toRuntimeOutcome(fallbackAttempt.runtimeResult, fallbackMeta)
          : undefined,
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Unknown error';
      this.modelPool.markExecutorUnhealthy(initialExecutor.name, message);
      return normalizeExecutionResult(task, initialExecutor, startTime, [], message);
    } finally {
      await sandboxed.release?.();
    }
  }

  private async runTaskAttempt(
    task: CoworkTask,
    executor: ExecutorRegistration,
    options: RuntimeExecutionOptions,
  ): Promise<ExecutionAttemptResult> {
    const runtimeResult = await this.evaluateRuntimePolicy(task, executor, options);
    if (runtimeResult && !this.canProceed(runtimeResult)) {
      return {
        diffs: [],
        runtimeResult,
        error: this.getRuntimeErrorMessage(runtimeResult),
        retryable: false,
      };
    }

    const diffs: TaskDiffs = [];
    if (task.input.files.length === 1) {
      const editResult = await this.executeTool(
        'file.edit',
        {
          file: task.input.files[0],
          instruction: task.input.instruction,
        },
        task,
        executor,
        options,
      );
      if (!editResult.ok) {
        return {
          diffs,
          runtimeResult,
          error: editResult.error?.message,
          retryable: true,
        };
      }
      const result = editResult.output as EditResult | undefined;
      if (result?.success) {
        diffs.push(result.diff);
      } else if (result?.message) {
        return {
          diffs,
          runtimeResult,
          error: result.message,
          retryable: true,
        };
      }
      return { diffs, runtimeResult, retryable: false };
    }

    if (task.input.files.length > 1) {
      const batchResult = await this.executeTool(
        'file.edit_multiple',
        {
          files: task.input.files,
          instruction: task.input.instruction,
        },
        task,
        executor,
        options,
      );
      if (!batchResult.ok) {
        return {
          diffs,
          runtimeResult,
          error: batchResult.error?.message,
          retryable: true,
        };
      }
      const results = (batchResult.output ?? []) as EditResult[];
      const failed = results.find((result: EditResult) => !result.success);
      for (const result of results) {
        if (result.success) {
          diffs.push(result.diff);
        }
      }
      if (failed?.message) {
        return {
          diffs,
          runtimeResult,
          error: failed.message,
          retryable: true,
        };
      }
    }

    return { diffs, runtimeResult, retryable: false };
  }

  private async executeTool(
    toolId: string,
    input: unknown,
    task: CoworkTask,
    executor: ExecutorRegistration,
    options: RuntimeExecutionOptions,
  ) {
    return this.toolExecutor.execute({
      toolId,
      input,
      context: this.createToolContext(task, executor, options),
    });
  }

  private createToolContext(
    task: CoworkTask,
    executor: ExecutorRegistration,
    options: RuntimeExecutionOptions,
  ) {
    return {
      entryPoint: 'agent' as const,
      taskId: task.id,
      agentId: executor.name,
      actor: {
        id: executor.name,
        type: 'agent' as const,
        name: executor.name,
      },
      metadata: {
        cwd: options.cwd,
        worktreePath: options.worktreePath,
        modelId: executor.modelId,
      },
      resources: {
        editor: executor.editor,
      },
    };
  }

  private async evaluateRuntimePolicy(
    task: CoworkTask,
    executor: ExecutorRegistration,
    options: RuntimeExecutionOptions,
  ): Promise<ExecutionSandboxResult | undefined> {
    const runtimePolicy = task.runtime?.policy;
    if (!runtimePolicy) {
      return undefined;
    }

    const command = runtimePolicy.command ?? executor.name;
    const profileBoundaries = runtimePolicy.boundaries ?? [];
    const requestBoundaries: RuntimeToolExecutionRequest['boundaries'] = [
      { type: 'command', value: command, risk: 'low' },
      ...(options.cwd ? [{ type: 'path' as const, value: options.cwd, risk: 'low' as const }] : []),
      ...(runtimePolicy.requestedBoundaries ?? []),
      ...((runtimePolicy.metadata?.requestedBoundaries as RuntimeToolExecutionRequest['boundaries'] | undefined) ?? []),
    ];
    const request: RuntimeToolExecutionRequest = {
      command,
      args: [],
      cwd: options.cwd,
      env: {
        ...(runtimePolicy.env ?? {}),
      },
      actor: {
        id: task.runtime?.actor?.id ?? executor.name,
        type: task.runtime?.actor?.type ?? 'agent',
        name: task.runtime?.actor?.name ?? executor.name,
        sessionId: task.runtime?.actor?.sessionId,
      },
      boundaries: requestBoundaries,
      metadata: {
        ...(runtimePolicy.metadata ?? {}),
        taskId: task.id,
        executor: executor.name,
        worktreePath: options.worktreePath,
      },
    };
    const profile: PermissionProfile = {
      id: runtimePolicy.profileId ?? `${task.id}:${executor.name}`,
      name: runtimePolicy.profileId ?? `${task.id}:${executor.name}`,
      defaultDecision: 'allow',
      boundaries: profileBoundaries,
      metadata: runtimePolicy.metadata,
    };

    const decision = this.policyEngine.evaluate(profile, request);
    const isolated = decision.decision === 'allow_with_isolation';
    let processId: string | undefined;
    if (isolated) {
      processId = `isolated:${task.id}`;
    }
    await this.auditTrail.recordDecision(request, decision, processId);

    return {
      processId,
      decision,
      isolated,
      snapshot: this.createRuntimeSnapshot(request, decision, isolated, processId),
    };
  }

  private sanitizeMetadataPreview(metadata?: Record<string, unknown>): Record<string, unknown> | undefined {
    return sanitizeRuntimeMetadataPreview(metadata);
  }

  private sanitizeMetadataValue(value: unknown): unknown {
    return sanitizeRuntimeMetadataValue(value);
  }

  private createRuntimeSnapshot(
    request: RuntimeToolExecutionRequest,
    decision: PolicyDecisionResult,
    isolated: boolean,
    processId?: string,
  ): ToolRuntimeExecutionSnapshot {
    return {
      decision: decision.decision,
      risk: decision.risk,
      isolated,
      processId,
      command: request.command,
      args: request.args ?? [],
      cwd: request.cwd,
      envKeys: Object.keys(request.env ?? {}).sort(),
      boundarySummary: {
        matched: decision.matchedBoundaries.length,
        missing: decision.missingBoundaries.length,
        required: request.boundaries.filter((boundary) => boundary.required).length,
      },
      metadataPreview: this.sanitizeMetadataPreview(request.metadata),
      notes: [...decision.notes],
    };
  }

  private canProceed(result: ExecutionSandboxResult): boolean {
    return result.decision.decision === 'allow' || result.decision.decision === 'allow_with_isolation';
  }

  private getRuntimeErrorMessage(result: ExecutionSandboxResult): string {
    if (result.decision.decision === 'require_approval') {
      return 'Execution requires manual approval';
    }
    if (result.decision.decision === 'deny') {
      return 'Execution blocked by runtime policy';
    }
    return 'Execution denied';
  }

  private toRuntimeOutcome(result: ExecutionSandboxResult, fallback?: RuntimeFallback): RuntimeOutcome {
    return {
      decision: result.decision.decision,
      risk: result.decision.risk,
      isolated: result.isolated,
      processId: result.processId,
      notes: [...result.snapshot.notes],
      snapshot: {
        command: result.snapshot.command,
        args: [...result.snapshot.args],
        cwd: result.snapshot.cwd,
        envKeys: [...result.snapshot.envKeys],
        boundarySummary: {
          matched: result.snapshot.boundarySummary.matched,
          missing: result.snapshot.boundarySummary.missing,
          required: result.snapshot.boundarySummary.required,
        },
        metadataPreview: result.snapshot.metadataPreview,
      },
      fallback,
    };
  }
}

export { InMemoryModelPool, DefaultContextAssembler, AllowAllPolicyGuard, PassthroughSandbox };
