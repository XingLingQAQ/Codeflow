/**
 * GitConflictDetector 单元测试
 */

import { describe, it, expect, beforeEach } from 'vitest';
import { GitConflictDetector } from '../GitConflictDetector.js';
import { Diff } from '../types.js';

describe('GitConflictDetector', () => {
  let detector: GitConflictDetector;

  beforeEach(() => {
    detector = new GitConflictDetector();
  });

  describe('constructor', () => {
    it('should create detector with default config', () => {
      const d = new GitConflictDetector();
      expect(d).toBeDefined();
    });

    it('should accept custom config', () => {
      const d = new GitConflictDetector({
        cwd: '/tmp',
        gitPath: '/usr/bin/git',
      });
      expect(d).toBeDefined();
    });
  });

  describe('detectDiffConflicts', () => {
    it('should return null for different files', () => {
      const diff1: Diff = {
        file: 'a.ts',
        hunks: [{ oldStart: 1, oldLines: 3, newStart: 1, newLines: 4, content: '+line' }],
        additions: 1,
        deletions: 0,
      };

      const diff2: Diff = {
        file: 'b.ts',
        hunks: [{ oldStart: 1, oldLines: 3, newStart: 1, newLines: 4, content: '+line' }],
        additions: 1,
        deletions: 0,
      };

      const conflict = detector.detectDiffConflicts(diff1, diff2);
      expect(conflict).toBeNull();
    });

    it('should detect overlapping hunks', () => {
      const diff1: Diff = {
        file: 'test.ts',
        hunks: [{ oldStart: 1, oldLines: 5, newStart: 1, newLines: 6, content: '+line' }],
        additions: 1,
        deletions: 0,
      };

      const diff2: Diff = {
        file: 'test.ts',
        hunks: [{ oldStart: 3, oldLines: 5, newStart: 3, newLines: 6, content: '+line' }],
        additions: 1,
        deletions: 0,
      };

      const conflict = detector.detectDiffConflicts(diff1, diff2);
      expect(conflict).not.toBeNull();
      expect(conflict?.file).toBe('test.ts');
      expect(conflict?.type).toBe('content');
    });

    it('should not detect non-overlapping hunks', () => {
      const diff1: Diff = {
        file: 'test.ts',
        hunks: [{ oldStart: 1, oldLines: 3, newStart: 1, newLines: 4, content: '+line' }],
        additions: 1,
        deletions: 0,
      };

      const diff2: Diff = {
        file: 'test.ts',
        hunks: [{ oldStart: 10, oldLines: 3, newStart: 11, newLines: 4, content: '+line' }],
        additions: 1,
        deletions: 0,
      };

      const conflict = detector.detectDiffConflicts(diff1, diff2);
      expect(conflict).toBeNull();
    });

    it('should detect adjacent hunks as non-conflicting', () => {
      const diff1: Diff = {
        file: 'test.ts',
        hunks: [{ oldStart: 1, oldLines: 3, newStart: 1, newLines: 4, content: '+line' }],
        additions: 1,
        deletions: 0,
      };

      const diff2: Diff = {
        file: 'test.ts',
        hunks: [{ oldStart: 4, oldLines: 3, newStart: 5, newLines: 4, content: '+line' }],
        additions: 1,
        deletions: 0,
      };

      const conflict = detector.detectDiffConflicts(diff1, diff2);
      expect(conflict).toBeNull();
    });
  });

  describe('detectMultipleDiffConflicts', () => {
    it('should return empty array for no conflicts', () => {
      const diffs: Diff[] = [
        {
          file: 'a.ts',
          hunks: [{ oldStart: 1, oldLines: 3, newStart: 1, newLines: 4, content: '+line' }],
          additions: 1,
          deletions: 0,
        },
        {
          file: 'b.ts',
          hunks: [{ oldStart: 1, oldLines: 3, newStart: 1, newLines: 4, content: '+line' }],
          additions: 1,
          deletions: 0,
        },
      ];

      const conflicts = detector.detectMultipleDiffConflicts(diffs);
      expect(conflicts).toEqual([]);
    });

    it('should detect conflicts in same file', () => {
      const diffs: Diff[] = [
        {
          file: 'test.ts',
          hunks: [{ oldStart: 1, oldLines: 5, newStart: 1, newLines: 6, content: '+line1' }],
          additions: 1,
          deletions: 0,
        },
        {
          file: 'test.ts',
          hunks: [{ oldStart: 3, oldLines: 5, newStart: 3, newLines: 6, content: '+line2' }],
          additions: 1,
          deletions: 0,
        },
      ];

      const conflicts = detector.detectMultipleDiffConflicts(diffs);
      expect(conflicts.length).toBe(1);
      expect(conflicts[0].file).toBe('test.ts');
    });

    it('should detect multiple conflicts', () => {
      const diffs: Diff[] = [
        {
          file: 'a.ts',
          hunks: [{ oldStart: 1, oldLines: 5, newStart: 1, newLines: 6, content: '+line' }],
          additions: 1,
          deletions: 0,
        },
        {
          file: 'a.ts',
          hunks: [{ oldStart: 3, oldLines: 5, newStart: 3, newLines: 6, content: '+line' }],
          additions: 1,
          deletions: 0,
        },
        {
          file: 'b.ts',
          hunks: [{ oldStart: 1, oldLines: 5, newStart: 1, newLines: 6, content: '+line' }],
          additions: 1,
          deletions: 0,
        },
        {
          file: 'b.ts',
          hunks: [{ oldStart: 2, oldLines: 5, newStart: 2, newLines: 6, content: '+line' }],
          additions: 1,
          deletions: 0,
        },
      ];

      const conflicts = detector.detectMultipleDiffConflicts(diffs);
      expect(conflicts.length).toBe(2);
    });

    it('should handle empty diffs array', () => {
      const conflicts = detector.detectMultipleDiffConflicts([]);
      expect(conflicts).toEqual([]);
    });

    it('should handle single diff', () => {
      const diffs: Diff[] = [
        {
          file: 'test.ts',
          hunks: [{ oldStart: 1, oldLines: 3, newStart: 1, newLines: 4, content: '+line' }],
          additions: 1,
          deletions: 0,
        },
      ];

      const conflicts = detector.detectMultipleDiffConflicts(diffs);
      expect(conflicts).toEqual([]);
    });
  });

  describe('isGitRepository', () => {
    it('should return true for current directory (assuming it is a git repo)', async () => {
      const isRepo = await detector.isGitRepository();
      expect(isRepo).toBe(true);
    });
  });

  describe('getCurrentBranch', () => {
    it('should return current branch name', async () => {
      const branch = await detector.getCurrentBranch();
      expect(typeof branch).toBe('string');
      expect(branch.length).toBeGreaterThan(0);
    });
  });
});
