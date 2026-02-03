/**
 * ExecutionLoop - 执行循环
 * 实现 Aha-Loop 的 5 阶段执行循环：Research → Explore → Review → Implement → QA
 */

import { EventEmitter } from 'events';
import * as fs from 'fs';
import * as path from 'path';
import {
  PlanSession,
  PlanTask,
  VisionDocument,
  ConstraintSet,
  ArchitectureArtifact,
  RoadmapArtifact,
  PRDItem,
} from './types.js';

/**
 * 执行阶段
 */
export type ExecutionPhase = 'research' | 'explore' | 'review' | 'implement' | 'qa';

/**
 * 阶段状态
 */
export type PhaseStatus = 'pending' | 'running' | 'completed' | 'skipped' | 'failed';

/**
 * 阶段结果
 */
export interface PhaseResult {
  phase: ExecutionPhase;
  status: PhaseStatus;
  startTime: number;
  endTime?: number;
  output?: unknown;
  error?: string;
  skippedReason?: string;
}

/**
 * 执行上下文
 */
export interface ExecutionContext {
  sessionId: string;
  prdItem: PRDItem;
  vision: VisionDocument;
  constraints: ConstraintSet;
  architecture?: ArchitectureArtifact;
  roadmap?: RoadmapArtifact;
  previousResults: Map<ExecutionPhase, PhaseResult>;
}

/**
 * Research 阶段输出
 */
export interface ResearchOutput {
  findings: string[];
  recommendations: string[];
  risks: string[];
  dependencies: string[];
}

/**
 * Explore 阶段输出
 */
export interface ExploreOutput {
  approaches: ExploreApproach[];
  selectedApproach?: string;
  parallelExploration: boolean;
}

/**
 * 探索方案
 */
export interface ExploreApproach {
  id: string;
  name: string;
  description: string;
  pros: string[];
  cons: string[];
  complexity: 'low' | 'medium' | 'high';
  recommended: boolean;
}

/**
 * Review 阶段输出
 */
export interface ReviewOutput {
  approved: boolean;
  feedback: string[];
  requiredChanges: string[];
  reviewers: string[];
}

/**
 * Implement 阶段输出
 */
export interface ImplementOutput {
  filesCreated: string[];
  filesModified: string[];
  testsAdded: string[];
  documentation: string[];
}

/**
 * QA 阶段输出
 */
export interface QAOutput {
  testsRun: number;
  testsPassed: number;
  testsFailed: number;
  coverage?: number;
  issues: QAIssue[];
  approved: boolean;
}

/**
 * QA 问题
 */
export interface QAIssue {
  severity: 'critical' | 'major' | 'minor';
  description: string;
  file?: string;
  line?: number;
  suggestion?: string;
}

/**
 * 阶段执行器回调
 */
export type PhaseExecutor<T> = (context: ExecutionContext) => Promise<T>;

/**
 * 执行循环配置
 */
export interface ExecutionLoopConfig {
  outputDir: string;
  autoSkipExplore: boolean;
  requireReviewApproval: boolean;
  maxQAIterations: number;
  persistState: boolean;
  phaseModels: {
    research?: string;
    explore?: string;
    review?: string;
    implement?: string;
    qa?: string;
  };
}

const DEFAULT_CONFIG: ExecutionLoopConfig = {
  outputDir: '.codeflow/execution',
  autoSkipExplore: false,
  requireReviewApproval: true,
  maxQAIterations: 3,
  persistState: true,
  phaseModels: {},
};

/**
 * 执行循环状态
 */
export interface ExecutionLoopState {
  sessionId: string;
  currentPhase: ExecutionPhase;
  phaseResults: PhaseResult[];
  startTime: number;
  endTime?: number;
  status: 'running' | 'completed' | 'failed' | 'paused';
  qaIterations: number;
}

/**
 * ResearchPhase - 技术研究阶段
 */
export class ResearchPhase extends EventEmitter {
  private executor?: PhaseExecutor<ResearchOutput>;

  constructor(executor?: PhaseExecutor<ResearchOutput>) {
    super();
    this.executor = executor;
  }

  async execute(context: ExecutionContext): Promise<PhaseResult> {
    const startTime = Date.now();
    this.emit('phase:start', { phase: 'research' });

    try {
      let output: ResearchOutput;

      if (this.executor) {
        output = await this.executor(context);
      } else {
        output = this.generateStaticOutput(context);
      }

      const result: PhaseResult = {
        phase: 'research',
        status: 'completed',
        startTime,
        endTime: Date.now(),
        output,
      };

      this.emit('phase:complete', result);
      return result;
    } catch (error) {
      const result: PhaseResult = {
        phase: 'research',
        status: 'failed',
        startTime,
        endTime: Date.now(),
        error: error instanceof Error ? error.message : 'Unknown error',
      };

      this.emit('phase:error', result);
      return result;
    }
  }

  private generateStaticOutput(context: ExecutionContext): ResearchOutput {
    return {
      findings: [
        `Research for: ${context.prdItem.title}`,
        `Based on vision: ${context.vision.title}`,
      ],
      recommendations: context.prdItem.acceptanceCriteria.map(c => `Implement: ${c}`),
      risks: context.vision.risks,
      dependencies: context.constraints.constraints.map(c => c.description),
    };
  }
}

/**
 * ExplorePhase - 并行探索阶段
 */
export class ExplorePhase extends EventEmitter {
  private executor?: PhaseExecutor<ExploreOutput>;

  constructor(executor?: PhaseExecutor<ExploreOutput>) {
    super();
    this.executor = executor;
  }

  async execute(context: ExecutionContext): Promise<PhaseResult> {
    const startTime = Date.now();
    this.emit('phase:start', { phase: 'explore' });

    try {
      let output: ExploreOutput;

      if (this.executor) {
        output = await this.executor(context);
      } else {
        output = this.generateStaticOutput(context);
      }

      const result: PhaseResult = {
        phase: 'explore',
        status: 'completed',
        startTime,
        endTime: Date.now(),
        output,
      };

      this.emit('phase:complete', result);
      return result;
    } catch (error) {
      const result: PhaseResult = {
        phase: 'explore',
        status: 'failed',
        startTime,
        endTime: Date.now(),
        error: error instanceof Error ? error.message : 'Unknown error',
      };

      this.emit('phase:error', result);
      return result;
    }
  }

  private generateStaticOutput(context: ExecutionContext): ExploreOutput {
    return {
      approaches: [
        {
          id: 'approach-1',
          name: 'Standard Implementation',
          description: `Standard approach for ${context.prdItem.title}`,
          pros: ['Well-tested patterns', 'Easy to maintain'],
          cons: ['May not be optimal'],
          complexity: 'medium',
          recommended: true,
        },
      ],
      selectedApproach: 'approach-1',
      parallelExploration: false,
    };
  }

  /**
   * 判断是否需要并行探索
   */
  shouldExploreInParallel(context: ExecutionContext): boolean {
    // 基于复杂度和风险判断
    const hasHighRisk = context.vision.risks.length > 2;
    const hasMultipleGoals = context.vision.goals.length > 3;
    const isLargeEffort = context.prdItem.estimatedEffort === 'large' || context.prdItem.estimatedEffort === 'xlarge';

    return hasHighRisk || (hasMultipleGoals && isLargeEffort);
  }
}

/**
 * ReviewPhase - 计划评审阶段
 */
export class ReviewPhase extends EventEmitter {
  private executor?: PhaseExecutor<ReviewOutput>;

  constructor(executor?: PhaseExecutor<ReviewOutput>) {
    super();
    this.executor = executor;
  }

  async execute(context: ExecutionContext): Promise<PhaseResult> {
    const startTime = Date.now();
    this.emit('phase:start', { phase: 'review' });

    try {
      let output: ReviewOutput;

      if (this.executor) {
        output = await this.executor(context);
      } else {
        output = this.generateStaticOutput(context);
      }

      const result: PhaseResult = {
        phase: 'review',
        status: 'completed',
        startTime,
        endTime: Date.now(),
        output,
      };

      this.emit('phase:complete', result);
      return result;
    } catch (error) {
      const result: PhaseResult = {
        phase: 'review',
        status: 'failed',
        startTime,
        endTime: Date.now(),
        error: error instanceof Error ? error.message : 'Unknown error',
      };

      this.emit('phase:error', result);
      return result;
    }
  }

  private generateStaticOutput(context: ExecutionContext): ReviewOutput {
    return {
      approved: true,
      feedback: ['Plan looks good', 'Ready for implementation'],
      requiredChanges: [],
      reviewers: ['AI Reviewer'],
    };
  }
}

/**
 * ImplementPhase - 代码实现阶段
 */
export class ImplementPhase extends EventEmitter {
  private executor?: PhaseExecutor<ImplementOutput>;

  constructor(executor?: PhaseExecutor<ImplementOutput>) {
    super();
    this.executor = executor;
  }

  async execute(context: ExecutionContext): Promise<PhaseResult> {
    const startTime = Date.now();
    this.emit('phase:start', { phase: 'implement' });

    try {
      let output: ImplementOutput;

      if (this.executor) {
        output = await this.executor(context);
      } else {
        output = this.generateStaticOutput(context);
      }

      const result: PhaseResult = {
        phase: 'implement',
        status: 'completed',
        startTime,
        endTime: Date.now(),
        output,
      };

      this.emit('phase:complete', result);
      return result;
    } catch (error) {
      const result: PhaseResult = {
        phase: 'implement',
        status: 'failed',
        startTime,
        endTime: Date.now(),
        error: error instanceof Error ? error.message : 'Unknown error',
      };

      this.emit('phase:error', result);
      return result;
    }
  }

  private generateStaticOutput(context: ExecutionContext): ImplementOutput {
    return {
      filesCreated: [],
      filesModified: [],
      testsAdded: [],
      documentation: [],
    };
  }
}

/**
 * QAPhase - 质量检查阶段
 */
export class QAPhase extends EventEmitter {
  private executor?: PhaseExecutor<QAOutput>;

  constructor(executor?: PhaseExecutor<QAOutput>) {
    super();
    this.executor = executor;
  }

  async execute(context: ExecutionContext): Promise<PhaseResult> {
    const startTime = Date.now();
    this.emit('phase:start', { phase: 'qa' });

    try {
      let output: QAOutput;

      if (this.executor) {
        output = await this.executor(context);
      } else {
        output = this.generateStaticOutput(context);
      }

      const result: PhaseResult = {
        phase: 'qa',
        status: 'completed',
        startTime,
        endTime: Date.now(),
        output,
      };

      this.emit('phase:complete', result);
      return result;
    } catch (error) {
      const result: PhaseResult = {
        phase: 'qa',
        status: 'failed',
        startTime,
        endTime: Date.now(),
        error: error instanceof Error ? error.message : 'Unknown error',
      };

      this.emit('phase:error', result);
      return result;
    }
  }

  private generateStaticOutput(context: ExecutionContext): QAOutput {
    return {
      testsRun: 10,
      testsPassed: 10,
      testsFailed: 0,
      coverage: 80,
      issues: [],
      approved: true,
    };
  }
}

/**
 * ExecutionLoop - 执行循环编排器
 */
export class ExecutionLoop extends EventEmitter {
  private config: ExecutionLoopConfig;
  private state?: ExecutionLoopState;
  private phases: {
    research: ResearchPhase;
    explore: ExplorePhase;
    review: ReviewPhase;
    implement: ImplementPhase;
    qa: QAPhase;
  };

  constructor(
    config: Partial<ExecutionLoopConfig> = {},
    executors?: {
      research?: PhaseExecutor<ResearchOutput>;
      explore?: PhaseExecutor<ExploreOutput>;
      review?: PhaseExecutor<ReviewOutput>;
      implement?: PhaseExecutor<ImplementOutput>;
      qa?: PhaseExecutor<QAOutput>;
    }
  ) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.phases = {
      research: new ResearchPhase(executors?.research),
      explore: new ExplorePhase(executors?.explore),
      review: new ReviewPhase(executors?.review),
      implement: new ImplementPhase(executors?.implement),
      qa: new QAPhase(executors?.qa),
    };

    // 转发阶段事件
    for (const [name, phase] of Object.entries(this.phases)) {
      phase.on('phase:start', (data) => this.emit('phase:start', { ...data, phaseName: name }));
      phase.on('phase:complete', (data) => this.emit('phase:complete', { ...data, phaseName: name }));
      phase.on('phase:error', (data) => this.emit('phase:error', { ...data, phaseName: name }));
    }
  }

  /**
   * 执行完整循环
   */
  async execute(context: ExecutionContext): Promise<ExecutionLoopState> {
    this.state = {
      sessionId: context.sessionId,
      currentPhase: 'research',
      phaseResults: [],
      startTime: Date.now(),
      status: 'running',
      qaIterations: 0,
    };

    this.emit('loop:start', { sessionId: context.sessionId });

    const phaseOrder: ExecutionPhase[] = ['research', 'explore', 'review', 'implement', 'qa'];

    for (const phase of phaseOrder) {
      this.state.currentPhase = phase;

      // 检查是否跳过 explore 阶段
      if (phase === 'explore' && this.shouldSkipExplore(context)) {
        const skipResult: PhaseResult = {
          phase: 'explore',
          status: 'skipped',
          startTime: Date.now(),
          endTime: Date.now(),
          skippedReason: 'Auto-skipped: simple task or no parallel exploration needed',
        };
        this.state.phaseResults.push(skipResult);
        context.previousResults.set('explore', skipResult);
        this.emit('phase:skipped', skipResult);
        continue;
      }

      // 执行阶段
      const result = await this.executePhase(phase, context);
      this.state.phaseResults.push(result);
      context.previousResults.set(phase, result);

      // 检查失败
      if (result.status === 'failed') {
        this.state.status = 'failed';
        this.state.endTime = Date.now();
        this.emit('loop:failed', { sessionId: context.sessionId, phase, error: result.error });
        await this.persistState();
        return this.state;
      }

      // Review 阶段检查
      if (phase === 'review' && this.config.requireReviewApproval) {
        const reviewOutput = result.output as ReviewOutput;
        if (!reviewOutput.approved) {
          this.state.status = 'paused';
          this.emit('loop:paused', { sessionId: context.sessionId, reason: 'Review not approved' });
          await this.persistState();
          return this.state;
        }
      }

      // QA 阶段检查
      if (phase === 'qa') {
        const qaOutput = result.output as QAOutput;
        if (!qaOutput.approved && this.state.qaIterations < this.config.maxQAIterations) {
          this.state.qaIterations++;
          // 回到 implement 阶段
          const implResult = await this.executePhase('implement', context);
          this.state.phaseResults.push(implResult);
          context.previousResults.set('implement', implResult);

          // 重新执行 QA
          const qaRetryResult = await this.executePhase('qa', context);
          this.state.phaseResults.push(qaRetryResult);
          context.previousResults.set('qa', qaRetryResult);
        }
      }
    }

    this.state.status = 'completed';
    this.state.endTime = Date.now();
    this.emit('loop:complete', { sessionId: context.sessionId, state: this.state });
    await this.persistState();

    return this.state;
  }

  /**
   * 执行单个阶段
   */
  async executePhase(phase: ExecutionPhase, context: ExecutionContext): Promise<PhaseResult> {
    switch (phase) {
      case 'research':
        return this.phases.research.execute(context);
      case 'explore':
        return this.phases.explore.execute(context);
      case 'review':
        return this.phases.review.execute(context);
      case 'implement':
        return this.phases.implement.execute(context);
      case 'qa':
        return this.phases.qa.execute(context);
    }
  }

  /**
   * 跳过阶段
   */
  async skipPhase(phase: ExecutionPhase, reason: string): Promise<void> {
    if (!this.state) return;

    const skipResult: PhaseResult = {
      phase,
      status: 'skipped',
      startTime: Date.now(),
      endTime: Date.now(),
      skippedReason: reason,
    };

    this.state.phaseResults.push(skipResult);
    this.emit('phase:skipped', skipResult);
  }

  /**
   * 判断是否跳过 explore 阶段
   */
  private shouldSkipExplore(context: ExecutionContext): boolean {
    if (this.config.autoSkipExplore) return true;
    return !this.phases.explore.shouldExploreInParallel(context);
  }

  /**
   * 恢复执行
   */
  async resume(context: ExecutionContext): Promise<ExecutionLoopState> {
    if (!this.state || this.state.status !== 'paused') {
      throw new Error('No paused execution to resume');
    }

    this.state.status = 'running';
    this.emit('loop:resumed', { sessionId: context.sessionId });

    // 从当前阶段继续
    const phaseOrder: ExecutionPhase[] = ['research', 'explore', 'review', 'implement', 'qa'];
    const currentIndex = phaseOrder.indexOf(this.state.currentPhase);

    for (let i = currentIndex + 1; i < phaseOrder.length; i++) {
      const phase = phaseOrder[i];
      this.state.currentPhase = phase;

      const result = await this.executePhase(phase, context);
      this.state.phaseResults.push(result);
      context.previousResults.set(phase, result);

      if (result.status === 'failed') {
        this.state.status = 'failed';
        this.state.endTime = Date.now();
        await this.persistState();
        return this.state;
      }
    }

    this.state.status = 'completed';
    this.state.endTime = Date.now();
    await this.persistState();

    return this.state;
  }

  /**
   * 持久化状态
   */
  private async persistState(): Promise<void> {
    if (!this.config.persistState || !this.state) return;

    const outputDir = this.config.outputDir;
    if (!fs.existsSync(outputDir)) {
      fs.mkdirSync(outputDir, { recursive: true });
    }

    const statePath = path.join(outputDir, `${this.state.sessionId}-state.json`);
    fs.writeFileSync(statePath, JSON.stringify(this.state, null, 2), 'utf-8');
  }

  /**
   * 加载状态
   */
  async loadState(sessionId: string): Promise<ExecutionLoopState | null> {
    const statePath = path.join(this.config.outputDir, `${sessionId}-state.json`);

    if (!fs.existsSync(statePath)) {
      return null;
    }

    const content = fs.readFileSync(statePath, 'utf-8');
    this.state = JSON.parse(content);
    return this.state!;
  }

  /**
   * 获取当前状态
   */
  getState(): ExecutionLoopState | undefined {
    return this.state;
  }

  /**
   * 获取阶段结果
   */
  getPhaseResult(phase: ExecutionPhase): PhaseResult | undefined {
    return this.state?.phaseResults.find(r => r.phase === phase);
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<ExecutionLoopConfig>): void {
    this.config = { ...this.config, ...config };
  }

  /**
   * 获取配置
   */
  getConfig(): ExecutionLoopConfig {
    return { ...this.config };
  }
}
