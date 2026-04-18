/**
 * EditorConfigManager 单元测试
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { EditorConfigManager } from '../EditorConfigManager.js';
import { unlink } from 'fs/promises';
import { join } from 'path';
import { tmpdir } from 'os';

describe('EditorConfigManager', () => {
  let manager: EditorConfigManager;
  let testConfigPath: string;

  beforeEach(async () => {
    testConfigPath = join(tmpdir(), `cowork-test-${Date.now()}`, 'editors.json');
    manager = new EditorConfigManager(testConfigPath);
  });

  afterEach(async () => {
    try {
      await unlink(testConfigPath);
    } catch {
      // Ignore
    }
  });

  describe('constructor', () => {
    it('should create manager with default path', () => {
      const m = new EditorConfigManager();
      expect(m).toBeDefined();
    });

    it('should create manager with custom path', () => {
      const m = new EditorConfigManager('/custom/path/editors.json');
      expect(m).toBeDefined();
    });
  });

  describe('setConfig and getConfig', () => {
    it('should set and get config', async () => {
      await manager.setConfig('claude', {
        enabled: true,
        apiKey: 'test-key',
        model: 'claude-sonnet-4-20250514',
      });

      const config = await manager.getConfig('claude');
      expect(config?.enabled).toBe(true);
      expect(config?.model).toBe('claude-sonnet-4-20250514');
    });

    it('should persist CLI editor config fields only', async () => {
      await manager.setConfig('gemini-cli', {
        enabled: true,
        geminiPath: 'gemini',
        model: 'gemini-2.5-pro',
        sandbox: true,
      });

      const config = await manager.getConfig('gemini-cli');
      expect(config).toMatchObject({
        enabled: true,
        geminiPath: 'gemini',
        model: 'gemini-2.5-pro',
        sandbox: true,
      });
      expect(config).not.toHaveProperty('extraArgs');
      expect(config).not.toHaveProperty('cwd');
      expect(config).not.toHaveProperty('env');
      expect(config).not.toHaveProperty('timeout');
    });

    it('should return undefined for unconfigured editor', async () => {
      const config = await manager.getConfig('gemini');
      expect(config).toBeUndefined();
    });
  });

  describe('getAllConfigs', () => {
    it('should return all configs', async () => {
      await manager.setConfig('claude', { enabled: true });
      await manager.setConfig('gemini', { enabled: false });

      const configs = await manager.getAllConfigs();
      expect(configs.claude?.enabled).toBe(true);
      expect(configs.gemini?.enabled).toBe(false);
    });
  });

  describe('isConfigured', () => {
    it('should return false for unconfigured editor', async () => {
      const configured = await manager.isConfigured('claude');
      expect(configured).toBe(false);
    });

    it('should return false for disabled editor', async () => {
      await manager.setConfig('claude', { enabled: false, apiKey: 'key' });
      const configured = await manager.isConfigured('claude');
      expect(configured).toBe(false);
    });

    it('should return true for configured editor with API key', async () => {
      await manager.setConfig('claude', { enabled: true, apiKey: 'test-key' });
      const configured = await manager.isConfigured('claude');
      expect(configured).toBe(true);
    });

    it('should return true for enabled CLI editors without API key', async () => {
      await manager.setConfig('gemini-cli', {
        enabled: true,
        geminiPath: 'gemini',
        model: 'gemini-2.5-pro',
      });
      await manager.setConfig('codex-cli', {
        enabled: true,
        codexPath: 'codex',
        model: 'gpt-5.4',
      });

      await expect(manager.isConfigured('gemini-cli')).resolves.toBe(true);
      await expect(manager.isConfigured('codex-cli')).resolves.toBe(true);
    });

    it('should return true for aider without API key', async () => {
      await manager.setConfig('aider', { enabled: true });
      const configured = await manager.isConfigured('aider');
      expect(configured).toBe(true);
    });
  });

  describe('getEnvApiKey', () => {
    it('should return undefined for aider', () => {
      const key = manager.getEnvApiKey('aider');
      expect(key).toBeUndefined();
    });

    it('should return undefined for CLI editors', () => {
      expect(manager.getEnvApiKey('gemini-cli')).toBeUndefined();
      expect(manager.getEnvApiKey('codex-cli')).toBeUndefined();
    });

    it('should check ANTHROPIC_API_KEY for claude', () => {
      const originalEnv = process.env.ANTHROPIC_API_KEY;
      process.env.ANTHROPIC_API_KEY = 'env-test-key';

      const key = manager.getEnvApiKey('claude');
      expect(key).toBe('env-test-key');

      if (originalEnv) {
        process.env.ANTHROPIC_API_KEY = originalEnv;
      } else {
        delete process.env.ANTHROPIC_API_KEY;
      }
    });
  });

  describe('validateConfig', () => {
    it('should return error for unconfigured editor', async () => {
      const result = await manager.validateConfig('claude');
      expect(result.valid).toBe(false);
      expect(result.errors.length).toBeGreaterThan(0);
    });

    it('should return warning for disabled editor', async () => {
      await manager.setConfig('claude', { enabled: false, apiKey: 'key' });
      const result = await manager.validateConfig('claude');
      expect(result.warnings.some((w) => w.includes('disabled'))).toBe(true);
    });

    it('should return error for missing API key', async () => {
      await manager.setConfig('claude', { enabled: true });
      const result = await manager.validateConfig('claude');
      expect(result.valid).toBe(false);
      expect(result.errors.some((e) => e.includes('API key'))).toBe(true);
    });

    it('should pass validation with API key', async () => {
      await manager.setConfig('claude', { enabled: true, apiKey: 'valid-api-key-here' });
      const result = await manager.validateConfig('claude');
      expect(result.valid).toBe(true);
    });

    it('should validate CLI config without API key requirement', async () => {
      await manager.setConfig('gemini-cli', {
        enabled: true,
        geminiPath: 'gemini',
        model: 'gemini-2.5-pro',
      });
      const result = await manager.validateConfig('gemini-cli');
      expect(result.errors).toEqual([]);
      expect(result.valid).toBe(true);
    });

    it('should warn when CLI executable path does not exist', async () => {
      await manager.setConfig('gemini-cli', {
        enabled: true,
        geminiPath: '/definitely/missing/gemini',
        model: 'gemini-2.5-pro',
      });
      await manager.setConfig('codex-cli', {
        enabled: true,
        codexPath: '/definitely/missing/codex',
        model: 'gpt-5.4',
      });

      const geminiResult = await manager.validateConfig('gemini-cli');
      const codexResult = await manager.validateConfig('codex-cli');

      expect(geminiResult.valid).toBe(true);
      expect(geminiResult.warnings.some((w) => w.includes('Gemini CLI path does not exist'))).toBe(true);
      expect(codexResult.valid).toBe(true);
      expect(codexResult.warnings.some((w) => w.includes('Codex CLI path does not exist'))).toBe(true);
    });

    it('should validate codex-cli against CLI whitelist and path warnings', async () => {
      await manager.setConfig('codex-cli', {
        enabled: true,
        codexPath: '/definitely/missing/codex',
        model: 'gpt-5.4',
      });

      const validResult = await manager.validateConfig('codex-cli');
      expect(validResult.valid).toBe(true);
      expect(validResult.warnings.some((w) => w.includes('Unknown model'))).toBe(false);
      expect(validResult.warnings.some((w) => w.includes('Codex CLI path does not exist'))).toBe(true);

      await manager.setConfig('codex-cli', {
        enabled: true,
        codexPath: 'codex',
        model: 'gpt-4',
      });

      const invalidModelResult = await manager.validateConfig('codex-cli');
      expect(invalidModelResult.valid).toBe(true);
      expect(invalidModelResult.warnings.some((w) => w.includes('Unknown model for codex-cli: gpt-4'))).toBe(true);
    });

    it('should warn for unknown model', async () => {
      await manager.setConfig('claude', {
        enabled: true,
        apiKey: 'valid-api-key-here',
        model: 'unknown-model',
      });
      const result = await manager.validateConfig('claude');
      expect(result.warnings.some((w) => w.includes('Unknown model'))).toBe(true);
    });
  });

  describe('getConfiguredEditors', () => {
    it('should return empty array when no editors configured', async () => {
      const editors = await manager.getConfiguredEditors();
      expect(editors).toEqual([]);
    });

    it('should return configured editors across API and CLI types', async () => {
      await manager.setConfig('claude', { enabled: true, apiKey: 'key' });
      await manager.setConfig('gemini-cli', {
        enabled: true,
        geminiPath: 'gemini',
        model: 'gemini-2.5-pro',
      });
      await manager.setConfig('codex-cli', {
        enabled: true,
        codexPath: 'codex',
        model: 'gpt-5.4',
      });
      await manager.setConfig('aider', { enabled: true });

      const editors = await manager.getConfiguredEditors();
      expect(editors).toContain('claude');
      expect(editors).toContain('gemini-cli');
      expect(editors).toContain('codex-cli');
      expect(editors).toContain('aider');
    });
  });

  describe('reset', () => {
    it('should clear all configs', async () => {
      await manager.setConfig('claude', { enabled: true, apiKey: 'key' });
      await manager.reset();

      const config = await manager.getConfig('claude');
      expect(config).toBeUndefined();
    });
  });
});
