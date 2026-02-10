import { afterEach, describe, expect, it } from 'vitest';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

import { ModelDictionary, ModelEntry, ModelField } from '../ModelDictionary.js';

describe('ModelDictionary', () => {
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

  it('register should add model when no duplicate exists', async () => {
    const dict = new ModelDictionary();

    const entry: ModelEntry = {
      name: 'User',
      fields: [
        { name: 'id', type: 'string', required: true },
        { name: 'email', type: 'string', required: true },
        { name: 'name', type: 'string', required: false },
      ],
      relationships: [],
      source: 'packages/core/src/models/User.ts',
      tags: ['user', 'auth'],
    };

    const result = await dict.register(entry);
    expect(result.isDuplicate).toBe(false);
    expect(dict.getModelCount()).toBe(1);
  });

  it('checkDuplicate should detect exact name duplicate', async () => {
    const dict = new ModelDictionary();

    const entry1: ModelEntry = {
      name: 'User',
      fields: [
        { name: 'id', type: 'string', required: true },
        { name: 'email', type: 'string', required: true },
      ],
      relationships: [],
      source: 'models/User.ts',
      tags: ['user'],
    };

    const entry2: ModelEntry = {
      name: 'User',
      fields: [
        { name: 'id', type: 'number', required: true },
        { name: 'username', type: 'string', required: true },
      ],
      relationships: [],
      source: 'dto/UserDTO.ts',
      tags: ['user', 'dto'],
    };

    await dict.register(entry1);
    const result = dict.checkDuplicate(entry2);
    expect(result.isDuplicate).toBe(true);
    expect(result.similarModels.length).toBeGreaterThan(0);
    expect(result.similarModels[0].similarity).toBe(1.0);
  });

  it('checkDuplicate should detect structurally similar models', async () => {
    const dict = new ModelDictionary({ similarityThreshold: 0.3 });

    const fields: ModelField[] = [
      { name: 'id', type: 'string', required: true },
      { name: 'email', type: 'string', required: true },
      { name: 'name', type: 'string', required: false },
      { name: 'createdAt', type: 'number', required: true },
    ];

    const entry1: ModelEntry = {
      name: 'UserEntity',
      fields,
      relationships: [],
      source: 'entities/User.ts',
      tags: ['user', 'entity'],
    };

    const entry2: ModelEntry = {
      name: 'UserDTO',
      fields: [
        { name: 'id', type: 'string', required: true },
        { name: 'email', type: 'string', required: true },
        { name: 'name', type: 'string', required: false },
      ],
      relationships: [],
      source: 'dto/UserDTO.ts',
      tags: ['user', 'dto'],
    };

    await dict.register(entry1);
    const result = dict.checkDuplicate(entry2);
    expect(result.isDuplicate).toBe(true);
    expect(result.similarModels.length).toBeGreaterThan(0);
  });

  it('search should find matching models', async () => {
    const dict = new ModelDictionary();

    await dict.register({
      name: 'User',
      fields: [{ name: 'id', type: 'string', required: true }],
      relationships: [],
      source: 'models/User.ts',
      tags: ['user', 'auth'],
    });

    await dict.register({
      name: 'Order',
      fields: [{ name: 'id', type: 'string', required: true }],
      relationships: [],
      source: 'models/Order.ts',
      tags: ['order', 'commerce'],
    });

    const results = dict.search('user');
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(results[0].name).toBe('User');
  });

  it('recordRelationship should store entity-dto relationships', () => {
    const dict = new ModelDictionary();

    dict.recordRelationship('UserEntity', 'UserDTO', 'map');
    dict.recordRelationship('OrderEntity', 'OrderResponse', 'subset');
    dict.recordRelationship('UserEntity', 'UserDTO', 'map'); // duplicate

    const rels = dict.getRelationships();
    expect(rels).toHaveLength(2);
    expect(rels[0]).toEqual({ entity: 'UserEntity', dto: 'UserDTO', type: 'map' });
    expect(rels[1]).toEqual({ entity: 'OrderEntity', dto: 'OrderResponse', type: 'subset' });
  });

  it('saveToYaml and loadFromYaml should persist models', async () => {
    const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'model-dict-'));
    tempDirs.push(tmpDir);

    const dictionaryPath = path.join(tmpDir, '.codeflow', 'registry', 'models.yaml');

    const dict1 = new ModelDictionary({ dictionaryPath });
    await dict1.register({
      name: 'User',
      fields: [
        { name: 'id', type: 'string', required: true },
        { name: 'email', type: 'string', required: true },
      ],
      relationships: [],
      source: 'models/User.ts',
      tags: ['user'],
    });
    dict1.recordRelationship('User', 'UserDTO', 'map');
    await dict1.saveToYaml();

    expect(fs.existsSync(dictionaryPath)).toBe(true);

    const dict2 = new ModelDictionary({ dictionaryPath });
    await dict2.loadFromYaml();
    expect(dict2.getModelCount()).toBe(1);
    expect(dict2.getModels()[0].name).toBe('User');
  });

  it('loadFromYaml should handle missing file gracefully', async () => {
    const dict = new ModelDictionary({ dictionaryPath: '/nonexistent/path/models.yaml' });
    await dict.loadFromYaml();
    expect(dict.getModelCount()).toBe(0);
  });
});
