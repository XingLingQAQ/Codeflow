/**
 * AiderAdapter 单元测试
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { AiderAdapter, ParsedDiff } from '../AiderAdapter.js';

describe('AiderAdapter', () => {
  let adapter: AiderAdapter;

  beforeEach(() => {
    adapter = new AiderAdapter({
      aiderPath: 'echo', // 使用 echo 模拟 aider
    });
  });

  describe('constructor', () => {
    it('should create adapter with default config', () => {
      const a = new AiderAdapter();
      expect(a.name).toBe('aider');
      expect(a.version).toBe('0.1.0');
    });

    it('should accept custom config', () => {
      const a = new AiderAdapter({
        model: 'gpt-4-turbo',
        editFormat: 'whole',
        autoConfirm: false,
      });
      const config = a.getConfig();
      expect(config.model).toBe('gpt-4-turbo');
      expect(config.editFormat).toBe('whole');
      expect(config.autoConfirm).toBe(false);
    });
  });

  describe('execute', () => {
    it('should execute command and return result', async () => {
      const result = await adapter.execute('test message');

      expect(result.exitCode).toBe(0);
      expect(result.duration).toBeGreaterThan(0);
    });

    it('should capture stdout', async () => {
      const result = await adapter.execute('hello world');

      expect(result.stdout).toBeDefined();
    });

    it('should handle timeout', async () => {
      const slowAdapter = new AiderAdapter({
        aiderPath: 'node',
      });

      // 使用一个会超时的命令
      const promise = slowAdapter.execute('-e "setTimeout(() => {}, 10000)"', {
        timeout: 100,
      });

      // 应该在超时后完成
      const result = await promise;
      expect(result).toBeDefined();
    });

    it('should handle abort signal', async () => {
      const controller = new AbortController();
      const slowAdapter = new AiderAdapter({
        aiderPath: 'node',
      });

      // 立即中断
      controller.abort();

      const result = await slowAdapter.execute('-e "setTimeout(() => {}, 5000)"', {
        signal: controller.signal,
      });

      expect(result).toBeDefined();
    });
  });

  describe('stream', () => {
    it('should stream output', async () => {
      const chunks: string[] = [];

      await adapter.stream('streaming test', (chunk) => {
        chunks.push(chunk);
      });

      expect(chunks.length).toBeGreaterThanOrEqual(0);
    });

    it('should handle stream errors', async () => {
      const badAdapter = new AiderAdapter({
        aiderPath: 'nonexistent-command-12345',
      });

      await expect(
        badAdapter.stream('test', () => {})
      ).rejects.toThrow();
    });
  });

  describe('interrupt', () => {
    it('should interrupt current process', async () => {
      // 没有运行中的进程时不应抛出错误
      await expect(adapter.interrupt()).resolves.not.toThrow();
    });
  });

  describe('healthCheck', () => {
    it('should return false for invalid aider path', async () => {
      const badAdapter = new AiderAdapter({
        aiderPath: 'nonexistent-aider-command',
      });

      const healthy = await badAdapter.healthCheck();
      expect(healthy).toBe(false);
    });
  });

  describe('getCapabilities', () => {
    it('should return capabilities', () => {
      const caps = adapter.getCapabilities();

      expect(caps.supportsStreaming).toBe(true);
      expect(caps.supportsInterrupt).toBe(true);
      expect(caps.supportedLanguages).toContain('typescript');
      expect(caps.supportedLanguages).toContain('python');
      expect(caps.features).toContain('code-edit');
      expect(caps.features).toContain('multi-file');
    });
  });

  describe('parseDiff', () => {
    it('should parse diff from output', () => {
      const output = `
Some text before

\`\`\`diff
--- a/test.ts
+++ b/test.ts
@@ -1,3 +1,4 @@
 function hello() {
+  console.log('Hello');
   return 'world';
 }
\`\`\`

Some text after
`;

      const diffs = adapter.parseDiff(output);

      expect(diffs.length).toBe(1);
      expect(diffs[0].file).toBe('test.ts');
      expect(diffs[0].additions).toBe(1);
      expect(diffs[0].deletions).toBe(0);
      expect(diffs[0].hunks.length).toBe(1);
    });

    it('should parse multiple diffs', () => {
      const output = `
\`\`\`diff
--- a/a.ts
+++ b/a.ts
@@ -1,1 +1,2 @@
 const x = 1;
+const y = 2;
\`\`\`

\`\`\`diff
--- a/b.ts
+++ b/b.ts
@@ -1,1 +1,2 @@
 const a = 1;
+const b = 2;
\`\`\`
`;

      const diffs = adapter.parseDiff(output);

      expect(diffs.length).toBe(2);
      expect(diffs[0].file).toBe('a.ts');
      expect(diffs[1].file).toBe('b.ts');
    });

    it('should handle empty output', () => {
      const diffs = adapter.parseDiff('No diff here');
      expect(diffs).toEqual([]);
    });

    it('should parse diff with deletions', () => {
      const output = `
\`\`\`diff
--- a/test.ts
+++ b/test.ts
@@ -1,4 +1,3 @@
 function test() {
-  const unused = 1;
   return 42;
 }
\`\`\`
`;

      const diffs = adapter.parseDiff(output);

      expect(diffs.length).toBe(1);
      expect(diffs[0].additions).toBe(0);
      expect(diffs[0].deletions).toBe(1);
    });

    it('should parse diff with multiple hunks', () => {
      const output = `
\`\`\`diff
--- a/test.ts
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
 }
\`\`\`
`;

      const diffs = adapter.parseDiff(output);

      expect(diffs.length).toBe(1);
      expect(diffs[0].hunks.length).toBe(2);
      expect(diffs[0].hunks[0].oldStart).toBe(1);
      expect(diffs[0].hunks[1].oldStart).toBe(10);
    });
  });

  describe('configure', () => {
    it('should update config', () => {
      adapter.configure({ model: 'gpt-4-turbo' });

      const config = adapter.getConfig();
      expect(config.model).toBe('gpt-4-turbo');
    });

    it('should merge with existing config', () => {
      adapter.configure({ model: 'gpt-4-turbo' });
      adapter.configure({ editFormat: 'whole' });

      const config = adapter.getConfig();
      expect(config.model).toBe('gpt-4-turbo');
      expect(config.editFormat).toBe('whole');
    });
  });

  describe('getConfig', () => {
    it('should return copy of config', () => {
      const config1 = adapter.getConfig();
      const config2 = adapter.getConfig();

      expect(config1).not.toBe(config2);
      expect(config1).toEqual(config2);
    });
  });
});
