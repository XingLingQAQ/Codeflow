/**
 * Codex Code Editor
 * 基于 CodexAdapter (OpenAI API) 实现 ICodeEditor 接口
 */
import { ICodeEditor, EditResult, Diff } from '../types.js';
import { CodexAdapter } from '../../adapters/CodexAdapter.js';
/**
 * Codex Editor 配置
 */
export interface CodexEditorConfig {
    cwd?: string;
    backupDir?: string;
    autoBackup?: boolean;
    model?: string;
    maxTokens?: number;
    temperature?: number;
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
 * Codex Code Editor
 */
export declare class CodexCodeEditor implements ICodeEditor {
    readonly name = "codex-editor";
    private adapter;
    private config;
    private backupStack;
    constructor(adapter: CodexAdapter, config?: CodexEditorConfig);
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
    /**
     * 解析 unified diff 格式
     */
    private parseDiff;
}
export {};
//# sourceMappingURL=CodexCodeEditor.d.ts.map