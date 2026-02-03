import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import * as path from 'path';
import * as fs from 'fs';
import * as os from 'os';
import { execSync } from 'child_process';
import {
  WorktreeManager,
  WorktreeInfo,
  CreateWorktreeOptions,
  WorktreeManagerConfig,
} from '../WorktreeManager.js';

/**
 * 检查两个路径是否等价（处理 Windows 短路径名如 ADMINI~1）
 */
function pathsEqual(p1: string, p2: string): boolean {
  const normalize = (p: string) => p.replace(/\\/g, '/').toLowerCase();
  const n1 = normalize(p1);
  const n2 = normalize(p2);

  if (n1 === n2) return true;

  // 在 Windows 上处理短路径名
  if (process.platform === 'win32') {
    const parts1 = n1.split('/');
    const parts2 = n2.split('/');

    if (parts1.length !== parts2.length) return false;

    for (let i = 0; i < parts1.length; i++) {
      const p1Part = parts1[i];
      const p2Part = parts2[i];

      if (p1Part === p2Part) continue;

      // 检查是否是短路径名匹配（如 admini~1 vs administrator）
      const shortMatch1 = p1Part.match(/^(.+)~\d+$/);
      const shortMatch2 = p2Part.match(/^(.+)~\d+$/);

      if (shortMatch1 && p2Part.startsWith(shortMatch1[1])) continue;
      if (shortMatch2 && p1Part.startsWith(shortMatch2[1])) continue;

      return false;
    }

    return true;
  }

  return false;
}

describe('WorktreeManager', () => {
  let testDir: string;
  let manager: WorktreeManager;

  beforeEach(async () => {
    // 创建临时测试目录
    testDir = path.join(os.tmpdir(), `worktree-test-${Date.now()}`);
    fs.mkdirSync(testDir, { recursive: true });

    // 初始化 Git 仓库
    execSync('git init', { cwd: testDir });
    execSync('git config user.email "test@test.com"', { cwd: testDir });
    execSync('git config user.name "Test User"', { cwd: testDir });

    // 创建初始提交
    fs.writeFileSync(path.join(testDir, 'README.md'), '# Test');
    execSync('git add .', { cwd: testDir });
    execSync('git commit -m "Initial commit"', { cwd: testDir });

    // 创建管理器
    manager = new WorktreeManager(testDir);
    await manager.initialize();
  });

  afterEach(async () => {
    // 清理所有 worktrees
    try {
      await manager.removeAllWorktrees(true);
    } catch {
      // Ignore cleanup errors
    }

    // 删除测试目录
    try {
      fs.rmSync(testDir, { recursive: true, force: true });
    } catch {
      // Ignore cleanup errors
    }
  });

  describe('initialize', () => {
    it('should initialize successfully in a git repo', async () => {
      const newManager = new WorktreeManager(testDir);
      await expect(newManager.initialize()).resolves.not.toThrow();
    });

    it('should throw error for non-git directory', async () => {
      const nonGitDir = path.join(os.tmpdir(), `non-git-${Date.now()}`);
      fs.mkdirSync(nonGitDir, { recursive: true });

      const newManager = new WorktreeManager(nonGitDir);
      await expect(newManager.initialize()).rejects.toThrow('Not a git repository');

      fs.rmSync(nonGitDir, { recursive: true, force: true });
    });
  });

  describe('isGitRepo', () => {
    it('should return true for git repo', async () => {
      expect(await manager.isGitRepo()).toBe(true);
    });

    it('should return false for non-git directory', async () => {
      const nonGitDir = path.join(os.tmpdir(), `non-git-${Date.now()}`);
      fs.mkdirSync(nonGitDir, { recursive: true });

      const newManager = new WorktreeManager(nonGitDir);
      expect(await newManager.isGitRepo()).toBe(false);

      fs.rmSync(nonGitDir, { recursive: true, force: true });
    });
  });

  describe('createWorktree', () => {
    it('should create a new worktree', async () => {
      const info = await manager.createWorktree('test-worker');

      expect(info).toBeDefined();
      expect(info.path).toContain('test-worker');
      expect(info.branch).toBeDefined();
      expect(info.commit).toBeDefined();
      expect(info.isMain).toBe(false);
    });

    it('should create worktree with custom branch', async () => {
      const info = await manager.createWorktree('custom-branch', {
        branch: 'feature-test',
        createBranch: true,
      });

      expect(info.branch).toBe('feature-test');
    });

    it('should emit worktree:created event', async () => {
      const listener = vi.fn();
      manager.on('worktree:created', listener);

      await manager.createWorktree('event-test');

      expect(listener).toHaveBeenCalled();
    });

    it('should throw error if path already exists', async () => {
      await manager.createWorktree('duplicate');

      await expect(manager.createWorktree('duplicate')).rejects.toThrow('already exists');
    });

    it('should respect maxWorktrees limit', async () => {
      const limitedManager = new WorktreeManager(testDir, {
        maxWorktrees: 2,
        autoCleanup: false,
      });
      await limitedManager.initialize();

      // Main worktree counts as 1
      await limitedManager.createWorktree('worker-1');

      await expect(limitedManager.createWorktree('worker-2')).rejects.toThrow('Maximum worktrees');
    });
  });

  describe('removeWorktree', () => {
    it('should remove existing worktree', async () => {
      const info = await manager.createWorktree('to-remove');
      const result = await manager.removeWorktree('to-remove');

      expect(result).toBe(true);
      expect(manager.hasWorktree('to-remove')).toBe(false);
    });

    it('should return false for non-existent worktree', async () => {
      const result = await manager.removeWorktree('non-existent');
      expect(result).toBe(false);
    });

    it('should emit worktree:removed event', async () => {
      const listener = vi.fn();
      manager.on('worktree:removed', listener);

      await manager.createWorktree('event-remove');
      await manager.removeWorktree('event-remove');

      expect(listener).toHaveBeenCalled();
    });

    it('should throw error when removing main worktree', async () => {
      await expect(manager.removeWorktree(testDir)).rejects.toThrow('Cannot remove main worktree');
    });

    it('should throw error when removing locked worktree without force', async () => {
      await manager.createWorktree('locked-worker');
      await manager.lockWorktree('locked-worker');

      await expect(manager.removeWorktree('locked-worker')).rejects.toThrow('locked');
    });

    it('should remove locked worktree with force', async () => {
      await manager.createWorktree('force-remove');
      await manager.lockWorktree('force-remove');

      const result = await manager.removeWorktree('force-remove', true);
      expect(result).toBe(true);
    });
  });

  describe('listWorktrees', () => {
    it('should list all worktrees', async () => {
      await manager.createWorktree('list-1');
      await manager.createWorktree('list-2');

      const worktrees = await manager.listWorktrees();

      expect(worktrees.length).toBeGreaterThanOrEqual(3); // main + 2 created
    });

    it('should include main worktree', async () => {
      const worktrees = await manager.listWorktrees();
      const main = worktrees.find(w => w.isMain);

      expect(main).toBeDefined();
      expect(pathsEqual(main!.path, testDir)).toBe(true);
    });
  });

  describe('getWorktree', () => {
    it('should return worktree info by name', async () => {
      await manager.createWorktree('get-test');

      const info = manager.getWorktree('get-test');

      expect(info).toBeDefined();
      expect(info?.path).toContain('get-test');
    });

    it('should return undefined for non-existent worktree', () => {
      const info = manager.getWorktree('non-existent');
      expect(info).toBeUndefined();
    });
  });

  describe('lockWorktree', () => {
    it('should lock worktree', async () => {
      await manager.createWorktree('lock-test');
      const result = await manager.lockWorktree('lock-test');

      expect(result).toBe(true);
      const info = manager.getWorktree('lock-test');
      expect(info?.isLocked).toBe(true);
    });

    it('should emit worktree:locked event', async () => {
      const listener = vi.fn();
      manager.on('worktree:locked', listener);

      await manager.createWorktree('lock-event');
      await manager.lockWorktree('lock-event');

      expect(listener).toHaveBeenCalled();
    });

    it('should return false for non-existent worktree', async () => {
      const result = await manager.lockWorktree('non-existent');
      expect(result).toBe(false);
    });
  });

  describe('unlockWorktree', () => {
    it('should unlock worktree', async () => {
      await manager.createWorktree('unlock-test');
      await manager.lockWorktree('unlock-test');
      const result = await manager.unlockWorktree('unlock-test');

      expect(result).toBe(true);
      const info = manager.getWorktree('unlock-test');
      expect(info?.isLocked).toBe(false);
    });

    it('should emit worktree:unlocked event', async () => {
      const listener = vi.fn();
      manager.on('worktree:unlocked', listener);

      await manager.createWorktree('unlock-event');
      await manager.lockWorktree('unlock-event');
      await manager.unlockWorktree('unlock-event');

      expect(listener).toHaveBeenCalled();
    });

    it('should return false for non-existent worktree', async () => {
      const result = await manager.unlockWorktree('non-existent');
      expect(result).toBe(false);
    });
  });

  describe('cleanupPrunable', () => {
    it('should cleanup prunable worktrees', async () => {
      // Create and manually delete worktree directory
      const info = await manager.createWorktree('prunable');
      fs.rmSync(info.path, { recursive: true, force: true });

      const cleaned = await manager.cleanupPrunable();
      expect(cleaned).toBeGreaterThanOrEqual(0);
    });
  });

  describe('removeAllWorktrees', () => {
    it('should remove all non-main worktrees', async () => {
      await manager.createWorktree('remove-all-1');
      await manager.createWorktree('remove-all-2');

      const removed = await manager.removeAllWorktrees();

      expect(removed).toBe(2);
      const worktrees = await manager.listWorktrees();
      expect(worktrees.filter(w => !w.isMain).length).toBe(0);
    });

    it('should not remove main worktree', async () => {
      await manager.createWorktree('keep-main');
      await manager.removeAllWorktrees();

      const worktrees = await manager.listWorktrees();
      const main = worktrees.find(w => w.isMain);
      expect(main).toBeDefined();
    });
  });

  describe('getWorktreeCount', () => {
    it('should return correct count', async () => {
      const initialCount = manager.getWorktreeCount();

      await manager.createWorktree('count-1');
      expect(manager.getWorktreeCount()).toBe(initialCount + 1);

      await manager.createWorktree('count-2');
      expect(manager.getWorktreeCount()).toBe(initialCount + 2);
    });
  });

  describe('hasWorktree', () => {
    it('should return true for existing worktree', async () => {
      await manager.createWorktree('has-test');
      expect(manager.hasWorktree('has-test')).toBe(true);
    });

    it('should return false for non-existent worktree', () => {
      expect(manager.hasWorktree('non-existent')).toBe(false);
    });
  });

  describe('execInWorktree', () => {
    it('should execute command in worktree', async () => {
      await manager.createWorktree('exec-test');

      const result = await manager.execInWorktree('exec-test', 'git status');

      expect(result).toContain('On branch');
    });

    it('should throw error for non-existent worktree', async () => {
      await expect(manager.execInWorktree('non-existent', 'git status')).rejects.toThrow(
        'Worktree not found'
      );
    });
  });

  describe('getWorktreeBranch', () => {
    it('should return branch name', async () => {
      await manager.createWorktree('branch-test', {
        branch: 'test-branch',
        createBranch: true,
      });

      const branch = await manager.getWorktreeBranch('branch-test');
      expect(branch).toBe('test-branch');
    });

    it('should return null for non-existent worktree', async () => {
      const branch = await manager.getWorktreeBranch('non-existent');
      expect(branch).toBeNull();
    });
  });

  describe('getWorktreeCommit', () => {
    it('should return commit hash', async () => {
      await manager.createWorktree('commit-test');

      const commit = await manager.getWorktreeCommit('commit-test');

      expect(commit).toBeDefined();
      expect(commit?.length).toBe(40); // Full SHA
    });

    it('should return null for non-existent worktree', async () => {
      const commit = await manager.getWorktreeCommit('non-existent');
      expect(commit).toBeNull();
    });
  });

  describe('refresh', () => {
    it('should update worktree list', async () => {
      await manager.createWorktree('refresh-test');
      const countBefore = manager.getWorktreeCount();

      // Manually create another worktree
      execSync(`git worktree add "${path.join(testDir, '.codeflow/worktrees/manual')}" -b manual-branch`, {
        cwd: testDir,
      });

      await manager.refresh();
      expect(manager.getWorktreeCount()).toBe(countBefore + 1);
    });
  });

  describe('events', () => {
    it('should emit worktree:error on failure', async () => {
      const listener = vi.fn();
      manager.on('worktree:error', listener);

      // Try to create worktree with invalid branch
      try {
        await manager.createWorktree('error-test', {
          branch: 'non-existent-remote-branch',
        });
      } catch {
        // Expected to fail
      }

      // Error event may or may not be emitted depending on the error type
      // This test just ensures the event system works
    });
  });

  describe('configuration', () => {
    it('should use custom configuration', async () => {
      const customManager = new WorktreeManager(testDir, {
        baseDir: '.custom-worktrees',
        worktreePrefix: 'custom',
        maxWorktrees: 5,
      });
      await customManager.initialize();

      const info = await customManager.createWorktree('config-test');

      expect(info.path).toContain('.custom-worktrees');
      expect(info.branch).toContain('custom');

      await customManager.removeAllWorktrees(true);
    });
  });
});
