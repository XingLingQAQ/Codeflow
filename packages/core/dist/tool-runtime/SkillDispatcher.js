const DEFAULT_TIMEOUT_MS = 30000;
function summarizeValue(value) {
    if (value === undefined)
        return 'undefined';
    if (value === null)
        return 'null';
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
        const keys = Object.keys(value);
        return `object(${keys.slice(0, 6).join(',')}${keys.length > 6 ? ',...' : ''})`;
    }
    return typeof value;
}
function createRecordId() {
    return `skill_${Date.now()}_${Math.random().toString(16).slice(2, 10)}`;
}
class AllowAllSkillAuthorizer {
    authorize() {
        return {
            allowed: true,
            approvalState: 'not_required',
        };
    }
}
export class SkillDispatcher {
    constructor(registry, runtime, options = {}) {
        this.registry = registry;
        this.runtime = runtime;
        this.options = options;
        this.records = [];
        this.authorizer = options.authorizer ?? new AllowAllSkillAuthorizer();
        this.recordLimit = options.recordLimit ?? 200;
    }
    async execute(request) {
        const startedAt = Date.now();
        const skill = this.registry.resolve(request.skillId, request.version);
        if (!skill) {
            return {
                ok: false,
                error: {
                    code: 'skill_not_found',
                    message: `Skill not found: ${request.skillId}`,
                },
                record: this.createRecord({
                    skillId: request.skillId,
                    version: request.version ?? 'missing',
                    description: 'Missing skill',
                    tags: [],
                    riskLevel: 'medium',
                    source: 'internal',
                    entryPoints: [request.context.entryPoint],
                    inputSchema: { type: 'object', properties: {}, required: [] },
                }, request, startedAt, {
                    status: 'failed',
                    approvalState: 'rejected',
                    lifecycle: ['registered'],
                    error: 'Skill not found',
                }),
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
        const lifecycle = ['registered', 'authorized', 'loaded'];
        const traceCountBefore = this.runtime.getToolTraceCount();
        const context = {
            ...request.context,
            runtime: this.runtime,
            triggerReason: request.triggerReason,
            approvalState: authorization.approvalState ?? 'not_required',
        };
        try {
            const output = await this.executeWithTimeout(manifest, () => skill.handler.execute(request.input, context));
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
                output: output,
                record,
            };
        }
        catch (error) {
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
                },
                record,
            };
        }
    }
    getRecords() {
        return [...this.records];
    }
    clearRecords() {
        this.records.length = 0;
    }
    async executeWithTimeout(manifest, fn) {
        const timeoutMs = manifest.defaultTimeoutMs ?? DEFAULT_TIMEOUT_MS;
        return await Promise.race([
            Promise.resolve(fn()),
            new Promise((_, reject) => {
                setTimeout(() => reject(new Error(`Skill timeout after ${timeoutMs}ms`)), timeoutMs);
            }),
        ]);
    }
    createRecord(manifest, request, startedAt, options) {
        const completedAt = Date.now();
        const traces = this.runtime.getToolTraces();
        const relatedTraces = traces.slice(options.traceCountBefore ?? 0).filter((trace) => (request.context.sessionId ? trace.sessionId === request.context.sessionId : true) &&
            (request.context.taskId ? trace.taskId === request.context.taskId : true) &&
            (request.context.agentId ? trace.agentId === request.context.agentId : true));
        const skillToolCallIds = this.extractSkillToolCallIds(options.output);
        const toolIds = manifest.toolIds && manifest.toolIds.length > 0
            ? Array.from(new Set([...manifest.toolIds, ...relatedTraces.map((trace) => trace.toolId)]))
            : Array.from(new Set(relatedTraces.map((trace) => trace.toolId)));
        const toolCallIds = Array.from(new Set([...relatedTraces.map((trace) => trace.toolCallId), ...skillToolCallIds]));
        const artifacts = relatedTraces.flatMap((trace) => trace.artifacts ?? []);
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
    pushRecord(record) {
        this.records.push(record);
        if (this.records.length > this.recordLimit) {
            this.records.splice(0, this.records.length - this.recordLimit);
        }
    }
    sanitizeOutput(output) {
        if (!output || typeof output !== 'object' || Array.isArray(output)) {
            return output;
        }
        const cloned = { ...output };
        delete cloned.__skillToolCallId;
        return cloned;
    }
    extractSkillToolCallIds(output) {
        if (!output || typeof output !== 'object' || Array.isArray(output)) {
            return [];
        }
        const toolCallId = output.__skillToolCallId;
        return typeof toolCallId === 'string' && toolCallId.length > 0 ? [toolCallId] : [];
    }
    async logAudit(manifest, record) {
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
//# sourceMappingURL=SkillDispatcher.js.map