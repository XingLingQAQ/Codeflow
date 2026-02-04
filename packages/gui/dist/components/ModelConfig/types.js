/**
 * ModelConfig Types - 模型配置组件类型定义
 */
// Phase 配置
export const PHASE_INFO = {
    vision: { icon: '💡', color: '#f59e0b', description: 'Build project vision and goals' },
    constraints: { icon: '🔒', color: '#8b5cf6', description: 'Extract constraints from requirements' },
    architecture: { icon: '🏗️', color: '#3b82f6', description: 'Design system architecture' },
    research: { icon: '🔍', color: '#06b6d4', description: 'Research technical solutions' },
    explore: { icon: '🧭', color: '#10b981', description: 'Explore implementation options' },
    review: { icon: '👁️', color: '#6366f1', description: 'Review and validate plans' },
    implement: { icon: '⚡', color: '#ec4899', description: 'Implement code changes' },
    qa: { icon: '✅', color: '#22c55e', description: 'Quality assurance and testing' },
};
// Agent 角色配置
export const AGENT_ROLE_INFO = {
    main: { icon: '🎯', color: '#3b82f6', description: 'Main orchestrator agent' },
    coder: { icon: '💻', color: '#22c55e', description: 'Code implementation agent' },
    research: { icon: '🔬', color: '#8b5cf6', description: 'Research and analysis agent' },
    check: { icon: '✅', color: '#f59e0b', description: 'Quality check agent' },
    dispatch: { icon: '📡', color: '#06b6d4', description: 'Task dispatch agent' },
};
// 任务类型配置
export const TASK_TYPE_INFO = {
    frontend: { icon: '🎨', label: 'Frontend' },
    backend: { icon: '⚙️', label: 'Backend' },
    algorithm: { icon: '🧮', label: 'Algorithm' },
    database: { icon: '🗄️', label: 'Database' },
    devops: { icon: '🔧', label: 'DevOps' },
};
// Provider 配置
export const PROVIDER_INFO = {
    anthropic: { icon: '🟠', color: '#d97706', name: 'Anthropic' },
    openai: { icon: '🟢', color: '#10b981', name: 'OpenAI' },
    google: { icon: '🔵', color: '#3b82f6', name: 'Google' },
    local: { icon: '🟣', color: '#8b5cf6', name: 'Local' },
};
// 格式化成本
export const formatCost = (cost) => {
    if (cost < 0.001)
        return `$${(cost * 1000).toFixed(3)}/1K`;
    return `$${cost.toFixed(4)}/1K`;
};
// 格式化上下文窗口
export const formatContextWindow = (tokens) => {
    if (tokens >= 1000000)
        return `${(tokens / 1000000).toFixed(1)}M`;
    if (tokens >= 1000)
        return `${(tokens / 1000).toFixed(0)}K`;
    return tokens.toString();
};
// 估算成本
export const estimateCost = (inputTokens, outputTokens, model) => {
    return (inputTokens / 1000) * model.costPerInputToken + (outputTokens / 1000) * model.costPerOutputToken;
};
//# sourceMappingURL=types.js.map