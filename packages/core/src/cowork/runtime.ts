/**
 * Cowork Runtime
 * 收敛执行器注册、上下文装配、策略校验与沙箱准备
 */

import type { IAuditManager } from '../audit/types.js';
import { HeadlessToolRuntime } from '../tool-runtime/HeadlessToolRuntime.js';
import type { FileOperationService } from '../tool-runtime/FileOperationService.js';
import type { ToolExecutor } from '../tool-runtime/ToolExecutor.js';
import type { ToolRegistry } from '../tool-runtime/ToolRegistry.js';
import type {
  ToolCallTrace,
  ToolContext,
  ToolExecutionResult,
} from '../tool-runtime/types.js';
import {
  AgentRuntimeLike,
  ContextAssembler,
  CoworkTask,
  ExecutionResult,
  ExecutorCapabilities,
  ExecutorRegistration,
  ICodeEditor,
  ModelPool,
  PolicyDecision,
  PolicyGuard,
  RuntimeExecutionOptions,
  SandboxedTask,
  ExecutionSandbox,
  Diff,
  EditResult,
} from './types.js';

class InMemoryModelPool implements ModelPool {
  private models = new Map<string, string | undefined>();

  registerExecutor(executor: ExecutorRegistration): void {
    this.models.set(executor.name, executor.modelId);
  }

  getModelId(executorName: string): string | undefined {
    return this.models.get(executorName);
  }
}

class DefaultContextAssembler implements ContextAssembler {
  buildContextFromResult(result: ExecutionResult): string {
    if (!result.output) return '';

    const parts: string[] = [];

    if (result.output.result) {
      parts.push(`Previous output:\n${result.output.result}`);
    }

    if (result.output.diffs && result.output.diffs.length > 0) {
      parts.push(
        `Previous changes:\n${result.output.diffs.map((d) => `- ${d.file}: +${d.additions}/-${d.deletions}`).join('\n')}`
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

function normalizeExecutionResult(
  task: CoworkTask,
  executor: ExecutorRegistration,
  startTime: number,
  diffs: Diff[],
  error?: string
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
  private executors = new Map<string, ExecutorRegistration>();
  private modelPool: ModelPool;
  private contextAssembler: ContextAssembler;
  private policyGuard: PolicyGuard;
  private sandbox: ExecutionSandbox;
  private headlessToolRuntime: HeadlessToolRuntime;
  private toolRegistry: ToolRegistry;
  private toolExecutor: ToolExecutor;
  private fileOperationService: FileOperationService;

  constructor(deps: AgentRuntimeDeps = {}) {
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

  registerExecutor(
    name: string,
    editor: ICodeEditor,
    capabilities: ExecutorCapabilities,
    modelId?: string
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

  getExecutor(name: string): ExecutorRegistration | undefined {
    const executor = this.executors.get(name);
    if (!executor) return undefined;

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
        `Executor '${task.executor}' not found`
      );
    }

    const policy = await this.policyGuard.canExecute(task, executor);
    if (!policy.allowed) {
      return normalizeExecutionResult(task, executor, startTime, [], policy.reason || 'Execution denied');
    }

    const sandboxed = await this.sandbox.prepare(task, executor, options);
    const activeExecutor = options.executorOverride ?? sandboxed.executor ?? executor;

    try {
      const diffs: Diff[] = [];
      const sandboxedTask = sandboxed.task;

      if (sandboxedTask.input.files.length === 1) {
        const editResult = await this.executeTool<EditResult>(
          'file.edit',
          {
            file: sandboxedTask.input.files[0],
            instruction: sandboxedTask.input.instruction,
          },
          sandboxedTask,
          activeExecutor,
          options
        );
        if (!editResult.ok) {
          return normalizeExecutionResult(
            task,
            activeExecutor,
            startTime,
            diffs,
            editResult.error?.message
          );
        }
        const result = editResult.output;
        if (result?.success) {
          diffs.push(result.diff);
        } else if (result?.message) {
          return normalizeExecutionResult(task, activeExecutor, startTime, diffs, result.message);
        }
      } else if (sandboxedTask.input.files.length > 1) {
        const batchResult = await this.executeTool<EditResult[]>(
          'file.edit_multiple',
          {
            files: sandboxedTask.input.files,
            instruction: sandboxedTask.input.instruction,
          },
          sandboxedTask,
          activeExecutor,
          options
        );
        if (!batchResult.ok) {
          return normalizeExecutionResult(
            task,
            activeExecutor,
            startTime,
            diffs,
            batchResult.error?.message
          );
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
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Unknown error';
      return normalizeExecutionResult(task, activeExecutor, startTime, [], message);
    } finally {
      await sandboxed.release?.();
    }
  }


  private async executeTool<TOutput>(
    toolId: string,
    input: unknown,
    task: CoworkTask,
    executor: ExecutorRegistration,
    options: RuntimeExecutionOptions
  ): Promise<ToolExecutionResult<TOutput>> {
    return this.toolExecutor.execute<TOutput>({
      toolId,
      input,
      context: this.createToolContext(task, executor, options),
    });
  }

  private createToolContext(
    task: CoworkTask,
    executor: ExecutorRegistration,
    options: RuntimeExecutionOptions
  ): ToolContext {
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

export {
  InMemoryModelPool,
  DefaultContextAssembler,
  AllowAllPolicyGuard,
  PassthroughSandbox,
};
