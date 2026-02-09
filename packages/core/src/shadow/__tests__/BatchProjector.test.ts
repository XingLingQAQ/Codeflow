import { afterEach, describe, expect, it, vi } from 'vitest';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

import { BatchProjector } from '../BatchProjector.js';

describe('BatchProjector', () => {
  const tempDirs: string[] = [];

  afterEach(() => {
    for (const dir of tempDirs) {
      try {
        fs.rmSync(dir, { recursive: true, force: true });
      } catch {
        // ignore cleanup error
      }
    }
    tempDirs.length = 0;
  });

  it('projectDirectory 应递归过滤 .ts/.js/.go/.py 并输出到 .codeflow/domain', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'batch-projector-'));
    tempDirs.push(projectRoot);

    fs.mkdirSync(path.join(projectRoot, 'src', 'nested'), { recursive: true });
    fs.writeFileSync(path.join(projectRoot, 'src', 'a.ts'), 'export function a() {}', 'utf-8');
    fs.writeFileSync(path.join(projectRoot, 'src', 'nested', 'b.py'), 'def b():\n    return 1', 'utf-8');
    fs.writeFileSync(path.join(projectRoot, 'src', 'nested', 'skip.txt'), 'skip', 'utf-8');

    const projector = {
      projectToIntentMarkdown: vi.fn().mockImplementation(async (filePath: string) => `# Intent\n\n${path.basename(filePath)}`),
    };

    const logs: string[] = [];
    const batch = new BatchProjector(projector as never, (message) => logs.push(message));
    const result = await batch.projectDirectory(path.join(projectRoot, 'src'), projectRoot);

    expect(result.total).toBe(2);
    expect(result.failed).toBe(0);
    expect(result.succeeded).toBe(2);

    const aIntent = path.join(projectRoot, '.codeflow', 'domain', 'src', 'a.intent.md');
    const bIntent = path.join(projectRoot, '.codeflow', 'domain', 'src', 'nested', 'b.intent.md');

    expect(fs.existsSync(aIntent)).toBe(true);
    expect(fs.existsSync(bIntent)).toBe(true);
    expect(fs.existsSync(path.join(projectRoot, '.codeflow', 'domain', 'src', 'nested', 'skip.intent.md'))).toBe(false);

    expect(logs.some((line) => line.includes('[projected]'))).toBe(true);
  });

  it('projectDirectory 遇到单文件失败时不应中断', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'batch-projector-fail-'));
    tempDirs.push(projectRoot);

    fs.mkdirSync(path.join(projectRoot, 'src'), { recursive: true });
    const okFile = path.join(projectRoot, 'src', 'ok.ts');
    const badFile = path.join(projectRoot, 'src', 'bad.ts');

    fs.writeFileSync(okFile, 'export function ok() {}', 'utf-8');
    fs.writeFileSync(badFile, 'export function bad() {}', 'utf-8');

    const projector = {
      projectToIntentMarkdown: vi.fn().mockImplementation(async (filePath: string) => {
        if (filePath.endsWith('bad.ts')) {
          throw new Error('mock projection failed');
        }
        return '# Intent\n\nok';
      }),
    };

    const logs: string[] = [];
    const batch = new BatchProjector(projector as never, (message) => logs.push(message));
    const result = await batch.projectDirectory(path.join(projectRoot, 'src'), projectRoot);

    expect(result.total).toBe(2);
    expect(result.succeeded).toBe(1);
    expect(result.failed).toBe(1);

    const failedItem = result.items.find((item) => item.sourceFile.endsWith('bad.ts'));
    expect(failedItem?.success).toBe(false);
    expect(failedItem?.error).toContain('mock projection failed');

    const okIntent = path.join(projectRoot, '.codeflow', 'domain', 'src', 'ok.intent.md');
    expect(fs.existsSync(okIntent)).toBe(true);
    expect(logs.some((line) => line.includes('[failed]'))).toBe(true);
  });

  it('projectFile 应生成正确影子路径并写入内容', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'batch-projector-single-'));
    tempDirs.push(projectRoot);

    fs.mkdirSync(path.join(projectRoot, 'pkg'), { recursive: true });
    const source = path.join(projectRoot, 'pkg', 'worker.go');
    fs.writeFileSync(source, 'package pkg', 'utf-8');

    const projector = {
      projectToIntentMarkdown: vi.fn().mockResolvedValue('# Intent\n\nworker'),
    };

    const batch = new BatchProjector(projector as never);
    const output = await batch.projectFile(source, projectRoot);

    expect(output.endsWith(path.join('.codeflow', 'domain', 'pkg', 'worker.intent.md'))).toBe(true);
    expect(fs.existsSync(output)).toBe(true);

    const content = fs.readFileSync(output, 'utf-8');
    expect(content).toContain('# Intent');
  });
});
