import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  ModelRegistry,
  ModelDefinition,
  ModelCapability,
  ModelProvider,
  ModelFilter,
} from '../ModelRegistry.js';

describe('ModelRegistry', () => {
  let registry: ModelRegistry;

  beforeEach(() => {
    registry = new ModelRegistry();
  });

  describe('constructor', () => {
    it('should load default models by default', () => {
      expect(registry.getModelCount()).toBeGreaterThan(0);
      expect(registry.hasModel('claude-opus-4')).toBe(true);
      expect(registry.hasModel('claude-sonnet-4')).toBe(true);
      expect(registry.hasModel('claude-haiku-4')).toBe(true);
    });

    it('should not load default models when loadDefaults is false', () => {
      const emptyRegistry = new ModelRegistry(false);
      expect(emptyRegistry.getModelCount()).toBe(0);
    });
  });

  describe('registerModel', () => {
    it('should register a new model', () => {
      const customModel: ModelDefinition = {
        id: 'custom-model',
        name: 'Custom Model',
        provider: 'custom',
        costPerMToken: { input: 1, output: 2 },
        capabilities: ['coding'],
      };

      registry.registerModel(customModel);

      expect(registry.hasModel('custom-model')).toBe(true);
      expect(registry.getModel('custom-model')).toEqual(customModel);
    });

    it('should emit model:registered event for new model', () => {
      const listener = vi.fn();
      registry.on('model:registered', listener);

      const customModel: ModelDefinition = {
        id: 'new-model',
        name: 'New Model',
        provider: 'custom',
        costPerMToken: { input: 1, output: 2 },
        capabilities: ['coding'],
      };

      registry.registerModel(customModel);

      expect(listener).toHaveBeenCalledWith(customModel);
    });

    it('should emit model:updated event for existing model', () => {
      const listener = vi.fn();
      registry.on('model:updated', listener);

      const updatedModel: ModelDefinition = {
        id: 'claude-opus-4',
        name: 'Claude Opus 4.5 Updated',
        provider: 'anthropic',
        costPerMToken: { input: 20, output: 80 },
        capabilities: ['reasoning', 'coding'],
      };

      registry.registerModel(updatedModel);

      expect(listener).toHaveBeenCalledWith(updatedModel);
    });

    it('should update existing model', () => {
      const updatedModel: ModelDefinition = {
        id: 'claude-opus-4',
        name: 'Claude Opus 4.5 Updated',
        provider: 'anthropic',
        costPerMToken: { input: 20, output: 80 },
        capabilities: ['reasoning'],
      };

      registry.registerModel(updatedModel);

      const model = registry.getModel('claude-opus-4');
      expect(model?.name).toBe('Claude Opus 4.5 Updated');
      expect(model?.costPerMToken.input).toBe(20);
    });
  });

  describe('registerModels', () => {
    it('should register multiple models', () => {
      const emptyRegistry = new ModelRegistry(false);
      const models: ModelDefinition[] = [
        {
          id: 'model-1',
          name: 'Model 1',
          provider: 'custom',
          costPerMToken: { input: 1, output: 2 },
          capabilities: ['coding'],
        },
        {
          id: 'model-2',
          name: 'Model 2',
          provider: 'custom',
          costPerMToken: { input: 2, output: 4 },
          capabilities: ['reasoning'],
        },
      ];

      emptyRegistry.registerModels(models);

      expect(emptyRegistry.getModelCount()).toBe(2);
      expect(emptyRegistry.hasModel('model-1')).toBe(true);
      expect(emptyRegistry.hasModel('model-2')).toBe(true);
    });
  });

  describe('removeModel', () => {
    it('should remove existing model', () => {
      expect(registry.hasModel('claude-opus-4')).toBe(true);

      const removed = registry.removeModel('claude-opus-4');

      expect(removed).toBe(true);
      expect(registry.hasModel('claude-opus-4')).toBe(false);
    });

    it('should return false for non-existent model', () => {
      const removed = registry.removeModel('non-existent');
      expect(removed).toBe(false);
    });

    it('should emit model:removed event', () => {
      const listener = vi.fn();
      registry.on('model:removed', listener);

      registry.removeModel('claude-opus-4');

      expect(listener).toHaveBeenCalledWith('claude-opus-4');
    });
  });

  describe('getModel', () => {
    it('should return model definition', () => {
      const model = registry.getModel('claude-opus-4');

      expect(model).toBeDefined();
      expect(model?.id).toBe('claude-opus-4');
      expect(model?.provider).toBe('anthropic');
    });

    it('should return undefined for non-existent model', () => {
      const model = registry.getModel('non-existent');
      expect(model).toBeUndefined();
    });

    it('should return a copy of the model', () => {
      const model1 = registry.getModel('claude-opus-4');
      const model2 = registry.getModel('claude-opus-4');

      expect(model1).not.toBe(model2);
      expect(model1).toEqual(model2);
    });
  });

  describe('getAvailableModels', () => {
    it('should return all models without filter', () => {
      const models = registry.getAvailableModels();
      expect(models.length).toBeGreaterThan(0);
    });

    it('should filter by provider', () => {
      const models = registry.getAvailableModels({ provider: 'anthropic' });

      expect(models.length).toBeGreaterThan(0);
      expect(models.every(m => m.provider === 'anthropic')).toBe(true);
    });

    it('should filter by capabilities', () => {
      const models = registry.getAvailableModels({ capabilities: ['reasoning'] });

      expect(models.length).toBeGreaterThan(0);
      expect(models.every(m => m.capabilities.includes('reasoning'))).toBe(true);
    });

    it('should filter by multiple capabilities', () => {
      const models = registry.getAvailableModels({
        capabilities: ['reasoning', 'coding'],
      });

      expect(models.length).toBeGreaterThan(0);
      expect(
        models.every(
          m => m.capabilities.includes('reasoning') && m.capabilities.includes('coding')
        )
      ).toBe(true);
    });

    it('should filter by max cost', () => {
      const models = registry.getAvailableModels({ maxCostPerMToken: 1 });

      expect(models.length).toBeGreaterThan(0);
      expect(models.every(m => m.costPerMToken.input <= 1)).toBe(true);
    });

    it('should filter by min context tokens', () => {
      const models = registry.getAvailableModels({ minContextTokens: 500000 });

      expect(models.length).toBeGreaterThan(0);
      expect(models.every(m => m.maxContextTokens && m.maxContextTokens >= 500000)).toBe(true);
    });

    it('should filter by vision support', () => {
      const models = registry.getAvailableModels({ supportsVision: true });

      expect(models.length).toBeGreaterThan(0);
      expect(models.every(m => m.supportsVision === true)).toBe(true);
    });

    it('should exclude deprecated models by default', () => {
      const deprecatedModel: ModelDefinition = {
        id: 'deprecated-model',
        name: 'Deprecated Model',
        provider: 'custom',
        costPerMToken: { input: 1, output: 2 },
        capabilities: ['coding'],
        deprecated: true,
      };
      registry.registerModel(deprecatedModel);

      const models = registry.getAvailableModels();

      expect(models.find(m => m.id === 'deprecated-model')).toBeUndefined();
    });

    it('should include deprecated models when specified', () => {
      const deprecatedModel: ModelDefinition = {
        id: 'deprecated-model',
        name: 'Deprecated Model',
        provider: 'custom',
        costPerMToken: { input: 1, output: 2 },
        capabilities: ['coding'],
        deprecated: true,
      };
      registry.registerModel(deprecatedModel);

      const models = registry.getAvailableModels({ includeDeprecated: true });

      expect(models.find(m => m.id === 'deprecated-model')).toBeDefined();
    });

    it('should combine multiple filters', () => {
      const models = registry.getAvailableModels({
        provider: 'anthropic',
        capabilities: ['coding'],
        maxCostPerMToken: 5,
      });

      expect(models.length).toBeGreaterThan(0);
      expect(
        models.every(
          m =>
            m.provider === 'anthropic' &&
            m.capabilities.includes('coding') &&
            m.costPerMToken.input <= 5
        )
      ).toBe(true);
    });
  });

  describe('getModelsByCapability', () => {
    it('should return models with specified capability', () => {
      const models = registry.getModelsByCapability('reasoning');

      expect(models.length).toBeGreaterThan(0);
      expect(models.every(m => m.capabilities.includes('reasoning'))).toBe(true);
    });

    it('should return empty array for non-existent capability', () => {
      const models = registry.getModelsByCapability('non-existent' as ModelCapability);
      expect(models).toEqual([]);
    });
  });

  describe('getModelsByProvider', () => {
    it('should return models from specified provider', () => {
      const models = registry.getModelsByProvider('anthropic');

      expect(models.length).toBeGreaterThan(0);
      expect(models.every(m => m.provider === 'anthropic')).toBe(true);
    });

    it('should return empty array for non-existent provider', () => {
      const models = registry.getModelsByProvider('non-existent' as ModelProvider);
      expect(models).toEqual([]);
    });
  });

  describe('getCheapestModel', () => {
    it('should return cheapest model overall', () => {
      const cheapest = registry.getCheapestModel();

      expect(cheapest).toBeDefined();
      const allModels = registry.getAvailableModels();
      const minCost = Math.min(
        ...allModels.map(m => m.costPerMToken.input + m.costPerMToken.output)
      );
      const cheapestCost = cheapest!.costPerMToken.input + cheapest!.costPerMToken.output;
      expect(cheapestCost).toBe(minCost);
    });

    it('should return cheapest model with specified capabilities', () => {
      const cheapest = registry.getCheapestModel(['reasoning']);

      expect(cheapest).toBeDefined();
      expect(cheapest?.capabilities.includes('reasoning')).toBe(true);
    });

    it('should return undefined when no models match', () => {
      const cheapest = registry.getCheapestModel(['non-existent' as ModelCapability]);
      expect(cheapest).toBeUndefined();
    });
  });

  describe('getMostCapableModel', () => {
    it('should return model with most capabilities', () => {
      const mostCapable = registry.getMostCapableModel();

      expect(mostCapable).toBeDefined();
      const allModels = registry.getAvailableModels();
      const maxCapabilities = Math.max(...allModels.map(m => m.capabilities.length));
      expect(mostCapable?.capabilities.length).toBe(maxCapabilities);
    });

    it('should return most capable model with specified capabilities', () => {
      const mostCapable = registry.getMostCapableModel(['coding']);

      expect(mostCapable).toBeDefined();
      expect(mostCapable?.capabilities.includes('coding')).toBe(true);
    });

    it('should return undefined when no models match', () => {
      const mostCapable = registry.getMostCapableModel(['non-existent' as ModelCapability]);
      expect(mostCapable).toBeUndefined();
    });
  });

  describe('hasModel', () => {
    it('should return true for existing model', () => {
      expect(registry.hasModel('claude-opus-4')).toBe(true);
    });

    it('should return false for non-existent model', () => {
      expect(registry.hasModel('non-existent')).toBe(false);
    });
  });

  describe('getModelCount', () => {
    it('should return correct count', () => {
      const count = registry.getModelCount();
      expect(count).toBeGreaterThan(0);
    });

    it('should update after adding model', () => {
      const initialCount = registry.getModelCount();

      registry.registerModel({
        id: 'new-model',
        name: 'New Model',
        provider: 'custom',
        costPerMToken: { input: 1, output: 2 },
        capabilities: ['coding'],
      });

      expect(registry.getModelCount()).toBe(initialCount + 1);
    });

    it('should update after removing model', () => {
      const initialCount = registry.getModelCount();

      registry.removeModel('claude-opus-4');

      expect(registry.getModelCount()).toBe(initialCount - 1);
    });
  });

  describe('getProviders', () => {
    it('should return all unique providers', () => {
      const providers = registry.getProviders();

      expect(providers).toContain('anthropic');
      expect(providers).toContain('google');
      expect(providers).toContain('openai');
    });

    it('should not contain duplicates', () => {
      const providers = registry.getProviders();
      const uniqueProviders = [...new Set(providers)];
      expect(providers.length).toBe(uniqueProviders.length);
    });
  });

  describe('getAllCapabilities', () => {
    it('should return all unique capabilities', () => {
      const capabilities = registry.getAllCapabilities();

      expect(capabilities).toContain('reasoning');
      expect(capabilities).toContain('coding');
      expect(capabilities).toContain('review');
    });

    it('should not contain duplicates', () => {
      const capabilities = registry.getAllCapabilities();
      const uniqueCapabilities = [...new Set(capabilities)];
      expect(capabilities.length).toBe(uniqueCapabilities.length);
    });
  });

  describe('estimateCost', () => {
    it('should calculate cost correctly', () => {
      const cost = registry.estimateCost('claude-opus-4', 1000000, 500000);

      expect(cost).toBeDefined();
      // Claude Opus 4: input $15/M, output $75/M
      // 1M input = $15, 0.5M output = $37.5
      expect(cost).toBe(15 + 37.5);
    });

    it('should return undefined for non-existent model', () => {
      const cost = registry.estimateCost('non-existent', 1000, 500);
      expect(cost).toBeUndefined();
    });

    it('should handle zero tokens', () => {
      const cost = registry.estimateCost('claude-opus-4', 0, 0);
      expect(cost).toBe(0);
    });

    it('should handle small token counts', () => {
      const cost = registry.estimateCost('claude-opus-4', 1000, 500);

      expect(cost).toBeDefined();
      // 1000 input tokens = $0.015, 500 output tokens = $0.0375
      expect(cost).toBeCloseTo(0.0525, 4);
    });
  });

  describe('exportConfig', () => {
    it('should export all models including deprecated', () => {
      const deprecatedModel: ModelDefinition = {
        id: 'deprecated-model',
        name: 'Deprecated Model',
        provider: 'custom',
        costPerMToken: { input: 1, output: 2 },
        capabilities: ['coding'],
        deprecated: true,
      };
      registry.registerModel(deprecatedModel);

      const exported = registry.exportConfig();

      expect(exported.find(m => m.id === 'deprecated-model')).toBeDefined();
    });

    it('should return copies of models', () => {
      const exported = registry.exportConfig();
      const model = exported.find(m => m.id === 'claude-opus-4');

      expect(model).toBeDefined();
      model!.name = 'Modified';

      const original = registry.getModel('claude-opus-4');
      expect(original?.name).not.toBe('Modified');
    });
  });

  describe('importConfig', () => {
    it('should import models', () => {
      const emptyRegistry = new ModelRegistry(false);
      const models: ModelDefinition[] = [
        {
          id: 'imported-1',
          name: 'Imported 1',
          provider: 'custom',
          costPerMToken: { input: 1, output: 2 },
          capabilities: ['coding'],
        },
        {
          id: 'imported-2',
          name: 'Imported 2',
          provider: 'custom',
          costPerMToken: { input: 2, output: 4 },
          capabilities: ['reasoning'],
        },
      ];

      emptyRegistry.importConfig(models);

      expect(emptyRegistry.hasModel('imported-1')).toBe(true);
      expect(emptyRegistry.hasModel('imported-2')).toBe(true);
    });

    it('should replace existing models when replace is true', () => {
      const models: ModelDefinition[] = [
        {
          id: 'new-model',
          name: 'New Model',
          provider: 'custom',
          costPerMToken: { input: 1, output: 2 },
          capabilities: ['coding'],
        },
      ];

      registry.importConfig(models, true);

      expect(registry.getModelCount()).toBe(1);
      expect(registry.hasModel('new-model')).toBe(true);
      expect(registry.hasModel('claude-opus-4')).toBe(false);
    });

    it('should merge with existing models when replace is false', () => {
      const initialCount = registry.getModelCount();
      const models: ModelDefinition[] = [
        {
          id: 'new-model',
          name: 'New Model',
          provider: 'custom',
          costPerMToken: { input: 1, output: 2 },
          capabilities: ['coding'],
        },
      ];

      registry.importConfig(models, false);

      expect(registry.getModelCount()).toBe(initialCount + 1);
      expect(registry.hasModel('new-model')).toBe(true);
      expect(registry.hasModel('claude-opus-4')).toBe(true);
    });
  });

  describe('reset', () => {
    it('should reset to default models', () => {
      registry.registerModel({
        id: 'custom-model',
        name: 'Custom Model',
        provider: 'custom',
        costPerMToken: { input: 1, output: 2 },
        capabilities: ['coding'],
      });
      registry.removeModel('claude-opus-4');

      registry.reset();

      expect(registry.hasModel('custom-model')).toBe(false);
      expect(registry.hasModel('claude-opus-4')).toBe(true);
    });
  });

  describe('default models', () => {
    it('should have Claude models', () => {
      expect(registry.hasModel('claude-opus-4')).toBe(true);
      expect(registry.hasModel('claude-sonnet-4')).toBe(true);
      expect(registry.hasModel('claude-haiku-4')).toBe(true);
    });

    it('should have Gemini models', () => {
      expect(registry.hasModel('gemini-2.0-flash')).toBe(true);
      expect(registry.hasModel('gemini-2.5-pro')).toBe(true);
    });

    it('should have OpenAI models', () => {
      expect(registry.hasModel('gpt-4o')).toBe(true);
      expect(registry.hasModel('gpt-4o-mini')).toBe(true);
    });

    it('should have correct cost structure', () => {
      const opus = registry.getModel('claude-opus-4');
      expect(opus?.costPerMToken.input).toBe(15);
      expect(opus?.costPerMToken.output).toBe(75);
      expect(opus?.costPerMToken.cached).toBe(1.875);
    });

    it('should have correct capabilities', () => {
      const opus = registry.getModel('claude-opus-4');
      expect(opus?.capabilities).toContain('reasoning');
      expect(opus?.capabilities).toContain('coding');
      expect(opus?.capabilities).toContain('review');

      const haiku = registry.getModel('claude-haiku-4');
      expect(haiku?.capabilities).toContain('coding');
      expect(haiku?.capabilities).toContain('simple-tasks');
    });
  });
});
