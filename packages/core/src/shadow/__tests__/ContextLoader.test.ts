import { afterEach, describe, expect, it } from 'vitest';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

import { ContextLoader } from '../ContextLoader.js';

describe('ContextLoader', () => {
  const tempDirs: string[] = [];

  afterEach(() => {
    for (const dir of tempDirs) {
      try {
        fs.rmSync(dir, { recursive: true, force: true });
      } catch {
        // ignore cleanup errors
      }
    }
    tempDirs.length = 0;
  });

  function createProjectStructure(projectRoot: string): void {
    const shadowDomain = path.join(projectRoot, '.codeflow', 'domain');
    fs.mkdirSync(path.join(shadowDomain, 'auth'), { recursive: true });
    fs.mkdirSync(path.join(shadowDomain, 'order'), { recursive: true });

    fs.writeFileSync(
      path.join(shadowDomain, 'auth', 'login.intent.md'),
      '# Login Intent\n\nHandles user authentication login flow.\nValidates credentials and returns JWT token.\nTags: user, auth, login, jwt',
      'utf-8',
    );

    fs.writeFileSync(
      path.join(shadowDomain, 'auth', 'register.intent.md'),
      '# Register Intent\n\nHandles user registration.\nCreates new user account with email validation.\nTags: user, auth, register',
      'utf-8',
    );

    fs.writeFileSync(
      path.join(shadowDomain, 'order', 'create.intent.md'),
      '# Create Order Intent\n\nHandles order creation.\nValidates cart items and processes payment.\nTags: order, create, payment',
      'utf-8',
    );
  }

  it('loadContext should return relevant docs within token budget', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'context-loader-'));
    tempDirs.push(projectRoot);
    createProjectStructure(projectRoot);

    const loader = new ContextLoader({
      projectRoot,
      shadowRoot: '.codeflow',
      maxTokenBudget: 8000,
      cacheEnabled: true,
    });

    const result = await loader.loadContext('user authentication login');
    expect(result.contexts.length).toBeGreaterThanOrEqual(1);
    expect(result.totalTokens).toBeGreaterThan(0);
    expect(result.totalTokens).toBeLessThanOrEqual(8000);
    expect(result.budgetRemaining).toBeGreaterThanOrEqual(0);
  });

  it('loadContext should respect token budget', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'context-loader-budget-'));
    tempDirs.push(projectRoot);
    createProjectStructure(projectRoot);

    const loader = new ContextLoader({
      projectRoot,
      shadowRoot: '.codeflow',
      maxTokenBudget: 10,
      cacheEnabled: false,
    });

    const result = await loader.loadContext('user auth login');
    expect(result.totalTokens).toBeLessThanOrEqual(10);
  });

  it('loadContext should return empty for unrelated query', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'context-loader-empty-'));
    tempDirs.push(projectRoot);
    createProjectStructure(projectRoot);

    const loader = new ContextLoader({
      projectRoot,
      shadowRoot: '.codeflow',
      maxTokenBudget: 8000,
    });

    const result = await loader.loadContext('zzzzzzzzz');
    expect(result.contexts).toHaveLength(0);
    expect(result.totalTokens).toBe(0);
  });

  it('loadWithDependencies should load intent doc for a source file', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'context-loader-deps-'));
    tempDirs.push(projectRoot);

    const srcDir = path.join(projectRoot, 'src', 'auth');
    fs.mkdirSync(srcDir, { recursive: true });

    const sourceFile = path.join(srcDir, 'login.ts');
    fs.writeFileSync(sourceFile, "import { validate } from './validator.js';\n\nexport function login() {}\n", 'utf-8');

    const shadowDomain = path.join(projectRoot, '.codeflow', 'domain', 'src', 'auth');
    fs.mkdirSync(shadowDomain, { recursive: true });

    fs.writeFileSync(
      path.join(shadowDomain, 'login.intent.md'),
      '# Login\n\nAuthentication login handler.',
      'utf-8',
    );

    const loader = new ContextLoader({
      projectRoot,
      shadowRoot: '.codeflow',
      maxTokenBudget: 8000,
    });

    const result = await loader.loadWithDependencies(sourceFile);
    expect(result.contexts.length).toBeGreaterThanOrEqual(1);
    expect(result.contexts[0].content).toContain('Login');
  });

  it('loadWithDependencies should handle missing intent doc gracefully', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'context-loader-missing-'));
    tempDirs.push(projectRoot);

    const sourceFile = path.join(projectRoot, 'src', 'nonexistent.ts');

    const loader = new ContextLoader({
      projectRoot,
      shadowRoot: '.codeflow',
      maxTokenBudget: 8000,
    });

    const result = await loader.loadWithDependencies(sourceFile);
    expect(result.contexts).toHaveLength(0);
    expect(result.totalTokens).toBe(0);
  });

  it('clearCache should empty the cache', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'context-loader-cache-'));
    tempDirs.push(projectRoot);
    createProjectStructure(projectRoot);

    const loader = new ContextLoader({
      projectRoot,
      shadowRoot: '.codeflow',
      maxTokenBudget: 8000,
      cacheEnabled: true,
    });

    await loader.loadContext('user auth');
    loader.clearCache();

    // After clearing cache, loading again should still work
    const result = await loader.loadContext('user auth');
    expect(result.contexts.length).toBeGreaterThanOrEqual(1);
  });
});
