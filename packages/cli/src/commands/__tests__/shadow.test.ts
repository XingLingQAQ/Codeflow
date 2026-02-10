import { describe, it, expect, vi, beforeEach } from 'vitest';
import { Command } from 'commander';
import { registerShadowCommand } from '../shadow.js';

// Mock @codeflow/core
vi.mock('@codeflow/core', () => {
  const mockInitialize = vi.fn().mockResolvedValue(undefined);
  const mockProjectDirectory = vi.fn().mockResolvedValue({
    total: 3,
    succeeded: 2,
    failed: 1,
    items: [],
  });
  const mockStart = vi.fn();
  const mockStop = vi.fn();

  return {
    ShadowScaffold: vi.fn().mockImplementation(() => ({
      initialize: mockInitialize,
    })),
    IntentProjector: vi.fn().mockImplementation(() => ({})),
    BatchProjector: vi.fn().mockImplementation(() => ({
      projectDirectory: mockProjectDirectory,
    })),
    DynamicSync: vi.fn().mockImplementation(() => ({
      start: mockStart,
      stop: mockStop,
    })),
    __mocks: {
      mockInitialize,
      mockProjectDirectory,
      mockStart,
      mockStop,
    },
  };
});

describe('ShadowCommand', () => {
  let program: Command;

  beforeEach(() => {
    vi.clearAllMocks();
    program = new Command();
    program.exitOverride(); // Prevent process.exit
    registerShadowCommand(program);
  });

  describe('registerShadowCommand', () => {
    it('should register shadow command with init/project/sync subcommands', () => {
      const shadow = program.commands.find((c) => c.name() === 'shadow');
      expect(shadow).toBeDefined();

      const subcommands = shadow!.commands.map((c) => c.name());
      expect(subcommands).toContain('init');
      expect(subcommands).toContain('project');
      expect(subcommands).toContain('sync');
    });

    it('should have correct descriptions', () => {
      const shadow = program.commands.find((c) => c.name() === 'shadow');
      expect(shadow!.description()).toContain('影子系统');

      const init = shadow!.commands.find((c) => c.name() === 'init');
      expect(init!.description()).toContain('初始化');

      const project = shadow!.commands.find((c) => c.name() === 'project');
      expect(project!.description()).toContain('投影');

      const sync = shadow!.commands.find((c) => c.name() === 'sync');
      expect(sync!.description()).toContain('监听');
    });
  });

  describe('shadow init', () => {
    it('should call ShadowScaffold.initialize with default path', async () => {
      const { ShadowScaffold } = await import('@codeflow/core');
      const consoleSpy = vi.spyOn(console, 'log').mockImplementation(() => {});

      await program.parseAsync(['node', 'codeflow', 'shadow', 'init']);

      expect(ShadowScaffold).toHaveBeenCalled();
      const instance = (ShadowScaffold as unknown as ReturnType<typeof vi.fn>).mock.results[0].value;
      expect(instance.initialize).toHaveBeenCalledWith(process.cwd());
      expect(consoleSpy).toHaveBeenCalledWith(expect.stringContaining('✅'));

      consoleSpy.mockRestore();
    });

    it('should call ShadowScaffold.initialize with custom path', async () => {
      const { ShadowScaffold } = await import('@codeflow/core');
      const consoleSpy = vi.spyOn(console, 'log').mockImplementation(() => {});

      await program.parseAsync(['node', 'codeflow', 'shadow', 'init', '/custom/path']);

      const instance = (ShadowScaffold as unknown as ReturnType<typeof vi.fn>).mock.results[0].value;
      expect(instance.initialize).toHaveBeenCalledWith('/custom/path');

      consoleSpy.mockRestore();
    });
  });

  describe('shadow project', () => {
    it('should call BatchProjector.projectDirectory with defaults', async () => {
      const { BatchProjector } = await import('@codeflow/core');
      const consoleSpy = vi.spyOn(console, 'log').mockImplementation(() => {});

      await program.parseAsync(['node', 'codeflow', 'shadow', 'project']);

      expect(BatchProjector).toHaveBeenCalled();
      const instance = (BatchProjector as unknown as ReturnType<typeof vi.fn>).mock.results[0].value;
      expect(instance.projectDirectory).toHaveBeenCalledWith('src', process.cwd());
      expect(consoleSpy).toHaveBeenCalledWith(expect.stringContaining('2 succeeded'));

      consoleSpy.mockRestore();
    });

    it('should call BatchProjector.projectDirectory with custom dir', async () => {
      const { BatchProjector } = await import('@codeflow/core');
      const consoleSpy = vi.spyOn(console, 'log').mockImplementation(() => {});

      await program.parseAsync(['node', 'codeflow', 'shadow', 'project', 'lib']);

      const instance = (BatchProjector as unknown as ReturnType<typeof vi.fn>).mock.results[0].value;
      expect(instance.projectDirectory).toHaveBeenCalledWith('lib', process.cwd());

      consoleSpy.mockRestore();
    });
  });

  describe('shadow sync', () => {
    it('should create DynamicSync and call start', async () => {
      const { DynamicSync } = await import('@codeflow/core');
      const consoleSpy = vi.spyOn(console, 'log').mockImplementation(() => {});

      // Mock process.on to prevent actual SIGINT handler
      const processOnSpy = vi.spyOn(process, 'on').mockImplementation(() => process);

      await program.parseAsync(['node', 'codeflow', 'shadow', 'sync']);

      expect(DynamicSync).toHaveBeenCalled();
      const instance = (DynamicSync as unknown as ReturnType<typeof vi.fn>).mock.results[0].value;
      expect(instance.start).toHaveBeenCalled();
      expect(consoleSpy).toHaveBeenCalledWith(expect.stringContaining('Watching'));

      consoleSpy.mockRestore();
      processOnSpy.mockRestore();
    });

    it('should pass custom debounce option', async () => {
      const { DynamicSync } = await import('@codeflow/core');
      const consoleSpy = vi.spyOn(console, 'log').mockImplementation(() => {});
      const processOnSpy = vi.spyOn(process, 'on').mockImplementation(() => process);

      await program.parseAsync(['node', 'codeflow', 'shadow', 'sync', '--debounce', '1000']);

      expect(DynamicSync).toHaveBeenCalledWith(
        expect.anything(),
        expect.objectContaining({ debounceMs: 1000 }),
        expect.any(Function)
      );

      consoleSpy.mockRestore();
      processOnSpy.mockRestore();
    });
  });

  describe('help text', () => {
    it('shadow --help should include all subcommands', () => {
      const shadow = program.commands.find((c) => c.name() === 'shadow');
      const helpText = shadow!.helpInformation();

      expect(helpText).toContain('init');
      expect(helpText).toContain('project');
      expect(helpText).toContain('sync');
    });
  });
});
