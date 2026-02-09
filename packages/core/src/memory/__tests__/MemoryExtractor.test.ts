import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';

import { MemoryExtractor } from '../MemoryExtractor.js';
import { AtomicMemoryService } from '../AtomicMemoryService.js';

describe('MemoryExtractor', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('extractFromConversation 应异步提取并写入 AtomicMemoryService', async () => {
    vi.useFakeTimers();

    const addMock = vi.fn().mockResolvedValue(undefined);
    const memoryService = {
      add: addMock,
    } as unknown as AtomicMemoryService;

    const adapter = {
      send: vi.fn().mockResolvedValue({
        content: JSON.stringify({
          memories: [
            {
              content: '用户偏好使用 Go 语言开发后端服务',
              tags: ['preference', 'golang'],
              importance: 0.9,
              source: 'user',
            },
          ],
        }),
        model: 'test-model',
      }),
    };

    const extractor = new MemoryExtractor(adapter, memoryService, {
      asyncDelayMs: 10,
      minImportance: 0.2,
    });

    extractor.extractFromConversation('我最近都在写 Go', '我会记住你的技术偏好', 'session-1');

    expect(adapter.send).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(10);

    expect(adapter.send).toHaveBeenCalledTimes(1);
    expect(addMock).toHaveBeenCalledTimes(1);

    const added = addMock.mock.calls[0][0];
    expect(added.content).toContain('Go');
    expect(added.tags).toContain('golang');
    expect(added.sessionId).toBe('session-1');
    expect(added.id).toMatch(/^mem_/);
    expect(typeof added.timestamp).toBe('number');
  });

  it('JSON 非法时应静默失败且不写入', async () => {
    vi.useFakeTimers();

    const addMock = vi.fn().mockResolvedValue(undefined);
    const memoryService = {
      add: addMock,
    } as unknown as AtomicMemoryService;

    const adapter = {
      send: vi.fn().mockResolvedValue({
        content: 'not-json-response',
        model: 'test-model',
      }),
    };

    const extractor = new MemoryExtractor(adapter, memoryService, {
      asyncDelayMs: 0,
      minImportance: 0.2,
    });

    extractor.extractFromConversation('hello', 'world', 'session-2');

    await vi.runAllTimersAsync();

    expect(adapter.send).toHaveBeenCalledTimes(1);
    expect(addMock).not.toHaveBeenCalled();
  });

  it('importance 低于阈值的记忆应被过滤', async () => {
    vi.useFakeTimers();

    const addMock = vi.fn().mockResolvedValue(undefined);
    const memoryService = {
      add: addMock,
    } as unknown as AtomicMemoryService;

    const adapter = {
      send: vi.fn().mockResolvedValue({
        content: JSON.stringify({
          memories: [
            {
              content: '临时性闲聊信息',
              tags: ['chitchat'],
              importance: 0.1,
            },
            {
              content: '用户偏好深色主题',
              tags: ['ui', 'preference'],
              importance: 0.8,
            },
          ],
        }),
        model: 'test-model',
      }),
    };

    const extractor = new MemoryExtractor(adapter, memoryService, {
      asyncDelayMs: 0,
      minImportance: 0.3,
    });

    extractor.extractFromConversation('我喜欢深色主题', '收到', 'session-3');

    await vi.runAllTimersAsync();

    expect(addMock).toHaveBeenCalledTimes(1);
    expect(addMock.mock.calls[0][0].content).toContain('深色主题');
  });

  it('空 sessionId 或空对话时应直接返回', async () => {
    vi.useFakeTimers();

    const addMock = vi.fn().mockResolvedValue(undefined);
    const memoryService = {
      add: addMock,
    } as unknown as AtomicMemoryService;

    const adapter = {
      send: vi.fn(),
    };

    const extractor = new MemoryExtractor(adapter, memoryService, {
      asyncDelayMs: 0,
    });

    extractor.extractFromConversation('message', 'reply', '   ');
    extractor.extractFromConversation('   ', '   ', 'session-4');

    await vi.runAllTimersAsync();

    expect(adapter.send).not.toHaveBeenCalled();
    expect(addMock).not.toHaveBeenCalled();
  });
});
