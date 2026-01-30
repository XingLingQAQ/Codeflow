/**
 * ClaudeCodeEditor 单元测试
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { ClaudeCodeEditor } from '../ClaudeCodeEditor.js';

// Mock ClaudeAdapter
const createMockAdapter = () => ({
  send: vi.fn(),
  stream: vi.fn(),
  getHistory: vi.fn().mockReturnValue([]),
  setHistory: vi.fn(),
  rewind: vi.fn(),
  compact: vi.fn(),
  configure: vi.fn(),
  getConfig: vi.fn(),
});

describe('ClaudeCodeEditor', () => {
  let editor: ClaudeCodeEditor;
  let mockAdapter: ReturnType<typeof createMockAdapter>;

  beforeEach(() => {
    mockAdapter = createMockAdapter();
    editor = new ClaudeCodeEditor(mockAdapter as any, {
      autoBackup: false, // 禁用备份简化测试
    });
  });

  describe('constructor', () => {
    it('should create editor with default config', () => {
      const ed = new ClaudeCodeEditor(mockAdapter as any);
      expect(ed.name).toBe('claude-editor');
    });

    it('should accept custom config', () => {
      const ed = new ClaudeCodeEditor(mockAdapter as any, {
        model: 'claude-3-opus',
        maxTokens: 8192,
        temperature: 0.5,
      });
      expect(ed.name).toBe('claude-editor');
    });
  });

  describe('edit', () => {
    it('should call adapter with correct prompt', async () => {
      mockAdapter.send.mockResolvedValue({
        content: `--- a/test.ts
+++ b/test.ts
@@ -1,3 +1,4 @@
 function hello() {
+  console.log('Hello');
   return 'world';
 }`,
      });

      const result = await editor.edit('test.ts', 'Add logging');

      expect(mockAdapter.send).toHaveBeenCalled();
      const prompt = mockAdapter.send.mock.calls[0][0];
      expect(prompt).toContain('test.ts');
      expect(prompt).toContain('Add logging');
    });

    it('should parse diff from response', async () => {
      mockAdapter.send.mockResolvedValue({
        content: `--- a/test.ts
+++ b/test.ts
@@ -1,3 +1,4 @@
 function hello() {
+  console.log('Hello');
   return 'world';
 }`,
      });

      const result = await editor.edit('test.ts', 'Add logging');

      expect(result.success).toBe(true);
      expect(result.diff.additions).toBe(1);
      expect(result.diff.deletions).toBe(0);
    });

    it('should handle empty response', async () => {
      mockAdapter.send.mockResolvedValue({
        content: 'No changes needed.',
      });

      const result = await editor.edit('test.ts', 'Check code');

      expect(result.success).toBe(true);
      expect(result.diff.hunks.length).toBe(0);
      expect(result.message).toBe('No changes made');
    });

    it('should handle adapter errors', async () => {
      mockAdapter.send.mockRejectedValue(new Error('API Error'));

      const result = await editor.edit('test.ts', 'Add logging');

      expect(result.success).toBe(false);
      expect(result.message).toBe('API Error');
    });
  });

  describe('editMultiple', () => {
    it('should edit multiple files sequentially', async () => {
      mockAdapter.send.mockResolvedValue({
        content: `--- a/file.ts
+++ b/file.ts
@@ -1,1 +1,2 @@
 const x = 1;
+const y = 2;`,
      });

      const results = await editor.editMultiple(['a.ts', 'b.ts'], 'Add variable');

      expect(results.length).toBe(2);
      expect(mockAdapter.send).toHaveBeenCalledTimes(2);
    });
  });

  describe('preview', () => {
    it('should return diff without applying changes', async () => {
      mockAdapter.send.mockResolvedValue({
        content: `--- a/test.ts
+++ b/test.ts
@@ -1,1 +1,2 @@
 const x = 1;
+const y = 2;`,
      });

      const diff = await editor.preview('test.ts', 'Add variable');

      expect(diff.additions).toBe(1);
      expect(diff.hunks.length).toBe(1);
    });

    it('should handle errors gracefully', async () => {
      mockAdapter.send.mockRejectedValue(new Error('API Error'));

      const diff = await editor.preview('test.ts', 'Add variable');

      expect(diff.hunks.length).toBe(0);
    });
  });

  describe('parseDiff', () => {
    it('should parse standard unified diff', async () => {
      mockAdapter.send.mockResolvedValue({
        content: `--- a/test.ts
+++ b/test.ts
@@ -1,5 +1,6 @@
 function test() {
+  // Added comment
   const x = 1;
-  const y = 2;
+  const y = 3;
   return x + y;
 }`,
      });

      const result = await editor.edit('test.ts', 'Modify');

      expect(result.diff.additions).toBe(2);
      expect(result.diff.deletions).toBe(1);
      expect(result.diff.hunks.length).toBe(1);
    });

    it('should parse multiple hunks', async () => {
      mockAdapter.send.mockResolvedValue({
        content: `--- a/test.ts
+++ b/test.ts
@@ -1,3 +1,4 @@
 function a() {
+  console.log('a');
   return 1;
 }
@@ -10,3 +11,4 @@
 function b() {
+  console.log('b');
   return 2;
 }`,
      });

      const result = await editor.edit('test.ts', 'Add logging');

      expect(result.diff.hunks.length).toBe(2);
      expect(result.diff.additions).toBe(2);
    });

    it('should handle diff with no line count', async () => {
      mockAdapter.send.mockResolvedValue({
        content: `--- a/test.ts
+++ b/test.ts
@@ -1 +1,2 @@
 const x = 1;
+const y = 2;`,
      });

      const result = await editor.edit('test.ts', 'Add variable');

      expect(result.diff.hunks.length).toBe(1);
      expect(result.diff.hunks[0].oldLines).toBe(1);
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
});
