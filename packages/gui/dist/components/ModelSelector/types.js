/**
 * ModelSelector 组件类型定义
 */
/**
 * 提供商图标映射
 */
export const PROVIDER_ICONS = {
    anthropic: '🅰️',
    openai: '🤖',
    google: '🔷',
    local: '💻',
    custom: '⚙️',
};
/**
 * 提供商显示名称
 */
export const PROVIDER_NAMES = {
    anthropic: 'Anthropic',
    openai: 'OpenAI',
    google: 'Google',
    local: 'Local',
    custom: 'Custom',
};
/**
 * 能力显示名称
 */
export const CAPABILITY_NAMES = {
    reasoning: 'Reasoning',
    coding: 'Coding',
    review: 'Review',
    research: 'Research',
    frontend: 'Frontend',
    backend: 'Backend',
    algorithm: 'Algorithm',
    'simple-tasks': 'Simple Tasks',
    vision: 'Vision',
    'long-context': 'Long Context',
};
/**
 * 能力颜色映射
 */
export const CAPABILITY_COLORS = {
    reasoning: '#9C27B0',
    coding: '#2196F3',
    review: '#FF9800',
    research: '#4CAF50',
    frontend: '#E91E63',
    backend: '#00BCD4',
    algorithm: '#673AB7',
    'simple-tasks': '#607D8B',
    vision: '#FF5722',
    'long-context': '#795548',
};
/**
 * 格式化成本显示
 */
export function formatCost(cost) {
    const total = cost.input + cost.output;
    if (total < 1) {
        return `$${(total * 100).toFixed(1)}¢/M`;
    }
    return `$${total.toFixed(2)}/M`;
}
/**
 * 格式化上下文窗口
 */
export function formatContextWindow(tokens) {
    if (!tokens)
        return 'N/A';
    if (tokens >= 1000000) {
        return `${(tokens / 1000000).toFixed(1)}M`;
    }
    return `${Math.round(tokens / 1000)}K`;
}
//# sourceMappingURL=types.js.map