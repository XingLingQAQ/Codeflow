/**
 * Editor 配置管理器
 * 管理 LLM Editor 的 API key 和其他配置
 * 配置由前端 UI 设置，后端提供存储和验证接口
 */

import { readFile, writeFile, mkdir } from 'fs/promises';
import { existsSync } from 'fs';
import { join, dirname } from 'path';
import { CLI_PROVIDER_MODEL_IDS } from '../hotswap/types.js';
import type {
  CodexCliProviderModelId,
  GeminiCliProviderModelId,
} from '../hotswap/types.js';

const API_EDITORS = ['claude', 'gemini', 'codex'] as const;
const CLI_EDITORS = ['gemini-cli', 'codex-cli'] as const;
const ALL_EDITORS = [...API_EDITORS, ...CLI_EDITORS, 'aider'] as const;

type CliEditorType = typeof CLI_EDITORS[number];

const CLI_VALID_MODELS: Record<CliEditorType, readonly string[]> = CLI_PROVIDER_MODEL_IDS;

const VALID_MODELS: Record<EditorType, readonly string[]> = {
  claude: ['claude-sonnet-4-20250514', 'claude-3-opus', 'claude-3-sonnet', 'claude-3-haiku'],
  gemini: ['gemini-2.0-flash-exp', 'gemini-2.5-pro', 'gemini-pro', 'gemini-pro-vision'],
  codex: ['gpt-4', 'gpt-4-turbo', 'gpt-3.5-turbo', 'gpt-5.1-codex', 'gpt-5-codex'],
  'gemini-cli': CLI_VALID_MODELS['gemini-cli'],
  'codex-cli': CLI_VALID_MODELS['codex-cli'],
  aider: [],
};

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

export type StoredEditorConfig =
  | EditorConfig
  | GeminiCliEditorConfig
  | CodexCliEditorConfig
  | StoredAiderConfig;

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
export class EditorConfigManager {
  private configPath: string;
  private configs: AllEditorConfigs = {};
  private loaded = false;

  constructor(configPath?: string) {
    this.configPath = configPath || join(process.cwd(), '.cowork', 'editors.json');
  }

  /**
   * 加载配置
   */
  async load(): Promise<AllEditorConfigs> {
    if (this.loaded) {
      return this.configs;
    }

    try {
      if (existsSync(this.configPath)) {
        const content = await readFile(this.configPath, 'utf-8');
        this.configs = JSON.parse(content);
      }
      this.loaded = true;
    } catch (error) {
      console.warn('Failed to load editor configs:', error);
      this.configs = {};
      this.loaded = true;
    }

    return this.configs;
  }

  /**
   * 保存配置
   */
  async save(): Promise<void> {
    await mkdir(dirname(this.configPath), { recursive: true });

    // 保存时不包含敏感信息的明文（API key 应该加密或使用环境变量）
    const safeConfigs = this.getSafeConfigs();
    await writeFile(this.configPath, JSON.stringify(safeConfigs, null, 2), 'utf-8');
  }

  /**
   * 获取单个 Editor 配置
   */
  async getConfig<TEditor extends EditorType>(
    editor: TEditor,
  ): Promise<EditorConfigMap[TEditor] | undefined> {
    await this.load();
    return this.configs[editor] as EditorConfigMap[TEditor] | undefined;
  }

  /**
   * 设置单个 Editor 配置
   */
  async setConfig<TEditor extends EditorType>(
    editor: TEditor,
    config: EditorConfigMap[TEditor],
  ): Promise<void> {
    await this.load();
    this.configs[editor] = config;
    await this.save();
  }

  /**
   * 获取所有配置
   */
  async getAllConfigs(): Promise<AllEditorConfigs> {
    await this.load();
    return { ...this.configs };
  }

  /**
   * 检查 Editor 是否已配置
   */
  async isConfigured(editor: EditorType): Promise<boolean> {
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
  getEnvApiKey(editor: EditorType): string | undefined {
    const envKeys: Partial<Record<EditorType, string>> = {
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
  async getEffectiveApiKey(editor: EditorType): Promise<string | undefined> {
    const config = await this.getConfig(editor);
    if (config && this.hasApiKeyField(config)) {
      return config.apiKey || this.getEnvApiKey(editor);
    }
    return this.getEnvApiKey(editor);
  }

  /**
   * 验证配置
   */
  async validateConfig(editor: EditorType): Promise<ConfigValidationResult> {
    const config = await this.getConfig(editor);
    const errors: string[] = [];
    const warnings: string[] = [];

    if (!config) {
      errors.push(`${editor} editor is not configured`);
      return { valid: false, errors, warnings };
    }

    if (!config.enabled) {
      warnings.push(`${editor} editor is disabled`);
    }

    if (this.isCliEditor(editor)) {
      this.validateCliConfig(editor, config, warnings);
    } else if (editor !== 'aider') {
      const apiKey = await this.getEffectiveApiKey(editor);
      if (!apiKey) {
        errors.push(`${editor} editor requires an API key`);
      } else if (apiKey.length < 10) {
        warnings.push(`${editor} API key seems too short`);
      }
    }

    if (editor === 'aider') {
      const aiderConfig = config as StoredAiderConfig;
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
  async getConfiguredEditors(): Promise<EditorType[]> {
    await this.load();
    const configured: EditorType[] = [];

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
  async reset(): Promise<void> {
    this.configs = {};
    this.loaded = false;
    await this.save();
  }

  // ==================== 私有方法 ====================

  private isCliEditor(editor: EditorType): editor is typeof CLI_EDITORS[number] {
    return (CLI_EDITORS as readonly string[]).includes(editor);
  }

  private validateCliConfig(
    editor: typeof CLI_EDITORS[number],
    config: StoredEditorConfig,
    warnings: string[],
  ): void {
    if (editor === 'gemini-cli') {
      const geminiCliConfig = config as GeminiCliEditorConfig;
      if (geminiCliConfig.geminiPath && !existsSync(geminiCliConfig.geminiPath)) {
        warnings.push(`Gemini CLI path does not exist: ${geminiCliConfig.geminiPath}`);
      }
      return;
    }

    const codexCliConfig = config as CodexCliEditorConfig;
    if (codexCliConfig.codexPath && !existsSync(codexCliConfig.codexPath)) {
      warnings.push(`Codex CLI path does not exist: ${codexCliConfig.codexPath}`);
    }
  }

  private hasApiKeyField(config: StoredEditorConfig): config is EditorConfig | StoredAiderConfig {
    return 'apiKey' in config;
  }

  private getSafeConfigs(): AllEditorConfigs {
    const safe: Partial<Record<EditorType, StoredEditorConfig>> = {};

    for (const [key, config] of Object.entries(this.configs) as [EditorType, StoredEditorConfig | undefined][]) {
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

    return safe as AllEditorConfigs;
  }

  /**
   * 掩码 API key
   */
  private maskApiKey(key: string): string {
    if (key.length <= 8) {
      return '****';
    }
    return key.slice(0, 4) + '****' + key.slice(-4);
  }
}

/**
 * 默认配置管理器实例
 */
let defaultManager: EditorConfigManager | null = null;

/**
 * 获取默认配置管理器
 */
export function getEditorConfigManager(): EditorConfigManager {
  if (!defaultManager) {
    defaultManager = new EditorConfigManager();
  }
  return defaultManager;
}
