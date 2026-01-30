/**
 * CLI 进程管理器
 * 管理 CLI 进程的生命周期：启动、停止、重启、健康检查
 */
import { spawn } from 'child_process';
import { EventEmitter } from 'events';
import { Readable } from 'stream';
/**
 * CLI 进程管理器
 */
export class CLIProcessManager extends EventEmitter {
    constructor() {
        super();
        this.processes = new Map();
        this.idCounter = 0;
    }
    /**
     * 启动新进程
     */
    async spawn(cli, args, config = {}) {
        const id = `proc_${++this.idCounter}_${Date.now()}`;
        const record = {
            info: {
                id,
                cli,
                args,
                status: 'starting',
                restartCount: 0,
            },
            process: null,
            config: {
                timeout: 60000,
                autoRestart: false,
                maxRestarts: 3,
                restartDelay: 1000,
                ...config,
            },
            outputBuffer: [],
            errorBuffer: [],
        };
        this.processes.set(id, record);
        await this.startProcess(id);
        return id;
    }
    /**
     * 内部启动进程
     */
    async startProcess(processId) {
        const record = this.processes.get(processId);
        if (!record) {
            throw new Error(`Process ${processId} not found`);
        }
        return new Promise((resolve, reject) => {
            try {
                const proc = spawn(record.info.cli, record.info.args, {
                    cwd: record.config.cwd,
                    env: { ...process.env, ...record.config.env },
                    shell: true,
                    stdio: ['pipe', 'pipe', 'pipe'],
                });
                record.process = proc;
                record.info.pid = proc.pid;
                record.info.status = 'running';
                record.info.startedAt = Date.now();
                this.emit('event', {
                    type: 'spawn',
                    processId,
                    pid: proc.pid,
                });
                // 处理 stdout
                proc.stdout?.on('data', (data) => {
                    const str = data.toString();
                    record.outputBuffer.push(str);
                    // 限制缓冲区大小
                    if (record.outputBuffer.length > 1000) {
                        record.outputBuffer.shift();
                    }
                    this.emit('event', {
                        type: 'stdout',
                        processId,
                        data: str,
                    });
                });
                // 处理 stderr
                proc.stderr?.on('data', (data) => {
                    const str = data.toString();
                    record.errorBuffer.push(str);
                    if (record.errorBuffer.length > 1000) {
                        record.errorBuffer.shift();
                    }
                    this.emit('event', {
                        type: 'stderr',
                        processId,
                        data: str,
                    });
                });
                // 处理退出
                proc.on('exit', (code, signal) => {
                    record.info.status = code === 0 ? 'stopped' : 'crashed';
                    record.info.stoppedAt = Date.now();
                    record.info.exitCode = code ?? undefined;
                    record.process = null;
                    this.emit('event', {
                        type: 'exit',
                        processId,
                        code,
                        signal,
                    });
                    // 自动重启逻辑
                    if (record.config.autoRestart &&
                        code !== 0 &&
                        record.info.restartCount < (record.config.maxRestarts || 3)) {
                        record.info.restartCount++;
                        this.emit('event', {
                            type: 'restart',
                            processId,
                            attempt: record.info.restartCount,
                        });
                        setTimeout(() => {
                            this.startProcess(processId).catch((err) => {
                                this.emit('event', {
                                    type: 'error',
                                    processId,
                                    error: err,
                                });
                            });
                        }, record.config.restartDelay || 1000);
                    }
                });
                // 处理错误
                proc.on('error', (error) => {
                    record.info.status = 'crashed';
                    this.emit('event', {
                        type: 'error',
                        processId,
                        error,
                    });
                    reject(error);
                });
                // 等待进程启动
                proc.once('spawn', () => {
                    resolve();
                });
            }
            catch (error) {
                record.info.status = 'crashed';
                reject(error);
            }
        });
    }
    /**
     * 终止进程
     */
    async kill(processId, signal = 'SIGTERM') {
        const record = this.processes.get(processId);
        if (!record) {
            throw new Error(`Process ${processId} not found`);
        }
        if (!record.process || record.info.status !== 'running') {
            return;
        }
        record.info.status = 'stopping';
        return new Promise((resolve, reject) => {
            const timeout = setTimeout(() => {
                // 强制终止
                record.process?.kill('SIGKILL');
            }, 5000);
            record.process.once('exit', () => {
                clearTimeout(timeout);
                resolve();
            });
            record.process.once('error', (err) => {
                clearTimeout(timeout);
                reject(err);
            });
            record.process.kill(signal);
        });
    }
    /**
     * 重启进程
     */
    async restart(processId) {
        const record = this.processes.get(processId);
        if (!record) {
            throw new Error(`Process ${processId} not found`);
        }
        // 先停止
        if (record.process && record.info.status === 'running') {
            await this.kill(processId);
        }
        // 清空缓冲区
        record.outputBuffer = [];
        record.errorBuffer = [];
        record.info.restartCount++;
        // 重新启动
        await this.startProcess(processId);
    }
    /**
     * 健康检查
     */
    async healthCheck(processId) {
        const record = this.processes.get(processId);
        if (!record) {
            return false;
        }
        // 检查进程是否存活
        if (!record.process || record.info.status !== 'running') {
            return false;
        }
        // 检查 PID 是否有效
        try {
            process.kill(record.process.pid, 0);
            return true;
        }
        catch {
            return false;
        }
    }
    /**
     * 获取进程信息
     */
    getInfo(processId) {
        return this.processes.get(processId)?.info;
    }
    /**
     * 获取所有进程信息
     */
    getAllProcesses() {
        return Array.from(this.processes.values()).map((r) => r.info);
    }
    /**
     * 获取进程输出
     */
    getOutput(processId) {
        return this.processes.get(processId)?.outputBuffer || [];
    }
    /**
     * 获取进程错误输出
     */
    getErrors(processId) {
        return this.processes.get(processId)?.errorBuffer || [];
    }
    /**
     * 创建输出流
     */
    createOutputStream(processId) {
        const record = this.processes.get(processId);
        if (!record) {
            throw new Error(`Process ${processId} not found`);
        }
        const stream = new Readable({
            read() { },
        });
        // 监听输出事件
        const handler = (event) => {
            if (event.type === 'stdout' && event.processId === processId) {
                stream.push(event.data);
            }
            if (event.type === 'exit' && event.processId === processId) {
                stream.push(null);
                this.off('event', handler);
            }
        };
        this.on('event', handler);
        return stream;
    }
    /**
     * 向进程发送输入
     */
    async sendInput(processId, input) {
        const record = this.processes.get(processId);
        if (!record || !record.process) {
            throw new Error(`Process ${processId} not found or not running`);
        }
        return new Promise((resolve, reject) => {
            record.process.stdin?.write(input, (err) => {
                if (err)
                    reject(err);
                else
                    resolve();
            });
        });
    }
    /**
     * 移除进程记录
     */
    remove(processId) {
        const record = this.processes.get(processId);
        if (!record) {
            return false;
        }
        // 确保进程已停止
        if (record.process && record.info.status === 'running') {
            record.process.kill('SIGKILL');
        }
        return this.processes.delete(processId);
    }
    /**
     * 清理所有进程
     */
    async cleanup() {
        const killPromises = Array.from(this.processes.keys()).map((id) => this.kill(id).catch(() => { }));
        await Promise.all(killPromises);
        this.processes.clear();
    }
}
//# sourceMappingURL=CLIProcessManager.js.map