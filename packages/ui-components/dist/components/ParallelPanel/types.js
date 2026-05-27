/**
 * ParallelPanel Types - 并行模式面板类型定义
 */
// Worker 状态配置
export const WORKER_STATUS_CONFIG = {
    idle: { color: '#94a3b8', label: 'Idle', icon: '⏸️' },
    running: { color: '#3b82f6', label: 'Running', icon: '🔄' },
    completed: { color: '#22c55e', label: 'Completed', icon: '✅' },
    failed: { color: '#ef4444', label: 'Failed', icon: '❌' },
};
// 模型提供商颜色
export const PROVIDER_COLORS = {
    anthropic: '#d97706',
    openai: '#10b981',
    google: '#3b82f6',
    local: '#8b5cf6',
};
// 指标配置
export const METRIC_CONFIG = {
    quality: { label: 'Quality', icon: '⭐', color: '#f59e0b' },
    performance: { label: 'Performance', icon: '⚡', color: '#3b82f6' },
    maintainability: { label: 'Maintainability', icon: '🔧', color: '#8b5cf6' },
    security: { label: 'Security', icon: '🔒', color: '#22c55e' },
    overall: { label: 'Overall', icon: '📊', color: '#6366f1' },
};
// 格式化时间
export const formatDuration = (ms) => {
    if (ms < 1000)
        return `${ms}ms`;
    if (ms < 60000)
        return `${(ms / 1000).toFixed(1)}s`;
    return `${(ms / 60000).toFixed(1)}m`;
};
// 格式化时间戳
export const formatTimestamp = (ts) => {
    const date = new Date(ts);
    return date.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
};
//# sourceMappingURL=types.js.map