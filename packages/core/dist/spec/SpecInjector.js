/**
 * SpecInjector - 规范自动注入系统
 * 实现规范自动注入机制，根据任务类型选择并注入相关规范
 */
import { EventEmitter } from 'events';
const DEFAULT_CONFIG = {
    maxTokens: 8000,
    priorityOrder: ['critical', 'high', 'medium', 'low'],
    defaultDomains: ['common', 'guides'],
    enableHooks: true,
    traceInjections: true,
};
/**
 * 任务类型关键词映射
 */
const TASK_TYPE_KEYWORDS = {
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
const TASK_TYPE_DOMAINS = {
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
    analyze(input) {
        const lowerInput = input.toLowerCase();
        const foundKeywords = [];
        const typeScores = new Map();
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
                typeScores.set(type, score);
            }
        }
        // 找出最高分的任务类型
        let bestType = 'unknown';
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
        const analysis = {
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
    analyzeFromPath(filePath) {
        const pathLower = filePath.toLowerCase();
        let taskType = 'unknown';
        // 根据路径推断任务类型
        if (pathLower.includes('component') || pathLower.includes('pages') || pathLower.includes('views')) {
            taskType = 'frontend';
        }
        else if (pathLower.includes('api') || pathLower.includes('routes') || pathLower.includes('controllers')) {
            taskType = 'backend';
        }
        else if (pathLower.includes('test') || pathLower.includes('spec')) {
            taskType = 'testing';
        }
        else if (pathLower.includes('model') || pathLower.includes('schema') || pathLower.includes('migration')) {
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
    extractTags(input) {
        const tags = [];
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
    extractTagsFromPath(filePath) {
        const tags = [];
        const pathLower = filePath.toLowerCase();
        if (pathLower.endsWith('.tsx')) {
            tags.push('react');
            tags.push('typescript');
        }
        else if (pathLower.endsWith('.jsx')) {
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
    constructor(library, config = {}) {
        super();
        this.library = library;
        this.config = { ...DEFAULT_CONFIG, ...config };
    }
    /**
     * 选择相关规范
     */
    select(analysis) {
        const candidates = [];
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
    sortByPriority(specs) {
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
    applyTokenLimit(specs) {
        const result = [];
        let totalTokens = 0;
        let truncated = false;
        for (const spec of specs) {
            const specTokens = this.estimateTokens(spec);
            if (totalTokens + specTokens <= this.config.maxTokens) {
                result.push(spec);
                totalTokens += specTokens;
            }
            else {
                truncated = true;
                break;
            }
        }
        return { specs: result, truncated, totalTokens };
    }
    /**
     * 估算 Token 数量
     */
    estimateTokens(spec) {
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
    constructor(library, config = {}) {
        super();
        this.hooks = new Map();
        this.injectionHistory = [];
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
    async inject(input) {
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
        const result = {
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
    async injectFromPath(filePath) {
        const analysis = this.analyzer.analyzeFromPath(filePath);
        let specs = this.selector.select(analysis);
        if (this.config.enableHooks) {
            specs = this.applyHooks(specs);
        }
        const { specs: limitedSpecs, truncated, totalTokens } = this.selector.applyTokenLimit(specs);
        const result = {
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
    async injectManual(specIds) {
        const specs = [];
        for (const id of specIds) {
            const spec = this.library.getSpec(id);
            if (spec) {
                specs.push(spec);
            }
        }
        const { specs: limitedSpecs, truncated, totalTokens } = this.selector.applyTokenLimit(specs);
        const result = {
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
    registerHook(hook) {
        this.hooks.set(hook.id, hook);
        this.emit('hook:registered', hook);
    }
    /**
     * 移除钩子
     */
    unregisterHook(hookId) {
        const removed = this.hooks.delete(hookId);
        if (removed) {
            this.emit('hook:unregistered', hookId);
        }
        return removed;
    }
    /**
     * 获取所有钩子
     */
    getHooks() {
        return Array.from(this.hooks.values());
    }
    /**
     * 启用/禁用钩子
     */
    setHookEnabled(hookId, enabled) {
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
    applyHooks(specs) {
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
    formatInjection(result) {
        const lines = [
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
    getInjectionHistory() {
        return [...this.injectionHistory];
    }
    /**
     * 清除注入历史
     */
    clearHistory() {
        this.injectionHistory = [];
        this.emit('history:cleared');
    }
    /**
     * 更新配置
     */
    updateConfig(config) {
        this.config = { ...this.config, ...config };
        this.selector = new RelevantSpecSelector(this.library, this.config);
    }
    /**
     * 获取配置
     */
    getConfig() {
        return { ...this.config };
    }
}
//# sourceMappingURL=SpecInjector.js.map