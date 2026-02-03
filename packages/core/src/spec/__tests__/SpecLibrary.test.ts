import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';
import {
  SpecLibrary,
  SpecLoader,
  SpecValidator,
  SpecDocument,
  SpecMetadata,
} from '../SpecLibrary.js';

describe('SpecLoader', () => {
  let loader: SpecLoader;
  let testDir: string;

  beforeEach(() => {
    testDir = path.join(os.tmpdir(), `spec-loader-test-${Date.now()}`);
    fs.mkdirSync(testDir, { recursive: true });
    loader = new SpecLoader({ rootDir: testDir });
  });

  afterEach(() => {
    try {
      fs.rmSync(testDir, { recursive: true, force: true });
    } catch {
      // Ignore cleanup errors
    }
  });

  describe('load', () => {
    it('should load a spec file', async () => {
      const specContent = `---
id: test-spec
name: Test Spec
domain: frontend
type: rule
priority: high
tags: react, components
version: 1.0.0
---

# Test Spec

This is a test specification.
`;
      const specPath = path.join(testDir, 'test.md');
      fs.writeFileSync(specPath, specContent);

      const doc = await loader.load(specPath);

      expect(doc).toBeDefined();
      expect(doc?.metadata.id).toBe('test-spec');
      expect(doc?.metadata.name).toBe('Test Spec');
      expect(doc?.metadata.domain).toBe('frontend');
      expect(doc?.metadata.priority).toBe('high');
      expect(doc?.metadata.tags).toContain('react');
    });

    it('should emit load:success event', async () => {
      const specPath = path.join(testDir, 'test.md');
      fs.writeFileSync(specPath, '# Test\n\nContent');

      const listener = vi.fn();
      loader.on('load:success', listener);

      await loader.load(specPath);

      expect(listener).toHaveBeenCalled();
    });

    it('should return null for non-existent file', async () => {
      const errorListener = vi.fn();
      loader.on('load:error', errorListener);

      const doc = await loader.load('non-existent.md');

      expect(doc).toBeNull();
      expect(errorListener).toHaveBeenCalled();
    });

    it('should cache loaded specs', async () => {
      const specPath = path.join(testDir, 'cached.md');
      fs.writeFileSync(specPath, '# Cached\n\nContent');

      await loader.load(specPath);
      await loader.load(specPath);

      expect(loader.getCachedSpecs().length).toBe(1);
    });

    it('should parse frontmatter correctly', async () => {
      const specContent = `---
id: fm-test
name: Frontmatter Test
domain: backend
type: pattern
priority: critical
tags: api, rest
version: 2.0.0
author: Test Author
description: A test description
---

# Content
`;
      const specPath = path.join(testDir, 'frontmatter.md');
      fs.writeFileSync(specPath, specContent);

      const doc = await loader.load(specPath);

      expect(doc?.metadata.author).toBe('Test Author');
      expect(doc?.metadata.description).toBe('A test description');
      expect(doc?.metadata.version).toBe('2.0.0');
    });
  });

  describe('loadDirectory', () => {
    it('should load all specs in a directory', async () => {
      const subDir = path.join(testDir, 'frontend');
      fs.mkdirSync(subDir);
      fs.writeFileSync(path.join(subDir, 'spec1.md'), '# Spec 1');
      fs.writeFileSync(path.join(subDir, 'spec2.md'), '# Spec 2');

      const docs = await loader.loadDirectory(subDir);

      expect(docs.length).toBe(2);
    });

    it('should recursively load nested directories', async () => {
      const subDir = path.join(testDir, 'frontend', 'components');
      fs.mkdirSync(subDir, { recursive: true });
      fs.writeFileSync(path.join(subDir, 'button.md'), '# Button');

      const docs = await loader.loadDirectory(path.join(testDir, 'frontend'));

      expect(docs.length).toBe(1);
    });
  });

  describe('loadDomain', () => {
    it('should load specs for a domain', async () => {
      const frontendDir = path.join(testDir, 'frontend');
      fs.mkdirSync(frontendDir);
      fs.writeFileSync(path.join(frontendDir, 'react.md'), '# React');

      const docs = await loader.loadDomain('frontend');

      expect(docs.length).toBe(1);
    });
  });

  describe('cache management', () => {
    it('should clear cache', async () => {
      const specPath = path.join(testDir, 'cache.md');
      fs.writeFileSync(specPath, '# Cache');

      await loader.load(specPath);
      expect(loader.getCachedSpecs().length).toBe(1);

      loader.clearCache();
      expect(loader.getCachedSpecs().length).toBe(0);
    });
  });
});

describe('SpecValidator', () => {
  let validator: SpecValidator;

  beforeEach(() => {
    validator = new SpecValidator();
  });

  describe('validate', () => {
    it('should validate a valid spec', () => {
      const doc: SpecDocument = {
        metadata: {
          id: 'valid-spec',
          name: 'Valid Spec',
          domain: 'frontend',
          type: 'rule',
          priority: 'high',
          tags: ['react'],
          version: '1.0.0',
          createdAt: Date.now(),
          updatedAt: Date.now(),
        },
        content: '# Valid Spec\n\nThis is valid content with enough length.',
        path: '/test/valid.md',
      };

      const result = validator.validate(doc);

      expect(result.valid).toBe(true);
      expect(result.errors.length).toBe(0);
    });

    it('should detect missing required fields', () => {
      const doc: SpecDocument = {
        metadata: {
          id: '',
          name: '',
          domain: 'frontend',
          type: 'rule',
          priority: 'high',
          tags: [],
          version: '1.0.0',
          createdAt: Date.now(),
          updatedAt: Date.now(),
        },
        content: '# Content',
        path: '/test/invalid.md',
      };

      const result = validator.validate(doc);

      expect(result.valid).toBe(false);
      expect(result.errors.some(e => e.field === 'id')).toBe(true);
      expect(result.errors.some(e => e.field === 'name')).toBe(true);
    });

    it('should warn about invalid version format', () => {
      const doc: SpecDocument = {
        metadata: {
          id: 'test',
          name: 'Test',
          domain: 'frontend',
          type: 'rule',
          priority: 'high',
          tags: [],
          version: 'invalid',
          createdAt: Date.now(),
          updatedAt: Date.now(),
        },
        content: '# Content with enough text',
        path: '/test/version.md',
      };

      const result = validator.validate(doc);

      expect(result.warnings.some(w => w.field === 'version')).toBe(true);
    });

    it('should warn about empty content', () => {
      const doc: SpecDocument = {
        metadata: {
          id: 'test',
          name: 'Test',
          domain: 'frontend',
          type: 'rule',
          priority: 'high',
          tags: ['tag'],
          version: '1.0.0',
          createdAt: Date.now(),
          updatedAt: Date.now(),
        },
        content: '',
        path: '/test/empty.md',
      };

      const result = validator.validate(doc);

      expect(result.valid).toBe(false);
      expect(result.errors.some(e => e.field === 'content')).toBe(true);
    });

    it('should warn about missing heading', () => {
      const doc: SpecDocument = {
        metadata: {
          id: 'test',
          name: 'Test',
          domain: 'frontend',
          type: 'rule',
          priority: 'high',
          tags: ['tag'],
          version: '1.0.0',
          createdAt: Date.now(),
          updatedAt: Date.now(),
        },
        content: 'Content without heading but with enough length to pass.',
        path: '/test/noheading.md',
      };

      const result = validator.validate(doc);

      expect(result.warnings.some(w => w.message.includes('heading'))).toBe(true);
    });
  });

  describe('validateBatch', () => {
    it('should validate multiple specs', () => {
      const docs: SpecDocument[] = [
        {
          metadata: {
            id: 'spec1',
            name: 'Spec 1',
            domain: 'frontend',
            type: 'rule',
            priority: 'high',
            tags: ['tag'],
            version: '1.0.0',
            createdAt: Date.now(),
            updatedAt: Date.now(),
          },
          content: '# Spec 1\n\nContent',
          path: '/test/spec1.md',
        },
        {
          metadata: {
            id: 'spec2',
            name: 'Spec 2',
            domain: 'backend',
            type: 'pattern',
            priority: 'medium',
            tags: ['tag'],
            version: '1.0.0',
            createdAt: Date.now(),
            updatedAt: Date.now(),
          },
          content: '# Spec 2\n\nContent',
          path: '/test/spec2.md',
        },
      ];

      const results = validator.validateBatch(docs);

      expect(results.size).toBe(2);
      expect(results.get('spec1')?.valid).toBe(true);
      expect(results.get('spec2')?.valid).toBe(true);
    });
  });
});

describe('SpecLibrary', () => {
  let library: SpecLibrary;
  let testDir: string;

  beforeEach(async () => {
    testDir = path.join(os.tmpdir(), `spec-library-test-${Date.now()}`);
    fs.mkdirSync(testDir, { recursive: true });
    library = new SpecLibrary({ rootDir: testDir, autoReload: false });
  });

  afterEach(() => {
    library.close();
    try {
      fs.rmSync(testDir, { recursive: true, force: true });
    } catch {
      // Ignore cleanup errors
    }
  });

  describe('initialize', () => {
    it('should create directory structure', async () => {
      await library.initialize();

      expect(fs.existsSync(path.join(testDir, 'frontend'))).toBe(true);
      expect(fs.existsSync(path.join(testDir, 'backend'))).toBe(true);
      expect(fs.existsSync(path.join(testDir, 'guides'))).toBe(true);
      expect(fs.existsSync(path.join(testDir, 'common'))).toBe(true);
    });

    it('should emit initialized event', async () => {
      const listener = vi.fn();
      library.on('library:initialized', listener);

      await library.initialize();

      expect(listener).toHaveBeenCalled();
    });

    it('should load existing specs', async () => {
      // Create a spec before initialization
      const frontendDir = path.join(testDir, 'frontend');
      fs.mkdirSync(frontendDir, { recursive: true });
      fs.writeFileSync(
        path.join(frontendDir, 'react.md'),
        '---\nid: react-spec\nname: React\ndomain: frontend\ntype: rule\npriority: high\ntags: react\nversion: 1.0.0\n---\n\n# React Spec\n\nContent here.'
      );

      await library.initialize();

      expect(library.getAllSpecs().length).toBe(1);
    });
  });

  describe('addSpec', () => {
    it('should add a new spec', async () => {
      await library.initialize();

      const doc = await library.addSpec(
        'frontend',
        'new-spec',
        '# New Spec\n\nThis is a new specification.',
        { tags: ['new', 'test'] }
      );

      expect(doc).toBeDefined();
      expect(doc.metadata.name).toBe('new-spec');
      expect(doc.metadata.tags).toContain('new');
    });

    it('should emit spec:added event', async () => {
      await library.initialize();

      const listener = vi.fn();
      library.on('spec:added', listener);

      await library.addSpec('backend', 'api-spec', '# API Spec');

      expect(listener).toHaveBeenCalled();
    });
  });

  describe('getSpec', () => {
    it('should get spec by id', async () => {
      await library.initialize();
      await library.addSpec('frontend', 'get-test', '# Get Test');

      const specs = library.getAllSpecs();
      const spec = library.getSpec(specs[0].metadata.id);

      expect(spec).toBeDefined();
    });
  });

  describe('getSpecsByDomain', () => {
    it('should filter specs by domain', async () => {
      await library.initialize();
      await library.addSpec('frontend', 'fe-spec', '# Frontend');
      await library.addSpec('backend', 'be-spec', '# Backend');

      const frontendSpecs = library.getSpecsByDomain('frontend');
      const backendSpecs = library.getSpecsByDomain('backend');

      expect(frontendSpecs.length).toBe(1);
      expect(backendSpecs.length).toBe(1);
    });
  });

  describe('getSpecsByType', () => {
    it('should filter specs by type', async () => {
      await library.initialize();
      await library.addSpec('frontend', 'rule-spec', '# Rule', { type: 'rule' });
      await library.addSpec('frontend', 'pattern-spec', '# Pattern', { type: 'pattern' });

      const rules = library.getSpecsByType('rule');
      const patterns = library.getSpecsByType('pattern');

      expect(rules.length).toBe(1);
      expect(patterns.length).toBe(1);
    });
  });

  describe('getSpecsByTag', () => {
    it('should filter specs by tag', async () => {
      await library.initialize();
      await library.addSpec('frontend', 'react-spec', '# React', { tags: ['react', 'ui'] });
      await library.addSpec('frontend', 'vue-spec', '# Vue', { tags: ['vue', 'ui'] });

      const reactSpecs = library.getSpecsByTag('react');
      const uiSpecs = library.getSpecsByTag('ui');

      expect(reactSpecs.length).toBe(1);
      expect(uiSpecs.length).toBe(2);
    });
  });

  describe('searchSpecs', () => {
    it('should search specs by query', async () => {
      await library.initialize();
      await library.addSpec('frontend', 'react-components', '# React Components\n\nBuild reusable components.');
      await library.addSpec('backend', 'api-design', '# API Design\n\nRESTful API patterns.');

      const reactResults = library.searchSpecs('react');
      const apiResults = library.searchSpecs('api');

      expect(reactResults.length).toBe(1);
      expect(apiResults.length).toBe(1);
    });
  });

  describe('updateSpec', () => {
    it('should update an existing spec', async () => {
      await library.initialize();
      const doc = await library.addSpec('frontend', 'update-test', '# Original');

      const updated = await library.updateSpec(
        doc.metadata.id,
        '# Updated Content',
        { priority: 'critical' }
      );

      expect(updated?.content).toContain('Updated Content');
      expect(updated?.metadata.priority).toBe('critical');
    });

    it('should return null for non-existent spec', async () => {
      await library.initialize();

      const result = await library.updateSpec('non-existent', '# Content');

      expect(result).toBeNull();
    });
  });

  describe('deleteSpec', () => {
    it('should delete a spec', async () => {
      await library.initialize();
      const doc = await library.addSpec('frontend', 'delete-test', '# Delete Me');

      const deleted = library.deleteSpec(doc.metadata.id);

      expect(deleted).toBe(true);
      expect(library.getSpec(doc.metadata.id)).toBeUndefined();
    });

    it('should return false for non-existent spec', async () => {
      await library.initialize();

      const deleted = library.deleteSpec('non-existent');

      expect(deleted).toBe(false);
    });
  });

  describe('getStats', () => {
    it('should return library statistics', async () => {
      await library.initialize();
      await library.addSpec('frontend', 'fe1', '# FE1', { priority: 'high' });
      await library.addSpec('frontend', 'fe2', '# FE2', { priority: 'medium' });
      await library.addSpec('backend', 'be1', '# BE1', { priority: 'high' });

      const stats = library.getStats();

      expect(stats.total).toBe(3);
      expect(stats.byDomain.frontend).toBe(2);
      expect(stats.byDomain.backend).toBe(1);
      expect(stats.byPriority.high).toBe(2);
      expect(stats.byPriority.medium).toBe(1);
    });
  });

  describe('reload', () => {
    it('should reload all specs', async () => {
      await library.initialize();
      await library.addSpec('frontend', 'reload-test', '# Reload');

      const reloadListener = vi.fn();
      library.on('library:reloaded', reloadListener);

      await library.reload();

      expect(reloadListener).toHaveBeenCalled();
      expect(library.getAllSpecs().length).toBe(1);
    });
  });

  describe('close', () => {
    it('should close the library', async () => {
      await library.initialize();

      const closeListener = vi.fn();
      library.on('library:closed', closeListener);

      library.close();

      expect(closeListener).toHaveBeenCalled();
      expect(library.getAllSpecs().length).toBe(0);
    });
  });
});
