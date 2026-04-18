/**
 * Gemini CLI 适配器
 * 基于 CLIProcessManager 封装 Gemini CLI 的非交互调用
 */

import type { Message } from '../../hooks/types.js';
import { HookManager } from '../../hooks/HookManager.js';
import { CLIProcessManager } from '../process/CLIProcessManager.js';
import {
  ICLIAdapter,
  CLIResult,
  CLICapabilities,
  ExecuteOptions,
} from '../types.js';

/**
 * Gemini CLI 配置
 */
export interface GeminiCLIAdapterConfig {
  geminiPath?: string;
  model?: string;
  cwd?: string;
  env?: Record<string, string>;
  timeout?: number;
  extraArgs?: string[];
  sandbox?: boolean;
  includeDirectories?: string[];
}

/**
 * Gemini CLI 适配器
 *
 * 说明：当前只接线官方文档确认的 headless 文本 prompt 与 stream-json 事件流。
 * 官方文档未给出稳定的 CLI 图片输入参数，因此此适配器明确声明多模态输入暂不支持。
 */
export class GeminiCLIAdapter implements ICLIAdapter {
  readonly name = 'gemini-cli';
  readonly version = '0.1.0';

  private config: GeminiCLIAdapterConfig;
  private processManager: CLIProcessManager;
  private currentProcessId: string | null = null;
  private hookManager?: HookManager;

  constructor(
    config: GeminiCLIAdapterConfig = {},
    processManager: CLIProcessManager = new CLIProcessManager(),
  ) {
    this.config = {
      geminiPath: 'gemini',
      model: 'gemini-2.0-flash-exp',
      extraArgs: [],
      includeDirectories: [],
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

  configure(config: Partial<GeminiCLIAdapterConfig>): void {
    this.config = {
      ...this.config,
      ...config,
      extraArgs: config.extraArgs ?? this.config.extraArgs,
      includeDirectories: config.includeDirectories ?? this.config.includeDirectories,
    };
  }

  getConfig(): GeminiCLIAdapterConfig {
    return {
      ...this.config,
      extraArgs: [...(this.config.extraArgs || [])],
      includeDirectories: [...(this.config.includeDirectories || [])],
      env: this.config.env ? { ...this.config.env } : undefined,
    };
  }

  /**
   * 非流式执行 Gemini CLI
   */
  async execute(command: string, options?: ExecuteOptions): Promise<CLIResult> {
    const payload = await this.applyBeforeSendHooks(command);
    const result = await this.runInvocation(this.buildPromptArgs(payload.prompt, payload.model), options);

    if (result.exitCode === 0 && this.hookManager) {
      await this.hookManager.hook_post_response({
        content: result.stdout,
        model: payload.model,
      });
    }

    return result;
  }

  /**
   * 流式执行 Gemini CLI
   *
   * 当前按官方 headless stream-json 输出原样透传事件行，不假设特定 JSON schema。
   */
  async stream(
    command: string,
    onChunk: (data: string) => void,
    options?: ExecuteOptions,
  ): Promise<void> {
    const payload = await this.applyBeforeSendHooks(command);
    const processId = await this.processManager.spawn(
      this.config.geminiPath!,
      this.buildPromptArgs(payload.prompt, payload.model, true),
      {
        cwd: options?.cwd || this.config.cwd,
        env: { ...this.config.env, ...options?.env },
      },
    );

    this.currentProcessId = processId;

    let streamIndex = 0;
    let stdout = '';
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
      stdout += text;
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
      const stderr = this.processManager.getErrors(processId).join('');

      if (this.hookManager) {
        this.hookManager.hook_on_stream({
          delta: '',
          index: streamIndex++,
          done: true,
        });
      }

      if (exitCode !== 0) {
        throw new Error(stderr || `Gemini CLI exited with code ${exitCode}`);
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
      return result.exitCode === 0 && result.stdout.toLowerCase().includes('gemini');
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
      maxContextTokens: 128000,
      features: [
        'code-edit',
        'multi-file',
        'headless-prompt',
        'stream-json-events',
        'sandbox',
        'hook-aware',
        'multimodal-input-unsupported',
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

  private buildVersionArgs(): string[] {
    return [...(this.config.extraArgs || []), '--version'];
  }

  private buildPromptArgs(prompt: string, model: string, streaming: boolean = false): string[] {
    const args = [...(this.config.extraArgs || [])];

    if (model) {
      args.push('-m', model);
    }

    if (this.config.sandbox) {
      args.push('--sandbox');
    }

    for (const directory of this.config.includeDirectories || []) {
      args.push('--include-directories', directory);
    }

    if (streaming) {
      args.push('--output-format', 'stream-json');
    }

    args.push('-p', prompt);

    return args;
  }

  private async runInvocation(args: string[], options?: ExecuteOptions): Promise<CLIResult> {
    const startTime = Date.now();
    const processId = await this.processManager.spawn(this.config.geminiPath!, args, {
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
