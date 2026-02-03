import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';
import {
  ExecutionLoop,
  ResearchPhase,
  ExplorePhase,
  ReviewPhase,
  ImplementPhase,
  QAPhase,
  ExecutionContext,
  ResearchOutput,
  ExploreOutput,
  ReviewOutput,
  ImplementOutput,
  QAOutput,
  PhaseExecutor,
} from '../ExecutionLoop.js';
import { VisionDocument, ConstraintSet, PRDItem } from '../types.js';

// Mock data
const createMockVision = (): VisionDocument => ({
  id: 'vision-1',
  title: 'Test Project',
  summary: 'A test project',
  goals: ['Goal 1', 'Goal 2'],
  scope: { included: ['Feature A'], excluded: [] },
  constraints: ['Constraint 1'],
  priorities: ['Priority 1'],
  risks: ['Risk 1'],
  answers: [],
  createdAt: Date.now(),
  updatedAt: Date.now(),
});

const createMockConstraints = (): ConstraintSet => ({
  id: 'constraints-1',
  visionId: 'vision-1',
  constraints: [
    { id: 'c1', type: 'technical', priority: 'must', description: 'Use TypeScript', verifiable: true },
  ],
  createdAt: Date.now(),
  updatedAt: Date.now(),
});

const createMockPRD = (): PRDItem => ({
  id: 'prd-1',
  title: 'Test PRD',
  description: 'Test PRD description',
  milestoneId: 'milestone-1',
  priority: 1,
  estimatedEffort: 'medium',
  status: 'ready',
  acceptanceCriteria: ['Criteria 1', 'Criteria 2'],
});

const createMockContext = (): ExecutionContext => ({
  sessionId: `session-${Date.now()}`,
  prdItem: createMockPRD(),
  vision: createMockVision(),
  constraints: createMockConstraints(),
  previousResults: new Map(),
});

describe('ResearchPhase', () => {
  let phase: ResearchPhase;

  beforeEach(() => {
    phase = new ResearchPhase();
  });

  it('should execute research phase', async () => {
    const context = createMockContext();
    const result = await phase.execute(context);

    expect(result.phase).toBe('research');
    expect(result.status).toBe('completed');
    expect(result.output).toBeDefined();
  });

  it('should emit phase events', async () => {
    const startListener = vi.fn();
    const completeListener = vi.fn();
    phase.on('phase:start', startListener);
    phase.on('phase:complete', completeListener);

    await phase.execute(createMockContext());

    expect(startListener).toHaveBeenCalled();
    expect(completeListener).toHaveBeenCalled();
  });

  it('should use custom executor', async () => {
    const customOutput: ResearchOutput = {
      findings: ['Custom finding'],
      recommendations: ['Custom recommendation'],
      risks: [],
      dependencies: [],
    };

    const executor: PhaseExecutor<ResearchOutput> = vi.fn().mockResolvedValue(customOutput);
    const customPhase = new ResearchPhase(executor);

    const result = await customPhase.execute(createMockContext());

    expect(executor).toHaveBeenCalled();
    expect((result.output as ResearchOutput).findings[0]).toBe('Custom finding');
  });

  it('should handle errors', async () => {
    const executor: PhaseExecutor<ResearchOutput> = vi.fn().mockRejectedValue(new Error('Test error'));
    const errorPhase = new ResearchPhase(executor);

    const errorListener = vi.fn();
    errorPhase.on('phase:error', errorListener);

    const result = await errorPhase.execute(createMockContext());

    expect(result.status).toBe('failed');
    expect(result.error).toBe('Test error');
    expect(errorListener).toHaveBeenCalled();
  });
});

describe('ExplorePhase', () => {
  let phase: ExplorePhase;

  beforeEach(() => {
    phase = new ExplorePhase();
  });

  it('should execute explore phase', async () => {
    const result = await phase.execute(createMockContext());

    expect(result.phase).toBe('explore');
    expect(result.status).toBe('completed');
    expect((result.output as ExploreOutput).approaches.length).toBeGreaterThan(0);
  });

  it('should determine if parallel exploration is needed', () => {
    const simpleContext = createMockContext();
    expect(phase.shouldExploreInParallel(simpleContext)).toBe(false);

    const complexContext: ExecutionContext = {
      ...createMockContext(),
      vision: {
        ...createMockVision(),
        risks: ['Risk 1', 'Risk 2', 'Risk 3'],
        goals: ['G1', 'G2', 'G3', 'G4'],
      },
      prdItem: { ...createMockPRD(), estimatedEffort: 'large' },
    };
    expect(phase.shouldExploreInParallel(complexContext)).toBe(true);
  });
});

describe('ReviewPhase', () => {
  let phase: ReviewPhase;

  beforeEach(() => {
    phase = new ReviewPhase();
  });

  it('should execute review phase', async () => {
    const result = await phase.execute(createMockContext());

    expect(result.phase).toBe('review');
    expect(result.status).toBe('completed');
    expect((result.output as ReviewOutput).approved).toBe(true);
  });

  it('should use custom executor for review', async () => {
    const customOutput: ReviewOutput = {
      approved: false,
      feedback: ['Needs improvement'],
      requiredChanges: ['Change 1'],
      reviewers: ['Reviewer 1'],
    };

    const executor: PhaseExecutor<ReviewOutput> = vi.fn().mockResolvedValue(customOutput);
    const customPhase = new ReviewPhase(executor);

    const result = await customPhase.execute(createMockContext());

    expect((result.output as ReviewOutput).approved).toBe(false);
  });
});

describe('ImplementPhase', () => {
  let phase: ImplementPhase;

  beforeEach(() => {
    phase = new ImplementPhase();
  });

  it('should execute implement phase', async () => {
    const result = await phase.execute(createMockContext());

    expect(result.phase).toBe('implement');
    expect(result.status).toBe('completed');
  });
});

describe('QAPhase', () => {
  let phase: QAPhase;

  beforeEach(() => {
    phase = new QAPhase();
  });

  it('should execute QA phase', async () => {
    const result = await phase.execute(createMockContext());

    expect(result.phase).toBe('qa');
    expect(result.status).toBe('completed');
    expect((result.output as QAOutput).approved).toBe(true);
  });

  it('should report test results', async () => {
    const result = await phase.execute(createMockContext());
    const output = result.output as QAOutput;

    expect(output.testsRun).toBeGreaterThan(0);
    expect(output.testsPassed).toBeLessThanOrEqual(output.testsRun);
  });
});

describe('ExecutionLoop', () => {
  let loop: ExecutionLoop;
  let testDir: string;

  beforeEach(() => {
    testDir = path.join(os.tmpdir(), `exec-loop-test-${Date.now()}`);
    fs.mkdirSync(testDir, { recursive: true });
    loop = new ExecutionLoop({ outputDir: testDir, persistState: true });
  });

  afterEach(() => {
    try {
      fs.rmSync(testDir, { recursive: true, force: true });
    } catch {
      // Ignore cleanup errors
    }
  });

  describe('execute', () => {
    it('should execute full loop', async () => {
      const context = createMockContext();
      const state = await loop.execute(context);

      expect(state.status).toBe('completed');
      expect(state.phaseResults.length).toBeGreaterThanOrEqual(4); // At least 4 phases (explore may be skipped)
    });

    it('should emit loop events', async () => {
      const startListener = vi.fn();
      const completeListener = vi.fn();
      loop.on('loop:start', startListener);
      loop.on('loop:complete', completeListener);

      await loop.execute(createMockContext());

      expect(startListener).toHaveBeenCalled();
      expect(completeListener).toHaveBeenCalled();
    });

    it('should skip explore phase when configured', async () => {
      const skipLoop = new ExecutionLoop({ outputDir: testDir, autoSkipExplore: true });
      const state = await skipLoop.execute(createMockContext());

      const exploreResult = state.phaseResults.find(r => r.phase === 'explore');
      expect(exploreResult?.status).toBe('skipped');
    });

    it('should persist state', async () => {
      const context = createMockContext();
      await loop.execute(context);

      const statePath = path.join(testDir, `${context.sessionId}-state.json`);
      expect(fs.existsSync(statePath)).toBe(true);
    });

    it('should handle phase failure', async () => {
      const failingExecutor: PhaseExecutor<ResearchOutput> = vi.fn().mockRejectedValue(new Error('Fail'));
      const failLoop = new ExecutionLoop(
        { outputDir: testDir },
        { research: failingExecutor }
      );

      const failedListener = vi.fn();
      failLoop.on('loop:failed', failedListener);

      const state = await failLoop.execute(createMockContext());

      expect(state.status).toBe('failed');
      expect(failedListener).toHaveBeenCalled();
    });

    it('should pause when review not approved', async () => {
      const rejectingReview: PhaseExecutor<ReviewOutput> = vi.fn().mockResolvedValue({
        approved: false,
        feedback: ['Not ready'],
        requiredChanges: ['Fix issues'],
        reviewers: ['Reviewer'],
      });

      const pauseLoop = new ExecutionLoop(
        { outputDir: testDir, requireReviewApproval: true },
        { review: rejectingReview }
      );

      const pausedListener = vi.fn();
      pauseLoop.on('loop:paused', pausedListener);

      const state = await pauseLoop.execute(createMockContext());

      expect(state.status).toBe('paused');
      expect(pausedListener).toHaveBeenCalled();
    });
  });

  describe('executePhase', () => {
    it('should execute individual phases', async () => {
      const context = createMockContext();

      const researchResult = await loop.executePhase('research', context);
      expect(researchResult.phase).toBe('research');

      const reviewResult = await loop.executePhase('review', context);
      expect(reviewResult.phase).toBe('review');
    });
  });

  describe('skipPhase', () => {
    it('should skip a phase with reason', async () => {
      const context = createMockContext();
      await loop.execute(context);

      const skippedListener = vi.fn();
      loop.on('phase:skipped', skippedListener);

      await loop.skipPhase('explore', 'Manual skip');

      expect(skippedListener).toHaveBeenCalled();
    });
  });

  describe('loadState', () => {
    it('should load persisted state', async () => {
      const context = createMockContext();
      await loop.execute(context);

      const newLoop = new ExecutionLoop({ outputDir: testDir });
      const loadedState = await newLoop.loadState(context.sessionId);

      expect(loadedState).toBeDefined();
      expect(loadedState?.sessionId).toBe(context.sessionId);
    });

    it('should return null for non-existent state', async () => {
      const state = await loop.loadState('non-existent');
      expect(state).toBeNull();
    });
  });

  describe('resume', () => {
    it('should resume paused execution', async () => {
      const rejectOnce = vi.fn()
        .mockResolvedValueOnce({
          approved: false,
          feedback: ['Not ready'],
          requiredChanges: [],
          reviewers: [],
        })
        .mockResolvedValueOnce({
          approved: true,
          feedback: ['Good'],
          requiredChanges: [],
          reviewers: [],
        });

      const pauseLoop = new ExecutionLoop(
        { outputDir: testDir, requireReviewApproval: true },
        { review: rejectOnce }
      );

      const context = createMockContext();
      let state = await pauseLoop.execute(context);
      expect(state.status).toBe('paused');

      // Resume with approved review
      state = await pauseLoop.resume(context);
      expect(state.status).toBe('completed');
    });

    it('should throw when no paused execution', async () => {
      await expect(loop.resume(createMockContext())).rejects.toThrow('No paused execution');
    });
  });

  describe('getters', () => {
    it('should get current state', async () => {
      expect(loop.getState()).toBeUndefined();

      await loop.execute(createMockContext());
      expect(loop.getState()).toBeDefined();
    });

    it('should get phase result', async () => {
      await loop.execute(createMockContext());

      const researchResult = loop.getPhaseResult('research');
      expect(researchResult).toBeDefined();
      expect(researchResult?.phase).toBe('research');
    });
  });

  describe('configuration', () => {
    it('should update config', () => {
      loop.updateConfig({ maxQAIterations: 5 });
      const config = loop.getConfig();
      expect(config.maxQAIterations).toBe(5);
    });

    it('should use phase models from config', () => {
      const modelLoop = new ExecutionLoop({
        outputDir: testDir,
        phaseModels: {
          research: 'claude-sonnet',
          implement: 'claude-haiku',
        },
      });

      const config = modelLoop.getConfig();
      expect(config.phaseModels.research).toBe('claude-sonnet');
      expect(config.phaseModels.implement).toBe('claude-haiku');
    });
  });

  describe('QA iterations', () => {
    it('should retry QA up to max iterations', async () => {
      let qaCallCount = 0;
      const failingQA: PhaseExecutor<QAOutput> = vi.fn().mockImplementation(() => {
        qaCallCount++;
        return Promise.resolve({
          testsRun: 10,
          testsPassed: qaCallCount >= 2 ? 10 : 5,
          testsFailed: qaCallCount >= 2 ? 0 : 5,
          issues: [],
          approved: qaCallCount >= 2,
        });
      });

      const retryLoop = new ExecutionLoop(
        { outputDir: testDir, maxQAIterations: 3 },
        { qa: failingQA }
      );

      const state = await retryLoop.execute(createMockContext());

      expect(state.status).toBe('completed');
      expect(state.qaIterations).toBeGreaterThanOrEqual(1);
    });
  });
});
