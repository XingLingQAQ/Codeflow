import { describe, expect, it, beforeEach, vi } from 'vitest';

import { AtomicMemoryService } from '../AtomicMemoryService.js';
import { AtomicMemory } from '../types.js';

function mockApiResponse<T>(data: T): Promise<Response> {
  return Promise.resolve({
    ok: true,
    status: 200,
    json: async () => ({
      success: true,
      data,
    }),
  } as unknown as Response);
}

describe('AtomicMemoryService', () => {
  const sampleMemory: AtomicMemory = {
    id: 'm-1',
    timestamp: 1739000000,
    content: '用户偏好深色主题',
    tags: ['preference', 'ui'],
    sessionId: 's-1',
    source: 'user',
    importance: 0.8,
  };

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('add 应发送到后端并写入本地缓存', async () => {
    const fetchMock = vi.fn().mockImplementation(() => mockApiResponse(sampleMemory));
    vi.stubGlobal('fetch', fetchMock);

    const service = new AtomicMemoryService({ baseUrl: 'http://localhost:8080' });

    await service.add(sampleMemory);

    expect(fetchMock).toHaveBeenCalledTimes(1);

    const local = await service.search('深色主题', { limit: 5, offset: 0, sessionId: 's-1' });
    expect(local.length).toBeGreaterThan(0);
    expect(local[0].id).toBe('m-1');
  });

  it('search 本地命中时应优先返回本地结果', async () => {
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => mockApiResponse(sampleMemory))
      .mockImplementationOnce(() => mockApiResponse([sampleMemory]));
    vi.stubGlobal('fetch', fetchMock);

    const service = new AtomicMemoryService({ baseUrl: 'http://localhost:8080' });

    await service.add(sampleMemory);
    const result = await service.search('深色主题', { limit: 10, offset: 0 });

    expect(result.length).toBeGreaterThan(0);
    expect(result[0].id).toBe('m-1');

    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('search 本地未命中时应回退后端并缓存', async () => {
    const remoteMemory = {
      id: 'm-2',
      timestamp: 1739000010,
      content: '用户使用 Go 进行后端开发',
      tags: ['tech', 'golang'],
      session_id: 's-2',
      source: 'assistant' as const,
      importance: 0.7,
    };

    const fetchMock = vi.fn().mockImplementation(() => mockApiResponse([remoteMemory]));
    vi.stubGlobal('fetch', fetchMock);

    const service = new AtomicMemoryService({ baseUrl: 'http://localhost:8080' });

    const result = await service.search('后端开发', { limit: 10, offset: 0, sessionId: 's-2' });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(result).toHaveLength(1);
    expect(result[0].sessionId).toBe('s-2');

    const cached = await service.search('后端开发', { limit: 10, offset: 0, sessionId: 's-2' });
    expect(cached).toHaveLength(1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('update 成功后应更新本地缓存', async () => {
    const created: AtomicMemory = {
      ...sampleMemory,
      id: 'm-3',
      content: '原始内容',
      sessionId: 's-3',
    };

    const updatedFromApi = {
      ...created,
      content: '更新后内容',
      session_id: 's-3',
    };

    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => mockApiResponse(created))
      .mockImplementationOnce(() => mockApiResponse(updatedFromApi));
    vi.stubGlobal('fetch', fetchMock);

    const service = new AtomicMemoryService({ baseUrl: 'http://localhost:8080' });

    await service.add(created);
    await service.update('m-3', { content: '更新后内容' });

    const result = await service.search('更新后内容', { limit: 10, offset: 0, sessionId: 's-3' });
    expect(result).toHaveLength(1);
    expect(result[0].content).toBe('更新后内容');
  });

  it('delete 成功后应删除本地缓存', async () => {
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => mockApiResponse(sampleMemory))
      .mockImplementationOnce(() => mockApiResponse({ deleted: true, id: 'm-1' }));
    vi.stubGlobal('fetch', fetchMock);

    const service = new AtomicMemoryService({ baseUrl: 'http://localhost:8080', fallbackToRemote: false });

    await service.add(sampleMemory);
    await service.delete('m-1');

    const result = await service.search('深色主题', { limit: 10, offset: 0, sessionId: 's-1' });
    expect(result).toHaveLength(0);
  });

  it('getBySession 应从后端拉取并写入缓存', async () => {
    const fetchMock = vi.fn().mockImplementation(() => mockApiResponse([sampleMemory]));
    vi.stubGlobal('fetch', fetchMock);

    const service = new AtomicMemoryService({ baseUrl: 'http://localhost:8080' });

    const bySession = await service.getBySession('s-1');
    expect(bySession).toHaveLength(1);
    expect(bySession[0].sessionId).toBe('s-1');

    const local = await service.search('深色主题', { limit: 10, offset: 0, sessionId: 's-1' });
    expect(local).toHaveLength(1);
  });

  it('searchByTimeRange 本地命中时不走后端', async () => {
    const fetchMock = vi.fn().mockImplementation(() => mockApiResponse(sampleMemory));
    vi.stubGlobal('fetch', fetchMock);

    const service = new AtomicMemoryService({ baseUrl: 'http://localhost:8080' });

    await service.add(sampleMemory);

    const result = await service.searchByTimeRange(1738999999, 1739000100);
    expect(result).toHaveLength(1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('searchByTags 本地命中时不走后端', async () => {
    const fetchMock = vi.fn().mockImplementation(() => mockApiResponse(sampleMemory));
    vi.stubGlobal('fetch', fetchMock);

    const service = new AtomicMemoryService({ baseUrl: 'http://localhost:8080' });

    await service.add(sampleMemory);

    const result = await service.searchByTags(['preference']);
    expect(result).toHaveLength(1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
