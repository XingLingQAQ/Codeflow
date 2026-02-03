import { describe, it, expect, vi } from 'vitest';
import React from 'react';
import {
  ModelOption,
  PROVIDER_ICONS,
  PROVIDER_NAMES,
  CAPABILITY_NAMES,
  CAPABILITY_COLORS,
  formatCost,
  formatContextWindow,
} from '../types';

// Mock model data
const mockModels: ModelOption[] = [
  {
    id: 'claude-opus-4',
    name: 'Claude Opus 4.5',
    provider: 'anthropic',
    cost: { input: 15, output: 75 },
    capabilities: ['reasoning', 'coding', 'review'],
    contextWindow: 200000,
    description: 'Most capable Claude model',
  },
  {
    id: 'claude-haiku-4',
    name: 'Claude Haiku 4',
    provider: 'anthropic',
    cost: { input: 0.25, output: 1.25 },
    capabilities: ['coding', 'simple-tasks'],
    contextWindow: 200000,
    description: 'Fast and cost-effective',
  },
  {
    id: 'gpt-4o',
    name: 'GPT-4o',
    provider: 'openai',
    cost: { input: 2.5, output: 10 },
    capabilities: ['reasoning', 'coding', 'vision'],
    contextWindow: 128000,
    description: 'OpenAI flagship model',
  },
  {
    id: 'gemini-2.5-pro',
    name: 'Gemini 2.5 Pro',
    provider: 'google',
    cost: { input: 1.25, output: 10 },
    capabilities: ['reasoning', 'coding', 'long-context'],
    contextWindow: 1000000,
    description: 'Long context model',
  },
];

describe('ModelSelector Types', () => {
  describe('PROVIDER_ICONS', () => {
    it('should have icons for all providers', () => {
      expect(PROVIDER_ICONS.anthropic).toBeDefined();
      expect(PROVIDER_ICONS.openai).toBeDefined();
      expect(PROVIDER_ICONS.google).toBeDefined();
      expect(PROVIDER_ICONS.local).toBeDefined();
      expect(PROVIDER_ICONS.custom).toBeDefined();
    });
  });

  describe('PROVIDER_NAMES', () => {
    it('should have names for all providers', () => {
      expect(PROVIDER_NAMES.anthropic).toBe('Anthropic');
      expect(PROVIDER_NAMES.openai).toBe('OpenAI');
      expect(PROVIDER_NAMES.google).toBe('Google');
    });
  });

  describe('CAPABILITY_NAMES', () => {
    it('should have names for all capabilities', () => {
      expect(CAPABILITY_NAMES.reasoning).toBe('Reasoning');
      expect(CAPABILITY_NAMES.coding).toBe('Coding');
      expect(CAPABILITY_NAMES.review).toBe('Review');
      expect(CAPABILITY_NAMES['simple-tasks']).toBe('Simple Tasks');
    });
  });

  describe('CAPABILITY_COLORS', () => {
    it('should have colors for all capabilities', () => {
      expect(CAPABILITY_COLORS.reasoning).toBeDefined();
      expect(CAPABILITY_COLORS.coding).toBeDefined();
      expect(CAPABILITY_COLORS.review).toBeDefined();
    });
  });

  describe('formatCost', () => {
    it('should format high costs correctly', () => {
      const result = formatCost({ input: 15, output: 75 });
      expect(result).toBe('$90.00/M');
    });

    it('should format low costs in cents', () => {
      const result = formatCost({ input: 0.25, output: 0.5 });
      expect(result).toBe('$75.0¢/M');
    });

    it('should format medium costs correctly', () => {
      const result = formatCost({ input: 2.5, output: 10 });
      expect(result).toBe('$12.50/M');
    });
  });

  describe('formatContextWindow', () => {
    it('should format millions correctly', () => {
      expect(formatContextWindow(1000000)).toBe('1.0M');
      expect(formatContextWindow(2000000)).toBe('2.0M');
    });

    it('should format thousands correctly', () => {
      expect(formatContextWindow(200000)).toBe('200K');
      expect(formatContextWindow(128000)).toBe('128K');
    });

    it('should handle undefined', () => {
      expect(formatContextWindow(undefined)).toBe('N/A');
    });
  });
});

describe('ModelOption interface', () => {
  it('should have required fields', () => {
    const model: ModelOption = mockModels[0];
    expect(model.id).toBeDefined();
    expect(model.name).toBeDefined();
    expect(model.provider).toBeDefined();
    expect(model.cost).toBeDefined();
    expect(model.capabilities).toBeDefined();
  });

  it('should have optional fields', () => {
    const model: ModelOption = mockModels[0];
    expect(model.contextWindow).toBeDefined();
    expect(model.description).toBeDefined();
  });
});

describe('Mock Models', () => {
  it('should have multiple providers', () => {
    const providers = new Set(mockModels.map(m => m.provider));
    expect(providers.size).toBeGreaterThan(1);
  });

  it('should have various capabilities', () => {
    const allCaps = new Set<string>();
    mockModels.forEach(m => m.capabilities.forEach(c => allCaps.add(c)));
    expect(allCaps.size).toBeGreaterThan(3);
  });

  it('should have different cost ranges', () => {
    const costs = mockModels.map(m => m.cost.input + m.cost.output);
    const min = Math.min(...costs);
    const max = Math.max(...costs);
    expect(max).toBeGreaterThan(min * 10); // At least 10x difference
  });
});
