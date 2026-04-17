/**
 * Codex Code Editor
 * 同时支持 Codex API adapter 与 cowork Codex CLI adapter
 */

import { readFile, writeFile, copyFile, unlink, mkdir } from 'fs/promises';
import { existsSync } from 'fs';
import { join, dirname } from 'path';
import { ICodeEditor, EditResult, Diff, DiffHunk } from '../types.js';
import { CodexAdapter } from '../../adapters/CodexAdapter.js';
import { CodexCLIAdapter } from '../adapters/CodexCLIAdapter.js';

export type CodexEditorAdapter = CodexAdapter | CodexCLIAdapter;

interface CodexPromptOptions {
  model?: string;
  maxTokens?: number;
  temperature?: number;
}

interface CodexPromptExecutor {
  executePrompt(prompt: string, options: CodexPromptOptions): Promise<string>;
}

/**
 * Codex Editor 配置
 */
export interface CodexEditorConfig {
  cwd?: string;
  backupDir?: string;
  autoBackup?: boolean;
  model?: string;
  maxTokens?: number;
  temperature?: number;
}

/**
 * 备份记录
 */
interface BackupRecord {
  file: string;
  backupPath: string;
  timestamp: number;
}

/**
 * Prompt 模板（与 Claude/Gemini Editor 一致）
 */
const EDIT_PROMPT_TEMPLATE = `You are a code editing assistant. Your task is to modify the given code according to the instruction.

## Rules:
1. Output ONLY the unified diff format
2. Use standard diff format with --- and +++ headers
3. Include line numbers in @@ markers
4. Do not include any explanation, just the diff

## Input File: {file}

## Current Content:
\`\`\`
{content}
\`\`\`

## Instruction:
{instruction}

## Output (unified diff only):`;

const PREVIEW_PROMPT_TEMPLATE = `You are a code editing assistant. Preview the changes that would be made to the given code.

## Rules:
1. Output ONLY the unified diff format
2. Use standard diff format with --- and +++ headers
3. Include line numbers in @@ markers
4. Do not actually apply changes, just show what would change

## Input File: {file}

## Current Content:
\`\`\`
{content}
\`\`\`

## Instruction:
{instruction}

## Output (unified diff only):`;

class CodexApiPromptExecutor implements CodexPromptExecutor {
  constructor(private readonly adapter: CodexAdapter) {}

  async executePrompt(prompt: string, options: CodexPromptOptions): Promise<string> {
    const response = await this.adapter.send(prompt, {
      model: options.model,
      maxTokens: options.maxTokens,
      temperature: options.temperature,
    });
    return response.content;
  }
}

class CodexCliPromptExecutor implements CodexPromptExecutor {
  constructor(private readonly adapter: CodexCLIAdapter) {}

  async executePrompt(prompt: string, options: CodexPromptOptions): Promise<string> {
    const originalConfig = this.adapter.getConfig();
    this.adapter.configure({
      model: options.model || originalConfig.model,
    });

    try {
      const result = await this.adapter.execute(prompt, {
        cwd: originalConfig.cwd,
      });

      if (result.exitCode !== 0) {
        throw new Error(result.stderr || `Codex CLI exited with code ${result.exitCode}`);
      }

      return result.stdout;
    } finally {
      this.adapter.configure({
        model: originalConfig.model,
      });
    }
  }
}

/**
 * Codex Code Editor
 */
export class CodexCodeEditor implements ICodeEditor {
  readonly name = 'codex-editor';

  private adapter: CodexEditorAdapter;
  private executor: CodexPromptExecutor;
  private config: CodexEditorConfig;
  private backupStack: BackupRecord[] = [];

  constructor(adapter: CodexEditorAdapter, config: CodexEditorConfig = {}) {
    this.adapter = adapter;
    this.executor = this.createPromptExecutor(adapter);
    this.config = {
      autoBackup: true,
      backupDir: '.codex-backups',
      model: 'gpt-4',
      maxTokens: 4096,
      temperature: 0.2,
      ...config,
    };
  }

  /**
   * 编辑单个文件
   */
  async edit(file: string, instruction: string): Promise<EditResult> {
    const fullPath = this.resolvePath(file);

    let content = '';
    if (existsSync(fullPath)) {
      content = await readFile(fullPath, 'utf-8');

      if (this.config.autoBackup) {
        await this.backup(fullPath);
      }
    }

    const prompt = EDIT_PROMPT_TEMPLATE.replace('{file}', file)
      .replace('{content}', content)
      .replace('{instruction}', instruction);

    try {
      const output = await this.executePrompt(prompt);
      const diff = this.parseDiff(output, file);

      if (diff.hunks.length === 0) {
        return {
          success: true,
          file,
          diff: this.emptyDiff(file),
          message: 'No changes made',
        };
      }

      await this.applyDiff(file, diff);

      return {
        success: true,
        file,
        diff,
      };
    } catch (error) {
      return {
        success: false,
        file,
        diff: this.emptyDiff(file),
        message: error instanceof Error ? error.message : 'Unknown error',
      };
    }
  }

  /**
   * 编辑多个文件
   */
  async editMultiple(files: string[], instruction: string): Promise<EditResult[]> {
    const results: EditResult[] = [];

    for (const file of files) {
      const result = await this.edit(file, instruction);
      results.push(result);
    }

    return results;
  }

  /**
   * 预览修改（不实际写入）
   */
  async preview(file: string, instruction: string): Promise<Diff> {
    const fullPath = this.resolvePath(file);

    let content = '';
    if (existsSync(fullPath)) {
      content = await readFile(fullPath, 'utf-8');
    }

    const prompt = PREVIEW_PROMPT_TEMPLATE.replace('{file}', file)
      .replace('{content}', content)
      .replace('{instruction}', instruction);

    try {
      const output = await this.executePrompt(prompt);
      return this.parseDiff(output, file);
    } catch {
      return this.emptyDiff(file);
    }
  }

  /**
   * 应用 Diff
   */
  async applyDiff(file: string, diff: Diff): Promise<void> {
    const fullPath = this.resolvePath(file);

    if (this.config.autoBackup && existsSync(fullPath)) {
      await this.backup(fullPath);
    }

    const content = existsSync(fullPath) ? await readFile(fullPath, 'utf-8') : '';
    const lines = content.split('\n');
    const sortedHunks = [...diff.hunks].sort((a, b) => b.oldStart - a.oldStart);

    for (const hunk of sortedHunks) {
      const hunkLines = hunk.content.split('\n').filter((l) => l.length > 0);
      const newLines: string[] = [];

      for (const line of hunkLines) {
        if (line.startsWith('+')) {
          newLines.push(line.slice(1));
        } else if (line.startsWith('-')) {
          continue;
        } else if (line.startsWith(' ')) {
          newLines.push(line.slice(1));
        } else {
          newLines.push(line);
        }
      }

      lines.splice(hunk.oldStart - 1, hunk.oldLines, ...newLines);
    }

    await mkdir(dirname(fullPath), { recursive: true });
    await writeFile(fullPath, lines.join('\n'), 'utf-8');
  }

  /**
   * 撤销上一次修改
   */
  async undo(): Promise<void> {
    const lastBackup = this.backupStack.pop();
    if (!lastBackup) {
      throw new Error('No backup available to undo');
    }

    await copyFile(lastBackup.backupPath, lastBackup.file);
    await unlink(lastBackup.backupPath);
  }

  /**
   * 获取备份栈
   */
  getBackupStack(): BackupRecord[] {
    return [...this.backupStack];
  }

  /**
   * 清理所有备份
   */
  async clearBackups(): Promise<void> {
    for (const backup of this.backupStack) {
      try {
        await unlink(backup.backupPath);
      } catch {
        // 忽略删除失败
      }
    }
    this.backupStack = [];
  }

  getAdapter(): CodexEditorAdapter {
    return this.adapter;
  }

  getConfig(): CodexEditorConfig {
    return { ...this.config };
  }

  // ==================== 私有方法 ====================

  private createPromptExecutor(adapter: CodexEditorAdapter): CodexPromptExecutor {
    if (adapter instanceof CodexCLIAdapter) {
      return new CodexCliPromptExecutor(adapter);
    }

    return new CodexApiPromptExecutor(adapter);
  }

  private async executePrompt(prompt: string): Promise<string> {
    return this.executor.executePrompt(prompt, {
      model: this.config.model,
      maxTokens: this.config.maxTokens,
      temperature: this.config.temperature,
    });
  }

  private resolvePath(file: string): string {
    if (this.config.cwd) {
      return join(this.config.cwd, file);
    }
    return file;
  }

  private async backup(file: string): Promise<void> {
    const timestamp = Date.now();
    const backupDir = this.config.backupDir || '.codex-backups';
    const backupPath = join(
      dirname(file),
      backupDir,
      `${timestamp}_${file.replace(/[/\\]/g, '_')}`,
    );

    await mkdir(dirname(backupPath), { recursive: true });
    await copyFile(file, backupPath);

    this.backupStack.push({
      file,
      backupPath,
      timestamp,
    });
  }

  private emptyDiff(file: string): Diff {
    return {
      file,
      hunks: [],
      additions: 0,
      deletions: 0,
    };
  }

  /**
   * 解析 unified diff 格式
   */
  private parseDiff(output: string, file: string): Diff {
    const hunks: DiffHunk[] = [];
    let additions = 0;
    let deletions = 0;

    const lines = output.split('\n');
    let currentHunk: DiffHunk | null = null;
    let hunkContent: string[] = [];

    for (const line of lines) {
      if (line.match(/@@ -(\d+),?(\d*) \+(\d+),?(\d*) @@/)) {
        if (currentHunk) {
          currentHunk.content = hunkContent.join('\n');
          hunks.push(currentHunk);
        }

        const match = line.match(/@@ -(\d+),?(\d*) \+(\d+),?(\d*) @@/);
        if (match) {
          currentHunk = {
            oldStart: parseInt(match[1], 10),
            oldLines: match[2] ? parseInt(match[2], 10) : 1,
            newStart: parseInt(match[3], 10),
            newLines: match[4] ? parseInt(match[4], 10) : 1,
            content: '',
          };
          hunkContent = [];
        }
      } else if (currentHunk) {
        if (line.startsWith('+') && !line.startsWith('+++')) {
          additions++;
          hunkContent.push(line);
        } else if (line.startsWith('-') && !line.startsWith('---')) {
          deletions++;
          hunkContent.push(line);
        } else if (line.startsWith(' ') || line === '') {
          hunkContent.push(line);
        }
      }
    }

    if (currentHunk) {
      currentHunk.content = hunkContent.join('\n');
      hunks.push(currentHunk);
    }

    return {
      file,
      hunks,
      additions,
      deletions,
    };
  }
}
