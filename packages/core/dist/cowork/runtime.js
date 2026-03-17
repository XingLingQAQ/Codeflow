/**
 * Cowork Runtime
 * 收敛执行器注册、上下文装配、策略校验与沙箱准备
 */
import { HeadlessToolRuntime } from '../tool-runtime/HeadlessToolRuntime.js';
class InMemoryModelPool {
    constructor() {
        this.models = new Map();
    }
    registerExecutor(executor) {
        this.models.set(executor.name, executor.modelId);
    }
    getModelId(executorName) {
        return this.models.get(executorName);
    }
}
class DefaultContextAssembler {
    buildContextFromResult(result) {
        if (!result.output)
            return '';
        const parts = [];
        if (result.output.result) {
            parts.push(`Previous output:\n${result.output.result}`);
        }
        if (result.output.diffs && result.output.diffs.length > 0) {
            parts.push(`Previous changes:\n${result.output.diffs.map((d) => `- ${d.file}: +${d.additions}/-${d.deletions}`).join('\n')}`);
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
function normalizeExecutionResult(task, executor, startTime, diffs, error) {
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
            metrics: {
                duration,
            },
        },
    };
}
export class AgentRuntime {
    constructor(deps = {}) {
        this.executors = new Map();
        this.modelPool = deps.modelPool ?? new InMemoryModelPool();
        this.contextAssembler = deps.contextAssembler ?? new DefaultContextAssembler();
        this.policyGuard = deps.policyGuard ?? new AllowAllPolicyGuard();
        this.sandbox = deps.sandbox ?? new PassthroughSandbox();
        this.headlessToolRuntime =
            deps.headlessToolRuntime ??
                new HeadlessToolRuntime({
                    auditManager: deps.auditManager,
                    toolRegistry: deps.toolRegistry,
                    toolExecutor: deps.toolExecutor,
                    fileOperationService: deps.fileOperationService,
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
    getExecutor(name) {
        const executor = this.executors.get(name);
        if (!executor)
            return undefined;
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
    getToolTraces() {
        return this.toolExecutor.getTraces();
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
        const activeExecutor = options.executorOverride ?? sandboxed.executor ?? executor;
        try {
            const diffs = [];
            const sandboxedTask = sandboxed.task;
            if (sandboxedTask.input.files.length === 1) {
                const editResult = await this.executeTool('file.edit', {
                    file: sandboxedTask.input.files[0],
                    instruction: sandboxedTask.input.instruction,
                }, sandboxedTask, activeExecutor, options);
                if (!editResult.ok) {
                    return normalizeExecutionResult(task, activeExecutor, startTime, diffs, editResult.error?.message);
                }
                const result = editResult.output;
                if (result?.success) {
                    diffs.push(result.diff);
                }
                else if (result?.message) {
                    return normalizeExecutionResult(task, activeExecutor, startTime, diffs, result.message);
                }
            }
            else if (sandboxedTask.input.files.length > 1) {
                const batchResult = await this.executeTool('file.edit_multiple', {
                    files: sandboxedTask.input.files,
                    instruction: sandboxedTask.input.instruction,
                }, sandboxedTask, activeExecutor, options);
                if (!batchResult.ok) {
                    return normalizeExecutionResult(task, activeExecutor, startTime, diffs, batchResult.error?.message);
                }
                const results = batchResult.output ?? [];
                const failed = results.find((result) => !result.success);
                for (const result of results) {
                    if (result.success) {
                        diffs.push(result.diff);
                    }
                }
                if (failed?.message) {
                    return normalizeExecutionResult(task, activeExecutor, startTime, diffs, failed.message);
                }
            }
            return normalizeExecutionResult(task, activeExecutor, startTime, diffs);
        }
        catch (error) {
            const message = error instanceof Error ? error.message : 'Unknown error';
            return normalizeExecutionResult(task, activeExecutor, startTime, [], message);
        }
        finally {
            await sandboxed.release?.();
        }
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
}
export { InMemoryModelPool, DefaultContextAssembler, AllowAllPolicyGuard, PassthroughSandbox, };
//# sourceMappingURL=runtime.js.map