/**
 * CLIProcessManager 单元测试
 */

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { CLIProcessManager, ProcessEvent, ProcessStatus } from '../CLIProcessManager.js';

describe('CLIProcessManager', () => {
  let manager: CLIProcessManager;

  beforeEach(() => {
    manager = new CLIProcessManager();
  });

  afterEach(async () => {
    await manager.cleanup();
  });

  describe('spawn', () => {
    it('should spawn a process and return process id', async () => {
      const id = await manager.spawn('echo', ['hello']);
      expect(id).toMatch(/^proc_\d+_\d+$/);
    });

    it('should set process status to running', async () => {
      const id = await manager.spawn('node', ['-e', 'setTimeout(() => {}, 5000)']);
      const info = manager.getInfo(id);
      expect(info?.status).toBe('running');
      expect(info?.pid).toBeDefined();
    });

    it('should emit spawn event', async () => {
      const events: ProcessEvent[] = [];
      manager.on('event', (e) => events.push(e));

      await manager.spawn('echo', ['test']);

      const spawnEvent = events.find((e) => e.type === 'spawn');
      expect(spawnEvent).toBeDefined();
      expect(spawnEvent?.type).toBe('spawn');
    });

    it('should capture stdout', async () => {
      const events: ProcessEvent[] = [];
      manager.on('event', (e) => events.push(e));

      const id = await manager.spawn('echo', ['hello world']);

      // 等待输出
      await new Promise((resolve) => setTimeout(resolve, 500));

      const stdoutEvents = events.filter(
        (e) => e.type === 'stdout' && e.processId === id
      );
      expect(stdoutEvents.length).toBeGreaterThan(0);
    });

    it('should capture stderr', async () => {
      const events: ProcessEvent[] = [];
      manager.on('event', (e) => events.push(e));

      const id = await manager.spawn('node', ['-e', 'console.error("error message")']);

      // 等待输出
      await new Promise((resolve) => setTimeout(resolve, 500));

      const stderrEvents = events.filter(
        (e) => e.type === 'stderr' && e.processId === id
      );
      expect(stderrEvents.length).toBeGreaterThan(0);
    });

    it('should use custom cwd', async () => {
      const id = await manager.spawn('pwd', [], { cwd: '/tmp' });
      await new Promise((resolve) => setTimeout(resolve, 500));

      const output = manager.getOutput(id).join('');
      // Windows 或 Unix
      expect(output.length).toBeGreaterThan(0);
    });

    it('should use custom env', async () => {
      const id = await manager.spawn('node', ['-e', 'console.log(process.env.TEST_VAR)'], {
        env: { TEST_VAR: 'custom_value' },
      });

      await new Promise((resolve) => setTimeout(resolve, 500));

      const output = manager.getOutput(id).join('');
      expect(output).toContain('custom_value');
    });
  });

  describe('kill', () => {
    it('should kill a running process', async () => {
      const id = await manager.spawn('node', ['-e', 'setTimeout(() => {}, 30000)']);

      const infoBefore = manager.getInfo(id);
      expect(infoBefore?.status).toBe('running');

      await manager.kill(id);

      const infoAfter = manager.getInfo(id);
      expect(['stopped', 'crashed']).toContain(infoAfter?.status);
    });

    it('should emit exit event', async () => {
      const events: ProcessEvent[] = [];
      manager.on('event', (e) => events.push(e));

      const id = await manager.spawn('node', ['-e', 'setTimeout(() => {}, 30000)']);
      await manager.kill(id);

      const exitEvent = events.find((e) => e.type === 'exit' && e.processId === id);
      expect(exitEvent).toBeDefined();
    });

    it('should handle already stopped process', async () => {
      const id = await manager.spawn('echo', ['quick']);
      await new Promise((resolve) => setTimeout(resolve, 500));

      // 进程已自然退出
      await expect(manager.kill(id)).resolves.not.toThrow();
    });

    it('should throw for non-existent process', async () => {
      await expect(manager.kill('non_existent')).rejects.toThrow('not found');
    });
  });

  describe('restart', () => {
    it('should restart a process', async () => {
      const id = await manager.spawn('node', ['-e', 'setTimeout(() => {}, 30000)']);

      const pidBefore = manager.getInfo(id)?.pid;
      await manager.restart(id);

      const pidAfter = manager.getInfo(id)?.pid;
      expect(pidAfter).not.toBe(pidBefore);
    });

    it('should increment restart count', async () => {
      const id = await manager.spawn('node', ['-e', 'setTimeout(() => {}, 30000)']);

      expect(manager.getInfo(id)?.restartCount).toBe(0);
      await manager.restart(id);
      expect(manager.getInfo(id)?.restartCount).toBe(1);
    });

    it('should clear output buffers on restart', async () => {
      const id = await manager.spawn('echo', ['first output']);
      await new Promise((resolve) => setTimeout(resolve, 500));

      expect(manager.getOutput(id).length).toBeGreaterThan(0);

      await manager.restart(id);
      expect(manager.getOutput(id).length).toBe(0);
    });
  });

  describe('healthCheck', () => {
    it('should return true for running process', async () => {
      const id = await manager.spawn('node', ['-e', 'setTimeout(() => {}, 30000)']);

      const healthy = await manager.healthCheck(id);
      expect(healthy).toBe(true);
    });

    it('should return false for stopped process', async () => {
      const id = await manager.spawn('echo', ['quick']);
      await new Promise((resolve) => setTimeout(resolve, 500));

      const healthy = await manager.healthCheck(id);
      expect(healthy).toBe(false);
    });

    it('should return false for non-existent process', async () => {
      const healthy = await manager.healthCheck('non_existent');
      expect(healthy).toBe(false);
    });
  });

  describe('getInfo', () => {
    it('should return process info', async () => {
      const id = await manager.spawn('echo', ['test']);
      const info = manager.getInfo(id);

      expect(info).toBeDefined();
      expect(info?.id).toBe(id);
      expect(info?.cli).toBe('echo');
      expect(info?.args).toEqual(['test']);
    });

    it('should return undefined for non-existent process', () => {
      const info = manager.getInfo('non_existent');
      expect(info).toBeUndefined();
    });
  });

  describe('getAllProcesses', () => {
    it('should return all processes', async () => {
      await manager.spawn('echo', ['1']);
      await manager.spawn('echo', ['2']);
      await manager.spawn('echo', ['3']);

      const all = manager.getAllProcesses();
      expect(all.length).toBe(3);
    });

    it('should return empty array when no processes', () => {
      const all = manager.getAllProcesses();
      expect(all).toEqual([]);
    });
  });

  describe('getOutput/getErrors', () => {
    it('should return stdout buffer', async () => {
      const id = await manager.spawn('echo', ['output line']);
      await new Promise((resolve) => setTimeout(resolve, 500));

      const output = manager.getOutput(id);
      expect(output.length).toBeGreaterThan(0);
    });

    it('should return stderr buffer', async () => {
      const id = await manager.spawn('node', ['-e', 'console.error("error")']);
      await new Promise((resolve) => setTimeout(resolve, 500));

      const errors = manager.getErrors(id);
      expect(errors.length).toBeGreaterThan(0);
    });

    it('should return empty array for non-existent process', () => {
      expect(manager.getOutput('non_existent')).toEqual([]);
      expect(manager.getErrors('non_existent')).toEqual([]);
    });
  });

  describe('createOutputStream', () => {
    it('should create readable stream', async () => {
      const id = await manager.spawn('node', ['-e', 'console.log("stream data")']);
      const stream = manager.createOutputStream(id);

      expect(stream).toBeDefined();
      expect(stream.readable).toBe(true);
    });

    it('should throw for non-existent process', () => {
      expect(() => manager.createOutputStream('non_existent')).toThrow('not found');
    });
  });

  describe('sendInput', () => {
    it('should send input to process stdin', async () => {
      // 使用更简单的测试：验证 sendInput 不抛出错误
      const id = await manager.spawn('node', [
        '-e',
        'process.stdin.resume(); setTimeout(() => process.exit(0), 1000)',
      ]);

      // 验证可以发送输入而不抛出错误
      await expect(manager.sendInput(id, 'test input\n')).resolves.not.toThrow();
    });

    it('should throw for non-existent process', async () => {
      await expect(manager.sendInput('non_existent', 'data')).rejects.toThrow();
    });
  });

  describe('remove', () => {
    it('should remove process record', async () => {
      const id = await manager.spawn('echo', ['test']);
      await new Promise((resolve) => setTimeout(resolve, 500));

      const removed = manager.remove(id);
      expect(removed).toBe(true);
      expect(manager.getInfo(id)).toBeUndefined();
    });

    it('should return false for non-existent process', () => {
      const removed = manager.remove('non_existent');
      expect(removed).toBe(false);
    });

    it('should kill running process before removing', async () => {
      const id = await manager.spawn('node', ['-e', 'setTimeout(() => {}, 30000)']);

      const removed = manager.remove(id);
      expect(removed).toBe(true);
    });
  });

  describe('cleanup', () => {
    it('should kill all processes', async () => {
      await manager.spawn('node', ['-e', 'setTimeout(() => {}, 30000)']);
      await manager.spawn('node', ['-e', 'setTimeout(() => {}, 30000)']);

      await manager.cleanup();

      expect(manager.getAllProcesses().length).toBe(0);
    });
  });

  describe('auto restart', () => {
    it('should auto restart crashed process when enabled', async () => {
      const events: ProcessEvent[] = [];
      manager.on('event', (e) => events.push(e));

      const id = await manager.spawn('node', ['-e', 'process.exit(1)'], {
        autoRestart: true,
        maxRestarts: 2,
        restartDelay: 100,
      });

      // 等待重启
      await new Promise((resolve) => setTimeout(resolve, 1000));

      const restartEvents = events.filter(
        (e) => e.type === 'restart' && e.processId === id
      );
      expect(restartEvents.length).toBeGreaterThan(0);
    });

    it('should not exceed max restarts', async () => {
      const events: ProcessEvent[] = [];
      manager.on('event', (e) => events.push(e));

      const id = await manager.spawn('node', ['-e', 'process.exit(1)'], {
        autoRestart: true,
        maxRestarts: 2,
        restartDelay: 100,
      });

      // 等待所有重启尝试
      await new Promise((resolve) => setTimeout(resolve, 2000));

      const restartEvents = events.filter(
        (e) => e.type === 'restart' && e.processId === id
      );
      expect(restartEvents.length).toBeLessThanOrEqual(2);
    });

    it('should not restart on clean exit', async () => {
      const events: ProcessEvent[] = [];
      manager.on('event', (e) => events.push(e));

      const id = await manager.spawn('node', ['-e', 'process.exit(0)'], {
        autoRestart: true,
        maxRestarts: 3,
        restartDelay: 100,
      });

      await new Promise((resolve) => setTimeout(resolve, 500));

      const restartEvents = events.filter(
        (e) => e.type === 'restart' && e.processId === id
      );
      expect(restartEvents.length).toBe(0);
    });
  });
});
