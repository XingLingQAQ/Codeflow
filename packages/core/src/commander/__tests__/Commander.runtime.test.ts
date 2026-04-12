import { describe, it, expect, vi } from 'vitest';
import { Commander } from '../Commander.js';
import { HeadlessToolRuntime } from '../../tool-runtime/HeadlessToolRuntime.js';
import type { ICliAdapter } from '../../adapters/types.js';

function createHookAwareAdapter(): ICliAdapter & { setHookManager: ReturnType<typeof vi.fn> } {
  return {
    send: vi.fn().mockResolvedValue({
      content: 'Mock response',
      model: 'test-model',
      usage: { promptTokens: 10, completionTokens: 5, totalTokens: 15 },
      finishReason: 'stop',
    }),
    receive: vi.fn(),
    getHistory: vi.fn().mockReturnValue([]),
    setHistory: vi.fn(),
    rewind: vi.fn(),
    compact: vi.fn(),
    configure: vi.fn(),
    getConfig: vi.fn().mockReturnValue({ model: 'test-model' }),
    setHookManager: vi.fn(),
  } as unknown as ICliAdapter & { setHookManager: ReturnType<typeof vi.fn> };
}

describe('Commander runtime wiring', () => {
  it('injects the shared runtime hook manager into hook-aware adapters', () => {
    const runtime = new HeadlessToolRuntime();
    const hookManager = runtime.getHookManager();
    const commander = new Commander(hookManager, 5);
    const adapter = createHookAwareAdapter();

    commander.registerAgent({
      role: 'main',
      adapter,
    });

    expect(adapter.setHookManager).toHaveBeenCalledWith(hookManager);
  });
});
