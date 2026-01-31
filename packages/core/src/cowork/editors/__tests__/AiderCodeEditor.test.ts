/**
 * AiderCodeEditor 单元测试
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { AiderCodeEditor } from '../AiderCodeEditor.js';
import { AiderAdapter } from '../../adapters/AiderAdapter.js';

// Mock AiderAdapter
const createMockAdapter = () => ({
  name: 'aider',
  version: '0.1.0',
  execute: vi.fn(),
  stream: vi.fn(),
  interrupt: vi.fn(),
  healthCheck: vi.fn(),
  getCapabilities: vi.fn().mockReturnValue({
    supportsStreaming: true,
    supportsInterrupt: true,
    supportedLanguages: ['typescript'],
    maxContextTokens: 128000,
    features: ['code-edit'],
  }),
  parseDiff: vi.fn().mockReturnValue([]),
  configure: vi.fn(),
  getConfig: vi.fn(),
});

describe('AiderCodeEditor', () => {
  let editor: AiderCodeEditor;
  let mockAdapter: ReturnType<typeof createMockAdapter>;

  beforeEach(() => {
    mockAdapter = createMockAdapter();
    editor = new AiderCodeEditor(mockAdapter as unknown as AiderAdapter, {
      autoBackup: false, // 禁用备份简化测试
    });
  });

  describe('constructor', () => {
    it('should create editor with default config', () => {
      const ed = new AiderCodeEditor(mockAdapter as unknown as AiderAdapter);
      expect(ed.name).toBe('aider-editor');
    });

    it('should accept custom config', () => {
      const ed = new AiderCodeEditor(mockAdapter as unknown as AiderAdapter, {
        cwd: '/custom/path',
        backupDir: '.custom-backups',
        dryRun: true,
      });
      expect(ed.name).toBe('aider-editor');
    });
  });

  describe('edit', () => {
    it('should call adapter execute with correct command', async () => {
      mockAdapter.execute.mockResolvedValue({
        stdout: '',
        stderr: '',
        exitCode: 0,
        duration: 100,
      });

      await editor.edit('test.ts', 'Add logging');

      expect(mockAdapter.execute).toHaveBeenCalled();
      const command = mockAdapter.execute.mock.calls[0][0];
      expect(command).toContain('Add logging');
      expect(command).toContain('test.ts');
    });

    it('should return success with parsed diff', async () => {
      mockAdapter.execute.mockResolvedValue({
        stdout: 'diff output',
        stderr: '',
        exitCode: 0,
        duration: 100,
      });
      mockAdapter.parseDiff.mockReturnValue([
        {
          file: 'test.ts',
          hunks: [{ oldStart: 1, oldLines: 3, newStart: 1, newLines: 4, content: '+line' }],
          additions: 1,
          deletions: 0,
        },
      ]);

      const result = await editor.edit('test.ts', 'Add logging');

      expect(result.success).toBe(true);
      expect(result.diff.additions).toBe(1);
    });

    it('should return no changes when no diff found', async () => {
      mockAdapter.execute.mockResolvedValue({
        stdout: 'No changes',
        stderr: '',
        exitCode: 0,
        duration: 100,
      });
      mockAdapter.parseDiff.mockReturnValue([]);

      const result = await editor.edit('test.ts', 'Check code');

      expect(result.success).toBe(true);
      expect(result.message).toBe('No changes made');
      expect(result.diff.hunks.length).toBe(0);
    });

    it('should return failure on non-zero exit code', async () => {
      mockAdapter.execute.mockResolvedValue({
        stdout: '',
        stderr: 'Error occurred',
        exitCode: 1,
        duration: 100,
      });

      const result = await editor.edit('test.ts', 'Add logging');

      expect(result.success).toBe(false);
      expect(result.message).toContain('Error occurred');
    });

    it('should handle adapter errors', async () => {
      mockAdapter.execute.mockRejectedValue(new Error('Connection failed'));

      const result = await editor.edit('test.ts', 'Add logging');

      expect(result.success).toBe(false);
      expect(result.message).toBe('Connection failed');
    });
  });

  describe('editMultiple', () => {
    it('should edit multiple files', async () => {
      mockAdapter.execute.mockResolvedValue({
        stdout: 'diff output',
        stderr: '',
        exitCode: 0,
        duration: 100,
      });
      mockAdapter.parseDiff.mockReturnValue([
        {
          file: 'a.ts',
          hunks: [{ oldStart: 1, oldLines: 1, newStart: 1, newLines: 2, content: '+line' }],
          additions: 1,
          deletions: 0,
        },
        {
          file: 'b.ts',
          hunks: [{ oldStart: 1, oldLines: 1, newStart: 1, newLines: 2, content: '+line' }],
          additions: 1,
          deletions: 0,
        },
      ]);

      const results = await editor.editMultiple(['a.ts', 'b.ts'], 'Add variable');

      expect(results.length).toBe(2);
      expect(mockAdapter.execute).toHaveBeenCalledTimes(1);
    });

    it('should handle errors for all files', async () => {
      mockAdapter.execute.mockRejectedValue(new Error('Failed'));

      const results = await editor.editMultiple(['a.ts', 'b.ts'], 'Add variable');

      expect(results.length).toBe(2);
      expect(results[0].success).toBe(false);
      expect(results[1].success).toBe(false);
    });
  });

  describe('preview', () => {
    it('should return diff without applying changes', async () => {
      mockAdapter.execute.mockResolvedValue({
        stdout: 'diff output',
        stderr: '',
        exitCode: 0,
        duration: 100,
      });
      mockAdapter.parseDiff.mockReturnValue([
        {
          file: 'test.ts',
          hunks: [{ oldStart: 1, oldLines: 1, newStart: 1, newLines: 2, content: '+line' }],
          additions: 1,
          deletions: 0,
        },
      ]);

      const diff = await editor.preview('test.ts', 'Add variable');

      expect(diff.additions).toBe(1);
      expect(diff.hunks.length).toBe(1);
    });

    it('should return empty diff when no changes', async () => {
      mockAdapter.execute.mockResolvedValue({
        stdout: '',
        stderr: '',
        exitCode: 0,
        duration: 100,
      });
      mockAdapter.parseDiff.mockReturnValue([]);

      const diff = await editor.preview('test.ts', 'Check code');

      expect(diff.hunks.length).toBe(0);
    });
  });

  describe('backup and undo', () => {
    it('should track backup stack', () => {
      const stack = editor.getBackupStack();
      expect(stack).toEqual([]);
    });

    it('should throw when no backup available', async () => {
      await expect(editor.undo()).rejects.toThrow('No backup available');
    });
  });

  describe('clearBackups', () => {
    it('should clear backup stack', async () => {
      await editor.clearBackups();
      expect(editor.getBackupStack()).toEqual([]);
    });
  });
});
