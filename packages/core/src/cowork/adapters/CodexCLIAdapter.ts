/**
 * Codex CLI 适配器
 * 基于 CLIProcessManager 封装 Codex CLI 的非交互调用
 */

import { readFile, unlink } from 'fs/promises';
import { tmpdir } from 'os';
import { join } from 'path';
import type { Message } from '../../hooks/types.js';
import { HookManager } from '../../hooks/HookManager.js';
import { CLIProcessManager } from '../process/CLIProcessManager.js';
import {
  ICLIAdapter,
  CLIResult,
  CLICapabilities,
  ExecuteOptions,
} from '../types.js';

interface OutputCapture {
  path: string;
}

/**
 * Codex CLI 配置
 */
export interface CodexCLIAdapterConfig {
  codexPath?: string;
  model?: string;
  cwd?: string;
  env?: Record<string, string>;
  timeout?: number;
  extraArgs?: string[];
  sandbox?: 'read-only' | 'workspace-write' | 'danger-full-access' | string;
  skipGitRepoCheck?: boolean;
  ephemeral?: boolean;
  outputLastMessage?: boolean;
  outputDirectory?: string;
}

/**
 * Codex CLI 适配器
 */
export class CodexCLIAdapter implements ICLIAdapter {
  readonly name = 'codex-cli';
  readonly version = '0.1.0';

  private config: CodexCLIAdapterConfig;
  private processManager: CLIProcessManager;
  private currentProcessId: string | null = null;
  private hookManager?: HookManager;

  constructor(
    config: CodexCLIAdapterConfig = {},
    processManager: CLIProcessManager = new CLIProcessManager(),
  ) {
    this.config = {
      codexPath: 'codex',
      model: 'gpt-5.4',
      extraArgs: [],
      outputLastMessage: true,
      ...config,
    };
    this.processManager = processManager;
  }

  setHookManager(hookManager?: HookManager): void {
    this.hookManager = hookManager;
  }

  getHookManager(): HookManager | undefined {
    return this.hookManager;
  }

  configure(config: Partial<CodexCLIAdapterConfig>): void {
    this.config = {
      ...this.config,
      ...config,
      extraArgs: config.extraArgs ?? this.config.extraArgs,
    };
  }

  getConfig(): CodexCLIAdapterConfig {
    return {
      ...this.config,
      extraArgs: [...(this.config.extraArgs || [])],
      env: this.config.env ? { ...this.config.env } : undefined,
    };
  }

  /**
   * 非流式执行 Codex CLI
   */
  async execute(command: string, options?: ExecuteOptions): Promise<CLIResult> {
    const payload = await this.applyBeforeSendHooks(command);
    const effectiveCwd = options?.cwd || this.config.cwd;
    const capture = this.createOutputCapture();

    try {
      const result = await this.runInvocation(
        this.buildExecArgs(payload.prompt, payload.model, effectiveCwd, capture?.path),
        options,
      );
      const stdout = await this.resolveOutput(capture, result.stdout);
      const normalized: CLIResult = {
        ...result,
        stdout,
      };

      if (normalized.exitCode === 0 && this.hookManager) {
        await this.hookManager.hook_post_response({
          content: normalized.stdout,
          model: payload.model,
        });
      }

      return normalized;
    } finally {
      await this.cleanupOutputCapture(capture);
    }
  }

  /**
   * 流式执行 Codex CLI
   */
  async stream(
    command: string,
    onChunk: (data: string) => void,
    options?: ExecuteOptions,
  ): Promise<void> {
    const payload = await this.applyBeforeSendHooks(command);
    const effectiveCwd = options?.cwd || this.config.cwd;
    const capture = this.createOutputCapture();
    const processId = await this.processManager.spawn(
      this.config.codexPath!,
      this.buildExecArgs(payload.prompt, payload.model, effectiveCwd, capture?.path),
      {
        cwd: effectiveCwd,
        env: { ...this.config.env, ...options?.env },
      },
    );

    this.currentProcessId = processId;

    let streamIndex = 0;
    const outputStream = this.processManager.createOutputStream(processId);
    const stderrHandler = (event: {
      type: string;
      processId: string;
      data?: string;
    }) => {
      if (event.type === 'stderr' && event.processId === processId && event.data) {
        onChunk(`[stderr] ${event.data}`);
      }
    };

    outputStream.on('data', (chunk: Buffer | string) => {
      const text = chunk.toString();
      onChunk(text);
      if (this.hookManager) {
        this.hookManager.hook_on_stream({
          delta: text,
          index: streamIndex++,
          done: false,
        });
      }
    });

    this.processManager.on('event', stderrHandler);
    const cleanupGuards = this.registerRuntimeGuards(processId, options);

    try {
      const exitCode = await this.waitForExit(processId);
      const stdout = await this.resolveOutput(capture, this.processManager.getOutput(processId).join(''));
      const stderr = this.processManager.getErrors(processId).join('');

      if (this.hookManager) {
        this.hookManager.hook_on_stream({
          delta: '',
          index: streamIndex++,
          done: true,
        });
      }

      if (exitCode !== 0) {
        throw new Error(stderr || `Codex CLI exited with code ${exitCode}`);
      }

      if (this.hookManager) {
        await this.hookManager.hook_post_response({
          content: stdout,
          model: payload.model,
        });
      }
    } finally {
      cleanupGuards();
      outputStream.removeAllListeners();
      this.processManager.off('event', stderrHandler);
      this.disposeProcess(processId);
      await this.cleanupOutputCapture(capture);
    }
  }

  /**
   * 中断当前执行
   */
  async interrupt(): Promise<void> {
    if (!this.currentProcessId) {
      return;
    }

    const processId = this.currentProcessId;
    this.currentProcessId = null;
    await this.processManager.kill(processId, 'SIGINT').catch(() => {});
  }

  /**
   * 健康检查
   */
  async healthCheck(): Promise<boolean> {
    try {
      const result = await this.runInvocation(this.buildVersionArgs(), { timeout: 5000 });
      return result.exitCode === 0 && result.stdout.toLowerCase().includes('codex');
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
      maxContextTokens: 200000,
      features: [
        'code-edit',
        'multi-file',
        'diff-output',
        'non-interactive',
        'hook-aware',
      ],
    };
  }

  private async applyBeforeSendHooks(command: string): Promise<{ prompt: string; model: string }> {
    if (!this.hookManager) {
      return {
        prompt: command,
        model: this.config.model!,
      };
    }

    const payload = await this.hookManager.hook_before_send({
      messages: [{ role: 'user', content: command }],
      model: this.config.model,
    });

    return {
      prompt: this.toPrompt(payload.messages),
      model:
        typeof payload.model === 'string' && payload.model.length > 0
          ? payload.model
          : this.config.model!,
    };
  }

  private toPrompt(messages?: Message[]): string {
    if (!messages || messages.length === 0) {
      return '';
    }

    return messages.map((message) => message.content).join('\n\n');
  }

  private createOutputCapture(): OutputCapture | null {
    if (this.config.outputLastMessage === false) {
      return null;
    }

    const baseDir = this.config.outputDirectory || tmpdir();
    return {
      path: join(baseDir, `codeflow-codex-${Date.now()}-${Math.random().toString(36).slice(2)}.txt`),
    };
  }

  private async resolveOutput(capture: OutputCapture | null, fallback: string): Promise<string> {
    if (!capture) {
      return fallback;
    }

    try {
      const captured = await readFile(capture.path, 'utf-8');
      return captured.length > 0 ? captured : fallback;
    } catch {
      return fallback;
    }
  }

  private async cleanupOutputCapture(capture: OutputCapture | null): Promise<void> {
    if (!capture) {
      return;
    }

    await unlink(capture.path).catch(() => {});
  }

  private buildVersionArgs(): string[] {
    return [...(this.config.extraArgs || []), '--version'];
  }

  private buildExecArgs(
    prompt: string,
    model: string,
    cwd?: string,
    outputPath?: string,
  ): string[] {
    const args = [...(this.config.extraArgs || []), 'exec'];

    if (model) {
      args.push('--model', model);
    }

    if (cwd) {
      args.push('--cd', cwd);
    }

    if (this.config.sandbox) {
      args.push('--sandbox', this.config.sandbox);
    }

    if (this.config.skipGitRepoCheck) {
      args.push('--skip-git-repo-check');
    }

    if (this.config.ephemeral) {
      args.push('--ephemeral');
    }

    if (outputPath) {
      args.push('--output-last-message', outputPath);
    }

    if (prompt) {
      args.push(prompt);
    }

    return args;
  }

  private async runInvocation(args: string[], options?: ExecuteOptions): Promise<CLIResult> {
    const startTime = Date.now();
    const processId = await this.processManager.spawn(this.config.codexPath!, args, {
      cwd: options?.cwd || this.config.cwd,
      env: { ...this.config.env, ...options?.env },
    });

    this.currentProcessId = processId;
    const cleanupGuards = this.registerRuntimeGuards(processId, options);

    try {
      const exitCode = await this.waitForExit(processId);
      return {
        stdout: this.processManager.getOutput(processId).join(''),
        stderr: this.processManager.getErrors(processId).join(''),
        exitCode,
        duration: Date.now() - startTime,
      };
    } finally {
      cleanupGuards();
      this.disposeProcess(processId);
    }
  }

  private registerRuntimeGuards(processId: string, options?: ExecuteOptions): () => void {
    const cleanups: Array<() => void> = [];
    const timeout = options?.timeout ?? this.config.timeout;

    if (timeout && timeout > 0) {
      const timer = setTimeout(() => {
        void this.processManager.kill(processId).catch(() => {});
      }, timeout);
      cleanups.push(() => clearTimeout(timer));
    }

    if (options?.signal) {
      const abortHandler = () => {
        void this.processManager.kill(processId).catch(() => {});
      };

      if (options.signal.aborted) {
        abortHandler();
      } else {
        options.signal.addEventListener('abort', abortHandler, { once: true });
        cleanups.push(() => options.signal?.removeEventListener('abort', abortHandler));
      }
    }

    return () => {
      for (const cleanup of cleanups) {
        cleanup();
      }
    };
  }

  private async waitForExit(processId: string): Promise<number> {
    while (true) {
      const info = this.processManager.getInfo(processId);

      if (!info) {
        return -1;
      }

      if (info.status === 'stopped' || info.status === 'crashed') {
        return info.exitCode ?? -1;
      }

      await new Promise((resolve) => setTimeout(resolve, 20));
    }
  }

  private disposeProcess(processId: string): void {
    if (this.currentProcessId === processId) {
      this.currentProcessId = null;
    }
    this.processManager.remove(processId);
  }
}
