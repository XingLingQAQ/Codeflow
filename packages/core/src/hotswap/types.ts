/**
 * 模型热切换类型定义
 */

import type { ResolvedConfig, RuntimeProviderFamily } from '../config/types.js';
import { Message } from '../hooks/types.js';

export const CLI_PROVIDER_MODEL_IDS = {
  'gemini-cli': ['gemini-2.0-flash-exp', 'gemini-2.5-pro'],
  'codex-cli': ['gpt-5.4', 'gpt-5-codex'],
} as const;

export type CliAdapterId = keyof typeof CLI_PROVIDER_MODEL_IDS;
export type CliProviderModelId<TAdapterId extends CliAdapterId = CliAdapterId> =
  (typeof CLI_PROVIDER_MODEL_IDS)[TAdapterId][number];
export type GeminiCliProviderModelId = CliProviderModelId<'gemini-cli'>;
export type CodexCliProviderModelId = CliProviderModelId<'codex-cli'>;

const PREDEFINED_CLI_MODELS: ModelInfo[] = [
  {
    id: 'gemini-cli',
    name: 'Gemini CLI',
    provider: 'gemini',
    capabilities: {
      streaming: true,
      vision: false,
      functionCalling: false,
      codeExecution: false,
      multimodal: false,
    },
    contextWindow: 128000,
    maxOutputTokens: 8192,
    available: false,
    status: 'offline',
    adapterKind: 'cli',
    adapterId: 'gemini-cli',
    supportedModelIds: CLI_PROVIDER_MODEL_IDS['gemini-cli'],
  },
  {
    id: 'codex-cli',
    name: 'Codex CLI',
    provider: 'codex',
    capabilities: {
      streaming: true,
      vision: false,
      functionCalling: false,
      codeExecution: false,
      multimodal: false,
    },
    contextWindow: 200000,
    maxOutputTokens: 4096,
    available: false,
    status: 'offline',
    adapterKind: 'cli',
    adapterId: 'codex-cli',
    supportedModelIds: CLI_PROVIDER_MODEL_IDS['codex-cli'],
  },
];

/**
 * 模型信息
 */
export interface ModelInfo {
  id: string;
  name: string;
  provider: RuntimeProviderFamily;
  capabilities: ModelCapabilities;
  contextWindow: number;
  maxOutputTokens: number;
  costPer1kInput?: number;
  costPer1kOutput?: number;
  available: boolean;
  status?: 'online' | 'degraded' | 'offline';
  adapterKind?: 'api' | 'cli';
  // CLI adapter identity；可能与 runtime executor key 和上游 provider model id 不同。
  adapterId?: string;
  // 上游 provider model id；仅在当前已解析/已选中时设置。
  modelId?: string;
  // CLI adapter 当前真实接线支持的 provider model 列表，不代表当前已选中哪一个。
  supportedModelIds?: readonly string[];
}

/**
 * 模型能力
 */
export interface ModelCapabilities {
  streaming: boolean;
  vision: boolean;
  functionCalling: boolean;
  codeExecution: boolean;
  multimodal: boolean;
}

/**
 * 切换选项
 */
export interface SwitchOptions {
  preserveHistory: boolean;
  migrateContext: boolean;
  fallbackOnError: boolean;
  retryCount?: number;
  resolvedConfig?: ResolvedConfig;
}

/**
 * 切换结果
 */
export interface SwitchResult {
  success: boolean;
  previousModel: string;
  currentModel: string;
  contextMigrated: boolean;
  tokensMigrated: number;
  error?: string;
}

/**
 * 重试策略
 */
export interface RetryStrategy {
  maxRetries: number;
  baseDelay: number;
  maxDelay: number;
  backoffMultiplier: number;
  retryableErrors: string[];
}

/**
 * 接力配置
 */
export interface RelayConfig {
  enabled: boolean;
  fallbackChain: string[];
  autoSwitch: boolean;
  switchThreshold: number;
}

/**
 * 上下文迁移结果
 */
export interface ContextMigrationResult {
  success: boolean;
  originalTokens: number;
  migratedTokens: number;
  truncated: boolean;
  messages: Message[];
}

/**
 * 热切换管理器接口
 */
export interface IHotSwapManager {
  // 模型管理
  getAvailableModels(): ModelInfo[];
  getCurrentModel(): ModelInfo | null;
  getModelInfo(modelId: string): ModelInfo | null;

  // 切换操作
  switchModel(modelId: string, options?: Partial<SwitchOptions>): Promise<SwitchResult>;
  canSwitch(modelId: string): boolean;

  // 重试/接力
  retry(options?: Partial<RetryStrategy>): Promise<SwitchResult>;
  relay(fallbackChain?: string[], options?: Partial<SwitchOptions>): Promise<SwitchResult>;

  // 上下文迁移
  migrateContext(targetModel: string): Promise<ContextMigrationResult>;

  // 配置
  configure(config: Partial<HotSwapConfig>): void;
  setRelayConfig(config: Partial<RelayConfig>): void;
}

/**
 * 热切换配置
 */
export interface HotSwapConfig {
  defaultModel: string;
  autoRetry: boolean;
  retryStrategy: RetryStrategy;
  relayConfig: RelayConfig;
  contextMigrationEnabled: boolean;
  maxContextTokens: number;
}

/**
 * 默认配置
 */
export const DEFAULT_HOTSWAP_CONFIG: HotSwapConfig = {
  defaultModel: 'claude-3-opus',
  autoRetry: true,
  retryStrategy: {
    maxRetries: 3,
    baseDelay: 1000,
    maxDelay: 10000,
    backoffMultiplier: 2,
    retryableErrors: ['rate_limit', 'timeout', 'server_error'],
  },
  relayConfig: {
    enabled: true,
    fallbackChain: ['claude-3-opus', 'gemini-pro', 'gpt-4'],
    autoSwitch: false,
    switchThreshold: 3,
  },
  contextMigrationEnabled: true,
  maxContextTokens: 100000,
};

/**
 * 预定义模型列表
 */
export const PREDEFINED_MODELS: ModelInfo[] = [
  {
    id: 'claude-3-opus',
    name: 'Claude 3 Opus',
    provider: 'claude',
    capabilities: {
      streaming: true,
      vision: true,
      functionCalling: true,
      codeExecution: false,
      multimodal: true,
    },
    contextWindow: 200000,
    maxOutputTokens: 4096,
    available: true,
    status: 'online',
  },
  {
    id: 'claude-3-sonnet',
    name: 'Claude 3 Sonnet',
    provider: 'claude',
    capabilities: {
      streaming: true,
      vision: true,
      functionCalling: true,
      codeExecution: false,
      multimodal: true,
    },
    contextWindow: 200000,
    maxOutputTokens: 4096,
    available: true,
    status: 'online',
  },
  {
    id: 'gemini-pro',
    name: 'Gemini Pro',
    provider: 'gemini',
    capabilities: {
      streaming: true,
      vision: true,
      functionCalling: true,
      codeExecution: false,
      multimodal: true,
    },
    contextWindow: 1000000,
    maxOutputTokens: 8192,
    available: true,
    status: 'online',
  },
  ...PREDEFINED_CLI_MODELS,
];
