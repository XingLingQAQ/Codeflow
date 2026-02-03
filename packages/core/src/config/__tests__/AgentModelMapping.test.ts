import { describe, it, expect, beforeEach, vi } from 'vitest';
import { ModelRegistry } from '../ModelRegistry.js';
import {
  AgentModelMapping,
  AgentType,
  TaskType,
  AgentModelConfig,
  ALL_AGENTS,
  ALL_TASK_TYPES,
} from '../AgentModelMapping.js';

describe('AgentModelMapping', () => {
  let registry: ModelRegistry;
  let mapping: AgentModelMapping;

  beforeEach(() => {
    registry = new ModelRegistry();
    mapping = new AgentModelMapping(registry);
  });

  describe('constructor', () => {
    it('should load default mappings', () => {
      const allMappings = mapping.getAllMappings();
      expect(allMappings.length).toBe(ALL_AGENTS.length);
    });

    it('should have mappings for all agents', () => {
      for (const agent of ALL_AGENTS) {
        const config = mapping.getAgentConfig(agent);
        expect(config).toBeDefined();
        expect(config?.agent).toBe(agent);
      }
    });
  });

  describe('getAgentConfig', () => {
    it('should return config for valid agent', () => {
      const config = mapping.getAgentConfig('main');

      expect(config).toBeDefined();
      expect(config?.agent).toBe('main');
      expect(config?.modelId).toBeDefined();
    });

    it('should return undefined for invalid agent', () => {
      const config = mapping.getAgentConfig('invalid' as AgentType);
      expect(config).toBeUndefined();
    });

    it('should return a copy of the config', () => {
      const config1 = mapping.getAgentConfig('main');
      const config2 = mapping.getAgentConfig('main');

      expect(config1).not.toBe(config2);
      expect(config1).toEqual(config2);
    });
  });

  describe('getModelForAgent', () => {
    it('should return model ID for valid agent', () => {
      const modelId = mapping.getModelForAgent('main');
      expect(modelId).toBeDefined();
      expect(registry.hasModel(modelId!)).toBe(true);
    });

    it('should return fallback model if primary not available', () => {
      registry.removeModel('claude-opus-4');

      const modelId = mapping.getModelForAgent('main');
      expect(modelId).toBe('claude-sonnet-4');
    });

    it('should return undefined if no models available', () => {
      registry.removeModel('claude-opus-4');
      registry.removeModel('claude-sonnet-4');

      const modelId = mapping.getModelForAgent('main');
      expect(modelId).toBeUndefined();
    });

    it('should return task type override for coder agent', () => {
      const modelId = mapping.getModelForAgent('coder', 'frontend');
      expect(modelId).toBe('gemini-2.0-flash');
    });

    it('should return default model if task type override not available', () => {
      registry.removeModel('gemini-2.0-flash');

      const modelId = mapping.getModelForAgent('coder', 'frontend');
      expect(modelId).toBe('claude-haiku-4');
    });

    it('should return default model for default task type', () => {
      const modelId = mapping.getModelForAgent('coder', 'default');
      expect(modelId).toBe('claude-haiku-4');
    });
  });

  describe('getModelDefinitionForAgent', () => {
    it('should return model definition for valid agent', () => {
      const model = mapping.getModelDefinitionForAgent('main');

      expect(model).toBeDefined();
      expect(model?.id).toBeDefined();
      expect(model?.name).toBeDefined();
    });

    it('should return undefined for invalid agent', () => {
      const model = mapping.getModelDefinitionForAgent('invalid' as AgentType);
      expect(model).toBeUndefined();
    });
  });

  describe('setModelForAgent', () => {
    it('should set model for agent', () => {
      const result = mapping.setModelForAgent('coder', 'claude-sonnet-4');

      expect(result).toBe(true);
      expect(mapping.getModelForAgent('coder')).toBe('claude-sonnet-4');
    });

    it('should return false for non-existent model', () => {
      const result = mapping.setModelForAgent('coder', 'non-existent');
      expect(result).toBe(false);
    });

    it('should return false if model lacks required capabilities', () => {
      // claude-haiku-4 doesn't have 'reasoning' capability required for main
      const result = mapping.setModelForAgent('main', 'claude-haiku-4');
      expect(result).toBe(false);
    });

    it('should emit mapping:changed event', () => {
      const listener = vi.fn();
      mapping.on('mapping:changed', listener);

      mapping.setModelForAgent('coder', 'claude-sonnet-4');

      expect(listener).toHaveBeenCalledWith('coder', 'claude-sonnet-4');
    });
  });

  describe('setTaskTypeOverride', () => {
    it('should set task type override', () => {
      const result = mapping.setTaskTypeOverride('coder', 'debug', 'claude-opus-4');

      expect(result).toBe(true);
      expect(mapping.getModelForAgent('coder', 'debug')).toBe('claude-opus-4');
    });

    it('should return false for non-existent model', () => {
      const result = mapping.setTaskTypeOverride('coder', 'debug', 'non-existent');
      expect(result).toBe(false);
    });

    it('should emit mapping:changed event with task type', () => {
      const listener = vi.fn();
      mapping.on('mapping:changed', listener);

      mapping.setTaskTypeOverride('coder', 'debug', 'claude-opus-4');

      expect(listener).toHaveBeenCalledWith('coder', 'claude-opus-4', 'debug');
    });
  });

  describe('removeTaskTypeOverride', () => {
    it('should remove task type override', () => {
      const result = mapping.removeTaskTypeOverride('coder', 'frontend');

      expect(result).toBe(true);
      // Should now return default model
      expect(mapping.getModelForAgent('coder', 'frontend')).toBe('claude-haiku-4');
    });

    it('should return false if no overrides exist', () => {
      const result = mapping.removeTaskTypeOverride('main', 'frontend');
      expect(result).toBe(false);
    });
  });

  describe('getTaskTypeOverrides', () => {
    it('should return task type overrides for coder', () => {
      const overrides = mapping.getTaskTypeOverrides('coder');

      expect(overrides).toBeDefined();
      expect(overrides?.frontend).toBe('gemini-2.0-flash');
      expect(overrides?.backend).toBe('claude-haiku-4');
    });

    it('should return undefined for agent without overrides', () => {
      const overrides = mapping.getTaskTypeOverrides('main');
      expect(overrides).toBeUndefined();
    });
  });

  describe('setFallbackModelForAgent', () => {
    it('should set fallback model', () => {
      const result = mapping.setFallbackModelForAgent('main', 'gemini-2.5-pro');

      expect(result).toBe(true);
      const config = mapping.getAgentConfig('main');
      expect(config?.fallbackModelId).toBe('gemini-2.5-pro');
    });

    it('should return false for non-existent model', () => {
      const result = mapping.setFallbackModelForAgent('main', 'non-existent');
      expect(result).toBe(false);
    });
  });

  describe('getAllMappings', () => {
    it('should return all mappings', () => {
      const allMappings = mapping.getAllMappings();

      expect(allMappings.length).toBe(ALL_AGENTS.length);
      for (const config of allMappings) {
        expect(ALL_AGENTS).toContain(config.agent);
      }
    });

    it('should return copies of mappings', () => {
      const mappings1 = mapping.getAllMappings();
      const mappings2 = mapping.getAllMappings();

      expect(mappings1).not.toBe(mappings2);
      expect(mappings1[0]).not.toBe(mappings2[0]);
    });
  });

  describe('getMappingsAsRecord', () => {
    it('should return mappings as record', () => {
      const record = mapping.getMappingsAsRecord();

      expect(typeof record).toBe('object');
      for (const agent of ALL_AGENTS) {
        expect(record[agent]).toBeDefined();
      }
    });
  });

  describe('setMappings', () => {
    it('should set multiple mappings', () => {
      mapping.setMappings({
        coder: 'claude-sonnet-4',
        dispatch: 'claude-sonnet-4',
      });

      expect(mapping.getModelForAgent('coder')).toBe('claude-sonnet-4');
      expect(mapping.getModelForAgent('dispatch')).toBe('claude-sonnet-4');
    });

    it('should skip invalid mappings', () => {
      const originalModel = mapping.getModelForAgent('main');

      mapping.setMappings({
        main: 'non-existent',
      });

      expect(mapping.getModelForAgent('main')).toBe(originalModel);
    });
  });

  describe('reset', () => {
    it('should reset to default mappings', () => {
      mapping.setModelForAgent('coder', 'claude-sonnet-4');
      mapping.reset();

      const config = mapping.getAgentConfig('coder');
      expect(config?.modelId).toBe('claude-haiku-4');
    });

    it('should emit mapping:reset event', () => {
      const listener = vi.fn();
      mapping.on('mapping:reset', listener);

      mapping.reset();

      expect(listener).toHaveBeenCalled();
    });
  });

  describe('validateMappings', () => {
    it('should return valid for default mappings', () => {
      const result = mapping.validateMappings();

      expect(result.valid).toBe(true);
      expect(result.errors).toEqual([]);
    });

    it('should return errors for missing models', () => {
      registry.removeModel('claude-opus-4');

      const result = mapping.validateMappings();

      expect(result.valid).toBe(false);
      expect(result.errors.length).toBeGreaterThan(0);
    });
  });

  describe('getSuitableModelsForAgent', () => {
    it('should return models with required capabilities', () => {
      const models = mapping.getSuitableModelsForAgent('main');

      expect(models.length).toBeGreaterThan(0);
      for (const model of models) {
        expect(model.capabilities).toContain('reasoning');
      }
    });
  });

  describe('estimateTotalCost', () => {
    it('should estimate total cost for all agents', () => {
      const cost = mapping.estimateTotalCost(10000);

      expect(cost).toBeGreaterThan(0);
    });

    it('should use default tokens per agent', () => {
      const cost = mapping.estimateTotalCost();

      expect(cost).toBeGreaterThan(0);
    });
  });

  describe('getCostOptimizedMappings', () => {
    it('should return cost-optimized mappings', () => {
      const optimized = mapping.getCostOptimizedMappings();

      expect(typeof optimized).toBe('object');
      for (const agent of ALL_AGENTS) {
        expect(optimized[agent]).toBeDefined();
      }
    });
  });

  describe('getQualityOptimizedMappings', () => {
    it('should return quality-optimized mappings', () => {
      const optimized = mapping.getQualityOptimizedMappings();

      expect(typeof optimized).toBe('object');
      for (const agent of ALL_AGENTS) {
        expect(optimized[agent]).toBeDefined();
      }
    });
  });

  describe('exportConfig', () => {
    it('should export all configurations', () => {
      const exported = mapping.exportConfig();

      expect(exported.length).toBe(ALL_AGENTS.length);
      for (const config of exported) {
        expect(config.agent).toBeDefined();
        expect(config.modelId).toBeDefined();
      }
    });
  });

  describe('importConfig', () => {
    it('should import configurations', () => {
      const configs: AgentModelConfig[] = [
        { agent: 'coder', modelId: 'claude-sonnet-4' },
        { agent: 'dispatch', modelId: 'claude-sonnet-4' },
      ];

      mapping.importConfig(configs);

      expect(mapping.getAgentConfig('coder')?.modelId).toBe('claude-sonnet-4');
      expect(mapping.getAgentConfig('dispatch')?.modelId).toBe('claude-sonnet-4');
    });

    it('should skip invalid agents', () => {
      const configs: AgentModelConfig[] = [
        { agent: 'invalid' as AgentType, modelId: 'claude-sonnet-4' },
      ];

      mapping.importConfig(configs);

      expect(mapping.getAgentConfig('invalid' as AgentType)).toBeUndefined();
    });
  });

  describe('getCoderTaskTypeMappings', () => {
    it('should return all task type mappings for coder', () => {
      const mappings = mapping.getCoderTaskTypeMappings();

      expect(typeof mappings).toBe('object');
      for (const taskType of ALL_TASK_TYPES) {
        expect(mappings[taskType]).toBeDefined();
      }
    });

    it('should include task type overrides', () => {
      const mappings = mapping.getCoderTaskTypeMappings();

      expect(mappings.frontend).toBe('gemini-2.0-flash');
      expect(mappings.algorithm).toBe('codex-mini');
    });
  });

  describe('default mappings', () => {
    it('should use opus for decision agents', () => {
      const mainModel = mapping.getModelForAgent('main');
      const checkModel = mapping.getModelForAgent('check');

      expect(mainModel).toBe('claude-opus-4');
      expect(checkModel).toBe('claude-opus-4');
    });

    it('should use haiku for implementation agents', () => {
      const coderModel = mapping.getModelForAgent('coder');
      const dispatchModel = mapping.getModelForAgent('dispatch');

      expect(coderModel).toBe('claude-haiku-4');
      expect(dispatchModel).toBe('claude-haiku-4');
    });

    it('should use sonnet for research agents', () => {
      const researchModel = mapping.getModelForAgent('research');
      const subModel = mapping.getModelForAgent('sub');

      expect(researchModel).toBe('claude-sonnet-4');
      expect(subModel).toBe('claude-sonnet-4');
    });

    it('should have task type overrides for coder', () => {
      const config = mapping.getAgentConfig('coder');

      expect(config?.taskTypeOverrides).toBeDefined();
      expect(config?.taskTypeOverrides?.frontend).toBe('gemini-2.0-flash');
      expect(config?.taskTypeOverrides?.backend).toBe('claude-haiku-4');
      expect(config?.taskTypeOverrides?.algorithm).toBe('codex-mini');
    });
  });

  describe('ALL_AGENTS', () => {
    it('should contain all expected agents', () => {
      expect(ALL_AGENTS).toContain('main');
      expect(ALL_AGENTS).toContain('coder');
      expect(ALL_AGENTS).toContain('research');
      expect(ALL_AGENTS).toContain('check');
      expect(ALL_AGENTS).toContain('dispatch');
      expect(ALL_AGENTS).toContain('sub');
    });

    it('should have 6 agents', () => {
      expect(ALL_AGENTS.length).toBe(6);
    });
  });

  describe('ALL_TASK_TYPES', () => {
    it('should contain all expected task types', () => {
      expect(ALL_TASK_TYPES).toContain('default');
      expect(ALL_TASK_TYPES).toContain('frontend');
      expect(ALL_TASK_TYPES).toContain('backend');
      expect(ALL_TASK_TYPES).toContain('algorithm');
      expect(ALL_TASK_TYPES).toContain('debug');
      expect(ALL_TASK_TYPES).toContain('refactor');
    });

    it('should have 6 task types', () => {
      expect(ALL_TASK_TYPES.length).toBe(6);
    });
  });
});
