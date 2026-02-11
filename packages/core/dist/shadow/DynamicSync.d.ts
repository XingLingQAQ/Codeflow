/**
 * DynamicSync - MS-250 动态同步意图文档
 *
 * 监听文件变更，自动触发 BatchProjector 重新生成意图文档。
 * 支持防抖、增量更新和进度回调。
 */
import { BatchProjector } from './BatchProjector.js';
/**
 * DynamicSync 配置
 */
export interface DynamicSyncConfig {
    /** 防抖延迟（毫秒） */
    debounceMs: number;
    /** 监听的文件扩展名 */
    watchExtensions: string[];
    /** 项目根目录 */
    projectRoot: string;
    /** 监听的目录列表（相对于 projectRoot） */
    watchDirs?: string[];
}
/**
 * 同步进度事件
 */
export interface SyncProgressEvent {
    type: 'start' | 'file_synced' | 'file_failed' | 'complete';
    filePath?: string;
    error?: string;
    total?: number;
    completed?: number;
}
export type SyncProgressCallback = (event: SyncProgressEvent) => void;
export declare class DynamicSync {
    private readonly batchProjector;
    private readonly config;
    private readonly onProgress;
    private watchers;
    private debounceTimers;
    private pendingFiles;
    private isRunning;
    private isSyncing;
    constructor(batchProjector: BatchProjector, config: Partial<DynamicSyncConfig> & {
        projectRoot: string;
    }, onProgress?: SyncProgressCallback);
    /**
     * 启动文件监听
     */
    start(): void;
    /**
     * 停止文件监听
     */
    stop(): void;
    /**
     * 是否正在运行
     */
    isActive(): boolean;
    /**
     * 处理文件变更事件（带防抖）
     */
    onFileChange(filePath: string): void;
    private handleFileEvent;
    /**
     * 同步单个文件
     */
    private syncFile;
    /**
     * 手动触发全量同步
     */
    syncAll(): Promise<void>;
    /**
     * 获取待同步文件数
     */
    getPendingCount(): number;
}
//# sourceMappingURL=DynamicSync.d.ts.map