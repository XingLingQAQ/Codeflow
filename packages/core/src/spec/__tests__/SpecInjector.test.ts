import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';
import {
  SpecInjector,
  ContextAnalyzer,
  RelevantSpecSelector,
  InjectionHook,
} from '../SpecInjector.js';
import { SpecLibrary } from '../SpecLibrary.js';

describe('ContextAnalyzer', () => {
  let analyzer: ContextAnalyzer;

  beforeEach(() => {
    analyzer = new ContextAnalyzer();
  });

  describe('analyze', () => {
    it('should detect frontend task type', () => {
      const result = analyzer.analyze('Create a React component for the dashboard');

      expect(result.taskType).toBe('frontend');
      expect(result.keywords).toContain('react');
      expect(result.keywords).toContain('component');
      expect(result.suggestedDomains).toContain('frontend');
    });

    it('should detect backend task type', () => {
      const result = analyzer.analyze('Build a REST API endpoint for user authentication');

      expect(result.taskType).toBe('backend');
      expect(result.keywords).toContain('api');
      expect(result.keywords).toContain('endpoint');
      expect(result.suggestedDomains).toContain('backend');
    });

    it('should detect testing task type', () => {
      const result = analyzer.analyze('Write unit tests for the user service');

      expect(result.taskType).toBe('testing');
      expect(result.keywords).toContain('test');
      expect(result.keywords).toContain('unit');
    });

    it('should detect database task type', () => {
      const result = analyzer.analyze('Create a database migration for the users table');

      expect(result.taskType).toBe('database');
      expect(result.keywords).toContain('database');
      expect(result.keywords).toContain('migration');
    });

    it('should return unknown for ambiguous input', () => {
      const result = analyzer.analyze('Do something');

      expect(result.taskType).toBe('unknown');
      expect(result.confidence).toBe(0);
    });

    it('should emit analysis:complete event', () => {
      const listener = vi.fn();
      analyzer.on('analysis:complete', listener);

      analyzer.analyze('Create a React component');

      expect(listener).toHaveBeenCalled();
    });

    it('should extract suggested tags', () => {
      const result = analyzer.analyze('Build a TypeScript React component');

      expect(result.suggestedTags).toContain('typescript');
      expect(result.suggestedTags).toContain('react');
    });
  });

  describe('analyzeFromPath', () => {
    it('should detect frontend from component path', () => {
      const result = analyzer.analyzeFromPath('src/components/Button.tsx');

      expect(result.taskType).toBe('frontend');
      expect(result.suggestedTags).toContain('react');
      expect(result.suggestedTags).toContain('typescript');
    });

    it('should detect backend from api path', () => {
      const result = analyzer.analyzeFromPath('src/api/users/controller.ts');

      expect(result.taskType).toBe('backend');
    });

    it('should detect testing from test path', () => {
      const result = analyzer.analyzeFromPath('src/__tests__/user.test.ts');

      expect(result.taskType).toBe('testing');
      expect(result.suggestedTags).toContain('testing');
    });

    it('should detect database from model path', () => {
      const result = analyzer.analyzeFromPath('src/models/User.ts');

      expect(result.taskType).toBe('database');
    });
  });
});

describe('RelevantSpecSelector', () => {
  let library: SpecLibrary;
  let selector: RelevantSpecSelector;
  let testDir: string;

  beforeEach(async () => {
    testDir = path.join(os.tmpdir(), `selector-test-${Date.now()}`);
    fs.mkdirSync(testDir, { recursive: true });
    library = new SpecLibrary({ rootDir: testDir, autoReload: false });
    await library.initialize();

    // Add test specs
    await library.addSpec('frontend', 'react-rules', '# React Rules\n\nReact best practices.', {
      tags: ['react', 'frontend'],
      priority: 'high',
    });
    await library.addSpec('backend', 'api-rules', '# API Rules\n\nAPI best practices.', {
      tags: ['api', 'backend'],
      priority: 'high',
    });
    await library.addSpec('common', 'coding-style', '# Coding Style\n\nGeneral coding style.', {
      tags: ['style'],
      priority: 'medium',
    });

    selector = new RelevantSpecSelector(library);
  });

  afterEach(() => {
    library.close();
    try {
      fs.rmSync(testDir, { recursive: true, force: true });
    } catch {
      // Ignore cleanup errors
    }
  });

  describe('select', () => {
    it('should select specs by domain', () => {
      const analysis = {
        taskType: 'frontend' as const,
        confidence: 0.8,
        keywords: ['react'],
        suggestedDomains: ['frontend' as const],
        suggestedTags: ['react'],
      };

      const specs = selector.select(analysis);

      expect(specs.some(s => s.metadata.name === 'react-rules')).toBe(true);
    });

    it('should include default domain specs', () => {
      const analysis = {
        taskType: 'frontend' as const,
        confidence: 0.8,
        keywords: ['react'],
        suggestedDomains: ['frontend' as const],
        suggestedTags: [],
      };

      const specs = selector.select(analysis);

      expect(specs.some(s => s.metadata.domain === 'common')).toBe(true);
    });

    it('should sort by priority', () => {
      const analysis = {
        taskType: 'frontend' as const,
        confidence: 0.8,
        keywords: [],
        suggestedDomains: ['frontend' as const, 'common' as const],
        suggestedTags: [],
      };

      const specs = selector.select(analysis);

      // High priority should come before medium
      const highIndex = specs.findIndex(s => s.metadata.priority === 'high');
      const mediumIndex = specs.findIndex(s => s.metadata.priority === 'medium');

      if (highIndex !== -1 && mediumIndex !== -1) {
        expect(highIndex).toBeLessThan(mediumIndex);
      }
    });

    it('should emit selection:complete event', () => {
      const listener = vi.fn();
      selector.on('selection:complete', listener);

      selector.select({
        taskType: 'frontend',
        confidence: 0.8,
        keywords: [],
        suggestedDomains: ['frontend'],
        suggestedTags: [],
      });

      expect(listener).toHaveBeenCalled();
    });
  });

  describe('applyTokenLimit', () => {
    it('should limit specs by token count', () => {
      const specs = library.getAllSpecs();
      const limitedSelector = new RelevantSpecSelector(library, { maxTokens: 100 });

      const result = limitedSelector.applyTokenLimit(specs);

      expect(result.totalTokens).toBeLessThanOrEqual(100);
    });

    it('should indicate when truncated', async () => {
      // Add many specs
      for (let i = 0; i < 10; i++) {
        await library.addSpec('common', `spec-${i}`, `# Spec ${i}\n\n${'Content '.repeat(100)}`);
      }

      const specs = library.getAllSpecs();
      const limitedSelector = new RelevantSpecSelector(library, { maxTokens: 500 });

      const result = limitedSelector.applyTokenLimit(specs);

      expect(result.truncated).toBe(true);
    });
  });
});

describe('SpecInjector', () => {
  let library: SpecLibrary;
  let injector: SpecInjector;
  let testDir: string;

  beforeEach(async () => {
    testDir = path.join(os.tmpdir(), `injector-test-${Date.now()}`);
    fs.mkdirSync(testDir, { recursive: true });
    library = new SpecLibrary({ rootDir: testDir, autoReload: false });
    await library.initialize();

    // Add test specs
    await library.addSpec('frontend', 'react-rules', '# React Rules\n\nReact best practices.', {
      tags: ['react'],
      priority: 'high',
    });
    await library.addSpec('backend', 'api-rules', '# API Rules\n\nAPI best practices.', {
      tags: ['api'],
      priority: 'high',
    });
    await library.addSpec('common', 'coding-style', '# Coding Style\n\nGeneral coding style.', {
      priority: 'medium',
    });

    injector = new SpecInjector(library);
  });

  afterEach(() => {
    library.close();
    try {
      fs.rmSync(testDir, { recursive: true, force: true });
    } catch {
      // Ignore cleanup errors
    }
  });

  describe('inject', () => {
    it('should inject relevant specs', async () => {
      const result = await injector.inject('Create a React component');

      expect(result.specs.length).toBeGreaterThan(0);
      expect(result.analysis.taskType).toBe('frontend');
    });

    it('should emit injection events', async () => {
      const startListener = vi.fn();
      const completeListener = vi.fn();
      injector.on('injection:start', startListener);
      injector.on('injection:complete', completeListener);

      await injector.inject('Build an API endpoint');

      expect(startListener).toHaveBeenCalled();
      expect(completeListener).toHaveBeenCalled();
    });

    it('should track injection history', async () => {
      await injector.inject('Create a React component');
      await injector.inject('Build an API endpoint');

      const history = injector.getInjectionHistory();

      expect(history.length).toBe(2);
    });
  });

  describe('injectFromPath', () => {
    it('should inject based on file path', async () => {
      const result = await injector.injectFromPath('src/components/Button.tsx');

      expect(result.analysis.taskType).toBe('frontend');
    });
  });

  describe('injectManual', () => {
    it('should inject specific specs by ID', async () => {
      const allSpecs = library.getAllSpecs();
      const specId = allSpecs[0].metadata.id;

      const result = await injector.injectManual([specId]);

      expect(result.specs.length).toBe(1);
      expect(result.specs[0].metadata.id).toBe(specId);
    });

    it('should handle non-existent spec IDs', async () => {
      const result = await injector.injectManual(['non-existent']);

      expect(result.specs.length).toBe(0);
    });
  });

  describe('hooks', () => {
    it('should register a hook', () => {
      const hook: InjectionHook = {
        id: 'test-hook',
        name: 'Test Hook',
        trigger: 'task',
        filter: (spec) => spec.metadata.priority === 'high',
        priority: 1,
        enabled: true,
      };

      injector.registerHook(hook);

      expect(injector.getHooks().length).toBe(1);
    });

    it('should unregister a hook', () => {
      const hook: InjectionHook = {
        id: 'test-hook',
        name: 'Test Hook',
        trigger: 'task',
        priority: 1,
        enabled: true,
      };

      injector.registerHook(hook);
      const removed = injector.unregisterHook('test-hook');

      expect(removed).toBe(true);
      expect(injector.getHooks().length).toBe(0);
    });

    it('should apply hook filters', async () => {
      const hook: InjectionHook = {
        id: 'high-priority-only',
        name: 'High Priority Only',
        trigger: 'task',
        filter: (spec) => spec.metadata.priority === 'high',
        priority: 1,
        enabled: true,
      };

      injector.registerHook(hook);
      const result = await injector.inject('Create a React component');

      // All specs should be high priority
      for (const spec of result.specs) {
        expect(spec.metadata.priority).toBe('high');
      }
    });

    it('should enable/disable hooks', () => {
      const hook: InjectionHook = {
        id: 'test-hook',
        name: 'Test Hook',
        trigger: 'task',
        priority: 1,
        enabled: true,
      };

      injector.registerHook(hook);
      injector.setHookEnabled('test-hook', false);

      const hooks = injector.getHooks();
      expect(hooks[0].enabled).toBe(false);
    });
  });

  describe('formatInjection', () => {
    it('should format injection result as markdown', async () => {
      const result = await injector.inject('Create a React component');
      const formatted = injector.formatInjection(result);

      expect(formatted).toContain('<!-- Injected Specifications -->');
      expect(formatted).toContain('##');
    });

    it('should include truncation note when truncated', async () => {
      // Add many specs to trigger truncation
      for (let i = 0; i < 20; i++) {
        await library.addSpec('common', `spec-${i}`, `# Spec ${i}\n\n${'Content '.repeat(200)}`);
      }

      const limitedInjector = new SpecInjector(library, { maxTokens: 500 });
      const result = await limitedInjector.inject('Do something');

      if (result.truncated) {
        const formatted = limitedInjector.formatInjection(result);
        expect(formatted).toContain('truncated');
      }
    });
  });

  describe('history management', () => {
    it('should clear history', async () => {
      await injector.inject('Task 1');
      await injector.inject('Task 2');

      injector.clearHistory();

      expect(injector.getInjectionHistory().length).toBe(0);
    });

    it('should emit history:cleared event', async () => {
      const listener = vi.fn();
      injector.on('history:cleared', listener);

      injector.clearHistory();

      expect(listener).toHaveBeenCalled();
    });
  });

  describe('configuration', () => {
    it('should update config', () => {
      injector.updateConfig({ maxTokens: 5000 });
      const config = injector.getConfig();

      expect(config.maxTokens).toBe(5000);
    });

    it('should disable history tracking', async () => {
      injector.updateConfig({ traceInjections: false });
      await injector.inject('Task');

      expect(injector.getInjectionHistory().length).toBe(0);
    });
  });
});
