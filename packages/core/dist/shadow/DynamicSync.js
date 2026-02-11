/**
 * DynamicSync - MS-250 动态同步意图文档
 *
 * 监听文件变更，自动触发 BatchProjector 重新生成意图文档。
 * 支持防抖、增量更新和进度回调。
 */
import * as fs from 'fs';
import * as path from 'path';
const DEFAULT_CONFIG = {
    debounceMs: 500,
    watchExtensions: ['.ts', '.js', '.go', '.py'],
    projectRoot: process.cwd(),
};
export class DynamicSync {
    constructor(batchProjector, config, onProgress = () => { }) {
        this.watchers = [];
        this.debounceTimers = new Map();
        this.pendingFiles = new Set();
        this.isRunning = false;
        this.isSyncing = false;
        this.batchProjector = batchProjector;
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.onProgress = onProgress;
    }
    /**
     * 启动文件监听
     */
    start() {
        if (this.isRunning)
            return;
        this.isRunning = true;
        const dirs = this.config.watchDirs
            ? this.config.watchDirs.map((d) => path.resolve(this.config.projectRoot, d))
            : [this.config.projectRoot];
        for (const dir of dirs) {
            try {
                const watcher = fs.watch(dir, { recursive: true }, (eventType, filename) => {
                    if (!filename)
                        return;
                    const fullPath = path.resolve(dir, filename);
                    this.handleFileEvent(fullPath);
                });
                watcher.on('error', () => {
                    // 忽略 watcher 错误，避免崩溃
                });
                this.watchers.push(watcher);
            }
            catch {
                // 目录不存在或无权限，跳过
            }
        }
    }
    /**
     * 停止文件监听
     */
    stop() {
        this.isRunning = false;
        for (const watcher of this.watchers) {
            try {
                watcher.close();
            }
            catch {
                // 忽略关闭错误
            }
        }
        this.watchers = [];
        for (const timer of this.debounceTimers.values()) {
            clearTimeout(timer);
        }
        this.debounceTimers.clear();
        this.pendingFiles.clear();
    }
    /**
     * 是否正在运行
     */
    isActive() {
        return this.isRunning;
    }
    /**
     * 处理文件变更事件（带防抖）
     */
    onFileChange(filePath) {
        this.handleFileEvent(filePath);
    }
    handleFileEvent(filePath) {
        if (!this.isRunning)
            return;
        const ext = path.extname(filePath).toLowerCase();
        if (!this.config.watchExtensions.includes(ext))
            return;
        if (filePath.includes('.codeflow'))
            return;
        if (filePath.includes('node_modules'))
            return;
        const existing = this.debounceTimers.get(filePath);
        if (existing) {
            clearTimeout(existing);
        }
        this.pendingFiles.add(filePath);
        const timer = setTimeout(() => {
            this.debounceTimers.delete(filePath);
            this.syncFile(filePath);
        }, this.config.debounceMs);
        this.debounceTimers.set(filePath, timer);
    }
    /**
     * 同步单个文件
     */
    async syncFile(filePath) {
        this.pendingFiles.delete(filePath);
        try {
            await fs.promises.access(filePath, fs.constants.R_OK);
        }
        catch {
            return;
        }
        this.onProgress({
            type: 'start',
            filePath,
            total: 1,
            completed: 0,
        });
        try {
            await this.batchProjector.projectFile(filePath, this.config.projectRoot);
            this.onProgress({
                type: 'file_synced',
                filePath,
                total: 1,
                completed: 1,
            });
        }
        catch (error) {
            const message = error instanceof Error ? error.message : String(error);
            this.onProgress({
                type: 'file_failed',
                filePath,
                error: message,
            });
        }
        this.onProgress({
            type: 'complete',
            total: 1,
            completed: 1,
        });
    }
    /**
     * 手动触发全量同步
     */
    async syncAll() {
        if (this.isSyncing)
            return;
        this.isSyncing = true;
        try {
            const dirs = this.config.watchDirs
                ? this.config.watchDirs.map((d) => path.resolve(this.config.projectRoot, d))
                : [this.config.projectRoot];
            this.onProgress({ type: 'start' });
            for (const dir of dirs) {
                await this.batchProjector.projectDirectory(dir, this.config.projectRoot);
            }
            this.onProgress({ type: 'complete' });
        }
        finally {
            this.isSyncing = false;
        }
    }
    /**
     * 获取待同步文件数
     */
    getPendingCount() {
        return this.pendingFiles.size;
    }
}
//# sourceMappingURL=DynamicSync.js.map