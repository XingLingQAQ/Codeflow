/**
 * DispatchAgent - 轻量路由调度 Agent
 * 实现 Trellis 风格的 Dispatch Agent，纯路由不读规范
 */

import { EventEmitter } from 'events';

/**
 * 任务类型
 */
export type DispatchTaskType =
  | 'frontend'
  | 'backend'
  | 'fullstack'
  | 'api'
  | 'database'
  | 'testing'
  | 'documentation'
  | 'refactoring'
  | 'bugfix'
  | 'feature'
  | 'research'
  | 'review'
  | 'unknown';

/**
 * Agent 类型
 */
export type DispatchAgentType =
  | 'main'
  | 'coder'
  | 'researcher'
  | 'reviewer'
  | 'documenter'
  | 'tester';

/**
 * 模型层级
 */
export type ModelTier = 'low' | 'medium' | 'high' | 'premium';

/**
 * 分类结果
 */
export interface ClassificationResult {
  taskType: DispatchTaskType;
  confidence: number;
  keywords: string[];
  complexity: 'simple' | 'moderate' | 'complex';
  estimatedTokens: number;
}

/**
 * 模型选择结果
 */
export interface ModelSelectionResult {
  modelId: string;
  tier: ModelTier;
  reason: string;
  fallback?: string;
}

/**
 * Agent 选择结果
 */
export interface AgentSelectionResult {
  agentType: DispatchAgentType;
  reason: string;
  capabilities: string[];
}

/**
 * 规范选择结果
 */
export interface SpecSelectionResult {
  specIds: string[];
  domains: string[];
  priority: 'critical' | 'high' | 'medium' | 'low';
  estimatedTokens: number;
}

/**
 * 路由决策
 */
export interface RoutingDecision {
  id: string;
  classification: ClassificationResult;
  agent: AgentSelectionResult;
  model: ModelSelectionResult;
  specs: SpecSelectionResult;
  timestamp: number;
  latency: number;
}

/**
 * Dispatch Agent 配置
 */
export interface DispatchAgentConfig {
  defaultModel: string;
  maxLatency: number;
  enableCaching: boolean;
  cacheTimeout: number;
  modelTierMapping: Record<ModelTier, string[]>;
  taskTypeAgentMapping: Record<DispatchTaskType, DispatchAgentType>;
  taskTypeSpecDomains: Record<DispatchTaskType, string[]>;
}

const DEFAULT_CONFIG: DispatchAgentConfig = {
  defaultModel: 'claude-3-haiku',
  maxLatency: 1000,
  enableCaching: true,
  cacheTimeout: 300000, // 5 minutes
  modelTierMapping: {
    low: ['claude-3-haiku', 'gemini-flash'],
    medium: ['claude-3-5-sonnet', 'gemini-pro'],
    high: ['claude-3-opus', 'gpt-4'],
    premium: ['claude-opus-4', 'o1'],
  },
  taskTypeAgentMapping: {
    frontend: 'coder',
    backend: 'coder',
    fullstack: 'coder',
    api: 'coder',
    database: 'coder',
    testing: 'tester',
    documentation: 'documenter',
    refactoring: 'coder',
    bugfix: 'coder',
    feature: 'coder',
    research: 'researcher',
    review: 'reviewer',
    unknown: 'main',
  },
  taskTypeSpecDomains: {
    frontend: ['frontend', 'common'],
    backend: ['backend', 'common'],
    fullstack: ['frontend', 'backend', 'common'],
    api: ['backend', 'common'],
    database: ['backend', 'common'],
    testing: ['common', 'guides'],
    documentation: ['guides', 'common'],
    refactoring: ['common', 'guides'],
    bugfix: ['common'],
    feature: ['common'],
    research: ['guides'],
    review: ['common', 'guides'],
    unknown: ['common'],
  },
};

/**
 * 任务类型关键词
 */
const TASK_TYPE_KEYWORDS: Record<DispatchTaskType, string[]> = {
  frontend: ['react', 'vue', 'angular', 'css', 'html', 'component', 'ui', 'ux', 'style', 'layout', 'responsive', 'dom', 'browser'],
  backend: ['server', 'database', 'endpoint', 'middleware', 'controller', 'service', 'node', 'express', 'fastify'],
  fullstack: ['full-stack', 'fullstack', 'end-to-end', 'e2e', 'both frontend and backend'],
  api: ['api', 'rest', 'graphql', 'endpoint', 'request', 'response', 'http', 'fetch', 'axios'],
  database: ['database', 'sql', 'query', 'migration', 'schema', 'model', 'orm', 'mongodb', 'postgresql', 'mysql', 'redis'],
  testing: ['test', 'spec', 'unit', 'integration', 'e2e', 'mock', 'stub', 'coverage', 'jest', 'vitest', 'cypress'],
  documentation: ['doc', 'readme', 'comment', 'jsdoc', 'tsdoc', 'markdown', 'wiki', 'guide'],
  refactoring: ['refactor', 'cleanup', 'optimize', 'improve', 'restructure', 'simplify', 'extract'],
  bugfix: ['bug', 'fix', 'issue', 'error', 'crash', 'broken', 'not working', 'fails'],
  feature: ['feature', 'implement', 'add', 'create', 'new', 'build'],
  research: ['research', 'investigate', 'explore', 'analyze', 'compare', 'evaluate', 'study'],
  review: ['review', 'check', 'audit', 'inspect', 'verify', 'validate'],
  unknown: [],
};

/**
 * 复杂度关键词
 */
const COMPLEXITY_KEYWORDS = {
  simple: ['simple', 'quick', 'easy', 'small', 'minor', 'trivial', 'basic'],
  complex: ['complex', 'difficult', 'large', 'major', 'significant', 'comprehensive', 'extensive', 'architecture', 'system'],
};

/**
 * TaskClassifier - 任务分类器
 */
export class TaskClassifier extends EventEmitter {
  /**
   * 分类任务
   */
  classify(input: string): ClassificationResult {
    const startTime = Date.now();
    const lowerInput = input.toLowerCase();
    const foundKeywords: string[] = [];
    const typeScores: Map<DispatchTaskType, number> = new Map();

    // 计算每种任务类型的匹配分数
    for (const [type, keywords] of Object.entries(TASK_TYPE_KEYWORDS)) {
      let score = 0;
      for (const keyword of keywords) {
        if (lowerInput.includes(keyword)) {
          score++;
          foundKeywords.push(keyword);
        }
      }
      if (score > 0) {
        typeScores.set(type as DispatchTaskType, score);
      }
    }

    // 找出最高分的任务类型
    let bestType: DispatchTaskType = 'unknown';
    let bestScore = 0;
    for (const [type, score] of typeScores) {
      if (score > bestScore) {
        bestScore = score;
        bestType = type;
      }
    }

    // 计算置信度
    const confidence = bestScore > 0 ? Math.min(bestScore / 5, 1) : 0;

    // 评估复杂度
    const complexity = this.assessComplexity(lowerInput);

    // 估算 Token 数
    const estimatedTokens = Math.ceil(input.length / 4);

    const result: ClassificationResult = {
      taskType: bestType,
      confidence,
      keywords: [...new Set(foundKeywords)],
      complexity,
      estimatedTokens,
    };

    this.emit('classification:complete', { result, latency: Date.now() - startTime });

    return result;
  }

  /**
   * 评估复杂度
   */
  private assessComplexity(input: string): ClassificationResult['complexity'] {
    const hasSimpleKeywords = COMPLEXITY_KEYWORDS.simple.some(k => input.includes(k));
    const hasComplexKeywords = COMPLEXITY_KEYWORDS.complex.some(k => input.includes(k));

    if (hasComplexKeywords && !hasSimpleKeywords) {
      return 'complex';
    } else if (hasSimpleKeywords && !hasComplexKeywords) {
      return 'simple';
    }

    // 基于长度估算
    if (input.length > 500) {
      return 'complex';
    } else if (input.length < 100) {
      return 'simple';
    }

    return 'moderate';
  }
}

/**
 * ModelSelector - 模型选择器
 */
export class ModelSelector extends EventEmitter {
  private config: DispatchAgentConfig;

  constructor(config: Partial<DispatchAgentConfig> = {}) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
  }

  /**
   * 选择模型
   */
  select(classification: ClassificationResult): ModelSelectionResult {
    const tier = this.determineTier(classification);
    const models = this.config.modelTierMapping[tier];
    const modelId = models[0] || this.config.defaultModel;

    const result: ModelSelectionResult = {
      modelId,
      tier,
      reason: this.generateReason(classification, tier),
      fallback: models[1],
    };

    this.emit('model:selected', result);

    return result;
  }

  /**
   * 确定模型层级
   */
  private determineTier(classification: ClassificationResult): ModelTier {
    // 基于复杂度和任务类型决定
    if (classification.complexity === 'complex') {
      return 'high';
    }

    if (classification.complexity === 'simple') {
      return 'low';
    }

    // 特定任务类型需要更高级模型
    const highTierTasks: DispatchTaskType[] = ['research', 'review', 'refactoring'];
    if (highTierTasks.includes(classification.taskType)) {
      return 'medium';
    }

    return 'low';
  }

  /**
   * 生成选择原因
   */
  private generateReason(classification: ClassificationResult, tier: ModelTier): string {
    const reasons: string[] = [];

    reasons.push(`Task type: ${classification.taskType}`);
    reasons.push(`Complexity: ${classification.complexity}`);
    reasons.push(`Selected tier: ${tier}`);

    if (classification.confidence < 0.5) {
      reasons.push('Low confidence - using conservative model selection');
    }

    return reasons.join('; ');
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<DispatchAgentConfig>): void {
    this.config = { ...this.config, ...config };
  }
}

/**
 * AgentSelector - Agent 选择器
 */
export class AgentSelector extends EventEmitter {
  private config: DispatchAgentConfig;

  constructor(config: Partial<DispatchAgentConfig> = {}) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
  }

  /**
   * 选择 Agent
   */
  select(classification: ClassificationResult): AgentSelectionResult {
    const agentType = this.config.taskTypeAgentMapping[classification.taskType] || 'main';
    const capabilities = this.getAgentCapabilities(agentType);

    const result: AgentSelectionResult = {
      agentType,
      reason: `Best suited for ${classification.taskType} tasks`,
      capabilities,
    };

    this.emit('agent:selected', result);

    return result;
  }

  /**
   * 获取 Agent 能力
   */
  private getAgentCapabilities(agentType: DispatchAgentType): string[] {
    const capabilityMap: Record<DispatchAgentType, string[]> = {
      main: ['orchestration', 'planning', 'decision-making'],
      coder: ['code-generation', 'debugging', 'refactoring'],
      researcher: ['information-gathering', 'analysis', 'comparison'],
      reviewer: ['code-review', 'quality-assessment', 'best-practices'],
      documenter: ['documentation', 'explanation', 'examples'],
      tester: ['test-generation', 'coverage-analysis', 'bug-detection'],
    };

    return capabilityMap[agentType] || [];
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<DispatchAgentConfig>): void {
    this.config = { ...this.config, ...config };
  }
}

/**
 * SpecSelector - 规范选择器
 */
export class SpecSelector extends EventEmitter {
  private config: DispatchAgentConfig;

  constructor(config: Partial<DispatchAgentConfig> = {}) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
  }

  /**
   * 选择规范
   */
  select(classification: ClassificationResult): SpecSelectionResult {
    const domains = this.config.taskTypeSpecDomains[classification.taskType] || ['common'];
    const priority = this.determinePriority(classification);

    // 生成规范 ID（基于领域）
    const specIds = domains.map(d => `spec_${d}`);

    // 估算 Token 数
    const estimatedTokens = domains.length * 500; // 假设每个领域约 500 tokens

    const result: SpecSelectionResult = {
      specIds,
      domains,
      priority,
      estimatedTokens,
    };

    this.emit('spec:selected', result);

    return result;
  }

  /**
   * 确定优先级
   */
  private determinePriority(classification: ClassificationResult): SpecSelectionResult['priority'] {
    if (classification.complexity === 'complex') {
      return 'high';
    }

    const criticalTasks: DispatchTaskType[] = ['bugfix', 'refactoring'];
    if (criticalTasks.includes(classification.taskType)) {
      return 'high';
    }

    if (classification.complexity === 'simple') {
      return 'low';
    }

    return 'medium';
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<DispatchAgentConfig>): void {
    this.config = { ...this.config, ...config };
  }
}

/**
 * DispatchAgent - 调度 Agent
 */
export class DispatchAgent extends EventEmitter {
  private config: DispatchAgentConfig;
  private classifier: TaskClassifier;
  private modelSelector: ModelSelector;
  private agentSelector: AgentSelector;
  private specSelector: SpecSelector;
  private cache: Map<string, { decision: RoutingDecision; timestamp: number }> = new Map();
  private decisionHistory: RoutingDecision[] = [];

  constructor(config: Partial<DispatchAgentConfig> = {}) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.classifier = new TaskClassifier();
    this.modelSelector = new ModelSelector(this.config);
    this.agentSelector = new AgentSelector(this.config);
    this.specSelector = new SpecSelector(this.config);

    // 转发事件
    this.classifier.on('classification:complete', (data) => this.emit('classification:complete', data));
    this.modelSelector.on('model:selected', (data) => this.emit('model:selected', data));
    this.agentSelector.on('agent:selected', (data) => this.emit('agent:selected', data));
    this.specSelector.on('spec:selected', (data) => this.emit('spec:selected', data));
  }

  /**
   * 路由请求
   */
  async route(input: string): Promise<RoutingDecision> {
    const startTime = Date.now();

    // 检查缓存
    if (this.config.enableCaching) {
      const cached = this.getCachedDecision(input);
      if (cached) {
        this.emit('route:cached', cached);
        return cached;
      }
    }

    this.emit('route:start', { input });

    // 1. 分类任务
    const classification = this.classifier.classify(input);

    // 2. 选择 Agent
    const agent = this.agentSelector.select(classification);

    // 3. 选择模型
    const model = this.modelSelector.select(classification);

    // 4. 选择规范
    const specs = this.specSelector.select(classification);

    const latency = Date.now() - startTime;

    // 构建决策
    const decision: RoutingDecision = {
      id: `decision_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      classification,
      agent,
      model,
      specs,
      timestamp: Date.now(),
      latency,
    };

    // 缓存决策
    if (this.config.enableCaching) {
      this.cacheDecision(input, decision);
    }

    // 记录历史
    this.decisionHistory.push(decision);

    // 检查延迟
    if (latency > this.config.maxLatency) {
      this.emit('route:slow', { decision, latency, maxLatency: this.config.maxLatency });
    }

    this.emit('route:complete', decision);

    return decision;
  }

  /**
   * 获取缓存的决策
   */
  private getCachedDecision(input: string): RoutingDecision | null {
    const cacheKey = this.generateCacheKey(input);
    const cached = this.cache.get(cacheKey);

    if (cached && Date.now() - cached.timestamp < this.config.cacheTimeout) {
      return cached.decision;
    }

    return null;
  }

  /**
   * 缓存决策
   */
  private cacheDecision(input: string, decision: RoutingDecision): void {
    const cacheKey = this.generateCacheKey(input);
    this.cache.set(cacheKey, { decision, timestamp: Date.now() });
  }

  /**
   * 生成缓存键
   */
  private generateCacheKey(input: string): string {
    // 简单的哈希函数
    let hash = 0;
    for (let i = 0; i < input.length; i++) {
      const char = input.charCodeAt(i);
      hash = ((hash << 5) - hash) + char;
      hash = hash & hash;
    }
    return `cache_${hash}`;
  }

  /**
   * 清除缓存
   */
  clearCache(): void {
    this.cache.clear();
    this.emit('cache:cleared');
  }

  /**
   * 获取决策历史
   */
  getDecisionHistory(): RoutingDecision[] {
    return [...this.decisionHistory];
  }

  /**
   * 获取统计信息
   */
  getStatistics(): {
    totalDecisions: number;
    averageLatency: number;
    cacheHitRate: number;
    byTaskType: Record<DispatchTaskType, number>;
    byAgent: Record<DispatchAgentType, number>;
  } {
    const totalDecisions = this.decisionHistory.length;
    const averageLatency = totalDecisions > 0
      ? this.decisionHistory.reduce((sum, d) => sum + d.latency, 0) / totalDecisions
      : 0;

    const byTaskType: Record<DispatchTaskType, number> = {} as Record<DispatchTaskType, number>;
    const byAgent: Record<DispatchAgentType, number> = {} as Record<DispatchAgentType, number>;

    for (const decision of this.decisionHistory) {
      byTaskType[decision.classification.taskType] = (byTaskType[decision.classification.taskType] || 0) + 1;
      byAgent[decision.agent.agentType] = (byAgent[decision.agent.agentType] || 0) + 1;
    }

    return {
      totalDecisions,
      averageLatency,
      cacheHitRate: 0, // 需要额外追踪
      byTaskType,
      byAgent,
    };
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<DispatchAgentConfig>): void {
    this.config = { ...this.config, ...config };
    this.modelSelector.updateConfig(this.config);
    this.agentSelector.updateConfig(this.config);
    this.specSelector.updateConfig(this.config);
    this.emit('config:updated', this.config);
  }

  /**
   * 获取配置
   */
  getConfig(): DispatchAgentConfig {
    return { ...this.config };
  }

  /**
   * 重置
   */
  reset(): void {
    this.cache.clear();
    this.decisionHistory = [];
    this.emit('dispatch:reset');
  }
}
