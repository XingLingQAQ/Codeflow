/**
 * Aider Code Editor
 * 基于 AiderAdapter 实现 ICodeEditor 接口
 */
import { ICodeEditor, EditResult, Diff } from '../types.js';
import { AiderAdapter } from '../adapters/AiderAdapter.js';
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
export declare class AiderCodeEditor implements ICodeEditor {
    readonly name = "aider-editor";
    private adapter;
    private config;
    private backupStack;
    constructor(adapter: AiderAdapter, config?: AiderEditorConfig);
    /**
     * 编辑单个文件
     */
    edit(file: string, instruction: string): Promise<EditResult>;
    /**
     * 编辑多个文件
     */
    editMultiple(files: string[], instruction: string): Promise<EditResult[]>;
    /**
     * 预览修改（不实际写入）
     */
    preview(file: string, instruction: string): Promise<Diff>;
    /**
     * 应用 Diff
     */
    applyDiff(file: string, diff: Diff): Promise<void>;
    /**
     * 撤销上一次修改
     */
    undo(): Promise<void>;
    /**
     * 获取备份栈
     */
    getBackupStack(): BackupRecord[];
    /**
     * 清理所有备份
     */
    clearBackups(): Promise<void>;
    private resolvePath;
    private backup;
    private emptyDiff;
    private convertDiff;
}
export {};
//# sourceMappingURL=AiderCodeEditor.d.ts.map