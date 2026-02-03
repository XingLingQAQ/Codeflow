import { EventEmitter } from 'events';
import { ModelRegistry, ModelDefinition, ModelCapability } from './ModelRegistry.js';

/**
 * Agent 角色类型（扩展自 Commander）
 */
export type AgentType =
  | 'main'       // 主控 Agent
  | 'coder'      // 代码实现 Agent
  | 'research'   // 研究 Agent
  | 'check'      // 检查 Agent
  | 'dispatch'   // 调度 Agent
  | 'sub';       // 子专家 Agent

/**
 * 任务类型（用于 Coder Agent 细分）
 */
export type TaskType =
  | 'default'    // 默认
  | 'frontend'   // 前端开发
  | 'backend'    // 后端开发
  | 'algorithm'  // 算法实现
  | 'debug'      // 调试
  | 'refactor';  // 重构

/**
 * Agent 模型配置
 */
export interface AgentModelConfig {
  agent: AgentType;
  modelId: string;
  fallbackModelId?: string;
  requiredCapabilities?: ModelCapability[];
  taskTypeOverrides?: Partial<Record<TaskType, string>>;
  description?: string;
}

/**
 * Agent 模型映射事件
 */
export interface AgentModelMappingEvents {
  'mapping:changed': (agent: AgentType, modelId: string, taskType?: TaskType) => void;
  'mapping:reset': () => void;
}

/**
 * 默认 Agent 模型映射
 * 基于成本优化原则：
 * - Main Agent 使用高端模型（决策）
 * - Coder Agent 使用低成本模型（代码生成）
 * - Research Agent 使用中端模型（研究）
 * - Check Agent 使用高端模型（审查）
 * - Dispatch Agent 使用最低成本模型（路由）
 */
const DEFAULT_AGENT_MAPPINGS: Record<AgentType, AgentModelConfig> = {
  main: {
    agent: 'main',
    modelId: 'claude-opus-4',
    fallbackModelId: 'claude-sonnet-4',
    requiredCapabilities: ['reasoning'],
    description: '主控 Agent 需要最强推理能力进行决策',
  },
  coder: {
    agent: 'coder',
    modelId: 'claude-haiku-4',
    fallbackModelId: 'gemini-2.0-flash',
    requiredCapabilities: ['coding'],
    taskTypeOverrides: {
      frontend: 'gemini-2.0-flash',
      backend: 'claude-haiku-4',
      algorithm: 'codex-mini',
      debug: 'claude-sonnet-4',
      refactor: 'claude-sonnet-4',
    },
    description: '代码实现 Agent 使用低成本模型，按任务类型细分',
  },
  research: {
    agent: 'research',
    modelId: 'claude-sonnet-4',
    fallbackModelId: 'gemini-2.5-pro',
    requiredCapabilities: ['research'],
    description: '研究 Agent 需要广泛知识和分析能力',
  },
  check: {
    agent: 'check',
    modelId: 'claude-opus-4',
    fallbackModelId: 'claude-sonnet-4',
    requiredCapabilities: ['review'],
    description: '检查 Agent 需要高质量审查能力',
  },
  dispatch: {
    agent: 'dispatch',
    modelId: 'claude-haiku-4',
    fallbackModelId: 'gpt-4o-mini',
    requiredCapabilities: ['coding'],
    description: '调度 Agent 使用最低成本模型进行快速路由',
  },
  sub: {
    agent: 'sub',
    modelId: 'claude-sonnet-4',
    fallbackModelId: 'gemini-2.5-pro',
    requiredCapabilities: ['reasoning'],
    description: '子专家 Agent 需要特定领域知识',
  },
};

/**
 * 所有 Agent 类型列表
 */
export const ALL_AGENTS: AgentType[] = [
  'main',
  'coder',
  'research',
  'check',
  'dispatch',
  'sub',
];

/**
 * 所有任务类型列表
 */
export const ALL_TASK_TYPES: TaskType[] = [
  'default',
  'frontend',
  'backend',
  'algorithm',
  'debug',
  'refactor',
];

/**
 * Agent 模型映射管理器
 * 管理各 Agent 使用的模型配置
 */
export class AgentModelMapping extends EventEmitter {
  private mappings: Map<AgentType, AgentModelConfig> = new Map();
  private modelRegistry: ModelRegistry;

  constructor(modelRegistry: ModelRegistry) {
    super();
    this.modelRegistry = modelRegistry;
    this.loadDefaultMappings();
  }

  /**
   * 加载默认映射
   */
  private loadDefaultMappings(): void {
    for (const agent of ALL_AGENTS) {
      const defaultConfig = DEFAULT_AGENT_MAPPINGS[agent];
      this.mappings.set(agent, {
        ...defaultConfig,
        taskTypeOverrides: defaultConfig.taskTypeOverrides
          ? { ...defaultConfig.taskTypeOverrides }
          : undefined,
      });
    }
  }

  /**
   * 获取 Agent 的模型配置
   */
  getAgentConfig(agent: AgentType): AgentModelConfig | undefined {
    const config = this.mappings.get(agent);
    return config ? { ...config } : undefined;
  }

  /**
   * 获取 Agent 使用的模型 ID
   */
  getModelForAgent(agent: AgentType, taskType?: TaskType): string | undefined {
    const config = this.mappings.get(agent);
    if (!config) return undefined;

    // 检查任务类型覆盖
    if (taskType && taskType !== 'default' && config.taskTypeOverrides) {
      const overrideModelId = config.taskTypeOverrides[taskType];
      if (overrideModelId && this.modelRegistry.hasModel(overrideModelId)) {
        return overrideModelId;
      }
    }

    // 检查主模型是否可用
    if (this.modelRegistry.hasModel(config.modelId)) {
      return config.modelId;
    }

    // 使用备用模型
    if (config.fallbackModelId && this.modelRegistry.hasModel(config.fallbackModelId)) {
      return config.fallbackModelId;
    }

    return undefined;
  }

  /**
   * 获取 Agent 使用的模型定义
   */
  getModelDefinitionForAgent(agent: AgentType, taskType?: TaskType): ModelDefinition | undefined {
    const modelId = this.getModelForAgent(agent, taskType);
    if (!modelId) return undefined;
    return this.modelRegistry.getModel(modelId);
  }

  /**
   * 设置 Agent 的模型
   */
  setModelForAgent(agent: AgentType, modelId: string): boolean {
    // 验证模型是否存在
    if (!this.modelRegistry.hasModel(modelId)) {
      return false;
    }

    // 验证模型是否满足 Agent 要求的能力
    const config = this.mappings.get(agent);
    if (config?.requiredCapabilities) {
      const model = this.modelRegistry.getModel(modelId);
      if (model) {
        const hasRequiredCapabilities = config.requiredCapabilities.every(
          cap => model.capabilities.includes(cap)
        );
        if (!hasRequiredCapabilities) {
          return false;
        }
      }
    }

    // 更新映射
    const existingConfig = this.mappings.get(agent);
    if (existingConfig) {
      existingConfig.modelId = modelId;
      this.mappings.set(agent, existingConfig);
    } else {
      this.mappings.set(agent, {
        agent,
        modelId,
      });
    }

    this.emit('mapping:changed', agent, modelId);
    return true;
  }

  /**
   * 设置 Agent 的任务类型覆盖模型
   */
  setTaskTypeOverride(agent: AgentType, taskType: TaskType, modelId: string): boolean {
    if (!this.modelRegistry.hasModel(modelId)) {
      return false;
    }

    const config = this.mappings.get(agent);
    if (!config) return false;

    if (!config.taskTypeOverrides) {
      config.taskTypeOverrides = {};
    }

    config.taskTypeOverrides[taskType] = modelId;
    this.mappings.set(agent, config);

    this.emit('mapping:changed', agent, modelId, taskType);
    return true;
  }

  /**
   * 移除任务类型覆盖
   */
  removeTaskTypeOverride(agent: AgentType, taskType: TaskType): boolean {
    const config = this.mappings.get(agent);
    if (!config?.taskTypeOverrides) return false;

    delete config.taskTypeOverrides[taskType];
    this.mappings.set(agent, config);
    return true;
  }

  /**
   * 获取 Agent 的所有任务类型覆盖
   */
  getTaskTypeOverrides(agent: AgentType): Partial<Record<TaskType, string>> | undefined {
    const config = this.mappings.get(agent);
    return config?.taskTypeOverrides ? { ...config.taskTypeOverrides } : undefined;
  }

  /**
   * 设置 Agent 的备用模型
   */
  setFallbackModelForAgent(agent: AgentType, modelId: string): boolean {
    if (!this.modelRegistry.hasModel(modelId)) {
      return false;
    }

    const config = this.mappings.get(agent);
    if (config) {
      config.fallbackModelId = modelId;
      this.mappings.set(agent, config);
      return true;
    }

    return false;
  }

  /**
   * 获取所有 Agent 的映射配置
   */
  getAllMappings(): AgentModelConfig[] {
    return Array.from(this.mappings.values()).map(config => ({ ...config }));
  }

  /**
   * 获取所有 Agent 的模型 ID 映射
   */
  getMappingsAsRecord(): Record<AgentType, string> {
    const result: Partial<Record<AgentType, string>> = {};
    for (const agent of ALL_AGENTS) {
      const modelId = this.getModelForAgent(agent);
      if (modelId) {
        result[agent] = modelId;
      }
    }
    return result as Record<AgentType, string>;
  }

  /**
   * 批量设置映射
   */
  setMappings(mappings: Partial<Record<AgentType, string>>): void {
    for (const [agent, modelId] of Object.entries(mappings)) {
      this.setModelForAgent(agent as AgentType, modelId);
    }
  }

  /**
   * 重置为默认映射
   */
  reset(): void {
    this.mappings.clear();
    this.loadDefaultMappings();
    this.emit('mapping:reset');
  }

  /**
   * 验证所有映射是否有效
   */
  validateMappings(): { valid: boolean; errors: string[] } {
    const errors: string[] = [];

    for (const agent of ALL_AGENTS) {
      const config = this.mappings.get(agent);
      if (!config) {
        errors.push(`Missing mapping for agent: ${agent}`);
        continue;
      }

      // 检查主模型
      if (!this.modelRegistry.hasModel(config.modelId)) {
        errors.push(`Model not found for agent ${agent}: ${config.modelId}`);
      }

      // 检查备用模型
      if (config.fallbackModelId && !this.modelRegistry.hasModel(config.fallbackModelId)) {
        errors.push(`Fallback model not found for agent ${agent}: ${config.fallbackModelId}`);
      }

      // 检查任务类型覆盖
      if (config.taskTypeOverrides) {
        for (const [taskType, modelId] of Object.entries(config.taskTypeOverrides)) {
          if (!this.modelRegistry.hasModel(modelId)) {
            errors.push(
              `Task type override model not found for agent ${agent}, task ${taskType}: ${modelId}`
            );
          }
        }
      }

      // 检查能力要求
      if (config.requiredCapabilities) {
        const model = this.modelRegistry.getModel(config.modelId);
        if (model) {
          for (const cap of config.requiredCapabilities) {
            if (!model.capabilities.includes(cap)) {
              errors.push(
                `Model ${config.modelId} missing required capability '${cap}' for agent ${agent}`
              );
            }
          }
        }
      }
    }

    return {
      valid: errors.length === 0,
      errors,
    };
  }

  /**
   * 获取适合指定 Agent 的模型列表
   */
  getSuitableModelsForAgent(agent: AgentType): ModelDefinition[] {
    const config = this.mappings.get(agent);
    if (!config?.requiredCapabilities) {
      return this.modelRegistry.getAvailableModels();
    }

    return this.modelRegistry.getAvailableModels({
      capabilities: config.requiredCapabilities,
    });
  }

  /**
   * 估算所有 Agent 的总成本
   */
  estimateTotalCost(tokensPerAgent: number = 10000): number {
    let totalCost = 0;

    for (const agent of ALL_AGENTS) {
      const model = this.getModelDefinitionForAgent(agent);
      if (model) {
        const cost = this.modelRegistry.estimateCost(
          model.id,
          tokensPerAgent,
          tokensPerAgent / 2
        );
        if (cost !== undefined) {
          totalCost += cost;
        }
      }
    }

    return totalCost;
  }

  /**
   * 获取成本最优的映射建议
   */
  getCostOptimizedMappings(): Record<AgentType, string> {
    const result: Partial<Record<AgentType, string>> = {};

    for (const agent of ALL_AGENTS) {
      const config = this.mappings.get(agent);
      const suitableModels = this.getSuitableModelsForAgent(agent);

      if (suitableModels.length > 0) {
        const cheapest = suitableModels.reduce((min, current) => {
          const minCost = min.costPerMToken.input + min.costPerMToken.output;
          const currentCost = current.costPerMToken.input + current.costPerMToken.output;
          return currentCost < minCost ? current : min;
        });
        result[agent] = cheapest.id;
      } else if (config) {
        result[agent] = config.modelId;
      }
    }

    return result as Record<AgentType, string>;
  }

  /**
   * 获取质量最优的映射建议
   */
  getQualityOptimizedMappings(): Record<AgentType, string> {
    const result: Partial<Record<AgentType, string>> = {};

    for (const agent of ALL_AGENTS) {
      const config = this.mappings.get(agent);
      const suitableModels = this.getSuitableModelsForAgent(agent);

      if (suitableModels.length > 0) {
        const best = suitableModels.reduce((max, current) => {
          return current.capabilities.length > max.capabilities.length ? current : max;
        });
        result[agent] = best.id;
      } else if (config) {
        result[agent] = config.modelId;
      }
    }

    return result as Record<AgentType, string>;
  }

  /**
   * 导出配置
   */
  exportConfig(): AgentModelConfig[] {
    return this.getAllMappings();
  }

  /**
   * 导入配置
   */
  importConfig(configs: AgentModelConfig[]): void {
    for (const config of configs) {
      if (ALL_AGENTS.includes(config.agent)) {
        this.mappings.set(config.agent, { ...config });
      }
    }
  }

  /**
   * 获取 Coder Agent 的完整任务类型映射
   */
  getCoderTaskTypeMappings(): Record<TaskType, string> {
    const config = this.mappings.get('coder');
    const result: Partial<Record<TaskType, string>> = {};

    for (const taskType of ALL_TASK_TYPES) {
      const modelId = this.getModelForAgent('coder', taskType);
      if (modelId) {
        result[taskType] = modelId;
      }
    }

    return result as Record<TaskType, string>;
  }
}
