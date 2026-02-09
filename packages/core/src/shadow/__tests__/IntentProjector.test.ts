import { afterEach, describe, expect, it, vi } from 'vitest';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

import { IntentProjector } from '../IntentProjector.js';

describe('IntentProjector', () => {
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

  it('应支持 TypeScript AST 提取 publicMethods/imports/classDefinitions', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'intent-projector-ts-'));
    tempDirs.push(projectRoot);

    const filePath = path.join(projectRoot, 'sample.ts');
    fs.writeFileSync(
      filePath,
      `
import { readFileSync } from 'fs';

export class UserService extends BaseService implements Auditable, Disposable {
  run(userId: string): Promise<void> {
    return Promise.resolve();
  }
}

export async function loadUser(id: string): Promise<string> {
  return String(id);
}
`,
      'utf-8'
    );

    const projector = new IntentProjector();
    const result = await projector.project(filePath);

    expect(result.language).toBe('typescript');
    expect(result.imports.some((item) => item.includes("import { readFileSync } from 'fs'"))).toBe(true);
    expect(result.classDefinitions.some((item) => item.name === 'UserService')).toBe(true);
    expect(result.publicMethods.some((item) => item.name === 'loadUser')).toBe(true);
  });

  it('应支持 Go 语言方法与结构体提取', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'intent-projector-go-'));
    tempDirs.push(projectRoot);

    const filePath = path.join(projectRoot, 'sample.go');
    fs.writeFileSync(
      filePath,
      `
package sample

import "fmt"

type Person struct {
  Name string
}

func (p *Person) Say(name string) string {
  fmt.Println(name)
  return name
}
`,
      'utf-8'
    );

    const projector = new IntentProjector();
    const result = await projector.project(filePath);

    expect(result.language).toBe('go');
    expect(result.classDefinitions.some((item) => item.name === 'Person')).toBe(true);
    expect(result.publicMethods.some((item) => item.name === 'Say')).toBe(true);
    expect(result.imports.some((item) => item.includes('import "fmt"'))).toBe(true);
  });

  it('应支持 Python 方法与类提取', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'intent-projector-py-'));
    tempDirs.push(projectRoot);

    const filePath = path.join(projectRoot, 'sample.py');
    fs.writeFileSync(
      filePath,
      `
import os

class Greeter(BaseGreeter):
    def greet(self, name: str) -> str:
        return f"hello {name}"
`,
      'utf-8'
    );

    const projector = new IntentProjector();
    const result = await projector.project(filePath);

    expect(result.language).toBe('python');
    expect(result.classDefinitions.some((item) => item.name === 'Greeter')).toBe(true);
    expect(result.classDefinitions.find((item) => item.name === 'Greeter')?.extends).toContain('BaseGreeter');
    expect(result.publicMethods.some((item) => item.name === 'greet')).toBe(true);
    expect(result.imports.some((item) => item.includes('import os'))).toBe(true);
  });

  it('应在不支持文件类型时给出友好错误', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'intent-projector-unsupported-'));
    tempDirs.push(projectRoot);

    const filePath = path.join(projectRoot, 'sample.md');
    fs.writeFileSync(filePath, '# hello', 'utf-8');

    const projector = new IntentProjector();

    await expect(projector.project(filePath)).rejects.toThrow('不支持的文件类型');
  });

  it('应支持 JavaScript 基础解析', async () => {
    const projectRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'intent-projector-js-'));
    tempDirs.push(projectRoot);

    const filePath = path.join(projectRoot, 'sample.js');
    fs.writeFileSync(
      filePath,
      `
import x from 'x';

function run(task) {
  return task;
}
`,
      'utf-8'
    );

    const projector = new IntentProjector();
    const result = await projector.project(filePath);

    expect(result.language).toBe('javascript');
    expect(result.publicMethods.some((item) => item.name === 'run')).toBe(true);
    expect(result.imports.some((item) => item.includes("import x from 'x'"))).toBe(true);
  });

  it('projectFromStructured 应通过 LLM 生成 Markdown', async () => {
    const send = vi.fn().mockResolvedValue({
      content: '# Intent\n\n- 核心流程：读取用户画像并生成提示',
      model: 'mock-model',
    });

    const projector = new IntentProjector(undefined, { send });
    const markdown = await projector.projectFromStructured({
      language: 'typescript',
      imports: ['import { x } from "y"'],
      publicMethods: [
        {
          name: 'run',
          params: ['input'],
          returnType: 'Promise<void>',
          docstring: '',
        },
      ],
      classDefinitions: [
        {
          name: 'Runner',
          extends: 'BaseRunner',
          implements: ['Disposable'],
        },
      ],
    });

    expect(send).toHaveBeenCalledTimes(1);
    expect(markdown).toContain('# Intent');
  });

  it('未配置 llmAdapter 时 projectFromStructured 应报错', async () => {
    const projector = new IntentProjector();

    await expect(
      projector.projectFromStructured({
        language: 'typescript',
        imports: [],
        publicMethods: [],
        classDefinitions: [],
      })
    ).rejects.toThrow('未配置 llmAdapter');
  });
});
