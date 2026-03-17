const DEFAULT_TRACE_LIMIT = 200;
const SYSTEM_ACTOR = {
    id: 'tool-runtime',
    type: 'system',
    name: 'tool-runtime',
};
function summarizeValue(value) {
    if (value === undefined) {
        return 'undefined';
    }
    if (value === null) {
        return 'null';
    }
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
function createToolCallId() {
    return `toolcall_${Date.now()}_${Math.random().toString(16).slice(2, 10)}`;
}
export class ToolExecutor {
    constructor(registry, options = {}) {
        this.registry = registry;
        this.options = options;
        this.traces = [];
        this.traceLimit = options.traceLimit ?? DEFAULT_TRACE_LIMIT;
    }
    async execute(request) {
        const startedAt = Date.now();
        const tool = this.registry.get(request.toolId);
        if (!tool) {
            const trace = this.createTrace({
                id: request.toolId,
                version: 'missing',
                description: 'Missing tool',
                tags: [],
                riskLevel: 'medium',
                executionModes: ['sync'],
                entryPoints: [request.context.entryPoint],
                source: 'internal',
                inputSchema: { type: 'object', properties: {}, required: [] },
            }, request, startedAt, undefined, {
                code: 'tool_not_found',
                message: `Tool not found: ${request.toolId}`,
            });
            this.pushTrace(trace);
            await this.logAudit(undefined, request, trace, 'failure');
            return {
                ok: false,
                error: {
                    code: 'tool_not_found',
                    message: `Tool not found: ${request.toolId}`,
                },
                trace,
            };
        }
        const policy = await this.options.policyGuard?.canExecute(tool, request);
        if (policy && !policy.allowed) {
            const trace = this.createTrace(tool, request, startedAt, undefined, {
                code: 'tool_denied',
                message: policy.reason || 'Tool execution denied',
            });
            this.pushTrace(trace);
            await this.logAudit(tool, request, trace, 'failure');
            return {
                ok: false,
                error: {
                    code: 'tool_denied',
                    message: policy.reason || 'Tool execution denied',
                },
                trace,
            };
        }
        try {
            const output = (await tool.handler.execute(request.input, request.context));
            const trace = this.createTrace(tool, request, startedAt, output);
            this.pushTrace(trace);
            await this.logAudit(tool, request, trace, 'success');
            return {
                ok: true,
                output,
                trace,
            };
        }
        catch (error) {
            const trace = this.createTrace(tool, request, startedAt, undefined, {
                code: 'tool_execution_failed',
                message: error instanceof Error ? error.message : String(error),
                details: error,
            });
            this.pushTrace(trace);
            await this.logAudit(tool, request, trace, 'failure');
            return {
                ok: false,
                error: {
                    code: 'tool_execution_failed',
                    message: error instanceof Error ? error.message : String(error),
                    details: error,
                },
                trace,
            };
        }
    }
    getTraces() {
        return [...this.traces];
    }
    clearTraces() {
        this.traces.length = 0;
    }
    createTrace(tool, request, startedAt, output, error) {
        const completedAt = Date.now();
        return {
            toolCallId: createToolCallId(),
            toolId: tool.id,
            source: tool.source,
            entryPoint: request.context.entryPoint,
            sessionId: request.context.sessionId,
            taskId: request.context.taskId,
            agentId: request.context.agentId,
            startedAt,
            completedAt,
            duration: completedAt - startedAt,
            inputSummary: summarizeValue(request.input),
            outputSummary: output === undefined ? undefined : summarizeValue(output),
            success: !error,
            error: error?.message,
        };
    }
    pushTrace(trace) {
        this.traces.push(trace);
        if (this.traces.length > this.traceLimit) {
            this.traces.splice(0, this.traces.length - this.traceLimit);
        }
    }
    async logAudit(tool, request, trace, outcome) {
        if (!this.options.auditManager) {
            return;
        }
        const actor = request.context.actor ?? SYSTEM_ACTOR;
        await this.options.auditManager.log({
            eventType: outcome === 'success' ? 'access' : 'error',
            severity: outcome === 'success' ? 'info' : 'warning',
            actor,
            resource: {
                type: 'tool',
                id: trace.toolId,
                name: tool?.description,
            },
            action: 'execute',
            outcome,
            details: {
                toolCallId: trace.toolCallId,
                entryPoint: trace.entryPoint,
                sessionId: trace.sessionId,
                taskId: trace.taskId,
                agentId: trace.agentId,
                source: trace.source,
                inputSummary: trace.inputSummary,
                outputSummary: trace.outputSummary,
                duration: trace.duration,
                error: trace.error,
            },
        });
    }
}
//# sourceMappingURL=ToolExecutor.js.map