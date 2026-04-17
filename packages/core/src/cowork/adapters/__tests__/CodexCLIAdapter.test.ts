/**
 * CodexCLIAdapter 单元测试
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { CodexCLIAdapter } from '../CodexCLIAdapter.js';
import { CLIProcessManager } from '../../process/CLIProcessManager.js';
import { HookManager } from '../../../hooks/HookManager.js';
import { HookEvent } from '../../../hooks/types.js';

describe('CodexCLIAdapter', () => {
  let processManager: CLIProcessManager;
  let adapter: CodexCLIAdapter;
  let testCwd: string;

  beforeEach(() => {
    processManager = new CLIProcessManager();
    testCwd = process.cwd();
    adapter = new CodexCLIAdapter(
      {
        codexPath: 'node',
      },
      processManager,
    );
    adapter.setHookManager(new HookManager());
  });

  afterEach(async () => {
    await processManager.cleanup();
  });

  describe('constructor', () => {
    it('should create adapter with default config', () => {
      const instance = new CodexCLIAdapter({}, processManager);

      expect(instance.name).toBe('codex-cli');
      expect(instance.version).toBe('0.1.0');
      expect(instance.getConfig().model).toBe('gpt-5.4');
      expect(instance.getConfig().outputLastMessage).toBe(true);
    });

    it('should accept custom config', () => {
      const instance = new CodexCLIAdapter(
        {
          codexPath: 'codex',
          model: 'gpt-5-codex',
          sandbox: 'workspace-write',
          skipGitRepoCheck: true,
          ephemeral: true,
        },
        processManager,
      );

      const config = instance.getConfig();
      expect(config.model).toBe('gpt-5-codex');
      expect(config.sandbox).toBe('workspace-write');
      expect(config.skipGitRepoCheck).toBe(true);
      expect(config.ephemeral).toBe(true);
    });
  });

  describe('execute', () => {
    it('should execute CLI command and return normalized result', async () => {
      adapter.configure({
        extraArgs: ['-e', 'console.log(process.argv.slice(1).join("|"))'],
        model: 'gpt-5-codex',
        sandbox: 'workspace-write',
        skipGitRepoCheck: true,
        ephemeral: true,
        outputLastMessage: false,
      });

      const result = await adapter.execute('hello codex', {
        cwd: testCwd,
      });

      expect(result.exitCode).toBe(0);
      expect(result.stdout).toContain('exec');
      expect(result.stdout).toContain('--model|gpt-5-codex');
      expect(result.stdout).toContain(`--cd|${testCwd}`);
      expect(result.stdout).toContain('--sandbox|workspace-write');
      expect(result.stdout).toContain('--skip-git-repo-check');
      expect(result.stdout).toContain('--ephemeral');
      expect(result.stdout).toContain('hello codex');
    });

    it('should prefer captured last message when output file is written', async () => {
      const outputFile = 'codeflow-captured.txt';
      adapter.configure({
        extraArgs: [
          '-e',
          [
            'const fs = require("fs")',
            'const args = process.argv.slice(1)',
            'const index = args.indexOf("--output-last-message")',
            'if (index >= 0) { fs.writeFileSync(args[index + 1], "captured output") }',
            'console.log("fallback output")',
          ].join(';'),
        ],
        outputDirectory: process.cwd(),
      });

      const result = await adapter.execute(outputFile);

      expect(result.exitCode).toBe(0);
      expect(result.stdout).toBe('captured output');
    });

    it('should run hook manager before send and after response', async () => {
      adapter.configure({
        extraArgs: ['-e', 'console.log(process.argv.slice(1).join("|"))'],
        outputLastMessage: false,
      });

      const hookManager = adapter.getHookManager();
      let postResponse = '';
      hookManager?.register(HookEvent.BEFORE_SEND, async (payload) => ({
        ...payload,
        messages: [{ role: 'user', content: 'rewritten prompt' }],
        model: 'hooked-model',
      }));
      hookManager?.register(HookEvent.POST_RESPONSE, async (payload) => {
        postResponse = payload.content;
      });

      await adapter.execute('original prompt');

      expect(postResponse).toContain('--model|hooked-model');
      expect(postResponse).toContain('rewritten prompt');
    });
  });

  describe('stream', () => {
    it('should stream stdout chunks and invoke stream hooks', async () => {
      adapter.configure({
        extraArgs: ['-e', 'console.log("chunk-1"); console.log("chunk-2")'],
        outputLastMessage: false,
      });

      const chunks: string[] = [];
      const streamEvents: Array<{ delta: string; done: boolean }> = [];
      adapter.getHookManager()?.register(HookEvent.ON_STREAM, (payload) => {
        streamEvents.push({ delta: payload.delta, done: payload.done });
      });

      await adapter.stream('ignored', (chunk) => {
        chunks.push(chunk);
      });

      expect(chunks.join('')).toContain('chunk-1');
      expect(chunks.join('')).toContain('chunk-2');
      expect(streamEvents.some((event) => event.delta.includes('chunk-1'))).toBe(true);
      expect(streamEvents.at(-1)?.done).toBe(true);
    });
  });

  describe('interrupt', () => {
    it('should interrupt current process', async () => {
      adapter.configure({
        extraArgs: ['-e', 'setTimeout(() => console.log("late"), 5000)'],
        outputLastMessage: false,
      });

      const run = adapter.stream('ignored', () => {}).catch((error) => error as Error);
      await new Promise((resolve) => setTimeout(resolve, 100));
      await adapter.interrupt();
      const outcome = await run;

      expect(outcome).toBeInstanceOf(Error);
    });
  });

  describe('healthCheck', () => {
    it('should return true when version command reports codex', async () => {
      const healthy = new CodexCLIAdapter({ codexPath: 'echo', extraArgs: ['codex'] }, processManager);

      await expect(healthy.healthCheck()).resolves.toBe(true);
    });

    it('should return false for invalid path', async () => {
      const invalid = new CodexCLIAdapter({ codexPath: 'nonexistent-codex-command-12345' }, processManager);

      await expect(invalid.healthCheck()).resolves.toBe(false);
    });
  });

  describe('getCapabilities', () => {
    it('should expose CLI lifecycle capabilities', () => {
      const capabilities = adapter.getCapabilities();

      expect(capabilities.supportsStreaming).toBe(true);
      expect(capabilities.supportsInterrupt).toBe(true);
      expect(capabilities.supportedLanguages).toContain('typescript');
      expect(capabilities.features).toContain('hook-aware');
      expect(capabilities.features).toContain('non-interactive');
    });
  });

  describe('configure/getConfig', () => {
    it('should merge config without mutating returned copies', () => {
      adapter.configure({
        env: { TEST_KEY: 'value' },
        extraArgs: ['--example'],
      });

      const config = adapter.getConfig();
      config.env!.TEST_KEY = 'changed';
      config.extraArgs!.push('--second');

      const nextConfig = adapter.getConfig();
      expect(nextConfig.env?.TEST_KEY).toBe('value');
      expect(nextConfig.extraArgs).toEqual(['--example']);
    });
  });
});
