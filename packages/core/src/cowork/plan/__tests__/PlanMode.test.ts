import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import * as path from 'path';
import * as fs from 'fs';
import * as os from 'os';
import {
  PlanModeOrchestrator,
  VisionBuilder,
  ConstraintExtractor,
  PlanArtifactManager,
  VisionDocument,
  ConstraintSet,
  PlanSession,
} from '../index.js';

describe('VisionBuilder', () => {
  let builder: VisionBuilder;

  beforeEach(() => {
    builder = new VisionBuilder();
  });

  describe('start', () => {
    it('should start vision building and return first question', () => {
      const question = builder.start();
      expect(question).toBeDefined();
      expect(question?.category).toBe('goal');
      expect(builder.building).toBe(true);
    });
  });

  describe('submitAnswer', () => {
    it('should accept answer and return next question', () => {
      builder.start();
      const nextQuestion = builder.submitAnswer('Build a new feature');
      expect(nextQuestion).toBeDefined();
      expect(builder.getAnswers().length).toBe(1);
    });

    it('should add follow-up questions when answer is provided', () => {
      builder.start();
      const initialProgress = builder.getProgress();
      builder.submitAnswer('Build a new feature');
      const newProgress = builder.getProgress();
      expect(newProgress.total).toBeGreaterThanOrEqual(initialProgress.total);
    });
  });

  describe('skipQuestion', () => {
    it('should skip optional questions', () => {
      builder.start();
      // Answer required questions first
      builder.submitAnswer('Goal 1');
      builder.submitAnswer('Success criteria');
      builder.submitAnswer('Included functionality');
      // Now we should be at an optional question
      const question = builder.getCurrentQuestion();
      if (question && !question.required) {
        const next = builder.skipQuestion();
        expect(next).toBeDefined();
      }
    });

    it('should throw error when skipping required question', () => {
      builder.start();
      expect(() => builder.skipQuestion()).toThrow('Cannot skip required question');
    });
  });

  describe('canComplete', () => {
    it('should return false when required questions not answered', () => {
      builder.start();
      expect(builder.canComplete()).toBe(false);
    });

    it('should return true when all required questions answered', () => {
      builder.start();
      // Answer all required questions
      while (builder.getCurrentQuestion()) {
        const q = builder.getCurrentQuestion();
        if (q?.required) {
          builder.submitAnswer('Test answer');
        } else {
          try {
            builder.skipQuestion();
          } catch {
            builder.submitAnswer('Test answer');
          }
        }
      }
      expect(builder.canComplete()).toBe(true);
    });
  });

  describe('complete', () => {
    it('should create vision document when requirements met', () => {
      builder.start();
      // Answer all questions
      while (builder.getCurrentQuestion()) {
        const q = builder.getCurrentQuestion();
        if (q?.required) {
          builder.submitAnswer('Test answer for ' + q.category);
        } else {
          try {
            builder.skipQuestion();
          } catch {
            builder.submitAnswer('Optional answer');
          }
        }
      }

      const vision = builder.complete('Test Vision');
      expect(vision).toBeDefined();
      expect(vision.title).toBe('Test Vision');
      expect(vision.goals.length).toBeGreaterThan(0);
      expect(builder.building).toBe(false);
    });

    it('should throw error when requirements not met', () => {
      builder.start();
      expect(() => builder.complete('Test')).toThrow('Cannot complete');
    });
  });

  describe('getProgress', () => {
    it('should return correct progress', () => {
      builder.start();
      const progress = builder.getProgress();
      expect(progress.current).toBe(0);
      expect(progress.total).toBeGreaterThan(0);
      expect(progress.percentage).toBe(0);

      builder.submitAnswer('Answer');
      const newProgress = builder.getProgress();
      expect(newProgress.current).toBe(1);
      expect(newProgress.percentage).toBeGreaterThan(0);
    });
  });

  describe('reset', () => {
    it('should reset builder state', () => {
      builder.start();
      builder.submitAnswer('Answer');
      builder.reset();
      expect(builder.building).toBe(false);
      expect(builder.getAnswers().length).toBe(0);
    });
  });

  describe('language support', () => {
    it('should support Chinese questions', () => {
      const zhBuilder = new VisionBuilder({ language: 'zh' });
      const question = zhBuilder.start();
      expect(question?.question).toContain('目标');
    });
  });
});

describe('ConstraintExtractor', () => {
  let extractor: ConstraintExtractor;
  let mockVision: VisionDocument;

  beforeEach(() => {
    extractor = new ConstraintExtractor();
    mockVision = {
      id: 'vision-1',
      title: 'Test Vision',
      summary: 'Test summary',
      goals: [
        'Must support 1000 concurrent users',
        'Should integrate with existing API',
      ],
      scope: {
        included: ['User authentication', 'Data validation'],
        excluded: ['Mobile app support'],
      },
      constraints: ['Use TypeScript', 'Response time < 100ms'],
      priorities: ['Must have: Core features', 'Nice to have: Analytics'],
      risks: ['Security vulnerabilities', 'Performance degradation'],
      answers: [],
      createdAt: Date.now(),
      updatedAt: Date.now(),
    };
  });

  describe('extract', () => {
    it('should extract constraints from vision', () => {
      const constraints = extractor.extract(mockVision);
      expect(constraints).toBeDefined();
      expect(constraints.visionId).toBe(mockVision.id);
      expect(constraints.constraints.length).toBeGreaterThan(0);
    });

    it('should identify constraint types', () => {
      const constraints = extractor.extract(mockVision);
      const types = new Set(constraints.constraints.map(c => c.type));
      expect(types.size).toBeGreaterThan(1);
    });

    it('should identify priorities', () => {
      const constraints = extractor.extract(mockVision);
      const mustConstraints = constraints.constraints.filter(c => c.priority === 'must');
      expect(mustConstraints.length).toBeGreaterThan(0);
    });

    it('should mark excluded scope as wont', () => {
      const constraints = extractor.extract(mockVision);
      const wontConstraints = constraints.constraints.filter(c => c.priority === 'wont');
      expect(wontConstraints.length).toBeGreaterThan(0);
    });

    it('should identify verifiable constraints', () => {
      const constraints = extractor.extract(mockVision);
      const verifiable = constraints.constraints.filter(c => c.verifiable);
      expect(verifiable.length).toBeGreaterThan(0);
    });
  });

  describe('addConstraint', () => {
    it('should add manual constraint', () => {
      const constraints = extractor.extract(mockVision);
      const initialCount = constraints.constraints.length;

      const newConstraint = extractor.addConstraint(constraints, {
        type: 'security',
        priority: 'must',
        description: 'All data must be encrypted',
        verifiable: true,
      });

      expect(constraints.constraints.length).toBe(initialCount + 1);
      expect(newConstraint.id).toContain('manual');
    });
  });

  describe('removeConstraint', () => {
    it('should remove constraint', () => {
      const constraints = extractor.extract(mockVision);
      const firstId = constraints.constraints[0].id;
      const initialCount = constraints.constraints.length;

      const result = extractor.removeConstraint(constraints, firstId);
      expect(result).toBe(true);
      expect(constraints.constraints.length).toBe(initialCount - 1);
    });

    it('should return false for non-existent constraint', () => {
      const constraints = extractor.extract(mockVision);
      const result = extractor.removeConstraint(constraints, 'non-existent');
      expect(result).toBe(false);
    });
  });

  describe('filterByType', () => {
    it('should filter constraints by type', () => {
      const constraints = extractor.extract(mockVision);
      const technical = extractor.filterByType(constraints, 'technical');
      expect(technical.every(c => c.type === 'technical')).toBe(true);
    });
  });

  describe('filterByPriority', () => {
    it('should filter constraints by priority', () => {
      const constraints = extractor.extract(mockVision);
      const must = extractor.filterByPriority(constraints, 'must');
      expect(must.every(c => c.priority === 'must')).toBe(true);
    });
  });

  describe('validate', () => {
    it('should validate constraint set', () => {
      const constraints = extractor.extract(mockVision);
      const result = extractor.validate(constraints);
      expect(result.valid).toBe(true);
      expect(result.issues.length).toBe(0);
    });
  });
});

describe('PlanArtifactManager', () => {
  let manager: PlanArtifactManager;
  let testDir: string;
  let mockVision: VisionDocument;
  let mockConstraints: ConstraintSet;

  beforeEach(async () => {
    testDir = path.join(os.tmpdir(), `artifact-test-${Date.now()}`);
    fs.mkdirSync(testDir, { recursive: true });

    manager = new PlanArtifactManager(testDir);
    await manager.initialize();

    mockVision = {
      id: 'vision-1',
      title: 'Test Feature',
      summary: 'Implement a new feature',
      goals: ['Goal 1', 'Goal 2'],
      scope: { included: ['Feature A'], excluded: ['Feature B'] },
      constraints: ['Use TypeScript'],
      priorities: ['High priority items'],
      risks: ['Risk 1'],
      answers: [],
      createdAt: Date.now(),
      updatedAt: Date.now(),
    };

    mockConstraints = {
      id: 'constraints-1',
      visionId: 'vision-1',
      constraints: [
        { id: 'c1', type: 'functional', priority: 'must', description: 'Must work', verifiable: true },
        { id: 'c2', type: 'technical', priority: 'should', description: 'Use TS', verifiable: false },
      ],
      createdAt: Date.now(),
      updatedAt: Date.now(),
    };
  });

  afterEach(async () => {
    await manager.cleanup();
    try {
      fs.rmSync(testDir, { recursive: true, force: true });
    } catch {
      // Ignore cleanup errors
    }
  });

  describe('initialize', () => {
    it('should create output directories', () => {
      const outputPath = manager.getOutputPath();
      expect(fs.existsSync(outputPath)).toBe(true);
      expect(fs.existsSync(path.join(outputPath, 'specs'))).toBe(true);
      expect(fs.existsSync(path.join(outputPath, 'backups'))).toBe(true);
    });
  });

  describe('createProposal', () => {
    it('should create proposal artifact', async () => {
      const proposal = await manager.createProposal(mockVision, mockConstraints);
      expect(proposal).toBeDefined();
      expect(proposal.type).toBe('proposal');
      expect(proposal.title).toBe(mockVision.title);
      expect(fs.existsSync(proposal.path)).toBe(true);
    });

    it('should generate markdown content', async () => {
      const proposal = await manager.createProposal(mockVision, mockConstraints);
      const content = await manager.readArtifact(proposal.id);
      expect(content).toContain('# Test Feature');
      expect(content).toContain('## Why');
      expect(content).toContain('## What');
    });
  });

  describe('createSpec', () => {
    it('should create spec artifact', async () => {
      const spec = await manager.createSpec(
        'User Login',
        'Users should be able to log in',
        [{ name: 'Valid login', given: 'Valid credentials', when: 'User submits', then: 'User is logged in' }]
      );
      expect(spec).toBeDefined();
      expect(spec.type).toBe('spec');
      expect(fs.existsSync(spec.path)).toBe(true);
    });
  });

  describe('createDesign', () => {
    it('should create design artifact', async () => {
      const design = await manager.createDesign(
        'System Design',
        'Overview of the system',
        [{ name: 'AuthService', responsibility: 'Handle auth', dependencies: [] }],
        [{ name: 'IAuthService', methods: ['login()', 'logout()'], description: 'Auth interface' }]
      );
      expect(design).toBeDefined();
      expect(design.type).toBe('design');
      expect(fs.existsSync(design.path)).toBe(true);
    });
  });

  describe('createTasks', () => {
    it('should create tasks artifact', async () => {
      const tasks = await manager.createTasks(
        'Implementation Tasks',
        [
          { title: 'Task 1', description: 'Do something', priority: 'high' },
          { title: 'Task 2', description: 'Do another thing', priority: 'medium' },
        ]
      );
      expect(tasks).toBeDefined();
      expect(tasks.type).toBe('tasks');
      expect(fs.existsSync(tasks.path)).toBe(true);
    });
  });

  describe('getArtifact', () => {
    it('should retrieve artifact by id', async () => {
      const proposal = await manager.createProposal(mockVision, mockConstraints);
      const retrieved = manager.getArtifact(proposal.id);
      expect(retrieved).toBeDefined();
      expect(retrieved?.id).toBe(proposal.id);
    });
  });

  describe('getAllArtifacts', () => {
    it('should return all artifacts', async () => {
      await manager.createProposal(mockVision, mockConstraints);
      await manager.createSpec('Spec', 'Req', []);
      const all = manager.getAllArtifacts();
      expect(all.length).toBe(2);
    });
  });

  describe('updateStatus', () => {
    it('should update artifact status', async () => {
      const proposal = await manager.createProposal(mockVision, mockConstraints);
      const result = await manager.updateStatus(proposal.id, 'approved');
      expect(result).toBe(true);
      expect(manager.getArtifact(proposal.id)?.status).toBe('approved');
    });
  });
});

describe('PlanModeOrchestrator', () => {
  let orchestrator: PlanModeOrchestrator;
  let testDir: string;

  beforeEach(async () => {
    testDir = path.join(os.tmpdir(), `plan-test-${Date.now()}`);
    fs.mkdirSync(testDir, { recursive: true });
    orchestrator = new PlanModeOrchestrator(testDir);
  });

  afterEach(async () => {
    await orchestrator.cleanup();
    try {
      fs.rmSync(testDir, { recursive: true, force: true });
    } catch {
      // Ignore cleanup errors
    }
  });

  describe('startPlanMode', () => {
    it('should start plan mode and return session', async () => {
      const session = await orchestrator.startPlanMode('Test Plan');
      expect(session).toBeDefined();
      expect(session.name).toBe('Test Plan');
      expect(session.status).toBe('running');
      expect(session.currentPhase).toBe('vision');
    });

    it('should emit phase:start event', async () => {
      const listener = vi.fn();
      orchestrator.addListener(listener);
      await orchestrator.startPlanMode('Test Plan');
      expect(listener).toHaveBeenCalledWith(
        expect.objectContaining({ type: 'phase:start', phase: 'vision' })
      );
    });
  });

  describe('vision building', () => {
    it('should get current vision question', async () => {
      await orchestrator.startPlanMode('Test Plan');
      const question = orchestrator.getCurrentVisionQuestion();
      expect(question).toBeDefined();
    });

    it('should submit vision answer', async () => {
      await orchestrator.startPlanMode('Test Plan');
      const next = orchestrator.submitVisionAnswer('Test answer');
      expect(next).toBeDefined();
    });
  });

  describe('completeVision', () => {
    it('should complete vision and advance to constraints', async () => {
      await orchestrator.startPlanMode('Test Plan');

      // Answer all questions
      while (orchestrator.getCurrentVisionQuestion()) {
        const q = orchestrator.getCurrentVisionQuestion();
        if (q?.required) {
          orchestrator.submitVisionAnswer('Test answer');
        } else {
          try {
            orchestrator.skipVisionQuestion();
          } catch {
            orchestrator.submitVisionAnswer('Optional');
          }
        }
      }

      const vision = await orchestrator.completeVision('Test Vision');
      expect(vision).toBeDefined();
      expect(orchestrator.getCurrentSession()?.vision).toBeDefined();
    });
  });

  describe('advanceToConstraints', () => {
    it('should extract constraints from vision', async () => {
      await orchestrator.startPlanMode('Test Plan');

      // Complete vision first
      while (orchestrator.getCurrentVisionQuestion()) {
        const q = orchestrator.getCurrentVisionQuestion();
        if (q?.required) {
          orchestrator.submitVisionAnswer('Test answer');
        } else {
          try {
            orchestrator.skipVisionQuestion();
          } catch {
            orchestrator.submitVisionAnswer('Optional');
          }
        }
      }
      await orchestrator.completeVision('Test Vision');

      const constraints = await orchestrator.advanceToConstraints();
      expect(constraints).toBeDefined();
      expect(constraints.constraints.length).toBeGreaterThan(0);
    });
  });

  describe('fastForward', () => {
    it('should generate all artifacts at once', async () => {
      await orchestrator.startPlanMode('Test Plan', { autoAdvance: false });

      const artifacts = await orchestrator.fastForward(
        'Test Feature',
        [{ name: 'Spec 1', requirement: 'Req 1', scenarios: [] }],
        {
          overview: 'Design overview',
          components: [{ name: 'Component', responsibility: 'Do stuff', dependencies: [] }],
          interfaces: [],
        },
        [{ title: 'Task 1', description: 'Do task', priority: 'high' }]
      );

      expect(artifacts.length).toBeGreaterThan(0);
      expect(artifacts.some(a => a.type === 'proposal')).toBe(true);
      expect(artifacts.some(a => a.type === 'spec')).toBe(true);
      expect(artifacts.some(a => a.type === 'design')).toBe(true);
      expect(artifacts.some(a => a.type === 'tasks')).toBe(true);
    });
  });

  describe('completePlanMode', () => {
    it('should complete plan mode', async () => {
      await orchestrator.startPlanMode('Test Plan');

      // Fast forward to completion
      await orchestrator.fastForward('Test');

      const session = await orchestrator.completePlanMode();
      expect(session.status).toBe('completed');
      expect(session.currentPhase).toBe('completed');
      expect(orchestrator.getCurrentSession()).toBeNull();
    });
  });

  describe('pausePlanMode', () => {
    it('should pause plan mode', async () => {
      await orchestrator.startPlanMode('Test Plan');
      orchestrator.pausePlanMode();
      expect(orchestrator.getCurrentSession()?.status).toBe('paused');
    });
  });

  describe('resumePlanMode', () => {
    it('should resume plan mode', async () => {
      await orchestrator.startPlanMode('Test Plan');
      orchestrator.pausePlanMode();
      orchestrator.resumePlanMode();
      expect(orchestrator.getCurrentSession()?.status).toBe('running');
    });
  });

  describe('cancelPlanMode', () => {
    it('should cancel plan mode', async () => {
      await orchestrator.startPlanMode('Test Plan');
      orchestrator.cancelPlanMode();
      expect(orchestrator.getCurrentSession()).toBeNull();
    });
  });

  describe('getters', () => {
    it('should return sub-components', async () => {
      await orchestrator.startPlanMode('Test Plan');
      expect(orchestrator.getVisionBuilder()).toBeDefined();
      expect(orchestrator.getConstraintExtractor()).toBeDefined();
      expect(orchestrator.getArtifactManager()).toBeDefined();
    });

    it('should return all sessions', async () => {
      await orchestrator.startPlanMode('Plan 1');
      await orchestrator.completePlanMode();
      await orchestrator.startPlanMode('Plan 2');

      const sessions = orchestrator.getAllSessions();
      expect(sessions.length).toBe(2);
    });
  });
});
