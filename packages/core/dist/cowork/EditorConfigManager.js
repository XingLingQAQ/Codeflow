/**
 * Editor 配置管理器
 * 管理 LLM Editor 的 API key 和其他配置
 * 配置由前端 UI 设置，后端提供存储和验证接口
 */
import { readFile, writeFile, mkdir } from 'fs/promises';
import { existsSync } from 'fs';
import { join, dirname } from 'path';
import { CLI_PROVIDER_MODEL_IDS } from '../hotswap/types.js';
const API_EDITORS = ['claude', 'gemini', 'codex'];
const CLI_EDITORS = ['gemini-cli', 'codex-cli'];
const ALL_EDITORS = [...API_EDITORS, ...CLI_EDITORS, 'aider'];
const CLI_VALID_MODELS = CLI_PROVIDER_MODEL_IDS;
const VALID_MODELS = {
    claude: ['claude-sonnet-4-20250514', 'claude-3-opus', 'claude-3-sonnet', 'claude-3-haiku'],
    gemini: ['gemini-2.0-flash-exp', 'gemini-2.5-pro', 'gemini-pro', 'gemini-pro-vision'],
    codex: ['gpt-4', 'gpt-4-turbo', 'gpt-3.5-turbo', 'gpt-5.1-codex', 'gpt-5-codex'],
    'gemini-cli': CLI_VALID_MODELS['gemini-cli'],
    'codex-cli': CLI_VALID_MODELS['codex-cli'],
    aider: [],
};
/**
 * Editor 配置管理器
 */
export class EditorConfigManager {
    constructor(configPath) {
        this.configs = {};
        this.loaded = false;
        this.configPath = configPath || join(process.cwd(), '.cowork', 'editors.json');
    }
    /**
     * 加载配置
     */
    async load() {
        if (this.loaded) {
            return this.configs;
        }
        try {
            if (existsSync(this.configPath)) {
                const content = await readFile(this.configPath, 'utf-8');
                this.configs = JSON.parse(content);
            }
            this.loaded = true;
        }
        catch (error) {
            console.warn('Failed to load editor configs:', error);
            this.configs = {};
            this.loaded = true;
        }
        return this.configs;
    }
    /**
     * 保存配置
     */
    async save() {
        await mkdir(dirname(this.configPath), { recursive: true });
        // 保存时不包含敏感信息的明文（API key 应该加密或使用环境变量）
        const safeConfigs = this.getSafeConfigs();
        await writeFile(this.configPath, JSON.stringify(safeConfigs, null, 2), 'utf-8');
    }
    /**
     * 获取单个 Editor 配置
     */
    async getConfig(editor) {
        await this.load();
        return this.configs[editor];
    }
    /**
     * 设置单个 Editor 配置
     */
    async setConfig(editor, config) {
        await this.load();
        this.configs[editor] = config;
        await this.save();
    }
    /**
     * 获取所有配置
     */
    async getAllConfigs() {
        await this.load();
        return { ...this.configs };
    }
    /**
     * 检查 Editor 是否已配置
     */
    async isConfigured(editor) {
        const config = await this.getConfig(editor);
        if (!config || !config.enabled) {
            return false;
        }
        if (this.isCliEditor(editor)) {
            return true;
        }
        // Aider 不需要 API key
        if (editor === 'aider') {
            return true;
        }
        // API editor 需要 API key
        return !!(await this.getEffectiveApiKey(editor));
    }
    /**
     * 获取环境变量中的 API key
     */
    getEnvApiKey(editor) {
        const envKeys = {
            claude: 'ANTHROPIC_API_KEY',
            gemini: 'GOOGLE_API_KEY',
            codex: 'OPENAI_API_KEY',
        };
        const envKey = envKeys[editor];
        return envKey ? process.env[envKey] : undefined;
    }
    /**
     * 获取有效的 API key（配置优先，环境变量次之）
     */
    async getEffectiveApiKey(editor) {
        const config = await this.getConfig(editor);
        if (config && this.hasApiKeyField(config)) {
            return config.apiKey || this.getEnvApiKey(editor);
        }
        return this.getEnvApiKey(editor);
    }
    /**
     * 验证配置
     */
    async validateConfig(editor) {
        const config = await this.getConfig(editor);
        const errors = [];
        const warnings = [];
        if (!config) {
            errors.push(`${editor} editor is not configured`);
            return { valid: false, errors, warnings };
        }
        if (!config.enabled) {
            warnings.push(`${editor} editor is disabled`);
        }
        if (this.isCliEditor(editor)) {
            this.validateCliConfig(editor, config, warnings);
        }
        else if (editor !== 'aider') {
            const apiKey = await this.getEffectiveApiKey(editor);
            if (!apiKey) {
                errors.push(`${editor} editor requires an API key`);
            }
            else if (apiKey.length < 10) {
                warnings.push(`${editor} API key seems too short`);
            }
        }
        if (editor === 'aider') {
            const aiderConfig = config;
            if (aiderConfig.cliPath && !existsSync(aiderConfig.cliPath)) {
                warnings.push(`Aider CLI path does not exist: ${aiderConfig.cliPath}`);
            }
        }
        if (config.model) {
            const validModels = VALID_MODELS[editor];
            if (validModels.length > 0 && !validModels.includes(config.model)) {
                warnings.push(`Unknown model for ${editor}: ${config.model}`);
            }
        }
        return {
            valid: errors.length === 0,
            errors,
            warnings,
        };
    }
    /**
     * 获取所有已配置的 Editor
     */
    async getConfiguredEditors() {
        await this.load();
        const configured = [];
        for (const editor of ALL_EDITORS) {
            if (await this.isConfigured(editor)) {
                configured.push(editor);
            }
        }
        return configured;
    }
    /**
     * 重置配置
     */
    async reset() {
        this.configs = {};
        this.loaded = false;
        await this.save();
    }
    // ==================== 私有方法 ====================
    isCliEditor(editor) {
        return CLI_EDITORS.includes(editor);
    }
    validateCliConfig(editor, config, warnings) {
        if (editor === 'gemini-cli') {
            const geminiCliConfig = config;
            if (geminiCliConfig.geminiPath && !existsSync(geminiCliConfig.geminiPath)) {
                warnings.push(`Gemini CLI path does not exist: ${geminiCliConfig.geminiPath}`);
            }
            return;
        }
        const codexCliConfig = config;
        if (codexCliConfig.codexPath && !existsSync(codexCliConfig.codexPath)) {
            warnings.push(`Codex CLI path does not exist: ${codexCliConfig.codexPath}`);
        }
    }
    hasApiKeyField(config) {
        return 'apiKey' in config;
    }
    getSafeConfigs() {
        const safe = {};
        for (const [key, config] of Object.entries(this.configs)) {
            if (!config) {
                continue;
            }
            if (this.hasApiKeyField(config)) {
                const { apiKey, ...rest } = config;
                safe[key] = {
                    ...rest,
                    apiKey: apiKey ? this.maskApiKey(apiKey) : undefined,
                };
                continue;
            }
            safe[key] = {
                ...config,
            };
        }
        return safe;
    }
    /**
     * 掩码 API key
     */
    maskApiKey(key) {
        if (key.length <= 8) {
            return '****';
        }
        return key.slice(0, 4) + '****' + key.slice(-4);
    }
}
/**
 * 默认配置管理器实例
 */
let defaultManager = null;
/**
 * 获取默认配置管理器
 */
export function getEditorConfigManager() {
    if (!defaultManager) {
        defaultManager = new EditorConfigManager();
    }
    return defaultManager;
}
//# sourceMappingURL=EditorConfigManager.js.map