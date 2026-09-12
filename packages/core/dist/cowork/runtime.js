import { AuditManager } from '../audit/AuditManager.js';
import { HeadlessToolRuntime } from '../tool-runtime/HeadlessToolRuntime.js';
class InMemoryModelPool {
    constructor() {
        this.models = new Map();
        this.executors = new Map();
        this.health = new Map();
    }
    registerExecutor(executor) {
        this.models.set(executor.name, executor.modelId);
        this.executors.set(executor.name, executor);
        this.markExecutorHealthy(executor.name);
    }
    getModelId(executorName) {
        return this.models.get(executorName);
    }
    markExecutorHealthy(executorName) {
        this.health.set(executorName, { healthy: true });
    }
    markExecutorUnhealthy(executorName, reason) {
        this.health.set(executorName, { healthy: false, reason });
    }
    getFallbackExecutor(task, currentExecutor) {
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
class DefaultContextAssembler {
    buildContextFromResult(result) {
        if (!result.output) {
            return '';
        }
        const parts = [];
        if (result.output.result) {
            parts.push(`Previous output:\n${result.output.result}`);
        }
        if (result.output.diffs && result.output.diffs.length > 0) {
            parts.push(`Previous changes:\n${result.output.diffs
                .map((diff) => `- ${diff.file}: +${diff.additions}/-${diff.deletions}`)
                .join('\n')}`);
        }
        return parts.join('\n\n');
    }
    attachPreviousResult(task, result) {
        return {
            ...task,
            input: {
                ...task.input,
                context: this.buildContextFromResult(result),
            },
        };
    }
}
class AllowAllPolicyGuard {
    canExecute() {
        return { allowed: true };
    }
}
class PassthroughSandbox {
    async prepare(task) {
        return { task };
    }
}
class RuntimePolicyEngine {
    evaluate(profile, request) {
        const matchedBoundaries = [];
        const missingBoundaries = request.boundaries
            .filter((boundary) => {
            const matched = profile.boundaries.find((candidate) => candidate.type === boundary.type && candidate.value === boundary.value);
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
            .filter((boundary) => !request.boundaries.some((candidate) => candidate.type === boundary.type && candidate.value === boundary.value))
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
    resolveRisk(levels) {
        if (levels.includes('high')) {
            return 'high';
        }
        if (levels.includes('medium')) {
            return 'medium';
        }
        return 'low';
    }
    resolveDecision(defaultDecision, violationRisk, hasViolations) {
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
    buildNotes(decision, risk, missingCount) {
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
function sanitizeRuntimeMetadataValue(value) {
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
function sanitizeRuntimeMetadataPreview(metadata) {
    if (!metadata) {
        return undefined;
    }
    const preview = Object.entries(metadata).reduce((acc, [key, value]) => {
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
    constructor(auditManager) {
        this.auditManager = auditManager;
    }
    async recordDecision(request, result, processId) {
        const denied = result.decision === 'deny' || result.decision === 'require_approval';
        await this.auditManager.log({
            eventType: 'security',
            severity: result.decision === 'deny'
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
function normalizeExecutionResult(task, executor, startTime, diffs, error, runtime) {
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
export class AgentRuntime {
    constructor(deps = {}) {
        this.executors = new Map();
        this.policyEngine = new RuntimePolicyEngine();
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
                    hookManager: deps.hookManager,
                    hookControls: deps.hookControls,
                });
        this.toolRegistry = this.headlessToolRuntime.getToolRegistry();
        this.toolExecutor = this.headlessToolRuntime.getToolExecutor();
        this.fileOperationService = this.headlessToolRuntime.getFileOperationService();
    }
    registerExecutor(name, editor, capabilities, modelId) {
        const registration = {
            name,
            editor,
            capabilities,
            modelId,
        };
        this.executors.set(name, registration);
        this.modelPool.registerExecutor(registration);
    }
    registerSkill(skill, replace = false) {
        this.headlessToolRuntime.registerSkill(skill, replace);
    }
    getExecutor(name) {
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
    getAllExecutors() {
        return Array.from(this.executors.keys())
            .map((name) => this.getExecutor(name))
            .filter((executor) => Boolean(executor));
    }
    buildContextFromResult(result) {
        return this.contextAssembler.buildContextFromResult(result);
    }
    attachPreviousResult(task, result) {
        return this.contextAssembler.attachPreviousResult(task, result);
    }
    getToolRegistry() {
        return this.toolRegistry;
    }
    getToolExecutor() {
        return this.toolExecutor;
    }
    getHookManager() {
        return this.headlessToolRuntime.getHookManager();
    }
    getToolTraces() {
        return this.toolExecutor.getTraces();
    }
    getSkillExecutionRecords() {
        return this.headlessToolRuntime.getSkillExecutionRecords();
    }
    async executeSkill(skillId, input, context = {}) {
        const result = await this.headlessToolRuntime.executeSkill({
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
                metadata: context.metadata,
            },
        });
        if (!result.ok) {
            throw new Error(result.error?.message ?? `Skill execution failed: ${skillId}`);
        }
        return result.output;
    }
    async executeTask(task, options = {}) {
        const startTime = Date.now();
        const executor = this.getExecutor(task.executor);
        if (!executor) {
            return normalizeExecutionResult(task, {
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
            }, startTime, [], `Executor '${task.executor}' not found`);
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
                return normalizeExecutionResult(task, initialExecutor, startTime, primaryAttempt.diffs, undefined, primaryAttempt.runtimeResult ? this.toRuntimeOutcome(primaryAttempt.runtimeResult) : undefined);
            }
            this.modelPool.markExecutorUnhealthy(initialExecutor.name, primaryAttempt.error);
            const fallback = this.modelPool.getFallbackExecutor(sandboxed.task, initialExecutor.name);
            if (!primaryAttempt.retryable || !fallback) {
                return normalizeExecutionResult(task, initialExecutor, startTime, primaryAttempt.diffs, primaryAttempt.error, primaryAttempt.runtimeResult
                    ? this.toRuntimeOutcome(primaryAttempt.runtimeResult, {
                        attempted: Boolean(fallback),
                        fromExecutor: initialExecutor.name,
                        toExecutor: fallback?.name,
                        reason: primaryAttempt.error,
                        recovered: false,
                    })
                    : undefined);
            }
            const fallbackAttempt = await this.runTaskAttempt(sandboxed.task, fallback, {
                ...options,
                executorOverride: fallback,
            });
            const fallbackMeta = {
                attempted: true,
                fromExecutor: initialExecutor.name,
                toExecutor: fallback.name,
                reason: primaryAttempt.error,
                recovered: !fallbackAttempt.error,
            };
            if (!fallbackAttempt.error) {
                this.modelPool.markExecutorHealthy(fallback.name);
                return normalizeExecutionResult(task, fallback, startTime, fallbackAttempt.diffs, undefined, fallbackAttempt.runtimeResult
                    ? this.toRuntimeOutcome(fallbackAttempt.runtimeResult, fallbackMeta)
                    : undefined);
            }
            this.modelPool.markExecutorUnhealthy(fallback.name, fallbackAttempt.error);
            return normalizeExecutionResult(task, fallback, startTime, fallbackAttempt.diffs, fallbackAttempt.error, fallbackAttempt.runtimeResult
                ? this.toRuntimeOutcome(fallbackAttempt.runtimeResult, fallbackMeta)
                : undefined);
        }
        catch (error) {
            const message = error instanceof Error ? error.message : 'Unknown error';
            this.modelPool.markExecutorUnhealthy(initialExecutor.name, message);
            return normalizeExecutionResult(task, initialExecutor, startTime, [], message);
        }
        finally {
            await sandboxed.release?.();
        }
    }
    async runTaskAttempt(task, executor, options) {
        const runtimeResult = await this.evaluateRuntimePolicy(task, executor, options);
        if (runtimeResult && !this.canProceed(runtimeResult)) {
            return {
                diffs: [],
                runtimeResult,
                error: this.getRuntimeErrorMessage(runtimeResult),
                retryable: false,
            };
        }
        const diffs = [];
        if (task.input.files.length === 1) {
            const editResult = await this.executeTool('file.edit', {
                file: task.input.files[0],
                instruction: task.input.instruction,
            }, task, executor, options);
            if (!editResult.ok) {
                return {
                    diffs,
                    runtimeResult,
                    error: editResult.error?.message,
                    retryable: true,
                };
            }
            const result = editResult.output;
            if (result?.success) {
                diffs.push(result.diff);
            }
            else if (result?.message) {
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
            const batchResult = await this.executeTool('file.edit_multiple', {
                files: task.input.files,
                instruction: task.input.instruction,
            }, task, executor, options);
            if (!batchResult.ok) {
                return {
                    diffs,
                    runtimeResult,
                    error: batchResult.error?.message,
                    retryable: true,
                };
            }
            const results = (batchResult.output ?? []);
            const failed = results.find((result) => !result.success);
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
    async executeTool(toolId, input, task, executor, options) {
        return this.toolExecutor.execute({
            toolId,
            input,
            context: this.createToolContext(task, executor, options),
        });
    }
    createToolContext(task, executor, options) {
        return {
            entryPoint: 'agent',
            taskId: task.id,
            agentId: executor.name,
            actor: {
                id: executor.name,
                type: 'agent',
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
    async evaluateRuntimePolicy(task, executor, options) {
        const runtimePolicy = task.runtime?.policy;
        if (!runtimePolicy) {
            return undefined;
        }
        const command = runtimePolicy.command ?? executor.name;
        const profileBoundaries = runtimePolicy.boundaries ?? [];
        const requestBoundaries = [
            { type: 'command', value: command, risk: 'low' },
            ...(options.cwd ? [{ type: 'path', value: options.cwd, risk: 'low' }] : []),
            ...(runtimePolicy.requestedBoundaries ?? []),
            ...(runtimePolicy.metadata?.requestedBoundaries ?? []),
        ];
        const request = {
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
        const profile = {
            id: runtimePolicy.profileId ?? `${task.id}:${executor.name}`,
            name: runtimePolicy.profileId ?? `${task.id}:${executor.name}`,
            defaultDecision: 'allow',
            boundaries: profileBoundaries,
            metadata: runtimePolicy.metadata,
        };
        const decision = this.policyEngine.evaluate(profile, request);
        const isolated = decision.decision === 'allow_with_isolation';
        let processId;
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
    sanitizeMetadataPreview(metadata) {
        return sanitizeRuntimeMetadataPreview(metadata);
    }
    sanitizeMetadataValue(value) {
        return sanitizeRuntimeMetadataValue(value);
    }
    createRuntimeSnapshot(request, decision, isolated, processId) {
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
    canProceed(result) {
        return result.decision.decision === 'allow' || result.decision.decision === 'allow_with_isolation';
    }
    getRuntimeErrorMessage(result) {
        if (result.decision.decision === 'require_approval') {
            return 'Execution requires manual approval';
        }
        if (result.decision.decision === 'deny') {
            return 'Execution blocked by runtime policy';
        }
        return 'Execution denied';
    }
    toRuntimeOutcome(result, fallback) {
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
//# sourceMappingURL=runtime.js.map