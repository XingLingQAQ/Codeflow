import { afterEach, describe, expect, it } from 'vitest';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

import { ShadowScaffold } from '../ShadowScaffold.js';

describe('ShadowScaffold', () => {
  const tempDirs: string[] = [];

  afterEach(() => {
    for (const dir of tempDirs) {
      try {
        fs.rmSync(dir, { recursive: true, force: true });
      } catch {
        // ignore cleanup errors
      }
    }
    tempDirs.length = 0;
  });

  it('initialize 应创建目录结构和 config.json，并确保 .codeflow 被追踪', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'shadow-scaffold-'));
    tempDirs.push(projectRoot);

    fs.writeFileSync(path.join(projectRoot, '.gitignore'), 'node_modules\n.codeflow\ndist\n', 'utf-8');

    const scaffold = new ShadowScaffold();
    await scaffold.initialize(projectRoot);

    const shadowRoot = path.join(projectRoot, '.codeflow');
    expect(fs.existsSync(path.join(shadowRoot, 'domain'))).toBe(true);
    expect(fs.existsSync(path.join(shadowRoot, 'governance'))).toBe(true);
    expect(fs.existsSync(path.join(shadowRoot, 'registry'))).toBe(true);

    const configPath = path.join(shadowRoot, 'config.json');
    expect(fs.existsSync(configPath)).toBe(true);

    const config = JSON.parse(fs.readFileSync(configPath, 'utf-8')) as {
      version: string;
      projectRoot: string;
      autoSync: boolean;
      intentProjection: { enabled: boolean; languages: string[] };
      registry: { apiRegistry: boolean; modelDictionary: boolean };
    };

    expect(config.version).toBe('1.0.0');
    expect(config.autoSync).toBe(true);
    expect(config.intentProjection.enabled).toBe(true);
    expect(config.intentProjection.languages).toEqual(['typescript', 'javascript', 'go', 'python']);
    expect(config.registry.apiRegistry).toBe(true);
    expect(config.registry.modelDictionary).toBe(true);

    const gitignore = fs.readFileSync(path.join(projectRoot, '.gitignore'), 'utf-8');
    expect(gitignore.includes('\n.codeflow\n') || gitignore.startsWith('.codeflow\n')).toBe(false);
    expect(gitignore.includes('\n.codeflow/\n') || gitignore.startsWith('.codeflow/\n')).toBe(false);
    expect(gitignore).toContain('# CodeFlow shadow system (tracked)');
  });

  it('initialize 幂等：重复执行不报错且 marker 不重复', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'shadow-scaffold-idempotent-'));
    tempDirs.push(projectRoot);

    const scaffold = new ShadowScaffold();
    await scaffold.initialize(projectRoot);
    await scaffold.initialize(projectRoot);

    const gitignore = fs.readFileSync(path.join(projectRoot, '.gitignore'), 'utf-8');
    const markerCount = (gitignore.match(/# CodeFlow shadow system \(tracked\)/g) || []).length;
    expect(markerCount).toBe(1);
  });
});
