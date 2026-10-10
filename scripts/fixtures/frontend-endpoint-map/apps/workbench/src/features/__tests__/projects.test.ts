// Test callers: a test file may only feed `test_callers`, never the production
// "somebody calls it" verdict. `deleteAtomicMemory` is called here and nowhere
// else, so its endpoint must read legacy_unused rather than being kept alive
// by a test; `listWorkspaceFiles` is called here while live code also imports
// it. `get('/api/v1/debug/state')` hits a path only router.go registers, with
// no contract operation - the row must read route_only.
import { describe, it } from 'node:test';
import { get } from '../../../api';
import { deleteAtomicMemory } from '../../../services/atomic_memory';
import { listWorkspaceFiles } from '../../../services/workspace';

describe('workspace client', () => {
  it('is imported for a spy', () => {
    const spy = listWorkspaceFiles;
    void spy;
  });
});

describe('atomic memory client', () => {
  it('has a test-only caller', async () => {
    await deleteAtomicMemory('1');
  });
});

describe('debug routes', () => {
  it('reads the router-only debug state', async () => {
    await get('/api/v1/debug/state');
  });
});
