/**
 * 配置系统类型定义
 * 支持三级配置继承：Global → Session → Role
 */

export type CanonicalProvider = 'anthropic' | 'openai' | 'google' | 'local' | 'custom';
export type APIChannelProvider = Exclude<CanonicalProvider, 'local'>;
export type RuntimeProviderFamily = 'claude' | 'gemini' | 'codex' | 'openai' | 'custom';

/**
 * 将运行时 provider family 归一到声明侧 canonical provider。
 */
export function toCanonicalProvider(
  provider: CanonicalProvider | RuntimeProviderFamily
): CanonicalProvider {
  switch (provider) {
    case 'claude':
      return 'anthropic';
    case 'gemini':
      return 'google';
    case 'codex':
      return 'openai';
    default:
      return provider;
  }
}

/**
 * 将声明侧 canonical provider 映射到运行时 provider family。
 */
export function toRuntimeProviderFamily(
  provider: APIChannelProvider | RuntimeProviderFamily
): RuntimeProviderFamily {
  switch (provider) {
    case 'anthropic':
      return 'claude';
    case 'google':
      return 'gemini';
    default:
      return provider;
  }
}

export interface GlobalConfig {
  defaultModel: string;
  apiPool: APIChannel[];
  publicMcp: string[];
  summaryThreshold?: number;
  maxRetries?: number;
  timeout?: number;
}

export interface APIChannel {
  id: string;
  name: string;
  provider: APIChannelProvider;
  apiKey?: string;
  baseURL?: string;
  enabled: boolean;
}

export interface SessionConfig {
  sessionId: string;
  mode: 'development' | 'research' | 'creative';
  overrideModel?: string;
  temperature?: number;
  maxTokens?: number;
}

export interface RoleConfig {
  model: string;
  temperature: number;
  topP?: number;
  apiChannel: string;
  mcpTools: string[];
  systemPrompt: string;
}

export interface ConfigHierarchy {
  global: GlobalConfig;
  session?: SessionConfig;
  role?: {
    main?: RoleConfig;
    coder?: RoleConfig;
    sub?: RoleConfig;
  };
}

export interface ResolvedConfig {
  model: string;
  temperature: number;
  topP?: number;
  maxTokens?: number;
  apiChannel?: APIChannel;
  mcpTools: string[];
  systemPrompt?: string;
  timeout?: number;
  maxRetries?: number;
}

export interface IConfigManager {
  // 配置加载
  loadGlobalConfig(): GlobalConfig;
  loadSessionConfig(sessionId: string): SessionConfig | null;
  loadRoleConfig(role: 'main' | 'coder' | 'sub'): RoleConfig | null;

  // 配置保存
  saveGlobalConfig(config: GlobalConfig): void;
  saveSessionConfig(config: SessionConfig): void;
  saveRoleConfig(role: 'main' | 'coder' | 'sub', config: RoleConfig): void;

  // 配置解析（三级继承）
  resolveConfig(sessionId?: string, role?: 'main' | 'coder' | 'sub'): ResolvedConfig;

  // 配置监听
  onConfigChange(callback: (config: ConfigHierarchy) => void): () => void;
}
