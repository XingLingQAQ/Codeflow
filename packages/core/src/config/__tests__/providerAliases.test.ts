import { describe, expect, it } from 'vitest';
import {
  toCanonicalProvider,
  toRuntimeProviderFamily,
  type CanonicalProvider,
  type RuntimeProviderFamily,
} from '../types.js';

describe('provider aliases', () => {
  it('normalizes runtime families to canonical providers', () => {
    const cases: Array<[RuntimeProviderFamily | CanonicalProvider, CanonicalProvider]> = [
      ['claude', 'anthropic'],
      ['gemini', 'google'],
      ['codex', 'openai'],
      ['openai', 'openai'],
      ['custom', 'custom'],
      ['anthropic', 'anthropic'],
      ['google', 'google'],
    ];

    for (const [input, expected] of cases) {
      expect(toCanonicalProvider(input)).toBe(expected);
    }
  });

  it('maps declaration providers to runtime families', () => {
    const cases: Array<[CanonicalProvider | RuntimeProviderFamily, RuntimeProviderFamily]> = [
      ['anthropic', 'claude'],
      ['google', 'gemini'],
      ['openai', 'openai'],
      ['custom', 'custom'],
      ['claude', 'claude'],
      ['codex', 'codex'],
    ];

    for (const [input, expected] of cases) {
      expect(toRuntimeProviderFamily(input)).toBe(expected);
    }
  });
});
