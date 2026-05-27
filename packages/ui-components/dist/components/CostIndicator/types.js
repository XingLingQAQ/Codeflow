/**
 * CostIndicator Types - 成本显示组件类型定义
 */
// 格式化成本
export const formatCost = (cost, currency = 'USD') => {
    if (currency === 'USD') {
        return `$${cost.toFixed(4)}`;
    }
    return `${cost.toFixed(4)} ${currency}`;
};
// 格式化 Token 数量
export const formatTokens = (tokens) => {
    if (tokens >= 1000000) {
        return `${(tokens / 1000000).toFixed(1)}M`;
    }
    if (tokens >= 1000) {
        return `${(tokens / 1000).toFixed(1)}K`;
    }
    return tokens.toString();
};
// 计算预算使用百分比
export const calculateBudgetPercentage = (current, budget) => {
    if (budget <= 0)
        return 0;
    return Math.min((current / budget) * 100, 100);
};
// 获取预算状态
export const getBudgetStatus = (percentage) => {
    if (percentage >= 90) {
        return { status: 'danger', color: '#ef4444', label: 'Over Budget' };
    }
    if (percentage >= 70) {
        return { status: 'warning', color: '#f59e0b', label: 'Near Limit' };
    }
    return { status: 'safe', color: '#22c55e', label: 'On Track' };
};
// Provider 颜色配置
export const PROVIDER_COLORS = {
    anthropic: '#d97706',
    openai: '#10b981',
    google: '#3b82f6',
    local: '#8b5cf6',
};
// 模型成本配置（每 1K tokens）
export const MODEL_COST_CONFIG = {
    'claude-3-opus': { input: 0.015, output: 0.075 },
    'claude-3-sonnet': { input: 0.003, output: 0.015 },
    'claude-3-haiku': { input: 0.00025, output: 0.00125 },
    'gpt-4o': { input: 0.005, output: 0.015 },
    'gpt-4o-mini': { input: 0.00015, output: 0.0006 },
    'gemini-1.5-pro': { input: 0.00125, output: 0.005 },
    'gemini-1.5-flash': { input: 0.000075, output: 0.0003 },
};
//# sourceMappingURL=types.js.map