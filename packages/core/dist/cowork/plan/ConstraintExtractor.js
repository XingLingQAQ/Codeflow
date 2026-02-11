/**
 * ConstraintExtractor - 约束集提取器
 * 来自 OpenSpec 的约束驱动能力
 */
import { EventEmitter } from 'events';
/**
 * 默认配置
 */
const DEFAULT_CONFIG = {
    minConstraints: 3,
    maxConstraints: 50,
    requireVerifiable: false,
};
/**
 * 约束关键词映射
 */
const CONSTRAINT_KEYWORDS = {
    functional: ['must', 'should', 'shall', 'need', 'require', 'support', 'allow', 'enable'],
    technical: ['use', 'implement', 'integrate', 'compatible', 'framework', 'library', 'api', 'protocol'],
    performance: ['fast', 'quick', 'latency', 'throughput', 'response time', 'load', 'scale', 'concurrent'],
    security: ['secure', 'encrypt', 'auth', 'permission', 'access', 'protect', 'validate', 'sanitize'],
    compatibility: ['backward', 'forward', 'cross-platform', 'browser', 'version', 'legacy', 'migrate'],
    resource: ['budget', 'time', 'cost', 'memory', 'cpu', 'storage', 'bandwidth', 'limit'],
};
/**
 * 优先级关键词映射
 */
const PRIORITY_KEYWORDS = {
    must: ['must', 'required', 'mandatory', 'critical', 'essential', 'necessary'],
    should: ['should', 'recommended', 'important', 'preferred', 'expected'],
    could: ['could', 'nice to have', 'optional', 'desirable', 'consider'],
    wont: ['wont', 'will not', 'out of scope', 'excluded', 'not supported'],
};
/**
 * ConstraintExtractor - 约束提取器
 */
export class ConstraintExtractor extends EventEmitter {
    constructor(config = {}) {
        super();
        this.config = { ...DEFAULT_CONFIG, ...config };
    }
    /**
     * 从愿景文档提取约束集
     */
    extract(vision) {
        const constraints = [];
        let constraintCounter = 0;
        // 从目标提取功能约束
        for (const goal of vision.goals) {
            const extracted = this.extractFromText(goal, ++constraintCounter);
            constraints.push(...extracted);
        }
        // 从范围提取约束
        for (const included of vision.scope.included) {
            const extracted = this.extractFromText(included, ++constraintCounter, 'functional');
            constraints.push(...extracted);
        }
        // 从排除范围提取 "wont" 约束
        for (const excluded of vision.scope.excluded) {
            constraints.push({
                id: `constraint-${++constraintCounter}`,
                type: 'functional',
                priority: 'wont',
                description: excluded,
                source: 'scope.excluded',
                verifiable: false,
            });
        }
        // 从显式约束提取
        for (const constraint of vision.constraints) {
            const extracted = this.extractFromText(constraint, ++constraintCounter);
            constraints.push(...extracted);
        }
        // 从优先级提取
        for (const priority of vision.priorities) {
            const extracted = this.extractFromText(priority, ++constraintCounter);
            for (const c of extracted) {
                // 优先级文本中的约束通常是 must/should
                if (priority.toLowerCase().includes('must') || priority.toLowerCase().includes('必须')) {
                    c.priority = 'must';
                }
            }
            constraints.push(...extracted);
        }
        // 从风险提取安全/性能约束
        for (const risk of vision.risks) {
            const type = this.inferConstraintType(risk);
            if (type === 'security' || type === 'performance') {
                constraints.push({
                    id: `constraint-${++constraintCounter}`,
                    type,
                    priority: 'should',
                    description: `Mitigate risk: ${risk}`,
                    source: 'risks',
                    verifiable: true,
                    verificationCriteria: `Risk "${risk}" is addressed`,
                });
            }
        }
        // 去重
        const uniqueConstraints = this.deduplicateConstraints(constraints);
        // 限制数量
        const finalConstraints = uniqueConstraints.slice(0, this.config.maxConstraints);
        // 验证最小数量
        if (finalConstraints.length < this.config.minConstraints) {
            // 添加默认约束
            const defaults = this.generateDefaultConstraints(constraintCounter);
            finalConstraints.push(...defaults.slice(0, this.config.minConstraints - finalConstraints.length));
        }
        const constraintSet = {
            id: `constraints-${Date.now()}`,
            visionId: vision.id,
            constraints: finalConstraints,
            createdAt: Date.now(),
            updatedAt: Date.now(),
        };
        this.emitEvent({ type: 'constraints:extracted', constraints: constraintSet });
        return constraintSet;
    }
    /**
     * 从文本提取约束
     */
    extractFromText(text, counter, defaultType) {
        const constraints = [];
        const sentences = this.splitIntoSentences(text);
        for (const sentence of sentences) {
            if (sentence.trim().length < 10)
                continue;
            const type = defaultType || this.inferConstraintType(sentence);
            const priority = this.inferPriority(sentence);
            const verifiable = this.isVerifiable(sentence);
            constraints.push({
                id: `constraint-${counter}-${constraints.length}`,
                type,
                priority,
                description: sentence.trim(),
                verifiable,
                verificationCriteria: verifiable ? this.generateVerificationCriteria(sentence) : undefined,
            });
        }
        return constraints;
    }
    /**
     * 分割成句子
     */
    splitIntoSentences(text) {
        // 按句号、分号、换行分割
        return text
            .split(/[.;。；\n]/)
            .map(s => s.trim())
            .filter(s => s.length > 0);
    }
    /**
     * 推断约束类型
     */
    inferConstraintType(text) {
        const lower = text.toLowerCase();
        for (const [type, keywords] of Object.entries(CONSTRAINT_KEYWORDS)) {
            for (const keyword of keywords) {
                if (lower.includes(keyword)) {
                    return type;
                }
            }
        }
        return 'functional';
    }
    /**
     * 推断优先级
     */
    inferPriority(text) {
        const lower = text.toLowerCase();
        for (const [priority, keywords] of Object.entries(PRIORITY_KEYWORDS)) {
            for (const keyword of keywords) {
                if (lower.includes(keyword)) {
                    return priority;
                }
            }
        }
        return 'should';
    }
    /**
     * 判断是否可验证
     */
    isVerifiable(text) {
        const verifiablePatterns = [
            /\d+/, // 包含数字
            /less than|more than|at least|at most/i,
            /within \d+/i,
            /support(s)? \d+/i,
            /handle(s)? \d+/i,
            /response time/i,
            /latency/i,
            /throughput/i,
            /availability/i,
            /uptime/i,
        ];
        return verifiablePatterns.some(pattern => pattern.test(text));
    }
    /**
     * 生成验证标准
     */
    generateVerificationCriteria(text) {
        // 提取数字和单位
        const numberMatch = text.match(/(\d+)\s*(ms|s|%|MB|GB|requests?|users?|concurrent)?/i);
        if (numberMatch) {
            return `Verify that the metric meets: ${numberMatch[0]}`;
        }
        return `Verify that: ${text}`;
    }
    /**
     * 去重约束
     */
    deduplicateConstraints(constraints) {
        const seen = new Set();
        const unique = [];
        for (const constraint of constraints) {
            const key = constraint.description.toLowerCase().trim();
            if (!seen.has(key)) {
                seen.add(key);
                unique.push(constraint);
            }
        }
        return unique;
    }
    /**
     * 生成默认约束
     */
    generateDefaultConstraints(startCounter) {
        return [
            {
                id: `constraint-${startCounter + 1}`,
                type: 'functional',
                priority: 'must',
                description: 'The system must be functional and meet the stated goals',
                verifiable: true,
                verificationCriteria: 'All acceptance criteria are met',
            },
            {
                id: `constraint-${startCounter + 2}`,
                type: 'technical',
                priority: 'should',
                description: 'The implementation should follow existing code patterns',
                verifiable: false,
            },
            {
                id: `constraint-${startCounter + 3}`,
                type: 'compatibility',
                priority: 'must',
                description: 'Changes must not break existing functionality',
                verifiable: true,
                verificationCriteria: 'All existing tests pass',
            },
        ];
    }
    /**
     * 手动添加约束
     */
    addConstraint(constraintSet, constraint) {
        const id = `constraint-manual-${Date.now()}`;
        const newConstraint = { ...constraint, id };
        constraintSet.constraints.push(newConstraint);
        constraintSet.updatedAt = Date.now();
        return newConstraint;
    }
    /**
     * 移除约束
     */
    removeConstraint(constraintSet, constraintId) {
        const index = constraintSet.constraints.findIndex(c => c.id === constraintId);
        if (index === -1)
            return false;
        constraintSet.constraints.splice(index, 1);
        constraintSet.updatedAt = Date.now();
        return true;
    }
    /**
     * 更新约束
     */
    updateConstraint(constraintSet, constraintId, updates) {
        const constraint = constraintSet.constraints.find(c => c.id === constraintId);
        if (!constraint)
            return null;
        Object.assign(constraint, updates);
        constraintSet.updatedAt = Date.now();
        return constraint;
    }
    /**
     * 按类型过滤约束
     */
    filterByType(constraintSet, type) {
        return constraintSet.constraints.filter(c => c.type === type);
    }
    /**
     * 按优先级过滤约束
     */
    filterByPriority(constraintSet, priority) {
        return constraintSet.constraints.filter(c => c.priority === priority);
    }
    /**
     * 获取必须满足的约束
     */
    getMustConstraints(constraintSet) {
        return constraintSet.constraints.filter(c => c.priority === 'must');
    }
    /**
     * 获取可验证的约束
     */
    getVerifiableConstraints(constraintSet) {
        return constraintSet.constraints.filter(c => c.verifiable);
    }
    /**
     * 验证约束集完整性
     */
    validate(constraintSet) {
        const issues = [];
        if (constraintSet.constraints.length < this.config.minConstraints) {
            issues.push(`Too few constraints: ${constraintSet.constraints.length} < ${this.config.minConstraints}`);
        }
        if (this.config.requireVerifiable) {
            const verifiable = constraintSet.constraints.filter(c => c.verifiable);
            if (verifiable.length === 0) {
                issues.push('No verifiable constraints found');
            }
        }
        const mustConstraints = constraintSet.constraints.filter(c => c.priority === 'must');
        if (mustConstraints.length === 0) {
            issues.push('No must-have constraints defined');
        }
        return {
            valid: issues.length === 0,
            issues,
        };
    }
    /**
     * 更新配置
     */
    updateConfig(config) {
        this.config = { ...this.config, ...config };
    }
    /**
     * 获取配置
     */
    getConfig() {
        return { ...this.config };
    }
    /**
     * 发送事件
     */
    emitEvent(event) {
        this.emit('event', event);
    }
}
//# sourceMappingURL=ConstraintExtractor.js.map