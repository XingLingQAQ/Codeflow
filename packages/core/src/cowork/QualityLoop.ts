/**
 * QualityLoop - 质量控制循环
 * 实现 Trellis 的 ralph-loop 质量控制循环：Check → Fix → Check
 */

import { EventEmitter } from 'events';

/**
 * 检查类型
 */
export type CheckType =
  | 'lint'
  | 'type'
  | 'test'
  | 'security'
  | 'performance'
  | 'style'
  | 'documentation'
  | 'custom';

/**
 * 检查严重程度
 */
export type CheckSeverity = 'error' | 'warning' | 'info';

/**
 * 检查问题
 */
export interface CheckIssue {
  id: string;
  type: CheckType;
  severity: CheckSeverity;
  message: string;
  file?: string;
  line?: number;
  column?: number;
  rule?: string;
  suggestion?: string;
  autoFixable: boolean;
}

/**
 * 检查结果
 */
export interface CheckResult {
  passed: boolean;
  issues: CheckIssue[];
  summary: {
    errors: number;
    warnings: number;
    infos: number;
    autoFixable: number;
  };
  duration: number;
  checkType: CheckType;
}

/**
 * 修复结果
 */
export interface FixResult {
  success: boolean;
  fixedIssues: string[];
  failedIssues: string[];
  filesModified: string[];
  duration: number;
}

/**
 * 迭代结果
 */
export interface IterationResult {
  iteration: number;
  checkResult: CheckResult;
  fixResult?: FixResult;
  status: 'passed' | 'fixed' | 'failed' | 'manual_required';
}

/**
 * 循环结果
 */
export interface LoopResult {
  passed: boolean;
  iterations: IterationResult[];
  totalDuration: number;
  finalIssues: CheckIssue[];
  requiresManualIntervention: boolean;
  summary: string;
}

/**
 * Check Agent 回调
 */
export type CheckAgentCallback = (
  files: string[],
  checkTypes: CheckType[]
) => Promise<CheckResult>;

/**
 * Fix Agent 回调
 */
export type FixAgentCallback = (
  issues: CheckIssue[]
) => Promise<FixResult>;

/**
 * Quality Loop 配置
 */
export interface QualityLoopConfig {
  maxIterations: number;
  checkTypes: CheckType[];
  autoFixEnabled: boolean;
  stopOnError: boolean;
  checkModelId?: string;
  fixModelId?: string;
  timeout: number;
}

const DEFAULT_CONFIG: QualityLoopConfig = {
  maxIterations: 3,
  checkTypes: ['lint', 'type', 'test'],
  autoFixEnabled: true,
  stopOnError: false,
  timeout: 60000,
};

/**
 * CheckAgent - 检查 Agent
 */
export class CheckAgent extends EventEmitter {
  private callback?: CheckAgentCallback;
  private modelId?: string;

  constructor(callback?: CheckAgentCallback, modelId?: string) {
    super();
    this.callback = callback;
    this.modelId = modelId;
  }

  /**
   * 执行检查
   */
  async check(files: string[], checkTypes: CheckType[]): Promise<CheckResult> {
    const startTime = Date.now();
    this.emit('check:start', { files, checkTypes });

    try {
      let result: CheckResult;

      if (this.callback) {
        result = await this.callback(files, checkTypes);
      } else {
        result = this.generateStaticCheck(files, checkTypes);
      }

      result.duration = Date.now() - startTime;
      this.emit('check:complete', result);
      return result;
    } catch (error) {
      const errorResult: CheckResult = {
        passed: false,
        issues: [{
          id: 'check-error',
          type: 'custom',
          severity: 'error',
          message: error instanceof Error ? error.message : 'Check failed',
          autoFixable: false,
        }],
        summary: { errors: 1, warnings: 0, infos: 0, autoFixable: 0 },
        duration: Date.now() - startTime,
        checkType: 'custom',
      };

      this.emit('check:error', errorResult);
      return errorResult;
    }
  }

  private generateStaticCheck(files: string[], checkTypes: CheckType[]): CheckResult {
    // 模拟检查结果
    const issues: CheckIssue[] = [];

    // 随机生成一些问题用于测试
    if (Math.random() > 0.7) {
      issues.push({
        id: `issue-${Date.now()}`,
        type: checkTypes[0] || 'lint',
        severity: 'warning',
        message: 'Potential issue detected',
        autoFixable: true,
      });
    }

    return {
      passed: issues.filter(i => i.severity === 'error').length === 0,
      issues,
      summary: {
        errors: issues.filter(i => i.severity === 'error').length,
        warnings: issues.filter(i => i.severity === 'warning').length,
        infos: issues.filter(i => i.severity === 'info').length,
        autoFixable: issues.filter(i => i.autoFixable).length,
      },
      duration: 0,
      checkType: checkTypes[0] || 'custom',
    };
  }

  /**
   * 获取模型 ID
   */
  getModelId(): string | undefined {
    return this.modelId;
  }
}

/**
 * FixAgent - 修复 Agent
 */
export class FixAgent extends EventEmitter {
  private callback?: FixAgentCallback;
  private modelId?: string;

  constructor(callback?: FixAgentCallback, modelId?: string) {
    super();
    this.callback = callback;
    this.modelId = modelId;
  }

  /**
   * 执行修复
   */
  async fix(issues: CheckIssue[]): Promise<FixResult> {
    const startTime = Date.now();
    this.emit('fix:start', { issues });

    // 只修复可自动修复的问题
    const fixableIssues = issues.filter(i => i.autoFixable);

    if (fixableIssues.length === 0) {
      const noFixResult: FixResult = {
        success: true,
        fixedIssues: [],
        failedIssues: [],
        filesModified: [],
        duration: Date.now() - startTime,
      };
      this.emit('fix:complete', noFixResult);
      return noFixResult;
    }

    try {
      let result: FixResult;

      if (this.callback) {
        result = await this.callback(fixableIssues);
      } else {
        result = this.generateStaticFix(fixableIssues);
      }

      result.duration = Date.now() - startTime;
      this.emit('fix:complete', result);
      return result;
    } catch (error) {
      const errorResult: FixResult = {
        success: false,
        fixedIssues: [],
        failedIssues: fixableIssues.map(i => i.id),
        filesModified: [],
        duration: Date.now() - startTime,
      };

      this.emit('fix:error', errorResult);
      return errorResult;
    }
  }

  private generateStaticFix(issues: CheckIssue[]): FixResult {
    // 模拟修复结果
    const fixedIssues = issues.map(i => i.id);
    const filesModified = [...new Set(issues.filter(i => i.file).map(i => i.file!))];

    return {
      success: true,
      fixedIssues,
      failedIssues: [],
      filesModified,
      duration: 0,
    };
  }

  /**
   * 获取模型 ID
   */
  getModelId(): string | undefined {
    return this.modelId;
  }
}

/**
 * IterationManager - 迭代管理器
 */
export class IterationManager extends EventEmitter {
  private iterations: IterationResult[] = [];
  private maxIterations: number;

  constructor(maxIterations: number = 3) {
    super();
    this.maxIterations = maxIterations;
  }

  /**
   * 记录迭代
   */
  recordIteration(result: IterationResult): void {
    this.iterations.push(result);
    this.emit('iteration:recorded', result);
  }

  /**
   * 是否可以继续迭代
   */
  canContinue(): boolean {
    return this.iterations.length < this.maxIterations;
  }

  /**
   * 获取当前迭代次数
   */
  getCurrentIteration(): number {
    return this.iterations.length;
  }

  /**
   * 获取所有迭代
   */
  getIterations(): IterationResult[] {
    return [...this.iterations];
  }

  /**
   * 获取最后一次迭代
   */
  getLastIteration(): IterationResult | undefined {
    return this.iterations[this.iterations.length - 1];
  }

  /**
   * 重置
   */
  reset(): void {
    this.iterations = [];
    this.emit('iterations:reset');
  }

  /**
   * 更新最大迭代次数
   */
  setMaxIterations(max: number): void {
    this.maxIterations = max;
  }
}

/**
 * QualityLoop - 质量控制循环
 */
export class QualityLoop extends EventEmitter {
  private config: QualityLoopConfig;
  private checkAgent: CheckAgent;
  private fixAgent: FixAgent;
  private iterationManager: IterationManager;
  private running: boolean = false;

  constructor(
    config: Partial<QualityLoopConfig> = {},
    callbacks?: {
      check?: CheckAgentCallback;
      fix?: FixAgentCallback;
    }
  ) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.checkAgent = new CheckAgent(callbacks?.check, this.config.checkModelId);
    this.fixAgent = new FixAgent(callbacks?.fix, this.config.fixModelId);
    this.iterationManager = new IterationManager(this.config.maxIterations);

    // 转发事件
    this.checkAgent.on('check:start', (data) => this.emit('check:start', data));
    this.checkAgent.on('check:complete', (data) => this.emit('check:complete', data));
    this.fixAgent.on('fix:start', (data) => this.emit('fix:start', data));
    this.fixAgent.on('fix:complete', (data) => this.emit('fix:complete', data));
    this.iterationManager.on('iteration:recorded', (data) => this.emit('iteration:recorded', data));
  }

  /**
   * 执行质量循环
   */
  async run(files: string[]): Promise<LoopResult> {
    if (this.running) {
      throw new Error('Quality loop is already running');
    }

    this.running = true;
    this.iterationManager.reset();
    const startTime = Date.now();

    this.emit('loop:start', { files, config: this.config });

    try {
      while (this.iterationManager.canContinue()) {
        const iteration = this.iterationManager.getCurrentIteration() + 1;
        this.emit('iteration:start', { iteration });

        // 执行检查
        const checkResult = await this.checkAgent.check(files, this.config.checkTypes);

        // 如果检查通过，结束循环
        if (checkResult.passed) {
          const iterationResult: IterationResult = {
            iteration,
            checkResult,
            status: 'passed',
          };
          this.iterationManager.recordIteration(iterationResult);
          break;
        }

        // 如果有错误且配置为停止，结束循环
        if (this.config.stopOnError && checkResult.summary.errors > 0) {
          const iterationResult: IterationResult = {
            iteration,
            checkResult,
            status: 'failed',
          };
          this.iterationManager.recordIteration(iterationResult);
          break;
        }

        // 尝试自动修复
        if (this.config.autoFixEnabled && checkResult.summary.autoFixable > 0) {
          const fixResult = await this.fixAgent.fix(checkResult.issues);

          const iterationResult: IterationResult = {
            iteration,
            checkResult,
            fixResult,
            status: fixResult.success ? 'fixed' : 'failed',
          };
          this.iterationManager.recordIteration(iterationResult);

          // 如果修复失败，结束循环
          if (!fixResult.success) {
            break;
          }
        } else {
          // 没有可自动修复的问题，需要人工介入
          const iterationResult: IterationResult = {
            iteration,
            checkResult,
            status: 'manual_required',
          };
          this.iterationManager.recordIteration(iterationResult);
          break;
        }
      }

      const result = this.buildLoopResult(startTime);
      this.emit('loop:complete', result);
      return result;
    } finally {
      this.running = false;
    }
  }

  /**
   * 构建循环结果
   */
  private buildLoopResult(startTime: number): LoopResult {
    const iterations = this.iterationManager.getIterations();
    const lastIteration = this.iterationManager.getLastIteration();

    const passed = lastIteration?.status === 'passed';
    const requiresManualIntervention = lastIteration?.status === 'manual_required' ||
      (lastIteration?.status === 'failed' && !this.iterationManager.canContinue());

    const finalIssues = lastIteration?.checkResult.issues.filter(i => i.severity === 'error') || [];

    let summary: string;
    if (passed) {
      summary = `Quality check passed after ${iterations.length} iteration(s)`;
    } else if (requiresManualIntervention) {
      summary = `Quality check requires manual intervention. ${finalIssues.length} issue(s) remaining.`;
    } else {
      summary = `Quality check failed after ${iterations.length} iteration(s). Max iterations reached.`;
    }

    return {
      passed,
      iterations,
      totalDuration: Date.now() - startTime,
      finalIssues,
      requiresManualIntervention,
      summary,
    };
  }

  /**
   * 停止循环
   */
  stop(): void {
    this.running = false;
    this.emit('loop:stopped');
  }

  /**
   * 是否正在运行
   */
  isRunning(): boolean {
    return this.running;
  }

  /**
   * 获取当前迭代次数
   */
  getCurrentIteration(): number {
    return this.iterationManager.getCurrentIteration();
  }

  /**
   * 获取迭代历史
   */
  getIterationHistory(): IterationResult[] {
    return this.iterationManager.getIterations();
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<QualityLoopConfig>): void {
    this.config = { ...this.config, ...config };
    this.iterationManager.setMaxIterations(this.config.maxIterations);
  }

  /**
   * 获取配置
   */
  getConfig(): QualityLoopConfig {
    return { ...this.config };
  }

  /**
   * 获取 Check Agent
   */
  getCheckAgent(): CheckAgent {
    return this.checkAgent;
  }

  /**
   * 获取 Fix Agent
   */
  getFixAgent(): FixAgent {
    return this.fixAgent;
  }
}
