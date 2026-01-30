/**
 * Aider Code Editor
 * 基于 AiderAdapter 实现 ICodeEditor 接口
 */

import { readFile, writeFile, copyFile, unlink } from 'fs/promises';
import { existsSync } from 'fs';
import { join, dirname } from 'path';
import {
  ICodeEditor,
  EditResult,
  Diff,
  DiffHunk,
} from '../types.js';
import { AiderAdapter, ParsedDiff } from '../adapters/AiderAdapter.js';

/**
 * Aider Editor 配置
 */
export interface AiderEditorConfig {
  cwd?: string;
  backupDir?: string;
  autoBackup?: boolean;
  dryRun?: boolean;
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
 * Aider Code Editor
 */
export class AiderCodeEditor implements ICodeEditor {
  readonly name = 'aider-editor';

  private adapter: AiderAdapter;
  private config: AiderEditorConfig;
  private backupStack: BackupRecord[] = [];

  constructor(adapter: AiderAdapter, config: AiderEditorConfig = {}) {
    this.adapter = adapter;
    this.config = {
      autoBackup: true,
      dryRun: false,
      backupDir: '.aider-backups',
      ...config,
    };
  }

  /**
   * 编辑单个文件
   */
  async edit(file: string, instruction: string): Promise<EditResult> {
    const fullPath = this.resolvePath(file);

    // 备份原文件
    if (this.config.autoBackup && existsSync(fullPath)) {
      await this.backup(fullPath);
    }

    // 构建 Aider 命令
    const command = `${instruction}\n\nFile: ${file}`;

    try {
      const result = await this.adapter.execute(command, {
        cwd: this.config.cwd,
      });

      if (result.exitCode !== 0) {
        return {
          success: false,
          file,
          diff: this.emptyDiff(file),
          message: result.stderr || 'Aider execution failed',
        };
      }

      // 解析输出中的 diff
      const diffs = this.adapter.parseDiff(result.stdout);
      const fileDiff = diffs.find((d) => d.file === file || d.file.endsWith(file));

      if (!fileDiff) {
        return {
          success: true,
          file,
          diff: this.emptyDiff(file),
          message: 'No changes made',
        };
      }

      return {
        success: true,
        file,
        diff: this.convertDiff(fileDiff),
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
    // 备份所有文件
    if (this.config.autoBackup) {
      for (const file of files) {
        const fullPath = this.resolvePath(file);
        if (existsSync(fullPath)) {
          await this.backup(fullPath);
        }
      }
    }

    // 构建 Aider 命令
    const fileList = files.join(' ');
    const command = `${instruction}\n\nFiles: ${fileList}`;

    try {
      const result = await this.adapter.execute(command, {
        cwd: this.config.cwd,
      });

      if (result.exitCode !== 0) {
        return files.map((file) => ({
          success: false,
          file,
          diff: this.emptyDiff(file),
          message: result.stderr || 'Aider execution failed',
        }));
      }

      // 解析输出中的 diff
      const diffs = this.adapter.parseDiff(result.stdout);

      return files.map((file) => {
        const fileDiff = diffs.find((d) => d.file === file || d.file.endsWith(file));

        if (!fileDiff) {
          return {
            success: true,
            file,
            diff: this.emptyDiff(file),
            message: 'No changes made',
          };
        }

        return {
          success: true,
          file,
          diff: this.convertDiff(fileDiff),
        };
      });
    } catch (error) {
      return files.map((file) => ({
        success: false,
        file,
        diff: this.emptyDiff(file),
        message: error instanceof Error ? error.message : 'Unknown error',
      }));
    }
  }

  /**
   * 预览修改（不实际写入）
   */
  async preview(file: string, instruction: string): Promise<Diff> {
    // 使用 dry-run 模式
    const originalDryRun = this.config.dryRun;
    this.config.dryRun = true;

    // 构建预览命令
    const command = `${instruction}\n\nFile: ${file}\n\n[DRY RUN - Do not apply changes]`;

    try {
      const result = await this.adapter.execute(command, {
        cwd: this.config.cwd,
      });

      const diffs = this.adapter.parseDiff(result.stdout);
      const fileDiff = diffs.find((d) => d.file === file || d.file.endsWith(file));

      if (!fileDiff) {
        return this.emptyDiff(file);
      }

      return this.convertDiff(fileDiff);
    } finally {
      this.config.dryRun = originalDryRun;
    }
  }

  /**
   * 应用 Diff
   */
  async applyDiff(file: string, diff: Diff): Promise<void> {
    const fullPath = this.resolvePath(file);

    // 备份原文件
    if (this.config.autoBackup && existsSync(fullPath)) {
      await this.backup(fullPath);
    }

    // 读取原文件
    const content = existsSync(fullPath) ? await readFile(fullPath, 'utf-8') : '';
    const lines = content.split('\n');

    // 应用 hunks（从后往前应用，避免行号偏移）
    const sortedHunks = [...diff.hunks].sort((a, b) => b.newStart - a.newStart);

    for (const hunk of sortedHunks) {
      const hunkLines = hunk.content.split('\n').filter((l) => l.length > 0);
      const newLines: string[] = [];

      for (const line of hunkLines) {
        if (line.startsWith('+')) {
          newLines.push(line.slice(1));
        } else if (line.startsWith('-')) {
          // 删除行，不添加
        } else if (line.startsWith(' ')) {
          newLines.push(line.slice(1));
        } else {
          newLines.push(line);
        }
      }

      // 替换对应行
      lines.splice(hunk.oldStart - 1, hunk.oldLines, ...newLines);
    }

    // 写入文件
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

    // 恢复文件
    await copyFile(lastBackup.backupPath, lastBackup.file);

    // 删除备份
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

  // ==================== 私有方法 ====================

  private resolvePath(file: string): string {
    if (this.config.cwd) {
      return join(this.config.cwd, file);
    }
    return file;
  }

  private async backup(file: string): Promise<void> {
    const timestamp = Date.now();
    const backupDir = this.config.backupDir || '.aider-backups';
    const backupPath = join(
      dirname(file),
      backupDir,
      `${timestamp}_${file.replace(/[/\\]/g, '_')}`
    );

    // 确保备份目录存在
    const { mkdir } = await import('fs/promises');
    await mkdir(dirname(backupPath), { recursive: true });

    // 复制文件
    await copyFile(file, backupPath);

    // 记录备份
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

  private convertDiff(parsed: ParsedDiff): Diff {
    return {
      file: parsed.file,
      hunks: parsed.hunks.map((h) => ({
        oldStart: h.oldStart,
        oldLines: h.oldLines,
        newStart: h.newStart,
        newLines: h.newLines,
        content: h.content,
      })),
      additions: parsed.additions,
      deletions: parsed.deletions,
    };
  }
}
