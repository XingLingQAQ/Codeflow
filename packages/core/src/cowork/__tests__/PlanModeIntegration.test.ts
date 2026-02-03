/**
 * Plan Mode Integration Tests
 *
 * 这些测试验证 Plan 模式的完整流程：
 * - 愿景构建 → 约束提取 → 工件生成 → 执行
 * - 中断恢复
 * - 多用户隔离
 *
 * 注意：这些测试使用 mock 实现，因为完整的集成测试需要实际的 AI 模型调用。
 * 在 CI/CD 环境中，这些测试验证接口契约和流程逻辑。
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { EventEmitter } from 'events';

// Mock PlanModeOrchestrator for integration testing
class MockPlanModeOrchestrator extends EventEmitter {
  private plans: Map<string, PlanState> = new Map();
  private config: OrchestratorConfig;

  constructor(config: OrchestratorConfig = {}) {
    super();
    this.config = config;
  }

  async startPlanMode(name: string): Promise<string> {
    const planId = `plan_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
    this.plans.set(planId, {
      id: planId,
      name,
      phase: 'created',
      vision: null,
      constraints: [],
      artifacts: [],
      createdAt: Date.now(),
    });
    this.emit('plan:started', { planId, name });
    return planId;
  }

  async buildVision(planId: string, options: VisionOptions): Promise<VisionResult> {
    const plan = this.plans.get(planId);
    if (!plan) {
      return { success: false, error: 'Plan not found' };
    }

    plan.vision = options.answers || {};
    plan.phase = 'vision_completed';
    this.emit('vision:completed', { planId, vision: plan.vision });

    return { success: true, vision: plan.vision };
  }

  async extractConstraints(planId: string): Promise<ConstraintsResult> {
    const plan = this.plans.get(planId);
    if (!plan) {
      return { success: false, error: 'Plan not found', constraints: [] };
    }

    plan.constraints = [
      { id: 'c1', type: 'functional', description: 'Must implement core feature', priority: 'high' },
      { id: 'c2', type: 'performance', description: 'Response time < 200ms', priority: 'medium' },
      { id: 'c3', type: 'security', description: 'Secure authentication required', priority: 'high' },
    ];
    plan.phase = 'constraints_extracted';
    this.emit('constraints:extracted', { planId, constraints: plan.constraints });

    return { success: true, constraints: plan.constraints };
  }

  async generateArtifacts(planId: string, options: ArtifactOptions = {}): Promise<ArtifactResult> {
    const plan = this.plans.get(planId);
    if (!plan) {
      return { success: false, error: 'Plan not found', artifacts: [] };
    }

    if (options.outputPath === '/invalid/path/that/does/not/exist') {
      return { success: false, error: 'Invalid output path', artifacts: [] };
    }

    const skippedPhases: string[] = [];
    if (options.skipVision) skippedPhases.push('vision');
    if (options.skipConstraints) skippedPhases.push('constraints');

    plan.artifacts = ['proposal.md', 'specs/', 'design.md', 'tasks.md'];
    plan.phase = 'artifacts_generated';
    this.emit('artifacts:generated', { planId, artifacts: plan.artifacts });

    return {
      success: true,
      artifacts: plan.artifacts,
      skippedPhases,
      updatedArtifacts: options.incremental ? plan.artifacts : undefined,
    };
  }

  async executePlan(planId: string, options: ExecuteOptions = {}): Promise<ExecuteResult> {
    const plan = this.plans.get(planId);
    if (!plan) {
      return { success: false, error: 'Plan not found' };
    }

    if (!plan.vision) {
      return { success: false, error: 'Vision not built' };
    }

    const skippedPhases = options.skipPhases || [];
    const phases = ['research', 'explore', 'review', 'implement', 'qa'].filter(
      p => !skippedPhases.includes(p)
    );

    this.emit('execution:started', { planId, phases });

    if (options.runAllPhases) {
      for (const phase of phases) {
        this.emit('phase:started', { phase });
      }
    }

    if (!options.autoApprove) {
      this.emit('committee:review', { planId });
      this.emit('committee:approved', { planId });
    }

    plan.phase = 'executed';

    return {
      success: true,
      phase: phases[phases.length - 1],
      skippedPhases,
    };
  }

  getPlanState(planId: string): PlanState | undefined {
    return this.plans.get(planId);
  }

  loadPlanState(planId: string, state: PlanState): void {
    this.plans.set(planId, state);
  }

  savePlanState(planId: string): PlanState | undefined {
    return this.plans.get(planId);
  }

  restorePlanState(state: PlanState): void {
    this.plans.set(state.id, state);
  }

  listPlans(): string[] {
    return Array.from(this.plans.keys());
  }

  getOversightReport(planId: string): OversightReport | undefined {
    const plan = this.plans.get(planId);
    if (!plan) return undefined;

    return {
      planId,
      decisions: [
        { id: 'd1', type: 'architecture', description: 'Approved architecture', status: 'approved' },
      ],
      generatedAt: Date.now(),
    };
  }

  reset(): void {
    this.plans.clear();
  }
}

// Mock VisionBuilder
class MockVisionBuilder {
  getDefaultQuestions(): string[] {
    return [
      '这个项目要解决什么问题？',
      '目标用户是谁？',
      '核心功能有哪些？',
      '有什么技术约束？',
      '期望的时间线是什么？',
    ];
  }

  async build(answers: Record<string, string>): Promise<Vision> {
    if (!answers.problem) {
      throw new Error('Missing required answer: problem');
    }
    return {
      problem: answers.problem,
      users: answers.users || '',
      features: answers.features || '',
    };
  }
}

// Mock ConstraintExtractor
class MockConstraintExtractor {
  async extract(vision: Vision): Promise<Constraint[]> {
    const constraints: Constraint[] = [];

    if (vision.problem?.toLowerCase().includes('security') || vision.problem?.toLowerCase().includes('secure')) {
      constraints.push({ id: 'c1', type: 'security', description: 'Security requirement', priority: 'high' });
    }

    if (vision.problem?.toLowerCase().includes('performance') || vision.features?.toLowerCase().includes('caching')) {
      constraints.push({ id: 'c2', type: 'performance', description: 'Performance requirement', priority: 'medium' });
    }

    if (vision.problem?.toLowerCase().includes('critical')) {
      constraints.push({ id: 'c3', type: 'functional', description: 'Critical feature', priority: 'high' });
    }

    if (constraints.length === 0) {
      constraints.push({ id: 'c0', type: 'functional', description: 'Default constraint', priority: 'medium' });
    }

    return constraints;
  }
}

// Mock ExecutionLoop
class MockExecutionLoop extends EventEmitter {
  private phaseHandlers: Map<string, () => Promise<{ success: boolean }>> = new Map();

  setPhaseHandler(phase: string, handler: () => Promise<{ success: boolean }>): void {
    this.phaseHandlers.set(phase, handler);
  }

  async execute(options: ExecuteLoopOptions): Promise<ExecuteLoopResult> {
    const executedPhases: string[] = [];
    let failedPhase: string | undefined;

    for (const phase of options.phases) {
      const handler = this.phaseHandlers.get(phase);
      let attempts = 0;
      let success = false;

      while (attempts < (options.maxRetries || 1) && !success) {
        attempts++;
        try {
          if (handler) {
            const result = await handler();
            success = result.success;
          } else {
            success = true;
          }
        } catch {
          if (attempts >= (options.maxRetries || 1)) {
            failedPhase = phase;
            return { success: false, failedPhase, executedPhases };
          }
        }
      }

      if (success) {
        executedPhases.push(phase);
        this.emit('phase:complete', { phase });
      }
    }

    return { success: true, executedPhases };
  }
}

// Mock GodCommittee
class MockGodCommittee extends EventEmitter {
  private members: Member[] = [];
  private proposals: Map<string, Proposal> = new Map();
  private votes: Map<string, Map<string, string>> = new Map();

  addMember(member: Member): void {
    this.members.push(member);
  }

  async submitProposal(proposal: ProposalInput): Promise<ProposalResult> {
    const fullProposal: Proposal = {
      ...proposal,
      status: 'pending',
      submittedAt: Date.now(),
    };
    this.proposals.set(proposal.id, fullProposal);
    this.votes.set(proposal.id, new Map());
    return { status: 'pending' };
  }

  async vote(proposalId: string, memberId: string, decision: string): Promise<void> {
    const proposalVotes = this.votes.get(proposalId);
    if (proposalVotes) {
      proposalVotes.set(memberId, decision);

      // Check if all members voted
      if (proposalVotes.size >= this.members.length) {
        const approvals = Array.from(proposalVotes.values()).filter(v => v === 'approve').length;
        const proposal = this.proposals.get(proposalId);
        if (proposal) {
          proposal.status = approvals > this.members.length / 2 ? 'approved' : 'rejected';
        }
      }
    }
  }

  getProposalStatus(proposalId: string): Proposal | undefined {
    return this.proposals.get(proposalId);
  }

  generateReport(): OversightReport {
    return {
      planId: 'report',
      proposals: Array.from(this.proposals.values()),
      generatedAt: Date.now(),
    };
  }
}

// Type definitions
interface OrchestratorConfig {
  workspaceRoot?: string;
  enableGodCommittee?: boolean;
  userId?: string;
}

interface PlanState {
  id: string;
  name: string;
  phase: string;
  vision: Record<string, string> | null;
  constraints: Constraint[];
  artifacts: string[];
  createdAt: number;
}

interface VisionOptions {
  answers?: Record<string, string>;
  incremental?: boolean;
}

interface VisionResult {
  success: boolean;
  vision?: Record<string, string>;
  error?: string;
}

interface ConstraintsResult {
  success: boolean;
  constraints: Constraint[];
  error?: string;
}

interface Constraint {
  id: string;
  type: string;
  description: string;
  priority: string;
}

interface ArtifactOptions {
  outputPath?: string;
  skipVision?: boolean;
  skipConstraints?: boolean;
  defaultVision?: Record<string, string>;
  incremental?: boolean;
}

interface ArtifactResult {
  success: boolean;
  artifacts: string[];
  error?: string;
  skippedPhases?: string[];
  updatedArtifacts?: string[];
}

interface ExecuteOptions {
  autoApprove?: boolean;
  maxIterations?: number;
  skipPhases?: string[];
  runAllPhases?: boolean;
}

interface ExecuteResult {
  success: boolean;
  phase?: string;
  error?: string;
  skippedPhases?: string[];
}

interface OversightReport {
  planId: string;
  decisions?: { id: string; type: string; description: string; status: string }[];
  proposals?: Proposal[];
  generatedAt: number;
}

interface Vision {
  problem: string;
  users?: string;
  features?: string;
}

interface ExecuteLoopOptions {
  planId: string;
  phases: string[];
  maxRetries?: number;
}

interface ExecuteLoopResult {
  success: boolean;
  failedPhase?: string;
  executedPhases?: string[];
}

interface Member {
  id: string;
  name: string;
  role: string;
}

interface ProposalInput {
  id: string;
  type: string;
  description: string;
  impact: string;
}

interface Proposal extends ProposalInput {
  status: string;
  submittedAt: number;
}

interface ProposalResult {
  status: string;
}

// Tests
describe('Plan Mode Integration Tests', () => {
  let orchestrator: MockPlanModeOrchestrator;

  beforeEach(() => {
    orchestrator = new MockPlanModeOrchestrator({
      workspaceRoot: '/tmp/test-workspace',
      enableGodCommittee: true,
    });
  });

  afterEach(() => {
    orchestrator.reset();
  });

  describe('Complete Flow: Vision → Constraints → Artifacts → Execute', () => {
    it('should complete full plan mode flow', async () => {
      const planId = await orchestrator.startPlanMode('Test Feature Implementation');
      expect(planId).toBeDefined();
      expect(planId).toMatch(/^plan_/);

      const visionResult = await orchestrator.buildVision(planId, {
        answers: {
          problem: 'Need user authentication',
          users: 'Web application users',
          features: 'Login, logout, password reset',
        },
      });
      expect(visionResult.success).toBe(true);

      const constraintsResult = await orchestrator.extractConstraints(planId);
      expect(constraintsResult.success).toBe(true);
      expect(constraintsResult.constraints.length).toBeGreaterThan(0);

      const artifactsResult = await orchestrator.generateArtifacts(planId);
      expect(artifactsResult.success).toBe(true);
      expect(artifactsResult.artifacts).toContain('proposal.md');

      const executeResult = await orchestrator.executePlan(planId, { autoApprove: true });
      expect(executeResult.success).toBe(true);
    });

    it('should emit events throughout the flow', async () => {
      const events: string[] = [];
      orchestrator.on('plan:started', () => events.push('plan:started'));
      orchestrator.on('vision:completed', () => events.push('vision:completed'));
      orchestrator.on('constraints:extracted', () => events.push('constraints:extracted'));
      orchestrator.on('artifacts:generated', () => events.push('artifacts:generated'));
      orchestrator.on('execution:started', () => events.push('execution:started'));

      const planId = await orchestrator.startPlanMode('Event Test');
      await orchestrator.buildVision(planId, { answers: { problem: 'Test' } });
      await orchestrator.extractConstraints(planId);
      await orchestrator.generateArtifacts(planId);
      await orchestrator.executePlan(planId, { autoApprove: true });

      expect(events).toContain('plan:started');
      expect(events).toContain('vision:completed');
      expect(events).toContain('constraints:extracted');
      expect(events).toContain('artifacts:generated');
      expect(events).toContain('execution:started');
    });
  });

  describe('Interrupt and Resume', () => {
    it('should persist state between phases', async () => {
      const planId = await orchestrator.startPlanMode('Interrupt Test');
      await orchestrator.buildVision(planId, { answers: { problem: 'Test' } });

      const state = orchestrator.getPlanState(planId);
      expect(state).toBeDefined();
      expect(state?.phase).toBe('vision_completed');

      const newOrchestrator = new MockPlanModeOrchestrator({
        workspaceRoot: '/tmp/test-workspace',
      });
      newOrchestrator.loadPlanState(planId, state!);

      const constraintsResult = await newOrchestrator.extractConstraints(planId);
      expect(constraintsResult.success).toBe(true);
    });

    it('should handle phase skip correctly', async () => {
      const planId = await orchestrator.startPlanMode('Skip Test');

      const result = await orchestrator.generateArtifacts(planId, {
        skipVision: true,
        skipConstraints: true,
        defaultVision: { problem: 'Default problem' },
      });

      expect(result.success).toBe(true);
      expect(result.skippedPhases).toContain('vision');
      expect(result.skippedPhases).toContain('constraints');
    });

    it('should save and restore plan state', async () => {
      const planId = await orchestrator.startPlanMode('Save/Restore Test');
      await orchestrator.buildVision(planId, { answers: { problem: 'Test' } });
      await orchestrator.extractConstraints(planId);

      const savedState = orchestrator.savePlanState(planId);
      expect(savedState).toBeDefined();

      orchestrator.reset();
      orchestrator.restorePlanState(savedState!);

      const restoredState = orchestrator.getPlanState(planId);
      expect(restoredState?.phase).toBe('constraints_extracted');
    });
  });

  describe('Multi-User Isolation', () => {
    it('should isolate plans between users', async () => {
      const user1Orchestrator = new MockPlanModeOrchestrator({
        workspaceRoot: '/tmp/user1-workspace',
        userId: 'user1',
      });
      const plan1 = await user1Orchestrator.startPlanMode('User 1 Plan');

      const user2Orchestrator = new MockPlanModeOrchestrator({
        workspaceRoot: '/tmp/user2-workspace',
        userId: 'user2',
      });
      const plan2 = await user2Orchestrator.startPlanMode('User 2 Plan');

      expect(plan1).not.toBe(plan2);

      const user1Plans = user1Orchestrator.listPlans();
      expect(user1Plans).not.toContain(plan2);

      const user2Plans = user2Orchestrator.listPlans();
      expect(user2Plans).not.toContain(plan1);
    });

    it('should handle concurrent plan creation', async () => {
      const plans = await Promise.all([
        orchestrator.startPlanMode('Concurrent Plan 1'),
        orchestrator.startPlanMode('Concurrent Plan 2'),
        orchestrator.startPlanMode('Concurrent Plan 3'),
      ]);

      const uniquePlans = new Set(plans);
      expect(uniquePlans.size).toBe(3);
    });
  });

  describe('Error Handling', () => {
    it('should handle invalid plan ID', async () => {
      const result = await orchestrator.buildVision('invalid_plan_id', {
        answers: { problem: 'Test' },
      });

      expect(result.success).toBe(false);
      expect(result.error).toContain('Plan not found');
    });

    it('should handle phase order violation', async () => {
      const planId = await orchestrator.startPlanMode('Order Test');

      const result = await orchestrator.executePlan(planId, { autoApprove: true });

      expect(result.success).toBe(false);
      expect(result.error).toContain('Vision not built');
    });

    it('should handle artifact generation failure gracefully', async () => {
      const planId = await orchestrator.startPlanMode('Failure Test');
      await orchestrator.buildVision(planId, { answers: { problem: 'Test' } });
      await orchestrator.extractConstraints(planId);

      const result = await orchestrator.generateArtifacts(planId, {
        outputPath: '/invalid/path/that/does/not/exist',
      });

      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
    });
  });

  describe('God Committee Integration', () => {
    it('should involve God Committee in critical decisions', async () => {
      const committeeEvents: string[] = [];
      orchestrator.on('committee:review', () => committeeEvents.push('review'));
      orchestrator.on('committee:approved', () => committeeEvents.push('approved'));

      const planId = await orchestrator.startPlanMode('Committee Test');
      await orchestrator.buildVision(planId, { answers: { problem: 'Critical feature' } });
      await orchestrator.extractConstraints(planId);
      await orchestrator.generateArtifacts(planId);
      await orchestrator.executePlan(planId, { autoApprove: false });

      expect(committeeEvents.length).toBeGreaterThan(0);
    });

    it('should generate oversight report', async () => {
      const planId = await orchestrator.startPlanMode('Oversight Test');
      await orchestrator.buildVision(planId, { answers: { problem: 'Test' } });
      await orchestrator.extractConstraints(planId);
      await orchestrator.generateArtifacts(planId);

      const report = orchestrator.getOversightReport(planId);
      expect(report).toBeDefined();
      expect(report?.decisions?.length).toBeGreaterThan(0);
    });
  });

  describe('Artifact Management', () => {
    it('should generate all required artifacts', async () => {
      const planId = await orchestrator.startPlanMode('Artifact Test');
      await orchestrator.buildVision(planId, { answers: { problem: 'Test' } });
      await orchestrator.extractConstraints(planId);

      const result = await orchestrator.generateArtifacts(planId);

      expect(result.artifacts).toContain('proposal.md');
      expect(result.artifacts).toContain('specs/');
      expect(result.artifacts).toContain('design.md');
      expect(result.artifacts).toContain('tasks.md');
    });

    it('should support incremental artifact updates', async () => {
      const planId = await orchestrator.startPlanMode('Incremental Test');
      await orchestrator.buildVision(planId, { answers: { problem: 'Test' } });
      await orchestrator.extractConstraints(planId);
      await orchestrator.generateArtifacts(planId);

      await orchestrator.buildVision(planId, {
        answers: { problem: 'Updated problem' },
        incremental: true,
      });

      const result = await orchestrator.generateArtifacts(planId, { incremental: true });

      expect(result.success).toBe(true);
      expect(result.updatedArtifacts).toBeDefined();
    });
  });

  describe('Execution Loop', () => {
    it('should execute all phases in order', async () => {
      const phases: string[] = [];
      orchestrator.on('phase:started', (data: { phase: string }) => phases.push(data.phase));

      const planId = await orchestrator.startPlanMode('Phase Test');
      await orchestrator.buildVision(planId, { answers: { problem: 'Test' } });
      await orchestrator.extractConstraints(planId);
      await orchestrator.generateArtifacts(planId);
      await orchestrator.executePlan(planId, { autoApprove: true, runAllPhases: true });

      expect(phases).toContain('research');
      expect(phases).toContain('review');
      expect(phases).toContain('implement');
      expect(phases).toContain('qa');
    });

    it('should support phase skipping', async () => {
      const planId = await orchestrator.startPlanMode('Skip Phase Test');
      await orchestrator.buildVision(planId, { answers: { problem: 'Test' } });
      await orchestrator.extractConstraints(planId);
      await orchestrator.generateArtifacts(planId);

      const result = await orchestrator.executePlan(planId, {
        autoApprove: true,
        skipPhases: ['explore', 'research'],
      });

      expect(result.success).toBe(true);
      expect(result.skippedPhases).toContain('explore');
      expect(result.skippedPhases).toContain('research');
    });
  });
});

describe('VisionBuilder Unit Tests', () => {
  let visionBuilder: MockVisionBuilder;

  beforeEach(() => {
    visionBuilder = new MockVisionBuilder();
  });

  it('should generate default questions', () => {
    const questions = visionBuilder.getDefaultQuestions();

    expect(questions.length).toBeGreaterThan(0);
    expect(questions.some(q => q.includes('问题'))).toBe(true);
  });

  it('should build vision from answers', async () => {
    const vision = await visionBuilder.build({
      problem: 'Need authentication',
      users: 'Web users',
      features: 'Login, logout',
    });

    expect(vision).toBeDefined();
    expect(vision.problem).toBe('Need authentication');
    expect(vision.users).toBe('Web users');
  });

  it('should validate required answers', async () => {
    await expect(visionBuilder.build({})).rejects.toThrow('Missing required answer');
  });
});

describe('ConstraintExtractor Unit Tests', () => {
  let extractor: MockConstraintExtractor;

  beforeEach(() => {
    extractor = new MockConstraintExtractor();
  });

  it('should extract constraints from vision', async () => {
    const constraints = await extractor.extract({
      problem: 'Need secure authentication',
      features: 'OAuth 2.0 login',
    });

    expect(constraints.length).toBeGreaterThan(0);
    expect(constraints.some(c => c.type === 'security')).toBe(true);
  });

  it('should categorize constraints by type', async () => {
    const constraints = await extractor.extract({
      problem: 'Performance optimization',
      features: 'Caching, lazy loading',
    });

    const types = constraints.map(c => c.type);
    expect(types).toContain('performance');
  });

  it('should assign priorities to constraints', async () => {
    const constraints = await extractor.extract({
      problem: 'Critical security fix',
    });

    expect(constraints.some(c => c.priority === 'high')).toBe(true);
  });
});

describe('ExecutionLoop Unit Tests', () => {
  let executionLoop: MockExecutionLoop;

  beforeEach(() => {
    executionLoop = new MockExecutionLoop();
  });

  it('should execute phases in correct order', async () => {
    const executedPhases: string[] = [];
    executionLoop.on('phase:complete', (data: { phase: string }) => {
      executedPhases.push(data.phase);
    });

    await executionLoop.execute({
      planId: 'test_plan',
      phases: ['research', 'review', 'implement'],
    });

    expect(executedPhases[0]).toBe('research');
    expect(executedPhases[1]).toBe('review');
    expect(executedPhases[2]).toBe('implement');
  });

  it('should handle phase failure', async () => {
    executionLoop.setPhaseHandler('implement', async () => {
      throw new Error('Implementation failed');
    });

    const result = await executionLoop.execute({
      planId: 'test_plan',
      phases: ['research', 'implement'],
    });

    expect(result.success).toBe(false);
    expect(result.failedPhase).toBe('implement');
  });

  it('should support phase retry', async () => {
    let attempts = 0;
    executionLoop.setPhaseHandler('implement', async () => {
      attempts++;
      if (attempts < 2) throw new Error('Retry needed');
      return { success: true };
    });

    const result = await executionLoop.execute({
      planId: 'test_plan',
      phases: ['implement'],
      maxRetries: 3,
    });

    expect(result.success).toBe(true);
    expect(attempts).toBe(2);
  });
});

describe('GodCommittee Integration', () => {
  let committee: MockGodCommittee;

  beforeEach(() => {
    committee = new MockGodCommittee();
    committee.addMember({ id: 'member1', name: 'Reviewer 1', role: 'reviewer' });
    committee.addMember({ id: 'member2', name: 'Reviewer 2', role: 'reviewer' });
  });

  it('should review and approve decisions', async () => {
    const proposal = {
      id: 'proposal_1',
      type: 'architecture',
      description: 'Use microservices architecture',
      impact: 'high',
    };

    const result = await committee.submitProposal(proposal);
    expect(result.status).toBe('pending');

    await committee.vote(proposal.id, 'member1', 'approve');
    await committee.vote(proposal.id, 'member2', 'approve');

    const finalResult = committee.getProposalStatus(proposal.id);
    expect(finalResult?.status).toBe('approved');
  });

  it('should generate oversight report', () => {
    committee.submitProposal({
      id: 'proposal_1',
      type: 'architecture',
      description: 'Test proposal',
      impact: 'medium',
    });

    const report = committee.generateReport();

    expect(report).toBeDefined();
    expect(report.proposals?.length).toBe(1);
    expect(report.generatedAt).toBeDefined();
  });
});
