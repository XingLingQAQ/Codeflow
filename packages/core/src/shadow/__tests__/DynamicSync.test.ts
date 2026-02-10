import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { DynamicSync, SyncProgressEvent } from '../DynamicSync.js';

function createMockBatchProjector() {
  return {
    projectFile: vi.fn().mockResolvedValue('/mock/output.intent.md'),
    projectDirectory: vi.fn().mockResolvedValue({
      total: 2,
      succeeded: 2,
      failed: 0,
      items: [],
    }),
  };
}

describe('DynamicSync', () => {
  let mockProjector: ReturnType<typeof createMockBatchProjector>;
  let events: SyncProgressEvent[];
  let sync: DynamicSync;

  beforeEach(() => {
    vi.restoreAllMocks();
    vi.useFakeTimers();
    mockProjector = createMockBatchProjector();
    events = [];
  });

  afterEach(() => {
    if (sync) {
      sync.stop();
    }
    vi.useRealTimers();
  });

  it('should start and stop without errors', () => {
    sync = new DynamicSync(
      mockProjector as never,
      { projectRoot: '/tmp/test-project', watchDirs: [] },
      (event) => events.push(event)
    );

    sync.start();
    expect(sync.isActive()).toBe(true);

    sync.stop();
    expect(sync.isActive()).toBe(false);
  });

  it('should not start twice', () => {
    sync = new DynamicSync(
      mockProjector as never,
      { projectRoot: '/tmp/test-project', watchDirs: [] }
    );

    sync.start();
    sync.start();
    expect(sync.isActive()).toBe(true);

    sync.stop();
  });

  it('should debounce file changes', () => {
    sync = new DynamicSync(
      mockProjector as never,
      {
        projectRoot: '/tmp/test-project',
        debounceMs: 300,
        watchDirs: [],
      },
      (event) => events.push(event)
    );

    sync.start();

    sync.onFileChange('/tmp/test-project/src/index.ts');
    sync.onFileChange('/tmp/test-project/src/index.ts');
    sync.onFileChange('/tmp/test-project/src/index.ts');

    expect(sync.getPendingCount()).toBe(1);
  });

  it('should ignore non-matching extensions', () => {
    sync = new DynamicSync(
      mockProjector as never,
      {
        projectRoot: '/tmp/test-project',
        watchExtensions: ['.ts', '.js'],
        watchDirs: [],
      },
      (event) => events.push(event)
    );

    sync.start();

    sync.onFileChange('/tmp/test-project/style.css');
    sync.onFileChange('/tmp/test-project/readme.md');

    expect(sync.getPendingCount()).toBe(0);
  });

  it('should ignore .codeflow and node_modules paths', () => {
    sync = new DynamicSync(
      mockProjector as never,
      {
        projectRoot: '/tmp/test-project',
        watchDirs: [],
      },
      (event) => events.push(event)
    );

    sync.start();

    sync.onFileChange('/tmp/test-project/.codeflow/domain/index.intent.md');
    sync.onFileChange('/tmp/test-project/node_modules/pkg/index.ts');

    expect(sync.getPendingCount()).toBe(0);
  });

  it('should not process events when stopped', () => {
    sync = new DynamicSync(
      mockProjector as never,
      {
        projectRoot: '/tmp/test-project',
        watchDirs: [],
      },
      (event) => events.push(event)
    );

    sync.onFileChange('/tmp/test-project/src/index.ts');
    expect(sync.getPendingCount()).toBe(0);
  });

  it('should clear pending files on stop', () => {
    sync = new DynamicSync(
      mockProjector as never,
      {
        projectRoot: '/tmp/test-project',
        debounceMs: 1000,
        watchDirs: [],
      },
      (event) => events.push(event)
    );

    sync.start();
    sync.onFileChange('/tmp/test-project/src/a.ts');
    sync.onFileChange('/tmp/test-project/src/b.ts');

    expect(sync.getPendingCount()).toBe(2);

    sync.stop();
    expect(sync.getPendingCount()).toBe(0);
  });

  it('should call syncAll with projectDirectory', async () => {
    sync = new DynamicSync(
      mockProjector as never,
      {
        projectRoot: '/tmp/test-project',
        watchDirs: ['src'],
      },
      (event) => events.push(event)
    );

    await sync.syncAll();

    expect(mockProjector.projectDirectory).toHaveBeenCalledTimes(1);
    expect(events.some((e) => e.type === 'start')).toBe(true);
    expect(events.some((e) => e.type === 'complete')).toBe(true);
  });

  it('should not run syncAll concurrently', async () => {
    sync = new DynamicSync(
      mockProjector as never,
      {
        projectRoot: '/tmp/test-project',
        watchDirs: ['src'],
      }
    );

    mockProjector.projectDirectory.mockImplementation(
      () => new Promise((resolve) => setTimeout(() => resolve({ total: 1, succeeded: 1, failed: 0, items: [] }), 100))
    );

    const p1 = sync.syncAll();
    const p2 = sync.syncAll();

    vi.advanceTimersByTime(200);
    await Promise.all([p1, p2]);

    expect(mockProjector.projectDirectory).toHaveBeenCalledTimes(1);
  });
});
