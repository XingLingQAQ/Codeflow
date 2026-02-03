import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  SolutionEvaluator,
  EvaluationDimension,
  SolutionEvaluation,
  ComparisonReport,
  AIEvaluateCallback,
} from '../SolutionEvaluator.js';
import { WorkerResult } from '../ResultCollector.js';
import { Diff } from '../../types.js';

// Mock data
const createMockDiff = (file: string, additions: number, deletions: number, content: string = ''): Diff => ({
  file,
  hunks: [{ oldStart: 1, oldLines: deletions, newStart: 1, newLines: additions, content }],
  additions,
  deletions,
});

const createMockResult = (diffs: Diff[]): WorkerResult => ({
  taskId: 'task-1',
  success: true,
  duration: 1000,
  diffs,
});

describe('SolutionEvaluator', () => {
  let evaluator: SolutionEvaluator;

  beforeEach(() => {
    evaluator = new SolutionEvaluator();
  });

  describe('evaluate', () => {
    it('should evaluate a solution', async () => {
      const diffs = [
        createMockDiff('src/index.ts', 50, 10, 'function test() { return true; }'),
      ];
      const result = createMockResult(diffs);

      const evaluation = await evaluator.evaluate('worker-1', 'claude', 'claude-opus-4', result);

      expect(evaluation).toBeDefined();
      expect(evaluation.workerId).toBe('worker-1');
      expect(evaluation.workerName).toBe('claude');
      expect(evaluation.modelId).toBe('claude-opus-4');
      expect(evaluation.totalScore).toBeGreaterThan(0);
      expect(evaluation.scores.length).toBe(5); // 5 dimensions
    });

    it('should evaluate all dimensions', async () => {
      const diffs = [createMockDiff('src/index.ts', 30, 5)];
      const result = createMockResult(diffs);

      const evaluation = await evaluator.evaluate('worker-1', 'claude', 'claude-opus-4', result);

      const dimensions = evaluation.scores.map(s => s.dimension);
      expect(dimensions).toContain('quality');
      expect(dimensions).toContain('performance');
      expect(dimensions).toContain('maintainability');
      expect(dimensions).toContain('security');
      expect(dimensions).toContain('completeness');
    });

    it('should calculate code metrics', async () => {
      const diffs = [createMockDiff('src/index.ts', 100, 20)];
      const result = createMockResult(diffs);

      const evaluation = await evaluator.evaluate('worker-1', 'claude', 'claude-opus-4', result);

      expect(evaluation.codeMetrics).toBeDefined();
      expect(evaluation.codeMetrics.linesOfCode).toBe(100);
    });

    it('should determine recommendation based on score', async () => {
      const diffs = [createMockDiff('src/index.ts', 30, 5)];
      const result = createMockResult(diffs);

      const evaluation = await evaluator.evaluate('worker-1', 'claude', 'claude-opus-4', result);

      expect(['recommended', 'acceptable', 'needs-improvement', 'not-recommended']).toContain(
        evaluation.recommendation
      );
    });
  });

  describe('evaluateAndCompare', () => {
    it('should compare multiple solutions', async () => {
      const results = [
        {
          workerId: 'worker-1',
          workerName: 'claude',
          modelId: 'claude-opus-4',
          result: createMockResult([createMockDiff('src/a.ts', 50, 10)]),
        },
        {
          workerId: 'worker-2',
          workerName: 'gemini',
          modelId: 'gemini-2.5-pro',
          result: createMockResult([createMockDiff('src/b.ts', 30, 5)]),
        },
      ];

      const report = await evaluator.evaluateAndCompare(results);

      expect(report).toBeDefined();
      expect(report.evaluations.length).toBe(2);
      expect(report.bestSolution).toBeDefined();
    });

    it('should rank solutions by score', async () => {
      const results = [
        {
          workerId: 'worker-1',
          workerName: 'claude',
          modelId: 'claude-opus-4',
          result: createMockResult([createMockDiff('src/a.ts', 500, 10)]), // Large change, lower score
        },
        {
          workerId: 'worker-2',
          workerName: 'gemini',
          modelId: 'gemini-2.5-pro',
          result: createMockResult([createMockDiff('src/b.ts', 30, 5)]), // Small change, higher score
        },
      ];

      const report = await evaluator.evaluateAndCompare(results);

      // Check that ranks are assigned
      expect(report.evaluations[0].rank).toBe(1);
      expect(report.evaluations[1].rank).toBe(2);
    });

    it('should generate dimension comparison', async () => {
      const results = [
        {
          workerId: 'worker-1',
          workerName: 'claude',
          modelId: 'claude-opus-4',
          result: createMockResult([createMockDiff('src/a.ts', 50, 10)]),
        },
        {
          workerId: 'worker-2',
          workerName: 'gemini',
          modelId: 'gemini-2.5-pro',
          result: createMockResult([createMockDiff('src/b.ts', 30, 5)]),
        },
      ];

      const report = await evaluator.evaluateAndCompare(results);

      expect(report.dimensionComparison.length).toBe(5);
      report.dimensionComparison.forEach(dc => {
        expect(dc.scores.length).toBe(2);
        expect(dc.best).toBeDefined();
      });
    });
  });

  describe('security evaluation', () => {
    it('should detect eval usage', async () => {
      const diffs = [createMockDiff('src/index.ts', 10, 0, 'eval(userInput)')];
      const result = createMockResult(diffs);

      const evaluation = await evaluator.evaluate('worker-1', 'claude', 'claude-opus-4', result);

      const securityScore = evaluation.scores.find(s => s.dimension === 'security');
      expect(securityScore).toBeDefined();
      expect(securityScore!.issues.some(i => i.description.includes('eval'))).toBe(true);
    });

    it('should detect innerHTML usage', async () => {
      const diffs = [createMockDiff('src/index.ts', 10, 0, 'element.innerHTML = data')];
      const result = createMockResult(diffs);

      const evaluation = await evaluator.evaluate('worker-1', 'claude', 'claude-opus-4', result);

      const securityScore = evaluation.scores.find(s => s.dimension === 'security');
      expect(securityScore!.issues.some(i => i.description.includes('innerHTML'))).toBe(true);
    });
  });

  describe('completeness evaluation', () => {
    it('should reward test files', async () => {
      const diffsWithTests = [
        createMockDiff('src/index.ts', 50, 10),
        createMockDiff('src/__tests__/index.test.ts', 30, 0),
      ];
      const diffsWithoutTests = [createMockDiff('src/index.ts', 50, 10)];

      const evalWithTests = await evaluator.evaluate(
        'worker-1',
        'claude',
        'claude-opus-4',
        createMockResult(diffsWithTests)
      );
      const evalWithoutTests = await evaluator.evaluate(
        'worker-2',
        'gemini',
        'gemini-2.5-pro',
        createMockResult(diffsWithoutTests)
      );

      const completenessWithTests = evalWithTests.scores.find(s => s.dimension === 'completeness');
      const completenessWithoutTests = evalWithoutTests.scores.find(s => s.dimension === 'completeness');

      expect(completenessWithTests!.score).toBeGreaterThan(completenessWithoutTests!.score);
    });
  });

  describe('AI evaluation', () => {
    it('should use AI callback when provided', async () => {
      const mockAI: AIEvaluateCallback = vi.fn().mockResolvedValue({
        score: 85,
        details: ['AI evaluated'],
        issues: [],
      });

      const aiEvaluator = new SolutionEvaluator({}, mockAI);
      const diffs = [createMockDiff('src/index.ts', 50, 10)];
      const result = createMockResult(diffs);

      await aiEvaluator.evaluate('worker-1', 'claude', 'claude-opus-4', result);

      expect(mockAI).toHaveBeenCalled();
    });
  });

  describe('getBestSolution', () => {
    it('should return the best solution', async () => {
      const results = [
        {
          workerId: 'worker-1',
          workerName: 'claude',
          modelId: 'claude-opus-4',
          result: createMockResult([createMockDiff('src/a.ts', 50, 10)]),
        },
        {
          workerId: 'worker-2',
          workerName: 'gemini',
          modelId: 'gemini-2.5-pro',
          result: createMockResult([createMockDiff('src/b.ts', 30, 5)]),
        },
      ];

      const report = await evaluator.evaluateAndCompare(results);
      const best = evaluator.getBestSolution(report);

      expect(best).toBeDefined();
      expect(best?.rank).toBe(1);
    });
  });

  describe('getRecommendedSolutions', () => {
    it('should return recommended solutions', async () => {
      const results = [
        {
          workerId: 'worker-1',
          workerName: 'claude',
          modelId: 'claude-opus-4',
          result: createMockResult([createMockDiff('src/a.ts', 50, 10)]),
        },
      ];

      const report = await evaluator.evaluateAndCompare(results);
      const recommended = evaluator.getRecommendedSolutions(report);

      expect(Array.isArray(recommended)).toBe(true);
    });
  });

  describe('configuration', () => {
    it('should use custom weights', async () => {
      const customEvaluator = new SolutionEvaluator({
        weights: {
          quality: 0.5,
          performance: 0.1,
          maintainability: 0.1,
          security: 0.2,
          completeness: 0.1,
        },
      });

      const diffs = [createMockDiff('src/index.ts', 50, 10)];
      const result = createMockResult(diffs);

      const evaluation = await customEvaluator.evaluate('worker-1', 'claude', 'claude-opus-4', result);

      const qualityScore = evaluation.scores.find(s => s.dimension === 'quality');
      expect(qualityScore?.weight).toBe(0.5);
    });

    it('should update config', () => {
      evaluator.updateConfig({ thresholds: { recommended: 90, acceptable: 70, needsImprovement: 50 } });
      const config = evaluator.getConfig();
      expect(config.thresholds.recommended).toBe(90);
    });
  });
});
