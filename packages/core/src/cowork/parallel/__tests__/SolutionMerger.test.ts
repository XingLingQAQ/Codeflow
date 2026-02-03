import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import * as path from 'path';
import * as fs from 'fs';
import * as os from 'os';
import { execSync } from 'child_process';
import {
  SolutionSelector,
  ConflictResolver,
  MergeStrategy,
  SolutionPreview,
} from '../SolutionMerger.js';
import { AgentWorker } from '../ParallelExecutor.js';
import { WorkerResult } from '../ResultCollector.js';
import { Diff } from '../../types.js';

// Helper to create mock data
const createMockDiff = (file: string, additions: number, deletions: number): Diff => ({
  file,
  hunks: [{ oldStart: 1, oldLines: deletions, newStart: 1, newLines: additions, content: '' }],
  additions,
  deletions,
});

const createMockWorker = (id: string, name: string, branch: string): AgentWorker => ({
  id,
  name,
  modelId: 'test-model',
  status: 'completed',
  worktree: {
    path: '/tmp/worktree',
    branch,
    locked: false,
    createdAt: Date.now(),
  },
});

const createMockResult = (diffs: Diff[]): WorkerResult => ({
  taskId: 'task-1',
  success: true,
  duration: 1000,
  diffs,
});

describe('SolutionSelector', () => {
  let selector: SolutionSelector;
  let testDir: string;

  beforeEach(async () => {
    // 创建临时测试目录
    testDir = path.join(os.tmpdir(), `merger-test-${Date.now()}`);
    fs.mkdirSync(testDir, { recursive: true });

    // 初始化 Git 仓库
    execSync('git init', { cwd: testDir });
    execSync('git config user.email "test@test.com"', { cwd: testDir });
    execSync('git config user.name "Test User"', { cwd: testDir });
    fs.writeFileSync(path.join(testDir, 'README.md'), '# Test');
    execSync('git add .', { cwd: testDir });
    execSync('git commit -m "Initial commit"', { cwd: testDir });

    selector = new SolutionSelector(testDir);
  });

  afterEach(async () => {
    try {
      fs.rmSync(testDir, { recursive: true, force: true });
    } catch {
      // Ignore cleanup errors
    }
  });

  describe('generatePreview', () => {
    it('should generate preview for a worker', () => {
      const worker = createMockWorker('worker-1', 'claude', 'feature-branch');
      const result = createMockResult([
        createMockDiff('src/index.ts', 50, 10),
        createMockDiff('src/utils.ts', 20, 5),
      ]);

      const preview = selector.generatePreview(worker, result);

      expect(preview).toBeDefined();
      expect(preview.workerId).toBe('worker-1');
      expect(preview.workerName).toBe('claude');
      expect(preview.filesChanged.length).toBe(2);
      expect(preview.additions).toBe(70);
      expect(preview.deletions).toBe(15);
    });

    it('should emit preview:generated event', () => {
      const listener = vi.fn();
      selector.on('preview:generated', listener);

      const worker = createMockWorker('worker-1', 'claude', 'feature-branch');
      const result = createMockResult([createMockDiff('src/index.ts', 50, 10)]);

      selector.generatePreview(worker, result);

      expect(listener).toHaveBeenCalled();
    });

    it('should store preview for later retrieval', () => {
      const worker = createMockWorker('worker-1', 'claude', 'feature-branch');
      const result = createMockResult([createMockDiff('src/index.ts', 50, 10)]);

      selector.generatePreview(worker, result);

      const retrieved = selector.getPreview('worker-1');
      expect(retrieved).toBeDefined();
      expect(retrieved?.workerId).toBe('worker-1');
    });
  });

  describe('getAllPreviews', () => {
    it('should return all previews', () => {
      const worker1 = createMockWorker('worker-1', 'claude', 'branch-1');
      const worker2 = createMockWorker('worker-2', 'gemini', 'branch-2');
      const result = createMockResult([createMockDiff('src/index.ts', 50, 10)]);

      selector.generatePreview(worker1, result);
      selector.generatePreview(worker2, result);

      const previews = selector.getAllPreviews();
      expect(previews.length).toBe(2);
    });
  });

  describe('cleanup', () => {
    it('should remove preview', async () => {
      const worker = createMockWorker('worker-1', 'claude', 'feature-branch');
      const result = createMockResult([createMockDiff('src/index.ts', 50, 10)]);

      selector.generatePreview(worker, result);
      expect(selector.getPreview('worker-1')).toBeDefined();

      await selector.cleanup('worker-1');
      expect(selector.getPreview('worker-1')).toBeUndefined();
    });

    it('should emit cleanup:complete event', async () => {
      const listener = vi.fn();
      selector.on('cleanup:complete', listener);

      const worker = createMockWorker('worker-1', 'claude', 'feature-branch');
      const result = createMockResult([createMockDiff('src/index.ts', 50, 10)]);

      selector.generatePreview(worker, result);
      await selector.cleanup('worker-1');

      expect(listener).toHaveBeenCalledWith('worker-1');
    });
  });

  describe('cleanupAll', () => {
    it('should remove all previews', async () => {
      const worker1 = createMockWorker('worker-1', 'claude', 'branch-1');
      const worker2 = createMockWorker('worker-2', 'gemini', 'branch-2');
      const result = createMockResult([createMockDiff('src/index.ts', 50, 10)]);

      selector.generatePreview(worker1, result);
      selector.generatePreview(worker2, result);

      await selector.cleanupAll();

      expect(selector.getAllPreviews().length).toBe(0);
    });
  });

  describe('configuration', () => {
    it('should use default config', () => {
      const config = selector.getConfig();
      expect(config.defaultStrategy).toBe('merge');
      expect(config.autoCleanup).toBe(true);
      expect(config.createBackup).toBe(true);
    });

    it('should update config', () => {
      selector.updateConfig({ defaultStrategy: 'squash', autoCleanup: false });
      const config = selector.getConfig();
      expect(config.defaultStrategy).toBe('squash');
      expect(config.autoCleanup).toBe(false);
    });
  });

  describe('canRollback', () => {
    it('should return false when no backup exists', () => {
      expect(selector.canRollback()).toBe(false);
    });
  });
});

describe('ConflictResolver', () => {
  describe('parseConflictMarkers', () => {
    it('should parse conflict markers', () => {
      const content = `<<<<<<< HEAD
our changes
=======
their changes
>>>>>>> feature`;

      const result = ConflictResolver.parseConflictMarkers(content);

      expect(result).toBeDefined();
      expect(result?.ours).toContain('our changes');
      expect(result?.theirs).toContain('their changes');
    });

    it('should return null for non-conflict content', () => {
      const content = 'normal content without conflicts';
      const result = ConflictResolver.parseConflictMarkers(content);
      expect(result).toBeNull();
    });
  });

  describe('autoResolve', () => {
    it('should resolve with ours strategy', () => {
      const result = ConflictResolver.autoResolve('our content', 'their content', 'ours');
      expect(result).toBe('our content');
    });

    it('should resolve with theirs strategy', () => {
      const result = ConflictResolver.autoResolve('our content', 'their content', 'theirs');
      expect(result).toBe('their content');
    });

    it('should resolve with union strategy', () => {
      const result = ConflictResolver.autoResolve('our content', 'their content', 'union');
      expect(result).toContain('our content');
      expect(result).toContain('their content');
    });
  });

  describe('canAutoResolve', () => {
    it('should return true when one side is empty', () => {
      expect(ConflictResolver.canAutoResolve('content', '')).toBe(true);
      expect(ConflictResolver.canAutoResolve('', 'content')).toBe(true);
    });

    it('should return true when content is identical', () => {
      expect(ConflictResolver.canAutoResolve('same content', 'same content')).toBe(true);
    });

    it('should return true when no overlap', () => {
      expect(ConflictResolver.canAutoResolve('line1\nline2', 'line3\nline4')).toBe(true);
    });

    it('should return false when there is overlap', () => {
      expect(ConflictResolver.canAutoResolve('line1\nline2', 'line2\nline3')).toBe(false);
    });
  });
});

describe('MergeStrategy types', () => {
  it('should support all merge strategies', () => {
    const strategies: MergeStrategy[] = ['merge', 'rebase', 'squash', 'cherry-pick'];
    expect(strategies.length).toBe(4);
  });
});
