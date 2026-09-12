import { EventEmitter } from 'events';
import { IConfigManager, GlobalConfig, SessionConfig, RoleConfig, ConfigHierarchy, ResolvedConfig, APIChannel } from './types.js';
type RuntimeMetadata = {
    answerStyle?: string;
    capabilities?: string[];
    allowedSkills?: string[];
    allowedHooks?: string[];
};
type ResolvedRoleConfig = RoleConfig & RuntimeMetadata;
type ResolvedRuntimeConfig = ResolvedConfig & RuntimeMetadata;
/**
 * 配置管理器实现
 * 支持三级配置继承：Global → Session → Role
 */
export declare class ConfigManager extends EventEmitter implements IConfigManager {
    private globalConfig;
    private sessionConfigs;
    private roleConfigs;
    private changeCallbacks;
    constructor(initialConfig?: Partial<GlobalConfig>);
    loadGlobalConfig(): GlobalConfig;
    loadSessionConfig(sessionId: string): SessionConfig | null;
    loadRoleConfig(role: 'main' | 'coder' | 'sub'): ResolvedRoleConfig | null;
    saveGlobalConfig(config: GlobalConfig): void;
    saveSessionConfig(config: SessionConfig): void;
    saveRoleConfig(role: 'main' | 'coder' | 'sub', config: RoleConfig): void;
    /**
     * 解析配置，按优先级合并：Role > Session > Global
     */
    resolveConfig(sessionId?: string, role?: 'main' | 'coder' | 'sub'): ResolvedRuntimeConfig;
    onConfigChange(callback: (config: ConfigHierarchy) => void): () => void;
    private resolveApiChannel;
    private notifyChange;
    private getConfigHierarchy;
    /**
     * 添加 API Channel
     */
    addApiChannel(channel: APIChannel): void;
    /**
     * 移除 API Channel
     */
    removeApiChannel(channelId: string): boolean;
    /**
     * 检测配置冲突
     */
    detectConflicts(): string[];
}
export {};
//# sourceMappingURL=ConfigManager.d.ts.map