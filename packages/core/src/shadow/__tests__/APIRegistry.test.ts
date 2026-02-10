import { afterEach, describe, expect, it } from 'vitest';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

import { APIRegistry, APIRegistryEntry } from '../APIRegistry.js';

describe('APIRegistry', () => {
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

  it('register should add entry when no duplicate exists', async () => {
    const registry = new APIRegistry();

    const entry: APIRegistryEntry = {
      path: '/api/v1/users',
      method: 'GET',
      description: 'List all users',
      handler: 'UserController.list',
      tags: ['user', 'list'],
    };

    const result = await registry.register(entry);
    expect(result.isDuplicate).toBe(false);
    expect(result.similarEntries).toHaveLength(0);
    expect(registry.getEntryCount()).toBe(1);
  });

  it('checkDuplicate should detect exact path+method duplicate', async () => {
    const registry = new APIRegistry();

    const entry1: APIRegistryEntry = {
      path: '/api/v1/users',
      method: 'GET',
      description: 'List all users',
      handler: 'UserController.list',
      tags: ['user', 'list'],
    };

    const entry2: APIRegistryEntry = {
      path: '/api/v1/users',
      method: 'GET',
      description: 'Get user list',
      handler: 'UserHandler.getAll',
      tags: ['user'],
    };

    await registry.register(entry1);
    const result = registry.checkDuplicate(entry2);
    expect(result.isDuplicate).toBe(true);
    expect(result.similarEntries.length).toBeGreaterThan(0);
    expect(result.similarEntries[0].similarity).toBe(1.0);
  });

  it('checkDuplicate should detect semantically similar APIs', async () => {
    const registry = new APIRegistry({ similarityThreshold: 0.4 });

    const entry1: APIRegistryEntry = {
      path: '/api/v1/users',
      method: 'GET',
      description: 'List all users with pagination',
      handler: 'UserController.list',
      tags: ['user', 'list', 'pagination'],
    };

    const entry2: APIRegistryEntry = {
      path: '/api/v2/users/list',
      method: 'GET',
      description: 'Get all users with pagination support',
      handler: 'UserHandler.listAll',
      tags: ['user', 'list', 'pagination'],
    };

    await registry.register(entry1);
    const result = registry.checkDuplicate(entry2);
    expect(result.isDuplicate).toBe(true);
    expect(result.similarEntries.length).toBeGreaterThan(0);
  });

  it('search should find matching APIs', async () => {
    const registry = new APIRegistry();

    await registry.register({
      path: '/api/v1/users',
      method: 'GET',
      description: 'List all users',
      handler: 'UserController.list',
      tags: ['user', 'list'],
    });

    await registry.register({
      path: '/api/v1/orders',
      method: 'POST',
      description: 'Create a new order',
      handler: 'OrderController.create',
      tags: ['order', 'create'],
    });

    await registry.register({
      path: '/api/v1/users/:id',
      method: 'GET',
      description: 'Get user by ID',
      handler: 'UserController.getById',
      tags: ['user', 'detail'],
    });

    const results = registry.search('user');
    expect(results.length).toBeGreaterThanOrEqual(2);
    expect(results.every((r) => r.similarity !== undefined && r.similarity > 0)).toBe(true);
  });

  it('search should return empty for unrelated query', async () => {
    const registry = new APIRegistry();

    await registry.register({
      path: '/api/v1/users',
      method: 'GET',
      description: 'List all users',
      handler: 'UserController.list',
      tags: ['user', 'list'],
    });

    const results = registry.search('zzzzzzzzz');
    expect(results).toHaveLength(0);
  });

  it('saveToYaml and loadFromYaml should persist entries', async () => {
    const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'api-registry-'));
    tempDirs.push(tmpDir);

    const registryPath = path.join(tmpDir, '.codeflow', 'registry', 'apis.yaml');

    const registry1 = new APIRegistry({ registryPath });
    await registry1.register({
      path: '/api/v1/users',
      method: 'GET',
      description: 'List all users',
      handler: 'UserController.list',
      tags: ['user', 'list'],
    });
    await registry1.register({
      path: '/api/v1/orders',
      method: 'POST',
      description: 'Create order',
      handler: 'OrderController.create',
      tags: ['order'],
    });
    await registry1.saveToYaml();

    expect(fs.existsSync(registryPath)).toBe(true);

    const registry2 = new APIRegistry({ registryPath });
    await registry2.loadFromYaml();
    expect(registry2.getEntryCount()).toBe(2);

    const entries = registry2.getEntries();
    expect(entries[0].path).toBe('/api/v1/users');
    expect(entries[1].path).toBe('/api/v1/orders');
  });

  it('loadFromYaml should handle missing file gracefully', async () => {
    const registry = new APIRegistry({ registryPath: '/nonexistent/path/apis.yaml' });
    await registry.loadFromYaml();
    expect(registry.getEntryCount()).toBe(0);
  });
});
