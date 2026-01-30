/**
 * Editor 配置管理器
 * 管理 LLM Editor 的 API key 和其他配置
 * 配置由前端 UI 设置，后端提供存储和验证接口
 */

import { readFile, writeFile, mkdir } from 'fs/promises';
import { existsSync } from 'fs';
import { join, dirname } from 'path';

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
  async getConfig(editor: EditorType): Promise<EditorConfig | undefined> {
    await this.load();
    return this.configs[editor];
  }

  /**
   * 设置单个 Editor 配置
   */
  async setConfig(editor: EditorType, config: EditorConfig): Promise<void> {
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

    // Aider 不需要 API key
    if (editor === 'aider') {
      return true;
    }

    // LLM Editor 需要 API key
    return !!config.apiKey || !!this.getEnvApiKey(editor);
  }

  /**
   * 获取环境变量中的 API key
   */
  getEnvApiKey(editor: EditorType): string | undefined {
    const envKeys: Record<EditorType, string> = {
      claude: 'ANTHROPIC_API_KEY',
      gemini: 'GOOGLE_API_KEY',
      codex: 'OPENAI_API_KEY',
      aider: '',
    };

    const envKey = envKeys[editor];
    return envKey ? process.env[envKey] : undefined;
  }

  /**
   * 获取有效的 API key（配置优先，环境变量次之）
   */
  async getEffectiveApiKey(editor: EditorType): Promise<string | undefined> {
    const config = await this.getConfig(editor);
    return config?.apiKey || this.getEnvApiKey(editor);
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

    // 检查 API key
    if (editor !== 'aider') {
      const apiKey = await this.getEffectiveApiKey(editor);
      if (!apiKey) {
        errors.push(`${editor} editor requires an API key`);
      } else if (apiKey.length < 10) {
        warnings.push(`${editor} API key seems too short`);
      }
    }

    // 检查 Aider CLI 路径
    if (editor === 'aider') {
      const aiderConfig = config as EditorConfig & { cliPath?: string };
      if (aiderConfig.cliPath && !existsSync(aiderConfig.cliPath)) {
        warnings.push(`Aider CLI path does not exist: ${aiderConfig.cliPath}`);
      }
    }

    // 检查模型配置
    if (config.model) {
      const validModels: Record<EditorType, string[]> = {
        claude: ['claude-sonnet-4-20250514', 'claude-3-opus', 'claude-3-sonnet', 'claude-3-haiku'],
        gemini: ['gemini-2.0-flash-exp', 'gemini-pro', 'gemini-pro-vision'],
        codex: ['gpt-4', 'gpt-4-turbo', 'gpt-3.5-turbo'],
        aider: [],
      };

      if (validModels[editor].length > 0 && !validModels[editor].includes(config.model)) {
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

    for (const editor of ['claude', 'gemini', 'codex', 'aider'] as EditorType[]) {
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

  /**
   * 获取不含敏感信息的配置（用于保存）
   */
  private getSafeConfigs(): AllEditorConfigs {
    const safe: AllEditorConfigs = {};

    for (const [key, config] of Object.entries(this.configs)) {
      if (config) {
        const { apiKey, ...rest } = config;
        // 如果 API key 存在，保存一个掩码版本用于显示
        safe[key as EditorType] = {
          ...rest,
          apiKey: apiKey ? this.maskApiKey(apiKey) : undefined,
        };
      }
    }

    return safe;
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
