import { beforeEach, describe, expect, it, vi } from 'vitest';
import { MemoryAgentClient } from '../MemoryAgentClient.js';

describe('MemoryAgentClient code change ingest', () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal('fetch', fetchMock);
  });

  it('maps code change events into code_diff ingest requests', async () => {
    fetchMock.mockResolvedValue({
      json: async () => ({
        success: true,
        data: {
          raw_archive_id: 'raw-1',
          atomic_memory_id: 'atomic-1',
          samg_triples_count: 2,
        },
      }),
    });

    const client = new MemoryAgentClient('http://localhost:8080', 1000);
    const result = await client.ingestCodeChange({
      summary: 'Edited demo.ts via shared runtime',
      session_id: 'sess-1',
      task_id: 'task-1',
      agent_id: 'agent-1',
      snapshot_id: 'snapshot-1',
      files: ['demo.ts'],
      event_type: 'file_edit',
      metadata: { tool: 'file.edit' },
    });

    expect(result.atomic_memory_id).toBe('atomic-1');
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [, options] = fetchMock.mock.calls[0] ?? [];
    const body = JSON.parse(String(options?.body));
    expect(body.type).toBe('code_diff');
    expect(body.source).toBe('system');
    expect(body.tags).toEqual(['code-change-event', 'file_edit']);
    expect(body.metadata.snapshot_id).toBe('snapshot-1');
    expect(body.metadata.files).toEqual(['demo.ts']);
    expect(body.metadata.tool).toBe('file.edit');
  });
});
