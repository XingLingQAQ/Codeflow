/**
 * Codex CLI 适配器
 * 基于 CLIProcessManager 封装 Codex CLI 的非交互调用
 */
import { readFile, unlink } from 'fs/promises';
import { tmpdir } from 'os';
import { join } from 'path';
import { CLIProcessManager } from '../process/CLIProcessManager.js';
/**
 * Codex CLI 适配器
 */
export class CodexCLIAdapter {
    constructor(config = {}, processManager = new CLIProcessManager()) {
        this.name = 'codex-cli';
        this.version = '0.1.0';
        this.currentProcessId = null;
        this.config = {
            codexPath: 'codex',
            model: 'gpt-5.4',
            extraArgs: [],
            outputLastMessage: true,
            ...config,
        };
        this.processManager = processManager;
    }
    setHookManager(hookManager) {
        this.hookManager = hookManager;
    }
    getHookManager() {
        return this.hookManager;
    }
    configure(config) {
        this.config = {
            ...this.config,
            ...config,
            extraArgs: config.extraArgs ?? this.config.extraArgs,
        };
    }
    getConfig() {
        return {
            ...this.config,
            extraArgs: [...(this.config.extraArgs || [])],
            env: this.config.env ? { ...this.config.env } : undefined,
        };
    }
    /**
     * 非流式执行 Codex CLI
     */
    async execute(command, options) {
        const payload = await this.applyBeforeSendHooks(command);
        const effectiveCwd = options?.cwd || this.config.cwd;
        const capture = this.createOutputCapture();
        try {
            const result = await this.runInvocation(this.buildExecArgs(payload.prompt, payload.model, effectiveCwd, capture?.path), options);
            const stdout = await this.resolveOutput(capture, result.stdout);
            const normalized = {
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
        }
        finally {
            await this.cleanupOutputCapture(capture);
        }
    }
    /**
     * 流式执行 Codex CLI
     */
    async stream(command, onChunk, options) {
        const payload = await this.applyBeforeSendHooks(command);
        const effectiveCwd = options?.cwd || this.config.cwd;
        const capture = this.createOutputCapture();
        const processId = await this.processManager.spawn(this.config.codexPath, this.buildExecArgs(payload.prompt, payload.model, effectiveCwd, capture?.path), {
            cwd: effectiveCwd,
            env: { ...this.config.env, ...options?.env },
        });
        this.currentProcessId = processId;
        let streamIndex = 0;
        const outputStream = this.processManager.createOutputStream(processId);
        const stderrHandler = (event) => {
            if (event.type === 'stderr' && event.processId === processId && event.data) {
                onChunk(`[stderr] ${event.data}`);
            }
        };
        outputStream.on('data', (chunk) => {
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
        }
        finally {
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
    async interrupt() {
        if (!this.currentProcessId) {
            return;
        }
        const processId = this.currentProcessId;
        this.currentProcessId = null;
        await this.processManager.kill(processId, 'SIGINT').catch(() => { });
    }
    /**
     * 健康检查
     */
    async healthCheck() {
        try {
            const result = await this.runInvocation(this.buildVersionArgs(), { timeout: 5000 });
            return result.exitCode === 0 && result.stdout.toLowerCase().includes('codex');
        }
        catch {
            return false;
        }
    }
    /**
     * 获取能力描述
     */
    getCapabilities() {
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
    async applyBeforeSendHooks(command) {
        if (!this.hookManager) {
            return {
                prompt: command,
                model: this.config.model,
            };
        }
        const payload = await this.hookManager.hook_before_send({
            messages: [{ role: 'user', content: command }],
            model: this.config.model,
        });
        return {
            prompt: this.toPrompt(payload.messages),
            model: typeof payload.model === 'string' && payload.model.length > 0
                ? payload.model
                : this.config.model,
        };
    }
    toPrompt(messages) {
        if (!messages || messages.length === 0) {
            return '';
        }
        return messages.map((message) => message.content).join('\n\n');
    }
    createOutputCapture() {
        if (this.config.outputLastMessage === false) {
            return null;
        }
        const baseDir = this.config.outputDirectory || tmpdir();
        return {
            path: join(baseDir, `codeflow-codex-${Date.now()}-${Math.random().toString(36).slice(2)}.txt`),
        };
    }
    async resolveOutput(capture, fallback) {
        if (!capture) {
            return fallback;
        }
        try {
            const captured = await readFile(capture.path, 'utf-8');
            return captured.length > 0 ? captured : fallback;
        }
        catch {
            return fallback;
        }
    }
    async cleanupOutputCapture(capture) {
        if (!capture) {
            return;
        }
        await unlink(capture.path).catch(() => { });
    }
    buildVersionArgs() {
        return [...(this.config.extraArgs || []), '--version'];
    }
    buildExecArgs(prompt, model, cwd, outputPath) {
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
    async runInvocation(args, options) {
        const startTime = Date.now();
        const processId = await this.processManager.spawn(this.config.codexPath, args, {
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
        }
        finally {
            cleanupGuards();
            this.disposeProcess(processId);
        }
    }
    registerRuntimeGuards(processId, options) {
        const cleanups = [];
        const timeout = options?.timeout ?? this.config.timeout;
        if (timeout && timeout > 0) {
            const timer = setTimeout(() => {
                void this.processManager.kill(processId).catch(() => { });
            }, timeout);
            cleanups.push(() => clearTimeout(timer));
        }
        if (options?.signal) {
            const abortHandler = () => {
                void this.processManager.kill(processId).catch(() => { });
            };
            if (options.signal.aborted) {
                abortHandler();
            }
            else {
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
    async waitForExit(processId) {
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
    disposeProcess(processId) {
        if (this.currentProcessId === processId) {
            this.currentProcessId = null;
        }
        this.processManager.remove(processId);
    }
}
//# sourceMappingURL=CodexCLIAdapter.js.map