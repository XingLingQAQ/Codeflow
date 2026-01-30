/**
 * Cowork Mode - 多 CLI 协作类型定义
 */

/**
 * CLI 执行选项
 */
export interface ExecuteOptions {
  cwd?: string;
  env?: Record<string, string>;
  timeout?: number;
  signal?: AbortSignal;
}

/**
 * CLI 执行结果
 */
export interface CLIResult {
  stdout: string;
  stderr: string;
  exitCode: number;
  duration: number;
}

/**
 * CLI 能力描述
 */
export interface CLICapabilities {
  supportsStreaming: boolean;
  supportsInterrupt: boolean;
  supportedLanguages: string[];
  maxContextTokens: number;
  features: string[];
}

/**
 * CLI 适配器接口
 */
export interface ICLIAdapter {
  readonly name: string;
  readonly version: string;
  execute(command: string, options?: ExecuteOptions): Promise<CLIResult>;
  stream(command: string, onChunk: (data: string) => void, options?: ExecuteOptions): Promise<void>;
  interrupt(): Promise<void>;
  healthCheck(): Promise<boolean>;
  getCapabilities(): CLICapabilities;
}

/**
 * Diff 结构
 */
export interface Diff {
  file: string;
  hunks: DiffHunk[];
  additions: number;
  deletions: number;
}

/**
 * Diff Hunk
 */
export interface DiffHunk {
  oldStart: number;
  oldLines: number;
  newStart: number;
  newLines: number;
  content: string;
}

/**
 * 编辑结果
 */
export interface EditResult {
  success: boolean;
  file: string;
  diff: Diff;
  message?: string;
}

/**
 * 代码编辑器接口
 */
export interface ICodeEditor {
  readonly name: string;
  edit(file: string, instruction: string): Promise<EditResult>;
  editMultiple(files: string[], instruction: string): Promise<EditResult[]>;
  preview(file: string, instruction: string): Promise<Diff>;
  applyDiff(file: string, diff: Diff): Promise<void>;
  undo(): Promise<void>;
}

/**
 * 任务类型
 */
export type CoworkTaskType = 'code-edit' | 'test-gen' | 'refactor' | 'review' | 'debug' | 'explain';

/**
 * 任务状态
 */
export type CoworkTaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';

/**
 * 任务输入
 */
export interface CoworkTaskInput {
  files: string[];
  instruction: string;
  context?: string;
  language?: string;
}

/**
 * 任务配置
 */
export interface CoworkTaskConfig {
  retry?: number;
  timeout?: number;
  priority?: number;
  allowParallel?: boolean;
}

/**
 * 任务输出
 */
export interface CoworkTaskOutput {
  result?: string;
  diffs?: Diff[];
  error?: string;
  metrics?: {
    duration: number;
    tokensUsed?: number;
  };
}

/**
 * Cowork 任务
 */
export interface CoworkTask {
  id: string;
  type: CoworkTaskType;
  executor: string;
  input: CoworkTaskInput;
  config?: CoworkTaskConfig;
  status: CoworkTaskStatus;
  output?: CoworkTaskOutput;
  createdAt: number;
  startedAt?: number;
  completedAt?: number;
}

/**
 * 执行器能力
 */
export interface ExecutorCapabilities {
  name: string;
  supportedTypes: CoworkTaskType[];
  maxConcurrency: number;
  estimatedSpeed: 'fast' | 'medium' | 'slow';
  costPerToken?: number;
  features: {
    streaming: boolean;
    multiFile: boolean;
    contextAware: boolean;
    codeReview: boolean;
  };
}

/**
 * 协作模式
 */
export type CoworkMode = 'parallel' | 'sequential' | 'debate';

/**
 * 并行执行选项
 */
export interface ParallelOptions {
  maxConcurrency?: number;
  failFast?: boolean;
  conflictStrategy?: 'fail' | 'merge' | 'last-wins';
}

/**
 * 顺序执行选项
 */
export interface SequentialOptions {
  stopOnError?: boolean;
  passContext?: boolean;
}

/**
 * 辩论执行选项
 */
export interface DebateOptions {
  maxRounds?: number;
  convergenceThreshold?: number;
  generator: string;
  critic: string;
}

/**
 * 执行结果
 */
export interface ExecutionResult {
  taskId: string;
  status: CoworkTaskStatus;
  output?: CoworkTaskOutput;
  executor: string;
  duration: number;
}

/**
 * 批量执行结果
 */
export interface BatchExecutionResult {
  mode: CoworkMode;
  results: ExecutionResult[];
  totalDuration: number;
  successCount: number;
  failureCount: number;
  conflicts?: ConflictInfo[];
}

/**
 * 冲突信息
 */
export interface ConflictInfo {
  file: string;
  executors: string[];
  type: 'content' | 'delete' | 'rename';
  resolution?: 'merged' | 'manual' | 'skipped';
}

/**
 * 辩论轮次
 */
export interface DebateRound {
  round: number;
  generator: {
    executor: string;
    output: string;
    diffs?: Diff[];
  };
  critic: {
    executor: string;
    feedback: string;
    issues: DebateIssue[];
  };
  refined?: {
    output: string;
    diffs?: Diff[];
  };
}

/**
 * 辩论问题
 */
export interface DebateIssue {
  type: 'bug' | 'style' | 'performance' | 'security' | 'logic';
  severity: 'low' | 'medium' | 'high' | 'critical';
  description: string;
  location?: {
    file: string;
    line?: number;
  };
  suggestion?: string;
}

/**
 * 辩论结果
 */
export interface DebateResult extends BatchExecutionResult {
  rounds: DebateRound[];
  converged: boolean;
  finalOutput?: string;
  finalDiffs?: Diff[];
}

/**
 * Orchestrator 事件
 */
export type OrchestratorEvent =
  | { type: 'task:start'; task: CoworkTask }
  | { type: 'task:progress'; taskId: string; progress: number; message?: string }
  | { type: 'task:complete'; taskId: string; result: ExecutionResult }
  | { type: 'task:error'; taskId: string; error: string }
  | { type: 'conflict:detected'; conflict: ConflictInfo }
  | { type: 'debate:round'; round: DebateRound };

/**
 * 事件监听器
 */
export type OrchestratorEventListener = (event: OrchestratorEvent) => void;
