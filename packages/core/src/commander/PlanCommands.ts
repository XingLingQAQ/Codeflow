/**
 * PlanCommands - Plan 模式 CLI 命令
 * 实现 /codeflow:plan-* 系列命令
 */

import { EventEmitter } from 'events';

/**
 * 命令参数基础接口
 */
export interface CommandParams {
  [key: string]: unknown;
}

/**
 * Plan 命令参数
 */
export interface PlanNewParams extends CommandParams {
  name: string;
  description?: string;
  template?: string;
}

export interface PlanVisionParams extends CommandParams {
  planId: string;
  interactive?: boolean;
  questions?: string[];
}

export interface PlanConstraintsParams extends CommandParams {
  planId: string;
  source?: 'vision' | 'requirements' | 'both';
  format?: 'json' | 'markdown';
}

export interface PlanFastForwardParams extends CommandParams {
  planId: string;
  skipPhases?: string[];
  dryRun?: boolean;
}

export interface PlanExecuteParams extends CommandParams {
  planId: string;
  phase?: string;
  autoApprove?: boolean;
  maxIterations?: number;
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
 * Plan 命令处理器
 */
export class PlanCommands extends EventEmitter {
  private commands: Map<string, CommandDefinition> = new Map();

  constructor() {
    super();
    this.registerCommands();
  }

  /**
   * 注册所有 Plan 命令
   */
  private registerCommands(): void {
    // /codeflow:plan-new
    this.commands.set('plan-new', {
      name: 'plan-new',
      description: '创建新的 Plan 模式计划',
      usage: '/codeflow:plan-new <name> [--description <desc>] [--template <template>]',
      examples: [
        '/codeflow:plan-new "用户认证系统"',
        '/codeflow:plan-new "API重构" --template api-refactor',
        '/codeflow:plan-new "性能优化" --description "优化首页加载速度"',
      ],
      parameters: [
        {
          name: 'name',
          type: 'string',
          description: '计划名称',
          required: true,
        },
        {
          name: 'description',
          type: 'string',
          description: '计划描述',
          required: false,
        },
        {
          name: 'template',
          type: 'string',
          description: '使用的模板',
          required: false,
          choices: ['default', 'api-refactor', 'feature', 'bugfix', 'performance'],
        },
      ],
    });

    // /codeflow:plan-vision
    this.commands.set('plan-vision', {
      name: 'plan-vision',
      description: '构建计划愿景（交互式问答提取需求）',
      usage: '/codeflow:plan-vision <planId> [--interactive] [--questions <q1,q2,...>]',
      examples: [
        '/codeflow:plan-vision plan_123',
        '/codeflow:plan-vision plan_123 --interactive',
        '/codeflow:plan-vision plan_123 --questions "目标用户是谁,核心功能有哪些"',
      ],
      parameters: [
        {
          name: 'planId',
          type: 'string',
          description: '计划 ID',
          required: true,
        },
        {
          name: 'interactive',
          type: 'boolean',
          description: '是否启用交互式问答',
          required: false,
          default: true,
        },
        {
          name: 'questions',
          type: 'array',
          description: '预设问题列表',
          required: false,
        },
      ],
    });

    // /codeflow:plan-constraints
    this.commands.set('plan-constraints', {
      name: 'plan-constraints',
      description: '从愿景/需求生成约束集',
      usage: '/codeflow:plan-constraints <planId> [--source <source>] [--format <format>]',
      examples: [
        '/codeflow:plan-constraints plan_123',
        '/codeflow:plan-constraints plan_123 --source vision',
        '/codeflow:plan-constraints plan_123 --format json',
      ],
      parameters: [
        {
          name: 'planId',
          type: 'string',
          description: '计划 ID',
          required: true,
        },
        {
          name: 'source',
          type: 'string',
          description: '约束来源',
          required: false,
          default: 'both',
          choices: ['vision', 'requirements', 'both'],
        },
        {
          name: 'format',
          type: 'string',
          description: '输出格式',
          required: false,
          default: 'markdown',
          choices: ['json', 'markdown'],
        },
      ],
    });

    // /codeflow:plan-ff
    this.commands.set('plan-ff', {
      name: 'plan-ff',
      description: 'Fast-forward 一次性生成所有工件',
      usage: '/codeflow:plan-ff <planId> [--skip-phases <phases>] [--dry-run]',
      examples: [
        '/codeflow:plan-ff plan_123',
        '/codeflow:plan-ff plan_123 --dry-run',
        '/codeflow:plan-ff plan_123 --skip-phases "research,explore"',
      ],
      parameters: [
        {
          name: 'planId',
          type: 'string',
          description: '计划 ID',
          required: true,
        },
        {
          name: 'skipPhases',
          type: 'array',
          description: '跳过的阶段',
          required: false,
        },
        {
          name: 'dryRun',
          type: 'boolean',
          description: '仅预览不执行',
          required: false,
          default: false,
        },
      ],
    });

    // /codeflow:plan-execute
    this.commands.set('plan-execute', {
      name: 'plan-execute',
      description: '执行计划（按阶段推进）',
      usage: '/codeflow:plan-execute <planId> [--phase <phase>] [--auto-approve] [--max-iterations <n>]',
      examples: [
        '/codeflow:plan-execute plan_123',
        '/codeflow:plan-execute plan_123 --phase implement',
        '/codeflow:plan-execute plan_123 --auto-approve --max-iterations 5',
      ],
      parameters: [
        {
          name: 'planId',
          type: 'string',
          description: '计划 ID',
          required: true,
        },
        {
          name: 'phase',
          type: 'string',
          description: '指定执行阶段',
          required: false,
          choices: ['research', 'explore', 'review', 'implement', 'qa'],
        },
        {
          name: 'autoApprove',
          type: 'boolean',
          description: '自动批准阶段转换',
          required: false,
          default: false,
        },
        {
          name: 'maxIterations',
          type: 'number',
          description: '最大迭代次数',
          required: false,
          default: 3,
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
        error: `Unknown command: ${commandName}. Use /codeflow:plan-help for available commands.`,
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
        case 'plan-new':
          result = await this.executePlanNew(params as PlanNewParams);
          break;
        case 'plan-vision':
          result = await this.executePlanVision(params as PlanVisionParams);
          break;
        case 'plan-constraints':
          result = await this.executePlanConstraints(params as PlanConstraintsParams);
          break;
        case 'plan-ff':
          result = await this.executePlanFastForward(params as PlanFastForwardParams);
          break;
        case 'plan-execute':
          result = await this.executePlanExecute(params as PlanExecuteParams);
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
   * 执行 plan-new 命令
   */
  private async executePlanNew(params: PlanNewParams): Promise<CommandResult> {
    const planId = `plan_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;

    const planData = {
      id: planId,
      name: params.name,
      description: params.description || '',
      template: params.template || 'default',
      createdAt: Date.now(),
      status: 'created',
      phases: {
        vision: 'pending',
        constraints: 'pending',
        artifacts: 'pending',
        execution: 'pending',
      },
    };

    this.emit('plan:created', planData);

    return {
      success: true,
      command: 'plan-new',
      output: `✅ Plan created successfully!\n\nPlan ID: ${planId}\nName: ${params.name}\nTemplate: ${params.template || 'default'}\n\nNext steps:\n1. Run /codeflow:plan-vision ${planId} to build vision\n2. Run /codeflow:plan-constraints ${planId} to generate constraints\n3. Run /codeflow:plan-ff ${planId} to generate all artifacts`,
      data: planData,
      duration: 0,
    };
  }

  /**
   * 执行 plan-vision 命令
   */
  private async executePlanVision(params: PlanVisionParams): Promise<CommandResult> {
    const visionData = {
      planId: params.planId,
      interactive: params.interactive !== false,
      questions: params.questions || [
        '这个项目要解决什么问题？',
        '目标用户是谁？',
        '核心功能有哪些？',
        '有什么技术约束？',
        '期望的时间线是什么？',
      ],
      status: 'in_progress',
    };

    this.emit('vision:started', visionData);

    const output = params.interactive !== false
      ? `🎯 Vision Builder Started (Interactive Mode)\n\nPlan ID: ${params.planId}\n\nI'll ask you a series of questions to understand your vision:\n\n${visionData.questions.map((q, i) => `${i + 1}. ${q}`).join('\n')}\n\nPlease answer each question to help me understand your requirements.`
      : `🎯 Vision Builder Started\n\nPlan ID: ${params.planId}\nQuestions: ${visionData.questions.length}\n\nProcessing vision extraction...`;

    return {
      success: true,
      command: 'plan-vision',
      output,
      data: visionData,
      duration: 0,
    };
  }

  /**
   * 执行 plan-constraints 命令
   */
  private async executePlanConstraints(params: PlanConstraintsParams): Promise<CommandResult> {
    const constraintsData = {
      planId: params.planId,
      source: params.source || 'both',
      format: params.format || 'markdown',
      constraints: [
        { id: 'c1', type: 'functional', description: '系统必须支持用户认证', priority: 'high' },
        { id: 'c2', type: 'performance', description: '响应时间 < 200ms', priority: 'medium' },
        { id: 'c3', type: 'security', description: '所有 API 需要认证', priority: 'high' },
        { id: 'c4', type: 'compatibility', description: '支持 Chrome/Firefox/Safari', priority: 'medium' },
      ],
      status: 'completed',
    };

    this.emit('constraints:generated', constraintsData);

    const formatOutput = params.format === 'json'
      ? JSON.stringify(constraintsData.constraints, null, 2)
      : constraintsData.constraints.map(c => `- [${c.priority.toUpperCase()}] ${c.type}: ${c.description}`).join('\n');

    return {
      success: true,
      command: 'plan-constraints',
      output: `📋 Constraints Generated\n\nPlan ID: ${params.planId}\nSource: ${params.source || 'both'}\nFormat: ${params.format || 'markdown'}\n\nConstraints:\n${formatOutput}`,
      data: constraintsData,
      duration: 0,
    };
  }

  /**
   * 执行 plan-ff 命令
   */
  private async executePlanFastForward(params: PlanFastForwardParams): Promise<CommandResult> {
    const artifacts = {
      planId: params.planId,
      dryRun: params.dryRun || false,
      skipPhases: params.skipPhases || [],
      generated: [
        { name: 'proposal.md', status: 'generated', path: '.codeflow/changes/proposal.md' },
        { name: 'specs/', status: 'generated', path: '.codeflow/changes/specs/' },
        { name: 'design.md', status: 'generated', path: '.codeflow/changes/design.md' },
        { name: 'tasks.md', status: 'generated', path: '.codeflow/changes/tasks.md' },
      ],
    };

    if (params.dryRun) {
      return {
        success: true,
        command: 'plan-ff',
        output: `🔍 Fast-Forward Preview (Dry Run)\n\nPlan ID: ${params.planId}\n\nArtifacts to be generated:\n${artifacts.generated.map(a => `  📄 ${a.name} → ${a.path}`).join('\n')}\n\nRun without --dry-run to generate artifacts.`,
        data: { ...artifacts, status: 'preview' },
        duration: 0,
      };
    }

    this.emit('artifacts:generated', artifacts);

    return {
      success: true,
      command: 'plan-ff',
      output: `⚡ Fast-Forward Complete!\n\nPlan ID: ${params.planId}\n\nGenerated Artifacts:\n${artifacts.generated.map(a => `  ✅ ${a.name} → ${a.path}`).join('\n')}\n\nNext: Run /codeflow:plan-execute ${params.planId} to start execution.`,
      data: { ...artifacts, status: 'completed' },
      duration: 0,
    };
  }

  /**
   * 执行 plan-execute 命令
   */
  private async executePlanExecute(params: PlanExecuteParams): Promise<CommandResult> {
    const executionData = {
      planId: params.planId,
      phase: params.phase || 'research',
      autoApprove: params.autoApprove || false,
      maxIterations: params.maxIterations || 3,
      status: 'in_progress',
      currentIteration: 1,
    };

    this.emit('execution:started', executionData);

    const phases = ['research', 'explore', 'review', 'implement', 'qa'];
    const currentPhaseIndex = phases.indexOf(executionData.phase);
    const progressBar = phases.map((p, i) => i < currentPhaseIndex ? '✅' : i === currentPhaseIndex ? '🔄' : '⬜').join(' ');

    return {
      success: true,
      command: 'plan-execute',
      output: `🚀 Plan Execution Started\n\nPlan ID: ${params.planId}\nCurrent Phase: ${executionData.phase}\nAuto-Approve: ${executionData.autoApprove}\nMax Iterations: ${executionData.maxIterations}\n\nProgress: ${progressBar}\n           ${phases.join(' → ')}\n\nExecuting ${executionData.phase} phase...`,
      data: executionData,
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

    return `📚 Plan Mode Commands\n\nAvailable commands:\n\n${commandList}\n\nUse /codeflow:plan-help <command> for detailed help on a specific command.`;
  }

  /**
   * 解析命令行参数
   */
  parseArgs(args: string): CommandParams {
    const params: CommandParams = {};
    const tokens = args.match(/(?:[^\s"]+|"[^"]*")+/g) || [];

    let i = 0;
    while (i < tokens.length) {
      const token = tokens[i];

      if (token.startsWith('--')) {
        const key = token.slice(2);
        const nextToken = tokens[i + 1];

        if (!nextToken || nextToken.startsWith('--')) {
          // Boolean flag
          params[key] = true;
        } else {
          // Value parameter
          let value: unknown = nextToken.replace(/^"|"$/g, '');

          // Try to parse as number
          if (/^\d+$/.test(value as string)) {
            value = parseInt(value as string, 10);
          } else if (/^\d+\.\d+$/.test(value as string)) {
            value = parseFloat(value as string);
          } else if ((value as string).includes(',')) {
            // Array
            value = (value as string).split(',').map(v => v.trim());
          }

          params[key] = value;
          i++;
        }
      } else if (!params['_positional']) {
        // First positional argument
        params['_positional'] = token.replace(/^"|"$/g, '');
      }

      i++;
    }

    return params;
  }
}
