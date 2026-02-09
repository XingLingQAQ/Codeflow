import * as fs from 'fs';
import * as path from 'path';

import { IntentProjector } from './IntentProjector.js';

export interface BatchProjectionItem {
  sourceFile: string;
  outputFile: string;
  success: boolean;
  error?: string;
}

export interface BatchProjectionResult {
  total: number;
  succeeded: number;
  failed: number;
  items: BatchProjectionItem[];
}

type ProgressReporter = (message: string) => void;

const SUPPORTED_EXTENSIONS = new Set(['.ts', '.js', '.go', '.py']);

export class BatchProjector {
  private readonly projector: IntentProjector;
  private readonly reportProgress: ProgressReporter;

  constructor(projector: IntentProjector, reportProgress: ProgressReporter = (message) => console.log(message)) {
    this.projector = projector;
    this.reportProgress = reportProgress;
  }

  async projectDirectory(dir: string, projectRoot: string): Promise<BatchProjectionResult> {
    const normalizedRoot = path.resolve(projectRoot);
    const targetDir = path.resolve(dir);
    const files = await this.collectSourceFiles(targetDir);

    const items: BatchProjectionItem[] = [];

    for (const filePath of files) {
      try {
        const outputFile = await this.projectFile(filePath, normalizedRoot);
        items.push({
          sourceFile: filePath,
          outputFile,
          success: true,
        });
        this.reportProgress(`[projected] ${path.relative(normalizedRoot, filePath)}`);
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        const outputFile = this.resolveOutputPath(filePath, normalizedRoot);
        items.push({
          sourceFile: filePath,
          outputFile,
          success: false,
          error: message,
        });
        this.reportProgress(`[failed] ${path.relative(normalizedRoot, filePath)}: ${message}`);
      }
    }

    const succeeded = items.filter((item) => item.success).length;
    return {
      total: items.length,
      succeeded,
      failed: items.length - succeeded,
      items,
    };
  }

  async projectFile(filePath: string, projectRoot: string): Promise<string> {
    const normalizedRoot = path.resolve(projectRoot);
    const normalizedFile = path.resolve(filePath);

    const intentMarkdown = await this.projector.projectToIntentMarkdown(normalizedFile);
    const outputFile = this.resolveOutputPath(normalizedFile, normalizedRoot);

    await fs.promises.mkdir(path.dirname(outputFile), { recursive: true });
    await fs.promises.writeFile(outputFile, `${intentMarkdown.trim()}\n`, 'utf-8');

    return outputFile;
  }

  private resolveOutputPath(sourceFile: string, projectRoot: string): string {
    const relativePath = path.relative(projectRoot, sourceFile);
    const ext = path.extname(relativePath);
    const withoutExt = relativePath.slice(0, relativePath.length - ext.length);

    return path.join(projectRoot, '.codeflow', 'domain', `${withoutExt}.intent.md`);
  }

  private async collectSourceFiles(targetDir: string): Promise<string[]> {
    const entries = await fs.promises.readdir(targetDir, { withFileTypes: true });
    const files: string[] = [];

    for (const entry of entries) {
      if (entry.name.startsWith('.')) {
        continue;
      }

      const fullPath = path.join(targetDir, entry.name);
      if (entry.isDirectory()) {
        const nested = await this.collectSourceFiles(fullPath);
        files.push(...nested);
        continue;
      }

      if (!entry.isFile()) {
        continue;
      }

      const ext = path.extname(entry.name).toLowerCase();
      if (SUPPORTED_EXTENSIONS.has(ext)) {
        files.push(fullPath);
      }
    }

    files.sort();
    return files;
  }
}
