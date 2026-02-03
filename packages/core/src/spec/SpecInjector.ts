/**
 * SpecInjector - 规范自动注入系统
 * 实现规范自动注入机制，根据任务类型选择并注入相关规范
 */

import { EventEmitter } from 'events';
import {
  SpecLibrary,
  SpecDocument,
  SpecDomain,
  SpecPriority,
} from './SpecLibrary.js';

/**
 * 任务类型
 */
export type TaskType =
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
  | 'unknown';

/**
 * 上下文分析结果
 */
export interface ContextAnalysis {
  taskType: TaskType;
  confidence: number;
  keywords: string[];
  suggestedDomains: SpecDomain[];
  suggestedTags: string[];
}

/**
 * 注入结果
 */
export interface InjectionResult {
  specs: SpecDocument[];
  totalTokens: number;
  truncated: boolean;
  analysis: ContextAnalysis;
}

/**
 * 注入钩子
 */
export interface InjectionHook {
  id: string;
  name: string;
  trigger: 'startup' | 'task' | 'file' | 'manual';
  filter?: (spec: SpecDocument) => boolean;
  priority: number;
  enabled: boolean;
}

/**
 * 注入器配置
 */
export interface SpecInjectorConfig {
  maxTokens: number;
  priorityOrder: SpecPriority[];
  defaultDomains: SpecDomain[];
  enableHooks: boolean;
  traceInjections: boolean;
}

const DEFAULT_CONFIG: SpecInjectorConfig = {
  maxTokens: 8000,
  priorityOrder: ['critical', 'high', 'medium', 'low'],
  defaultDomains: ['common', 'guides'],
  enableHooks: true,
  traceInjections: true,
};

/**
 * 任务类型关键词映射
 */
const TASK_TYPE_KEYWORDS: Record<TaskType, string[]> = {
  frontend: ['react', 'vue', 'angular', 'css', 'html', 'component', 'ui', 'ux', 'style', 'layout', 'responsive'],
  backend: ['api', 'server', 'database', 'endpoint', 'rest', 'graphql', 'middleware', 'controller', 'service'],
  fullstack: ['full-stack', 'fullstack', 'end-to-end', 'e2e'],
  api: ['api', 'rest', 'graphql', 'endpoint', 'request', 'response', 'http'],
  database: ['database', 'sql', 'query', 'migration', 'schema', 'model', 'orm', 'mongodb', 'postgresql'],
  testing: ['test', 'spec', 'unit', 'integration', 'e2e', 'mock', 'stub', 'coverage'],
  documentation: ['doc', 'readme', 'comment', 'jsdoc', 'tsdoc', 'markdown'],
  refactoring: ['refactor', 'cleanup', 'optimize', 'improve', 'restructure'],
  bugfix: ['bug', 'fix', 'issue', 'error', 'crash', 'broken'],
  feature: ['feature', 'implement', 'add', 'create', 'new'],
  unknown: [],
};

/**
 * 任务类型到领域映射
 */
const TASK_TYPE_DOMAINS: Record<TaskType, SpecDomain[]> = {
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
  unknown: ['common'],
};

/**
 * ContextAnalyzer - 上下文分析器
 */
export class ContextAnalyzer extends EventEmitter {
  /**
   * 分析任务上下文
   */
  analyze(input: string): ContextAnalysis {
    const lowerInput = input.toLowerCase();
    const foundKeywords: string[] = [];
    const typeScores: Map<TaskType, number> = new Map();

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
        typeScores.set(type as TaskType, score);
      }
    }

    // 找出最高分的任务类型
    let bestType: TaskType = 'unknown';
    let bestScore = 0;
    for (const [type, score] of typeScores) {
      if (score > bestScore) {
        bestScore = score;
        bestType = type;
      }
    }

    // 计算置信度
    const totalKeywords = Object.values(TASK_TYPE_KEYWORDS).flat().length;
    const confidence = bestScore > 0 ? Math.min(bestScore / 5, 1) : 0;

    // 获取建议的领域
    const suggestedDomains = TASK_TYPE_DOMAINS[bestType] || ['common'];

    // 提取可能的标签
    const suggestedTags = this.extractTags(input);

    const analysis: ContextAnalysis = {
      taskType: bestType,
      confidence,
      keywords: [...new Set(foundKeywords)],
      suggestedDomains,
      suggestedTags,
    };

    this.emit('analysis:complete', analysis);
    return analysis;
  }

  /**
   * 从文件路径分析
   */
  analyzeFromPath(filePath: string): ContextAnalysis {
    const pathLower = filePath.toLowerCase();
    let taskType: TaskType = 'unknown';

    // 根据路径推断任务类型
    if (pathLower.includes('component') || pathLower.includes('pages') || pathLower.includes('views')) {
      taskType = 'frontend';
    } else if (pathLower.includes('api') || pathLower.includes('routes') || pathLower.includes('controllers')) {
      taskType = 'backend';
    } else if (pathLower.includes('test') || pathLower.includes('spec')) {
      taskType = 'testing';
    } else if (pathLower.includes('model') || pathLower.includes('schema') || pathLower.includes('migration')) {
      taskType = 'database';
    }

    return {
      taskType,
      confidence: taskType !== 'unknown' ? 0.7 : 0,
      keywords: [],
      suggestedDomains: TASK_TYPE_DOMAINS[taskType],
      suggestedTags: this.extractTagsFromPath(filePath),
    };
  }

  /**
   * 提取标签
   */
  private extractTags(input: string): string[] {
    const tags: string[] = [];
    const tagPatterns = [
      /\b(react|vue|angular|svelte)\b/gi,
      /\b(typescript|javascript|python|go|rust)\b/gi,
      /\b(api|rest|graphql)\b/gi,
      /\b(test|testing|unit|integration)\b/gi,
    ];

    for (const pattern of tagPatterns) {
      const matches = input.match(pattern);
      if (matches) {
        tags.push(...matches.map(m => m.toLowerCase()));
      }
    }

    return [...new Set(tags)];
  }

  /**
   * 从路径提取标签
   */
  private extractTagsFromPath(filePath: string): string[] {
    const tags: string[] = [];
    const pathLower = filePath.toLowerCase();

    if (pathLower.endsWith('.tsx')) {
      tags.push('react');
      tags.push('typescript');
    } else if (pathLower.endsWith('.jsx')) {
      tags.push('react');
    }
    if (pathLower.endsWith('.vue')) {
      tags.push('vue');
    }
    if (pathLower.endsWith('.ts') && !pathLower.endsWith('.d.ts')) {
      tags.push('typescript');
    }
    if (pathLower.includes('.test.') || pathLower.includes('.spec.')) {
      tags.push('testing');
    }

    return tags;
  }
}

/**
 * RelevantSpecSelector - 相关规范选择器
 */
export class RelevantSpecSelector extends EventEmitter {
  private library: SpecLibrary;
  private config: SpecInjectorConfig;

  constructor(library: SpecLibrary, config: Partial<SpecInjectorConfig> = {}) {
    super();
    this.library = library;
    this.config = { ...DEFAULT_CONFIG, ...config };
  }

  /**
   * 选择相关规范
   */
  select(analysis: ContextAnalysis): SpecDocument[] {
    const candidates: SpecDocument[] = [];

    // 1. 按领域获取规范
    for (const domain of analysis.suggestedDomains) {
      candidates.push(...this.library.getSpecsByDomain(domain));
    }

    // 2. 按标签获取规范
    for (const tag of analysis.suggestedTags) {
      const tagSpecs = this.library.getSpecsByTag(tag);
      for (const spec of tagSpecs) {
        if (!candidates.find(c => c.metadata.id === spec.metadata.id)) {
          candidates.push(spec);
        }
      }
    }

    // 3. 添加默认领域的规范
    for (const domain of this.config.defaultDomains) {
      if (!analysis.suggestedDomains.includes(domain)) {
        const defaultSpecs = this.library.getSpecsByDomain(domain);
        for (const spec of defaultSpecs) {
          if (!candidates.find(c => c.metadata.id === spec.metadata.id)) {
            candidates.push(spec);
          }
        }
      }
    }

    // 4. 按优先级排序
    const sorted = this.sortByPriority(candidates);

    this.emit('selection:complete', { count: sorted.length, analysis });
    return sorted;
  }

  /**
   * 按优先级排序
   */
  private sortByPriority(specs: SpecDocument[]): SpecDocument[] {
    const priorityOrder = this.config.priorityOrder;

    return specs.sort((a, b) => {
      const aIndex = priorityOrder.indexOf(a.metadata.priority);
      const bIndex = priorityOrder.indexOf(b.metadata.priority);
      return aIndex - bIndex;
    });
  }

  /**
   * 应用 Token 限制
   */
  applyTokenLimit(specs: SpecDocument[]): { specs: SpecDocument[]; truncated: boolean; totalTokens: number } {
    const result: SpecDocument[] = [];
    let totalTokens = 0;
    let truncated = false;

    for (const spec of specs) {
      const specTokens = this.estimateTokens(spec);

      if (totalTokens + specTokens <= this.config.maxTokens) {
        result.push(spec);
        totalTokens += specTokens;
      } else {
        truncated = true;
        break;
      }
    }

    return { specs: result, truncated, totalTokens };
  }

  /**
   * 估算 Token 数量
   */
  private estimateTokens(spec: SpecDocument): number {
    // 简单估算：每 4 个字符约 1 个 token
    const contentLength = spec.content.length;
    const metadataLength = JSON.stringify(spec.metadata).length;
    return Math.ceil((contentLength + metadataLength) / 4);
  }
}

/**
 * SpecInjector - 规范注入器
 */
export class SpecInjector extends EventEmitter {
  private library: SpecLibrary;
  private analyzer: ContextAnalyzer;
  private selector: RelevantSpecSelector;
  private config: SpecInjectorConfig;
  private hooks: Map<string, InjectionHook> = new Map();
  private injectionHistory: InjectionResult[] = [];

  constructor(library: SpecLibrary, config: Partial<SpecInjectorConfig> = {}) {
    super();
    this.library = library;
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.analyzer = new ContextAnalyzer();
    this.selector = new RelevantSpecSelector(library, this.config);

    // 转发事件
    this.analyzer.on('analysis:complete', (analysis) => this.emit('analysis:complete', analysis));
    this.selector.on('selection:complete', (data) => this.emit('selection:complete', data));
  }

  /**
   * 注入规范
   */
  async inject(input: string): Promise<InjectionResult> {
    this.emit('injection:start', { input });

    // 1. 分析上下文
    const analysis = this.analyzer.analyze(input);

    // 2. 选择相关规范
    let specs = this.selector.select(analysis);

    // 3. 应用钩子过滤
    if (this.config.enableHooks) {
      specs = this.applyHooks(specs);
    }

    // 4. 应用 Token 限制
    const { specs: limitedSpecs, truncated, totalTokens } = this.selector.applyTokenLimit(specs);

    const result: InjectionResult = {
      specs: limitedSpecs,
      totalTokens,
      truncated,
      analysis,
    };

    // 5. 记录历史
    if (this.config.traceInjections) {
      this.injectionHistory.push(result);
    }

    this.emit('injection:complete', result);
    return result;
  }

  /**
   * 从文件路径注入
   */
  async injectFromPath(filePath: string): Promise<InjectionResult> {
    const analysis = this.analyzer.analyzeFromPath(filePath);
    let specs = this.selector.select(analysis);

    if (this.config.enableHooks) {
      specs = this.applyHooks(specs);
    }

    const { specs: limitedSpecs, truncated, totalTokens } = this.selector.applyTokenLimit(specs);

    const result: InjectionResult = {
      specs: limitedSpecs,
      totalTokens,
      truncated,
      analysis,
    };

    if (this.config.traceInjections) {
      this.injectionHistory.push(result);
    }

    this.emit('injection:complete', result);
    return result;
  }

  /**
   * 手动注入指定规范
   */
  async injectManual(specIds: string[]): Promise<InjectionResult> {
    const specs: SpecDocument[] = [];

    for (const id of specIds) {
      const spec = this.library.getSpec(id);
      if (spec) {
        specs.push(spec);
      }
    }

    const { specs: limitedSpecs, truncated, totalTokens } = this.selector.applyTokenLimit(specs);

    const result: InjectionResult = {
      specs: limitedSpecs,
      totalTokens,
      truncated,
      analysis: {
        taskType: 'unknown',
        confidence: 1,
        keywords: [],
        suggestedDomains: [],
        suggestedTags: [],
      },
    };

    if (this.config.traceInjections) {
      this.injectionHistory.push(result);
    }

    this.emit('injection:complete', result);
    return result;
  }

  /**
   * 注册钩子
   */
  registerHook(hook: InjectionHook): void {
    this.hooks.set(hook.id, hook);
    this.emit('hook:registered', hook);
  }

  /**
   * 移除钩子
   */
  unregisterHook(hookId: string): boolean {
    const removed = this.hooks.delete(hookId);
    if (removed) {
      this.emit('hook:unregistered', hookId);
    }
    return removed;
  }

  /**
   * 获取所有钩子
   */
  getHooks(): InjectionHook[] {
    return Array.from(this.hooks.values());
  }

  /**
   * 启用/禁用钩子
   */
  setHookEnabled(hookId: string, enabled: boolean): boolean {
    const hook = this.hooks.get(hookId);
    if (hook) {
      hook.enabled = enabled;
      return true;
    }
    return false;
  }

  /**
   * 应用钩子过滤
   */
  private applyHooks(specs: SpecDocument[]): SpecDocument[] {
    let filtered = specs;

    // 按优先级排序钩子
    const sortedHooks = Array.from(this.hooks.values())
      .filter(h => h.enabled && h.filter)
      .sort((a, b) => b.priority - a.priority);

    for (const hook of sortedHooks) {
      if (hook.filter) {
        filtered = filtered.filter(hook.filter);
      }
    }

    return filtered;
  }

  /**
   * 格式化注入内容
   */
  formatInjection(result: InjectionResult): string {
    const lines: string[] = [
      '<!-- Injected Specifications -->',
      '',
    ];

    for (const spec of result.specs) {
      lines.push(`## ${spec.metadata.name}`);
      lines.push(`> Domain: ${spec.metadata.domain} | Priority: ${spec.metadata.priority}`);
      lines.push('');
      lines.push(spec.content);
      lines.push('');
      lines.push('---');
      lines.push('');
    }

    if (result.truncated) {
      lines.push('> Note: Some specifications were truncated due to token limit.');
    }

    return lines.join('\n');
  }

  /**
   * 获取注入历史
   */
  getInjectionHistory(): InjectionResult[] {
    return [...this.injectionHistory];
  }

  /**
   * 清除注入历史
   */
  clearHistory(): void {
    this.injectionHistory = [];
    this.emit('history:cleared');
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<SpecInjectorConfig>): void {
    this.config = { ...this.config, ...config };
    this.selector = new RelevantSpecSelector(this.library, this.config);
  }

  /**
   * 获取配置
   */
  getConfig(): SpecInjectorConfig {
    return { ...this.config };
  }
}
