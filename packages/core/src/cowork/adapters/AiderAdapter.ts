/**
 * Aider CLI 适配器
 * 封装 Aider CLI 调用，提供统一的 ICLIAdapter 接口
 */

import { spawn, ChildProcess } from 'child_process';
import {
  ICLIAdapter,
  CLIResult,
  CLICapabilities,
  ExecuteOptions,
  DiffHunk,
  Diff,
} from '../types.js';

/**
 * Aider 配置
 */
export interface AiderConfig {
  aiderPath?: string;
  model?: string;
  editFormat?: 'diff' | 'whole' | 'diff-fenced';
  autoConfirm?: boolean;
  stream?: boolean;
  cwd?: string;
  env?: Record<string, string>;
}

/**
 * Aider 适配器
 */
export class AiderAdapter implements ICLIAdapter {
  readonly name = 'aider';
  readonly version = '0.1.0';

  private config: AiderConfig;
  private currentProcess: ChildProcess | null = null;

  constructor(config: AiderConfig = {}) {
    this.config = {
      aiderPath: 'aider',
      model: 'gpt-4',
      editFormat: 'diff',
      autoConfirm: true,
      stream: true,
      ...config,
    };
  }

  /**
   * 执行 Aider 命令
   */
  async execute(command: string, options?: ExecuteOptions): Promise<CLIResult> {
    const startTime = Date.now();
    const args = this.buildArgs(command, options);

    return new Promise((resolve, reject) => {
      const proc = spawn(this.config.aiderPath!, args, {
        cwd: options?.cwd || this.config.cwd,
        env: { ...process.env, ...this.config.env, ...options?.env },
        shell: true,
      });

      this.currentProcess = proc;

      let stdout = '';
      let stderr = '';

      proc.stdout?.on('data', (data: Buffer) => {
        stdout += data.toString();
      });

      proc.stderr?.on('data', (data: Buffer) => {
        stderr += data.toString();
      });

      proc.on('error', (error) => {
        this.currentProcess = null;
        reject(error);
      });

      proc.on('exit', (code) => {
        this.currentProcess = null;
        resolve({
          stdout,
          stderr,
          exitCode: code ?? -1,
          duration: Date.now() - startTime,
        });
      });

      // 超时处理
      if (options?.timeout) {
        setTimeout(() => {
          if (this.currentProcess) {
            this.currentProcess.kill('SIGTERM');
          }
        }, options.timeout);
      }

      // 中断信号处理
      if (options?.signal) {
        options.signal.addEventListener('abort', () => {
          if (this.currentProcess) {
            this.currentProcess.kill('SIGTERM');
          }
        });
      }
    });
  }

  /**
   * 流式执行 Aider 命令
   */
  async stream(
    command: string,
    onChunk: (data: string) => void,
    options?: ExecuteOptions
  ): Promise<void> {
    const args = this.buildArgs(command, options);

    return new Promise((resolve, reject) => {
      const proc = spawn(this.config.aiderPath!, args, {
        cwd: options?.cwd || this.config.cwd,
        env: { ...process.env, ...this.config.env, ...options?.env },
        shell: true,
      });

      this.currentProcess = proc;

      proc.stdout?.on('data', (data: Buffer) => {
        onChunk(data.toString());
      });

      proc.stderr?.on('data', (data: Buffer) => {
        onChunk(`[stderr] ${data.toString()}`);
      });

      proc.on('error', (error) => {
        this.currentProcess = null;
        reject(error);
      });

      proc.on('exit', (code) => {
        this.currentProcess = null;
        if (code === 0) {
          resolve();
        } else {
          reject(new Error(`Aider exited with code ${code}`));
        }
      });

      // 超时处理
      if (options?.timeout) {
        setTimeout(() => {
          if (this.currentProcess) {
            this.currentProcess.kill('SIGTERM');
            reject(new Error('Aider timeout'));
          }
        }, options.timeout);
      }
    });
  }

  /**
   * 中断当前执行
   */
  async interrupt(): Promise<void> {
    if (this.currentProcess) {
      this.currentProcess.kill('SIGINT');
      this.currentProcess = null;
    }
  }

  /**
   * 健康检查
   */
  async healthCheck(): Promise<boolean> {
    try {
      const result = await this.execute('--version', { timeout: 5000 });
      return result.exitCode === 0 && result.stdout.includes('aider');
    } catch {
      return false;
    }
  }

  /**
   * 获取能力描述
   */
  getCapabilities(): CLICapabilities {
    return {
      supportsStreaming: true,
      supportsInterrupt: true,
      supportedLanguages: [
        'typescript',
        'javascript',
        'python',
        'go',
        'rust',
        'java',
        'c',
        'cpp',
        'csharp',
        'ruby',
        'php',
      ],
      maxContextTokens: 128000, // GPT-4 Turbo
      features: [
        'code-edit',
        'multi-file',
        'git-integration',
        'auto-commit',
        'diff-output',
      ],
    };
  }

  /**
   * 解析 Aider 输出的 diff
   */
  parseDiff(output: string): ParsedDiff[] {
    const diffs: ParsedDiff[] = [];
    const diffRegex = /```diff\n([\s\S]*?)```/g;

    let match;
    while ((match = diffRegex.exec(output)) !== null) {
      const diffContent = match[1];
      const parsed = this.parseSingleDiff(diffContent);
      if (parsed) {
        diffs.push(parsed);
      }
    }

    return diffs;
  }

  /**
   * 解析单个 diff
   */
  private parseSingleDiff(diffContent: string): ParsedDiff | null {
    const lines = diffContent.split('\n');
    let file = '';
    let additions = 0;
    let deletions = 0;
    const hunks: DiffHunk[] = [];
    let currentHunk: DiffHunk | null = null;

    for (const line of lines) {
      // 文件头
      if (line.startsWith('--- ')) {
        file = line.slice(4).replace(/^a\//, '');
      } else if (line.startsWith('+++ ')) {
        file = line.slice(4).replace(/^b\//, '');
      }
      // Hunk 头
      else if (line.startsWith('@@')) {
        if (currentHunk) {
          hunks.push(currentHunk);
        }
        const hunkMatch = line.match(/@@ -(\d+),?(\d*) \+(\d+),?(\d*) @@/);
        if (hunkMatch) {
          currentHunk = {
            oldStart: parseInt(hunkMatch[1], 10),
            oldLines: parseInt(hunkMatch[2] || '1', 10),
            newStart: parseInt(hunkMatch[3], 10),
            newLines: parseInt(hunkMatch[4] || '1', 10),
            content: '',
          };
        }
      }
      // 内容行
      else if (currentHunk) {
        currentHunk.content += line + '\n';
        if (line.startsWith('+') && !line.startsWith('+++')) {
          additions++;
        } else if (line.startsWith('-') && !line.startsWith('---')) {
          deletions++;
        }
      }
    }

    if (currentHunk) {
      hunks.push(currentHunk);
    }

    if (!file) {
      return null;
    }

    return {
      file,
      hunks,
      additions,
      deletions,
    };
  }

  /**
   * 构建命令行参数
   */
  private buildArgs(command: string, options?: ExecuteOptions): string[] {
    const args: string[] = [];

    // 基础参数
    if (this.config.autoConfirm) {
      args.push('--yes');
    }

    if (this.config.stream) {
      args.push('--stream');
    }

    if (this.config.editFormat) {
      args.push('--edit-format', this.config.editFormat);
    }

    if (this.config.model) {
      args.push('--model', this.config.model);
    }

    // 消息参数
    if (command && !command.startsWith('--')) {
      args.push('--message', command);
    } else {
      args.push(command);
    }

    return args;
  }

  /**
   * 配置更新
   */
  configure(config: Partial<AiderConfig>): void {
    this.config = { ...this.config, ...config };
  }

  /**
   * 获取配置
   */
  getConfig(): AiderConfig {
    return { ...this.config };
  }
}

/**
 * 解析后的 Diff（Aider 特有格式）
 */
export interface ParsedDiff {
  file: string;
  hunks: DiffHunk[];
  additions: number;
  deletions: number;
}
