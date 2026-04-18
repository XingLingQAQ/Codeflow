import { describe, it, expect, beforeEach, vi } from 'vitest';
import { ConfigManager } from '../ConfigManager.js';
import { GlobalConfig, SessionConfig, RoleConfig, APIChannel } from '../types.js';

describe('ConfigManager', () => {
  let manager: ConfigManager;

  beforeEach(() => {
    manager = new ConfigManager();
  });

  describe('constructor', () => {
    it('should create manager with default config', () => {
      const m = new ConfigManager();
      expect(m).toBeDefined();

      const global = m.loadGlobalConfig();
      expect(global.defaultModel).toBe('claude-3-5-sonnet-20241022');
    });

    it('should create manager with custom initial config', () => {
      const m = new ConfigManager({
        defaultModel: 'gpt-4',
        summaryThreshold: 10000,
      });

      const global = m.loadGlobalConfig();
      expect(global.defaultModel).toBe('gpt-4');
      expect(global.summaryThreshold).toBe(10000);
    });

    it('should merge initial config with defaults', () => {
      const m = new ConfigManager({
        defaultModel: 'custom-model',
      });

      const global = m.loadGlobalConfig();
      expect(global.defaultModel).toBe('custom-model');
      expect(global.maxRetries).toBe(3); // Default value preserved
      expect(global.timeout).toBe(60000); // Default value preserved
    });
  });

  describe('loadGlobalConfig', () => {
    it('should return copy of global config', () => {
      const config1 = manager.loadGlobalConfig();
      const config2 = manager.loadGlobalConfig();

      expect(config1).not.toBe(config2);
      expect(config1).toEqual(config2);
    });

    it('should include default values', () => {
      const config = manager.loadGlobalConfig();

      expect(config.defaultModel).toBeDefined();
      expect(config.apiPool).toBeDefined();
      expect(config.publicMcp).toBeDefined();
    });
  });

  describe('loadSessionConfig', () => {
    it('should return null for non-existent session', () => {
      const config = manager.loadSessionConfig('non-existent');
      expect(config).toBeNull();
    });

    it('should return saved session config', () => {
      const sessionConfig: SessionConfig = {
        sessionId: 'test-session',
        mode: 'development',
        temperature: 0.5,
      };

      manager.saveSessionConfig(sessionConfig);
      const loaded = manager.loadSessionConfig('test-session');

      expect(loaded).toEqual(sessionConfig);
    });

    it('should return copy of session config', () => {
      const sessionConfig: SessionConfig = {
        sessionId: 'test-session',
        mode: 'research',
      };

      manager.saveSessionConfig(sessionConfig);

      const config1 = manager.loadSessionConfig('test-session');
      const config2 = manager.loadSessionConfig('test-session');

      expect(config1).not.toBe(config2);
      expect(config1).toEqual(config2);
    });
  });

  describe('loadRoleConfig', () => {
    it('should return default config for main role', () => {
      const config = manager.loadRoleConfig('main');

      expect(config).toBeDefined();
      expect(config?.model).toBe('claude-3-5-sonnet-20241022');
      expect(config?.temperature).toBe(1.0);
      expect(config?.answerStyle).toBe('balanced');
    });

    it('should return default config for coder role', () => {
      const config = manager.loadRoleConfig('coder');

      expect(config).toBeDefined();
      expect(config?.temperature).toBe(0.7);
      expect(config?.mcpTools).toContain('filesystem');
    });

    it('should return default config for sub role', () => {
      const config = manager.loadRoleConfig('sub');

      expect(config).toBeDefined();
      expect(config?.model).toBe('claude-3-5-haiku-20241022');
    });

    it('should return saved role config', () => {
      const roleConfig: RoleConfig = {
        model: 'custom-model',
        temperature: 0.9,
        apiChannel: 'custom-channel',
        mcpTools: ['custom-tool'],
        systemPrompt: 'Custom prompt',
      };

      manager.saveRoleConfig('main', roleConfig);
      const loaded = manager.loadRoleConfig('main');

      expect(loaded).toEqual(roleConfig);
    });
  });

  describe('saveGlobalConfig', () => {
    it('should save and load global config', () => {
      const newConfig: GlobalConfig = {
        defaultModel: 'new-model',
        apiPool: [],
        publicMcp: ['tool1', 'tool2'],
        summaryThreshold: 15000,
      };

      manager.saveGlobalConfig(newConfig);
      const loaded = manager.loadGlobalConfig();

      expect(loaded.defaultModel).toBe('new-model');
      expect(loaded.publicMcp).toEqual(['tool1', 'tool2']);
    });

    it('should notify change listeners', () => {
      const callback = vi.fn();
      manager.onConfigChange(callback);

      manager.saveGlobalConfig({
        defaultModel: 'test',
        apiPool: [],
        publicMcp: [],
      });

      expect(callback).toHaveBeenCalled();
    });
  });

  describe('saveSessionConfig', () => {
    it('should save session config', () => {
      const config: SessionConfig = {
        sessionId: 'session-1',
        mode: 'creative',
        overrideModel: 'creative-model',
      };

      manager.saveSessionConfig(config);
      const loaded = manager.loadSessionConfig('session-1');

      expect(loaded?.mode).toBe('creative');
      expect(loaded?.overrideModel).toBe('creative-model');
    });

    it('should overwrite existing session config', () => {
      manager.saveSessionConfig({
        sessionId: 'session-1',
        mode: 'development',
      });

      manager.saveSessionConfig({
        sessionId: 'session-1',
        mode: 'research',
        temperature: 0.8,
      });

      const loaded = manager.loadSessionConfig('session-1');
      expect(loaded?.mode).toBe('research');
      expect(loaded?.temperature).toBe(0.8);
    });
  });

  describe('saveRoleConfig', () => {
    it('should save role config', () => {
      const config: RoleConfig = {
        model: 'role-model',
        temperature: 0.6,
        apiChannel: 'channel-1',
        mcpTools: ['tool1'],
        systemPrompt: 'Role prompt',
      };

      manager.saveRoleConfig('coder', config);
      const loaded = manager.loadRoleConfig('coder');

      expect(loaded?.model).toBe('role-model');
      expect(loaded?.temperature).toBe(0.6);
    });
  });

  describe('resolveConfig', () => {
    it('should return global defaults when no session or role', () => {
      const resolved = manager.resolveConfig();

      expect(resolved.model).toBe('claude-3-5-sonnet-20241022');
      expect(resolved.temperature).toBe(1.0);
    });

    it('should apply session overrides', () => {
      manager.saveSessionConfig({
        sessionId: 'test-session',
        mode: 'development',
        overrideModel: 'session-model',
        temperature: 0.5,
        maxTokens: 2000,
      });

      const resolved = manager.resolveConfig('test-session');

      expect(resolved.model).toBe('session-model');
      expect(resolved.temperature).toBe(0.5);
      expect(resolved.maxTokens).toBe(2000);
    });

    it('should apply role overrides (highest priority)', () => {
      manager.saveSessionConfig({
        sessionId: 'test-session',
        mode: 'development',
        overrideModel: 'session-model',
        temperature: 0.5,
      });

      manager.saveRoleConfig('coder', {
        model: 'role-model',
        temperature: 0.7,
        apiChannel: 'default',
        mcpTools: ['coder-tool'],
        systemPrompt: 'Coder prompt',
      });

      const resolved = manager.resolveConfig('test-session', 'coder');

      // Role config takes precedence
      expect(resolved.model).toBe('role-model');
      expect(resolved.temperature).toBe(0.7);
      expect(resolved.systemPrompt).toBe('Coder prompt');
    });

    it('should merge MCP tools from global and role', () => {
      manager.saveGlobalConfig({
        defaultModel: 'test',
        apiPool: [],
        publicMcp: ['global-tool'],
      });

      manager.saveRoleConfig('main', {
        model: 'test',
        temperature: 1.0,
        apiChannel: 'default',
        mcpTools: ['role-tool'],
        systemPrompt: 'Test',
      });

      const resolved = manager.resolveConfig(undefined, 'main');

      expect(resolved.mcpTools).toContain('global-tool');
      expect(resolved.mcpTools).toContain('role-tool');
    });

    it('should deduplicate MCP tools', () => {
      manager.saveGlobalConfig({
        defaultModel: 'test',
        apiPool: [],
        publicMcp: ['shared-tool', 'global-tool'],
      });

      manager.saveRoleConfig('main', {
        model: 'test',
        temperature: 1.0,
        apiChannel: 'default',
        mcpTools: ['shared-tool', 'role-tool'],
        systemPrompt: 'Test',
      });

      const resolved = manager.resolveConfig(undefined, 'main');

      const sharedToolCount = resolved.mcpTools.filter(t => t === 'shared-tool').length;
      expect(sharedToolCount).toBe(1);
    });

    it('should resolve API channel', () => {
      const channel: APIChannel = {
        id: 'test-channel',
        name: 'Test Channel',
        provider: 'anthropic',
        enabled: true,
      };

      manager.addApiChannel(channel);

      manager.saveRoleConfig('main', {
        model: 'test',
        temperature: 1.0,
        apiChannel: 'test-channel',
        mcpTools: [],
        systemPrompt: 'Test',
      });

      const resolved = manager.resolveConfig(undefined, 'main');

      expect(resolved.apiChannel).toBeDefined();
      expect(resolved.apiChannel?.id).toBe('test-channel');
    });

    it('should use default role config when not customized', () => {
      const resolved = manager.resolveConfig(undefined, 'coder');

      expect(resolved.model).toBe('claude-3-5-sonnet-20241022');
      expect(resolved.temperature).toBe(0.7);
      expect(resolved.mcpTools).toContain('filesystem');
    });

    it('should carry runtime metadata from role config', () => {
      manager.saveRoleConfig('coder', {
        model: 'role-model',
        temperature: 0.7,
        apiChannel: 'default',
        mcpTools: ['coder-tool'],
        systemPrompt: 'Coder prompt',
        answerStyle: 'precise',
        capabilities: ['code', 'diff'],
        allowedSkills: ['skill.inspect'],
        allowedHooks: ['before_send'],
      } as RoleConfig & {
        answerStyle?: string;
        capabilities?: string[];
        allowedSkills?: string[];
        allowedHooks?: string[];
      });

      const resolved = manager.resolveConfig(undefined, 'coder');

      expect(resolved.answerStyle).toBe('precise');
      expect(resolved.capabilities).toEqual(['code', 'diff']);
      expect(resolved.allowedSkills).toEqual(['skill.inspect']);
      expect(resolved.allowedHooks).toEqual(['before_send']);
    });

    it('should include timeout and maxRetries from global', () => {
      const resolved = manager.resolveConfig();

      expect(resolved.timeout).toBe(60000);
      expect(resolved.maxRetries).toBe(3);
    });
  });

  describe('onConfigChange', () => {
    it('should register callback', () => {
      const callback = vi.fn();
      manager.onConfigChange(callback);

      manager.saveGlobalConfig({
        defaultModel: 'test',
        apiPool: [],
        publicMcp: [],
      });

      expect(callback).toHaveBeenCalledTimes(1);
    });

    it('should return unsubscribe function', () => {
      const callback = vi.fn();
      const unsubscribe = manager.onConfigChange(callback);

      unsubscribe();

      manager.saveGlobalConfig({
        defaultModel: 'test',
        apiPool: [],
        publicMcp: [],
      });

      expect(callback).not.toHaveBeenCalled();
    });

    it('should support multiple callbacks', () => {
      const callback1 = vi.fn();
      const callback2 = vi.fn();

      manager.onConfigChange(callback1);
      manager.onConfigChange(callback2);

      manager.saveGlobalConfig({
        defaultModel: 'test',
        apiPool: [],
        publicMcp: [],
      });

      expect(callback1).toHaveBeenCalled();
      expect(callback2).toHaveBeenCalled();
    });

    it('should pass config hierarchy to callback', () => {
      const callback = vi.fn();
      manager.onConfigChange(callback);

      manager.saveGlobalConfig({
        defaultModel: 'test-model',
        apiPool: [],
        publicMcp: [],
      });

      expect(callback).toHaveBeenCalledWith(
        expect.objectContaining({
          global: expect.objectContaining({
            defaultModel: 'test-model',
          }),
        })
      );
    });
  });

  describe('addApiChannel', () => {
    it('should add new API channel', () => {
      const channel: APIChannel = {
        id: 'new-channel',
        name: 'New Channel',
        provider: 'openai',
        enabled: true,
      };

      manager.addApiChannel(channel);

      const global = manager.loadGlobalConfig();
      expect(global.apiPool).toContainEqual(channel);
    });

    it('should update existing API channel', () => {
      const channel1: APIChannel = {
        id: 'channel-1',
        name: 'Channel 1',
        provider: 'anthropic',
        enabled: true,
      };

      const channel2: APIChannel = {
        id: 'channel-1',
        name: 'Updated Channel',
        provider: 'openai',
        enabled: false,
      };

      manager.addApiChannel(channel1);
      manager.addApiChannel(channel2);

      const global = manager.loadGlobalConfig();
      const found = global.apiPool.find(ch => ch.id === 'channel-1');

      expect(found?.name).toBe('Updated Channel');
      expect(found?.provider).toBe('openai');
      expect(found?.enabled).toBe(false);
    });

    it('should notify change listeners', () => {
      const callback = vi.fn();
      manager.onConfigChange(callback);

      manager.addApiChannel({
        id: 'test',
        name: 'Test',
        provider: 'google',
        enabled: true,
      });

      expect(callback).toHaveBeenCalled();
    });
  });

  describe('removeApiChannel', () => {
    it('should remove existing API channel', () => {
      manager.addApiChannel({
        id: 'to-remove',
        name: 'To Remove',
        provider: 'anthropic',
        enabled: true,
      });

      const result = manager.removeApiChannel('to-remove');

      expect(result).toBe(true);

      const global = manager.loadGlobalConfig();
      expect(global.apiPool.find(ch => ch.id === 'to-remove')).toBeUndefined();
    });

    it('should return false for non-existent channel', () => {
      const result = manager.removeApiChannel('non-existent');
      expect(result).toBe(false);
    });

    it('should notify change listeners on removal', () => {
      manager.addApiChannel({
        id: 'test',
        name: 'Test',
        provider: 'anthropic',
        enabled: true,
      });

      const callback = vi.fn();
      manager.onConfigChange(callback);

      manager.removeApiChannel('test');

      expect(callback).toHaveBeenCalled();
    });
  });

  describe('detectConflicts', () => {
    it('should return empty array when no conflicts', () => {
      const conflicts = manager.detectConflicts();
      expect(conflicts).toEqual([]);
    });

    it('should detect non-existent API channel reference', () => {
      manager.saveRoleConfig('main', {
        model: 'test',
        temperature: 1.0,
        apiChannel: 'non-existent-channel',
        mcpTools: [],
        systemPrompt: 'Test',
      });

      const conflicts = manager.detectConflicts();

      expect(conflicts.length).toBeGreaterThan(0);
      expect(conflicts[0]).toContain('non-existent');
    });

    it('should detect disabled API channel reference', () => {
      manager.addApiChannel({
        id: 'disabled-channel',
        name: 'Disabled',
        provider: 'anthropic',
        enabled: false,
      });

      manager.saveRoleConfig('coder', {
        model: 'test',
        temperature: 0.7,
        apiChannel: 'disabled-channel',
        mcpTools: [],
        systemPrompt: 'Test',
      });

      const conflicts = manager.detectConflicts();

      expect(conflicts.length).toBeGreaterThan(0);
      expect(conflicts[0]).toContain('disabled');
    });

    it('should not report conflict for default channel', () => {
      // Default role configs use 'default' channel
      const conflicts = manager.detectConflicts();
      expect(conflicts).toEqual([]);
    });

    it('should check all roles for conflicts', () => {
      manager.saveRoleConfig('main', {
        model: 'test',
        temperature: 1.0,
        apiChannel: 'missing-1',
        mcpTools: [],
        systemPrompt: 'Test',
      });

      manager.saveRoleConfig('coder', {
        model: 'test',
        temperature: 0.7,
        apiChannel: 'missing-2',
        mcpTools: [],
        systemPrompt: 'Test',
      });

      const conflicts = manager.detectConflicts();

      expect(conflicts.length).toBe(2);
    });
  });

  describe('EventEmitter integration', () => {
    it('should emit change event', () => {
      const handler = vi.fn();
      manager.on('change', handler);

      manager.saveGlobalConfig({
        defaultModel: 'test',
        apiPool: [],
        publicMcp: [],
      });

      expect(handler).toHaveBeenCalled();
    });

    it('should emit change event with config hierarchy', () => {
      const handler = vi.fn();
      manager.on('change', handler);

      manager.saveRoleConfig('main', {
        model: 'test',
        temperature: 1.0,
        apiChannel: 'default',
        mcpTools: [],
        systemPrompt: 'Test',
      });

      expect(handler).toHaveBeenCalledWith(
        expect.objectContaining({
          global: expect.any(Object),
          role: expect.objectContaining({
            main: expect.any(Object),
          }),
        })
      );
    });
  });
});
