/**
 * Editor 配置管理器
 * 管理 LLM Editor 的 API key 和其他配置
 * 配置由前端 UI 设置，后端提供存储和验证接口
 */
/**
 * Editor 类型
 */
export type EditorType = 'claude' | 'gemini' | 'codex' | 'aider';
/**
 * 单个 Editor 配置
 */
export interface EditorConfig {
    enabled: boolean;
    apiKey?: string;
    baseURL?: string;
    model?: string;
    maxTokens?: number;
    temperature?: number;
    timeout?: number;
    customOptions?: Record<string, unknown>;
}
/**
 * 所有 Editor 配置
 */
export interface AllEditorConfigs {
    claude?: EditorConfig;
    gemini?: EditorConfig;
    codex?: EditorConfig;
    aider?: EditorConfig & {
        cliPath?: string;
        autoCommit?: boolean;
    };
}
/**
 * 配置验证结果
 */
export interface ConfigValidationResult {
    valid: boolean;
    errors: string[];
    warnings: string[];
}
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
    getConfig(editor: EditorType): Promise<EditorConfig | undefined>;
    /**
     * 设置单个 Editor 配置
     */
    setConfig(editor: EditorType, config: EditorConfig): Promise<void>;
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
    /**
     * 获取不含敏感信息的配置（用于保存）
     */
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
//# sourceMappingURL=EditorConfigManager.d.ts.map