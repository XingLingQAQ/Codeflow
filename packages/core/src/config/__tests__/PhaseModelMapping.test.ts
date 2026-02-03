import { describe, it, expect, beforeEach, vi } from 'vitest';
import { ModelRegistry } from '../ModelRegistry.js';
import {
  PhaseModelMapping,
  PlanPhase,
  PhaseModelConfig,
  ALL_PHASES,
} from '../PhaseModelMapping.js';

describe('PhaseModelMapping', () => {
  let registry: ModelRegistry;
  let mapping: PhaseModelMapping;

  beforeEach(() => {
    registry = new ModelRegistry();
    mapping = new PhaseModelMapping(registry);
  });

  describe('constructor', () => {
    it('should load default mappings', () => {
      const allMappings = mapping.getAllMappings();
      expect(allMappings.length).toBe(ALL_PHASES.length);
    });

    it('should have mappings for all phases', () => {
      for (const phase of ALL_PHASES) {
        const config = mapping.getPhaseConfig(phase);
        expect(config).toBeDefined();
        expect(config?.phase).toBe(phase);
      }
    });
  });

  describe('getPhaseConfig', () => {
    it('should return config for valid phase', () => {
      const config = mapping.getPhaseConfig('vision');

      expect(config).toBeDefined();
      expect(config?.phase).toBe('vision');
      expect(config?.modelId).toBeDefined();
    });

    it('should return undefined for invalid phase', () => {
      const config = mapping.getPhaseConfig('invalid' as PlanPhase);
      expect(config).toBeUndefined();
    });

    it('should return a copy of the config', () => {
      const config1 = mapping.getPhaseConfig('vision');
      const config2 = mapping.getPhaseConfig('vision');

      expect(config1).not.toBe(config2);
      expect(config1).toEqual(config2);
    });
  });

  describe('getModelForPhase', () => {
    it('should return model ID for valid phase', () => {
      const modelId = mapping.getModelForPhase('vision');
      expect(modelId).toBeDefined();
      expect(registry.hasModel(modelId!)).toBe(true);
    });

    it('should return fallback model if primary not available', () => {
      // Remove primary model
      registry.removeModel('claude-opus-4');

      const modelId = mapping.getModelForPhase('vision');
      expect(modelId).toBe('claude-sonnet-4');
    });

    it('should return undefined if no models available', () => {
      // Remove both primary and fallback
      registry.removeModel('claude-opus-4');
      registry.removeModel('claude-sonnet-4');

      const modelId = mapping.getModelForPhase('vision');
      expect(modelId).toBeUndefined();
    });
  });

  describe('getModelDefinitionForPhase', () => {
    it('should return model definition for valid phase', () => {
      const model = mapping.getModelDefinitionForPhase('vision');

      expect(model).toBeDefined();
      expect(model?.id).toBeDefined();
      expect(model?.name).toBeDefined();
    });

    it('should return undefined for invalid phase', () => {
      const model = mapping.getModelDefinitionForPhase('invalid' as PlanPhase);
      expect(model).toBeUndefined();
    });
  });

  describe('setModelForPhase', () => {
    it('should set model for phase', () => {
      const result = mapping.setModelForPhase('implement', 'claude-sonnet-4');

      expect(result).toBe(true);
      expect(mapping.getModelForPhase('implement')).toBe('claude-sonnet-4');
    });

    it('should return false for non-existent model', () => {
      const result = mapping.setModelForPhase('implement', 'non-existent');
      expect(result).toBe(false);
    });

    it('should return false if model lacks required capabilities', () => {
      // claude-haiku-4 doesn't have 'reasoning' capability required for vision
      const result = mapping.setModelForPhase('vision', 'claude-haiku-4');
      expect(result).toBe(false);
    });

    it('should emit mapping:changed event', () => {
      const listener = vi.fn();
      mapping.on('mapping:changed', listener);

      mapping.setModelForPhase('implement', 'claude-sonnet-4');

      expect(listener).toHaveBeenCalledWith('implement', 'claude-sonnet-4');
    });
  });

  describe('setFallbackModelForPhase', () => {
    it('should set fallback model', () => {
      const result = mapping.setFallbackModelForPhase('vision', 'gemini-2.5-pro');

      expect(result).toBe(true);
      const config = mapping.getPhaseConfig('vision');
      expect(config?.fallbackModelId).toBe('gemini-2.5-pro');
    });

    it('should return false for non-existent model', () => {
      const result = mapping.setFallbackModelForPhase('vision', 'non-existent');
      expect(result).toBe(false);
    });
  });

  describe('getAllMappings', () => {
    it('should return all mappings', () => {
      const allMappings = mapping.getAllMappings();

      expect(allMappings.length).toBe(ALL_PHASES.length);
      for (const config of allMappings) {
        expect(ALL_PHASES).toContain(config.phase);
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
      for (const phase of ALL_PHASES) {
        expect(record[phase]).toBeDefined();
      }
    });
  });

  describe('setMappings', () => {
    it('should set multiple mappings', () => {
      mapping.setMappings({
        implement: 'claude-sonnet-4',
        explore: 'claude-sonnet-4',
      });

      expect(mapping.getModelForPhase('implement')).toBe('claude-sonnet-4');
      expect(mapping.getModelForPhase('explore')).toBe('claude-sonnet-4');
    });

    it('should skip invalid mappings', () => {
      const originalModel = mapping.getModelForPhase('vision');

      mapping.setMappings({
        vision: 'non-existent',
      });

      expect(mapping.getModelForPhase('vision')).toBe(originalModel);
    });
  });

  describe('reset', () => {
    it('should reset to default mappings', () => {
      mapping.setModelForPhase('implement', 'claude-sonnet-4');
      mapping.reset();

      const config = mapping.getPhaseConfig('implement');
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

  describe('getSuitableModelsForPhase', () => {
    it('should return models with required capabilities', () => {
      const models = mapping.getSuitableModelsForPhase('vision');

      expect(models.length).toBeGreaterThan(0);
      for (const model of models) {
        expect(model.capabilities).toContain('reasoning');
      }
    });

    it('should return all models if no capabilities required', () => {
      // Create a phase without required capabilities
      const emptyRegistry = new ModelRegistry(false);
      emptyRegistry.registerModel({
        id: 'test-model',
        name: 'Test Model',
        provider: 'custom',
        costPerMToken: { input: 1, output: 2 },
        capabilities: [],
      });

      const emptyMapping = new PhaseModelMapping(emptyRegistry);
      // Manually set a phase without required capabilities
      emptyMapping['mappings'].set('explore', {
        phase: 'explore',
        modelId: 'test-model',
      });

      const models = emptyMapping.getSuitableModelsForPhase('explore');
      expect(models.length).toBe(1);
    });
  });

  describe('estimateTotalCost', () => {
    it('should estimate total cost for all phases', () => {
      const cost = mapping.estimateTotalCost(10000);

      expect(cost).toBeGreaterThan(0);
    });

    it('should use default tokens per phase', () => {
      const cost = mapping.estimateTotalCost();

      expect(cost).toBeGreaterThan(0);
    });
  });

  describe('getCostOptimizedMappings', () => {
    it('should return cost-optimized mappings', () => {
      const optimized = mapping.getCostOptimizedMappings();

      expect(typeof optimized).toBe('object');
      for (const phase of ALL_PHASES) {
        expect(optimized[phase]).toBeDefined();
      }
    });

    it('should prefer cheaper models', () => {
      const optimized = mapping.getCostOptimizedMappings();

      // For implement phase, should prefer haiku or flash (cheapest)
      const implementModel = registry.getModel(optimized.implement);
      expect(implementModel).toBeDefined();
      expect(implementModel!.costPerMToken.input).toBeLessThan(5);
    });
  });

  describe('getQualityOptimizedMappings', () => {
    it('should return quality-optimized mappings', () => {
      const optimized = mapping.getQualityOptimizedMappings();

      expect(typeof optimized).toBe('object');
      for (const phase of ALL_PHASES) {
        expect(optimized[phase]).toBeDefined();
      }
    });

    it('should prefer models with more capabilities', () => {
      const optimized = mapping.getQualityOptimizedMappings();

      // For vision phase, should prefer opus (most capabilities)
      const visionModel = registry.getModel(optimized.vision);
      expect(visionModel).toBeDefined();
      expect(visionModel!.capabilities.length).toBeGreaterThan(3);
    });
  });

  describe('exportConfig', () => {
    it('should export all configurations', () => {
      const exported = mapping.exportConfig();

      expect(exported.length).toBe(ALL_PHASES.length);
      for (const config of exported) {
        expect(config.phase).toBeDefined();
        expect(config.modelId).toBeDefined();
      }
    });
  });

  describe('importConfig', () => {
    it('should import configurations', () => {
      const configs: PhaseModelConfig[] = [
        { phase: 'implement', modelId: 'claude-sonnet-4' },
        { phase: 'explore', modelId: 'claude-sonnet-4' },
      ];

      mapping.importConfig(configs);

      expect(mapping.getPhaseConfig('implement')?.modelId).toBe('claude-sonnet-4');
      expect(mapping.getPhaseConfig('explore')?.modelId).toBe('claude-sonnet-4');
    });

    it('should skip invalid phases', () => {
      const configs: PhaseModelConfig[] = [
        { phase: 'invalid' as PlanPhase, modelId: 'claude-sonnet-4' },
      ];

      mapping.importConfig(configs);

      // Should not throw and should not add invalid phase
      expect(mapping.getPhaseConfig('invalid' as PlanPhase)).toBeUndefined();
    });
  });

  describe('default mappings', () => {
    it('should use opus for decision phases', () => {
      const visionModel = mapping.getModelForPhase('vision');
      const architectureModel = mapping.getModelForPhase('architecture');
      const reviewModel = mapping.getModelForPhase('review');
      const qaModel = mapping.getModelForPhase('qa');

      expect(visionModel).toBe('claude-opus-4');
      expect(architectureModel).toBe('claude-opus-4');
      expect(reviewModel).toBe('claude-opus-4');
      expect(qaModel).toBe('claude-opus-4');
    });

    it('should use haiku for implementation phases', () => {
      const implementModel = mapping.getModelForPhase('implement');
      const exploreModel = mapping.getModelForPhase('explore');

      expect(implementModel).toBe('claude-haiku-4');
      expect(exploreModel).toBe('claude-haiku-4');
    });

    it('should use sonnet for research phases', () => {
      const researchModel = mapping.getModelForPhase('research');
      const constraintsModel = mapping.getModelForPhase('constraints');

      expect(researchModel).toBe('claude-sonnet-4');
      expect(constraintsModel).toBe('claude-sonnet-4');
    });
  });

  describe('ALL_PHASES', () => {
    it('should contain all expected phases', () => {
      expect(ALL_PHASES).toContain('vision');
      expect(ALL_PHASES).toContain('constraints');
      expect(ALL_PHASES).toContain('architecture');
      expect(ALL_PHASES).toContain('research');
      expect(ALL_PHASES).toContain('explore');
      expect(ALL_PHASES).toContain('review');
      expect(ALL_PHASES).toContain('implement');
      expect(ALL_PHASES).toContain('qa');
    });

    it('should have 8 phases', () => {
      expect(ALL_PHASES.length).toBe(8);
    });
  });
});
