/**
 * Editor 配置管理器
 * 管理 LLM Editor 的 API key 和其他配置
 * 配置由前端 UI 设置，后端提供存储和验证接口
 */
import type { CodexCliProviderModelId, GeminiCliProviderModelId } from '../hotswap/types.js';
/**
 * Editor 类型
 */
export type EditorType = 'claude' | 'gemini' | 'codex' | 'gemini-cli' | 'codex-cli' | 'aider';
/**
 * 通用 Editor 配置
 */
export interface BaseEditorConfig {
    enabled: boolean;
    model?: string;
    timeout?: number;
    customOptions?: Record<string, unknown>;
}
/**
 * API Editor 配置
 */
export interface EditorConfig extends BaseEditorConfig {
    apiKey?: string;
    baseURL?: string;
    maxTokens?: number;
    temperature?: number;
}
export interface GeminiCliEditorConfig extends BaseEditorConfig {
    geminiPath?: string;
    model?: GeminiCliProviderModelId;
    sandbox?: boolean;
    includeDirectories?: string[];
}
export interface CodexCliEditorConfig extends BaseEditorConfig {
    codexPath?: string;
    model?: CodexCliProviderModelId;
    sandbox?: 'read-only' | 'workspace-write' | 'danger-full-access' | string;
    skipGitRepoCheck?: boolean;
    ephemeral?: boolean;
    outputLastMessage?: boolean;
}
export interface StoredAiderConfig extends EditorConfig {
    cliPath?: string;
    autoCommit?: boolean;
}
export type StoredEditorConfig = EditorConfig | GeminiCliEditorConfig | CodexCliEditorConfig | StoredAiderConfig;
/**
 * 所有 Editor 配置
 */
export interface AllEditorConfigs {
    claude?: EditorConfig;
    gemini?: EditorConfig;
    codex?: EditorConfig;
    'gemini-cli'?: GeminiCliEditorConfig;
    'codex-cli'?: CodexCliEditorConfig;
    aider?: StoredAiderConfig;
}
/**
 * 配置验证结果
 */
export interface ConfigValidationResult {
    valid: boolean;
    errors: string[];
    warnings: string[];
}
type EditorConfigMap = {
    claude: EditorConfig;
    gemini: EditorConfig;
    codex: EditorConfig;
    'gemini-cli': GeminiCliEditorConfig;
    'codex-cli': CodexCliEditorConfig;
    aider: StoredAiderConfig;
};
/**
 * Editor 配置管理器
 */
export declare class EditorConfigManager {
    private configPath;
    private configs;
    private loaded;
    constructor(configPath?: string);
    /**
     * 加载配置
     */
    load(): Promise<AllEditorConfigs>;
    /**
     * 保存配置
     */
    save(): Promise<void>;
    /**
     * 获取单个 Editor 配置
     */
    getConfig<TEditor extends EditorType>(editor: TEditor): Promise<EditorConfigMap[TEditor] | undefined>;
    /**
     * 设置单个 Editor 配置
     */
    setConfig<TEditor extends EditorType>(editor: TEditor, config: EditorConfigMap[TEditor]): Promise<void>;
    /**
     * 获取所有配置
     */
    getAllConfigs(): Promise<AllEditorConfigs>;
    /**
     * 检查 Editor 是否已配置
     */
    isConfigured(editor: EditorType): Promise<boolean>;
    /**
     * 获取环境变量中的 API key
     */
    getEnvApiKey(editor: EditorType): string | undefined;
    /**
     * 获取有效的 API key（配置优先，环境变量次之）
     */
    getEffectiveApiKey(editor: EditorType): Promise<string | undefined>;
    /**
     * 验证配置
     */
    validateConfig(editor: EditorType): Promise<ConfigValidationResult>;
    /**
     * 获取所有已配置的 Editor
     */
    getConfiguredEditors(): Promise<EditorType[]>;
    /**
     * 重置配置
     */
    reset(): Promise<void>;
    private isCliEditor;
    private validateCliConfig;
    private hasApiKeyField;
    private getSafeConfigs;
    /**
     * 掩码 API key
     */
    private maskApiKey;
}
/**
 * 获取默认配置管理器
 */
export declare function getEditorConfigManager(): EditorConfigManager;
export {};
//# sourceMappingURL=EditorConfigManager.d.ts.map