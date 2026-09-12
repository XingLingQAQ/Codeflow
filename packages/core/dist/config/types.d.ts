import type { AdapterConfig } from '../adapters/types.js';
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
export declare function toCanonicalProvider(provider: CanonicalProvider | RuntimeProviderFamily): CanonicalProvider;
/**
 * 将声明侧 canonical provider 映射到运行时 provider family。
 */
export declare function toRuntimeProviderFamily(provider: APIChannelProvider | RuntimeProviderFamily): RuntimeProviderFamily;
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
export declare function toAdapterConfigPatch(config: ResolvedConfig): Partial<AdapterConfig>;
export interface IConfigManager {
    loadGlobalConfig(): GlobalConfig;
    loadSessionConfig(sessionId: string): SessionConfig | null;
    loadRoleConfig(role: 'main' | 'coder' | 'sub'): RoleConfig | null;
    saveGlobalConfig(config: GlobalConfig): void;
    saveSessionConfig(config: SessionConfig): void;
    saveRoleConfig(role: 'main' | 'coder' | 'sub', config: RoleConfig): void;
    resolveConfig(sessionId?: string, role?: 'main' | 'coder' | 'sub'): ResolvedConfig;
    onConfigChange(callback: (config: ConfigHierarchy) => void): () => void;
}
//# sourceMappingURL=types.d.ts.map