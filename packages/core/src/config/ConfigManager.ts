import { EventEmitter } from 'events';
import {
  IConfigManager,
  GlobalConfig,
  SessionConfig,
  RoleConfig,
  ConfigHierarchy,
  ResolvedConfig,
  APIChannel,
} from './types.js';

type RuntimeMetadata = {
  answerStyle?: string;
  capabilities?: string[];
  allowedSkills?: string[];
  allowedHooks?: string[];
};

type ResolvedRoleConfig = RoleConfig & RuntimeMetadata;
type ResolvedRuntimeConfig = ResolvedConfig & RuntimeMetadata;

/**
 * 默认全局配置
 */
const DEFAULT_GLOBAL_CONFIG: GlobalConfig = {
  defaultModel: 'claude-3-5-sonnet-20241022',
  apiPool: [],
  publicMcp: [],
  summaryThreshold: 20000,
  maxRetries: 3,
  timeout: 60000,
};

/**
 * 默认角色配置
 */
const DEFAULT_ROLE_CONFIGS: Record<'main' | 'coder' | 'sub', ResolvedRoleConfig> = {
  main: {
    model: 'claude-3-5-sonnet-20241022',
    temperature: 1.0,
    apiChannel: 'default',
    mcpTools: ['orchestrator'],
    systemPrompt: 'You are the main AI commander.',
    answerStyle: 'balanced',
  },
  coder: {
    model: 'claude-3-5-sonnet-20241022',
    temperature: 0.7,
    apiChannel: 'default',
    mcpTools: ['filesystem', 'linter'],
    systemPrompt: 'You are a code implementation expert.',
    answerStyle: 'concise',
    capabilities: ['code'],
  },
  sub: {
    model: 'claude-3-5-haiku-20241022',
    temperature: 0.8,
    apiChannel: 'default',
    mcpTools: ['websearch'],
    systemPrompt: 'You are a research assistant.',
    answerStyle: 'concise',
  },
};

/**
 * 配置管理器实现
 * 支持三级配置继承：Global → Session → Role
 */
export class ConfigManager extends EventEmitter implements IConfigManager {
  private globalConfig: GlobalConfig;
  private sessionConfigs: Map<string, SessionConfig> = new Map();
  private roleConfigs: Map<string, ResolvedRoleConfig> = new Map();
  private changeCallbacks: Set<(config: ConfigHierarchy) => void> = new Set();

  constructor(initialConfig?: Partial<GlobalConfig>) {
    super();
    this.globalConfig = { ...DEFAULT_GLOBAL_CONFIG, ...initialConfig };
  }

  // ==================== 配置加载 ====================

  loadGlobalConfig(): GlobalConfig {
    return { ...this.globalConfig };
  }

  loadSessionConfig(sessionId: string): SessionConfig | null {
    const config = this.sessionConfigs.get(sessionId);
    return config ? { ...config } : null;
  }

  loadRoleConfig(role: 'main' | 'coder' | 'sub'): ResolvedRoleConfig | null {
    const config = this.roleConfigs.get(role);
    return config ? { ...config } : { ...DEFAULT_ROLE_CONFIGS[role] };
  }

  // ==================== 配置保存 ====================

  saveGlobalConfig(config: GlobalConfig): void {
    this.globalConfig = { ...config };
    this.notifyChange();
  }

  saveSessionConfig(config: SessionConfig): void {
    this.sessionConfigs.set(config.sessionId, { ...config });
    this.notifyChange();
  }

  saveRoleConfig(role: 'main' | 'coder' | 'sub', config: RoleConfig): void {
    this.roleConfigs.set(role, { ...config } as ResolvedRoleConfig);
    this.notifyChange();
  }

  // ==================== 配置解析（三级继承） ====================

  /**
   * 解析配置，按优先级合并：Role > Session > Global
   */
  resolveConfig(sessionId?: string, role?: 'main' | 'coder' | 'sub'): ResolvedRuntimeConfig {
    // 1. 从 Global 开始
    let model = this.globalConfig.defaultModel;
    let temperature = 1.0;
    let topP: number | undefined;
    let maxTokens: number | undefined;
    let mcpTools: string[] = [...this.globalConfig.publicMcp];
    let systemPrompt: string | undefined;
    let answerStyle: string | undefined;
    let capabilities: string[] | undefined;
    let allowedSkills: string[] | undefined;
    let allowedHooks: string[] | undefined;
    let apiChannelId = 'default';

    // 2. 应用 Session 配置
    if (sessionId) {
      const sessionConfig = this.sessionConfigs.get(sessionId);
      if (sessionConfig) {
        if (sessionConfig.overrideModel) {
          model = sessionConfig.overrideModel;
        }
        if (sessionConfig.temperature !== undefined) {
          temperature = sessionConfig.temperature;
        }
        if (sessionConfig.maxTokens !== undefined) {
          maxTokens = sessionConfig.maxTokens;
        }
      }
    }

    // 3. 应用 Role 配置（最高优先级）
    if (role) {
      const roleConfig = this.roleConfigs.get(role) || DEFAULT_ROLE_CONFIGS[role];
      model = roleConfig.model;
      temperature = roleConfig.temperature;
      topP = roleConfig.topP;
      apiChannelId = roleConfig.apiChannel;
      mcpTools = [...mcpTools, ...roleConfig.mcpTools];
      systemPrompt = roleConfig.systemPrompt;
      answerStyle = roleConfig.answerStyle;
      capabilities = roleConfig.capabilities ? [...roleConfig.capabilities] : undefined;
      allowedSkills = roleConfig.allowedSkills ? [...roleConfig.allowedSkills] : undefined;
      allowedHooks = roleConfig.allowedHooks ? [...roleConfig.allowedHooks] : undefined;
    }

    // 4. 解析 API Channel
    const apiChannel = this.resolveApiChannel(apiChannelId);

    return {
      model,
      temperature,
      topP,
      maxTokens,
      apiChannel,
      mcpTools: [...new Set(mcpTools)], // 去重
      systemPrompt,
      answerStyle,
      capabilities,
      allowedSkills,
      allowedHooks,
      timeout: this.globalConfig.timeout,
      maxRetries: this.globalConfig.maxRetries,
    };
  }

  // ==================== 配置监听 ====================

  onConfigChange(callback: (config: ConfigHierarchy) => void): () => void {
    this.changeCallbacks.add(callback);
    return () => {
      this.changeCallbacks.delete(callback);
    };
  }

  // ==================== 辅助方法 ====================

  private resolveApiChannel(channelId: string): APIChannel | undefined {
    if (channelId === 'default') {
      return this.globalConfig.apiPool[0];
    }
    return this.globalConfig.apiPool.find((ch) => ch.id === channelId);
  }

  private notifyChange(): void {
    const hierarchy = this.getConfigHierarchy();
    this.changeCallbacks.forEach((cb) => cb(hierarchy));
    this.emit('change', hierarchy);
  }

  private getConfigHierarchy(): ConfigHierarchy {
    const roleConfigs: ConfigHierarchy['role'] = {};
    for (const role of ['main', 'coder', 'sub'] as const) {
      const config = this.roleConfigs.get(role);
      if (config) {
        roleConfigs[role] = { ...config };
      }
    }

    return {
      global: { ...this.globalConfig },
      role: Object.keys(roleConfigs).length > 0 ? roleConfigs : undefined,
    };
  }

  /**
   * 添加 API Channel
   */
  addApiChannel(channel: APIChannel): void {
    const existing = this.globalConfig.apiPool.findIndex((ch) => ch.id === channel.id);
    if (existing >= 0) {
      this.globalConfig.apiPool[existing] = channel;
    } else {
      this.globalConfig.apiPool.push(channel);
    }
    this.notifyChange();
  }

  /**
   * 移除 API Channel
   */
  removeApiChannel(channelId: string): boolean {
    const index = this.globalConfig.apiPool.findIndex((ch) => ch.id === channelId);
    if (index >= 0) {
      this.globalConfig.apiPool.splice(index, 1);
      this.notifyChange();
      return true;
    }
    return false;
  }

  /**
   * 检测配置冲突
   */
  detectConflicts(): string[] {
    const conflicts: string[] = [];

    // 检查 API Channel 是否存在
    for (const role of ['main', 'coder', 'sub'] as const) {
      const roleConfig = this.roleConfigs.get(role) || DEFAULT_ROLE_CONFIGS[role];
      if (roleConfig.apiChannel !== 'default') {
        const channel = this.globalConfig.apiPool.find((ch) => ch.id === roleConfig.apiChannel);
        if (!channel) {
          conflicts.push(
            `Role '${role}' references non-existent API channel '${roleConfig.apiChannel}'`
          );
        } else if (!channel.enabled) {
          conflicts.push(
            `Role '${role}' references disabled API channel '${roleConfig.apiChannel}'`
          );
        }
      }
    }

    return conflicts;
  }
}
