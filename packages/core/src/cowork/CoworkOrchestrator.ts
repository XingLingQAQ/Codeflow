/**
 * Cowork Orchestrator
 * 多 CLI 协作编排器 - 支持并行、顺序、辩论三种协作模式
 */

import { EventEmitter } from 'events';
import {
  AgentRuntimeLike,
  CoworkTask,
  CoworkTaskStatus,
  ICodeEditor,
  ExecutorCapabilities,
  CoworkMode,
  ParallelOptions,
  SequentialOptions,
  DebateOptions,
  ExecutionResult,
  BatchExecutionResult,
  ConflictInfo,
  DebateRound,
  DebateResult,
  DebateIssue,
  OrchestratorEvent,
  OrchestratorEventListener,
  Diff,
  ExecutorRegistration,
} from './types.js';
import { CLIProcessManager } from './process/CLIProcessManager.js';
import { GitConflictDetector } from './GitConflictDetector.js';
import { AgentRuntime } from './runtime.js';

/**
 * 执行器注册信息
 */

/**
 * Blackboard 条目
 */
interface BlackboardEntry {
  key: string;
  value: unknown;
  source: string;
  timestamp: number;
}

/**
 * Cowork Orchestrator
 */
export class CoworkOrchestrator extends EventEmitter {
  private runtime: AgentRuntimeLike;
  private processManager: CLIProcessManager;
  private blackboard: Map<string, BlackboardEntry> = new Map();
  private runningTasks: Map<string, CoworkTask> = new Map();
  private gitConflictDetector: GitConflictDetector;

  constructor(processManager?: CLIProcessManager, cwd?: string, runtime?: AgentRuntimeLike) {
    super();
    this.processManager = processManager || new CLIProcessManager();
    this.gitConflictDetector = new GitConflictDetector({ cwd });
    this.runtime = runtime || new AgentRuntime();
  }

  /**
   * 注册执行器
   */
  registerExecutor(
    name: string,
    editor: ICodeEditor,
    capabilities: ExecutorCapabilities,
    modelId?: string
  ): void {
    this.runtime.registerExecutor(name, editor, capabilities, modelId);
    this.emitEvent({ type: 'task:start', task: { id: `register_${name}` } as CoworkTask });
  }

  /**
   * 获取执行器
   */
  getExecutor(name: string): ExecutorRegistration | undefined {
    return this.runtime.getExecutor(name);
  }

  /**
   * 获取所有执行器
   */
  getAllExecutors(): ExecutorRegistration[] {
    return this.runtime.getAllExecutors();
  }

  /**
   * 执行单个任务
   */
  async execute(task: CoworkTask): Promise<ExecutionResult> {
    const startTime = Date.now();

    task.status = 'running';
    task.startedAt = startTime;
    this.runningTasks.set(task.id, task);

    this.emitEvent({ type: 'task:start', task });

    const result = await this.runtime.executeTask(task);

    task.status = result.status;
    task.completedAt = Date.now();
    task.output = result.output;

    if (result.status === 'completed') {
      this.emitEvent({ type: 'task:complete', taskId: task.id, result });
    } else {
      this.emitEvent({ type: 'task:error', taskId: task.id, error: result.output?.error || 'Unknown error' });
    }

    this.runningTasks.delete(task.id);
    return result;
  }

  /**
   * 并行执行多个任务
   */
  async executeParallel(
    tasks: CoworkTask[],
    options: ParallelOptions = {}
  ): Promise<BatchExecutionResult> {
    const startTime = Date.now();
    const { maxConcurrency = 5, failFast = false, conflictStrategy = 'fail' } = options;

    const results: ExecutionResult[] = [];
    const conflicts: ConflictInfo[] = [];

    // 检测文件冲突
    const fileToTasks = new Map<string, CoworkTask[]>();
    for (const task of tasks) {
      for (const file of task.input.files) {
        const existing = fileToTasks.get(file) || [];
        existing.push(task);
        fileToTasks.set(file, existing);
      }
    }

    // 报告冲突
    for (const [file, fileTasks] of fileToTasks) {
      if (fileTasks.length > 1) {
        const conflict: ConflictInfo = {
          file,
          executors: fileTasks.map((t) => t.executor),
          type: 'content',
        };
        conflicts.push(conflict);
        this.emitEvent({ type: 'conflict:detected', conflict });

        if (conflictStrategy === 'fail') {
          return {
            mode: 'parallel',
            results: [],
            totalDuration: Date.now() - startTime,
            successCount: 0,
            failureCount: tasks.length,
            conflicts,
          };
        }
      }
    }

    // 分批执行
    const batches: CoworkTask[][] = [];
    for (let i = 0; i < tasks.length; i += maxConcurrency) {
      batches.push(tasks.slice(i, i + maxConcurrency));
    }

    for (const batch of batches) {
      const batchPromises = batch.map((task) => this.execute(task));

      if (failFast) {
        // 任一失败立即停止
        const batchResults = await Promise.all(
          batchPromises.map((p) =>
            p.catch((e) => ({
              taskId: 'unknown',
              status: 'failed' as CoworkTaskStatus,
              output: { error: e.message },
              executor: 'unknown',
              duration: 0,
            }))
          )
        );

        results.push(...batchResults);

        if (batchResults.some((r) => r.status === 'failed')) {
          break;
        }
      } else {
        const batchResults = await Promise.allSettled(batchPromises);
        for (const result of batchResults) {
          if (result.status === 'fulfilled') {
            results.push(result.value);
          } else {
            results.push({
              taskId: 'unknown',
              status: 'failed',
              output: { error: result.reason?.message || 'Unknown error' },
              executor: 'unknown',
              duration: 0,
            });
          }
        }
      }
    }

    // 执行后检测 diff 冲突
    const allDiffs: Diff[] = [];
    for (const result of results) {
      if (result.status === 'completed' && result.output?.diffs) {
        allDiffs.push(...result.output.diffs);
      }
    }

    // 检测 diff 之间的冲突
    const diffConflicts = this.gitConflictDetector.detectMultipleDiffConflicts(allDiffs);
    for (const conflict of diffConflicts) {
      if (!conflicts.some((c) => c.file === conflict.file)) {
        conflicts.push(conflict);
        this.emitEvent({ type: 'conflict:detected', conflict });
      }
    }

    return {
      mode: 'parallel',
      results,
      totalDuration: Date.now() - startTime,
      successCount: results.filter((r) => r.status === 'completed').length,
      failureCount: results.filter((r) => r.status === 'failed').length,
      conflicts,
    };
  }

  /**
   * 顺序执行任务链
   */
  async executeSequence(
    tasks: CoworkTask[],
    options: SequentialOptions = {}
  ): Promise<BatchExecutionResult> {
    const startTime = Date.now();
    const { stopOnError = true, passContext = true, signal, onBeforeTask, onAfterTask } = options;

    const results: ExecutionResult[] = [];
    let interrupted = false;

    for (let i = 0; i < tasks.length; i++) {
      const task = tasks[i];

      // 检查中断信号
      if (signal?.aborted) {
        interrupted = true;
        break;
      }

      // 执行前回调
      if (onBeforeTask) {
        const shouldContinue = await onBeforeTask(task, i);
        if (!shouldContinue) {
          interrupted = true;
          break;
        }
      }

      // 传递上下文
      if (passContext && i > 0) {
        const prevResult = results[i - 1];
        if (prevResult.status === 'completed' && prevResult.output) {
          const nextTask = this.runtime.attachPreviousResult(task, prevResult);
          task.input.context = nextTask.input.context;

          this.setBlackboardEntry(
            `task_${tasks[i - 1].id}_output`,
            prevResult.output,
            tasks[i - 1].executor
          );
        }
      }

      // 报告进度
      this.emitEvent({
        type: 'task:progress',
        taskId: task.id,
        progress: (i / tasks.length) * 100,
        message: `Executing task ${i + 1}/${tasks.length}`,
      });

      const result = await this.execute(task);
      results.push(result);

      // 执行后回调
      if (onAfterTask) {
        await onAfterTask(task, result, i);
      }

      // 再次检查中断信号
      if (signal?.aborted) {
        interrupted = true;
        break;
      }

      if (stopOnError && result.status === 'failed') {
        break;
      }
    }

    return {
      mode: 'sequential',
      results,
      totalDuration: Date.now() - startTime,
      successCount: results.filter((r) => r.status === 'completed').length,
      failureCount: results.filter((r) => r.status === 'failed').length,
      interrupted,
    };
  }

  /**
   * 辩论模式执行
   */
  async executeDebate(
    task: CoworkTask,
    options: DebateOptions
  ): Promise<DebateResult> {
    const startTime = Date.now();
    const {
      maxRounds = 3,
      convergenceThreshold = 0.8,
      generator,
      critic,
      signal,
      onRound,
      checkConvergence,
      minAgreementScore = 0.8,
    } = options;

    const rounds: DebateRound[] = [];
    let converged = false;
    let finalOutput: string | undefined;
    let finalDiffs: Diff[] | undefined;
    let interrupted = false;

    // 获取执行器
    const generatorExecutor = this.runtime.getExecutor(generator);
    const criticExecutor = this.runtime.getExecutor(critic);

    if (!generatorExecutor || !criticExecutor) {
      return {
        mode: 'debate',
        results: [],
        totalDuration: Date.now() - startTime,
        successCount: 0,
        failureCount: 1,
        rounds: [],
        converged: false,
      };
    }

    let currentInstruction = task.input.instruction;

    for (let round = 1; round <= maxRounds && !converged && !interrupted; round++) {
      // 检查中断信号
      if (signal?.aborted) {
        interrupted = true;
        break;
      }

      // Generator 生成
      const generatorTask: CoworkTask = {
        ...task,
        id: `${task.id}_gen_${round}`,
        executor: generator,
        input: {
          ...task.input,
          instruction: currentInstruction,
        },
      };

      const genResult = await this.execute(generatorTask);
      const genOutput = genResult.output?.result || '';
      const genDiffs = genResult.output?.diffs || [];

      // 检查中断信号
      if (signal?.aborted) {
        interrupted = true;
        break;
      }

      // Critic 评审
      const criticTask: CoworkTask = {
        ...task,
        id: `${task.id}_critic_${round}`,
        executor: critic,
        type: 'review',
        input: {
          ...task.input,
          instruction: `Review the following code changes and identify issues:\n\n${genOutput}`,
        },
      };

      const criticResult = await this.execute(criticTask);
      const criticOutput = criticResult.output?.result || '';
      const issues = this.parseIssues(criticOutput);

      const debateRound: DebateRound = {
        round,
        generator: {
          executor: generator,
          output: genOutput,
          diffs: genDiffs,
        },
        critic: {
          executor: critic,
          feedback: criticOutput,
          issues,
        },
      };

      // 检查收敛（使用自定义检查器或默认逻辑）
      if (checkConvergence) {
        converged = checkConvergence(debateRound, rounds);
      } else {
        // 默认收敛逻辑：无严重问题
        const criticalIssues = issues.filter(
          (i) => i.severity === 'critical' || i.severity === 'high'
        );

        // 计算一致性分数
        const totalIssues = issues.length;
        const agreementScore = totalIssues === 0 ? 1 : 1 - criticalIssues.length / totalIssues;

        if (criticalIssues.length === 0 || agreementScore >= minAgreementScore) {
          converged = true;
        }
      }

      if (converged) {
        finalOutput = genOutput;
        finalDiffs = genDiffs;
        debateRound.refined = { output: genOutput, diffs: genDiffs };
      } else {
        // 根据反馈调整指令
        currentInstruction = this.refineInstruction(task.input.instruction, issues);
      }

      rounds.push(debateRound);
      this.emitEvent({ type: 'debate:round', round: debateRound });

      // 执行回调
      if (onRound) {
        await onRound(debateRound);
      }
    }

    return {
      mode: 'debate',
      results: [],
      totalDuration: Date.now() - startTime,
      successCount: converged ? 1 : 0,
      failureCount: converged ? 0 : 1,
      rounds,
      converged,
      finalOutput,
      finalDiffs,
      interrupted,
    };
  }

  /**
   * Blackboard 操作
   */
  setBlackboardEntry(key: string, value: unknown, source: string): void {
    this.blackboard.set(key, {
      key,
      value,
      source,
      timestamp: Date.now(),
    });
  }

  getBlackboardEntry(key: string): BlackboardEntry | undefined {
    return this.blackboard.get(key);
  }

  getAllBlackboardEntries(): BlackboardEntry[] {
    return Array.from(this.blackboard.values());
  }

  clearBlackboard(): void {
    this.blackboard.clear();
  }

  /**
   * 获取运行中的任务
   */
  getRunningTasks(): CoworkTask[] {
    return Array.from(this.runningTasks.values());
  }

  /**
   * 取消任务
   */
  async cancelTask(taskId: string): Promise<boolean> {
    const task = this.runningTasks.get(taskId);
    if (!task) {
      return false;
    }

    task.status = 'cancelled';
    task.completedAt = Date.now();
    this.runningTasks.delete(taskId);

    return true;
  }

  /**
   * 获取进程管理器
   */
  getProcessManager(): CLIProcessManager {
    return this.processManager;
  }

  /**
   * 获取 Git 冲突检测器
   */
  getGitConflictDetector(): GitConflictDetector {
    return this.gitConflictDetector;
  }

  /**
   * 清理资源
   */
  async cleanup(): Promise<void> {
    // 取消所有运行中的任务
    for (const taskId of this.runningTasks.keys()) {
      await this.cancelTask(taskId);
    }

    // 清理进程
    await this.processManager.cleanup();

    // 清理 Blackboard
    this.clearBlackboard();
  }

  // ==================== 私有方法 ====================

  private emitEvent(event: OrchestratorEvent): void {
    this.emit('event', event);
  }

  private parseIssues(criticOutput: string): DebateIssue[] {
    const issues: DebateIssue[] = [];
    const lines = criticOutput.split('\n');

    // 正则模式匹配常见的问题格式
    const issuePatterns = [
      // 匹配 "- [severity] description" 或 "* [severity] description" 格式
      /^[\-\*]\s*\[?(critical|high|medium|low)\]?\s*[:\-]?\s*(.+)/i,
      // 匹配 "Issue: description" 或 "Bug: description" 格式
      /^(bug|error|issue|security|vulnerability|performance|style|warning)\s*[:\-]\s*(.+)/i,
      // 匹配 "Line X: description" 格式
      /^line\s*(\d+)\s*[:\-]\s*(.+)/i,
    ];

    for (const line of lines) {
      const trimmedLine = line.trim();
      if (!trimmedLine || trimmedLine.length < 5) continue;

      const lowerLine = trimmedLine.toLowerCase();
      let issue: DebateIssue | null = null;

      // 尝试匹配结构化格式
      for (const pattern of issuePatterns) {
        const match = trimmedLine.match(pattern);
        if (match) {
          const [, typeOrSeverity, description] = match;
          const lower = typeOrSeverity.toLowerCase();

          // 判断是严重性还是类型
          if (['critical', 'high', 'medium', 'low'].includes(lower)) {
            issue = {
              type: this.inferIssueType(description),
              severity: lower as DebateIssue['severity'],
              description: description.trim(),
            };
          } else {
            issue = {
              type: this.mapToIssueType(lower),
              severity: this.inferSeverity(lower, description),
              description: description.trim(),
            };
          }
          break;
        }
      }

      // 如果没有匹配结构化格式，使用关键词检测
      if (!issue) {
        if (lowerLine.includes('critical') || lowerLine.includes('severe')) {
          issue = {
            type: this.inferIssueType(trimmedLine),
            severity: 'critical',
            description: trimmedLine,
          };
        } else if (lowerLine.includes('bug') || lowerLine.includes('error') || lowerLine.includes('crash')) {
          issue = {
            type: 'bug',
            severity: lowerLine.includes('critical') ? 'critical' : 'medium',
            description: trimmedLine,
          };
        } else if (lowerLine.includes('security') || lowerLine.includes('vulnerability') || lowerLine.includes('injection') || lowerLine.includes('xss')) {
          issue = {
            type: 'security',
            severity: 'high',
            description: trimmedLine,
          };
        } else if (lowerLine.includes('performance') || lowerLine.includes('slow') || lowerLine.includes('memory leak') || lowerLine.includes('inefficient')) {
          issue = {
            type: 'performance',
            severity: 'medium',
            description: trimmedLine,
          };
        } else if (lowerLine.includes('style') || lowerLine.includes('format') || lowerLine.includes('naming') || lowerLine.includes('convention')) {
          issue = {
            type: 'style',
            severity: 'low',
            description: trimmedLine,
          };
        } else if (lowerLine.includes('logic') || lowerLine.includes('incorrect') || lowerLine.includes('wrong')) {
          issue = {
            type: 'logic',
            severity: 'medium',
            description: trimmedLine,
          };
        }
      }

      if (issue) {
        // 尝试提取位置信息
        const locationMatch = trimmedLine.match(/(?:line|L)?\s*(\d+)(?:\s*[:\-,]\s*(?:col(?:umn)?|C)?\s*(\d+))?/i);
        if (locationMatch) {
          issue.location = {
            file: '',
            line: parseInt(locationMatch[1], 10),
          };
        }

        // 提取建议（如果有）
        const suggestionMatch = trimmedLine.match(/(?:suggest(?:ion)?|fix|solution|recommend)\s*[:\-]\s*(.+)/i);
        if (suggestionMatch) {
          issue.suggestion = suggestionMatch[1].trim();
        }

        issues.push(issue);
      }
    }

    return issues;
  }

  /**
   * 根据描述推断问题类型
   */
  private inferIssueType(description: string): DebateIssue['type'] {
    const lower = description.toLowerCase();
    if (lower.includes('security') || lower.includes('vulnerability')) return 'security';
    if (lower.includes('performance') || lower.includes('slow')) return 'performance';
    if (lower.includes('style') || lower.includes('format')) return 'style';
    if (lower.includes('logic') || lower.includes('incorrect')) return 'logic';
    return 'bug';
  }

  /**
   * 将关键词映射到问题类型
   */
  private mapToIssueType(keyword: string): DebateIssue['type'] {
    const mapping: Record<string, DebateIssue['type']> = {
      bug: 'bug',
      error: 'bug',
      issue: 'bug',
      security: 'security',
      vulnerability: 'security',
      performance: 'performance',
      style: 'style',
      warning: 'bug',
    };
    return mapping[keyword] || 'bug';
  }

  /**
   * 根据类型和描述推断严重性
   */
  private inferSeverity(type: string, description: string): DebateIssue['severity'] {
    const lower = description.toLowerCase();

    // 安全问题默认高严重性
    if (type === 'security' || type === 'vulnerability') return 'high';

    // 检查描述中的严重性关键词
    if (lower.includes('critical') || lower.includes('severe') || lower.includes('crash')) return 'critical';
    if (lower.includes('high') || lower.includes('important') || lower.includes('major')) return 'high';
    if (lower.includes('low') || lower.includes('minor') || lower.includes('trivial')) return 'low';

    // 样式问题默认低严重性
    if (type === 'style') return 'low';

    return 'medium';
  }

  private refineInstruction(original: string, issues: DebateIssue[]): string {
    const issueDescriptions = issues
      .filter((i) => i.severity === 'critical' || i.severity === 'high')
      .map((i) => `- ${i.description}`)
      .join('\n');

    return `${original}\n\nPlease address the following issues:\n${issueDescriptions}`;
  }
}
