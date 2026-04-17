/**
 * GeminiCLIAdapter 单元测试
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { GeminiCLIAdapter } from '../GeminiCLIAdapter.js';
import { CLIProcessManager } from '../../process/CLIProcessManager.js';
import { HookManager } from '../../../hooks/HookManager.js';
import { HookEvent } from '../../../hooks/types.js';

describe('GeminiCLIAdapter', () => {
  let processManager: CLIProcessManager;
  let adapter: GeminiCLIAdapter;
  let testCwd: string;

  beforeEach(() => {
    processManager = new CLIProcessManager();
    testCwd = process.cwd();
    adapter = new GeminiCLIAdapter(
      {
        geminiPath: 'node',
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
      const instance = new GeminiCLIAdapter({}, processManager);

      expect(instance.name).toBe('gemini-cli');
      expect(instance.version).toBe('0.1.0');
      expect(instance.getConfig().model).toBe('gemini-2.0-flash-exp');
      expect(instance.getConfig().includeDirectories).toEqual([]);
    });

    it('should accept custom config', () => {
      const instance = new GeminiCLIAdapter(
        {
          geminiPath: 'gemini',
          model: 'gemini-2.5-pro',
          sandbox: true,
          includeDirectories: ['src', 'tests'],
        },
        processManager,
      );

      const config = instance.getConfig();
      expect(config.model).toBe('gemini-2.5-pro');
      expect(config.sandbox).toBe(true);
      expect(config.includeDirectories).toEqual(['src', 'tests']);
    });
  });

  describe('execute', () => {
    it('should execute CLI command and return normalized result', async () => {
      adapter.configure({
        extraArgs: ['-e', 'console.log(process.argv.slice(1).join("|"))'],
        model: 'gemini-2.5-pro',
        sandbox: true,
        includeDirectories: ['src', 'tests'],
      });

      const result = await adapter.execute('hello gemini', {
        cwd: testCwd,
      });

      expect(result.exitCode).toBe(0);
      expect(result.stdout).toContain('-m|gemini-2.5-pro');
      expect(result.stdout).toContain('--sandbox');
      expect(result.stdout).toContain('--include-directories|src');
      expect(result.stdout).toContain('--include-directories|tests');
      expect(result.stdout).toContain('-p|hello gemini');
    });

    it('should run hook manager before send and after response', async () => {
      adapter.configure({
        extraArgs: ['-e', 'console.log(process.argv.slice(1).join("|"))'],
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

      expect(postResponse).toContain('-m|hooked-model');
      expect(postResponse).toContain('-p|rewritten prompt');
    });
  });

  describe('stream', () => {
    it('should stream stdout chunks and invoke stream hooks', async () => {
      adapter.configure({
        extraArgs: [
          '-e',
          'console.log(process.argv.slice(1).join("|")); console.log("chunk-1"); console.log("chunk-2")',
        ],
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
      expect(chunks.join('')).toContain('--output-format|stream-json');
      expect(streamEvents.some((event) => event.delta.includes('chunk-1'))).toBe(true);
      expect(streamEvents.at(-1)?.done).toBe(true);
    });
  });

  describe('interrupt', () => {
    it('should interrupt current process', async () => {
      adapter.configure({
        extraArgs: ['-e', 'setTimeout(() => console.log("late"), 5000)'],
      });

      const run = adapter.stream('ignored', () => {}).catch((error) => error as Error);
      await new Promise((resolve) => setTimeout(resolve, 100));
      await adapter.interrupt();
      const outcome = await run;

      expect(outcome).toBeInstanceOf(Error);
    });
  });

  describe('healthCheck', () => {
    it('should return true when version command reports gemini', async () => {
      const healthy = new GeminiCLIAdapter({ geminiPath: 'echo', extraArgs: ['gemini'] }, processManager);

      await expect(healthy.healthCheck()).resolves.toBe(true);
    });

    it('should return false for invalid path', async () => {
      const invalid = new GeminiCLIAdapter({ geminiPath: 'nonexistent-gemini-command-12345' }, processManager);

      await expect(invalid.healthCheck()).resolves.toBe(false);
    });
  });

  describe('getCapabilities', () => {
    it('should expose truthful Gemini CLI capabilities', () => {
      const capabilities = adapter.getCapabilities();

      expect(capabilities.supportsStreaming).toBe(true);
      expect(capabilities.supportsInterrupt).toBe(true);
      expect(capabilities.supportedLanguages).toContain('typescript');
      expect(capabilities.features).toContain('hook-aware');
      expect(capabilities.features).toContain('stream-json-events');
      expect(capabilities.features).toContain('multimodal-input-unsupported');
    });
  });

  describe('configure/getConfig', () => {
    it('should merge config without mutating returned copies', () => {
      adapter.configure({
        env: { TEST_KEY: 'value' },
        extraArgs: ['--example'],
        includeDirectories: ['src'],
      });

      const config = adapter.getConfig();
      config.env!.TEST_KEY = 'changed';
      config.extraArgs!.push('--second');
      config.includeDirectories!.push('tests');

      const nextConfig = adapter.getConfig();
      expect(nextConfig.env?.TEST_KEY).toBe('value');
      expect(nextConfig.extraArgs).toEqual(['--example']);
      expect(nextConfig.includeDirectories).toEqual(['src']);
    });
  });
});
