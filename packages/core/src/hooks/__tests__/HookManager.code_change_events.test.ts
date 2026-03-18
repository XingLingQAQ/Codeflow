import { describe, it, expect } from 'vitest';
import { HookManager } from '../HookManager.js';
import { InMemoryCodeChangeEventStore } from '../CodeChangeEventStore.js';

describe('HookManager code change events', () => {
  it('records exec and checkpoint events for file edits', async () => {
    const store = new InMemoryCodeChangeEventStore();
    const manager = new HookManager(store);

    const snapshotId = await manager.hook_after_exec({
      command: 'node edit-file.js',
      exitCode: 0,
      stdout: 'ok',
      stderr: '',
      timestamp: Date.now(),
      sessionId: 'sess-1',
      taskId: 'task-1',
      agentId: 'agent-1',
      filesModified: ['demo.ts'],
    });

    const events = store.listCodeChangeEvents();
    expect(snapshotId).toBeDefined();
    expect(events).toHaveLength(2);
    expect(events[0]?.type).toBe('file_edit');
    expect(events[0]?.snapshotId).toBe(snapshotId);
    expect(events[0]?.files).toEqual(['demo.ts']);
    expect(events[1]?.type).toBe('checkpoint_create');
    expect(events[1]?.metadata?.trigger).toBe('hook_after_exec');
  });

  it('records restore events', async () => {
    const store = new InMemoryCodeChangeEventStore();
    const manager = new HookManager(store);

    await manager.hook_restore_state('snapshot-123');

    const events = store.listCodeChangeEvents();
    expect(events).toHaveLength(1);
    expect(events[0]?.type).toBe('restore');
    expect(events[0]?.snapshotId).toBe('snapshot-123');
  });

  it('classifies batch edits and formatting commands', async () => {
    const store = new InMemoryCodeChangeEventStore();
    const manager = new HookManager(store);

    await manager.hook_after_exec({
      command: 'prettier --write src',
      exitCode: 0,
      stdout: '',
      stderr: '',
      timestamp: Date.now(),
      filesModified: ['a.ts', 'b.ts'],
    });

    const events = store.listCodeChangeEvents({ limit: 2 });
    expect(events[0]?.type).toBe('batch_edit');
    expect(events[1]?.type).toBe('checkpoint_create');
  });
});
