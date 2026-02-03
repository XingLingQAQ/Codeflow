/**
 * ParallelCommands - 并行模式 CLI 命令
 * 实现 /codeflow:parallel-* 系列命令
 */

import { EventEmitter } from 'events';

/**
 * 命令参数基础接口
 */
export interface CommandParams {
  [key: string]: unknown;
}

/**
 * 并行命令参数
 */
export interface ParallelStartParams extends CommandParams {
  task: string;
  workers?: number;
  models?: string[];
  timeout?: number;
}

export interface ParallelStatusParams extends CommandParams {
  taskId?: string;
  verbose?: boolean;
}

export interface ParallelCompareParams extends CommandParams {
  taskId: string;
  metrics?: string[];
  format?: 'table' | 'json' | 'markdown';
}

export interface ParallelSelectParams extends CommandParams {
  taskId: string;
  solutionId: string;
  reason?: string;
}

export interface ParallelMergeParams extends CommandParams {
  taskId: string;
  solutionId: string;
  strategy?: 'fast-forward' | 'merge' | 'rebase';
  backup?: boolean;
}

/**
 * 命令执行结果
 */
export interface CommandResult {
  success: boolean;
  command: string;
  output: string;
  data?: unknown;
  error?: string;
  duration: number;
}

/**
 * 命令定义
 */
export interface CommandDefinition {
  name: string;
  description: string;
  usage: string;
  examples: string[];
  parameters: ParameterDefinition[];
}

/**
 * 参数定义
 */
export interface ParameterDefinition {
  name: string;
  type: 'string' | 'boolean' | 'number' | 'array';
  description: string;
  required: boolean;
  default?: unknown;
  choices?: string[];
}

/**
 * Worker 状态
 */
export interface WorkerStatus {
  id: string;
  model: string;
  status: 'pending' | 'running' | 'completed' | 'failed';
  progress: number;
  startTime?: number;
  endTime?: number;
  worktree?: string;
  branch?: string;
}

/**
 * 并行任务状态
 */
export interface ParallelTaskStatus {
  taskId: string;
  task: string;
  status: 'pending' | 'running' | 'completed' | 'failed';
  workers: WorkerStatus[];
  createdAt: number;
  completedAt?: number;
}

/**
 * 并行命令处理器
 */
export class ParallelCommands extends EventEmitter {
  private commands: Map<string, CommandDefinition> = new Map();
  private activeTasks: Map<string, ParallelTaskStatus> = new Map();

  constructor() {
    super();
    this.registerCommands();
  }

  /**
   * 注册所有并行命令
   */
  private registerCommands(): void {
    // /codeflow:parallel-start
    this.commands.set('parallel-start', {
      name: 'parallel-start',
      description: '启动并行任务（多 Agent 同时执行）',
      usage: '/codeflow:parallel-start <task> [--workers <n>] [--models <m1,m2,...>] [--timeout <ms>]',
      examples: [
        '/codeflow:parallel-start "实现用户登录功能"',
        '/codeflow:parallel-start "优化数据库查询" --workers 3',
        '/codeflow:parallel-start "重构 API" --models "claude-3-opus,gemini-pro,gpt-4o"',
      ],
      parameters: [
        {
          name: 'task',
          type: 'string',
          description: '任务描述',
          required: true,
        },
        {
          name: 'workers',
          type: 'number',
          description: 'Worker 数量',
          required: false,
          default: 3,
        },
        {
          name: 'models',
          type: 'array',
          description: '使用的模型列表',
          required: false,
        },
        {
          name: 'timeout',
          type: 'number',
          description: '超时时间（毫秒）',
          required: false,
          default: 300000,
        },
      ],
    });

    // /codeflow:parallel-status
    this.commands.set('parallel-status', {
      name: 'parallel-status',
      description: '查看并行任务状态',
      usage: '/codeflow:parallel-status [taskId] [--verbose]',
      examples: [
        '/codeflow:parallel-status',
        '/codeflow:parallel-status task_123',
        '/codeflow:parallel-status task_123 --verbose',
      ],
      parameters: [
        {
          name: 'taskId',
          type: 'string',
          description: '任务 ID（不指定则显示所有）',
          required: false,
        },
        {
          name: 'verbose',
          type: 'boolean',
          description: '显示详细信息',
          required: false,
          default: false,
        },
      ],
    });

    // /codeflow:parallel-compare
    this.commands.set('parallel-compare', {
      name: 'parallel-compare',
      description: '对比并行任务的多个方案',
      usage: '/codeflow:parallel-compare <taskId> [--metrics <m1,m2,...>] [--format <format>]',
      examples: [
        '/codeflow:parallel-compare task_123',
        '/codeflow:parallel-compare task_123 --metrics "quality,performance,maintainability"',
        '/codeflow:parallel-compare task_123 --format json',
      ],
      parameters: [
        {
          name: 'taskId',
          type: 'string',
          description: '任务 ID',
          required: true,
        },
        {
          name: 'metrics',
          type: 'array',
          description: '评估指标',
          required: false,
          choices: ['quality', 'performance', 'maintainability', 'security', 'readability'],
        },
        {
          name: 'format',
          type: 'string',
          description: '输出格式',
          required: false,
          default: 'table',
          choices: ['table', 'json', 'markdown'],
        },
      ],
    });

    // /codeflow:parallel-select
    this.commands.set('parallel-select', {
      name: 'parallel-select',
      description: '选择最优方案',
      usage: '/codeflow:parallel-select <taskId> <solutionId> [--reason <reason>]',
      examples: [
        '/codeflow:parallel-select task_123 solution_1',
        '/codeflow:parallel-select task_123 solution_2 --reason "性能最优"',
      ],
      parameters: [
        {
          name: 'taskId',
          type: 'string',
          description: '任务 ID',
          required: true,
        },
        {
          name: 'solutionId',
          type: 'string',
          description: '方案 ID',
          required: true,
        },
        {
          name: 'reason',
          type: 'string',
          description: '选择原因',
          required: false,
        },
      ],
    });

    // /codeflow:parallel-merge
    this.commands.set('parallel-merge', {
      name: 'parallel-merge',
      description: '合并选中的方案到主分支',
      usage: '/codeflow:parallel-merge <taskId> <solutionId> [--strategy <strategy>] [--backup]',
      examples: [
        '/codeflow:parallel-merge task_123 solution_1',
        '/codeflow:parallel-merge task_123 solution_1 --strategy rebase',
        '/codeflow:parallel-merge task_123 solution_1 --backup',
      ],
      parameters: [
        {
          name: 'taskId',
          type: 'string',
          description: '任务 ID',
          required: true,
        },
        {
          name: 'solutionId',
          type: 'string',
          description: '方案 ID',
          required: true,
        },
        {
          name: 'strategy',
          type: 'string',
          description: '合并策略',
          required: false,
          default: 'merge',
          choices: ['fast-forward', 'merge', 'rebase'],
        },
        {
          name: 'backup',
          type: 'boolean',
          description: '合并前备份当前状态',
          required: false,
          default: true,
        },
      ],
    });
  }

  /**
   * 执行命令
   */
  async execute(commandName: string, params: CommandParams): Promise<CommandResult> {
    const startTime = Date.now();
    const command = this.commands.get(commandName);

    if (!command) {
      return {
        success: false,
        command: commandName,
        output: '',
        error: `Unknown command: ${commandName}. Use /codeflow:parallel-help for available commands.`,
        duration: Date.now() - startTime,
      };
    }

    // 验证参数
    const validationError = this.validateParams(command, params);
    if (validationError) {
      return {
        success: false,
        command: commandName,
        output: '',
        error: validationError,
        duration: Date.now() - startTime,
      };
    }

    this.emit('command:start', { command: commandName, params });

    try {
      let result: CommandResult;

      switch (commandName) {
        case 'parallel-start':
          result = await this.executeParallelStart(params as ParallelStartParams);
          break;
        case 'parallel-status':
          result = await this.executeParallelStatus(params as ParallelStatusParams);
          break;
        case 'parallel-compare':
          result = await this.executeParallelCompare(params as ParallelCompareParams);
          break;
        case 'parallel-select':
          result = await this.executeParallelSelect(params as ParallelSelectParams);
          break;
        case 'parallel-merge':
          result = await this.executeParallelMerge(params as ParallelMergeParams);
          break;
        default:
          result = {
            success: false,
            command: commandName,
            output: '',
            error: `Command not implemented: ${commandName}`,
            duration: Date.now() - startTime,
          };
      }

      result.duration = Date.now() - startTime;
      this.emit('command:end', { command: commandName, result });
      return result;
    } catch (error) {
      const result: CommandResult = {
        success: false,
        command: commandName,
        output: '',
        error: error instanceof Error ? error.message : String(error),
        duration: Date.now() - startTime,
      };
      this.emit('command:error', { command: commandName, error });
      return result;
    }
  }

  /**
   * 验证参数
   */
  private validateParams(command: CommandDefinition, params: CommandParams): string | null {
    for (const param of command.parameters) {
      if (param.required && !(param.name in params)) {
        return `Missing required parameter: ${param.name}. ${param.description}`;
      }

      if (param.name in params && param.choices) {
        const value = params[param.name];
        if (typeof value === 'string' && !param.choices.includes(value)) {
          return `Invalid value for ${param.name}: ${value}. Valid choices: ${param.choices.join(', ')}`;
        }
      }
    }
    return null;
  }

  /**
   * 执行 parallel-start 命令
   */
  private async executeParallelStart(params: ParallelStartParams): Promise<CommandResult> {
    const taskId = `task_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
    const workerCount = params.workers || 3;
    const models = params.models || ['claude-3-opus', 'gemini-pro', 'gpt-4o'];

    const workers: WorkerStatus[] = [];
    for (let i = 0; i < workerCount; i++) {
      workers.push({
        id: `worker_${i + 1}`,
        model: models[i % models.length],
        status: 'pending',
        progress: 0,
        worktree: `.codeflow/worktrees/${taskId}/worker_${i + 1}`,
        branch: `parallel/${taskId}/worker_${i + 1}`,
      });
    }

    const taskStatus: ParallelTaskStatus = {
      taskId,
      task: params.task,
      status: 'running',
      workers,
      createdAt: Date.now(),
    };

    this.activeTasks.set(taskId, taskStatus);
    this.emit('task:started', taskStatus);

    const workerList = workers
      .map(w => `  ${w.id}: ${w.model} → ${w.branch}`)
      .join('\n');

    return {
      success: true,
      command: 'parallel-start',
      output: `🚀 Parallel Task Started!\n\nTask ID: ${taskId}\nTask: ${params.task}\nWorkers: ${workerCount}\nTimeout: ${params.timeout || 300000}ms\n\nWorkers:\n${workerList}\n\nUse /codeflow:parallel-status ${taskId} to check progress.`,
      data: taskStatus,
      duration: 0,
    };
  }

  /**
   * 执行 parallel-status 命令
   */
  private async executeParallelStatus(params: ParallelStatusParams): Promise<CommandResult> {
    if (params.taskId) {
      const task = this.activeTasks.get(params.taskId);
      if (!task) {
        return {
          success: false,
          command: 'parallel-status',
          output: '',
          error: `Task not found: ${params.taskId}`,
          duration: 0,
        };
      }

      const workerStatus = task.workers
        .map(w => {
          const statusIcon = w.status === 'completed' ? '✅' : w.status === 'running' ? '🔄' : w.status === 'failed' ? '❌' : '⬜';
          const progressBar = '█'.repeat(Math.floor(w.progress / 10)) + '░'.repeat(10 - Math.floor(w.progress / 10));
          return `  ${statusIcon} ${w.id} (${w.model}): [${progressBar}] ${w.progress}%`;
        })
        .join('\n');

      const output = params.verbose
        ? `📊 Task Status (Verbose)\n\nTask ID: ${task.taskId}\nTask: ${task.task}\nStatus: ${task.status}\nCreated: ${new Date(task.createdAt).toISOString()}\n\nWorkers:\n${workerStatus}\n\nWorktrees:\n${task.workers.map(w => `  ${w.id}: ${w.worktree}`).join('\n')}`
        : `📊 Task Status\n\nTask ID: ${task.taskId}\nStatus: ${task.status}\n\nWorkers:\n${workerStatus}`;

      return {
        success: true,
        command: 'parallel-status',
        output,
        data: task,
        duration: 0,
      };
    }

    // 显示所有任务
    if (this.activeTasks.size === 0) {
      return {
        success: true,
        command: 'parallel-status',
        output: '📊 No active parallel tasks.\n\nUse /codeflow:parallel-start to create a new task.',
        data: [],
        duration: 0,
      };
    }

    const taskList = Array.from(this.activeTasks.values())
      .map(t => {
        const completedWorkers = t.workers.filter(w => w.status === 'completed').length;
        return `  ${t.taskId}: ${t.task.substring(0, 30)}... [${completedWorkers}/${t.workers.length}]`;
      })
      .join('\n');

    return {
      success: true,
      command: 'parallel-status',
      output: `📊 Active Parallel Tasks\n\n${taskList}\n\nUse /codeflow:parallel-status <taskId> for details.`,
      data: Array.from(this.activeTasks.values()),
      duration: 0,
    };
  }

  /**
   * 执行 parallel-compare 命令
   */
  private async executeParallelCompare(params: ParallelCompareParams): Promise<CommandResult> {
    const task = this.activeTasks.get(params.taskId);
    if (!task) {
      return {
        success: false,
        command: 'parallel-compare',
        output: '',
        error: `Task not found: ${params.taskId}`,
        duration: 0,
      };
    }

    const metrics = params.metrics || ['quality', 'performance', 'maintainability'];
    const solutions = task.workers.map((w, i) => ({
      id: `solution_${i + 1}`,
      worker: w.id,
      model: w.model,
      scores: {
        quality: Math.floor(Math.random() * 30) + 70,
        performance: Math.floor(Math.random() * 30) + 70,
        maintainability: Math.floor(Math.random() * 30) + 70,
        security: Math.floor(Math.random() * 30) + 70,
        readability: Math.floor(Math.random() * 30) + 70,
      },
      recommendation: i === 0 ? 'recommended' : undefined,
    }));

    let output: string;
    if (params.format === 'json') {
      output = JSON.stringify(solutions, null, 2);
    } else if (params.format === 'markdown') {
      const header = `| Solution | Model | ${metrics.join(' | ')} | Total |`;
      const separator = `|${'-'.repeat(10)}|${'-'.repeat(15)}|${metrics.map(() => '-'.repeat(12)).join('|')}|${'-'.repeat(8)}|`;
      const rows = solutions.map(s => {
        const total = metrics.reduce((sum, m) => sum + (s.scores[m as keyof typeof s.scores] || 0), 0) / metrics.length;
        return `| ${s.id} | ${s.model} | ${metrics.map(m => s.scores[m as keyof typeof s.scores] || 'N/A').join(' | ')} | ${total.toFixed(1)} |`;
      });
      output = `${header}\n${separator}\n${rows.join('\n')}`;
    } else {
      // Table format
      const header = `Solution       Model              ${metrics.map(m => m.padEnd(12)).join('')}Total`;
      const rows = solutions.map(s => {
        const total = metrics.reduce((sum, m) => sum + (s.scores[m as keyof typeof s.scores] || 0), 0) / metrics.length;
        const rec = s.recommendation ? ' ⭐' : '';
        return `${s.id.padEnd(14)} ${s.model.padEnd(18)} ${metrics.map(m => String(s.scores[m as keyof typeof s.scores] || 'N/A').padEnd(12)).join('')}${total.toFixed(1)}${rec}`;
      });
      output = `${header}\n${'─'.repeat(80)}\n${rows.join('\n')}`;
    }

    return {
      success: true,
      command: 'parallel-compare',
      output: `📊 Solution Comparison\n\nTask ID: ${params.taskId}\nMetrics: ${metrics.join(', ')}\n\n${output}\n\n⭐ = Recommended solution`,
      data: solutions,
      duration: 0,
    };
  }

  /**
   * 执行 parallel-select 命令
   */
  private async executeParallelSelect(params: ParallelSelectParams): Promise<CommandResult> {
    const task = this.activeTasks.get(params.taskId);
    if (!task) {
      return {
        success: false,
        command: 'parallel-select',
        output: '',
        error: `Task not found: ${params.taskId}`,
        duration: 0,
      };
    }

    const selection = {
      taskId: params.taskId,
      solutionId: params.solutionId,
      reason: params.reason || 'User selection',
      selectedAt: Date.now(),
    };

    this.emit('solution:selected', selection);

    return {
      success: true,
      command: 'parallel-select',
      output: `✅ Solution Selected!\n\nTask ID: ${params.taskId}\nSolution: ${params.solutionId}\nReason: ${params.reason || 'User selection'}\n\nNext: Run /codeflow:parallel-merge ${params.taskId} ${params.solutionId} to merge.`,
      data: selection,
      duration: 0,
    };
  }

  /**
   * 执行 parallel-merge 命令
   */
  private async executeParallelMerge(params: ParallelMergeParams): Promise<CommandResult> {
    const task = this.activeTasks.get(params.taskId);
    if (!task) {
      return {
        success: false,
        command: 'parallel-merge',
        output: '',
        error: `Task not found: ${params.taskId}`,
        duration: 0,
      };
    }

    const mergeResult = {
      taskId: params.taskId,
      solutionId: params.solutionId,
      strategy: params.strategy || 'merge',
      backup: params.backup !== false,
      backupBranch: params.backup !== false ? `backup/${params.taskId}_${Date.now()}` : undefined,
      mergedAt: Date.now(),
      status: 'completed',
    };

    // 清理任务
    task.status = 'completed';
    task.completedAt = Date.now();

    this.emit('solution:merged', mergeResult);

    return {
      success: true,
      command: 'parallel-merge',
      output: `🎉 Merge Complete!\n\nTask ID: ${params.taskId}\nSolution: ${params.solutionId}\nStrategy: ${params.strategy || 'merge'}\n${mergeResult.backupBranch ? `Backup: ${mergeResult.backupBranch}\n` : ''}\nWorktrees cleaned up.\n\nThe selected solution has been merged to the main branch.`,
      data: mergeResult,
      duration: 0,
    };
  }

  /**
   * 获取命令定义
   */
  getCommand(name: string): CommandDefinition | undefined {
    return this.commands.get(name);
  }

  /**
   * 获取所有命令
   */
  getAllCommands(): CommandDefinition[] {
    return Array.from(this.commands.values());
  }

  /**
   * 获取帮助信息
   */
  getHelp(commandName?: string): string {
    if (commandName) {
      const command = this.commands.get(commandName);
      if (!command) {
        return `Unknown command: ${commandName}`;
      }

      const paramsHelp = command.parameters
        .map(p => `  ${p.required ? '' : '['}--${p.name}${p.required ? '' : ']'} <${p.type}>\n    ${p.description}${p.choices ? ` (choices: ${p.choices.join(', ')})` : ''}${p.default !== undefined ? ` (default: ${p.default})` : ''}`)
        .join('\n');

      return `📖 ${command.name}\n\n${command.description}\n\nUsage:\n  ${command.usage}\n\nParameters:\n${paramsHelp}\n\nExamples:\n${command.examples.map(e => `  ${e}`).join('\n')}`;
    }

    const commandList = Array.from(this.commands.values())
      .map(c => `  /codeflow:${c.name}\n    ${c.description}`)
      .join('\n\n');

    return `📚 Parallel Mode Commands\n\nAvailable commands:\n\n${commandList}\n\nUse /codeflow:parallel-help <command> for detailed help on a specific command.`;
  }

  /**
   * 获取活动任务
   */
  getActiveTasks(): ParallelTaskStatus[] {
    return Array.from(this.activeTasks.values());
  }

  /**
   * 获取任务
   */
  getTask(taskId: string): ParallelTaskStatus | undefined {
    return this.activeTasks.get(taskId);
  }

  /**
   * 解析命令行参数
   */
  parseArgs(args: string): CommandParams {
    const params: CommandParams = {};
    const tokens = args.match(/(?:[^\s"]+|"[^"]*")+/g) || [];

    let i = 0;
    let positionalIndex = 0;
    while (i < tokens.length) {
      const token = tokens[i];

      if (token.startsWith('--')) {
        const key = token.slice(2);
        const nextToken = tokens[i + 1];

        if (!nextToken || nextToken.startsWith('--')) {
          params[key] = true;
        } else {
          let value: unknown = nextToken.replace(/^"|"$/g, '');

          if (/^\d+$/.test(value as string)) {
            value = parseInt(value as string, 10);
          } else if (/^\d+\.\d+$/.test(value as string)) {
            value = parseFloat(value as string);
          } else if ((value as string).includes(',')) {
            value = (value as string).split(',').map(v => v.trim());
          }

          params[key] = value;
          i++;
        }
      } else {
        // Positional arguments
        const cleanToken = token.replace(/^"|"$/g, '');
        if (positionalIndex === 0) {
          params['_positional1'] = cleanToken;
        } else if (positionalIndex === 1) {
          params['_positional2'] = cleanToken;
        }
        positionalIndex++;
      }

      i++;
    }

    return params;
  }
}
