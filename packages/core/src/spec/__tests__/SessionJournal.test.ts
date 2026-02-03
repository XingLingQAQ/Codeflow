import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import * as fs from 'fs/promises';
import * as path from 'path';
import {
  SessionJournal,
  JournalWriter,
  JournalIndexManager,
  ContextRestorer,
  SessionEntry,
  SessionSummary,
  JournalIndexEntry,
} from '../SessionJournal.js';

// Mock fs
vi.mock('fs/promises', () => ({
  readFile: vi.fn(),
  writeFile: vi.fn(),
  access: vi.fn(),
  mkdir: vi.fn(),
}));

const mockFs = fs as unknown as {
  readFile: ReturnType<typeof vi.fn>;
  writeFile: ReturnType<typeof vi.fn>;
  access: ReturnType<typeof vi.fn>;
  mkdir: ReturnType<typeof vi.fn>;
};

// Mock data
const createMockEntry = (role: SessionEntry['role'], content: string): Omit<SessionEntry, 'timestamp'> => ({
  role,
  content,
});

const createMockSummary = (sessionId: string = 'session-1'): SessionSummary => ({
  id: `summary_${sessionId}`,
  sessionId,
  userId: 'user-1',
  title: 'Test Session',
  summary: 'This is a test session summary.',
  keyPoints: ['Point 1', 'Point 2'],
  decisions: ['Decision 1'],
  codeChanges: [
    { file: 'src/index.ts', type: 'modified', description: 'Updated exports' },
  ],
  tags: ['typescript', 'test'],
  startTime: Date.now() - 3600000,
  endTime: Date.now(),
  entryCount: 10,
  createdAt: Date.now(),
});

describe('JournalWriter', () => {
  let writer: JournalWriter;

  beforeEach(() => {
    vi.clearAllMocks();
    mockFs.access.mockRejectedValue(new Error('Not found'));
    mockFs.mkdir.mockResolvedValue(undefined);
    mockFs.writeFile.mockResolvedValue(undefined);
    writer = new JournalWriter({ workspaceDir: '/test/workspace' });
  });

  describe('write', () => {
    it('should write summary to file', async () => {
      const summary = createMockSummary();
      const filePath = await writer.write(summary);

      expect(filePath).toContain('journal-');
      expect(filePath).toContain('.md');
      expect(mockFs.writeFile).toHaveBeenCalled();
    });

    it('should emit journal:written event', async () => {
      const listener = vi.fn();
      writer.on('journal:written', listener);

      await writer.write(createMockSummary());

      expect(listener).toHaveBeenCalled();
    });

    it('should create directory if not exists', async () => {
      await writer.write(createMockSummary());

      expect(mockFs.mkdir).toHaveBeenCalledWith(
        expect.stringContaining('user-1'),
        { recursive: true }
      );
    });

    it('should format summary as markdown', async () => {
      const summary = createMockSummary();
      await writer.write(summary);

      const writeCall = mockFs.writeFile.mock.calls[0];
      const content = writeCall[1] as string;

      expect(content).toContain('# Test Session');
      expect(content).toContain('## Summary');
      expect(content).toContain('## Key Points');
      expect(content).toContain('## Decisions');
      expect(content).toContain('## Code Changes');
      expect(content).toContain('## Tags');
    });
  });
});

describe('JournalIndexManager', () => {
  let indexManager: JournalIndexManager;

  beforeEach(() => {
    vi.clearAllMocks();
    mockFs.access.mockRejectedValue(new Error('Not found'));
    mockFs.mkdir.mockResolvedValue(undefined);
    mockFs.writeFile.mockResolvedValue(undefined);
    mockFs.readFile.mockRejectedValue(new Error('Not found'));
    indexManager = new JournalIndexManager({ workspaceDir: '/test/workspace' });
  });

  describe('loadIndex', () => {
    it('should create new index if not exists', async () => {
      const index = await indexManager.loadIndex('user-1');

      expect(index.userId).toBe('user-1');
      expect(index.entries).toHaveLength(0);
    });

    it('should parse existing index', async () => {
      const indexContent = `# Session Journal Index

> User: user-1
> Version: 1.0
> Updated: 2026-02-03T00:00:00.000Z
> Total: 1 sessions

## Sessions

| Date | Title | Summary | Session ID | Path |
|------|-------|---------|------------|------|
| 2026-02-03 | Test | Test summary | session-1 | journal-2026-02-03.md |`;

      mockFs.readFile.mockResolvedValue(indexContent);

      const index = await indexManager.loadIndex('user-1');

      expect(index.entries).toHaveLength(1);
      expect(index.entries[0].title).toBe('Test');
    });

    it('should cache loaded index', async () => {
      await indexManager.loadIndex('user-1');
      await indexManager.loadIndex('user-1');

      // readFile should only be called once due to caching
      expect(mockFs.readFile).toHaveBeenCalledTimes(1);
    });
  });

  describe('addEntry', () => {
    it('should add entry to index', async () => {
      const entry: JournalIndexEntry = {
        id: 'entry-1',
        sessionId: 'session-1',
        title: 'Test',
        summary: 'Test summary',
        tags: ['test'],
        date: '2026-02-03',
        path: 'journal-2026-02-03.md',
      };

      await indexManager.addEntry('user-1', entry);

      const index = await indexManager.loadIndex('user-1');
      expect(index.entries).toContainEqual(entry);
    });

    it('should emit index:updated event', async () => {
      const listener = vi.fn();
      indexManager.on('index:updated', listener);

      await indexManager.addEntry('user-1', {
        id: 'entry-1',
        sessionId: 'session-1',
        title: 'Test',
        summary: 'Test summary',
        tags: [],
        date: '2026-02-03',
        path: 'journal.md',
      });

      expect(listener).toHaveBeenCalled();
    });

    it('should limit entries to maxJournals', async () => {
      const manager = new JournalIndexManager({
        workspaceDir: '/test/workspace',
        maxJournals: 2,
      });

      for (let i = 0; i < 5; i++) {
        await manager.addEntry('user-1', {
          id: `entry-${i}`,
          sessionId: `session-${i}`,
          title: `Test ${i}`,
          summary: 'Summary',
          tags: [],
          date: '2026-02-03',
          path: `journal-${i}.md`,
        });
      }

      const index = await manager.loadIndex('user-1');
      expect(index.entries.length).toBeLessThanOrEqual(2);
    });
  });

  describe('search', () => {
    beforeEach(async () => {
      await indexManager.addEntry('user-1', {
        id: 'entry-1',
        sessionId: 'session-1',
        title: 'React Component',
        summary: 'Building a new component',
        tags: ['react', 'frontend'],
        date: '2026-02-03',
        path: 'journal-1.md',
      });
      await indexManager.addEntry('user-1', {
        id: 'entry-2',
        sessionId: 'session-2',
        title: 'API Integration',
        summary: 'Integrating REST API',
        tags: ['api', 'backend'],
        date: '2026-02-03',
        path: 'journal-2.md',
      });
    });

    it('should search by title', async () => {
      const results = await indexManager.search('user-1', 'React');
      expect(results).toHaveLength(1);
      expect(results[0].title).toBe('React Component');
    });

    it('should search by summary', async () => {
      const results = await indexManager.search('user-1', 'REST');
      expect(results).toHaveLength(1);
      expect(results[0].title).toBe('API Integration');
    });

    it('should search by tags', async () => {
      const results = await indexManager.search('user-1', 'frontend');
      expect(results).toHaveLength(1);
    });

    it('should return empty for no matches', async () => {
      const results = await indexManager.search('user-1', 'nonexistent');
      expect(results).toHaveLength(0);
    });
  });

  describe('getRecent', () => {
    it('should return recent entries', async () => {
      for (let i = 0; i < 10; i++) {
        await indexManager.addEntry('user-1', {
          id: `entry-${i}`,
          sessionId: `session-${i}`,
          title: `Test ${i}`,
          summary: 'Summary',
          tags: [],
          date: '2026-02-03',
          path: `journal-${i}.md`,
        });
      }

      const recent = await indexManager.getRecent('user-1', 3);
      expect(recent).toHaveLength(3);
    });
  });
});

describe('ContextRestorer', () => {
  let restorer: ContextRestorer;
  let indexManager: JournalIndexManager;

  beforeEach(() => {
    vi.clearAllMocks();
    mockFs.access.mockRejectedValue(new Error('Not found'));
    mockFs.mkdir.mockResolvedValue(undefined);
    mockFs.writeFile.mockResolvedValue(undefined);
    mockFs.readFile.mockRejectedValue(new Error('Not found'));

    indexManager = new JournalIndexManager({ workspaceDir: '/test/workspace' });
    restorer = new ContextRestorer({ workspaceDir: '/test/workspace' }, indexManager);
  });

  describe('restore', () => {
    it('should restore context with empty history', async () => {
      const result = await restorer.restore('user-1');

      expect(result.summaries).toHaveLength(0);
      expect(result.suggestions).toContain('No previous sessions found. Start a new conversation!');
    });

    it('should emit restore:complete event', async () => {
      const listener = vi.fn();
      restorer.on('restore:complete', listener);

      await restorer.restore('user-1');

      expect(listener).toHaveBeenCalled();
    });

    it('should build relevant context', async () => {
      // Add some entries
      await indexManager.addEntry('user-1', {
        id: 'entry-1',
        sessionId: 'session-1',
        title: 'Test Session',
        summary: 'Test summary',
        tags: ['test'],
        date: '2026-02-03',
        path: 'journal-1.md',
      });

      // Mock reading the journal file
      const journalContent = `# Test Session

> Session: session-1
> Date: 2026-02-03T00:00:00.000Z
> Entries: 5

## Summary

This is a test summary.

## Key Points

- Point 1
- Point 2`;

      mockFs.readFile.mockResolvedValue(journalContent);

      const result = await restorer.restore('user-1');

      expect(result.relevantContext).toContain('Recent Session Context');
    });
  });
});

describe('SessionJournal', () => {
  let journal: SessionJournal;

  beforeEach(() => {
    vi.clearAllMocks();
    mockFs.access.mockRejectedValue(new Error('Not found'));
    mockFs.mkdir.mockResolvedValue(undefined);
    mockFs.writeFile.mockResolvedValue(undefined);
    mockFs.readFile.mockRejectedValue(new Error('Not found'));

    journal = new SessionJournal({ workspaceDir: '/test/workspace' });
  });

  describe('startSession', () => {
    it('should start a new session', () => {
      journal.startSession('session-1', 'user-1');

      expect(journal.getCurrentSessionId()).toBe('session-1');
      expect(journal.getCurrentSession()).toHaveLength(0);
    });

    it('should emit session:started event', () => {
      const listener = vi.fn();
      journal.on('session:started', listener);

      journal.startSession('session-1', 'user-1');

      expect(listener).toHaveBeenCalledWith({
        sessionId: 'session-1',
        userId: 'user-1',
      });
    });
  });

  describe('addEntry', () => {
    beforeEach(() => {
      journal.startSession('session-1', 'user-1');
    });

    it('should add entry to current session', () => {
      journal.addEntry(createMockEntry('user', 'Hello'));

      const session = journal.getCurrentSession();
      expect(session).toHaveLength(1);
      expect(session[0].content).toBe('Hello');
    });

    it('should emit entry:added event', () => {
      const listener = vi.fn();
      journal.on('entry:added', listener);

      journal.addEntry(createMockEntry('user', 'Hello'));

      expect(listener).toHaveBeenCalled();
    });

    it('should add timestamp to entry', () => {
      journal.addEntry(createMockEntry('user', 'Hello'));

      const session = journal.getCurrentSession();
      expect(session[0].timestamp).toBeDefined();
    });
  });

  describe('endSession', () => {
    beforeEach(() => {
      journal.startSession('session-1', 'user-1');
    });

    it('should return null for empty session', async () => {
      const summary = await journal.endSession();
      expect(summary).toBeNull();
    });

    it('should generate summary for non-empty session', async () => {
      journal.addEntry(createMockEntry('user', 'Hello, how are you?'));
      journal.addEntry(createMockEntry('assistant', 'I am doing well!'));

      const summary = await journal.endSession();

      expect(summary).not.toBeNull();
      expect(summary?.sessionId).toBe('session-1');
      expect(summary?.entryCount).toBe(2);
    });

    it('should emit session:ended event', async () => {
      const listener = vi.fn();
      journal.on('session:ended', listener);

      journal.addEntry(createMockEntry('user', 'Hello'));
      await journal.endSession();

      expect(listener).toHaveBeenCalled();
    });

    it('should clear current session after ending', async () => {
      journal.addEntry(createMockEntry('user', 'Hello'));
      await journal.endSession();

      expect(journal.getCurrentSession()).toHaveLength(0);
    });

    it('should use custom summary generator', async () => {
      const customSummary = createMockSummary('session-1');
      const customGenerator = vi.fn().mockResolvedValue(customSummary);

      const customJournal = new SessionJournal(
        { workspaceDir: '/test/workspace' },
        customGenerator
      );

      customJournal.startSession('session-1', 'user-1');
      customJournal.addEntry(createMockEntry('user', 'Hello'));

      const summary = await customJournal.endSession();

      expect(customGenerator).toHaveBeenCalled();
      expect(summary?.title).toBe('Test Session');
    });
  });

  describe('default summary generation', () => {
    beforeEach(() => {
      journal.startSession('session-1', 'user-1');
    });

    it('should extract title from first user message', async () => {
      journal.addEntry(createMockEntry('user', 'Help me with React components'));
      journal.addEntry(createMockEntry('assistant', 'Sure!'));

      const summary = await journal.endSession();

      expect(summary?.title).toContain('Help me with React');
    });

    it('should extract tags from content', async () => {
      journal.addEntry(createMockEntry('user', 'I need help with TypeScript and React'));
      journal.addEntry(createMockEntry('assistant', 'Let me help with that.'));

      const summary = await journal.endSession();

      expect(summary?.tags).toContain('typescript');
      expect(summary?.tags).toContain('react');
    });

    it('should extract code changes from assistant messages', async () => {
      journal.addEntry(createMockEntry('user', 'Update the index file'));
      journal.addEntry(createMockEntry('assistant', 'I updated `src/index.ts` with the new exports.'));

      const summary = await journal.endSession();

      expect(summary?.codeChanges.some(c => c.file === 'src/index.ts')).toBe(true);
    });
  });

  describe('restoreContext', () => {
    it('should restore context for user', async () => {
      const result = await journal.restoreContext('user-1');

      expect(result).toBeDefined();
      expect(result.summaries).toBeDefined();
      expect(result.suggestions).toBeDefined();
    });

    it('should restore context with query', async () => {
      const result = await journal.restoreContext('user-1', 'react');

      expect(result).toBeDefined();
    });
  });

  describe('getRecentJournals', () => {
    it('should get recent journals', async () => {
      const recent = await journal.getRecentJournals('user-1', 5);
      expect(recent).toBeDefined();
    });
  });

  describe('searchJournals', () => {
    it('should search journals', async () => {
      const results = await journal.searchJournals('user-1', 'test');
      expect(results).toBeDefined();
    });
  });

  describe('configuration', () => {
    it('should update config', () => {
      journal.updateConfig({ maxJournals: 50 });
      const config = journal.getConfig();

      expect(config.maxJournals).toBe(50);
    });

    it('should emit config:updated event', () => {
      const listener = vi.fn();
      journal.on('config:updated', listener);

      journal.updateConfig({ maxJournals: 50 });

      expect(listener).toHaveBeenCalled();
    });

    it('should get config', () => {
      const config = journal.getConfig();

      expect(config.workspaceDir).toBeDefined();
      expect(config.maxJournals).toBeDefined();
      expect(config.maxContextLength).toBeDefined();
    });
  });
});
